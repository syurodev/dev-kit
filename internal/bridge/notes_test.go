package bridge

import (
	"context"
	"testing"

	"devkit/internal/app"
)

func TestNotesBridgeCreateListGet(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}

	summary, err := service.NotesCreate(context.Background(), "Bridge note", "secret via bridge", "", nil)
	if err != nil {
		t.Fatalf("NotesCreate: %v", err)
	}

	list, err := service.NotesList(context.Background())
	if err != nil {
		t.Fatalf("NotesList: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}

	note, err := service.NotesGet(context.Background(), summary.ID)
	if err != nil {
		t.Fatalf("NotesGet: %v", err)
	}
	if note.Body != "secret via bridge" {
		t.Fatalf("body = %q", note.Body)
	}
}

func TestNotesBridgeUpdate(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}

	summary, err := service.NotesCreate(context.Background(), "Old title", "old body", "", nil)
	if err != nil {
		t.Fatalf("NotesCreate: %v", err)
	}

	updated, err := service.NotesUpdate(context.Background(), summary.ID, "New title", "new body")
	if err != nil {
		t.Fatalf("NotesUpdate: %v", err)
	}
	if updated.Title != "New title" || updated.Body != "new body" {
		t.Fatalf("updated = %+v", updated)
	}

	note, err := service.NotesGet(context.Background(), summary.ID)
	if err != nil {
		t.Fatalf("NotesGet: %v", err)
	}
	if note.Title != "New title" || note.Body != "new body" {
		t.Fatalf("note = %+v", note)
	}
}

func TestNotesBridgeSearch(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}
	if _, err := service.NotesCreate(context.Background(), "Groceries", "buy milk", "", nil); err != nil {
		t.Fatalf("NotesCreate: %v", err)
	}

	results, err := service.NotesSearch(context.Background(), "grocer")
	if err != nil {
		t.Fatalf("NotesSearch: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
}

func TestNotesBridgeFoldersAndOrganization(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}
	folder, err := service.NotesFolderCreate(context.Background(), "Work", "blue", []string{"work"})
	if err != nil {
		t.Fatalf("NotesFolderCreate: %v", err)
	}
	summary, err := service.NotesCreate(context.Background(), "Standup", "body", folder.ID, []string{"standup"})
	if err != nil {
		t.Fatalf("NotesCreate: %v", err)
	}
	if summary.FolderID != folder.ID {
		t.Fatalf("FolderID = %q", summary.FolderID)
	}
	organized, err := service.NotesSetOrganization(context.Background(), summary.ID, "", []string{"solo"}, "red")
	if err != nil {
		t.Fatalf("NotesSetOrganization: %v", err)
	}
	if organized.Color != "red" {
		t.Fatalf("Color = %q, want red", organized.Color)
	}
	if err := service.NotesFolderDelete(context.Background(), folder.ID); err != nil {
		t.Fatalf("NotesFolderDelete: %v", err)
	}
	list, err := service.NotesFolderList(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
}

func TestNotesBridgeCreateFailsWhenLocked(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	// No SetupVault: vault is not configured, so notes cannot be created.
	if _, err := service.NotesCreate(context.Background(), "n", "b", "", nil); err == nil {
		t.Fatal("NotesCreate must fail when vault is not unlocked")
	}
}
