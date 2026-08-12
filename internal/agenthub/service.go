package agenthub

import (
	"context"
	"encoding/json"
	"errors"
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

// InvokeInput is a blocking prompt against an existing hub session.
type InvokeInput struct {
	SessionID       string
	Prompt          string
	CallerSessionID string // optional; used for CheckCycle on cross-session invoke
}

// InvokeResult is the MCP/UI-facing outcome of a blocking Invoke.
type InvokeResult struct {
	Status       string   `json:"status"` // completed | cancelled | error
	Summary      string   `json:"summary"`
	FilesChanged []string `json:"filesChanged"`
	TurnCount    int      `json:"turnCount"`
	StopReason   string   `json:"stopReason"`
}

// Service orchestrates vendor adapters, session persistence, and live runtimes.
type Service struct {
	reg               *Registry
	store             Store
	runtimes          map[string]SessionRuntime
	invoking          map[string]struct{}
	mu                sync.Mutex
	emit              func(sessionID string, update json.RawMessage)
	ResolvePermission PermissionResolver // nil = fail closed (no auto-approve)
	PermissionTimeout time.Duration      // 0 = interaction.DefaultTimeout (60s)
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
		invoking: make(map[string]struct{}),
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

// ListVendorTypes returns registered adapter types in stable order.
func (s *Service) ListVendorTypes() []AgentType {
	return s.reg.Types()
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

// Invoke runs a blocking prompt turn on an existing session.
func (s *Service) Invoke(ctx context.Context, in InvokeInput) (InvokeResult, error) {
	sess, err := s.store.GetSession(ctx, in.SessionID)
	if err != nil {
		return InvokeResult{}, err
	}
	if sess.Status == SessionStatusClosed {
		return InvokeResult{}, ErrSessionNotFound
	}
	if sess.Status == SessionStatusRunning || sess.Status == SessionStatusAwaitingPermission {
		return InvokeResult{}, ErrSessionBusy
	}

	if in.CallerSessionID != "" {
		list, err := s.store.ListSessions(ctx)
		if err != nil {
			return InvokeResult{}, err
		}
		byID := make(map[string]AgentSession, len(list))
		for _, item := range list {
			byID[item.ID] = item
		}
		if err := CheckCycle(byID, in.CallerSessionID, in.SessionID); err != nil {
			return InvokeResult{}, err
		}
	}

	s.mu.Lock()
	if _, busy := s.invoking[in.SessionID]; busy {
		s.mu.Unlock()
		return InvokeResult{}, ErrSessionBusy
	}
	rt := s.runtimes[in.SessionID]
	if rt == nil {
		s.mu.Unlock()
		return InvokeResult{}, ErrUnavailable
	}
	s.invoking[in.SessionID] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.invoking, in.SessionID)
		s.mu.Unlock()
	}()

	adapter, ok := s.reg.Get(sess.AgentType)
	if !ok {
		return InvokeResult{}, ErrVendorUnknown
	}

	if err := s.setSessionStatus(ctx, in.SessionID, SessionStatusRunning); err != nil {
		return InvokeResult{}, err
	}

	var lastUpdate json.RawMessage
	rt.SetHandlers(RuntimeHandlers{
		OnUpdate: func(update json.RawMessage, meta json.RawMessage) {
			_ = meta
			lastUpdate = append(json.RawMessage(nil), update...)
			s.emit(in.SessionID, update)
		},
		OnPermission: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			return s.mediatePermission(ctx, in.SessionID, params)
		},
	})

	outcome, promptErr := rt.Prompt(ctx, in.Prompt)
	if promptErr != nil {
		finalStatus := SessionStatusError
		if errors.Is(promptErr, ErrCancelled) || errors.Is(promptErr, context.Canceled) {
			finalStatus = SessionStatusIdle
			promptErr = ErrCancelled
		}
		_ = s.setSessionStatus(ctx, in.SessionID, finalStatus)
		return InvokeResult{Status: statusForInvokeErr(promptErr)}, promptErr
	}

	usage, err := adapter.NormalizeUsage(outcome.Meta)
	if err != nil {
		_ = s.setSessionStatus(ctx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}
	files, summary, err := adapter.NormalizeToolSummary(outcome.Meta, lastUpdate)
	if err != nil {
		_ = s.setSessionStatus(ctx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}
	if summary == "" {
		summary = "turn completed"
	}

	usageJSON, err := json.Marshal(usage)
	if err != nil {
		_ = s.setSessionStatus(ctx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}

	turn := TurnRecord{
		ID:         uuid.NewString(),
		SessionID:  in.SessionID,
		StopReason: outcome.StopReason,
		UsageJSON:  usageJSON,
		RawMeta:    append(json.RawMessage(nil), outcome.Meta...),
		CreatedAt:  time.Now().UTC(),
	}
	if err := s.store.InsertTurn(ctx, turn); err != nil {
		_ = s.setSessionStatus(ctx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}

	turns, err := s.store.ListTurns(ctx, in.SessionID)
	if err != nil {
		_ = s.setSessionStatus(ctx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, err
	}

	if err := s.setSessionStatus(ctx, in.SessionID, SessionStatusIdle); err != nil {
		return InvokeResult{Status: "error"}, err
	}

	return InvokeResult{
		Status:       "completed",
		Summary:      summary,
		FilesChanged: files,
		TurnCount:    len(turns),
		StopReason:   outcome.StopReason,
	}, nil
}

// Cancel requests cancellation of the in-flight prompt on a session.
func (s *Service) Cancel(ctx context.Context, id string) error {
	if _, err := s.store.GetSession(ctx, id); err != nil {
		return err
	}
	s.mu.Lock()
	rt := s.runtimes[id]
	s.mu.Unlock()
	if rt == nil {
		return ErrUnavailable
	}
	return rt.Cancel(ctx)
}

func (s *Service) setSessionStatus(ctx context.Context, id string, status SessionStatus) error {
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	sess.Status = status
	sess.UpdatedAt = time.Now().UTC()
	return s.store.UpdateSession(ctx, sess)
}

func statusForInvokeErr(err error) string {
	if errors.Is(err, ErrCancelled) {
		return "cancelled"
	}
	return "error"
}
