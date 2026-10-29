package agenthub_test

import (
	"context"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

// newLoadableIdleFixture is newIdleFixture over a vendor that can load a
// conversation again. The plain mock cannot, so once a turn has run a session of
// it is (correctly) not reopenable and never released; a reopen test that runs a
// turn needs this one.
func newLoadableIdleFixture(t *testing.T) *idleFixture {
	t.Helper()
	svc, st := newServiceForPreviewTest(t, resumableMock{agenthub.NewMockAdapter()})
	closed := 0
	svc.OnSessionClosed = func(string) { closed++ }
	ctx := context.Background()
	parent, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return &idleFixture{svc: svc, store: st, parent: parent, closedEvents: &closed}
}

func TestAMarkedSessionIsReleasedAtTheEndOfItsTurn(t *testing.T) {
	f := newLoadableIdleFixture(t)
	ctx := context.Background()
	f.svc.MarkReopenForGrantForTest(f.parent.ID)
	if _, err := f.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: f.parent.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if f.hasRuntime(f.parent.ID) {
		t.Fatal("the marked session kept its old process: the folder would not reach its shell")
	}
	if f.svc.ReopenMarkedForTest(f.parent.ID) {
		t.Fatal("the mark outlived its turn")
	}
	if got := f.status(t, f.parent.ID); got != agenthub.SessionStatusIdle {
		t.Fatalf("status = %q: a release must leave the session idle, not closed", got)
	}
	if _, err := f.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: f.parent.ID, Prompt: "again"}); err != nil || !f.hasRuntime(f.parent.ID) {
		t.Fatalf("the next prompt did not reopen the session: %v", err)
	}
}

func TestAnUnmarkedSessionKeepsItsProcess(t *testing.T) {
	f := newLoadableIdleFixture(t)
	if _, err := f.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: f.parent.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if !f.hasRuntime(f.parent.ID) {
		t.Fatal("a session nobody marked was released at the end of a turn")
	}
}

func TestTheReopenGuards(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(t *testing.T, f *idleFixture){
		"a child is running": func(t *testing.T, f *idleFixture) {
			if err := f.store.UpdateSessionStatus(ctx, f.kid.ID, agenthub.SessionStatusRunning, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		},
		"a vendor task is running": func(t *testing.T, f *idleFixture) {
			f.svc.HandleUpdateForTest(ctx, f.parent.ID, []byte(spawnShellJSON))
		},
		"an agent-started turn is running": func(t *testing.T, f *idleFixture) {
			f.svc.HandleUpdateForTest(ctx, f.parent.ID, []byte(spawnTurnJSON))
		},
		"a peer message is queued": func(t *testing.T, f *idleFixture) {
			rec := agenthub.PeerOutboxRecord{ID: "msg-q", From: agenthub.MessageSource{Kind: "session", SessionID: f.kid.ID}, FromSession: f.kid.ID,
				To: f.parent.ID, At: time.Now().UTC(), UpdatedAt: time.Now().UTC(), State: "queued"}
			if err := f.store.SavePeerOutbox(ctx, rec); err != nil {
				t.Fatal(err)
			}
		},
		"the session itself is running": func(t *testing.T, f *idleFixture) {
			if err := f.store.UpdateSessionStatus(ctx, f.parent.ID, agenthub.SessionStatusRunning, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		},
		"the vendor cannot load the session again": func(t *testing.T, f *idleFixture) {
			markPrompted(t, f.store, f.parent.ID)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newIdleFixture(t)
			setup(t, f)
			f.svc.MarkReopenForGrantForTest(f.parent.ID)
			f.svc.ReopenAfterGrantForTest(ctx, f.parent.ID, time.Time{})
			if !f.hasRuntime(f.parent.ID) {
				t.Fatalf("released although %s: that would kill work or lose the conversation", name)
			}
			if f.svc.ReopenMarkedForTest(f.parent.ID) {
				t.Fatal("the mark stayed: a blocked reopen must not retry in a loop")
			}
		})
	}
}

func TestAGrantMarksASessionOnlyWhenTheVendorCanReopenIt(t *testing.T) {
	r := newGrantRig(t)
	ctx := agentCtx(r.root.ID)
	plan, _ := r.svc.PrepareWorkspaceWrite(ctx, "fsap-audit-service")
	res, err := r.svc.CommitWorkspaceWrite(ctx, plan)
	if err != nil || res.Effective.ShellAndFiles != agenthub.EffectAfterThisTurn {
		t.Fatalf("result = %+v, %v; a never-prompted mock session can be reopened", res, err)
	}
	if !r.svc.ReopenMarkedForTest(r.root.ID) {
		t.Fatal("the session was not marked")
	}
}

// The turn's claim is dropped before the end-of-turn step runs, so a prompt that
// arrives in between can claim the session and be running on this very process.
func TestAClaimedSessionIsNotReleasedForAGrant(t *testing.T) {
	f := newLoadableIdleFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: f.parent.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if !f.hasRuntime(f.parent.ID) {
		t.Fatal("setup: the session has no process")
	}
	releaseClaim := f.svc.ClaimSessionForTest(f.parent.ID)
	defer releaseClaim()
	f.svc.MarkReopenForGrantForTest(f.parent.ID)
	f.svc.ReopenAfterGrantForTest(ctx, f.parent.ID, time.Time{})
	if !f.hasRuntime(f.parent.ID) {
		t.Fatal("released a session whose next turn had already claimed it: that kills the turn")
	}
}

func TestARuntimeUsedAfterTheTurnEndedIsNotReleasedForAGrant(t *testing.T) {
	f := newLoadableIdleFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: f.parent.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	turnEnded := time.Now().Add(-time.Minute)
	f.svc.SetLastUsedForTest(f.parent.ID, time.Now()) // a later caller took the runtime
	f.svc.MarkReopenForGrantForTest(f.parent.ID)
	f.svc.ReopenAfterGrantForTest(ctx, f.parent.ID, turnEnded)
	if !f.hasRuntime(f.parent.ID) {
		t.Fatal("released a runtime that a caller took after the turn ended")
	}
	// The same call with the turn ending after that use releases it.
	f.svc.MarkReopenForGrantForTest(f.parent.ID)
	f.svc.ReopenAfterGrantForTest(ctx, f.parent.ID, time.Now().Add(time.Minute))
	if f.hasRuntime(f.parent.ID) {
		t.Fatal("a runtime last used before the turn ended should be released")
	}
}

// The window reads the session again when it hears the turn is over. If the old
// process is released only after that event, the read still lists the folder as
// waiting for a reopen that has already happened, and nothing asks again.
func TestAMarkedSessionIsReleasedBeforeTheTurnIsReportedFinished(t *testing.T) {
	f := newLoadableIdleFixture(t)
	f.svc.MarkReopenForGrantForTest(f.parent.ID)
	var finished, releasedAtEvent bool
	f.svc.OnTurnFinished = func(id string, _ agenthub.SessionStatus, _ string) {
		finished = true
		releasedAtEvent = !f.hasRuntime(id)
	}
	if _, err := f.svc.Invoke(context.Background(), agenthub.InvokeInput{SessionID: f.parent.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	if !finished {
		t.Fatal("OnTurnFinished did not fire")
	}
	if !releasedAtEvent {
		t.Fatal("the turn was reported finished while the old process was still held")
	}
}
