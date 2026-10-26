package agenthub

import (
	"context"
	"errors"
)

// ErrSessionScopeUnavailable means the session is unknown, closed or has no
// workspace, so it has no scope a harness change could target.
var ErrSessionScopeUnavailable = errors.New("session has no harness scope")

// HarnessScope is the trusted host read the harness proposal service uses to
// turn an authenticated session id into a workspace and a vendor. It is not a
// caller-facing method: it does not authorize on behalf of anyone, so it must
// only be reached with an id taken from the caller's identity or from a UI
// request that the caller (the UI) is allowed to make. A closed session, one
// with no workspace and an unknown id are all the same answer.
func (s *Service) HarnessScope(ctx context.Context, sessionID string) (workspaceID string, vendor AgentType, err error) {
	if sessionID == "" {
		return "", "", ErrSessionScopeUnavailable
	}
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil || sess.Status == SessionStatusClosed || sess.WorkspaceID == "" {
		return "", "", ErrSessionScopeUnavailable
	}
	return sess.WorkspaceID, sess.AgentType, nil
}
