package harnessconfig

import (
	"testing"

	"devkit/internal/harness"
	"devkit/internal/identity"
)

// digestOf re-evaluates a pending proposal's stored request, as the apply step
// will, and returns the digest.
func digestOf(t *testing.T, e *env, session string, req Request) string {
	t.Helper()
	patch, err := req.Patch.normalized()
	if err != nil {
		t.Fatal(err)
	}
	req.Patch, req.Rationale = patch, cleanRationale(req.Rationale)
	ev, _, err := e.svc.evaluate(agent(session), identity.Agent(session), req, e.svc.liveReaderFor(agent(session)))
	if err != nil {
		t.Fatal(err)
	}
	return ev.digest()
}

func TestTheBindingIsStableAndMovesWithEveryInput(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	e.seedOverride("w1", "s1", "tasks")
	req := enableGit()
	ref := digestOf(t, e, "s1", req)
	if again := digestOf(t, e, "s1", req); again != ref {
		t.Fatal("an unchanged world must evaluate to the same binding")
	}
	moves := map[string]func(){
		"V revision": func() { e.vendors.set("claude", func(s *VendorState) { s.Vendor.Revision++ }) },
		"policy":     func() { e.policy.p.Revision = "p2" },
		"W": func() {
			e.seedProfile("w1", func(p *harness.Profile) { p.ToolGroups = map[string]bool{"git": false, "tasks": true} })
		},
		"S": func() { e.seedOverride("w1", "s1", "tasks", "codegraph") },
		"vendor server": func() {
			e.vendors.set("claude", func(s *VendorState) { s.Environment.Servers = []harness.Server{stdio("v", "cmd")} })
		},
		"vendor command": func() {
			e.vendors.set("claude", func(s *VendorState) { s.Environment.Servers = []harness.Server{stdio("v", "other")} })
		},
		"vendor skill": func() {
			e.vendors.set("claude", func(s *VendorState) { s.Environment.Skills = []string{"/s/SKILL.md"} })
		},
	}
	for name, mutate := range moves {
		e = newEnv(t)
		gitOff(e, "w1")
		e.seedOverride("w1", "s1", "tasks")
		before := digestOf(t, e, "s1", req)
		mutate()
		if digestOf(t, e, "s1", req) == before {
			t.Errorf("%s did not move the binding", name)
		}
	}
	// A different patch is a different binding, even for the same proposer.
	e = newEnv(t)
	gitOff(e, "w1")
	a := digestOf(t, e, "s1", enableGit())
	b := digestOf(t, e, "s1", Request{Target: TargetSession, Patch: Patch{EnableGroups: []string{"git", "tasks"}}})
	if a == b {
		t.Error("a changed patch kept the binding")
	}
}

func TestEquivalentRepresentationsBindIdentically(t *testing.T) {
	e := newEnv(t)
	e.seedProfile("w1", func(p *harness.Profile) { p.ToolGroups = map[string]bool{"git": false} })
	a := Request{Target: TargetSession, Patch: Patch{EnableGroups: []string{"git", "tasks", "git"}}}
	b := Request{Target: TargetSession, Patch: Patch{EnableGroups: []string{"tasks", "git"}}}
	if digestOf(t, e, "s1", a) != digestOf(t, e, "s1", b) {
		t.Error("order and duplicates in a set must not change the binding")
	}
	srvA := &[]harness.Server{stdio("a", "x"), stdio("b", "y")}
	srvB := &[]harness.Server{stdio("b", "y"), stdio("a", "x")}
	wa := Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Servers: srvA}}}
	wb := Request{Target: TargetWorkspace, Patch: Patch{Workspace: &ProfilePatch{Servers: srvB}}}
	// Both are executable and refused at Prepare, but their evaluations agree.
	patchA, _ := wa.Patch.normalized()
	patchB, _ := wb.Patch.normalized()
	if len(*patchA.Workspace.Servers) != 2 || (*patchA.Workspace.Servers)[0].Name != (*patchB.Workspace.Servers)[0].Name {
		t.Error("servers must normalize to one order")
	}
}

func TestCallerOwnedValuesCannotChangeAPreparedProposal(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	groups := []string{"git"}
	req := Request{Target: TargetSession, Patch: Patch{EnableGroups: groups}, Rationale: "enable it"}
	got, err := e.svc.Prepare(agent("s1"), req)
	if err != nil {
		t.Fatal(err)
	}
	groups[0] = "tasks"
	req.Patch.EnableGroups = append(req.Patch.EnableGroups, "codegraph")
	req.Rationale = "something else"
	p := e.svc.pending[got.ID]
	if p.req.Patch.EnableGroups[0] != "git" || len(p.req.Patch.EnableGroups) != 1 || p.req.Rationale != "enable it" {
		t.Fatalf("the prepared request aliases the caller's values: %+v", p.req)
	}
	ev, _, err := e.svc.evaluate(agent("s1"), identity.Agent("s1"), p.req, e.svc.liveReaderFor(agent("s1")))
	if err != nil || ev.digest() != p.bound.digest() {
		t.Fatalf("re-evaluating the stored request must reproduce the binding: %v", err)
	}
}

func TestRetargetingASkillSymlinkChangesTheBinding(t *testing.T) {
	e := newEnv(t)
	e.canonical.m["/link/skills"] = "/real/a"
	e.seedProfile("w1", func(p *harness.Profile) {
		p.ToolGroups = map[string]bool{"git": false}
		p.SkillFolders = []string{"/link/skills"}
	})
	ref := digestOf(t, e, "s1", enableGit())
	e.canonical.m["/link/skills"] = "/real/b"
	if digestOf(t, e, "s1", enableGit()) == ref {
		t.Fatal("a symlink that now points elsewhere left the binding unchanged")
	}
}

func TestRevokingOverrideGroupsIsANarrowingPath(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	e.seedOverride("w1", "s1", "git")
	got, err := e.svc.Prepare(agent("s1"), Request{Target: TargetSession, Patch: Patch{RevokeGroups: []string{"git"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got.View.Tier != "narrowing" {
		t.Fatalf("revoking an override = %s", got.View.Tier)
	}
	w := e.svc.pending[got.ID].bound.write
	if w.Profile != nil || w.Override == nil || w.Override.Value != nil {
		t.Fatalf("revoking the last group deletes S, W untouched: %+v", w)
	}
	// A narrowing that nobody asked a person about is still gated by the setting.
	if !got.View.NeedsConfirmation {
		t.Error("narrowing asks by default")
	}
}
