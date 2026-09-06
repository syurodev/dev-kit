package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"devkit/internal/interaction"
	"devkit/internal/vaultitem"
)

// PermissionResolver mediates ACP session/request_permission against UI / interaction.
// nil means fail-closed deny (headless / no prompter).
type PermissionResolver func(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error)

// deniedPermissionOutcome is the ACP reject response when permission is denied
// or times out (fail closed — never auto-approve).
var deniedPermissionOutcome = json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`)

// PendingPermission is a permission prompt awaiting UI resolution.
type PendingPermission struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	Params    json.RawMessage `json:"params"`
	CreatedAt time.Time       `json:"createdAt"`
	// ExpiresAt is when the gate closes on its own. It ships with the request
	// because the UI cannot infer it: a toast may mount long after the request
	// arrived — the rail was closed, the user was in another module, the lazy
	// chunk was still loading — and a countdown started at mount would promise
	// time that no longer exists.
	ExpiresAt time.Time `json:"expiresAt"`
}

// PermissionWaiter blocks ACP session/request_permission until Complete or timeout.
//
// Dedicated waiter (not interaction.Hub): ACP permission option lists
// (allow_once / reject_once / allow_always / …) do not map cleanly onto
// interaction's select/confirm kinds. Bridge/UI should emit
// agenthub:permission and resolve via Complete (mirror interaction.Complete).
type PermissionWaiter struct {
	notify func(PendingPermission)
	// Timeout must match Service.PermissionTimeout — it only stamps ExpiresAt
	// for the UI; the deadline that actually closes the gate is the context in
	// Service.mediatePermission. Zero means interaction.DefaultTimeout.
	Timeout time.Duration

	mu      sync.Mutex
	pending map[string]*permissionWait // by ID; at most one per SessionID
}

type permissionWait struct {
	PendingPermission
	result   chan json.RawMessage
	finished bool
}

// NewPermissionWaiter creates a waiter. notify may be nil (still waits for Complete).
func NewPermissionWaiter(notify func(PendingPermission)) *PermissionWaiter {
	return &PermissionWaiter{notify: notify, pending: map[string]*permissionWait{}}
}

// Resolve implements PermissionResolver for Service.ResolvePermission.
func (w *PermissionWaiter) Resolve(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	return w.Wait(ctx, sessionID, params)
}

// Wait registers a pending prompt and blocks until Complete, ctx cancel, or deadline.
func (w *PermissionWaiter) Wait(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	id, err := vaultitem.NewID()
	if err != nil {
		return nil, err
	}
	resCh := make(chan json.RawMessage, 1)
	now := time.Now().UTC()
	p := PendingPermission{
		ID:        id,
		SessionID: sessionID,
		Params:    append(json.RawMessage(nil), params...),
		CreatedAt: now,
		ExpiresAt: now.Add(w.timeout()),
	}

	w.mu.Lock()
	for _, other := range w.pending {
		if other.SessionID == sessionID {
			// A session prompts synchronously; a second prompt from the same
			// session is a protocol violation, not a queue case.
			w.mu.Unlock()
			return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
		}
	}
	w.pending[id] = &permissionWait{PendingPermission: p, result: resCh}
	w.mu.Unlock()

	defer w.invalidate(id)

	if w.notify != nil {
		w.notify(p)
	}

	select {
	case <-ctx.Done():
		w.invalidate(id)
		return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
	case outcome := <-resCh:
		if isDeniedPermissionOutcome(outcome) {
			return outcome, ErrPermissionDenied
		}
		return outcome, nil
	}
}

// Complete resolves a pending permission prompt with an ACP outcome JSON body.
func (w *PermissionWaiter) Complete(id string, outcome json.RawMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return errors.New("agenthub: no pending permission")
	}
	p, ok := w.pending[id]
	if !ok || p.finished {
		return errors.New("agenthub: invalid permission id")
	}
	select {
	case p.result <- append(json.RawMessage(nil), outcome...):
		p.finished = true
		return nil
	default:
		return errors.New("agenthub: no pending permission")
	}
}

// PendingList returns every pending prompt, oldest first.
func (w *PermissionWaiter) PendingList() []PendingPermission {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]PendingPermission, 0, len(w.pending))
	for _, p := range w.pending {
		cp := p.PendingPermission
		cp.Params = append(json.RawMessage(nil), cp.Params...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Pending returns the oldest pending prompt, or nil.
func (w *PermissionWaiter) Pending() *PendingPermission {
	list := w.PendingList()
	if len(list) == 0 {
		return nil
	}
	return &list[0]
}

func (w *PermissionWaiter) invalidate(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if p, ok := w.pending[id]; ok {
		p.finished = true
		delete(w.pending, id)
	}
}

func (w *PermissionWaiter) timeout() time.Duration {
	if w.Timeout > 0 {
		return w.Timeout
	}
	return interaction.DefaultTimeout
}

// PermissionTimeoutOrDefault exposes the deadline the gate actually enforces so
// the waiter can stamp a matching ExpiresAt instead of duplicating the constant.
func (s *Service) PermissionTimeoutOrDefault() time.Duration {
	return s.permissionTimeout()
}

func (s *Service) permissionTimeout() time.Duration {
	if s.PermissionTimeout > 0 {
		return s.PermissionTimeout
	}
	return interaction.DefaultTimeout
}

// isDeniedPermissionOutcome reports cancelled/rejected/deny ACP outcomes.
// Malformed bodies fail closed (treated as deny).
func isDeniedPermissionOutcome(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var wrap struct {
		Outcome struct {
			Outcome string `json:"outcome"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(wrap.Outcome.Outcome)) {
	case "selected":
		return false
	case "cancelled", "canceled", "rejected", "deny", "denied":
		return true
	default:
		// Unknown / empty outcome → fail closed.
		return true
	}
}

