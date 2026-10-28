//go:build unix

package git

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// modes runs a test once on the platform's own path and once with every command
// started through the helper (internal/fchdirexec), which is what a platform without
// /proc uses. Both bind the directory by descriptor.
func modes(t *testing.T, fn func(t *testing.T)) {
	t.Run("native", fn)
	t.Run("helper", func(t *testing.T) {
		forceHelper = true
		t.Cleanup(func() { forceHelper = false })
		fn(t)
	})
}

func commitIn(t *testing.T, svc *Service, dir, subject string) {
	t.Helper()
	runGit(t, svc, dir, "commit", "-q", "--allow-empty", "-m", subject)
}

func subjectOf(t *testing.T, s *Service) string {
	t.Helper()
	commits, err := s.Log(agentCtx(), 1)
	if err != nil || len(commits) != 1 {
		t.Fatalf("log: %v %v", commits, err)
	}
	return commits[0].Subject
}

func requireMoved(t *testing.T, err error, what string) {
	t.Helper()
	if !isMoved(err) {
		t.Fatalf("%s: err = %v, want git.repoOutsideWorkspace", what, err)
	}
}

// Probe 1 at the service: after the handle opened a folder, the folder is renamed away and a
// link to another repository takes its name. The handle refuses, and what it would have said
// is not the other repository's.
func TestBoundHandleRefusesAFolderThatWasSwappedForALink(t *testing.T) {
	modes(t, func(t *testing.T) {
		svc, dir := newRepo(t)
		commitIn(t, svc, dir, "GRANTED")
		victim := t.TempDir()
		runGit(t, svc, victim, "init", "-q", "--initial-branch=main")
		commitIn(t, svc, victim, "VICTIM")

		agent := agentAt(t, svc, dir)
		defer agent.Close()
		if !agent.Bound() {
			t.Fatal("an agent-origin handle is not bound")
		}
		if got := subjectOf(t, agent); got != "GRANTED" {
			t.Fatalf("before: %q", got)
		}
		if err := os.Rename(dir, dir+".moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, dir); err != nil {
			t.Fatal(err)
		}
		_, err := agent.Log(agentCtx(), 1)
		requireMoved(t, err, "log after the swap")
		_, err = agent.Status(agentCtx())
		requireMoved(t, err, "status after the swap")
	})
}

// The descriptors alone, with the identity check made to pass: what git is started in is
// the directory that was opened, not what its name leads to now. The check names the held
// directories by their /proc entries (Linux), so it cannot tell, and the commands must
// still answer from the repository that was opened.
func TestBoundCommandsRunInTheHeldDirectoryWhateverItsNameLeadsTo(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc to make the identity check blind")
	}
	modes(t, func(t *testing.T) {
		svc, dir := newRepo(t)
		commitIn(t, svc, dir, "GRANTED")
		victim := t.TempDir()
		runGit(t, svc, victim, "init", "-q", "--initial-branch=main")
		commitIn(t, svc, victim, "VICTIM")
		write(t, victim, "VICTIM_ONLY.txt", "x")

		agent := agentAt(t, svc, dir)
		defer agent.Close()
		b := agent.runner.bound
		pid := strconv.Itoa(os.Getpid())
		fdName := func(f *os.File) string { return "/proc/" + pid + "/fd/" + strconv.Itoa(int(f.Fd())) }
		b.path, b.topPath, b.gitPath = fdName(b.cwd), fdName(b.top), fdName(b.gitDir)
		b.dotGitIs = false

		if err := os.Rename(dir, dir+".moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, dir); err != nil {
			t.Fatal(err)
		}
		if got := subjectOf(t, agent); got != "GRANTED" {
			t.Fatalf("log after the swap answers %q", got)
		}
		status, err := agent.Status(agentCtx())
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range status.Untracked {
			if strings.Contains(name, "VICTIM_ONLY") {
				t.Fatalf("status read the other work tree: %v", status.Untracked)
			}
		}
	})
}

