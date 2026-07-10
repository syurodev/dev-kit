package database

import (
	"path/filepath"
	"testing"
	"time"

	"changeme/internal/crypto"
)

func sampleProfile(id string) ProfileRecord {
	return ProfileRecord{
		ID:       id,
		Name:     "profile-" + id,
		Host:     "127.0.0.1",
		Port:     5432,
		Database: "devkitdb",
		Username: "devkit",
		SSLMode:  "disable",
		Password: crypto.Envelope{
			Version:    1,
			Algorithm:  crypto.AlgorithmXChaCha20Poly1305,
			KeyID:      "db-password",
			Nonce:      []byte("nonce-bytes"),
			Ciphertext: []byte("ciphertext-bytes"),
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func TestFileRepositoryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	repo := NewFileRepository(path)

	if err := repo.Put(sampleProfile("a")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := repo.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "profile-a" {
		t.Fatalf("name = %q", got.Name)
	}

	if err := repo.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get("a"); err == nil {
		t.Fatal("Get after Delete must error")
	}
}
