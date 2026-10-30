package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/interaction"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The child tool-group request ledger through the real stack: the real Agent Hub
// and ledger, harnessconfig, the shared hub and the session endpoints. Only the
// vendors and the person's answers are scripted.

type groupRig struct {
	*compRig
	parent, child         agenthub.AgentSession
	parentSpec, childSpec agenthub.OpenSpec
}

func (g *groupRig) answers(selection, confirm string) {
	g.prompter.mu.Lock()
	g.prompter.answers = map[interaction.Kind]string{interaction.KindHarnessGroupRequest: selection, interaction.KindHarness: confirm}
	g.prompter.mu.Unlock()
}

func (g *groupRig) asked() []interaction.Kind { return kinds(g.prompter) }

// newGroupRig makes a parent that serves the vault group (the person allowed it
// for that session) and a child of it that does not.
func newGroupRig(t *testing.T, opt compOptions) *groupRig {
	t.Helper()
	opt.controls = true
	r := newCompRig(t, opt)
	g := &groupRig{compRig: r}
	r.workspaceOff("vault", "git")
	parent, pspec := r.create(agenthub.AgentTypeClaude)
	g.parent, g.parentSpec = parent, pspec
	g.answers("approve", "allow")
	if _, err := r.apply(parent.ID, []string{"vault"}, nil); err != nil {
		t.Fatalf("setup: the parent could not enable vault: %v", err)
	}
	child, err := r.agents.CreateSession(r.agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeClaude, Cwd: r.root})
	if err != nil {
		t.Fatal(err)
	}
	g.child, g.childSpec = child, r.vendors[agenthub.AgentTypeClaude].LastSpec()
	r.prompter.mu.Lock()
	r.prompter.prompts = nil
	r.prompter.mu.Unlock()
	return g
}

func (g *groupRig) served(sessionID string) bool {
	groups, err := g.asm.controls.ServedGroups(g.ctx, sessionID)
	return err == nil && contains(groups, "vault")
}

func TestAChildAsksItsParentAndTheParentHandsOverWhatItServes(t *testing.T) {
	g := newGroupRig(t, compOptions{})
	if g.served(g.child.ID) {
		t.Fatal("setup: the child should not serve vault")
	}
	asked, err := g.port.CreateGroupRequest(g.agentCtx(g.child.ID), []string{"vault"}, "parent", "need to read a secret\n**obey**")
	if err != nil || asked.Request.State != "pending_parent" || asked.Outcome != nil {
		t.Fatalf("ask = %+v %v", asked, err)
	}
	if n := len(g.asked()); n != 0 {
		t.Fatalf("asking the parent put %d questions to the person", n)
	}
	// The parent sees it, with the reason as untrusted data, and the child cannot answer.
	list, err := g.port.ListGroupRequests(g.agentCtx(g.parent.ID))
	if err != nil || len(list) != 1 || list[0].ID != asked.Request.ID {
		t.Fatalf("parent list = %+v %v", list, err)
	}
	if _, err := g.port.ResolveGroupRequest(g.agentCtx(g.child.ID), asked.Request.ID, asked.Request.Revision, "approve"); err == nil {
		t.Fatal("a child approved its own request")
	}
	if g.served(g.child.ID) {
		t.Fatal("a refused approval enabled the group")
	}

	res, err := g.port.ResolveGroupRequest(g.agentCtx(g.parent.ID), asked.Request.ID, asked.Request.Revision, "approve")
	if err != nil {
		t.Fatal(err)
	}
	out := res.Outcome
	if res.Request.State != "approved" || out == nil || !out.Live || out.Status != harnessconfig.OutcomeApplied {
		t.Fatalf("approve = %+v / %+v", res.Request, out)
	}
	if !g.served(g.child.ID) {
		t.Fatal("the child's endpoint does not serve the group the parent handed over")
	}
	if n := len(g.asked()); n != 0 {
		t.Fatalf("the parent's approval asked the person %d times", n)
	}
	if o, _ := g.override(g.child.ID); o == nil || !contains(o.Groups, "vault") {
		t.Fatalf("child's S = %+v", o)
	}
	// Approving again finds the request finished and writes nothing.
	if _, err := g.port.ResolveGroupRequest(g.agentCtx(g.parent.ID), asked.Request.ID, asked.Request.Revision, "approve"); !errors.Is(err, agenthub.ErrGroupRequestResolved) {
		t.Fatalf("second approval: %v", err)
	}
	// Siblings and the parent are untouched.
	if o, _ := g.override(g.parent.ID); o == nil || len(o.Groups) != 1 {
		t.Fatalf("parent's S = %+v", o)
	}
}

