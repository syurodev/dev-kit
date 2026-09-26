package tasks

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"devkit/internal/storage"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

func newService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	db, err := storage.Open(t.TempDir() + "/devkit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return NewService(vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)), db
}

func newBoard(t *testing.T, s *Service) Board {
	t.Helper()
	board, err := s.CreateBoard("DevKit", "DEV", "blue", Workflow{})
	if err != nil {
		t.Fatal(err)
	}
	return board
}

var agent = Caller{SessionID: "sess-1"}
var user = Caller{}

func TestCreateDerivesAKeyFromTheRecordID(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "Add refunds"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(task.Key, "DEV-") {
		t.Fatalf("key = %q", task.Key)
	}
	if !strings.HasPrefix(task.ID, strings.TrimPrefix(task.Key, "DEV-")) {
		t.Fatalf("key %q is not derived from id %q", task.Key, task.ID)
	}
	if task.Status != "todo" {
		t.Fatalf("status = %q, want the first open status", task.Status)
	}
}

// Every rule reads category, so renaming a column cannot move a gate.
func TestRulesReadCategoryNotStatusName(t *testing.T) {
	svc, _ := newService(t)
	board, err := svc.CreateBoard("Custom", "CUS", "", Workflow{Statuses: []Status{
		{ID: "backlog", Name: "Backlog", Category: CategoryOpen},
		{ID: "doing", Name: "Đang làm", Category: CategoryActive},
		{ID: "shipped", Name: "Đã ship", Category: CategoryTerminal},
	}})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "backlog" {
		t.Fatalf("status = %q", task.Status)
	}
	// "shipped" is terminal although it is not called Done, so an agent
	// closing it must go through the gate — here, with no gate, be refused.
	svc.SetGate(nil)
	if _, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "shipped"}); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("closing a renamed terminal column = %v", err)
	}
}

func TestWorkflowMustBeRunnable(t *testing.T) {
	svc, _ := newService(t)
	// No terminal category: a board you cannot finish work on is not a board.
	_, err := svc.CreateBoard("Bad", "BAD", "", Workflow{Statuses: []Status{
		{ID: "a", Name: "A", Category: CategoryOpen},
		{ID: "b", Name: "B", Category: CategoryActive},
	}})
	if err == nil {
		t.Fatal("a workflow with no terminal status was accepted")
	}
	// Duplicate ids would make CategoryOf ambiguous.
	_, err = svc.CreateBoard("Bad", "BAD", "", Workflow{Statuses: []Status{
		{ID: "a", Name: "A", Category: CategoryOpen},
		{ID: "a", Name: "A again", Category: CategoryActive},
		{ID: "z", Name: "Z", Category: CategoryTerminal},
	}})
	if err == nil {
		t.Fatal("a workflow with duplicate status ids was accepted")
	}
}

func TestAgentMovesFreelyBelowTerminal(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	ctx := context.Background()
	for _, to := range []string{"in_progress", "review", "todo"} {
		result, err := svc.Transition(ctx, agent, TransitionInput{TaskID: task.ID, To: to})
		if err != nil {
			t.Fatalf("transition to %s: %v", to, err)
		}
		if result.Verifying {
			t.Fatalf("a non-terminal transition went through the gate")
		}
		if result.Task.Status != to {
			t.Fatalf("status = %q, want %q", result.Task.Status, to)
		}
	}
	after, _ := svc.Get(user, task.ID)
	if len(after.History) != 3 {
		t.Fatalf("history = %+v", after.History)
	}
	if after.History[0].By != "agent:sess-1" {
		t.Fatalf("history author = %q", after.History[0].By)
	}
}

