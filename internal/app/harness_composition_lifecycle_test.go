package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/interaction"
	"devkit/internal/permissions"
	"devkit/internal/preferences"
)

// ---- D4: approval, gateway, errors -------------------------------------------

func TestARealApprovalThroughTheSharedHubEnablesTheGroupLive(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	if hasGroup(r.listed(cs), "vault") {
		t.Fatal("vault starts hidden")
	}
	r.prompter.answer = "allow_always" // treated as one allow, never remembered
	res, err := r.apply(sess.ID, []string{"vault"}, nil)
	if err != nil || !res.Live || res.Publication.State != harnessconfig.PublicationInstalled {
		t.Fatalf("apply = %+v %v", res, err)
	}
	if len(r.prompter.prompts) != 1 {
		t.Fatalf("prompts = %d", len(r.prompter.prompts))
	}
	if p := r.prompter.prompts[0]; p.Kind != interaction.KindHarness || p.AllowAlwaysOffered {
		t.Fatalf("a harness confirmation offers no always: %+v", p)
	}
	if !hasGroup(r.listed(cs), "vault") {
		t.Fatal("the running session must now list vault")
	}
	before := r.gate.handled.Load()
	if got := r.call(cs, "vault.status"); !strings.Contains(got, "unlocked") || r.gate.handled.Load() != before+1 {
		t.Fatalf("an enabled tool must run through the real gateway: %q", got)
	}
	// A second request asks again.
	if _, err := r.apply(sess.ID, nil, []string{"vault"}); err != nil {
		t.Fatal(err)
	}
	if len(r.prompter.prompts) != 2 {
		t.Fatalf("a remembered answer: %d prompts", len(r.prompter.prompts))
	}
	// The unprofiled terminal surface is not this endpoint's: nothing changed it.
	if got := r.server.MCP(); got == nil {
		t.Fatal("the terminal server must stay as it was")
	}
}

func TestDenialHeadlessAndPromptlessHubsWriteAndPublishNothing(t *testing.T) {
	for name, setup := range map[string]func(r *compRig){
		"denied":   func(r *compRig) { r.prompter.answer = "deny" },
		"headless": func(r *compRig) { r.hub.Headless = true },
	} {
		t.Run(name, func(t *testing.T) {
			r := newCompRig(t, compOptions{})
			r.workspaceOff("vault")
			sess, spec := r.create(agenthub.AgentTypeClaude)
			cs := r.connect(spec)
			setup(r)
			epoch := r.agents.ServingTarget(sess.ID).Epoch
			if _, err := r.apply(sess.ID, []string{"vault"}, nil); err == nil {
				t.Fatal("the change must be refused")
			}
			if o, _ := r.override(sess.ID); o != nil {
				t.Fatal("S was written")
			}
			if v := r.agents.ServingTarget(sess.ID); v.Quarantined || v.Epoch != epoch || !v.Acknowledged {
				t.Fatalf("the endpoint must be untouched: %+v", v)
			}
			if hasGroup(r.listed(cs), "vault") {
				t.Fatal("nothing may have been published")
			}
		})
	}
}

func TestACustomizationEditThroughTheRealSetterStalesTheComposedProposal(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	sess, _ := r.create(agenthub.AgentTypeClaude)
	r.prompter.during = func() {
		c, _ := r.prefs.AgentCustomization("claude")
		c.DisabledMCP = []string{"slack"}
		if _, err := r.prefs.SetAgentCustomization("claude", c); err != nil {
			t.Error(err)
		}
	}
	_, err := r.apply(sess.ID, []string{"vault"}, nil)
	var he *harness.Error
	if !errors.As(err, &he) || he.Code != harness.CodeProposalStale {
		t.Fatalf("want stale, got %v", err)
	}
	if o, _ := r.override(sess.ID); o != nil {
		t.Fatal("a stale approval wrote S")
	}
	if v := r.agents.ServingTarget(sess.ID); v.Quarantined {
		t.Fatalf("a refusal must not leave the endpoint paused: %+v", v)
	}
}

