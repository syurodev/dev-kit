package notes

import (
	"testing"

	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

func newService(t *testing.T) *Service {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v))
}

func TestCreateAndGet(t *testing.T) {
	s := newService(t)

	summary, err := s.Create("Title", "body text")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if summary.Title != "Title" {
		t.Fatalf("title = %q", summary.Title)
	}

	note, err := s.Get(summary.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if note.Body != "body text" {
		t.Fatalf("body = %q", note.Body)
	}
}

func TestCreateRequiresTitle(t *testing.T) {
	s := newService(t)
	if _, err := s.Create("", "b"); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestUpdateReplacesTitleAndBody(t *testing.T) {
	s := newService(t)
	summary, err := s.Create("Old title", "old body")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := s.Update(summary.ID, "New title", "new body")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "New title" || updated.Body != "new body" {
		t.Fatalf("updated = %+v", updated)
	}

	fetched, err := s.Get(summary.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Title != "New title" || fetched.Body != "new body" {
		t.Fatalf("fetched = %+v", fetched)
	}
}

func TestUpdateRequiresTitle(t *testing.T) {
	s := newService(t)
	summary, _ := s.Create("Title", "body")
	if _, err := s.Update(summary.ID, "", "body"); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestUpdateFailsForUnknownID(t *testing.T) {
	s := newService(t)
	if _, err := s.Update("does-not-exist", "Title", "body"); err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestListReturnsSummaries(t *testing.T) {
	s := newService(t)
	s.Create("A", "1")
	s.Create("B", "2")
	list, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("len = %d", len(list))
	}
}

func TestSearchByTitle(t *testing.T) {
	s := newService(t)
	s.Create("staging deploy", "x")
	s.Create("prod deploy", "y")
	hits, err := s.Search("staging")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Title != "staging deploy" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchByBodyWhenUnlocked(t *testing.T) {
	s := newService(t)
	s.Create("note one", "contains rocketship")
	hits, err := s.Search("rocketship")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected body match, got %d", len(hits))
	}
}

func TestDelete(t *testing.T) {
	s := newService(t)
	summary, _ := s.Create("x", "y")
	if err := s.Delete(summary.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(summary.ID); err == nil {
		t.Fatal("expected get to fail after delete")
	}
}
