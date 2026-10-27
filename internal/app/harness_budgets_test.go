package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
	"devkit/internal/interaction"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Result budgets through the real stack: SQLite HarnessStore, the preferences
// policy source and writer boundary, the shared hub, the MCP server and
// SessionHost, SDK clients and the H4c coordinator. Vendors and the person's
// answers are scripted; nothing calls a model or an account.

func kibp(n int) *int { v := n * 1024; return &v }

func identityTerminal(r *compRig) context.Context {
	return identity.WithCaller(r.ctx, identity.UserTerminal())
}

func (r *compRig) budgetOf(sessionID, tool string) harness.Budget {
	r.t.Helper()
	v := r.agents.ServingTarget(sessionID)
	if !v.Active {
		r.t.Fatalf("session %s has no endpoint", sessionID)
	}
	b, ok := v.Effective.Budget(tool)
	if !ok {
		r.t.Fatalf("%s is not registered", tool)
	}
	return b
}

func (r *compRig) savedW() *harness.Profile {
	r.t.Helper()
	w, _, err := r.asm.Store.GetProfile(context.Background(), r.wsID)
	if err != nil {
		r.t.Fatal(err)
	}
	return w
}

func budgetCall(r *compRig, cs *sdk.ClientSession, edits ...map[string]any) *sdk.CallToolResult {
	r.t.Helper()
	return r.callRaw(cs, harness.ProposeTool, map[string]any{"budgets": edits, "rationale": "tune the budget"})
}

func (r *compRig) resetPrompts() {
	r.prompter.mu.Lock()
	r.prompter.prompts = nil
	r.prompter.mu.Unlock()
}

// A saved budget edit reaches the next open, not a running session: the requester
// and every other running endpoint keep the limits they opened with, and a session
// opened afterwards gets the saved ones. There is no scope question: it is the
// workspace profile, and S is never written.
func TestABudgetEditChangesTheSavedProfileAndNoRunningEndpoint(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	a, aSpec := r.create(agenthub.AgentTypeClaude)
	b, _ := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(aSpec)
	_, sBefore := r.override(a.ID)

	res := budgetCall(r, cs, map[string]any{"kind": "group", "name": "vault", "bytes": 8 * 1024, "cut": "headTail"})
	if res.IsError {
		t.Fatalf("budget proposal failed: %+v", res.Content)
	}
	out := decode[harnessconfig.RequestResult](t, res).Outcome
	if out == nil || out.Status != harnessconfig.OutcomeApplied || out.Target != "workspace" || out.Live || !out.ReloadRequired || out.OverridePresent {
		t.Fatalf("a saved workspace edit: not live, needs a reload, no S: %+v", out)
	}
	if k := kinds(r.prompter); len(k) != 1 || k[0] != interaction.KindHarness {
		t.Fatalf("a budget edit has no scope to choose: only the confirmation is asked, got %v", k)
	}
	if rule := r.savedW().ResultBudget.Groups["vault"]; rule.Bytes == nil || *rule.Bytes != 8*1024 || rule.Cut != harness.CutHeadTail {
		t.Fatalf("saved rule = %+v", rule)
	}
	if _, sAfter := r.override(a.ID); sAfter != sBefore {
		t.Fatal("S moved: it holds enabled groups only")
	}

	for _, id := range []string{a.ID, b.ID} {
		if got := r.budgetOf(id, "vault.status"); got.Bytes != harness.DefaultResultBudget || got.Cut != harness.CutHead {
			t.Errorf("%s: a running endpoint must keep its pinned limits, got %+v", id, got)
		}
	}
	if hs, _ := r.agents.HarnessStatus(r.ui(), a.ID); hs.State != agenthub.HarnessReloadRequired || hs.Live != true {
		t.Errorf("the requester should be told to reload, and still be live on what it has: %+v", hs.State)
	}
	c, _ := r.create(agenthub.AgentTypeClaude)
	if got := r.budgetOf(c.ID, "vault.status"); got.Bytes != 8*1024 || got.Cut != harness.CutHeadTail {
		t.Fatalf("a session opened afterwards gets the saved limits, got %+v", got)
	}
}

