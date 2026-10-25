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
	board, err := s.boardFor(caller, task.BoardID)
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
	if category == CategoryTerminal {
		if err := s.checkTerminalPolicy(ctx, caller, task, in.To); err != nil {
			return TransitionResult{}, err
		}
		if caller.IsAgent() {
			return s.closeThroughGate(ctx, caller, board, task, in.To)
		}
	}
	moved, err := s.applyTransition(task.ID, caller, in.To)
	return TransitionResult{Task: moved}, err
}

func (s *Service) checkTerminalPolicy(ctx context.Context, caller Caller, task Task, targetStatus string) error {
	policy := s.terminalPolicySnapshot()
	if policy == nil {
		return nil
	}
	return policy.CheckTerminal(ctx, caller, task, targetStatus)
}

// applyTransition writes the new status and appends to the bounded history.
func (s *Service) applyTransition(id string, caller Caller, to string) (Task, error) {
	at := s.now().UTC().Format(time.RFC3339)
	return s.mutate(id, func(meta *taskMetadata) error {
		if meta.Status == to {
			return nil
		}
		appendHistory(meta, Transition{From: meta.Status, To: to, By: caller.Author(), At: at})
		meta.Status = to
		return nil
	})
}

// appendHistory adds one entry and keeps the history bounded.
func appendHistory(meta *taskMetadata, entry Transition) {
	meta.History = append(meta.History, entry)
	if len(meta.History) > HistoryLimit {
		meta.History = meta.History[len(meta.History)-HistoryLimit:]
	}
}

// IsPick reports whether a history entry records a claim rather than a move:
// From and To are the same column. A task's first entry, From "", is its
// creation (Service.Create).
func (t Transition) IsPick() bool { return t.From != "" && t.From == t.To }

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
	task, board, err := s.claimable(caller, id, caller.SessionID, false)
	if err != nil {
		return Task{}, err
	}
	return s.writeClaim(task, board, caller.SessionID, caller.Author())
}

// Assignable says whether Assign would succeed now, and writes nothing. A
// parent asks before it starts a child, so a task that cannot be given away
// does not leave a session behind.
func (s *Service) Assignable(caller Caller, id string) (Task, error) {
	if !caller.IsAgent() {
		return Task{}, ErrInvalid
	}
	// The holder does not exist yet: "" is any session but the caller's own,
	// which is who a task already held may be taken from.
	task, _, err := s.claimable(caller, id, "", true)
	return task, err
}

// Assign is Claim on another session's behalf: an agent that starts a child
// for a task gives it the task, so the child does not have to take it, and
// the board shows the child as the holder from its first moment. The caller's
// scope decides what it may give (a task of a board it cannot see is not
// found); the holder is any session id, because the caller has just made it.
//
// Unlike Claim it never reopens: a finished or archived task is not given to
// anyone (ErrNotAssignable). A task the caller itself holds can be handed over;
// one a different live session holds cannot, as with Claim.
func (s *Service) Assign(caller Caller, id, sessionID string) (Task, error) {
	if !caller.IsAgent() || sessionID == "" || sessionID == caller.SessionID {
		return Task{}, ErrInvalid
	}
	task, board, err := s.claimable(caller, id, sessionID, true)
	if err != nil {
		return Task{}, err
	}
	return s.writeClaim(task, board, sessionID, Caller{SessionID: sessionID}.Author())
}

// claimable reads the task and applies the rules a claim by holder is subject
// to. handOver is an assignment by another session: it adds the refusals
// that Claim, which has always reopened a finished task, does not make.
func (s *Service) claimable(caller Caller, id, holder string, handOver bool) (Task, Board, error) {
	task, err := s.get(id)
	if err != nil {
		return Task{}, Board{}, err
	}
	// The board first: ErrClaimed on a task out of scope would say it exists.
	board, err := s.boardFor(caller, task.BoardID)
	if err != nil {
		return Task{}, Board{}, err
	}
	if handOver {
		if task.ArchivedAt != "" {
			return Task{}, Board{}, ErrNotAssignable
		}
		if category, ok := board.Workflow.CategoryOf(task.Status); ok && category == CategoryTerminal {
			return Task{}, Board{}, ErrNotAssignable
		}
	}
	if task.SessionID != "" && task.SessionID != holder && (!handOver || task.SessionID != caller.SessionID) {
		// A claim held by a session that has died can be taken over: holding
		// is so the agent can reconnect to its own work, not a lock forever.
		if s.live == nil || s.live.Alive(task.SessionID) {
			return Task{}, Board{}, ErrClaimed
		}
	}
	return task, board, nil
}

