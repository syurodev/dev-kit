package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	replicationsync "changeme/internal/sync"
)

func newSyncPersistence(t *testing.T) (*SQLOutbox, *sql.DB, *SQLCursorStore, func()) {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := Migrate(context.Background(), db); err != nil {
		db.Close()
		t.Fatalf("migrate: %v", err)
	}
	return NewSQLOutbox(db, time.Minute), db, NewSQLCursorStore(db), func() { _ = db.Close() }
}

func testSQLSyncContext() replicationsync.SyncContext {
	return replicationsync.SyncContext{
		AccountID:       "acct-1",
		DeviceID:        "dev-1",
		ProtocolVersion: replicationsync.SyncProtocolVersion,
		OperationID:     "op-1",
		EntityVersion:   1,
	}
}

func testSQLChange(key string, createdAt time.Time) replicationsync.Change {
	return replicationsync.NewChange(key, "record-1", "note", replicationsync.OperationCreate, []byte{0x01, 0x02}, 1, testSQLSyncContext(), "key-1", "digest-1", createdAt)
}

func testSQLConflict(now time.Time) replicationsync.Conflict {
	return replicationsync.Conflict{
		ID:              "conflict-1",
		RecordID:        "record-1",
		RecordType:      "note",
		LocalChangeKey:  "local-1",
		RemoteChangeKey: "remote-1",
		DetectedAt:      now,
	}
}

func TestSQLOutboxEnqueueGetAndIdempotency(t *testing.T) {
	outbox, _, _, cleanup := newSyncPersistence(t)
	defer cleanup()

	now := time.Unix(100, 123).UTC()
	change := testSQLChange("key-1", now)
	if err := outbox.Enqueue(change); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	change.Payload[0] = 0xff
	if err := outbox.Enqueue(testSQLChange("key-1", now)); err != nil {
		t.Fatalf("idempotent enqueue: %v", err)
	}

	got, err := outbox.Get("key-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RecordType != "note" || got.Payload[0] != 0x01 {
		t.Fatalf("stored change = %+v, payload %x", got, got.Payload)
	}
	got.Payload[0] = 0xee
	again, err := outbox.Get("key-1")
	if err != nil || again.Payload[0] != 0x01 {
		t.Fatalf("get returned mutable payload = %x, err %v", again.Payload, err)
	}

	conflicting := testSQLChange("key-1", now)
	conflicting.Payload = []byte{0x03}
	if err := outbox.Enqueue(conflicting); !errors.Is(err, replicationsync.ErrIdempotencyConflict) {
		t.Fatalf("conflicting enqueue error = %v, want %v", err, replicationsync.ErrIdempotencyConflict)
	}
}

func TestSQLOutboxPendingIsTimeAwareDeterministicAndLimited(t *testing.T) {
	outbox, _, _, cleanup := newSyncPersistence(t)
	defer cleanup()

	base := time.Unix(100, 0).UTC()
	for _, change := range []replicationsync.Change{
		testSQLChange("b", base),
		testSQLChange("a", base),
		testSQLChange("c", base.Add(time.Minute)),
	} {
		if err := outbox.Enqueue(change); err != nil {
			t.Fatalf("enqueue %s: %v", change.IdempotencyKey, err)
		}
	}

	ready, err := outbox.Pending(base, 0)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(ready) != 2 || ready[0].IdempotencyKey != "a" || ready[1].IdempotencyKey != "b" {
		t.Fatalf("pending = %+v, want [a b]", ready)
	}
	limited, err := outbox.Pending(base, 1)
	if err != nil || len(limited) != 1 || limited[0].IdempotencyKey != "a" {
		t.Fatalf("limited pending = %+v, err %v", limited, err)
	}
	if _, err := outbox.Pending(base, -1); err == nil {
		t.Fatal("negative limit accepted")
	}

	if err := outbox.MarkRetry("b", base.Add(time.Hour), replicationsync.FailureTimeout); err != nil {
		t.Fatalf("mark retry: %v", err)
	}
	ready, err = outbox.Pending(base.Add(time.Minute), 0)
	if err != nil || len(ready) != 2 || ready[0].IdempotencyKey != "a" || ready[1].IdempotencyKey != "c" {
		t.Fatalf("pending before retry time = %+v, err %v", ready, err)
	}
}

