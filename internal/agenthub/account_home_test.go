package agenthub

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPrepareAccountHomeLinksTheSharedStore(t *testing.T) {
	shared := t.TempDir()
	home := filepath.Join(t.TempDir(), "acct")
	if err := PrepareAccountHome(AgentTypeCodex, home, shared); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "sessions")
	target, err := os.Readlink(link)
	if err != nil || target != filepath.Join(shared, "sessions") {
		t.Fatalf("sessions link = %q, %v; want %q", target, err, filepath.Join(shared, "sessions"))
	}
	if err := PrepareAccountHome(AgentTypeCodex, home, shared); err != nil {
		t.Fatalf("preparing twice must be a no-op: %v", err)
	}
	if info, _ := os.Stat(home); info.Mode().Perm() != 0o700 {
		t.Errorf("home mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestPrepareAccountHomeNeverReplacesRealData(t *testing.T) {
	shared := t.TempDir()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := PrepareAccountHome(AgentTypeCodex, home, shared); err == nil {
		t.Fatal("a real sessions directory must be refused, not replaced")
	}
	if _, err := os.Stat(filepath.Join(home, "sessions")); err != nil {
		t.Errorf("the existing directory was touched: %v", err)
	}
}

func TestPrepareAccountHomeRefusesRelativePaths(t *testing.T) {
	if err := PrepareAccountHome(AgentTypeClaude, "relative/home", t.TempDir()); err == nil {
		t.Fatal("a relative home must be refused")
	}
}

func TestSharedStorePaths(t *testing.T) {
	got := SharedStorePaths(AgentTypeClaude, "/x/.claude")
	if !slices.Equal(got, []string{"/x/.claude/projects"}) {
		t.Errorf("claude shared paths = %v", got)
	}
	if got := SharedStorePaths(AgentTypeMock, "/x"); len(got) != 0 {
		t.Errorf("a vendor with no shared store has none, got %v", got)
	}
}

func TestCarveOutAllKeepsEveryRootReachableAndHidesTheRest(t *testing.T) {
	data := t.TempDir()
	mk := func(p ...string) string {
		path := filepath.Join(append([]string{data}, p...)...)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	mk("agent-homes", "codex", "sessions")
	mk("agent-homes", "codex", "auth")
	keepHome := mk("agent-accounts", "codex", "work")
	mk("agent-accounts", "codex", "other")
	keepShared := filepath.Join(data, "agent-homes", "codex", "sessions")
	deny := []string{filepath.Join(data, "agent-homes"), filepath.Join(data, "agent-accounts"), filepath.Join(data, "devkit.db")}

	got := carveOutAll(deny, keepHome, keepShared)

	for _, hidden := range []string{
		filepath.Join(data, "agent-homes", "codex", "auth"),
		filepath.Join(data, "agent-accounts", "codex", "other"),
		filepath.Join(data, "devkit.db"),
	} {
		if !slices.Contains(got, hidden) {
			t.Errorf("%s must stay hidden; deny = %v", hidden, got)
		}
	}
	for _, kept := range []string{keepHome, keepShared, filepath.Join(data, "agent-homes"), filepath.Join(data, "agent-accounts")} {
		if slices.Contains(got, kept) {
			t.Errorf("%s must not be denied (it is, or holds, a kept root); deny = %v", kept, got)
		}
	}
}
