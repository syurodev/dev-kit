package agenthub

import (
	"context"
	"log"
	"time"
)

// accountSwitch is when a session last moved to another account, and from which.
// Set by the switch itself (Task 10).
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
// are always set together. It also records which account the runtime will run
// as, which is the built-in one whenever the spec was left alone.
func (s *Service) applyAccount(ctx context.Context, spec *OpenSpec, sess AgentSession) {
	id := BuiltinAccountID(sess.AgentType)
	defer func() {
		s.mu.Lock()
		s.sessionAccount[sess.ID] = id
		s.mu.Unlock()
	}()
	if s.Accounts == nil || sess.AccountID == "" {
		return
	}
	acct, err := s.Accounts.Get(ctx, sess.AccountID)
	if err != nil {
		log.Printf("agenthub: session %s: account %s: %v; opening as the built-in account", sess.ID, sess.AccountID, err)
		return
	}
	id = sess.AccountID
	if acct.Builtin() {
		return
	}
	spec.AccountHome = acct.HomeDir
	if s.AccountSharedHome != nil {
		spec.AccountSharedPaths = SharedStorePaths(acct.Vendor, s.AccountSharedHome(acct.Vendor))
	}
}

// forgetSessionAccountLocked drops the per-session account state, with s.mu
// held, where a session is closed or deleted (nothing runs as an account then).
func (s *Service) forgetSessionAccountLocked(id string) {
	delete(s.sessionAccount, id)
	delete(s.accountSwitches, id)
	delete(s.turnProgress, id)
}
