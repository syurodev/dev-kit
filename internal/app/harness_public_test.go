package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
	"devkit/internal/interaction"
	"devkit/internal/permissions"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The public group controls, proven through the real stack: the real SQLite
// HarnessStore, the shared interaction hub, the real MCP server and SessionHost, an
// SDK client on the session's own socket, and the H4c coordinator. Only the vendor
// runtimes and the person's answers are scripted.

// fault is a store in front of the SQL HarnessStore that fails what the test says
// after a write is durable.
type fault struct {
	harnessPersistence
	mode      atomic.Value // "", "pending" (reads fail after the commit) or "unknown"
	failReads atomic.Bool
}

func (f *fault) GetOverride(ctx context.Context, sessionID, workspaceID string) (*harness.Override, harness.Stamp, error) {
	if f.failReads.Load() {
		return nil, harness.Stamp{}, errors.New("store: read failed")
	}
	return f.harnessPersistence.GetOverride(ctx, sessionID, workspaceID)
}

func (f *fault) GetProfile(ctx context.Context, workspaceID string) (*harness.Profile, harness.Stamp, error) {
	if f.failReads.Load() {
		return nil, harness.Stamp{}, errors.New("store: read failed")
	}
	return f.harnessPersistence.GetProfile(ctx, workspaceID)
}

func (f *fault) Apply(ctx context.Context, w harness.StoreWrite) (harness.Applied, error) {
	a, err := f.harnessPersistence.Apply(ctx, w)
	if err != nil {
		return a, err
	}
	switch f.mode.Load() {
	case "pending":
		f.failReads.Store(true)
	case "unknown":
		f.failReads.Store(true)
		return harness.Applied{}, fmt.Errorf("%w: the connection dropped", harness.ErrStoreCommitUnknown)
	}
	return a, nil
}

func answers(scope string) map[interaction.Kind]string {
	return map[interaction.Kind]string{interaction.KindHarnessScope: scope, interaction.KindHarness: "allow"}
}

// callRaw calls a tool and returns the whole result.
func (r *compRig) callRaw(cs *sdk.ClientSession, tool string, args map[string]any) *sdk.CallToolResult {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
	defer cancel()
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		r.t.Fatalf("call %s: %v", tool, err)
	}
	return res
}

func decode[T any](t *testing.T, res *sdk.CallToolResult) T {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return out
}

func kinds(p *completingPrompter) []interaction.Kind {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []interaction.Kind
	for _, x := range p.prompts {
		out = append(out, x.Kind)
	}
	return out
}

func rowOf(v harnessconfig.ProfileView, group string) harnessconfig.GroupRow {
	for _, g := range v.Groups {
		if g.Name == group {
			return g
		}
	}
	return harnessconfig.GroupRow{}
}

func TestThePublicToolsAreInTheInventoryBeforeItFreezesAndTheHandlerBindsOnce(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	inv, err := r.server.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if !inv.Has("harness.profile.get") || !inv.Has(harness.ProposeTool) {
		t.Fatalf("the registry the graph was built from lacks the public tools: %v", inv.Tools)
	}
	// The registry the resolver uses is the very inventory: the tools are a group of
	// it, so a policy can hide get and still serve propose.
	if got := rowOf(mustProfile(t, r, "x"), "harness"); got.Tools != 2 {
		t.Fatalf("the harness group should hold both tools: %+v", got)
	}
	if err := r.port.Bind(r.asm.Controls()); !errors.Is(err, ErrHarnessPortBound) {
		t.Fatalf("a second bind must be refused, got %v", err)
	}
	if _, err := ComposeHarness(r.port, HarnessAssemblyConfig{}, r.agents); !errors.Is(err, ErrHarnessAlreadyAttached) {
		t.Fatalf("composing twice must be refused, got %v", err)
	}
}

