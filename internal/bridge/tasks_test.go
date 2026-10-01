package bridge

import (
	"path/filepath"
	"testing"

	"devkit/internal/app"
	"devkit/internal/storage"
	"devkit/internal/tasks"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
	"devkit/internal/workspace"
)

// newBridgeWithTasksWorkspace wires a real vault, a real sqlite handle for the
// board-bindings table, and a workspace with one service — the setup Bind and
// Unbind need to do anything, since a nil db (an ephemeral app.NewContainer())
// leaves TasksBoardBind with no table to write to.
func newBridgeWithTasksWorkspace(t *testing.T) (*AppService, *app.Container, workspace.Workspace) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(t.TempDir(), "devkit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := storage.Migrate(t.Context(), db); err != nil {
		t.Fatal(err)
	}

	container := app.NewContainer()
	container.Vault = v
	container.Items = vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	container.Tasks = tasks.NewService(container.Items)
	// The bindings table's workspace_id has a real foreign key into
	// `workspaces`, so the registry has to be the same sqlite handle — a
	// memory-backed one leaves no row for it to reference.
	container.Workspace = workspace.NewService(workspace.NewSQLRepository(db))

	ws, _, err := container.Workspace.AddWorkspace("Acme", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// The scope wiring a real run does in main.go, minus Agent Hub: the bridge
	// caller here is always the user (tasks.Caller{}), which the binding never
	// restricts, so a nil hub is enough to exercise Bind/Unbind/Create/SetVerify.
	app.WireAgentLiveness(container, db, storage.TableTaskBoardBindings)

	return NewAppService(container), container, ws
}

