package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
	"devkit/internal/interaction"
)

// harnessControls is the one application facade for the public group controls
// (H5a). The MCP tools harness.profile.get and harness.profile.propose and the
// window's editor call it and nothing else; it owns no policy of its own. Scope
// comes from the authenticated identity and the persisted session and workspace,
// every change goes through the existing Prepare and Apply (the shared hub asks,
// the H4c coordinator publishes), and reads are detached values.

// Fixed refusals the facade adds. Each is a fixed host sentence.
var (
	// errHarnessUnsupportedVendor: the session's vendor is not one the harness
	// covers, so there is nothing to configure or read.
	errHarnessUnsupportedVendor = &harness.Error{Code: harness.CodeNotAvailable, Message: "This session's agent is not covered by tool-group settings."}
	// errHarnessGroupUnknown: a requested group is not one the server registers.
	errHarnessGroupUnknown = &harness.Error{Code: harness.CodeGroupUnknown, Message: "One of the requested tool groups does not exist."}
	// errHarnessSelectionUnavailable: nobody can choose where to save the change.
	errHarnessSelectionUnavailable = &harness.Error{Code: harness.CodeProfileDenied, Message: "No one can choose where to save a harness change right now."}
	// errHarnessNotSelected: the person did not choose a scope.
	errHarnessNotSelected = &harness.Error{Code: harness.CodeProfileDenied, Message: "No scope was chosen, so nothing was changed."}
)

const (
	maxGroupName     = 64
	maxRequestGroups = 16
)

type harnessControls struct {
	svc      *harnessconfig.Service
	provider *harnessconfig.Provider
	store    harnessconfig.Store
	dir      harnessconfig.Directory
	vendors  harnessconfig.Vendors
	agents   *agenthub.Service
	hub      *interaction.Hub
	registry func() harness.Registry

	mu      sync.Mutex
	targets map[string]harnessconfig.Target // prepared id → target (for the Outcome)
	// asking marks the group requests whose question is on screen.
	asking map[string]bool
}

var _ harnessconfig.Controls = (*harnessControls)(nil)

// Controls is the assembly's public facade. It is built with the graph and is
// not reachable from anywhere until the composition root binds it (HarnessPort).
func (a *HarnessAssembly) Controls() harnessconfig.Controls { return a.controls }

// ---- reads -----------------------------------------------------------------

func (c *harnessControls) Profile(ctx context.Context) (harnessconfig.ProfileView, error) {
	sessionID, scope, err := c.agentScope(ctx)
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	return c.view(ctx, scope.WorkspaceID, sessionID, scope.Vendor)
}

func (c *harnessControls) WorkspaceProfile(ctx context.Context, workspaceID string) (harnessconfig.ProfileView, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.ProfileView{}, err
	}
	if err := c.requireWorkspace(ctx, workspaceID); err != nil {
		return harnessconfig.ProfileView{}, err
	}
	return c.view(ctx, workspaceID, "", "")
}

func (c *harnessControls) SessionProfile(ctx context.Context, workspaceID, sessionID string) (harnessconfig.ProfileView, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.ProfileView{}, err
	}
	scope, err := c.uiSession(ctx, workspaceID, sessionID)
	if err != nil {
		return harnessconfig.ProfileView{}, err
	}
	return c.view(ctx, workspaceID, sessionID, scope.Vendor)
}

