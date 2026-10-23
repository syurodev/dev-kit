package agenthub

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// mkTree makes the directories and files (names ending in ".file") under root.
func mkTree(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(root, p)
		if filepath.Ext(p) == ".file" {
			if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(full, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func assertDeny(t *testing.T, got []string, want ...string) {
	t.Helper()
	got = slices.Clone(got)
	want = slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("deny set\n got  %v\n want %v", got, want)
	}
}

func TestCarveOutAllTwoKeepsUnderOneDenyRoot(t *testing.T) {
	data := t.TempDir()
	mkTree(t, data,
		"agent-homes/codex/sessions", "agent-homes/codex/auth", "agent-homes/codex/config.file",
		"agent-homes/other/x", "agent-homes/top.file")
	root := filepath.Join(data, "agent-homes")

	got := carveOutAll([]string{root},
		filepath.Join(root, "codex", "sessions"), filepath.Join(root, "codex", "auth"))

	// Both kept paths are reachable; every sibling on the way is denied whole.
	assertDeny(t, got,
		filepath.Join(root, "codex", "config.file"),
		filepath.Join(root, "other"),
		filepath.Join(root, "top.file"))
}

func TestCarveOutAllAHomeAndASharedStoreUnderOneRoot(t *testing.T) {
	data := t.TempDir()
	mkTree(t, data,
		"agent-homes/codex/sessions", "agent-homes/codex/auth.file",
		"agent-homes/accounts/work/state", "agent-homes/accounts/other/state",
		"agent-homes/misc.file")
	root := filepath.Join(data, "agent-homes")

	got := carveOutAll([]string{root},
		filepath.Join(root, "accounts", "work"), filepath.Join(root, "codex", "sessions"))

	assertDeny(t, got,
		filepath.Join(root, "codex", "auth.file"),
		filepath.Join(root, "accounts", "other"),
		filepath.Join(root, "misc.file"))
}

func TestCarveOutAllKeepsEveryRootReachableAndHidesTheRest(t *testing.T) {
	data := t.TempDir()
	mkTree(t, data,
		"agent-homes/codex/sessions", "agent-homes/codex/auth",
		"agent-accounts/codex/work", "agent-accounts/codex/other")
	keepHome := filepath.Join(data, "agent-accounts", "codex", "work")
	keepShared := filepath.Join(data, "agent-homes", "codex", "sessions")
	deny := []string{
		filepath.Join(data, "agent-homes"),
		filepath.Join(data, "agent-accounts"),
		filepath.Join(data, "devkit.db"), // does not exist; no kept path inside it
	}

	got := carveOutAll(deny, keepHome, keepShared)

	assertDeny(t, got,
		filepath.Join(data, "agent-homes", "codex", "auth"),
		filepath.Join(data, "agent-accounts", "codex", "other"),
		filepath.Join(data, "devkit.db"))
}

func TestCarveOutAllWithNoKeepsDeniesEverythingWhole(t *testing.T) {
	data := t.TempDir()
	mkTree(t, data, "a/b")
	deny := []string{filepath.Join(data, "a"), filepath.Join(data, "c")}
	assertDeny(t, carveOutAll(deny), deny...)
}

func TestCarveOutAllAKeepEqualToADenyPathIsAllowed(t *testing.T) {
	data := t.TempDir()
	mkTree(t, data, "a/inner", "b")
	a, b := filepath.Join(data, "a"), filepath.Join(data, "b")

	assertDeny(t, carveOutAll([]string{a, b}, a), b)
	// carveOut, in contrast, keeps denying a path equal to the keep.
	assertDeny(t, carveOut([]string{a, b}, a), a, b)
}

func TestCarveOutAllADenyDirThatDoesNotExistIsDeniedWhole(t *testing.T) {
	data := t.TempDir()
	missing := filepath.Join(data, "missing")

	assertDeny(t, carveOutAll([]string{missing}, filepath.Join(missing, "keep")), missing)
	// carveOut, in contrast, returns nothing for it.
	assertDeny(t, carveOut([]string{missing}, filepath.Join(missing, "keep")))
}

func TestCarveOutAllMatchesLexicallyAndFailsClosed(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	mkTree(t, real, "homes/codex/sessions", "homes/codex/auth")
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	deny := []string{filepath.Join(real, "homes")}

	// Same spelling: carved.
	assertDeny(t, carveOutAll(deny, filepath.Join(real, "homes", "codex", "sessions")),
		filepath.Join(real, "homes", "codex", "auth"))
	// Another spelling of the same place: no match, so the directory stays
	// denied whole — the kept path is unreachable, never exposed beyond it.
	assertDeny(t, carveOutAll(deny, filepath.Join(alias, "homes", "codex", "sessions")), deny...)
}
