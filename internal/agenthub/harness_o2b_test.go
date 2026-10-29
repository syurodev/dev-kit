package agenthub_test

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/agenthub"
)

// O-2b (owner decision, 2026-10-29): a build without the harness (a rollback)
// must not erase what an activated build recorded. Rollback preserves data; a
// deleted reference would recreate O-2 on the next activation.

func TestARollbackReopenLeavesTheStoredReferenceByteForByte(t *testing.T) {
	ctx := context.Background()
	e := newLaunchEnv(t, policy)
	e.harness.set(workspaceServer(t, false))
	s := e.mustCreate()
	before := e.appliedRows()
	if len(before) != 1 {
		t.Fatalf("setup: rows = %v", before)
	}
	// Both reopen paths of an inert build: a restarted Invoke and a reload.
	rb := e.restart()
	rb.Harness = nil
	if _, err := rb.Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: "rolled back"}); err != nil {
		t.Fatal(err)
	}
	if err := rb.ResumeSession(ctx, s.ID, true); err != nil {
		t.Fatal(err)
	}
	if after := e.appliedRows(); !sameStrings(before, after) {
		t.Fatalf("a rollback build changed the stored reference:\nbefore %v\nafter  %v", before, after)
	}
}

// B as the owner intends it: after a rollback and a re-activation, a session that
// had a reference is judged against it (a new workspace executable needs a new
// session), and a session that never had one is refused by the no-baseline rule.
// Neither adopts a workspace executable because the inert build ran in between.
func TestAfterARollbackReactivationStillRefusesANewWorkspaceExecutable(t *testing.T) {
	ctx := context.Background()
	for name, setup := range map[string]func(e *launchEnv) agenthub.AgentSession{
		"a session that had a reference": func(e *launchEnv) agenthub.AgentSession {
			e.harness.set(workspaceServer(t, false))
			return e.mustCreate()
		},
		"a session that never had one": func(e *launchEnv) agenthub.AgentSession { return e.legacySession() },
	} {
		t.Run(name, func(t *testing.T) {
			e := newLaunchEnv(t, policy)
			s := setup(e)
			hadRow := len(e.appliedRows()) == 1

			// Rollback: the inert build opens and uses the session.
			e.harness.set(workspaceServer(t, false))
			rb := e.restart()
			rb.Harness = nil
			if _, err := rb.Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: "rolled back"}); err != nil {
				t.Fatalf("the rolled-back build must still work: %v", err)
			}
			if _, _, found := e.durable(s.ID); found != hadRow {
				t.Fatalf("the rollback build changed whether a reference exists: had=%v now=%v", hadRow, found)
			}

			// Re-activation with a workspace profile that now owns an executable.
			e.harness.set(workspaceServer(t, true))
			specs := len(e.adapter.specs)
			if _, err := e.restart().Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: "reactivated"}); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
				t.Fatalf("re-activation adopted a workspace executable: %v", err)
			}
			if len(e.adapter.specs) != specs {
				t.Fatal("the vendor was launched on the refused reopen")
			}
		})
	}
}

// The rewind commit of a rollback build writes no reference either (every write
// to the table is made to fail, so any attempt would fail the rewind).
func TestARollbackRewindLeavesTheStoredReferenceAlone(t *testing.T) {
	e, session, promptID, _ := newHarnessRewindEnv(t)
	ctx := context.Background()
	oldRef, enforced, found := e.durable(session.ID)
	if !found {
		t.Fatal("setup: no reference")
	}
	e.harness.set(workspaceServer(t, false))
	e.svc.Harness = nil
	preview, err := e.svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: session.ID, PromptMessageID: promptID, Mode: agenthub.RewindConversation})
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"UPDATE", "INSERT", "DELETE"} {
		// Operation is a fixed test case, never untrusted SQL.
		if _, err := e.db.Exec("CREATE TRIGGER fail_" + operation + " BEFORE " + operation + " ON agent_session_harness_applied BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.ApplyRewind(ctx, preview.Token, nil); err != nil {
		t.Fatalf("a rollback build's rewind must not touch the reference: %v", err)
	}
	got, gotEnforced, found := e.durable(session.ID)
	if !found || !got.Equal(oldRef) || gotEnforced != enforced {
		t.Fatalf("the reference changed or was removed: found=%v", found)
	}
}

// Where the harness IS attached and the session has no workspace, no reference
// belongs to it: such a row is still removed (the only deletion left).
func TestAnAttachedHarnessStillRemovesTheReferenceOfASessionWithNoWorkspace(t *testing.T) {
	ctx := context.Background()
	e := newLaunchEnv(t, policy)
	other := e.mustCreate()
	e.svc.MatchingWorkspaces = func(string) ([]string, error) { return nil, nil }
	s, err := e.svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// A row that cannot belong to a workspace-less session (only a leftover):
	// borrow a real reference from a session that has one.
	ref, enforced, found := e.durable(other.ID)
	if !found {
		t.Fatal("setup: no reference to borrow")
	}
	ref = ref.Clone()
	ref.SessionID = s.ID
	if err := agenthub.NewSQLStore(e.db).SetHarnessApplied(ctx, s.ID, ref, enforced, other.CreatedAt); err != nil {
		t.Fatal(err)
	}
	attached := e.restart()
	attached.MatchingWorkspaces = func(string) ([]string, error) { return nil, nil }
	if err := attached.ResumeSession(ctx, s.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, _, found := e.durable(s.ID); found {
		t.Fatal("an attached harness kept a reference for a session with no workspace")
	}
}