// view builds the projection. vendor is empty for a workspace-only read.
func (c *harnessControls) view(ctx context.Context, workspaceID, sessionID, vendor string) (harnessconfig.ProfileView, error) {
	w, wStamp, err := c.store.GetProfile(ctx, workspaceID)
	if err != nil {
		return harnessconfig.ProfileView{}, harnessconfigStoreErr(err)
	}
	v := harnessconfig.ProfileView{WorkspaceID: workspaceID, SessionID: sessionID, ProfileRevision: wStamp.Revision, ProfilePresent: wStamp.Present}

	var o *harness.Override
	if sessionID != "" {
		var oStamp harness.Stamp
		if o, oStamp, err = c.store.GetOverride(ctx, sessionID, workspaceID); err != nil {
			return harnessconfig.ProfileView{}, harnessconfigStoreErr(err)
		}
		v.OverrideRevision, v.OverridePresent = oStamp.Revision, oStamp.Present
	}

	saved := map[string]bool{} // group → on, from the resolved saved configuration
	savedKnown := false
	running := map[string]bool{}
	runningKnown := false
	if sessionID != "" {
		if snap, err := c.provider.Snapshot(ctx, harnessconfig.SnapshotRequest{SessionID: sessionID, WorkspaceID: workspaceID, Vendor: vendor}); err == nil {
			savedKnown = true
			for _, g := range snap.Effective.Groups {
				saved[g.Name] = g.On
			}
		}
		if t := c.agents.ServingTarget(sessionID); t.Acknowledged {
			runningKnown = true
			for _, g := range t.Effective.Groups {
				running[g.Name] = g.On
			}
		}
		v.Session = c.sessionState(ctx, sessionID, o)
	}

	type agg struct{ tools, bytes int }
	groups := map[string]*agg{}
	for _, t := range c.registry().Tools {
		g := harness.Group(t.Name)
		if groups[g] == nil {
			groups[g] = &agg{}
		}
		groups[g].tools++
		groups[g].bytes += t.DefinitionBytes
	}
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		row := harnessconfig.GroupRow{Name: n, Tools: groups[n].tools, DefinitionBytes: groups[n].bytes,
			Workspace: harnessconfig.SettingInherit, Saved: harnessconfig.AvailableUnknown, Running: harnessconfig.AvailableUnknown}
		if w != nil {
			if on, ok := w.ToolGroups[n]; ok {
				row.Workspace = settingOf(on)
			}
		}
		if o != nil {
			row.Session = slices.Contains(o.Groups, n)
		}
		switch {
		case sessionID == "":
			// A workspace read: W alone decides, the default being on.
			row.Saved = availabilityOf(row.Workspace != harnessconfig.SettingOff)
		case savedKnown:
			row.Saved = availabilityOf(saved[n])
		}
		if runningKnown {
			row.Running = availabilityOf(running[n])
		}
		v.Groups = append(v.Groups, row)
	}
	return v, nil
}

func (c *harnessControls) sessionState(ctx context.Context, sessionID string, o *harness.Override) *harnessconfig.SessionState {
	st := &harnessconfig.SessionState{State: string(agenthub.HarnessUnavailable), Overrides: []string{}}
	if o != nil {
		st.Overrides = slices.Clone(o.Groups)
	}
	hs, err := c.agents.HarnessStatus(ctx, sessionID)
	if err != nil {
		return st
	}
	st.State = string(hs.State)
	st.Live = hs.Live
	st.Active, st.Paused, st.Pending = hs.Serving.Active, hs.Serving.Paused, hs.Serving.Pending
	st.NewSessionRequired = hs.State == agenthub.HarnessNewSessionRequired
	st.ReloadRequired = hs.State == agenthub.HarnessReloadRequired || hs.Serving.DiscoveryReloadRequired
	return st
}

// ---- the agent's request ---------------------------------------------------