func TestOptimisticTransitionRefusesRatherThanOverwrites(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	ctx := context.Background()
	if _, err := svc.Transition(ctx, agent, TransitionInput{TaskID: task.ID, To: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	// A second agent still thinks it is in todo.
	_, err := svc.Transition(ctx, Caller{SessionID: "sess-2"}, TransitionInput{
		TaskID: task.ID, To: "review", ExpectedStatus: "todo",
	})
	if !errors.Is(err, ErrStatusChanged) {
		t.Fatalf("stale transition = %v, want ErrStatusChanged", err)
	}
	after, _ := svc.Get(user, task.ID)
	if after.Status != "in_progress" {
		t.Fatalf("the losing transition overwrote: %q", after.Status)
	}
}

type liveness map[string]bool

func (l liveness) Alive(id string) bool { return l[id] }

func TestClaimRefusesALiveSessionAndTakesOverADeadOne(t *testing.T) {
	svc, _ := newService(t)
	svc.SetLiveness(liveness{"sess-1": true})
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})

	claimed, err := svc.Claim(agent, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.SessionID != "sess-1" || claimed.Status != "in_progress" {
		t.Fatalf("claim = %+v", claimed)
	}
	if _, err := svc.Claim(Caller{SessionID: "sess-2"}, task.ID); !errors.Is(err, ErrClaimed) {
		t.Fatal("a live claim was taken over")
	}
	// The holder dies; the claim stays on the task so it can reconnect, and
	// another session may now take it.
	svc.SetLiveness(liveness{})
	still, _ := svc.Get(user, task.ID)
	if still.SessionID != "sess-1" {
		t.Fatal("a dead session's claim was cleared automatically")
	}
	if _, err := svc.Claim(Caller{SessionID: "sess-2"}, task.ID); err != nil {
		t.Fatalf("a dead session's claim blocked a new one: %v", err)
	}
}

func TestClaimNeverTouchesTheWorkTree(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	claimed, err := svc.Claim(agent, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Nothing about a branch: the branch is written by the agent afterwards,
	// through SetFields, when it explicitly creates one.
	if claimed.Branch != "" {
		t.Fatalf("claim set a branch: %q", claimed.Branch)
	}
}

// A board with no verify command asks every single time.
func TestCloseWithoutVerifyAsksEveryTime(t *testing.T) {
	svc, db := newService(t)
	confirm := &stubConfirm{allow: true}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, confirm, nil))
	board := newBoard(t, svc)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
		if _, err := svc.Transition(ctx, agent, TransitionInput{TaskID: task.ID, To: "done"}); err != nil {
			t.Fatal(err)
		}
	}
	if confirm.asked != 2 {
		t.Fatalf("asked %d times for 2 closes; there is no allow-always here", confirm.asked)
	}
}

func TestCloseIsRefusedWhenDeniedAndWhenHeadless(t *testing.T) {
	svc, db := newService(t)
	denied := &stubConfirm{allow: false}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, denied, nil))
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	ctx := context.Background()
	if _, err := svc.Transition(ctx, agent, TransitionInput{TaskID: task.ID, To: "done"}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("denied close = %v", err)
	}
	after, _ := svc.Get(user, task.ID)
	if after.Status == "done" {
		t.Fatal("a denied close still moved the task")
	}

	// Headless: no confirmer at all.
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, nil, nil))
	if _, err := svc.Transition(ctx, agent, TransitionInput{TaskID: task.ID, To: "done"}); !errors.Is(err, ErrConfirmationRequired) {
		t.Fatalf("headless close = %v, want ErrConfirmationRequired", err)
	}
}

// The user closes a task from the UI without any gate: verify only stands in
// the MCP path.
func TestUserClosesWithoutTheGate(t *testing.T) {
	svc, db := newService(t)
	confirm := &stubConfirm{allow: false}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, confirm, nil))
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	result, err := svc.Transition(context.Background(), user, TransitionInput{TaskID: task.ID, To: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Task.Status != "done" {
		t.Fatalf("status = %q", result.Task.Status)
	}
	if confirm.asked != 0 {
		t.Fatal("the user was asked to approve their own click")
	}
}

type stubConfirm struct {
	allow bool
	asked int
}

func (s *stubConfirm) Confirm(context.Context, string, string) (bool, error) {
	s.asked++
	return s.allow, nil
}

type stubNotify struct {
	mu    chan struct{}
	calls []string
}

func (n *stubNotify) Notify(module, kind, id, title, _ string) {
	n.calls = append(n.calls, module+"/"+kind+"/"+id+": "+title)
	select {
	case n.mu <- struct{}{}:
	default:
	}
}

// The verify path, end to end: DevKit runs the command and the exit code
// decides. Nothing an agent says is consulted.
func TestVerifyPassCloses(t *testing.T) {
	svc, db := newService(t)
	notify := &stubNotify{mu: make(chan struct{}, 4)}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, nil, notify))
	board := newBoard(t, svc)
	project := t.TempDir()
	board, err := svc.UpdateBoard(board.ID, func(b *Board) {
		b.Verify = &Verify{Command: "true", TimeoutSec: 30}
		b.ProjectPath = project
	})
	if err != nil {
		t.Fatal(err)
	}
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})

	result, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Verifying || result.RunID == "" {
		t.Fatalf("a board with verify must queue a run: %+v", result)
	}
	// The task has not moved yet: showing done and retracting it would make
	// the board lie.
	if result.Task.Status != "todo" {
		t.Fatalf("task moved before the run landed: %q", result.Task.Status)
	}

	waitForNotification(t, notify)
	after, _ := svc.Get(user, task.ID)
	if after.Status != "done" {
		t.Fatalf("status after a passing verify = %q", after.Status)
	}
	runs, err := svc.Gate().Runs(task.ID, 5)
	if err != nil || len(runs) != 1 || runs[0].State != RunPassed {
		t.Fatalf("runs = %+v (%v)", runs, err)
	}
	assertSystemComment(t, svc, task.ID, "exited 0")
}

