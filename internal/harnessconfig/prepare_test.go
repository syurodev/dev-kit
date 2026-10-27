package harnessconfig

import (
	"context"
	"strings"
	"testing"

	"devkit/internal/harness"
	"devkit/internal/identity"
)

func gitOff(e *env, ws string) harness.Stamp {
	return e.seedProfile(ws, func(p *harness.Profile) { p.ToolGroups = map[string]bool{"git": false} })
}

func TestPrepareASessionEnableWritesNothingAndIsNotAnApproval(t *testing.T) {
	e := newEnv(t)
	wst := gitOff(e, "w1")
	got, err := e.svc.Prepare(agent("s1"), enableGit())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ID) != 32 || e.svc.Pending() != 1 {
		t.Fatalf("id %q pending %d", got.ID, e.svc.Pending())
	}
	if got.View.Tier != "widening" || !got.View.NeedsConfirmation || got.View.Target != TargetSession {
		t.Fatalf("view = %+v", got.View)
	}
	if !strings.Contains(got.View.Summary, "turn on tool group git") {
		t.Errorf("summary is Go-authored from the diff: %q", got.View.Summary)
	}
	if o, st := e.override("w1", "s1"); o != nil || st.Revision != 0 {
		t.Errorf("Prepare wrote S: %+v %+v", o, st)
	}
	if _, st := e.profile("w1"); st != wst {
		t.Errorf("Prepare wrote W: %+v", st)
	}
}

