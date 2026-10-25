package bridge

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"devkit/internal/app"
	"devkit/internal/git"
)

func TestGitCreateGuardUsesCanonicalRepositoryPathThroughSymlink(t *testing.T) {
	service, container := newBridgeWithAccounts(t)
	container.GitBindings = app.NewGitBindings(container.Items)
	container.Git = git.NewService(context.Background())
	if !container.Git.Available() {
		t.Skip("no usable Git")
	}
	container.Git.SetGuards(container.GitBindings)

	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "repo-link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(container.Git.Availability().Path, append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	run("init", "--initial-branch=main", "--template=")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "tracked.txt")
	run("commit", "-m", "base")
	if err := container.Git.Open(context.Background(), link); err != nil {
		t.Fatal(err)
	}
	policyKey := container.Git.PolicyProject()
	if policyKey == link {
		t.Fatalf("test symlink did not produce a distinct canonical key: %q", policyKey)
	}

	guard, err := service.GitCreateGuard("main", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if guard.ProjectKey != policyKey {
		t.Fatalf("saved guard project key = %q, want canonical %q", guard.ProjectKey, policyKey)
	}
	guards, err := service.GitGuards()
	if err != nil || len(guards) != 1 || guards[0].ID != guard.ID {
		t.Fatalf("guards after save = %+v, %v; want saved guard", guards, err)
	}

	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := container.Git.Stage(context.Background(), []string{"tracked.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := container.Git.Commit(context.Background(), git.CommitInput{Message: "must be guarded"}); !errors.Is(err, git.ErrNotAllowed) {
		t.Fatalf("commit via symlinked repo = %v, want ErrNotAllowed", err)
	}
}
