package notes

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

func TestCreateListGetRoundTrip(t *testing.T) {
	v := newUnlockedVault(t)
	svc := NewService(NewMemoryRepository(), v)

	summary, err := svc.Create("My note", "secret body")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if summary.ID == "" || summary.Title != "My note" {
		t.Fatalf("summary = %+v", summary)
	}

	list, err := svc.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}

	note, err := svc.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if note.Body != "secret body" {
		t.Fatalf("body = %q, want %q", note.Body, "secret body")
	}
}

func TestCreateRequiresTitle(t *testing.T) {
	svc := NewService(NewMemoryRepository(), newUnlockedVault(t))
	if _, err := svc.Create("", "body"); err == nil {
		t.Fatal("Create must require a title")
	}
}

func TestCreateFailsWhenVaultLocked(t *testing.T) {
	v := newUnlockedVault(t)
	v.Lock()
	svc := NewService(NewMemoryRepository(), v)
	if _, err := svc.Create("n", "b"); err == nil {
		t.Fatal("Create must fail when vault locked")
	}
}

func TestGetFailsWhenVaultLocked(t *testing.T) {
	v := newUnlockedVault(t)
	svc := NewService(NewMemoryRepository(), v)
	summary, err := svc.Create("n", "b")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	v.Lock()
	if _, err := svc.Get(summary.ID); err == nil {
		t.Fatal("Get must fail when vault locked")
	}
}
