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
