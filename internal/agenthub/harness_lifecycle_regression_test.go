package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
)

func workspaceServer(t *testing.T, enabled bool) func(string, string, string) (harness.Snapshot, error) {
	t.Helper()
	return func(s, ws, v string) (harness.Snapshot, error) {
		return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
			in.Workspace = &harness.Profile{Schema: 1, Revision: 1}
			if enabled {
				in.Workspace.Servers = []harness.Server{{Name: "db", Command: "command", Enabled: true}}
			}
		}), nil
	}
}

func TestCloseKeepsExecutableGuardForSameSessionReopen(t *testing.T) {
	for _, restart := range []bool{false, true} {
		for _, changed := range []bool{false, true} {
			name := "same-process"
			if restart {
				name = "restart"
			}
			if changed {
				name += "/new-executable"
			} else {
				name += "/unchanged"
			}
			t.Run(name, func(t *testing.T) {
				e := newLaunchEnv(t, plain)
				session := e.mustCreate()
				before, _, _ := e.svc.HarnessApplied(session.ID)
				if err := e.svc.CloseSession(context.Background(), session.ID); err != nil {
					t.Fatal(err)
				}
				durable, _, found := e.durable(session.ID)
				if !found || !durable.Equal(before) {
					t.Fatal("Close lost the last applied reference")
				}
				if changed {
					e.harness.set(workspaceServer(t, true))
				}
				svc := e.svc
				if restart {
					svc = e.restart()
				}
				_, err := svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: session.ID, Prompt: "continue"})
				if changed {
					if !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
						t.Fatalf("same session adopted a new executable: %v", err)
					}
				} else if err != nil {
					t.Fatalf("unchanged closed session could not reopen: %v", err)
				}
			})
		}
	}
}

// Conversation-only rewinds use real SQL and an adapter that supplies vendor
// message IDs. No filesystem restore or live vendor is needed for this defect.
type harnessRewindCommitObserver struct {
	*agenthub.SQLStore
	afterCommit func(agenthub.RewindSpec)
}

func (st *harnessRewindCommitObserver) RewindSession(ctx context.Context, spec agenthub.RewindSpec) error {
	if err := st.SQLStore.RewindSession(ctx, spec); err != nil {
		return err
	}
	if st.afterCommit != nil {
		st.afterCommit(spec)
	}
	return nil
}

func newHarnessRewindEnv(t *testing.T) (*launchEnv, agenthub.AgentSession, string, *harnessRewindCommitObserver) {
	t.Helper()
	e := newLaunchEnv(t, policy)
	reg := agenthub.NewRegistry()
	reg.Register(replyingAdapter{MockAdapter: agenthub.NewMockAdapter()})
	store := &harnessRewindCommitObserver{SQLStore: agenthub.NewSQLStore(e.db)}
	e.svc = agenthub.NewService(reg, store, func(string, json.RawMessage) {})
	e.svc.MatchingWorkspaces = func(string) ([]string, error) { return []string{"w1"}, nil }
	e.svc.MCPEndpoints = policy(e.ev)
	e.svc.Harness = e.harness
	e.harness.set(workspaceServer(t, true))
	session := e.mustCreate()
	for _, prompt := range []string{"first", "second"} {
		if _, err := e.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: session.ID, Prompt: prompt}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := agenthub.NewSQLStore(e.db).ListTurns(context.Background(), session.ID)
	if err != nil || len(rows) != 2 {
		t.Fatalf("turns = %d: %v", len(rows), err)
	}
	return e, session, rows[1].PromptMessageID, store
}

