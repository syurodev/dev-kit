package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devkit/internal/codegraph"
	"devkit/internal/workspace"
)

// twoFolderWorkspace is a workspace of two folders that both hold src/config.ts.
func twoFolderWorkspace(t *testing.T) (*WorkspaceCode, workspace.Workspace, []workspace.WorkspaceService) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"api", "web"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "src", "config.ts"), []byte("export const name = '"+name+"'\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := &Container{Workspace: workspace.NewService(workspace.NewMemoryRepository())}
	ws, services, err := c.Workspace.CreateWorkspace("w", []string{root})
	if err != nil || len(services) != 2 {
		t.Fatalf("workspace: %v %+v", err, services)
	}
	return NewWorkspaceCode(c), ws, services
}

func TestSameRelativePathInTwoFoldersIsTwoDocumentsAndSavesDoNotCross(t *testing.T) {
	code, ws, services := twoFolderWorkspace(t)
	a, b := services[0], services[1]

	ra, err := code.ReadFile(ws.ID, a.ID, "src/config.ts")
	if err != nil {
		t.Fatal(err)
	}
	rb, err := code.ReadFile(ws.ID, b.ID, "src/config.ts")
	if err != nil {
		t.Fatal(err)
	}
	if ra.Content == rb.Content {
		t.Fatal("the two folders' files must differ")
	}
	if _, err := code.WriteFile(ws.ID, a.ID, "src/config.ts", "changed\n", ra.Revision); err != nil {
		t.Fatal(err)
	}
	after, _ := code.ReadFile(ws.ID, b.ID, "src/config.ts")
	if after.Content != rb.Content {
		t.Fatal("saving the first folder's buffer wrote into the second")
	}
	// A save is checked against its own folder's revision, not the other's.
	if _, err := code.WriteFile(ws.ID, a.ID, "src/config.ts", "again\n", rb.Revision); !errors.Is(err, codegraph.ErrFileConflict) {
		t.Fatalf("err = %v, want a conflict (rb.Revision belongs to the other folder's file)", err)
	}
}

func TestChangingThePrimaryDoesNotChangeWhereASaveLands(t *testing.T) {
	code, ws, services := twoFolderWorkspace(t)
	owner := services[1] // not the primary
	read, _ := code.ReadFile(ws.ID, owner.ID, "src/config.ts")

	if _, err := code.SetPrimary(ws.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := code.WriteFile(ws.ID, owner.ID, "src/config.ts", "saved\n", read.Revision); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(owner.Path, "src", "config.ts"))
	if string(got) != "saved\n" {
		t.Fatalf("the save did not land in its owner: %q", got)
	}
	other, _ := os.ReadFile(filepath.Join(services[0].Path, "src", "config.ts"))
	if string(other) == "saved\n" {
		t.Fatal("the save landed in the other folder")
	}
}

func TestAFolderRemovedFromTheWorkspaceCannotBeWrittenAndANewOneWithTheSamePathIsAnotherFolder(t *testing.T) {
	code, ws, services := twoFolderWorkspace(t)
	gone := services[1]
	read, _ := code.ReadFile(ws.ID, gone.ID, "src/config.ts")

	if _, _, err := code.c.Workspace.UpdateWorkspace(ws.ID, "w", []string{services[0].Path}); err != nil {
		t.Fatal(err)
	}
	if _, err := code.WriteFile(ws.ID, gone.ID, "src/config.ts", "edit\n", read.Revision); !errors.Is(err, ErrFolderNotInWorkspace) {
		t.Fatalf("write to a removed folder err = %v", err)
	}
	if _, err := code.ReadFile(ws.ID, gone.ID, "src/config.ts"); !errors.Is(err, ErrFolderNotInWorkspace) {
		t.Fatalf("read of a removed folder err = %v", err)
	}
	disk, _ := os.ReadFile(filepath.Join(gone.Path, "src", "config.ts"))
	if string(disk) == "edit\n" {
		t.Fatal("a removed folder was written")
	}

	// Adding the same path back is a new folder with a new ID: a buffer opened
	// under the old ID does not follow it.
	_, again, err := code.c.Workspace.UpdateWorkspace(ws.ID, "w", []string{services[0].Path, gone.Path})
	if err != nil {
		t.Fatal(err)
	}
	var readded workspace.WorkspaceService
	for _, s := range again {
		if s.Path == gone.Path {
			readded = s
		}
	}
	if readded.ID == "" || readded.ID == gone.ID {
		t.Fatalf("re-added folder reused ID %q", readded.ID)
	}
	if _, err := code.WriteFile(ws.ID, gone.ID, "src/config.ts", "edit\n", read.Revision); !errors.Is(err, ErrFolderNotInWorkspace) {
		t.Fatalf("the old ID must stay invalid, err = %v", err)
	}
}

