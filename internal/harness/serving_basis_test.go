package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// basisFor resolves in once, as a launch does, and freezes it.
func basisFor(t *testing.T, in Inputs, w, s Stamp) (*ServingBasis, Effective) {
	t.Helper()
	rec := NewCanonRecorder(in.Canonical)
	in.Canonical = rec.Canonical
	eff := resolve(t, in)
	b, err := NewServingBasis(in, eff, rec, w, s)
	if err != nil {
		t.Fatalf("NewServingBasis: %v", err)
	}
	return b, eff
}

func gitOff() *Profile {
	return profile(func(p *Profile) { p.ToolGroups = map[string]bool{"git": false} })
}

func TestRevokingSAgainstThePinnedWDisablesWhatWDisabled(t *testing.T) {
	// W (rev 3) turns git off; the launch had S enabling it.
	in := base()
	in.Workspace = gitOff()
	in.Session = &Override{Schema: SchemaVersion, Revision: 5, Groups: []string{"git"}}
	b, launch := basisFor(t, in, Stamp{Revision: 3, Present: true}, Stamp{Revision: 5, Present: true})
	if !launch.Served("git.status") {
		t.Fatal("the launch should serve git through S")
	}
	// S is revoked: a tombstone at revision 6. Resolve against the launch's W.
	live, err := b.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if live.Served("git.status") || live.Served("git.commit") {
		t.Fatal("revoking S must fall back to the pinned W, which turns git off")
	}
	if live.Revisions.Workspace == nil || *live.Revisions.Workspace != 3 {
		t.Fatalf("W revision = %v, want the pinned 3", live.Revisions.Workspace)
	}
	if live.Revisions.Session != nil {
		t.Fatalf("a revoked S is an absent input, got %v", live.Revisions.Session)
	}
}

func TestLiveResolutionIgnoresWhatWBecameAfterLaunch(t *testing.T) {
	// The launch ran with git on (W absent). Saved W later turns git off: the
	// basis holds the launch's W, so an S write cannot adopt that.
	in := base()
	b, _ := basisFor(t, in, Stamp{Revision: 2}, Stamp{})
	live, err := b.Resolve(&Override{Schema: SchemaVersion, Revision: 1, Groups: []string{"tasks"}})
	if err != nil {
		t.Fatal(err)
	}
	if !live.Served("git.status") {
		t.Fatal("pinned W is absent, so git stays on whatever saved W says now")
	}
	if live.Revisions.Workspace != nil {
		t.Fatalf("an absent pinned W must stay nil, got %v", live.Revisions.Workspace)
	}
}

func TestLiveResolutionChangesGroupsOnly(t *testing.T) {
	budgets := &BudgetProfile{Groups: map[string]BudgetRule{"tasks": {Bytes: ptr(4096)}}}
	in := base()
	in.Workspace = profile(func(p *Profile) {
		p.ToolGroups = map[string]bool{"git": false}
		p.ResultBudget = budgets
		p.DisabledMCP = []string{"x"}
	})
	b, launch := basisFor(t, in, Stamp{Revision: 3, Present: true}, Stamp{})
	live, err := b.Resolve(&Override{Schema: SchemaVersion, Revision: 1, Groups: []string{"git"}})
	if err != nil {
		t.Fatal(err)
	}
	if live.ExecutableDigest() != launch.ExecutableDigest() {
		t.Fatal("a group change must leave the executable digest alone")
	}
	for _, tool := range []string{"tasks.list", "git.status", "codegraph.searchSymbol"} {
		l, _ := launch.Budget(tool)
		n, _ := live.Budget(tool)
		if l != n {
			t.Errorf("budget of %s moved from %+v to %+v", tool, l, n)
		}
	}
	if got, want := live.DisabledMCP, launch.DisabledMCP; len(got) != len(want) {
		t.Errorf("deny list moved: %v -> %v", want, got)
	}
	if !live.Served("git.status") {
		t.Error("S should have turned git on")
	}
}

