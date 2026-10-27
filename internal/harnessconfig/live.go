package harnessconfig

import (
	"context"
	"errors"
	"fmt"

	"devkit/internal/harness"
)

// Live is the port to the running session's enforcing endpoint, implemented by
// the composition root over Agent Hub and the MCP session host. This package
// never imports either: it describes what it needs to bind an approval to the
// effect on a running session and to publish a committed change to it.
//
// A nil Live keeps the stored-only behaviour: nothing here then changes a
// running session and a result is never Live.
type Live interface {
	// Target reads the session's current endpoint without changing anything. It is
	// used while preparing and while evaluating; Apply re-reads it under the
	// session's coordinator through LiveTxn.Target.
	Target(ctx context.Context, sessionID string) (LiveTarget, error)
	// Coordinate runs fn while holding the session's commit/publication
	// coordinator, which every writer of S, every endpoint replacement and the
	// session's close share. Lock order: the V boundary is entered first and this
	// second; fn never waits for a person or a vendor. It returns fn's error.
	Coordinate(ctx context.Context, sessionID string, fn func(LiveTxn) error) error
	// Reconcile brings the session's endpoint to the latest durable S that read
	// returns, under the coordinator. It starts no runtime and sends no prompt; with
	// no active endpoint it does nothing.
	Reconcile(ctx context.Context, sessionID string, read func(context.Context) (*harness.Override, harness.Stamp, error)) (Publication, error)
}

// LiveTarget is a point-in-time observation of a session's endpoint. The zero
// value means no enforcing endpoint (the session is not running, or its endpoint
// cannot enforce a policy). Basis is nil only then.
type LiveTarget struct {
	Active bool
	Staged bool // an enforcing endpoint whose vendor runtime is still opening
	// Incarnation is opaque and unique to one endpoint instance. A replacement
	// endpoint, even one serving an identical policy, has another.
	Incarnation string
	// Epoch counts the session's S commits and lifecycle fences; a change means
	// something that matters to an approval happened.
	Epoch uint64
	Basis *harness.ServingBasis
	// Serving is what the endpoint has acknowledged it serves.
	Serving harness.ServingIdentity
	// Quarantined means admission is closed: a publication is in flight or failed
	// and awaits reconciliation.
	Quarantined bool
}

// LiveTxn is the session's coordinator while it is held. Its methods act only on
// the endpoint it observed (Target), never on a replacement.
type LiveTxn interface {
	Target() LiveTarget
	// Quarantine closes the endpoint's admission before the commit. Calls already
	// admitted finish under the policy they loaded; nothing is drained.
	Quarantine() error
	// Restore reopens admission under the prior policy after a PROVEN rollback. It
	// does nothing unless the endpoint and the operation are still the current ones.
	Restore() error
	// MarkPending records that S moved (or may have) without installing anything:
	// the epoch advances and an endpoint's admission stays closed until a
	// reconciliation installs the latest durable S.
	MarkPending()
	// Publish installs the policy resolved from the pinned basis and the given
	// durable S (the latest stamp, a tombstone included) on the endpoint, and
	// releases the quarantine only after the endpoint acknowledges exactly that.
	// With no active endpoint it only records that S moved and reports no
	// publication. It does not use the caller's context, so a cancelled request
	// cannot leave the endpoint half-updated.
	Publish(stamp harness.Stamp, durable *harness.Override) (Publication, error)
}

// PublicationState says what happened to the running endpoint after a commit.
type PublicationState string

const (
	// PublicationNone: there was no active enforcing endpoint, or the change did
	// not touch S. The next permitted launch reads the stored value.
	PublicationNone PublicationState = ""
	// PublicationInstalled: the current endpoint installed the exact committed
	// serving identity and acknowledged it.
	PublicationInstalled PublicationState = "installed"
	// PublicationStaged: the endpoint installed the policy, but the vendor runtime
	// has not been published and may still fail or be discarded. It is not live.
	PublicationStaged PublicationState = "staged"
	// PublicationPending: the change is stored but the endpoint is unavailable (its
	// admission stays closed) until it is reconciled with the latest durable S.
	PublicationPending PublicationState = "pending"
)

