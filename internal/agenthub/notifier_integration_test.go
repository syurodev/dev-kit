package agenthub_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/notify"
)

// blockingDesktop hangs every delivery, which is what macOS does while its own
// dialog is up. Authorization answers so the notifier reaches delivery at all.
type blockingDesktop struct {
	block chan struct{}

	mu   sync.Mutex
	sent []notify.Notification
}

func (d *blockingDesktop) Start() error                        { return nil }
func (d *blockingDesktop) RequestAuthorization() (bool, error) { return true, nil }

func (d *blockingDesktop) Send(n notify.Notification, _ bool) error {
	<-d.block
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sent = append(d.sent, n)
	return nil
}

func sessionTarget(id string) notify.Target {
	return notify.Target{Module: "agenthub", Kind: "session", ID: id}
}

// The regression itself, kept next to the code it is about: with delivery
// hung, an Allow still reaches the waiting agent at once, a second session's
// permission is not queued behind the first, and Invoke returns although its
// turn end notifies.
func TestPermissionsAndTurnsAreNotHeldByANotification(t *testing.T) {
	desktop := &blockingDesktop{block: make(chan struct{})}
	t.Cleanup(func() { close(desktop.block) })
	notifier := &notify.Notifier{Desktop: desktop, Focused: func() bool { return false }}
	notifier.Authorize()

	waiter := agenthub.NewPermissionWaiter(func(p agenthub.PendingPermission) {
		notifier.Notify(sessionTarget(p.SessionID), "Permission needed", "The agent is waiting for your approval.")
	})
	svc := newTestService(t)
	svc.ResolvePermission = waiter.Resolve
	svc.OnTurnFinished = func(sessionID string, _ agenthub.SessionStatus, _ string) {
		notifier.Notify(sessionTarget(sessionID), "Answer ready", "The agent finished its turn.")
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
	deadline := time.Now().Add(2 * time.Second)
	for len(waiter.PendingList()) < sessions {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for both sessions' permission requests")
		}
		time.Sleep(5 * time.Millisecond)
	}
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