func TestAFolderMissingOnDiskRefusesTheSaveWithoutRecreatingIt(t *testing.T) {
	code, ws, services := twoFolderWorkspace(t)
	f := services[0]
	read, _ := code.ReadFile(ws.ID, f.ID, "src/config.ts")
	if err := os.RemoveAll(f.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := code.WriteFile(ws.ID, f.ID, "src/config.ts", "x", read.Revision); err == nil {
		t.Fatal("saving into a folder that vanished must fail")
	}
	if _, err := os.Stat(f.Path); err == nil {
		t.Fatal("the vanished folder was recreated")
	}
	states, err := code.FolderStates(context.Background(), ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range states {
		if st.FolderID == f.ID && (st.Accessible || st.Error == "") {
			t.Fatalf("state = %+v, want inaccessible with a reason", st)
		}
	}
}

func TestSearchRunsOverTheChosenFoldersOnlyAndNamesTheOwner(t *testing.T) {
	code, ws, services := twoFolderWorkspace(t)
	all, err := code.Search(context.Background(), SearchRequest{WorkspaceID: ws.ID, Query: "export const"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Files) != 2 || all.Files[0].FolderID == all.Files[1].FolderID {
		t.Fatalf("whole workspace = %+v", all.Files)
	}
	one, err := code.Search(context.Background(), SearchRequest{WorkspaceID: ws.ID, FolderIDs: []string{services[1].ID}, Query: "export const"})
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Files) != 1 || one.Files[0].FolderID != services[1].ID {
		t.Fatalf("scoped to one folder = %+v", one.Files)
	}
	if _, err := code.Search(context.Background(), SearchRequest{WorkspaceID: ws.ID, FolderIDs: []string{"ffffffffffffffffffffffff"}, Query: "x"}); !errors.Is(err, ErrFolderNotInWorkspace) {
		t.Fatalf("unknown folder err = %v", err)
	}
}

func TestANewSearchWithTheSameRequestIDSupersedesTheOldOne(t *testing.T) {
	code, ws, _ := twoFolderWorkspace(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := code.Search(ctx, SearchRequest{RequestID: "r1", WorkspaceID: ws.ID, Query: "x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context err = %v", err)
	}
	code.CancelSearch("r1") // forgetting a finished search is harmless
	code.mu.Lock()
	n := len(code.searches)
	code.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d search registrations leaked", n)
	}
}

func TestWatchDocumentsReportsEvents(t *testing.T) {
	code, ws, services := twoFolderWorkspace(t)
	got := make(chan []string, 4)
	if err := code.StartWatcher(func(keys []string, resync bool) { got <- keys }); err != nil {
		t.Fatal(err)
	}
	defer code.Close()
	doc := WatchedDocument{WorkspaceID: ws.ID, FolderID: services[0].ID, Path: "src/config.ts"}
	missing := WatchedDocument{WorkspaceID: ws.ID, FolderID: "ffffffffffffffffffffffff", Path: "x"}
	un := code.WatchDocuments([]WatchedDocument{doc, missing})
	if len(un) != 1 || un[0] != DocumentKey(missing) {
		t.Fatalf("unwatched = %v", un)
	}
	if err := os.WriteFile(filepath.Join(services[0].Path, "src", "config.ts"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case keys := <-got:
		if len(keys) != 1 || keys[0] != DocumentKey(doc) {
			t.Fatalf("keys = %v", keys)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
	}
}

func TestMergeIndexStatusExplainsAnEmptyList(t *testing.T) {
	empty := func(state string) codegraph.EntryPointsView {
		return codegraph.EntryPointsView{State: state, Entries: []codegraph.EntryPoint{}}
	}
	cases := []struct {
		name  string
		view  codegraph.EntryPointsView
		st    codegraph.ServiceIndexStatus
		state string
		stale bool
	}{
		{"queued", empty(codegraph.ViewNotIndexed), codegraph.ServiceIndexStatus{State: codegraph.IndexPending}, codegraph.ViewIndexing, false},
		{"refreshing keeps entries", empty(codegraph.ViewIndexed), codegraph.ServiceIndexStatus{State: codegraph.IndexIndexing}, codegraph.ViewIndexed, true},
		{"failed", empty(codegraph.ViewNotIndexed), codegraph.ServiceIndexStatus{State: codegraph.IndexFailed, Error: "boom"}, codegraph.ViewFailed, false},
		{"failed refresh keeps the old index", empty(codegraph.ViewIndexed), codegraph.ServiceIndexStatus{State: codegraph.IndexFailed, Error: "boom"}, codegraph.ViewIndexed, false},
		{"unsupported", empty(codegraph.ViewNotIndexed), codegraph.ServiceIndexStatus{State: codegraph.IndexUnsupported}, codegraph.ViewUnsupported, false},
	}
	for _, c := range cases {
		got := mergeIndexStatus(c.view, c.st)
		if got.State != c.state || got.Stale != c.stale {
			t.Errorf("%s: state %q stale %v, want %q %v", c.name, got.State, got.Stale, c.state, c.stale)
		}
	}
}
