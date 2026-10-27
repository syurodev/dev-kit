package agenthub

import (
	"context"
	"errors"
	"log"
	"time"

	"devkit/internal/acp"
	"devkit/internal/harness"
	"devkit/internal/preferences"
)

// HarnessSnapshots resolves the harness configuration a runtime is about to
// open with (implemented over harnessconfig.Provider by the composition root).
// The ids come from Agent Hub's own session row, never from a tool argument.
type HarnessSnapshots interface {
	Snapshot(ctx context.Context, sessionID, workspaceID, vendor string) (harness.Snapshot, error)
}

// PolicyEndpoints is an MCPEndpoints that can serve a prepared policy from its
// first call: groups and budgets come from the snapshot that was prepared for
// this launch, so no early tool call ever sees an unrelated full registry. An
// endpoint that does not implement it can only serve today's default, and a
// profile that needs more is refused rather than run unfiltered (H4 implements
// this on the real session host).
type PolicyEndpoints interface {
	MCPEndpoints
	OpenWithPolicy(sessionID string, policy harness.Effective) (MCPEndpoint, error)
}

// Refusals. Fixed host text; the codes are published when H5 exposes them.
var (
	ErrHarnessUnavailable        = &harness.Error{Code: harness.CodeSnapshotUnavailable, Message: "The harness configuration for this session could not be resolved, so it was not started."}
	ErrHarnessNotEnforceable     = &harness.Error{Code: harness.CodeNotEnforceable, Message: "This workspace's harness profile cannot be enforced yet, so the session was not started."}
	ErrHarnessNewSessionRequired = &harness.Error{Code: harness.CodeNewSessionRequired, Message: "The workspace profile changed which servers or skills load. Open a new session to use them."}
)

// appliedHarness is what an opened runtime was actually given. It is published
// only after the runtime opened, with the usage facts, and survives a runtime
// teardown (account move, reload, crash) until the next successful open
// replaces it.
type appliedHarness struct {
	Ref harness.SnapshotRef
	// Enforced is true when the endpoint serves this snapshot's policy. Without
	// it a W or S revision is not claimed in usage attribution.
	Enforced bool
	// Served is the DevKit surface this runtime's guidance was written for. It
	// lives in memory only: a reopen prepares it again.
	Served *DevKitServing
}

// HarnessLifecycle is told when a session is explicitly closed so the override
// (S) it owned is removed through the owning service. Reload, resume, rewind,
// an account move and a crashed process are not closes and never call it.
type HarnessLifecycle interface {
	SessionClosed(ctx context.Context, sessionID string) error
}

// ErrHarnessCleanupFailed is returned by CloseSession when the session closed
// but its override could not be removed. Closing again retries the cleanup.
var ErrHarnessCleanupFailed = errors.New("session closed, but its harness override could not be removed; close it again to retry")

// harnessLaunch is the one prepared value a runtime opens with: the endpoint,
// the snapshot it was built from and whether the endpoint enforces it.
type harnessLaunch struct {
	snapshot      *harness.Snapshot // nil: no harness configured, today's behavior
	enforced      bool
	closeEndpoint func()
	// served is what the endpoint serves under the prepared policy, the one
	// input of host guidance. nil: no policy was prepared (legacy).
	served *DevKitServing
	// reservation is the session's S/lifecycle epoch as this launch saw it before
	// it read S; publishServingLaunch validates it through publication.
	reservation servingReservation
	incarnation string
}

