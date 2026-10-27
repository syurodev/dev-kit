package harnessconfig

import (
	"context"
	"strings"
	"testing"

	"devkit/internal/harness"
)

func kib(n int) *int  { v := n * 1024; return &v }
func intp(n int) *int { return &n }

func budgetEdit(edits ...BudgetEdit) Request {
	return Request{WorkspaceID: "w1", Target: TargetWorkspace, Patch: Patch{BudgetEdits: edits}}
}

func (e *env) effectiveBudget(ws, tool string) harness.Budget {
	e.t.Helper()
	w, st := e.profile(ws)
	if w != nil {
		w.Revision = st.Revision
	}
	eff, err := harness.Resolve(harness.Inputs{Registry: registry(), Vendor: harness.Vendor{Revision: 1}, Workspace: w, Canonical: e.canonical.resolve})
	if err != nil {
		e.t.Fatalf("Resolve: %v", err)
	}
	b, ok := eff.Budget(tool)
	if !ok {
		e.t.Fatalf("%s not registered", tool)
	}
	return b
}

func seedBudgets(e *env, ws string, groups, tools map[string]harness.BudgetRule) harness.Stamp {
	return e.seedProfile(ws, func(p *harness.Profile) {
		p.ToolGroups = map[string]bool{"tasks": true}
		p.DisabledSkills = []string{"/skills/keep/SKILL.md"}
		p.ResultBudget = &harness.BudgetProfile{Groups: groups, Tools: tools}
	})
}

func prepareApply(t *testing.T, a *applyEnv, ctx context.Context, req Request) (Prepared, Result, error) {
	t.Helper()
	p, err := a.svc.Prepare(ctx, req)
	if err != nil {
		return Prepared{}, Result{}, err
	}
	res, err := a.svc.Apply(ctx, p.ID)
	return p, res, err
}

// Size and cut mode reset separately: a reset of one keeps the other, in the same
// rule; a rule left with nothing is removed; nothing else in W moves.
func TestAPartialResetKeepsTheSiblingFieldAndEveryOtherFieldOfW(t *testing.T) {
	a := newApplyEnv(t)
	seedBudgets(a.env, "w1",
		map[string]harness.BudgetRule{"git": {Bytes: kib(8), Cut: harness.CutHeadTail}},
		map[string]harness.BudgetRule{"git.status": {Bytes: kib(16), Cut: harness.CutHead}})

	// Reset the tool's SIZE: its explicit head cut stays, the size falls back to the group's 8 KiB.
	if _, _, err := prepareApply(t, a, ui(), budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", ResetBytes: true})); err != nil {
		t.Fatal(err)
	}
	w, _ := a.profile("w1")
	rule := w.ResultBudget.Tools["git.status"]
	if rule.Bytes != nil || rule.Cut != harness.CutHead {
		t.Fatalf("a size reset must keep the explicit cut: %+v", rule)
	}
	if got := a.effectiveBudget("w1", "git.status"); got.Bytes != 8*1024 || got.Cut != harness.CutHead {
		t.Fatalf("effective after the size reset = %+v, want 8 KiB/head (group size, own cut)", got)
	}
	if !w.ToolGroups["tasks"] || len(w.DisabledSkills) != 1 || w.ResultBudget.Groups["git"].Cut != harness.CutHeadTail {
		t.Fatalf("an unedited field changed: %+v", w)
	}

	// Reset the CUT too: the rule is empty and goes; the group's headTail applies.
	if _, _, err := prepareApply(t, a, ui(), budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", ResetCut: true})); err != nil {
		t.Fatal(err)
	}
	w, _ = a.profile("w1")
	if _, ok := w.ResultBudget.Tools["git.status"]; ok || len(w.ResultBudget.Tools) != 0 {
		t.Fatalf("an empty rule must be removed: %+v", w.ResultBudget.Tools)
	}
	if got := a.effectiveBudget("w1", "git.status"); got.Bytes != 8*1024 || got.Cut != harness.CutHeadTail {
		t.Fatalf("effective after both resets = %+v", got)
	}

	// Reset the last group rule: no budget profile remains, W's other fields stay.
	if _, _, err := prepareApply(t, a, ui(), budgetEdit(BudgetEdit{Kind: BudgetGroup, Name: "git", ResetBytes: true, ResetCut: true})); err != nil {
		t.Fatal(err)
	}
	w, st := a.profile("w1")
	if w.ResultBudget != nil || !w.ToolGroups["tasks"] || len(w.DisabledSkills) != 1 || st.Revision != 4 {
		t.Fatalf("W after clearing every budget: %+v %+v", w, st)
	}
	if got := a.effectiveBudget("w1", "git.status"); got.Bytes != harness.DefaultResultBudget || got.Cut != harness.CutHead {
		t.Fatalf("back to the default: %+v", got)
	}
}