func TestTasksBoardCreateBindsWhenGivenAWorkspace(t *testing.T) {
	svc, _, ws := newBridgeWithTasksWorkspace(t)

	board, err := svc.TasksBoardCreate("DevKit", "DEV", "blue", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if board.WorkspaceID != ws.ID {
		t.Fatalf("workspaceId = %q, want %q", board.WorkspaceID, ws.ID)
	}
}

// The plan's out-of-scope note for board create: no workspace named means no
// bind attempted, and the board is simply unbound — not an error.
func TestTasksBoardCreateWithNoWorkspaceLeavesItUnbound(t *testing.T) {
	svc, _, _ := newBridgeWithTasksWorkspace(t)

	board, err := svc.TasksBoardCreate("DevKit", "DEV", "blue", "")
	if err != nil {
		t.Fatal(err)
	}
	if board.WorkspaceID != "" {
		t.Fatalf("workspaceId = %q, want unbound", board.WorkspaceID)
	}
}

func TestTasksBoardBindAndUnbind(t *testing.T) {
	svc, _, ws := newBridgeWithTasksWorkspace(t)

	board, err := svc.TasksBoardCreate("DevKit", "DEV", "blue", "")
	if err != nil {
		t.Fatal(err)
	}

	bound, err := svc.TasksBoardBind(board.ID, ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bound.WorkspaceID != ws.ID {
		t.Fatalf("workspaceId after bind = %q, want %q", bound.WorkspaceID, ws.ID)
	}

	unbound, err := svc.TasksBoardUnbind(board.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unbound.WorkspaceID != "" {
		t.Fatalf("workspaceId after unbind = %q, want empty", unbound.WorkspaceID)
	}
}

// Binding to a workspace that does not exist is refused rather than silently
// recorded — a dangling binding would make a board look bound to nothing.
func TestTasksBoardBindRejectsUnknownWorkspace(t *testing.T) {
	svc, _, _ := newBridgeWithTasksWorkspace(t)

	board, err := svc.TasksBoardCreate("DevKit", "DEV", "blue", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TasksBoardBind(board.ID, "ws-does-not-exist"); err == nil {
		t.Fatal("bound to a workspace that does not exist")
	}
}

// TasksBoardSetVerify stores verify.service and leaves ProjectPath alone: it
// is the deprecated fallback the legacy verifyDir path still reads, and this
// call must not be the thing that erases it out from under a board that has
// not been migrated to a binding yet.
func TestTasksBoardSetVerifyStoresServiceAndKeepsProjectPath(t *testing.T) {
	svc, container, _ := newBridgeWithTasksWorkspace(t)

	board, err := svc.TasksBoardCreate("DevKit", "DEV", "blue", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := container.Tasks.UpdateBoard(board.ID, func(b *tasks.Board) {
		b.ProjectPath = "/legacy/path"
	}); err != nil {
		t.Fatal(err)
	}

	updated, err := svc.TasksBoardSetVerify(board.ID, "go test ./...", 600, "billing-service")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Verify == nil || updated.Verify.Command != "go test ./..." || updated.Verify.Service != "billing-service" {
		t.Fatalf("verify = %+v", updated.Verify)
	}
	if updated.ProjectPath != "/legacy/path" {
		t.Fatalf("projectPath = %q, want untouched", updated.ProjectPath)
	}

	cleared, err := svc.TasksBoardSetVerify(board.ID, "", 600, "")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Verify != nil {
		t.Fatalf("verify = %+v, want cleared", cleared.Verify)
	}
}

// A patch writes exactly the fields it carries: a nil one is untouched and an
// empty one clears. The positional form read "" as "leave it", so a priority
// set from the UI could never be taken off again.
func TestTasksSetFieldsClearsWhatThePatchCarries(t *testing.T) {
	svc, _, ws := newBridgeWithTasksWorkspace(t)
	board, err := svc.TasksBoardCreate("DevKit", "DEV", "blue", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := svc.TasksCreate(board.ID, "Blocker", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.TasksCreate(board.ID, "Work", "", "high", "", []string{"ui"})
	if err != nil {
		t.Fatal(err)
	}

	assignee := "me"
	blockedBy := []string{blocker.ID}
	task, err = svc.TasksSetFields(task.ID, TaskFieldsPatch{Assignee: &assignee, BlockedBy: &blockedBy})
	if err != nil {
		t.Fatal(err)
	}
	if task.Priority != "high" || len(task.Labels) != 1 {
		t.Fatalf("fields the patch did not carry changed: %+v", task)
	}
	if task.Assignee != "me" || len(task.BlockedBy) != 1 || task.BlockedBy[0] != blocker.ID {
		t.Fatalf("patch not applied: %+v", task)
	}

	empty := ""
	none := []string{}
	task, err = svc.TasksSetFields(task.ID, TaskFieldsPatch{Priority: &empty, Labels: &none, BlockedBy: &none})
	if err != nil {
		t.Fatal(err)
	}
	if task.Priority != "" || len(task.Labels) != 0 || len(task.BlockedBy) != 0 {
		t.Fatalf("clearing did not clear: %+v", task)
	}
	if task.Assignee != "me" {
		t.Fatalf("assignee was not in the patch but changed to %q", task.Assignee)
	}
}

func TestTasksUnarchiveReturnsTheTaskToItsBoard(t *testing.T) {
	svc, _, ws := newBridgeWithTasksWorkspace(t)
	board, err := svc.TasksBoardCreate("DevKit", "DEV", "blue", ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.TasksCreate(board.ID, "Work", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TasksArchive(task.ID); err != nil {
		t.Fatal(err)
	}
	if listed, _ := svc.TasksList(board.ID, "", "", "", false, 0); len(listed) != 0 {
		t.Fatalf("archived task still listed: %d", len(listed))
	}
	back, err := svc.TasksUnarchive(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ArchivedAt != "" {
		t.Fatalf("still archived: %q", back.ArchivedAt)
	}
	if listed, _ := svc.TasksList(board.ID, "", "", "", false, 0); len(listed) != 1 {
		t.Fatalf("unarchived task not listed: %d", len(listed))
	}
}