func (c *harnessControls) Request(ctx context.Context, req harnessconfig.GroupRequest) (harnessconfig.RequestResult, error) {
	sessionID, scope, err := c.agentScope(ctx)
	if err != nil {
		return harnessconfig.RequestResult{}, err
	}
	if len(req.Budgets) > 0 {
		// The two forms are exclusive: a group request chooses a scope with the
		// person, a budget edit has only the workspace.
		if len(req.Groups) > 0 {
			return harnessconfig.RequestResult{}, harnessconfig.ErrInvalid
		}
		return c.requestBudgets(ctx, sessionID, scope, req)
	}
	groups, err := c.validGroups(req.Groups)
	if err != nil {
		return harnessconfig.RequestResult{}, err
	}
	if len(groups) == 0 {
		// Discovery: what is off and what it would cost, with nothing prepared and
		// nobody asked.
		view, err := c.view(ctx, scope.WorkspaceID, sessionID, scope.Vendor)
		if err != nil {
			return harnessconfig.RequestResult{}, err
		}
		view.Groups = offGroups(view.Groups)
		return harnessconfig.RequestResult{Discovery: true, Profile: view}, nil
	}

	// A group the person locked for this session is not asked about: no agent path
	// adds it, and only the window lifts the lock.
	if err := c.refuseLocked(ctx, sessionID, groups); err != nil {
		return harnessconfig.RequestResult{}, err
	}
	// Nobody to ask: refuse before choosing anything, so a headless or server-mode
	// host never even opens a selection.
	if !c.hub.CanAsk() {
		return harnessconfig.RequestResult{}, harnessconfig.ErrConfirmUnavailable
	}
	view, err := c.view(ctx, scope.WorkspaceID, sessionID, scope.Vendor)
	if err != nil {
		return harnessconfig.RequestResult{}, err
	}
	choice, err := c.hub.SelectHarnessScope(ctx, scopeQuestion(view, groups, req.Rationale))
	if err != nil {
		if errors.Is(err, interaction.ErrSelectionRequired) {
			return harnessconfig.RequestResult{}, errHarnessSelectionUnavailable
		}
		return harnessconfig.RequestResult{}, errHarnessNotSelected
	}

	// Only now is the final change built, so the diff the person approves is the
	// one for the scope they chose. A different scope is a different preparation.
	r := harnessconfig.Request{SessionID: sessionID, Rationale: req.Rationale}
	switch choice {
	case interaction.HarnessScopeSession:
		r.Target, r.Patch = harnessconfig.TargetSession, harnessconfig.Patch{EnableGroups: groups}
	case interaction.HarnessScopeWorkspace:
		on := true
		edits := map[string]*bool{}
		for _, g := range groups {
			edits[g] = &on
		}
		r.Target = harnessconfig.TargetWorkspace
		r.Patch = harnessconfig.Patch{EnableGroups: groups, Workspace: &harnessconfig.ProfilePatch{Groups: edits}}
	default:
		return harnessconfig.RequestResult{}, errHarnessNotSelected
	}
	prepared, err := c.svc.Prepare(ctx, r)
	if err != nil {
		return harnessconfig.RequestResult{}, err
	}
	res, err := c.svc.Apply(ctx, prepared.ID)
	out, oerr := harnessconfig.OutcomeOf(r.Target, res, err)
	if oerr != nil {
		// A refusal before the write: nothing was saved, and nothing is claimed.
		return harnessconfig.RequestResult{}, oerr
	}
	if out.Live {
		out.Guidance = c.guidance(sessionID, scope.Vendor, groups)
	}
	if t := c.agents.ServingTarget(sessionID); t.Active {
		out.DiscoveryReloadRequired = c.discoveryReload(sessionID)
	}
	after, verr := c.view(ctx, scope.WorkspaceID, sessionID, scope.Vendor)
	if verr != nil {
		after = harnessconfig.ProfileView{WorkspaceID: scope.WorkspaceID, SessionID: sessionID}
	}
	return harnessconfig.RequestResult{Profile: after, Outcome: &out}, nil
}

func (c *harnessControls) discoveryReload(sessionID string) bool {
	hs, err := c.agents.HarnessStatus(context.Background(), sessionID)
	return err == nil && hs.Serving.DiscoveryReloadRequired
}

// guidance is the replacement note for the model, covering every requested group
// the endpoint actually serves and the launch did not, in one bounded text. It is
// derived by Agent Hub from the acknowledged serving set, so it names nothing that
// is not served.
func (c *harnessControls) guidance(sessionID, vendor string, groups []string) string {
	return c.agents.ReplacementGuidanceFor(sessionID, groups, agenthub.AgentType(vendor))
}

// scopeQuestion is the host-authored text of the selection. The agent's own
// words appear only under an explicit untrusted label.
func scopeQuestion(view harnessconfig.ProfileView, groups []string, rationale string) string {
	byName := map[string]harnessconfig.GroupRow{}
	for _, g := range view.Groups {
		byName[g.Name] = g
	}
	var b strings.Builder
	b.WriteString("An agent asks to turn on tool groups for its session:\n")
	for _, g := range groups {
		row := byName[g]
		fmt.Fprintf(&b, "  • %s (%d tools, about %d bytes of tool definitions)\n", g, row.Tools, row.DefinitionBytes)
	}
	b.WriteString("\nChoose where to save the change. \"This session only\" affects only this session. \"Save to this workspace\" also turns the groups on in the workspace profile; other running sessions keep the profile they opened with until they are reloaded or opened again.")
	b.WriteString("\n\nYou will review the exact change before anything is saved.")
	if r := harnessconfig.CleanRationale(rationale); r != "" {
		b.WriteString("\n\nNote from the agent (untrusted text, not checked by DevKit): ")
		b.WriteString(r)
	}
	return b.String()
}

