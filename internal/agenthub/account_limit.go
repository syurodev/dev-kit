package agenthub

import (
	"context"
	"errors"
	"log"
	"time"

	"devkit/internal/preferences"
)

// A real limit: the vendor refused a turn because the account it runs as is at
// its usage limit (or throttled). The account is kept out until it is usable
// again, and the person's onLimitRefusal decides whether anything is sent on
// the next account. Design: docs/superpowers/specs/2026-10-22-agent-multi-account-failover-design.md,
// "A real limit".

// continueAfterLimit is what `continue` sends on the next account.
const continueAfterLimit = "The previous account reached its usage limit. Continue where you left off."

// markAccountLimited records, after a turn the vendor refused for a real limit,
// when the account is usable again: the blocking window's reset (reachedReset),
// else the stop's retry time, else accountProbeCooldown, after which the next
// turn may try it again. Any other stop marks nothing.
//
// The account is the one the session's runtime runs as (observedAccountID), not
// whatever the row names: the two differ when the row was moved without the
// process following, and it is the process's account the vendor refused.
//
// Only a vendor the person configured is marked: one with an entry in the
// account policy, enabled or not. Without it the vendor has no accounts to
// choose between, and marking would only create its built-in account's row
// (an opencode or mock turn refused for a limit would leave "opencode-default").
//
// Best effort, like observing usage: it runs while the turn's failure is being
// recorded, so an error is logged and never returned, and s.mu is never held
// across the book.
func (s *Service) markAccountLimited(ctx context.Context, sess AgentSession, stop SessionStop) {
	if s.Accounts == nil || (stop.Kind != StopUsageLimit && stop.Kind != StopRateLimited) {
		return
	}
	if !s.vendorConfigured(sess.AgentType) {
		return
	}
	// Creates the built-in account's row, which exists only once the book was listed.
	if _, err := s.Accounts.List(ctx, sess.AgentType); err != nil {
		log.Printf("agenthub: session %s: accounts of %s: %v; its limit is not recorded", sess.ID, sess.AgentType, err)
		return
	}
	id := s.observedAccountID(ctx, sess.ID)
	if id == "" {
		id = accountIDOf(sess)
	}
	acct, err := s.Accounts.Get(ctx, id)
	if err != nil {
		log.Printf("agenthub: session %s: account %s: %v; its limit is not recorded", sess.ID, id, err)
		return
	}
	now := time.Now()
	until := now.Add(accountProbeCooldown)
	if reset := reachedReset(acct.Usage, now); !reset.IsZero() {
		until = reset
	} else if stop.RetryAfter != nil && stop.RetryAfter.After(now) {
		until = *stop.RetryAfter
	}
	if err := s.Accounts.MarkExhausted(ctx, id, until); err != nil {
		log.Printf("agenthub: account %s: marking its limit: %v", id, err)
	}
}

// noteSignedOut marks the account a refused turn ran as signed out when the vendor
// said sign-in is required, so PickAccount skips it as a target and moves a
// session off it until it is signed in again. Only a turn that failed with
// ErrAgentAuthRequired counts: StopFromError also reads a missing CLI
// (ErrAgentNotInstalled) and a vendor-wide bad API key (ErrAgentAPIKeyUnusable)
// as auth_required, but those are not one account's login dying, and marking
// the account would leave it out for every session. Setting it back is the job
// of the status refresh / sign-in flow of a later task; nothing here does it.
// Only for a vendor the person configured, like markAccountLimited. Best effort:
// errors are logged.
func (s *Service) noteSignedOut(ctx context.Context, sess AgentSession, stop SessionStop, turnErr error) {
	if s.Accounts == nil || stop.Kind != StopAuthRequired || !errors.Is(turnErr, ErrAgentAuthRequired) || !s.vendorConfigured(sess.AgentType) {
		return
	}
	if _, err := s.Accounts.List(ctx, sess.AgentType); err != nil { // creates the built-in row
		log.Printf("agenthub: accounts of %s: %v", sess.AgentType, err)
		return
	}
	id := s.observedAccountID(ctx, sess.ID)
	if id == "" {
		id = accountIDOf(sess)
	}
	if err := s.Accounts.SetSignedIn(ctx, id, false); err != nil {
		if !errors.Is(err, ErrAccountNotFound) {
			log.Printf("agenthub: account %s: marking it signed out: %v", id, err)
		}
		return
	}
	s.accountsChanged()
}

