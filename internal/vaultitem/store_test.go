package vaultitem

import (
	"encoding/json"
	"testing"

	"changeme/internal/vault"
)

func newUnlockedStore(t *testing.T) *Store {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewStore(NewMemoryRepository(), v)
}

func newStoreWithVault(t *testing.T) (*Store, *vault.Service) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewStore(NewMemoryRepository(), v), v
}

func TestStoreCreateOpenRoundTrip(t *testing.T) {
	s := newUnlockedStore(t)

	rec, err := s.Create("note", "note-body", json.RawMessage(`{"title":"hello"}`), []byte("secret body"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(rec.ID) != 24 {
		t.Fatalf("id %q length = %d, want 24 hex chars", rec.ID, len(rec.ID))
	}
	if rec.KeyVersion != 1 || rec.SyncState != "local" {
		t.Fatalf("key_version/sync_state = %d/%q, want 1/local", rec.KeyVersion, rec.SyncState)
	}

	plain, err := s.OpenPayload(rec, "note-body")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(plain) != "secret body" {
		t.Fatalf("plain = %q, want %q", plain, "secret body")
	}
}

func TestStoreOpenRejectsSwappedID(t *testing.T) {
	s := newUnlockedStore(t)

	a, _ := s.Create("note", "note-body", json.RawMessage(`{"title":"a"}`), []byte("A body"))
	b, _ := s.Create("note", "note-body", json.RawMessage(`{"title":"b"}`), []byte("B body"))

	b.Payload = a.Payload
	if _, err := s.OpenPayload(b, "note-body"); err == nil {
		t.Fatal("expected open to fail for swapped payload (AAD mismatch)")
	}
}

func TestStoreSearchMetadataAcrossTypes(t *testing.T) {
	s := newUnlockedStore(t)

	s.Create("note", "note-body", json.RawMessage(`{"title":"staging notes"}`), []byte("x"))
	s.Create("db-profile", "db-password", json.RawMessage(`{"name":"staging db"}`), []byte("pw"))
	s.Create("ssh-profile", "ssh-secret", json.RawMessage(`{"name":"prod box"}`), []byte("key"))

	hits, err := s.Search("staging")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("Search(staging) returned %d, want 2", len(hits))
	}

	scoped, err := s.Search("staging", "note")
	if err != nil {
		t.Fatalf("scoped search: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Type != "note" {
		t.Fatalf("scoped Search = %+v, want one note", scoped)
	}
}

func TestStoreCreateFailsWhenVaultLocked(t *testing.T) {
	s, v := newStoreWithVault(t)
	v.Lock()

	if _, err := s.Create("note", "note-body", json.RawMessage(`{"title":"x"}`), []byte("secret")); err == nil {
		t.Fatal("expected Create to fail when vault is locked")
	}
}

func TestStoreOpenPayloadFailsWhenVaultLocked(t *testing.T) {
	s, v := newStoreWithVault(t)

	rec, err := s.Create("note", "note-body", json.RawMessage(`{"title":"x"}`), []byte("secret body"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	v.Lock()

	if _, err := s.OpenPayload(rec, "note-body"); err == nil {
		t.Fatal("expected OpenPayload to fail when vault is locked")
	}
}

func TestStoreSearchWorksWhenVaultLocked(t *testing.T) {
	s, v := newStoreWithVault(t)

	if _, err := s.Create("note", "note-body", json.RawMessage(`{"title":"staging notes"}`), []byte("body")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Create("db-profile", "db-password", json.RawMessage(`{"name":"prod db"}`), []byte("pw")); err != nil {
		t.Fatalf("create: %v", err)
	}

	v.Lock()

	// Search reads plaintext metadata only, so it still works while locked.
	hits, err := s.Search("staging")
	if err != nil {
		t.Fatalf("search while locked: %v", err)
	}
	if len(hits) != 1 || hits[0].Type != "note" {
		t.Fatalf("Search(staging) while locked = %+v, want one note", hits)
	}
}

func TestUpdateMetadataMarksPulledRecordLocal(t *testing.T) {
	repo := NewMemoryRepository()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	store := NewStore(repo, v)
	record, err := store.Create("note", "note-body", json.RawMessage(`{"title":"remote"}`), []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	record.SyncState = "synced"
	if err := repo.Put(record); err != nil {
		t.Fatal(err)
	}

	updated, err := store.UpdateMetadata(record.ID, json.RawMessage(`{"title":"local edit"}`))
	if err != nil {
		t.Fatal(err)
	}
	if updated.SyncState != "local" {
		t.Fatalf("sync state = %q, want local", updated.SyncState)
	}
}
