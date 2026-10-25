package agentwork

import (
	"errors"
	"fmt"
	"time"
)

// Ledger state names are deliberately independent of task-board status labels.
type GroupState struct {
	ID, WorkspaceID, BoardID, ParentTaskID, CoordinatorID string
	Enabled, Paused                                       bool
	PolicyRevision                                        int64
	Revision                                              int64
	CreatedAt, UpdatedAt                                  time.Time
}

type WorkItem struct {
	ID, GroupID, WorkspaceID, BoardID, ParentTaskID, TaskID string
	Role, State                                             string
	TaskRevision, Revision                                  int64
	RequirementsRevision                                    string
	CurrentAttemptID                                        string
	CreatedAt, UpdatedAt                                    time.Time
}

type Attempt struct {
	ID, WorkItemID, GroupID, WorkspaceID, BoardID, ParentTaskID string
	SessionID, Role, State, DispatchState                       string
	TaskRevision, Generation, Revision                          int64
	RequirementsRevision                                        string
	CreatedAt, UpdatedAt                                        time.Time
}

var (
	ErrInvalidTransition = errors.New("invalid managed-work transition")
	ErrStaleRevision     = errors.New("stale managed-work revision")
)

// CanTransitionAttempt enforces that an attempt's terminal state cannot be
// reopened. An ambiguous dispatch is terminal for automatic execution; a new
// attempt must be explicitly created after reconciliation.
func CanTransitionAttempt(from, to string) error {
	allowed := map[string]map[string]bool{
		"reserved":        {"session_created": true, "failed": true, "cancelled": true},
		"session_created": {"task_assigned": true, "failed": true, "cancelled": true},
		"task_assigned":   {"prompt_enqueued": true, "failed": true, "cancelled": true},
		"prompt_enqueued": {"running": true, "interrupted_unknown": true, "failed": true, "cancelled": true},
		"running":         {"awaiting_review": true, "blocked": true, "failed": true, "cancelled": true, "interrupted_unknown": true},
	}
	known := map[string]bool{"reserved": true, "session_created": true, "task_assigned": true, "prompt_enqueued": true, "running": true, "awaiting_review": true, "blocked": true, "failed": true, "cancelled": true, "interrupted_unknown": true, "superseded": true}
	if !known[from] || !known[to] {
		return fmt.Errorf("%w: unknown attempt state %q -> %q", ErrInvalidTransition, from, to)
	}
	if from == to && from != "" {
		return nil
	}
	if !allowed[from][to] {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}

// ValidateScope checks the immutable identifiers copied onto a work record.
func ValidateScope(workspaceID, boardID, parentTaskID, taskID string) error {
	if workspaceID == "" || boardID == "" || parentTaskID == "" || taskID == "" {
		return fmt.Errorf("managed-work scope requires workspace, board, parent task, and task IDs")
	}
	if taskID == parentTaskID {
		return fmt.Errorf("managed work item cannot be its own parent task")
	}
	return nil
}
