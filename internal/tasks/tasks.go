// Package tasks is a board where an agent is a first-class actor.
//
// An agent creates, claims, transitions and comments through MCP without
// anyone doing it on its behalf. Two things stay with a person: closing a task
// and deleting one. Closing is the last control there is, so it does not open
// on a claim — see gate.go, where a board's verify command is run by DevKit
// and its exit code decides, rather than an agent reporting that tests passed.
package tasks

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Category is what a status means. Status names are the user's; every rule in
// this package reads the category instead, so renaming a column cannot move a
// gate.
type Category string

const (
	CategoryOpen     Category = "open"
	CategoryActive   Category = "active"
	CategoryTerminal Category = "terminal"
)

// CommentKind separates the three voices. System comments are the service's
// own record of what happened and are shown in their own tab, not mixed into
// the conversation.
type CommentKind string

const (
	CommentHuman  CommentKind = "human"
	CommentAgent  CommentKind = "agent"
	CommentSystem CommentKind = "system"
)

var (
	ErrNotFound = errors.New("tasks.notFound")
	ErrInvalid  = errors.New("tasks.invalid")
	// ErrStatusChanged is an optimistic transition losing its race. It is a
	// refusal rather than an overwrite: two agents on one task must not
	// silently clobber each other.
	ErrStatusChanged = errors.New("tasks.statusChanged")
	// ErrClaimed is a task another live session holds.
	ErrClaimed = errors.New("tasks.claimed")
	// ErrNotAssignable is a task that is finished or archived: it is not
	// given to a session, because that would reopen it.
	ErrNotAssignable = errors.New("tasks.notAssignable")
	// ErrConfirmationRequired is a terminal transition with nobody to ask.
	ErrConfirmationRequired = errors.New("tasks.confirmationRequired")
	ErrPermissionDenied     = errors.New("tasks.permissionDenied")
	// ErrVerifying is a close already in flight for this task.
	ErrVerifying = errors.New("tasks.verifying")
	// ErrParentHasParent guards the one-level rule.
	ErrParentHasParent = errors.New("tasks.parentHasParent")
	// ErrBoardUnbound is a board that declared a verify command but has no
	// directory this machine can resolve: no workspace binding, or a binding
	// whose service does not resolve here. Falling back to asking a person
	// would silently swap the board's rule — an exit code decides — for a
	// different one nobody agreed to, so this refuses instead.
	ErrBoardUnbound = errors.New("tasks.boardUnbound")
	// ErrDependencyCycle is a blocked_by list that would make a task wait,
	// directly or through others, on itself — a set of tasks none of which
	// can ever start.
	ErrDependencyCycle = errors.New("tasks.dependencyCycle")
)

// Status is one column of a board.
type Status struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category Category `json:"category"`
}

// Workflow is a board's columns in order.
type Workflow struct {
	Statuses []Status `json:"statuses"`
}

// Verify is the command DevKit runs when an agent asks to close a task.
//
// It is here, on the board, because it must be the user's: boards are
// tasks:admin and UI-only, so an agent can trigger this command and can never
// change it. An agent's own claim that tests passed is a sentence with nothing
// behind it — DevKit cannot see the tools it ran — so the exit code of a
// command a person chose is what opens the gate instead.
type Verify struct {
	Command    string `json:"command"`
	TimeoutSec int    `json:"timeout_sec"`
	// Service is the NAME of the workspace service the command runs in, empty
	// for the primary. A name, not a path: this travels with the board to
	// every device, and a name means the same service on each of them where a
	// path means a folder on one.
	Service string `json:"service,omitempty"`
}

// Board is one board.
type Board struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	KeyPrefix      string   `json:"keyPrefix"`
	Color          string   `json:"color"`
	Workflow       Workflow `json:"workflow"`
	Verify         *Verify  `json:"verify,omitempty"`
	BranchTemplate string   `json:"branchTemplate,omitempty"`
	ProjectPath    string   `json:"projectPath,omitempty"`

	// WorkspaceID is the workspace this board is bound to on THIS machine,
	// read from task_board_bindings every time and never written into the
	// board's metadata. The board syncs; a workspace id is only meaningful on
	// the machine that created it, so storing it on the board would hand every
	// other device a pointer into nothing — see binding.go.
	WorkspaceID string `json:"workspaceId,omitempty"`
}

// Transition is one entry of a task's history.
type Transition struct {
	From string `json:"from"`
	To   string `json:"to"`
	By   string `json:"by"`
	At   string `json:"at"`
}

// HistoryLimit bounds a task's transition history. It lives in the task's own
// metadata rather than in a fourth record type: nobody queries "every
// transition in the vault", it is small, and keeping it on the record means a
// task and its history always arrive on another device together.
const HistoryLimit = 50

