package harnessconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"devkit/internal/harness"
)

// Stale session-override repair (R1a-1 of the AP1 review).
//
// F-C repairs the workspace profile (W). A session override (S) can be stale in the
// same way: it names a group the build no longer registers, so the session cannot be
// opened and the ordinary revoke cannot even evaluate the "before" state. This is the
// session-level counterpart, and it follows F-C's rules rather than relaxing the
// resolver: the window only, removal only, the removals derived here from the stored S
// and the registry, always confirmed, bound to the exact S and W revisions, spent on
// use, and an uncertain commit reported as such.
//
// What differs from the workspace repair is the effect. S can only turn groups on, and
// the groups removed are ones this build does not serve, so nothing is served that was
// not and nothing new becomes available: the review says "removal only" and does not
// claim to widen. It is still always asked, whatever the auto-narrowing setting.

// TargetSessionRepair is the Target of a prepared stale session-override repair. No
// Request can carry it.
const TargetSessionRepair Target = "sessionRepair"

// ErrNotStaleGroup refuses a repair of a name the stored session override does not
// hold as a group this build lacks: a registered group, or a name S never had. It is
// the same fixed sentence as an unknown group, so it says nothing about what is stored.
var ErrNotStaleGroup = &harness.Error{Code: harness.CodeGroupUnknown, Message: "One of the requested tool groups does not exist."}

// ErrSessionRepairNeedsProfile: the session's own override is repairable, but the
// workspace profile does not resolve either, so the result could not be checked.
var ErrSessionRepairNeedsProfile = &harness.Error{Code: harness.CodeProfileInvalid, Message: "This workspace's saved profile must be repaired first, in the workspace tool-group settings, before this session's choices can be."}

// InspectStaleOverride lists the groups S enables that the registry does not have,
// sorted and without duplicates. It is pure.
func InspectStaleOverride(o *harness.Override, reg harness.Registry) []string {
	if o == nil || len(o.Groups) == 0 {
		return nil
	}
	known := map[string]bool{}
	for _, t := range reg.Tools {
		known[harness.Group(t.Name)] = true
	}
	var out []string
	for _, g := range o.Groups {
		if !known[g] && !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out
}

// RepairedOverride is a copy of o without exactly the given groups, nil (delete S)
// when none remains, as an ordinary revoke leaves it.
func RepairedOverride(o *harness.Override, stale []string) *harness.Override {
	if o == nil {
		return nil
	}
	out := cloneOverride(o)
	out.Groups = out.Groups[:0:0]
	for _, g := range o.Groups {
		if !slices.Contains(stale, g) {
			out.Groups = append(out.Groups, g)
		}
	}
	if len(out.Groups) == 0 {
		return nil
	}
	return out
}

func overrideDigest(o *harness.Override) string {
	if o == nil {
		return "none"
	}
	sum := sha256.Sum256([]byte(strings.Join(groupsOf(o), "\x00")))
	return hex.EncodeToString(sum[:])
}

// scanStaleSession reads S, W, the registry, the session's vendor and the policy and
// decides what a session repair would be. It writes nothing. The repaired S must
// resolve with the stored W exactly as the launch would, or nothing is offered.
func (s *Service) scanStaleSession(ctx context.Context, workspaceID, sessionID, vendor string) (repairScan, error) {
	var sc repairScan
	o, oStamp, err := s.cfg.Store.GetOverride(ctx, sessionID, workspaceID)
	if err != nil {
		if errors.Is(err, harness.ErrStoreCorrupt) {
			sc.state, sc.message = StaleUnrepairable, messageUnreadable
			return sc, nil
		}
		return sc, storeErr(err)
	}
	sc.o, sc.oStamp = o, oStamp
	if o == nil {
		sc.state = StaleNone
		return sc, nil
	}
	reg := s.cfg.Registry()
	if len(reg.Tools) == 0 {
		sc.state, sc.message = StaleUnrepairable, messageNoRegistry
		return sc, nil
	}
	if o.Schema != harness.SchemaVersion {
		sc.state, sc.message = StaleUnrepairable, messageInvalid
		return sc, nil
	}
	sc.staleGroups = InspectStaleOverride(o, reg)
	if len(sc.staleGroups) == 0 {
		sc.state = StaleNone
		return sc, nil
	}
	w, wStamp, err := s.cfg.Store.GetProfile(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, harness.ErrStoreCorrupt) {
			sc.state, sc.message = StaleUnrepairable, messageUnreadable
			return sc, nil
		}
		return sc, storeErr(err)
	}
	sc.w, sc.stamp = w, wStamp
	candidate := RepairedOverride(o, sc.staleGroups)
	vs, err := s.cfg.Vendors.State(ctx, vendor)
	if err != nil {
		return sc, ErrFactsUnavailable
	}
	env := vs.Environment
	if !vs.FactsComplete {
		env = harness.Environment{}
	}
	if _, err := harness.Resolve(harness.Inputs{Registry: reg, Vendor: vs.Vendor, Environment: env,
		Workspace: withRevision(w, wStamp.Revision), Session: overrideWithRevision(candidate, oStamp.Revision+1), Canonical: s.cfg.Canonical}); err != nil {
		sc.state, sc.message = StaleUnrepairable, messageInvalid
		sc.needsProfile = len(InspectStale(w, reg)) > 0
		return sc, nil
	}
	policy, err := s.cfg.Policy.Policy(ctx)
	if err != nil {
		return sc, ErrDenied
	}
	sc.state, sc.repairedS = StaleRepairable, candidate
	sc.binding = repairBinding{WorkspaceID: workspaceID, W: wStamp, SessionID: sessionID, S: oStamp, RemovedGroups: sc.staleGroups,
		Registry: registryIdentity(reg), Vendors: []string{fmt.Sprintf("%s:%d", vendor, vs.Vendor.Revision)}, Policy: policy.Revision,
		Repaired: overrideDigest(candidate)}
	return sc, nil
}