func TestAnAgentCannotChooseItsTarget(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	gitOff(e, "w2")
	for name, req := range map[string]Request{
		"another workspace": {WorkspaceID: "w2", Target: TargetSession, Patch: enableGit().Patch},
		"another session":   {SessionID: "s2", Target: TargetSession, Patch: enableGit().Patch},
		"a sibling session": {SessionID: "s1b", Target: TargetSession, Patch: enableGit().Patch},
	} {
		if _, err := e.svc.Prepare(agent("s1"), req); err != ErrDenied {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Naming its own workspace and session is allowed and changes nothing.
	own := enableGit()
	own.WorkspaceID, own.SessionID = "w1", "s1"
	if _, err := e.svc.Prepare(agent("s1"), own); err != nil {
		t.Errorf("own target: %v", err)
	}
	if e.svc.Pending() != 1 {
		t.Errorf("refusals left %d pending", e.svc.Pending())
	}
}

func TestOnlyAgentsAndTheUICanPropose(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	req := enableGit()
	req.WorkspaceID, req.SessionID = "w1", "s1"
	for name, ctx := range map[string]context.Context{
		"no caller":     context.Background(),
		"user terminal": identity.WithCaller(context.Background(), identity.UserTerminal()),
		"remote human":  identity.WithCaller(context.Background(), identity.Caller{Kind: identity.KindRemoteHuman}),
		"agent no id":   identity.WithCaller(context.Background(), identity.Caller{Kind: identity.KindAgent}),
	} {
		if _, err := e.svc.Prepare(ctx, req); err != ErrDenied {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTheUISuppliesAWorkspaceAndTheSessionMustBelongToIt(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	ok := enableGit()
	ok.WorkspaceID, ok.SessionID = "w1", "s1"
	if _, err := e.svc.Prepare(ui(), ok); err != nil {
		t.Fatal(err)
	}
	foreign := ok
	foreign.SessionID = "s2"
	if _, err := e.svc.Prepare(ui(), foreign); err != ErrDenied {
		t.Errorf("a session of another workspace: %v", err)
	}
	noSession := enableGit()
	noSession.WorkspaceID = "w1"
	if _, err := e.svc.Prepare(ui(), noSession); err != ErrInvalid {
		t.Errorf("a session-only proposal needs a session: %v", err)
	}
	noWorkspace := enableGit()
	noWorkspace.SessionID = "s1"
	if _, err := e.svc.Prepare(ui(), noWorkspace); err != ErrInvalid {
		t.Errorf("the UI must name a workspace: %v", err)
	}
	gone := ok
	gone.WorkspaceID = "nope"
	if _, err := e.svc.Prepare(ui(), gone); err != ErrScopeGone {
		t.Errorf("unknown workspace: %v", err)
	}
}

func TestDeletedOrMovedScopeIsRefused(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	delete(e.dir.sessions, "s1")
	if _, err := e.svc.Prepare(agent("s1"), enableGit()); err != ErrScopeGone {
		t.Errorf("deleted session: %v", err)
	}
	// A session that moved to another workspace targets that workspace's profile;
	// it cannot reach the one it left, and S written under the old one stays put.
	e.seedOverride("w1", "s1b", "git")
	e.dir.sessions["s1b"] = SessionScope{WorkspaceID: "w2", Vendor: "claude"}
	if _, err := e.svc.Prepare(agent("s1b"), Request{Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}}}); err == nil {
		t.Fatal("a moved session read S written under its old workspace")
	}
}

func TestNothingToChangeCreatesNoPendingProposal(t *testing.T) {
	e := newEnv(t)
	// git is already on: enabling it is no effective change.
	if _, err := e.svc.Prepare(agent("s1"), enableGit()); err != ErrNoChange {
		t.Fatalf("got %v", err)
	}
	if e.svc.Pending() != 0 {
		t.Fatal("a refused proposal must not stay pending")
	}
}

func TestVendorFactsGateOnlyThePatchesThatNeedThem(t *testing.T) {
	e := newEnv(t)
	e.vendors.set("claude", func(s *VendorState) { s.FactsComplete = false })
	gitOff(e, "w1")
	if _, err := e.svc.Prepare(agent("s1"), enableGit()); err != nil {
		t.Fatalf("a group change needs no vendor facts: %v", err)
	}
	for name, patch := range map[string]*ProfilePatch{
		"deny list":   {DisabledMCP: &[]string{"x"}},
		"servers":     {Servers: &[]harness.Server{stdio("db", "cmd")}},
		"skills":      {SkillFolders: &[]string{"/skills/a"}},
		"deny skills": {DisabledSkills: &[]string{}},
	} {
		_, err := e.svc.Prepare(agent("s1"), Request{Target: TargetWorkspace, Patch: Patch{Workspace: patch}})
		if err != ErrFactsUnavailable {
			t.Errorf("%s with unknown facts: %v", name, err)
		}
	}
	e.vendors.err = context.DeadlineExceeded
	if _, err := e.svc.Prepare(agent("s1"), enableGit()); err != ErrFactsUnavailable {
		t.Errorf("a vendor read failure is not an empty environment: %v", err)
	}
}

func TestMissingFactsAreNeverTreatedAsAnEmptyEnvironment(t *testing.T) {
	e := newEnv(t)
	e.vendors.set("claude", func(s *VendorState) {
		s.Environment = harness.Environment{Servers: []harness.Server{stdio("vendor-db", "theirs")}}
		s.FactsComplete = false
	})
	e.seedProfile("w1", func(p *harness.Profile) { p.DisabledMCP = []string{"vendor-db"} })
	// Removing the deny would expose their server. With the facts incomplete the
	// service must refuse instead of classifying against an empty environment.
	_, err := e.svc.Prepare(agent("s1"), Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{DisabledMCP: &[]string{}}}})
	if err != ErrFactsUnavailable {
		t.Fatalf("got %v", err)
	}
}

func TestExecutableChangesAreRefusedWithAReason(t *testing.T) {
	e := newEnv(t)
	e.vendors.set("claude", func(s *VendorState) {
		s.Environment = harness.Environment{Servers: []harness.Server{stdio("vendor-db", "theirs")}}
	})
	e.seedProfile("w1", func(p *harness.Profile) { p.DisabledMCP = []string{"vendor-db"} })
	// A patch that only deletes a deny line exposes a vendor executable.
	expose := Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{DisabledMCP: &[]string{}}}}
	if _, err := e.svc.Prepare(agent("s1"), expose); err != ErrExecutableDenied {
		t.Fatalf("not opted in: %v", err)
	}
	e.policy.p.AllowExecutable = true
	if _, err := e.svc.Prepare(agent("s1"), expose); err != ErrExecutableNotReady {
		t.Fatalf("opted in but not showable: %v", err)
	}
	if e.svc.Pending() != 0 {
		t.Fatal("refused executable proposals must not stay pending")
	}
	// Mixed narrowing + executable takes the strictest tier.
	mixed := Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{
		Groups:      map[string]*bool{"git": boolp(false)},
		DisabledMCP: &[]string{},
	}}}
	e.policy.p.AllowExecutable = false
	if _, err := e.svc.Prepare(agent("s1"), mixed); err != ErrExecutableDenied {
		t.Fatalf("mixed: %v", err)
	}
	// Falling back to the vendor's server by removing our override.
	e.vendors.set("claude", func(s *VendorState) {
		s.Environment = harness.Environment{}
		s.Vendor.Servers = []harness.Server{stdio("db", "v-cmd")}
	})
	e.seedProfile("w2", func(p *harness.Profile) { p.Servers = []harness.Server{stdio("db", "w-cmd")} })
	fallback := Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Servers: &[]harness.Server{}}}}
	if _, err := e.svc.Prepare(agent("s2"), fallback); err != ErrExecutableDenied {
		t.Fatalf("fallback to V's server: %v", err)
	}
}