func TestSQLOutboxStateTransitionsAndSafeFailureReasons(t *testing.T) {
	outbox, _, _, cleanup := newSyncPersistence(t)
	defer cleanup()

	now := time.Unix(100, 0).UTC()
	if err := outbox.Enqueue(testSQLChange("retry", now)); err != nil {
		t.Fatal(err)
	}
	if err := outbox.MarkSyncing("retry", now); err != nil {
		t.Fatal(err)
	}
	next := now.Add(time.Hour)
	if err := outbox.MarkRetry("retry", next, replicationsync.FailureTimeout); err != nil {
		t.Fatal(err)
	}
	got, _ := outbox.Get("retry")
	if got.Status != replicationsync.StatusPending || got.Attempts != 1 || got.LastError != replicationsync.FailureTimeout || !got.NextAttemptAt.Equal(next) {
		t.Fatalf("retry state = %+v", got)
	}
	if err := outbox.MarkSyncing("retry", now); err != nil {
		t.Fatal(err)
	}
	if err := outbox.MarkSent("retry"); err != nil {
		t.Fatal(err)
	}
	got, _ = outbox.Get("retry")
	if got.Status != replicationsync.StatusSynced || got.LastError != "" || !got.NextAttemptAt.IsZero() {
		t.Fatalf("sent state = %+v", got)
	}
	if err := outbox.MarkRetry("retry", next, replicationsync.FailureTimeout); !errors.Is(err, replicationsync.ErrInvalidState) {
		t.Fatalf("retry after sent = %v, want %v", err, replicationsync.ErrInvalidState)
	}

	if err := outbox.Enqueue(testSQLChange("failed", now)); err != nil {
		t.Fatal(err)
	}
	if err := outbox.MarkFailed("failed", replicationsync.FailureNetwork); err != nil {
		t.Fatal(err)
	}
	failed, _ := outbox.Get("failed")
	if failed.Status != replicationsync.StatusFailed || failed.Attempts != 1 || failed.LastError != replicationsync.FailureNetwork || !failed.NextAttemptAt.IsZero() {
		t.Fatalf("failed state = %+v", failed)
	}
	if err := outbox.MarkFailed("failed", replicationsync.FailureNetwork); !errors.Is(err, replicationsync.ErrInvalidState) {
		t.Fatalf("failed transition = %v, want %v", err, replicationsync.ErrInvalidState)
	}
	if err := outbox.MarkFailed("missing", replicationsync.FailureNetwork); !errors.Is(err, replicationsync.ErrNotFound) {
		t.Fatalf("missing transition = %v, want %v", err, replicationsync.ErrNotFound)
	}
	if err := outbox.MarkFailed("failed", replicationsync.FailureReason("raw transport error")); err == nil {
		t.Fatal("unsafe failure reason accepted")
	}
}

func TestSQLOutboxConflictPersistsSafeCopy(t *testing.T) {
	outbox, _, _, cleanup := newSyncPersistence(t)
	defer cleanup()

	now := time.Unix(100, 0).UTC()
	if err := outbox.Enqueue(testSQLChange("local-1", now)); err != nil {
		t.Fatal(err)
	}
	conflict := testSQLConflict(now)
	if err := outbox.MarkConflict("local-1", conflict); err != nil {
		t.Fatalf("mark conflict: %v", err)
	}
	conflict.RemoteChangeKey = "mutated"
	got, err := outbox.Get("local-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != replicationsync.StatusConflict || got.Conflict == nil || got.Conflict.RemoteChangeKey != "remote-1" {
		t.Fatalf("conflict state = %+v", got)
	}
	if pending, err := outbox.Pending(now, 0); err != nil || len(pending) != 0 {
		t.Fatalf("conflict pending = %+v, err %v", pending, err)
	}
	if err := outbox.MarkConflict("local-1", conflict); !errors.Is(err, replicationsync.ErrInvalidState) {
		t.Fatalf("repeat conflict = %v, want %v", err, replicationsync.ErrInvalidState)
	}
}

