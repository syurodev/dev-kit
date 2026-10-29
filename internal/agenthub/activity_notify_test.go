package agenthub

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

type activityRecorder struct {
	mu      sync.Mutex
	calls   []activityCall
	gate    chan struct{} // when set, fire blocks until it is closed
	entered chan struct{} // when set, closed when fire is first entered
	once    sync.Once
}

type activityCall struct {
	id string
	a  SessionActivity
}

func (r *activityRecorder) fire(id string, a SessionActivity) {
	if r.entered != nil {
		r.once.Do(func() { close(r.entered) })
	}
	if r.gate != nil {
		<-r.gate
	}
	r.mu.Lock()
	r.calls = append(r.calls, activityCall{id, a})
	r.mu.Unlock()
}

func (r *activityRecorder) got() []activityCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]activityCall(nil), r.calls...)
}

// state is a settable per-session snapshot.
type activityState struct {
	mu sync.Mutex
	m  map[string]SessionActivity
}

func (s *activityState) set(id string, a SessionActivity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]SessionActivity{}
	}
	s.m[id] = a
}

func (s *activityState) snap(id string) SessionActivity {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[id]
}

func TestActivityNotifierCollapsesABurstToTheLastState(t *testing.T) {
	st := &activityState{}
	rec := &activityRecorder{gate: make(chan struct{})}
	n := newActivityNotifier(st.snap, rec.fire)
	defer n.stop()

	st.set("s1", SessionActivity{Tasks: 1})
	n.mark("s1") // the goroutine takes this one and blocks inside fire
	for i := 2; i <= 50; i++ {
		st.set("s1", SessionActivity{Tasks: i})
		n.mark("s1")
	}
	close(rec.gate)
	n.waitIdle()
	calls := rec.got()
	if len(calls) > 2 {
		t.Fatalf("a burst of 50 updates became %d calls: %+v", len(calls), calls)
	}
	if last := calls[len(calls)-1]; last.a != (SessionActivity{Tasks: 50}) {
		t.Fatalf("the final state was not delivered: %+v", last)
	}
}

func TestActivityNotifierKeepsEachSessionsOrderAndFinalState(t *testing.T) {
	st := &activityState{}
	rec := &activityRecorder{}
	n := newActivityNotifier(st.snap, rec.fire)
	defer n.stop()

	for i := 1; i <= 20; i++ {
		for _, id := range []string{"a", "b"} {
			st.set(id, SessionActivity{Tasks: i})
			n.mark(id)
		}
		if i%5 == 0 {
			n.waitIdle()
		}
	}
	n.waitIdle()
	last := map[string]int{}
	for _, c := range rec.got() {
		if c.a.Tasks < last[c.id] {
			t.Fatalf("session %s went backwards: %d after %d", c.id, c.a.Tasks, last[c.id])
		}
		last[c.id] = c.a.Tasks
	}
	if last["a"] != 20 || last["b"] != 20 {
		t.Fatalf("final states not delivered: %v", last)
	}
}

func TestActivityNotifierMarkNeverBlocksOnASlowHook(t *testing.T) {
	st := &activityState{}
	rec := &activityRecorder{gate: make(chan struct{}), entered: make(chan struct{})}
	n := newActivityNotifier(st.snap, rec.fire)
	st.set("s1", SessionActivity{Tasks: 1})
	n.mark("s1")
	<-rec.entered // the hook is inside its call and will not return until the gate opens
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			n.mark("s1")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("mark blocked behind a hook that had not returned")
	}
	close(rec.gate)
	n.stop()
}

func TestActivityNotifierStopsItsGoroutineAndIgnoresLaterMarks(t *testing.T) {
	st := &activityState{}
	rec := &activityRecorder{}
	n := newActivityNotifier(st.snap, rec.fire)
	st.set("s1", SessionActivity{Tasks: 1})
	n.mark("s1")
	n.waitIdle()
	select {
	case <-n.done:
		t.Fatal("the goroutine ended before stop")
	default:
	}
	n.stop()
	n.stop() // twice is fine
	select {
	case <-n.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the goroutine is still running after stop")
	}
	calls := len(rec.got())
	n.mark("s1")
	n.waitIdle() // returns at once once stopped
	time.Sleep(20 * time.Millisecond)
	if len(rec.got()) != calls {
		t.Fatal("a stopped notifier still called the hook")
	}
}

func TestActivityNotifierStopWithNothingMarkedStartsNothing(t *testing.T) {
	n := newActivityNotifier(func(string) SessionActivity { return SessionActivity{} }, func(string, SessionActivity) {})
	n.stop()
	n.mu.Lock()
	started := n.started
	n.mu.Unlock()
	if started {
		t.Fatal("a notifier nobody marked started a goroutine")
	}
}

func TestActivityNotifierStopDoesNotWaitForeverOnABlockedHook(t *testing.T) {
	st := &activityState{}
	rec := &activityRecorder{gate: make(chan struct{}), entered: make(chan struct{})}
	n := newActivityNotifier(st.snap, rec.fire)
	st.set("s1", SessionActivity{Tasks: 1})
	n.mark("s1")
	<-rec.entered
	stopped := make(chan struct{})
	go func() { n.stop(); close(stopped) }()
	// stop has closed the notifier before it waits for the hook; once it has, a mark is a no-op.
	for {
		n.mu.Lock()
		closed := n.closed
		n.mu.Unlock()
		if closed {
			break
		}
		runtime.Gosched()
	}
	select {
	case <-stopped:
		t.Fatal("stop returned while the hook was still inside its call")
	default:
	}
	close(rec.gate)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not return once the hook did")
	}
	select {
	case <-n.done:
	default:
		t.Fatal("the goroutine is still running after stop returned")
	}
}