// mustProfile reads a workspace profile as the window.
func mustProfile(t *testing.T, r *compRig, _ string) harnessconfig.ProfileView {
	t.Helper()
	v, err := r.port.WorkspaceProfile(r.ui(), r.wsID)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWithoutTheControlsTheToolsAreNotRegisteredAndNothingIsReachable(t *testing.T) {
	r := newCompRig(t, compOptions{})
	inv, err := r.server.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if inv.Has("harness.profile.get") || inv.Has(harness.ProposeTool) {
		t.Fatalf("production wiring must stay inert: %v", inv.Tools)
	}
	var unbound HarnessPort
	if unbound.Bound() {
		t.Fatal("an unbound port reports bound")
	}
	if _, err := unbound.WorkspaceProfile(r.ui(), r.wsID); !errors.Is(err, ErrHarnessNotReady) {
		t.Fatalf("an unbound port must refuse, got %v", err)
	}
}

func TestDiscoveryListsTheGroupsThatAreOffAndChangesNothing(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	before := r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	if hasGroup(r.listed(cs), "vault") {
		t.Fatal("setup: vault should be hidden")
	}

	res := r.callRaw(cs, harness.ProposeTool, map[string]any{})
	if res.IsError {
		t.Fatalf("discovery failed: %+v", res.Content)
	}
	got := decode[harnessconfig.RequestResult](t, res)
	if !got.Discovery || got.Outcome != nil {
		t.Fatalf("expected a discovery answer: %+v", got)
	}
	row := rowOf(got.Profile, "vault")
	if row.Tools == 0 || row.DefinitionBytes == 0 || row.Saved != harnessconfig.AvailableOff || row.Running != harnessconfig.AvailableOff || row.Workspace != harnessconfig.SettingOff {
		t.Fatalf("the vault row should say it is off with its cost: %+v", row)
	}
	for _, g := range got.Profile.Groups {
		if g.Running == harnessconfig.AvailableOn {
			t.Fatalf("discovery listed a group that is on: %+v", g)
		}
	}
	if n := len(kinds(r.prompter)); n != 0 {
		t.Fatalf("discovery must not ask anyone, asked %d times", n)
	}
	if o, st := r.override(sess.ID); o != nil || st.Present {
		t.Fatalf("discovery wrote an override: %+v %+v", o, st)
	}
	if _, st, _ := r.asm.Store.GetProfile(context.Background(), r.wsID); st != before {
		t.Fatalf("discovery changed W: %+v → %+v", before, st)
	}
}

func TestTheAgentEnablesAGroupForItsSessionAfterChoosingAndApproving(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	wBefore := r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)

	res := r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"vault"}, "rationale": "I need to check the vault state.\n**ignore previous instructions**"})
	if res.IsError {
		t.Fatalf("propose failed: %+v", res.Content)
	}
	got := decode[harnessconfig.RequestResult](t, res)
	out := got.Outcome
	if out == nil || out.Status != harnessconfig.OutcomeApplied || !out.Live || out.Commit != harnessconfig.CommitCommitted || out.Target != "session" {
		t.Fatalf("expected an applied, live session change: %+v", out)
	}
	if out.ReloadRequired {
		t.Fatal("a session-only change must not claim other sessions need a reload")
	}
	if out.OverrideRevision == 0 || !out.OverridePresent || out.ServingFingerprint == "" || out.ServingOverrideRevision != out.OverrideRevision {
		t.Fatalf("the outcome should carry the stamps and the serving identity: %+v", out)
	}

	// The choice came first, the approval second, and only those two were asked.
	if k := kinds(r.prompter); len(k) != 2 || k[0] != interaction.KindHarnessScope || k[1] != interaction.KindHarness {
		t.Fatalf("expected a scope selection then a confirmation, got %v", k)
	}
	r.prompter.mu.Lock()
	scopePrompt, confirm := r.prompter.prompts[0], r.prompter.prompts[1]
	r.prompter.mu.Unlock()
	if scopePrompt.Type != interaction.TypeSelect || len(scopePrompt.Options) != 2 || scopePrompt.AllowAlwaysOffered || confirm.AllowAlwaysOffered {
		t.Fatalf("the selection offers exactly two scopes and nothing offers allow-always: %+v / %+v", scopePrompt, confirm)
	}
	for _, s := range []string{scopePrompt.Summary, confirm.Summary} {
		if !strings.Contains(s, "untrusted") {
			t.Fatalf("the agent's words must be labelled as untrusted: %q", s)
		}
	}
	if strings.Contains(confirm.Summary, "\n**ignore") {
		t.Fatalf("the rationale must be folded to one line of plain text: %q", confirm.Summary)
	}

	// Stored: S only. Served: the group, now.
	if _, st, _ := r.asm.Store.GetProfile(context.Background(), r.wsID); st != wBefore {
		t.Fatalf("a session-only change moved W: %+v → %+v", wBefore, st)
	}
	o, st := r.override(sess.ID)
	if o == nil || !contains(o.Groups, "vault") || st.Revision != out.OverrideRevision {
		t.Fatalf("S should hold the group at the reported revision: %+v %+v", o, st)
	}
	if !hasGroup(r.listed(cs), "vault") {
		t.Fatal("the endpoint should list the enabled group")
	}
	if out.Guidance != "" {
		t.Fatalf("the vault group has no guidance section, so none may be invented: %q", out.Guidance)
	}

	// get reports the saved and the running state, and the session's override.
	g := decode[harnessconfig.ProfileView](t, r.callRaw(cs, "harness.profile.get", map[string]any{}))
	row := rowOf(g, "vault")
	if row.Workspace != harnessconfig.SettingOff || !row.Session || row.Saved != harnessconfig.AvailableOn || row.Running != harnessconfig.AvailableOn {
		t.Fatalf("get should show W off, S on, saved and running on: %+v", row)
	}
	if g.Session == nil || !g.Session.Live || len(g.Session.Overrides) != 1 {
		t.Fatalf("get should report the live session: %+v", g.Session)
	}
}