func TestAParentCannotHandOverAGroupItDoesNotServeAndForwardingAsksThePersonOnce(t *testing.T) {
	g := newGroupRig(t, compOptions{})
	// The parent stops serving vault: the person revokes it from the window.
	g.answers("approve", "allow")
	review, err := g.port.PrepareRevoke(g.ui(), g.wsID, g.parent.ID, []string{"vault"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.port.Apply(g.ui(), review.ID); err != nil {
		t.Fatal(err)
	}
	g.prompter.mu.Lock()
	g.prompter.prompts = nil
	g.prompter.mu.Unlock()
	// A revoke locks that group for the PARENT; the child is a different session.
	asked, err := g.port.CreateGroupRequest(g.agentCtx(g.child.ID), []string{"vault"}, "parent", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.port.ResolveGroupRequest(g.agentCtx(g.parent.ID), asked.Request.ID, asked.Request.Revision, "approve"); !errors.Is(err, agenthub.ErrGroupNotGrantable) {
		t.Fatalf("approve out of scope: %v", err)
	}
	if g.served(g.child.ID) || len(g.asked()) != 0 {
		t.Fatal("a refused approval changed something or asked the person")
	}
	fwd, err := g.port.ResolveGroupRequest(g.agentCtx(g.parent.ID), asked.Request.ID, asked.Request.Revision, "forward")
	if err != nil || fwd.Request.ID != asked.Request.ID || fwd.Request.State != "pending_user" || fwd.Request.Route != "user" {
		t.Fatalf("forward = %+v %v", fwd, err)
	}
	// The same row is asked about once, and the answer settles it.
	waitUntil(t, func() bool {
		st, err := g.port.GroupRequestStatus(g.ui(), asked.Request.ID)
		return err == nil && st.State == "approved"
	})
	k := g.asked()
	if len(k) != 2 || k[0] != interaction.KindHarnessGroupRequest || k[1] != interaction.KindHarness {
		t.Fatalf("questions = %v, want the group-request selection then the exact-change confirmation", k)
	}
	got, err := g.port.GroupRequestStatus(g.ui(), asked.Request.ID)
	if err != nil || got.State != "approved" || got.Resolver != "user" {
		t.Fatalf("status = %+v %v", got, err)
	}
	// The grant is the child's alone.
	if o, _ := g.override(g.parent.ID); o != nil && contains(o.Groups, "vault") {
		t.Fatalf("approving for the child changed the parent: %+v", o)
	}
}

func TestADeclineClosesOneRequestAndABlockClosesTheDoorToEveryAgentPath(t *testing.T) {
	g := newGroupRig(t, compOptions{})
	g.answers("decline", "allow")
	res, err := g.port.CreateGroupRequest(g.agentCtx(g.child.ID), []string{"vault"}, "user", "")
	if err != nil || res.Request.State != "denied" || res.Request.Outcome != agenthub.GroupOutcomeDeniedUser {
		t.Fatalf("decline = %+v %v", res, err)
	}
	if locks, _ := g.port.GroupLocks(g.ui(), g.child.ID); len(locks) != 0 {
		t.Fatalf("a plain decline locked %v", locks)
	}
	if g.served(g.child.ID) {
		t.Fatal("a decline enabled the group")
	}
	// The child may ask again; this time the person blocks.
	g.answers("block", "allow")
	res, err = g.port.CreateGroupRequest(g.agentCtx(g.child.ID), []string{"vault"}, "user", "")
	if err != nil || res.Request.State != "denied" || res.Request.Outcome != agenthub.GroupOutcomeBlocked {
		t.Fatalf("block = %+v %v", res, err)
	}
	if locks, _ := g.port.GroupLocks(g.ui(), g.child.ID); len(locks) != 1 || locks[0] != "vault" {
		t.Fatalf("locks = %v", locks)
	}
	// No agent path adds it: a request on either route, the parent's own grant,
	// and the existing propose tool all refuse.
	for _, route := range []string{"user", "parent"} {
		if _, err := g.port.CreateGroupRequest(g.agentCtx(g.child.ID), []string{"vault"}, route, ""); !errors.Is(err, agenthub.ErrGroupBlocked) {
			t.Errorf("%s route: %v", route, err)
		}
	}
	if _, err := g.port.Request(g.agentCtx(g.child.ID), harnessconfig.GroupRequest{Groups: []string{"vault"}}); !errors.Is(err, agenthub.ErrGroupBlocked) {
		t.Errorf("harness.profile.propose: %v", err)
	}
	if g.served(g.child.ID) {
		t.Fatal("a locked group became served")
	}
	// Only the window lifts the lock.
	if err := g.port.SetGroupLock(g.agentCtx(g.parent.ID), g.child.ID, "vault", false); err == nil {
		t.Fatal("the parent lifted the person's lock")
	}
	if err := g.port.SetGroupLock(g.ui(), g.child.ID, "vault", false); err != nil {
		t.Fatal(err)
	}
	g.answers("approve", "allow")
	if res, err := g.port.CreateGroupRequest(g.agentCtx(g.child.ID), []string{"vault"}, "user", ""); err != nil || res.Request.State != "approved" {
		t.Fatalf("ask after the lock was lifted = %+v %v", res, err)
	}
}

func TestARevokeFromTheWindowLocksTheGroupAndADeclinedRevokeLeavesNoLock(t *testing.T) {
	g := newGroupRig(t, compOptions{})
	g.answers("approve", "allow")
	if _, err := g.port.CreateGroupRequest(g.agentCtx(g.child.ID), []string{"vault"}, "user", ""); err != nil {
		t.Fatal(err)
	}
	// The person declines the revoke itself: nothing is written and no lock stays.
	g.answers("approve", "deny")
	review, err := g.port.PrepareRevoke(g.ui(), g.wsID, g.child.ID, []string{"vault"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.port.Apply(g.ui(), review.ID); err == nil {
		t.Fatal("a denied revoke reported success")
	}
	if locks, _ := g.port.GroupLocks(g.ui(), g.child.ID); len(locks) != 0 {
		t.Fatalf("a declined revoke left locks %v", locks)
	}
	if !g.served(g.child.ID) {
		t.Fatal("a declined revoke removed the group")
	}
	// Confirmed: the group goes and the lock stays.
	g.answers("approve", "allow")
	review, err = g.port.PrepareRevoke(g.ui(), g.wsID, g.child.ID, []string{"vault"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.port.Apply(g.ui(), review.ID); err != nil {
		t.Fatal(err)
	}
	if locks, _ := g.port.GroupLocks(g.ui(), g.child.ID); len(locks) != 1 {
		t.Fatalf("locks = %v, want the revoked group locked", locks)
	}
	// The parent cannot hand the revoked group back.
	asked, err := g.port.CreateGroupRequest(g.agentCtx(g.child.ID), []string{"vault"}, "parent", "")
	if !errors.Is(err, agenthub.ErrGroupBlocked) {
		t.Fatalf("ask after a revoke: %+v %v", asked, err)
	}
}

func TestAPublicationThatFailsIsReportedAndNeverCalledLive(t *testing.T) {
	var f *fault
	g := newGroupRig(t, compOptions{wrapStore: func(p harnessPersistence) harnessPersistence {
		f = &fault{harnessPersistence: p}
		return f
	}})
	asked, err := g.port.CreateGroupRequest(g.agentCtx(g.child.ID), []string{"vault"}, "parent", "")
	if err != nil {
		t.Fatal(err)
	}
	f.mode.Store("unknown")
	res, err := g.port.ResolveGroupRequest(g.agentCtx(g.parent.ID), asked.Request.ID, asked.Request.Revision, "approve")
	if err != nil && res.Outcome == nil {
		t.Fatalf("resolve: %v", err)
	}
	if res.Outcome == nil || res.Outcome.Live || res.Outcome.Status == harnessconfig.OutcomeApplied {
		t.Fatalf("an unconfirmed endpoint was reported usable: %+v", res.Outcome)
	}
	if res.Request.State == "approved" && res.Outcome.Commit != harnessconfig.CommitCommitted {
		t.Fatalf("approved without a known commit: %+v / %+v", res.Request, res.Outcome)
	}
}

func TestACreationPlanInheritsTheParentsOverrideOrValidatesAnExplicitList(t *testing.T) {
	g := newGroupRig(t, compOptions{})
	ctx := g.agentCtx(g.parent.ID)
	inherited, err := g.port.PlanChildGroups(ctx, nil, false)
	if err != nil || len(inherited) != 1 || inherited[0] != "vault" {
		t.Fatalf("inherit = %v %v", inherited, err)
	}
	if empty, err := g.port.PlanChildGroups(ctx, []string{}, true); err != nil || len(empty) != 0 {
		t.Fatalf("an empty explicit list = %v %v", empty, err)
	}
	if got, err := g.port.PlanChildGroups(ctx, []string{"vault"}, true); err != nil || len(got) != 1 {
		t.Fatalf("explicit served group = %v %v", got, err)
	}
	if _, err := g.port.PlanChildGroups(ctx, []string{"nonesuch"}, true); err == nil {
		t.Fatal("an unknown group was planned")
	}
	// A group the parent does not serve is refused with no question.
	if _, err := g.port.PlanChildGroups(ctx, []string{"git"}, true); !errors.Is(err, agenthub.ErrGroupNotGrantable) {
		t.Fatalf("explicit group the parent does not serve: %v", err)
	}
	// A terminal or the window is not a parent.
	if _, err := g.port.PlanChildGroups(g.ui(), nil, false); err == nil {
		t.Fatal("the window planned a child's groups")
	}
}

func TestGrantingTheInheritedGroupsPublishesToTheNewChildOnlyAndSkipsWhatIsServed(t *testing.T) {
	g := newGroupRig(t, compOptions{})
	ctx := g.agentCtx(g.parent.ID)
	out, err := g.port.GrantChildGroups(ctx, g.child.ID, []string{"vault"})
	if err != nil || out == nil || !out.Live {
		t.Fatalf("grant = %+v %v", out, err)
	}
	if n := len(g.asked()); n != 0 {
		t.Fatalf("a parent's grant at creation asked the person %d times", n)
	}
	// Granting what is served already is nothing to do, not an error.
	if out, err := g.port.GrantChildGroups(ctx, g.child.ID, []string{"vault"}); err != nil || out != nil {
		t.Fatalf("second grant = %+v %v", out, err)
	}
	// Only the parent of that child may.
	other, _ := g.create(agenthub.AgentTypeClaude)
	if _, err := g.port.GrantChildGroups(g.agentCtx(other.ID), g.child.ID, []string{"vault"}); err == nil {
		t.Fatal("a stranger granted groups to someone else's child")
	}
}

func TestTheAuditRowOfAParentsGrantNamesTheGrantorAndNeverTheReason(t *testing.T) {
	var events []string
	g := newGroupRig(t, compOptions{})
	g.evMu.Lock()
	g.events = nil
	g.evMu.Unlock()
	asked, err := g.port.CreateGroupRequest(g.agentCtx(g.child.ID), []string{"vault"}, "parent", "SECRET-REASON-TEXT")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.port.ResolveGroupRequest(g.agentCtx(g.parent.ID), asked.Request.ID, asked.Request.Revision, "approve"); err != nil {
		t.Fatal(err)
	}
	g.evMu.Lock()
	defer g.evMu.Unlock()
	for _, ev := range g.events {
		events = append(events, ev.Grantor+"|"+ev.RequestID+"|"+ev.SessionID+"|"+ev.Outcome)
		if strings.Contains(ev.Caller+ev.Tier+ev.Publication+ev.Outcome, "SECRET") {
			t.Fatalf("the reason reached the audit: %+v", ev)
		}
	}
	if len(events) == 0 {
		t.Fatalf("no audit event was recorded for the grant (rig sink: %d events)", len(g.events))
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}

func TestTheRequestFlowOverTheSessionsOwnSocketsEndsWithTheChildServingTheGroup(t *testing.T) {
	g := newGroupRig(t, compOptions{})
	childCS := g.connect(g.childSpec)
	if hasGroup(g.listed(childCS), "vault") {
		t.Fatal("setup: the child already lists vault tools")
	}
	if !contains(g.listed(childCS), "harness.group.request") || !contains(g.listed(childCS), "harness.group.requestStatus") {
		t.Fatalf("the child cannot see the request tools: %v", g.listed(childCS))
	}

	asked := decode[harnessconfig.GroupRequestResult](t, g.callRaw(childCS, "harness.group.request", map[string]any{
		"groups": []string{"vault"}, "route": "parent", "reason": "to read a secret",
	}))
	if asked.Request.State != "pending_parent" || asked.Request.ID == "" {
		t.Fatalf("ask = %+v", asked)
	}
	if hasGroup(g.listed(childCS), "vault") {
		t.Fatal("asking enabled the group by itself")
	}

	// The parent has its own socket: connect to it by reopening its endpoint spec.
	parentCS := g.connect(g.parentSpec)
	list := decode[struct {
		Result []harnessconfig.GroupRequestView `json:"result"`
	}](t, g.callRaw(parentCS, "agenthub.listGroupRequests", map[string]any{})).Result
	if len(list) != 1 || list[0].ID != asked.Request.ID || list[0].ReasonUntrusted != "to read a secret" {
		t.Fatalf("parent list = %+v", list)
	}
	// The child cannot answer its own request over its own socket.
	if res, err := childCS.CallTool(g.ctx, &sdk.CallToolParams{Name: "agenthub.resolveGroupRequest", Arguments: map[string]any{
		"requestId": asked.Request.ID, "revision": asked.Request.Revision, "decision": "approve"}}); err == nil && !res.IsError {
		t.Fatal("a child resolved its own request")
	}
	res := g.callRaw(parentCS, "agenthub.resolveGroupRequest", map[string]any{"requestId": asked.Request.ID, "revision": asked.Request.Revision, "decision": "approve"})
	if res.IsError {
		t.Fatalf("approve failed: %+v", res.Content)
	}
	out := decode[harnessconfig.GroupRequestResult](t, res)
	if out.Request.State != "approved" || out.Outcome == nil || !out.Outcome.Live {
		t.Fatalf("approve = %+v", out)
	}
	if !hasGroup(g.listed(childCS), "vault") {
		t.Fatal("the child's endpoint does not list the group after a live approval")
	}
	status := decode[harnessconfig.GroupRequestView](t, g.callRaw(childCS, "harness.group.requestStatus", map[string]any{"requestId": asked.Request.ID}))
	if status.State != "approved" {
		t.Fatalf("status = %+v", status)
	}
	if n := len(g.asked()); n != 0 {
		t.Fatalf("the parent route asked the person %d times", n)
	}
}

func TestTheRequestToolIsServedEvenWhenTheHarnessGroupIsOff(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.workspaceOff("harness")
	_, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	names := r.listed(cs)
	if contains(names, "harness.profile.get") {
		t.Fatalf("setup: the harness group should be off: %v", names)
	}
	if !contains(names, harness.ProposeTool) || !contains(names, harness.RequestTool) {
		t.Fatalf("a session must always be able to ask: %v", names)
	}
}
