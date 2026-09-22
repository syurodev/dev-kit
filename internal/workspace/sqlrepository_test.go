package workspace_test

import (
	"context"
	"path/filepath"
	"testing"

	"devkit/internal/storage"
	"devkit/internal/workspace"
)

func newTestSQLRepo(t *testing.T) *workspace.SQLRepository {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return workspace.NewSQLRepository(db)
}

func TestSQLRepository_CreateAndListRoundTrip(t *testing.T) {
	repo := newTestSQLRepo(t)
	ws := workspace.Workspace{ID: "000000000000000000000001", Name: "acme", Root: "/acme"}
	services := []workspace.WorkspaceService{
		{ID: "000000000000000000000002", WorkspaceID: ws.ID, Name: "billing", Path: "/acme/billing", NameSource: workspace.NameSourceFolder},
		{ID: "000000000000000000000003", WorkspaceID: ws.ID, Name: "refund", Path: "/acme/refund", NameSource: workspace.NameSourceFolder},
	}
	if err := repo.CreateWorkspace(ws, services); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	got, err := repo.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(got) != 1 || got[0] != ws {
		t.Fatalf("ListWorkspaces = %+v, want [%+v]", got, ws)
	}

	gotServices, err := repo.ListServices(ws.ID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(gotServices) != 2 {
		t.Fatalf("ListServices = %+v, want 2 entries", gotServices)
	}
}

func TestSQLRepository_DeleteCascadesServices(t *testing.T) {
	repo := newTestSQLRepo(t)
	ws := workspace.Workspace{ID: "000000000000000000000004", Name: "acme", Root: "/acme"}
	services := []workspace.WorkspaceService{
		{ID: "000000000000000000000005", WorkspaceID: ws.ID, Name: "billing", Path: "/acme/billing", NameSource: workspace.NameSourceFolder},
	}
	if err := repo.CreateWorkspace(ws, services); err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	if err := repo.DeleteWorkspace(ws.ID); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}

	list, err := repo.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ListWorkspaces after delete = %+v, want empty", list)
	}
	gotServices, err := repo.ListServices(ws.ID)
	if err != nil {
		t.Fatalf("ListServices after delete: %v", err)
	}
	if len(gotServices) != 0 {
		t.Fatalf("ListServices after delete = %+v, want empty", gotServices)
	}
}

// A duplicate workspace ID collides on the primary key, and the whole write
// — workspace row and every service row — must not partially land.
func TestSQLRepository_CreateWorkspaceIsAtomic(t *testing.T) {
	repo := newTestSQLRepo(t)
	ws := workspace.Workspace{ID: "000000000000000000000006", Name: "acme", Root: "/acme"}
	if err := repo.CreateWorkspace(ws, nil); err != nil {
		t.Fatalf("first CreateWorkspace: %v", err)
	}

	dupe := workspace.Workspace{ID: ws.ID, Name: "acme-again", Root: "/acme-again"}
	services := []workspace.WorkspaceService{
		{ID: "000000000000000000000007", WorkspaceID: dupe.ID, Name: "billing", Path: "/acme-again/billing", NameSource: workspace.NameSourceFolder},
	}
	if err := repo.CreateWorkspace(dupe, services); err == nil {
		t.Fatal("expected a primary key conflict on the duplicate workspace id")
	}

	gotServices, err := repo.ListServices(ws.ID)
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(gotServices) != 0 {
		t.Fatalf("ListServices = %+v, want empty — the failed create must not have left a service row behind", gotServices)
	}
}
