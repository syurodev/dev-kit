package app

import (
	"context"
	"errors"
	"reflect"
	"sort"

	"github.com/google/uuid"

	"devkit/internal/agenthub"
	"devkit/internal/agentwork"
	"devkit/internal/harness"
	"devkit/internal/identity"
)

// This is the only place that knows both agentwork (a stdlib leaf that never
// imports the harness) and the harness. It maps a resolved snapshot to
// agentwork.HarnessFacts and answers the HarnessHost questions for a managed
// child. It reads; it never creates a session, enables a group, saves a
// profile, chooses a model or account, sends a prompt or opens a runtime, and
// nothing in production calls it yet (no dispatcher, no public surface).

// managedRoleGroups is the one table of tool groups a role needs to do its work
// as the managed-work contract describes it: a child records its branch and
// result with tasks.*, reports to its parent with agenthub.sendMessage, and a
// code-editing role needs git.push, which only pushes a branch a task records.
// No role needs every group, and the harness proposal tool is always served, so
// none is required. A change is one edit here with its tests.
var managedRoleGroups = map[string][]string{
	"coordinator": {"agenthub", "tasks"},
	"reviewer":    {"agenthub", "tasks"},
	"implementer": {"agenthub", "git", "tasks"},
	"integrator":  {"agenthub", "git", "tasks"},
}

// managedWriterResolver is the existing ownership check (identity, workspace,
// parent equals the group coordinator, registered folder scope, worktree
// identity, grants contained in the checkout); it is reused, not copied.
type managedWriterResolver interface {
	ResolveManagedWriter(ctx context.Context, attempt agentwork.Attempt) (agentwork.ManagedWriterBinding, error)
}

// managedHarnessHub is the narrow Agent Hub read surface the adapter uses.
type managedHarnessHub interface {
	GetStatus(ctx context.Context, id string) (agenthub.AgentSession, error)
	HarnessStatus(ctx context.Context, sessionID string) (agenthub.HarnessStatus, error)
	ServedTools(sessionID string) ([]string, bool)
	PermissionModeOf(ctx context.Context, sessionID string) (string, bool)
	ReadPermissionMode(ctx context.Context, sessionID string) (string, agenthub.ModeRead)
}

type managedHarnessHost struct {
	// hub and snapshots are read at call time and never captured at wiring, so
	// the order of wiring relative to ComposeHarness does not matter and a hub
	// without a harness answers "not configured".
	hub       func() managedHarnessHub
	snapshots func() agenthub.HarnessSnapshots
	ownership managedWriterResolver
	roles     map[string][]string
	probeID   func() string
}

var _ agentwork.HarnessHost = (*managedHarnessHost)(nil)

func newManagedHarnessHost(c *Container, ownership managedWriterResolver) *managedHarnessHost {
	return &managedHarnessHost{
		hub: func() managedHarnessHub {
			if c == nil || c.AgentHub == nil {
				return nil
			}
			return c.AgentHub
		},
		snapshots: func() agenthub.HarnessSnapshots {
			if c == nil || c.AgentHub == nil {
				return nil
			}
			return c.AgentHub.Harness
		},
		ownership: ownership, roles: managedRoleGroups, probeID: uuid.NewString,
	}
}

func managedHarnessRefusal(code string, groups ...string) error {
	return &agentwork.HarnessRefusal{Code: code, Groups: groups}
}

// RequiredGroups is the role's requirement from the table. An unknown role is an
// error (the service reads that as unavailable), never an empty requirement.
func (h *managedHarnessHost) RequiredGroups(ctx context.Context, role string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	groups, ok := h.roles[role]
	if !ok || len(groups) == 0 {
		return nil, managedHarnessRefusal(agentwork.HarnessUnavailable)
	}
	return append([]string(nil), groups...), nil
}

func managedHarnessFacts(ref harness.SnapshotRef) agentwork.HarnessFacts {
	return agentwork.HarnessFacts{
		SourceSchema: ref.Schema, Vendor: ref.Vendor, WorkspaceID: ref.WorkspaceID, VendorRevision: ref.Revisions.Vendor,
		WorkspaceRevision: managedCloneRevision(ref.Revisions.Workspace), SessionRevision: managedCloneRevision(ref.Revisions.Session),
		Fingerprint: ref.Fingerprint, ExecutableDigest: ref.ExecutableDigest, FactsComplete: ref.FactsComplete, Default: ref.Default,
	}
}

func managedCloneRevision(r *int64) *int64 {
	if r == nil {
		return nil
	}
	v := *r
	return &v
}

// ResolveIntended resolves what a fresh child of this vendor would get: the
// workspace profile and the vendor customization, with no session override. It
// asks for a throwaway session id that no session owns, so S is absent by
// construction and no other session's S (the coordinator's included) is ever
// read. The workspace comes from the attempt's group, never from a caller.
func (h *managedHarnessHost) ResolveIntended(ctx context.Context, scope agentwork.PackageScope, child agentwork.IntendedChild) (agentwork.HarnessResolution, error) {
	if h.snapshots == nil {
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessNotConfigured)
	}
	snapshots := h.snapshots()
	if isNilInterface(snapshots) {
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessNotConfigured)
	}
	if scope.WorkspaceID == "" || child.Vendor == "" {
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessUnavailable)
	}
	required, err := h.RequiredGroups(ctx, child.Role)
	if err != nil {
		return agentwork.HarnessResolution{}, err
	}
	probe := h.probeID()
	snap, err := snapshots.Snapshot(ctx, probe, scope.WorkspaceID, child.Vendor)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return agentwork.HarnessResolution{}, ctxErr
		}
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessUnavailable)
	}
	ref := snap.Ref
	if ref.SessionID != probe || ref.WorkspaceID != scope.WorkspaceID || ref.Vendor != child.Vendor || ref.Fingerprint == "" || ref.ExecutableDigest == "" {
		// The resolver answered about something else, or with an unreadable reference.
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessUnavailable)
	}
	var on []string
	registry := map[string]bool{}
	for _, g := range snap.Effective.Groups {
		registry[g.Name] = true
		if g.On {
			on = append(on, g.Name)
		}
	}
	for _, g := range required {
		if !registry[g] {
			// A required name this build's registry lacks is a table error, not a
			// group the person can turn on.
			return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessUnavailable)
		}
	}
	sort.Strings(on)
	return agentwork.HarnessResolution{Facts: managedHarnessFacts(ref), Groups: on}, nil
}

