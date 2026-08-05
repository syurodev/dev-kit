package vaultitem_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"changeme/internal/storage"
	replicationsync "changeme/internal/sync"
	"changeme/internal/syncstore"
	"changeme/internal/vault"
	"changeme/internal/vaultitem"
)

func newMutationStore(t *testing.T) (*sql.DB, *vaultitem.Store) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	return db, vaultitem.NewStore(vaultitem.NewSQLMutationRepository(db), v)
}

func TestSQLMutationRepositoryCapturesCreateUpdateDeleteAtomically(t *testing.T) {
	db, store := newMutationStore(t)
	record, err := store.Create("note", "note-body", json.RawMessage(`{"title":"first"}`), []byte("secret body"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateMetadata(record.ID, json.RawMessage(`{"title":"second"}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(record.ID); err != nil {
		t.Fatal(err)
	}

	intents, err := syncstore.NewSQLLocalMutationStore(db).Pending(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 3 {
		t.Fatalf("intent count = %d, want 3", len(intents))
	}
	for i, want := range []replicationsync.Operation{
		replicationsync.OperationCreate,
		replicationsync.OperationUpdate,
		replicationsync.OperationDelete,
	} {
		if intents[i].Operation != want || intents[i].EntityVersion != uint32(i+1) || intents[i].RecordID != record.ID {
			t.Fatalf("intent %d = %+v", i, intents[i])
		}
		if intents[i].KeyID != "vault-item" || intents[i].KeyVersion != 1 {
			t.Fatalf("intent key context = %+v", intents[i])
		}
	}
	if json.Valid(intents[0].Snapshot) == false || json.Valid(intents[2].Snapshot) == false {
		t.Fatal("captured snapshots must be valid JSON")
	}
	if _, err := store.Get(record.ID); err != vaultitem.ErrNotFound {
		t.Fatalf("deleted record error = %v", err)
	}
	var version, tombstone int
	if err := db.QueryRow("SELECT entity_version, tombstone FROM sync_local_entity_versions WHERE record_id = ?", record.ID).Scan(&version, &tombstone); err != nil {
		t.Fatal(err)
	}
	if version != 3 || tombstone != 1 {
		t.Fatalf("local head = version %d tombstone %d", version, tombstone)
	}
	var outboxCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sync_outbox").Scan(&outboxCount); err != nil || outboxCount != 0 {
		t.Fatalf("pre-auth outbox count = %d, err = %v", outboxCount, err)
	}
}

func TestSQLMutationRepositoryRollsBackItemWhenIntentInsertFails(t *testing.T) {
	db, store := newMutationStore(t)
	if _, err := db.Exec(`CREATE TRIGGER fail_local_mutation BEFORE INSERT ON sync_local_mutations
		BEGIN SELECT RAISE(FAIL, 'forced local mutation failure'); END`); err != nil {
		t.Fatal(err)
	}
	record, err := store.Create("note", "note-body", json.RawMessage(`{"title":"rollback"}`), []byte("secret"))
	if err == nil {
		t.Fatal("Create error = nil, want forced capture failure")
	}
	var itemCount, entityCount, mutationCount int
	for query, destination := range map[string]*int{
		"SELECT COUNT(*) FROM vault_items":                &itemCount,
		"SELECT COUNT(*) FROM sync_local_entity_versions": &entityCount,
		"SELECT COUNT(*) FROM sync_local_mutations":       &mutationCount,
	} {
		if scanErr := db.QueryRow(query).Scan(destination); scanErr != nil {
			t.Fatal(scanErr)
		}
	}
	if itemCount != 0 || entityCount != 0 || mutationCount != 0 {
		t.Fatalf("partial state after rollback for %s: items=%d entities=%d mutations=%d", record.ID, itemCount, entityCount, mutationCount)
	}
}

func TestSQLMutationRepositoryDeleteMissingDoesNotInventIntent(t *testing.T) {
	db, store := newMutationStore(t)
	if err := store.Delete("missing"); err != nil {
		t.Fatal(err)
	}
	intents, err := syncstore.NewSQLLocalMutationStore(db).Pending(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 0 {
		t.Fatalf("missing delete intents = %+v", intents)
	}
}

func TestSQLMutationRepositoryRejectsOversizedSnapshotBeforeTransaction(t *testing.T) {
	db, store := newMutationStore(t)
	metadata, err := json.Marshal(map[string]string{"title": strings.Repeat("x", replicationsync.MaxRecordSnapshotBytes)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create("note", "note-body", metadata, []byte("secret")); err == nil {
		t.Fatal("oversized local snapshot was accepted")
	}
	for _, table := range []string{"vault_items", "sync_local_entity_versions", "sync_local_mutations"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count = %d, err = %v", table, count, err)
		}
	}
}
