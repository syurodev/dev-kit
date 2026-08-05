package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	replicationsync "changeme/internal/sync"
)

// seedCapturedConflict drives a device into the state right after it lost a
// same-version push and captured "theirs": ours at v1 content, local head at
// v2, a conflicted outbox row, and a captured remote snapshot. It returns the
// winning remote mutation ("theirs").
func seedCapturedConflict(t *testing.T, db *sql.DB, store *SQLRemoteStore) RemoteMutation {
	t.Helper()
	ours := remoteMutation("record-1", replicationsync.OperationCreate, 1)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-1", []RemoteMutation{ours}); err != nil {
		t.Fatalf("seed v1: %v", err)
	}
	if _, err := db.Exec(`UPDATE sync_entity_versions
		SET entity_version = 2, operation_id = 'op-local-b', operation = 'update'
		WHERE account_id = ? AND record_id = 'record-1'`, testRemoteAccountID); err != nil {
		t.Fatalf("advance account head: %v", err)
	}
	if _, err := db.Exec(`UPDATE sync_local_entity_versions
		SET entity_version = 2, operation_id = 'op-local-b', operation = 'update'
		WHERE record_id = 'record-1'`); err != nil {
		t.Fatalf("advance local head: %v", err)
	}

	outbox := NewSQLOutbox(db)
	losing := replicationsync.NewChange(
		"local-key-b", "record-1", "note", replicationsync.OperationUpdate, []byte("ours-cipher"), 1,
		replicationsync.SyncContext{
			AccountID: testRemoteAccountID, DeviceID: "dev-b",
			ProtocolVersion: replicationsync.SyncProtocolVersion, OperationID: "op-local-b", EntityVersion: 2,
		},
		"key-1", time.Unix(100, 0).UTC())
	if err := outbox.Enqueue(losing); err != nil {
		t.Fatalf("enqueue losing push: %v", err)
	}
	if err := outbox.MarkConflict("local-key-b", replicationsync.Conflict{
		ID: "conflict-1", RecordID: "record-1", RecordType: "note",
		LocalChangeKey: "local-key-b", RemoteChangeKey: "op-remote-a", DetectedAt: time.Unix(100, 0).UTC(),
	}); err != nil {
		t.Fatalf("mark conflict: %v", err)
	}

	theirs := remoteMutation("record-1", replicationsync.OperationUpdate, 2)
	if err := store.ApplyRemoteBatch(context.Background(), testRemoteAccountID, "pull", "cursor-2", []RemoteMutation{theirs}); err != nil {
		t.Fatalf("capture theirs: %v", err)
	}
	return theirs
}

func TestResolveUseTheirsAdoptsRemoteAndClearsConflict(t *testing.T) {
	db, store := newTestRemoteStore(t)
	theirs := seedCapturedConflict(t, db, store)

	if err := NewSQLConflictResolver(db).ResolveUseTheirs(context.Background(), testRemoteAccountID, "record-1"); err != nil {
		t.Fatalf("resolve use theirs: %v", err)
	}

	// Ours now equals theirs, and the account ledger points at the remote op.
	assertVaultData(t, db, "record-1", theirs.RecordJSON)
	var version int
	var op string
	if err := db.QueryRow(`SELECT entity_version, operation_id FROM sync_entity_versions
		WHERE account_id = ? AND record_id = 'record-1'`, testRemoteAccountID).Scan(&version, &op); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if version != 2 || op != theirs.OperationID {
		t.Fatalf("ledger = v%d op=%s, want v2 op=%s", version, op, theirs.OperationID)
	}

	var appliedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_applied_operations
		WHERE account_id = ? AND operation_id = ?`, testRemoteAccountID, theirs.OperationID).Scan(&appliedCount); err != nil {
		t.Fatalf("read applied ops: %v", err)
	}
	if appliedCount != 1 {
		t.Fatalf("applied op count = %d, want 1", appliedCount)
	}

	assertConflictCleared(t, db)
}

func TestResolveUseLocalReMaterialisesAtRemoteHeadPlusOne(t *testing.T) {
	db, store := newTestRemoteStore(t)
	_ = seedCapturedConflict(t, db, store)

	oursJSON := []byte(`{"id":"record-1","type":"note","metadata":{"marker":"ours-kept"},"key_version":1,"sync_state":"local"}`)
	res := LocalResolution{
		OperationID: "op-resolve-ours",
		Operation:   replicationsync.OperationUpdate,
		KeyVersion:  1,
		KeyID:       "vault-item",
		RecordJSON:  oursJSON,
		Snapshot:    []byte("ours-snapshot-bytes"),
		CreatedAt:   time.Unix(200, 0).UTC(),
	}
	if err := NewSQLConflictResolver(db).ResolveUseLocal(context.Background(), testRemoteAccountID, "record-1", res); err != nil {
		t.Fatalf("resolve use local: %v", err)
	}

	// Ours content is kept and re-queued as a new mutation at remote_head+1 = 3.
	assertVaultData(t, db, "record-1", oursJSON)
	var localVersion int
	var localOp string
	if err := db.QueryRow(`SELECT entity_version, operation_id FROM sync_local_entity_versions
		WHERE record_id = 'record-1'`).Scan(&localVersion, &localOp); err != nil {
		t.Fatalf("read local head: %v", err)
	}
	if localVersion != 3 || localOp != "op-resolve-ours" {
		t.Fatalf("local head = v%d op=%s, want v3 op=op-resolve-ours", localVersion, localOp)
	}
	var mutationCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_local_mutations
		WHERE operation_id = 'op-resolve-ours' AND entity_version = 3 AND key_id = 'vault-item'`).Scan(&mutationCount); err != nil {
		t.Fatalf("read local mutation: %v", err)
	}
	if mutationCount != 1 {
		t.Fatalf("local mutation rows = %d, want 1", mutationCount)
	}

	// The account ledger is untouched; SyncNow's materialisation advances it.
	var accountVersion int
	if err := db.QueryRow(`SELECT entity_version FROM sync_entity_versions
		WHERE account_id = ? AND record_id = 'record-1'`, testRemoteAccountID).Scan(&accountVersion); err != nil {
		t.Fatalf("read account head: %v", err)
	}
	if accountVersion != 2 {
		t.Fatalf("account head = v%d, want v2 (untouched)", accountVersion)
	}

	assertConflictCleared(t, db)
}