// A red suite refuses and does NOT fall back to asking a person: the answer
// is already there, and forwarding it makes the user read the output instead.
func TestVerifyFailureDoesNotFallBackToAsking(t *testing.T) {
	svc, db := newService(t)
	confirm := &stubConfirm{allow: true}
	notify := &stubNotify{mu: make(chan struct{}, 4)}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, confirm, notify))
	board := newBoard(t, svc)
	project := t.TempDir()
	board, _ = svc.UpdateBoard(board.ID, func(b *Board) {
		b.Verify = &Verify{Command: "echo failing-output; exit 3", TimeoutSec: 30}
		b.ProjectPath = project
	})
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})

	if _, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"}); err != nil {
		t.Fatal(err)
	}
	waitForNotification(t, notify)

	after, _ := svc.Get(user, task.ID)
	if after.Status == "done" {
		t.Fatal("a failing verify closed the task")
	}
	if confirm.asked != 0 {
		t.Fatal("a failing verify fell back to asking a person")
	}
	runs, _ := svc.Gate().Runs(task.ID, 5)
	if len(runs) != 1 || runs[0].State != RunFailed || runs[0].ExitCode != 3 {
		t.Fatalf("runs = %+v", runs)
	}
	assertSystemComment(t, svc, task.ID, "failing-output")
}

// The invariant a durable queue must not break: a run nobody saw finish comes
// back as queued, never as a result.
func TestRecoverRequeuesAnInterruptedRun(t *testing.T) {
	svc, db := newService(t)
	gate := NewGate(db, storage.TableTaskVerifyRuns, nil, nil)
	svc.SetGate(gate)
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})

	if _, err := db.Exec(`INSERT INTO `+storage.TableTaskVerifyRuns+
		` (id, task_id, board_id, project_path, target_status, command, timeout_sec, requested_by, requested_at, state)
		  VALUES ('run-1', ?, ?, '/tmp/p', 'done', 'true', 30, 'agent:x', ?, 'running')`,
		task.ID, board.ID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := gate.Recover(); err != nil {
		t.Fatal(err)
	}
	runs, _ := gate.Runs(task.ID, 5)
	if len(runs) != 1 || runs[0].State != RunQueued {
		t.Fatalf("after recovery = %+v; an unfinished run must never read as a result", runs)
	}
	after, _ := svc.Get(user, task.ID)
	if after.Status == "done" {
		t.Fatal("recovery closed a task from a run that never finished")
	}
}