func offGroups(rows []harnessconfig.GroupRow) []harnessconfig.GroupRow {
	out := []harnessconfig.GroupRow{}
	for _, r := range rows {
		off := r.Running == harnessconfig.AvailableOff || (r.Running == harnessconfig.AvailableUnknown && r.Saved != harnessconfig.AvailableOn)
		if off {
			out = append(out, r)
		}
	}
	return out
}

// ---- the window's changes --------------------------------------------------

func (c *harnessControls) PrepareWorkspaceGroups(ctx context.Context, workspaceID string, edits []harnessconfig.GroupEdit) (harnessconfig.Review, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.Review{}, err
	}
	if err := c.requireWorkspace(ctx, workspaceID); err != nil {
		return harnessconfig.Review{}, err
	}
	if len(edits) == 0 || len(edits) > maxRequestGroups*4 {
		return harnessconfig.Review{}, harnessconfig.ErrInvalid
	}
	known := c.groupSet()
	patch := map[string]*bool{}
	for _, e := range edits {
		if !known[e.Group] {
			return harnessconfig.Review{}, errHarnessGroupUnknown
		}
		if _, dup := patch[e.Group]; dup {
			return harnessconfig.Review{}, harnessconfig.ErrInvalid
		}
		switch e.Setting {
		case harnessconfig.SettingOn:
			on := true
			patch[e.Group] = &on
		case harnessconfig.SettingOff:
			off := false
			patch[e.Group] = &off
		case harnessconfig.SettingInherit:
			patch[e.Group] = nil // removes the entry
		default:
			return harnessconfig.Review{}, harnessconfig.ErrInvalid
		}
	}
	// Only the group entries are edited; every other field of W is left alone.
	req := harnessconfig.Request{WorkspaceID: workspaceID, Target: harnessconfig.TargetWorkspace, Patch: harnessconfig.Patch{Workspace: &harnessconfig.ProfilePatch{Groups: patch}}}
	return c.prepare(ctx, req)
}

func (c *harnessControls) PrepareRevoke(ctx context.Context, workspaceID, sessionID string, groups []string) (harnessconfig.Review, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.Review{}, err
	}
	if _, err := c.uiSession(ctx, workspaceID, sessionID); err != nil {
		return harnessconfig.Review{}, err
	}
	names, err := c.validGroups(groups)
	if errors.Is(err, errHarnessGroupUnknown) {
		// A name the registry lacks may be a group this session's stored override
		// still holds from a build that had it (R1a-1). That is not a revoke the
		// resolver could evaluate: it is the stale-override repair, which removes
		// exactly the stale groups Go finds in the stored S and refuses any name that
		// is not one of them. A request that mixes it with a registered group is
		// still refused.
		return c.prepareStaleOverrideRepair(ctx, workspaceID, sessionID, groups)
	}
	if err != nil {
		return harnessconfig.Review{}, err
	}
	if len(names) == 0 {
		return harnessconfig.Review{}, harnessconfig.ErrInvalid
	}
	// The revoke carries its block: they are written in one transaction when the
	// person confirms, so there is no moment with one and not the other and
	// nothing to undo if the revoke is declined or loses.
	return c.prepare(ctx, harnessconfig.Request{WorkspaceID: workspaceID, SessionID: sessionID, Target: harnessconfig.TargetSession,
		Patch: harnessconfig.Patch{RevokeGroups: names, LockRevoked: true}})
}

// prepareStaleOverrideRepair is PrepareRevoke for names the registry does not have.
// The names only select the repair: they are bounded and must all be unregistered
// here and then stale in the stored S there.
func (c *harnessControls) prepareStaleOverrideRepair(ctx context.Context, workspaceID, sessionID string, groups []string) (harnessconfig.Review, error) {
	known := c.groupSet()
	var named []string
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" || utf8.RuneCountInString(g) > maxGroupName || known[g] {
			return harnessconfig.Review{}, errHarnessGroupUnknown
		}
		if !slices.Contains(named, g) {
			named = append(named, g)
		}
	}
	p, err := c.svc.PrepareSessionRepair(ctx, workspaceID, sessionID, named)
	if err != nil {
		return harnessconfig.Review{}, err
	}
	return c.review(p, harnessconfig.TargetSessionRepair, workspaceID, sessionID), nil
}

