package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A bare repository on disk is a real remote as far as git is concerned: the
// same argv, the same refspecs, the same tracking configuration. What it does
// not exercise is authentication, which is why credentialEnv is tested
// directly below rather than through a server that would have to be faked.
func newRepoWithRemote(t *testing.T) (svc *Service, work, bare string) {
	t.Helper()
	svc, work = newRepo(t)
	bare = filepath.Join(t.TempDir(), "origin.git")
	runGit(t, svc, "", "init", "--bare", "--initial-branch=main", bare)
	runGit(t, svc, work, "remote", "add", "origin", bare)
	return svc, work, bare
}

func runGit(t *testing.T, svc *Service, dir string, args ...string) string {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command(svc.availability.Path, args...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.x",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.x")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// stubRemotes is the binding store: what the composition root would have read
// out of the vault.
type stubRemotes []Remote

func (r stubRemotes) ListRemotes(string) ([]Remote, error) { return r, nil }

type stubCreds struct {
	user, secret string
	asked        int
}

func (c *stubCreds) ResolveForRemote(context.Context, string, string) (string, string, error) {
	c.asked++
	return c.user, c.secret, nil
}

func TestPushSendsTheBranchAndSetsUpstream(t *testing.T) {
	ctx := context.Background()
	svc, _, bare := newRepoWithRemote(t)

	if err := svc.Push(ctx, PushInput{Remote: "origin", Branch: "main", SetUpstream: true}); err != nil {
		t.Fatal(err)
	}
	if refs := runGit(t, svc, bare, "for-each-ref", "--format=%(refname)"); !strings.Contains(refs, "refs/heads/main") {
		t.Fatalf("the remote has no main after a push: %q", refs)
	}
	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Upstream != "origin/main" {
		t.Fatalf("upstream after -u = %q", status.Upstream)
	}
	if status.Ahead != 0 || status.Behind != 0 {
		t.Fatalf("ahead/behind after a push = %d/%d", status.Ahead, status.Behind)
	}
}

// Push is the gated action, always: it leaves the machine and cannot be
// undone. A refused confirmation must leave the remote untouched, not push and
// then complain.
func TestPushOnAGatedRemoteAsksAndARefusalSendsNothing(t *testing.T) {
	ctx := context.Background()
	svc, _, bare := newRepoWithRemote(t)
	svc.SetRemotes(stubRemotes{{Name: "origin", Env: "prod"}})
	svc.SetEnvs(stubEnvs{"prod": true})

	refused := &stubConfirm{allow: false}
	svc.SetConfirmer(refused)
	if err := svc.Push(ctx, PushInput{Remote: "origin", Branch: "main"}); err != ErrNotAllowed {
		t.Fatalf("a refused push = %v, want ErrNotAllowed", err)
	}
	if refused.asked != 1 {
		t.Fatalf("asked %d times, want 1", refused.asked)
	}
	if refs := runGit(t, svc, bare, "for-each-ref", "--format=%(refname)"); strings.Contains(refs, "refs/heads/main") {
		t.Fatal("a refused push reached the remote anyway")
	}

	allowed := &stubConfirm{allow: true}
	svc.SetConfirmer(allowed)
	if err := svc.Push(ctx, PushInput{Remote: "origin", Branch: "main"}); err != nil {
		t.Fatal(err)
	}
	if refs := runGit(t, svc, bare, "for-each-ref", "--format=%(refname)"); !strings.Contains(refs, "refs/heads/main") {
		t.Fatal("an allowed push did not reach the remote")
	}
}

// Headless is a refusal, not a silent allow: with nothing that can ask, a
// gated push must not go out.
func TestGatedPushFailsClosedWithoutAConfirmer(t *testing.T) {
	svc, _, bare := newRepoWithRemote(t)
	svc.SetRemotes(stubRemotes{{Name: "origin", Env: "prod"}})
	svc.SetEnvs(stubEnvs{"prod": true})

	if err := svc.Push(context.Background(), PushInput{Remote: "origin", Branch: "main"}); err != ErrNotAllowed {
		t.Fatalf("a headless gated push = %v, want ErrNotAllowed", err)
	}
	if refs := runGit(t, svc, bare, "for-each-ref", "--format=%(refname)"); strings.Contains(refs, "refs/heads/main") {
		t.Fatal("a headless gated push reached the remote")
	}
}

// Pull is --ff-only on purpose: a merge started behind the user's back is how
// a conflict appears with nobody expecting one.
func TestFetchAndPullBringTheOtherSideDown(t *testing.T) {
	ctx := context.Background()
	svc, work, bare := newRepoWithRemote(t)
	if err := svc.Push(ctx, PushInput{Remote: "origin", Branch: "main", SetUpstream: true}); err != nil {
		t.Fatal(err)
	}

	// Someone else pushes to the same remote.
	other := filepath.Join(t.TempDir(), "other")
	runGit(t, svc, "", "clone", bare, other)
	runGit(t, svc, other, "config", "user.name", "Other")
	runGit(t, svc, other, "config", "user.email", "other@example.com")
	if err := os.WriteFile(filepath.Join(other, "theirs.txt"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, svc, other, "add", ".")
	runGit(t, svc, other, "commit", "-m", "from the other side")
	runGit(t, svc, other, "push", "origin", "main")

	if err := svc.Fetch(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Behind != 1 {
		t.Fatalf("behind after a fetch = %d, want 1", status.Behind)
	}

	if err := svc.Pull(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "theirs.txt")); err != nil {
		t.Fatalf("pull did not bring the other side's file down: %v", err)
	}
}

// The credential never reaches argv or the URL.
//
// Both are visible in the process list to every other user on the machine, and
// the URL would also be written into .git/config and end up in anyone's shell
// history. It travels as environment, read back by a helper git calls — so the
// helper itself holds no secret either, and is the same three lines whatever
// the credential is.
func TestCredentialTravelsAsEnvironmentAndNeverAsArgvOrURL(t *testing.T) {
	ctx := context.Background()
	svc, work, _ := newRepoWithRemote(t)
	const secret = "gho-do-not-put-me-in-argv"
	creds := &stubCreds{user: "octocat", secret: secret}
	svc.SetRemotes(stubRemotes{{Name: "origin", AccountID: "acct-1", Env: "prod"}})
	svc.SetCredentials(creds)

	env, cleanup, err := svc.credentialEnv(ctx, "origin")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if creds.asked != 1 {
		t.Fatalf("resolved %d times, want 1", creds.asked)
	}

	var askpass string
	found := map[string]bool{}
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		found[name] = true
		if name == "GIT_ASKPASS" {
			askpass = value
		}
	}
	for _, want := range []string{"GIT_ASKPASS", "GIT_TERMINAL_PROMPT", "DEVKIT_GIT_USER", "DEVKIT_GIT_SECRET"} {
		if !found[want] {
			t.Fatalf("%s missing from the credential environment", want)
		}
	}

	// The helper carries no secret of its own: it echoes the environment.
	body, err := os.ReadFile(askpass)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secret) {
		t.Fatal("the askpass script has the secret written into it")
	}
	info, err := os.Stat(askpass)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("askpass mode = %v, want 0700", info.Mode().Perm())
	}

	// git asks by running it with the prompt as $1.
	for prompt, want := range map[string]string{
		"Username for 'https://github.com': ": "octocat",
		"Password for 'https://github.com': ": secret,
	} {
		cmd := exec.Command(askpass, prompt)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("askpass %q: %v", prompt, err)
		}
		if string(out) != want {
			t.Fatalf("askpass %q = %q, want %q", prompt, out, want)
		}
	}

	// Nothing was written into the repository's own configuration.
	config, err := os.ReadFile(filepath.Join(work, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), secret) || strings.Contains(string(config), "octocat") {
		t.Fatalf("the credential landed in .git/config:\n%s", config)
	}

	cleanup()
	if _, err := os.Stat(askpass); !os.IsNotExist(err) {
		t.Fatalf("the askpass helper outlived the command: %v", err)
	}
}