func TestNilEmptyAndTombstoneAreDistinctIdentities(t *testing.T) {
	b, _ := basisFor(t, base(), Stamp{}, Stamp{})
	never, err := b.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := b.Resolve(&Override{Schema: SchemaVersion, Revision: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Same served set; different inputs, so different identities.
	if never.Fingerprint == empty.Fingerprint {
		t.Fatal("an absent S and a present-empty S must not share a fingerprint")
	}
	idNever := b.Identity(Stamp{Revision: 0}, never)
	idTomb := b.Identity(Stamp{Revision: 7}, never)
	idPresent := b.Identity(Stamp{Revision: 7, Present: true}, empty)
	if idNever == idTomb {
		t.Fatal("never-present and absent-at-7 must differ")
	}
	if idTomb == idPresent {
		t.Fatal("absent-at-7 and present-at-7 must differ")
	}
}

func TestBasisIsDetachedFromItsSources(t *testing.T) {
	in := base()
	in.Vendor = Vendor{Revision: 9, DisabledMCP: []string{"a"}, Servers: []Server{stdio("s", "/bin/s")}}
	in.Workspace = profile(func(p *Profile) { p.ToolGroups = map[string]bool{"git": false} })
	b, _ := basisFor(t, in, Stamp{Revision: 3, Present: true}, Stamp{})
	// Mutate every source after the launch.
	in.Vendor.DisabledMCP[0] = "mutated"
	in.Vendor.Servers[0].Command = "/bin/evil"
	in.Workspace.ToolGroups["git"] = true
	in.Registry.Tools[0].Name = "renamed"
	live, err := b.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if live.Served("git.status") {
		t.Fatal("the basis aliased W")
	}
	if len(live.DisabledMCP) != 1 || live.DisabledMCP[0] != "a" {
		t.Fatalf("the basis aliased V: %v", live.DisabledMCP)
	}
	if live.Servers[0].Command != "/bin/s" {
		t.Fatalf("the basis aliased V servers: %+v", live.Servers)
	}
	if !live.GroupOn("tasks") || !live.Served("tasks.list") {
		t.Fatal("the basis aliased the registry")
	}
}

func TestBasisFreezesSymlinkResolutionAtLaunch(t *testing.T) {
	dir := t.TempDir()
	one, two := filepath.Join(dir, "one"), filepath.Join(dir, "two")
	for _, d := range []string{one, two} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(one, link); err != nil {
		t.Fatal(err)
	}
	in := base()
	in.Vendor = Vendor{SkillFolders: []string{link}}
	in.Canonical = filepath.EvalSymlinks
	b, launch := basisFor(t, in, Stamp{}, Stamp{})
	// Retarget the link after launch.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(two, link); err != nil {
		t.Fatal(err)
	}
	live, err := b.Resolve(&Override{Schema: SchemaVersion, Revision: 1, Groups: []string{"git"}})
	if err != nil {
		t.Fatal(err)
	}
	if live.ExecutableDigest() != launch.ExecutableDigest() {
		t.Fatal("a retargeted symlink must not change what a group edit resolves")
	}
	if len(live.SkillFolders) != 1 || live.SkillFolders[0] != launch.SkillFolders[0] {
		t.Fatalf("skill folder = %v, want the launch's %v", live.SkillFolders, launch.SkillFolders)
	}
}

func TestLiveResolutionFailsClosed(t *testing.T) {
	b, _ := basisFor(t, base(), Stamp{}, Stamp{})
	if _, err := b.Resolve(&Override{Schema: SchemaVersion, Revision: 1, Groups: []string{"nope"}}); err == nil || code(t, err) != CodeGroupUnknown {
		t.Fatalf("an unknown group must be refused, got %v", err)
	}
	if _, err := b.Resolve(&Override{Schema: 99, Revision: 1}); err == nil {
		t.Fatal("an unsupported schema must be refused")
	}
	var nilBasis *ServingBasis
	if _, err := nilBasis.Resolve(nil); err == nil || code(t, err) != CodeSnapshotUnavailable {
		t.Fatalf("a missing basis is unavailable, got %v", err)
	}
}

func TestBasisRefusesAnIncoherentLaunch(t *testing.T) {
	in := base()
	rec := NewCanonRecorder(nil)
	in.Canonical = rec.Canonical
	eff := resolve(t, in)
	// A launch Effective that was not resolved from these inputs.
	other := base()
	other.Workspace = gitOff()
	otherEff := resolve(t, other)
	if _, err := NewServingBasis(in, otherEff, rec, Stamp{}, Stamp{}); err == nil {
		t.Fatal("inputs that do not reproduce the launch effective must be refused")
	}
	if _, err := NewServingBasis(in, eff, nil, Stamp{}, Stamp{}); err == nil {
		t.Fatal("a basis without a canonical recording is not frozen")
	}
}

func TestBasisAccessors(t *testing.T) {
	in := base()
	in.Vendor = Vendor{Revision: 9}
	in.Workspace = gitOff()
	b, launch := basisFor(t, in, Stamp{Revision: 3, Present: true}, Stamp{Revision: 2})
	if b.WorkspaceStamp() != (Stamp{Revision: 3, Present: true}) || b.LaunchSession() != (Stamp{Revision: 2}) {
		t.Fatalf("stamps = %+v %+v", b.WorkspaceStamp(), b.LaunchSession())
	}
	if b.VendorRevision() != 9 || b.ExecutableDigest() != launch.ExecutableDigest() {
		t.Fatalf("vendor/executable = %d %s", b.VendorRevision(), b.ExecutableDigest())
	}
	if !b.Launch().Served("tasks.list") || b.Launch().Served("git.status") {
		t.Fatal("Launch() must return the launch effective")
	}
}