func TestRewindCommitsAppliedReferenceWithConversation(t *testing.T) {
	e, session, promptID, observer := newHarnessRewindEnv(t)
	committed := false
	observer.afterCommit = func(spec agenthub.RewindSpec) {
		committed = true
		durable, enforced, found := e.durable(session.ID)
		if spec.HarnessRef == nil || !found || !enforced || !durable.Equal(*spec.HarnessRef) {
			t.Fatal("cut committed before its applied reference")
		}
		mem, _, _ := e.svc.HarnessApplied(session.ID)
		if mem.Equal(durable) {
			t.Fatal("test boundary must be before runtime publication")
		}
		// A new process at this exact boundary sees the narrowed set even though
		// the original process has not published its runtime yet.
		e.harness.set(workspaceServer(t, true))
		restarted := e.restart()
		err := restarted.ResumeSession(context.Background(), session.ID, true)
		if !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
			t.Fatalf("restart between commit and publication bypassed guard: %v", err)
		}
		e.harness.set(workspaceServer(t, false))
	}
	e.harness.set(workspaceServer(t, false))
	preview, err := e.svc.PlanRewind(context.Background(), agenthub.RewindRequest{SessionID: session.ID, PromptMessageID: promptID, Mode: agenthub.RewindConversation})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.ApplyRewind(context.Background(), preview.Token, nil); err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("rewind did not reach its commit boundary")
	}
	durable, enforced, found := e.durable(session.ID)
	mem, _, _ := e.svc.HarnessApplied(session.ID)
	if !found || !enforced || !durable.Equal(mem) || len(durable.ExecutableEntries) != 0 {
		t.Fatalf("rewind did not persist its applied reference: %+v", durable)
	}
	rows, err := agenthub.NewSQLStore(e.db).ListTurns(context.Background(), session.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("turns = %d: %v", len(rows), err)
	}
	// Returning to the old executable set after a restart must be judged against
	// the narrowed set that rewind actually opened, not the older permissive set.
	e.harness.set(workspaceServer(t, true))
	restarted := e.restart()
	if _, err := restarted.Invoke(context.Background(), agenthub.InvokeInput{SessionID: session.ID, Prompt: "continue"}); !errors.Is(err, agenthub.ErrHarnessNewSessionRequired) {
		t.Fatalf("restart adopted a removed executable: %v", err)
	}
}

func TestRewindReferenceWriteFailureRollsBackConversation(t *testing.T) {
	for _, operation := range []string{"UPDATE", "INSERT", "DELETE"} {
		t.Run(operation, func(t *testing.T) {
			e, session, promptID, _ := newHarnessRewindEnv(t)
			ctx := context.Background()
			store := agenthub.NewSQLStore(e.db)
			before, err := store.GetSession(ctx, session.ID)
			if err != nil {
				t.Fatal(err)
			}
			oldRef, _, _ := e.svc.HarnessApplied(session.ID)
			e.harness.set(workspaceServer(t, false))
			if operation == "INSERT" {
				if err = store.DeleteHarnessApplied(ctx, session.ID); err != nil {
					t.Fatal(err)
				}
			}
			if operation == "DELETE" {
				e.svc.Harness = nil
			}
			preview, err := e.svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: session.ID, PromptMessageID: promptID, Mode: agenthub.RewindConversation})
			if err != nil {
				t.Fatal(err)
			}
			// Operation is a fixed test case, never untrusted SQL.
			if _, err = e.db.Exec("CREATE TRIGGER fail_ref BEFORE " + operation + " ON agent_session_harness_applied BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
				t.Fatal(err)
			}
			if _, err = e.svc.ApplyRewind(ctx, preview.Token, nil); err == nil {
				t.Fatal("rewind accepted a failed applied-reference write")
			}
			after, err := store.GetSession(ctx, session.ID)
			if err != nil || after.ACPSessionID != before.ACPSessionID {
				t.Fatalf("vendor id changed despite failed commit: %+v %v", after, err)
			}
			rows, err := store.ListTurns(ctx, session.ID)
			if err != nil || len(rows) != 2 {
				t.Fatalf("failed commit cut turns: %d %v", len(rows), err)
			}
			texts := transcriptTexts(t, store, session.ID)
			if len(texts) != 2 || texts[0] != "first" || texts[1] != "second" {
				t.Fatalf("failed commit cut transcript: %v", texts)
			}
			mem, _, ok := e.svc.HarnessApplied(session.ID)
			if !ok || !mem.Equal(oldRef) {
				t.Fatal("failed rewind published a new in-memory reference")
			}
			durable, _, found := e.durable(session.ID)
			if operation == "INSERT" {
				if found {
					t.Fatal("failed transaction created a durable reference")
				}
			} else if !found || !durable.Equal(oldRef) {
				t.Fatal("failed transaction changed the old durable reference")
			}
			endpoints := e.svc.MCPEndpoints.(*policyEndpoints)
			if len(endpoints.closed) != 2 {
				t.Fatalf("old and staged endpoints not closed: %v", endpoints.closed)
			}
		})
	}
}

