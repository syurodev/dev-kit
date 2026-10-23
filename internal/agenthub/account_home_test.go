package agenthub

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
)

func TestPrepareAccountHomeLinksTheStore(t *testing.T) {
	shared := t.TempDir()
	home := filepath.Join(t.TempDir(), "acct")
	if err := PrepareAccountHome(AgentTypeCodex, home, shared); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(shared, "sessions")
	target, err := os.Readlink(filepath.Join(home, "sessions"))
	if err != nil || target != want {
		t.Fatalf("sessions link = %q, %v; want %q", target, err, want)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Errorf("the shared store directory must exist: %v", err)
	}
}

func TestPrepareAccountHomeTwiceIsANoOp(t *testing.T) {
	shared := t.TempDir()
	home := filepath.Join(t.TempDir(), "acct")
	for range 2 {
		if err := PrepareAccountHome(AgentTypeClaude, home, shared); err != nil {
			t.Fatal(err)
		}
	}
	target, err := os.Readlink(filepath.Join(home, "projects"))
	if err != nil || target != filepath.Join(shared, "projects") {
		t.Fatalf("projects link = %q, %v", target, err)
	}
}

func TestPrepareAccountHomeAcceptsUncleanPaths(t *testing.T) {
	base := t.TempDir()
	shared := filepath.Join(base, "shared")
	home := filepath.Join(base, "acct")
	if err := PrepareAccountHome(AgentTypeCodex, home, shared); err != nil {
		t.Fatal(err)
	}
	// The same homes spelled with a detour and a trailing slash match the link.
	if err := PrepareAccountHome(AgentTypeCodex, home+"/", filepath.Join(base, "x", "..", "shared")+"/"); err != nil {
		t.Fatalf("an unclean spelling of the same paths must match the existing link: %v", err)
	}
}

func TestPrepareAccountHomeCreatesItPrivate(t *testing.T) {
	home := filepath.Join(t.TempDir(), "acct")
	if err := PrepareAccountHome(AgentTypeCodex, home, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(home); info.Mode().Perm() != 0o700 {
		t.Errorf("home mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestPrepareAccountHomeTightensAnExistingHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "acct")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := PrepareAccountHome(AgentTypeCodex, home, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(home); info.Mode().Perm() != 0o700 {
		t.Errorf("existing home mode = %v, want 0700", info.Mode().Perm())
	}
}

func TestPrepareAccountHomeRefusesASymlinkedHome(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	if err := os.Symlink(real, home); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	err := PrepareAccountHome(AgentTypeCodex, home, t.TempDir())
	if !errors.Is(err, ErrAccountHome) {
		t.Fatalf("a symlinked home must be refused, got %v", err)
	}
	if entries, _ := os.ReadDir(real); len(entries) != 0 {
		t.Errorf("the link's target was written to: %v", entries)
	}
}

func TestPrepareAccountHomeRefusesAHomeThatIsAFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(home, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareAccountHome(AgentTypeCodex, home, t.TempDir()); !errors.Is(err, ErrAccountHome) {
		t.Fatalf("a file as home must be refused, got %v", err)
	}
}

func TestPrepareAccountHomeNeverReplacesRealData(t *testing.T) {
	shared := t.TempDir()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	err := PrepareAccountHome(AgentTypeCodex, home, shared)
	if !errors.Is(err, ErrAccountHome) {
		t.Fatalf("a real sessions directory must be refused with ErrAccountHome, got %v", err)
	}
	if info, err := os.Lstat(filepath.Join(home, "sessions")); err != nil || !info.IsDir() {
		t.Errorf("the existing directory was touched: %v", err)
	}
}

func TestPrepareAccountHomeRefusesALinkToSomewhereElse(t *testing.T) {
	shared := t.TempDir()
	elsewhere := t.TempDir()
	home := t.TempDir()
	link := filepath.Join(home, "sessions")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	err := PrepareAccountHome(AgentTypeCodex, home, shared)
	if !errors.Is(err, ErrAccountHome) {
		t.Fatalf("a link to somewhere else must be refused with ErrAccountHome, got %v", err)
	}
	if got, _ := os.Readlink(link); got != elsewhere {
		t.Errorf("the link was changed to %q, want it left at %q", got, elsewhere)
	}
}

func TestPrepareAccountHomeRefusesRelativePaths(t *testing.T) {
	if err := PrepareAccountHome(AgentTypeClaude, "relative/home", t.TempDir()); !errors.Is(err, ErrAccountHome) {
		t.Errorf("a relative home must be refused, got %v", err)
	}
	if err := PrepareAccountHome(AgentTypeClaude, filepath.Join(t.TempDir(), "home"), "relative/shared"); !errors.Is(err, ErrAccountHome) {
		t.Errorf("a relative sharedFrom must be refused, got %v", err)
	}
}

func TestPrepareAccountHomeRefusesAHomeInsideTheSharedStore(t *testing.T) {
	shared := t.TempDir()
	store := filepath.Join(shared, "sessions")
	for _, home := range []string{store, filepath.Join(store, "acct")} {
		if err := PrepareAccountHome(AgentTypeCodex, home, shared); !errors.Is(err, ErrAccountHome) {
			t.Errorf("home %s is in the shared store and must be refused, got %v", home, err)
		}
	}
	if _, err := os.Lstat(store); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("nothing may be created for a refused home: %v", err)
	}
}

func TestPrepareAccountHomeKeepsTheOSError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := PrepareAccountHome(AgentTypeCodex, filepath.Join(blocker, "acct"), t.TempDir())
	if !errors.Is(err, ErrAccountHome) || !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("err = %v, want ErrAccountHome wrapping ENOTDIR", err)
	}
}

func TestSharedStorePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".claude")
	got := SharedStorePaths(AgentTypeClaude, root)
	if want := []string{filepath.Join(root, "projects")}; !slices.Equal(got, want) {
		t.Errorf("claude shared paths = %v, want %v", got, want)
	}
	if got := SharedStorePaths(AgentTypeMock, root); len(got) != 0 {
		t.Errorf("a vendor with no shared store has none, got %v", got)
	}
}
