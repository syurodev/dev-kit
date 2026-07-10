package ssh

import (
	"testing"

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

func TestCreateProfileEncryptsSecret(t *testing.T) {
	v := newUnlockedVault(t)
	repo := NewMemoryRepository()
	svc := NewService(repo, v)

	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:       "prod",
		Host:       "127.0.0.1",
		Username:   "ubuntu",
		AuthMethod: AuthPassword,
		Secret:     "s3cr3t",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if summary.Port != 22 {
		t.Fatalf("default port = %d, want 22", summary.Port)
	}

	record, err := repo.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(record.Secret.Ciphertext) == "s3cr3t" {
		t.Fatal("secret must be stored encrypted, not plaintext")
	}
	plain, err := v.Open(record.Secret)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(plain) != "s3cr3t" {
		t.Fatalf("decrypted secret = %q", plain)
	}
}

func TestCreateProfileValidatesRequiredFields(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newUnlockedVault(t))
	if _, err := svc.CreateProfile(CreateProfileInput{Host: "h", Username: "u"}); err == nil {
		t.Fatal("missing name must error")
	}
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "n", Username: "u"}); err == nil {
		t.Fatal("missing host must error")
	}
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "n", Host: "h"}); err == nil {
		t.Fatal("missing username must error")
	}
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "n", Host: "h", Username: "u", AuthMethod: "bogus"}); err == nil {
		t.Fatal("bad auth method must error")
	}
}

func TestListProfiles(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newUnlockedVault(t))
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "a", Host: "h", Username: "u"}); err != nil {
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
