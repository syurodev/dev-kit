package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"devkit/internal/agenthub"
	"devkit/internal/harness"
	"devkit/internal/harnessconfig"
	"devkit/internal/identity"
	"devkit/internal/interaction"
)

// F-C: a stored workspace profile that names a group or tool the registry no longer
// has locks the workspace out, and the window can repair exactly that. These run the
// real stack: the SQL HarnessStore, preferences and the writer boundary, the shared
// hub, the composed graph with Agent Hub and the MCP server. Only vendors and the
// person's answers are scripted.

func intPtr(n int) *int { return &n }

// storeProfile writes W straight into the store, as an older build or an earlier
// registry would have left it. It is the fixture, not the repair.
func (r *compRig) storeProfile(f func(*harness.Profile)) harness.Stamp {
	r.t.Helper()
	ctx := context.Background()
	cur, st, err := r.asm.Store.GetProfile(ctx, r.wsID)
	if err != nil {
		r.t.Fatal(err)
	}
	p := &harness.Profile{Schema: harness.SchemaVersion}
	if cur != nil {
		p = cur
	}
	f(p)
	a, err := r.asm.Store.Apply(ctx, harness.StoreWrite{WorkspaceID: r.wsID, Profile: &harness.ProfileWrite{Expect: st, Value: p}})
	if err != nil {
		r.t.Fatal(err)
	}
	return a.Profile
}

func (r *compRig) storedProfile() (*harness.Profile, harness.Stamp) {
	r.t.Helper()
	p, st, err := r.asm.Store.GetProfile(context.Background(), r.wsID)
	if err != nil {
		r.t.Fatal(err)
	}
	return p, st
}

func (r *compRig) body(p *harness.Profile) string {
	r.t.Helper()
	q := *p
	q.Revision = 0
	b, err := json.Marshal(q)
	if err != nil {
		r.t.Fatal(err)
	}
	return string(b)
}

func (r *compRig) stale() harnessconfig.StaleDiagnosis {
	r.t.Helper()
	d, err := r.asm.Controls().StaleProfile(r.ui(), r.wsID)
	if err != nil {
		r.t.Fatal(err)
	}
	return d
}

func (r *compRig) newSessionErr() error {
	_, err := r.agents.CreateSession(r.ui(), agenthub.CreateInput{AgentType: agenthub.AgentTypeClaude, Cwd: r.root, WorkspaceID: r.wsID})
	return err
}

func entryNames(d harnessconfig.StaleDiagnosis) string {
	var out []string
	for _, e := range d.Entries {
		out = append(out, string(e.Kind)+":"+e.Name)
	}
	return strings.Join(out, ",")
}

