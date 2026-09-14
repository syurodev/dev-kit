package agenthub_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agenthub"
)

type sentNotification struct {
	agenthub.DesktopNotification
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

func (f *fakeDesktop) Send(n agenthub.DesktopNotification, update bool) error {
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

func authorizedNotifier(t *testing.T, desktop *fakeDesktop, focused *atomic.Bool) *agenthub.Notifier {
	t.Helper()
	desktop.authorized = true
	n := &agenthub.Notifier{Desktop: desktop, Focused: focused.Load}
	n.Authorize()
	eventually(t, "authorization", func() bool { return desktop.requests.Load() == 1 })
	return n
}

// Neither a hung authorization dialog nor a hung delivery may hold the caller:
// Notify runs on the permission path and in Invoke's deferred turn end.
func TestNotifierNeverBlocksItsCaller(t *testing.T) {
	desktop := &fakeDesktop{authorized: true, authBlock: make(chan struct{}), sendBlock: make(chan struct{})}
	t.Cleanup(func() { close(desktop.authBlock); close(desktop.sendBlock) })
	n := &agenthub.Notifier{Desktop: desktop, Focused: func() bool { return false }}

	done := make(chan struct{})
	go func() {
		n.Authorize() // the dialog never answers
		for i := 0; i < 1000; i++ {
			n.Notify("s1", "Permission needed", "waiting")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Authorize/Notify blocked on the desktop backend")
	}
}

// The regression itself: with delivery hung, an Allow still reaches the
// waiting agent at once, a second session's permission is not queued behind
// the first, and Invoke returns although its turn end notifies.
func TestPermissionsAndTurnsAreNotHeldByADesktopNotification(t *testing.T) {
	// Authorization answers; every delivery hangs.
	desktop := &fakeDesktop{authorized: true, sendBlock: make(chan struct{})}
	t.Cleanup(func() { close(desktop.sendBlock) })
	var focused atomic.Bool
	notifier := authorizedNotifier(t, desktop, &focused)

	waiter := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) {
		notifier.Notify(p.SessionID, "Permission needed", "The agent is waiting for your approval.")
	})
	svc := newTestService(t)
	svc.ResolvePermission = waiter.Resolve
	svc.OnTurnFinished = func(sessionID string, _ agenthub.SessionStatus, _ string) {
		notifier.Notify(sessionID, "Answer ready", "The agent finished its turn.")
	}

	ctx := context.Background()
	const sessions = 2
	results := make(chan error, sessions)
	for i := 0; i < sessions; i++ {
		sess, err := svc.CreateSession(ctx, agenthub.CreateInput{AgentType: agenthub.AgentTypeMock, Cwd: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			_, err := svc.Invoke(ctx, agenthub.InvokeInput{SessionID: sess.ID, Prompt: "NEED_PERMISSION"})
			results <- err
		}()
	}
	eventually(t, "both sessions' permission requests", func() bool { return len(waiter.PendingList()) == sessions })
	allow := json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`)
	for _, p := range waiter.PendingList() {
		if err := waiter.Complete(p.ID, allow); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < sessions; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("Invoke: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Invoke did not return while a desktop notification was hung")
		}
	}
}

// A backend that cannot start — no session D-Bus on Linux, no bundle
// identifier on macOS — turns notifications off and leaves everything else
// running; it is reported once, not per event.
func TestNotifierThatCannotStartSendsNothingAndSaysSoOnce(t *testing.T) {
	desktop := &fakeDesktop{startErr: errors.New("no session bus"), authorized: true}
	var logs atomic.Int32
	n := &agenthub.Notifier{
		Desktop: desktop,
		Focused: func() bool { return false },
		Logf:    func(string, ...any) { logs.Add(1) },
	}
	n.Authorize()
	eventually(t, "start attempt", func() bool { return logs.Load() == 1 })
	n.Authorize()
	n.Notify("s1", "Answer ready", "done")
	time.Sleep(50 * time.Millisecond)

	if got := desktop.starts.Load(); got != 1 {
		t.Fatalf("Start called %d times, want 1", got)
	}
	if got := desktop.requests.Load(); got != 0 {
		t.Fatalf("authorization requested %d times after a failed start", got)
	}
	if sent := desktop.sentList(); len(sent) != 0 {
		t.Fatalf("sent %v through a backend that never started", sent)
	}
	if got := logs.Load(); got != 1 {
		t.Fatalf("logged %d times, want 1", got)
	}
}

// Only while the window is unfocused, one thread per session (the first send
// creates it, later ones update it), carrying the session id a click opens.
func TestNotifierOnlyWhenUnfocusedAndGroupedBySession(t *testing.T) {
	desktop := &fakeDesktop{authorized: true}
	var focused atomic.Bool
	var focusChecks atomic.Int32
	focused.Store(true)
	n := &agenthub.Notifier{Desktop: desktop, Focused: func() bool {
		focusChecks.Add(1)
		return focused.Load()
	}}
	n.Authorize()
	eventually(t, "authorization", func() bool { return desktop.requests.Load() == 1 })

	n.Notify("seen", "Answer ready", "the user is looking")
	// Focus is checked at delivery; flip it only once that one was checked.
	eventually(t, "the focused delivery to be considered", func() bool { return focusChecks.Load() == 1 })
	focused.Store(false)
	n.Notify("s1", "Permission needed", "a")
	n.Notify("s1", "Answer ready", "b")
	n.Notify("s2", "Answer ready", "c")
	eventually(t, "three deliveries", func() bool { return len(desktop.sentList()) == 3 })

	want := []sentNotification{
		{agenthub.DesktopNotification{SessionID: "s1", Title: "Permission needed", Body: "a"}, false},
		{agenthub.DesktopNotification{SessionID: "s1", Title: "Answer ready", Body: "b"}, true},
		{agenthub.DesktopNotification{SessionID: "s2", Title: "Answer ready", Body: "c"}, false},
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
	n := &agenthub.Notifier{Desktop: desktop, Busy: busy.Load}

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
	n := &agenthub.Notifier{Desktop: desktop, Focused: func() bool { return false }, Enabled: func() bool {
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
	n.Notify("off", "Answer ready", "must not show")
	eventually(t, "the turned-off delivery to be considered", func() bool { return enabledChecks.Load() > checks })
	enabled.Store(true)
	n.Notify("on", "Answer ready", "shows")
	eventually(t, "a delivery", func() bool { return len(desktop.sentList()) == 1 })
	if got := desktop.sentList()[0].SessionID; got != "on" {
		t.Fatalf("delivered %q, want only the one sent while turned on", got)
	}
}
