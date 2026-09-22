package workspace

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mkGitDir(t *testing.T, repoPath string) {
	t.Helper()
	mkdir(t, filepath.Join(repoPath, ".git"))
}

func mkGitWorktreeFile(t *testing.T, repoPath string) {
	t.Helper()
	mkdir(t, repoPath)
	if err := os.WriteFile(filepath.Join(repoPath, ".git"), []byte("gitdir: /primary/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatalf("write worktree .git file: %v", err)
	}
}

func names(services []DiscoveredService) []string {
	out := make([]string, len(services))
	for i, s := range services {
		out[i] = s.Name
	}
	sort.Strings(out)
	return out
}

func TestDiscoverServices_MultipleRepos(t *testing.T) {
	root := t.TempDir()
	mkGitDir(t, filepath.Join(root, "billing-service"))
	mkGitDir(t, filepath.Join(root, "refund-service"))
	mkdir(t, filepath.Join(root, "docs")) // no .git — not a service

	got, err := DiscoverServices(root)
	if err != nil {
		t.Fatalf("DiscoverServices: %v", err)
	}
	if want := []string{"billing-service", "refund-service"}; !equal(names(got), want) {
		t.Fatalf("names = %v, want %v", names(got), want)
	}
	for _, s := range got {
		if s.Path != filepath.Join(root, s.Name) {
			t.Errorf("service %q path = %s, want under root", s.Name, s.Path)
		}
	}
}

func TestDiscoverServices_WorktreeGitFile(t *testing.T) {
	root := t.TempDir()
	mkGitWorktreeFile(t, filepath.Join(root, "billing-service"))

	got, err := DiscoverServices(root)
	if err != nil {
		t.Fatalf("DiscoverServices: %v", err)
	}
	if want := []string{"billing-service"}; !equal(names(got), want) {
		t.Fatalf("names = %v, want %v", names(got), want)
	}
}

func TestDiscoverServices_NoNestedRepos_FallsBackToRoot(t *testing.T) {
	root := t.TempDir()
	// A plain folder of source files, no subdirectory holds its own .git —
	// including the case where root ITSELF is the repo (its .git is at
	// root, not one level down, so the depth-1 scan still finds nothing).
	mkGitDir(t, root)
	mkdir(t, filepath.Join(root, "src"))

	got, err := DiscoverServices(root)
	if err != nil {
		t.Fatalf("DiscoverServices: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d services, want 1 (root itself): %v", len(got), got)
	}
	if got[0].Path != root {
		t.Errorf("service path = %s, want root %s", got[0].Path, root)
	}
	if got[0].Name != filepath.Base(root) {
		t.Errorf("service name = %s, want %s", got[0].Name, filepath.Base(root))
	}
}

func TestDiscoverServices_EmptyFolder_FallsBackToRoot(t *testing.T) {
	root := t.TempDir()

	got, err := DiscoverServices(root)
	if err != nil {
		t.Fatalf("DiscoverServices: %v", err)
	}
	if len(got) != 1 || got[0].Path != root {
		t.Fatalf("got %v, want root itself as the single service", got)
	}
}

func TestDiscoverServices_IgnoresFilesAtRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mkGitDir(t, filepath.Join(root, "api"))

	got, err := DiscoverServices(root)
	if err != nil {
		t.Fatalf("DiscoverServices: %v", err)
	}
	if want := []string{"api"}; !equal(names(got), want) {
		t.Fatalf("names = %v, want %v", names(got), want)
	}
}

func TestDiscoverServices_MissingRoot(t *testing.T) {
	if _, err := DiscoverServices(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error for a root that does not exist")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
