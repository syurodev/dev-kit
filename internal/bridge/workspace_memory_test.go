package bridge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/app"
	"devkit/internal/apperrors"
	"devkit/internal/identity"
	"devkit/internal/storage"
	"devkit/internal/workspace"
	"devkit/internal/workspacememory"
)

func memoryManagerFixture(t *testing.T) (*AppService, *app.Container, string) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	c := app.NewContainer()
	c.Workspace = workspace.NewService(workspace.NewSQLRepository(db))
	c.WorkspaceMemory = workspacememory.NewService(workspacememory.NewSQLRepository(db))
	ws, _, err := c.Workspace.CreateWorkspace("Memory workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	return NewAppService(c), c, ws.ID
}
func memoryManagerError(t *testing.T, err error, code string) {
	t.Helper()
	var ae *apperrors.AppError
	if !errors.As(err, &ae) || ae.Code != code {
		t.Fatalf("error=%v want %s", err, code)
	}
}
func TestWorkspaceMemoryManagerCRUDHistoryAndAudit(t *testing.T) {
	s, c, ws := memoryManagerFixture(t)
	ctx := context.Background()
	in := workspacememory.Input{Kind: "decision", Title: "Private service decision", Summary: "Searchable contracts summary", Body: "Private details never enter the audit"}
	entry, err := s.WorkspaceMemoryCreate(ctx, ws, in)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Source.Caller != "ui" || entry.Source.AgentType != "" {
		t.Fatalf("source=%+v", entry.Source)
	}
	page, err := s.WorkspaceMemoryBrowse(ctx, ws, workspacememory.BrowseInput{Query: "contracts", Limit: 20})
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	in.Summary = "Updated contracts summary"
	updated, err := s.WorkspaceMemoryUpdate(ctx, ws, entry.ID, entry.Revision, in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WorkspaceMemoryUpdate(ctx, ws, entry.ID, entry.Revision, in)
	memoryManagerError(t, err, "memory.revisionConflict")
	history, err := s.WorkspaceMemoryHistoryPage(ctx, ws, entry.ID, "", 1)
	if err != nil || len(history.Entries) != 1 || !history.HasMore || history.NextCursor == "" {
		t.Fatalf("history=%+v %v", history, err)
	}
	older, err := s.WorkspaceMemoryHistoryPage(ctx, ws, entry.ID, history.NextCursor, 1)
	if err != nil || len(older.Entries) != 1 || older.Entries[0].Revision != 1 {
		t.Fatalf("older=%+v %v", older, err)
	}
	revision, err := s.WorkspaceMemoryReadRevision(ctx, ws, entry.ID, 1)
	if err != nil || revision.Entry.Body != in.Body {
		t.Fatalf("revision=%+v %v", revision, err)
	}
	deleted, err := s.WorkspaceMemoryDelete(ctx, ws, entry.ID, updated.Revision)
	if err != nil || !deleted.Deleted {
		t.Fatalf("delete=%+v %v", deleted, err)
	}
	trash, err := s.WorkspaceMemoryBrowse(ctx, ws, workspacememory.BrowseInput{Deleted: true})
	if err != nil || len(trash.Entries) != 1 {
		t.Fatalf("trash=%+v %v", trash, err)
	}
	restored, err := s.WorkspaceMemoryRestore(ctx, ws, entry.ID, deleted.Revision, 1)
	if err != nil || restored.Deleted || restored.Revision != 4 {
		t.Fatalf("restore=%+v %v", restored, err)
	}
	audit := fmt.Sprint(c.Audit.Events())
	for _, private := range []string{in.Title, in.Summary, in.Body, "contracts"} {
		if strings.Contains(audit, private) {
			t.Fatalf("audit contains private memory text %q", private)
		}
	}
	if !strings.Contains(audit, "memory_restore") {
		t.Fatal("restore was not audited")
	}
}
func TestWorkspaceMemoryManagerScopeAndIdentity(t *testing.T) {
	s, c, ws := memoryManagerFixture(t)
	ctx := context.Background()
	in := workspacememory.Input{Kind: "knowledge", Title: "Title", Summary: "Summary", Body: "Body"}
	entry, err := s.WorkspaceMemoryCreate(ctx, ws, in)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := c.Workspace.CreateWorkspace("Other", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WorkspaceMemoryGet(ctx, other.ID, entry.ID)
	memoryManagerError(t, err, "memory.notFound")
	for _, caller := range []identity.Caller{identity.Agent("agent-id"), identity.UserTerminal()} {
		_, err = s.WorkspaceMemoryUpdate(identity.WithCaller(ctx, caller), ws, entry.ID, entry.Revision, in)
		memoryManagerError(t, err, codePermissionDenied)
	}
	current, err := s.WorkspaceMemoryGet(ctx, ws, entry.ID)
	if err != nil || current.Revision != 1 {
		t.Fatalf("denied update changed data: %+v %v", current, err)
	}
	_, err = s.WorkspaceMemoryBrowse(ctx, ws, workspacememory.BrowseInput{Cursor: "invalid"})
	memoryManagerError(t, err, codeValidationFailed)
	empty := NewAppService(app.NewContainer())
	_, err = empty.WorkspaceMemoryGet(ctx, ws, entry.ID)
	memoryManagerError(t, err, codeInternalError)
}
