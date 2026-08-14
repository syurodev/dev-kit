package notes

import "testing"

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

func TestEmptyFolderAllowed(t *testing.T) {
	s := newService(t)
	if _, err := s.CreateFolder("Empty", "gray", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
}