func TestResolveUseLocalDeleteQueuesTombstone(t *testing.T) {
	db, store := newTestRemoteStore(t)
	_ = seedCapturedConflict(t, db, store)

	res := LocalResolution{
		OperationID: "op-resolve-delete",
		Operation:   replicationsync.OperationDelete,
		KeyVersion:  1,
		KeyID:       "vault-item",
		Snapshot:    []byte(`{"record_id":"record-1","record_type":"note","deleted":true}`),
		CreatedAt:   time.Unix(200, 0).UTC(),
	}
	if err := NewSQLConflictResolver(db).ResolveUseLocal(context.Background(), testRemoteAccountID, "record-1", res); err != nil {
		t.Fatalf("resolve use local delete: %v", err)
	}

	var itemCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM vault_items WHERE id = 'record-1'").Scan(&itemCount); err != nil {
		t.Fatalf("count items: %v", err)
	}
	if itemCount != 0 {
		t.Fatalf("item count = %d, want 0 after delete resolution", itemCount)
	}
	var tombstone int
	if err := db.QueryRow(`SELECT tombstone FROM sync_local_entity_versions
		WHERE record_id = 'record-1'`).Scan(&tombstone); err != nil {
		t.Fatalf("read local head: %v", err)
	}
	if tombstone != 1 {
		t.Fatalf("tombstone = %d, want 1", tombstone)
	}
	assertConflictCleared(t, db)
}

func TestResolveMissingConflictFailsClosed(t *testing.T) {
	db, _ := newTestRemoteStore(t)
	resolver := NewSQLConflictResolver(db)
	if err := resolver.ResolveUseTheirs(context.Background(), testRemoteAccountID, "missing"); err != ErrConflictNotFound {
		t.Fatalf("use theirs missing = %v, want ErrConflictNotFound", err)
	}
	res := LocalResolution{
		OperationID: "op-x", Operation: replicationsync.OperationUpdate, KeyVersion: 1,
		KeyID: "vault-item", RecordJSON: []byte(`{}`), Snapshot: []byte("s"), CreatedAt: time.Unix(1, 0).UTC(),
	}
	if err := resolver.ResolveUseLocal(context.Background(), testRemoteAccountID, "missing", res); err != ErrConflictNotFound {
		t.Fatalf("use local missing = %v, want ErrConflictNotFound", err)
	}
}

func assertConflictCleared(t *testing.T, db *sql.DB) {
	t.Helper()
	var conflictCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sync_conflicts WHERE record_id = 'record-1'").Scan(&conflictCount); err != nil {
		t.Fatalf("count conflicts: %v", err)
	}
	if conflictCount != 0 {
		t.Fatalf("conflict rows = %d, want 0", conflictCount)
	}
	var outboxCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sync_outbox WHERE idempotency_key = 'local-key-b'").Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 0 {
		t.Fatalf("conflicted outbox rows = %d, want 0", outboxCount)
	}
}
