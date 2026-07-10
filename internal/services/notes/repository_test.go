package notes

import (
	"path/filepath"
	"testing"
	"time"

	"changeme/internal/crypto"
)

func sampleRecord(id string) Record {
	return Record{
		ID:    id,
		Title: "title-" + id,
		Body: crypto.Envelope{
			Version:    1,
			Algorithm:  crypto.AlgorithmXChaCha20Poly1305,
			KeyID:      "note-body",
			Nonce:      []byte("nonce-bytes"),
			Ciphertext: []byte("ciphertext-bytes"),
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}

func TestFileRepositoryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.json")
	repo := NewFileRepository(path)

	if err := repo.Put(sampleRecord("a")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := repo.Put(sampleRecord("b")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2", len(list))
	}

	got, err := repo.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "title-a" {
		t.Fatalf("title = %q", got.Title)
	}

	if err := repo.Delete("a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get("a"); err == nil {
		t.Fatal("Get after Delete must error")
	}
}