// A live S change is resolved against the W the session opened with, so enabling a
// group after a budget edit does not pick up the new saved budget.
func TestAGroupEnabledAfterABudgetEditUsesThePinnedLaunchBasis(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	r.workspaceOff("vault")
	a, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)

	// The saved profile now also carries an 8 KiB limit for the vault group.
	if res := budgetCall(r, cs, map[string]any{"kind": "group", "name": "vault", "bytes": 8 * 1024}); res.IsError {
		t.Fatalf("budget edit: %+v", res.Content)
	}
	if res := r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"vault"}}); res.IsError {
		t.Fatalf("enable: %+v", res.Content)
	}
	if !hasGroup(r.listed(cs), "vault") {
		t.Fatal("setup: the group should be live")
	}
	if got := r.budgetOf(a.ID, "vault.status"); got.Bytes != harness.DefaultResultBudget {
		t.Fatalf("the live group must use the pinned launch budget (24 KiB), not the newer saved 8 KiB: %+v", got)
	}
	// Saved: the next launch has both the group off in W and the 8 KiB rule.
	if w := r.savedW(); *w.ResultBudget.Groups["vault"].Bytes != 8*1024 || w.ToolGroups["vault"] {
		t.Fatalf("saved W: %+v", w)
	}
}

func narrowing() map[string]any {
	return map[string]any{"kind": "tool", "name": "vault.status", "bytes": 8 * 1024}
}