func TestSQLOutboxReclaimsStaleSyncing(t *testing.T) {
	outbox, _, _, cleanup := newSyncPersistence(t)
	defer cleanup()

	now := time.Unix(1000, 0).UTC()
	if err := outbox.Enqueue(testSQLChange("k1", now)); err != nil {
		t.Fatal(err)
	}
	if err := outbox.MarkSyncing("k1", now); err != nil {
		t.Fatal(err)
	}
	if pending, _ := outbox.Pending(now.Add(30*time.Second), 0); len(pending) != 0 {
		t.Fatal("fresh syncing must not be pending")
	}
	ready, err := outbox.Pending(now.Add(time.Minute+time.Second), 0)
	if err != nil || len(ready) != 1 {
		t.Fatalf("stale syncing reclaim: len=%d err=%v", len(ready), err)
	}
}

func TestSQLRecoverStaleSyncing(t *testing.T) {
	outbox, _, _, cleanup := newSyncPersistence(t)
	defer cleanup()

	now := time.Unix(1000, 0).UTC()
	if err := outbox.Enqueue(testSQLChange("k1", now)); err != nil {
		t.Fatal(err)
	}
	if err := outbox.MarkSyncing("k1", now); err != nil {
		t.Fatal(err)
	}
	if err := outbox.RecoverStaleSyncing(now.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := outbox.Get("k1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != replicationsync.StatusPending || !got.SyncingSince.IsZero() {
		t.Fatalf("recovered = %+v", got)
	}
}

func TestSQLOutboxReclaimsSyncingWithEmptyLeaseTimestamp(t *testing.T) {
	outbox, db, _, cleanup := newSyncPersistence(t)
	defer cleanup()

	now := time.Unix(1000, 0).UTC()
	if err := outbox.Enqueue(testSQLChange("k1", now)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sync_outbox SET status = ?, syncing_since = '' WHERE idempotency_key = ?`,
		string(replicationsync.StatusSyncing), "k1"); err != nil {
		t.Fatal(err)
	}
	pending, err := outbox.Pending(now, 0)
	if err != nil || len(pending) != 1 {
		t.Fatalf("empty syncing_since must be immediately stale: len=%d err=%v", len(pending), err)
	}
	if err := outbox.MarkSyncing("k1", now); err != nil {
		t.Fatalf("reclaim syncing row with empty lease timestamp: %v", err)
	}
}

func TestSQLCursorStoreRoundTripValidationAndInitialValue(t *testing.T) {
	_, _, cursors, cleanup := newSyncPersistence(t)
	defer cleanup()

	ctx := context.Background()
	initial, err := cursors.GetCursor(ctx, "default")
	if err != nil {
		t.Fatalf("initial cursor: %v", err)
	}
	if initial.Token != "" {
		t.Fatalf("initial cursor = %+v, want empty", initial)
	}
	if err := cursors.PutCursor(ctx, "default", replicationsync.Cursor{Token: "opaque-1"}); err != nil {
		t.Fatalf("put cursor: %v", err)
	}
	got, err := cursors.GetCursor(ctx, "default")
	if err != nil || got.Token != "opaque-1" {
		t.Fatalf("get cursor = %+v, err %v", got, err)
	}
	if err := cursors.PutCursor(ctx, "default", replicationsync.Cursor{Token: "opaque-2"}); err != nil {
		t.Fatalf("update cursor: %v", err)
	}
	got, _ = cursors.GetCursor(ctx, "default")
	if got.Token != "opaque-2" {
		t.Fatalf("updated cursor = %+v", got)
	}
	if err := cursors.PutCursor(ctx, "bad\nname", replicationsync.Cursor{}); err == nil {
		t.Fatal("invalid cursor name accepted")
	}
	if err := cursors.PutCursor(ctx, "default", replicationsync.Cursor{Token: "bad\n token"}); err == nil {
		t.Fatal("invalid cursor token accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := cursors.GetCursor(canceled, "default"); err == nil {
		t.Fatal("canceled cursor query returned nil error")
	}
}
