package notes

import (
	"context"
	"path/filepath"
	"testing"

	"changeme/internal/storage"
)

func TestSQLRepositoryPersistsEncryptedNote(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "devkit.db")
	v := newUnlockedVault(t)

	db, err := storage.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	svc := NewService(NewSQLRepository(storage.NewKV(db, storage.TableNotes)), v)
	summary, err := svc.Create("Persisted", "secret body")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	db.Close()

	// Reopen the DB (same in-memory vault key) and confirm the encrypted note
	// persisted and still decrypts.
	db2, err := storage.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if err := storage.Migrate(ctx, db2); err != nil {
		t.Fatalf("Migrate (2nd): %v", err)
	}
	svc2 := NewService(NewSQLRepository(storage.NewKV(db2, storage.TableNotes)), v)
	note, err := svc2.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if note.Body != "secret body" {
		t.Fatalf("body = %q, want %q", note.Body, "secret body")
	}
}
