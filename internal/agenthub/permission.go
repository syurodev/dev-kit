package agenthub

import (
	"context"
	"encoding/json"
)

// PermissionResolver mediates ACP session/request_permission against UI / interaction.
// Production wiring is filled in Task 10; nil means fail-closed deny.
type PermissionResolver func(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error)

// deniedPermissionOutcome is the ACP reject response when no resolver is configured.
var deniedPermissionOutcome = json.RawMessage(`{"outcome":{"outcome":"cancelled"}}`)

// mediatePermission fails closed when ResolvePermission is nil.
func (s *Service) mediatePermission(ctx context.Context, sessionID string, params json.RawMessage) (json.RawMessage, error) {
	if s.ResolvePermission == nil {
		return append(json.RawMessage(nil), deniedPermissionOutcome...), ErrPermissionDenied
	}

	if err := s.setSessionStatus(ctx, sessionID, SessionStatusAwaitingPermission); err != nil {
		return nil, err
	}

	outcome, err := s.ResolvePermission(ctx, sessionID, params)

	// Restore running so a continuing turn is still marked busy.
	if setErr := s.setSessionStatus(ctx, sessionID, SessionStatusRunning); setErr != nil && err == nil {
		return nil, setErr
	}
	return outcome, err
}