// prepareLaunch is the single place an endpoint and a harness snapshot are
// produced together, before launch arguments, prompt or pre-approved lists are
// finalized. It runs for CreateSession, resumeRuntime (Load, reload, account
// reopen) and both rewind opens. Order: resolve the snapshot, refuse what
// cannot be honestly served, then open the endpoint (staged with the policy
// when it can serve one), then fill the spec from the endpoint. Nothing is
// published here; a failure leaves no endpoint and no record behind. No lock is
// held, and no vendor process exists yet.
//
// A launch whose S (or lifecycle) moved while it was preparing is discarded and
// prepared again, at most launchAttempts times in all; it never sends a prompt or
// picks another account.
func (s *Service) prepareLaunch(ctx context.Context, spec *OpenSpec, sessionID, workspaceID string, vendor AgentType, previous *appliedHarness) (harnessLaunch, error) {
	for attempt := 1; ; attempt++ {
		l, err := s.prepareLaunchOnce(ctx, spec, sessionID, workspaceID, vendor, previous)
		if err != nil && errors.Is(err, ErrHarnessLaunchStale) && attempt < launchAttempts {
			continue
		}
		return l, err
	}
}

func (s *Service) prepareLaunchOnce(ctx context.Context, spec *OpenSpec, sessionID, workspaceID string, vendor AgentType, previous *appliedHarness) (harnessLaunch, error) {
	l := harnessLaunch{closeEndpoint: func() {}}
	// Before S is read: any S commit after this point makes the launch stale.
	var reserveErr error
	l.reservation, reserveErr = s.reserveServing(sessionID)
	if reserveErr != nil {
		return l, reserveErr
	}
	if s.Harness != nil && workspaceID != "" {
		snap, err := s.Harness.Snapshot(ctx, sessionID, workspaceID, string(vendor))
		if err != nil {
			log.Printf("agenthub: harness snapshot for session %s: %v", sessionID, err)
			return l, ErrHarnessUnavailable
		}
		l.snapshot = &snap
	}
	policyEndpoints, canEnforce := s.MCPEndpoints.(PolicyEndpoints)
	if err := s.harnessGate(l.snapshot, previous, canEnforce); err != nil {
		return l, err
	}
	if s.MCPEndpoints == nil {
		return l, nil
	}
	var ep MCPEndpoint
	var err error
	if canEnforce && l.snapshot != nil {
		ep, err = policyEndpoints.OpenWithPolicy(sessionID, l.snapshot.Effective)
		l.enforced = err == nil
	} else {
		ep, err = s.MCPEndpoints.Open(sessionID)
	}
	if err != nil {
		return l, err
	}
	l.closeEndpoint = ep.Close
	if l.enforced {
		// A prepared policy without the endpoint's served inventory cannot be
		// described to the model, and the full guidance is not a safe default.
		if ep.Served == nil {
			ep.Close()
			l.closeEndpoint = func() {}
			l.enforced = false
			log.Printf("agenthub: session %s: the policy endpoint reported no served tools", sessionID)
			return l, ErrHarnessUnavailable
		}
		// Registered with the session's coordinator before the vendor exists, so a
		// concurrent S commit either made this launch stale (refused here) or
		// reaches this endpoint like a published one. A snapshot with no basis, or
		// an endpoint with no publication handle, is never live-updated.
		if ep.Live != nil && l.snapshot.Basis != nil {
			launchID := l.snapshot.Basis.Identity(l.snapshot.Basis.LaunchSession(), l.snapshot.Effective)
			if err := s.stageServing(sessionID, l.reservation, ep.Live, l.snapshot.Basis, launchID, l.snapshot.Effective); err != nil {
				ep.Close()
				l.closeEndpoint = func() {}
				l.enforced = false
				return l, err
			}
			closeEp, incarnation := ep.Close, ep.Live.Incarnation()
			l.incarnation = incarnation
			l.closeEndpoint = func() {
				closeEp()
				s.detachServing(sessionID, incarnation)
			}
		}
		l.served = ep.Served.Clone()
		spec.DevKit = ep.Served.Clone()
	}
	spec.UsageToken = ep.UsageToken
	spec.AllowUnixSockets = []string{ep.SocketPath}
	spec.McpServers = []acp.McpServerStdio{ep.Server}
	spec.GateOwnedMCP = map[string][]string{ep.Server.Name: ep.GateOwnedTools}
	spec.PreApprovedMCP = map[string][]string{ep.Server.Name: ep.PreApprovedTools}
	spec.ReadOnlyPaths = []string{ep.Server.Command}
	return l, nil
}