// PrepareSessionRepair prepares removing the stale groups of one session's stored
// override. The window names the session and the unavailable group(s) it is looking
// at; they only select the repair: each must be one of the stale groups Go finds in
// the stored S, and the removal is every stale group Go finds, all shown in the
// review. A registered group, or a name S does not hold, is refused. It writes
// nothing and asks nobody; Apply asks.
func (s *Service) PrepareSessionRepair(ctx context.Context, workspaceID, sessionID string, named []string) (Prepared, error) {
	caller, err := requireUICaller(ctx)
	if err != nil {
		return Prepared{}, err
	}
	if workspaceID == "" || sessionID == "" || len(named) == 0 {
		return Prepared{}, ErrInvalid
	}
	if exists, err := s.cfg.Directory.WorkspaceExists(ctx, workspaceID); err != nil || !exists {
		return Prepared{}, ErrScopeGone
	}
	scope, err := s.cfg.Directory.SessionScope(ctx, sessionID)
	if err != nil {
		return Prepared{}, ErrScopeGone
	}
	if scope.WorkspaceID != workspaceID {
		return Prepared{}, ErrDenied
	}
	sc, err := s.scanStaleSession(ctx, workspaceID, sessionID, scope.Vendor)
	if err != nil {
		return Prepared{}, err
	}
	switch sc.state {
	case StaleNone:
		// Nothing is stale in S: the name is, to this path, an unknown group.
		return Prepared{}, ErrNotStaleGroup
	case StaleUnrepairable:
		if sc.needsProfile {
			return Prepared{}, ErrSessionRepairNeedsProfile
		}
		return Prepared{}, ErrRepairUnavailable
	}
	for _, n := range named {
		if !slices.Contains(sc.staleGroups, n) {
			return Prepared{}, ErrNotStaleGroup
		}
	}
	if len(sc.staleGroups) > MaxStaleEntries {
		return Prepared{}, ErrRepairTooLarge
	}
	view, err := buildSessionRepairView(sc.staleGroups)
	if err != nil {
		if errors.Is(err, ErrTooLargeToShow) {
			return Prepared{}, ErrRepairTooLarge // not "split it": a repair cannot be split
		}
		return Prepared{}, err
	}
	id, err := newID()
	if err != nil {
		return Prepared{}, err
	}
	b := sc.binding
	p := &pendingProposal{id: id, caller: caller.String(), req: Request{WorkspaceID: workspaceID, SessionID: sessionID, Target: TargetSessionRepair}, view: view, repair: &b}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) >= maxPending {
		return Prepared{}, ErrBusy
	}
	s.pending[id] = p
	return Prepared{ID: id, View: view}, nil
}

// buildSessionRepairView writes the confirmation. It lists every removal and states
// the effect truthfully: a removal, nothing new served.
func buildSessionRepairView(groups []string) (View, error) {
	v := View{Target: TargetSessionRepair, Tier: harness.TierNarrowing.String(), NeedsConfirmation: true}
	var b strings.Builder
	fmt.Fprintf(&b, "Repair this session's saved tool-group choices. %d group(s) this session had turned on are not available in this build, which stops the session from starting. Approving removes exactly these from this session and changes nothing else:", len(groups))
	for _, g := range groups {
		name := displayName(g)
		line := fmt.Sprintf("remove tool group %q from the groups turned on for this session", name)
		v.Changes = append(v.Changes, ViewChange{Field: "session.groups", Key: name, Effect: line, Scope: ScopeStored})
		fmt.Fprintf(&b, "\n- %s", line)
	}
	b.WriteString("\nEffect: removal only. Nothing is turned on and no tool becomes available. If a group with the same name returns in a later build, this session will not have it turned on until you turn it on again.")
	b.WriteString("\nThe workspace profile, other groups and other sessions are not changed. The session is not started or reloaded now; open it again afterwards.")
	b.WriteString("\nIf you would rather restore the missing module or build, cancel and do that instead.")
	if len(v.Changes) > maxViewChanges || b.Len() > maxSummaryBytes {
		return View{}, ErrTooLargeToShow
	}
	v.Summary = b.String()
	return v, nil
}