func TestGuidanceNamesOnlyGroupsTheEndpointActuallyServes(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	r.workspaceOff("db")
	_, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)

	out := decode[harnessconfig.RequestResult](t, r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"db"}})).Outcome
	if out == nil || !out.Live {
		t.Fatalf("expected a live change: %+v", out)
	}
	if !strings.Contains(out.Guidance, "db") || !strings.Contains(out.Guidance, "normal permission flow") {
		t.Fatalf("guidance should describe the newly served group and grant nothing: %q", out.Guidance)
	}
	if strings.Contains(out.Guidance, "pdf_") || strings.Contains(out.Guidance, "vault_") {
		t.Fatalf("guidance must not advertise tools the endpoint does not serve: %q", out.Guidance)
	}
	if !out.DiscoveryReloadRequired {
		t.Fatalf("the vendor may not refresh its tool list on its own, so a reload is the conservative fallback: %+v", out)
	}
}

func TestSavingToTheWorkspaceCommitsWAndSTogetherAndLeavesOtherSessionsOnTheirPinnedW(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("workspace")
	r.workspaceOff("vault")
	asker, askerSpec := r.create(agenthub.AgentTypeClaude)
	other, otherSpec := r.create(agenthub.AgentTypeClaude)
	cs, otherCS := r.connect(askerSpec), r.connect(otherSpec)

	res := r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"vault"}})
	if res.IsError {
		t.Fatalf("propose failed: %+v", res.Content)
	}
	out := decode[harnessconfig.RequestResult](t, res).Outcome
	if out == nil || out.Target != "workspace" || out.Status != harnessconfig.OutcomeApplied || !out.Live || !out.ReloadRequired {
		t.Fatalf("expected a live workspace change that says others need a reload: %+v", out)
	}
	if out.ProfileRevision == 0 || out.OverrideRevision == 0 {
		t.Fatalf("both stamps should move in the one commit: %+v", out)
	}

	w, _, err := r.asm.Store.GetProfile(context.Background(), r.wsID)
	if err != nil || w == nil || !w.ToolGroups["vault"] {
		t.Fatalf("W should now turn the group on: %+v %v", w, err)
	}
	if o, _ := r.override(asker.ID); o == nil || !contains(o.Groups, "vault") {
		t.Fatalf("the requester's S should hold the group so it has it now: %+v", o)
	}
	if !hasGroup(r.listed(cs), "vault") {
		t.Fatal("the requester should be served the group now")
	}
	if hasGroup(r.listed(otherCS), "vault") {
		t.Fatal("another running session must keep the W it opened with")
	}
	if o, st := r.override(other.ID); o != nil || st.Present {
		t.Fatalf("the other session's S must not be touched: %+v %+v", o, st)
	}
	disc := decode[harnessconfig.RequestResult](t, r.callRaw(otherCS, harness.ProposeTool, map[string]any{}))
	if row := rowOf(disc.Profile, "vault"); row.Running == harnessconfig.AvailableOn {
		t.Fatalf("the other session must not be reported as serving the group: %+v", row)
	}
}