func (c *harnessControls) prepare(ctx context.Context, req harnessconfig.Request) (harnessconfig.Review, error) {
	p, err := c.svc.Prepare(ctx, req)
	if err != nil {
		return harnessconfig.Review{}, err
	}
	return c.review(p, req.Target, req.WorkspaceID, req.SessionID), nil
}

// review remembers the target of a prepared id (for the Outcome) and shapes the
// window's Review from the host-authored view.
func (c *harnessControls) review(p harnessconfig.Prepared, target harnessconfig.Target, workspaceID, sessionID string) harnessconfig.Review {
	c.mu.Lock()
	if c.targets == nil {
		c.targets = map[string]harnessconfig.Target{}
	}
	if len(c.targets) >= 64 {
		c.targets = map[string]harnessconfig.Target{}
	}
	c.targets[p.ID] = target
	c.mu.Unlock()
	rev := harnessconfig.Review{ID: p.ID, Target: string(p.View.Target), WorkspaceID: workspaceID, SessionID: sessionID,
		Tier: p.View.Tier, Summary: p.View.Summary, NeedsConfirmation: p.View.NeedsConfirmation, RationaleUntrusted: p.View.RationaleUntrusted,
		Changes: []harnessconfig.ReviewChange{}}
	for _, ch := range p.View.Changes {
		rev.Changes = append(rev.Changes, harnessconfig.ReviewChange(ch))
	}
	return rev
}

// StaleProfile diagnoses a workspace's stored profile. Window only, and it does not
// depend on the profile resolving or on any session.
func (c *harnessControls) StaleProfile(ctx context.Context, workspaceID string) (harnessconfig.StaleDiagnosis, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.StaleDiagnosis{}, err
	}
	if err := c.requireWorkspace(ctx, workspaceID); err != nil {
		return harnessconfig.StaleDiagnosis{}, err
	}
	return c.svc.DiagnoseStale(ctx, workspaceID)
}

// PrepareStaleRepair prepares removing the stale references of a workspace's
// stored profile. The caller names only the workspace; the removals are derived
// from the stored profile and the registry.
func (c *harnessControls) PrepareStaleRepair(ctx context.Context, workspaceID string) (harnessconfig.Review, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.Review{}, err
	}
	if err := c.requireWorkspace(ctx, workspaceID); err != nil {
		return harnessconfig.Review{}, err
	}
	p, err := c.svc.PrepareRepair(ctx, workspaceID)
	if err != nil {
		return harnessconfig.Review{}, err
	}
	return c.review(p, harnessconfig.TargetRepair, workspaceID, ""), nil
}

func (c *harnessControls) Apply(ctx context.Context, id string) (harnessconfig.Outcome, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.Outcome{}, err
	}
	c.mu.Lock()
	target := c.targets[id]
	delete(c.targets, id)
	c.mu.Unlock()
	res, err := c.svc.Apply(ctx, id)
	return harnessconfig.OutcomeOf(target, res, err)
}

func (c *harnessControls) Cancel(ctx context.Context, id string) error {
	if err := c.requireUI(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	delete(c.targets, id)
	c.mu.Unlock()
	return c.svc.Cancel(ctx, id)
}

func (c *harnessControls) Reconcile(ctx context.Context, workspaceID, sessionID string) (harnessconfig.Outcome, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.Outcome{}, err
	}
	if _, err := c.uiSession(ctx, workspaceID, sessionID); err != nil {
		return harnessconfig.Outcome{}, err
	}
	pub, err := c.svc.Reconcile(ctx, sessionID)
	return harnessconfig.OutcomeOfReconcile(pub, err)
}

// ---- identity and scope ----------------------------------------------------

