package harnessconfig

import (
	"context"
	"errors"
	"time"

	"devkit/internal/harness"
	"devkit/internal/identity"
)

// Result says what was stored and what became of the requester's running
// session. A returned error with a populated Result (see PublicationError) means
// the change IS stored.
type Result struct {
	Profile, Override harness.Stamp
	// Live is true only when the requester's CURRENT endpoint installed and
	// acknowledged the exact committed serving identity. A stored change, an
	// opening endpoint, a paused one or a notification is never enough.
	Live bool
	// ReloadRequired is true for a workspace change: other sessions keep the
	// profile they opened with until reloaded.
	ReloadRequired bool
	Commit         CommitState
	Publication    Publication
}

// publishTimeout bounds the work that finishes a committed change on a host
// context, independent of the requester's context.
const publishTimeout = 5 * time.Second

// Apply asks the person, rechecks every input and commits the proposal once.
// The order is the contract:
//
//  1. take the proposal (single use: it is spent whatever happens next);
//  2. fail closed if nobody can answer, then ask with no lock and no
//     transaction held;
//  3. inside the validation boundary, and for an S change under the session's
//     coordinator, re-read identity, scope, policy, V, W, S, the vendor facts and
//     the running endpoint, rebuild the stored and live evaluations, and require
//     the same binding the person saw;
//  4. close the running endpoint's admission (calls already admitted finish
//     under the policy they loaded; nothing is drained);
//  5. commit W and S in one short transaction with the expected stamps;
//  6. on a proven rollback reopen that same admission; on an unknown outcome
//     read the stamps back and keep admission closed if still unsure;
//  7. on a commit, install the policy and discovery resolved from the pinned
//     launch basis and the latest durable S, and release admission only when the
//     endpoint acknowledges exactly that.
//
// Any refusal leaves W and S unchanged. A later retry needs a fresh Prepare. A
// committed change whose publication failed returns its Result with a
// *PublicationError; it is reconciled, never proposed again.
func (s *Service) Apply(ctx context.Context, id string) (Result, error) {
	caller, ok := identity.FromContext(ctx)
	p, err := s.take(caller, ok, id)
	if err != nil {
		return Result{}, err
	}
	// The proposal is spent from here on, success or not.
	defer s.drop(id)
	if p.repair != nil {
		return s.applyRepair(ctx, p)
	}

	fail := func(err error) (Result, error) {
		s.audit(p, harness.Applied{}, outcomeOf(err), "")
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
		if err := s.confirm(ctx, p, Confirmation{
			Target: p.view.Target, Tier: p.view.Tier, Summary: p.view.Summary, RationaleUntrusted: p.view.RationaleUntrusted,
		}); err != nil {
			return fail(err)
		}
	}

	return s.runCommit(ctx, caller, p)
}

// runCommit is everything Apply does once the question is settled: hold the V
// boundary, coordinate the session, commit, publish and audit. ApplyDelegated
// shares it, so a delegated change meets exactly the same checks.
func (s *Service) runCommit(ctx context.Context, caller identity.Caller, p *pendingProposal) (Result, error) {
	fail := func(err error) (Result, error) {
		s.audit(p, harness.Applied{}, outcomeOf(err), "")
		return Result{}, err
	}
	var out committed
	err := s.cfg.Boundary.Hold(ctx, func() error {
		// An S change on a session is coordinated with every other writer of S,
		// every endpoint replacement and the session's close. Order: the V boundary
		// (held here), then the session coordinator, then the host's own locks.
		if s.cfg.Live != nil && p.bound.SessionID != "" && p.bound.write.Override != nil {
			if cerr := s.cfg.Live.Coordinate(ctx, p.bound.SessionID, func(txn LiveTxn) error {
				return s.commit(ctx, caller, p, txn, &out)
			}); cerr != nil {
				return refusalFor(cerr)
			}
			return nil
		}
		return s.commit(ctx, caller, p, nil, &out)
	})
	if err != nil {
		return fail(err)
	}

	res := Result{
		Profile: out.applied.Profile, Override: out.applied.Override,
		ReloadRequired: p.bound.write.Profile != nil,
		Commit:         out.commit, Publication: out.pub,
		Live: out.pub.State == PublicationInstalled,
	}
	s.audit(p, out.applied, "applied", string(out.pub.State))
	if out.err != nil {
		return res, &PublicationError{Result: res, Cause: out.err}
	}
	return res, nil
}

// confirm asks the person about a pending proposal through the shared confirmer. A
// Cancel while the question is open closes it and refuses the commit.
func (s *Service) confirm(ctx context.Context, p *pendingProposal, c Confirmation) error {
	askCtx, abort := context.WithCancel(ctx)
	defer abort()
	s.mu.Lock()
	p.abort = abort
	cancelled := p.cancelled
	s.mu.Unlock()
	if cancelled {
		return ErrCancelled
	}
	if err := s.cfg.Confirmer.Confirm(askCtx, c); err != nil {
		if s.wasCancelled(p) {
			return ErrCancelled
		}
		return ErrNotConfirmed
	}
	if err := ctx.Err(); err != nil {
		return ErrNotConfirmed
	}
	return nil
}