func TestDecliningTheChoiceOrTheApprovalChangesNothing(t *testing.T) {
	for name, answer := range map[string]map[interaction.Kind]string{
		"cancelled choice":  {interaction.KindHarnessScope: "", interaction.KindHarness: "allow"},
		"unoffered choice":  {interaction.KindHarnessScope: "everywhere", interaction.KindHarness: "allow"},
		"denied approval":   {interaction.KindHarnessScope: "session", interaction.KindHarness: "deny"},
		"unreadable answer": {interaction.KindHarnessScope: "session", interaction.KindHarness: "maybe"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newCompRig(t, compOptions{controls: true})
			r.prompter.answers = answer
			wBefore := r.workspaceOff("vault")
			sess, spec := r.create(agenthub.AgentTypeClaude)
			cs := r.connect(spec)

			res := r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"vault"}})
			if !res.IsError {
				t.Fatalf("a declined request must be a failure response: %+v", res)
			}
			if o, st := r.override(sess.ID); o != nil || st.Present {
				t.Fatalf("nothing may be saved: %+v %+v", o, st)
			}
			if _, st, _ := r.asm.Store.GetProfile(context.Background(), r.wsID); st != wBefore {
				t.Fatal("W moved")
			}
			if hasGroup(r.listed(cs), "vault") {
				t.Fatal("the group must stay hidden")
			}
			if n := r.asm.Service.Pending(); n != 0 {
				t.Fatalf("a declined request left %d preparations behind", n)
			}
		})
	}
}

func TestProposeIsServedWhenItsGroupIsOffAndGetIsNot(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	r.workspaceOff("harness")
	_, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)

	listed := r.listed(cs)
	if contains(listed, "harness.profile.get") || !contains(listed, harness.ProposeTool) {
		t.Fatalf("only propose is the always-served bootstrap: %v", listed)
	}
	res := r.callRaw(cs, "harness.profile.get", map[string]any{})
	if !res.IsError {
		t.Fatalf("get must follow its group policy: %+v", res)
	}
	disc := decode[harnessconfig.RequestResult](t, r.callRaw(cs, harness.ProposeTool, map[string]any{}))
	if !disc.Discovery || rowOf(disc.Profile, "harness").Name == "" {
		t.Fatalf("discovery should make the way back discoverable: %+v", disc)
	}
}