// The opt-in lets an agent's narrowing budget edit skip the question, and nothing
// else. Every other proposal still asks; and with nobody to ask it refuses.
func TestAutoNarrowingSkipsTheQuestionOnlyForAnAgentsNarrowingBudgetEdit(t *testing.T) {
	setup := func(t *testing.T, on bool) (*compRig, *sdk.ClientSession) {
		r := newCompRig(t, compOptions{controls: true})
		r.prompter.answers = answers("session")
		if on {
			if _, err := r.prefs.SetAgentHarnessAutoNarrowing(0, true); err != nil {
				t.Fatal(err)
			}
		}
		_, spec := r.create(agenthub.AgentTypeClaude)
		return r, r.connect(spec)
	}

	t.Run("off: it asks", func(t *testing.T) {
		r, cs := setup(t, false)
		if res := budgetCall(r, cs, narrowing()); res.IsError || len(kinds(r.prompter)) != 1 {
			t.Fatalf("by default every proposal asks: err=%v asked=%v", res.IsError, kinds(r.prompter))
		}
	})
	t.Run("on: an agent's narrowing is applied without a question", func(t *testing.T) {
		r, cs := setup(t, true)
		res := budgetCall(r, cs, narrowing())
		if res.IsError || len(kinds(r.prompter)) != 0 {
			t.Fatalf("expected an unasked, applied edit: err=%v asked=%v", res.IsError, kinds(r.prompter))
		}
		if out := decode[harnessconfig.RequestResult](t, res).Outcome; out == nil || out.Status != harnessconfig.OutcomeApplied || out.Live {
			t.Fatalf("outcome: %+v", out)
		}
		if b := r.savedW().ResultBudget.Tools["vault.status"]; b.Bytes == nil || *b.Bytes != 8*1024 {
			t.Fatalf("saved: %+v", b)
		}
	})
	t.Run("on: a widening or mixed edit still asks", func(t *testing.T) {
		r, cs := setup(t, true)
		if res := budgetCall(r, cs, narrowing()); res.IsError { // 24 → 8 KiB, unasked
			t.Fatal(res.Content)
		}
		r.resetPrompts()
		widen := map[string]any{"kind": "tool", "name": "vault.status", "resetBytes": true} // 8 KiB → 24 KiB
		if res := budgetCall(r, cs, widen); res.IsError || len(kinds(r.prompter)) != 1 {
			t.Fatalf("a widening must ask: err=%v asked=%v", res.IsError, kinds(r.prompter))
		}
		r.resetPrompts()
		mixed := []map[string]any{
			{"kind": "tool", "name": "vault.status", "bytes": 2 * 1024},
			{"kind": "tool", "name": "workspace.list", "bytes": 64 * 1024}, // 24 → 64 KiB hidden in the batch
		}
		if res := budgetCall(r, cs, mixed...); res.IsError || len(kinds(r.prompter)) != 1 {
			t.Fatalf("a widening among narrowing edits must ask: err=%v asked=%v", res.IsError, kinds(r.prompter))
		}
	})
	t.Run("on: the window and group requests still ask", func(t *testing.T) {
		r, cs := setup(t, true)
		review, err := r.port.PrepareBudgetEdits(r.ui(), r.wsID, []harnessconfig.BudgetEdit{{Kind: harnessconfig.BudgetTool, Name: "vault.status", Bytes: kibp(8)}})
		if err != nil || !review.NeedsConfirmation {
			t.Fatalf("a window edit is always confirmed: %+v %v", review, err)
		}
		if _, err := r.port.Apply(r.ui(), review.ID); err != nil {
			t.Fatal(err)
		}
		if k := kinds(r.prompter); len(k) != 1 || k[0] != interaction.KindHarness {
			t.Fatalf("the window's change is asked once: %v", k)
		}
		r.workspaceOff("vault")
		r.resetPrompts()
		if res := r.callRaw(cs, harness.ProposeTool, map[string]any{"groups": []string{"vault"}}); res.IsError || len(kinds(r.prompter)) != 2 {
			t.Fatalf("a group request still chooses a scope and asks: %v", kinds(r.prompter))
		}
	})
	t.Run("on but nobody to ask: refused, nothing written", func(t *testing.T) {
		r, cs := setup(t, true)
		r.hub.Headless = true
		if res := budgetCall(r, cs, narrowing()); !res.IsError {
			t.Fatalf("headless must fail closed even for an opted-in narrowing: %+v", res)
		}
		if w := r.savedW(); w != nil && w.ResultBudget != nil {
			t.Fatalf("nothing may be written: %+v", w.ResultBudget)
		}
	})
}

// Any change of the policy between Prepare and the commit makes the proposal stale
// — including switching it on and off again, which a boolean cannot show.
func TestAPolicyChangeWhileAProposalIsPendingMakesItStale(t *testing.T) {
	for name, flips := range map[string][]bool{"switched on": {true}, "on and off again": {true, false}} {
		t.Run(name, func(t *testing.T) {
			r := newCompRig(t, compOptions{controls: true})
			r.prompter.answers = answers("session")
			_, spec := r.create(agenthub.AgentTypeClaude)
			cs := r.connect(spec)

			r.prompter.mu.Lock()
			r.prompter.during = func() { // the person is deciding; the lock is not held
				rev := int64(0)
				for _, on := range flips {
					p, err := r.prefs.SetAgentHarnessAutoNarrowing(rev, on)
					if err != nil {
						t.Error(err)
						return
					}
					rev = p.Revision
				}
			}
			r.prompter.mu.Unlock()

			res := budgetCall(r, cs, narrowing())
			if !res.IsError || !strings.Contains(resultText(res), harness.CodeProposalStale) {
				t.Fatalf("a policy change must stale the proposal: %+v", res)
			}
			if w := r.savedW(); w != nil && w.ResultBudget != nil {
				t.Fatalf("nothing may be written: %+v", w.ResultBudget)
			}
		})
	}
}

