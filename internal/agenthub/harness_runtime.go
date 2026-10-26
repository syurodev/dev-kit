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
}

// prepareLaunch is the single place an endpoint and a harness snapshot are
// produced together, before launch arguments, prompt or pre-approved lists are
// finalized. It runs for CreateSession, resumeRuntime (Load, reload, account
// reopen) and both rewind opens. Order: resolve the snapshot, refuse what
// cannot be honestly served, then open the endpoint (staged with the policy
// when it can serve one), then fill the spec from the endpoint. Nothing is
// published here; a failure leaves no endpoint and no record behind. No lock is
// held, and no vendor process exists yet.
func (s *Service) prepareLaunch(ctx context.Context, spec *OpenSpec, sessionID, workspaceID string, vendor AgentType, previous *appliedHarness) (harnessLaunch, error) {
	l := harnessLaunch{closeEndpoint: func() {}}
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
	spec.UsageToken = ep.UsageToken
	spec.AllowUnixSockets = []string{ep.SocketPath}
	spec.McpServers = []acp.McpServerStdio{ep.Server}
	spec.GateOwnedMCP = map[string][]string{ep.Server.Name: ep.GateOwnedTools}
	spec.PreApprovedMCP = map[string][]string{ep.Server.Name: ep.PreApprovedTools}
	spec.ReadOnlyPaths = []string{ep.Server.Command}
	return l, nil
}

// sameWorkspaceRevision is true when W did not move between two snapshots (both
// absent, or the same revision). A changed set of executables with an unmoved W
// comes from the vendor customization edited in Settings, which a reload has
// always adopted; with a moved W the cause is ambiguous, so it needs a new
// session.
func sameWorkspaceRevision(a, b harness.SnapshotRef) bool {
	x, y := a.Revisions.Workspace, b.Revisions.Workspace
	if x == nil || y == nil {
		return x == nil && y == nil
	}
	return *x == *y
}

// applyHarnessLaunch puts the snapshot's launch configuration (deny lists,
// servers, skill folders) into the spec. It runs after applySandboxExtras, which
// loads the vendor customization (V) this replaces for a profiled session. With
// no profile and no override the spec keeps V exactly as before.
func (s *Service) applyHarnessLaunch(spec *OpenSpec, l harnessLaunch, vendor AgentType) {
	if l.snapshot == nil || l.snapshot.Ref.Default {
		return
	}
	eff := l.snapshot.Effective
	custom := make(map[string]preferences.AgentCustomization, len(spec.Customizations)+1)
	for k, v := range spec.Customizations {
		custom[k] = v
	}
	servers := make([]preferences.AgentMCPConfig, 0, len(eff.Servers))
	for _, sv := range eff.Servers {
		servers = append(servers, preferences.AgentMCPConfig{Name: sv.Name, Command: sv.Command, Args: append([]string(nil), sv.Args...), URL: sv.URL, EnvKeys: append([]string(nil), sv.EnvKeys...), Enabled: sv.Enabled})
	}
	custom[string(vendor)] = preferences.AgentCustomization{
		Revision:       l.snapshot.Ref.Revisions.Vendor,
		SkillFolders:   append([]string(nil), eff.SkillFolders...),
		DisabledSkills: append([]string(nil), eff.DisabledSkills...),
		DisabledMCP:    append([]string(nil), eff.DisabledMCP...),
		Servers:        servers,
	}
	spec.Customizations = custom
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
	s.harnessApplied[sessionID] = appliedHarness{Ref: l.snapshot.Ref.Clone(), Enforced: l.enforced}
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

// forgetHarnessApplied drops the record of a session that was explicitly closed.
func (s *Service) forgetHarnessApplied(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	delete(s.harnessApplied, sessionID)
	s.mu.Unlock()
	if durable, ok := s.store.(HarnessAppliedStore); ok {
		return durable.DeleteHarnessApplied(ctx, sessionID)
	}
	return nil
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
	if previous != nil && previous.Ref.ExecutableDigest != snap.Ref.ExecutableDigest && !sameWorkspaceRevision(previous.Ref, snap.Ref) {
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