// applySessionRepair is Apply for a prepared session repair: it always asks, then,
// inside the validation boundary and under the session's coordinator, scans again and
// requires the very same binding before one compare-and-swap write of S. W, other
// sessions, applied references and running endpoints are not touched; a session with a
// running endpoint is refused, because a running session cannot hold a group the
// registry lacks.
func (s *Service) applySessionRepair(ctx context.Context, p *pendingProposal) (Result, error) {
	fail := func(err error) (Result, error) {
		s.auditRepair(p, harness.Applied{}, outcomeOf(err))
		return Result{}, err
	}
	if s.cfg.Confirmer == nil || !s.cfg.Confirmer.Available() || s.cfg.Boundary == nil {
		return fail(ErrConfirmUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return fail(ErrNotConfirmed)
	}
	if err := s.confirm(ctx, p, Confirmation{Target: TargetSessionRepair, Tier: p.view.Tier, Summary: p.view.Summary}); err != nil {
		return fail(err)
	}
	var out committed
	b := p.repair
	run := func(txn LiveTxn) error {
		if txn != nil {
			if t := txn.Target(); t.Active || t.Staged || t.Quarantined {
				return ErrLiveUnavailable
			}
		}
		scope, err := s.cfg.Directory.SessionScope(ctx, b.SessionID)
		if err != nil || scope.WorkspaceID != b.WorkspaceID {
			return ErrScopeGone
		}
		if exists, err := s.cfg.Directory.WorkspaceExists(ctx, b.WorkspaceID); err != nil || !exists {
			return ErrScopeGone
		}
		sc, err := s.scanStaleSession(ctx, b.WorkspaceID, b.SessionID, scope.Vendor)
		if err != nil {
			return refusalFor(err)
		}
		if sc.state != StaleRepairable || len(sc.staleGroups) > MaxStaleEntries || sc.binding.digest() != b.digest() {
			return ErrStale
		}
		if !s.beginCommit(p) {
			return ErrCancelled
		}
		write := harness.StoreWrite{WorkspaceID: b.WorkspaceID, SessionID: b.SessionID,
			Override: &harness.OverrideWrite{Expect: sc.oStamp, Value: sc.repairedS}}
		applied, err := s.cfg.Store.Apply(ctx, write)
		switch {
		case err == nil:
			out.applied, out.commit = applied, CommitCommitted
		case storeCommitUnknown(err):
			rctx, cancel := hostContext(ctx)
			defer cancel()
			_, st, rerr := s.cfg.Store.GetOverride(rctx, b.SessionID, b.WorkspaceID)
			switch {
			case rerr != nil:
				out.commit = CommitUnknown
			case st.Revision == sc.oStamp.Revision+1:
				out.applied, out.commit = harness.Applied{Override: st}, CommitCommitted
			case st.Revision == sc.oStamp.Revision:
				return ErrStale // proven: nothing was written
			default:
				out.commit = CommitUnknown
			}
		default:
			return mapApplyErr(err)
		}
		if txn == nil {
			return nil
		}
		if out.commit == CommitUnknown {
			txn.MarkPending()
			return nil
		}
		// No endpoint is active (checked above), so this only records that S moved:
		// an opening launch that read the old S is made stale.
		pctx, cancel := hostContext(ctx)
		defer cancel()
		durable, stamp, rerr := s.cfg.Store.GetOverride(pctx, b.SessionID, b.WorkspaceID)
		if rerr != nil {
			txn.MarkPending()
			out.pub, out.err = Publication{State: PublicationPending}, rerr
			return nil
		}
		if pub, perr := txn.Publish(stamp, durable); perr != nil {
			out.pub, out.err = Publication{State: PublicationPending}, perr
		} else {
			out.pub = pub
		}
		return nil
	}
	err := s.cfg.Boundary.Hold(ctx, func() error {
		if s.cfg.Live != nil {
			if cerr := s.cfg.Live.Coordinate(ctx, b.SessionID, run); cerr != nil {
				return refusalFor(cerr)
			}
			return nil
		}
		return run(nil)
	})
	if err != nil {
		return fail(err)
	}
	res := Result{Override: out.applied.Override, Commit: out.commit, Publication: out.pub}
	if out.commit == CommitUnknown {
		s.auditRepair(p, harness.Applied{}, "unknown")
		return res, &PublicationError{Result: res, Cause: harness.ErrStoreCommitUnknown}
	}
	s.auditRepair(p, out.applied, "applied")
	if out.err != nil {
		return res, &PublicationError{Result: res, Cause: out.err}
	}
	return res, nil
}
