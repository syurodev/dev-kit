package harnessconfig

import (
	"context"
	"errors"

	"devkit/internal/harness"
	"devkit/internal/identity"
)

// Result says what was stored. Nothing here changes a running session: H3 and H4
// own publishing a committed change to a runtime, so Live is always false and a
// workspace change means other sessions need a reload to pick it up.
type Result struct {
	Profile, Override harness.Stamp
	Live              bool
	ReloadRequired    bool
}

// Apply asks the person, rechecks every input and commits the proposal once.
// The order is the contract:
//
//  1. take the proposal (single use: it is spent whatever happens next);
//  2. fail closed if nobody can answer, then ask with no lock and no
//     transaction held;
//  3. inside the validation boundary re-read identity, scope, policy, V, W, S
//     and the vendor facts, rebuild the effective before and after, and require
//     the same binding the person saw;
//  4. commit W and S in one short transaction with the expected stamps.
//
// Any refusal leaves W and S unchanged. A later retry needs a fresh Prepare.
func (s *Service) Apply(ctx context.Context, id string) (Result, error) {
	caller, ok := identity.FromContext(ctx)
	p, err := s.take(caller, ok, id)
	if err != nil {
		return Result{}, err
	}
	// The proposal is spent from here on, success or not.
	defer s.drop(id)

	fail := func(err error) (Result, error) {
		s.audit(p, harness.Applied{}, outcomeOf(err))
		return Result{}, err
	}
	if s.cfg.Confirmer == nil || !s.cfg.Confirmer.Available() || s.cfg.Boundary == nil {
		return fail(ErrConfirmUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return fail(ErrNotConfirmed)
	}
	// Narrowing the person has allowed the agent to apply by itself skips the
	// question, but only when someone could have answered (checked above).
	if p.bound.needsApproval {
		// A Cancel while the question is open closes it (and refuses the commit).
		askCtx, abort := context.WithCancel(ctx)
		defer abort()
		s.mu.Lock()
		p.abort = abort
		cancelled := p.cancelled
		s.mu.Unlock()
		if cancelled {
			return fail(ErrCancelled)
		}
		if err := s.cfg.Confirmer.Confirm(askCtx, Confirmation{
			Target: p.view.Target, Tier: p.view.Tier, Summary: p.view.Summary, RationaleUntrusted: p.view.RationaleUntrusted,
		}); err != nil {
			if s.wasCancelled(p) {
				return fail(ErrCancelled)
			}
			return fail(ErrNotConfirmed)
		}
		if err := ctx.Err(); err != nil {
			return fail(ErrNotConfirmed)
		}
	}

	var applied harness.Applied
	err = s.cfg.Boundary.Hold(ctx, func() error {
		ev, policy, err := s.evaluate(ctx, caller, p.req)
		if err != nil {
			return refusalFor(err)
		}
		if ev.Tier == harness.TierExecutable {
			if !policy.AllowExecutable {
				return ErrExecutableDenied
			}
			return ErrExecutableNotReady
		}
		if ev.W != p.bound.W {
			return ErrConflict
		}
		if ev.digest() != p.bound.digest() {
			return ErrStale
		}
		// The point of no return: after this Cancel is too late. Checked and set
		// together under the lock, so a Cancel either lands before (and wins) or
		// is told it is too late.
		if !s.beginCommit(p) {
			return ErrCancelled
		}
		applied, err = s.cfg.Store.Apply(ctx, ev.write)
		return mapApplyErr(err)
	})
	if err != nil {
		return fail(err)
	}
	s.audit(p, applied, "applied")
	return Result{Profile: applied.Profile, Override: applied.Override, ReloadRequired: p.bound.write.Profile != nil}, nil
}

// take marks a proposal as in use by its proposer. Only the caller that
// prepared it can; a second call, concurrent or later, finds it gone.
func (s *Service) take(caller identity.Caller, ok bool, id string) (*pendingProposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pending[id]
	if !ok || p == nil || p.caller != caller.String() || p.taken {
		return nil, ErrUnknownProposal
	}
	p.taken = true
	return p, nil
}

func (s *Service) drop(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, id)
}

// refusalFor keeps typed refusals and turns everything else (a store or port
// failure, a cancelled context) into a stale proposal: the approval is never
// carried over to inputs that could not be re-read.
func refusalFor(err error) error {
	var he *harness.Error
	if errors.As(err, &he) {
		return he
	}
	return ErrStale
}

func mapApplyErr(err error) error {
	var conflict *harness.ConflictError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &conflict) && conflict.Target == "profile":
		return ErrConflict
	case errors.Is(err, harness.ErrStoreConflict):
		return ErrStale
	case errors.Is(err, harness.ErrStoreScope):
		return ErrScopeGone
	}
	return ErrStale
}

func outcomeOf(err error) string {
	var he *harness.Error
	if errors.As(err, &he) {
		return he.Code
	}
	return "error"
}

func (s *Service) audit(p *pendingProposal, applied harness.Applied, outcome string) {
	if s.cfg.Audit == nil {
		return
	}
	s.cfg.Audit(Event{Outcome: outcome, Tier: p.view.Tier, Target: p.view.Target, Caller: p.caller,
		WorkspaceID: p.bound.WorkspaceID, SessionID: p.bound.SessionID,
		Profile: applied.Profile.Revision, Override: applied.Override.Revision})
}

func (s *Service) wasCancelled(p *pendingProposal) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return p.cancelled
}

// beginCommit reports whether the commit may start; if so, no Cancel can stop it.
func (s *Service) beginCommit(p *pendingProposal) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.cancelled {
		return false
	}
	p.committing = true
	return true
}
