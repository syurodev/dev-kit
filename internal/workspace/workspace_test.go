package workspace_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/workspace"
)

func mkGitDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
}

// AddWorkspace on a folder holding several git repos splits it into one
// service per repo — the exact case this increment exists for.
func TestAddWorkspace_SplitsMultipleRepos(t *testing.T) {
	root := t.TempDir()
	mkGitDir(t, filepath.Join(root, "billing-service"))
	mkGitDir(t, filepath.Join(root, "refund-service"))

	svc := workspace.NewService(workspace.NewMemoryRepository())
	ws, services, err := svc.AddWorkspace("", root)
	if err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	// Root is the primary service — the first one — not the folder picked.
	if ws.Root != services[0].Path {
		t.Errorf("ws.Root = %s, want the first service %s", ws.Root, services[0].Path)
	}
	if ws.Name != filepath.Base(root) {
		t.Errorf("ws.Name = %s, want %s (fallback to folder name)", ws.Name, filepath.Base(root))
	}
	if len(services) != 2 {
		t.Fatalf("len(services) = %d, want 2: %+v", len(services), services)
	}
	for _, s := range services {
		if s.WorkspaceID != ws.ID {
			t.Errorf("service %s workspace_id = %s, want %s", s.Name, s.WorkspaceID, ws.ID)
		}
		if s.NameSource != workspace.NameSourceFolder {
			t.Errorf("service %s NameSource = %s, want %s", s.Name, s.NameSource, workspace.NameSourceFolder)
		}
	}
}

// A folder with no nested repo becomes a single service: itself. This is
// also what happens when the picked folder is directly a repo — its .git is
// at the folder's own level, not one below, so the depth-1 scan still finds
// nothing and falls back the same way.
func TestAddWorkspace_NoNestedRepos_AddsFolderItself(t *testing.T) {
	root := t.TempDir()

	svc := workspace.NewService(workspace.NewMemoryRepository())
	ws, services, err := svc.AddWorkspace("acme", root)
	if err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("len(services) = %d, want 1: %+v", len(services), services)
	}
	if services[0].Path != root {
		t.Errorf("service path = %s, want root %s", services[0].Path, root)
	}
	if ws.Name != "acme" {
		t.Errorf("ws.Name = %s, want acme (explicit name kept)", ws.Name)
	}
}

func TestListWorkspacesAndDelete(t *testing.T) {
	root := t.TempDir()
	svc := workspace.NewService(workspace.NewMemoryRepository())
	ws, _, err := svc.AddWorkspace("acme", root)
	if err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	list, err := svc.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(list) != 1 || list[0].ID != ws.ID {
		t.Fatalf("ListWorkspaces = %+v, want [%+v]", list, ws)
	}

	if err := svc.DeleteWorkspace(ws.ID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	list, err = svc.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ListWorkspaces after delete = %+v, want empty", list)
	}
	services, err := svc.ListServices(ws.ID)
	if err != nil {
		t.Fatalf("ListServices after delete: %v", err)
	}
	if len(services) != 0 {
		t.Fatalf("ListServices after delete = %+v, want empty (cascade)", services)
	}
}

func TestAddWorkspace_MissingRootIsAnError(t *testing.T) {
	svc := workspace.NewService(workspace.NewMemoryRepository())
	if _, _, err := svc.AddWorkspace("", filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected an error for a root that does not exist")
	}
}

// A workspace is created by name first; folders can come later.
func TestCreateWorkspace_NameFirstFoldersLater(t *testing.T) {
	svc := workspace.NewService(workspace.NewMemoryRepository())
	if _, _, err := svc.CreateWorkspace("  ", nil); !errors.Is(err, workspace.ErrNameRequired) {
		t.Fatalf("blank name: err = %v, want ErrNameRequired", err)
	}
	ws, services, err := svc.CreateWorkspace("fsap", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "fsap" || ws.Root != "" || len(services) != 0 {
		t.Fatalf("ws = %+v, services = %+v", ws, services)
	}
}

func TestMatchingWorkspaceIDsIncludesSharedService(t *testing.T) {
	shared, a, b := t.TempDir(), t.TempDir(), t.TempDir()
	svc := workspace.NewService(workspace.NewMemoryRepository())
	first, _, err := svc.CreateWorkspace("first", []string{a, shared})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := svc.CreateWorkspace("second", []string{b, shared})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := svc.MatchingWorkspaceIDs(shared)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || !(ids[0] == first.ID && ids[1] == second.ID || ids[0] == second.ID && ids[1] == first.ID) {
		t.Fatalf("shared project matches = %v", ids)
	}
	if _, _, err := svc.UpdateWorkspace(first.ID, "first", []string{shared, a}); err != nil {
		t.Fatal(err)
	}
	ids, err = svc.MatchingWorkspaceIDs(shared)
	if err != nil || len(ids) != 2 {
		t.Fatalf("after primary change: %v, %v", ids, err)
	}
}

// Update keeps what is still listed (a manual name survives), discovers what
// is new, drops what is gone, and makes the first entry the primary.
func TestUpdateWorkspace_AddsRemovesAndReordersFolders(t *testing.T) {
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	svc := workspace.NewService(workspace.NewMemoryRepository())
	var added []string
	svc.OnServicesAdded = func(s []workspace.WorkspaceService) {
		for _, x := range s {
			added = append(added, x.Path)
		}
	}
	ws, services, err := svc.CreateWorkspace("fsap", []string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Root != a || len(services) != 2 {
		t.Fatalf("create: ws = %+v, services = %+v", ws, services)
	}
	keptID := services[1].ID
	added = nil

	ws, services, err = svc.UpdateWorkspace(ws.ID, "fsap platform", []string{b, c})
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "fsap platform" || ws.Root != b {
		t.Fatalf("ws = %+v, want renamed with b primary", ws)
	}
	if len(services) != 2 || services[0].Path != b || services[0].ID != keptID || services[1].Path != c {
		t.Fatalf("services = %+v, want b (same row) then c", services)
	}
	if len(added) != 1 || added[0] != c {
		t.Fatalf("added = %v, want only c queued", added)
	}
	stored, _ := svc.ListServices(ws.ID)
	if len(stored) != 2 {
		t.Fatalf("stored = %+v, want a removed", stored)
	}
	if _, _, err := svc.UpdateWorkspace("nope", "x", nil); !errors.Is(err, workspace.ErrWorkspaceNotFound) {
		t.Fatalf("unknown id: err = %v", err)
	}
}
