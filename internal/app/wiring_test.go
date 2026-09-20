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
func TestGitWorkTreeFollowsTheOpenProject(t *testing.T) {
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

	// Nothing has opened the project on the Git service: the adapter does it.
	adapter := gitWorkTree{git: svc, roots: stubRoot(repo)}
	changes, err := adapter.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if changes["a.txt"] != "M" {
		t.Fatalf("changes = %v, want a.txt modified", changes)
	}
	if svc.Project() != repo {
		t.Fatalf("the adapter did not point git at the open project: %q", svc.Project())
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
