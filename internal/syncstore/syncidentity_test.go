package syncstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"devkit/internal/storage"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := storage.Migrate(context.Background(), db); err != nil {
		db.Close()
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestEnsureLocalDevicePersistsStableIDWithoutMintingAccountIdentity(t *testing.T) {
	db := openTestDB(t)
	store := NewSQLDeviceIdentityStore(db)
	d1, err := store.EnsureLocalDevice(context.Background())
	if err != nil || d1 == "" {
		t.Fatalf("first: %v %q", err, d1)
	}
	d2, err := store.EnsureLocalDevice(context.Background())
	if err != nil || d1 != d2 {
		t.Fatalf("second differed: %q vs %q err=%v", d1, d2, err)
	}
	var accountID string
	if err := db.QueryRow("SELECT account_id FROM sync_identity WHERE id = 'local'").Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if accountID != "" {
		t.Fatalf("local store minted account identity %q", accountID)
	}
}
