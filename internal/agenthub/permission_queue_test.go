package agenthub

import (
	"context"
	"encoding/json"
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
