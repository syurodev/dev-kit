package harnessconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"devkit/internal/harness"
)

func (e *env) provider() *Provider {
	return NewProvider(ProviderConfig{Store: e.store, Vendors: e.vendors, Registry: registry, Canonical: e.canonical.resolve})
}

func snapReq(session, ws, vendor string) SnapshotRequest {
	return SnapshotRequest{SessionID: session, WorkspaceID: ws, Vendor: vendor}
}

func TestAnUnprofiledSessionResolvesToTheDefaultNotToAnAbsence(t *testing.T) {
	e := newEnv(t)
	snap, err := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if err != nil {
		t.Fatal(err)
	}
	r := snap.Ref
	if !r.Default || r.Revisions.Workspace != nil || r.Revisions.Session != nil || r.Revisions.Vendor != 4 || r.Schema != harness.SnapshotSchema {
		t.Fatalf("ref = %+v", r)
	}
	if !snap.Effective.ServingIsDefault() || !r.FactsComplete || r.Fingerprint == "" || r.ExecutableDigest == "" {
		t.Fatalf("a true default serves everything: %+v", r)
	}
	for _, tool := range registry().Tools {
		if !snap.Effective.Served(tool.Name) {
			t.Errorf("%s not served by the default", tool.Name)
		}
	}
}

func TestAPresentEmptyProfileIsNotTheDefaultReference(t *testing.T) {
	e := newEnv(t)
	e.seedProfile("w1", func(p *harness.Profile) {})
	snap, err := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Ref.Default || snap.Ref.Revisions.Workspace == nil || *snap.Ref.Revisions.Workspace != 1 || !snap.Effective.ServingIsDefault() {
		t.Fatalf("an empty profile serves the default but is not 'no profile': %+v", snap.Ref)
	}
}

func TestAnUnresolvableConfigurationIsUnavailableNeverDefaulted(t *testing.T) {
	cases := map[string]func(e *env) *Provider{
		"corrupt profile": func(e *env) *Provider {
			e.seedProfile("w1", func(p *harness.Profile) {})
			if _, err := e.storeDB().ExecContext(context.Background(), `UPDATE workspace_harness_profiles SET body='{bad' WHERE workspace_id='w1'`); err != nil {
				t.Fatal(err)
			}
			return e.provider()
		},
		"corrupt override": func(e *env) *Provider {
			e.seedOverride("w1", "s1", "git")
			if _, err := e.storeDB().ExecContext(context.Background(), `UPDATE session_harness_overrides SET body='{bad' WHERE session_id='s1'`); err != nil {
				t.Fatal(err)
			}
			return e.provider()
		},
		"vendor read fails": func(e *env) *Provider { e.vendors.err = context.DeadlineExceeded; return e.provider() },
		"no registry": func(e *env) *Provider {
			return NewProvider(ProviderConfig{Store: e.store, Vendors: e.vendors})
		},
		"profile naming a removed group": func(e *env) *Provider {
			e.seedProfile("w1", func(p *harness.Profile) { p.ToolGroups = map[string]bool{"gone": false} })
			return e.provider()
		},
		"nil provider": func(*env) *Provider { return nil },
	}
	for name, build := range cases {
		e := newEnv(t)
		snap, err := build(e).Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
		var he *harness.Error
		if !errors.As(err, &he) || he.Code != harness.CodeSnapshotUnavailable {
			t.Errorf("%s: %v", name, err)
		}
		if snap.Ref.Default || snap.Effective.Served("git.status") {
			t.Errorf("%s: an unresolved configuration looked like a default: %+v", name, snap.Ref)
		}
	}
	for _, bad := range []SnapshotRequest{snapReq("", "w1", "claude"), snapReq("s1", "", "claude"), snapReq("s1", "w1", "")} {
		if _, err := newEnv(t).provider().Snapshot(context.Background(), bad); err == nil {
			t.Errorf("%+v resolved", bad)
		}
	}
}

func TestOnlyTheActualWorkspaceAndSessionScopeApply(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w2")
	e.seedOverride("w1", "s1", "tasks")
	snap, err := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Ref.Default || snap.Ref.Revisions.Workspace != nil || snap.Ref.Revisions.Session == nil {
		t.Fatalf("another workspace's profile leaked: %+v", snap.Ref)
	}
	// A fresh child asks with its own id: the parent's override is not seen.
	child, err := e.provider().Snapshot(context.Background(), snapReq("s1b", "w1", "claude"))
	if err != nil || child.Ref.Revisions.Session != nil || !child.Ref.Default {
		t.Fatalf("a child inherited its sibling or parent's override: %+v %v", child.Ref, err)
	}
	// An override written under another workspace does not follow the session.
	moved, err := e.provider().Snapshot(context.Background(), snapReq("s1", "w2", "claude"))
	if err != nil || moved.Ref.Revisions.Session != nil {
		t.Fatalf("a moved session kept its override: %+v %v", moved.Ref, err)
	}
}

func TestSessionOverrideTurnsAGroupOnInTheSnapshot(t *testing.T) {
	e := newEnv(t)
	gitOff(e, "w1")
	off, _ := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if off.Effective.Served("git.status") || off.Effective.ServingIsDefault() {
		t.Fatal("git should be off")
	}
	e.seedOverride("w1", "s1", "git")
	on, _ := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if !on.Effective.Served("git.status") || on.Ref.Revisions.Session == nil || *on.Ref.Revisions.Workspace != 1 {
		t.Fatalf("%+v", on.Ref)
	}
	if on.Ref.Equal(off.Ref) {
		t.Fatal("different configurations compared equal")
	}
}