// A rewind whose fork the launch gate would refuse must leave the running
// session as it is, as a reload does: the gate is judged before the live runtime
// and its endpoint are closed, not after the fork is already being opened.
func TestARewindTheLaunchGateRefusesLeavesTheRunningSessionAlone(t *testing.T) {
	changedServer := func(t *testing.T) func(string, string, string) (harness.Snapshot, error) {
		return func(s, ws, v string) (harness.Snapshot, error) {
			return snapshotFor(t, s, ws, v, func(in *harness.Inputs) {
				in.Workspace = &harness.Profile{Schema: 1, Revision: 2,
					Servers: []harness.Server{{Name: "db", Command: "a-different-command", Enabled: true}}}
			}), nil
		}
	}
	unreadable := func(string, string, string) (harness.Snapshot, error) {
		return harness.Snapshot{}, errors.New("the workspace profile could not be read")
	}
	for name, tc := range map[string]struct {
		next func(*testing.T) func(string, string, string) (harness.Snapshot, error)
		want error
	}{
		"a new workspace executable": {changedServer, agenthub.ErrHarnessNewSessionRequired},
		"an unresolvable snapshot":   {func(*testing.T) func(string, string, string) (harness.Snapshot, error) { return unreadable }, agenthub.ErrHarnessUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			e, session, promptID, _ := newHarnessRewindEnv(t)
			store := agenthub.NewSQLStore(e.db)
			oldRef, _, _ := e.svc.HarnessApplied(session.ID)
			opens := e.ev.count("endpoint.openWithPolicy")
			closes := e.ev.count("endpoint.close")

			preview, err := e.svc.PlanRewind(ctx, agenthub.RewindRequest{SessionID: session.ID, PromptMessageID: promptID, Mode: agenthub.RewindConversation})
			if err != nil {
				t.Fatal(err)
			}
			e.harness.set(tc.next(t))
			if _, err = e.svc.ApplyRewind(ctx, preview.Token, nil); !errors.Is(err, tc.want) {
				t.Fatalf("rewind = %v, want %v", err, tc.want)
			}

			if got := e.ev.count("endpoint.close"); got != closes {
				t.Fatalf("a refused rewind closed the running session's endpoint (%d closes before, %d after)", closes, got)
			}
			if got := e.ev.count("endpoint.openWithPolicy"); got != opens {
				t.Fatalf("a refused rewind staged another endpoint: %d -> %d", opens, got)
			}
			if mem, _, ok := e.svc.HarnessApplied(session.ID); !ok || !mem.Equal(oldRef) {
				t.Fatal("a refused rewind changed the applied reference")
			}
			if texts := transcriptTexts(t, store, session.ID); len(texts) != 2 {
				t.Fatalf("a refused rewind cut the transcript: %v", texts)
			}
			// The session still answers on the runtime it had: no reopen, so the
			// gate that refused the rewind is never asked again.
			e.harness.set(workspaceServer(t, true))
			if _, err := e.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: session.ID, Prompt: "still here"}); err != nil {
				t.Fatalf("the running session did not survive a refused rewind: %v", err)
			}
		})
	}
}
