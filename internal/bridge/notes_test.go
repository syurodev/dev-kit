package bridge

import (
	"testing"

	"changeme/internal/app"
)

func TestNotesBridgeCreateListGet(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}

	summary, err := service.NotesCreate("Bridge note", "secret via bridge")
	if err != nil {
		t.Fatalf("NotesCreate: %v", err)
	}

	list, err := service.NotesList()
	if err != nil {
		t.Fatalf("NotesList: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}

	note, err := service.NotesGet(summary.ID)
	if err != nil {
		t.Fatalf("NotesGet: %v", err)
	}
	if note.Body != "secret via bridge" {
		t.Fatalf("body = %q", note.Body)
	}
}

func TestNotesBridgeCreateFailsWhenLocked(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	// No SetupVault: vault is not configured, so notes cannot be created.
	if _, err := service.NotesCreate("n", "b"); err == nil {
		t.Fatal("NotesCreate must fail when vault is not unlocked")
	}
}