// Publication is the endpoint's account of a publish.
type Publication struct {
	State   PublicationState
	Serving harness.ServingIdentity
	// ToolsChanged is true when the set of tools the endpoint lists changed.
	ToolsChanged bool
	// NotificationScheduled means the SDK scheduled a list-changed notification.
	// It is not an acknowledgement and says nothing about what the vendor saw.
	NotificationScheduled bool
}

// CommitState is what is known about the durable write.
type CommitState string

const (
	CommitNone      CommitState = ""
	CommitCommitted CommitState = "committed"
	// CommitUnknown: the store could not say, and the read-back failed too.
	CommitUnknown CommitState = "unknown"
)

// ErrLiveUnavailable: the running session's endpoint is in a state a change
// cannot be judged against (paused for reconciliation, or not serving what is
// stored). No synthetic default is used; the proposal is refused.
var ErrLiveUnavailable = &harness.Error{Code: harness.CodeNotAvailable, Message: "This session's tool access is being updated or is not in step with its stored configuration. Try again shortly."}

// ErrPublicationPending is what a committed change returns when the running
// endpoint could not be confirmed: the change IS stored, the endpoint serves
// nothing until it is reconciled.
var ErrPublicationPending = &harness.Error{Code: harness.CodeSnapshotUnavailable, Message: "The change was saved, but this session's tool access is paused until it is reconciled."}

// PublicationError carries the committed result of a change whose publication
// failed. The proposal is spent; retry publication by reconciling, not by
// proposing again.
type PublicationError struct {
	Result Result
	Cause  error
}

func (e *PublicationError) Error() string { return ErrPublicationPending.Error() }
func (e *PublicationError) Unwrap() error { return ErrPublicationPending }

// liveEffect is the bound effect on one running session: the pinned launch
// facts, what the endpoint serves now, and the exact difference the change makes
// to it. Only exported fields reach the digest. It is computed from the pinned
// basis and durable S, never from the saved W.
type liveEffect struct {
	Incarnation string
	Staged      bool
	Epoch       uint64
	Serving     harness.ServingIdentity
	// Pinned launch facts the effect is judged under.
	PinnedVendor    int64
	PinnedWorkspace harness.Stamp
	LaunchSession   harness.Stamp
	Executable      string
	Before, After   string // fingerprints of the live effective configurations
	Tier            harness.Tier
	Changes         []harness.Change
	Groups          []liveGroup
}

// liveGroup is what the change does to one tool group of the running session.
type liveGroup struct {
	Name                 string
	BeforeOn, AfterOn    bool
	Tools                int
	BeforeMin, BeforeMax int // smallest and largest effective result budget before the change
	AfterMin, AfterMax   int // and after it
}

// liveEffectFor judges an S change against the running session: pinned basis
// plus durable S before and after. It fails closed when the endpoint is not in a
// state that can be judged.
func liveEffectFor(lt LiveTarget, o, o2 *harness.Override, oStamp harness.Stamp) (*liveEffect, harness.Classification, error) {
	var none harness.Classification
	if lt.Basis == nil || lt.Quarantined || lt.Serving.Session != oStamp {
		return nil, none, ErrLiveUnavailable
	}
	before, err := lt.Basis.Resolve(overrideWithRevision(o, oStamp.Revision))
	if err != nil || before.Fingerprint != lt.Serving.Fingerprint {
		return nil, none, ErrLiveUnavailable
	}
	after, err := lt.Basis.Resolve(overrideWithRevision(o2, oStamp.Revision+1))
	if err != nil {
		return nil, none, invalid(err)
	}
	class := harness.Classify(before, after)
	le := &liveEffect{
		Incarnation: lt.Incarnation, Staged: lt.Staged, Epoch: lt.Epoch, Serving: lt.Serving,
		PinnedVendor: lt.Basis.VendorRevision(), PinnedWorkspace: lt.Basis.WorkspaceStamp(), LaunchSession: lt.Basis.LaunchSession(),
		Executable: lt.Basis.ExecutableDigest(),
		Before:     before.Fingerprint, After: after.Fingerprint,
		Tier: class.Tier, Changes: class.Changes,
	}
	reg := lt.Basis.Registry()
	for _, g := range after.Groups {
		var b harness.GroupState
		for _, bg := range before.Groups {
			if bg.Name == g.Name {
				b = bg
			}
		}
		if b.On == g.On {
			continue
		}
		lo, hi := budgetRange(before, reg, g.Name)
		alo, ahi := budgetRange(after, reg, g.Name)
		le.Groups = append(le.Groups, liveGroup{Name: g.Name, BeforeOn: b.On, AfterOn: g.On, Tools: g.Tools,
			BeforeMin: lo, BeforeMax: hi, AfterMin: alo, AfterMax: ahi})
	}
	return le, class, nil
}

