package tasks

import (
	"context"
	"encoding/json"
	"time"

	"devkit/internal/vaultitem"
)

// TransitionInput is one status change.
type TransitionInput struct {
	TaskID string
	To     string
	// ExpectedStatus makes the change optimistic: a task that moved since the
	// caller read it is refused rather than overwritten. Two agents on one
	// task must not clobber each other silently.
	ExpectedStatus string
}

// TransitionResult says what happened. A terminal transition that started a
// verify run is neither done nor refused — it is pending, and the task has
// not moved.
type TransitionResult struct {
	Task Task `json:"task"`
	// Verifying is set when a background verify decides this close.
	Verifying bool   `json:"verifying"`
	RunID     string `json:"runId,omitempty"`
}

// Transition moves a task.
//
// Open and active statuses go straight through. Terminal ones are the last
// control a person has, so they go through the board's gate: a verify command
// DevKit runs itself, or an interactive confirmation every time.
func (s *Service) Transition(ctx context.Context, caller Caller, in TransitionInput) (TransitionResult, error) {
	task, err := s.get(in.TaskID)
	if err != nil {
		return TransitionResult{}, err
	}
	board, err := s.Board(task.BoardID)
	if err != nil {
		return TransitionResult{}, err
	}
	if _, known := board.Workflow.CategoryOf(in.To); !known {
		return TransitionResult{}, ErrInvalid
	}
	if in.ExpectedStatus != "" && task.Status != in.ExpectedStatus {
		return TransitionResult{}, ErrStatusChanged
	}
	category, _ := board.Workflow.CategoryOf(in.To)
	if category == CategoryTerminal && caller.IsAgent() {
		return s.closeThroughGate(ctx, caller, board, task, in.To)
	}
	moved, err := s.applyTransition(task.ID, caller, in.To)
	return TransitionResult{Task: moved}, err
}

// applyTransition writes the new status and appends to the bounded history.
func (s *Service) applyTransition(id string, caller Caller, to string) (Task, error) {
	at := s.now().UTC().Format(time.RFC3339)
	return s.mutate(id, func(meta *taskMetadata) error {
		if meta.Status == to {
			return nil
		}
		meta.History = append(meta.History, Transition{
			From: meta.Status, To: to, By: caller.Author(), At: at,
		})
		if len(meta.History) > HistoryLimit {
			meta.History = meta.History[len(meta.History)-HistoryLimit:]
		}
		meta.Status = to
		return nil
	})
}

// Claim gives a task to an agent session.
//
// It never touches the work tree. Claim is a tool an agent calls freely with
// no confirmation, so checking out a branch here would rewrite the files on
// the user's screen with nobody asked. Creating a branch is a separate,
// explicit act.
func (s *Service) Claim(caller Caller, id string) (Task, error) {
	if !caller.IsAgent() {
		return Task{}, ErrInvalid
	}
	task, err := s.get(id)
	if err != nil {
		return Task{}, err
	}
	if task.SessionID != "" && task.SessionID != caller.SessionID {
		// A claim held by a session that has died can be taken over: holding
		// is so the agent can reconnect to its own work, not a lock forever.
		if s.live == nil || s.live.Alive(task.SessionID) {
			return Task{}, ErrClaimed
		}
	}
	board, err := s.Board(task.BoardID)
	if err != nil {
		return Task{}, err
	}
	active, ok := board.Workflow.FirstOf(CategoryActive)
	if !ok {
		return Task{}, ErrInvalid
	}
	claimed, err := s.mutate(id, func(meta *taskMetadata) error {
		meta.SessionID = caller.SessionID
		return nil
	})
	if err != nil {
		return Task{}, err
	}
	if claimed.Status != active {
		return s.applyTransition(id, caller, active)
	}
	return claimed, nil
}

// Release drops a claim without moving the task. A dead session's claim is
// deliberately NOT cleared automatically: the agent reopening reclaims its own
// work, and a live claim can take a dead one over anyway.
func (s *Service) Release(id string) (Task, error) {
	return s.mutate(id, func(meta *taskMetadata) error {
		meta.SessionID = ""
		return nil
	})
}

// ---- comments --------------------------------------------------------

// AddComment writes a comment. Authorship comes from the caller, never from a
// parameter: an agent cannot sign as the user.
func (s *Service) AddComment(caller Caller, taskID, body string) (Comment, error) {
	kind := CommentHuman
	if caller.IsAgent() {
		kind = CommentAgent
	}
	return s.writeComment(taskID, caller.Author(), kind, body)
}

// systemComment is the service's own record of something that happened. It
// goes to its own Activity tab rather than into the conversation.
func (s *Service) systemComment(taskID, body string) (Comment, error) {
	return s.writeComment(taskID, "system", CommentSystem, body)
}

func (s *Service) writeComment(taskID, author string, kind CommentKind, body string) (Comment, error) {
	if taskID == "" || body == "" {
		return Comment{}, ErrInvalid
	}
	if _, err := s.items.Get(taskID); err != nil {
		return Comment{}, ErrNotFound
	}
	meta := commentMetadata{TaskID: taskID, Author: author, Kind: string(kind)}
	raw, err := json.Marshal(meta)
	if err != nil {
		return Comment{}, err
	}
	record, err := s.items.Create(vaultitem.ItemTypeTaskComment, KeyIDComment, raw, []byte(body))
	if err != nil {
		return Comment{}, err
	}
	return Comment{
		ID: record.ID, TaskID: taskID, Author: author, Kind: kind,
		Body: body, CreatedAt: record.CreatedAt,
	}, nil
}

// Comments returns a task's comments. Human and agent comments are the
// conversation; system ones are the Activity tab, which is why the kind is on
// every row rather than inferred from the author.
func (s *Service) Comments(taskID string) ([]Comment, error) {
	records, err := s.items.ListByTypeAndMetaField(vaultitem.ItemTypeTaskComment, "task_id", taskID)
	if err != nil {
		return nil, err
	}
	out := make([]Comment, 0, len(records))
	for _, record := range records {
		var meta commentMetadata
		if decodeInto(record.Metadata, &meta) != nil {
			continue
		}
		comment := Comment{
			ID: record.ID, TaskID: meta.TaskID, Author: meta.Author,
			Kind: CommentKind(meta.Kind), CreatedAt: record.CreatedAt,
		}
		if body, err := s.items.OpenPayload(record, KeyIDComment); err == nil {
			comment.Body = string(body)
		}
		out = append(out, comment)
	}
	return out, nil
}

// Progress is a parent task's subtask counter. It counts by category, so a
// board that renamed its Done column still counts correctly, and it counts
// without blocking: closing a parent with open children stays allowed, the
// same rule blocked_by and the git facts already follow.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

func (s *Service) Progress(parentID string) (Progress, error) {
	parent, err := s.get(parentID)
	if err != nil {
		return Progress{}, err
	}
	board, err := s.Board(parent.BoardID)
	if err != nil {
		return Progress{}, err
	}
	children, err := s.List(ListInput{BoardID: parent.BoardID, ParentID: parentID})
	if err != nil {
		return Progress{}, err
	}
	progress := Progress{Total: len(children)}
	for _, child := range children {
		if category, ok := board.Workflow.CategoryOf(child.Status); ok && category == CategoryTerminal {
			progress.Done++
		}
	}
	return progress, nil
}
