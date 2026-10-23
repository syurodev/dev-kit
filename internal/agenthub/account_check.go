package agenthub

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

const (
	usageCheckSlots = 2                // checks running at once, app-wide
	manualCheckGap  = 10 * time.Second // a person's Refresh, per account
	autoCheckGap    = 60 * time.Second // a pre-turn check, per account
)

// turnCheckTimeout is how long a turn waits for a pre-turn check; a var for tests.
var turnCheckTimeout = 5 * time.Second

type usageFlight struct {
	done    chan struct{}
	windows []LimitWindow
	err     error
}

// usageChecks keeps what the guards need: one check per account at a time, a floor
// on how often one account is asked, and a cap on checks running at once. The zero
// value works.
type usageChecks struct {
	once   sync.Once
	mu     sync.Mutex
	last   map[string]time.Time
	flight map[string]*usageFlight
	slots  chan struct{}
}

func (c *usageChecks) init() {
	c.once.Do(func() {
		c.last = map[string]time.Time{}
		c.flight = map[string]*usageFlight{}
		c.slots = make(chan struct{}, usageCheckSlots)
	})
}

func (s *Service) checkNow() time.Time {
	s.mu.Lock()
	clock := s.checkClock
	s.mu.Unlock()
	if clock != nil {
		return clock()
	}
	return time.Now()
}

// accountProbeSpec is the OpenSpec a vendor probe runs under for an account. It is
// built the way a session's is, because a probe runs the vendor's CLI under the same
// jail: the account's home and shared store (fillAccountSpec), DevKit's data directory
// hidden (ProtectedPaths, so this account's CLI never reads another account's home)
// and the person's home-sharing choice (applySandboxExtras). It leaves out what a probe
// has no use for: Docker, customizations, subagents, the session brief.
//
// The adapters take the spec as given and add no deny paths of their own, so this is
// the one place that fills it.
func (s *Service) accountProbeSpec(acct AccountRecord) OpenSpec {
	var spec OpenSpec
	if !acct.Builtin() {
		spec.AccountHome = acct.HomeDir
		if s.AccountSharedHome != nil {
			spec.AccountSharedPaths = SharedStorePaths(acct.Vendor, s.AccountSharedHome(acct.Vendor))
		}
	}
	if s.ProtectedPaths != nil {
		spec.DenyReadPaths = append(spec.DenyReadPaths, s.ProtectedPaths()...)
	}
	if s.SandboxExtras != nil && s.SandboxExtras().HomeShared() {
		shared := false
		spec.PrivateHome = &shared
		spec.ReadOnlyPaths = append(spec.ReadOnlyPaths, developmentToolRoots()...)
	}
	return spec
}

// liveRateReaderOn is the connection of a running session that runs as the account
// and can answer the usage question itself, if there is one.
func (s *Service) liveRateReaderOn(accountID string) (RateLimitReader, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sessionID, rt := range s.runtimes {
		if s.sessionAccount[sessionID] != accountID {
			continue
		}
		if reader, ok := rt.(RateLimitReader); ok {
			return reader, true
		}
	}
	return nil, false
}