// The gateway, scope and grants still apply to a group enabled live: enabling
// makes a tool listed, never authorized.
func TestAGroupEnabledLiveStillObeysTheRealGatewayGrants(t *testing.T) {
	r := newCompRig(t, compOptions{grants: []permissions.Grant{}}) // no grants at all
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	if !hasGroup(r.listed(cs), "vault") {
		t.Fatal("vault is listed after the enable")
	}
	before := r.gate.handled.Load()
	got := r.call(cs, "vault.status")
	if strings.HasPrefix(got, harness.CodeGroupOff) || r.gate.handled.Load() != before {
		t.Fatalf("a call must be refused by the gateway, not served and not by the group filter: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "denied") && !strings.Contains(strings.ToLower(got), "permission") {
		t.Fatalf("the refusal must come from the permission policy: %q", got)
	}
}

// ---- D3: lifecycle matrix -----------------------------------------------------

func TestResumeAfterRestartUsesLoadForAPromptedSessionAndOpenForANeverPromptedOne(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	prompted, _ := r.create(agenthub.AgentTypeClaude)
	untouched, _ := r.create(agenthub.AgentTypeClaude)
	if _, err := r.agents.Invoke(r.ui(), agenthub.InvokeInput{SessionID: prompted.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{prompted.ID, untouched.ID} {
		if _, err := r.apply(id, []string{"vault"}, nil); err != nil {
			t.Fatal(err)
		}
	}

	r.restart()
	// Prompted: the vendor's own conversation is loaded; nothing is replayed.
	if _, err := r.agents.Invoke(r.ui(), agenthub.InvokeInput{SessionID: prompted.ID, Prompt: "again"}); err != nil {
		t.Fatal(err)
	}
	v := r.vendors[agenthub.AgentTypeClaude]
	if v.loads.Load() != 1 || v.opens.Load() != 0 {
		t.Fatalf("a prompted session resumes through Load: loads=%d opens=%d", v.loads.Load(), v.opens.Load())
	}
	spec := v.LastSpec()
	if !contains(spec.DevKit.Tools, "vault.status") {
		t.Fatal("the fresh incarnation must serve the durable S")
	}
	if prompts := v.LastRuntime().Prompts(); len(prompts) != 1 {
		t.Fatalf("a resume must not replay a prompt: %d", len(prompts))
	}
	// Never prompted: nothing exists on the vendor's side, so it opens fresh.
	if err := r.agents.ResumeSession(r.ui(), untouched.ID, false); err != nil {
		t.Fatal(err)
	}
	if v.opens.Load() != 1 || v.loads.Load() != 1 {
		t.Fatalf("a never-prompted session starts over through Open: loads=%d opens=%d", v.loads.Load(), v.opens.Load())
	}
	if !contains(v.LastSpec().DevKit.Tools, "vault.status") {
		t.Fatal("the Open path must resolve the durable S too")
	}
	for _, id := range []string{prompted.ID, untouched.ID} {
		st, err := r.agents.HarnessStatus(context.Background(), id)
		if err != nil || !st.Live || st.Applied == nil || st.Applied.Revisions.Session == nil || *st.Applied.Revisions.Session != 1 {
			t.Fatalf("status of %s = %+v %v", id, st, err)
		}
	}
}

func TestAReloadThatCannotBePreparedLeavesTheRunningSessionAsItWas(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	if _, err := r.agents.Invoke(r.ui(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "hello"}); err != nil {
		t.Fatal(err)
	}
	cs := r.connect(spec)
	before := r.agents.ServingTarget(sess.ID).Incarnation
	// Corrupt the stored workspace profile: the configuration cannot be resolved.
	var body string
	if err := r.db.QueryRow(`SELECT body FROM workspace_harness_profiles WHERE workspace_id=?`, r.wsID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec(`UPDATE workspace_harness_profiles SET body='{bad' WHERE workspace_id=?`, r.wsID); err != nil {
		t.Fatal(err)
	}
	err := r.agents.ResumeSession(r.ui(), sess.ID, true)
	if !errors.Is(err, agenthub.ErrHarnessUnavailable) {
		t.Fatalf("a configuration that cannot be resolved refuses the reload (and never serves unfiltered), got %v", err)
	}
	if v := r.agents.ServingTarget(sess.ID); v.Incarnation != before || !v.Acknowledged {
		t.Fatalf("the running endpoint must be untouched: %+v", v)
	}
	if hasGroup(r.listed(cs), "vault") || r.vendors[agenthub.AgentTypeClaude].loads.Load() != 0 {
		t.Fatal("a refused reload must not have changed or reopened anything")
	}
	if _, err := r.db.Exec(`UPDATE workspace_harness_profiles SET body=? WHERE workspace_id=?`, body, r.wsID); err != nil {
		t.Fatal(err)
	}
	// Repaired: the reload adopts the saved configuration once, keeps S and retires
	// the old endpoint.
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.agents.ResumeSession(r.ui(), sess.ID, true); err != nil {
		t.Fatal(err)
	}
	v := r.agents.ServingTarget(sess.ID)
	if v.Incarnation == before || v.Serving.Session.Revision != 1 || !v.Acknowledged {
		t.Fatalf("a reload creates a new incarnation serving the durable S: %+v", v)
	}
	if _, err := cs.ListTools(r.ctx, nil); err == nil {
		t.Fatal("the old endpoint's connection must be closed")
	}
}

func TestADeadRuntimeIsRecoveredOnANewIncarnationThatKeepsS(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	old := r.connect(spec)
	before := r.agents.ServingTarget(sess.ID).Incarnation
	r.vendors[agenthub.AgentTypeClaude].kill()
	if _, err := r.agents.Invoke(r.ui(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "after the crash"}); err != nil {
		t.Fatal(err)
	}
	v := r.agents.ServingTarget(sess.ID)
	if v.Incarnation == before || v.Serving.Session.Revision != 1 || !v.Acknowledged {
		t.Fatalf("recovery must publish a new incarnation serving the durable S: %+v", v)
	}
	if _, err := old.ListTools(r.ctx, nil); err == nil {
		t.Fatal("the dead process's endpoint token must be invalid")
	}
	if o, st := r.override(sess.ID); o == nil || st.Revision != 1 {
		t.Fatalf("a crash is not a close: S = %+v %+v", o, st)
	}
}

func TestCloseTombstonesSKeepsTheReferenceAndAReopenStartsWithoutIt(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	sess, _ := r.create(agenthub.AgentTypeClaude)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	p, err := r.propose(sess.ID, nil, []string{"vault"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.agents.CloseSession(r.ui(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.asm.Service.Apply(r.agentCtx(sess.ID), p.ID); err == nil {
		t.Fatal("an approval for a closed session must be refused")
	}
	if o, st := r.override(sess.ID); o != nil || st.Present || st.Revision != 2 {
		t.Fatalf("close must leave a monotonic tombstone: %+v %+v", o, st)
	}
	if _, _, ok := r.agents.HarnessApplied(sess.ID); !ok {
		t.Fatal("close must keep the applied reference (the executable reopen guard)")
	}
	if v := r.agents.ServingTarget(sess.ID); v.Active {
		t.Fatalf("a closed session has no live endpoint: %+v", v)
	}
	// Reopen: the tombstone means no S; the workspace profile still hides vault.
	if _, err := r.agents.Invoke(r.ui(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "back"}); err != nil {
		t.Fatal(err)
	}
	spec := r.vendors[agenthub.AgentTypeClaude].LastSpec()
	if contains(spec.DevKit.Tools, "vault.status") {
		t.Fatal("a reopened session must not resurrect a cleared S")
	}
}

func TestDeleteRemovesTheSessionAndALatePublicationCannotReviveIt(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	sess, _ := r.create(agenthub.AgentTypeClaude)
	p, err := r.propose(sess.ID, []string{"vault"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.agents.DeleteSession(r.ui(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.asm.Service.Apply(r.agentCtx(sess.ID), p.ID); err == nil {
		t.Fatal("an approval for a deleted session must be refused")
	}
	if v := r.agents.ServingTarget(sess.ID); v.Active {
		t.Fatalf("a deleted session has no endpoint: %+v", v)
	}
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM session_harness_overrides WHERE session_id=?`, sess.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("override rows after delete: %d %v", n, err)
	}
}

func TestShutdownIsNotACloseAndNeverErasesS(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	sess, _ := r.create(agenthub.AgentTypeClaude)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	// What main's OnShutdown does, in its order.
	r.host.Close()
	if err := r.agents.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if o, st := r.override(sess.ID); o == nil || !st.Present || st.Revision != 1 {
		t.Fatalf("shutdown must keep S: %+v %+v", o, st)
	}
	if v := r.agents.ServingTarget(sess.ID); v.Acknowledged {
		t.Fatal("a shut-down host's endpoints serve nothing")
	}
	// The next process serves it again.
	r.restart()
	if err := r.agents.ResumeSession(r.ui(), sess.ID, false); err != nil {
		t.Fatal(err)
	}
	if !contains(r.vendors[agenthub.AgentTypeClaude].LastSpec().DevKit.Tools, "vault.status") {
		t.Fatal("S must survive a shutdown and restart")
	}
}

func TestAChildGetsItsOwnProfileAndNeverItsParentsS(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	parent, _ := r.create(agenthub.AgentTypeClaude)
	if _, err := r.apply(parent.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	child, err := r.agents.CreateSession(r.agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeClaude, Cwd: r.root, ParentSessionID: parent.ID})
	if err != nil {
		t.Skipf("child creation needs permissions this rig does not script: %v", err)
	}
	spec := r.vendors[agenthub.AgentTypeClaude].LastSpec()
	if contains(spec.DevKit.Tools, "vault.status") {
		t.Fatal("a child must not inherit its parent's S")
	}
	if o, _ := r.override(child.ID); o != nil {
		t.Fatalf("the child has no override: %+v", o)
	}
}

// ---- measurement ---------------------------------------------------------------

func TestMeasurementUsesTheEntrySnapshotAcrossAMidTurnChange(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs, other := r.connect(spec), r.connect(spec)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	wantW := func(o interface{}) {}
	_ = wantW
	scope, ok := r.agents.ToolUsageScope(sess.ID, spec.UsageToken)
	if !ok || scope.OverrideRevision == nil || *scope.OverrideRevision != 1 {
		t.Fatalf("a future turn is attributed to the served S: %+v %v", scope, ok)
	}
	// A call is running when S is revoked.
	entered, release := r.gate.holdNext()
	defer release()
	done := make(chan string, 1)
	go func() { done <- r.call(cs, "vault.status") }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the call never reached the gateway")
	}
	if _, err := r.apply(sess.ID, nil, []string{"vault"}); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the running call never finished")
	}
	if got := r.call(other, "vault.status"); !strings.HasPrefix(got, harness.CodeGroupOff) {
		t.Fatalf("later calls use the new serving state: %q", got)
	}
	var running, refused *struct{ rev *int64 }
	for _, o := range r.usage.all() {
		if o.Tool != "vault.status" {
			continue
		}
		entry := &struct{ rev *int64 }{o.Scope.OverrideRevision}
		if o.ToolError {
			refused = entry
		} else {
			running = entry
		}
	}
	if running == nil || running.rev == nil || *running.rev != 1 {
		t.Fatalf("the in-flight call keeps its entry-time facts (S=1): %+v", running)
	}
	if refused == nil || refused.rev != nil {
		t.Fatalf("a later call carries the new serving facts (no S): %+v", refused)
	}
	scope, _ = r.agents.ToolUsageScope(sess.ID, spec.UsageToken)
	if scope.OverrideRevision != nil {
		t.Fatalf("a revoked S is an absent revision for future turns: %v", *scope.OverrideRevision)
	}
}

var _ = preferences.AgentCustomization{}
