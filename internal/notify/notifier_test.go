package notify_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/notify"
)

type sentNotification struct {
	notify.Notification
	update bool
}

// fakeDesktop is the OS side. authBlock and sendBlock, when non-nil, hold
// their call until closed — what macOS does to RequestNotificationAuthorization
// while its dialog is up, for up to three minutes.
type fakeDesktop struct {
	startErr   error
	authorized bool
	authBlock  chan struct{}
	sendBlock  chan struct{}

	starts   atomic.Int32
	requests atomic.Int32
	mu       sync.Mutex
	sent     []sentNotification
}

func (f *fakeDesktop) Start() error {
	f.starts.Add(1)
	return f.startErr
}

func (f *fakeDesktop) RequestAuthorization() (bool, error) {
	f.requests.Add(1)
	if f.authBlock != nil {
		<-f.authBlock
	}
	return f.authorized, nil
}

func (f *fakeDesktop) Send(n notify.Notification, update bool) error {
	if f.sendBlock != nil {
		<-f.sendBlock
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentNotification{n, update})
	return nil
}

func (f *fakeDesktop) sentList() []sentNotification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentNotification(nil), f.sent...)
}

// session is the target agenthub uses; the tests keep using it because the
// behaviour under test (one thread per target) is the same behaviour.
func session(id string) notify.Target {
	return notify.Target{Module: "agenthub", Kind: "session", ID: id}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Neither a hung authorization dialog nor a hung delivery may hold the caller:
// Notify runs on the permission path and in Invoke's deferred turn end.
func TestNotifierNeverBlocksItsCaller(t *testing.T) {
	desktop := &fakeDesktop{authorized: true, authBlock: make(chan struct{}), sendBlock: make(chan struct{})}
	t.Cleanup(func() { close(desktop.authBlock); close(desktop.sendBlock) })
	n := &notify.Notifier{Desktop: desktop, Focused: func() bool { return false }}

	done := make(chan struct{})
	go func() {
		n.Authorize() // the dialog never answers
		for i := 0; i < 1000; i++ {
			n.Notify(session("s1"), "Permission needed", "waiting")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Authorize/Notify blocked on the desktop backend")
	}
}

// A backend that cannot start — no session D-Bus on Linux, no bundle
// identifier on macOS — turns notifications off and leaves everything else
// running; it is reported once, not per event.
func TestNotifierThatCannotStartSendsNothingAndSaysSoOnce(t *testing.T) {
	desktop := &fakeDesktop{startErr: errors.New("no session bus"), authorized: true}
	var logs, considered atomic.Int32
	n := &notify.Notifier{
		Desktop: desktop,
		Focused: func() bool {
			considered.Add(1)
			return false
		},
		Logf: func(string, ...any) { logs.Add(1) },
	}
	n.Authorize()
	eventually(t, "start attempt", func() bool { return logs.Load() == 1 })
	n.Authorize()
	// Delivery is one goroutine in queue order: once the probe queued behind
	// it has been considered, the first notification's turn — and any Send it
	// made — is over.
	n.Notify(session("s1"), "Answer ready", "done")
	n.Notify(session("probe"), "Answer ready", "probe")
	eventually(t, "both notifications to be considered", func() bool { return considered.Load() == 2 })

	if got := desktop.starts.Load(); got != 1 {
		t.Fatalf("Start called %d times, want 1", got)
	}
	if got := desktop.requests.Load(); got != 0 {
		t.Fatalf("authorization requested %d times after a failed start", got)
	}
	for _, sent := range desktop.sentList() {
		if sent.Target == session("s1") {
			t.Fatalf("sent %v through a backend that never started", sent)
		}
	}
	if got := logs.Load(); got != 1 {
		t.Fatalf("logged %d times, want 1", got)
	}
}

// Only while the window is unfocused, one thread per target (the first send
// creates it, later ones update it), carrying the target a click opens.
func TestNotifierOnlyWhenUnfocusedAndGroupedByTarget(t *testing.T) {
	desktop := &fakeDesktop{authorized: true}
	var focused atomic.Bool
	var focusChecks atomic.Int32
	focused.Store(true)
	n := &notify.Notifier{Desktop: desktop, Focused: func() bool {
		focusChecks.Add(1)
		return focused.Load()
	}}
	n.Authorize()
	eventually(t, "authorization", func() bool { return desktop.requests.Load() == 1 })

	n.Notify(session("seen"), "Answer ready", "the user is looking")
	// Focus is checked at delivery; flip it only once that one was checked.
	eventually(t, "the focused delivery to be considered", func() bool { return focusChecks.Load() == 1 })
	focused.Store(false)
	n.Notify(session("s1"), "Permission needed", "a")
	n.Notify(session("s1"), "Answer ready", "b")
	n.Notify(session("s2"), "Answer ready", "c")
	eventually(t, "three deliveries", func() bool { return len(desktop.sentList()) == 3 })

	want := []sentNotification{
		{notify.Notification{Target: session("s1"), Title: "Permission needed", Body: "a"}, false},
		{notify.Notification{Target: session("s1"), Title: "Answer ready", Body: "b"}, true},
		{notify.Notification{Target: session("s2"), Title: "Answer ready", Body: "c"}, false},
	}
	got := desktop.sentList()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delivery %d = %+v, want %+v (all: %+v)", i, got[i], want[i], got)
		}
	}
}

// The OS dialog must not land on top of a permission the user has to answer.
func TestNotifierDoesNotAskForAuthorizationWhileAPermissionWaits(t *testing.T) {
	desktop := &fakeDesktop{authorized: true}
	var busy atomic.Bool
	busy.Store(true)
	n := &notify.Notifier{Desktop: desktop, Busy: busy.Load}

	n.Authorize()
	if got := desktop.requests.Load(); got != 0 || desktop.starts.Load() != 0 {
		t.Fatalf("asked the OS (%d requests) while a permission was waiting", got)
	}
	busy.Store(false)
	n.Authorize() // the next focus
	eventually(t, "authorization once nothing waits", func() bool { return desktop.requests.Load() == 1 })
}

func TestNotifierSendsNothingWhileTurnedOff(t *testing.T) {
	desktop := &fakeDesktop{authorized: true}
	var enabled atomic.Bool
	var enabledChecks atomic.Int32
	n := &notify.Notifier{Desktop: desktop, Focused: func() bool { return false }, Enabled: func() bool {
		enabledChecks.Add(1)
		return enabled.Load()
	}}

	n.Authorize()
	if got := desktop.requests.Load(); got != 0 {
		t.Fatalf("asked for authorization %d times while turned off", got)
	}

	enabled.Store(true)
	n.Authorize() // what turning it on in Settings does
	eventually(t, "authorization once turned on", func() bool { return desktop.requests.Load() == 1 })

	enabled.Store(false)
	checks := enabledChecks.Load()
	n.Notify(session("off"), "Answer ready", "must not show")
	eventually(t, "the turned-off delivery to be considered", func() bool { return enabledChecks.Load() > checks })
	enabled.Store(true)
	n.Notify(session("on"), "Answer ready", "shows")
	eventually(t, "a delivery", func() bool { return len(desktop.sentList()) == 1 })
	if got := desktop.sentList()[0].Target; got != session("on") {
		t.Fatalf("delivered %+v, want only the one sent while turned on", got)
	}
}