func resultText(res *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// The agent can neither read nor write the policy, and cannot carry an approval,
// a revision or an executable flag in its budget form.
func TestTheAgentFormsCarryNoPolicyApprovalOrExecutableField(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	_, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	for tool, err := range cs.Tools(r.ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		if tool.Name != harness.ProposeTool && tool.Name != "harness.profile.get" {
			continue
		}
		raw, _ := json.Marshal(tool.InputSchema)
		for _, forbidden := range []string{"policy", "autoNarrowing", "allowExecutable", "approve", "allow_always", "allowAlways", "workspaceId", "sessionId", "target", "servers", "skills"} {
			if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(forbidden)) {
				t.Errorf("%s accepts %q: %s", tool.Name, forbidden, raw)
			}
		}
	}
	// And a smuggled field is not a way in: unknown properties do not select a target.
	res := r.callRaw(cs, harness.ProposeTool, map[string]any{"budgets": []map[string]any{narrowing()}, "autoNarrowing": true, "workspaceId": "other"})
	_ = res
	if p, _ := r.prefs.AgentHarnessPolicy(); p.AutoNarrowing {
		t.Fatal("an agent turned the opt-in on")
	}
}

func TestBudgetRequestsAreBoundedExclusiveAndValidatedAgainstTheRegistry(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	_, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)

	many := make([]map[string]any, 20)
	for i := range many {
		many[i] = map[string]any{"kind": "tool", "name": fmt.Sprintf("vault.t%d", i), "bytes": 8 * 1024}
	}
	for name, args := range map[string]map[string]any{
		"with groups":    {"groups": []string{"vault"}, "budgets": []map[string]any{narrowing()}},
		"too many":       {"budgets": many},
		"unknown tool":   {"budgets": []map[string]any{{"kind": "tool", "name": "vault.nope", "bytes": 8 * 1024}}},
		"exempt tool":    {"budgets": []map[string]any{{"kind": "tool", "name": "pdf.readText", "bytes": 8 * 1024}}},
		"all-exempt":     {"budgets": []map[string]any{{"kind": "group", "name": "pdf", "bytes": 8 * 1024}}},
		"out of range":   {"budgets": []map[string]any{{"kind": "tool", "name": "vault.status", "bytes": 100}}},
		"unknown mode":   {"budgets": []map[string]any{{"kind": "tool", "name": "vault.status", "cut": "middle"}}},
		"empty edit":     {"budgets": []map[string]any{{"kind": "tool", "name": "vault.status"}}},
		"set and reset":  {"budgets": []map[string]any{{"kind": "tool", "name": "vault.status", "bytes": 8 * 1024, "resetBytes": true}}},
		"duplicate edit": {"budgets": []map[string]any{narrowing(), narrowing()}},
	} {
		if res := r.callRaw(cs, harness.ProposeTool, args); !res.IsError {
			t.Errorf("%s should be refused: %+v", name, res)
		}
	}
	if len(kinds(r.prompter)) != 0 || r.asm.Service.Pending() != 0 {
		t.Fatalf("a refused request must not ask anyone or leave a preparation: asked %v, pending %d", kinds(r.prompter), r.asm.Service.Pending())
	}
	if w := r.savedW(); w != nil && w.ResultBudget != nil {
		t.Fatalf("nothing may be written: %+v", w.ResultBudget)
	}
	// A legacy group request is unaffected.
	if res := r.callRaw(cs, harness.ProposeTool, map[string]any{}); res.IsError {
		t.Fatalf("discovery must keep working: %+v", res.Content)
	}
}

