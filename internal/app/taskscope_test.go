package app

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/storage"
	"devkit/internal/tasks"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
	"devkit/internal/workspace"
)

// sessionHub is the Agent Hub surface taskScope needs (agentSessions), stood
// up from a fixed table of sessions rather than a real hub.
type sessionHub map[string]agenthub.AgentSession

func (h sessionHub) GetStatus(_ context.Context, id string) (agenthub.AgentSession, error) {
	session, ok := h[id]
	if !ok {
		return agenthub.AgentSession{}, errors.New("no such session")
	}
	return session, nil
}

// twoWorkspaces registers two single-service workspaces over real temp
// folders (AddWorkspace's auto-discovery reads the filesystem) and returns
// them, primary path first for each.
func twoWorkspaces(t *testing.T) (ws *workspace.Service, a, b workspace.Workspace) {
	t.Helper()
	ws = workspace.NewService(workspace.NewMemoryRepository())
	a, _, err := ws.AddWorkspace("A", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, _, err = ws.AddWorkspace("B", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return ws, a, b
}

// A session's own working directory is enough to own a workspace, with no
// grant needed — the common case, one agent in one service.
func TestTaskScopeSessionWorkspacesByCwd(t *testing.T) {
	ws, a, _ := twoWorkspaces(t)
	scope := newTaskScope(ws, sessionHub{
		"in-a": {ID: "in-a", ProjectPath: a.Root},
	})

	got := scope.SessionWorkspaces("in-a")
	if len(got) != 1 || got[0] != a.ID {
		t.Fatalf("SessionWorkspaces(in-a) = %v, want [%s]", got, a.ID)
	}
}

// A granted path beside the session's own cwd — the jail widening Agent Hub
// gives a "whole workspace" session — must also count, even into a second
// workspace the cwd itself is nowhere near.
func TestTaskScopeSessionWorkspacesByGrantedPath(t *testing.T) {
	ws, _, b := twoWorkspaces(t)
	scope := newTaskScope(ws, sessionHub{
		"granted-b": {ID: "granted-b", ProjectPath: t.TempDir(), ExtraReadWritePaths: []string{b.Root}},
	})

	got := scope.SessionWorkspaces("granted-b")
	if len(got) != 1 || got[0] != b.ID {
		t.Fatalf("SessionWorkspaces(granted-b) = %v, want [%s]", got, b.ID)
	}
}

// A session nobody has heard of must read as no workspaces, not every one —
// this is what makes an unresolved session fail closed rather than open.
func TestTaskScopeSessionWorkspacesUnknownSessionIsNil(t *testing.T) {
	ws, _, _ := twoWorkspaces(t)
	scope := newTaskScope(ws, sessionHub{})
	if got := scope.SessionWorkspaces("vanished"); got != nil {
		t.Fatalf("SessionWorkspaces(vanished) = %v, want nil", got)
	}
	// A nil hub (no Agent Hub wired at all) must not panic and must also fail
	// closed.
	scope = newTaskScope(ws, nil)
	if got := scope.SessionWorkspaces("in-a"); got != nil {
		t.Fatalf("SessionWorkspaces with nil hub = %v, want nil", got)
	}
}

func TestTaskScopeServicePathEmptyNameIsThePrimary(t *testing.T) {
	ws, a, _ := twoWorkspaces(t)
	scope := newTaskScope(ws, nil)

	path, ok := scope.ServicePath(a.ID, "")
	if !ok || path != a.Root {
		t.Fatalf("ServicePath(a, \"\") = %q, %v, want %q, true", path, ok, a.Root)
	}
	if _, ok := scope.ServicePath(a.ID, "no-such-service"); ok {
		t.Fatal("ServicePath resolved a service name that does not exist")
	}
}

func TestTaskScopeHasPathAndExists(t *testing.T) {
	ws, a, _ := twoWorkspaces(t)
	scope := newTaskScope(ws, nil)

	if !scope.HasPath(a.ID, a.Root) {
		t.Fatal("HasPath did not find the workspace's own primary")
	}
	if scope.HasPath(a.ID, "/not/a/real/path") {
		t.Fatal("HasPath matched a path the workspace does not have")
	}
	if !scope.Exists(a.ID) {
		t.Fatal("Exists(a) = false")
	}
	if scope.Exists("no-such-workspace") {
		t.Fatal("Exists reported an unregistered workspace as present")
	}
}

// migrationContainer wires just enough of a Container to exercise
// MigrateBoardBindings and the reconcile fallback: a real SQLite handle,
// Tasks scoped against its binding table, and a workspace registry backed by
// the SAME handle — task_board_bindings.workspace_id has a foreign key to
// workspaces(id), so an in-memory workspace registry would fail every bind
// with nothing to point at. Returns the database too, since a test that
// wants to rewire scope after adding workspaces (SessionWorkspaces needs
// their ids, which do not exist until AddWorkspace runs) needs the same
// handle and table back.
func migrationContainer(t *testing.T, hub agentSessions) (*Container, *sql.DB) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(t.TempDir() + "/devkit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	c := NewContainer()
	c.Items = vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	c.Tasks = tasks.NewService(c.Items)
	c.Workspace = workspace.NewService(workspace.NewSQLRepository(db))
	c.Tasks.SetScope(newTaskScope(c.Workspace, hub), db, storage.TableTaskBoardBindings)
	return c, db
}

func boardWithProjectPath(t *testing.T, c *Container, name, path string) tasks.Board {
	t.Helper()
	board, err := c.Tasks.CreateBoard(name, name, "blue", tasks.Workflow{})
	if err != nil {
		t.Fatal(err)
	}
	board, err = c.Tasks.UpdateBoard(board.ID, func(b *tasks.Board) { b.ProjectPath = path })
	if err != nil {
		t.Fatal(err)
	}
	return board
}

// The migration's whole point: a board written before workspaces existed
// keeps being visible, bound to whichever workspace has a service at its old
// project_path, and a board matching nothing is simply left in the unbound
// group rather than erroring the whole pass out.
func TestMigrateBoardBindingsBindsByProjectPathAndSkipsUnmatched(t *testing.T) {
	c, _ := migrationContainer(t, nil)
	a, _, err := c.Workspace.AddWorkspace("A", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	matched := boardWithProjectPath(t, c, "Matched", a.Root)
	unmatched := boardWithProjectPath(t, c, "Unmatched", t.TempDir())

	MigrateBoardBindings(c)

	got, err := c.Tasks.Board(tasks.Caller{}, matched.ID)
	if err != nil || got.WorkspaceID != a.ID {
		t.Fatalf("matched board = %+v, %v, want workspace %s", got, err, a.ID)
	}
	still, err := c.Tasks.Board(tasks.Caller{}, unmatched.ID)
	if err != nil || still.WorkspaceID != "" {
		t.Fatalf("unmatched board = %+v, %v, want no binding", still, err)
	}
}

// Idempotent: a board the first run already bound is untouched by a second
// — no error, no re-bind, no second row racing the first (Bind's own
// ON CONFLICT would make a re-bind harmless anyway, but the migration should
// not even attempt one once board.WorkspaceID reads back non-empty).
func TestMigrateBoardBindingsSecondRunChangesNothing(t *testing.T) {
	c, _ := migrationContainer(t, nil)
	a, _, err := c.Workspace.AddWorkspace("A", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	board := boardWithProjectPath(t, c, "Matched", a.Root)

	MigrateBoardBindings(c)
	first, err := c.Tasks.Board(tasks.Caller{}, board.ID)
	if err != nil || first.WorkspaceID != a.ID {
		t.Fatalf("first migration = %+v, %v, want workspace %s", first, err, a.ID)
	}

	MigrateBoardBindings(c)

	second, err := c.Tasks.Board(tasks.Caller{}, board.ID)
	if err != nil || second.WorkspaceID != a.ID {
		t.Fatalf("second migration changed the binding: %+v, %v", second, err)
	}
}

// The gap the whole change closes: a person unbinding a legacy board must
// not have it drift back bound on the next startup just because its stale
// project_path still matches a workspace.
func TestMigrateBoardBindingsDoesNotRebindADeliberateUnbind(t *testing.T) {
	c, _ := migrationContainer(t, nil)
	a, _, err := c.Workspace.AddWorkspace("A", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	board := boardWithProjectPath(t, c, "Matched", a.Root)

	MigrateBoardBindings(c)
	bound, err := c.Tasks.Board(tasks.Caller{}, board.ID)
	if err != nil || bound.WorkspaceID != a.ID {
		t.Fatalf("first migration = %+v, %v, want workspace %s", bound, err, a.ID)
	}

	if _, err := c.Tasks.UnbindBoard(board.ID); err != nil {
		t.Fatal(err)
	}

	MigrateBoardBindings(c)

	still, err := c.Tasks.Board(tasks.Caller{}, board.ID)
	if err != nil || still.WorkspaceID != "" {
		t.Fatalf("migration re-bound a deliberately unbound board: %+v, %v", still, err)
	}
	if !c.Tasks.Considered(board.ID) {
		t.Fatal("an unbound board must still be Considered")
	}
}

// A board bound to a workspace that is later deleted must not be picked up
// by a second migration and bound to some other workspace that happens to
// have a service at the same path — the deleted workspace's binding row is
// still Considered, not a blank slate.
func TestMigrateBoardBindingsDoesNotRebindAfterWorkspaceDeleted(t *testing.T) {
	c, db := migrationContainer(t, nil)
	root := t.TempDir()
	a, _, err := c.Workspace.AddWorkspace("A", root)
	if err != nil {
		t.Fatal(err)
	}
	board := boardWithProjectPath(t, c, "Board", root)
	if _, err := c.Tasks.BindBoard(board.ID, a.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`DELETE FROM `+storage.TableWorkspaces+` WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	// A second workspace registers at the exact same path...
	if _, _, err := c.Workspace.AddWorkspace("B", root); err != nil {
		t.Fatal(err)
	}

	MigrateBoardBindings(c)

	got, err := c.Tasks.Board(tasks.Caller{}, board.ID)
	if err != nil || got.WorkspaceID != "" {
		t.Fatalf("migration rebound a board whose workspace was deleted: %+v, %v, want unbound", got, err)
	}
}

// A board bound to a different workspace must stay out of the reconcile pass
// even when its stale, deprecated project_path happens to equal root — the
// binding is now the source of truth, not the field migration reads once.
func TestBoardMatchesProjectSkipsABoardBoundToAnotherWorkspace(t *testing.T) {
	c, _ := migrationContainer(t, nil)
	a, _, err := c.Workspace.AddWorkspace("A", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	board := boardWithProjectPath(t, c, "Board", root)
	if _, err := c.Tasks.BindBoard(board.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	bound, err := c.Tasks.Board(tasks.Caller{}, board.ID)
	if err != nil {
		t.Fatal(err)
	}

	if boardMatchesProject(c, bound, root) {
		t.Fatal("a board bound to a workspace with no service at root matched root anyway")
	}

	// The workspace it IS bound to still matches by its own service path.
	if !boardMatchesProject(c, bound, a.Root) {
		t.Fatal("a board did not match the workspace it is actually bound to")
	}
}

// An unbound board, or a container with no workspace registry, falls back to
// the legacy comparison — the only one available before bindings existed.
func TestBoardMatchesProjectFallsBackWithNoBinding(t *testing.T) {
	c, _ := migrationContainer(t, nil)
	root := t.TempDir()
	board := boardWithProjectPath(t, c, "Board", root)
	unbound, err := c.Tasks.Board(tasks.Caller{}, board.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !boardMatchesProject(c, unbound, root) {
		t.Fatal("an unbound board did not fall back to project_path")
	}

	c.Workspace = nil
	if !boardMatchesProject(c, unbound, root) {
		t.Fatal("a container with no workspace registry did not fall back to project_path")
	}
}

// The end-to-end shape the whole task is for: once WireAgentLiveness has
// wired scope, an agent session that owns WS-A's folder cannot reach a task
// that lives on a board bound to WS-B — not even by id.
func TestWiredScopeHidesAnotherWorkspacesTaskFromAnAgent(t *testing.T) {
	// Built with no hub first: SessionWorkspaces needs the workspaces' ids,
	// which do not exist until AddWorkspace runs, so the real hub is wired in
	// afterwards with the same db and table migrationContainer already set up
	// — exactly what WireAgentLiveness does once Agent Hub exists.
	c, db := migrationContainer(t, nil)
	wsA, _, err := c.Workspace.AddWorkspace("A", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wsB, _, err := c.Workspace.AddWorkspace("B", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// sessionHub stands in for Agent Hub here — the same shape
	// WireAgentLiveness wires *agenthub.Service through, exercised via the
	// narrow agentSessions interface rather than a real hub.
	hub := sessionHub{"agent-a": {ID: "agent-a", ProjectPath: wsA.Root}}
	c.Tasks.SetScope(newTaskScope(c.Workspace, hub), db, storage.TableTaskBoardBindings)

	boardA, err := c.Tasks.CreateBoard("Board A", "BA", "blue", tasks.Workflow{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tasks.BindBoard(boardA.ID, wsA.ID); err != nil {
		t.Fatal(err)
	}
	boardB, err := c.Tasks.CreateBoard("Board B", "BB", "blue", tasks.Workflow{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Tasks.BindBoard(boardB.ID, wsB.ID); err != nil {
		t.Fatal(err)
	}
	taskB, err := c.Tasks.Create(tasks.Caller{}, tasks.CreateInput{BoardID: boardB.ID, Title: "only on B"})
	if err != nil {
		t.Fatal(err)
	}

	agent := tasks.Caller{SessionID: "agent-a"}
	if _, err := c.Tasks.Get(agent, taskB.ID); !errors.Is(err, tasks.ErrNotFound) {
		t.Fatalf("Get(WS-B task) by a WS-A agent = %v, want ErrNotFound", err)
	}
}