// A linked worktree and a submodule have a .git file; the git directory is elsewhere.
func TestBoundHandleOfALinkedWorktreeAndASubmodule(t *testing.T) {
	modes(t, func(t *testing.T) {
		svc, dir := newRepo(t)
		runGit(t, svc, dir, "branch", "wt")
		worktree := filepath.Join(t.TempDir(), "wt")
		runGit(t, svc, dir, "worktree", "add", worktree, "wt")
		commitIn(t, svc, worktree, "IN-THE-WORKTREE")

		agent := agentAt(t, svc, worktree)
		defer agent.Close()
		dot, err := agent.DotGit()
		if err != nil || dot.Kind != "file" || dot.Gitdir == "" {
			t.Fatalf("dotgit = %+v, %v", dot, err)
		}
		if raw, err := agent.GitDirFile("gitdir"); err != nil || !strings.Contains(string(raw), "wt") {
			t.Fatalf("gitdir file = %q, %v", raw, err)
		}
		if got := subjectOf(t, agent); got != "IN-THE-WORKTREE" {
			t.Fatalf("linked worktree answers %q", got)
		}
		write(t, worktree, "w.txt", "w\n")
		if st, err := agent.Status(agentCtx()); err != nil || len(st.Untracked) != 1 {
			t.Fatalf("status = %+v, %v", st, err)
		}

		sub := t.TempDir()
		runGit(t, svc, sub, "init", "-q", "--initial-branch=main")
		commitIn(t, svc, sub, "SUBMODULE")
		runGit(t, svc, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "vendored")
		agentSub := agentAt(t, svc, filepath.Join(dir, "vendored"))
		defer agentSub.Close()
		if got := subjectOf(t, agentSub); got != "SUBMODULE" {
			t.Fatalf("submodule answers %q", got)
		}
		if _, err := agentSub.Status(agentCtx()); err != nil {
			t.Fatal(err)
		}
	})
}

// A handle opened in a sub folder answers as an unbound one does.
func TestBoundHandleInASubFolderSeesWhatAnUnboundOneSees(t *testing.T) {
	modes(t, func(t *testing.T) {
		svc, dir := newRepo(t)
		write(t, dir, "sub/deeper/a.txt", "a\n")
		runGit(t, svc, dir, "add", "sub")
		commitIn(t, svc, dir, "with sub")
		write(t, dir, "sub/deeper/b.txt", "b\n")
		write(t, dir, "top.txt", "t\n")

		plain, err := svc.At(context.Background(), filepath.Join(dir, "sub", "deeper"))
		if err != nil {
			t.Fatal(err)
		}
		agent := agentAt(t, svc, filepath.Join(dir, "sub", "deeper"))
		defer agent.Close()
		if !agent.Bound() {
			t.Fatal("not bound")
		}
		want, err := plain.Status(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got, err := agent.Status(agentCtx())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got.Untracked, ",") != strings.Join(want.Untracked, ",") || got.Branch != want.Branch {
			t.Fatalf("sub folder: bound %+v, unbound %+v", got, want)
		}
		if id, ok := agent.Identity(); !ok || id.Top != dir && id.Top != realDir(t, dir) {
			t.Fatalf("identity = %+v %v", id, ok)
		}
	})
}

