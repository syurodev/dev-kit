package git

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"devkit/internal/identity"
)

func agentCtx() context.Context {
	return identity.WithCaller(context.Background(), identity.Agent("11111111-2222-3333-4444-555555555555"))
}

// agentAt opens the repository the way an agent-origin call does.
func agentAt(t *testing.T, svc *Service, dir string) *Service {
	t.Helper()
	agent, err := svc.At(agentCtx(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return agent
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func markerIn(t *testing.T) string { return filepath.Join(t.TempDir(), "ran") }

func writeHook(t *testing.T, dir, name, marker string) {
	t.Helper()
	path := filepath.Join(dir, ".git", "hooks", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts as hooks and filters")
	}
}

// A hook in the repository's own .git runs in the person's commit and not in an
// agent's: the agent can write that file, the host's git would run it.
func TestAnAgentCommitDoesNotRunTheRepositorysHooks(t *testing.T) {
	skipOnWindows(t)
	svc, dir := newRepo(t)
	marker := markerIn(t)
	for _, hook := range []string{"pre-commit", "commit-msg", "post-commit"} {
		writeHook(t, dir, hook, marker)
	}
	agent := agentAt(t, svc, dir)
	write(t, dir, "a.txt", "a\n")
	if err := agent.Stage(agentCtx(), []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Commit(agentCtx(), CommitInput{Message: "by an agent", ByAgent: true}); err != nil {
		t.Fatal(err)
	}
	if exists(marker) {
		t.Fatal("a hook of the repository ran in an agent's commit")
	}

	// The person's own commit is what it was.
	write(t, dir, "b.txt", "b\n")
	if err := svc.Stage(context.Background(), []string{"b.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(context.Background(), CommitInput{Message: "by the person"}); err != nil {
		t.Fatal(err)
	}
	if !exists(marker) {
		t.Fatal("the person's commit no longer runs the repository's hooks")
	}
}

// A handle opened by a person is never hardened by what it is called with later;
// one opened by an agent stays hardened when a caller passes a plain context
// (the review's probe did), and a person's handle called with an agent's context
// is hardened for that call.
func TestHardeningFollowsTheHandleAndTheCallerOfTheCall(t *testing.T) {
	skipOnWindows(t)
	svc, dir := newRepo(t)
	run := func(s *Service, ctx context.Context) bool {
		marker := markerIn(t)
		runGit(t, svc, dir, "config", "core.fsmonitor", "touch '"+marker+"'; true")
		_, _ = s.Status(ctx)
		return exists(marker)
	}
	if !run(svc, context.Background()) {
		t.Fatal("control: the person's status no longer runs the repository's core.fsmonitor")
	}
	if run(agentAt(t, svc, dir), context.Background()) {
		t.Error("an agent's handle ran core.fsmonitor when called with a plain context")
	}
	if run(svc, agentCtx()) {
		t.Error("a person's handle ran core.fsmonitor for a call that carries an agent")
	}
}

// Filters, diff and merge drivers are named by the repository and run by the host's
// git; the agent chooses the names, so they are read from the configuration.
func TestAnAgentDoesNotRunFiltersTextconvExternalDiffOrMergeDrivers(t *testing.T) {
	skipOnWindows(t)
	svc, dir := newRepo(t)
	marker := markerIn(t)
	touch := "touch '" + marker + "'; cat"
	write(t, dir, ".gitattributes", "*.txt filter=evil diff=evil merge=evil\n")
	runGit(t, svc, dir, "add", ".gitattributes")
	runGit(t, svc, dir, "commit", "-m", "attributes")
	write(t, dir, "a.txt", "base\n")
	runGit(t, svc, dir, "add", "a.txt")
	runGit(t, svc, dir, "commit", "-m", "a")
	for key, value := range map[string]string{
		"filter.evil.clean":    touch,
		"filter.evil.smudge":   touch,
		"filter.evil.process":  "touch '" + marker + "'; false",
		"filter.evil.required": "true",
		"diff.evil.textconv":   touch,
		"diff.evil.command":    "touch '" + marker + "'; true",
		"diff.external":        "touch '" + marker + "'; true",
		"merge.evil.driver":    "touch '" + marker + "'; cp %B %A; true",
		"credential.helper":    "!touch '" + marker + "'; true",
	} {
		runGit(t, svc, dir, "config", key, value)
	}
	agent := agentAt(t, svc, dir)
	ctx := agentCtx()
	// The test's own further steps go through the agent's handle as well: a plain
	// git here would run the filters, which is what the person's git does.
	ag := func(args ...string) string {
		t.Helper()
		out, err := agent.runner.run(ctx, nil, args...)
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return string(out)
	}

	write(t, dir, "a.txt", "changed\n")
	if _, err := agent.Status(ctx); err != nil {
		t.Fatal(err)
	}
	if err := agent.Stage(ctx, []string{"a.txt"}); err != nil {
		t.Fatalf("stage through a neutralised filter: %v", err)
	}
	if _, err := agent.Commit(ctx, CommitInput{Message: "agent", ByAgent: true}); err != nil {
		t.Fatalf("commit through a neutralised filter: %v", err)
	}
	// What an agent's commit stores is the file as it is, not whatever a filter made of it.
	if got := ag("show", "HEAD:a.txt"); got != "changed\n" {
		t.Fatalf("committed %q", got)
	}
	// A plain `git diff` and `git log -p`, with no --no-ext-diff or --no-textconv.
	write(t, dir, "a.txt", "changed again\n")
	for _, args := range [][]string{{"diff"}, {"log", "-p", "-n", "2"}, {"show", "HEAD"}} {
		if _, err := agent.runner.run(ctx, nil, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	// A merge driver that would report a clean merge is a conflict, never a quiet success.
	ag("checkout", "--", "a.txt")
	ag("checkout", "-q", "-b", "other", "HEAD~1")
	write(t, dir, "a.txt", "from other\n")
	ag("add", "a.txt")
	ag("commit", "-m", "other")
	ag("checkout", "-q", "main")
	ag("checkout", "--", "a.txt")
	if _, err := agent.runner.run(ctx, nil, "merge", "--no-edit", "other"); err == nil {
		t.Fatal("a merge through a neutralised driver reported success")
	}
	if exists(marker) {
		t.Fatal("a program named by the repository's configuration ran for an agent")
	}
}

// What the repository's own program options would run at the other end of a
// remote, and what runs inside a local remote when it receives the push.
func TestAnAgentPushAndFetchDoNotRunWhatTheRepositoryNames(t *testing.T) {
	skipOnWindows(t)
	svc, dir, bare := newRepoWithRemote(t)
	markers := map[string]string{}
	for _, name := range []string{"receivepack", "uploadpack", "pre-push", "pre-receive", "post-receive"} {
		markers[name] = markerIn(t)
	}
	runGit(t, svc, dir, "config", "remote.origin.receivepack", "touch '"+markers["receivepack"]+"'; git-receive-pack")
	runGit(t, svc, dir, "config", "remote.origin.uploadpack", "touch '"+markers["uploadpack"]+"'; git-upload-pack")
	runGit(t, svc, dir, "config", "core.hooksPath", filepath.Join(dir, "evil-hooks"))
	writeHookAt(t, filepath.Join(dir, "evil-hooks"), "pre-push", markers["pre-push"])
	// The remote's own hooks run inside the receiving git, in this process tree.
	writeHookAt(t, filepath.Join(bare, "hooks"), "pre-receive", markers["pre-receive"])
	writeHookAt(t, filepath.Join(bare, "hooks"), "post-receive", markers["post-receive"])

	agent := agentAt(t, svc, dir)
	ctx := agentCtx()
	if err := agent.Push(ctx, PushInput{Remote: "origin", Branch: "main", SetUpstream: true}); err != nil {
		t.Fatalf("a push by an agent: %v", err)
	}
	if refs := runGit(t, svc, bare, "for-each-ref", "--format=%(refname)"); !strings.Contains(refs, "refs/heads/main") {
		t.Fatalf("the push did not arrive: %q", refs)
	}
	if err := agent.Fetch(ctx, "origin"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	for name, marker := range markers {
		if exists(marker) {
			t.Errorf("%s ran in an agent's push or fetch", name)
		}
	}
}

func writeHookAt(t *testing.T, dir, name, marker string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// An SSH remote with no account bound still pushes: git runs `ssh` from PATH, as it
// would with nothing configured, and not what core.sshCommand of the repository says.
func TestAnAgentPushesOverSSHWithoutTheRepositorysSSHCommand(t *testing.T) {
	skipOnWindows(t)
	svc, dir, bare := newRepoWithRemote(t)
	marker := markerIn(t)
	bin := t.TempDir()
	// ssh [options] host command: run the command here, as the remote would.
	script := "#!/bin/sh\nfor last; do :; done\nexec sh -c \"$last\"\n"
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, name := range []string{"GIT_SSH_COMMAND", "GIT_SSH"} {
		t.Setenv(name, "") // restores the old value afterwards
		os.Unsetenv(name)  // git reads an empty one as a program to run
	}
	runGit(t, svc, dir, "remote", "set-url", "origin", "ssh://host"+bare)
	runGit(t, svc, dir, "config", "core.sshCommand", "touch '"+marker+"'; false")

	agent := agentAt(t, svc, dir)
	if err := agent.Push(agentCtx(), PushInput{Remote: "origin", Branch: "main"}); err != nil {
		t.Fatalf("an ssh push by an agent without a bound account: %v", err)
	}
	if refs := runGit(t, svc, bare, "for-each-ref", "--format=%(refname)"); !strings.Contains(refs, "refs/heads/main") {
		t.Fatalf("the push did not arrive: %q", refs)
	}
	if exists(marker) {
		t.Fatal("the repository's core.sshCommand ran in an agent's push")
	}
}

func lastEnv(env []string, name string) (string, bool) {
	value, ok := "", false
	for _, entry := range env {
		if key, v, _ := strings.Cut(entry, "="); key == name {
			value, ok = v, true
		}
	}
	return value, ok
}

// What the host decides for a command (an account's askpass for an https remote, a
// key's ssh command, the committer identity) reaches git after the hardening, so a
// push to an https or ssh remote with a bound account is what it was.
func TestTheHostsOwnCredentialEnvironmentSurvivesHardening(t *testing.T) {
	t.Setenv("GIT_EXTERNAL_DIFF", "/host/external-diff")
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	t.Setenv("GIT_WORK_TREE", "/elsewhere")
	t.Setenv("SSH_ASKPASS", "/host/ssh-askpass")
	t.Setenv("GIT_ASKPASS", "/host/askpass")
	r := runner{git: "git", work: t.TempDir(), hardened: true, gitDir: "/checked/.git", workTree: "/checked"}
	host := []string{"GIT_ASKPASS=/devkit/askpass.sh", "DEVKIT_GIT_SECRET=s3", "GIT_SSH_COMMAND=ssh -o IdentityAgent=/sock", "GIT_COMMITTER_NAME=Acct"}
	cmd, _ := r.command(context.Background(), host, "push", "origin", "main")
	for name, want := range map[string]string{
		"GIT_ASKPASS":         "/devkit/askpass.sh",
		"DEVKIT_GIT_SECRET":   "s3",
		"GIT_SSH_COMMAND":     "ssh -o IdentityAgent=/sock",
		"GIT_COMMITTER_NAME":  "Acct",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_DIR":             "/checked/.git",
		"GIT_WORK_TREE":       "/checked",
		"SSH_ASKPASS":         "",
	} {
		if got, ok := lastEnv(cmd.Env, name); !ok || got != want {
			t.Errorf("%s = %q (set %v), want %q", name, got, ok, want)
		}
	}
	if v, ok := lastEnv(cmd.Env, "GIT_EXTERNAL_DIFF"); ok {
		t.Errorf("GIT_EXTERNAL_DIFF = %q was inherited", v)
	}
	args := strings.Join(cmd.Args, " ")
	for _, want := range []string{"-c core.fsmonitor=false", "-c core.hooksPath=", "-c credential.helper=", "-c core.sshCommand=", "-c core.pager=cat"} {
		if !strings.Contains(args, want) {
			t.Errorf("argv lacks %q: %s", want, args)
		}
	}

	// The person's runner is what it was: nothing added, nothing removed.
	person, _ := runner{git: "git", work: r.work}.command(context.Background(), host, "push", "origin", "main")
	if v, _ := lastEnv(person.Env, "GIT_EXTERNAL_DIFF"); v != "/host/external-diff" {
		t.Errorf("the person's environment lost GIT_EXTERNAL_DIFF: %q", v)
	}
	if strings.Contains(strings.Join(person.Args, " "), "core.fsmonitor") {
		t.Errorf("the person's argv was hardened: %v", person.Args)
	}
}

// The names the agent gives its drivers are found in the configuration, and each is
// neutralised with a value that cannot do harm: a merge driver fails, an empty one
// would be a shell that succeeds.
func TestDriversTheRepositoryNamesAreNeutralisedByName(t *testing.T) {
	svc, dir := newRepo(t)
	for key, value := range map[string]string{
		"filter.lfs.clean":                 "git-lfs clean -- %f",
		"filter.lfs.smudge":                "git-lfs smudge -- %f",
		"filter.lfs.process":               "git-lfs filter-process",
		"filter.lfs.required":              "true",
		"diff.x.command":                   "evil",
		"diff.x.textconv":                  "evil",
		"merge.m.driver":                   "evil %O %A %B",
		"credential.https://h.test.helper": "evil",
		"diff.x.binary":                    "true",
		"user.name":                        "Test",
	} {
		runGit(t, svc, dir, "config", key, value)
	}
	r := runner{git: svc.availability.Path, work: dir, hardened: true}
	got := strings.Join(r.repositoryDrivers(context.Background()), " ")
	for _, want := range []string{
		"-c filter.lfs.clean=", "-c filter.lfs.smudge=", "-c filter.lfs.process=", "-c filter.lfs.required=false",
		"-c diff.x.command=true", "-c diff.x.textconv=cat", "-c merge.m.driver=false",
		"-c credential.https://h.test.helper=",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %s", want, got)
		}
	}
	if strings.Contains(got, "binary") || strings.Contains(got, "user.name") {
		t.Errorf("an unrelated key was overridden: %s", got)
	}
}

// git.repos lists the workspace's repositories with their state, for an agent too.
func TestSummariesOfAnAgentDoNotRunTheRepositorysFsmonitor(t *testing.T) {
	skipOnWindows(t)
	svc, dir := newRepo(t)
	marker := markerIn(t)
	runGit(t, svc, dir, "config", "core.fsmonitor", "touch '"+marker+"'; true")
	if _, err := svc.Summaries(agentCtx(), []string{dir}); err != nil {
		t.Fatal(err)
	}
	if exists(marker) {
		t.Fatal("core.fsmonitor ran for an agent's repository summary")
	}
	if _, err := svc.Summaries(context.Background(), []string{dir}); err != nil {
		t.Fatal(err)
	}
	if !exists(marker) {
		t.Fatal("control: the person's summary no longer reads the repository with its own configuration")
	}
}

// Once pinned, a handle goes on using the git directory that was checked even if the
// .git file of the checkout is rewritten to name another repository.
func TestAPinnedHandleIgnoresALaterRewriteOfTheGitFile(t *testing.T) {
	svc, dir := newRepo(t)
	runGit(t, svc, dir, "branch", "wt")
	worktree := filepath.Join(t.TempDir(), "wt")
	runGit(t, svc, dir, "worktree", "add", worktree, "wt")
	write(t, worktree, "w.txt", "w\n")
	runGit(t, svc, worktree, "add", "w.txt")
	runGit(t, svc, worktree, "commit", "-m", "IN-THE-WORKTREE")

	victim := t.TempDir()
	runGit(t, svc, victim, "init", "-q", "--initial-branch=main")
	runGit(t, svc, victim, "commit", "-q", "--allow-empty", "-m", "VICTIM")

	agent := agentAt(t, svc, worktree)
	gitDir, commonDir := agent.GitDirs()
	if gitDir == commonDir || !strings.HasSuffix(filepath.ToSlash(commonDir), "/.git") {
		t.Fatalf("not a linked worktree: %q %q", gitDir, commonDir)
	}
	top := agent.Toplevel()
	agent.Confine(gitDir, top)

	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+filepath.Join(victim, ".git")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commits, err := agent.Log(agentCtx(), 1)
	if err != nil || len(commits) != 1 {
		t.Fatalf("log: %v %v", commits, err)
	}
	if commits[0].Subject != "IN-THE-WORKTREE" {
		t.Fatalf("the pinned handle answered %q after the .git file was rewritten", commits[0].Subject)
	}

	// Not pinned, the same rewrite redirects it: this is what Confine is for.
	loose := agentAt(t, svc, worktree)
	commits, err = loose.Log(agentCtx(), 1)
	if err != nil || len(commits) != 1 || commits[0].Subject != "VICTIM" {
		t.Fatalf("control: an unpinned handle should follow the rewritten .git file: %v %v", commits, err)
	}

	// The person's handle is never pinned.
	person, err := svc.At(context.Background(), worktree)
	if err != nil {
		t.Fatal(err)
	}
	person.Confine(gitDir, top)
	if person.runner.gitDir != "" {
		t.Fatal("Confine pinned a handle that is not an agent's")
	}
}

// A worktree added through an agent's handle (the code-editing child's checkout) is
// created, listed and removed with the git directory pinned.
func TestAgentWorktreesWorkOnAPinnedHandle(t *testing.T) {
	svc, dir := newRepo(t)
	agent := agentAt(t, svc, dir)
	gitDir, _ := agent.GitDirs()
	agent.Confine(gitDir, agent.Toplevel())
	ctx := agentCtx()
	_, err := agent.WorktreeAdd(ctx, WorktreeAddInput{Branch: "child-work"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	list, err := agent.Worktrees(ctx)
	if err != nil || len(list) < 2 {
		t.Fatalf("list: %v %v", list, err)
	}
	if err := agent.WorktreeRemove(ctx, "child-work"); err != nil {
		t.Fatalf("remove: %v", err)
	}
}