// budgetRange is the smallest and largest effective result budget among a
// group's tools that are cut (exempt tools have no budget). 0, 0 when none.
func budgetRange(e harness.Effective, reg harness.Registry, group string) (lo, hi int) {
	for _, t := range reg.Tools {
		if harness.Group(t.Name) != group {
			continue
		}
		b, ok := e.Budget(t.Name)
		if !ok || b.Exempt {
			continue
		}
		if lo == 0 || b.Bytes < lo {
			lo = b.Bytes
		}
		if b.Bytes > hi {
			hi = b.Bytes
		}
	}
	return lo, hi
}

// liveLine is one sentence of the live section of a confirmation. Opposite
// effects stay separate lines: nothing here is merged with the stored effect.
func liveLine(g liveGroup) string {
	state := func(on bool) string {
		if on {
			return "available"
		}
		return "unavailable"
	}
	line := fmt.Sprintf("tool group %s: %s → %s (%d tools", g.Name, state(g.BeforeOn), state(g.AfterOn), g.Tools)
	if g.AfterOn && g.AfterMax > 0 {
		if g.AfterMin == g.AfterMax {
			line += fmt.Sprintf(", results up to %d bytes", g.AfterMax)
		} else {
			line += fmt.Sprintf(", results %d to %d bytes", g.AfterMin, g.AfterMax)
		}
	}
	return line + ")"
}

// Reconcile is the bounded, idempotent repair of a session whose stored S and
// running endpoint may disagree (a failed or unconfirmed publication, an unknown
// commit outcome). It resolves and installs the LATEST durable S, never a
// captured older candidate, and cannot bring a closed or deleted session back:
// with no scope or no active endpoint it only reports that nothing is running.
// It makes no retry of its own.
func (s *Service) Reconcile(ctx context.Context, sessionID string) (Publication, error) {
	if s.cfg.Live == nil || sessionID == "" {
		return Publication{}, nil
	}
	sc, err := s.cfg.Directory.SessionScope(ctx, sessionID)
	if err != nil {
		return Publication{}, ErrScopeGone
	}
	pub, err := s.cfg.Live.Reconcile(ctx, sessionID, func(c context.Context) (*harness.Override, harness.Stamp, error) {
		return s.cfg.Store.GetOverride(c, sessionID, sc.WorkspaceID)
	})
	if err != nil {
		pub.State = PublicationPending
		return pub, &PublicationError{Result: Result{Publication: pub}, Cause: err}
	}
	return pub, nil
}

// liveReader reads the observation an evaluation is bound to: Prepare reads the
// port, Apply reads the coordinator it holds.
type liveReader func(sessionID string) (LiveTarget, error)

func (s *Service) liveReaderFor(ctx context.Context) liveReader {
	if s.cfg.Live == nil {
		return nil
	}
	return func(sessionID string) (LiveTarget, error) { return s.cfg.Live.Target(ctx, sessionID) }
}

// storeCommitUnknown reports a write whose outcome the store could not prove.
func storeCommitUnknown(err error) bool { return errors.Is(err, harness.ErrStoreCommitUnknown) }
