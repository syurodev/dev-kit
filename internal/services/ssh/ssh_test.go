package ssh

import (
	"testing"

	"changeme/internal/crypto"
	"changeme/internal/services/vaultitem"
	"changeme/internal/vault"
)

func newService(t *testing.T) *Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v))
}

// newServiceWithStore is like newService but also returns the concrete store
// so tests can inspect records (and their still-encrypted payloads) directly.
func newServiceWithStore(t *testing.T) (*Service, *vaultitem.Store) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	store := vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	return NewService(store), store
}

func TestCreateProfileEncryptsSecret(t *testing.T) {
	svc, store := newServiceWithStore(t)

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

	record, err := store.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.Payload == nil {
		t.Fatal("expected payload to be set")
	}
	if string(record.Payload.Ciphertext) == "s3cr3t" {
		t.Fatal("secret must be stored encrypted, not plaintext")
	}
	plain, err := store.OpenPayload(record, secretKeyID)
	if err != nil {
		t.Fatalf("OpenPayload: %v", err)
	}
	if string(plain) != "s3cr3t" {
		t.Fatalf("decrypted secret = %q", plain)
	}
}

func TestCreateProfileUsesV2EnvelopeBoundToProfile(t *testing.T) {
	svc, store := newServiceWithStore(t)

	summary, err := svc.CreateProfile(CreateProfileInput{Name: "prod", Host: "127.0.0.1", Username: "ubuntu", Secret: "s3cr3t"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	record, err := store.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.Payload == nil {
		t.Fatal("expected payload to be set")
	}
	if record.Payload.Version != crypto.EnvelopeVersionV2 {
		t.Fatalf("envelope version = %d, want V2", record.Payload.Version)
	}
}

func TestCreateProfileValidatesRequiredFields(t *testing.T) {
	svc := newService(t)
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
	svc := newService(t)
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
