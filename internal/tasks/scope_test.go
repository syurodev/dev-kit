package tasks

import (
	"context"
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
	// services is workspace id to service name ("" is the primary) to path,
	// the fixture's stand-in for workspace.Service.ListServices.
	services map[string]map[string]string
}

func (f *fakeScope) ServicePath(workspaceID, service string) (string, bool) {
	paths, ok := f.services[workspaceID]
	if !ok {
		return "", false
	}
	path, ok := paths[service]
	return path, ok
}
func (f *fakeScope) HasPath(workspaceID, path string) bool {
	for _, p := range f.services[workspaceID] {
		if p == path {
			return true
		}
	}
	return false
}
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
	scope := &fakeScope{db: db, sessions: map[string][]string{}, gone: map[string]bool{}, services: map[string]map[string]string{}}
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
	read, err := svc.Board(user, board.ID)
	if err != nil || read.WorkspaceID != "ws-a" {
		t.Fatalf("Board = %+v, %v", read, err)
	}
	all, err := svc.Boards(user)
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
	if read, _ := svc.Board(user, board.ID); read.WorkspaceID != "ws-b" {
		t.Fatalf("rebind left workspace %q", read.WorkspaceID)
	}
	unbound, err := svc.UnbindBoard(board.ID)
	if err != nil || unbound.WorkspaceID != "" {
		t.Fatalf("UnbindBoard = %+v, %v", unbound, err)
	}
	if read, _ := svc.Board(user, board.ID); read.WorkspaceID != "" {
		t.Fatalf("unbound board still reads workspace %q", read.WorkspaceID)
	}
	// Unbinding twice is not an error.
	if _, err := svc.UnbindBoard(board.ID); err != nil {
		t.Fatal(err)
	}
}

// Unbinding a board leaves it unbound but Considered: the row records that
// this machine has already decided about it, so a migration must not treat
// it as a legacy board it has never seen.
func TestUnbindBoardIsUnboundButConsidered(t *testing.T) {
	svc, db, _ := scopedService(t)
	addWorkspace(t, db, "ws-a")
	board := newBoard(t, svc)

	if svc.Considered(board.ID) {
		t.Fatal("a board never bound is already Considered")
	}
	if _, err := svc.BindBoard(board.ID, "ws-a"); err != nil {
		t.Fatal(err)
	}
	if !svc.Considered(board.ID) {
		t.Fatal("a bound board is not Considered")
	}

	if _, err := svc.UnbindBoard(board.ID); err != nil {
		t.Fatal(err)
	}
	if read, _ := svc.Board(user, board.ID); read.WorkspaceID != "" {
		t.Fatalf("unbound board still reads workspace %q", read.WorkspaceID)
	}
	if !svc.Considered(board.ID) {
		t.Fatal("an unbound board must still be Considered")
	}
}