// applyHarnessLaunch puts the snapshot's launch configuration (deny lists,
// servers, skill folders) into the spec. It runs after applySandboxExtras, which
// reads the vendor customization (V) from Settings; the snapshot's pinned V (or
// the effective merge for a profiled session) replaces that read, so the spec
// and the applied reference come from the same value.
func (s *Service) applyHarnessLaunch(spec *OpenSpec, l harnessLaunch, vendor AgentType) {
	if l.snapshot == nil {
		return
	}
	custom := make(map[string]preferences.AgentCustomization, len(spec.Customizations)+1)
	for k, v := range spec.Customizations {
		custom[k] = v
	}
	if l.snapshot.Ref.Default {
		// No profile: the vendor customization is used verbatim, but the one the
		// snapshot was resolved from, not a second read of Settings. Otherwise an
		// edit between the two reads would launch a configuration the applied
		// reference does not describe.
		v := l.snapshot.Vendor
		custom[string(vendor)] = preferences.AgentCustomization{
			Revision:       v.Revision,
			SkillFolders:   append([]string(nil), v.SkillFolders...),
			DisabledSkills: append([]string(nil), v.DisabledSkills...),
			DisabledMCP:    append([]string(nil), v.DisabledMCP...),
			Servers:        preferencesServers(v.Servers),
		}
		spec.Customizations = custom
		return
	}
	eff := l.snapshot.Effective
	custom[string(vendor)] = preferences.AgentCustomization{
		Revision:       l.snapshot.Ref.Revisions.Vendor,
		SkillFolders:   append([]string(nil), eff.SkillFolders...),
		DisabledSkills: append([]string(nil), eff.DisabledSkills...),
		DisabledMCP:    append([]string(nil), eff.DisabledMCP...),
		Servers:        preferencesServers(eff.Servers),
	}
	spec.Customizations = custom
}

func preferencesServers(in []harness.Server) []preferences.AgentMCPConfig {
	out := make([]preferences.AgentMCPConfig, 0, len(in))
	for _, sv := range in {
		out = append(out, preferences.AgentMCPConfig{Name: sv.Name, Command: sv.Command, Args: append([]string(nil), sv.Args...), URL: sv.URL, EnvKeys: append([]string(nil), sv.EnvKeys...), Enabled: sv.Enabled})
	}
	return out
}

// publishHarnessLocked records what the just-opened runtime was given. The
// caller holds s.mu and has already published the runtime and its usage facts.
// W and S revisions reach usage attribution only when the endpoint enforces the
// snapshot: a revision for a configuration that was not applied is not stated.
func (s *Service) publishHarnessLocked(sessionID string, l harnessLaunch) {
	if l.snapshot == nil {
		delete(s.harnessApplied, sessionID)
		return
	}
	s.harnessApplied[sessionID] = appliedHarness{Ref: l.snapshot.Ref.Clone(), Enforced: l.enforced, Served: l.served.Clone()}
	if l.enforced {
		facts := s.runtimeUsage[sessionID]
		facts.ProfileRevision, facts.OverrideRevision = cloneRevision(l.snapshot.Ref.Revisions.Workspace), cloneRevision(l.snapshot.Ref.Revisions.Session)
		s.runtimeUsage[sessionID] = facts
	}
}

func cloneRevision(r *int64) *int64 {
	if r == nil {
		return nil
	}
	v := *r
	return &v
}

