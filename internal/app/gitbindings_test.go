package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	gitservice "devkit/internal/git"
	"devkit/internal/vault"
	"devkit/internal/vaultitem"
)

func newBindings(t *testing.T) (*GitBindings, *vaultitem.Store) {
	t.Helper()
	v := vault.NewService(vault.NewMemoryKeyringStore())
	if _, err := v.Setup("correct-horse"); err != nil {
		t.Fatal(err)
	}
	store := vaultitem.NewStore(vaultitem.NewMemoryRepository(), v)
	return NewGitBindings(store), store
}

func TestGuardsAreScopedToTheirProject(t *testing.T) {
	bindings, _ := newBindings(t)
	if _, err := bindings.CreateGuard("/a", "main", "prod"); err != nil {
		t.Fatal(err)
	}
	if _, err := bindings.CreateGuard("/b", "main", "stg"); err != nil {
		t.Fatal(err)
	}
	guards, err := bindings.ListGuards("/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(guards) != 1 || guards[0].Env != "prod" {
		t.Fatalf("guards for /a = %+v; another project's guard leaked", guards)
	}
	if none, _ := bindings.ListGuards("/never-opened"); len(none) != 0 {
		t.Fatalf("a project with no guards got %+v", none)
	}
}

// An exact branch name must beat a glob that also covers it, or the broader
// rule would decide and the specific one would never apply.
func TestTheMoreSpecificGuardWins(t *testing.T) {
	bindings, _ := newBindings(t)
	if _, err := bindings.CreateGuard("/p", "release/*", "stg"); err != nil {
		t.Fatal(err)
	}
	if _, err := bindings.CreateGuard("/p", "release/2026-09", "prod"); err != nil {
		t.Fatal(err)
	}
	guards, err := bindings.ListGuards("/p")
	if err != nil {
		t.Fatal(err)
	}
	if len(guards) != 2 {
		t.Fatalf("guards = %+v", guards)
	}
	if !guards[0].Matches("release/2026-09") || guards[0].Env != "prod" {
		t.Fatalf("first match for release/2026-09 = %+v, want the exact one", guards[0])
	}
}

// One binding per remote: two would make "which account authenticates to
// origin" ambiguous.
func TestBindingARemoteTwiceReplacesRatherThanAdds(t *testing.T) {
	bindings, _ := newBindings(t)
	if _, err := bindings.BindRemote("/p", "origin", "acct-1", "prod"); err != nil {
		t.Fatal(err)
	}
	if _, err := bindings.BindRemote("/p", "origin", "acct-2", "stg"); err != nil {
		t.Fatal(err)
	}
	remotes, err := bindings.ListRemotes("/p")
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 1 || remotes[0].AccountID != "acct-2" || remotes[0].Env != "stg" {
		t.Fatalf("remotes = %+v", remotes)
	}
}

func TestRemoteBindingAnswersAccountUsage(t *testing.T) {
	bindings, _ := newBindings(t)
	if _, err := bindings.BindRemote("/p", "origin", "acct-1", "prod"); err != nil {
		t.Fatal(err)
	}
	used := bindings.UsedBy("acct-1")
	if len(used) != 1 || used[0] != "git remote origin" {
		t.Fatalf("UsedBy = %v; deleting the account would not say what needs it", used)
	}
	if len(bindings.UsedBy("acct-other")) != 0 {
		t.Fatal("an unrelated account reported usage")
	}
}

func TestGuardEnvBlocksDeletingItsTag(t *testing.T) {
	bindings, store := newBindings(t)
	if _, err := bindings.CreateGuard("/p", "main", "prod"); err != nil {
		t.Fatal(err)
	}
	usage := newVaultItemEnvUsage(store)
	inUse, err := usage.EnvInUse("prod")
	if err != nil || !inUse {
		t.Fatalf("EnvInUse(prod) = %v, %v; a guard holds it", inUse, err)
	}
	if inUse, err := usage.EnvInUse("dev"); err != nil || inUse {
		t.Fatalf("EnvInUse(dev) = %v, %v", inUse, err)
	}
}

// The gap this whole file exists to close: with the store wired, a guard
// actually gates a commit. Before it, GuardStore was nil in production and
// commit gating silently never fired.
func TestAWiredGuardActuallyGatesACommit(t *testing.T) {
	ctx := context.Background()
	git := gitservice.NewService(ctx)
	if !git.Available() {
		t.Skip("no usable git on this machine")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(git.Availability().Path, append([]string{"-C", repo}, args...)...)
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
	if err := git.Open(ctx, repo); err != nil {
		t.Fatal(err)
	}

	bindings, _ := newBindings(t)
	git.SetGuards(bindings)
	git.SetEnvs(confirmingEnvs{"prod": true})
	// No confirmer: headless, which a guarded branch must treat as a refusal.
	if _, err := bindings.CreateGuard(repo, "main", "prod"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := git.Stage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := git.Commit(ctx, gitservice.CommitInput{Message: "on a guarded branch"}); !errors.Is(err, gitservice.ErrNotAllowed) {
		t.Fatalf("commit on a guarded branch = %v, want ErrNotAllowed", err)
	}

	// An unguarded branch is untouched: the default is not to be in the way.
	if _, err := git.CreateBranch(ctx, "feature", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := git.Commit(ctx, gitservice.CommitInput{Message: "on an unguarded branch"}); err != nil {
		t.Fatalf("commit on an unguarded branch was blocked: %v", err)
	}
}

type confirmingEnvs map[string]bool

func (e confirmingEnvs) RequiresConfirmation(name string) bool { return e[name] }