// Task is one item of work.
type Task struct {
	ID         string       `json:"id"`
	BoardID    string       `json:"boardId"`
	Key        string       `json:"key"`
	Title      string       `json:"title"`
	Status     string       `json:"status"`
	Priority   string       `json:"priority"`
	Labels     []string     `json:"labels"`
	Assignee   string       `json:"assignee"`
	ParentID   string       `json:"parentId"`
	BlockedBy  []string     `json:"blockedBy"`
	PlanNodeID string       `json:"planNodeId"`
	SessionID  string       `json:"sessionId"`
	Branch     string       `json:"branch"`
	ArchivedAt string       `json:"archivedAt"`
	History    []Transition `json:"history"`
	CreatedAt  time.Time    `json:"createdAt"`
	UpdatedAt  time.Time    `json:"updatedAt"`

	// Description is only set by Get; List deliberately leaves it out so a
	// board of two thousand tasks is not two thousand decryptions.
	Description string `json:"description,omitempty"`
}

// Comment is one note on a task.
type Comment struct {
	ID        string      `json:"id"`
	TaskID    string      `json:"taskId"`
	Author    string      `json:"author"`
	Kind      CommentKind `json:"kind"`
	Body      string      `json:"body"`
	CreatedAt time.Time   `json:"createdAt"`
}

// Key ids for the three payloads. Distinct on purpose: an envelope written as
// one must not open as another.
const (
	KeyIDBoard   = "task-board"
	KeyIDTask    = "task-body"
	KeyIDComment = "task-comment-body"
)

// DefaultWorkflow is what a new board starts with.
func DefaultWorkflow() Workflow {
	return Workflow{Statuses: []Status{
		{ID: "todo", Name: "To do", Category: CategoryOpen},
		{ID: "in_progress", Name: "In progress", Category: CategoryActive},
		{ID: "review", Name: "In review", Category: CategoryActive},
		{ID: "done", Name: "Done", Category: CategoryTerminal},
		{ID: "wont_do", Name: "Won't do", Category: CategoryTerminal},
	}}
}

// CategoryOf finds a status's category. An unknown status has none, which
// every caller treats as "not terminal" — a board whose workflow changed under
// a task must not turn that task's column into a gate by accident.
func (w Workflow) CategoryOf(statusID string) (Category, bool) {
	for _, status := range w.Statuses {
		if status.ID == statusID {
			return status.Category, true
		}
	}
	return "", false
}

// FirstOf returns the first status of a category, which is where a claim puts
// a task and where a finished turn leaves it.
func (w Workflow) FirstOf(category Category) (string, bool) {
	for _, status := range w.Statuses {
		if status.Category == category {
			return status.ID, true
		}
	}
	return "", false
}

// LastOf returns the last status of a category — "In review" on the default
// workflow, which is where a completed turn leaves a task.
func (w Workflow) LastOf(category Category) (string, bool) {
	found, id := false, ""
	for _, status := range w.Statuses {
		if status.Category == category {
			id, found = status.ID, true
		}
	}
	return id, found
}

// Valid reports whether a workflow can run a board: statuses are unique, and
// there is at least one of each category, because a board you cannot start or
// finish work on is not a board.
func (w Workflow) Valid() bool {
	if len(w.Statuses) == 0 {
		return false
	}
	seen := map[string]bool{}
	has := map[Category]bool{}
	for _, status := range w.Statuses {
		if status.ID == "" || status.Name == "" || seen[status.ID] {
			return false
		}
		switch status.Category {
		case CategoryOpen, CategoryActive, CategoryTerminal:
		default:
			return false
		}
		seen[status.ID] = true
		has[status.Category] = true
	}
	return has[CategoryOpen] && has[CategoryActive] && has[CategoryTerminal]
}

// Key is the human-readable id, derived from the record id rather than from a
// counter: a counter needs one authority, and there is none when two devices
// create tasks offline. The trade is that keys do not reflect creation order.
func Key(prefix, recordID string) string {
	prefix = strings.ToUpper(strings.TrimSpace(prefix))
	if len(recordID) > 6 {
		recordID = recordID[:6]
	}
	return prefix + "-" + recordID
}

// boardMetadata is a board's plaintext half.
type boardMetadata struct {
	Name           string   `json:"name"`
	KeyPrefix      string   `json:"key_prefix"`
	Color          string   `json:"color,omitempty"`
	Workflow       Workflow `json:"workflow"`
	Verify         *Verify  `json:"verify,omitempty"`
	BranchTemplate string   `json:"branch_template,omitempty"`
	ProjectPath    string   `json:"project_path,omitempty"`
}

// taskMetadata is a task's plaintext half — everything the board renders,
// filters and sorts by, so none of it needs the vault open.
type taskMetadata struct {
	BoardID    string       `json:"board_id"`
	Key        string       `json:"key"`
	Title      string       `json:"title"`
	Status     string       `json:"status"`
	Priority   string       `json:"priority,omitempty"`
	Labels     []string     `json:"labels,omitempty"`
	Assignee   string       `json:"assignee,omitempty"`
	ParentID   string       `json:"parent_id,omitempty"`
	BlockedBy  []string     `json:"blocked_by,omitempty"`
	PlanNodeID string       `json:"plan_node_id,omitempty"`
	SessionID  string       `json:"session_id,omitempty"`
	Branch     string       `json:"branch,omitempty"`
	ArchivedAt string       `json:"archived_at,omitempty"`
	History    []Transition `json:"history,omitempty"`
}

type commentMetadata struct {
	TaskID string `json:"task_id"`
	Author string `json:"author"`
	Kind   string `json:"kind"`
}

func decodeInto(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrInvalid
	}
	return nil
}
