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

// Any hex color is accepted and stored in one canonical spelling, presets
// keep working, and anything else is refused as a named, correctable
// validation error.
func TestFolderColorIsAPresetOrHex(t *testing.T) {
	s := newService(t)
	folder, err := s.CreateFolder("Work", "#3B82F6", nil)
	if err != nil || folder.Color != "#3b82f6" {
		t.Fatalf("CreateFolder(#3B82F6) = %+v, %v; want color #3b82f6", folder, err)
	}
	short, err := s.CreateFolder("Short", "#abc", nil)
	if err != nil || short.Color != "#aabbcc" {
		t.Fatalf("CreateFolder(#abc) = %+v, %v; want color #aabbcc", short, err)
	}
	preset, err := s.CreateFolder("Preset", " Blue ", nil)
	if err != nil || preset.Color != "blue" {
		t.Fatalf("CreateFolder(\" Blue \") = %+v, %v; want color blue", preset, err)
	}
	for _, bad := range []string{"rgb(1,2,3)", "#12345", "#gggggg", "navy"} {
		_, err := s.CreateFolder("Bad", bad, nil)
		if !errors.Is(err, ErrInvalidColor) || !strings.Contains(err.Error(), "hex") {
			t.Fatalf("CreateFolder(%q) err = %v, want ErrInvalidColor explaining the accepted forms", bad, err)
		}
	}
	if _, err := s.UpdateFolder(folder.ID, "Work", "", nil); !errors.Is(err, ErrInvalidColor) {
		t.Fatalf("UpdateFolder with no color: err = %v, want ErrInvalidColor (a folder must have one)", err)
	}
	listed, err := s.ListFolders()
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("ListFolders = %d folders, want the 3 created (a hex folder must pass the record schema on read)", len(listed))
	}
}

func TestNoteColorAcceptsHex(t *testing.T) {
	s := newService(t)
	summary, err := s.Create("Hello", "body", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := s.SetOrganization(summary.ID, "", nil, "#FF8800")
	if err != nil || updated.Color != "#ff8800" || updated.EffectiveColor != "#ff8800" {
		t.Fatalf("SetOrganization(#FF8800) = %+v, %v", updated, err)
	}
}