func TestOnlyTheAgentItselfCanAskAndOnlyTheWindowCanEdit(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	sess, spec := r.create(agenthub.AgentTypeClaude)
	_ = spec
	terminal := identity.WithCaller(r.ctx, identity.UserTerminal())
	agent := r.agentCtx(sess.ID)

	if _, err := r.port.Profile(terminal); !isCode(err, harness.CodeProfileDenied) {
		t.Fatalf("a terminal is not an agent session: %v", err)
	}
	if _, err := r.port.Request(r.ui(), harnessconfig.GroupRequest{Groups: []string{"vault"}}); !isCode(err, harness.CodeProfileDenied) {
		t.Fatalf("the window has no session of its own to ask for: %v", err)
	}
	for name, call := range map[string]func() error{
		"workspace profile":  func() error { _, err := r.port.WorkspaceProfile(agent, r.wsID); return err },
		"session profile":    func() error { _, err := r.port.SessionProfile(agent, r.wsID, sess.ID); return err },
		"prepare":            func() error { _, err := r.port.PrepareWorkspaceGroups(agent, r.wsID, nil); return err },
		"revoke":             func() error { _, err := r.port.PrepareRevoke(agent, r.wsID, sess.ID, []string{"vault"}); return err },
		"apply":              func() error { _, err := r.port.Apply(agent, "x"); return err },
		"cancel":             func() error { return r.port.Cancel(agent, "x") },
		"reconcile":          func() error { _, err := r.port.Reconcile(agent, r.wsID, sess.ID); return err },
		"terminal apply":     func() error { _, err := r.port.Apply(terminal, "x"); return err },
		"terminal reconcile": func() error { _, err := r.port.Reconcile(terminal, r.wsID, sess.ID); return err },
	} {
		if err := call(); !isCode(err, harness.CodeProfileDenied) {
			t.Errorf("%s: only the window may do this, got %v", name, err)
		}
	}

	// The shared terminal socket, over a real SDK client, is refused too.
	st, ct := sdk.NewInMemoryTransports()
	ss, err := r.server.MCP().Connect(r.ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "term", Version: "0"}, nil).Connect(r.ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res := r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"vault"}})
	if !res.IsError {
		t.Fatalf("the shared socket must not receive an agent's authority: %+v", res)
	}
}

func TestAnUnknownGroupAndAnOversizeRequestAreRefusedBeforeAnyoneIsAsked(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	_, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)

	many := make([]string, 40)
	for i := range many {
		many[i] = fmt.Sprintf("g%d", i)
	}
	for name, groups := range map[string][]string{"unknown": {"nonesuch"}, "oversize": many, "blank": {" "}} {
		if res := r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": groups}); !res.IsError {
			t.Errorf("%s: should be refused: %+v", name, res)
		}
	}
	if n := len(kinds(r.prompter)); n != 0 {
		t.Fatalf("a refused request must not ask anyone, asked %d times", n)
	}
}

func isCode(err error, code string) bool {
	var he *harness.Error
	return errors.As(err, &he) && he.Code == code
}

func TestACommittedChangeWhoseEndpointCannotBePublishedKeepsItsFactsThroughTheSDKAndReconciles(t *testing.T) {
	var f *fault
	r := newCompRig(t, compOptions{controls: true, wrapStore: func(p harnessPersistence) harnessPersistence {
		f = &fault{harnessPersistence: p}
		return f
	}})
	r.prompter.answers = answers("session")
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	f.mode.Store("pending") // reads fail only after the change's own commit

	res := r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"vault"}})
	if !res.IsError {
		t.Fatalf("a saved-but-paused change is a failure response: %+v", res)
	}
	out := decode[harnessconfig.RequestResult](t, res).Outcome
	if out == nil {
		t.Fatalf("the failure response must keep the committed facts: %+v", res.StructuredContent)
	}
	if out.Status != harnessconfig.OutcomePaused || out.Commit != harnessconfig.CommitCommitted || out.Publication != harnessconfig.PublicationPending || out.Live {
		t.Fatalf("saved, paused, not live: %+v", out)
	}
	if out.OverrideRevision == 0 || !out.OverridePresent || out.Code != harness.CodeSnapshotUnavailable || out.Recovery == "" || out.Message == "" {
		t.Fatalf("stamps, public code and recovery text are the contract: %+v", out)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			text += tc.Text
		}
	}
	if !strings.Contains(text, "saved") || strings.Contains(text, "read failed") {
		t.Fatalf("the text is the fixed host sentence, never the cause: %q", text)
	}

	// Nothing may be proposed or applied again, and the endpoint serves nothing new.
	f.failReads.Store(false)
	f.mode.Store("")
	o, st := r.override(sess.ID)
	if o == nil || !contains(o.Groups, "vault") || st.Revision != out.OverrideRevision {
		t.Fatalf("the change IS saved: %+v %+v", o, st)
	}
	if again := r.callRaw(cs, "workspace.list", map[string]any{}); !again.IsError {
		t.Fatalf("a paused endpoint refuses calls until it is reconciled: %+v", again)
	}

	// Reconcile (the window) installs the latest durable S and reopens the endpoint.
	rec, err := r.port.Reconcile(r.ui(), r.wsID, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != harnessconfig.OutcomeApplied || !rec.Live || rec.Publication != harnessconfig.PublicationInstalled {
		t.Fatalf("reconcile should leave the endpoint live: %+v", rec)
	}
	if !hasGroup(r.listed(cs), "vault") {
		t.Fatal("after reconcile the group is served")
	}
	again, err := r.port.Reconcile(r.ui(), r.wsID, sess.ID)
	if err != nil || again.Status != harnessconfig.OutcomeApplied || !again.Live {
		t.Fatalf("reconcile is idempotent: %+v %v", again, err)
	}
}

