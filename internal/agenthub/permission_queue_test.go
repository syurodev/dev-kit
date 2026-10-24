package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPermissionWaiterQueuesTwoSessions(t *testing.T) {
	w := NewPermissionWaiter(nil)
	params := json.RawMessage(`{"toolCall":{}}`)
	results := make(chan error, 2)
	for _, sid := range []string{"s1", "s2"} {
		go func(sid string) {
			_, err := w.Wait(context.Background(), sid, params)
			results <- err
		}(sid)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(w.PendingList()) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("pending = %d, want 2 — second session must queue, not be denied", len(w.PendingList()))
		}
		time.Sleep(2 * time.Millisecond)
	}
	for _, p := range w.PendingList() {
		if err := w.Complete(p.ID, json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatalf("wait %d: %v", i, err)
		}
	}
}

func TestPermissionBrokerQueuesParallelGatesAndStopDeniesAll(t *testing.T) {
	notify := make(chan PendingPermission, 2)
	w := NewPermissionWaiter(func(p PendingPermission) { notify <- p })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := w.ResolveGate(ctx, "same-session", json.RawMessage(`{"origin":"devkit_gate"}`))
			done <- err
		}()
	}
	for range 2 {
		select {
		case p := <-notify:
			if p.ExpiresAt.IsZero() {
				t.Fatal("gate deadline missing")
			}
		case <-time.After(time.Second):
			t.Fatal("parallel gate not queued")
		}
	}
	if w.CancelSession("same-session") != 2 {
		t.Fatal("stop did not cancel both gates")
	}
	for range 2 {
		if err := <-done; err == nil {
			t.Fatal("stopped gate accepted")
		}
	}
	if len(w.PendingList()) != 0 {
		t.Fatal("stopped card remains pending")
	}
}

// A request whose ctx ends announces itself closed exactly once, and answering
// it afterwards is the published "no longer waiting" error, not a bare string.
func TestPermissionWaiterAnnouncesWithdrawnRequestOnceAndCompleteReportsIt(t *testing.T) {
	closed := make(chan PermissionClosed, 4)
	notify := make(chan PendingPermission, 1)
	w := NewPermissionWaiter(func(p PendingPermission) { notify <- p })
	w.OnClosed = func(c PermissionClosed) { closed <- c }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := w.Wait(ctx, "s1", json.RawMessage(`{"toolCall":{}}`))
		done <- err
	}()
	p := <-notify
	cancel()
	if err := <-done; !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("Wait = %v, want ErrPermissionDenied", err)
	}
	got := <-closed
	if got.ID != p.ID || got.SessionID != "s1" || got.Reason != PermissionWithdrawn {
		t.Fatalf("closed = %+v, want id %s, session s1, reason withdrawn", got, p.ID)
	}
	if err := w.Complete(p.ID, json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`)); !errors.Is(err, ErrPermissionNotWaiting) {
		t.Fatalf("Complete after close = %v, want ErrPermissionNotWaiting", err)
	}
	select {
	case extra := <-closed:
		t.Fatalf("closed fired twice: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPermissionWaiterAnnouncesAnswerAndStopOnce(t *testing.T) {
	closed := make(chan PermissionClosed, 4)
	notify := make(chan PendingPermission, 2)
	w := NewPermissionWaiter(func(p PendingPermission) { notify <- p })
	w.OnClosed = func(c PermissionClosed) { closed <- c }
	for _, sid := range []string{"a", "b"} {
		go func(sid string) { _, _ = w.Wait(context.Background(), sid, json.RawMessage(`{}`)) }(sid)
	}
	first, second := <-notify, <-notify
	answered, stopped := first, second
	if first.SessionID != "a" {
		answered, stopped = second, first
	}
	if err := w.Complete(answered.ID, json.RawMessage(`{"outcome":{"outcome":"selected","optionId":"allow"}}`)); err != nil {
		t.Fatal(err)
	}
	if n := w.CancelSession(stopped.SessionID); n != 1 {
		t.Fatalf("CancelSession = %d, want 1", n)
	}
	reasons := map[string]string{}
	for range 2 {
		select {
		case c := <-closed:
			reasons[c.ID] = c.Reason
		case <-time.After(time.Second):
			t.Fatalf("only %d closed events", len(reasons))
		}
	}
	if reasons[answered.ID] != PermissionAnswered || reasons[stopped.ID] != PermissionTurnEnded {
		t.Fatalf("reasons = %v", reasons)
	}
	select {
	case extra := <-closed:
		t.Fatalf("closed fired again: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}
