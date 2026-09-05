package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devkit/internal/acp"
	"devkit/internal/identity"

	"github.com/google/uuid"
)

// DefaultInvokeTimeout bounds Invoke when the caller context has no deadline.
// Overridable in tests.
var DefaultInvokeTimeout = 10 * time.Minute

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

// MCPEndpoint is the identity-bound MCP socket handed to one vendor process.
type MCPEndpoint struct {
	SocketPath string
	Server     acp.McpServerStdio
	Close      func()
}

// MCPEndpoints opens one MCPEndpoint per session. nil means sessions get no
// DevKit MCP server (tests only; production wires mcp.SessionHost).
type MCPEndpoints interface {
	Open(sessionID string) (MCPEndpoint, error)
}

// Service orchestrates vendor adapters, session persistence, and live runtimes.
type Service struct {
	reg               *Registry
	store             Store
	runtimes          map[string]SessionRuntime
	endpoints         map[string]func() // per-session MCP socket close
	invoking          map[string]struct{}
	mu                sync.Mutex
	emit              func(sessionID string, update json.RawMessage)
	ResolvePermission PermissionResolver // nil = fail closed (no auto-approve)
	PermissionTimeout time.Duration      // 0 = interaction.DefaultTimeout (60s)
	// MCPEndpoints opens the per-session MCP socket before the vendor spawns.
	MCPEndpoints MCPEndpoints
	// OnSessionClosed fires after a session is closed (interaction.Hub forgets its allow_always).
	OnSessionClosed func(sessionID string)
}

// NewService wires registry + store. emit may be nil (no-op).
func NewService(reg *Registry, store Store, emit func(sessionID string, update json.RawMessage)) *Service {
	if emit == nil {
		emit = func(string, json.RawMessage) {}
	}
	return &Service{
		reg:       reg,
		store:     store,
		runtimes:  make(map[string]SessionRuntime),
		endpoints: make(map[string]func()),
		invoking:  make(map[string]struct{}),
		emit:      emit,
	}
}

// CreateSession opens a vendor runtime and persists an idle hub session.
//
// Order matters: the session id is generated first because the MCP socket is
// named after it and the sandbox must know that path before the vendor spawns.
func (s *Service) CreateSession(ctx context.Context, in CreateInput) (AgentSession, error) {
	adapter, ok := s.reg.Get(in.AgentType)
	if !ok {
		return AgentSession{}, ErrVendorUnknown
	}

	caller, _ := identity.FromContext(ctx)
	parentID := in.ParentSessionID
	if caller.IsAgent() {
		// An agent never chooses its parent: the parent is the caller.
		if parentID != "" && parentID != caller.SessionID {
			return AgentSession{}, ErrCallerMismatch
		}
		parentID = caller.SessionID
	}

	depth := 0
	var parent *AgentSession
	if parentID != "" {
		p, err := s.store.GetSession(ctx, parentID)
		if err != nil {
			return AgentSession{}, err
		}
		if err := CheckDepth(p.Depth, MaxDepth); err != nil {
			return AgentSession{}, err
		}
		depth = p.Depth + 1
		parent = &p
	}

	cwd, err := resolveCwd(in.Cwd, parent, caller.IsAgent())
	if err != nil {
		return AgentSession{}, err
	}

	id := uuid.NewString()
	spec := OpenSpec{Cwd: cwd}
	closeEndpoint := func() {}
	if s.MCPEndpoints != nil {
		ep, err := s.MCPEndpoints.Open(id)
		if err != nil {
			return AgentSession{}, err
		}
		closeEndpoint = ep.Close
		spec.AllowUnixSockets = []string{ep.SocketPath}
		spec.McpServers = []acp.McpServerStdio{ep.Server}
		spec.ReadOnlyPaths = []string{ep.Server.Command}
	}

	rt, err := adapter.Open(ctx, spec)
	if err != nil {
		closeEndpoint()
		return AgentSession{}, err
	}

	now := time.Now().UTC()
	sess := AgentSession{
		ID:           id,
		AgentType:    in.AgentType,
		ProjectPath:  cwd,
		Status:       SessionStatusIdle,
		Depth:        depth,
		Title:        in.Title,
		ACPSessionID: rt.ACPSessionID(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if parentID != "" {
		pid := parentID
		sess.ParentSessionID = &pid
	}

	if err := s.store.InsertSession(ctx, sess); err != nil {
		_ = rt.Close(ctx)
		closeEndpoint()
		return AgentSession{}, err
	}

	s.mu.Lock()
	s.runtimes[sess.ID] = rt
	s.endpoints[sess.ID] = closeEndpoint
	s.mu.Unlock()

	return sess, nil
}

// resolveCwd applies the cwd rules. Nobody gets "/" or the home directory.
// An agent caller with no cwd inherits its parent's project; with one, it
// must stay inside the parent's project (symlinks resolved on both sides).
func resolveCwd(cwd string, parent *AgentSession, agentCaller bool) (string, error) {
	if agentCaller && cwd == "" && parent != nil {
		cwd = parent.ProjectPath
	}
	if cwd == "" || !filepath.IsAbs(cwd) {
		return "", ErrInvalidCwd
	}
	clean := filepath.Clean(cwd)
	if clean == "/" {
		return "", ErrInvalidCwd
	}
	if home, err := os.UserHomeDir(); err == nil && clean == filepath.Clean(home) {
		return "", ErrInvalidCwd
	}
	if agentCaller && parent != nil {
		resolved, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return "", ErrInvalidCwd
		}
		parentResolved, err := filepath.EvalSymlinks(parent.ProjectPath)
		if err != nil {
			return "", ErrInvalidCwd
		}
		rel, err := filepath.Rel(parentResolved, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", ErrCwdOutsideParent
		}
	}
	return clean, nil
}

// sessionsByID loads every session keyed by id (for ancestry walks).
func (s *Service) sessionsByID(ctx context.Context) (map[string]AgentSession, error) {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]AgentSession, len(list))
	for _, item := range list {
		byID[item.ID] = item
	}
	return byID, nil
}