func TestTheSameModelUnderADifferentHarnessIsADifferentSnapshot(t *testing.T) {
	e := newEnv(t)
	model := harness.ModelProfileRef{ProfileID: "p", Vendor: "claude", Model: "m", Effort: "high"}
	a, _ := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	gitOff(e, "w1")
	b, _ := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if a.Ref.Equal(b.Ref) || a.Ref.Fingerprint == b.Ref.Fingerprint {
		t.Fatal("two harnesses compared equal")
	}
	if model.Effort != "high" || strings.Contains(fmt.Sprintf("%+v", a.Ref), "high") || strings.Contains(fmt.Sprintf("%+v", b.Ref), "claude-model") {
		t.Fatal("the model profile and the harness reference must stay separate")
	}
}

func TestSnapshotsAreDetachedFromEverythingTheyWereBuiltFrom(t *testing.T) {
	e := newEnv(t)
	args := []string{"--a"}
	e.vendors.set("claude", func(s *VendorState) {
		s.Vendor.Servers = []harness.Server{{Name: "db", Command: "cmd", Args: args, Enabled: true}}
		s.Vendor.DisabledMCP = []string{"x"}
	})
	snap, err := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if err != nil {
		t.Fatal(err)
	}
	args[0] = "--mutated"
	if snap.Effective.Servers[0].Args[0] != "--a" {
		t.Fatal("the vendor's slice is shared with the snapshot")
	}
	clone := snap.Clone()
	snap.Effective.Servers[0].Args[0] = "--tampered"
	snap.Effective.DisabledMCP[0] = "tampered"
	snap.Effective.Groups[0].On = !snap.Effective.Groups[0].On
	r := int64(99)
	snap.Ref.Revisions.Workspace = &r
	if clone.Effective.Servers[0].Args[0] != "--a" || clone.Effective.DisabledMCP[0] != "x" || clone.Ref.Revisions.Workspace != nil {
		t.Fatal("a clone shares memory with its source")
	}
	// Mutating the group table of a clone must not change what the original serves.
	again, _ := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	c2 := again.Clone()
	c2.Effective.Groups[0].On = false
	if !again.Effective.Groups[0].On {
		t.Fatal("clone mutation reached the original")
	}
}

func TestFactsCoverageIsReportedAndNeverOverstated(t *testing.T) {
	e := newEnv(t)
	e.vendors.set("claude", func(s *VendorState) {
		s.FactsComplete = false
		s.Environment = harness.Environment{Servers: []harness.Server{stdio("v", "SECRET-CMD")}}
	})
	snap, err := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Ref.FactsComplete {
		t.Fatal("incomplete vendor facts were reported as verified")
	}
	// Incomplete facts are not used at all, and the public reference carries no
	// command, path or argument.
	if strings.Contains(fmt.Sprintf("%+v", snap.Ref), "SECRET-CMD") {
		t.Fatal("the reference leaks a command")
	}
	bareIncomplete, _ := newEnv(t).provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if snap.Ref.Fingerprint != bareIncomplete.Ref.Fingerprint || snap.Ref.ExecutableDigest != bareIncomplete.Ref.ExecutableDigest {
		t.Fatal("facts the host called incomplete were used anyway")
	}
	complete := newEnv(t)
	complete.vendors.set("claude", func(s *VendorState) {
		s.Environment = harness.Environment{Servers: []harness.Server{stdio("v", "cmd")}}
	})
	full, _ := complete.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	bare, _ := newEnv(t).provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if !full.Ref.FactsComplete || full.Ref.Fingerprint == bare.Ref.Fingerprint || full.Ref.ExecutableDigest == bare.Ref.ExecutableDigest {
		t.Fatal("complete vendor facts must show up in the fingerprint and the executables")
	}
}

func TestExecutableDigestFollowsTheResolvedTargetAndIgnoresOrder(t *testing.T) {
	e := newEnv(t)
	e.canonical.m["/link/s"] = "/real/a"
	e.seedProfile("w1", func(p *harness.Profile) { p.SkillFolders = []string{"/link/s", "/other"} })
	a, _ := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	e.canonical.m["/link/s"] = "/real/b"
	b, _ := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if a.Ref.Fingerprint == b.Ref.Fingerprint || a.Ref.ExecutableDigest == b.Ref.ExecutableDigest {
		t.Fatal("a retargeted symlink did not change the snapshot identity")
	}
	e2 := newEnv(t)
	e2.seedProfile("w1", func(p *harness.Profile) { p.SkillFolders = []string{"/other", "/link/s"} })
	e2.canonical.m["/link/s"] = "/real/b"
	c, _ := e2.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if b.Ref.ExecutableDigest != c.Ref.ExecutableDigest {
		t.Fatal("list order changed the executable digest")
	}
}

func TestAStaleReferenceIsNotEqualToTheCurrentOne(t *testing.T) {
	e := newEnv(t)
	first, _ := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	e.vendors.set("claude", func(s *VendorState) { s.Vendor.Revision++ })
	second, _ := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if first.Ref.Equal(second.Ref) {
		t.Fatal("a V revision change kept the reference equal")
	}
	if !first.Ref.Equal(first.Clone().Ref) {
		t.Fatal("a clone must be equal to its source")
	}
}
