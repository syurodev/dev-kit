package agenthub

import (
	"context"
	"time"

	"devkit/internal/identity"
)

// Accessors the external test package needs and production has no use for.
// In _test.go so they cannot be called from the app.

// RuntimeForTest returns a session's live runtime.
func (s *Service) RuntimeForTest(sessionID string) (SessionRuntime, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt, ok := s.runtimes[sessionID]
	return rt, ok
}

// BindSessionAccountForTest writes a session's account without going through a turn.
func (s *Service) BindSessionAccountForTest(ctx context.Context, id, accountID string) error {
	return s.store.UpdateSessionAccount(ctx, id, accountID, time.Now().UTC())
}

// MoveSessionForTest moves a session to another account the way a turn does,
// without the turn (and without its claim).
func (s *Service) MoveSessionForTest(ctx context.Context, id, accountID string) error {
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	return s.moveSession(ctx, &sess, accountID)
}

// SetWaitHookForTest sets what is called when a caller is about to wait on a
// resume or an account move in flight; waiter is "move" or "resume".
func (s *Service) SetWaitHookForTest(hook func(waiter, sessionID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.waitHook = hook
}

// SessionAccountCachedForTest returns the account id a session's runtime is cached as running.
func (s *Service) SessionAccountCachedForTest(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessionAccount[id]
	return v, ok
}

// SetFollowUpHookForTest sets what is called just before the follow-up of a
// turn refused for a real limit is sent.
func (s *Service) SetFollowUpHookForTest(hook func(sessionID string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followUpHook = hook
}

// ClaimSessionForTest takes a session's turn claim the way a running turn
// holds it; release gives it back.
func (s *Service) ClaimSessionForTest(id string) (release func()) {
	s.mu.Lock()
	s.invoking[id] = struct{}{}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.invoking, id)
		s.mu.Unlock()
	}
}

// MarkAccountLimitedForTest calls markAccountLimited directly with a session's
// row and a stop, without a turn.
func (s *Service) MarkAccountLimitedForTest(ctx context.Context, id string, stop SessionStop) error {
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	s.markAccountLimited(ctx, sess, stop)
	return nil
}

// UpgradeStopForTest calls upgradeStopForReachedWindow directly with a
// session's row and a stop, without a turn.
func (s *Service) UpgradeStopForTest(ctx context.Context, id string, stop SessionStop) (SessionStop, error) {
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		return SessionStop{}, err
	}
	return s.upgradeStopForReachedWindow(ctx, sess, stop), nil
}

// SessionAccountCacheForTest reads and sets what the service remembers of the account a session runs as.
func (s *Service) SessionAccountCacheForTest(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionAccount[id]
}

func (s *Service) SetSessionAccountCacheForTest(id, accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionAccount[id] = accountID
}

// SetResumingForTest raises a session's resume barrier the way resumeRuntime does
// before it has published a runtime; release drops it.
func (s *Service) SetResumingForTest(id string) (release func()) {
	s.mu.Lock()
	s.resuming[id] = &resumeCall{done: make(chan struct{})}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.resuming, id)
		s.mu.Unlock()
	}
}

// SetAccountSwitchForTest records that a session last moved away from an account.
func (s *Service) SetAccountSwitchForTest(sessionID, from string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accountSwitches[sessionID] = accountSwitch{at: time.Now(), from: from}
}

// AccountSwitchKnownForTest reports whether a session still has a recorded move.
func (s *Service) AccountSwitchKnownForTest(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.accountSwitches[sessionID]
	return ok
}

// SetUsageSeenForTest and UsageSeenKnownForTest read and set the last usage
// written for an account.
func (s *Service) SetUsageSeenForTest(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usageSeen[accountID] = seenUsage{windows: []LimitWindow{{Label: "Weekly", UsedPercent: 1}}, at: time.Now()}
}

func (s *Service) UsageSeenKnownForTest(accountID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.usageSeen[accountID]
	return ok
}

// RecordStopForTest records a session's turn as ended with err, the way a turn's
// failure is recorded (stop, account marks), without running a turn.
func (s *Service) RecordStopForTest(ctx context.Context, id string, err error) error {
	sess, gerr := s.store.GetSession(ctx, id)
	if gerr != nil {
		return gerr
	}
	s.recordStop(ctx, sess, err, false, identity.Caller{})
	return nil
}

// SetCheckClockForTest replaces the clock the usage-check floor reads.
func (s *Service) SetCheckClockForTest(now func() time.Time) {
	s.mu.Lock()
	s.checkClock = now
	s.mu.Unlock()
}

// SetCheckWaitHookForTest sets what is called when a usage check finds one for the
// same account already running, just before it waits on it.
func (s *Service) SetCheckWaitHookForTest(hook func(accountID string)) {
	s.mu.Lock()
	s.checkWaitHook = hook
	s.mu.Unlock()
}

// SetTurnCheckTimeoutForTest shortens how long a turn waits for a pre-turn check;
// call the returned func to restore it.
func SetTurnCheckTimeoutForTest(d time.Duration) (restore func()) {
	old := turnCheckTimeout
	turnCheckTimeout = d
	return func() { turnCheckTimeout = old }
}

// SetRuntimeForTest puts rt in the service's runtime map for the session and
// records the account it runs as.
func (s *Service) SetRuntimeForTest(sessionID, accountID string, rt SessionRuntime) {
	s.mu.Lock()
	s.runtimes[sessionID] = rt
	s.sessionAccount[sessionID] = accountID
	s.mu.Unlock()
}

// SetCheckFlightHookForTest sets what is called once a usage check has registered
// its flight, before it waits for a slot.
func (s *Service) SetCheckFlightHookForTest(hook func(accountID string)) {
	s.mu.Lock()
	s.checkFlightHook = hook
	s.mu.Unlock()
}

// SetUsageCheckTimeoutForTest shortens how long one vendor usage call may run; call
// the returned func to restore it.
func SetUsageCheckTimeoutForTest(d time.Duration) (restore func()) {
	old := usageCheckTimeout
	usageCheckTimeout = d
	return func() { usageCheckTimeout = old }
}

// CheckUsageForTest runs the account's usage check as RefreshAccountUsage does,
// without the permission check and the book read, and says whether the vendor was asked.
func (s *Service) CheckUsageForTest(ctx context.Context, id string, minGap time.Duration) (asked bool, err error) {
	acct, err := s.Accounts.Get(context.Background(), id)
	if err != nil {
		return false, err
	}
	_, asked, err = s.checkUsage(ctx, acct, minGap)
	return asked, err
}
