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
// Best effort, like observing usage: it runs while the turn's failure is being
// recorded, so an error is logged and never returned, and s.mu is never held
// across the book.
func (s *Service) markAccountLimited(ctx context.Context, sess AgentSession, stop SessionStop) {
	if s.Accounts == nil || (stop.Kind != StopUsageLimit && stop.Kind != StopRateLimited) {
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

// noteTurnProgress marks the session's running turn as having produced output
// or a tool call, which is what makes a refusal after it unsafe to resend. The
// echo of the prompt and a usage report are not progress.
func (s *Service) noteTurnProgress(sessionID string, kind UpdateKind) {
	switch kind {
	case UpdateAgentMessageChunk, UpdateAgentThoughtChunk, UpdateToolCall:
		s.mu.Lock()
		s.turnProgress[sessionID] = true
		s.mu.Unlock()
	}
}

func (s *Service) turnHadProgress(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnProgress[sessionID]
}

// resetTurnProgress starts a turn with no progress; invoke calls it once the
// session is claimed.
func (s *Service) resetTurnProgress(sessionID string) {
	s.mu.Lock()
	delete(s.turnProgress, sessionID)
	s.mu.Unlock()
}

// refusalFollowUp is what to send after a turn was refused for a real limit, per
// the person's onLimitRefusal. ok is false for `none`, for a turn that made
// progress when the choice is resendIfNoProgress, and when there is no other
// account to run it on (AllLimited or no switch: a follow-up on the refused
// account would only fail again).
//
// It runs after the refused turn's invoke has returned, so the progress it reads
// is that turn's — unless another turn (a queued peer message) has claimed the
// session since; that turn holds the claim, and the follow-up gives up as busy.
func (s *Service) refusalFollowUp(ctx context.Context, in InvokeInput) (InvokeInput, bool) {
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

// followRefusal runs the one follow-up a refused turn may get. It is called by
// Invoke after the refused turn's invoke returned (its claim released), and
// calls invoke, not Invoke, so a refused follow-up is never followed again.
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
func (s *Service) followRefusal(ctx context.Context, in InvokeInput, result InvokeResult, origErr error) (InvokeResult, error) {
	next, ok := s.refusalFollowUp(ctx, in)
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
