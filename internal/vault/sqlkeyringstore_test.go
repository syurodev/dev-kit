package vault

import (
	"context"
	"path/filepath"
	"testing"

	"devkit/internal/storage"
)

func TestSQLKeyringStorePersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "devkit.db")

	db, err := storage.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := storage.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	svc := NewService(NewSQLKeyringStore(storage.NewKV(db, storage.TableVaultKeyring)))

	state, _ := svc.Status()
	if state != StateNotConfigured {
		t.Fatalf("fresh state = %q, want not_configured", state)
	}
	if _, err := svc.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	db.Close()

	// Simulate an app restart: reopen the DB, build a fresh service.
	db2, err := storage.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if err := storage.Migrate(ctx, db2); err != nil {
		t.Fatalf("Migrate (2nd): %v", err)
	}
	svc2 := NewService(NewSQLKeyringStore(storage.NewKV(db2, storage.TableVaultKeyring)))

	state, _ = svc2.Status()
	if state != StateLocked {
		t.Fatalf("state after restart = %q, want locked", state)
	}
	if err := svc2.Unlock("master-password"); err != nil {
		t.Fatalf("Unlock after restart: %v", err)
	}
}
