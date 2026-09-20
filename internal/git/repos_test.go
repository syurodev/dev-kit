package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
}

func newServiceOrSkip(t *testing.T) *Service {
	t.Helper()
	s := NewService(context.Background())
	if !s.Available() {
		t.Skip("no usable git on this machine")
	}
	return s
}

func TestRepositoriesFindsTheReposUnderAFolderOfServices(t *testing.T) {
	s := newServiceOrSkip(t)
	root := t.TempDir()
	initRepo(t, filepath.Join(root, "billing"))
	initRepo(t, filepath.Join(root, "clients", "web"))
	// Not worth listing: a plain folder, a vendored copy, a hidden folder.
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	initRepo(t, filepath.Join(root, "node_modules", "dep"))
	initRepo(t, filepath.Join(root, ".cache", "thing"))
	// One with work in it.
	if err := os.WriteFile(filepath.Join(root, "billing", "new.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	repos, err := s.Repositories(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("found %d repositories, want 2: %+v", len(repos), repos)
	}
	if repos[0].Name != "billing" || repos[1].Name != "clients/web" {
		t.Fatalf("names = %q, %q", repos[0].Name, repos[1].Name)
	}
	if repos[0].Changed != 1 || repos[1].Changed != 0 {
		t.Fatalf("changed = %d, %d, want 1, 0", repos[0].Changed, repos[1].Changed)
	}
	if repos[0].Branch == "" || repos[0].Error != "" {
		t.Fatalf("billing was not read: %+v", repos[0])
	}
}

func TestRepositoriesOfARepoIsThatRepoAlone(t *testing.T) {
	s := newServiceOrSkip(t)
	root := t.TempDir()
	initRepo(t, root)
	// A nested repo belongs to the folder it sits in; this is the answer Open gives.
	initRepo(t, filepath.Join(root, "inner"))

	repos, err := s.Repositories(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Path != root || repos[0].Name != filepath.Base(root) {
		t.Fatalf("repos = %+v", repos)
	}
}

func TestRepositoriesWithNoneIsEmptyAndRefusesARelativeFolder(t *testing.T) {
	s := newServiceOrSkip(t)
	repos, err := s.Repositories(t.Context(), t.TempDir())
	if err != nil || len(repos) != 0 {
		t.Fatalf("repos = %+v, err = %v", repos, err)
	}
	if _, err := s.Repositories(t.Context(), "relative/dir"); err == nil {
		t.Fatal("a relative folder was searched")
	}
}