func realDir(t *testing.T, dir string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// Push from a bound agent handle to a local bare remote, through the guarded path: only a
// branch recorded on a task, and the remote's own hooks do not run.
func TestBoundAgentPushToALocalBareRemote(t *testing.T) {
	modes(t, func(t *testing.T) {
		svc, work, bare := newRepoWithRemote(t)
		runGit(t, svc, work, "checkout", "-q", "-b", "feature")
		commitIn(t, svc, work, "FEATURE")
		marker := markerIn(t)
		hook := filepath.Join(bare, "hooks", "pre-receive")
		if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		agent := agentAt(t, svc, work)
		defer agent.Close()
		err := agent.Push(agentCtx(), PushInput{Remote: "origin", Branch: "feature", SetUpstream: true, ByAgent: true, TaskBranches: []string{"feature"}})
		if err != nil {
			t.Fatal(err)
		}
		if refs := runGit(t, svc, bare, "for-each-ref", "--format=%(refname)"); !strings.Contains(refs, "refs/heads/feature") {
			t.Fatalf("the remote has no feature branch: %q", refs)
		}
		if exists(marker) {
			t.Fatal("the bare remote's hook ran in the host")
		}
		if err := agent.Push(agentCtx(), PushInput{Remote: "origin", Branch: "main", ByAgent: true, TaskBranches: []string{"feature"}}); err == nil {
			t.Fatal("an agent pushed a branch no task holds")
		}
	})
}

// The hardening of the first delivery is still what a bound command runs with, and the
// host's own environment still wins over it.
func TestBoundCommandKeepsTheHardenedEnvironment(t *testing.T) {
	svc, dir := newRepo(t)
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	t.Setenv("GIT_WORK_TREE", "/elsewhere")
	t.Setenv("GIT_ASKPASS", "/host/askpass")
	agent := agentAt(t, svc, dir)
	defer agent.Close()
	host := []string{"GIT_ASKPASS=/devkit/askpass.sh", "DEVKIT_GIT_SECRET=s3"}
	cmd, err := agent.runner.command(agentCtx(), host, "push", "origin", "main")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"GIT_ASKPASS":         "/devkit/askpass.sh",
		"DEVKIT_GIT_SECRET":   "s3",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_WORK_TREE":       ".",
	} {
		if got, ok := lastEnv(cmd.Env, name); !ok || got != want {
			t.Errorf("%s = %q (set %v), want %q", name, got, ok, want)
		}
	}
	gitDir, ok := lastEnv(cmd.Env, "GIT_DIR")
	if !ok || strings.Contains(gitDir, "elsewhere") {
		t.Errorf("GIT_DIR = %q", gitDir)
	}
	if runtime.GOOS == "linux" && !strings.HasPrefix(gitDir, "/proc/self/fd/") {
		t.Errorf("GIT_DIR = %q, want a descriptor", gitDir)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "core.hooksPath="+noHooksPath()) || strings.Contains(joined, " -C ") {
		t.Errorf("args = %s", joined)
	}
}

