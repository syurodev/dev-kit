package agenthub

import (
	"context"
	"log"
	"time"
)

// accountSwitch is when a session last moved to another account, and from which.
type accountSwitch struct {
	at   time.Time
	from string
}

// accountIDOf is the account a session runs as: its own, else the vendor's built-in.
func accountIDOf(sess AgentSession) string {
	if sess.AccountID != "" {
		return sess.AccountID
	}
	return BuiltinAccountID(sess.AgentType)
}

// applyAccount points an OpenSpec at the session's account home. The built-in
// account changes nothing. An account that is gone falls back to the built-in
// one (logged): a session must still open. The home and the shared store paths
// are always set together.
func (s *Service) applyAccount(ctx context.Context, spec *OpenSpec, sess AgentSession) {
	id := accountIDOf(sess)
	s.mu.Lock()
	s.sessionAccount[sess.ID] = id
	s.mu.Unlock()
	if s.Accounts == nil || sess.AccountID == "" {
		return
	}
	acct, err := s.Accounts.Get(ctx, sess.AccountID)
	if err != nil {
		log.Printf("agenthub: session %s: account %s: %v; opening as the built-in account", sess.ID, sess.AccountID, err)
		return
	}
	if acct.Builtin() {
		return
	}
	spec.AccountHome = acct.HomeDir
	if s.AccountSharedHome != nil {
		spec.AccountSharedPaths = SharedStorePaths(acct.Vendor, s.AccountSharedHome(acct.Vendor))
	}
}