// writeClaim records holder on the task. author is who the history names.
func (s *Service) writeClaim(task Task, board Board, holder, author string) (Task, error) {
	active, ok := board.Workflow.FirstOf(CategoryActive)
	if !ok {
		return Task{}, ErrInvalid
	}
	// One write: the holder, the move into the first active column when the
	// task is not in one, and — when this session did not already hold it — a
	// pick in the history: an entry whose From and To are the same column.
	// That is what keeps a task in the list of the session that picked it up
	// after it lets go or another takes it over, beside the session that made
	// it (Agent Hub's tray). A shape every version of the history already
	// reads, not a new field the strict decoder would refuse on an older one.
	at := s.now().UTC().Format(time.RFC3339)
	picked := task.SessionID != holder
	return s.mutate(task.ID, func(meta *taskMetadata) error {
		meta.SessionID = holder
		if meta.Status != active {
			appendHistory(meta, Transition{From: meta.Status, To: active, By: author, At: at})
			meta.Status = active
		}
		if picked {
			appendHistory(meta, Transition{From: active, To: active, By: author, At: at})
		}
		return nil
	})
}

// Release drops a claim without moving the task. A dead session's claim is
// deliberately NOT cleared automatically: the agent reopening reclaims its own
// work, and a live claim can take a dead one over anyway.
func (s *Service) Release(caller Caller, id string) (Task, error) {
	return s.mutateAs(caller, id, func(meta *taskMetadata) error {
		meta.SessionID = ""
		return nil
	})
}

// ---- comments --------------------------------------------------------

// AddComment writes a comment. Authorship comes from the caller, never from a
// parameter: an agent cannot sign as the user.
func (s *Service) AddComment(caller Caller, taskID, body string) (Comment, error) {
	if s.scoped(caller) {
		if _, err := s.taskFor(caller, taskID); err != nil {
			return Comment{}, err
		}
	}
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
	taskRecord, err := s.items.Get(taskID)
	if err != nil {
		return Comment{}, ErrNotFound
	}
	// Best effort: a task whose metadata does not decode still gets its
	// comment written (unchanged from before this notification existed), just
	// with no board to name in the notification.
	var taskMeta taskMetadata
	_ = decodeInto(taskRecord.Metadata, &taskMeta)
	meta := commentMetadata{TaskID: taskID, Author: author, Kind: string(kind)}
	raw, err := json.Marshal(meta)
	if err != nil {
		return Comment{}, err
	}
	record, err := s.items.Create(vaultitem.ItemTypeTaskComment, KeyIDComment, raw, []byte(body))
	if err != nil {
		return Comment{}, err
	}
	s.changed(taskMeta.BoardID, taskID)
	return Comment{
		ID: record.ID, TaskID: taskID, Author: author, Kind: kind,
		Body: body, CreatedAt: record.CreatedAt,
	}, nil
}

// Comments returns a task's comments. Human and agent comments are the
// conversation; system ones are the Activity tab, which is why the kind is on
// every row rather than inferred from the author.
func (s *Service) Comments(caller Caller, taskID string) ([]Comment, error) {
	if s.scoped(caller) {
		if _, err := s.taskFor(caller, taskID); err != nil {
			return nil, err
		}
	}
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

func (s *Service) Progress(caller Caller, parentID string) (Progress, error) {
	parent, err := s.get(parentID)
	if err != nil {
		return Progress{}, err
	}
	board, err := s.boardFor(caller, parent.BoardID)
	if err != nil {
		return Progress{}, err
	}
	children, err := s.list(ListInput{BoardID: parent.BoardID, ParentID: parentID})
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