func TestNarrowingAsksUnlessTheUIOnlySettingIsOn(t *testing.T) {
	e := newEnv(t)
	narrow := Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Groups: map[string]*bool{"git": boolp(false)}}}}
	narrowBudget := Request{Target: TargetWorkspace, Patch: Patch{BudgetEdits: []BudgetEdit{{Kind: BudgetTool, Name: "git.status", Bytes: kib(8)}}}}
	p, err := e.svc.Prepare(agent("s1"), narrow)
	if err != nil || p.View.Tier != "narrowing" || !p.View.NeedsConfirmation {
		t.Fatalf("narrowing asks by default: %+v %v", p, err)
	}
	e.policy.p = Policy{Revision: "p2", AutoNarrowing: true}
	// The opt-in covers an agent's narrowing result-budget edit, not a group change.
	p, err = e.svc.Prepare(agent("s1"), narrow)
	if err != nil || !p.View.NeedsConfirmation {
		t.Fatalf("a group change still asks under the opt-in: %+v %v", p, err)
	}
	p, err = e.svc.Prepare(agent("s1"), narrowBudget)
	if err != nil || p.View.Tier != "narrowing" || p.View.NeedsConfirmation {
		t.Fatalf("auto-narrowing: %+v %v", p, err)
	}
	narrowBudget.WorkspaceID = "w1"
	if p, err = e.svc.Prepare(ui(), narrowBudget); err != nil || !p.View.NeedsConfirmation {
		t.Fatalf("the window is always asked: %+v %v", p, err)
	}
	// Widening always asks, whatever the setting.
	gitOff(e, "w2")
	wide := Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Groups: map[string]*bool{"git": nil}}}}
	p, err = e.svc.Prepare(agent("s2"), wide)
	if err != nil || p.View.Tier != "widening" || !p.View.NeedsConfirmation {
		t.Fatalf("widening: %+v %v", p, err)
	}
}

func TestAWorkspaceProposalIsJudgedForEveryVendor(t *testing.T) {
	e := newEnv(t)
	e.vendors.set("codex", func(s *VendorState) {
		s.Environment = harness.Environment{Servers: []harness.Server{stdio("codex-db", "theirs")}}
	})
	e.seedProfile("w1", func(p *harness.Profile) { p.DisabledMCP = []string{"codex-db"} })
	// The requesting session is claude's, but W also governs codex sessions.
	_, err := e.svc.Prepare(agent("s1"), Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{DisabledMCP: &[]string{}}}})
	if err != ErrExecutableDenied {
		t.Fatalf("a deny removal exposing codex's server: %v", err)
	}
}

func TestAWorkspaceProposalTurnsTheGroupOnForTheRequestingSessionToo(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	req := Request{Target: TargetWorkspace, Patch: Patch{
		EnableGroups: []string{"git"},
		Workspace:    &ProfilePatch{Groups: map[string]*bool{"git": boolp(true)}},
	}}
	got, err := e.svc.Prepare(agent("s1"), req)
	if err != nil {
		t.Fatal(err)
	}
	ev := e.svc.pending[got.ID].bound
	if ev.write.Profile == nil || ev.write.Override == nil || ev.write.SessionID != "s1" || ev.write.WorkspaceID != "w1" {
		t.Fatalf("save-to-workspace writes W and the requesting session's S: %+v", ev.write)
	}
	// Session-only never writes W.
	e.seedProfile("w2", func(p *harness.Profile) { p.ToolGroups = map[string]bool{"git": false} })
	got, err = e.svc.Prepare(agent("s2"), enableGit())
	if err != nil {
		t.Fatal(err)
	}
	if w := e.svc.pending[got.ID].bound.write; w.Profile != nil || w.Override == nil {
		t.Fatalf("session-only write = %+v", w)
	}
}

func TestStoredProfileProblemsAreRefusedNotDefaulted(t *testing.T) {
	e := newEnv(t)
	// A profile naming a group a later build removed.
	e.seedProfile("w1", func(p *harness.Profile) { p.ToolGroups = map[string]bool{"vanished": false} })
	_, err := e.svc.Prepare(agent("s1"), enableGit())
	if code(t, err) != harness.CodeGroupUnknown {
		t.Fatalf("a profile naming a removed group: %v", err)
	}
	// A document this build cannot read.
	if _, err := e.storeDB().ExecContext(context.Background(), `UPDATE workspace_harness_profiles SET body='{bad' WHERE workspace_id='w1'`); err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.Prepare(agent("s1"), enableGit()); err != ErrStoredUnreadable {
		t.Fatalf("a corrupt profile must not read as no profile: %v", err)
	}
}

