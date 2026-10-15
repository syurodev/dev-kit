package agenthub

import (
	"context"
	"time"

	"devkit/internal/identity"
)

// ResumeSession loads the same native conversation without sending a prompt.
// Reload also replaces an idle process so new customization takes effect.
func (s *Service) ResumeSession(ctx context.Context, id string, reload bool) error {
	if caller, ok := identity.FromContext(ctx); ok && caller.Kind != identity.KindUI {
		return ErrPermissionDenied
	}
	if err := s.authorizeTarget(ctx, id); err != nil {
		return err
	}
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	if reload {
		adapter, found := s.reg.Get(sess.AgentType)
		if !found {
			return ErrVendorUnknown
		}
		if _, ok := adapter.(SessionLoader); !ok || sess.ACPSessionID == "" {
			return ErrUnavailable
		}
	}
	s.mu.Lock()
	_, invoking := s.invoking[id]
	if invoking || s.resuming[id] != nil || sess.Status == SessionStatusRunning || sess.Status == SessionStatusAwaitingPermission {
		s.mu.Unlock()
		return ErrSessionBusy
	}
	s.invoking[id] = struct{}{}
	var barrier *resumeCall
	var rt SessionRuntime
	var closeEndpoint func()
	if reload {
		barrier = &resumeCall{done: make(chan struct{})}
		s.resuming[id] = barrier
		rt = s.runtimes[id]
		closeEndpoint = s.endpoints[id]
		delete(s.runtimes, id)
		delete(s.endpoints, id)
		s.forgetWorkspaceContextLocked(id)
	}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.invoking, id); s.mu.Unlock() }()
	if reload {
		if rt != nil {
			err = rt.Close(ctx)
		}
		if closeEndpoint != nil {
			closeEndpoint()
		}
		if flushErr := s.recorder.Flush(ctx, id); err == nil {
			err = flushErr
		}
		s.mu.Lock()
		delete(s.resuming, id)
		s.mu.Unlock()
		barrier.err = err
		close(barrier.done)
		if err != nil {
			return err
		}
	}
	if _, err := s.runtimeFor(ctx, sess); err != nil {
		return err
	}
	return s.store.UpdateSessionStatus(ctx, id, SessionStatusIdle, time.Now().UTC())
}
