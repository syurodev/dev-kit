package agenthub

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// A new session chooses its account before anything is opened, by the same
// policy a turn uses (PickAccount), so a built-in account that is out of quota
// cannot keep a session from starting while another account has room. The
// choice grants nothing and holds no credential: it only fills the OpenSpec's
// account fields, and every other gate (workspace, model, mode, grants) ran
// before it and is unchanged.
//
// A fresh session has no previous account: it is not a move, so nothing is
// remembered for PickAccount's hold and return rules and no notice is written.

// errNoNextAccount: after a refusal, no account is left to try.
var errNoNextAccount = errors.New("agenthub: no other account to try")

// openAccount is the account one open attempt runs as.
type openAccount struct {
	// id is the canonical account id (the built-in's is BuiltinAccountID).
	id string
	// bound is how the session row names it: "" for the built-in account.
	bound string
	// rec is the record the OpenSpec is filled from; nil leaves the built-in
	// account's defaults.
	rec *AccountRecord
}

func (a openAccount) apply(s *Service, spec *OpenSpec) {
	spec.UsageAccountGeneration = "builtin"
	if a.rec != nil {
		s.fillFromAccount(spec, *a.rec)
	}
}

// accountChooser picks the account of one session being created and holds it
// against removal while the session opens. It is used by that one creation.
type accountChooser struct {
	s         *Service
	vendor    AgentType
	sessionID string
	// enabled is false when switching is off for the vendor: the built-in
	// account opens as it always did.
	enabled bool
	// captured is the candidate set read when the choice began; no account added
	// later joins it, so the attempts are finite. tried holds those already opened.
	captured map[string]bool
	tried    map[string]bool
}

// newAccountChooser reads whether switching is on for the vendor. An unreadable
// policy counts as off, as it does at a turn (accountPolicyFor logs it).
//
// Switching is also off while the vendor's built-in account is pinned to a
// credential the person chose (Codex's API key): added accounts are subscription
// sign-ins and never use that key, so moving a new session to one would replace
// what was configured. Which credential applies is decided by that choice only,
// never inferred from a quota error.
func (s *Service) newAccountChooser(ctx context.Context, adapter VendorAdapter, sessionID string) *accountChooser {
	_, on := s.accountPolicyFor(adapter.Type())
	if pin, ok := adapter.(BuiltinCredentialPin); on && ok && pin.BuiltinPinnedToAPIKey(ctx) {
		on = false
	}
	return &accountChooser{s: s, vendor: adapter.Type(), sessionID: sessionID, enabled: on, tried: map[string]bool{}}
}

// release lets go of the account held for the session. It is deferred by
// CreateSession, so it runs after publication too, when sessionAccount names the
// account instead. Safe to call again.
func (c *accountChooser) release() {
	c.s.mu.Lock()
	delete(c.s.creating, c.sessionID)
	c.s.mu.Unlock()
}

// first chooses the account the session opens as. With switching off it is the
// built-in account. Otherwise PickAccount decides as for a session on the
// built-in account that has never moved: a healthy built-in account stays, an
// ineligible one gives way to the first eligible account in the person's order,
// and every account out of quota is ErrAllAccountsLimited with nothing opened.
// A book that cannot be read is an error, not a launch on the built-in account.
func (c *accountChooser) first(ctx context.Context) (openAccount, error) {
	builtin := openAccount{id: BuiltinAccountID(c.vendor)}
	if !c.enabled {
		c.hold(builtin.id)
		return builtin, nil
	}
	next, err := c.pick(ctx, builtin.id, true)
	if errors.Is(err, errNoNextAccount) {
		return openAccount{}, fmt.Errorf("%w: no account could be chosen for the session", ErrAccountsUnavailable)
	}
	return next, err
}

// afterRefusal is called with the account whose open the vendor refused with
// err. It returns the next account to try, ok=false when there is none (the
// caller reports err), or an error that replaces err because every account is
// known to be out of quota. Only a refusal the adapter typed as a usage limit,
// on a context that is still alive, counts: any other failure, a cancellation
// included, ends the creation without marking or rotating.
func (c *accountChooser) afterRefusal(ctx context.Context, failed openAccount, err error) (next openAccount, ok bool, replaced error) {
	c.release()
	if !c.enabled || !errors.Is(err, ErrAgentUsageLimit) || ctx.Err() != nil ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return openAccount{}, false, nil
	}
	c.tried[failed.id] = true
	hint, _ := stopHintOf(err)
	var retry *time.Time
	if !hint.RetryAfter.IsZero() {
		retry = &hint.RetryAfter
	}
	c.s.markExhausted(ctx, failed.id, retry, "new "+string(c.vendor)+" session")
	c.s.accountsChanged() // the Accounts page shows the limit
	// Switching may have been turned off since the choice began.
	if _, still := c.s.accountPolicyFor(c.vendor); !still {
		return openAccount{}, false, nil
	}
	next, perr := c.pick(ctx, failed.id, false)
	if perr != nil {
		if errors.Is(perr, ErrAllAccountsLimited) {
			return openAccount{}, false, perr
		}
		log.Printf("agenthub: new %s session: no other account to try after %s refused: %v", c.vendor, failed.id, perr)
		return openAccount{}, false, nil
	}
	return next, true, nil
}