// The report's own scenario: the optional pdf group saved off, then a build without
// the module. The session cannot start; the repair is offered, can be declined, and
// when approved the workspace starts sessions again. If the module comes back, the
// removed "off" is gone and the group is available again, as the review said.
func TestAnOffSettingForAnUnwiredGroupBlocksLaunchUntilRepaired(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.workspaceOff("pdf")
	r.opt.noPDF = true
	r.restart()

	if err := r.newSessionErr(); !errors.Is(err, agenthub.ErrHarnessUnavailable) {
		t.Fatalf("setup: the stale profile must block the launch, got %v", err)
	}
	d := r.stale()
	if d.State != harnessconfig.StaleRepairable || entryNames(d) != "group:pdf" || d.Entries[0].On == nil || *d.Entries[0].On {
		t.Fatalf("diagnosis = %+v", d)
	}
	// Ordinary editing is still refused: the repair is the only way out.
	if _, err := r.asm.Controls().PrepareWorkspaceGroups(r.ui(), r.wsID, []harnessconfig.GroupEdit{{Group: "vault", Setting: harnessconfig.SettingOff}}); err == nil {
		t.Fatal("an ordinary edit must stay strict")
	}

	// Declined: nothing changes, the offer remains.
	_, before := r.storedProfile()
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "deny"}
	rev, err := r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Tier != "widening" || !rev.NeedsConfirmation || len(rev.Changes) != 1 || !strings.Contains(rev.Summary, `"off" setting for tool group "pdf"`) {
		t.Fatalf("review = %+v", rev)
	}
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err == nil {
		t.Fatal("a denied repair must be refused")
	}
	if _, now := r.storedProfile(); now != before || r.stale().State != harnessconfig.StaleRepairable {
		t.Fatal("a denial must change nothing")
	}

	// Approved.
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
	if rev, err = r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID); err != nil {
		t.Fatal(err)
	}
	out, err := r.asm.Controls().Apply(r.ui(), rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != harnessconfig.OutcomeApplied || out.Live || !out.ReloadRequired || out.Target != string(harnessconfig.TargetRepair) || out.Commit != harnessconfig.CommitCommitted {
		t.Fatalf("outcome = %+v", out)
	}
	if _, now := r.storedProfile(); now.Revision != before.Revision+1 {
		t.Fatalf("revision %d -> %d, want one step", before.Revision, now.Revision)
	}
	if d := r.stale(); d.State != harnessconfig.StaleNone {
		t.Fatalf("after: %+v", d)
	}
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	names := r.listed(cs)
	if hasGroup(names, "pdf") || !hasGroup(names, "vault") {
		t.Fatalf("a new session must serve what is registered: %v", names)
	}
	_ = sess

	// The module returns: the removed "off" is not remembered, the group is available.
	r.opt.noPDF = false
	r.restart()
	_, spec = r.create(agenthub.AgentTypeClaude)
	if !hasGroup(r.listed(r.connect(spec)), "pdf") {
		t.Fatal("the removed setting must not come back with the module: the group uses the default")
	}
}

// Every kind of stale key in one profile; every known field survives; the revision
// advances once.
func TestRepairRemovesEveryKindOfStaleKeyAndKeepsTheRest(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	st := r.storeProfile(func(p *harness.Profile) {
		p.ToolGroups = map[string]bool{"vault": false, "gone": true, "pdf-old": false}
		p.ResultBudget = &harness.BudgetProfile{
			Tools:  map[string]harness.BudgetRule{"git.status": {Bytes: intPtr(8192), Cut: harness.CutHeadTail}, "gone.tool": {Bytes: intPtr(4096)}},
			Groups: map[string]harness.BudgetRule{"tasks": {Bytes: intPtr(16384)}, "ghost": {Cut: harness.CutHeadTail}},
		}
		p.DisabledMCP = []string{"slack"}
	})
	d := r.stale()
	if got := entryNames(d); got != "group:gone,group:pdf-old,groupBudget:ghost,toolBudget:gone.tool" || d.Total != 4 {
		t.Fatalf("entries = %s (total %d)", got, d.Total)
	}
	before, _ := r.storedProfile()

	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
	rev, err := r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rev.Changes) != 4 {
		t.Fatalf("every removal must be listed: %+v", rev.Changes)
	}
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err != nil {
		t.Fatal(err)
	}
	after, now := r.storedProfile()
	if now.Revision != st.Revision+1 {
		t.Fatalf("revision %d -> %d", st.Revision, now.Revision)
	}
	want := harnessconfig.RepairedProfile(before, harnessconfig.InspectStale(before, r.inventoryRegistry()))
	if r.body(after) != r.body(want) {
		t.Fatalf("stored = %s\nwant   %s", r.body(after), r.body(want))
	}
	for _, kept := range []string{`"vault":false`, `"git.status":{"bytes":8192,"cut":"headTail"}`, `"tasks":{"bytes":16384}`, `"slack"`} {
		if !strings.Contains(r.body(after), kept) {
			t.Errorf("lost a known setting %s in %s", kept, r.body(after))
		}
	}
}

func (r *compRig) inventoryRegistry() harness.Registry {
	reg, err := r.inventory()
	if err != nil {
		r.t.Fatal(err)
	}
	return reg
}

