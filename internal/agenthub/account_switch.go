package agenthub

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"devkit/internal/preferences"
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

// accountPolicyFor reads the person's policy for a vendor; ok is false when the
// feature is off (no account book, no policy, the vendor not enabled) or the
// policy is unreadable (logged), so the turn runs as before.
func (s *Service) accountPolicyFor(vendor AgentType) (preferences.AccountVendorPolicy, bool) {
	if s.Accounts == nil || s.AccountPolicy == nil {
		return preferences.AccountVendorPolicy{}, false
	}
	policy, err := s.AccountPolicy()
	if err != nil {
		log.Printf("agenthub: account policy: %v", err)
		return preferences.AccountVendorPolicy{}, false
	}
	vp, ok := policy.Vendors[string(vendor)]
	return vp, ok && vp.Enabled
}

// decideAccount asks PickAccount about a session as it is now. ok is false when
// switching is off for its vendor or its accounts cannot be read: nothing to do.
func (s *Service) decideAccount(ctx context.Context, sess AgentSession) (PickDecision, bool) {
	vp, ok := s.accountPolicyFor(sess.AgentType)
	if !ok {
		return PickDecision{}, false
	}
	accounts, err := s.Accounts.List(ctx, sess.AgentType)
	if err != nil {
		log.Printf("agenthub: accounts of %s: %v", sess.AgentType, err)
		return PickDecision{}, false
	}
	s.mu.Lock()
	prev := s.accountSwitches[sess.ID]
	s.mu.Unlock()
	return PickAccount(PickInput{
		Policy: vp, Accounts: accounts, Current: sess.AccountID,
		Now: time.Now(), SwitchedAt: prev.at, Previous: prev.from,
	}), true
}

// switchAccountForTurn runs at the start of a turn, with the session claimed and
// before its runtime is resolved. It moves the session to the account
// PickAccount names (closing the idle process; the resolve that follows reopens
// it in the new home), or refuses the turn with ErrAllAccountsLimited when every
// account is at its limit. A failure to move leaves the session where it is: the
// turn runs, or fails, as it did before accounts.
func (s *Service) switchAccountForTurn(ctx context.Context, sess *AgentSession) error {
	d, ok := s.decideAccount(ctx, *sess)
	if !ok {
		return nil
	}
	if d.AllLimited {
		return fmt.Errorf("%w: every account is at its limit; the first returns at %s",
			ErrAllAccountsLimited, d.RetryAt.Local().Format("15:04 Mon 2 Jan"))
	}
	if !d.Switch {
		return nil
	}
	from := accountIDOf(*sess)
	if err := s.moveSession(ctx, sess, d.Target); err != nil {
		log.Printf("agenthub: session %s: could not move to account %s: %v", sess.ID, d.Target, err)
		return nil
	}
	s.mu.Lock()
	s.accountSwitches[sess.ID] = accountSwitch{at: time.Now(), from: from}
	s.mu.Unlock()
	s.handleUpdate(context.WithoutCancel(ctx), sess.ID, accountSwitchUpdate(from, d.Target, d.Reason))
	return nil
}

// moveSession closes the session's idle runtime and binds it to target. The
// caller holds the session's claim (invoking), so no other turn or Resume runs
// meanwhile. s.mu is held only around the maps: Close, the flush and the store
// write each reach the database or a process.
//
// The runtime is closed before the row is written, so a failed write leaves the
// session bound where it was with no process: the caller's resolve reopens it
// on that account. A failed Close is logged, not returned — the runtime is out
// of the maps either way, and the move stands.
func (s *Service) moveSession(ctx context.Context, sess *AgentSession, target string) error {
	s.mu.Lock()
	rt := s.runtimes[sess.ID]
	closeEndpoint := s.endpoints[sess.ID]
	delete(s.runtimes, sess.ID)
	delete(s.endpoints, sess.ID)
	s.forgetWorkspaceContextLocked(sess.ID)
	s.mu.Unlock()
	if rt != nil {
		if err := rt.Close(ctx); err != nil {
			log.Printf("agenthub: session %s: closing its process to change account: %v", sess.ID, err)
		}
	}
	if closeEndpoint != nil {
		closeEndpoint()
	}
	if err := s.recorder.Flush(ctx, sess.ID); err != nil && s.OnRecordError != nil {
		s.OnRecordError(sess.ID, err)
	}
	if err := s.store.UpdateSessionAccount(ctx, sess.ID, target, time.Now().UTC()); err != nil {
		return err
	}
	sess.AccountID = target
	s.mu.Lock()
	s.sessionAccount[sess.ID] = target
	s.mu.Unlock()
	return nil
}

// accountSwitchUpdate is the transcript notice of a move: a session_info_update
// carrying DevKit's own key under _meta, which ParseSessionUpdate reads as a
// session_info_update with nothing applied (no title, failure or goal) and the
// recorder keeps as an ordinary row with its raw body. The window learns to draw
// it in the accounts UI plan.
func accountSwitchUpdate(from, to, reason string) json.RawMessage {
	return marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdateSessionInfo),
		"_meta": map[string]any{
			"devkit": map[string]any{"accountSwitch": map[string]any{"from": from, "to": to, "reason": reason}},
		},
	})
}
