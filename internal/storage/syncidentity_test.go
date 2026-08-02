package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := Migrate(context.Background(), db); err != nil {
		db.Close()
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestEnsureLocalPersistsStableIDs(t *testing.T) {
	db := openTestDB(t)
	store := NewSQLIdentityStore(db)
	a1, d1, err := store.EnsureLocal(context.Background())
	if err != nil || a1 == "" || d1 == "" {
		t.Fatalf("first: %v %q %q", err, a1, d1)
	}
	a2, d2, err := store.EnsureLocal(context.Background())
	if err != nil || a1 != a2 || d1 != d2 {
		t.Fatalf("second differed: %q/%q vs %q/%q err=%v", a1, d1, a2, d2, err)
	}
}