func TestAHealthyOrAbsentProfileOffersNoRepair(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	if d := r.stale(); d.State != harnessconfig.StaleNone {
		t.Fatalf("no profile: %+v", d)
	}
	r.workspaceOff("vault")
	d := r.stale()
	if d.State != harnessconfig.StaleNone || len(d.Entries) != 0 {
		t.Fatalf("healthy: %+v", d)
	}
	if _, err := r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID); !errors.Is(err, harnessconfig.ErrNoChange) {
		t.Fatalf("prepare = %v", err)
	}
}

func TestACorruptProfileIsNotOfferedARepairAndIsNotTouched(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.workspaceOff("pdf")
	r.opt.noPDF = true
	r.restart()
	if _, err := r.db.Exec(`UPDATE workspace_harness_profiles SET body='{bad' WHERE workspace_id=?`, r.wsID); err != nil {
		t.Fatal(err)
	}
	d := r.stale()
	if d.State != harnessconfig.StaleUnrepairable || d.Message == "" || len(d.Entries) != 0 {
		t.Fatalf("diagnosis = %+v", d)
	}
	if _, err := r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID); !errors.Is(err, harnessconfig.ErrRepairUnavailable) {
		t.Fatalf("prepare = %v", err)
	}
	var body string
	if err := r.db.QueryRow(`SELECT body FROM workspace_harness_profiles WHERE workspace_id=?`, r.wsID).Scan(&body); err != nil || body != "{bad" {
		t.Fatalf("the stored record must be untouched: %q %v", body, err)
	}
	if n := len(kinds(r.prompter)); n != 0 {
		t.Fatalf("nobody should be asked about something that cannot be repaired (%d)", n)
	}
}

