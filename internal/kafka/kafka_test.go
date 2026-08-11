package kafka

import (
	"testing"

	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

type stubEnvLookup map[string]bool

func (s stubEnvLookup) Exists(name string) bool { return s[name] }

var testEnvs = stubEnvLookup{"dev": true, "stg": true, "prod": true}

func newService(t *testing.T) *Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v), testEnvs, nil)
}

func TestCreateProfileRequiresEnv(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{
		Name:             "events",
		BootstrapServers: []string{"127.0.0.1:9092"},
		SecurityProtocol: "PLAINTEXT",
	}); err == nil {
		t.Fatal("missing env must error")
	}
}

func TestCreateProfileRejectsUnknownEnv(t *testing.T) {
	svc := newService(t)
	if _, err := svc.CreateProfile(CreateProfileInput{
		Name:             "events",
		BootstrapServers: []string{"127.0.0.1:9092"},
		SecurityProtocol: "PLAINTEXT",
		Env:              "canary",
	}); err == nil {
		t.Fatal("unknown env must error")
	}
}

func TestCreateListDeleteProfile(t *testing.T) {
	svc := newService(t)
	summary, err := svc.CreateProfile(CreateProfileInput{
		Name:             "events",
		BootstrapServers: []string{"127.0.0.1:9092"},
		SecurityProtocol: "PLAINTEXT",
		Secret:           "secret",
		Env:              "dev",
	})
	if err != nil {
		t.Fatalf("CreateProfile: %v", err)
	}
	if summary.Name != "events" {
		t.Fatalf("Name = %q, want events", summary.Name)
	}
	if len(summary.BootstrapServers) != 1 || summary.BootstrapServers[0] != "127.0.0.1:9092" {
		t.Fatalf("BootstrapServers = %v", summary.BootstrapServers)
	}
	if summary.Env != "dev" {
		t.Fatalf("Env = %q, want dev", summary.Env)
	}

	list, err := svc.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0].ID != summary.ID {
		t.Fatalf("list ID = %q, want %q", list[0].ID, summary.ID)
	}

	if err := svc.DeleteProfile(summary.ID); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	list, err = svc.ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("len(list) after delete = %d, want 0", len(list))
	}
}