// ResolveActual describes the child that exists: after the ownership checks
// (identity, scope, grants) and the mode rule, what its runtime was opened with
// (Applied; the saved Desired only once it has drifted, so the service sees the
// difference) and what its endpoint serves now.
func (h *managedHarnessHost) ResolveActual(ctx context.Context, attempt agentwork.Attempt) (agentwork.HarnessResolution, error) {
	if h.hub == nil || h.ownership == nil || attempt.SessionID == "" {
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessUnavailable)
	}
	hub := h.hub()
	if isNilInterface(hub) {
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessNotConfigured)
	}
	// 1. Identity, workspace, parent, folder scope, worktree and grants.
	if _, err := h.ownership.ResolveManagedWriter(ctx, attempt); err != nil {
		switch {
		case ctx.Err() != nil:
			return agentwork.HarnessResolution{}, ctx.Err()
		case errors.Is(err, agentwork.ErrManagedHostUnavailable):
			return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessUnavailable)
		}
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessChildScope)
	}
	// 2. The child's mode must not be wider than its coordinator's now.
	internal := identity.WithCaller(ctx, identity.UI())
	child, err := hub.GetStatus(internal, attempt.SessionID)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return agentwork.HarnessResolution{}, ctxErr
	}
	if err != nil || child.ParentSessionID == nil || *child.ParentSessionID == "" {
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessChildScope)
	}
	parentMode, parentRead := hub.ReadPermissionMode(ctx, *child.ParentSessionID)
	childMode, childRead := hub.ReadPermissionMode(ctx, attempt.SessionID)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return agentwork.HarnessResolution{}, ctxErr
	}
	switch {
	case parentRead == agenthub.ModeUnreadable || childRead == agenthub.ModeUnreadable:
		// A mode that cannot be read is not "no mode concept": fail closed.
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessChildMode)
	case parentRead == agenthub.ModeNone:
		// The coordinator has no permission-mode concept: nothing limits the child.
	case childRead == agenthub.ModeNone:
		// The coordinator has a mode and the child has none to compare with.
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessChildMode)
	default:
		if ok, _ := agenthub.ModeNotWiderThan(parentMode, childMode); !ok {
			return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessChildMode)
		}
	}
	// 3. The harness state of the running child.
	st, err := hub.HarnessStatus(ctx, attempt.SessionID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return agentwork.HarnessResolution{}, ctxErr
		}
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessUnavailable)
	}
	switch st.State {
	case agenthub.HarnessUnconfigured:
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessNotConfigured)
	case agenthub.HarnessUnavailable, agenthub.HarnessNotApplied:
		// Not applied: no record of what the runtime was opened with, so nothing
		// can be said about it; that is not a configuration to fix.
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessUnavailable)
	}
	applied := st.Applied
	if applied == nil || applied.Fingerprint == "" || applied.ExecutableDigest == "" {
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessUnavailable)
	}
	if !st.Serving.Active || !st.Serving.Acknowledged || st.Serving.Paused || st.Serving.Pending {
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessEndpointNotLive)
	}
	// The facts the service judges. Nothing has drifted: what the runtime was
	// opened with (Applied). After a saved W, V or S change (reload or new session
	// required) the saved configuration no longer is that, so the facts are what
	// it resolves to now (Desired): the service then finds the difference from
	// the bound facts and names it in its fixed order (an override, then a group
	// that is off, then a changed configuration) instead of this adapter picking
	// the verdict first.
	source := *applied
	drifted := st.State == agenthub.HarnessReloadRequired || st.State == agenthub.HarnessNewSessionRequired
	if drifted {
		if st.Desired == nil || st.Desired.Fingerprint == "" || st.Desired.ExecutableDigest == "" {
			return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessUnavailable)
		}
		source = *st.Desired
	}
	facts := managedHarnessFacts(source)
	// A session override committed live after launch is on the endpoint though not
	// in the launch reference; the child has an S of its own either way.
	if live := st.Serving.Applied.Session; live.Present && facts.SessionRevision == nil && live.Revision > 0 {
		r := live.Revision
		facts.SessionRevision = &r
	}
	if drifted && facts.SessionRevision == nil && reflect.DeepEqual(facts, managedHarnessFacts(*applied)) {
		// Reported as changed but the compared facts look alike: refuse here rather
		// than let a change the comparison cannot see pass.
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessConfigChanged)
	}
	// 4. What the endpoint serves now, so a group revoked after launch is seen.
	tools, live := hub.ServedTools(attempt.SessionID)
	if !live {
		return agentwork.HarnessResolution{}, managedHarnessRefusal(agentwork.HarnessEndpointNotLive)
	}
	seen := map[string]bool{}
	var groups []string
	for _, tool := range tools {
		if g := harness.Group(tool); !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	sort.Strings(groups)
	return agentwork.HarnessResolution{Facts: facts, Groups: groups}, nil
}