// pick asks PickAccount which account follows current and holds it. The
// account's record is read again at the hold, so its home and usage generation
// are those of the account that exists when the session opens: one removed or
// replaced since the listing is chosen around, never opened as the built-in
// account in its place.
func (c *accountChooser) pick(ctx context.Context, current string, firstChoice bool) (openAccount, error) {
	// The listing is repeated only because a chosen account can vanish before it
	// is held; each pass either holds an account or finds one fewer, so it ends.
	for range 8 {
		vp, on := c.s.accountPolicyFor(c.vendor)
		if !on {
			if firstChoice {
				builtin := openAccount{id: BuiltinAccountID(c.vendor)}
				c.hold(builtin.id)
				return builtin, nil
			}
			return openAccount{}, errNoNextAccount
		}
		accounts, err := c.s.Accounts.List(ctx, c.vendor)
		if err != nil {
			log.Printf("agenthub: accounts of %s: %v", c.vendor, err)
			return openAccount{}, fmt.Errorf("%w: the accounts could not be read", ErrAccountsUnavailable)
		}
		if c.captured == nil {
			c.captured = make(map[string]bool, len(accounts))
			for _, a := range accounts {
				c.captured[a.ID] = true
			}
		}
		d := PickAccount(PickInput{Policy: vp, Accounts: accounts, Current: current, Now: time.Now()})
		if d.AllLimited {
			return openAccount{}, allAccountsLimited(d.RetryAt)
		}
		target := current
		if d.Switch {
			target = d.Target
		}
		if c.tried[target] {
			return openAccount{}, errNoNextAccount
		}
		if target == BuiltinAccountID(c.vendor) {
			c.hold(target)
			return openAccount{id: target}, nil
		}
		if !c.captured[target] {
			return openAccount{}, errNoNextAccount
		}
		acct, held := c.holdAdded(ctx, target)
		if held != nil {
			return openAccount{}, held
		}
		if acct == nil {
			// Gone since the listing: choose again among what remains.
			c.tried[target] = true
			continue
		}
		return openAccount{id: target, bound: target, rec: acct}, nil
	}
	return openAccount{}, errNoNextAccount
}

// hold records the account the session opens as, for accountInUse.
func (c *accountChooser) hold(id string) {
	c.s.mu.Lock()
	c.s.creating[c.sessionID] = id
	c.s.mu.Unlock()
}

// holdAdded reads the added account's record and holds it in one step that
// RemoveAccount cannot interleave with: removal checks and deletes under
// accountMgmt, and so does this. A nil record without an error means the
// account no longer exists.
func (c *accountChooser) holdAdded(ctx context.Context, id string) (*AccountRecord, error) {
	c.s.mu.Lock()
	hook := c.s.accountPickedHook
	c.s.mu.Unlock()
	if hook != nil {
		hook(id)
	}
	c.s.accountMgmt.Lock()
	defer c.s.accountMgmt.Unlock()
	acct, err := c.s.Accounts.Get(ctx, id)
	switch {
	case errors.Is(err, ErrAccountNotFound):
		return nil, nil
	case err != nil:
		log.Printf("agenthub: account %s: %v", id, err)
		return nil, fmt.Errorf("%w: the account could not be read", ErrAccountsUnavailable)
	case acct.Builtin():
		return nil, nil
	}
	c.hold(id)
	return &acct, nil
}

// allAccountsLimited is the refusal when every signed-in account is out of
// quota; first is when the earliest comes back.
func allAccountsLimited(first time.Time) error {
	return fmt.Errorf("%w: every account of this vendor has reached its limit; the first one is available again at %s",
		ErrAllAccountsLimited, retryClock(first))
}

// newRuntime is a runtime that opened for a new session, with the spec and the
// prepared launch it opened with and the account it runs as.
type newRuntime struct {
	rt      SessionRuntime
	launch  harnessLaunch
	spec    OpenSpec
	account openAccount
}

// openNewRuntime opens the vendor process of a new session as the account the
// chooser names. base is the spec with everything account-independent. Each
// attempt builds its own endpoint, harness snapshot, jail and temp directory
// from base, so nothing of a refused attempt (token, socket, account home)
// reaches the next one; a failed attempt is closed completely before the
// next. A prompt is never sent here, so none can be replayed.
//
// On error nothing is left open: no process, endpoint, temp directory or
// account held. The caller's deferred cleanup covers the held account and the
// last temp directory.
func (s *Service) openNewRuntime(ctx context.Context, adapter VendorAdapter, base OpenSpec, c *accountChooser) (newRuntime, error) {
	acct, err := c.first(ctx)
	if err != nil {
		return newRuntime{}, err
	}
	for {
		spec := base
		acct.apply(s, &spec)
		launch, err := s.prepareLaunch(ctx, &spec, base.SessionID, base.WorkspaceID, adapter.Type(), nil, launchCreate) // a new session: nothing earlier to compare with, and not judged as a reopen
		if err != nil {
			log.Printf("agenthub: %s session: prepare launch: %v", adapter.Type(), err)
			return newRuntime{}, err
		}
		s.applySandboxExtras(&spec)
		s.applyHarnessLaunch(&spec, launch, adapter.Type())
		s.applySubagents(&spec, adapter.Type())
		if err := prepareSessionTemp(&spec, true); err != nil {
			launch.closeEndpoint()
			return newRuntime{}, err
		}
		rt, err := s.openTracked(base.SessionID, func() (SessionRuntime, error) { return adapter.Open(ctx, spec) })
		if err == nil {
			return newRuntime{rt: rt, launch: launch, spec: spec, account: acct}, nil
		}
		// The bridge redacts this to one sentence; the log keeps the cause.
		log.Printf("agenthub: open %s session in %s: %v", adapter.Type(), base.Cwd, err)
		launch.closeEndpoint()
		removeSessionTemp(base.SessionID)
		next, ok, replaced := c.afterRefusal(ctx, acct, err)
		if replaced != nil {
			return newRuntime{}, replaced
		}
		if !ok {
			return newRuntime{}, err
		}
		acct = next
	}
}