// The service's own reads and writes in the work tree go through the held directory and
// refuse a link anywhere on the way.
func TestBoundWorkTreeFilesAreReadAndWrittenThroughTheHeldDirectory(t *testing.T) {
	svc, dir := newRepo(t)
	agent := agentAt(t, svc, dir)
	defer agent.Close()
	r := agent.runner

	if err := r.writeWork("a/b/c.txt", "one\n"); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := r.readWork("a/b/c.txt"); err != nil || !ok || got != "one\n" {
		t.Fatalf("read = %q %v %v", got, ok, err)
	}
	if _, ok, err := r.readWork("missing/x"); err != nil || ok {
		t.Fatalf("missing = %v %v", ok, err)
	}
	if mode, size, err := r.lstatWork("a/b/c.txt"); err != nil || !mode.IsRegular() || size != 4 {
		t.Fatalf("lstat = %v %d %v", mode, size, err)
	}
	for _, bad := range []string{"../x", "/etc/passwd", ""} {
		if err := r.writeWork(bad, "x"); err == nil {
			t.Errorf("wrote %q", bad)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := r.writeWork("link/escaped.txt", "x"); err == nil {
		t.Error("wrote through a link directory")
	}
	if exists(filepath.Join(outside, "escaped.txt")) {
		t.Error("the file reached the link's target")
	}
	if err := os.Symlink(filepath.Join(outside, "t"), filepath.Join(dir, "filelink")); err != nil {
		t.Fatal(err)
	}
	if err := r.writeWork("filelink", "x"); err == nil || exists(filepath.Join(outside, "t")) {
		t.Errorf("wrote through a link file: %v", err)
	}
}

// The operation probe asks the held git directory, not the name git would print for it.
func TestBoundInProgressReadsTheHeldGitDirectory(t *testing.T) {
	svc, dir := newRepo(t)
	agent := agentAt(t, svc, dir)
	defer agent.Close()
	if op, err := agent.InProgress(agentCtx()); err != nil || op != OperationNone {
		t.Fatalf("in progress = %v %v", op, err)
	}
	sha := runGit(t, svc, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, ".git", "MERGE_HEAD"), []byte(strings.TrimSpace(sha)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if op, err := agent.InProgress(agentCtx()); err != nil || op != OperationMerge {
		t.Fatalf("in progress = %v %v", op, err)
	}
}

// A folder that is not there, or is a link, is not opened, and a bound handle is closed
// by Close and refuses afterwards.
func TestBoundOpenRefusesALinkAndCloseEndsTheHandle(t *testing.T) {
	svc, dir := newRepo(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	// At resolves a link once, so a link that is still there opens the directory it led to.
	linked, err := svc.At(agentCtx(), link)
	if err != nil {
		t.Fatal(err)
	}
	if got := linked.BoundPath(); got != realDir(t, dir) {
		t.Errorf("bound path = %q, want %q", got, realDir(t, dir))
	}
	linked.Close()

	if _, err := svc.At(agentCtx(), filepath.Join(dir, "nope")); err != ErrNoRepository {
		t.Errorf("a missing folder: %v", err)
	}
	agent := agentAt(t, svc, dir)
	agent.Close()
	agent.Close()
	if _, err := agent.Log(agentCtx(), 1); err == nil {
		t.Error("a closed handle answered")
	}
}

// The walk itself: a component that is a link is refused, never followed.
func TestWalkDirRefusesLinksOnTheWay(t *testing.T) {
	root := realDir(t, t.TempDir())
	real := filepath.Join(root, "real", "inner")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	f, err := walkDir(real)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if f, err := walkDir(filepath.Join(root, "alias", "inner")); err == nil {
		f.Close()
		t.Fatal("walked through a link")
	}
	if _, err := openBound(filepath.Join(root, "alias", "inner")); err != nil {
		t.Errorf("openBound resolves a link once and holds what it led to: %v", err)
	}
}

// The guard judges the repository the command runs in: the handle's policy key was read
// from the held directory when it opened, so a guarded branch is guarded, and a folder
// swapped for another repository cannot make the command run (or commit) there, where
// no guard is set.
func TestBoundGuardIsTheOneOfTheRepositoryTheCommandRunsIn(t *testing.T) {
	svc, dir := newRepo(t)
	victim := t.TempDir()
	runGit(t, svc, victim, "init", "-q", "--initial-branch=main")
	commitIn(t, svc, victim, "VICTIM")
	before := runGit(t, svc, victim, "rev-parse", "HEAD")

	confirm := &stubConfirm{allow: false}
	svc.SetConfirmer(confirm)
	agent := agentAt(t, svc, dir)
	defer agent.Close()
	svc.SetGuards(keyedGuardStore{rows: map[string][]Guard{agent.PolicyProject(): {{Pattern: "main", Env: "prod"}}}})
	agent.guards, agent.confirm = svc.guards, confirm
	svc.SetEnvs(stubEnvs{"prod": true})
	agent.envs = svc.envs

	write(t, dir, "a.txt", "a\n")
	if err := agent.Stage(agentCtx(), []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Commit(agentCtx(), CommitInput{Message: "guarded", ByAgent: true}); err == nil || confirm.asked != 1 {
		t.Fatalf("a commit on a guarded branch: err=%v asked=%d", err, confirm.asked)
	}

	if err := os.Rename(dir, dir+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, dir); err != nil {
		t.Fatal(err)
	}
	write(t, victim, "b.txt", "b\n")
	if _, err := agent.Commit(agentCtx(), CommitInput{Message: "into the victim", ByAgent: true}); err == nil {
		t.Fatal("a commit went through a folder that was swapped for another repository")
	}
	if after := runGit(t, svc, victim, "rev-parse", "HEAD"); after != before {
		t.Fatalf("the other repository got a commit: %s -> %s", strings.TrimSpace(before), strings.TrimSpace(after))
	}
}
