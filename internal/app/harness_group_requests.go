package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
	"devkit/internal/interaction"
)

// The child tool-group request ledger joins Agent Hub (the ledger and its state
// machine) with harnessconfig (writing S) and the shared hub (asking the person).
// This is the only place that sees all three; Agent Hub never imports the
// harness packages. See docs/superpowers/specs/2026-10-30-child-tool-group-requests-design.md.

var (
	_ harnessconfig.GroupRequestControls = (*harnessControls)(nil)
	_ agenthub.GroupRequestHost          = (*harnessControls)(nil)
)

// errNoConfirmedEndpoint: the host cannot say what a session serves because it
// has no acknowledged endpoint.
var errNoConfirmedEndpoint = errors.New("no confirmed endpoint")

// KnownGroup implements agenthub.GroupRequestHost.
func (c *harnessControls) KnownGroup(name string) bool { return c.groupSet()[name] }

// ServedGroups implements agenthub.GroupRequestHost: the groups of the tools the
// session's endpoint serves now, and only while that endpoint is acknowledged.
func (c *harnessControls) ServedGroups(_ context.Context, sessionID string) ([]string, error) {
	tools, live := c.agents.ServedTools(sessionID)
	if !live {
		return nil, errNoConfirmedEndpoint
	}
	seen := map[string]bool{}
	out := []string{}
	for _, t := range tools {
		g := harness.Group(t)
		if !seen[g] {
			seen[g] = true
			out = append(out, g)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (c *harnessControls) groupRequestView(r agenthub.GroupRequest) harnessconfig.GroupRequestView {
	return harnessconfig.GroupRequestView{
		ID: r.ID, ChildID: r.ChildID, Groups: append([]string(nil), r.Groups...), Route: string(r.Route), State: string(r.State),
		Revision: r.Revision, ReasonUntrusted: harnessconfig.CleanRationale(r.Reason), Resolver: r.Resolver, Outcome: r.Outcome,
		CreatedAt: r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func (c *harnessControls) groupResult(r agenthub.GroupRequest, payload any) harnessconfig.GroupRequestResult {
	res := harnessconfig.GroupRequestResult{Request: c.groupRequestView(r)}
	if out, ok := payload.(*harnessconfig.Outcome); ok {
		res.Outcome = out
	}
	return res
}

// CreateGroupRequest implements harnessconfig.GroupRequestControls.
func (c *harnessControls) CreateGroupRequest(ctx context.Context, groups []string, route, reason string) (harnessconfig.GroupRequestResult, error) {
	if _, _, err := c.agentScope(ctx); err != nil {
		return harnessconfig.GroupRequestResult{}, err
	}
	r, err := c.agents.CreateGroupRequest(ctx, groups, agenthub.GroupRequestRoute(route), reason)
	if err != nil {
		return harnessconfig.GroupRequestResult{}, err
	}
	if r.State == agenthub.GroupRequestPendingUser {
		return c.askUser(ctx, r)
	}
	return c.groupResult(r, nil), nil
}

// askUser puts one question to the person for a request that waits on them, and
// settles the request with the answer. A request is asked about once at a time.
func (c *harnessControls) askUser(ctx context.Context, r agenthub.GroupRequest) (harnessconfig.GroupRequestResult, error) {
	if !c.beginAsking(r.ID) {
		return c.groupResult(r, nil), nil // already on screen
	}
	defer c.endAsking(r.ID)
	if !c.hub.CanAsk() {
		next, _, err := c.agents.AnswerGroupRequestAsUser(ctx, r.ID, r.Revision, agenthub.UserAnswer{NoPrompt: true}, nil)
		c.agents.NoticeGroupRequestResolved(next)
		return c.groupResult(next, nil), err
	}
	choice, err := c.hub.SelectGroupRequestAnswer(ctx, c.groupRequestQuestion(ctx, r))
	switch {
	case errors.Is(err, interaction.ErrSelectionRequired):
		next, _, aerr := c.agents.AnswerGroupRequestAsUser(ctx, r.ID, r.Revision, agenthub.UserAnswer{NoPrompt: true}, nil)
		c.agents.NoticeGroupRequestResolved(next)
		return c.groupResult(next, nil), aerr
	case errors.Is(err, interaction.ErrConfirmationDenied):
		choice = interaction.GroupRequestDecline // closing the question declines this request only
	case err != nil:
		// The asker went away (its call was cancelled): the request stays open and
		// the window can still answer it.
		return c.groupResult(r, nil), err
	}
	return c.settleUser(ctx, r, choice)
}

func (c *harnessControls) beginAsking(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.asking == nil {
		c.asking = map[string]bool{}
	}
	if c.asking[id] {
		return false
	}
	c.asking[id] = true
	return true
}

func (c *harnessControls) endAsking(id string) {
	c.mu.Lock()
	delete(c.asking, id)
	c.mu.Unlock()
}

// groupRequestQuestion is host-authored. The session's own words appear only
// under an explicit untrusted label.
func (c *harnessControls) groupRequestQuestion(ctx context.Context, r agenthub.GroupRequest) string {
	var b strings.Builder
	label := r.ChildID
	if len(label) > 8 {
		label = label[:8]
	}
	fmt.Fprintf(&b, "Session %s asks to turn on these tool groups for itself only:\n", label)
	reg := c.registry()
	for _, g := range r.Groups {
		tools, bytes := 0, 0
		for _, t := range reg.Tools {
			if harness.Group(t.Name) == g {
				tools++
				bytes += t.DefinitionBytes
			}
		}
		fmt.Fprintf(&b, "  • %s (%d tools, about %d bytes of tool definitions)\n", g, tools, bytes)
	}
	b.WriteString("\n\"Review and allow\" shows the exact change before anything is saved. It affects this session only: not its parent, its siblings or the workspace. ")
	b.WriteString("\"Decline this request\" refuses only this request. \"Decline and block\" also stops any agent from adding these groups to this session until you lift the block.")
	if reason := harnessconfig.CleanRationale(r.Reason); reason != "" {
		b.WriteString("\n\nNote from the session (untrusted text, not checked by DevKit): ")
		b.WriteString(reason)
	}
	return b.String()
}

// settleUser records the person's answer. Approving writes through the ordinary
// Prepare and Apply, which show the exact change and ask again.
func (c *harnessControls) settleUser(ctx context.Context, r agenthub.GroupRequest, choice string) (harnessconfig.GroupRequestResult, error) {
	ans := agenthub.UserAnswer{Approve: choice == interaction.GroupRequestApprove, Block: choice == interaction.GroupRequestBlock}
	next, payload, err := c.agents.AnswerGroupRequestAsUser(ctx, r.ID, r.Revision, ans, c.userApply)
	c.afterGroupRequest(next, err)
	return c.groupResult(next, payload), err
}

func (c *harnessControls) afterGroupRequest(r agenthub.GroupRequest, err error) {
	if r.ID != "" && !r.Open() && r.Resolver != "system" {
		c.agents.NoticeGroupRequestResolved(r)
	}
	_ = err
}

// userApply writes the groups after the person chose to review them. The window
// identity names the child; the shared hub shows the exact change and asks.
func (c *harnessControls) userApply(ctx context.Context, req agenthub.GroupRequest, _ []string) (agenthub.GroupApplyResult, error) {
	uctx := identity.WithCaller(ctx, identity.UI())
	scope, err := c.dir.SessionScope(uctx, req.ChildID)
	if err != nil {
		return agenthub.GroupApplyResult{}, harnessconfig.ErrScopeGone
	}
	r := harnessconfig.Request{WorkspaceID: scope.WorkspaceID, SessionID: req.ChildID, Target: harnessconfig.TargetSession,
		Patch: harnessconfig.Patch{EnableGroups: req.Groups}, Rationale: req.Reason}
	prepared, err := c.svc.Prepare(uctx, r)
	if err != nil {
		return agenthub.GroupApplyResult{}, err
	}
	res, err := c.svc.Apply(uctx, prepared.ID)
	return c.groupApplyResult(req, scope, res, err)
}

// groupApplyResult turns a harnessconfig result into the ledger's: a refusal
// before the write is an error and nothing is committed; a write whose
// publication failed is committed and carries its outcome.
func (c *harnessControls) groupApplyResult(req agenthub.GroupRequest, scope harnessconfig.SessionScope, res harnessconfig.Result, err error) (agenthub.GroupApplyResult, error) {
	out, oerr := harnessconfig.OutcomeOf(harnessconfig.TargetSession, res, err)
	if oerr != nil {
		return agenthub.GroupApplyResult{}, oerr
	}
	if out.Live {
		out.Guidance = c.guidance(req.ChildID, scope.Vendor, req.Groups)
	}
	if t := c.agents.ServingTarget(req.ChildID); t.Active {
		out.DiscoveryReloadRequired = c.discoveryReload(req.ChildID)
	}
	// A commit whose outcome is unknown may or may not be durable: the request
	// stays open, and a later answer finds the groups served (superseded) or
	// writes them again under the same compare-and-swap.
	return agenthub.GroupApplyResult{Payload: &out, Committed: out.Commit == harnessconfig.CommitCommitted}, nil
}

// GroupRequestStatus implements harnessconfig.GroupRequestControls.
func (c *harnessControls) GroupRequestStatus(ctx context.Context, id string) (harnessconfig.GroupRequestView, error) {
	r, err := c.agents.GetGroupRequest(ctx, id)
	if err != nil {
		return harnessconfig.GroupRequestView{}, err
	}
	return c.groupRequestView(r), nil
}

// ListGroupRequests implements harnessconfig.GroupRequestControls.
func (c *harnessControls) ListGroupRequests(ctx context.Context) ([]harnessconfig.GroupRequestView, error) {
	list, err := c.agents.ListGroupRequests(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]harnessconfig.GroupRequestView, 0, len(list))
	for _, r := range list {
		out = append(out, c.groupRequestView(r))
	}
	return out, nil
}

// ResolveGroupRequest implements harnessconfig.GroupRequestControls: the direct
// parent's answer.
func (c *harnessControls) ResolveGroupRequest(ctx context.Context, id string, revision int64, decision string) (harnessconfig.GroupRequestResult, error) {
	caller, ok := identity.FromContext(ctx)
	if !ok || !caller.IsAgent() {
		return harnessconfig.GroupRequestResult{}, harnessconfig.ErrDenied
	}
	apply := func(actx context.Context, req agenthub.GroupRequest, grantable []string) (agenthub.GroupApplyResult, error) {
		scope, err := c.dir.SessionScope(actx, req.ChildID)
		if err != nil {
			return agenthub.GroupApplyResult{}, harnessconfig.ErrScopeGone
		}
		res, err := c.svc.ApplyDelegated(actx, harnessconfig.Delegation{WorkspaceID: scope.WorkspaceID, ChildSessionID: req.ChildID,
			Groups: req.Groups, GrantorSessionID: caller.SessionID, RequestID: req.ID, Grantable: grantable})
		return c.groupApplyResult(req, scope, res, err)
	}
	next, payload, err := c.agents.ResolveGroupRequestAsParent(ctx, id, revision, agenthub.GroupDecision(decision), apply)
	if err == nil && next.State == agenthub.GroupRequestPendingUser {
		// Forwarded: the same row now waits on the person, who is asked once.
		go func() { _, _ = c.askUser(context.WithoutCancel(ctx), next) }()
	}
	c.afterGroupRequest(next, err)
	return c.groupResult(next, payload), err
}

// AnswerGroupRequest implements harnessconfig.GroupRequestControls: the window's
// answer to a request waiting on the person.
func (c *harnessControls) AnswerGroupRequest(ctx context.Context, id string, revision int64, answer string) (harnessconfig.GroupRequestResult, error) {
	if err := c.requireUI(ctx); err != nil {
		return harnessconfig.GroupRequestResult{}, err
	}
	if answer != interaction.GroupRequestApprove && answer != interaction.GroupRequestDecline && answer != interaction.GroupRequestBlock {
		return harnessconfig.GroupRequestResult{}, harnessconfig.ErrInvalid
	}
	r, err := c.agents.GetGroupRequest(ctx, id)
	if err != nil {
		return harnessconfig.GroupRequestResult{}, err
	}
	r.Revision = revision // the answer is bound to what the person saw
	return c.settleUser(ctx, r, answer)
}

// GroupLocks and SetGroupLock implement harnessconfig.GroupRequestControls.
func (c *harnessControls) GroupLocks(ctx context.Context, sessionID string) ([]string, error) {
	return c.agents.GroupLocks(ctx, sessionID)
}

func (c *harnessControls) SetGroupLock(ctx context.Context, sessionID, group string, locked bool) error {
	return c.agents.SetGroupLock(ctx, sessionID, group, locked)
}

// refuseLocked is the lock check every agent-origin path shares: a group the
// person locked for the session cannot be added by an agent, however it asks.
func (c *harnessControls) refuseLocked(ctx context.Context, sessionID string, groups []string) error {
	locks, err := c.agents.GroupLocks(ctx, sessionID)
	if err != nil {
		if errors.Is(err, agenthub.ErrGroupRequestUnavailable) {
			return nil
		}
		return err
	}
	for _, g := range groups {
		if slices.Contains(locks, g) {
			return agenthub.ErrGroupBlocked
		}
	}
	return nil
}

// PlanChildGroups implements harnessconfig.GroupRequestControls.
func (c *harnessControls) PlanChildGroups(ctx context.Context, explicit []string, given bool) ([]string, error) {
	parentID, scope, err := c.agentScope(ctx)
	if err != nil {
		// A parent whose vendor or workspace tool groups do not cover has nothing
		// to inherit; that is not a reason to refuse the child. Anything else is.
		if !given && (errors.Is(err, harnessconfig.ErrScopeGone) || errors.Is(err, errHarnessUnsupportedVendor)) {
			return nil, nil
		}
		return nil, err
	}
	if !given {
		// Inherit: a snapshot of the parent's own session override. What the
		// workspace profile serves needs no copy; the child's profile gives it.
		o, _, err := c.store.GetOverride(ctx, parentID, scope.WorkspaceID)
		if err != nil {
			return nil, harnessconfig.ErrScopeGone
		}
		out := []string{}
		if o != nil {
			known := c.groupSet()
			for _, g := range o.Groups {
				if known[g] {
					out = append(out, g)
				}
			}
		}
		sort.Strings(out)
		return out, nil
	}
	names, err := c.validGroups(explicit)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return names, nil
	}
	served, err := c.ServedGroups(ctx, parentID)
	if err != nil {
		return nil, agenthub.ErrGroupNotGrantable
	}
	for _, g := range names {
		if !slices.Contains(served, g) {
			return nil, agenthub.ErrGroupNotGrantable
		}
	}
	return names, nil
}

// GrantChildGroups implements harnessconfig.GroupRequestControls.
func (c *harnessControls) GrantChildGroups(ctx context.Context, childID string, groups []string) (*harnessconfig.Outcome, error) {
	parentID, _, err := c.agentScope(ctx)
	if err != nil {
		return nil, err
	}
	child, err := c.agents.GetStatus(ctx, childID)
	if err != nil {
		return nil, err
	}
	if child.ParentSessionID == nil || *child.ParentSessionID != parentID {
		return nil, agenthub.ErrNotDescendant
	}
	if served, serr := c.ServedGroups(ctx, childID); serr == nil {
		groups = slices.DeleteFunc(slices.Clone(groups), func(g string) bool { return slices.Contains(served, g) })
	}
	if len(groups) == 0 {
		return nil, nil
	}
	grantable, err := c.agents.GrantableGroups(ctx, child)
	if err != nil {
		return nil, err
	}
	scope, err := c.dir.SessionScope(ctx, childID)
	if err != nil {
		return nil, harnessconfig.ErrScopeGone
	}
	res, err := c.svc.ApplyDelegated(ctx, harnessconfig.Delegation{WorkspaceID: scope.WorkspaceID, ChildSessionID: childID,
		Groups: groups, GrantorSessionID: parentID, RequestID: "create:" + childID, Grantable: grantable})
	if errors.Is(err, harnessconfig.ErrNoChange) {
		return nil, nil
	}
	req := agenthub.GroupRequest{ChildID: childID, Groups: groups}
	applied, err := c.groupApplyResult(req, scope, res, err)
	if err != nil {
		return nil, err
	}
	out, _ := applied.Payload.(*harnessconfig.Outcome)
	return out, nil
}
