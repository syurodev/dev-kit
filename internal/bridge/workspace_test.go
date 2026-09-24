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

func TestWorkspaceCreateSplitsMultipleRepos(t *testing.T) {
	svc := NewAppService(app.NewContainer())
	root := t.TempDir()
	mkGitDir(t, filepath.Join(root, "billing-service"))
	mkGitDir(t, filepath.Join(root, "refund-service"))

	result, err := svc.WorkspaceCreate("acme", []string{root})
	if err != nil {
		t.Fatalf("WorkspaceCreate: %v", err)
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

func TestWorkspaceCreateNeedsANameButNotFolders(t *testing.T) {
	svc := NewAppService(app.NewContainer())
	if _, err := svc.WorkspaceCreate("", []string{t.TempDir()}); err == nil {
		t.Fatal("a blank name must be refused")
	}
	result, err := svc.WorkspaceCreate("fsap", nil)
	if err != nil {
		t.Fatalf("WorkspaceCreate: %v", err)
	}
	if len(result.Services) != 0 {
		t.Fatalf("services = %+v, want none yet", result.Services)
	}

	root := t.TempDir() // no nested repos: the folder is the service
	updated, err := svc.WorkspaceUpdate(result.Workspace.ID, "fsap", []string{root})
	if err != nil {
		t.Fatalf("WorkspaceUpdate: %v", err)
	}
	if len(updated.Services) != 1 || updated.Services[0].Path != root || updated.Workspace.Root != root {
		t.Fatalf("updated = %+v, want the folder as its one, primary service", updated)
	}
}

func TestWorkspaceDeleteRemovesServices(t *testing.T) {
	svc := NewAppService(app.NewContainer())
	root := t.TempDir()
	result, err := svc.WorkspaceCreate("acme", []string{root})
	if err != nil {
		t.Fatalf("WorkspaceCreate: %v", err)
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
