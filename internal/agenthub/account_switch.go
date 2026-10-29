package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"devkit/internal/preferences"
)

// accountProbeCooldown is how long an account stays out after a refusal that
// names no reset, or after a reopen that failed, before the next turn may try
// it again.
const accountProbeCooldown = 15 * time.Minute

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

// fillAccountSpec points an OpenSpec at the session's account home and returns
// the id of the account the spec will run as. The built-in account changes
// nothing. An account that is gone, or no account book, falls back to the
// built-in one (logged for a missing row): a session must still open. The home
// and the shared store paths are always set together.
//
// It is pure with respect to the service: it records nothing, so a read-only
// caller (Environment) cannot write a stale account over what a concurrent move
// just set. A caller that goes on to open a runtime records the returned id with
// recordSessionAccount.
func (s *Service) fillAccountSpec(ctx context.Context, spec *OpenSpec, sess AgentSession) string {
	id := BuiltinAccountID(sess.AgentType)
	spec.UsageAccountGeneration = "builtin"
	if s.Accounts == nil || sess.AccountID == "" {
		return id
	}
	acct, err := s.Accounts.Get(ctx, sess.AccountID)
	if err != nil {
		log.Printf("agenthub: session %s: account %s: %v; opening as the built-in account", sess.ID, sess.AccountID, err)
		return id
	}
	s.fillFromAccount(spec, acct)
	return sess.AccountID
}

// fillFromAccount points an OpenSpec at one account's record: its usage
// generation, and for an added account its home and the shared store paths,
// always together. The built-in account changes nothing but the generation.
func (s *Service) fillFromAccount(spec *OpenSpec, acct AccountRecord) {
	spec.UsageAccountGeneration = acct.UsageGeneration
	if acct.Builtin() {
		return
	}
	spec.AccountHome = acct.HomeDir
	if s.AccountSharedHome != nil {
		spec.AccountSharedPaths = SharedStorePaths(acct.Vendor, s.AccountSharedHome(acct.Vendor))
	}
}

// recordSessionAccount reserves the account while a runtime is opening, so
// account removal sees it before publication (resume, rewind): the id fillAccountSpec
// returned, the built-in one whenever the spec was left alone.
func (s *Service) recordSessionAccount(sessionID, accountID string) {
	s.mu.Lock()
	s.sessionAccount[sessionID] = accountID
	s.mu.Unlock()
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
	policy, err := s.cachedAccountPolicy()
	if err != nil {
		log.Printf("agenthub: account policy: %v", err)
		return preferences.AccountVendorPolicy{}, false
	}
	vp, ok := policy.Vendors[string(vendor)]
	return vp, ok && vp.Enabled
}

// decideAccount asks PickAccount about a session as it is now. ok is false when
// switching is off for its vendor or its accounts cannot be read: nothing to do.
func (s *Service) decideAccount(ctx context.Context, sess *AgentSession) (PickDecision, bool) {
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
		Model: s.modelForPick(ctx, sess.ID, accounts),
	}), true
}

// modelForPick is the model a session runs, read from its last config-options
// row, for the windows that belong to one model. It reads only when an account
// carries such a window, so a vendor without them costs nothing. "" when unknown
// (no row, no model option, a read error): such a window then does not count.
func (s *Service) modelForPick(ctx context.Context, sessionID string, accounts []AccountRecord) string {
	scoped := false
	for _, a := range accounts {
		for _, w := range a.Usage {
			if strings.HasPrefix(w.Label, modelWindowPrefix) {
				scoped = true
			}
		}
	}
	if !scoped {
		return ""
	}
	entry, found, err := s.store.FindTranscriptKind(ctx, sessionID, UpdateConfigOptions)
	if err != nil || !found {
		return ""
	}
	var saved SessionUpdate
	if json.Unmarshal(entry.Payload, &saved) != nil {
		return ""
	}
	for _, option := range saved.ConfigOptions {
		var value string
		if option.ID == "model" && json.Unmarshal(option.CurrentValue, &value) == nil {
			return value
		}
	}
	return ""
}

