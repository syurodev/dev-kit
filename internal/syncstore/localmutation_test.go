package syncstore

import (
	"bytes"
	"context"
	"testing"
	"time"

	replicationsync "changeme/internal/sync"
)

func TestSQLLocalMutationStoreRejectsOversizedStoredSnapshotWithoutReturningIt(t *testing.T) {
	_, db, _, cleanup := newSyncPersistence(t)
	defer cleanup()
	if _, err := db.Exec(`INSERT INTO sync_local_mutations
		(operation_id, record_id, record_type, entity_version, operation, snapshot, key_version, key_id, created_at)
		VALUES (?, ?, ?, 1, 'create', ?, 1, 'vault-item', ?)`,
		"oversized-op", "record-oversized", "note",
		bytes.Repeat([]byte{1}, replicationsync.MaxRecordSnapshotBytes+1),
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSQLLocalMutationStore(db).Pending(context.Background(), 10); err == nil {
		t.Fatal("oversized stored local mutation was returned")
	}
}
