package database

import (
	"testing"

	"changeme/internal/crypto"
	"changeme/internal/vault"
)

func newUnlockedVault(t *testing.T) *vault.Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("master-password"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	return v
}

func TestCreateProfileEncryptsPassword(t *testing.T) {
	v := newUnlockedVault(t)
	repo := NewMemoryRepository()
	svc := NewService(repo, v)

	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:     "local",
		Host:     "127.0.0.1",
		Port:     5432,
		Database: "devkitdb",
		Username: "devkit",
		Password: "devkitpass",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}

	record, err := repo.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(record.Password.Ciphertext) == "devkitpass" {
		t.Fatal("password must be stored encrypted, not plaintext")
	}
	plain, err := v.OpenWithAD(record.Password, crypto.AssociatedData{RecordType: "database-profile", RecordID: record.ID, KeyID: passwordKeyID})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(plain) != "devkitpass" {
		t.Fatalf("decrypted password = %q", plain)
	}
}

func TestCreateProfileUsesV2EnvelopeBoundToProfile(t *testing.T) {
	v := newUnlockedVault(t)
	repo := NewMemoryRepository()
	svc := NewService(repo, v)

	summary, err := svc.CreateProfile(CreateProfileInput{Name: "local", Host: "127.0.0.1", Password: "devkitpass"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	record, err := repo.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.Password.Version != crypto.EnvelopeVersionV2 {
		t.Fatalf("envelope version = %d, want V2", record.Password.Version)
	}
}

func TestCreateProfileRequiresNameAndHost(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newUnlockedVault(t))
	if _, err := svc.CreateProfile(CreateProfileInput{Host: "h"}); err == nil {
		t.Fatal("missing name must error")
	}
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "n"}); err == nil {
		t.Fatal("missing host must error")
	}
}

func TestListProfiles(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newUnlockedVault(t))
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "a", Host: "h"}); err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	list, err := svc.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
}