// committed is what the commit step leaves for Apply to report.
type committed struct {
	applied harness.Applied
	commit  CommitState
	pub     Publication
	err     error // a publication failure; the write itself is durable (or unknown)
}

// commit runs steps 3 to 7 of Apply. Its error is a refusal before or instead of
// a commit; after the commit point it fills out and returns nil.
func (s *Service) commit(ctx context.Context, caller identity.Caller, p *pendingProposal, txn LiveTxn, out *committed) error {
	read := s.liveReaderFor(ctx)
	if txn != nil {
		read = func(string) (LiveTarget, error) { return txn.Target(), nil }
	}
	ev, policy, err := s.evaluate(ctx, caller, p.req, read)
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

	// Admission closes BEFORE the commit, so no call can be admitted under the old
	// policy after the new one is durable. A Cancel still wins until beginCommit.
	closed := false
	if ev.Live != nil {
		if txn == nil || txn.Quarantine() != nil {
			return ErrStale
		}
		closed = true
	}
	// The point of no return: after this Cancel is too late. Checked and set
	// together under the lock, so a Cancel either lands before (and wins) or is
	// told it is too late.
	if !s.beginCommit(p) {
		if closed {
			_ = txn.Restore()
		}
		return ErrCancelled
	}

	applied, err := s.cfg.Store.Apply(ctx, ev.write)
	switch {
	case err == nil:
		out.commit = CommitCommitted
	case storeCommitUnknown(err):
		// Not a rollback: the write may be durable. Read the stamps back on a host
		// context. A proven absence is a rollback; anything else keeps admission
		// closed.
		rctx, cancel := hostContext(ctx)
		verdict, got := s.readBack(rctx, ev)
		cancel()
		switch verdict {
		case readBackNotCommitted:
			if closed {
				_ = txn.Restore()
			}
			return ErrStale
		case readBackCommitted:
			applied, out.commit = got, CommitCommitted
		default:
			out.commit = CommitUnknown
			if txn != nil {
				txn.MarkPending()
			}
			out.pub = Publication{State: PublicationPending}
			out.err = err
			return nil
		}
	default:
		// A proven rollback: nothing is durable.
		if closed {
			_ = txn.Restore()
		}
		return mapApplyErr(err)
	}
	out.applied = applied

	if txn == nil || ev.write.Override == nil {
		return nil // nothing to publish: W only, or no coordinated session
	}
	pctx, cancel := hostContext(ctx)
	defer cancel()
	durable, stamp, rerr := s.cfg.Store.GetOverride(pctx, ev.SessionID, ev.WorkspaceID)
	if rerr != nil {
		// Committed, but the latest durable S cannot be read: install nothing.
		txn.MarkPending()
		out.pub = Publication{State: PublicationPending}
		out.err = rerr
		return nil
	}
	pub, perr := txn.Publish(stamp, durable)
	out.pub = pub
	if perr != nil {
		out.pub.State = PublicationPending
		out.err = perr
	}
	return nil
}

type readBackVerdict int

const (
	readBackUnknown readBackVerdict = iota
	readBackCommitted
	readBackNotCommitted
)

// readBack decides an unknown commit from the durable stamps. The write was
// compare-and-swap against Expect, so a stamp of Expect+1 is this write and
// Expect is its absence; anything else proves nothing.
func (s *Service) readBack(ctx context.Context, ev evaluation) (readBackVerdict, harness.Applied) {
	var got harness.Applied
	committed, absent := true, true
	if w := ev.write.Profile; w != nil {
		_, st, err := s.cfg.Store.GetProfile(ctx, ev.WorkspaceID)
		if err != nil {
			return readBackUnknown, got
		}
		got.Profile = st
		committed = committed && st.Revision == w.Expect.Revision+1
		absent = absent && st.Revision == w.Expect.Revision
	}
	if w := ev.write.Override; w != nil {
		_, st, err := s.cfg.Store.GetOverride(ctx, ev.SessionID, ev.WorkspaceID)
		if err != nil {
			return readBackUnknown, got
		}
		got.Override = st
		committed = committed && st.Revision == w.Expect.Revision+1
		absent = absent && st.Revision == w.Expect.Revision
	}
	switch {
	case committed:
		return readBackCommitted, got
	case absent:
		return readBackNotCommitted, got
	}
	return readBackUnknown, got
}

// hostContext is a bounded context that outlives the requester's cancellation:
// finishing a committed change must not depend on the caller still waiting.
func hostContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
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

func (s *Service) audit(p *pendingProposal, applied harness.Applied, outcome, publication string) {
	if s.cfg.Audit == nil {
		return
	}
	s.cfg.Audit(Event{Outcome: outcome, Tier: p.view.Tier, Target: p.view.Target, Caller: p.caller,
		WorkspaceID: p.bound.WorkspaceID, SessionID: p.bound.SessionID,
		Profile: applied.Profile.Revision, Override: applied.Override.Revision, Publication: publication,
		Grantor: p.grantor, RequestID: p.requestID})
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