func TestAnUnknownCommitIsNeitherARollbackNorASuccess(t *testing.T) {
	var f *fault
	r := newCompRig(t, compOptions{controls: true, wrapStore: func(p harnessPersistence) harnessPersistence {
		f = &fault{harnessPersistence: p}
		return f
	}})
	r.prompter.answers = answers("session")
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	f.mode.Store("unknown")

	res := r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"vault"}})
	if !res.IsError {
		t.Fatalf("an unknown commit is a failure response: %+v", res)
	}
	out := decode[harnessconfig.RequestResult](t, res).Outcome
	if out == nil || out.Status != harnessconfig.OutcomeUnknown || out.Commit != harnessconfig.CommitUnknown || out.Live {
		t.Fatalf("unknown commit, not live: %+v", out)
	}
	if out.Recovery == "" || !strings.Contains(strings.ToLower(out.Recovery), "reconcile") {
		t.Fatalf("it points to reconciliation, not to proposing again: %q", out.Recovery)
	}
	// Fail closed: the endpoint neither lists nor serves anything until reconciled.
	for tool, err := range cs.Tools(r.ctx, nil) {
		if err == nil && harness.Group(tool.Name) == "vault" {
			t.Fatal("an unknown commit must not widen what is served")
		}
	}
	if !r.callRaw(cs, "workspace.list", map[string]any{}).IsError {
		t.Fatal("a paused endpoint must refuse calls")
	}

	// The write did land. Reconciliation reads the durable truth and serves it.
	f.failReads.Store(false)
	f.mode.Store("")
	if o, _ := r.override(sess.ID); o == nil || !contains(o.Groups, "vault") {
		t.Fatalf("the durable S holds the group: %+v", o)
	}
	rec, err := r.port.Reconcile(r.ui(), r.wsID, sess.ID)
	if err != nil || !rec.Live {
		t.Fatalf("reconcile: %+v %v", rec, err)
	}
	if !hasGroup(r.listed(cs), "vault") {
		t.Fatal("reconciled: the group is served")
	}
}