// agentScope is the calling agent's own session and its workspace and vendor,
// from host data. A terminal, the window or an unknown origin is not an agent.
func (c *harnessControls) agentScope(ctx context.Context) (string, harnessconfig.SessionScope, error) {
	caller, ok := identity.FromContext(ctx)
	if !ok || !caller.IsAgent() {
		return "", harnessconfig.SessionScope{}, harnessconfig.ErrDenied
	}
	scope, err := c.dir.SessionScope(ctx, caller.SessionID)
	if err != nil {
		return "", harnessconfig.SessionScope{}, harnessconfig.ErrScopeGone
	}
	if exists, err := c.dir.WorkspaceExists(ctx, scope.WorkspaceID); err != nil || !exists {
		return "", harnessconfig.SessionScope{}, harnessconfig.ErrScopeGone
	}
	if !slices.Contains(c.vendors.Names(), scope.Vendor) {
		return "", harnessconfig.SessionScope{}, errHarnessUnsupportedVendor
	}
	return caller.SessionID, scope, nil
}

// requireUI is the Go-side half of "the window only". The bridge checks too; a
// terminal on the shared socket, or an agent, never gets these operations.
func (c *harnessControls) requireUI(ctx context.Context) error {
	caller, ok := identity.FromContext(ctx)
	if !ok || caller.Kind != identity.KindUI {
		return harnessconfig.ErrDenied
	}
	return nil
}

func (c *harnessControls) requireWorkspace(ctx context.Context, workspaceID string) error {
	if workspaceID == "" {
		return harnessconfig.ErrInvalid
	}
	if exists, err := c.dir.WorkspaceExists(ctx, workspaceID); err != nil || !exists {
		return harnessconfig.ErrScopeGone
	}
	return nil
}

// uiSession checks that the named session belongs to the named workspace, from
// the persisted row, and that its vendor is covered.
func (c *harnessControls) uiSession(ctx context.Context, workspaceID, sessionID string) (harnessconfig.SessionScope, error) {
	if workspaceID == "" || sessionID == "" {
		return harnessconfig.SessionScope{}, harnessconfig.ErrInvalid
	}
	scope, err := c.dir.SessionScope(ctx, sessionID)
	if err != nil {
		return harnessconfig.SessionScope{}, harnessconfig.ErrScopeGone
	}
	if scope.WorkspaceID != workspaceID {
		return harnessconfig.SessionScope{}, harnessconfig.ErrDenied
	}
	if err := c.requireWorkspace(ctx, workspaceID); err != nil {
		return harnessconfig.SessionScope{}, err
	}
	if !slices.Contains(c.vendors.Names(), scope.Vendor) {
		return harnessconfig.SessionScope{}, errHarnessUnsupportedVendor
	}
	return scope, nil
}

// ---- validation helpers ----------------------------------------------------

func (c *harnessControls) groupSet() map[string]bool {
	out := map[string]bool{}
	for _, t := range c.registry().Tools {
		out[harness.Group(t.Name)] = true
	}
	return out
}

// validGroups normalizes requested group names: bounded, printable, known to the
// server's registry, sorted and de-duplicated.
func (c *harnessControls) validGroups(in []string) ([]string, error) {
	if len(in) > maxRequestGroups {
		return nil, harnessconfig.ErrInvalid
	}
	known := c.groupSet()
	seen := map[string]bool{}
	out := []string{}
	for _, g := range in {
		g = strings.TrimSpace(g)
		if g == "" || utf8.RuneCountInString(g) > maxGroupName {
			return nil, harnessconfig.ErrInvalid
		}
		if !known[g] {
			return nil, errHarnessGroupUnknown
		}
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out, nil
}

func settingOf(on bool) harnessconfig.GroupSetting {
	if on {
		return harnessconfig.SettingOn
	}
	return harnessconfig.SettingOff
}

func availabilityOf(on bool) harnessconfig.Availability {
	if on {
		return harnessconfig.AvailableOn
	}
	return harnessconfig.AvailableOff
}

// harnessconfigStoreErr maps a read failure to a fixed refusal: a row for another
// workspace is a gone scope, anything else (a corrupt document, a failed read) asks
// for a repair and never falls back to defaults.
func harnessconfigStoreErr(err error) error {
	if errors.Is(err, harness.ErrStoreScope) {
		return harnessconfig.ErrScopeGone
	}
	return harnessconfig.ErrStoredUnreadable
}