// servedFor is what the session's live runtime was told it can use: nil for a
// legacy launch or a session with no live runtime record.
func (s *Service) servedFor(sessionID string) *DevKitServing {
	// A running enforcing endpoint is the authority: after a live change its
	// acknowledged surface, not the launch's, is what guidance may name.
	if cur := s.servingServed(sessionID); cur != nil {
		return cur
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.harnessApplied[sessionID].Served.Clone()
}

// HarnessApplied reports the configuration the session's runtime was actually
// opened with, if one was prepared. It is an internal read; the UI read model is
// H3d.
func (s *Service) HarnessApplied(sessionID string) (ref harness.SnapshotRef, enforced, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, found := s.harnessApplied[sessionID]
	return a.Ref.Clone(), a.Enforced, found
}

// previousHarness is what the session's runtime was last opened with: the
// in-memory record, else the durable reference (so a restart still knows). A
// reference that cannot be read refuses the launch instead of being ignored.
func (s *Service) previousHarness(ctx context.Context, sessionID string) (*appliedHarness, error) {
	s.mu.Lock()
	if a, ok := s.harnessApplied[sessionID]; ok {
		s.mu.Unlock()
		return &a, nil
	}
	s.mu.Unlock()
	durable, ok := s.store.(HarnessAppliedStore)
	if !ok {
		return nil, nil
	}
	ref, enforced, found, err := durable.GetHarnessApplied(ctx, sessionID)
	if err != nil {
		log.Printf("agenthub: harness reference for session %s: %v", sessionID, err)
		return nil, ErrHarnessUnavailable
	}
	if !found {
		return nil, nil
	}
	return &appliedHarness{Ref: ref, Enforced: enforced}, nil
}

// recordHarnessApplied writes the durable reference of a runtime that has just
// opened, before the runtime is published. If it cannot be written the caller
// closes the runtime: a launch the restart gate could not later recognise is not
// accepted. With no snapshot the row is removed.
func (s *Service) recordHarnessApplied(ctx context.Context, sessionID string, l harnessLaunch) error {
	durable, ok := s.store.(HarnessAppliedStore)
	if !ok {
		return nil
	}
	if l.snapshot == nil {
		return durable.DeleteHarnessApplied(ctx, sessionID)
	}
	return durable.SetHarnessApplied(ctx, sessionID, l.snapshot.Ref, l.enforced, time.Now().UTC())
}

// harnessGate refuses what cannot honestly be launched: a profile that needs the
// server to enforce it when the endpoint cannot, and a reopen that would adopt a
// changed set of executables the person has not seen in a new session.
func (s *Service) harnessGate(snap *harness.Snapshot, previous *appliedHarness, canEnforce bool) error {
	if snap == nil {
		return nil
	}
	if !snap.Effective.ServingIsDefault() && !canEnforce {
		return ErrHarnessNotEnforceable
	}
	// Judged on what changed and why, not on a revision: an executable that is new
	// or altered because the workspace profile's content changed (a retargeted
	// symlink counts) needs a new session; removals, narrowing and changes that
	// come from the vendor customization with W's content unchanged do not.
	if previous != nil && snap.Ref.AdoptsNewExecutables(previous.Ref) {
		return ErrHarnessNewSessionRequired
	}
	return nil
}

// preflightHarness runs the same resolution and gate as prepareLaunch without
// opening anything. A reload calls it before it closes the live runtime, so a
// reload that would be refused leaves the running session exactly as it was.
// The launch repeats the check; if the configuration moved in between, the
// launch is the one that decides.
func (s *Service) preflightHarness(ctx context.Context, sess AgentSession) error {
	if s.Harness == nil || sess.WorkspaceID == "" {
		return nil
	}
	snap, err := s.Harness.Snapshot(ctx, sess.ID, sess.WorkspaceID, string(sess.AgentType))
	if err != nil {
		log.Printf("agenthub: harness snapshot for session %s: %v", sess.ID, err)
		return ErrHarnessUnavailable
	}
	previous, err := s.previousHarness(ctx, sess.ID)
	if err != nil {
		return err
	}
	_, canEnforce := s.MCPEndpoints.(PolicyEndpoints)
	return s.harnessGate(&snap, previous, canEnforce)
}
