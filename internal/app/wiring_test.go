package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	gitservice "devkit/internal/git"
)

// The claim the design makes: one answer to "what changed". Code Graph's file
// tree reads the Git module rather than running a second git status, and it
// follows the open project without anyone remembering to tell it.
func TestGitWorkTreeReadsCodeGraphsRootWithoutMovingGit(t *testing.T) {
	ctx := context.Background()
	svc := gitservice.NewService(ctx)
	if !svc.Available() {
		t.Skip("no usable git on this machine")
	}

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(svc.Availability().Path, append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.x",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.x")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main", "--template=")
	run("config", "user.name", "T")
	run("config", "user.email", "t@e.x")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "first")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Git is pointed at another repository — the person pinned it there.
	// Code Graph's markers still come from Code Graph's root…
	pinned := t.TempDir()
	if out, err := exec.Command(svc.Availability().Path, "-C", pinned, "init", "--template=").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := svc.Open(ctx, pinned); err != nil {
		t.Fatal(err)
	}
	adapter := gitWorkTree{git: svc, roots: stubRoot(repo)}
	changes, err := adapter.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if changes["a.txt"] != "M" {
		t.Fatalf("changes = %v, want a.txt modified", changes)
	}
	// …and reading them does not drag Git back to it.
	if svc.Project() != pinned {
		t.Fatalf("reading Code Graph's changes moved git to %q, want it left on %q", svc.Project(), pinned)
	}
}

// A folder that is not a repository must draw no markers rather than break the
// file tree.
func TestGitWorkTreeOnANonRepositoryIsEmptyNotAnError(t *testing.T) {
	ctx := context.Background()
	svc := gitservice.NewService(ctx)
	if !svc.Available() {
		t.Skip("no usable git on this machine")
	}
	adapter := gitWorkTree{git: svc, roots: stubRoot(t.TempDir())}
	changes, err := adapter.Changes(ctx)
	if err != nil {
		t.Fatalf("a non-repository reported an error: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %v", changes)
	}
}

type stubRoot string

func (s stubRoot) ProjectRoot() string { return string(s) }