// An unbound remote resolves nothing: git's own configuration — ssh keys, a
// credential helper — is what authenticates, and DevKit must not get in front
// of it.
func TestAnUnboundRemoteResolvesNoCredential(t *testing.T) {
	svc, _, _ := newRepoWithRemote(t)
	creds := &stubCreds{user: "u", secret: "s"}
	svc.SetRemotes(stubRemotes{{Name: "origin"}})
	svc.SetCredentials(creds)

	env, cleanup, err := svc.credentialEnv(context.Background(), "origin")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(env) != 0 {
		t.Fatalf("an unbound remote produced an environment: %v", env)
	}
	if creds.asked != 0 {
		t.Fatalf("an unbound remote resolved a credential %d times", creds.asked)
	}
}

// Remotes merges what git knows with what the vault stores; a binding for a
// remote git does not have must not invent one.
func TestRemotesMergesBindingsOntoWhatGitReports(t *testing.T) {
	svc, _, _ := newRepoWithRemote(t)
	svc.SetRemotes(stubRemotes{
		{Name: "origin", AccountID: "acct-1", Env: "prod"},
		{Name: "gone", AccountID: "acct-2", Env: "dev"},
	})
	remotes, err := svc.Remotes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(remotes) != 1 {
		t.Fatalf("remotes = %+v, want only the one git has", remotes)
	}
	if remotes[0].Name != "origin" || remotes[0].AccountID != "acct-1" || remotes[0].Env != "prod" {
		t.Fatalf("binding was not merged onto the remote: %+v", remotes[0])
	}
	if remotes[0].URL == "" {
		t.Fatal("the URL came from git and must survive the merge")
	}
}
