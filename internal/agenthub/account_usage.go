package agenthub

import (
	"context"
	"errors"
	"log"
	"slices"
	"time"
)

// observeAccountUsage merges the usage windows an update carries into the
// account the session runs as. It is only what this app's sessions saw: on a
// shared account, a lower bound. Best effort: it runs inside handleUpdate, on
// the vendor's update goroutine, so a failure is logged and never returned (the
// update must still reach the transcript), and s.mu is never held across the
// book or the store.
func (s *Service) observeAccountUsage(ctx context.Context, sessionID string, parsed SessionUpdate) {
	if s.Accounts == nil || parsed.Kind != UpdateUsage || parsed.Usage == nil || len(parsed.Usage.Limits) == 0 {
		return
	}
	id := s.observedAccountID(ctx, sessionID)
	if id == "" || !s.usageChanged(id, parsed.Usage.Limits) {
		return
	}
	switch err := s.saveObservedUsage(ctx, sessionID, id, parsed.Usage.Limits); {
	case err == nil:
	case errors.Is(err, ErrAccountNotFound):
		// The account was deleted while its session still runs. Nothing to
		// keep the reading on, and the dedupe entry stays so identical updates
		// do not repeat the failing write.
	default:
		log.Printf("agenthub: account %s: saving usage: %v", id, err)
		s.forgetUsageSeen(id)
	}
}

// saveObservedUsage writes the reading, creating the built-in account's row
// first when it has none: that row only exists once the book was listed. The
// vendor comes from the session's own row, and only the session's built-in
// account id is ever created that way.
func (s *Service) saveObservedUsage(ctx context.Context, sessionID, id string, windows []LimitWindow) error {
	err := s.Accounts.SaveUsage(ctx, id, windows, "observed", time.Now())
	if !errors.Is(err, ErrAccountNotFound) {
		return err
	}
	sess, gerr := s.store.GetSession(ctx, sessionID)
	if gerr != nil || id != BuiltinAccountID(sess.AgentType) {
		return err
	}
	if _, lerr := s.Accounts.List(ctx, sess.AgentType); lerr != nil {
		return lerr
	}
	return s.Accounts.SaveUsage(ctx, id, windows, "observed", time.Now())
}

// observedAccountID is the account the session's runtime runs as. A runtime
// opened by CreateSession is not recorded in sessionAccount and is always the
// built-in account, which the stored row names.
func (s *Service) observedAccountID(ctx context.Context, sessionID string) string {
	s.mu.Lock()
	id := s.sessionAccount[sessionID]
	s.mu.Unlock()
	if id != "" {
		return id
	}
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return ""
	}
	id = accountIDOf(sess)
	s.mu.Lock()
	if cur := s.sessionAccount[sessionID]; cur != "" {
		id = cur
	} else {
		s.sessionAccount[sessionID] = id
	}
	s.mu.Unlock()
	return id
}

// usageChanged reports whether windows differ from what was last written for
// the account, and remembers them.
func (s *Service) usageChanged(accountID string, windows []LimitWindow) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.usageSeen[accountID]; ok && slices.Equal(prev, windows) {
		return false
	}
	s.usageSeen[accountID] = slices.Clone(windows)
	return true
}

// forgetUsageSeen lets the next reading of an account be written again. It
// must be called by every other writer of an account's stored usage (a later
// on-demand "checked" reading, say) and after a failed write: otherwise an
// identical later observed reading is skipped as already written.
func (s *Service) forgetUsageSeen(accountID string) {
	s.mu.Lock()
	delete(s.usageSeen, accountID)
	s.mu.Unlock()
}