// authorizeTarget enforces the descendant rule: an agent caller may only act
// on itself or sessions inside its subtree. UI and terminal callers pass.
func (s *Service) authorizeTarget(ctx context.Context, targetID string) error {
	caller, _ := identity.FromContext(ctx)
	if !caller.IsAgent() {
		return nil
	}
	if targetID == caller.SessionID {
		return nil
	}
	byID, err := s.sessionsByID(ctx)
	if err != nil {
		return err
	}
	if ancestorContains(byID, targetID, caller.SessionID) {
		return nil
	}
	return ErrNotDescendant
}

// ListSessions returns all persisted hub sessions visible to the caller.
// Agents only see themselves and their descendants.
//
// Note: after a process restart, store rows may outlive in-memory runtimes
// (orphan sessions). Invoke/Cancel return ErrUnavailable for those; callers
// should not assume every listed session has a live runtime.
func (s *Service) ListSessions(ctx context.Context) ([]AgentSession, error) {
	list, err := s.store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	caller, _ := identity.FromContext(ctx)
	if !caller.IsAgent() {
		return list, nil
	}
	byID := make(map[string]AgentSession, len(list))
	for _, item := range list {
		byID[item.ID] = item
	}
	out := make([]AgentSession, 0, len(list))
	for _, item := range list {
		if item.ID == caller.SessionID || ancestorContains(byID, item.ID, caller.SessionID) {
			out = append(out, item)
		}
	}
	return out, nil
}

// ListVendorTypes returns registered adapter types in stable order.
func (s *Service) ListVendorTypes() []AgentType {
	return s.reg.Types()
}

// GetStatus returns the persisted session record by id.
func (s *Service) GetStatus(ctx context.Context, id string) (AgentSession, error) {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return AgentSession{}, err
	}
	return s.store.GetSession(ctx, id)
}

// CloseSession closes the live runtime (if any) and marks the session closed.
func (s *Service) CloseSession(ctx context.Context, id string) error {
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}

	s.mu.Lock()
	rt := s.runtimes[id]
	delete(s.runtimes, id)
	closeEndpoint := s.endpoints[id]
	delete(s.endpoints, id)
	s.mu.Unlock()

	if rt != nil {
		if err := rt.Close(ctx); err != nil {
			return err
		}
	}
	if closeEndpoint != nil {
		closeEndpoint()
	}

	sess.Status = SessionStatusClosed
	sess.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return err
	}
	if s.OnSessionClosed != nil {
		s.OnSessionClosed(id)
	}
	return nil
}

// Invoke runs a blocking prompt turn on an existing session.
func (s *Service) Invoke(ctx context.Context, in InvokeInput) (InvokeResult, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && DefaultInvokeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultInvokeTimeout)
		defer cancel()
	}

	sess, err := s.store.GetSession(ctx, in.SessionID)
	if err != nil {
		return InvokeResult{}, err
	}
	if err := s.authorizeTarget(ctx, in.SessionID); err != nil {
		return InvokeResult{}, err
	}
	if caller, _ := identity.FromContext(ctx); caller.IsAgent() {
		if in.CallerSessionID != "" && in.CallerSessionID != caller.SessionID {
			return InvokeResult{}, ErrCallerMismatch
		}
		in.CallerSessionID = caller.SessionID // CheckCycle below runs on the real caller
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

	var (
		lastUpdateMu sync.Mutex
		lastUpdate   json.RawMessage
		permDenied   atomic.Bool
	)
	rt.SetHandlers(RuntimeHandlers{
		OnUpdate: func(update json.RawMessage, meta json.RawMessage) {
			_ = meta
			lastUpdateMu.Lock()
			lastUpdate = append(json.RawMessage(nil), update...)
			lastUpdateMu.Unlock()
			s.emit(in.SessionID, update)
		},
		OnPermission: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			outcome, err := s.mediatePermission(ctx, in.SessionID, params)
			if err != nil || isDeniedPermissionOutcome(outcome) {
				permDenied.Store(true)
				if len(outcome) == 0 {
					outcome = append(json.RawMessage(nil), deniedPermissionOutcome...)
				}
				return outcome, ErrPermissionDenied
			}
			return outcome, nil
		},
	})

	outcome, promptErr := rt.Prompt(ctx, in.Prompt)
	if permDenied.Load() {
		_ = s.setSessionStatus(ctx, in.SessionID, SessionStatusError)
		return InvokeResult{Status: "error"}, ErrPermissionDenied
	}
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
	lastUpdateMu.Lock()
	updateCopy := append(json.RawMessage(nil), lastUpdate...)
	lastUpdateMu.Unlock()
	files, summary, err := adapter.NormalizeToolSummary(outcome.Meta, updateCopy)
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
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
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
