package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenRestrictsSQLiteFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("database permissions = %o, want %o", got, want)
	}
}

func TestSQLiteKVPersistenceAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	kv := NewKV(db, TableNotes)
	if err := kv.Put(ctx, "a", []byte(`{"x":1}`)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	list, err := kv.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	db.Close()

	// Reopen the same file: the row must still be there.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if err := Migrate(ctx, db2); err != nil {
		t.Fatalf("Migrate (2nd, must be idempotent): %v", err)
	}
	kv2 := NewKV(db2, TableNotes)
	data, ok, err := kv2.Get(ctx, "a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || string(data) != `{"x":1}` {
		t.Fatalf("persisted data = %q, ok=%v", data, ok)
	}

	if err := kv2.Delete(ctx, "a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := kv2.Get(ctx, "a"); ok {
		t.Fatal("row should be gone after Delete")
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate #1: %v", err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate #2: %v", err)
	}
}
