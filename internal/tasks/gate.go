package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"devkit/internal/vaultitem"
)

// RunState is where one verify run stands.
type RunState string

const (
	RunQueued    RunState = "queued"
	RunRunning   RunState = "running"
	RunPassed    RunState = "passed"
	RunFailed    RunState = "failed"
	RunCancelled RunState = "cancelled"
	// RunStale is a request that waited too long. Grading today's code against
	// a request from two days ago answers a question nobody asked.
	RunStale RunState = "stale"
)

// StaleAfter is how long a queued request stays worth running.
const StaleAfter = 24 * time.Hour

// outputTail bounds what a run's output contributes to a comment.
const outputTail = 4000

// Confirmer is the interaction gate, for boards with no verify command.
type Confirmer interface {
	Confirm(ctx context.Context, title, detail string) (bool, error)
}

// Notifier raises an OS notification when a run lands. It is
// internal/notify, declared locally so tasks does not import it.
type Notifier interface {
	Notify(module, kind, id, title, body string)
}

// Run is one queued or finished verify.
type Run struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"taskId"`
	BoardID     string    `json:"boardId"`
	ProjectPath string    `json:"projectPath"`
	Target      string    `json:"target"`
	Command     string    `json:"command"`
	TimeoutSec  int       `json:"timeoutSec"`
	RequestedBy string    `json:"requestedBy"`
	RequestedAt time.Time `json:"requestedAt"`
	State       RunState  `json:"state"`
	ExitCode    int       `json:"exitCode"`
	Output      string    `json:"output"`
}

// verifyGate owns the queue. One run at a time per project, because two runs
// share one work tree and both results are then worthless.
type verifyGate struct {
	db      *sql.DB
	table   string
	confirm Confirmer
	notify  Notifier
	svc     *Service
	now     func() time.Time

	mu       sync.Mutex
	draining map[string]bool
}

// NewGate builds the close gate. db may be nil, which leaves only the
// interactive path.
func NewGate(db *sql.DB, table string, confirm Confirmer, notify Notifier) *verifyGate {
	return &verifyGate{db: db, table: table, confirm: confirm, notify: notify,
		now: time.Now, draining: map[string]bool{}}
}

// closeThroughGate is the terminal-transition path for an agent.
//
// A board with a verify command queues a run and returns at once: the task
// keeps its current status until the run lands, because moving it to done and
// retracting it on a red result makes the board lie for a few minutes, and a
// board that lies for a few minutes is not one anyone trusts.
//
// A board without one asks, every time, with no allow-always.
func (s *Service) closeThroughGate(ctx context.Context, caller Caller, board Board, task Task, to string) (TransitionResult, error) {
	if s.gate == nil {
		return TransitionResult{}, ErrConfirmationRequired
	}
	if board.Verify == nil || strings.TrimSpace(board.Verify.Command) == "" {
		allowed, err := s.gate.ask(ctx, task, to)
		if err != nil {
			return TransitionResult{}, err
		}
		if !allowed {
			return TransitionResult{}, ErrPermissionDenied
		}
		moved, err := s.applyTransition(task.ID, caller, to)
		return TransitionResult{Task: moved}, err
	}
	dir, err := s.verifyDir(board)
	if err != nil {
		return TransitionResult{}, err
	}
	runID, err := s.gate.enqueue(board, dir, task, to, caller.Author())
	if err != nil {
		return TransitionResult{}, err
	}
	s.gate.drain(dir)
	return TransitionResult{Task: task, Verifying: true, RunID: runID}, nil
}

// verifyDir resolves the work tree a board's verify command runs in.
//
// The board's binding is local and never travels with the board (see
// binding.go), so this is worked out here, at the point of use, from
// whatever workspace the board is bound to on THIS machine — never stored,
// because a resolved path stored anywhere that syncs would be exactly the
// mistake `project_path` already made.
//
// Resolution order:
//  1. bound, and the board names a service ⇒ that service's path; not found
//     here ⇒ ErrBoardUnbound.
//  2. bound, no service named, but the board's legacy `project_path` still
//     names one of the workspace's services ⇒ that path, so a board written
//     before `verify.service` existed keeps working with no write of its own.
//  3. bound, no service named and no matching legacy path ⇒ the workspace's
//     primary.
//  4. no scope wired at all ⇒ `project_path`, exactly today's behaviour.
//  5. otherwise — scoped, but the board is not bound to a workspace here ⇒
//     ErrBoardUnbound.
//
// A resolution that comes out empty is folded into ErrBoardUnbound too: an
// empty `project_path` is a board with nowhere to run in, and enqueuing it
// anyway would write a run under a key nothing ever drains.
func (s *Service) verifyDir(board Board) (string, error) {
	dir, err := s.resolveVerifyDir(board)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", ErrBoardUnbound
	}
	return dir, nil
}

func (s *Service) resolveVerifyDir(board Board) (string, error) {
	if s.scope == nil {
		return board.ProjectPath, nil
	}
	if board.WorkspaceID == "" {
		return "", ErrBoardUnbound
	}
	service := ""
	if board.Verify != nil {
		service = board.Verify.Service
	}
	if service != "" {
		if path, ok := s.scope.ServicePath(board.WorkspaceID, service); ok {
			return path, nil
		}
		return "", ErrBoardUnbound
	}
	if board.ProjectPath != "" && s.scope.HasPath(board.WorkspaceID, board.ProjectPath) {
		return board.ProjectPath, nil
	}
	if path, ok := s.scope.ServicePath(board.WorkspaceID, ""); ok {
		return path, nil
	}
	return "", ErrBoardUnbound
}

func (g *verifyGate) ask(ctx context.Context, task Task, to string) (bool, error) {
	if g.confirm == nil {
		// Headless. A close that nobody can approve is a refusal, not a pass.
		return false, ErrConfirmationRequired
	}
	return g.confirm.Confirm(ctx,
		"Close "+task.Key+"?",
		"An agent wants to move "+task.Title+" to "+to+".")
}

// enqueue writes a queued run. dir is the already-resolved work tree — the
// caller decides that (verifyDir), never this, so a refusal to resolve never
// reaches the point of writing a row.
func (g *verifyGate) enqueue(board Board, dir string, task Task, to, by string) (string, error) {
	if g.db == nil {
		return "", ErrConfirmationRequired
	}
	if existing, err := g.pendingFor(task.ID); err == nil && existing != "" {
		return "", ErrVerifying
	}
	id, err := vaultitem.NewID()
	if err != nil {
		return "", err
	}
	_, err = g.db.Exec(fmt.Sprintf(
		`INSERT INTO %s (id, task_id, board_id, project_path, target_status, command, timeout_sec, requested_by, requested_at, state)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, g.table),
		id, task.ID, board.ID, dir, to, board.Verify.Command,
		board.Verify.TimeoutSec, by, g.now().UTC().Format(time.RFC3339Nano), string(RunQueued))
	if err != nil {
		return "", err
	}
	return id, nil
}

