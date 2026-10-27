package harnessconfig

import (
	"context"
	"testing"

	"devkit/internal/harness"
)

func TestSnapshotCarriesTheBasisItWasResolvedFrom(t *testing.T) {
	e := newEnv(t)
	e.seedProfile("w1", func(p *harness.Profile) { p.ToolGroups = map[string]bool{"git": false} })
	e.seedOverride("w1", "s1", "git")
	snap, err := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if err != nil {
		t.Fatal(err)
	}
	b := snap.Basis
	if b == nil {
		t.Fatal("a snapshot must carry its serving basis")
	}
	if b.WorkspaceStamp() != (harness.Stamp{Revision: 1, Present: true}) || b.LaunchSession() != (harness.Stamp{Revision: 1, Present: true}) {
		t.Fatalf("stamps = %+v %+v", b.WorkspaceStamp(), b.LaunchSession())
	}
	if b.VendorRevision() != 4 || b.ExecutableDigest() != snap.Ref.ExecutableDigest {
		t.Fatalf("vendor/executable = %d %s", b.VendorRevision(), b.ExecutableDigest())
	}
	// Edits made after the launch reach neither the basis nor a live resolve.
	e.seedProfile("w1", func(p *harness.Profile) {})
	e.vendors.set("claude", func(s *VendorState) { s.Vendor.Revision = 9; s.Vendor.DisabledMCP = []string{"x"} })
	live, err := b.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if live.Served("git.status") {
		t.Fatal("revoking S must fall back to the W the session launched with, not the saved one")
	}
	if live.Revisions.Vendor != 4 || len(live.DisabledMCP) != 0 {
		t.Fatalf("V leaked into a running session: %+v", live.Revisions)
	}
}

func TestSnapshotKeepsATombstonedOverrideStamp(t *testing.T) {
	e := newEnv(t)
	st := e.seedOverride("w1", "s1", "git")
	cleared, err := e.store.ClearOverride(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := e.provider().Snapshot(context.Background(), snapReq("s1", "w1", "claude"))
	if err != nil {
		t.Fatal(err)
	}
	got := snap.Basis.LaunchSession()
	if got.Present || got.Revision != cleared.Revision || got.Revision <= st.Revision {
		t.Fatalf("an absent-at-N override must keep its tombstone revision, got %+v (cleared %+v)", got, cleared)
	}
	if snap.Ref.Revisions.Session != nil {
		t.Fatalf("an absent S stays a nil revision in the reference: %v", snap.Ref.Revisions.Session)
	}
}
