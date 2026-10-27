package tasks

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

type denyTerminalPolicy struct{ calls int }

func (p *denyTerminalPolicy) CheckTerminal(_ context.Context, caller Caller, task Task, target string) error {
	p.calls++
	if task.ID == "" || target == "" {
		return ErrInvalid
	}
	if caller.IsAgent() {
		return ErrManagedTerminalNotReady
	}
	return nil
}

func TestTerminalPolicyRunsBeforeGateAndObservesHuman(t *testing.T) {
	svc, db := newService(t)
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "managed work"})
	if err != nil {
		t.Fatal(err)
	}
	policy := &denyTerminalPolicy{}
	svc.SetTerminalPolicy(policy)
	// The managed policy refusal wins before any existing verify/confirmation
	// path can accept the agent's own close request.
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, &stubConfirm{allow: true}, nil))
	if _, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"}); !errors.Is(err, ErrManagedTerminalNotReady) {
		t.Fatalf("agent close = %v, want managed-review refusal", err)
	}
	if policy.calls != 1 {
		t.Fatalf("policy calls = %d, want 1", policy.calls)
	}
	// Human UI closure remains on its existing path; policy observes it but
	// does not insert an approval step.
	if _, err := svc.Transition(context.Background(), user, TransitionInput{TaskID: task.ID, To: "done"}); err != nil {
		t.Fatalf("human close = %v", err)
	}
	if policy.calls != 2 {
		t.Fatalf("terminal actions consulted policy %d times, want 2", policy.calls)
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
	// The creation, then the three moves.
	if len(after.History) != 4 {
		t.Fatalf("history = %+v", after.History)
	}
	if created := after.History[0]; created.From != "" || created.To != "todo" || created.By != "user" {
		t.Fatalf("creation entry = %+v", created)
	}
	if after.History[1].By != "agent:sess-1" {
		t.Fatalf("history author = %q", after.History[1].By)
	}
}

func TestATaskRecordsWhoCreatedIt(t *testing.T) {
	svc, _ := newService(t)
	board := newBoard(t, svc)
	task, err := svc.Create(agent, CreateInput{BoardID: board.ID, Title: "made by an agent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.History) != 1 || task.History[0].From != "" || task.History[0].By != "agent:sess-1" {
		t.Fatalf("history = %+v, want one creation entry by the agent", task.History)
	}
	listed, err := svc.List(user, ListInput{BoardID: board.ID})
	if err != nil || len(listed) != 1 || len(listed[0].History) != 1 {
		t.Fatalf("a list carries the creation too: %+v, %v", listed, err)
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

func TestConfirmationPathRechecksTerminalPolicyBeforeMoving(t *testing.T) {
	svc, db := newService(t)
	confirm := &stubConfirm{allow: true}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, confirm, nil))
	policy := &denyAfterFirstTerminalPolicy{}
	svc.SetTerminalPolicy(policy)
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "managed during confirmation"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"}); !errors.Is(err, ErrManagedTerminalNotReady) {
		t.Fatalf("terminal transition = %v, want rechecked policy refusal", err)
	}
	if confirm.asked != 1 {
		t.Fatalf("confirmation prompts = %d, want one", confirm.asked)
	}
	after, err := svc.Get(user, task.ID)
	if err != nil || after.Status != "todo" {
		t.Fatalf("task after changed policy = %+v, %v", after, err)
	}
	policy.mu.Lock()
	calls := policy.calls
	policy.mu.Unlock()
	if calls != 2 {
		t.Fatalf("terminal policy calls = %d, want request and post-confirmation checks", calls)
	}
}

type taskMovingConfirm struct {
	svc    *Service
	taskID string
	asked  int
}

func (c *taskMovingConfirm) Confirm(ctx context.Context, _, _ string) (bool, error) {
	c.asked++
	_, err := c.svc.Transition(ctx, user, TransitionInput{TaskID: c.taskID, To: "review"})
	return err == nil, err
}

