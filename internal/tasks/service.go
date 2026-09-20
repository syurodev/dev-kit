package tasks

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"devkit/internal/vaultitem"
)

// ItemStore is the vault-item surface, declared locally so this package
// accepts an interface like every other module on the substrate.
type ItemStore interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	ListByTypeAndMetaField(itemType, field, value string) ([]vaultitem.Record, error)
	UpdateMetadata(id string, metadata json.RawMessage) (vaultitem.Record, error)
	Update(id, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	Delete(id string) error
}

// Caller is who is asking. The service stamps authorship from this rather
// than from a parameter: an agent must not be able to claim it is the user.
type Caller struct {
	// SessionID is set for an agent; empty for the user.
	SessionID string
}

// Author is the stable label for comments and history.
func (c Caller) Author() string {
	if c.SessionID != "" {
		return "agent:" + c.SessionID
	}
	return "user"
}

// IsAgent reports whether the caller is an agent session.
func (c Caller) IsAgent() bool { return c.SessionID != "" }

// SessionLiveness reports whether an agent session is still running. A claim
// held by a dead session can be taken over; holding is not locking forever.
type SessionLiveness interface {
	Alive(sessionID string) bool
}

// Service is the module.
type Service struct {
	items ItemStore
	live  SessionLiveness
	gate  *verifyGate
	now   func() time.Time
}

func NewService(items ItemStore) *Service {
	return &Service{items: items, now: time.Now}
}

// SetLiveness wires the agent-session check used by claim takeover.
func (s *Service) SetLiveness(l SessionLiveness) { s.live = l }

// SetGate wires the close gate. Nil means every terminal transition from an
// agent is refused, which is the correct headless answer.
func (s *Service) SetGate(g *verifyGate) {
	s.gate = g
	if g != nil {
		g.svc = s
	}
}

// Gate exposes the queue for the bridge (run history, cancel, recovery).
func (s *Service) Gate() *verifyGate { return s.gate }

// ---- boards ----------------------------------------------------------

// CreateBoard makes a board. Workflow must be runnable; a board you cannot
// start or finish work on is not a board.
func (s *Service) CreateBoard(name, keyPrefix, color string, workflow Workflow) (Board, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(keyPrefix) == "" {
		return Board{}, ErrInvalid
	}
	if len(workflow.Statuses) == 0 {
		workflow = DefaultWorkflow()
	}
	if !workflow.Valid() {
		return Board{}, ErrInvalid
	}
	meta := boardMetadata{Name: name, KeyPrefix: strings.ToUpper(keyPrefix), Color: color, Workflow: workflow}
	raw, err := json.Marshal(meta)
	if err != nil {
		return Board{}, err
	}
	record, err := s.items.Create(vaultitem.ItemTypeTaskBoard, KeyIDBoard, raw, []byte{})
	if err != nil {
		return Board{}, err
	}
	return meta.board(record.ID), nil
}

// UpdateBoard changes a board, including its workflow and verify command.
// This is tasks:admin and never reachable from MCP: changing the workflow is
// changing the rule that binds the agent, and the verify command is what
// closing a task runs.
func (s *Service) UpdateBoard(id string, apply func(*Board)) (Board, error) {
	record, err := s.items.Get(id)
	if err != nil {
		return Board{}, ErrNotFound
	}
	var meta boardMetadata
	if err := decodeInto(record.Metadata, &meta); err != nil {
		return Board{}, err
	}
	board := meta.board(id)
	apply(&board)
	if !board.Workflow.Valid() || board.Name == "" || board.KeyPrefix == "" {
		return Board{}, ErrInvalid
	}
	updated := boardMetadata{
		Name: board.Name, KeyPrefix: board.KeyPrefix, Color: board.Color,
		Workflow: board.Workflow, Verify: board.Verify,
		BranchTemplate: board.BranchTemplate, ProjectPath: board.ProjectPath,
	}
	raw, err := json.Marshal(updated)
	if err != nil {
		return Board{}, err
	}
	if _, err := s.items.UpdateMetadata(id, raw); err != nil {
		return Board{}, err
	}
	return updated.board(id), nil
}

func (m boardMetadata) board(id string) Board {
	return Board{
		ID: id, Name: m.Name, KeyPrefix: m.KeyPrefix, Color: m.Color,
		Workflow: m.Workflow, Verify: m.Verify,
		BranchTemplate: m.BranchTemplate, ProjectPath: m.ProjectPath,
	}
}

