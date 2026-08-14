package notes

import (
	"fmt"
	"strings"
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

	summary, err := s.Create("Title", "body text", "", nil)
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
	if _, err := s.Create("", "b", "", nil); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestUpdateReplacesTitleAndBody(t *testing.T) {
	s := newService(t)
	summary, err := s.Create("Old title", "old body", "", nil)
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
	summary, _ := s.Create("Title", "body", "", nil)
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
	s.Create("A", "1", "", nil)
	s.Create("B", "2", "", nil)
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
	s.Create("staging deploy", "x", "", nil)
	s.Create("prod deploy", "y", "", nil)
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
	s.Create("note one", "contains rocketship", "", nil)
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
	summary, _ := s.Create("x", "y", "", nil)
	if err := s.Delete(summary.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get(summary.ID); err == nil {
		t.Fatal("expected get to fail after delete")
	}
}

func TestCreateInFolderWithTags(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", []string{"work"})
	summary, err := s.Create("Standup", "body", folder.ID, []string{"standup"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if summary.FolderID != folder.ID {
		t.Fatalf("FolderID = %q", summary.FolderID)
	}
	if len(summary.EffectiveTags) != 2 {
		t.Fatalf("EffectiveTags = %#v", summary.EffectiveTags)
	}
}

func TestCreateRejectsUnknownFolder(t *testing.T) {
	s := newService(t)
	_, err := s.Create("T", "b", "deadbeefdeadbeefdeadbeef", nil)
	if err == nil {
		t.Fatal("expected folder not found")
	}
	if err.Error() != "folder not found" {
		t.Fatalf("Create error = %q, want folder not found", err.Error())
	}
}

func TestCreateRejectsNoteAsFolder(t *testing.T) {
	s := newService(t)
	note, err := s.Create("T", "b", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = s.Create("Other", "b", note.ID, nil)
	if err == nil {
		t.Fatal("expected folder not found")
	}
	if err.Error() != "folder not found" {
		t.Fatalf("Create error = %q, want folder not found", err.Error())
	}
}

func TestUpdatePreservesOrganization(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", []string{"work"})
	summary, _ := s.Create("T", "old", folder.ID, []string{"standup"})
	updated, err := s.Update(summary.ID, "T2", "new")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Body != "new" || updated.FolderID != folder.ID || updated.Tags[0] != "standup" {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestSetOrganizationAndLiveInheritance(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", []string{"work"})
	summary, _ := s.Create("T", "b", folder.ID, nil)
	if _, err := s.UpdateFolder(folder.ID, "Work", "blue", []string{"work", "q3"}); err != nil {
		t.Fatalf("UpdateFolder: %v", err)
	}
	got, err := s.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Body != "b" {
		t.Fatalf("body changed: %q", got.Body)
	}
	joined := strings.Join(got.EffectiveTags, ",")
	if !strings.Contains(joined, "work") || !strings.Contains(joined, "q3") {
		t.Fatalf("EffectiveTags = %#v", got.EffectiveTags)
	}
	if _, err := s.SetOrganization(summary.ID, "", []string{"keep"}, ""); err != nil {
		t.Fatalf("SetOrganization: %v", err)
	}
	moved, _ := s.Get(summary.ID)
	if moved.FolderID != "" || moved.EffectiveTags[0] != "keep" {
		t.Fatalf("moved = %+v", moved)
	}
	if moved.Body != "b" {
		t.Fatal("SetOrganization resealed or lost body")
	}
}

func TestListPreview(t *testing.T) {
	s := newService(t)
	s.Create("T", "hello   world from the body", "", nil)
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list[0].Preview != "hello world from the body" {
		t.Fatalf("Preview = %q", list[0].Preview)
	}
}

func TestSearchByTagAndFolderName(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Engineering", "blue", []string{"platform"})
	s.Create("N1", "alpha", folder.ID, []string{"standup"})
	s.Create("N2", "beta", "", nil)
	byTag, _ := s.Search("platform")
	if len(byTag) != 1 || byTag[0].Title != "N1" {
		t.Fatalf("byTag = %+v", byTag)
	}
	byFolder, _ := s.Search("engineer")
	if len(byFolder) != 1 || byFolder[0].Title != "N1" {
		t.Fatalf("byFolder = %+v", byFolder)
	}
	byOwn, _ := s.Search("standup")
	if len(byOwn) != 1 {
		t.Fatalf("byOwn = %+v", byOwn)
	}
}

func TestDeleteFolderCopyDown(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", []string{"work"})
	summary, _ := s.Create("T", "keep-body", folder.ID, []string{"standup"})
	if err := s.DeleteFolder(folder.ID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	got, _ := s.Get(summary.ID)
	if got.FolderID != "" {
		t.Fatalf("still in folder: %q", got.FolderID)
	}
	if got.Body != "keep-body" {
		t.Fatalf("body = %q", got.Body)
	}
	joined := strings.Join(got.Tags, ",")
	if !strings.Contains(joined, "standup") || !strings.Contains(joined, "work") {
		t.Fatalf("own tags after copy-down = %#v", got.Tags)
	}
	list, _ := s.ListFolders()
	if len(list) != 0 {
		t.Fatalf("folder still listed: %+v", list)
	}
}

func TestEffectiveColorFromFolder(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	summary, err := s.Create("T", "b", folder.ID, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if summary.Color != "" {
		t.Fatalf("Color = %q, want empty", summary.Color)
	}
	if summary.EffectiveColor != "blue" {
		t.Fatalf("EffectiveColor = %q, want blue", summary.EffectiveColor)
	}
}

func TestSetOrganizationOwnColorOverridesFolder(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	summary, _ := s.Create("T", "b", folder.ID, nil)
	updated, err := s.SetOrganization(summary.ID, folder.ID, nil, "red")
	if err != nil {
		t.Fatalf("SetOrganization: %v", err)
	}
	if updated.Color != "red" || updated.EffectiveColor != "red" {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestSetOrganizationRejectsInvalidColor(t *testing.T) {
	s := newService(t)
	summary, _ := s.Create("T", "b", "", nil)
	if _, err := s.SetOrganization(summary.ID, "", nil, "neon"); err == nil {
		t.Fatal("expected error for invalid color")
	}
}

func TestSetOrganizationClearingColorRevertsToFolder(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	summary, _ := s.Create("T", "b", folder.ID, nil)
	if _, err := s.SetOrganization(summary.ID, folder.ID, nil, "red"); err != nil {
		t.Fatalf("SetOrganization: %v", err)
	}
	cleared, err := s.SetOrganization(summary.ID, folder.ID, nil, "")
	if err != nil {
		t.Fatalf("SetOrganization: %v", err)
	}
	if cleared.Color != "" || cleared.EffectiveColor != "blue" {
		t.Fatalf("cleared = %+v", cleared)
	}
}

func TestDeleteFolderCopiesColorWhenInheriting(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	summary, _ := s.Create("T", "b", folder.ID, nil)
	if err := s.DeleteFolder(folder.ID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	got, _ := s.Get(summary.ID)
	if got.Color != "blue" {
		t.Fatalf("Color = %q, want blue", got.Color)
	}
	if got.EffectiveColor != "blue" {
		t.Fatalf("EffectiveColor = %q, want blue", got.EffectiveColor)
	}
}

func TestDeleteFolderKeepsExplicitNoteColor(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	summary, _ := s.Create("T", "b", folder.ID, nil)
	if _, err := s.SetOrganization(summary.ID, folder.ID, nil, "red"); err != nil {
		t.Fatalf("SetOrganization: %v", err)
	}
	if err := s.DeleteFolder(folder.ID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	got, _ := s.Get(summary.ID)
	if got.Color != "red" {
		t.Fatalf("Color = %q, want red (untouched)", got.Color)
	}
}

func TestSetOrganizationRejectsNonNoteRecord(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	if _, err := s.SetOrganization(folder.ID, "", nil, ""); err == nil {
		t.Fatal("expected error when setting org on non-note record")
	}
}

func TestDeleteFolderAtomicValidation(t *testing.T) {
	s := newService(t)
	folderTags := make([]string, 20)
	for i := range folderTags {
		folderTags[i] = fmt.Sprintf("ftag%d", i)
	}
	folder, err := s.CreateFolder("Work", "blue", folderTags)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	n1, _ := s.Create("N1", "b1", folder.ID, []string{"t1", "t2", "t3", "t4", "t5"})
	note2Tags := make([]string, 20)
	for i := range note2Tags {
		note2Tags[i] = fmt.Sprintf("ntag%d", i)
	}
	n2, _ := s.Create("N2", "b2", folder.ID, note2Tags)

	if err := s.DeleteFolder(folder.ID); err == nil {
		t.Fatal("expected DeleteFolder to fail due to tag limit")
	}

	gotN1, _ := s.Get(n1.ID)
	if gotN1.FolderID != folder.ID {
		t.Fatalf("Note 1 was detached prematurely: folder_id = %q", gotN1.FolderID)
	}
	gotN2, _ := s.Get(n2.ID)
	if gotN2.FolderID != folder.ID {
		t.Fatalf("Note 2 was detached: folder_id = %q", gotN2.FolderID)
	}
	folders, _ := s.ListFolders()
	if len(folders) != 1 {
		t.Fatalf("folder was deleted: len = %d", len(folders))
	}
}
