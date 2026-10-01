package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/app"
	"devkit/internal/workspace"
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

	result, err := svc.WorkspaceCreate("acme", []string{root}, "")
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
	if _, err := svc.WorkspaceCreate("", []string{t.TempDir()}, ""); err == nil {
		t.Fatal("a blank name must be refused")
	}
	result, err := svc.WorkspaceCreate("fsap", nil, "")
	if err != nil {
		t.Fatalf("WorkspaceCreate: %v", err)
	}
	if len(result.Services) != 0 {
		t.Fatalf("services = %+v, want none yet", result.Services)
	}

	root := t.TempDir() // no nested repos: the folder is the service
	updated, err := svc.WorkspaceUpdate(result.Workspace.ID, "fsap", []string{root}, "")
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
	result, err := svc.WorkspaceCreate("acme", []string{root}, "")
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

func TestWorkspaceInstructionsPersistWithoutAuditingContents(t *testing.T) {
	container := app.NewContainer()
	svc := NewAppService(container)
	initial := "Private initial workspace instruction — do not log this."
	replacement := "Private updated workspace instruction — do not log this either."
	created, err := svc.WorkspaceCreate("first", nil, initial)
	if err != nil {
		t.Fatal(err)
	}
	if created.Workspace.Instructions != initial {
		t.Fatal("create lost instructions")
	}
	updated, err := svc.WorkspaceUpdate(created.Workspace.ID, "renamed", nil, replacement)
	if err != nil || updated.Workspace.Instructions != replacement {
		t.Fatalf("update instructions = %+v, %v", updated, err)
	}
	listed, err := svc.WorkspaceList()
	if err != nil || len(listed) != 1 || listed[0] != updated.Workspace {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
	auditText := fmt.Sprint(container.Audit.Events())
	for _, text := range []string{initial, replacement} {
		if strings.Contains(auditText, text) {
			t.Fatal("audit contains instruction contents")
		}
	}
	for _, action := range []string{"create_workspace", "update_workspace"} {
		if !strings.Contains(auditText, action) {
			t.Fatalf("%s was not audited", action)
		}
	}
	oversized := strings.Repeat("a", workspace.MaxInstructionsBytes+1)
	if _, err := svc.WorkspaceCreate("invalid", nil, oversized); err == nil || !strings.Contains(err.Error(), "validation_failed") {
		t.Fatalf("oversized create = %v", err)
	}
	if _, err := svc.WorkspaceUpdate(created.Workspace.ID, "invalid rename", []string{t.TempDir()}, oversized); err == nil || !strings.Contains(err.Error(), "validation_failed") {
		t.Fatalf("oversized update = %v", err)
	}
	listed, err = svc.WorkspaceList()
	if err != nil || len(listed) != 1 || listed[0] != updated.Workspace {
		t.Fatalf("invalid save changed workspace: %+v, %v", listed, err)
	}
	folders, err := svc.WorkspaceListServices(created.Workspace.ID)
	if err != nil || len(folders) != 0 {
		t.Fatalf("invalid save changed folders: %+v, %v", folders, err)
	}
	if _, err := svc.WorkspaceUpdate("missing", "name", nil, "text"); err == nil {
		t.Fatal("missing workspace accepted")
	}
}
