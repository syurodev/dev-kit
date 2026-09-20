package app

import (
	"os/exec"
	"testing"

	"devkit/internal/workfolder"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	return dir
}

func TestGitFollowsTheWorkFolder(t *testing.T) {
	c := NewContainer()
	if !c.Git.Available() {
		t.Skip("no usable git on this machine")
	}
	repoA, repoB, plain := gitRepo(t), gitRepo(t), t.TempDir()
	WireWorkFolder(c)

	c.WorkFolder.SetCurrent(repoA)
	if got := c.Git.Project(); got != repoA {
		t.Fatalf("git project = %q, want it to follow to %q", got, repoA)
	}

	// A folder that is not a repository must not leave the last one showing.
	c.WorkFolder.SetCurrent(plain)
	if got := c.Git.Project(); got != "" {
		t.Fatalf("git project = %q in a folder that is no repository, want none", got)
	}

	c.WorkFolder.SetCurrent(repoA)
	if err := c.WorkFolder.Pin(workfolder.ModuleGit, repoB); err != nil {
		t.Fatal(err)
	}
	c.WorkFolder.SetCurrent(plain)
	if got := c.Git.Project(); got != repoB {
		t.Fatalf("pinned git project = %q, want it to stay on %q", got, repoB)
	}

	c.WorkFolder.Follow(workfolder.ModuleGit)
	if got := c.Git.Project(); got != "" {
		t.Fatalf("git project after following into a plain folder = %q, want none", got)
	}
}

// The last project is restored during startup, which can run before anything
// has subscribed.
func TestWireWorkFolderAppliesTheFolderAlreadyOpen(t *testing.T) {
	c := NewContainer()
	if !c.Git.Available() {
		t.Skip("no usable git on this machine")
	}
	repo := gitRepo(t)
	c.WorkFolder.SetCurrent(repo)

	WireWorkFolder(c)

	if got := c.Git.Project(); got != repo {
		t.Fatalf("git project = %q, want the folder that was already open: %q", got, repo)
	}
}