// switchAccountForTurn runs at the start of a turn, with the session claimed and
// before its runtime is resolved. It moves the session to the account
// PickAccount names and opens it there, or refuses the turn with
// ErrAllAccountsLimited when every account is at its limit.
//
// A move that does not happen leaves the session where it was and the turn
// runs, or fails, as it did before accounts: a failed write changes nothing,
// and a process that will not open in the new account's home (its sign-in
// expired, the home cannot be prepared) moves the session back and keeps that
// account out for accountProbeCooldown. Only a move whose process opened is
// remembered and announced.
func (s *Service) switchAccountForTurn(ctx context.Context, sess *AgentSession) error {
	// A shared account's reading may be old: ask again first (time-boxed), so
	// the pick below reads what the account has left now.
	s.refreshStaleForTurn(ctx, sess)
	d, ok := s.decideAccount(ctx, sess)
	if !ok {
		return nil
	}
	if d.AllLimited {
		return allAccountsLimited(d.RetryAt)
	}
	if !d.Switch {
		return nil
	}
	// The move closes the running process and reopens it through the launch gate.
	// A reopen the gate would refuse (a new workspace executable, an unresolvable
	// snapshot) cannot succeed on any account, so the session stays where it is: the
	// running process is not closed for nothing, and the target is not put on
	// cooldown for a refusal that is about the workspace, not the account.
	if err := s.preflightHarness(ctx, *sess); err != nil {
		log.Printf("agenthub: session %s: not moving to account %s, a reopen would be refused: %v", sess.ID, d.Target, err)
		return nil
	}
	from, bound := accountIDOf(*sess), sess.AccountID
	if err := s.moveSession(ctx, sess, d.Target); err != nil {
		log.Printf("agenthub: session %s: could not move to account %s: %v", sess.ID, d.Target, err)
		return nil
	}
	// Opened here rather than by the turn's own resolve, so that a home which
	// cannot open does not keep the session: the resolve that follows finds
	// this runtime live.
	if _, err := s.runtimeFor(ctx, *sess); err != nil {
		s.abandonMove(ctx, sess, bound, from, d.Target, d.Reason, err)
		return nil
	}
	s.noteAccountSwitch(ctx, sess.ID, from, d.Target, d.Reason)
	return nil
}

// abandonMove handles a move whose process would not open in the target's
// home: back to the account the row named before (bound, exactly as it was
// spelled), the target kept out for accountProbeCooldown (a cooldown, not an
// exhaustion: it has not reached a limit), and a notice that the switch
// failed. If the move back cannot be written the session stays on the
// target — the row says so, so the transcript says the move happened — and the
// turn's own resolve tries the target once more, failing as any resume does. That
// edge is announced like a move that stands (noteAccountSwitch: the hooks fire and
// the pick rules remember it) although the target's process never opened, because
// the row is what the window and the next pick read.
//
// The clean-up runs on a context that outlives the turn's: the open that failed
// may have failed because the turn's own context ended, and a dead context
// would make every database call here return its error, leaving the session on
// a target that never opened. When that is why the open failed the account was
// not at fault, so it is not put on the cooldown and no notice blames it.
func (s *Service) abandonMove(ctx context.Context, sess *AgentSession, bound, from, target, reason string, openErr error) {
	turnEnded := ctx.Err() != nil || errors.Is(openErr, context.Canceled) || errors.Is(openErr, context.DeadlineExceeded)
	ctx = context.WithoutCancel(ctx)
	backErr := error(nil)
	if turnEnded {
		log.Printf("agenthub: session %s: the turn ended while opening on account %s: %v; moving it back to %s without blaming the account", sess.ID, target, openErr, from)
		backErr = s.moveSession(ctx, sess, bound)
	} else {
		log.Printf("agenthub: session %s: could not open on account %s: %v; moving it back to %s", sess.ID, target, openErr, from)
		backErr = s.moveSession(ctx, sess, bound)
		if err := s.Accounts.MarkCooldown(ctx, target, time.Now().Add(accountProbeCooldown)); err != nil {
			log.Printf("agenthub: account %s: could not keep it out after a failed open: %v", target, err)
		} else {
			s.accountsChanged() // the Accounts page shows the cooldown
		}
	}
	if backErr != nil {
		log.Printf("agenthub: session %s: could not move back to account %s: %v; it stays on %s", sess.ID, from, backErr, target)
		s.noteAccountSwitch(ctx, sess.ID, from, target, reason, false)
		return
	}
	if turnEnded {
		return
	}
	s.handleUpdate(ctx, sess.ID, accountSwitchFailedUpdate(from, target, switchFailureReason(openErr)))
}

// switchFailureReason is the sentence a failed-switch notice carries. The notice is
// stored in the transcript and sent to the window, and the error behind it can hold
// the account's home path or the vendor's stderr, so it is never quoted: a known
// failure maps to a fixed phrase and anything else to a generic one.
func switchFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrAccountHome):
		return "the account's folder could not be prepared"
	case errors.Is(err, ErrAgentAuthRequired):
		return "its sign-in is no longer valid"
	case errors.Is(err, ErrProcessFailed):
		return "its process could not be started"
	default:
		return "it could not be opened"
	}
}

