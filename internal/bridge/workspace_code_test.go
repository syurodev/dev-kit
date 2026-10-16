package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/app"
	"devkit/internal/apperrors"
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

// Every Workspace* method the editor calls must be allowed for the UI caller
// by the real default grants. A method whose (capability, scope) pair no grant
// covers answers "permission_denied" to the person, which a unit test of the
// code behind it never sees — Reindex shipped that way.
func TestEveryWorkspaceBridgeMethodIsGrantedToTheUI(t *testing.T) {
	svc, ws := codeWorkspace(t)
	ctx := context.Background()
	w, f := ws.Workspace.ID, ws.Services[0]
	folderIDs := []string{f.ID}

	calls := map[string]func() error{
		"WorkspaceFolderStates": func() error { _, err := svc.WorkspaceFolderStates(ctx, w); return err },
		"WorkspaceFileList":     func() error { _, err := svc.WorkspaceFileList(ctx, w, f.ID); return err },
		"WorkspaceFileRead":     func() error { _, err := svc.WorkspaceFileRead(w, f.ID, "main.txt"); return err },
		"WorkspaceFileStat":     func() error { _, err := svc.WorkspaceFileStat(w, f.ID, "main.txt"); return err },
		"WorkspaceFileWrite":    func() error { _, err := svc.WorkspaceFileWrite(w, f.ID, "new.txt", "x", "absent"); return err },
		"WorkspaceGitStatus":    func() error { _, err := svc.WorkspaceGitStatus(ctx, w, f.ID); return err },
		"WorkspaceEntryPoints":  func() error { _, err := svc.WorkspaceEntryPoints(ctx, w, f.ID); return err },
		"WorkspacePdfInfo":      func() error { _, err := svc.WorkspacePdfInfo(ctx, w, f.ID, "main.txt"); return err },
		"WorkspacePdfPage":      func() error { _, err := svc.WorkspacePdfPage(ctx, w, f.ID, "main.txt", "", 1, 0); return err },
		"WorkspacePdfSearch":    func() error { _, err := svc.WorkspacePdfSearch(ctx, w, f.ID, "main.txt", "", "x"); return err },
		"WorkspaceFileOutline":  func() error { _, err := svc.WorkspaceFileOutline(ctx, w, f.ID, "main.txt"); return err },
		"WorkspaceSearchSymbols": func() error {
			_, err := svc.WorkspaceSearchSymbols(ctx, w, folderIDs, "main")
			return err
		},
		"WorkspaceSearch": func() error {
			_, err := svc.WorkspaceSearch(ctx, app.SearchRequest{WorkspaceID: w, Query: "api"})
			return err
		},
		"WorkspaceSearchCancel":     func() error { return svc.WorkspaceSearchCancel("none") },
		"WorkspaceSetPrimary":       func() error { _, err := svc.WorkspaceSetPrimary(w, ws.Services[1].ID); return err },
		"WorkspaceReindex":          func() error { return svc.WorkspaceReindex(w, folderIDs, false) },
		"WorkspaceReindex (cancel)": func() error { return svc.WorkspaceReindex(w, folderIDs, true) },
		"WorkspaceReindexService":   func() error { return svc.WorkspaceReindexService(f.Path) },
		"WorkspaceWatchDocuments": func() error {
			_, err := svc.WorkspaceWatchDocuments([]app.WatchedDocument{{WorkspaceID: w, FolderID: f.ID, Path: "main.txt"}})
			return err
		},
		"WorkspaceIndexStatus":       func() error { _, err := svc.WorkspaceIndexStatus(w); return err },
		"WorkspacePlanProgress":      func() error { _, err := svc.WorkspacePlanProgress(ctx, w, f.ID); return err },
		"WorkspacePlanFlow":          func() error { _, err := svc.WorkspacePlanFlow(ctx, w, f.ID); return err },
		"WorkspacePlanCommentCounts": func() error { _, err := svc.WorkspacePlanCommentCounts(ctx, w, f.ID); return err },
	}
	for name, call := range calls {
		if err := call(); err != nil && publicCode(err) == "permission_denied" {
			t.Errorf("%s: permission_denied for the UI caller — add the grant for its (capability, scope)", name)
		}
	}
}
