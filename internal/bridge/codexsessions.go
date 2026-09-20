package bridge

import (
	"context"

	"devkit/internal/codexsessions"
)

// The UI's read-only view onto what DevKit observed of an external Codex CLI
// session — see internal/codexhook for how the data arrives and
// internal/codexsessions for how it is stored. codex:read/write are separate
// from codexhook:report, the capability Codex's own hooks call through: a
// Codex process should never be able to clear its own history, and the
// person viewing it should never need a hook's capability to do so.

func (s *AppService) CodexSessionsList() ([]codexsessions.Session, error) {
	result, err := s.call(context.Background(), "builtin.codexhook", "codex:read", "session:list", map[string]any{
		"action": "list",
	}, func(context.Context) (any, error) {
		return s.container.CodexSessions.Sessions()
	})
	if err != nil {
		return nil, err
	}
	return result.([]codexsessions.Session), nil
}

// CodexSessionEvents returns one session's transcript, oldest first. limit
// <= 0 means every event the session has, up to codexsessions.EventLimit.
func (s *AppService) CodexSessionEvents(sessionID string, limit int) ([]codexsessions.Event, error) {
	opaqueID := recordID(sessionID)
	result, err := s.call(context.Background(), "builtin.codexhook", "codex:read", "session:"+opaqueID, map[string]any{
		"action": "events",
	}, func(context.Context) (any, error) {
		return s.container.CodexSessions.Events(sessionID, limit)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codexsessions.Event), nil
}

func (s *AppService) CodexSessionsClearSession(sessionID string) error {
	opaqueID := recordID(sessionID)
	_, err := s.call(context.Background(), "builtin.codexhook", "codex:write", "session:"+opaqueID, map[string]any{
		"action": "clear_session",
	}, func(context.Context) (any, error) {
		return nil, s.container.CodexSessions.ClearSession(sessionID)
	})
	return err
}

// CodexSessionsClear forgets every session this machine has observed.
func (s *AppService) CodexSessionsClear() error {
	_, err := s.call(context.Background(), "builtin.codexhook", "codex:write", "session:*", map[string]any{
		"action": "clear_all",
	}, func(context.Context) (any, error) {
		return nil, s.container.CodexSessions.Clear()
	})
	return err
}