// Enabling a group through the public request makes its tools served, never
// exempt and never pre-approved: the launch permission lists are fixed, and the
// environment rule of the tool itself runs on every call.
func TestAGroupEnabledByARequestKeepsTheLaunchPermissionsAndTheEnvironmentRule(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true, realEnvs: true})
	r.prompter.answers = answers("session")
	r.workspaceOff("db")
	_, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	preApproved := strings.Join(spec.PreApprovedMCP["devkit"], ",")
	gateOwned := strings.Join(spec.GateOwnedMCP["devkit"], ",")

	if out := decode[harnessconfig.RequestResult](t, r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"db"}})).Outcome; out == nil || !out.Live {
		t.Fatalf("setup: expected a live change: %+v", out)
	}
	if got := r.vendors[agenthub.AgentTypeClaude].LastSpec(); strings.Join(got.PreApprovedMCP["devkit"], ",") != preApproved || strings.Join(got.GateOwnedMCP["devkit"], ",") != gateOwned {
		t.Fatal("a live enable must not add a pre-approval or a gate delegation to the launch")
	}
	for _, list := range [][]string{spec.PreApprovedMCP["devkit"], spec.GateOwnedMCP["devkit"]} {
		if contains(list, "db.query") || contains(list, "db.testConnection") {
			t.Fatalf("enabling a group must not pre-approve its tools: %v", list)
		}
	}

	args := map[string]any{"profileId": probeProfileID}
	r.prompter.mu.Lock()
	r.prompter.answers[interaction.KindDatabase] = "allow_always"
	r.prompter.mu.Unlock()
	for _, tag := range []string{"prod", "a-tag-nobody-defined"} {
		r.dbEnv.Store(tag)
		before := len(kinds(r.prompter))
		for i := 1; i <= 2; i++ {
			_ = r.callWith(cs, "db.testConnection", args)
			if n := len(kinds(r.prompter)) - before; n != i {
				t.Fatalf("tag %q must ask on every call: %d prompts after %d calls", tag, n, i)
			}
		}
	}
	r.dbEnv.Store("prod")
	r.prompter.mu.Lock()
	r.prompter.answers[interaction.KindDatabase] = "deny"
	r.prompter.mu.Unlock()
	handled := r.gate.handled.Load()
	_ = r.callWith(cs, "db.testConnection", args)
	if r.gate.handled.Load() != handled {
		t.Fatal("a denied confirmation must stop the call before its handler")
	}
}

// A transport that goes away while the person is choosing leaves nothing behind:
// no preparation, no pending question, no override.
func TestADisconnectWhileChoosingLeavesNothingBehind(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.workspaceOff("vault")
	sess, _ := r.create(agenthub.AgentTypeClaude)
	ctx, cancel := context.WithCancel(r.agentCtx(sess.ID))
	defer cancel()
	// The caller disappears as the question is shown, before any answer.
	r.prompter.mu.Lock()
	r.prompter.during = func() { cancel(); time.Sleep(50 * time.Millisecond) }
	r.prompter.mu.Unlock()

	if _, err := r.port.Request(ctx, harnessconfig.GroupRequest{Groups: []string{"vault"}}); err == nil {
		t.Fatal("a vanished caller cannot have chosen anything")
	}
	time.Sleep(100 * time.Millisecond)
	if n := r.asm.Service.Pending(); n != 0 {
		t.Fatalf("left %d preparations behind", n)
	}
	if left := r.hub.PendingList(); len(left) != 0 {
		t.Fatalf("left %d questions open", len(left))
	}
	if o, st := r.override(sess.ID); o != nil || st.Present {
		t.Fatalf("nothing may be saved: %+v %+v", o, st)
	}
}

// Without the grants the default policy installs, the tools are refused by the
// capability gateway, not by the facade: the authority to ask is itself a grant.
func TestTheToolsNeedTheirOwnGrants(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true, grants: []permissions.Grant{{SubjectID: "builtin.workspace", Capability: "ws:read", Scope: "workspace:list"}}})
	r.prompter.answers = answers("session")
	r.workspaceOff("vault")
	_, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	for _, tool := range []string{"harness.profile.get", harness.ProposeTool} {
		res := r.callRaw(cs, tool, map[string]any{"groups": []string{"vault"}})
		if !res.IsError {
			t.Errorf("%s must be refused without its grant: %+v", tool, res)
		}
	}
	if len(kinds(r.prompter)) != 0 {
		t.Fatal("a refused grant must not reach the person")
	}
}

// Production wiring stays inert: main.go builds no harness assembly, binds no
// port and hands the MCP server no controls. This is a tripwire for the activation
// change, which must update it together with the evidence it depends on
// (docs/agent-reference/harness-composition-activation.md).
func TestProductionDoesNotWireTheHarnessYet(t *testing.T) {
	raw, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	for _, forbidden := range []string{"NewHarnessAssembly", "ComposeHarness", "NewHarnessPort", "Harness:", ".Harness.Bind"} {
		if strings.Contains(src, forbidden) {
			t.Errorf("main.go mentions %q: production activation has not been accepted", forbidden)
		}
	}
}
