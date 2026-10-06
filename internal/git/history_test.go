package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// gitIn runs git in dir as the test identity and returns its trimmed output.
func gitIn(t *testing.T, svc *Service, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(svc.availability.Path, append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitFile(t *testing.T, svc *Service, dir, rel, content, message string) {
	t.Helper()
	write(t, dir, rel, content)
	gitIn(t, svc, dir, "add", rel)
	gitIn(t, svc, dir, "commit", "-m", message)
}

// withRemote gives the repository an origin that has everything committed so
// far: from here on, only new commits are unpushed.
func withRemote(t *testing.T, svc *Service, dir string) {
	t.Helper()
	bare := t.TempDir()
	gitIn(t, svc, bare, "init", "--bare", "--initial-branch=main")
	gitIn(t, svc, dir, "remote", "add", "origin", bare)
	gitIn(t, svc, dir, "push", "-u", "origin", "main")
}

func TestAmendRewritesOnlyAnUnpushedCommit(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	withRemote(t, svc, dir)

	if _, err := svc.Amend(ctx, AmendInput{Message: "nope"}); !errors.Is(err, ErrPushedHistory) {
		t.Fatalf("amending a pushed commit: err = %v, want ErrPushedHistory", err)
	}

	commitFile(t, svc, dir, "a.txt", "a\n", "add a")
	write(t, dir, "b.txt", "b\n")
	gitIn(t, svc, dir, "add", "b.txt")
	amended, err := svc.Amend(ctx, AmendInput{Message: "add a and b", TaskKey: "BE-1"})
	if err != nil {
		t.Fatal(err)
	}
	if amended.Subject != "add a and b" || TaskKeyOf(fullMessage(amended)) != "BE-1" {
		t.Fatalf("amended = %+v", amended)
	}
	if files := gitIn(t, svc, dir, "show", "--name-only", "--format=", "HEAD"); files != "a.txt\nb.txt" {
		t.Fatalf("the staged file was not folded in: %q", files)
	}
	// Empty message keeps the one it had.
	kept, err := svc.Amend(ctx, AmendInput{})
	if err != nil || kept.Subject != "add a and b" {
		t.Fatalf("kept = %+v, %v", kept, err)
	}
}

func TestSquashCombinesUnpushedCommitsAndKeepsEveryTask(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	withRemote(t, svc, dir)
	commitFile(t, svc, dir, "a.txt", "a\n", "add a\n\nTask: BE-1")
	commitFile(t, svc, dir, "b.txt", "b\n", "add b")
	commitFile(t, svc, dir, "c.txt", "c\n", "add c\n\nTask: BE-2")

	if _, err := svc.Squash(ctx, SquashInput{Count: 4}); !errors.Is(err, ErrPushedHistory) {
		t.Fatalf("squashing into pushed history: err = %v", err)
	}
	write(t, dir, "a.txt", "dirty\n")
	if _, err := svc.Squash(ctx, SquashInput{Count: 3}); !errors.Is(err, ErrDirtyWorkTree) {
		t.Fatalf("dirty work tree: err = %v", err)
	}
	gitIn(t, svc, dir, "checkout", "--", "a.txt")

	squashed, err := svc.Squash(ctx, SquashInput{Count: 3})
	if err != nil {
		t.Fatal(err)
	}
	if count := gitIn(t, svc, dir, "rev-list", "--count", "origin/main..HEAD"); count != "1" {
		t.Fatalf("commits after squash = %s, want 1", count)
	}
	message := fullMessage(squashed)
	for _, want := range []string{"add a", "add b", "add c", "Task: BE-1", "Task: BE-2"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q lacks %q", message, want)
		}
	}
	if strings.Count(message, "Task: ") != 2 {
		t.Fatalf("trailers duplicated: %q", message)
	}
	if files := gitIn(t, svc, dir, "show", "--name-only", "--format=", "HEAD"); files != "a.txt\nb.txt\nc.txt" {
		t.Fatalf("files = %q", files)
	}
	if _, err := svc.Squash(ctx, SquashInput{Count: 1}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("one commit: err = %v", err)
	}
}

func TestRebaseReplaysUnpushedWorkAndStopsOnAConflict(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	gitIn(t, svc, dir, "checkout", "-b", "feature")
	commitFile(t, svc, dir, "feature.txt", "f\n", "feature work")
	commitFile(t, svc, dir, "README.md", "hello from feature\n", "feature readme")
	gitIn(t, svc, dir, "checkout", "main")
	commitFile(t, svc, dir, "main.txt", "m\n", "main work")
	commitFile(t, svc, dir, "README.md", "hello from main\n", "main readme")
	gitIn(t, svc, dir, "checkout", "feature")

	result, err := svc.Rebase(ctx, RebaseInput{Onto: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stopped != OperationRebase || len(result.Conflicted) != 1 || result.Conflicted[0] != "README.md" {
		t.Fatalf("result = %+v, want a stop on README.md", result)
	}
	if op, _ := svc.InProgress(ctx); op != OperationRebase {
		t.Fatalf("in progress = %q", op)
	}
	// Nothing else may start meanwhile.
	if _, err := svc.Squash(ctx, SquashInput{Count: 2}); !errors.Is(err, ErrInProgress) {
		t.Fatalf("squash during a rebase: err = %v", err)
	}

	// Abort puts the branch back exactly where it was.
	before := gitIn(t, svc, dir, "rev-parse", "ORIG_HEAD")
	if _, err := svc.Rebase(ctx, RebaseInput{Action: RebaseAbort}); err != nil {
		t.Fatal(err)
	}
	if head := gitIn(t, svc, dir, "rev-parse", "HEAD"); head != before {
		t.Fatalf("abort left HEAD at %s, want %s", head, before)
	}

	// Again, resolving and continuing this time.
	if _, err := svc.Rebase(ctx, RebaseInput{Onto: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Resolve(ctx, "README.md", "hello from both\n"); err != nil {
		t.Fatal(err)
	}
	done, err := svc.Rebase(ctx, RebaseInput{Action: RebaseContinue})
	if err != nil {
		t.Fatal(err)
	}
	if done.Stopped != OperationNone || done.Head.Subject != "feature readme" {
		t.Fatalf("done = %+v", done)
	}
	if base := gitIn(t, svc, dir, "merge-base", "HEAD", "main"); base != gitIn(t, svc, dir, "rev-parse", "main") {
		t.Fatalf("feature is not on top of main")
	}
	if _, err := svc.Rebase(ctx, RebaseInput{Action: RebaseContinue}); !errors.Is(err, ErrNothingInProgress) {
		t.Fatalf("continue with nothing under way: err = %v", err)
	}
}

func TestRebaseRefusesToReplayPushedCommits(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	gitIn(t, svc, dir, "checkout", "-b", "feature")
	commitFile(t, svc, dir, "feature.txt", "f\n", "feature work")
	withRemote(t, svc, dir) // pushes main
	gitIn(t, svc, dir, "push", "origin", "feature")
	gitIn(t, svc, dir, "checkout", "main")
	commitFile(t, svc, dir, "main.txt", "m\n", "main work")
	gitIn(t, svc, dir, "checkout", "feature")

	if _, err := svc.Rebase(ctx, RebaseInput{Onto: "main"}); !errors.Is(err, ErrPushedHistory) {
		t.Fatalf("err = %v, want ErrPushedHistory", err)
	}
	if op, _ := svc.InProgress(ctx); op != OperationNone {
		t.Fatalf("a refused rebase left %q under way", op)
	}
}

func TestRevertAndCherryPickAddHistoryEvenOverPushedCommits(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	commitFile(t, svc, dir, "a.txt", "a\n", "add a")
	withRemote(t, svc, dir)
	pushed := gitIn(t, svc, dir, "rev-parse", "HEAD")

	reverted, err := svc.Revert(ctx, PickInput{Commits: []string{pushed}, TaskKey: "BE-9"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(reverted.Head.Subject, "Revert") || TaskKeyOf(fullMessage(reverted.Head)) != "BE-9" {
		t.Fatalf("reverted = %+v", reverted.Head)
	}
	if _, err := os.Stat(dir + "/a.txt"); !os.IsNotExist(err) {
		t.Fatalf("a.txt still there after the revert")
	}

	gitIn(t, svc, dir, "checkout", "-b", "other", "HEAD~1")
	commitFile(t, svc, dir, "x.txt", "x\n", "add x")
	picked := gitIn(t, svc, dir, "rev-parse", "HEAD")
	gitIn(t, svc, dir, "checkout", "main")
	result, err := svc.CherryPick(ctx, PickInput{Commits: []string{picked}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Head.Subject != "add x" || !strings.Contains(result.Head.Body, "cherry picked from commit "+picked) {
		t.Fatalf("picked = %+v", result.Head)
	}
	if _, err := svc.CherryPick(ctx, PickInput{Commits: []string{"--hard"}}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("a flag as a commit: err = %v", err)
	}
}

// A guarded branch guards its history too: rewriting it asks, and with nobody
// to ask (headless) the answer is no — and the branch is left as it was.
func TestHistoryRewritesOnAGuardedBranchFailClosed(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	commitFile(t, svc, dir, "a.txt", "a\n", "add a")
	commitFile(t, svc, dir, "b.txt", "b\n", "add b")
	head := gitIn(t, svc, dir, "rev-parse", "HEAD")
	svc.SetGuards(stubGuards{{Pattern: "main", Env: "prod"}})
	svc.SetEnvs(stubEnvs{"prod": true})

	if _, err := svc.Squash(ctx, SquashInput{Count: 2}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("squash: err = %v, want ErrNotAllowed", err)
	}
	if _, err := svc.Amend(ctx, AmendInput{Message: "x"}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("amend: err = %v, want ErrNotAllowed", err)
	}
	if now := gitIn(t, svc, dir, "rev-parse", "HEAD"); now != head {
		t.Fatalf("a refused rewrite moved HEAD")
	}
}
