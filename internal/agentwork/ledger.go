package agentwork

import (
	"errors"
	"fmt"
	"time"
)

// Ledger state names are deliberately independent of task-board status labels.
type GroupState struct {
	ID, WorkspaceID, BoardID, ParentTaskID, CoordinatorID string
	StatusMap                                             StatusMap
	SchemaVersion                                         int
	EndedAt                                               *time.Time
	TombstoneReason                                       string
	Enabled, Paused                                       bool
	PolicyRevision                                        int64
	Revision                                              int64
	CreatedAt, UpdatedAt                                  time.Time
}

type WorkItem struct {
	ID, GroupID, WorkspaceID, BoardID, ParentTaskID, TaskID string
	Role, State                                             string
	HostStatusID, TombstoneReason                           string
	SchemaVersion                                           int
	EndedAt                                                 *time.Time
	TaskRevision, Revision                                  int64
	RequirementsRevision                                    string
	CurrentAttemptID                                        string
	CreatedAt, UpdatedAt                                    time.Time
}

// StatusMap binds the four lifecycle meanings to real status IDs on the
// task board. Review is explicit; it is never inferred from a status category.
type StatusMap struct {
	Ready, Running, AwaitingReview, Accepted string
}

const ManagedSchemaVersion = 1

const (
	TombstoneUserEnded       = "user_ended"
	TombstoneOperatorStopped = "operator_stopped"
	TombstoneParentDeleted   = "parent_deleted"
	TombstoneParentArchived  = "parent_archived"
	TombstoneParentTerminal  = "parent_terminal"
	TombstoneDeleted         = "deleted"
	TombstoneArchived        = "archived"
	TombstoneTerminal        = "terminal"
)

func (m StatusMap) Validate(statuses []BoardStatus) error {
	if m.Ready == "" || m.Running == "" || m.AwaitingReview == "" || m.Accepted == "" {
		return errors.New("managed status map requires ready, running, awaitingReview and accepted IDs")
	}
	byID := make(map[string]BoardStatus, len(statuses))
	for _, status := range statuses {
		byID[status.ID] = status
	}
	for _, id := range []string{m.Ready, m.Running, m.AwaitingReview, m.Accepted} {
		if _, ok := byID[id]; !ok {
			return fmt.Errorf("managed status %q is not on the board", id)
		}
	}
	if byID[m.Ready].Category != "open" || byID[m.Running].Category != "active" || byID[m.AwaitingReview].Category != "active" {
		return errors.New("ready must be open and running/awaitingReview must be active board statuses")
	}
	if byID[m.Accepted].Category != "terminal" || byID[m.Accepted].WontDo || m.Accepted == "wont_do" {
		return errors.New("accepted status must be a real terminal status other than won't do")
	}
	return nil
}

type BoardStatus struct {
	ID, Category string
	WontDo       bool
}

type GroupPage struct {
	Items []GroupState
	Next  string
}

type Attempt struct {
	ID, WorkItemID, GroupID, WorkspaceID, BoardID, ParentTaskID string
	SessionID, Role, State, DispatchState                       string
	TaskRevision, Generation, Revision                          int64
	SchemaVersion                                               int
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
