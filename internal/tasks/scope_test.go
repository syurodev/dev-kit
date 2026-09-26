package tasks

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"devkit/internal/storage"
)

// fakeScope is the workspace registry and Agent Hub as tasks sees them. A
// workspace exists when its row does, unless the test says it has vanished
// behind the table's back.
type fakeScope struct {
	db       *sql.DB
	sessions map[string][]string
	gone     map[string]bool
}

func (f *fakeScope) ServicePath(string, string) (string, bool) { return "", false }
func (f *fakeScope) SessionWorkspaces(sessionID string) []string {
	return f.sessions[sessionID]
}
func (f *fakeScope) Exists(workspaceID string) bool {
	if f.gone[workspaceID] {
		return false
	}
	var n int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM `+storage.TableWorkspaces+` WHERE id = ?`, workspaceID).Scan(&n)
	return n == 1
}

// scopedService is a service with bindings wired against the real migrated
// schema, so the foreign key to workspaces is the one production has.
func scopedService(t *testing.T) (*Service, *sql.DB, *fakeScope) {
	t.Helper()
	svc, db := newService(t)
	scope := &fakeScope{db: db, sessions: map[string][]string{}, gone: map[string]bool{}}
	svc.SetScope(scope, db, storage.TableTaskBoardBindings)
	return svc, db, scope
}

func addWorkspace(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO `+storage.TableWorkspaces+` (id, name, root) VALUES (?, ?, ?)`,
		id, id, "/"+id); err != nil {
		t.Fatal(err)
	}
}

func TestBindingFillsWorkspaceIDOnReadAndNeverStoresIt(t *testing.T) {
	svc, db, _ := scopedService(t)
	addWorkspace(t, db, "ws-a")
	board := newBoard(t, svc)

	bound, err := svc.BindBoard(board.ID, "ws-a")
	if err != nil {
		t.Fatal(err)
	}
	if bound.WorkspaceID != "ws-a" {
		t.Fatalf("BindBoard returned workspace %q", bound.WorkspaceID)
	}
	read, err := svc.Board(board.ID)
	if err != nil || read.WorkspaceID != "ws-a" {
		t.Fatalf("Board = %+v, %v", read, err)
	}
	all, err := svc.Boards()
	if err != nil || len(all) != 1 || all[0].WorkspaceID != "ws-a" {
		t.Fatalf("Boards = %+v, %v", all, err)
	}

	// An edit through UpdateBoard rewrites the metadata; the binding must not
	// ride along into it, because the metadata syncs and the id would dangle.
	if _, err := svc.UpdateBoard(board.ID, func(b *Board) { b.Color = "red" }); err != nil {
		t.Fatal(err)
	}
	record, err := svc.items.Get(board.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(record.Metadata), "ws-a") || strings.Contains(string(record.Metadata), "workspace") {
		t.Fatalf("the binding leaked into synced metadata: %s", record.Metadata)
	}
}

func TestBindRefusesAMissingBoardOrWorkspace(t *testing.T) {
	svc, db, _ := scopedService(t)
	addWorkspace(t, db, "ws-a")
	board := newBoard(t, svc)

	if _, err := svc.BindBoard("no-such-board", "ws-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("binding a missing board = %v", err)
	}
	if _, err := svc.BindBoard(board.ID, "ws-gone"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("binding to a missing workspace = %v", err)
	}
	if _, err := svc.BindBoard(board.ID, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("binding to no workspace = %v", err)
	}
}

