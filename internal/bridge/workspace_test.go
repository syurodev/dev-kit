package bridge

import (
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/app"
)

func mkGitDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
}

func TestWorkspaceAddSplitsMultipleRepos(t *testing.T) {
	svc := NewAppService(app.NewContainer())
	root := t.TempDir()
	mkGitDir(t, filepath.Join(root, "billing-service"))
	mkGitDir(t, filepath.Join(root, "refund-service"))

	result, err := svc.WorkspaceAdd("acme", root)
	if err != nil {
		t.Fatalf("WorkspaceAdd: %v", err)
	}
	if result.Workspace.Name != "acme" {
		t.Fatalf("workspace name = %q, want acme", result.Workspace.Name)
	}
	if len(result.Services) != 2 {
		t.Fatalf("len(services) = %d, want 2: %+v", len(result.Services), result.Services)
	}

	list, err := svc.WorkspaceList()
	if err != nil {
		t.Fatalf("WorkspaceList: %v", err)
	}
	if len(list) != 1 || list[0].ID != result.Workspace.ID {
		t.Fatalf("WorkspaceList = %+v, want [%+v]", list, result.Workspace)
	}

	services, err := svc.WorkspaceListServices(result.Workspace.ID)
	if err != nil {
		t.Fatalf("WorkspaceListServices: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("WorkspaceListServices = %+v, want 2 entries", services)
	}
}

func TestWorkspaceAddFallsBackToFolderItself(t *testing.T) {
	svc := NewAppService(app.NewContainer())
	root := t.TempDir() // no nested repos

	result, err := svc.WorkspaceAdd("", root)
	if err != nil {
		t.Fatalf("WorkspaceAdd: %v", err)
	}
	if len(result.Services) != 1 || result.Services[0].Path != root {
		t.Fatalf("services = %+v, want a single service at root %s", result.Services, root)
	}
	if result.Workspace.Name != filepath.Base(root) {
		t.Fatalf("workspace name = %q, want folder name fallback %q", result.Workspace.Name, filepath.Base(root))
	}
}

func TestWorkspaceDeleteRemovesServices(t *testing.T) {
	svc := NewAppService(app.NewContainer())
	root := t.TempDir()
	result, err := svc.WorkspaceAdd("acme", root)
	if err != nil {
		t.Fatalf("WorkspaceAdd: %v", err)
	}

	if err := svc.WorkspaceDelete(result.Workspace.ID); err != nil {
		t.Fatalf("WorkspaceDelete: %v", err)
	}
	list, err := svc.WorkspaceList()
	if err != nil {
		t.Fatalf("WorkspaceList: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("WorkspaceList after delete = %+v, want empty", list)
	}
}

// The folder picker follows TasksBoardChooseProject's contract: cancelling
// returns "" and is not an error.
func TestWorkspaceChooseFolderPassesStartInAndReturnsChoice(t *testing.T) {
	svc := NewAppService(app.NewContainer())
	var gotStartIn string
	svc.openDirDialog = func(_, startIn string) (string, error) {
		gotStartIn = startIn
		return "/acme", nil
	}

	got, err := svc.WorkspaceChooseFolder("/home")
	if err != nil {
		t.Fatalf("WorkspaceChooseFolder: %v", err)
	}
	if got != "/acme" {
		t.Fatalf("path = %q, want /acme", got)
	}
	if gotStartIn != "/home" {
		t.Fatalf("startIn = %q, want /home", gotStartIn)
	}
}

func TestWorkspaceChooseFolderCancelIsNotAnError(t *testing.T) {
	svc := NewAppService(app.NewContainer())
	svc.openDirDialog = func(string, string) (string, error) { return "", nil }

	got, err := svc.WorkspaceChooseFolder("")
	if err != nil {
		t.Fatalf("cancel reported as error: %v", err)
	}
	if got != "" {
		t.Fatalf("path = %q, want empty", got)
	}
}