// The read is bounded and paged by tool, with the cursor bound to its filter, the
// explicit rules and the effective values with their sources.
func TestTheBudgetReadIsPagedBoundToItsFilterAndShowsSources(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.prompter.answers = answers("session")
	review, err := r.port.PrepareBudgetEdits(r.ui(), r.wsID, []harnessconfig.BudgetEdit{
		{Kind: harnessconfig.BudgetGroup, Name: "vault", Bytes: kibp(8), Cut: "headTail"},
		{Kind: harnessconfig.BudgetTool, Name: "vault.status", Bytes: kibp(16)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.port.Apply(r.ui(), review.ID); err != nil {
		t.Fatal(err)
	}

	first, err := r.port.WorkspaceBudgets(r.ui(), r.wsID, harnessconfig.BudgetFilter{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Tools) != 5 || first.NextCursor == "" || len(first.Groups) == 0 || first.DefaultBytes != 24*1024 || first.MinBytes != 2048 || first.MaxBytes != 65536 {
		t.Fatalf("first page: %d tools, cursor %q, %d groups", len(first.Tools), first.NextCursor, len(first.Groups))
	}
	seen := map[string]bool{}
	cursor, total := first.NextCursor, len(first.Tools)
	for _, row := range first.Tools {
		seen[row.Tool] = true
	}
	for cursor != "" {
		page, err := r.port.WorkspaceBudgets(r.ui(), r.wsID, harnessconfig.BudgetFilter{Limit: 5, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Groups) != 0 {
			t.Fatal("groups belong to the first page only")
		}
		for _, row := range page.Tools {
			if seen[row.Tool] {
				t.Fatalf("%s appeared twice", row.Tool)
			}
			seen[row.Tool] = true
		}
		total += len(page.Tools)
		cursor = page.NextCursor
	}
	if inv, _ := r.server.Inventory(); total != len(inv.Tools) {
		t.Fatalf("paging covered %d tools of %d", total, len(inv.Tools))
	}

	vault, err := r.port.WorkspaceBudgets(r.ui(), r.wsID, harnessconfig.BudgetFilter{Group: "vault"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range vault.Tools {
		switch row.Tool {
		case "vault.status":
			if row.EffectiveBytes != 16*1024 || row.BytesFrom != harnessconfig.BudgetFromTool || row.EffectiveCut != "headTail" || row.CutFrom != harnessconfig.BudgetFromGroup ||
				row.ToolBytes == nil || row.GroupBytes == nil || *row.GroupBytes != 8*1024 {
				t.Errorf("vault.status = %+v", row)
			}
		default:
			if row.EffectiveBytes != 8*1024 || row.BytesFrom != harnessconfig.BudgetFromGroup || row.ToolBytes != nil {
				t.Errorf("%s = %+v", row.Tool, row)
			}
		}
	}
	other, err := r.port.WorkspaceBudgets(r.ui(), r.wsID, harnessconfig.BudgetFilter{Group: "workspace"})
	if err != nil || len(other.Tools) == 0 || other.Tools[0].BytesFrom != harnessconfig.BudgetFromDefault || other.Tools[0].EffectiveBytes != 24*1024 {
		t.Fatalf("an unbudgeted group shows the default: %+v %v", other.Tools, err)
	}
	// A cursor made for one group is not another list's.
	if _, err := r.port.WorkspaceBudgets(r.ui(), r.wsID, harnessconfig.BudgetFilter{Group: "workspace", Cursor: first.NextCursor}); err == nil {
		t.Fatal("a cursor must be bound to its filter")
	}
	if _, err := r.port.WorkspaceBudgets(r.ui(), r.wsID, harnessconfig.BudgetFilter{Cursor: "!!"}); err == nil {
		t.Fatal("a garbled cursor must be refused")
	}
	// Exempt tools are shown as such, with no limit.
	pdf, _ := r.port.WorkspaceBudgets(r.ui(), r.wsID, harnessconfig.BudgetFilter{Group: "pdf"})
	for _, row := range pdf.Tools {
		if !row.Exempt || row.EffectiveBytes != 0 {
			t.Errorf("%s is exempt from cutting and has no limit: %+v", row.Tool, row)
		}
	}
	// The window only.
	if _, err := r.port.WorkspaceBudgets(identityTerminal(r), r.wsID, harnessconfig.BudgetFilter{}); !isCode(err, harness.CodeProfileDenied) {
		t.Fatalf("a terminal may not read the window's budget view: %v", err)
	}
}