// noteAccountSwitch remembers a move that stands (for PickAccount's hold and
// return rules) and records its notice in the transcript.
func (s *Service) noteAccountSwitch(ctx context.Context, id, from, to, reason string, reopened ...bool) {
	s.mu.Lock()
	s.accountSwitches[id] = accountSwitch{at: time.Now(), from: from}
	s.mu.Unlock()
	opened := true
	if len(reopened) > 0 {
		opened = reopened[0]
	}
	parsed := ParseSessionUpdate(accountSwitchUpdate(from, to, reason))
	s.mu.Lock()
	facts := s.runtimeUsage[id]
	s.mu.Unlock()
	parsed.AccountNotice.RuntimeOpened = &opened
	if opened && facts.AccountID == to {
		parsed.AccountNotice.ToGeneration = facts.AccountGeneration
	}
	// Host-only qualification is stamped after parsing. Vendor metadata is never trusted for it.
	s.emit(id, accountSwitchUpdate(from, to, reason))
	if err := s.recorder.Record(context.WithoutCancel(ctx), id, parsed); err != nil && s.OnRecordError != nil {
		s.OnRecordError(id, err)
	}
	if s.OnAccountSwitched != nil {
		s.OnAccountSwitched(id, from, to, reason)
	}
	s.accountsChanged()
}

// moveSession closes the session's idle runtime and binds it to target. The
// caller holds the session's claim (invoking), so no other turn or Resume runs
// meanwhile. s.mu is held only around the maps: Close, the flush and the store
// write each reach the database or a process.
//
// The settings calls and PrepareSession resume without the claim, so the move
// coordinates with s.resuming the way Reload does: it waits for a resume in
// flight (which would publish a process opened on the old account) and then
// holds its own barrier from the detach until the row and the cache name the
// target. A resume that starts meanwhile waits on it and reopens from the row.
//
// The runtime is closed before the row is written, so a failed write leaves the
// session bound where it was with no process: the caller's resolve reopens it
// on that account. A failed Close is logged, not returned — the runtime is out
// of the maps either way, and the move stands.
func (s *Service) moveSession(ctx context.Context, sess *AgentSession, target string) error {
	barrier := &resumeCall{done: make(chan struct{})}
	var rt SessionRuntime
	var closeEndpoint func()
	for {
		s.mu.Lock()
		call := s.resuming[sess.ID]
		if call == nil {
			s.resuming[sess.ID] = barrier
			rt = s.runtimes[sess.ID]
			closeEndpoint = s.endpoints[sess.ID]
			delete(s.runtimes, sess.ID)
			s.bgwork.forget(sess.ID)
			delete(s.runtimeUsage, sess.ID)
			delete(s.endpoints, sess.ID)
			s.forgetWorkspaceContextLocked(sess.ID)
			s.mu.Unlock()
			break
		}
		hook := s.waitHook
		s.mu.Unlock()
		if hook != nil {
			hook("move", sess.ID)
		}
		select {
		case <-call.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// barrier.err stays nil whatever happens: a waiter finds no runtime and
	// reopens the session itself, on whichever account its row then names.
	defer func() {
		s.mu.Lock()
		if s.resuming[sess.ID] == barrier {
			delete(s.resuming, sess.ID)
		}
		s.mu.Unlock()
		close(barrier.done)
	}()
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
	s.sessionAccount[sess.ID] = accountIDOf(*sess)
	s.mu.Unlock()
	return nil
}

// accountSwitchUpdate is the transcript notice of a move: a session_info_update
// carrying DevKit's own key under _meta, which ParseSessionUpdate reads as a
// session_info_update with a typed AccountNotice (no title, failure or goal) and the
// recorder keeps as an ordinary row. The window draws it in the accounts UI work
// (docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md).
func accountSwitchUpdate(from, to, reason string) json.RawMessage {
	return devkitInfoUpdate("accountSwitch", map[string]any{"from": from, "to": to, "reason": reason})
}

// accountSwitchFailedUpdate is the notice of a move that was tried and undone:
// the target's process would not open, and the session stayed on from. Same
// shape as accountSwitchUpdate; reason is why the open failed.
func accountSwitchFailedUpdate(from, to, reason string) json.RawMessage {
	return devkitInfoUpdate("accountSwitchFailed", map[string]any{"from": from, "to": to, "reason": reason})
}

func devkitInfoUpdate(key string, value map[string]any) json.RawMessage {
	return marshalUpdate(map[string]any{
		"sessionUpdate": string(UpdateSessionInfo),
		"_meta":         map[string]any{"devkit": map[string]any{key: value}},
	})
}
