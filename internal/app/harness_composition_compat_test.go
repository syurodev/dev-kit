package app

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- historical attribution -------------------------------------------------

// A live S publication, a refused change, a failed launch, a reconciliation and a
// close never rewrite a row that was recorded: tool-usage rows and the facts a
// turn started with are history, and NULL stays unknown.
func TestHistoricalAttributionIsNeverRewrittenByTheHarness(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := r.call(cs, "vault.status"); !strings.Contains(got, "unlocked") {
		t.Fatalf("%q", got)
	}
	if _, err := r.agents.Invoke(r.ui(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "with S"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.apply(sess.ID, nil, []string{"vault"}); err != nil {
		t.Fatal(err)
	}
	_ = r.call(cs, "vault.status") // refused under the new serving state
	if _, err := r.agents.Invoke(r.ui(), agenthub.InvokeInput{SessionID: sess.ID, Prompt: "without S"}); err != nil {
		t.Fatal(err)
	}
	rows, facts := r.usageRows(), r.turnFacts(sess.ID)
	if len(rows) < 2 || len(facts) != 2 {
		t.Fatalf("fixture: rows=%v facts=%v", rows, facts)
	}
	if !strings.HasSuffix(facts[0], "=1") || !strings.HasSuffix(facts[1], "=NULL") {
		t.Fatalf("a turn starts with the facts of the moment it started: %v", facts)
	}

	// 1. A change that is refused after the dialog (the write fails and rolls back).
	if _, err := r.db.Exec(`CREATE TRIGGER fail_s BEFORE UPDATE ON session_harness_overrides BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err == nil {
		t.Fatal("the failed write must fail the apply")
	}
	if _, err := r.db.Exec(`DROP TRIGGER fail_s`); err != nil {
		t.Fatal(err)
	}
	// 2. A launch that cannot be resolved.
	var body string
	if err := r.db.QueryRow(`SELECT body FROM workspace_harness_profiles WHERE workspace_id=?`, r.wsID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec(`UPDATE workspace_harness_profiles SET body='{bad' WHERE workspace_id=?`, r.wsID); err != nil {
		t.Fatal(err)
	}
	if err := r.agents.ResumeSession(r.ui(), sess.ID, true); !errors.Is(err, agenthub.ErrHarnessUnavailable) {
		t.Fatalf("want an unresolved-configuration refusal, got %v", err)
	}
	if _, err := r.db.Exec(`UPDATE workspace_harness_profiles SET body=? WHERE workspace_id=?`, body, r.wsID); err != nil {
		t.Fatal(err)
	}
	// 3. A reconciliation with nothing to reconcile.
	if _, err := r.asm.Service.Reconcile(r.agentCtx(sess.ID), sess.ID); err != nil {
		t.Fatal(err)
	}
	// 4. Close.
	if err := r.agents.CloseSession(r.ui(), sess.ID); err != nil {
		t.Fatal(err)
	}
	if after := r.usageRows(); !slices.Equal(after, rows) {
		t.Fatalf("the harness rewrote tool-usage history:\nbefore %v\nafter  %v", rows, after)
	}
	if after := r.turnFacts(sess.ID); !slices.Equal(after, facts) {
		t.Fatalf("the harness rewrote the facts a turn started with:\nbefore %v\nafter  %v", facts, after)
	}
}

func TestARewindLeavesToolUsageHistoryAlone(t *testing.T) {
	r := newCompRig(t, compOptions{})
	sess, prompts := promptedSession(t, r)
	cs := r.connect(r.vendors[agenthub.AgentTypeClaude].LastSpec())
	_ = r.call(cs, "vault.status")
	rows := r.usageRows()
	if len(rows) == 0 {
		t.Fatal("fixture: a call must have been recorded")
	}
	if err := r.rewind(sess.ID, prompts[1]); err != nil {
		t.Fatal(err)
	}
	if after := r.usageRows(); !slices.Equal(after, rows) {
		t.Fatalf("a rewind rewrote tool-usage history:\nbefore %v\nafter  %v", rows, after)
	}
}

// A session with no workspace (a scratch or catalog run) has no harness scope: it
// launches as it always did, records no applied reference and is not "live".
func TestASessionWithNoWorkspaceStaysLegacyAndWritesNoReference(t *testing.T) {
	r := newCompRig(t, compOptions{})
	outside := t.TempDir() // inside no workspace
	sess, err := r.agents.CreateSession(r.ui(), agenthub.CreateInput{AgentType: agenthub.AgentTypeClaude, Cwd: outside})
	if err != nil {
		t.Fatal(err)
	}
	if sess.WorkspaceID != "" {
		t.Fatalf("fixture: the session must have no workspace, got %q", sess.WorkspaceID)
	}
	spec := r.vendors[agenthub.AgentTypeClaude].LastSpec()
	if spec.DevKit != nil {
		t.Fatalf("a workspace-less launch is the legacy launch: %+v", spec.DevKit)
	}
	st, err := r.agents.HarnessStatus(context.Background(), sess.ID)
	if err != nil || st.State != agenthub.HarnessUnconfigured || st.Live {
		t.Fatalf("status = %+v %v", st, err)
	}
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM agent_session_harness_applied WHERE session_id=?`, sess.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a workspace-less session must not gain an applied reference: %d %v", n, err)
	}
	if v := r.agents.ServingTarget(sess.ID); v.Active {
		t.Fatalf("a legacy endpoint is not a policy endpoint: %+v", v)
	}
}

// Deleting a session removes its subtree; a late approval for any of them is
// refused and no coordinator, endpoint or override outlives the rows.
func TestDeletingAParentRemovesTheSubtreeAndEveryHarnessRecord(t *testing.T) {
	r := newCompRig(t, compOptions{})
	r.workspaceOff("vault")
	parent, _ := r.create(agenthub.AgentTypeClaude)
	child, err := r.agents.CreateSession(r.agentCtx(parent.ID), agenthub.CreateInput{AgentType: agenthub.AgentTypeClaude, Cwd: r.root, ParentSessionID: parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{parent.ID, child.ID} {
		if _, err := r.apply(id, []string{"vault"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	late, err := r.propose(child.ID, nil, []string{"vault"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.agents.DeleteSession(r.ui(), parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.asm.Service.Apply(r.agentCtx(child.ID), late.ID); err == nil {
		t.Fatal("a late approval for a deleted child must be refused")
	}
	for _, id := range []string{parent.ID, child.ID} {
		if v := r.agents.ServingTarget(id); v.Active {
			t.Fatalf("a deleted session has no endpoint: %+v", v)
		}
		for _, table := range []string{"session_harness_overrides", "agent_session_harness_applied", "agent_sessions"} {
			col := "session_id"
			if table == "agent_sessions" {
				col = "id"
			}
			var n int
			if err := r.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE `+col+`=?`, id).Scan(&n); err != nil || n != 0 {
				t.Fatalf("%s rows for deleted session %s: %d %v", table, id, n, err)
			}
		}
	}
	if socks, _ := filepath.Glob(filepath.Join(r.sockDir, "*.sock")); len(socks) != 0 {
		t.Fatalf("endpoints outlived their sessions: %v", socks)
	}
}

// ---- terminal / server mode / optional modules / empty -----------------------

// The shared (terminal) server has no policy holder: nothing a session's override
// does reaches it.
func TestTheUnprofiledTerminalSurfaceIsIndependentOfSessionPolicy(t *testing.T) {
	r := newCompRig(t, compOptions{})
	list := func() []string {
		st, ct := sdk.NewInMemoryTransports()
		ss, err := r.server.MCP().Connect(r.ctx, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ss.Close()
		cs, err := sdk.NewClient(&sdk.Implementation{Name: "terminal", Version: "0"}, nil).Connect(r.ctx, ct, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer cs.Close()
		return r.listed(cs)
	}
	inv, _ := r.inventory()
	before := list()
	if len(before) != len(inv.Tools) {
		t.Fatalf("the terminal surface serves the whole registry: %d of %d", len(before), len(inv.Tools))
	}
	r.workspaceOff("vault")
	sess, _ := r.create(agenthub.AgentTypeClaude)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.apply(sess.ID, nil, []string{"vault"}); err != nil {
		t.Fatal(err)
	}
	if after := list(); !slices.Equal(after, before) {
		t.Fatalf("a session's policy changed the terminal surface: %d -> %d tools", len(before), len(after))
	}
}

// Server mode keeps the vendor types and refuses their sessions, with the hub
// headless so nothing can approve itself. The assembly does not make it an
// alternate route: a launch is refused and leaves nothing behind.
func TestServerModeRefusesSessionsAndLeavesNothingBehind(t *testing.T) {
	r := newCompRig(t, compOptions{serverMode: true})
	r.hub.Headless = true
	_, err := r.agents.CreateSession(r.ui(), agenthub.CreateInput{AgentType: agenthub.AgentTypeClaude, Cwd: r.root, WorkspaceID: r.wsID})
	if !errors.Is(err, agenthub.ErrUnavailableInServerMode) {
		t.Fatalf("want the server-mode refusal, got %v", err)
	}
	if socks, _ := filepath.Glob(filepath.Join(r.sockDir, "*.sock")); len(socks) != 0 {
		t.Fatalf("a refused launch leaked endpoints: %v", socks)
	}
	if (harnessConfirmer{hub: r.hub}).Available() {
		t.Fatal("a headless hub can never approve a harness change")
	}
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM agent_sessions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("session rows after a refused launch: %d %v", n, err)
	}
}

func TestAnUnwiredOptionalModuleIsNotRegisteredServedOrAdvertised(t *testing.T) {
	r := newCompRig(t, compOptions{noPDF: true})
	inv, err := r.inventory()
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range inv.Tools {
		if harness.Group(tool.Name) == "pdf" {
			t.Fatalf("an unwired module registered %s", tool.Name)
		}
	}
	_, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	if hasGroup(r.listed(cs), "pdf") || hasGroup(spec.DevKit.Tools, "pdf") || hasGroup(spec.PreApprovedMCP["devkit"], "pdf") {
		t.Fatal("an absent module must not be listed, described or pre-approved")
	}
	// And the full inventory has it when it is wired.
	full := newCompRig(t, compOptions{})
	fullInv, _ := full.inventory()
	wired := false
	for _, tool := range fullInv.Tools {
		wired = wired || harness.Group(tool.Name) == "pdf"
	}
	if !wired || len(fullInv.Tools) <= len(inv.Tools) {
		t.Fatalf("the wired registry must be larger: %d vs %d", len(fullInv.Tools), len(inv.Tools))
	}
}

func TestAPolicyResolvedAgainstAnotherRegistryIsRefusedByTheComposedHost(t *testing.T) {
	r := newCompRig(t, compOptions{})
	inv, _ := r.inventory()
	short := harness.Registry{Tools: inv.Tools[:len(inv.Tools)-1]}
	eff, err := harness.Resolve(harness.Inputs{Registry: short})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.host.OpenWithPolicy("aaaaaaaa-0000-0000-0000-000000000000", eff)
	var he *harness.Error
	if !errors.As(err, &he) || he.Code != harness.CodeNotEnforceable {
		t.Fatalf("a registry mismatch must refuse, got %v", err)
	}
}

func TestAConfiguredEmptyEndpointStillDiscoversALaterEnable(t *testing.T) {
	r := newCompRig(t, compOptions{})
	inv, _ := r.inventory()
	var groups []string
	for _, tool := range inv.Tools {
		if g := harness.Group(tool.Name); !slices.Contains(groups, g) {
			groups = append(groups, g)
		}
	}
	r.workspaceOff(groups...)
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	if got := r.listed(cs); len(got) != 0 {
		t.Fatalf("every group is off: %v", got)
	}
	if spec.DevKit == nil || len(spec.DevKit.Tools) != 0 {
		t.Fatalf("a policy that serves nothing is positive and empty, not legacy: %+v", spec.DevKit)
	}
	if len(spec.PreApprovedMCP["devkit"]) != 0 {
		t.Fatalf("nothing is pre-approved when nothing is served: %v", spec.PreApprovedMCP["devkit"])
	}
	res, err := r.apply(sess.ID, []string{"vault"}, nil)
	if err != nil || !res.Live {
		t.Fatalf("apply = %+v %v", res, err)
	}
	if !hasGroup(r.listed(cs), "vault") {
		t.Fatal("a later enable must be discoverable on an endpoint that started empty")
	}
	// Enabling never pre-approves: the launch lists are fixed until a reload.
	if len(spec.PreApprovedMCP["devkit"]) != 0 {
		t.Fatal("the launch permission lists must stay as launched")
	}
}