// noteSignedIn is the way back from noteSignedOut that needs no one to press
// Refresh: a turn that completed on an account marked signed out is the vendor
// accepting the login (the person signed in in a terminal, or the mark was
// wrong), so the mark is set back. Only for a vendor the person configured, and
// the event fires only when the mark really changed. Best effort: errors are
// logged, nothing is held across the book calls.
func (s *Service) noteSignedIn(ctx context.Context, sess AgentSession) {
	if s.Accounts == nil || !s.vendorConfigured(sess.AgentType) {
		return
	}
	id := s.observedAccountID(ctx, sess.ID)
	if id == "" {
		id = accountIDOf(sess)
	}
	acct, err := s.Accounts.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrAccountNotFound) {
			log.Printf("agenthub: account %s: reading its sign-in mark: %v", id, err)
		}
		return
	}
	if acct.SignedIn {
		return
	}
	if err := s.Accounts.SetSignedIn(ctx, id, true); err != nil {
		if !errors.Is(err, ErrAccountNotFound) {
			log.Printf("agenthub: account %s: marking it signed in: %v", id, err)
		}
		return
	}
	s.accountsChanged()
}

// vendorConfigured reports whether the person's account policy has an entry for
// the vendor, enabled or not. No policy, or one that cannot be read (logged),
// configures nothing.
func (s *Service) vendorConfigured(vendor AgentType) bool {
	if s.AccountPolicy == nil {
		return false
	}
	policy, err := s.cachedAccountPolicy()
	if err != nil {
		log.Printf("agenthub: account policy: %v", err)
		return false
	}
	_, ok := policy.Vendors[string(vendor)]
	return ok
}

// reachedWindowFresh is how recently an account's usage must have been observed
// for a reached window on it to explain a turn's failure: the window must have
// been seen during this turn or just before. An older reading says the account
// was at its limit then, not that the limit is why this turn failed.
const reachedWindowFresh = 10 * time.Minute

// upgradeStopForReachedWindow reads a turn's plain failure as the usage limit it
// most likely was. Only Codex refuses with a sentinel (ErrAgentUsageLimit);
// Claude Code's refusal wording is not one DevKit can recognise (a limit cannot
// be provoked on demand to measure it), so its refusal classes as `failed`. What
// Claude does say, in structured form, is the rate-limit event in its stream:
// observeAccountUsage stores it as a `reached` window on the account the runtime
// runs as. A `failed` stop on an account with such a window, seen within
// reachedWindowFresh and with a reset still ahead, becomes `usage_limit`, not
// retryable before that reset; the vendor's sentence, note, time and attempts
// are kept.
//
// Any other kind is left as it is: a cancel, a timeout, a missing sign-in, a dead
// process or a provider outage each has its own cause, and a usage or rate
// limit is already one. Best effort: it never fails or blocks the recording of
// the stop (errors are logged, the stop is returned unchanged), never holds s.mu
// across the book, and does nothing without an account book or for a vendor the
// person has no account policy entry for (vendorConfigured).
func (s *Service) upgradeStopForReachedWindow(ctx context.Context, sess AgentSession, stop SessionStop) SessionStop {
	if s.Accounts == nil || stop.Kind != StopFailed {
		return stop
	}
	// Like markAccountLimited, only for a vendor the person configured: without
	// an entry nothing observed usage (observeAccountUsage), and a reading that
	// is there says nothing about why a plain failure happened.
	if !s.vendorConfigured(sess.AgentType) {
		return stop
	}
	id := s.observedAccountID(ctx, sess.ID)
	if id == "" {
		id = accountIDOf(sess)
	}
	acct, err := s.Accounts.Get(ctx, id)
	if err != nil {
		// A built-in account no session has reported usage for has no row: nothing seen.
		if !errors.Is(err, ErrAccountNotFound) {
			log.Printf("agenthub: session %s: account %s: %v; its failure is not read against its limits", sess.ID, id, err)
		}
		return stop
	}
	now := time.Now()
	if acct.ObservedAt.IsZero() || now.Sub(acct.ObservedAt) > reachedWindowFresh {
		return stop
	}
	reset := reachedReset(acct.Usage, now)
	if reset.IsZero() {
		return stop
	}
	at := reset.UTC()
	stop.Kind = StopUsageLimit
	stop.Retryable = false
	stop.RetryAfter = &at
	return stop
}

// reachedReset is when the window that blocks the account resets, or zero when
// no window says so. Only windows the vendor reports as reached, with a known
// reset still ahead, count. Among them the ones at 100% are the ones that really
// block, and of those the earliest reset is when the account is usable again.
// With none at 100%, the earliest reset among the reached ones: Codex marks
// both its windows reached when either is, so a weekly window at 20% is
// "reached" beside the 5-hour one that is full, and taking the latest reset
// would keep the account out for a week over a 5-hour block.
func reachedReset(windows []LimitWindow, now time.Time) time.Time {
	var full, reached time.Time
	for _, w := range windows {
		if w.Status != "reached" || w.ResetsAt <= now.Unix() {
			continue
		}
		t := time.Unix(w.ResetsAt, 0)
		if reached.IsZero() || t.Before(reached) {
			reached = t
		}
		if w.UsedPercent >= 100 && (full.IsZero() || t.Before(full)) {
			full = t
		}
	}
	if !full.IsZero() {
		return full
	}
	return reached
}

