package vaultitem

import (
	"encoding/json"
	"testing"
)

func TestMemoryRepositoryRoundTrip(t *testing.T) {
	repo := NewMemoryRepository()

	rec := Record{ID: "a1", Type: "note", Metadata: json.RawMessage(`{"title":"x"}`)}
	if err := repo.Put(rec); err != nil {
		t.Fatalf("put: %v", err)
	}

	got, err := repo.Get("a1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Type != "note" {
		t.Fatalf("type = %q, want note", got.Type)
	}

	repo.Put(Record{ID: "b2", Type: "db-profile"})
	notes, err := repo.ListByType("note")
	if err != nil {
		t.Fatalf("listbytype: %v", err)
	}
	if len(notes) != 1 || notes[0].ID != "a1" {
		t.Fatalf("ListByType(note) = %+v, want just a1", notes)
	}

	if err := repo.Delete("a1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Get("a1"); err != ErrNotFound {
		t.Fatalf("get after delete err = %v, want ErrNotFound", err)
	}
}