// The repair is the window's: an agent, the terminal and an unknown origin are
// refused in Go whatever they can reach, a stolen prepared id is nothing, and the
// agent tools gained no repair tool or argument.
func TestOnlyTheWindowCanDiagnoseOrPrepareARepairAndNoToolOffersOne(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	sess, spec := r.create(agenthub.AgentTypeClaude)
	r.storeProfile(func(p *harness.Profile) { p.ToolGroups = map[string]bool{"gone": false} })
	ctl := r.asm.Controls()
	for name, ctx := range map[string]context.Context{
		"agent":    r.agentCtx(sess.ID),
		"terminal": identity.WithCaller(r.ctx, identity.UserTerminal()),
		"unknown":  r.ctx,
	} {
		if _, err := ctl.StaleProfile(ctx, r.wsID); !errors.Is(err, harnessconfig.ErrDenied) {
			t.Errorf("%s diagnosis = %v", name, err)
		}
		if _, err := ctl.PrepareStaleRepair(ctx, r.wsID); !errors.Is(err, harnessconfig.ErrDenied) {
			t.Errorf("%s prepare = %v", name, err)
		}
	}
	if _, err := ctl.StaleProfile(r.ui(), "no-such-workspace"); err == nil {
		t.Fatal("an absent workspace must be refused")
	}
	if _, err := ctl.StaleProfile(r.ui(), ""); err == nil {
		t.Fatal("an empty workspace must be refused")
	}

	rev, err := ctl.PrepareStaleRepair(r.ui(), r.wsID)
	if err != nil {
		t.Fatal(err)
	}
	_, before := r.storedProfile()
	if _, err := ctl.Apply(r.agentCtx(sess.ID), rev.ID); !errors.Is(err, harnessconfig.ErrDenied) {
		t.Fatalf("an agent applying through the facade = %v", err)
	}
	if _, err := r.asm.Service.Apply(r.agentCtx(sess.ID), rev.ID); !errors.Is(err, harnessconfig.ErrUnknownProposal) {
		t.Fatalf("an agent applying through the service = %v", err)
	}
	if err := ctl.Cancel(r.agentCtx(sess.ID), rev.ID); !errors.Is(err, harnessconfig.ErrDenied) {
		t.Fatalf("an agent cancelling = %v", err)
	}
	if _, now := r.storedProfile(); now != before {
		t.Fatal("nothing may be written")
	}

	cs := r.connect(spec)
	for tool, err := range cs.Tools(r.ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(tool)
		if s := strings.ToLower(string(raw)); strings.Contains(strings.ToLower(tool.Name), "repair") || strings.Contains(strings.ToLower(tool.Name), "stale") ||
			(tool.Name == harness.ProposeTool || tool.Name == "harness.profile.get") && (strings.Contains(s, "repair") || strings.Contains(s, "stale")) {
			t.Fatalf("an agent-visible tool mentions the repair: %s", tool.Name)
		}
	}

	// Ownership by the window is the same caller: it can cancel, and a spent id is gone.
	if err := ctl.Cancel(r.ui(), rev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ctl.Apply(r.ui(), rev.ID); err == nil {
		t.Fatal("a cancelled id must not apply")
	}
}

// While the person decides, anything the approval was bound to may move; the old
// removals must never erase what is newer.
func TestAChangeWhileTheRepairQuestionIsOpenMakesItStale(t *testing.T) {
	cases := map[string]func(r *compRig){
		"workspace edit": func(r *compRig) { r.storeProfile(func(p *harness.Profile) { p.ToolGroups["vault"] = false }) },
		"vendor edit": func(r *compRig) {
			c, _ := r.prefs.AgentCustomization("claude")
			c.DisabledMCP = []string{"slack"}
			if _, err := r.prefs.SetAgentCustomization("claude", c); err != nil {
				r.t.Error(err)
			}
		},
		"policy off then on": func(r *compRig) {
			p, _ := r.prefs.AgentHarnessPolicy()
			on, err := r.prefs.SetAgentHarnessAutoNarrowing(p.Revision, true)
			if err != nil {
				r.t.Error(err)
				return
			}
			if _, err := r.prefs.SetAgentHarnessAutoNarrowing(on.Revision, false); err != nil {
				r.t.Error(err)
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := newCompRig(t, compOptions{controls: true})
			r.storeProfile(func(p *harness.Profile) { p.ToolGroups = map[string]bool{"gone": false} })
			rev, err := r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID)
			if err != nil {
				t.Fatal(err)
			}
			r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
			r.prompter.mu.Lock()
			r.prompter.during = func() { change(r) }
			r.prompter.mu.Unlock()
			if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err == nil {
				t.Fatal("the repair must be stale")
			}
			got, _ := r.storedProfile()
			if _, ok := got.ToolGroups["gone"]; !ok {
				t.Fatalf("a stale repair removed something: %v", got.ToolGroups)
			}
			if name == "workspace edit" && got.ToolGroups["vault"] {
				t.Fatalf("the newer setting was overwritten: %v", got.ToolGroups)
			}
		})
	}
}

// Whatever the auto-narrowing setting and whatever was said before, a repair asks
// every time; and with nobody to ask it writes nothing.
func TestARepairAlwaysAsksAndHeadlessWritesNothing(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	p, _ := r.prefs.AgentHarnessPolicy()
	if _, err := r.prefs.SetAgentHarnessAutoNarrowing(p.Revision, true); err != nil {
		t.Fatal(err)
	}
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow_always"}
	for i := 1; i <= 2; i++ {
		r.storeProfile(func(p *harness.Profile) { p.ToolGroups = map[string]bool{"gone": false} })
		rev, err := r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, k := range kinds(r.prompter) {
			if k == interaction.KindHarness {
				n++
			}
		}
		if n != i {
			t.Fatalf("after repair %d the person was asked %d times, want %d", i, n, i)
		}
	}

	r.storeProfile(func(p *harness.Profile) { p.ToolGroups = map[string]bool{"gone": false} })
	rev, err := r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID)
	if err != nil {
		t.Fatal(err)
	}
	_, before := r.storedProfile()
	r.hub.Headless = true
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); !errors.Is(err, harnessconfig.ErrConfirmUnavailable) {
		t.Fatalf("headless = %v", err)
	}
	if _, now := r.storedProfile(); now != before {
		t.Fatal("headless must write nothing")
	}
}

