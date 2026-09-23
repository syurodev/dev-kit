package notes

import (
	"errors"
	"strings"
	"testing"
)

func TestCreateAndListFolder(t *testing.T) {
	s := newService(t)
	folder, err := s.CreateFolder("Work", "blue", []string{" work "})
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if folder.Name != "Work" || folder.Color != "blue" || len(folder.Tags) != 1 || folder.Tags[0] != "work" {
		t.Fatalf("folder = %+v", folder)
	}
	list, err := s.ListFolders()
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(list) != 1 || list[0].ID != folder.ID {
		t.Fatalf("list = %+v", list)
	}
}

func TestCreateFolderValidation(t *testing.T) {
	s := newService(t)
	if _, err := s.CreateFolder("", "blue", nil); err == nil {
		t.Fatal("expected error for empty name")
	}
	if _, err := s.CreateFolder("Work", "neon", nil); err == nil {
		t.Fatal("expected error for invalid color")
	}
}

func TestUpdateFolderRenames(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	updated, err := s.UpdateFolder(folder.ID, "Ops", "red", []string{"ops"})
	if err != nil {
		t.Fatalf("UpdateFolder: %v", err)
	}
	if updated.Name != "Ops" || updated.Color != "red" || updated.Tags[0] != "ops" {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestUpdateFolderRejectsNote(t *testing.T) {
	s := newService(t)
	note, err := s.Create("T", "b", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = s.UpdateFolder(note.ID, "X", "blue", nil)
	if err == nil {
		t.Fatal("expected error when updating a note as folder")
	}
	if err.Error() != "folder not found" {
		t.Fatalf("UpdateFolder error = %q, want folder not found", err.Error())
	}
	got, err := s.Get(note.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "T" {
		t.Fatalf("title = %q, want T", got.Title)
	}
}

func TestEmptyFolderAllowed(t *testing.T) {
	s := newService(t)
	if _, err := s.CreateFolder("Empty", "gray", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
}

// A hex value — what an agent reaches for first — is refused as a named,
// correctable validation error that lists the palette, and a name written
// with other casing or spacing is accepted as that name.
func TestFolderColorIsAPaletteName(t *testing.T) {
	s := newService(t)
	_, err := s.CreateFolder("Work", "#3b82f6", nil)
	if !errors.Is(err, ErrInvalidColor) {
		t.Fatalf("err = %v, want ErrInvalidColor", err)
	}
	if !strings.Contains(err.Error(), `"#3b82f6"`) || !strings.Contains(err.Error(), "blue") {
		t.Fatalf("message %q should name the value given and the accepted ones", err.Error())
	}
	folder, err := s.CreateFolder("Work", " Blue ", nil)
	if err != nil || folder.Color != "blue" {
		t.Fatalf("CreateFolder(\" Blue \") = %+v, %v; want color blue", folder, err)
	}
	if _, err := s.UpdateFolder(folder.ID, "Work", "", nil); !errors.Is(err, ErrInvalidColor) {
		t.Fatalf("UpdateFolder with no color: err = %v, want ErrInvalidColor (a folder must have one)", err)
	}
}