// HookDenyReason is what the PreToolUse hook says when it blocks a tool from
// reaching Claude Code's own credentials, and what a test asserts on to prove
// the hook ran at all.
//
// It lives here rather than in cmd/devkit-mcp so the two sides cannot drift:
// the hook is installed through claude-agent-acp's private
// _meta.claudeCode.options channel, and a silently reworded reason would make
// the integration test that guards that channel pass while guarding nothing.
const HookDenyReason = "DevKit blocks agent access to Claude Code's own credentials"

// mediatePermission fails closed: nil resolver, timeout, cancel, or deny → deny.
// Never auto-approves. Returns an ACP cancelled/rejected Result body together
// with ErrPermissionDenied so Conn can deliver Result while Invoke fails closed.
func (s *Service) mediatePermission(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	if s.ResolvePermission == nil {
		return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
	}

	if err := s.setSessionStatus(ctx, sessionID, SessionStatusAwaitingPermission); err != nil {
		return nil, err
	}

	waitCtx, cancel := context.WithTimeout(ctx, s.permissionTimeout())
	defer cancel()

	outcome, err := s.ResolvePermission(waitCtx, sessionID, params)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			err = ErrPermissionDenied
		}
		if !errors.Is(err, ErrPermissionDenied) {
			// Unexpected resolver failure — still fail closed for the turn.
			err = ErrPermissionDenied
		}
		if len(outcome) == 0 {
			outcome = append(json.RawMessage(nil), deniedPermissionOutcome...)
		}
		// Leave status as awaiting_permission; Invoke error path sets final status.
		return outcome, err
	}

	if isDeniedPermissionOutcome(outcome) {
		if len(outcome) == 0 {
			outcome = append(json.RawMessage(nil), deniedPermissionOutcome...)
		}
		return outcome, ErrPermissionDenied
	}

	// Restore running so a continuing turn is still marked busy.
	if setErr := s.setSessionStatus(ctx, sessionID, SessionStatusRunning); setErr != nil {
		return nil, setErr
	}
	return outcome, nil
}
