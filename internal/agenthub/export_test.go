package agenthub

import (
	"context"
	"time"
)

// Accessors the external test package needs and production has no use for.
// In _test.go so they cannot be called from the app.

// RuntimeForTest returns a session's live runtime.
func (s *Service) RuntimeForTest(sessionID string) (SessionRuntime, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, ok := s.runtimes[sessionID]
	return rt, ok
}

// BindSessionAccountForTest writes a session's account without going through a turn.
func (s *Service) BindSessionAccountForTest(ctx context.Context, id, accountID string) error {
	return s.store.UpdateSessionAccount(ctx, id, accountID, time.Now().UTC())
}