func TestConfirmationPathRefusesWhenTaskMovedWhileWaiting(t *testing.T) {
	svc, db := newService(t)
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "changed during confirmation"})
	if err != nil {
		t.Fatal(err)
	}
	confirm := &taskMovingConfirm{svc: svc, taskID: task.ID}
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, confirm, nil))
	if _, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"}); !errors.Is(err, ErrStatusChanged) {
		t.Fatalf("close after concurrent move = %v, want status-changed refusal", err)
	}
	if confirm.asked != 1 {
		t.Fatalf("confirmation prompts = %d, want one", confirm.asked)
	}
	after, err := svc.Get(user, task.ID)
	if err != nil || after.Status != "review" {
		t.Fatalf("task after concurrent move = %+v, %v", after, err)
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

type denyAfterFirstTerminalPolicy struct {
	mu    sync.Mutex
	calls int
}

func (p *denyAfterFirstTerminalPolicy) CheckTerminal(context.Context, Caller, Task, string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls > 1 {
		return ErrManagedTerminalNotReady
	}
	return nil
}

func waitForTerminalGateDrain(t *testing.T, gate *verifyGate) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		gate.mu.Lock()
		quiet := len(gate.draining) == 0
		gate.mu.Unlock()
		if quiet {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("terminal gate did not drain")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVerifyPassRechecksTerminalPolicyBeforeApplying(t *testing.T) {
	svc, db := newService(t)
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, nil, nil))
	policy := &denyAfterFirstTerminalPolicy{}
	svc.SetTerminalPolicy(policy)
	board := newBoard(t, svc)
	project := t.TempDir()
	if _, err := svc.UpdateBoard(board.ID, func(b *Board) {
		b.Verify = &Verify{Command: "true", TimeoutSec: 30}
		b.ProjectPath = project
	}); err != nil {
		t.Fatal(err)
	}
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "managed after queue"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"})
	if err != nil || !result.Verifying {
		t.Fatalf("queued transition = %+v, %v", result, err)
	}
	waitForTerminalGateDrain(t, svc.Gate())
	runs, err := svc.Gate().Runs(task.ID, 1)
	if err != nil || len(runs) != 1 || runs[0].State != RunPassed {
		t.Fatalf("verify did not pass: %+v, %v", runs, err)
	}
	after, err := svc.Get(user, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != "todo" {
		t.Fatalf("a policy change during verification closed the task: %q", after.Status)
	}
	policy.mu.Lock()
	calls := policy.calls
	policy.mu.Unlock()
	if calls != 2 {
		t.Fatalf("terminal policy calls = %d, want request and verifier-completion checks", calls)
	}
}

func TestCallerFromAuthorFailsClosedForMalformedVerifyRequest(t *testing.T) {
	for _, author := range []string{"", "user", "agent:", "unknown:session"} {
		if caller, ok := callerFromAuthor(author); ok || caller.IsAgent() {
			t.Fatalf("callerFromAuthor(%q) = %+v, %v; want refusal", author, caller, ok)
		}
	}
	if caller, ok := callerFromAuthor("agent:session-1"); !ok || caller.SessionID != "session-1" {
		t.Fatalf("valid agent author = %+v, %v", caller, ok)
	}
}

type sliceTerminalPolicy struct{ state []string }

func (sliceTerminalPolicy) CheckTerminal(context.Context, Caller, Task, string) error { return nil }

func TestHasTerminalPolicyHandlesNonComparableImplementations(t *testing.T) {
	svc, _ := newService(t)
	policy := sliceTerminalPolicy{state: []string{"managed"}}
	svc.SetTerminalPolicy(policy)
	if svc.HasTerminalPolicy(policy) {
		t.Fatal("non-comparable policy was reported as an exact installed guard")
	}
}

type denyAllTerminalPolicy struct{}

func (denyAllTerminalPolicy) CheckTerminal(context.Context, Caller, Task, string) error {
	return ErrManagedTerminalNotReady
}

type allowAllTerminalPolicy struct{}

func (allowAllTerminalPolicy) CheckTerminal(context.Context, Caller, Task, string) error { return nil }

func TestVerifyCompletionUsesCurrentPolicyAfterReplacement(t *testing.T) {
	svc, db := newService(t)
	svc.SetGate(NewGate(db, storage.TableTaskVerifyRuns, nil, nil))
	svc.SetTerminalPolicy(allowAllTerminalPolicy{})
	board := newBoard(t, svc)
	project := t.TempDir()
	if _, err := svc.UpdateBoard(board.ID, func(b *Board) {
		b.Verify = &Verify{Command: "while [ ! -f release-verify ]; do sleep 0.01; done; true", TimeoutSec: 30}
		b.ProjectPath = project
	}); err != nil {
		t.Fatal(err)
	}
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "policy replaced during verify"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Transition(context.Background(), agent, TransitionInput{TaskID: task.ID, To: "done"})
	if err != nil || !result.Verifying {
		t.Fatalf("queued transition = %+v, %v", result, err)
	}
	newPolicy := denyAllTerminalPolicy{}
	svc.SetTerminalPolicy(newPolicy)
	if err := os.WriteFile(filepath.Join(project, "release-verify"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForTerminalGateDrain(t, svc.Gate())
	runs, err := svc.Gate().Runs(task.ID, 1)
	if err != nil || len(runs) != 1 || runs[0].State != RunPassed {
		t.Fatalf("verify did not pass: %+v, %v", runs, err)
	}
	after, err := svc.Get(user, task.ID)
	if err != nil || after.Status != "todo" {
		t.Fatalf("task after policy replacement = %+v, %v", after, err)
	}
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
	if _, err := gate.enqueue(board, board.ProjectPath, task, "done", "agent:x"); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.enqueue(board, board.ProjectPath, task, "done", "agent:x"); !errors.Is(err, ErrVerifying) {
		t.Fatalf("second enqueue = %v, want ErrVerifying", err)
	}
}

// With no scope wired at all, verifyDir must read exactly as it did before
// bindings existed: the board's own project_path, unconditionally.
func TestVerifyDirWithNoScopeUsesProjectPathAsBefore(t *testing.T) {
	svc, _ := newService(t)
	dir, err := svc.verifyDir(Board{ProjectPath: "/some/path"})
	if err != nil || dir != "/some/path" {
		t.Fatalf("verifyDir with no scope = %q, %v", dir, err)
	}
}

// Recover alone leaves an interrupted run queued, not running; DrainAll is
// the sweep that actually finishes it, the way startup runs it once after
// Recover.
func TestRecoverAndDrainAllFinishAnInterruptedRun(t *testing.T) {
	svc, db := newService(t)
	notify := &stubNotify{mu: make(chan struct{}, 4)}
	gate := NewGate(db, storage.TableTaskVerifyRuns, nil, notify)
	svc.SetGate(gate)
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	dir := t.TempDir()

	if _, err := db.Exec(`INSERT INTO `+storage.TableTaskVerifyRuns+
		` (id, task_id, board_id, project_path, target_status, command, timeout_sec, requested_by, requested_at, state)
		  VALUES ('run-interrupted', ?, ?, ?, 'done', 'true', 30, 'agent:x', ?, 'running')`,
		task.ID, board.ID, dir, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := gate.Recover(); err != nil {
		t.Fatal(err)
	}
	gate.DrainAll()
	waitForNotification(t, notify)

	after, _ := svc.Get(user, task.ID)
	if after.Status != "done" {
		t.Fatalf("status after Recover+DrainAll = %q", after.Status)
	}
	runs, _ := gate.Runs(task.ID, 5)
	if len(runs) != 1 || runs[0].State != RunPassed {
		t.Fatalf("runs = %+v", runs)
	}
}

// A queued run whose folder has vanished from this machine — a different
// device, a moved or deleted worktree — grades nothing rather than nothing:
// it goes stale, same as a request that simply waited too long, and the task
// stays exactly where it was.
func TestQueuedRunWithAMissingProjectPathGoesStale(t *testing.T) {
	svc, db := newService(t)
	gate := NewGate(db, storage.TableTaskVerifyRuns, nil, nil)
	svc.SetGate(gate)
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x"})
	missing := t.TempDir() + "/gone"

	if _, err := db.Exec(`INSERT INTO `+storage.TableTaskVerifyRuns+
		` (id, task_id, board_id, project_path, target_status, command, timeout_sec, requested_by, requested_at, state)
		  VALUES ('run-missing', ?, ?, ?, 'done', 'true', 30, 'agent:x', ?, 'queued')`,
		task.ID, board.ID, missing, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	run, err := gate.next(missing)
	if err != nil {
		t.Fatal(err)
	}
	if run.ID != "" {
		t.Fatalf("a run whose folder is gone was started: %+v", run)
	}
	runs, _ := gate.Runs(task.ID, 5)
	if len(runs) != 1 || runs[0].State != RunStale {
		t.Fatalf("runs = %+v, want stale", runs)
	}
	after, _ := svc.Get(user, task.ID)
	if after.Status == "done" {
		t.Fatal("a missing folder still closed the task")
	}
	assertSystemComment(t, svc, task.ID, "not on this machine")
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

// A dependency that would loop back is refused: A waiting on B while B waits
// on A is a pair of tasks neither of which can ever start.
func TestSetFieldsRefusesDependencyCycles(t *testing.T) {
	svc, board := newDependencyBoard(t)
	a := mustCreate(t, svc, board.ID, "A")
	b := mustCreate(t, svc, board.ID, "B")
	c := mustCreate(t, svc, board.ID, "C")

	// A depends on B, B depends on C: a chain, which is fine.
	if _, err := svc.SetFields(Caller{}, a.ID, Fields{BlockedBy: &[]string{b.ID, b.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetFields(Caller{}, b.ID, Fields{BlockedBy: &[]string{c.ID}}); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(Caller{}, a.ID)
	if len(got.BlockedBy) != 1 {
		t.Fatalf("duplicates kept: %v", got.BlockedBy)
	}
	// C depending on A closes the loop A → B → C → A.
	if _, err := svc.SetFields(Caller{}, c.ID, Fields{BlockedBy: &[]string{a.ID}}); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("cycle err = %v", err)
	}
	if _, err := svc.SetFields(Caller{}, a.ID, Fields{BlockedBy: &[]string{a.ID}}); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("self err = %v", err)
	}
	if _, err := svc.SetFields(Caller{}, a.ID, Fields{BlockedBy: &[]string{"missing"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err = %v", err)
	}
	// Clearing is always allowed.
	if _, err := svc.SetFields(Caller{}, a.ID, Fields{BlockedBy: &[]string{}}); err != nil {
		t.Fatal(err)
	}
}

// A blocker has to be on the task's own board.
func TestSetFieldsRefusesBlockersFromAnotherBoard(t *testing.T) {
	svc, board := newDependencyBoard(t)
	other, err := svc.CreateBoard("Other", "OTH", "", Workflow{})
	if err != nil {
		t.Fatal(err)
	}
	a := mustCreate(t, svc, board.ID, "A")
	elsewhere := mustCreate(t, svc, other.ID, "Elsewhere")
	if _, err := svc.SetFields(Caller{}, a.ID, Fields{BlockedBy: &[]string{elsewhere.ID}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-board err = %v", err)
	}
}

func TestSetFieldsExpectedDependenciesRejectStaleAndSerializeConcurrentReplacements(t *testing.T) {
	svc, board := newDependencyBoard(t)
	a := mustCreate(t, svc, board.ID, "A")
	b := mustCreate(t, svc, board.ID, "B")
	c := mustCreate(t, svc, board.ID, "C")
	empty := []string{}
	if _, err := svc.SetFields(Caller{}, a.ID, Fields{BlockedBy: &[]string{b.ID}, ExpectedBlockedBy: &empty}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetFields(Caller{}, a.ID, Fields{BlockedBy: &[]string{c.ID}, ExpectedBlockedBy: &empty}); !errors.Is(err, ErrDependenciesChanged) {
		t.Fatalf("stale expected state: %v", err)
	}

	current := []string{b.ID}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, replacement := range [][]string{{}, {c.ID}} {
		replacement := replacement
		go func() {
			<-start
			_, err := svc.SetFields(Caller{}, a.ID, Fields{BlockedBy: &replacement, ExpectedBlockedBy: &current})
			errs <- err
		}()
	}
	close(start)
	first, second := <-errs, <-errs
	successes, conflicts := 0, 0
	for _, err := range []error{first, second} {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrDependenciesChanged) {
			conflicts++
		} else {
			t.Fatalf("unexpected result: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func newDependencyBoard(t *testing.T) (*Service, Board) {
	t.Helper()
	svc, _ := newService(t)
	board, err := svc.CreateBoard("Deps", "DEP", "", Workflow{})
	if err != nil {
		t.Fatal(err)
	}
	return svc, board
}

func mustCreate(t *testing.T, svc *Service, boardID, title string) Task {
	t.Helper()
	task, err := svc.Create(Caller{}, CreateInput{BoardID: boardID, Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// The session that made a task and every session that picked it up are both
// in its history, so each keeps it after the other takes it over.
func TestAPickIsRecordedBesideTheCreation(t *testing.T) {
	svc, _ := newService(t)
	svc.SetLiveness(liveness{}) // nobody alive: a later claim may take over
	board := newBoard(t, svc)
	maker := Caller{SessionID: "sess-maker"}
	task, err := svc.Create(maker, CreateInput{BoardID: board.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := svc.Claim(agent, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Created by the maker, moved into the active column, picked by sess-1.
	h := claimed.History
	if len(h) != 3 || h[0].By != "agent:sess-maker" || h[0].From != "" || h[1].IsPick() || !h[2].IsPick() || h[2].By != "agent:sess-1" {
		t.Fatalf("history = %+v", h)
	}
	if claimed.Status != "in_progress" || claimed.SessionID != "sess-1" {
		t.Fatalf("claimed = %+v", claimed)
	}

	// Claiming what it already holds records nothing more.
	again, err := svc.Claim(agent, task.ID)
	if err != nil || len(again.History) != 3 {
		t.Fatalf("re-claim history = %+v, %v", again.History, err)
	}

	// sess-1 is dead; sess-2 takes it over. Already active: a pick, no move.
	taken, err := svc.Claim(Caller{SessionID: "sess-2"}, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	last := taken.History[len(taken.History)-1]
	if len(taken.History) != 4 || !last.IsPick() || last.By != "agent:sess-2" || taken.SessionID != "sess-2" {
		t.Fatalf("takeover history = %+v", taken.History)
	}
}

func TestAssignGivesTheTaskToAnotherSessionAndWritesItsPick(t *testing.T) {
	svc, _ := newService(t)
	svc.SetLiveness(liveness{"sess-1": true, "child-1": true})
	board := newBoard(t, svc)
	task, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "x", Description: "the brief"})

	// Asking changes nothing.
	if _, err := svc.Assignable(agent, task.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(user, task.ID); got.SessionID != "" || got.Status != "todo" {
		t.Fatalf("Assignable wrote: %+v", got)
	}

	got, err := svc.Assign(agent, task.ID, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	last := got.History[len(got.History)-1]
	if got.SessionID != "child-1" || got.Status != "in_progress" || last.By != "agent:child-1" || last.From != last.To {
		t.Fatalf("assign = %+v, last history %+v: want child-1 holding it in the active column, picked by the child", got, last)
	}
	// The parent did not become the holder, and the child's own claim of it is a no-op.
	if _, err := svc.Claim(Caller{SessionID: "child-1"}, task.ID); err != nil {
		t.Fatalf("the child cannot claim what it was given: %v", err)
	}
	// Another live session cannot take it, nor be given it.
	if _, err := svc.Assign(agent, task.ID, "child-2"); !errors.Is(err, ErrClaimed) {
		t.Fatalf("a live child's task was given away: %v", err)
	}
}

func TestAssignRefusesWhatClaimWouldQuietlyReopenOrSteal(t *testing.T) {
	svc, _ := newService(t)
	svc.SetLiveness(liveness{"sess-1": true, "sess-2": true})
	board := newBoard(t, svc)

	// A finished task is not given to anyone; Claim has always reopened one.
	done, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "done"})
	if _, err := svc.mutate(done.ID, func(m *taskMetadata) error { m.Status = "done"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Assign(agent, done.ID, "child-1"); !errors.Is(err, ErrNotAssignable) {
		t.Errorf("a finished task = %v, want ErrNotAssignable", err)
	}
	// An archived one neither.
	old, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "old"})
	if _, err := svc.Archive(user, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Assign(agent, old.ID, "child-1"); !errors.Is(err, ErrNotAssignable) {
		t.Errorf("an archived task = %v, want ErrNotAssignable", err)
	}
	// A task another live session holds.
	held, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "held"})
	if _, err := svc.Claim(Caller{SessionID: "sess-2"}, held.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Assign(agent, held.ID, "child-1"); !errors.Is(err, ErrClaimed) {
		t.Errorf("another live session's task = %v, want ErrClaimed", err)
	}
	// But one the caller holds itself is handed over, and a dead holder's is taken.
	mine, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "mine"})
	if _, err := svc.Claim(agent, mine.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.Assign(agent, mine.ID, "child-1"); err != nil || got.SessionID != "child-1" {
		t.Errorf("handing over its own task = %+v, %v", got, err)
	}
	svc.SetLiveness(liveness{"sess-1": true})
	if got, err := svc.Assign(agent, held.ID, "child-2"); err != nil || got.SessionID != "child-2" {
		t.Errorf("a dead holder's task = %+v, %v", got, err)
	}
	// Only an agent gives, and not to itself, or to nobody.
	fresh, _ := svc.Create(user, CreateInput{BoardID: board.ID, Title: "fresh"})
	for name, call := range map[string]func() error{
		"user":    func() error { _, err := svc.Assign(user, fresh.ID, "child-1"); return err },
		"self":    func() error { _, err := svc.Assign(agent, fresh.ID, "sess-1"); return err },
		"nobody":  func() error { _, err := svc.Assign(agent, fresh.ID, ""); return err },
		"missing": func() error { _, err := svc.Assign(agent, "no-such-task", "child-1"); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

type liveSessions map[string]bool

func (l liveSessions) Alive(id string) bool { return l[id] }

// A claim held by another live session is not dropped or re-aimed by an agent:
// Release followed by Claim would otherwise take the task from a session that Claim
// itself refuses, and the branch on the task is what its holder may push.
func TestAnAgentDoesNotReleaseOrReaimTheLiveClaimOfAnotherSession(t *testing.T) {
	svc, _ := newService(t)
	live := liveSessions{"holder": true, "other": true}
	svc.SetLiveness(live)
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "held"})
	if err != nil {
		t.Fatal(err)
	}
	holder, other := Caller{SessionID: "holder"}, Caller{SessionID: "other"}
	if _, err := svc.Claim(holder, task.ID); err != nil {
		t.Fatal(err)
	}
	branch := "feature/mine"
	if _, err := svc.SetFields(holder, task.ID, Fields{Branch: &branch}); err != nil {
		t.Fatal(err)
	}

	hijack := "feature/hijack"
	if _, err := svc.SetFields(other, task.ID, Fields{Branch: &hijack}); !errors.Is(err, ErrClaimed) {
		t.Fatalf("re-aiming the branch of another session's task = %v, want ErrClaimed", err)
	}
	if _, err := svc.Release(other, task.ID); !errors.Is(err, ErrClaimed) {
		t.Fatalf("releasing another session's live claim = %v, want ErrClaimed", err)
	}
	if got, _ := svc.Get(user, task.ID); got.SessionID != "holder" || got.Branch != branch {
		t.Fatalf("a refused write changed the claim: %+v", got)
	}

	// Everything that is not a claim stays open to a collaborator.
	priority := "high"
	if _, err := svc.SetFields(other, task.ID, Fields{Priority: &priority}); err != nil {
		t.Fatalf("a non-claim field of a held task: %v", err)
	}
	// The holder, the person, and anyone once the holder is gone are not refused.
	if _, err := svc.SetFields(holder, task.ID, Fields{Branch: &hijack}); err != nil {
		t.Fatalf("the holder re-aims its own branch: %v", err)
	}
	live["holder"] = false
	if _, err := svc.SetFields(other, task.ID, Fields{Branch: &branch}); err != nil {
		t.Fatalf("a dead session's claim must not be held against anyone: %v", err)
	}
	if _, err := svc.Release(other, task.ID); err != nil {
		t.Fatalf("releasing a dead session's claim: %v", err)
	}
	if _, err := svc.Claim(holder, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Release(user, task.ID); err != nil {
		t.Fatalf("the person releases: %v", err)
	}
}

// barrierLive reports a dead holder and, once, parks the first liveness check so a
// test can run another write in the gap between a holder check and its write.
type barrierLive struct {
	entered chan struct{}
	proceed chan struct{}
	armed   atomic.Bool
}

// Alive parks only the first check after arming; later checks (the other session's
// claim) answer at once, which is what opens the gap.
func (b *barrierLive) Alive(string) bool {
	if b.armed.CompareAndSwap(true, false) {
		close(b.entered)
		<-b.proceed
	}
	return false
}

// A Release that found the holder dead must not erase a claim another session took
// while it was deciding: the check and the write are one step.
func TestAReleaseDoesNotEraseAClaimTakenWhileItDecided(t *testing.T) {
	svc, _ := newService(t)
	live := &barrierLive{entered: make(chan struct{}), proceed: make(chan struct{})}
	svc.SetLiveness(live)
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "contended"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Claim(Caller{SessionID: "dead"}, task.ID); err != nil {
		t.Fatal(err)
	}
	live.armed.Store(true) // only the check made inside Release parks

	released := make(chan error, 1)
	go func() { _, err := svc.Release(Caller{SessionID: "releaser"}, task.ID); released <- err }()
	<-live.entered
	claimed := make(chan error, 1)
	go func() { _, err := svc.Claim(Caller{SessionID: "claimer"}, task.ID); claimed <- err }()
	time.Sleep(100 * time.Millisecond) // the claim reaches its write, or waits for the release
	close(live.proceed)
	if err := <-released; err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := <-claimed; err != nil {
		t.Fatalf("claim: %v", err)
	}
	if got, _ := svc.Get(user, task.ID); got.SessionID != "claimer" {
		t.Fatalf("the claim taken during the release was erased: holder %q", got.SessionID)
	}
}

// Two sessions claiming a free task at once: exactly one holds it.
func TestTwoConcurrentClaimsOfAFreeTaskLeaveOneHolder(t *testing.T) {
	svc, _ := newService(t)
	svc.SetLiveness(liveSessions{"a": true, "b": true})
	board := newBoard(t, svc)
	task, err := svc.Create(user, CreateInput{BoardID: board.ID, Title: "free"})
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		go func() { _, err := svc.Claim(Caller{SessionID: id}, task.ID); errs <- err }()
	}
	var ok, claimed int
	for range 2 {
		switch err := <-errs; {
		case err == nil:
			ok++
		case errors.Is(err, ErrClaimed):
			claimed++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || claimed != 1 {
		t.Fatalf("%d claims succeeded and %d were refused, want one of each", ok, claimed)
	}
}