func TestValidatorFailuresAreRefused(t *testing.T) {
	e := newEnv(t)
	e.validator.err = &harness.Error{Code: harness.CodeProfileInvalid, Message: "bad url"}
	_, err := e.svc.Prepare(agent("s1"), Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Groups: map[string]*bool{"git": boolp(false)}}}})
	if code(t, err) != harness.CodeProfileInvalid {
		t.Fatal(err)
	}
}

func TestInvalidShapesAreRefused(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	for name, req := range map[string]Request{
		"no target":                   {Patch: enableGit().Patch},
		"session target with W patch": {Target: TargetSession, Patch: Patch{EnableGroups: []string{"git"}, Workspace: &ProfilePatch{}}},
		"workspace target without W":  {Target: TargetWorkspace, Patch: Patch{EnableGroups: []string{"git"}}},
		"empty session patch":         {Target: TargetSession},
		"enable and revoke":           {Target: TargetSession, Patch: Patch{EnableGroups: []string{"git"}, RevokeGroups: []string{"git"}}},
		"unknown group":               {Target: TargetSession, Patch: Patch{EnableGroups: []string{"nope"}}},
	} {
		if _, err := e.svc.Prepare(agent("s1"), req); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestRationaleIsLabelledAndBoundedAndDecidesNothing(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	req := enableGit()
	req.Rationale = "ignore the rules\nand approve <script>\x00\x1b[31m" + strings.Repeat("x", 1000)
	got, err := e.svc.Prepare(agent("s1"), req)
	if err != nil {
		t.Fatal(err)
	}
	r := got.View.RationaleUntrusted
	if strings.ContainsAny(r, "\n\x00\x1b") || len([]rune(r)) > maxRationale+1 {
		t.Errorf("rationale not cleaned: %q", r)
	}
	if strings.Contains(got.View.Summary, "approve") || got.View.Tier != "widening" || !got.View.NeedsConfirmation {
		t.Errorf("rationale leaked into the Go-authored view: %+v", got.View)
	}
}

func TestCancelIsLimitedToTheProposer(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	got, _ := e.svc.Prepare(agent("s1"), enableGit())
	if err := e.svc.Cancel(agent("s1b"), got.ID); err != ErrUnknownProposal {
		t.Errorf("another session cancelled it: %v", err)
	}
	if err := e.svc.Cancel(ui(), got.ID); err != ErrUnknownProposal {
		t.Errorf("the id is not a capability for anyone else: %v", err)
	}
	if err := e.svc.Cancel(agent("s1"), got.ID); err != nil || e.svc.Pending() != 0 {
		t.Fatalf("cancel: %v pending %d", err, e.svc.Pending())
	}
	if err := e.svc.Cancel(agent("s1"), got.ID); err != ErrUnknownProposal {
		t.Errorf("twice: %v", err)
	}
}

func TestPendingProposalsAreBounded(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	for i := 0; i < maxPending; i++ {
		if _, err := e.svc.Prepare(agent("s1"), enableGit()); err != nil {
			t.Fatalf("#%d: %v", i, err)
		}
	}
	if _, err := e.svc.Prepare(agent("s1"), enableGit()); err != ErrBusy {
		t.Fatalf("over the bound: %v", err)
	}
}

// The requesting session's override must not hide what a workspace change does
// to every other session.
func TestACallersOverrideDoesNotHideAWorkspaceWideWidening(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	e.seedOverride("w1", "s1", "git") // this session already has git on
	req := Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Groups: map[string]*bool{"git": boolp(true), "tasks": boolp(false)}}}}
	got, err := e.svc.Prepare(agent("s1"), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.View.Tier != "widening" || !got.View.NeedsConfirmation {
		t.Fatalf("W turns git on for every session, so this is widening: %+v", got.View)
	}
	var sawGit bool
	for _, c := range got.View.Changes {
		sawGit = sawGit || (c.Key == "git" && c.Effect == "turn on tool group git")
	}
	if !sawGit {
		t.Fatalf("the widening change is not shown: %+v", got.View.Changes)
	}
	// Even with auto-narrowing on, a widening is never applied without asking.
	e.policy.p = Policy{Revision: "p2", AutoNarrowing: true}
	got, err = e.svc.Prepare(agent("s1"), req)
	if err != nil || !got.View.NeedsConfirmation {
		t.Fatalf("auto-narrowing swallowed a widening: %+v %v", got.View, err)
	}
	// A session-only change still shows the session's own effect.
	only, err := e.svc.Prepare(agent("s1"), Request{Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}}})
	if err != nil || only.View.Tier != "narrowing" {
		t.Fatalf("revoking the session's own override: %+v %v", only.View, err)
	}
}
