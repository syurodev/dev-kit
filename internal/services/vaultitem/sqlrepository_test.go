package vaultitem_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"changeme/internal/services/vaultitem"
	"changeme/internal/storage"
)

func newTestDB(t *testing.T) *vaultitem.SQLRepository {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return vaultitem.NewSQLRepository(db)
}

func TestSQLRepositoryRoundTrip(t *testing.T) {
	repo := newTestDB(t)

	if err := repo.Put(vaultitem.Record{ID: "a1", Type: "note", Metadata: json.RawMessage(`{"title":"x"}`)}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := repo.Put(vaultitem.Record{ID: "b2", Type: "db-profile", Metadata: json.RawMessage(`{"name":"y"}`)}); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := repo.Get("a1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Type != "note" {
		t.Fatalf("type = %q, want note", got.Type)
	}

	notes, err := repo.ListByType("note")
	if err != nil {
		t.Fatalf("listbytype: %v", err)
	}
	if len(notes) != 1 || notes[0].ID != "a1" {
		t.Fatalf("ListByType(note) = %+v, want just a1", notes)
	}

	if err := repo.Delete("a1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get("a1"); err != vaultitem.ErrNotFound {
		t.Fatalf("get after delete err = %v, want ErrNotFound", err)
	}
}