// checkUsage asks the vendor what the account has left and stores the answer as the
// account's whole "checked" reading. asked is false when nothing was started because
// the account was asked less than minGap ago. Concurrent callers for one account share
// one check; at most usageCheckSlots run at once. A session already running on the
// account answers on its own connection instead of a second process starting on the
// same home (two processes could refresh the login's tokens at the same moment).
//
// A check that fails stores nothing: the old reading stays. Neither s.mu nor a
// transaction is held while the vendor or the book is called.
func (s *Service) checkUsage(ctx context.Context, acct AccountRecord, minGap time.Duration) (windows []LimitWindow, asked bool, err error) {
	adapter, ok := s.reg.Get(acct.Vendor)
	if !ok {
		return nil, false, ErrVendorUnknown
	}
	checker, isChecker := adapter.(UsageChecker)
	c := &s.checks
	c.init()
	now := s.checkNow()
	c.mu.Lock()
	if running, ok := c.flight[acct.ID]; ok {
		c.mu.Unlock()
		s.mu.Lock()
		hook := s.checkWaitHook
		s.mu.Unlock()
		if hook != nil {
			hook(acct.ID)
		}
		select {
		case <-running.done:
			return running.windows, true, running.err
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	previous, hadPrevious := c.last[acct.ID]
	if hadPrevious && now.Sub(previous) < minGap {
		c.mu.Unlock()
		return nil, false, nil
	}
	f := &usageFlight{done: make(chan struct{})}
	c.flight[acct.ID] = f
	c.last[acct.ID] = now
	c.mu.Unlock()
	started := false
	defer func() {
		c.mu.Lock()
		delete(c.flight, acct.ID)
		if !started {
			// Nothing was asked of the vendor: do not hold the next caller to the floor.
			if hadPrevious {
				c.last[acct.ID] = previous
			} else {
				delete(c.last, acct.ID)
			}
		}
		c.mu.Unlock()
		f.windows, f.err = windows, err
		close(f.done)
	}()

	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
	started = true
	if reader, ok := s.liveRateReaderOn(acct.ID); ok {
		windows, err = reader.ReadRateLimits(ctx)
	} else if isChecker {
		windows, err = checker.CheckUsage(ctx, s.accountProbeSpec(acct))
	} else {
		err = ErrUsageNotCheckable
	}
	if err != nil {
		return nil, true, err
	}
	if windows == nil {
		windows = []LimitWindow{}
	}
	if saveErr := s.Accounts.ReplaceUsage(ctx, acct.ID, windows, "checked", now); saveErr != nil {
		return nil, true, saveErr
	}
	// The next identical observed reading must not be skipped as "unchanged".
	s.forgetUsageSeen(acct.ID)
	s.accountsChanged()
	return windows, true, nil
}

// RefreshAccountUsage is the Refresh button: one account, the window only (an agent
// must not be able to make DevKit start a vendor process). A second press within
// manualCheckGap of the last check asks nothing and answers with what is stored.
func (s *Service) RefreshAccountUsage(ctx context.Context, id string) (AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, err
	}
	acct, err := book.Get(ctx, id)
	if err != nil {
		return AccountRecord{}, err
	}
	if _, _, err := s.checkUsage(ctx, acct, manualCheckGap); err != nil {
		return acct, err
	}
	return book.Get(ctx, id)
}

// refreshStaleForTurn runs at the start of a turn, before the pick: a shared account's
// reading older than the policy allows is asked again, because what this app's sessions
// saw is only a lower bound of a login other people use. The session's own account is
// checked first, then the one the pick is about to choose. Only shared, signed-in
// accounts are checked; each check is time-boxed and a failure keeps the old reading,
// so a check never fails or delays a turn beyond turnCheckTimeout.
func (s *Service) refreshStaleForTurn(ctx context.Context, sess *AgentSession) {
	vp, ok := s.accountPolicyFor(sess.AgentType)
	if !ok {
		return
	}
	maxAge := time.Duration(vp.UsageMaxAgeSeconds) * time.Second
	if maxAge <= 0 {
		maxAge = 300 * time.Second
	}
	stale := func(a *AccountRecord) bool {
		return a != nil && a.Shared && a.SignedIn && (a.ObservedAt.IsZero() || s.checkNow().Sub(a.ObservedAt) > maxAge)
	}
	check := func(a AccountRecord) {
		cctx, cancel := context.WithTimeout(ctx, turnCheckTimeout)
		defer cancel()
		if _, _, err := s.checkUsage(cctx, a, autoCheckGap); err != nil && !errors.Is(err, ErrUsageNotCheckable) {
			log.Printf("agenthub: account %s: checking its usage before a turn: %v", a.ID, err)
		}
	}
	accounts, err := s.Accounts.List(ctx, sess.AgentType)
	if err != nil {
		return
	}
	if cur := findAccount(accounts, sess.AccountID); stale(cur) {
		check(*cur)
	}
	d, ok := s.decideAccount(ctx, sess)
	if !ok || !d.Switch {
		return
	}
	if accounts, err = s.Accounts.List(ctx, sess.AgentType); err != nil {
		return
	}
	if target := findAccount(accounts, d.Target); stale(target) {
		check(*target)
	}
}
