package agenthub

import (
	"context"

	"devkit/internal/harness"
)

// HarnessState says how a session's saved harness configuration relates to the
// runtime it is actually running. Empty data never means "applied": a session
// with no record is not_applied, one whose configuration cannot be read is
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
	// Live is always false until a change to an override is published to the
	// running endpoint (H4); a saved change is stored, not applied.
	Live bool
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
	desired, err := s.Harness.Snapshot(ctx, sessionID, sess.WorkspaceID, string(sess.AgentType))
	if err != nil {
		st.State = HarnessUnavailable
		return st, nil
	}
	ref := desired.Ref.Clone()
	st.Desired = &ref
	switch {
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
