package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

// promptedSession is a session with two replied turns and S enabling vault.
func promptedSession(t *testing.T, r *compRig) (agenthub.AgentSession, []string) {
	t.Helper()
	r.workspaceOff("vault")
	sess, _ := r.create(agenthub.AgentTypeClaude)
	for _, p := range []string{"first", "second"} {
		if _, err := r.agents.Invoke(r.ui(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: p}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	turns, err := agenthub.NewSQLStore(r.db).ListTurns(context.Background(), sess.ID)
	if err != nil || len(turns) != 2 {
		t.Fatalf("turns = %d %v", len(turns), err)
	}
	return sess, []string{turns[0].PromptMessageID, turns[1].PromptMessageID}
}

func (r *compRig) rewind(sessionID, promptID string) error {
	r.t.Helper()
	preview, err := r.agents.PlanRewind(r.ui(), agenthub.RewindRequest{SessionID: sessionID, PromptMessageID: promptID, Mode: agenthub.RewindConversation})
	if err != nil {
		r.t.Fatal(err)
	}
	_, err = r.agents.ApplyRewind(r.ui(), preview.Token, nil)
	return err
}

func (r *compRig) turns(sessionID string) int {
	r.t.Helper()
	rows, err := agenthub.NewSQLStore(r.db).ListTurns(context.Background(), sessionID)
	if err != nil {
		r.t.Fatal(err)
	}
	return len(rows)
}

// Both rewind opens use the shared preparation: a fork at an earlier message, and
// a fresh open when there is nothing before the turn to branch from.
func TestBothRewindOpensServeTheDurableSAndKeepTheReference(t *testing.T) {
	for name, which := range map[string]int{"fork": 1, "fresh open": 0} {
		t.Run(name, func(t *testing.T) {
			r := newCompRig(t, compOptions{})
			sess, prompts := promptedSession(t, r)
			before := r.agents.ServingTarget(sess.ID).Incarnation
			v := r.vendors[agenthub.AgentTypeClaude]
			forks, opens := v.forks.Load(), v.opens.Load()
			if err := r.rewind(sess.ID, prompts[which]); err != nil {
				t.Fatal(err)
			}
			if name == "fork" && v.forks.Load() != forks+1 {
				t.Fatal("rewinding to a later turn forks the conversation")
			}
			if name == "fresh open" && v.opens.Load() != opens+1 {
				t.Fatal("rewinding to the first turn starts a fresh conversation through Open")
			}
			now := r.agents.ServingTarget(sess.ID)
			if now.Incarnation == before || !now.Acknowledged || now.Serving.Session.Revision != 1 {
				t.Fatalf("a rewind publishes a new incarnation serving the durable S: %+v", now)
			}
			if !contains(v.LastSpec().DevKit.Tools, "vault.status") {
				t.Fatal("the rewound launch must be prepared from the latest durable S")
			}
			if o, st := r.override(sess.ID); o == nil || st.Revision != 1 {
				t.Fatalf("a rewind keeps S: %+v %+v", o, st)
			}
			st, err := r.agents.HarnessStatus(context.Background(), sess.ID)
			if err != nil || st.Applied == nil || st.Applied.Revisions.Session == nil || *st.Applied.Revisions.Session != 1 {
				t.Fatalf("the committed reference describes the rewound launch: %+v %v", st, err)
			}
		})
	}
}

// The applied reference is written in the same transaction as the conversation
// cut: a failing write rolls the cut back, and the staged fork's endpoint is
// discarded without touching S.
func TestARewindWhoseReferenceWriteFailsCutsNothingAndDiscardsTheStagedEndpoint(t *testing.T) {
	for _, operation := range []string{"UPDATE", "INSERT"} {
		t.Run(operation, func(t *testing.T) {
			r := newCompRig(t, compOptions{})
			sess, prompts := promptedSession(t, r)
			if operation == "INSERT" {
				if err := agenthub.NewSQLStore(r.db).DeleteHarnessApplied(context.Background(), sess.ID); err != nil {
					t.Fatal(err)
				}
			}
			preview, err := r.agents.PlanRewind(r.ui(), agenthub.RewindRequest{SessionID: sess.ID, PromptMessageID: prompts[1], Mode: agenthub.RewindConversation})
			if err != nil {
				t.Fatal(err)
			}
			// Operation is a fixed test case, never untrusted SQL.
			if _, err := r.db.Exec("CREATE TRIGGER fail_ref BEFORE " + operation + " ON agent_session_harness_applied BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
				t.Fatal(err)
			}
			if _, err := r.agents.ApplyRewind(r.ui(), preview.Token, nil); err == nil {
				t.Fatal("a failed reference write must fail the rewind")
			}
			if r.turns(sess.ID) != 2 {
				t.Fatal("the conversation cut must roll back with the reference")
			}
			if v := r.agents.ServingTarget(sess.ID); v.Active {
				t.Fatalf("the staged fork's endpoint must be discarded, not left serving: %+v", v)
			}
			if o, st := r.override(sess.ID); o == nil || st.Revision != 1 {
				t.Fatalf("a failed rewind must not touch S: %+v %+v", o, st)
			}
		})
	}
}

// O-2b through the composed graph: a rolled-back build (no harness attached)
// that rewinds a session must not write, update or delete its applied reference.
// Every write to the table is made to fail, so any attempt would fail the rewind.
func TestARollbackBuildRewindLeavesTheAppliedReferenceAlone(t *testing.T) {
	r := newCompRig(t, compOptions{})
	sess, prompts := promptedSession(t, r)
	store := agenthub.NewSQLStore(r.db)
	before, enforced, found, err := store.GetHarnessApplied(context.Background(), sess.ID)
	if err != nil || !found {
		t.Fatalf("setup: the activated build must have recorded a reference: found=%v %v", found, err)
	}
	preview, err := r.agents.PlanRewind(r.ui(), agenthub.RewindRequest{SessionID: sess.ID, PromptMessageID: prompts[1], Mode: agenthub.RewindConversation})
	if err != nil {
		t.Fatal(err)
	}
	r.agents.Harness, r.agents.HarnessLifecycle = nil, nil
	for _, operation := range []string{"UPDATE", "INSERT", "DELETE"} {
		// Operation is a fixed test case, never untrusted SQL.
		if _, err := r.db.Exec("CREATE TRIGGER fail_" + operation + " BEFORE " + operation + " ON agent_session_harness_applied BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.agents.ApplyRewind(r.ui(), preview.Token, nil); err != nil {
		t.Fatalf("a rollback build's rewind must not touch the reference: %v", err)
	}
	if r.turns(sess.ID) != 1 {
		t.Fatal("the rewind did not cut")
	}
	after, afterEnforced, found, err := store.GetHarnessApplied(context.Background(), sess.ID)
	if err != nil || !found || !after.Equal(before) || afterEnforced != enforced {
		t.Fatalf("the reference changed or was removed: found=%v %v", found, err)
	}
}

// S changes while the fork is opening: the staged endpoint is updated like a
// published one, and the rewind is refused rather than committing a launch whose
// guidance and permissions were written for the old S.
func TestARewindWhoseSChangesWhileTheForkOpensIsRefusedAndCutsNothing(t *testing.T) {
	r := newCompRig(t, compOptions{})
	sess, prompts := promptedSession(t, r)
	v := r.vendors[agenthub.AgentTypeClaude]
	var applyErr error
	v.mu.Lock()
	v.onOpen = func() {
		v.mu.Lock()
		v.onOpen = nil
		v.mu.Unlock()
		_, applyErr = r.apply(sess.ID, nil, []string{"vault"})
	}
	v.mu.Unlock()
	err := r.rewind(sess.ID, prompts[1])
	if applyErr != nil {
		t.Fatalf("the change committed during the fork open must succeed: %v", applyErr)
	}
	if !errors.Is(err, agenthub.ErrHarnessLaunchStale) {
		t.Fatalf("want a stale launch refusal, got %v", err)
	}
	if r.turns(sess.ID) != 2 {
		t.Fatal("a refused rewind must not cut the conversation")
	}
	if o, st := r.override(sess.ID); o != nil || st.Present || st.Revision != 2 {
		t.Fatalf("the committed revoke stays committed: %+v %+v", o, st)
	}
	// The next open resolves it.
	if err := r.rewind(sess.ID, prompts[1]); err != nil {
		t.Fatal(err)
	}
	if contains(v.LastSpec().DevKit.Tools, "vault.status") {
		t.Fatal("the reopened launch must not serve a revoked group")
	}
}

// ---- account-driven reopen ------------------------------------------------------

func (r *compRig) fillBuiltin(pct float64) {
	r.t.Helper()
	id := agenthub.BuiltinAccountID(agenthub.AgentTypeClaude)
	if _, err := r.book.List(r.ctx, agenthub.AgentTypeClaude); err != nil {
		r.t.Fatal(err)
	}
	w := agenthub.LimitWindow{Label: "Weekly", UsedPercent: pct, ResetsAt: time.Now().Add(24 * time.Hour).Unix()}
	if err := r.book.SaveUsage(r.ctx, id, []agenthub.LimitWindow{w}, "observed", time.Now()); err != nil {
		r.t.Fatal(err)
	}
}

// A turn that moves the session to another account closes the endpoint and
// reopens on the target account's home. The reopen is the existing path: S
// survives, the launch is prepared again, and nothing about S moves the account.
func TestAnAccountMoveReopensThroughTheSharedPreparationAndKeepsS(t *testing.T) {
	r := newCompRig(t, compOptions{accounts: true})
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	if spec.AccountHome != "" {
		t.Fatalf("a new session runs as the built-in account, got %q", spec.AccountHome)
	}
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	// A live S change never moves the account.
	if got := r.vendors[agenthub.AgentTypeClaude].opens.Load(); got != 1 {
		t.Fatalf("a live S change must not reopen the runtime: opens=%d", got)
	}
	before := r.agents.ServingTarget(sess.ID).Incarnation
	r.fillBuiltin(95)
	if _, err := r.agents.Invoke(r.ui(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	moved := r.vendors[agenthub.AgentTypeClaude].LastSpec()
	if moved.AccountHome != r.home {
		t.Fatalf("the turn must run on the account it moved to: home %q want %q", moved.AccountHome, r.home)
	}
	now := r.agents.ServingTarget(sess.ID)
	if now.Incarnation == before || !now.Acknowledged || now.Serving.Session.Revision != 1 {
		t.Fatalf("the reopen publishes a new incarnation serving the durable S: %+v", now)
	}
	if !contains(moved.DevKit.Tools, "vault.status") {
		t.Fatal("the account reopen must resolve the durable S")
	}
	if o, st := r.override(sess.ID); o == nil || st.Revision != 1 {
		t.Fatalf("an account move keeps S: %+v %+v", o, st)
	}
}

var _ = harness.CodeGroupOff

// A move to another account closes the running process and reopens it through the
// launch gate. When the gate refuses (the workspace profile gained an executable
// since the session opened), the move must not start: the session keeps the
// runtime it has, stays on its account, and the target account is not blamed for
// a refusal that is about the workspace.
func TestAnAccountMoveTheLaunchGateWouldRefuseLeavesTheSessionAlone(t *testing.T) {
	r := newCompRig(t, compOptions{accounts: true})
	r.workspaceOff("vault")
	sess, _ := r.create(agenthub.AgentTypeClaude)
	v := r.vendors[agenthub.AgentTypeClaude]
	opens, before := v.opens.Load(), r.agents.ServingTarget(sess.ID).Incarnation

	// The workspace adds an executable of its own: a reopen of this session is now
	// refused ("open a new session"), so is any account move that reopens it.
	_, st, err := r.asm.Store.GetProfile(context.Background(), r.wsID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.asm.Store.Apply(context.Background(), harness.StoreWrite{WorkspaceID: r.wsID, Profile: &harness.ProfileWrite{Expect: st,
		Value: &harness.Profile{Schema: harness.SchemaVersion, ToolGroups: map[string]bool{"vault": false},
			Servers: []harness.Server{{Name: "db", Command: "/bin/db", Enabled: true}}}}}); err != nil {
		t.Fatal(err)
	}
	if err = r.agents.ResumeSession(r.ui(), sess.ID, true); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("the premise: a reopen must be refused, got %v", err)
	}

	r.fillBuiltin(95) // the session's account is nearly out: the next turn would move it
	if _, err = r.agents.Invoke(r.ui(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatalf("the turn must run on the runtime the session has: %v", err)
	}
	if got := v.opens.Load(); got != opens {
		t.Fatalf("a refused move reopened the runtime: opens %d -> %d", opens, got)
	}
	if now := r.agents.ServingTarget(sess.ID); now.Incarnation != before || !now.Acknowledged {
		t.Fatalf("the running endpoint was replaced: %+v", now)
	}
	row, err := agenthub.NewSQLStore(r.db).GetSession(context.Background(), sess.ID)
	if err != nil || row.AccountID != sess.AccountID {
		t.Fatalf("the session was moved off its account: %q -> %q (%v)", sess.AccountID, row.AccountID, err)
	}
}
