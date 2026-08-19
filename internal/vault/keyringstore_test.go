package vault

import (
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/crypto"
)

func TestFileKeyringStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring.json")
	store := NewFileKeyringStore(path)

	exists, err := store.Exists()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("store should not exist yet")
	}

	keyring, _, err := crypto.NewKeyring("master-password")
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	if err := store.Save(keyring); err != nil {
		t.Fatalf("Save: %v", err)
	}

	exists, _ = store.Exists()
	if !exists {
		t.Fatal("store should exist after Save")
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := loaded.UnlockWithPassword("master-password"); err != nil {
		t.Fatalf("loaded keyring must unlock: %v", err)
	}
}

func TestFileKeyringStoreModeIsRestrictive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring.json")
	store := NewFileKeyringStore(path)
	keyring, _, _ := crypto.NewKeyring("master-password")
	if err := store.Save(keyring); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("keyring file mode = %v, want no group/other access", info.Mode().Perm())
	}
}

func TestFileKeyringStoreSaveLeavesNoTempResidue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keyring.json")
	store := NewFileKeyringStore(path)
	keyring, _, _ := crypto.NewKeyring("master-password")
	if err := store.Save(keyring); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The atomic write must not leave temp files behind in the keyring dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "keyring.json" {
			t.Fatalf("unexpected file %q left behind by Save", entry.Name())
		}
	}

	// Re-saving over an existing keyring keeps it loadable (rename replaces).
	if err := store.Save(keyring); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if _, err := store.Load(); err != nil {
		t.Fatalf("Load after overwrite: %v", err)
	}
}
