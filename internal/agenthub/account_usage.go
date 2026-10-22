package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
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
	err := s.Accounts.SaveUsage(ctx, id, parsed.Usage.Limits, "observed", time.Now())
	if errors.Is(err, ErrAccountNotFound) {
		// The built-in account has no row until the book is first listed.
		if vendor, ok := builtinAccountVendor(id); ok {
			if _, lerr := s.Accounts.List(ctx, vendor); lerr != nil {
				err = lerr
			} else {
				err = s.Accounts.SaveUsage(ctx, id, parsed.Usage.Limits, "observed", time.Now())
			}
		}
	}
	if err != nil {
		log.Printf("agenthub: account %s: saving usage: %v", id, err)
		s.forgetUsageSeen(id)
	}
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

// builtinAccountVendor is the vendor of a built-in account id.
func builtinAccountVendor(id string) (AgentType, bool) {
	name, ok := strings.CutSuffix(id, "-default")
	if !ok || name == "" {
		return "", false
	}
	vendor := AgentType(name)
	return vendor, BuiltinAccountID(vendor) == id
}

// usageChanged reports whether windows differ from what was last written for
// the account, and remembers them.
func (s *Service) usageChanged(accountID string, windows []LimitWindow) bool {
	raw, _ := json.Marshal(windows)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.usageSeen[accountID] == string(raw) {
		return false
	}
	s.usageSeen[accountID] = string(raw)
	return true
}

// forgetUsageSeen lets the next reading of an account be written again, after
// a write that failed.
func (s *Service) forgetUsageSeen(accountID string) {
	s.mu.Lock()
	delete(s.usageSeen, accountID)
	s.mu.Unlock()
}
