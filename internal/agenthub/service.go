package agenthub

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
)

// CreateInput is the request to open a new agent session.
type CreateInput struct {
	AgentType       AgentType
	Cwd             string
	ParentSessionID string // empty = root session
	Title           *string
}

// Service orchestrates vendor adapters, session persistence, and live runtimes.
type Service struct {
	reg      *Registry
	store    Store
	runtimes map[string]SessionRuntime
	mu       sync.Mutex
	emit     func(sessionID string, update json.RawMessage)
}

// NewService wires registry + store. emit may be nil (no-op).
func NewService(reg *Registry, store Store, emit func(sessionID string, update json.RawMessage)) *Service {
	if emit == nil {
		emit = func(string, json.RawMessage) {}
	}
	return &Service{
		reg:      reg,
		store:    store,
		runtimes: make(map[string]SessionRuntime),
		emit:     emit,
	}
}

// CreateSession opens a vendor runtime and persists an idle hub session.
func (s *Service) CreateSession(ctx context.Context, in CreateInput) (AgentSession, error) {
	adapter, ok := s.reg.Get(in.AgentType)
	if !ok {
		return AgentSession{}, ErrVendorUnknown
	}

	depth := 0
	var parentID *string
	if in.ParentSessionID != "" {
		parent, err := s.store.GetSession(ctx, in.ParentSessionID)
		if err != nil {
			return AgentSession{}, err
		}
		if err := CheckDepth(parent.Depth, MaxDepth); err != nil {
			return AgentSession{}, err
		}
		depth = parent.Depth + 1
		pid := in.ParentSessionID
		parentID = &pid
	}

	rt, err := adapter.Open(ctx, in.Cwd)
	if err != nil {
		return AgentSession{}, err
	}

	now := time.Now().UTC()
	sess := AgentSession{
		ID:              uuid.NewString(),
		AgentType:       in.AgentType,
		ProjectPath:     in.Cwd,
		Status:          SessionStatusIdle,
		ParentSessionID: parentID,
		Depth:           depth,
		Title:           in.Title,
		ACPSessionID:    rt.ACPSessionID(),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.store.InsertSession(ctx, sess); err != nil {
		_ = rt.Close(ctx)
		return AgentSession{}, err
	}

	s.mu.Lock()
	s.runtimes[sess.ID] = rt
	s.mu.Unlock()

	return sess, nil
}

// ListSessions returns all persisted hub sessions.
func (s *Service) ListSessions(ctx context.Context) ([]AgentSession, error) {
	return s.store.ListSessions(ctx)
}

// GetStatus returns the persisted session record by id.
func (s *Service) GetStatus(ctx context.Context, id string) (AgentSession, error) {
	return s.store.GetSession(ctx, id)
}

// CloseSession closes the live runtime (if any) and marks the session closed.
func (s *Service) CloseSession(ctx context.Context, id string) error {
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	rt := s.runtimes[id]
	delete(s.runtimes, id)
	s.mu.Unlock()

	if rt != nil {
		if err := rt.Close(ctx); err != nil {
			return err
		}
	}

	sess.Status = SessionStatusClosed
	sess.UpdatedAt = time.Now().UTC()
	return s.store.UpdateSession(ctx, sess)
}