// limitFollowUpInput is what to send after a turn was refused for a real limit,
// per the person's onLimitRefusal. ok is false for `none`, for a turn that made
// progress when the choice is resendIfNoProgress, and when there is no other
// account to run it on (AllLimited or no switch: a follow-up on the refused
// account would only fail again).
//
// There is only ever one follow-up: a second refusal means the next account is
// limited too, and the person should hear about it rather than DevKit cascading
// through every account.
//
// It runs after the refused turn's invoke has returned, so the progress it reads
// is that turn's — unless another turn (a queued peer message) has claimed the
// session since; that turn holds the claim, and the follow-up gives up as busy.
func (s *Service) limitFollowUpInput(ctx context.Context, in InvokeInput) (InvokeInput, bool) {
	sess, err := s.store.GetSession(ctx, in.SessionID)
	if err != nil {
		return InvokeInput{}, false
	}
	vp, ok := s.accountPolicyFor(sess.AgentType)
	if !ok {
		return InvokeInput{}, false
	}
	switch vp.OnLimitRefusal {
	case preferences.RefusalResendIfNoProgress, preferences.RefusalContinue:
	default:
		return InvokeInput{}, false
	}
	if vp.OnLimitRefusal == preferences.RefusalResendIfNoProgress && s.turnHadProgress(in.SessionID) {
		return InvokeInput{}, false
	}
	d, ok := s.decideAccount(ctx, &sess)
	if !ok || d.AllLimited || !d.Switch {
		return InvokeInput{}, false
	}
	if vp.OnLimitRefusal == preferences.RefusalContinue {
		return InvokeInput{SessionID: in.SessionID, CallerSessionID: in.CallerSessionID, Prompt: continueAfterLimit}, true
	}
	// The same input: a PDF it carried is already the session's (importDocuments
	// keeps one document per content), so nothing is attached twice.
	return in, true
}

// runLimitFollowUp runs the one follow-up a refused turn may get. It is called
// by Invoke after the refused turn's invoke returned (its claim released), and
// calls invoke, not Invoke, so a refused follow-up is never followed again: a
// second refusal means the next account is limited too, and the person should
// hear about it rather than DevKit cascading through every account.
//
// What it returns:
//   - no follow-up (none, progress, nowhere to go): the original error;
//   - the follow-up could not start because the session is busy (another turn
//     took the claim first) or every account is now at its limit (decided again
//     at the follow-up's start): the original error, quietly — the person's
//     turn was refused for a limit, and that is what they are told;
//   - the follow-up ran: its own result, success or failure. A follow-up that
//     ran and failed failed for its own reason (the next account refused it
//     too, say), and that is the newer fact about the session.
func (s *Service) runLimitFollowUp(ctx context.Context, in InvokeInput, result InvokeResult, origErr error) (InvokeResult, error) {
	next, ok := s.limitFollowUpInput(ctx, in)
	if !ok {
		return result, origErr
	}
	s.mu.Lock()
	hook := s.followUpHook
	s.mu.Unlock()
	if hook != nil {
		hook(in.SessionID)
	}
	followResult, err := s.invoke(ctx, next, nil)
	if errors.Is(err, ErrSessionBusy) || errors.Is(err, ErrAllAccountsLimited) {
		log.Printf("agenthub: session %s: no follow-up after the limit refusal: %v", in.SessionID, err)
		return result, origErr
	}
	return followResult, err
}

// endedInUsageLimit reports whether the turn Invoke just ran ended in a usage
// limit, which is what earns it a follow-up. Two sources say so: the vendor's
// own sentinel (Codex's ErrAgentUsageLimit), and the stop recordStop wrote for
// this turn, which may have been upgraded from a plain failure by a reached
// window (upgradeStopForReachedWindow; Claude). A turn that never ran (busy,
// every account limited) or was cancelled is no refusal. started is when Invoke
// began: a stop written before it belongs to an earlier turn, and an invoke that
// failed before running anything (a refused attachment, a process that would not
// open) leaves that stop in place.
func (s *Service) endedInUsageLimit(ctx context.Context, sessionID string, err error, started time.Time) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrAgentUsageLimit):
		return true
	case errors.Is(err, ErrSessionBusy), errors.Is(err, ErrAllAccountsLimited),
		errors.Is(err, ErrCancelled), errors.Is(err, context.Canceled):
		return false
	}
	sess, gerr := s.store.GetSession(ctx, sessionID)
	if gerr != nil || sess.Stop == nil {
		return false
	}
	return sess.Stop.Kind == StopUsageLimit && !sess.Stop.Since.Before(started)
}
