package agenthub

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	usageCheckSlots = 2                // checks running at once, app-wide
	manualCheckGap  = 10 * time.Second // a person's Refresh, per account
	autoCheckGap    = 60 * time.Second // a pre-turn check, per account
)

// turnCheckTimeout is how long a turn waits for its pre-turn checks, all of them
// together; a var for tests.
var turnCheckTimeout = 5 * time.Second

// usageCheckTimeout bounds one vendor usage call (a live session's read or a probe
// process), whoever asked; a var for tests. usageSaveTimeout bounds writing the answer,
// which is done even when the caller's context has ended.
var usageCheckTimeout = 30 * time.Second

const usageSaveTimeout = 5 * time.Second

var errUsageCheckAborted = errors.New("usage check aborted")

type usageFlight struct {
	done    chan struct{}
	windows []LimitWindow
	err     error
}

// usageChecks keeps what the guards need: one check per account at a time, a floor
// on how often one account is asked, and a cap on checks running at once. The zero
// value works.
type usageChecks struct {
	once sync.Once
	mu   sync.Mutex
	last map[string]time.Time
	// checkedAt is when each account's last SUCCESSFUL stored check began. Unlike last
	// (the floor, set when a check starts), a failed or empty check never moves it, and
	// observed readings do not either: it is what "stale" means for a shared login.
	checkedAt map[string]time.Time
	flight    map[string]*usageFlight
	slots     chan struct{}
}