// The repair touches W and nothing else: not S, not a running session's admission,
// not the applied reference or the usage history.
func TestARepairLeavesSessionsOverridesReferencesAndUsageAlone(t *testing.T) {
	r := newCompRig(t, compOptions{controls: true})
	r.workspaceOff("vault")
	sess, spec := r.create(agenthub.AgentTypeClaude)
	cs := r.connect(spec)
	if _, err := r.apply(sess.ID, []string{"vault"}, nil); err != nil {
		t.Fatal(err)
	}
	r.storeProfile(func(p *harness.Profile) { p.ToolGroups["gone"] = true })

	listedBefore := strings.Join(r.listed(cs), ",")
	servingBefore := r.agents.ServingTarget(sess.ID)
	_, overrideBefore := r.override(sess.ID)
	refBefore, enforcedBefore, _ := r.agents.HarnessApplied(sess.ID)
	usageBefore := strings.Join(r.usageRows(), ";")

	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
	rev, err := r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err != nil {
		t.Fatal(err)
	}

	servingAfter := r.agents.ServingTarget(sess.ID)
	if servingAfter.Incarnation != servingBefore.Incarnation || servingAfter.Epoch != servingBefore.Epoch || servingAfter.Serving != servingBefore.Serving ||
		servingAfter.Quarantined || !servingAfter.Acknowledged {
		t.Fatalf("the running endpoint must be untouched: %+v -> %+v", servingBefore, servingAfter)
	}
	if got := strings.Join(r.listed(cs), ","); got != listedBefore {
		t.Fatal("the running session's tool list changed")
	}
	if _, now := r.override(sess.ID); now != overrideBefore {
		t.Fatalf("S moved: %+v -> %+v", overrideBefore, now)
	}
	refAfter, enforcedAfter, _ := r.agents.HarnessApplied(sess.ID)
	if refAfter.Fingerprint != refBefore.Fingerprint || refAfter.ExecutableDigest != refBefore.ExecutableDigest || enforcedAfter != enforcedBefore {
		t.Fatal("the applied reference changed")
	}
	if got := strings.Join(r.usageRows(), ";"); got != usageBefore {
		t.Fatalf("usage history changed:\n%s\n%s", usageBefore, got)
	}
}

// An ambiguous commit is reported as uncertain, never retried, and never offered a
// session reconcile; a fresh read shows what is really stored.
func TestAnAmbiguousRepairCommitIsReportedAsUncertain(t *testing.T) {
	var f *fault
	r := newCompRig(t, compOptions{controls: true, wrapStore: func(p harnessPersistence) harnessPersistence {
		f = &fault{harnessPersistence: p}
		return f
	}})
	r.storeProfile(func(p *harness.Profile) { p.ToolGroups = map[string]bool{"gone": false} })
	rev, err := r.asm.Controls().PrepareStaleRepair(r.ui(), r.wsID)
	if err != nil {
		t.Fatal(err)
	}
	f.mode.Store("unknown")
	r.prompter.answers = map[interaction.Kind]string{interaction.KindHarness: "allow"}
	out, err := r.asm.Controls().Apply(r.ui(), rev.ID)
	if err != nil {
		t.Fatalf("an uncertain commit is an outcome, not an error: %v", err)
	}
	if out.Status != harnessconfig.OutcomeUnknown || out.Live || strings.Contains(strings.ToLower(out.Message+out.Recovery), "reconcile") {
		t.Fatalf("outcome = %+v", out)
	}
	if _, err := r.asm.Controls().Apply(r.ui(), rev.ID); err == nil {
		t.Fatal("the same id must not be applied again")
	}
	f.failReads.Store(false)
	f.mode.Store("")
	if d := r.stale(); d.State != harnessconfig.StaleNone {
		t.Fatalf("a fresh read shows the write: %+v", d)
	}
}
