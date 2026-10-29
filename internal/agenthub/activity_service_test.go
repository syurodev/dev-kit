package agenthub_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

const (
	doneTurnJSON = `{"sessionUpdate":"async_task_state_update","asyncTaskId":"autonomous-turn-c1514d72","state":"completed"}`
)

// activityLog records OnActivityChanged calls.
type activityLog struct {
	mu    sync.Mutex
	calls []activityCall
}

type activityCall struct {
	id string
	a  agenthub.SessionActivity
}

func (l *activityLog) hook(id string, a agenthub.SessionActivity) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, activityCall{id, a})
}

func (l *activityLog) all() []activityCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]activityCall(nil), l.calls...)
}

func TestActivityHookSeesTheTurnOnThenOffOnce(t *testing.T) {
	f := newIdleFixture(t)
	log := &activityLog{}
	f.svc.OnActivityChanged = log.hook
	ctx := context.Background()

	f.svc.HandleUpdateForTest(ctx, f.parent.ID, []byte(spawnTurnJSON))
	f.svc.WaitActivityIdleForTest()
	f.svc.HandleUpdateForTest(ctx, f.parent.ID, []byte(doneTurnJSON))
	f.svc.WaitActivityIdleForTest()

	want := []activityCall{
		{f.parent.ID, agenthub.SessionActivity{AgentTurn: true}},
		{f.parent.ID, agenthub.SessionActivity{}},
	}
	got := log.all()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("hook calls = %+v, want %+v", got, want)
	}
}

func TestActivityHookIgnoresUpdatesThatChangeNothing(t *testing.T) {
	f := newIdleFixture(t)
	log := &activityLog{}
	f.svc.OnActivityChanged = log.hook
	ctx := context.Background()
	progress := `{"sessionUpdate":"async_task_progress","asyncTaskId":"t1","taskType":"shell"}`
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(progress))
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(doneShellJSON)) // an end for a task never seen
	f.svc.WaitActivityIdleForTest()
	if got := log.all(); len(got) != 0 {
		t.Fatalf("hook called for no change: %+v", got)
	}
}

// Releasing, closing, rewinding or switching the account of a process takes the
// service lock around forget. The hook may take that lock itself: a notification
// delivered on the caller's goroutine would deadlock here.
func TestActivityHookNeverRunsWithTheServiceLockHeld(t *testing.T) {
	f := newIdleFixture(t)
	ctx := context.Background()
	var hookRan sync.WaitGroup
	hookRan.Add(1)
	var once sync.Once
	f.svc.OnActivityChanged = func(id string, a agenthub.SessionActivity) {
		if !a.Zero() {
			return
		}
		f.svc.WithLockForTest(func() {}) // would block forever if the caller held it
		once.Do(hookRan.Done)
	}
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	f.svc.WaitActivityIdleForTest()

	done := make(chan struct{})
	go func() {
		f.svc.WithLockForTest(func() { f.svc.ForgetBackgroundWorkForTest(f.kid.ID) })
		hookRan.Wait()
		f.svc.WaitActivityIdleForTest()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the hook did not run, or deadlocked on the service lock")
	}
}

func TestActivityHookSeesAProcessGoingAway(t *testing.T) {
	f := newIdleFixture(t)
	log := &activityLog{}
	f.svc.OnActivityChanged = log.hook
	ctx := context.Background()
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnTurnJSON))
	f.svc.WaitActivityIdleForTest()
	if err := f.svc.CloseSession(ctx, f.kid.ID); err != nil {
		t.Fatal(err)
	}
	f.svc.WaitActivityIdleForTest()
	got := log.all()
	if len(got) == 0 || !got[len(got)-1].a.Zero() {
		t.Fatalf("closing the session left the last notification at %+v", got)
	}
}

func TestShutdownStopsTheActivityNotifier(t *testing.T) {
	f := newIdleFixture(t)
	log := &activityLog{}
	f.svc.OnActivityChanged = log.hook
	ctx := context.Background()
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(spawnShellJSON))
	f.svc.WaitActivityIdleForTest()
	if err := f.svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	n := len(log.all())
	f.svc.HandleUpdateForTest(ctx, f.kid.ID, []byte(doneShellJSON))
	f.svc.WaitActivityIdleForTest() // returns at once: stopped
	time.Sleep(20 * time.Millisecond)
	if len(log.all()) != n {
		t.Fatal("the hook was called after Shutdown")
	}
}
