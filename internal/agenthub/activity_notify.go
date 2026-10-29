package agenthub

import (
	"slices"
	"sync"
)

// activityNotifier tells the window that a session's activity (SessionActivity)
// changed, without ever running the hook on the caller's goroutine.
//
// bgwork changes under the Service's locks (forget runs with s.mu held), and a
// hook that emits a window event or reads a session could take those locks again.
// So a change only marks the session dirty and signals one goroutine, which on
// each wake reads every dirty session's snapshot and calls the hook with no lock
// held. Per-session order holds because one goroutine delivers; a burst of marks
// before a delivery becomes one call with the last state; a mark that arrives
// during a delivery is delivered by the next round, so the final state always
// arrives. No goroutine per update; the one goroutine starts on the first mark
// and ends with stop (Service.Shutdown).
type activityNotifier struct {
	snapshot func(sessionID string) SessionActivity
	fire     func(sessionID string, a SessionActivity)

	mu      sync.Mutex
	idle    *sync.Cond // signalled when a round ends or the notifier stops
	dirty   map[string]struct{}
	busy    bool // a round is delivering
	started bool
	closed  bool
	wake    chan struct{} // capacity 1: a pending wake already covers every mark
	quit    chan struct{}
	done    chan struct{}
}

func newActivityNotifier(snapshot func(string) SessionActivity, fire func(string, SessionActivity)) *activityNotifier {
	n := &activityNotifier{
		snapshot: snapshot,
		fire:     fire,
		dirty:    make(map[string]struct{}),
		wake:     make(chan struct{}, 1),
		quit:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	n.idle = sync.NewCond(&n.mu)
	return n
}

// mark says the session's snapshot changed. It never blocks on the hook.
func (n *activityNotifier) mark(sessionID string) {
	if n == nil {
		return
	}
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return
	}
	n.dirty[sessionID] = struct{}{}
	if !n.started {
		n.started = true
		go n.run()
	}
	n.mu.Unlock()
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

func (n *activityNotifier) run() {
	defer close(n.done)
	for {
		select {
		case <-n.quit:
			return
		case <-n.wake:
		}
		n.mu.Lock()
		ids := make([]string, 0, len(n.dirty))
		for id := range n.dirty {
			ids = append(ids, id)
		}
		clear(n.dirty)
		n.busy = len(ids) > 0
		n.mu.Unlock()
		slices.Sort(ids)
		for _, id := range ids {
			select {
			case <-n.quit:
				return
			default:
			}
			n.fire(id, n.snapshot(id))
		}
		n.mu.Lock()
		n.busy = false
		n.idle.Broadcast()
		n.mu.Unlock()
	}
}

// waitIdle blocks until nothing is waiting to be delivered, or the notifier
// stopped. For tests: production never waits on a notification.
func (n *activityNotifier) waitIdle() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for !n.closed && (n.busy || len(n.dirty) > 0) {
		n.idle.Wait()
	}
}

// stop ends the goroutine, waiting for a hook call in flight to return, and makes
// every later mark a no-op. Safe to call twice and when nothing was ever marked.
func (n *activityNotifier) stop() {
	if n == nil {
		return
	}
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		return
	}
	n.closed = true
	started := n.started
	close(n.quit)
	n.idle.Broadcast()
	n.mu.Unlock()
	if started {
		<-n.done
	}
}

// noteActivityChange tells the window about what an update did to a session's
// activity. Callable with s.mu held: it only marks the session.
func (s *Service) noteActivityChange(sessionID string, ch activityChange) {
	if ch.Changed() {
		s.activity.mark(sessionID)
	}
}

// forgetBackgroundWork drops a session's tasks because its process is gone and
// tells the window, since those tasks died with it. Callable with s.mu held.
func (s *Service) forgetBackgroundWork(sessionID string) {
	s.noteActivityChange(sessionID, s.bgwork.forget(sessionID))
}