// A request that waited too long grades code it was never made against.
func TestStaleRequestIsNotRun(t *testing.T) {
	svc, db := newService(t)
	gate := NewGate(db, storage.TableTaskVerifyRuns, nil, nil)
	svc.SetGate(gate)
	board := newBoard(t, svc)
	project := t.TempDir()
	board, _ = svc.UpdateBoard(board.ID, func(b *Board) {
		b.Verify = &Verify{Command: "true", TimeoutSec: 30}
		b.ProjectPath = project
	})
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})

	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO `+storage.TableTaskVerifyRuns+
		` (id, task_id, board_id, project_path, target_status, command, timeout_sec, requested_by, requested_at, state)
		  VALUES ('run-old', ?, ?, ?, 'done', 'true', 30, 'agent:x', ?, 'queued')`,
		task.ID, board.ID, project, old); err != nil {
		t.Fatal(err)
	}
	run, err := gate.next(project)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "" {
		t.Fatalf("a two-day-old request was run: %+v", run)
	}
	runs, _ := gate.Runs(task.ID, 5)
	if len(runs) != 1 || runs[0].State != RunStale {
		t.Fatalf("runs = %+v, want stale", runs)
	}
}

func TestOneVerifyPerTaskAtATime(t *testing.T) {
	svc, db := newService(t)
	gate := NewGate(db, storage.TableTaskVerifyRuns, nil, nil)
	svc.SetGate(gate)
	board := newBoard(t, svc)
	board, _ = svc.UpdateBoard(board.ID, func(b *Board) {
		// Never finishes on its own within the test; the point is the second
		// enqueue, not the run.
		b.Verify = &Verify{Command: "true", TimeoutSec: 30}
		b.ProjectPath = t.TempDir()
	})
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if _, err := gate.enqueue(board, task, "done", "agent:x"); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.enqueue(board, task, "done", "agent:x"); !errors.Is(err, ErrVerifying) {
		t.Fatalf("second enqueue = %v, want ErrVerifying", err)
	}
}

func TestSubtasksAreOneLevelAndCountedByCategory(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	parent, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "parent"})
	childA, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "a", ParentID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "b", ParentID: parent.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "deep", ParentID: childA.ID}); !errors.Is(err, ErrParentHasParent) {
		t.Fatalf("a subtask of a subtask was allowed: %v", err)
	}

	progress, err := svc.Progress(user, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Done != 0 || progress.Total != 2 {
		t.Fatalf("progress = %+v", progress)
	}
	if _, err := svc.Transition(context.Background(), user, TransitionInput{TaskID: childA.ID, To: "done"}); err != nil {
		t.Fatal(err)
	}
	progress, _ = svc.Progress(user, parent.ID)
	if progress.Done != 1 || progress.Total != 2 {
		t.Fatalf("progress = %+v", progress)
	}
	// Counting is not blocking: the parent closes with a child still open.
	if _, err := svc.Transition(context.Background(), user, TransitionInput{TaskID: parent.ID, To: "done"}); err != nil {
		t.Fatalf("closing a parent with open children was blocked: %v", err)
	}
}

// Authorship is stamped from the caller, so an agent cannot sign as a person.
func TestCommentAuthorshipComesFromTheCaller(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	if _, err := svc.AddComment(agent, task.ID, "from the agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddComment(user, task.ID, "from the user"); err != nil {
		t.Fatal(err)
	}
	comments, err := svc.Comments(user, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[CommentKind]string{}
	for _, comment := range comments {
		kinds[comment.Kind] = comment.Author
	}
	if kinds[CommentAgent] != "agent:sess-1" {
		t.Fatalf("agent comment author = %q", kinds[CommentAgent])
	}
	if kinds[CommentHuman] != "user" {
		t.Fatalf("human comment author = %q", kinds[CommentHuman])
	}
}

func TestListLeavesDescriptionsEncrypted(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	if _, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x", Description: "secret body"}); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(user, ListInput{BoardID: board.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Description != "" {
		t.Fatalf("List returned a description: %+v", list)
	}
	full, err := svc.Get(user, list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Description != "secret body" {
		t.Fatalf("Get description = %q", full.Description)
	}
}

func TestListRequiresABoard(t *testing.T) {
	svc, _ := newService(t)
	if _, err := svc.List(user, ListInput{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an unbounded listing was allowed: %v", err)
	}
}

func TestHistoryIsBounded(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	ctx := context.Background()
	for i := 0; i < HistoryLimit+10; i++ {
		to := "in_progress"
		if i%2 == 1 {
			to = "todo"
		}
		if _, err := svc.Transition(ctx, user, TransitionInput{TaskID: task.ID, To: to}); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := svc.Get(user, task.ID)
	if len(after.History) != HistoryLimit {
		t.Fatalf("history = %d entries, want %d", len(after.History), HistoryLimit)
	}
}

func waitForNotification(t *testing.T, n *stubNotify) {
	t.Helper()
	select {
	case <-n.mu:
	case <-time.After(10 * time.Second):
		t.Fatal("the verify run never landed")
	}
}

func assertSystemComment(t *testing.T, svc *Service, taskID, want string) {
	t.Helper()
	comments, err := svc.Comments(user, taskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, comment := range comments {
		if comment.Kind == CommentSystem && strings.Contains(comment.Body, want) {
			return
		}
	}
	t.Fatalf("no system comment containing %q in %+v", want, comments)
}