func (c *usageChecks) init() {
	c.once.Do(func() {
		c.last = map[string]time.Time{}
		c.checkedAt = map[string]time.Time{}
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
	if s.SandboxExtras != nil && s.SandboxExtras().DeveloperToolchainsAllowed() {
		spec.ReadOnlyPaths = append(spec.ReadOnlyPaths, developerToolchainRoots()...)
	}
	return spec
}

// liveRateReaderOn is the connection of a running session that runs as the account
// and can answer the usage question itself, if there is one. Only the Codex runtime
// implements RateLimitReader; for a Claude account there is none and checkUsage starts
// a probe process on the home (see checkUsage).
//
// sessionAccount is published when a runtime opens. A legacy runtime with no
// entry runs as whatever its session's row names, which observedAccountID reads and
// remembers. The rows are read with no lock held.
func (s *Service) liveRateReaderOn(ctx context.Context, accountID string) (RateLimitReader, bool) {
	type candidate struct {
		sessionID string
		reader    RateLimitReader
		known     string
	}
	var candidates []candidate
	s.mu.Lock()
	for sessionID, rt := range s.runtimes {
		if reader, ok := rt.(RateLimitReader); ok {
			candidates = append(candidates, candidate{sessionID, reader, s.sessionAccount[sessionID]})
		}
	}
	s.mu.Unlock()
	for _, c := range candidates {
		acct := c.known
		if acct == "" {
			acct = s.observedAccountID(ctx, c.sessionID)
		}
		if acct == accountID {
			return c.reader, true
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
// Only the Codex runtime implements RateLimitReader. On Claude a probe therefore still
// starts a second CLI process on a home a session may be using, so the token-refresh
// race of two processes on one home remains there (unverified; the probe is read-only
// and short, and the floors bound how often it happens).
//
// The vendor call alone is bounded by usageCheckTimeout (a wedged session or CLI must
// not hold the caller, the flight and a slot for ever); waiting for a slot is bounded
// only by the caller's context. A caller that joins a check in flight never takes over
// the first caller's context error: when the flight ended on a cancellation or a
// deadline and the joiner's own context is live, the joiner looks again (the floor
// answers it if the vendor was asked, otherwise it starts its own check).
//
// A check that fails stores nothing: the old reading stays, and so does an answer with
// no window at all (a vendor that cannot say is "not checkable", not "nothing used").
// Neither s.mu nor a transaction is held while the vendor or the book is called.
func (s *Service) checkUsage(ctx context.Context, acct AccountRecord, minGap time.Duration) (windows []LimitWindow, asked bool, err error) {
	adapter, ok := s.reg.Get(acct.Vendor)
	if !ok {
		return nil, false, ErrVendorUnknown
	}
	checker, isChecker := adapter.(UsageChecker)
	c := &s.checks
	c.init()
	var (
		f           *usageFlight
		now         time.Time
		previous    time.Time
		hadPrevious bool
	)
	for f == nil {
		now = s.checkNow()
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
				if isContextError(running.err) {
					// The context error is the first caller's, not this one's.
					if ctx.Err() != nil {
						return nil, false, ctx.Err()
					}
					continue
				}
				return running.windows, true, running.err
			case <-ctx.Done():
				return nil, false, ctx.Err()
			}
		}
		previous, hadPrevious = c.last[acct.ID]
		if hadPrevious && now.Sub(previous) < minGap {
			c.mu.Unlock()
			return nil, false, nil
		}
		f = &usageFlight{done: make(chan struct{})}
		c.flight[acct.ID] = f
		c.last[acct.ID] = now
		c.mu.Unlock()
	}
	started, finished := false, false
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
		if !finished {
			// The vendor call panicked (the panic goes on): whoever waits on this
			// flight must not read nil/nil as a success.
			windows, err = nil, errUsageCheckAborted
		}
		f.windows, f.err = windows, err
		close(f.done)
	}()
	windows, asked, err = func() ([]LimitWindow, bool, error) {
		s.mu.Lock()
		hook := s.checkFlightHook
		s.mu.Unlock()
		if hook != nil {
			hook(acct.ID)
		}
		// A done context and a free slot are both ready to a select, which would
		// pick at random: look first.
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		select {
		case c.slots <- struct{}{}:
			defer func() { <-c.slots }()
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
		started = true
		vctx, cancel := context.WithTimeout(ctx, usageCheckTimeout)
		defer cancel()
		var windows []LimitWindow
		var err error
		if reader, ok := s.liveRateReaderOn(ctx, acct.ID); ok {
			windows, err = reader.ReadRateLimits(vctx)
		} else if isChecker {
			windows, err = checker.CheckUsage(vctx, s.accountProbeSpec(acct))
		} else {
			err = ErrUsageNotCheckable
		}
		if err != nil {
			if ctx.Err() == nil && errors.Is(vctx.Err(), context.DeadlineExceeded) && isContextError(err) {
				err = fmt.Errorf("the vendor did not answer within %s: %w", usageCheckTimeout, err)
			}
			return nil, true, err
		}
		if len(windows) == 0 {
			return nil, true, fmt.Errorf("%w: the vendor listed no window", ErrUsageNotCheckable)
		}
		// The answer is in: keep it even if the caller's box ended in the meantime.
		saveCtx, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), usageSaveTimeout)
		defer cancelSave()
		if saveErr := s.Accounts.ReplaceUsage(saveCtx, acct.ID, windows, "checked", now); saveErr != nil {
			return nil, true, saveErr
		}
		c.mu.Lock()
		c.checkedAt[acct.ID] = now
		c.mu.Unlock()
		// The next identical observed reading must not be skipped as "unchanged".
		s.forgetUsageSeen(acct.ID)
		s.accountsChanged()
		return windows, true, nil
	}()
	finished = true
	return windows, asked, err
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// forgetUsageCheck drops an account's check floor (a removed account: one added again
// under the same label gets the same id and must not wait out its predecessor's floor).
// A check still in flight is left alone.
func (s *Service) forgetUsageCheck(id string) {
	c := &s.checks
	c.init()
	c.mu.Lock()
	delete(c.last, id)
	delete(c.checkedAt, id)
	c.mu.Unlock()
}

// lastCheckedAt is when the account's last successful check began, in this process
// (zero: none yet, e.g. since the app started).
func (s *Service) lastCheckedAt(id string) time.Time {
	c := &s.checks
	c.init()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.checkedAt[id]
}

// RefreshAccountUsage is the Refresh button: one account, the window only (an agent
// must not be able to make DevKit start a vendor process). A second press within
// manualCheckGap of the last check asks nothing and answers with what is stored.
func (s *Service) RefreshAccountUsage(ctx context.Context, id string) (AccountRecord, error) {
	book, err := s.accountBook(ctx)
	if err != nil {
		return AccountRecord{}, err
	}
	acct, err := s.accountRow(ctx, book, id)
	if err != nil {
		return AccountRecord{}, err
	}
	if _, _, err := s.checkUsage(ctx, acct, manualCheckGap); err != nil {
		return acct, err
	}
	return book.Get(ctx, id)
}

// refreshStaleForTurn runs at the start of a turn, before the pick: a shared account
// whose last successful CHECK is older than the policy allows (or that has not been
// checked since the app started) is asked again, because what this app's sessions saw
// is only a lower bound of a login other people use. Staleness is the age of the last
// check, never of the last observation: sessions here keep refreshing observed readings,
// which would make the account look fresh for ever. The session's own account is
// checked first, then the one the pick is about to choose. Only shared, signed-in
// accounts are checked, and only when the vendor has at least two signed-in accounts
// (with one there is nothing to switch to, so nothing to ask). A failure keeps the old
// reading and never fails the turn.
//
// Both checks share ONE time box of turnCheckTimeout, so the checks never hold the turn
// (and the session's claim) longer than that, plus the moment a probe killed at the end
// of the box needs to be reaped (up to its exit grace, about 2 s). A turn started by an
// agent (invokeAgent, sendMessage) comes through here too, so an agent can cause a
// probe indirectly; that is bounded by the 60 s floor per account, the 2 slots and this
// box, and it asks the vendor about an account, never reads or changes anything.
func (s *Service) refreshStaleForTurn(ctx context.Context, sess *AgentSession) {
	vp, ok := s.accountPolicyFor(sess.AgentType)
	if !ok {
		return
	}
	accounts, err := s.Accounts.List(ctx, sess.AgentType)
	if err != nil {
		return
	}
	signedIn := 0
	for _, a := range accounts {
		if a.SignedIn {
			signedIn++
		}
	}
	if signedIn < 2 {
		return
	}
	maxAge := time.Duration(vp.UsageMaxAgeSeconds) * time.Second
	if maxAge <= 0 {
		maxAge = 300 * time.Second
	}
	stale := func(a *AccountRecord) bool {
		if a == nil || !a.Shared || !a.SignedIn {
			return false
		}
		lastCheck := s.lastCheckedAt(a.ID)
		return lastCheck.IsZero() || s.checkNow().Sub(lastCheck) > maxAge
	}
	cctx, cancel := context.WithTimeout(ctx, turnCheckTimeout)
	defer cancel()
	check := func(a AccountRecord) {
		if _, _, err := s.checkUsage(cctx, a, autoCheckGap); err != nil && !errors.Is(err, ErrUsageNotCheckable) {
			log.Printf("agenthub: account %s: checking its usage before a turn: %v", a.ID, err)
		}
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
