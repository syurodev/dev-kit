package workspace_test

import (
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
	if ws.Root != root {
		t.Errorf("ws.Root = %s, want %s", ws.Root, root)
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
