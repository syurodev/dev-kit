// internal/bridge/terminal.go
package bridge

import (
	"context"

	"devkit/internal/terminal"
)

func (s *AppService) TerminalWrite(sessionID string, data []byte) error {
	owner, ok := s.container.Terminal.Owner(sessionID)
	if !ok {
		return terminal.ErrSessionNotFound
	}
	_, err := s.call(context.Background(), owner, "terminal:io", "session:"+sessionID, map[string]any{
		"action":     "terminal_write",
		"session_id": sessionID,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Terminal.Write(sessionID, data)
	})
	return err
}

func (s *AppService) TerminalResize(sessionID string, rows, cols int) error {
	owner, ok := s.container.Terminal.Owner(sessionID)
	if !ok {
		return terminal.ErrSessionNotFound
	}
	_, err := s.call(context.Background(), owner, "terminal:io", "session:"+sessionID, map[string]any{
		"action":     "terminal_resize",
		"session_id": sessionID,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Terminal.Resize(sessionID, rows, cols)
	})
	return err
}

func (s *AppService) TerminalClose(sessionID string) error {
	owner, ok := s.container.Terminal.Owner(sessionID)
	if !ok {
		return terminal.ErrSessionNotFound
	}
	_, err := s.call(context.Background(), owner, "terminal:io", "session:"+sessionID, map[string]any{
		"action":     "terminal_close",
		"session_id": sessionID,
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.Terminal.Close(sessionID)
	})
	return err
}
