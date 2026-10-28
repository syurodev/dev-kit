package agenthub

import (
	"context"

	"devkit/internal/harness"
)

// HarnessState says how a session's saved harness configuration relates to the
// runtime it is actually running. Empty data never means "applied": a session
// with no record is not_applied (or new_session_required when its saved
// configuration owns a workspace executable: a reopen would be refused, O-2), one whose configuration cannot be read is
// unavailable, and one with no harness provider is unconfigured.
type HarnessState string

const (
	HarnessUnconfigured       HarnessState = "unconfigured"
	HarnessUnavailable        HarnessState = "unavailable"
	HarnessNotApplied         HarnessState = "not_applied"
	HarnessCurrent            HarnessState = "current"
	HarnessReloadRequired     HarnessState = "reload_required"
	HarnessNewSessionRequired HarnessState = "new_session_required"
)

// HarnessStatus separates three things the UI must not blur:
// Desired is the saved configuration resolved right now (workspace profile,
// session override and vendor customization), Applied is the reference of what
// the runtime was actually opened with, and State is their relationship.
type HarnessStatus struct {
	State   HarnessState
	Desired *harness.SnapshotRef
	Applied *harness.SnapshotRef
	// Enforced is true when the applied runtime's MCP endpoint serves the applied
	// policy. A false value means the revisions are not claimed in attribution.
	Enforced bool
	// AppliedFactsComplete is false when the vendor's own servers and skills could
	// not be read in full when the runtime was opened: the applied reference then
	// describes DevKit's configuration, not a verified account of the vendor's.
	AppliedFactsComplete bool
	// Live is true only while the session's CURRENT endpoint is open, serves the
	// exact identity it acknowledged and has nothing pending. Historical
	// enforcement, a stored change, an endpoint still opening, a paused or closed
	// one, and a list-changed notification are none of them enough. It is a
	// point-in-time observation, not a promise.
	Live bool
	// Serving separates what the running endpoint serves from what the launch was
	// given (Applied, a durable reference live S never rewrites) and from what the
	// saved configuration resolves to now (Desired).
	Serving HarnessServing
	// State compares Desired with the LAUNCH reference. A live S publication does
	// not make a pending workspace or vendor change look applied: a session whose
	// endpoint already serves the saved S still reports reload_required while W
	// or V moved, and new_session_required while executables moved.
}

// HarnessStatus is an internal read model. It does not authorize on behalf of a
// caller, opens nothing, starts no process and calls no model; the transport that
// exposes it (H5) owns who may ask. The session's own row supplies workspace and
// vendor.
func (s *Service) HarnessStatus(ctx context.Context, sessionID string) (HarnessStatus, error) {
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return HarnessStatus{}, err
	}
	if s.Harness == nil || sess.WorkspaceID == "" {
		return HarnessStatus{State: HarnessUnconfigured}, nil
	}
	var st HarnessStatus
	applied, err := s.previousHarness(ctx, sessionID)
	if err != nil {
		return HarnessStatus{State: HarnessUnavailable}, nil
	}
	if applied != nil {
		ref := applied.Ref.Clone()
		st.Applied, st.Enforced, st.AppliedFactsComplete = &ref, applied.Enforced, applied.Ref.FactsComplete
	}
	// What the endpoint serves is observed even when the desired configuration
	// cannot be resolved.
	st.Serving = s.servingStatus(sessionID, nil)
	st.Live = st.Serving.Acknowledged
	desired, err := s.Harness.Snapshot(ctx, sessionID, sess.WorkspaceID, string(sess.AgentType))
	if err != nil {
		st.State = HarnessUnavailable
		return st, nil
	}
	ref := desired.Ref.Clone()
	st.Desired = &ref
	st.Serving = s.servingStatus(sessionID, &desired)
	st.Live = st.Serving.Acknowledged
	switch {
	case applied == nil && len(desired.Ref.WorkspaceOwned) > 0:
		// The same fact the reopen gate judges (O-2): no baseline and a workspace-owned
		// executable in what the session would open with. Say it before the launch
		// refuses; nothing is claimed about the profile being approved.
		st.State = HarnessNewSessionRequired
	case applied == nil:
		st.State = HarnessNotApplied
	case desired.Ref.Equal(applied.Ref):
		st.State = HarnessCurrent
	case desired.Ref.AdoptsNewExecutables(applied.Ref):
		st.State = HarnessNewSessionRequired
	default:
		st.State = HarnessReloadRequired
	}
	return st, nil
}
