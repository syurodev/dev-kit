package agenthub_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

var testIdlePolicy = agenthub.IdlePolicy{Enabled: true, ChildAfter: 10 * time.Minute, TopAfter: 30 * time.Minute}

// idleFixture is a parent and its child, each with a live runtime.
type idleFixture struct {
	svc          *agenthub.Service
	store        *agenthub.SQLStore
	parent, kid  agenthub.AgentSession
	closedEvents *int
}

func newIdleFixture(t *testing.T) *idleFixture {
	t.Helper()
	svc, st := newServiceForPreviewTest(t, agenthub.NewMockAdapter())
	closed := 0
	svc.OnSessionClosed = func(string) { closed++ }
	ctx := context.Background()
	cwd := t.TempDir()
	parent, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	kid, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: cwd, ParentSessionID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	return &idleFixture{svc: svc, store: st, parent: parent, kid: kid, closedEvents: &closed}
}

func (f *idleFixture) hasRuntime(id string) bool {
	_, ok := f.svc.RuntimeForTest(id)
	return ok
}

func (f *idleFixture) status(t *testing.T, id string) agenthub.SessionStatus {
	t.Helper()
	got, err := f.store.GetSession(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got.Status
}

func TestReleaseIdleReleasesAnIdleChildAndKeepsTheSession(t *testing.T) {
	f := newIdleFixture(t)
	now := time.Now().Add(11 * time.Minute)

	if n := f.svc.ReleaseIdle(context.Background(), now, testIdlePolicy); n != 1 {
		t.Fatalf("released %d, want 1 (the child; the parent is under its 30 minutes)", n)
	}
	if f.hasRuntime(f.kid.ID) {
		t.Fatal("the child's runtime is still open")
	}
	if !f.hasRuntime(f.parent.ID) {
		t.Fatal("the parent was released before its threshold")
	}
	if got := f.status(t, f.kid.ID); got != agenthub.SessionStatusIdle {
		t.Fatalf("child status = %q, want idle: the session is parked, not closed", got)
	}
	if *f.closedEvents != 0 {
		t.Fatal("a release fired OnSessionClosed")
	}
}

func TestReleaseIdleUsesTheLongerThresholdForATopLevelSession(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()

	f.svc.ReleaseIdle(ctx, time.Now().Add(11*time.Minute), testIdlePolicy) // takes the child
	if !f.hasRuntime(f.parent.ID) {
		t.Fatal("parent released at 11 minutes")
	}
	if n := f.svc.ReleaseIdle(ctx, time.Now().Add(31*time.Minute), testIdlePolicy); n != 1 || f.hasRuntime(f.parent.ID) {
		t.Fatalf("released %d, parent still open = %v; want the parent released at 31 minutes", n, f.hasRuntime(f.parent.ID))
	}
}

func TestReleaseIdleDoesNothingWhenDisabledOrUnset(t *testing.T) {
	f := newIdleFixture(t)
	far := time.Now().Add(24 * time.Hour)
	for name, p := range map[string]agenthub.IdlePolicy{
		"disabled":      {Enabled: false, ChildAfter: time.Minute, TopAfter: time.Minute},
		"no child time": {Enabled: true, TopAfter: time.Minute},
		"no top time":   {Enabled: true, ChildAfter: time.Minute},
	} {
		if n := f.svc.ReleaseIdle(context.Background(), far, p); n != 0 {
			t.Fatalf("%s released %d", name, n)
		}
	}
	if !f.hasRuntime(f.kid.ID) || !f.hasRuntime(f.parent.ID) {
		t.Fatal("a runtime was released")
	}
}

func TestReleaseIdleKeepsSessionsThatAreNotIdle(t *testing.T) {
	for _, status := range []agenthub.SessionStatus{agenthub.SessionStatusRunning, agenthub.SessionStatusAwaitingPermission} {
		t.Run(string(status), func(t *testing.T) {
			f := newIdleFixture(t)
			if err := f.store.UpdateSessionStatus(context.Background(), f.kid.ID, status, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			f.svc.ReleaseIdle(context.Background(), time.Now().Add(24*time.Hour), testIdlePolicy)
			if !f.hasRuntime(f.kid.ID) {
				t.Fatalf("a %s session was released", status)
			}
		})
	}
}

func TestReleaseIdleKeepsAParentWhileAChildWorks(t *testing.T) {
	f := newIdleFixture(t)
	if err := f.store.UpdateSessionStatus(context.Background(), f.kid.ID, agenthub.SessionStatusRunning, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	f.svc.ReleaseIdle(context.Background(), time.Now().Add(24*time.Hour), testIdlePolicy)
	if !f.hasRuntime(f.parent.ID) {
		t.Fatal("a parent whose child is running was released: the child's result is about to wake it")
	}
}

func TestReleaseIdleKeepsASessionUsedRecently(t *testing.T) {
	f := newIdleFixture(t)
	now := time.Now().Add(11 * time.Minute)
	f.svc.SetLastUsedForTest(f.kid.ID, now.Add(-time.Minute))
	if n := f.svc.ReleaseIdle(context.Background(), now, testIdlePolicy); n != 0 || !f.hasRuntime(f.kid.ID) {
		t.Fatalf("released %d: a runtime handed out a minute ago must stay", n)
	}
}

func TestReleaseIdleKeepsASessionTheVendorCannotReopen(t *testing.T) {
	f := newIdleFixture(t)
	// A conversation exists and the mock adapter cannot load it: a release would
	// be permanent (resumeRuntime answers ErrUnavailable).
	markPrompted(t, f.store, f.kid.ID)
	f.svc.ReleaseIdle(context.Background(), time.Now().Add(11*time.Minute), testIdlePolicy)
	if !f.hasRuntime(f.kid.ID) {
		t.Fatal("a session that could not be reopened was released")
	}
}

func TestReleaseIdleKeepsASessionWithAMessageOnTheWay(t *testing.T) {
	for _, state := range []string{"queued", "dispatching"} {
		t.Run(state, func(t *testing.T) {
			f := newIdleFixture(t)
			rec := agenthub.PeerOutboxRecord{ID: "msg-" + state, From: agenthub.MessageSource{Kind: "session", SessionID: f.parent.ID}, FromSession: f.parent.ID, To: f.kid.ID,
				At: time.Now().UTC(), UpdatedAt: time.Now().UTC(), State: state}
			if err := f.store.SavePeerOutbox(context.Background(), rec); err != nil {
				t.Fatal(err)
			}
			f.svc.ReleaseIdle(context.Background(), time.Now().Add(11*time.Minute), testIdlePolicy)
			if !f.hasRuntime(f.kid.ID) {
				t.Fatalf("a session with a %s peer message was released", state)
			}
		})
	}
}

func TestAReleasedSessionReopensOnTheNextPrompt(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	f.svc.ReleaseIdle(ctx, time.Now().Add(11*time.Minute), testIdlePolicy)
	if f.hasRuntime(f.kid.ID) {
		t.Fatal("the child was not released")
	}
	out, err := f.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: f.kid.ID, Prompt: "hello again"})
	if err != nil {
		t.Fatalf("invoke after release: %v", err)
	}
	if out.Status != "completed" {
		t.Fatalf("status = %q, want completed", out.Status)
	}
	if !f.hasRuntime(f.kid.ID) {
		t.Fatal("the session did not reopen")
	}
}

// A sweep running while callers use the same sessions must end every call in a
// turn or a reopen, never in "no runtime". Run under -race.
func TestReleaseIdleRacingWithInvokeNeverLosesACall(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	far := time.Now().Add(24 * time.Hour)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				f.svc.ReleaseIdle(ctx, far, testIdlePolicy)
			}
		}
	}()
	for i := 0; i < 12; i++ {
		if _, err := f.svc.Invoke(ctx, agenthub.InvokeInput{SessionID: f.kid.ID, Prompt: "ping"}); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("invoke %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}

func TestReleaseIdleKeepsASessionWithBackgroundWork(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	now := time.Now().Add(11 * time.Minute) // past the child's 10 minutes, short of the parent's 30
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	if n := f.svc.ReleaseIdle(ctx, now, testIdlePolicy); n != 0 || !f.hasRuntime(f.kid.ID) {
		t.Fatalf("released %d: a session whose build is still running must keep its process", n)
	}
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(doneShellJSON))
	if n := f.svc.ReleaseIdle(ctx, now, testIdlePolicy); n != 1 || f.hasRuntime(f.kid.ID) {
		t.Fatalf("released %d: once the build ended the idle session should go", n)
	}
}

func TestReleaseRuntimeRechecksBackgroundWorkUnderTheLock(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	now := time.Now().Add(11 * time.Minute)
	// The work starts after the sweep chose its candidates and before the release.
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	if released := f.svc.ReleaseRuntimeForTest(ctx, f.kid.ID, now, 10*time.Minute); released {
		t.Fatal("releaseRuntime released a session that has a task")
	}
}

// leakedClosed is a child whose row says closed while its process is still alive:
// the state a runtime opened for a closed session leaves. CloseSession would have
// removed the process, so the row is written directly.
func leakedClosed(t *testing.T) *idleFixture {
	t.Helper()
	f := newIdleFixture(t)
	if err := f.store.UpdateSessionStatus(context.Background(), f.kid.ID, agenthub.SessionStatusClosed, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if !f.hasRuntime(f.kid.ID) {
		t.Fatal("fixture: the child has no runtime")
	}
	return f
}

// A closed session has no use for a process, so the sweep releases it at once: no
// threshold (now is the present, far under the child's 10 minutes), the row stays
// closed, and nothing that closing does is done again.
func TestReleaseIdleReleasesAClosedSessionsProcessAtOnce(t *testing.T) {
	f := leakedClosed(t)
	ctx := context.Background()
	// A task the vendor started would keep an idle session's process. Closing kills it.
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	if n := f.svc.ReleaseIdle(ctx, time.Now(), testIdlePolicy); n != 1 {
		t.Fatalf("released %d, want 1 (the closed child)", n)
	}
	if f.hasRuntime(f.kid.ID) {
		t.Fatal("the closed session's process is still open")
	}
	if !f.hasRuntime(f.parent.ID) {
		t.Fatal("the idle parent, under its threshold, was released")
	}
	if got := f.status(t, f.kid.ID); got != agenthub.SessionStatusClosed {
		t.Fatalf("status = %q, want closed: a release never changes the row", got)
	}
	if *f.closedEvents != 0 {
		t.Fatal("a release fired OnSessionClosed")
	}
	if f.svc.HasBackgroundWorkForTest(f.kid.ID) {
		t.Fatal("the released process's task is still recorded")
	}
	if n := f.svc.ReleaseIdle(ctx, time.Now(), testIdlePolicy); n != 0 {
		t.Fatalf("a second sweep released %d", n)
	}
}

// A turn claim, or a resume in flight, means someone is about to use the process:
// even a closed session keeps it until they are done.
func TestReleaseIdleKeepsAClosedSessionsProcessWhileATurnOrAResumeHoldsIt(t *testing.T) {
	f := leakedClosed(t)
	ctx := context.Background()

	releaseClaim := f.svc.ClaimInvokeForTest(f.kid.ID)
	if n := f.svc.ReleaseIdle(ctx, time.Now(), testIdlePolicy); n != 0 || !f.hasRuntime(f.kid.ID) {
		t.Fatalf("released %d: a closed session whose turn claim is held keeps its process", n)
	}
	if f.svc.ReleaseRuntimeForTest(ctx, f.kid.ID, time.Now(), 0) {
		t.Fatal("releaseRuntime released a process whose turn claim is held")
	}
	releaseClaim()

	releaseResume := f.svc.SetResumingForTest(f.kid.ID)
	if n := f.svc.ReleaseIdle(ctx, time.Now(), testIdlePolicy); n != 0 || !f.hasRuntime(f.kid.ID) {
		t.Fatalf("released %d: a resume is in flight", n)
	}
	if f.svc.ReleaseRuntimeForTest(ctx, f.kid.ID, time.Now(), 0) {
		t.Fatal("releaseRuntime released a process whose resume is in flight")
	}
	releaseResume()

	if n := f.svc.ReleaseIdle(ctx, time.Now(), testIdlePolicy); n != 1 || f.hasRuntime(f.kid.ID) {
		t.Fatalf("released %d: once nobody held it the closed session's process should go", n)
	}
}

// What is waiting on a closed session's process (here a peer message on the way)
// is not dropped by the sweep: the same screen an idle session gets.
func TestReleaseIdleKeepsAClosedSessionWithAMessageOnTheWay(t *testing.T) {
	f := leakedClosed(t)
	rec := agenthub.PeerOutboxRecord{ID: "msg-closed", From: agenthub.MessageSource{Kind: "session", SessionID: f.parent.ID}, FromSession: f.parent.ID, To: f.kid.ID,
		At: time.Now().UTC(), UpdatedAt: time.Now().UTC(), State: "queued"}
	if err := f.store.SavePeerOutbox(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if n := f.svc.ReleaseIdle(context.Background(), time.Now(), testIdlePolicy); n != 0 || !f.hasRuntime(f.kid.ID) {
		t.Fatalf("released %d: a closed session with a peer message on the way was released", n)
	}
}

// Reopenable is the idle screen against a release that would be permanent. A closed
// session is not reopened by use but by a prompt, and the vendor that cannot load it
// lost it when it was closed: its process is released all the same.
func TestReleaseIdleReleasesAClosedSessionTheVendorCannotReopen(t *testing.T) {
	f := leakedClosed(t)
	markPrompted(t, f.store, f.kid.ID)
	if n := f.svc.ReleaseIdle(context.Background(), time.Now(), testIdlePolicy); n != 1 || f.hasRuntime(f.kid.ID) {
		t.Fatalf("released %d: a closed session's process was kept for a reopen that cannot happen", n)
	}
}