func TestRebindMovesAndUnbindReleases(t *testing.T) {
	svc, db, _ := scopedService(t)
	addWorkspace(t, db, "ws-a")
	addWorkspace(t, db, "ws-b")
	board := newBoard(t, svc)

	if _, err := svc.BindBoard(board.ID, "ws-a"); err != nil {
		t.Fatal(err)
	}
	// One workspace per board per machine: binding again moves it.
	if _, err := svc.BindBoard(board.ID, "ws-b"); err != nil {
		t.Fatal(err)
	}
	if read, _ := svc.Board(board.ID); read.WorkspaceID != "ws-b" {
		t.Fatalf("rebind left workspace %q", read.WorkspaceID)
	}
	unbound, err := svc.UnbindBoard(board.ID)
	if err != nil || unbound.WorkspaceID != "" {
		t.Fatalf("UnbindBoard = %+v, %v", unbound, err)
	}
	if read, _ := svc.Board(board.ID); read.WorkspaceID != "" {
		t.Fatalf("unbound board still reads workspace %q", read.WorkspaceID)
	}
	// Unbinding twice is not an error.
	if _, err := svc.UnbindBoard(board.ID); err != nil {
		t.Fatal(err)
	}
}

// Deleting a workspace leaves its boards unbound, never gone: the tasks are
// the project's record, and the workspace was only this machine's view of it.
func TestDeletingTheWorkspaceUnbindsTheBoard(t *testing.T) {
	svc, db, _ := scopedService(t)
	addWorkspace(t, db, "ws-a")
	board := newBoard(t, svc)
	if _, err := svc.BindBoard(board.ID, "ws-a"); err != nil {
		t.Fatal(err)
	}
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`DELETE FROM `+storage.TableWorkspaces+` WHERE id = ?`, "ws-a"); err != nil {
		t.Fatal(err)
	}
	read, err := svc.Board(board.ID)
	if err != nil || read.WorkspaceID != "" {
		t.Fatalf("board after workspace delete = %+v, %v", read, err)
	}
	if _, err := svc.get(task.ID); err != nil {
		t.Fatalf("the task went with the workspace: %v", err)
	}
}

// The foreign key normally removes the binding; Exists is the net for a
// database without foreign keys switched on.
func TestABindingToAVanishedWorkspaceReadsAsUnbound(t *testing.T) {
	svc, db, scope := scopedService(t)
	addWorkspace(t, db, "ws-a")
	board := newBoard(t, svc)
	if _, err := svc.BindBoard(board.ID, "ws-a"); err != nil {
		t.Fatal(err)
	}
	scope.gone["ws-a"] = true
	if read, _ := svc.Board(board.ID); read.WorkspaceID != "" {
		t.Fatalf("Board = %q", read.WorkspaceID)
	}
	all, _ := svc.Boards()
	if len(all) != 1 || all[0].WorkspaceID != "" {
		t.Fatalf("Boards = %+v", all)
	}
}

func TestBoardForFollowsTheSessionsWorkspaces(t *testing.T) {
	svc, db, scope := scopedService(t)
	addWorkspace(t, db, "ws-a")
	addWorkspace(t, db, "ws-b")
	scope.sessions["sess-1"] = []string{"ws-a"}

	inA := newBoard(t, svc)
	inB := newBoard(t, svc)
	unbound := newBoard(t, svc)
	if _, err := svc.BindBoard(inA.ID, "ws-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BindBoard(inB.ID, "ws-b"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		caller  Caller
		boardID string
		want    error
	}{
		{"agent, own workspace", agent, inA.ID, nil},
		{"agent, other workspace", agent, inB.ID, ErrNotFound},
		{"agent, unbound board", agent, unbound.ID, ErrNotFound},
		{"agent, missing board", agent, "no-such-board", ErrNotFound},
		{"agent in no workspace", Caller{SessionID: "sess-lost"}, inA.ID, ErrNotFound},
		{"user, other workspace", user, inB.ID, nil},
		{"user, unbound board", user, unbound.ID, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.boardFor(tc.caller, tc.boardID)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("boardFor = %v, want %v", err, tc.want)
			}
		})
	}
}

// Without a scope nothing is narrowed: that is the behaviour before bindings,
// and the headless wiring's.
func TestNilScopeSeesEveryBoard(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	if _, err := svc.boardFor(agent, board.ID); err != nil {
		t.Fatalf("an unscoped service hid a board: %v", err)
	}
	if _, err := svc.BindBoard(board.ID, "ws-a"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("binding with no table = %v", err)
	}
}
