package agenthub

import (
	"context"
	"encoding/json"
	"errors"
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
}

// PermissionWaiter blocks ACP session/request_permission until Complete or timeout.
//
// Dedicated waiter (not interaction.Hub): ACP permission option lists
// (allow_once / reject_once / allow_always / …) do not map cleanly onto
// interaction's select/confirm kinds. Bridge/UI should emit
// agenthub:permission and resolve via Complete (mirror interaction.Complete).
type PermissionWaiter struct {
	notify func(PendingPermission)

	mu      sync.Mutex
	pending *permissionWait
}

type permissionWait struct {
	PendingPermission
	result   chan json.RawMessage
	finished bool
}

// NewPermissionWaiter creates a waiter. notify may be nil (still waits for Complete).
func NewPermissionWaiter(notify func(PendingPermission)) *PermissionWaiter {
	return &PermissionWaiter{notify: notify}
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
	p := PendingPermission{
		ID:        id,
		SessionID: sessionID,
		Params:    append(json.RawMessage(nil), params...),
	}

	w.mu.Lock()
	if w.pending != nil {
		w.mu.Unlock()
		return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
	}
	w.pending = &permissionWait{PendingPermission: p, result: resCh}
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.pending = nil
		w.mu.Unlock()
	}()

	if w.notify != nil {
		w.notify(p)
	}

	select {
	case <-ctx.Done():
		w.invalidate(id)
		return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
	case outcome := <-resCh:
		return outcome, nil
	}
}

// Complete resolves a pending permission prompt with an ACP outcome JSON body.
func (w *PermissionWaiter) Complete(id string, outcome json.RawMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending == nil || w.pending.finished {
		return errors.New("agenthub: no pending permission")
	}
	if w.pending.ID != id {
		return errors.New("agenthub: invalid permission id")
	}
	select {
	case w.pending.result <- append(json.RawMessage(nil), outcome...):
		w.pending.finished = true
		return nil
	default:
		return errors.New("agenthub: no pending permission")
	}
}

// Pending returns a copy of the current pending permission, or nil.
func (w *PermissionWaiter) Pending() *PendingPermission {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending == nil {
		return nil
	}
	cp := w.pending.PendingPermission
	cp.Params = append(json.RawMessage(nil), cp.Params...)
	return &cp
}

func (w *PermissionWaiter) invalidate(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending != nil && w.pending.ID == id {
		w.pending.finished = true
		w.pending = nil
	}
}

func (s *Service) permissionTimeout() time.Duration {
	if s.PermissionTimeout > 0 {
		return s.PermissionTimeout
	}
	return interaction.DefaultTimeout
}

// mediatePermission fails closed: nil resolver, timeout, or cancel → deny.
// Never auto-approves.
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
		if len(outcome) == 0 {
			outcome = append(json.RawMessage(nil), deniedPermissionOutcome...)
		}
		// Leave status as awaiting_permission; Invoke error path sets final status.
		return outcome, err
	}

	// Restore running so a continuing turn is still marked busy.
	if setErr := s.setSessionStatus(ctx, sessionID, SessionStatusRunning); setErr != nil {
		return nil, setErr
	}
	return outcome, nil
}
