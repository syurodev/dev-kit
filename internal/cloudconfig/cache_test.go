package cloudconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)

	_, found, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Fatal("cache should be empty initially")
	}

	want := Snapshot{
		ConfigVersion: "1",
		OIDCIssuer:    "https://auth.example.test/realms/devkit",
		OIDCClientID:  "devkit-desktop",
		MinAppVersion: "0.1.0",
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, found, err := store.Load()
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if !found {
		t.Fatal("cache should exist after Save")
	}
	if got != want {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
}

func TestFileStoreModeIsRestrictive(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir)
	if err := store.Save(Snapshot{
		OIDCIssuer: "https://auth.example.test/realms/devkit",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "DevKit", "desktop-config.json"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("cache file mode = %v, want no group/other access", info.Mode().Perm())
	}
}