// Boards lists every board.
func (s *Service) Boards() ([]Board, error) {
	records, err := s.items.ListByType(vaultitem.ItemTypeTaskBoard)
	if err != nil {
		return nil, err
	}
	out := make([]Board, 0, len(records))
	for _, record := range records {
		var meta boardMetadata
		if decodeInto(record.Metadata, &meta) != nil {
			continue
		}
		out = append(out, meta.board(record.ID))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Board returns one board.
func (s *Service) Board(id string) (Board, error) {
	record, err := s.items.Get(id)
	if err != nil {
		return Board{}, ErrNotFound
	}
	var meta boardMetadata
	if err := decodeInto(record.Metadata, &meta); err != nil {
		return Board{}, err
	}
	return meta.board(record.ID), nil
}

// DeleteBoard removes an empty board. A board with live tasks refuses: the
// tasks are the project's record, not the board's decoration.
func (s *Service) DeleteBoard(id string) error {
	tasks, err := s.List(ListInput{BoardID: id, IncludeArchived: true})
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.ArchivedAt == "" {
			return ErrInvalid
		}
	}
	return s.items.Delete(id)
}

// ---- tasks -----------------------------------------------------------

// CreateInput is a new task.
type CreateInput struct {
	BoardID     string
	Title       string
	Description string
	Priority    string
	Labels      []string
	Assignee    string
	ParentID    string
	PlanNodeID  string
}

// Create makes a task, including a subtask of the one an agent is working on.
func (s *Service) Create(caller Caller, in CreateInput) (Task, error) {
	board, err := s.Board(in.BoardID)
	if err != nil {
		return Task{}, err
	}
	if strings.TrimSpace(in.Title) == "" {
		return Task{}, ErrInvalid
	}
	if in.ParentID != "" {
		parent, err := s.get(in.ParentID)
		if err != nil {
			return Task{}, err
		}
		// One level is one level: a subtask of a subtask would make the
		// checklist a tree, which the board does not render and the pagination
		// does not bound.
		if parent.ParentID != "" {
			return Task{}, ErrParentHasParent
		}
	}
	first, ok := board.Workflow.FirstOf(CategoryOpen)
	if !ok {
		return Task{}, ErrInvalid
	}
	id, err := vaultitem.NewID()
	if err != nil {
		return Task{}, err
	}
	meta := taskMetadata{
		BoardID: in.BoardID, Key: Key(board.KeyPrefix, id), Title: in.Title,
		Status: first, Priority: in.Priority, Labels: in.Labels,
		Assignee: in.Assignee, ParentID: in.ParentID, PlanNodeID: in.PlanNodeID,
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return Task{}, err
	}
	record, err := s.items.Create(vaultitem.ItemTypeTask, KeyIDTask, raw, []byte(in.Description))
	if err != nil {
		return Task{}, err
	}
	// The key derives from the record id, which Create only just assigned, so
	// it is written back once rather than guessed beforehand.
	meta.Key = Key(board.KeyPrefix, record.ID)
	raw, err = json.Marshal(meta)
	if err != nil {
		return Task{}, err
	}
	updated, err := s.items.UpdateMetadata(record.ID, raw)
	if err != nil {
		return Task{}, err
	}
	return meta.task(record.ID, updated.CreatedAt, updated.UpdatedAt), nil
}

func (m taskMetadata) task(id string, created, updated time.Time) Task {
	return Task{
		ID: id, BoardID: m.BoardID, Key: m.Key, Title: m.Title, Status: m.Status,
		Priority: m.Priority, Labels: append([]string(nil), m.Labels...),
		Assignee: m.Assignee, ParentID: m.ParentID,
		BlockedBy: append([]string(nil), m.BlockedBy...), PlanNodeID: m.PlanNodeID,
		SessionID: m.SessionID, Branch: m.Branch, ArchivedAt: m.ArchivedAt,
		History:   append([]Transition(nil), m.History...),
		CreatedAt: created, UpdatedAt: updated,
	}
}

func (s *Service) get(id string) (Task, error) {
	record, err := s.items.Get(id)
	if err != nil {
		return Task{}, ErrNotFound
	}
	var meta taskMetadata
	if err := decodeInto(record.Metadata, &meta); err != nil {
		return Task{}, err
	}
	return meta.task(record.ID, record.CreatedAt, record.UpdatedAt), nil
}

// ListInput bounds a listing. BoardID is required: "every task in the vault"
// is not a question the board asks, and answering it would be unbounded.
type ListInput struct {
	BoardID         string
	Status          string
	Assignee        string
	Label           string
	ParentID        string
	IncludeArchived bool
	Limit           int
}

// List returns task summaries — no description, so a board of two thousand
// tasks is not two thousand decryptions.
func (s *Service) List(in ListInput) ([]Task, error) {
	if in.BoardID == "" {
		return nil, ErrInvalid
	}
	records, err := s.items.ListByTypeAndMetaField(vaultitem.ItemTypeTask, "board_id", in.BoardID)
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(records))
	for _, record := range records {
		var meta taskMetadata
		if decodeInto(record.Metadata, &meta) != nil {
			continue
		}
		task := meta.task(record.ID, record.CreatedAt, record.UpdatedAt)
		if !in.IncludeArchived && task.ArchivedAt != "" {
			continue
		}
		if in.Status != "" && task.Status != in.Status {
			continue
		}
		if in.Assignee != "" && task.Assignee != in.Assignee {
			continue
		}
		if in.ParentID != "" && task.ParentID != in.ParentID {
			continue
		}
		if in.Label != "" && !contains(task.Labels, in.Label) {
			continue
		}
		out = append(out, task)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if in.Limit > 0 && len(out) > in.Limit {
		out = out[:in.Limit]
	}
	return out, nil
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// Get returns one task with its description decrypted.
func (s *Service) Get(id string) (Task, error) {
	record, err := s.items.Get(id)
	if err != nil {
		return Task{}, ErrNotFound
	}
	var meta taskMetadata
	if err := decodeInto(record.Metadata, &meta); err != nil {
		return Task{}, err
	}
	task := meta.task(record.ID, record.CreatedAt, record.UpdatedAt)
	body, err := s.items.OpenPayload(record, KeyIDTask)
	if err != nil {
		// Locked vault: the caller gets the metadata half and a distinguishable
		// error, not a task with an empty description that reads like an empty
		// description.
		return task, err
	}
	task.Description = string(body)
	return task, nil
}

// Update changes title and description, leaving status, labels and assignee
// alone — those move through their own calls so a body edit cannot silently
// reopen a task.
func (s *Service) Update(id, title, description string) (Task, error) {
	record, err := s.items.Get(id)
	if err != nil {
		return Task{}, ErrNotFound
	}
	var meta taskMetadata
	if err := decodeInto(record.Metadata, &meta); err != nil {
		return Task{}, err
	}
	if strings.TrimSpace(title) == "" {
		return Task{}, ErrInvalid
	}
	meta.Title = title
	raw, err := json.Marshal(meta)
	if err != nil {
		return Task{}, err
	}
	updated, err := s.items.Update(id, KeyIDTask, raw, []byte(description))
	if err != nil {
		return Task{}, err
	}
	task := meta.task(id, updated.CreatedAt, updated.UpdatedAt)
	task.Description = description
	return task, nil
}

// Fields are the metadata-only changes. They never reseal the payload, so an
// agent updating progress is not decrypting and re-encrypting a description.
type Fields struct {
	Priority  *string
	Labels    *[]string
	Assignee  *string
	ParentID  *string
	BlockedBy *[]string
	Branch    *string
	BoardID   *string
}

// SetFields applies metadata-only changes.
func (s *Service) SetFields(id string, fields Fields) (Task, error) {
	return s.mutate(id, func(meta *taskMetadata) error {
		if fields.Priority != nil {
			meta.Priority = *fields.Priority
		}
		if fields.Labels != nil {
			meta.Labels = *fields.Labels
		}
		if fields.Assignee != nil {
			meta.Assignee = *fields.Assignee
		}
		if fields.ParentID != nil {
			if *fields.ParentID != "" {
				parent, err := s.get(*fields.ParentID)
				if err != nil {
					return err
				}
				if parent.ParentID != "" {
					return ErrParentHasParent
				}
				if parent.ID == id {
					return ErrInvalid
				}
			}
			meta.ParentID = *fields.ParentID
		}
		if fields.BlockedBy != nil {
			meta.BlockedBy = *fields.BlockedBy
		}
		if fields.Branch != nil {
			meta.Branch = *fields.Branch
		}
		if fields.BoardID != nil {
			meta.BoardID = *fields.BoardID
		}
		return nil
	})
}

// mutate reads, changes and writes a task's metadata.
func (s *Service) mutate(id string, apply func(*taskMetadata) error) (Task, error) {
	record, err := s.items.Get(id)
	if err != nil {
		return Task{}, ErrNotFound
	}
	var meta taskMetadata
	if err := decodeInto(record.Metadata, &meta); err != nil {
		return Task{}, err
	}
	if err := apply(&meta); err != nil {
		return Task{}, err
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return Task{}, err
	}
	updated, err := s.items.UpdateMetadata(id, raw)
	if err != nil {
		return Task{}, err
	}
	return meta.task(id, updated.CreatedAt, updated.UpdatedAt), nil
}

// Archive hides a task without destroying it. Deleting is a person's call and
// never reachable from MCP; archiving is recoverable, so it needs no gate.
func (s *Service) Archive(id string) (Task, error) {
	at := s.now().UTC().Format(time.RFC3339)
	return s.mutate(id, func(meta *taskMetadata) error {
		meta.ArchivedAt = at
		return nil
	})
}

func (s *Service) Unarchive(id string) (Task, error) {
	return s.mutate(id, func(meta *taskMetadata) error {
		meta.ArchivedAt = ""
		return nil
	})
}

// Delete removes a task and its comments. UI-only.
func (s *Service) Delete(ctx context.Context, id string) error {
	comments, err := s.items.ListByTypeAndMetaField(vaultitem.ItemTypeTaskComment, "task_id", id)
	if err != nil {
		return err
	}
	for _, comment := range comments {
		if err := s.items.Delete(comment.ID); err != nil {
			return err
		}
	}
	return s.items.Delete(id)
}
