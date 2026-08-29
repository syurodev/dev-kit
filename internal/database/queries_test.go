package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/storage"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

// newSyncBackedService builds the real thing: a SQLite database whose vault
// items flow into the sync outbox, plus the local history table beside it.
// Nothing here is a fake, because the claim under test is precisely which of
// the two storage paths sync can observe.
func newSyncBackedService(t *testing.T) (*Service, *vault.Service, *sql.DB) {
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
	svc := NewService(vaultitem.NewStore(vaultitem.NewSQLMutationRepository(db), v), testEnvs)
	svc.SetQueryLog(NewSQLQueryHistory(db), v)
	return svc, v, db
}

// syncLedgerCount counts captured local mutations — the ledger every vault-item
// write lands in, and the thing sync later materialises into the outbox. It is
// the honest place to ask "will this leave the device?".
func syncLedgerCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + storage.TableSyncLocalMutations).Scan(&n); err != nil {
		t.Fatalf("count local mutations: %v", err)
	}
	return n
}

func TestSavedQuerySyncsButHistoryDoesNot(t *testing.T) {
	svc, _, db := newSyncBackedService(t)

	before := syncLedgerCount(t, db)
	if _, err := svc.SaveQuery("profile-1", "monthly totals", "SELECT 1"); err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	afterSaved := syncLedgerCount(t, db)
	if afterSaved <= before {
		t.Fatalf("saved query never entered the sync ledger: %d -> %d", before, afterSaved)
	}

	// The same statement recorded as history must leave the outbox untouched.
	svc.recordHistory("profile-1", "SELECT 1")
	entries, err := svc.ListQueryHistory("profile-1")
	if err != nil {
		t.Fatalf("ListQueryHistory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("history entries = %d, want 1", len(entries))
	}
	if got := syncLedgerCount(t, db); got != afterSaved {
		t.Fatalf("history entered the sync ledger: %d -> %d", afterSaved, got)
	}
}

func TestHistoryPrunesToFiftyPerProfile(t *testing.T) {
	svc, _, _ := newSyncBackedService(t)

	for i := 0; i < 51; i++ {
		svc.recordHistory("profile-1", "SELECT "+time.Now().Format("150405.000000000"))
		time.Sleep(time.Millisecond)
	}
	svc.recordHistory("profile-2", "SELECT 'other'")

	first, err := svc.ListQueryHistory("profile-1")
	if err != nil {
		t.Fatalf("ListQueryHistory: %v", err)
	}
	if len(first) != HistoryLimit {
		t.Fatalf("profile-1 history = %d, want %d", len(first), HistoryLimit)
	}
	second, err := svc.ListQueryHistory("profile-2")
	if err != nil {
		t.Fatalf("ListQueryHistory: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("pruning profile-1 touched profile-2: %d rows", len(second))
	}
}

func TestHistoryEnvelopeDoesNotOpenAsSavedQuery(t *testing.T) {
	svc, v, _ := newSyncBackedService(t)
	svc.recordHistory("profile-1", "SELECT secret")

	repo, _ := svc.queryLog()
	rows, err := repo.ListByProfile("profile-1", 1)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListByProfile: %v rows=%d", err, len(rows))
	}
	envelope, err := decodeEnvelope(rows[0].Envelope)
	if err != nil {
		t.Fatalf("decodeEnvelope: %v", err)
	}

	// Same key id, different record type — the AAD must still refuse.
	if _, err := v.OpenWithAD(envelope, associatedDataFor(savedQueryType, rows[0].ID, HistoryKeyID)); err == nil {
		t.Fatal("a history envelope opened under the saved-query record type")
	}
	if _, err := v.OpenWithAD(envelope, historyAD(rows[0].ID)); err != nil {
		t.Fatalf("history envelope failed to open under its own AAD: %v", err)
	}
}

type failingHistory struct{ QueryHistoryRepository }

func (failingHistory) Insert(HistoryRow) error { return errors.New("disk on fire") }

func TestHistoryFailureNeverBreaksTheQuery(t *testing.T) {
	svc, v, _ := newSyncBackedService(t)
	svc.SetQueryLog(failingHistory{NewMemoryQueryHistory()}, v)

	// recordHistory returns nothing by design: a bookkeeping failure has no
	// channel through which it could turn a finished query into an error.
	svc.recordHistory("profile-1", "SELECT 1")

	entries, err := svc.ListQueryHistory("profile-1")
	if err != nil {
		t.Fatalf("ListQueryHistory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no history after a failed insert, got %d", len(entries))
	}
}

func TestDeleteProfileDropsItsHistory(t *testing.T) {
	svc, _, _ := newSyncBackedService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{Name: "local", Host: "127.0.0.1", Port: 5432, Database: "devkitdb", Username: "devkit", Password: "devkitpass", SSLMode: "disable", Env: "dev"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	svc.recordHistory(summary.ID, "SELECT 1")
	svc.recordHistory("other-profile", "SELECT 2")

	if err := svc.DeleteProfile(summary.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	gone, err := svc.ListQueryHistory(summary.ID)
	if err != nil {
		t.Fatalf("ListQueryHistory: %v", err)
	}
	if len(gone) != 0 {
		t.Fatalf("history survived profile deletion: %d rows", len(gone))
	}
	kept, err := svc.ListQueryHistory("other-profile")
	if err != nil || len(kept) != 1 {
		t.Fatalf("unrelated history was collateral damage: err=%v rows=%d", err, len(kept))
	}
}

func TestLockedVaultErrorsRatherThanReturningNothing(t *testing.T) {
	svc, v, _ := newSyncBackedService(t)
	if _, err := svc.SaveQuery("profile-1", "kept", "SELECT 1"); err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	svc.recordHistory("profile-1", "SELECT 1")
	v.Lock()

	if _, err := svc.ListQueryHistory("profile-1"); err == nil {
		t.Fatal("history returned a list while the vault was locked — an empty list reads as \"never queried\"")
	}
	if _, err := svc.ListSavedQueries("profile-1"); err == nil {
		t.Fatal("saved queries returned a list while the vault was locked")
	}
}

func TestSavedQueryKeepsSQLOutOfMetadata(t *testing.T) {
	svc, _, _ := newSyncBackedService(t)
	const secret = "SELECT * FROM cards WHERE pan = '4111111111111111'"
	saved, err := svc.SaveQuery("profile-1", "card lookup", secret)
	if err != nil {
		t.Fatalf("SaveQuery: %v", err)
	}
	record, err := svc.store.Get(saved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(record.Metadata, &meta); err != nil {
		t.Fatalf("metadata: %v", err)
	}
	for key, value := range meta {
		if str, ok := value.(string); ok && str == secret {
			t.Fatalf("metadata field %q holds the statement in plaintext", key)
		}
	}
	if _, found := meta["kind"]; found {
		t.Fatal("kind is gone from the design: storage decides the kind, not a flag")
	}
}
