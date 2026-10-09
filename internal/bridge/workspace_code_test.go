package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/apperrors"
	"devkit/internal/app"
)

func publicCode(err error) string {
	var app *apperrors.AppError
	if errors.As(err, &app) {
		return app.Code
	}
	return ""
}

func codeWorkspace(t *testing.T) (*AppService, WorkspaceAddResult) {
	t.Helper()
	svc := NewAppService(app.NewContainer())
	root := t.TempDir()
	for _, name := range []string{"api", "web"} {
		mkGitDir(t, filepath.Join(root, name))
		if err := os.WriteFile(filepath.Join(root, name, "main.txt"), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := svc.WorkspaceCreate("ws", []string{root}, "")
	if err != nil || len(result.Services) != 2 {
		t.Fatalf("create: %v %+v", err, result)
	}
	return svc, result
}

func TestWorkspaceFileSaveConflictAndRemovedFolderHaveTheirOwnCodes(t *testing.T) {
	svc, ws := codeWorkspace(t)
	folder := ws.Services[0]

	read, err := svc.WorkspaceFileRead(ws.Workspace.ID, folder.ID, "main.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder.Path, "main.txt"), []byte("someone else\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = svc.WorkspaceFileWrite(ws.Workspace.ID, folder.ID, "main.txt", "mine\n", read.Revision)
	if publicCode(err) != "file_conflict" {
		t.Fatalf("code = %q (%v), want file_conflict", publicCode(err), err)
	}

	stat, err := svc.WorkspaceFileStat(ws.Workspace.ID, folder.ID, "main.txt")
	if err != nil || !stat.Exists || stat.Revision == read.Revision {
		t.Fatalf("stat = %+v, %v", stat, err)
	}
	if _, err := svc.WorkspaceFileWrite(ws.Workspace.ID, folder.ID, "main.txt", "mine\n", stat.Revision); err != nil {
		t.Fatalf("a save based on the current revision must land: %v", err)
	}

	if _, err := svc.WorkspaceUpdate(ws.Workspace.ID, "ws", []string{ws.Services[1].Path}, ""); err != nil {
		t.Fatal(err)
	}
	_, err = svc.WorkspaceFileWrite(ws.Workspace.ID, folder.ID, "main.txt", "late\n", stat.Revision)
	if publicCode(err) != "folder_not_in_workspace" {
		t.Fatalf("code = %q (%v), want folder_not_in_workspace", publicCode(err), err)
	}
}

func TestWorkspaceSearchReportsAnInvalidRegexAsItsOwnCode(t *testing.T) {
	svc, ws := codeWorkspace(t)
	_, err := svc.WorkspaceSearch(context.Background(), app.SearchRequest{WorkspaceID: ws.Workspace.ID, Query: "(", Regex: true})
	if publicCode(err) != "invalid_query" {
		t.Fatalf("code = %q (%v), want invalid_query", publicCode(err), err)
	}
	res, err := svc.WorkspaceSearch(context.Background(), app.SearchRequest{WorkspaceID: ws.Workspace.ID, Query: "api"})
	if err != nil || len(res.Files) != 1 {
		t.Fatalf("search = %+v, %v", res, err)
	}
}

func TestWorkspaceSetPrimaryIsAWorkspaceWrite(t *testing.T) {
	svc, ws := codeWorkspace(t)
	second := ws.Services[1]
	got, err := svc.WorkspaceSetPrimary(ws.Workspace.ID, second.ID)
	if err != nil || got.Root != second.Path {
		t.Fatalf("SetPrimary = %+v, %v", got, err)
	}
	if _, err := svc.WorkspaceSetPrimary(ws.Workspace.ID, "ffffffffffffffffffffffff"); publicCode(err) != "folder_not_in_workspace" {
		t.Fatalf("unknown folder code = %q", publicCode(err))
	}
}
