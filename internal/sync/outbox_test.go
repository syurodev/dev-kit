package sync

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMemoryOutboxEnqueueIsIdempotentAndCopiesPayload(t *testing.T) {
	outbox := NewMemoryOutbox()
	now := time.Unix(100, 0).UTC()
	change := validChange("key-1", now)
	if err := outbox.Enqueue(change); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	change.Payload[0] = 0xff
	if err := outbox.Enqueue(validChange("key-1", now)); err != nil {
		t.Fatalf("same change enqueue: %v", err)
	}
	conflicting := validChange("key-1", now)
	conflicting.Payload = []byte{0x03}
	if err := outbox.Enqueue(conflicting); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflicting enqueue error = %v, want %v", err, ErrIdempotencyConflict)
	}
	got, err := outbox.Get("key-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Payload[0] != 0x01 {
		t.Fatalf("payload was not copied: %x", got.Payload)
	}
	got.Payload[0] = 0xee
	again, _ := outbox.Get("key-1")
	if again.Payload[0] != 0x01 {
		t.Fatalf("public state exposed payload: %x", again.Payload)
	}
}

func TestMemoryOutboxPendingOrderAndLimit(t *testing.T) {
	outbox := NewMemoryOutbox()
	base := time.Unix(100, 0).UTC()
	for _, change := range []Change{
		validChange("b", base),
		validChange("a", base),
		validChange("c", base.Add(time.Minute)),
	} {
		if err := outbox.Enqueue(change); err != nil {
			t.Fatalf("enqueue %s: %v", change.IdempotencyKey, err)
		}
	}
	changes, err := outbox.Pending(base, 0)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if got := []string{changes[0].IdempotencyKey, changes[1].IdempotencyKey}; got[0] != "a" || got[1] != "b" {
		t.Fatalf("pending order = %v, want [a b]", got)
	}
	limited, err := outbox.Pending(base, 1)
	if err != nil || len(limited) != 1 || limited[0].IdempotencyKey != "a" {
		t.Fatalf("limited pending = %+v, err %v", limited, err)
	}
}

func TestMemoryOutboxRetryFailureAndSent(t *testing.T) {
	outbox := NewMemoryOutbox()
	now := time.Unix(100, 0).UTC()
	if err := outbox.Enqueue(validChange("retry", now)); err != nil {
		t.Fatal(err)
	}
	if err := outbox.MarkSyncing("retry"); err != nil {
		t.Fatal(err)
	}
	next := now.Add(time.Hour)
	if err := outbox.MarkRetry("retry", next, FailureTimeout); err != nil {
		t.Fatal(err)
	}
	got, _ := outbox.Get("retry")
	if got.Status != StatusPending || got.Attempts != 1 || got.LastError != FailureTimeout || !got.NextAttemptAt.Equal(next) {
		t.Fatalf("retry state = %+v", got)
	}
	ready, _ := outbox.Pending(now, 0)
	if len(ready) != 0 {
		t.Fatalf("retry was returned before next attempt: %+v", ready)
	}
	if err := outbox.MarkSyncing("retry"); err != nil {
		t.Fatal(err)
	}
	if err := outbox.MarkSent("retry"); err != nil {
		t.Fatal(err)
	}
	got, _ = outbox.Get("retry")
	if got.Status != StatusSynced || got.LastError != "" || !got.NextAttemptAt.IsZero() {
		t.Fatalf("sent state = %+v", got)
	}

	if err := outbox.Enqueue(validChange("failed", now)); err != nil {
		t.Fatal(err)
	}
	if err := outbox.MarkFailed("failed", FailureNetwork); err != nil {
		t.Fatal(err)
	}
	failed, _ := outbox.Get("failed")
	if failed.Status != StatusFailed || failed.Attempts != 1 || failed.LastError != FailureNetwork {
		t.Fatalf("failed state = %+v", failed)
	}
	if ready, _ := outbox.Pending(now.Add(time.Hour), 0); len(ready) != 0 {
		t.Fatalf("failed change was returned as pending: %+v", ready)
	}
}

func TestMemoryOutboxConflict(t *testing.T) {
	outbox := NewMemoryOutbox()
	now := time.Unix(100, 0).UTC()
	if err := outbox.Enqueue(validChange("local-1", now)); err != nil {
		t.Fatal(err)
	}
	conflict := Conflict{
		ID:              "conflict-1",
		RecordID:        "record-1",
		RecordType:      "note",
		LocalChangeKey:  "local-1",
		RemoteChangeKey: "remote-1",
		DetectedAt:      now,
	}
	if err := outbox.MarkConflict("local-1", conflict); err != nil {
		t.Fatalf("mark conflict: %v", err)
	}
	got, _ := outbox.Get("local-1")
	if got.Status != StatusConflict || got.Conflict == nil || got.Conflict.RemoteChangeKey != "remote-1" {
		t.Fatalf("conflict state = %+v", got)
	}
	if pending, _ := outbox.Pending(now, 0); len(pending) != 0 {
		t.Fatalf("conflict returned as pending: %+v", pending)
	}
}

func TestMemoryOutboxIsThreadSafe(t *testing.T) {
	outbox := NewMemoryOutbox()
	now := time.Unix(100, 0).UTC()
	var wait sync.WaitGroup
	for i := 0; i < 20; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			change := validChange("key-"+string(rune('a'+i)), now)
			if err := outbox.Enqueue(change); err != nil {
				t.Errorf("enqueue: %v", err)
			}
			if _, err := outbox.Get(change.IdempotencyKey); err != nil {
				t.Errorf("get: %v", err)
			}
		}(i)
	}
	wait.Wait()
	pending, err := outbox.Pending(now, 0)
	if err != nil || len(pending) != 20 {
		t.Fatalf("pending count = %d, err %v", len(pending), err)
	}
}