func TestSizeAndCutInheritIndependentlyAcrossToolAndGroup(t *testing.T) {
	a := newApplyEnv(t)
	seedBudgets(a.env, "w1", map[string]harness.BudgetRule{"git": {Bytes: kib(8), Cut: harness.CutHeadTail}}, nil)
	// Tool bytes over a group's mode; then tool mode over a group's bytes.
	for _, edit := range []BudgetEdit{
		{Kind: BudgetTool, Name: "git.status", Bytes: kib(16)},
		{Kind: BudgetTool, Name: "git.commit", Cut: "head"},
	} {
		if _, _, err := prepareApply(t, a, ui(), budgetEdit(edit)); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.effectiveBudget("w1", "git.status"); got.Bytes != 16*1024 || got.Cut != harness.CutHeadTail {
		t.Errorf("tool bytes + group mode: %+v", got)
	}
	if got := a.effectiveBudget("w1", "git.commit"); got.Bytes != 8*1024 || got.Cut != harness.CutHead {
		t.Errorf("tool mode + group bytes: %+v", got)
	}
	if got := a.effectiveBudget("w1", "tasks.list"); got.Bytes != harness.DefaultResultBudget || got.Cut != harness.CutHead {
		t.Errorf("a tool outside the group: %+v", got)
	}
}

// The tier is judged on the effective before and after, never on the edit.
func TestBudgetEditsAreClassifiedOnTheEffectiveLimits(t *testing.T) {
	cases := []struct {
		name  string
		tools map[string]harness.BudgetRule
		edit  BudgetEdit
		tier  string
	}{
		{"delete a 16 KiB override: falls back to 24 KiB", map[string]harness.BudgetRule{"git.status": {Bytes: kib(16)}}, BudgetEdit{Kind: BudgetTool, Name: "git.status", ResetBytes: true}, "widening"},
		{"64 KiB to 32 KiB", map[string]harness.BudgetRule{"git.status": {Bytes: kib(64)}}, BudgetEdit{Kind: BudgetTool, Name: "git.status", Bytes: kib(32)}, "narrowing"},
		{"2 KiB to 16 KiB", map[string]harness.BudgetRule{"git.status": {Bytes: kib(2)}}, BudgetEdit{Kind: BudgetTool, Name: "git.status", Bytes: kib(16)}, "widening"},
		{"only the cut changes", nil, BudgetEdit{Kind: BudgetTool, Name: "git.status", Cut: "headTail"}, "narrowing"},
		{"a tool override equal to the default changes nothing", map[string]harness.BudgetRule{"git.status": {Bytes: kib(24)}}, BudgetEdit{Kind: BudgetTool, Name: "git.status", ResetBytes: true}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			seedBudgets(e, "w1", nil, c.tools)
			p, err := e.svc.Prepare(ui(), budgetEdit(c.edit))
			if c.tier == "" {
				if code(t, err) != harness.CodeNoChange {
					t.Fatalf("expected no change, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.View.Tier != c.tier {
				t.Fatalf("tier = %s, want %s: %s", p.View.Tier, c.tier, p.View.Summary)
			}
		})
	}
}

func TestAMixedBudgetEditTakesTheStrictestTierAndShowsExactNumbers(t *testing.T) {
	e := newEnv(t)
	seedBudgets(e, "w1", nil, map[string]harness.BudgetRule{"git.status": {Bytes: kib(16)}, "git.commit": {Bytes: kib(64)}})
	p, err := e.svc.Prepare(ui(), budgetEdit(
		BudgetEdit{Kind: BudgetTool, Name: "git.status", ResetBytes: true}, // 16 KiB → 24 KiB: widening
		BudgetEdit{Kind: BudgetTool, Name: "git.commit", Bytes: kib(32)},   // 64 → 32 KiB: narrowing
	))
	if err != nil {
		t.Fatal(err)
	}
	if p.View.Tier != "widening" || !p.View.NeedsConfirmation {
		t.Fatalf("a widening hidden among narrowing must win: %+v", p.View)
	}
	for _, want := range []string{
		"results from git.status: limit 16384 → 24576 bytes",
		"results from git.commit: limit 65536 → 32768 bytes",
		"tool git.status: size reset to the inherited value",
		"tool git.commit: size set to 32768 bytes",
	} {
		if !strings.Contains(p.View.Summary, want) {
			t.Errorf("the review lacks %q:\n%s", want, p.View.Summary)
		}
	}
}

func TestAGroupEditReachesOnlyItsCuttableMembers(t *testing.T) {
	e := newEnv(t)
	p, err := e.svc.Prepare(ui(), budgetEdit(BudgetEdit{Kind: BudgetGroup, Name: "git", Bytes: kib(8), Cut: "headTail"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.View.Summary, "results from git.status: limit 24576 → 8192 bytes, cut head → headTail") ||
		!strings.Contains(p.View.Summary, "results from git.commit:") || strings.Contains(p.View.Summary, "tasks.list") {
		t.Fatalf("a group edit lists each member it reaches, and only those:\n%s", p.View.Summary)
	}
}

func TestBudgetEditsAreValidatedAgainstTheRegistryAndTheirOwnShape(t *testing.T) {
	e := newEnv(t)
	bad := map[string]struct {
		req  Request
		code string
	}{
		"unknown tool":              {budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.nope", Bytes: kib(8)}), harness.CodeBudgetInvalid},
		"unknown group":             {budgetEdit(BudgetEdit{Kind: BudgetGroup, Name: "nope", Bytes: kib(8)}), harness.CodeGroupUnknown},
		"exempt tool":               {budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "codegraph.readFile", Bytes: kib(8)}), harness.CodeBudgetInvalid},
		"below the floor":           {budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", Bytes: intp(2047)}), harness.CodeBudgetInvalid},
		"above the ceiling":         {budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", Bytes: intp(65537)}), harness.CodeBudgetInvalid},
		"unknown mode":              {budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", Cut: "middle"}), harness.CodeBudgetInvalid},
		"set and reset one field":   {budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", Bytes: kib(8), ResetBytes: true}), harness.CodeProfileInvalid},
		"an edit that does nothing": {budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status"}), harness.CodeProfileInvalid},
		"duplicate target":          {budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", Bytes: kib(8)}, BudgetEdit{Kind: BudgetTool, Name: "git.status", Cut: "head"}), harness.CodeProfileInvalid},
		"unknown kind":              {budgetEdit(BudgetEdit{Kind: "server", Name: "git", Bytes: kib(8)}), harness.CodeProfileInvalid},
		"a session target":          {Request{Target: TargetSession, Patch: Patch{BudgetEdits: []BudgetEdit{{Kind: BudgetTool, Name: "git.status", Bytes: kib(8)}}}}, harness.CodeProfileInvalid},
		"mixed with a group enable": {Request{Target: TargetWorkspace, Patch: Patch{EnableGroups: []string{"git"}, BudgetEdits: []BudgetEdit{{Kind: BudgetTool, Name: "git.status", Bytes: kib(8)}}}}, harness.CodeProfileInvalid},
	}
	for name, c := range bad {
		ctx := ui()
		if strings.Contains(name, "session") || strings.Contains(name, "mixed") {
			ctx = agent("s1")
		}
		req := c.req
		if ctx == nil || req.Target == TargetSession {
			req.SessionID = "s1"
		}
		if req.WorkspaceID == "" && ctx == ui() {
			req.WorkspaceID = "w1"
		}
		_, err := e.svc.Prepare(withWorkspace(ctx, name), withWS(req, name))
		if err == nil || code(t, err) != c.code {
			t.Errorf("%s: want %s, got %v", name, c.code, err)
		}
	}
	if e.svc.Pending() != 0 {
		t.Fatal("a refused edit left a preparation")
	}
}

// ui() carries no workspace; the window names one in the request.
func withWorkspace(ctx context.Context, _ string) context.Context { return ctx }
func withWS(r Request, _ string) Request {
	if r.WorkspaceID == "" {
		r.WorkspaceID = "w1"
	}
	return r
}

func TestAnAllExemptGroupHasNoBudgetToEdit(t *testing.T) {
	e := newEnv(t)
	// The test registry's codegraph group has a cuttable member; build one that has none.
	e.svc = New(Config{Store: e.store, Directory: e.dir, Vendors: e.vendors, Policy: e.policy, Validator: e.validator,
		Registry: func() harness.Registry {
			return harness.Registry{Tools: []harness.Tool{
				{Name: "git.status", DefinitionBytes: 10}, {Name: "pdf.info", DefinitionBytes: 10, BudgetExempt: true}, {Name: "pdf.search", DefinitionBytes: 10, BudgetExempt: true},
			}}
		}, Canonical: e.canonical.resolve})
	req := budgetEdit(BudgetEdit{Kind: BudgetGroup, Name: "pdf", Bytes: kib(8)})
	req.WorkspaceID = "w1"
	if _, err := e.svc.Prepare(ui(), req); err == nil || code(t, err) != harness.CodeBudgetInvalid {
		t.Fatalf("an all-exempt group edit must be refused, got %v", err)
	}
}

// An agent's budget edit is a saved-workspace edit only: W moves, S never does,
// the requester's pinned launch limits do not, and its target is its own workspace.
func TestAnAgentBudgetEditWritesTheWorkspaceProfileOnly(t *testing.T) {
	a := newApplyEnv(t)
	a.confirm.answer = nil
	seedBudgets(a.env, "w1", nil, map[string]harness.BudgetRule{"git.status": {Bytes: kib(16)}})
	_, sBefore := a.override("w1", "s1")
	p, res, err := prepareApply(t, a, agent("s1"), budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", Bytes: kib(8)}))
	if err != nil {
		t.Fatal(err)
	}
	if !p.View.NeedsConfirmation || a.confirm.calls != 1 {
		t.Fatalf("without the opt-in even a narrowing budget edit asks: needs=%v asked=%d", p.View.NeedsConfirmation, a.confirm.calls)
	}
	if !res.ReloadRequired || res.Live || res.Override != (harness.Stamp{}) {
		t.Fatalf("a W edit needs a reload and touches no S: %+v", res)
	}
	if got := a.effectiveBudget("w1", "git.status"); got.Bytes != 8*1024 {
		t.Fatalf("the saved profile should hold 8 KiB: %+v", got)
	}
	if _, sAfter := a.override("w1", "s1"); sAfter != sBefore {
		t.Fatalf("S moved: %+v → %+v", sBefore, sAfter)
	}
	// The agent cannot name another workspace.
	other := budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", Bytes: kib(8)})
	other.WorkspaceID = "w2"
	if _, err := a.svc.Prepare(agent("s1"), other); code(t, err) != harness.CodeProfileDenied {
		t.Fatalf("an agent cannot target another workspace: %v", err)
	}
}

// The person's opt-in lets an agent's pure narrowing budget edit skip the
// question and nothing else.
func TestAutoNarrowingSkipsTheQuestionOnlyForAnAgentsNarrowingBudgetEdit(t *testing.T) {
	narrowing := budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", Bytes: kib(8)})
	widening := budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", ResetBytes: true})
	mixed := budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", ResetBytes: true}, BudgetEdit{Kind: BudgetTool, Name: "git.commit", Bytes: kib(8)})
	cutOnly := budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.commit", Cut: "headTail"})

	run := func(t *testing.T, ctx context.Context, req Request, autoOn bool, seed bool) (asked int, err error) {
		a := newApplyEnv(t)
		a.policy.p = Policy{Revision: "p2", AutoNarrowing: autoOn}
		if seed {
			seedBudgets(a.env, "w1", nil, map[string]harness.BudgetRule{"git.status": {Bytes: kib(16)}, "git.commit": {Bytes: kib(24)}})
		}
		if ctx == nil {
			ctx = agent("s1")
		}
		if req.WorkspaceID == "" && ctx == ui() {
			req.WorkspaceID = "w1"
		}
		_, _, err = prepareApply(t, a, ctx, req)
		return a.confirm.calls, err
	}
	for _, c := range []struct {
		name  string
		agent bool
		req   Request
		auto  bool
		asked int
	}{
		{"agent narrowing, opted in", true, narrowing, true, 0},
		{"agent narrowing, not opted in", true, narrowing, false, 1},
		{"agent cut-only change, opted in", true, cutOnly, true, 0},
		{"agent widening, opted in", true, widening, true, 1},
		{"agent mixed with a widening, opted in", true, mixed, true, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			var ctx context.Context
			if !c.agent {
				ctx = ui()
			}
			asked, err := run(t, ctx, c.req, c.auto, true)
			if err != nil {
				t.Fatal(err)
			}
			if asked != c.asked {
				t.Fatalf("asked %d times, want %d", asked, c.asked)
			}
		})
	}
	t.Run("the window is always asked", func(t *testing.T) {
		req := narrowing
		req.WorkspaceID = "w1"
		asked, err := run(t, ui(), req, true, true)
		if err != nil || asked != 1 {
			t.Fatalf("a window edit is confirmed even when the opt-in is on: asked %d (%v)", asked, err)
		}
	})
	t.Run("a group revoke by an agent is not a budget edit", func(t *testing.T) {
		a := newApplyEnv(t)
		a.policy.p = Policy{Revision: "p2", AutoNarrowing: true}
		gitOff(a.env, "w1") // git is off in W and on for the session: a revoke narrows
		a.seedOverride("w1", "s1", "git")
		if _, _, err := prepareApply(t, a, agent("s1"), Request{Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}}}); err != nil {
			t.Fatal(err)
		}
		if a.confirm.calls != 1 {
			t.Fatalf("the opt-in covers budget edits only; asked %d times", a.confirm.calls)
		}
	})
	t.Run("nobody to ask still refuses, opt-in or not", func(t *testing.T) {
		a := newApplyEnv(t)
		a.policy.p = Policy{Revision: "p2", AutoNarrowing: true}
		a.confirm.available = false
		seedBudgets(a.env, "w1", nil, map[string]harness.BudgetRule{"git.status": {Bytes: kib(16)}})
		_, _, err := prepareApply(t, a, agent("s1"), narrowing)
		if code(t, err) != harness.CodeProfileDenied {
			t.Fatalf("headless must fail closed: %v", err)
		}
		if got := a.effectiveBudget("w1", "git.status"); got.Bytes != 16*1024 {
			t.Fatalf("nothing may be written: %+v", got)
		}
	})
}

// Policy.Revision binds an approval: any change to the policy between Prepare and
// the commit makes the proposal stale, a toggle off and on again included.
func TestAPolicyChangeBetweenPrepareAndCommitStalesTheProposal(t *testing.T) {
	for name, revs := range map[string][]string{
		"toggled on":         {"p3"},
		"toggled off and on": {"p3", "p2"}, // ends where it started, as a different revision
	} {
		t.Run(name, func(t *testing.T) {
			a := newApplyEnv(t)
			a.policy.p = Policy{Revision: "p1", AutoNarrowing: false}
			seedBudgets(a.env, "w1", nil, map[string]harness.BudgetRule{"git.status": {Bytes: kib(16)}})
			p, err := a.svc.Prepare(agent("s1"), budgetEdit(BudgetEdit{Kind: BudgetTool, Name: "git.status", Bytes: kib(8)}))
			if err != nil {
				t.Fatal(err)
			}
			a.confirm.during = func() {
				for _, r := range revs {
					a.policy.mu.Lock()
					a.policy.p = Policy{Revision: r, AutoNarrowing: r == "p3" || r == "p2"}
					a.policy.mu.Unlock()
				}
			}
			if _, err := a.svc.Apply(agent("s1"), p.ID); err == nil || code(t, err) != harness.CodeProposalStale {
				t.Fatalf("a policy change must stale the proposal, got %v", err)
			}
			if got := a.effectiveBudget("w1", "git.status"); got.Bytes != 16*1024 {
				t.Fatalf("nothing may be written: %+v", got)
			}
		})
	}
}
