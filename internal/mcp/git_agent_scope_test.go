package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	gitservice "devkit/internal/git"
)

// The directory-swap probes of the independent review (renaming a granted folder
// for a link after gitFor opened it, and the RENAME_EXCHANGE race) are in
// git_dirswap_test.go: they are what descriptor-bound execution (steps 3 and 4 of the
// report's design 3.5) answers. The two weaknesses below are steps 1 and 2.

func probeGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func probeRepo(t *testing.T, dir, subject string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	probeGit(t, dir, "init", "-q")
	probeGit(t, dir, "commit", "-q", "--allow-empty", "-m", subject)
}

// A granted repository's own `core.fsmonitor` must not run in the host's git when the
// git tool reads status. (A harmless `touch` of a marker file.)
func TestAgentGitDoesNotRunTheRepositorysFsmonitor(t *testing.T) {
	svc := gitservice.NewService(context.Background())
	if !svc.Available() {
		t.Skip("no git")
	}
	srv, root, _ := linkedScopeServer(t, GitReposOf(svc))
	pkg := filepath.Join(root, "pkg")
	probeRepo(t, pkg, "GRANTED-REPO")
	marker := filepath.Join(t.TempDir(), "ran")
	probeGit(t, pkg, "config", "core.fsmonitor", "touch "+marker+"; true")
	api, _, err := srv.gitFor(agentCtx(), pkg, false)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = api.Status(context.Background())
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("HOST EXEC: the repository's core.fsmonitor command ran in the host's git process")
	}
}

// A directory inside the granted folder whose `.git` is a FILE
// (`gitdir: <other repository>/.git`) is a work tree of the other repository to Git.
// The Toplevel check passes (the work tree root is the granted directory); the
// handle must be refused.
func TestAgentGitRefusesAGitdirFileThatLeavesTheScope(t *testing.T) {
	svc := gitservice.NewService(context.Background())
	if !svc.Available() {
		t.Skip("no git")
	}
	srv, root, outside := linkedScopeServer(t, GitReposOf(svc))
	probeRepo(t, outside, "VICTIM-REPO-OUTSIDE-THE-GRANT")
	pkg := filepath.Join(root, "pkg")
	if err := os.WriteFile(filepath.Join(pkg, ".git"), []byte("gitdir: "+filepath.Join(outside, ".git")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	api, _, err := srv.gitFor(agentCtx(), pkg, false)
	if err != nil {
		t.Logf("refused at open: %v", err)
		return
	}
	commits, err := api.Log(context.Background(), 1)
	if err != nil || len(commits) != 1 {
		t.Fatalf("log: %v %v", commits, err)
	}
	t.Logf("handle for %s answers from: %q", pkg, commits[0].Subject)
	if strings.Contains(commits[0].Subject, "VICTIM") {
		t.Errorf("GITDIR FILE: a granted folder redirected to a repository outside the grant, no race needed")
	}
}
