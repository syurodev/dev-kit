package agenthub

import "devkit/internal/harness"

// What a running session serves is not what it was launched with, and neither is
// what the saved configuration resolves to today. Three values stay apart:
//
//	launch-applied: the durable reference and executable baseline of the launch
//	                (harnessApplied, SetHarnessApplied). Live S never touches it.
//	serving-applied: what the current endpoint acknowledged (liveServing).
//	desired: the saved V, W and S resolved now (HarnessStatus.Desired).

// HarnessServing is the point-in-time account of a session's live endpoint. It is
// an observation, not a promise the endpoint stays open after the read.
type HarnessServing struct {
	// Active: an enforcing endpoint is running (or launching) for the session.
	Active bool
	// Launch is the identity of the configuration the endpoint was opened with.
	Launch harness.ServingIdentity
	// Applied is what the endpoint acknowledged serving; zero when not Active.
	Applied harness.ServingIdentity
	// Acknowledged: the endpoint is open, serves exactly Applied and nothing is
	// pending. HarnessStatus.Live is this and nothing weaker.
	Acknowledged bool
	// Paused: admission is closed. Pending: a stored change awaits reconciliation.
	Paused, Pending bool
	// Current: Applied is exactly what the saved V, W and S resolve to now.
	Current bool
	// DiscoveryReloadRequired: the endpoint serves tools the launch did not offer.
	// Whether the vendor refreshes its tool list is not known, so the model is told
	// to expect them only after a refresh or a reload; the server itself already
	// accepts calls to them (under the normal permission flow).
	DiscoveryReloadRequired bool
}

// servedFrom is the DevKit surface eff serves, from the launch's pinned registry:
// the same derivation the endpoint uses when it opens.
func servedFrom(b *harness.ServingBasis, eff harness.Effective) *DevKitServing {
	reg := b.Registry()
	names := make([]string, 0, len(reg.Tools))
	for _, t := range reg.Tools {
		if eff.Served(t.Name) {
			names = append(names, t.Name)
		}
	}
	return NewDevKitServing(names)
}

// servingServed is the surface the session's endpoint last acknowledged, or nil
// when no endpoint is running. New turn guidance is written from it, never from
// the launch value, so guidance and enforcement cannot disagree after a live
// change. The value handed to the adapters at launch is not touched.
func (s *Service) servingServed(sessionID string) *DevKitServing {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	rt := c.rt
	if rt == nil || !rt.acked || rt.ep.State().Closed {
		return nil
	}
	return servedFrom(rt.basis, rt.eff)
}

// noteServedFacts records the S revision the endpoint now serves in the facts
// that FUTURE turn admissions copy. A turn already started keeps the facts it
// started with, and a pending or paused endpoint is never recorded. The caller
// holds the session's coordinator; Service.mu is taken inside it (the only
// order).
func (s *Service) noteServedFacts(sessionID string, rt *liveServing) {
	if !rt.published || !rt.acked || rt.pending {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.runtimeUsage[sessionID]
	if !ok {
		return
	}
	// Replace, never mutate: a turn that copied the old struct shares the pointer.
	f.OverrideRevision = nil
	if rt.serving.Session.Present {
		r := rt.serving.Session.Revision
		f.OverrideRevision = &r
	}
	s.runtimeUsage[sessionID] = f
}

// servingStatus observes the live endpoint against the desired snapshot.
func (s *Service) servingStatus(sessionID string, desired *harness.Snapshot) HarnessServing {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	v := c.viewLocked()
	if !v.Active {
		return HarnessServing{}
	}
	rt := c.rt
	out := HarnessServing{
		Active:       true,
		Launch:       rt.basis.Identity(rt.basis.LaunchSession(), rt.basis.Launch()),
		Applied:      v.Serving,
		Acknowledged: v.Acknowledged,
		Paused:       rt.ep.State().Paused,
		Pending:      v.Pending,
	}
	launch := rt.basis.Launch()
	for _, t := range rt.basis.Registry().Tools {
		if rt.eff.Served(t.Name) && !launch.Served(t.Name) {
			out.DiscoveryReloadRequired = true
			break
		}
	}
	if desired != nil && desired.Basis != nil && v.Acknowledged {
		d := desired.Basis
		b := rt.basis
		out.Current = v.Serving.Session == d.LaunchSession() &&
			b.WorkspaceStamp() == d.WorkspaceStamp() && b.VendorRevision() == d.VendorRevision() &&
			b.ExecutableDigest() == d.ExecutableDigest()
	}
	return out
}

// ReplacementGuidance is the bounded text that tells the model a newly enabled
// group is available. It is returned only when the endpoint has ACKNOWLEDGED a
// serving set that contains the group and the launch did not, and it is derived
// from the whole new served set so a multi-group change stays truthful. It
// grants nothing: the launch permission lists are fixed until a reload and each
// tool follows the normal permission flow. Empty otherwise; the caller says a
// reload is required instead.
func (s *Service) ReplacementGuidance(sessionID, group string, naming AgentType) string {
	return s.ReplacementGuidanceFor(sessionID, []string{group}, naming)
}

// ReplacementGuidanceFor is ReplacementGuidance for every group of a change at
// once. Only groups the acknowledged endpoint serves now and the launch did not
// count; a group that is hidden, unregistered (an optional module that is not
// wired) or already served at launch contributes nothing. Sections shared by two
// groups appear once, whole, under one header.
func (s *Service) ReplacementGuidanceFor(sessionID string, groups []string, naming AgentType) string {
	c := s.servingCoord(sessionID)
	c.mu.Lock()
	defer c.mu.Unlock()
	rt := c.rt
	if rt == nil || !rt.published || !rt.acked || rt.pending || rt.ep.State().Closed || rt.ep.State().Paused {
		return ""
	}
	launch := rt.basis.Launch()
	var enabledNow []string
	for _, group := range groups {
		for _, t := range rt.basis.Registry().Tools {
			if harness.Group(t.Name) == group && rt.eff.Served(t.Name) && !launch.Served(t.Name) {
				enabledNow = append(enabledNow, group)
				break
			}
		}
	}
	if len(enabledNow) == 0 {
		return ""
	}
	return replacementGuidanceFor(namingFor(naming), servedFrom(rt.basis, rt.eff), enabledNow)
}
