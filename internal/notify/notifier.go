// Package notify raises OS notifications on behalf of any module.
//
// It is a leaf: it must not import any devkit/internal package. That is why
// Target is three strings of its own rather than the workbench's EntityRef —
// EntityRef carries a label, a project and a resolver registry that belong to
// the frontend, and the bridge translates between the two. The two types
// describing the same thing from opposite sides of a layer boundary is the
// intent, not duplication to be merged.
package notify

import (
	"log"
	"sync"
)

// Target names what a notification is about, and what a click on it opens.
// Module is the owning module's id ("agenthub", "tasks"), Kind the thing's
// kind within it ("session", "task"), ID its id.
type Target struct {
	Module string
	Kind   string
	ID     string
}

// Notification is one OS notification.
type Notification struct {
	Target Target
	Title  string
	Body   string
}

// Notifications is the OS notification service, as the Notifier needs
// it. Every method may block — macOS holds RequestAuthorization for as long as
// its dialog is up, up to three minutes — which is why only the Notifier's own
// goroutines call them.
type Notifications interface {
	// Start brings the service up. It fails where the OS has no notification
	// service to offer: no session D-Bus on Linux (WSL, minimal WMs,
	// containers), no bundle identifier on macOS (a plain `go build`).
	Start() error
	RequestAuthorization() (bool, error)
	// Send delivers n; update replaces the target's earlier notification
	// instead of adding another.
	Send(n Notification, update bool) error
}

// notifyQueueSize bounds how many notifications wait while the OS is slow to
// take them. Past it they are dropped: a notification is a nudge, and nothing
// that raises one may wait for room.
const notifyQueueSize = 32

type notifierState int

const (
	notifierUnasked notifierState = iota
	notifierAsking
	notifierAuthorized
	notifierUnavailable
)

// Notifier raises OS notifications without ever holding up what raised them.
//
// It used to run inline: the permission waiter notified before it started
// waiting for the answer, and Invoke notified in its deferred turn end, both
// under one mutex that also covered the first authorization request. While
// macOS showed its dialog, an Allow sat unread, a cancel went unseen, every
// other session's permission queued behind the mutex, and Invoke did not
// return. Now Notify only enqueues, delivery happens on one goroutine, and
// authorization is asked for only through Authorize — when the user turns the
// setting on or focuses the window, never on the path of a pending permission.
type Notifier struct {
	Desktop Notifications
	// Enabled is the user's setting; nil means on.
	Enabled func() bool
	// Focused reports whether the user is already looking at the app, in which
	// case nothing is shown. nil means never focused.
	Focused func() bool
	// Busy reports a permission or a question waiting on the user. Authorization is not
	// asked for then: the OS dialog would land on top of the prompt the user
	// has to answer. nil means never busy.
	Busy func() bool
	// Logf defaults to log.Printf.
	Logf func(format string, args ...any)

	queueOnce sync.Once
	queue     chan Notification
	gate      Gate

	mu      sync.Mutex
	state   notifierState
	started bool
}

// Notify queues a notification and returns at once. A nil Notifier — server
// mode, where there is no desktop — does nothing.
func (n *Notifier) Notify(target Target, title, body string) {
	if n == nil {
		return
	}
	n.queueOnce.Do(func() {
		n.queue = make(chan Notification, notifyQueueSize)
		go n.deliver()
	})
	select {
	case n.queue <- Notification{Target: target, Title: title, Body: body}:
	default:
	}
}

// Authorize asks the OS for permission to notify, at most once at a time and
// not again once granted or once the service turned out to be unavailable. The
// OS calls run on their own goroutine; the enabled and busy checks run on the
// caller's, so a caller on a UI thread should hop off it first.
func (n *Notifier) Authorize() {
	if n == nil || !n.enabled() || (n.Busy != nil && n.Busy()) {
		return
	}
	n.mu.Lock()
	if n.state != notifierUnasked {
		n.mu.Unlock()
		return
	}
	n.state = notifierAsking
	n.mu.Unlock()
	go n.authorize()
}

func (n *Notifier) authorize() {
	next := notifierUnasked
	defer func() {
		n.mu.Lock()
		n.state = next
		n.mu.Unlock()
	}()

	n.mu.Lock()
	started := n.started
	n.mu.Unlock()
	if !started {
		if err := n.Desktop.Start(); err != nil {
			// Off for the rest of the run, and said once: the app itself is fine.
			n.logf("notify: desktop notifications unavailable: %v", err)
			next = notifierUnavailable
			return
		}
		n.mu.Lock()
		n.started = true
		n.mu.Unlock()
	}
	allowed, err := n.Desktop.RequestAuthorization()
	if err != nil {
		n.logf("notify: desktop notification authorization: %v", err)
		return
	}
	if allowed {
		next = notifierAuthorized
	}
	// Refused stays unasked: asking again on a later focus costs nothing on
	// macOS, which shows its dialog once, and notices a grant made since in
	// System Settings.
}

func (n *Notifier) deliver() {
	for note := range n.queue {
		if !n.enabled() || (n.Focused != nil && n.Focused()) {
			continue
		}
		n.mu.Lock()
		authorized := n.state == notifierAuthorized
		n.mu.Unlock()
		if !authorized {
			continue
		}
		update := n.gate.Next(note.Target)
		if err := n.Desktop.Send(note, update); err != nil {
			if !update {
				n.gate.Reset(note.Target)
			}
			n.logf("notify: send desktop notification for %s/%s %s: %v",
				note.Target.Module, note.Target.Kind, note.Target.ID, err)
		}
	}
}

func (n *Notifier) enabled() bool {
	return n.Enabled == nil || n.Enabled()
}

func (n *Notifier) logf(format string, args ...any) {
	if n.Logf != nil {
		n.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}