// Deleting a bound board's workspace leaves it unbound, and it must still
// read as Considered — the row survives the delete with a NULL workspace —
// so a migration does not bind it to some other workspace behind the user's
// back.
func TestConsideredSurvivesTheWorkspaceBeingDeleted(t *testing.T) {
	svc, db, _ := scopedService(t)
	addWorkspace(t, db, "ws-a")
	board := newBoard(t, svc)
	if _, err := svc.BindBoard(board.ID, "ws-a"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`DELETE FROM `+storage.TableWorkspaces+` WHERE id = ?`, "ws-a"); err != nil {
		t.Fatal(err)
	}

	if read, _ := svc.Board(user, board.ID); read.WorkspaceID != "" {
		t.Fatalf("board after workspace delete = %q, want unbound", read.WorkspaceID)
	}
	if !svc.Considered(board.ID) {
		t.Fatal("a board whose workspace was deleted must stay Considered")
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
	read, err := svc.Board(user, board.ID)
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
	if read, _ := svc.Board(user, board.ID); read.WorkspaceID != "" {
		t.Fatalf("Board = %q", read.WorkspaceID)
	}
	all, _ := svc.Boards(user)
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

// scopeFixture is one agent in ws-a, and a board in each of three places: its
// own workspace, another one, and none.
type scopeFixture struct {
	svc                  *Service
	scope                *fakeScope
	home, other, unbound Board
	homeTask, otherTask  Task
	unboundTask          Task
}

func newScopeFixture(t *testing.T) scopeFixture {
	t.Helper()
	svc, db, scope := scopedService(t)
	addWorkspace(t, db, "ws-a")
	addWorkspace(t, db, "ws-b")
	scope.sessions[agent.SessionID] = []string{"ws-a"}

	f := scopeFixture{svc: svc, scope: scope}
	for _, b := range []struct {
		board     *Board
		task      *Task
		workspace string
	}{
		{&f.home, &f.homeTask, "ws-a"},
		{&f.other, &f.otherTask, "ws-b"},
		{&f.unbound, &f.unboundTask, ""},
	} {
		*b.board = newBoard(t, svc)
		if b.workspace != "" {
			if _, err := svc.BindBoard(b.board.ID, b.workspace); err != nil {
				t.Fatal(err)
			}
		}
		task, err := svc.Create(user, CreateInput{BoardID: b.board.ID, Title: "work", Description: "secret"})
		if err != nil {
			t.Fatal(err)
		}
		*b.task = task
	}
	return f
}

// scopedCall is one public method, pointed at a target task. home is a board
// the caller can reach, for the calls that combine the two.
type scopedCall struct {
	name string
	call func(s *Service, c Caller, target Task, home Board) error
}

// everyScopedMethod is every public path that reads or writes a task or a
// board on the caller's behalf. Delete is last because it destroys the target.
func everyScopedMethod() []scopedCall {
	ctx := context.Background()
	return []scopedCall{
		{"Board", func(s *Service, c Caller, t Task, _ Board) error { _, err := s.Board(c, t.BoardID); return err }},
		{"List", func(s *Service, c Caller, t Task, _ Board) error {
			_, err := s.List(c, ListInput{BoardID: t.BoardID})
			return err
		}},
		{"Get", func(s *Service, c Caller, t Task, _ Board) error { _, err := s.Get(c, t.ID); return err }},
		{"Update", func(s *Service, c Caller, t Task, _ Board) error {
			_, err := s.Update(c, t.ID, "renamed", "rewritten")
			return err
		}},
		{"SetFields", func(s *Service, c Caller, t Task, _ Board) error {
			high := "high"
			_, err := s.SetFields(c, t.ID, Fields{Priority: &high})
			return err
		}},
		{"Archive", func(s *Service, c Caller, t Task, _ Board) error { _, err := s.Archive(c, t.ID); return err }},
		{"Unarchive", func(s *Service, c Caller, t Task, _ Board) error { _, err := s.Unarchive(c, t.ID); return err }},
		{"Claim", func(s *Service, c Caller, t Task, _ Board) error { _, err := s.Claim(c, t.ID); return err }},
		{"Assignable", func(s *Service, c Caller, t Task, _ Board) error {
			_, err := s.Assignable(c, t.ID)
			return err
		}},
		{"Assign", func(s *Service, c Caller, t Task, _ Board) error {
			_, err := s.Assign(c, t.ID, "assigned-child")
			return err
		}},
		{"Release", func(s *Service, c Caller, t Task, _ Board) error { _, err := s.Release(c, t.ID); return err }},
		{"Transition", func(s *Service, c Caller, t Task, _ Board) error {
			_, err := s.Transition(ctx, c, TransitionInput{TaskID: t.ID, To: "review"})
			return err
		}},
		{"AddComment", func(s *Service, c Caller, t Task, _ Board) error {
			_, err := s.AddComment(c, t.ID, "hello")
			return err
		}},
		{"Comments", func(s *Service, c Caller, t Task, _ Board) error { _, err := s.Comments(c, t.ID); return err }},
		{"Progress", func(s *Service, c Caller, t Task, _ Board) error { _, err := s.Progress(c, t.ID); return err }},
		{"Search", func(s *Service, c Caller, t Task, _ Board) error {
			_, err := s.Search(c, t.BoardID, "work")
			return err
		}},
		{"LinkPlanNode", func(s *Service, c Caller, t Task, _ Board) error {
			_, err := s.LinkPlanNode(c, t.ID, "n1")
			return err
		}},
		{"ByPlanNode", func(s *Service, c Caller, t Task, _ Board) error { _, err := s.ByPlanNode(c, t.BoardID); return err }},
		{"CreateFromPlan", func(s *Service, c Caller, t Task, _ Board) error {
			_, err := s.CreateFromPlan(c, t.BoardID, []PlanSeed{{PlanNodeID: "n2", Label: "Step"}})
			return err
		}},
		{"Create", func(s *Service, c Caller, t Task, _ Board) error {
			_, err := s.Create(c, CreateInput{BoardID: t.BoardID, Title: "more"})
			return err
		}},
		// A subtask on a board the caller can reach, under a parent it may
		// not: the parent is how it would learn the target exists.
		{"Create subtask", func(s *Service, c Caller, t Task, home Board) error {
			_, err := s.Create(c, CreateInput{BoardID: home.ID, Title: "child", ParentID: t.ID})
			return err
		}},
		{"Delete", func(s *Service, c Caller, t Task, _ Board) error { return s.Delete(ctx, c, t.ID) }},
	}
}

func TestAnAgentWorksOnItsOwnWorkspacesBoard(t *testing.T) {
	f := newScopeFixture(t)
	for _, method := range everyScopedMethod() {
		if err := method.call(f.svc, agent, f.homeTask, f.home); err != nil {
			t.Errorf("%s on its own workspace's task = %v", method.name, err)
		}
	}
}

// Every path refuses in the same words, and the words are "not found": which
// boards another workspace holds is what the scope keeps from the agent, and
// "permission denied" would give it away.
func TestAnAgentCannotReachAnotherWorkspaceOrAnUnboundBoard(t *testing.T) {
	for _, target := range []string{"other workspace", "unbound"} {
		t.Run(target, func(t *testing.T) {
			f := newScopeFixture(t)
			task := f.otherTask
			if target == "unbound" {
				task = f.unboundTask
			}
			for _, method := range everyScopedMethod() {
				if err := method.call(f.svc, agent, task, f.home); !errors.Is(err, ErrNotFound) {
					t.Errorf("%s = %v, want ErrNotFound", method.name, err)
				}
			}
			// And nothing was written on the way to the refusal.
			after, err := f.svc.Get(user, task.ID)
			if err != nil {
				t.Fatalf("the refused calls destroyed the task: %v", err)
			}
			if after.Title != "work" || after.Description != "secret" || after.Status != "todo" ||
				after.ArchivedAt != "" || after.SessionID != "" || after.PlanNodeID != "" || after.Priority != "" {
				t.Fatalf("a refused call still wrote: %+v", after)
			}
			if comments, _ := f.svc.Comments(user, task.ID); len(comments) != 0 {
				t.Fatalf("a refused comment landed: %+v", comments)
			}
			if listed, _ := f.svc.List(user, ListInput{BoardID: task.BoardID}); len(listed) != 1 {
				t.Fatalf("a refused create landed: %+v", listed)
			}
		})
	}
}

func TestBoardsShowsAnAgentOnlyItsWorkspace(t *testing.T) {
	f := newScopeFixture(t)
	mine, err := f.svc.Boards(agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 1 || mine[0].ID != f.home.ID || mine[0].WorkspaceID != "ws-a" {
		t.Fatalf("agent Boards = %+v", mine)
	}
	everything, err := f.svc.Boards(user)
	if err != nil {
		t.Fatal(err)
	}
	if len(everything) != 3 {
		t.Fatalf("user Boards = %+v", everything)
	}
}

// The user — and a terminal CLI on the shared socket, which carries no
// session — sees every board, bound or not.
func TestTheUserReachesEveryBoard(t *testing.T) {
	for _, target := range []string{"home", "other workspace", "unbound"} {
		t.Run(target, func(t *testing.T) {
			f := newScopeFixture(t)
			task := map[string]Task{"home": f.homeTask, "other workspace": f.otherTask, "unbound": f.unboundTask}[target]
			for _, method := range everyScopedMethod() {
				if method.name == "Claim" || method.name == "Assign" || method.name == "Assignable" {
					continue // a claim is an agent's act; the user is refused everywhere
				}
				if err := method.call(f.svc, user, task, f.home); err != nil {
					t.Errorf("%s as the user = %v", method.name, err)
				}
			}
		})
	}
}

// Checking only the board a task leaves would let an agent carry work out of
// its own reach and into another workspace's board.
func TestSetFieldsRefusesMovingATaskOutOfScope(t *testing.T) {
	f := newScopeFixture(t)
	for _, destination := range []Board{f.other, f.unbound} {
		id := destination.ID
		if _, err := f.svc.SetFields(agent, f.homeTask.ID, Fields{BoardID: &id}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("moving to %s = %v, want ErrNotFound", id, err)
		}
	}
	after, _ := f.svc.Get(user, f.homeTask.ID)
	if after.BoardID != f.home.ID {
		t.Fatalf("the task moved to %s", after.BoardID)
	}
}

// ErrClaimed would confirm the task exists; out of scope comes first.
func TestAClaimOutOfScopeIsNotFoundEvenWhenHeld(t *testing.T) {
	f := newScopeFixture(t)
	f.scope.sessions["sess-2"] = []string{"ws-b"}
	f.svc.SetLiveness(liveness{"sess-2": true})
	if _, err := f.svc.Claim(Caller{SessionID: "sess-2"}, f.otherTask.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Claim(agent, f.otherTask.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("claiming a held task out of scope = %v", err)
	}
}

// ---- verify resolves through the binding, not project_path -----------

// verifyBoard is a board bound to ws-a with a verify command, ready for
// closeThroughGate to run.
func verifyBoard(t *testing.T, svc *Service, apply func(*Verify, *Board)) Board {
	t.Helper()
	board := newBoard(t, svc)
	if _, err := svc.BindBoard(board.ID, "ws-a"); err != nil {
		t.Fatal(err)
	}
	verify := &Verify{Command: "true", TimeoutSec: 30}
	updated, err := svc.UpdateBoard(board.ID, func(b *Board) {
		b.Verify = verify
		apply(verify, b)
	})
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

// runProjectPath reads back the project_path a Transition's run landed with.
func runProjectPath(t *testing.T, db *sql.DB, runID string) string {
	t.Helper()
	var path string
	if err := db.QueryRow(`SELECT project_path FROM `+storage.TableTaskVerifyRuns+` WHERE id = ?`, runID).Scan(&path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestVerifyRunsInTheNamedServicesPath(t *testing.T) {
	svc, db, scope := scopedService(t)
	addWorkspace(t, db, "ws-a")
	scope.sessions[agent.SessionID] = []string{"ws-a"}
	apiDir := t.TempDir()
	scope.services["ws-a"] = map[string]string{"": t.TempDir(), "api": apiDir}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, nil, nil))
	// Transition only enqueues; join the worker before TempDir/SQLite cleanup.
	defer waitForTerminalGateDrain(t, svc.Gate())

	board := verifyBoard(t, svc, func(v *Verify, b *Board) { v.Service = "api" })
	task, err := svc.Create(agent, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if got := runProjectPath(t, db, result.RunID); got != apiDir {
		t.Fatalf("project_path = %q, want the api service's path %q", got, apiDir)
	}
}

func TestVerifyWithNoServiceNamedUsesThePrimary(t *testing.T) {
	svc, db, scope := scopedService(t)
	addWorkspace(t, db, "ws-a")
	scope.sessions[agent.SessionID] = []string{"ws-a"}
	primary := t.TempDir()
	scope.services["ws-a"] = map[string]string{"": primary}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, nil, nil))
	// Transition only enqueues; join the worker before TempDir/SQLite cleanup.
	defer waitForTerminalGateDrain(t, svc.Gate())

	board := verifyBoard(t, svc, func(v *Verify, b *Board) {})
	task, err := svc.Create(agent, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if got := runProjectPath(t, db, result.RunID); got != primary {
		t.Fatalf("project_path = %q, want the primary %q", got, primary)
	}
}

// A board written before verify.service existed still has its legacy
// project_path; as long as that path still names one of the bound
// workspace's services, it keeps working with no write of its own.
func TestVerifyLegacyProjectPathKeepsWorkingWhenItMatchesAService(t *testing.T) {
	svc, db, scope := scopedService(t)
	addWorkspace(t, db, "ws-a")
	scope.sessions[agent.SessionID] = []string{"ws-a"}
	legacy := t.TempDir()
	scope.services["ws-a"] = map[string]string{"": t.TempDir(), "api": legacy}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, nil, nil))
	// Transition only enqueues; join the worker before TempDir/SQLite cleanup.
	defer waitForTerminalGateDrain(t, svc.Gate())

	board := verifyBoard(t, svc, func(v *Verify, b *Board) { b.ProjectPath = legacy })
	task, err := svc.Create(agent, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if got := runProjectPath(t, db, result.RunID); got != legacy {
		t.Fatalf("project_path = %q, want the legacy path %q", got, legacy)
	}
}

// A board bound to a workspace that names a verify.service which does not
// resolve here refuses outright rather than falling back to asking a
// person — and nothing is written to the queue on the way to the refusal.
func TestCloseRefusesRatherThanEnqueueWhenTheServiceDoesNotResolve(t *testing.T) {
	svc, db, scope := scopedService(t)
	addWorkspace(t, db, "ws-a")
	scope.sessions[agent.SessionID] = []string{"ws-a"}
	scope.services["ws-a"] = map[string]string{"": t.TempDir()} // no "api" service
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, nil, nil))

	board := verifyBoard(t, svc, func(v *Verify, b *Board) { v.Service = "api" })
	task, err := svc.Create(agent, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"}); !errors.Is(err, ErrBoardUnbound) {
		t.Fatalf("transition = %v, want ErrBoardUnbound", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + storage.TableTaskVerifyRuns).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("a refused close still enqueued %d rows", count)
	}
	after, _ := svc.Get(user, task.ID)
	if after.Status == "done" {
		t.Fatal("a refused close still moved the task")
	}
}

// An unbound board is invisible to an agent through boardFor, so this checks
// the resolver directly rather than through Transition (which an agent could
// never even reach for a board outside its scope).
func TestVerifyDirRefusesAnUnboundBoard(t *testing.T) {
	svc, _, _ := scopedService(t)
	board := newBoard(t, svc)
	updated, err := svc.UpdateBoard(board.ID, func(b *Board) {
		b.Verify = &Verify{Command: "true", TimeoutSec: 30}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.verifyDir(updated); !errors.Is(err, ErrBoardUnbound) {
		t.Fatalf("verifyDir on an unbound board = %v, want ErrBoardUnbound", err)
	}
}
