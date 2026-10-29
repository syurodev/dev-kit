package agenthub_test

import (
	"context"
	"testing"

	"devkit/internal/agenthub"
)

// The rewind path of the O-2b rule (RewindSpec.HarnessClear): the harness is
// attached and the fork has no snapshot because the session has no workspace. A
// leftover row is deleted inside the rewind transaction, so a failing DELETE rolls
// the whole cut back and leaves the conversation and the reference as they were.
func TestAnAttachedRewindOfASessionWithNoWorkspaceRemovesItsReferenceInTheCutTransaction(t *testing.T) {
	for _, failDelete := range []bool{false, true} {
		name := "the row is deleted with the cut"
		if failDelete {
			name = "a failing DELETE rolls the cut back"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			e, withWorkspace, _, _ := newHarnessRewindEnv(t)
			store := agenthub.NewSQLStore(e.db)
			// A session with no workspace, with two prompts.
			e.svc.MatchingWorkspaces = func(string) ([]string, error) { return nil, nil }
			s, err := e.svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			for _, prompt := range []string{"first", "second"} {
				if _, err := e.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: s.ID, Prompt: prompt}); err != nil {
					t.Fatal(err)
				}
			}
			// A row that cannot belong to a workspace-less session (only a leftover).
			ref, enforced, found := e.durable(withWorkspace.ID)
			if !found {
				t.Fatal("setup: no reference to borrow")
			}
			ref = ref.Clone()
			ref.SessionID = s.ID
			if err := store.SetHarnessApplied(ctx, s.ID, ref, enforced, withWorkspace.CreatedAt); err != nil {
				t.Fatal(err)
			}
			turns, err := store.ListTurns(ctx, s.ID)
			if err != nil || len(turns) != 2 {
				t.Fatalf("turns = %d: %v", len(turns), err)
			}
			preview, err := e.svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: s.ID, PromptMessageID: turns[1].PromptMessageID, Mode: agenthub.RewindConversation})
			if err != nil {
				t.Fatal(err)
			}
			if failDelete {
				// Fixed test statement, never untrusted SQL.
				if _, err := e.db.Exec("CREATE TRIGGER fail_ref BEFORE DELETE ON agent_session_harness_applied BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
					t.Fatal(err)
				}
			}
			_, err = e.svc.ApplyRewind(ctx, preview.Token, nil)
			after, err2 := store.ListTurns(ctx, s.ID)
			if err2 != nil {
				t.Fatal(err2)
			}
			_, _, still := e.durable(s.ID)
			if failDelete {
				if err == nil {
					t.Fatal("a failed reference delete must fail the rewind")
				}
				if len(after) != 2 || !still {
					t.Fatalf("the cut did not roll back with the reference: turns=%d row=%v", len(after), still)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != 1 || still {
				t.Fatalf("the rewind must cut and remove the leftover row together: turns=%d row=%v", len(after), still)
			}
		})
	}
}
