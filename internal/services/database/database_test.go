package database

import (
	"context"
	"encoding/json"
	"errors"
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

func TestCreateProfileEncryptsPassword(t *testing.T) {
	svc, store := newServiceWithStore(t)

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

	record, err := store.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.Payload == nil {
		t.Fatal("expected payload to be set")
	}
	if string(record.Payload.Ciphertext) == "devkitpass" {
		t.Fatal("password must be stored encrypted, not plaintext")
	}
	plain, err := store.OpenPayload(record, passwordKeyID)
	if err != nil {
		t.Fatalf("OpenPayload: %v", err)
	}
	if string(plain) != "devkitpass" {
		t.Fatalf("decrypted password = %q", plain)
	}
}

func TestCreateProfileUsesV2EnvelopeBoundToProfile(t *testing.T) {
	svc, store := newServiceWithStore(t)

	summary, err := svc.CreateProfile(CreateProfileInput{Name: "local", Host: "127.0.0.1", Password: "devkitpass"})
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

func TestCreateProfileRequiresNameAndHost(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{Host: "h"}); err == nil {
		t.Fatal("missing name must error")
	}
	if _, err := svc.CreateProfile(CreateProfileInput{Name: "n"}); err == nil {
		t.Fatal("missing host must error")
	}
}

func TestCreateProfileDefaultsRemotePostgresToVerifyFull(t *testing.T) {
	svc := newService(t)

	summary, err := svc.CreateProfile(CreateProfileInput{
		Name: "production",
		Host: "db.example.com",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if summary.SSLMode != "verify-full" {
		t.Fatalf("SSLMode = %q, want verify-full", summary.SSLMode)
	}
}

func TestCreateProfileRejectsExplicitInsecureTLSForRemoteHost(t *testing.T) {
	svc := newService(t)

	_, err := svc.CreateProfile(CreateProfileInput{
		Name:    "production",
		Host:    "db.example.com",
		SSLMode: "disable",
	})
	if !errors.Is(err, ErrInsecureTLSForRemoteHost) {
		t.Fatalf("CreateProfile error = %v, want ErrInsecureTLSForRemoteHost", err)
	}
}

func TestCreateProfilePermitsExplicitInsecureTLSForLoopbackHosts(t *testing.T) {
	for _, host := range []string{"localhost", "127.0.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			svc := newService(t)
			summary, err := svc.CreateProfile(CreateProfileInput{
				Name:    "local",
				Host:    host,
				SSLMode: "disable",
			})
			if err != nil {
				t.Fatalf("CreateProfile: %v", err)
			}
			if summary.SSLMode != "disable" {
				t.Fatalf("SSLMode = %q, want disable", summary.SSLMode)
			}
		})
	}
}

func TestDSNRejectsPersistedRemoteProfileWithInsecureTLS(t *testing.T) {
	svc, store := newServiceWithStore(t)
	summary, err := svc.CreateProfile(CreateProfileInput{Name: "production", Host: "db.example.com", Password: "secret"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	record, err := store.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		t.Fatalf("decodeMetadata: %v", err)
	}
	meta.SSLMode = "disable"
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := store.UpdateMetadata(summary.ID, raw); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	_, err = svc.dsn(summary.ID)
	if !errors.Is(err, ErrInsecureTLSForRemoteHost) {
		t.Fatalf("dsn error = %v, want ErrInsecureTLSForRemoteHost", err)
	}
}

func TestQueryPreservesCallerCancellation(t *testing.T) {
	svc := newService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{Name: "local", Host: "127.0.0.1", Password: "secret"})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.Query(ctx, summary.ID, "select 1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Query error = %v, want context.Canceled", err)
	}
}

func TestListProfiles(t *testing.T) {
	svc := newService(t)
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