func (g *verifyGate) pendingFor(taskID string) (string, error) {
	var id string
	err := g.db.QueryRow(fmt.Sprintf(
		`SELECT id FROM %s WHERE task_id = ? AND state IN (?, ?) LIMIT 1`, g.table),
		taskID, string(RunQueued), string(RunRunning)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// Recover puts interrupted runs back in the queue.
//
// This is what makes a durable queue safe: a run nobody saw finish is never
// read as a result. It becomes queued again, and running it again is a new,
// complete run — so "a task only closes on a run that finished" still holds.
func (g *verifyGate) Recover() error {
	if g.db == nil {
		return nil
	}
	_, err := g.db.Exec(fmt.Sprintf(`UPDATE %s SET state = ?, started_at = NULL WHERE state = ?`, g.table),
		string(RunQueued), string(RunRunning))
	return err
}

// Drain runs the queue for a project. Called when a project opens and after
// each enqueue.
//
// Exported for the bridge: DevKit drains again whenever a workspace's work
// folder changes, on top of the two calls this package already makes on its
// own — otherwise a request that arrived while nobody was draining that path
// waits for the next enqueue to notice it.
func (g *verifyGate) Drain(projectPath string) { g.drain(projectPath) }

// DrainAll drains every project with a queued run. This is the sweep DevKit
// runs once at startup, right after Recover has put interrupted runs back in
// the queue — Recover alone leaves them queued, not running, and nothing
// drains them without this.
func (g *verifyGate) DrainAll() {
	if g.db == nil {
		return
	}
	rows, err := g.db.Query(fmt.Sprintf(
		`SELECT DISTINCT project_path FROM %s WHERE state = ?`, g.table), string(RunQueued))
	if err != nil {
		return
	}
	var paths []string
	for rows.Next() {
		var path string
		if rows.Scan(&path) == nil {
			paths = append(paths, path)
		}
	}
	rows.Close()
	for _, path := range paths {
		g.drain(path)
	}
}

func (g *verifyGate) drain(projectPath string) {
	if g.db == nil || projectPath == "" {
		return
	}
	g.mu.Lock()
	if g.draining[projectPath] {
		g.mu.Unlock()
		return
	}
	g.draining[projectPath] = true
	g.mu.Unlock()

	go func() {
		defer func() {
			g.mu.Lock()
			delete(g.draining, projectPath)
			g.mu.Unlock()
		}()
		for {
			run, err := g.next(projectPath)
			if err != nil || run.ID == "" {
				return
			}
			g.execute(run)
		}
	}()
}

func (g *verifyGate) next(projectPath string) (Run, error) {
	row := g.db.QueryRow(fmt.Sprintf(
		`SELECT id, task_id, board_id, project_path, target_status, command, timeout_sec, requested_by, requested_at
		 FROM %s WHERE project_path = ? AND state = ? ORDER BY requested_at LIMIT 1`, g.table),
		projectPath, string(RunQueued))
	var run Run
	var requestedAt string
	err := row.Scan(&run.ID, &run.TaskID, &run.BoardID, &run.ProjectPath, &run.Target,
		&run.Command, &run.TimeoutSec, &run.RequestedBy, &requestedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, nil
	}
	if err != nil {
		return Run{}, err
	}
	run.RequestedAt, _ = time.Parse(time.RFC3339Nano, requestedAt)
	if g.now().Sub(run.RequestedAt) > StaleAfter {
		_ = g.finish(run, RunStale, 0, "")
		g.comment(run, "Verify was not run: the request waited longer than "+StaleAfter.String()+
			" and the code it was made against has moved on.")
		return g.next(projectPath)
	}
	if _, err := os.Stat(run.ProjectPath); err != nil {
		// The work tree this run was queued against is gone from this
		// machine — a different device, a moved folder, a workspace that was
		// removed. Grading nothing is not a result either.
		_ = g.finish(run, RunStale, 0, "")
		g.comment(run, "Verify was not run: "+run.ProjectPath+" is not on this machine any more.")
		return g.next(projectPath)
	}
	if _, err := g.db.Exec(fmt.Sprintf(`UPDATE %s SET state = ?, started_at = ? WHERE id = ? AND state = ?`, g.table),
		string(RunRunning), g.now().UTC().Format(time.RFC3339Nano), run.ID, string(RunQueued)); err != nil {
		return Run{}, err
	}
	return run, nil
}

// execute runs the board's command and lets the exit code decide.
func (g *verifyGate) execute(run Run) {
	timeout := time.Duration(run.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// The command is the board's, which is tasks:admin and UI-only, so an
	// agent can trigger it and can never choose it. No caller parameter
	// reaches this line.
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", run.Command)
	cmd.Dir = run.ProjectPath
	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	tail := string(output)
	if len(tail) > outputTail {
		tail = "…" + tail[len(tail)-outputTail:]
	}

	state := RunPassed
	if exitCode != 0 {
		state = RunFailed
	}
	_ = g.finish(run, state, exitCode, tail)

	if state == RunPassed && g.svc != nil {
		// The close is recorded as the service, not as the agent: it is the
		// exit code that opened the gate.
		if _, err := g.svc.applyTransition(run.TaskID, Caller{}, run.Target); err != nil {
			g.comment(run, "Verify passed but the task could not be moved: "+err.Error())
			return
		}
	}
	g.comment(run, verifySummary(run, state, exitCode, tail))
	g.raise(run, state)
}

func verifySummary(run Run, state RunState, exitCode int, tail string) string {
	header := fmt.Sprintf("Verify %s: `%s` exited %d.", state, run.Command, exitCode)
	if tail == "" {
		return header
	}
	return header + "\n\n```\n" + tail + "\n```"
}

func (g *verifyGate) finish(run Run, state RunState, exitCode int, output string) error {
	_, err := g.db.Exec(fmt.Sprintf(
		`UPDATE %s SET state = ?, exit_code = ?, output = ?, finished_at = ? WHERE id = ?`, g.table),
		string(state), exitCode, output, g.now().UTC().Format(time.RFC3339Nano), run.ID)
	return err
}

func (g *verifyGate) comment(run Run, body string) {
	if g.svc == nil {
		return
	}
	_, _ = g.svc.systemComment(run.TaskID, body)
}

// raise is the notification. A durable queue's worst case is a task closing
// silently after a restart; this is what stops it being silent.
func (g *verifyGate) raise(run Run, state RunState) {
	if g.notify == nil || g.svc == nil {
		return
	}
	task, err := g.svc.get(run.TaskID)
	if err != nil {
		return
	}
	title := "Verify passed"
	body := task.Key + " " + task.Title + " is closed."
	if state != RunPassed {
		title = "Verify failed"
		body = task.Key + " " + task.Title + " stayed where it was."
	}
	g.notify.Notify("tasks", "task", run.TaskID, title, body)
}

// Runs reports a task's verify history, newest first.
func (g *verifyGate) Runs(taskID string, limit int) ([]Run, error) {
	if g.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := g.db.Query(fmt.Sprintf(
		`SELECT id, task_id, board_id, project_path, target_status, command, timeout_sec,
		        requested_by, requested_at, state, exit_code, output
		 FROM %s WHERE task_id = ? ORDER BY requested_at DESC LIMIT ?`, g.table), taskID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var run Run
		var requestedAt, state string
		if err := rows.Scan(&run.ID, &run.TaskID, &run.BoardID, &run.ProjectPath, &run.Target,
			&run.Command, &run.TimeoutSec, &run.RequestedBy, &requestedAt, &state,
			&run.ExitCode, &run.Output); err != nil {
			return nil, err
		}
		run.RequestedAt, _ = time.Parse(time.RFC3339Nano, requestedAt)
		run.State = RunState(state)
		out = append(out, run)
	}
	return out, rows.Err()
}

// Cancel stops a queued run. A ten-minute suite the user just realised is
// pointless has to be stoppable, and the queue must carry on afterwards.
func (g *verifyGate) Cancel(runID string) error {
	if g.db == nil {
		return nil
	}
	_, err := g.db.Exec(fmt.Sprintf(`UPDATE %s SET state = ? WHERE id = ? AND state = ?`, g.table),
		string(RunCancelled), runID, string(RunQueued))
	return err
}
