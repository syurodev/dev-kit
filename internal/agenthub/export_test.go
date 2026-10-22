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

// MoveSessionForTest moves a session to another account the way a turn does,
// without the turn (and without its claim).
func (s *Service) MoveSessionForTest(ctx context.Context, id, accountID string) error {
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	return s.moveSession(ctx, &sess, accountID)
}

// SetWaitHookForTest sets what is called when a caller is about to wait on a
// resume or an account move in flight; waiter is "move" or "resume".
func (s *Service) SetWaitHookForTest(hook func(waiter, sessionID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waitHook = hook
}

// SessionAccountCachedForTest returns the account id a session's runtime is cached as running.
func (s *Service) SessionAccountCachedForTest(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessionAccount[id]
	return v, ok
}
