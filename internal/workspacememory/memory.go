// Package workspacememory stores shared, machine-local workspace knowledge.
// It is plaintext like workspace instructions, outside the vault and sync.
package workspacememory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrNotFound          = errors.New("workspace memory not found")
	ErrConflict          = errors.New("workspace memory revision conflict")
	ErrInvalidInput      = errors.New("invalid workspace memory input")
	ErrWorkspaceNotFound = errors.New("workspace not found")
)

const MaxContextBytes = 8192

type Input struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Body    string `json:"body"`
}
type Source struct {
	Caller    string `json:"caller"`
	SessionID string `json:"sessionId"`
	AgentType string `json:"agentType"`
}
type Entry struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Body        string `json:"body"`
	Revision    int64  `json:"revision"`
	Deleted     bool   `json:"deleted"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	Source      Source `json:"source"`
}
type Summary struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Revision    int64  `json:"revision"`
	Deleted     bool   `json:"deleted"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	Source      Source `json:"source"`
}
type Page struct {
	Entries []Summary `json:"entries"`
	HasMore bool      `json:"hasMore"`
}
type Revision struct {
	Entry     Entry  `json:"entry"`
	Operation string `json:"operation"`
}

// Write atomically compares the current revision and saves an immutable snapshot.
// expected=0 inserts a new entry; a positive expected updates an existing entry.
type Repository interface {
	WorkspaceExists(context.Context, string) (bool, error)
	List(context.Context, string, string, int, int) (Page, error)
	Get(context.Context, string, string) (Entry, error)
	Write(context.Context, Entry, int64, string) error
	History(context.Context, string, string, int, int) ([]Revision, error)
	GetRevision(context.Context, string, string, int64) (Revision, error)
}
type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }
func (s *Service) workspace(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return ErrWorkspaceNotFound
	}
	ok, err := s.repo.WorkspaceExists(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrWorkspaceNotFound
	}
	return nil
}
func textValid(v string, max int, required bool) bool {
	return utf8.ValidString(v) && len(v) <= max && (!required || strings.TrimSpace(v) != "")
}
func validate(in Input) error {
	switch in.Kind {
	case "decision", "convention", "knowledge", "handoff":
	default:
		return ErrInvalidInput
	}
	if !textValid(in.Title, 256, true) || !textValid(in.Summary, 1024, true) || !textValid(in.Body, 32768, true) {
		return ErrInvalidInput
	}
	return nil
}
func pageBounds(limit, offset int) (int, error) {
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return 0, ErrInvalidInput
	}
	return limit, nil
}
func (s *Service) List(ctx context.Context, ws, q string, limit, offset int) (Page, error) {
	limit, err := pageBounds(limit, offset)
	if err != nil {
		return Page{}, err
	}
	if !textValid(q, 256, false) {
		return Page{}, ErrInvalidInput
	}
	if err = s.workspace(ctx, ws); err != nil {
		return Page{}, err
	}
	return s.repo.List(ctx, ws, q, limit, offset)
}
func (s *Service) Get(ctx context.Context, ws, id string) (Entry, error) {
	if err := s.workspace(ctx, ws); err != nil {
		return Entry{}, err
	}
	return s.repo.Get(ctx, ws, id)
}
func (s *Service) Create(ctx context.Context, ws string, in Input, source Source) (Entry, error) {
	if err := validate(in); err != nil {
		return Entry{}, err
	}
	if err := s.workspace(ctx, ws); err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	e := Entry{ID: uuid.NewString(), WorkspaceID: ws, Kind: in.Kind, Title: in.Title, Summary: in.Summary, Body: in.Body, Revision: 1, CreatedAt: now, UpdatedAt: now, Source: source}
	if err := s.repo.Write(ctx, e, 0, "create"); err != nil {
		return Entry{}, err
	}
	return e, nil
}
func (s *Service) mutate(ctx context.Context, ws, id string, expected int64, in *Input, source Source, op string, target int64) (Entry, error) {
	if expected <= 0 {
		return Entry{}, ErrInvalidInput
	}
	if in != nil {
		if err := validate(*in); err != nil {
			return Entry{}, err
		}
	}
	e, err := s.Get(ctx, ws, id)
	if err != nil {
		return Entry{}, err
	}
	if e.Revision != expected {
		return Entry{}, ErrConflict
	}
	switch op {
	case "update":
		if e.Deleted {
			return Entry{}, ErrNotFound
		}
		e.Kind = in.Kind
		e.Title = in.Title
		e.Summary = in.Summary
		e.Body = in.Body
	case "delete":
		if e.Deleted {
			return Entry{}, ErrNotFound
		}
		e.Deleted = true
	case "restore":
		if target <= 0 {
			return Entry{}, ErrInvalidInput
		}
		rev, err := s.repo.GetRevision(ctx, ws, id, target)
		if err != nil {
			return Entry{}, err
		}
		if rev.Entry.Deleted {
			return Entry{}, ErrInvalidInput
		}
		old := rev.Entry
		e.Kind, e.Title, e.Summary, e.Body = old.Kind, old.Title, old.Summary, old.Body
		e.Deleted = false
	}
	e.Revision++
	e.UpdatedAt = time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	e.Source = source
	if err = s.repo.Write(ctx, e, expected, op); err != nil {
		return Entry{}, err
	}
	return e, nil
}
func (s *Service) Update(ctx context.Context, ws, id string, expected int64, in Input, source Source) (Entry, error) {
	return s.mutate(ctx, ws, id, expected, &in, source, "update", 0)
}
func (s *Service) Delete(ctx context.Context, ws, id string, expected int64, source Source) (Entry, error) {
	return s.mutate(ctx, ws, id, expected, nil, source, "delete", 0)
}
func (s *Service) Restore(ctx context.Context, ws, id string, expected, target int64, source Source) (Entry, error) {
	return s.mutate(ctx, ws, id, expected, nil, source, "restore", target)
}
func (s *Service) History(ctx context.Context, ws, id string, limit, offset int) ([]Revision, error) {
	limit, err := pageBounds(limit, offset)
	if err != nil {
		return nil, err
	}
	if _, err = s.Get(ctx, ws, id); err != nil {
		return nil, err
	}
	return s.repo.History(ctx, ws, id, limit, offset)
}
func (s *Service) Context(ctx context.Context, ws string) (string, error) {
	page, err := s.List(ctx, ws, "", 100, 0)
	if err != nil {
		return "", err
	}
	type item struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Title    string `json:"title"`
		Summary  string `json:"summary"`
		Revision int64  `json:"revision"`
	}
	payload := struct {
		Entries []item `json:"entries"`
		Omitted bool   `json:"omitted"`
	}{Entries: make([]item, 0), Omitted: page.HasMore}
	for _, e := range page.Entries {
		payload.Entries = append(payload.Entries, item{e.ID, e.Kind, e.Title, e.Summary, e.Revision})
		b, _ := json.Marshal(payload)
		if len(b) > MaxContextBytes {
			payload.Entries = payload.Entries[:len(payload.Entries)-1]
			payload.Omitted = true
			break
		}
	}
	b, _ := json.Marshal(payload)
	return string(b), nil
}
