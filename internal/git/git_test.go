package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRepo builds a real repository. There is no useful fake of git's output,
// and the parsers exist precisely to read what the real thing prints.
func newRepo(t *testing.T) (*Service, string) {
	t.Helper()
	ctx := context.Background()
	svc := NewService(ctx)
	if !svc.Available() {
		t.Skip("no usable git on this machine")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(svc.availability.Path, append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main", "--template=")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	write(t, dir, "README.md", "hello\n")
	run("add", ".")
	run("commit", "-m", "first")
	if err := svc.Open(ctx, dir); err != nil {
		t.Fatalf("open: %v", err)
	}
	return svc, dir
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRejectsADirectoryThatIsNotARepository(t *testing.T) {
	svc := NewService(context.Background())
	if !svc.Available() {
		t.Skip("no usable git")
	}
	err := svc.Open(context.Background(), t.TempDir())
	if !errors.Is(err, ErrNoRepository) {
		t.Fatalf("open = %v, want ErrNoRepository", err)
	}
}

func TestStatusSplitsStagedFromUnstaged(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	write(t, dir, "staged.txt", "a\n")
	write(t, dir, "README.md", "changed\n")
	write(t, dir, "untracked.txt", "u\n")
	if err := svc.Stage(ctx, []string{"staged.txt"}); err != nil {
		t.Fatal(err)
	}
	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Branch != "main" {
		t.Fatalf("branch = %q", status.Branch)
	}
	if len(status.Staged) != 1 || status.Staged[0] != "staged.txt" {
		t.Fatalf("staged = %v", status.Staged)
	}
	if len(status.Unstaged) != 1 || status.Unstaged[0] != "README.md" {
		t.Fatalf("unstaged = %v", status.Unstaged)
	}
	if len(status.Untracked) != 1 || status.Untracked[0] != "untracked.txt" {
		t.Fatalf("untracked = %v", status.Untracked)
	}
	if status.Files["untracked.txt"] != "A" {
		t.Fatalf("untracked state = %q", status.Files["untracked.txt"])
	}
}

func TestCommitAndLog(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	write(t, dir, "a.txt", "a\n")
	if err := svc.Stage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	commit, err := svc.Commit(ctx, CommitInput{Message: "add a"})
	if err != nil {
		t.Fatal(err)
	}
	if commit.Subject != "add a" {
		t.Fatalf("subject = %q", commit.Subject)
	}
	log, err := svc.Log(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0].Subject != "add a" {
		t.Fatalf("log = %+v", log)
	}
}

// The task link, end to end, through the real grep.
func TestTaskTrailerIsHowCommitsFindTheirTask(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	write(t, dir, "a.txt", "a\n")
	if err := svc.Stage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "add a", TaskKey: "DEV-4f2a91"}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "b.txt", "b\n")
	if err := svc.Stage(ctx, []string{"b.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "unrelated"}); err != nil {
		t.Fatal(err)
	}

	found, err := svc.CommitsForTask(ctx, "DEV-4f2a91", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Subject != "add a" {
		t.Fatalf("commits for task = %+v", found)
	}
	if TaskKeyOf(found[0].Body) != "DEV-4f2a91" {
		t.Fatalf("trailer not readable back: %q", found[0].Body)
	}
	if none, _ := svc.CommitsForTask(ctx, "DEV-nothing", 10); len(none) != 0 {
		t.Fatalf("a key nothing carries matched %+v", none)
	}
}

func TestTaskTrailerIsNotDuplicated(t *testing.T) {
	msg := WithTaskTrailer("fix\n\nTask: DEV-1", "DEV-1")
	if strings.Count(msg, "Task: DEV-1") != 1 {
		t.Fatalf("trailer duplicated: %q", msg)
	}
	if got := WithTaskTrailer("fix", "DEV-1"); got != "fix\n\nTask: DEV-1" {
		t.Fatalf("WithTaskTrailer = %q", got)
	}
	if got := WithTaskTrailer("fix", ""); got != "fix" {
		t.Fatalf("no key must add nothing: %q", got)
	}
}

// StageTurn stages exactly the turn's paths, and silently drops the ones the
// turn changed and then changed back — staging those would add empty entries.
func TestStageTurnStagesOnlyWhatIsStillChanged(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	write(t, dir, "touched.txt", "x\n")
	write(t, dir, "other.txt", "o\n")
	staged, err := svc.StageTurn(ctx, []string{"touched.txt", "reverted.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(staged) != 1 || staged[0] != "touched.txt" {
		t.Fatalf("staged = %v", staged)
	}
	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Staged) != 1 || status.Staged[0] != "touched.txt" {
		t.Fatalf("other.txt was staged too: %v", status.Staged)
	}
}

func TestDiffCountsLines(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	write(t, dir, "README.md", "hello\nworld\n")
	changes, err := svc.Diff(ctx, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "README.md" || changes[0].Added != 1 {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestBranchCreateReturnsTheNameForTheTaskToRecord(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()
	name, err := svc.CreateBranch(ctx, "DEV-4f2a91-add-refunds", "")
	if err != nil {
		t.Fatal(err)
	}
	if name != "DEV-4f2a91-add-refunds" {
		t.Fatalf("CreateBranch returned %q", name)
	}
	branches, err := svc.Branches(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var current string
	for _, b := range branches {
		if b.Current {
			current = b.Name
		}
	}
	if current != name {
		t.Fatalf("current branch = %q, want %q", current, name)
	}
}

// Checkout refuses to carry uncommitted work across, because a caller that is
// not looking would move the user's changes onto another branch unnoticed.
func TestCheckoutRefusesOverUncommittedWork(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	if _, err := svc.CreateBranch(ctx, "feature", ""); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "README.md", "dirty\n")
	if err := svc.Checkout(ctx, "main"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("checkout over dirty tree = %v, want ErrNotAllowed", err)
	}
	if err := svc.Stash(ctx, "wip"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Checkout(ctx, "main"); err != nil {
		t.Fatalf("checkout after stash: %v", err)
	}
}

type stubConfirm struct {
	allow  bool
	asked  int
	titles []string
}

func (s *stubConfirm) Confirm(_ context.Context, title, _ string) (bool, error) {
	s.asked++
	s.titles = append(s.titles, title)
	return s.allow, nil
}

type stubGuards []Guard

func (g stubGuards) ListGuards(string) ([]Guard, error) { return g, nil }

type stubEnvs map[string]bool

func (e stubEnvs) RequiresConfirmation(name string) bool { return e[name] }

// Commit is not gated by default — it is local and reversible. A guard is
// what makes it ask, for the repository whose commits are deploy manifests.
func TestCommitIsGatedOnlyOnAGuardedBranch(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	confirm := &stubConfirm{allow: true}
	svc.SetConfirmer(confirm)
	svc.SetEnvs(stubEnvs{"prod": true})

	write(t, dir, "a.txt", "a\n")
	if err := svc.Stage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "unguarded"}); err != nil {
		t.Fatal(err)
	}
	if confirm.asked != 0 {
		t.Fatalf("an unguarded commit asked %d times", confirm.asked)
	}

	svc.SetGuards(stubGuards{{Pattern: "main", Env: "prod"}})
	write(t, dir, "b.txt", "b\n")
	if err := svc.Stage(ctx, []string{"b.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "guarded"}); err != nil {
		t.Fatal(err)
	}
	if confirm.asked != 1 {
		t.Fatalf("a guarded commit asked %d times, want 1", confirm.asked)
	}
}

// Headless is a denial, not a bypass.
func TestGuardedCommitFailsClosedWithoutAConfirmer(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	svc.SetGuards(stubGuards{{Pattern: "main", Env: "prod"}})
	svc.SetEnvs(stubEnvs{"prod": true})
	write(t, dir, "a.txt", "a\n")
	if err := svc.Stage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "headless"}); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("headless guarded commit = %v, want ErrNotAllowed", err)
	}
}

func TestGuardPatternMatchesAGlob(t *testing.T) {
	guard := Guard{Pattern: "release/*", Env: "prod"}
	if !guard.Matches("release/2026-09") {
		t.Fatal("glob did not match")
	}
	if guard.Matches("main") {
		t.Fatal("glob matched an unrelated branch")
	}
	if (Guard{}).Matches("main") {
		t.Fatal("an empty pattern matched")
	}
}

// The agent push rule, which is durable because it reads the task rather than
// a list living in the session.
func TestAgentPushRule(t *testing.T) {
	if err := protectedForAgent("main", "main", []string{"main"}); !errors.Is(err, ErrProtectedBranch) {
		t.Fatal("an agent was allowed to push to the default branch")
	}
	if err := protectedForAgent("DEV-1-x", "main", nil); !errors.Is(err, ErrProtectedBranch) {
		t.Fatal("an agent pushed a branch no task records")
	}
	if err := protectedForAgent("DEV-1-x", "main", []string{"DEV-1-x"}); err != nil {
		t.Fatalf("a branch recorded on the agent's task was refused: %v", err)
	}
	if err := protectedForAgent("master", "main", []string{"master"}); !errors.Is(err, ErrProtectedBranch) {
		t.Fatal("master is protected whatever the task says")
	}
}

func TestBranchNameFor(t *testing.T) {
	if got := BranchNameFor("", "DEV-4f2a91", "Add refunds endpoint!"); got != "DEV-4f2a91-add-refunds-endpoint" {
		t.Fatalf("BranchNameFor = %q", got)
	}
	if got := BranchNameFor("agent/{key}", "DEV-1", "x"); got != "agent/DEV-1" {
		t.Fatalf("template ignored: %q", got)
	}
}

// A conflicted path is not "modified in both": staging it would commit the
// markers, so it is reported separately and Commit refuses.
func TestConflictIsReportedAndBlocksCommit(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command(svc.availability.Path, append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.x",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.x")
		_ = cmd.Run() // merge is expected to fail
	}
	write(t, dir, "shared.txt", "base\n")
	git("add", ".")
	git("commit", "-m", "base")
	git("switch", "-c", "other")
	write(t, dir, "shared.txt", "theirs\n")
	git("add", ".")
	git("commit", "-m", "theirs")
	git("switch", "main")
	write(t, dir, "shared.txt", "ours\n")
	git("add", ".")
	git("commit", "-m", "ours")
	git("merge", "other")

	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Conflicted) != 1 || status.Conflicted[0] != "shared.txt" {
		t.Fatalf("conflicted = %v", status.Conflicted)
	}
	for _, staged := range status.Staged {
		if staged == "shared.txt" {
			t.Fatal("a conflicted path was reported as staged")
		}
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("commit during a conflict = %v, want ErrConflict", err)
	}

	conflict, err := svc.Conflict(ctx, "shared.txt")
	if err != nil {
		t.Fatal(err)
	}
	if conflict.Ours != "ours\n" || conflict.Theirs != "theirs\n" || conflict.Base != "base\n" {
		t.Fatalf("three stages = %+v", conflict)
	}

	if err := svc.Resolve(ctx, "shared.txt", "merged\n"); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Conflicted) != 0 {
		t.Fatalf("still conflicted after resolve: %v", after.Conflicted)
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "merged"}); err != nil {
		t.Fatalf("commit after resolve: %v", err)
	}
}

// A path from the frontend or an agent is input, not a fact.
func TestResolveRefusesToEscapeTheProject(t *testing.T) {
	svc, dir := newRepo(t)
	if err := svc.Resolve(context.Background(), "../escaped.txt", "x"); err == nil {
		t.Fatal("a path outside the project was written")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escaped.txt")); err == nil {
		t.Fatal("the escape actually wrote a file")
	}
}

// The pairing that makes hunk staging correct: the patch is measured against
// the index, and StageHunk applies it to the index. A file already partly
// staged therefore diffs against what is staged, not against HEAD.
func TestFilePatchIsAgainstTheIndexSoHunksApply(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	write(t, dir, "many.txt", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\n14\n15\n")
	if err := svc.Stage(ctx, []string{"many.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "base"}); err != nil {
		t.Fatal(err)
	}

	// Two separate edits, far enough apart to be two hunks.
	write(t, dir, "many.txt", "1\nTOP\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n13\nBOTTOM\n15\n")
	patch, err := svc.FilePatch(ctx, "many.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(patch, "\n@@") < 2 {
		t.Fatalf("expected two hunks:\n%s", patch)
	}

	// Feed back only the first hunk, the way the UI will.
	lines := strings.SplitAfter(patch, "\n")
	var head []string
	var first []string
	seen := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			seen++
			if seen > 1 {
				break
			}
		}
		if seen == 0 {
			head = append(head, line)
			continue
		}
		first = append(first, line)
	}
	if err := svc.StageHunk(ctx, strings.Join(head, "")+strings.Join(first, "")); err != nil {
		t.Fatalf("applying one hunk: %v", err)
	}

	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Staged and unstaged at once: the first edit is in the index, the second
	// is still only in the work tree.
	if len(status.Staged) != 1 || len(status.Unstaged) != 1 {
		t.Fatalf("staged=%v unstaged=%v; one hunk should land and one should not",
			status.Staged, status.Unstaged)
	}

	// And the remaining diff is now against the new index, so the second hunk
	// is all that is left to stage.
	rest, err := svc.FilePatch(ctx, "many.txt")
	if err != nil {
		t.Fatal(err)
	}
	// Only the CHANGED lines matter here: "TOP" also appears as the @@ header's
	// context label, which is a hint about where the hunk sits, not an edit.
	if strings.Contains(rest, "\n+TOP") {
		t.Fatalf("the staged hunk is still reported as unstaged:\n%s", rest)
	}
	if !strings.Contains(rest, "\n+BOTTOM") {
		t.Fatalf("the unstaged hunk vanished:\n%s", rest)
	}
}

// A binary file has no hunks to pick; the caller stages the whole thing.
func TestFilePatchOnABinaryFileHasNoHunks(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0, 1, 2, 0, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.Stage(ctx, []string{"blob.bin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "binary"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte{0, 9, 9, 0, 9}, 0o644); err != nil {
		t.Fatal(err)
	}
	patch, err := svc.FilePatch(ctx, "blob.bin")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(patch, "@@") {
		t.Fatalf("a binary file produced hunks:\n%s", patch)
	}
}

// mergeConflict builds a real merge conflict: `main` and `other` each change
// the same path away from a common base, then `other` is merged into `main`.
// The two mutate funcs run on their branch with the work tree checked out, so
// a test can delete the file on one side instead of editing it.
func mergeConflict(t *testing.T, path string, base string, ours, theirs func(dir string)) (*Service, string) {
	t.Helper()
	svc, dir := newRepo(t)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command(svc.availability.Path, append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.x",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.x")
		_ = cmd.Run() // the merge itself is expected to fail
	}
	write(t, dir, path, base)
	git("add", ".")
	git("commit", "-m", "base")
	git("switch", "-c", "other")
	theirs(dir)
	git("add", "-A")
	git("commit", "-m", "theirs")
	git("switch", "main")
	ours(dir)
	git("add", "-A")
	git("commit", "-m", "ours")
	git("merge", "other")
	return svc, dir
}

// The panel edits git's own merge, not one of the two sides. Seeding from
// Ours would silently drop every hunk the other side merged cleanly, which
// looks like the editor eating someone's work.
func TestConflictCarriesGitsOwnMergeWithMarkers(t *testing.T) {
	svc, _ := mergeConflict(t, "shared.txt",
		"one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n",
		func(dir string) {
			write(t, dir, "shared.txt", "ONE\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")
		},
		func(dir string) {
			write(t, dir, "shared.txt", "1\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nNINE\n")
		},
	)
	conflict, err := svc.Conflict(context.Background(), "shared.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !conflict.HasOurs || !conflict.HasTheirs || !conflict.HasBase {
		t.Fatalf("all three stages exist here: %+v", conflict)
	}
	if !strings.Contains(conflict.Merged, "<<<<<<<") || !strings.Contains(conflict.Merged, ">>>>>>>") {
		t.Fatalf("merged has no conflict markers:\n%s", conflict.Merged)
	}
	// The far end of the file merged cleanly and must be present un-marked —
	// this is the part seeding from Ours would have lost.
	if !strings.Contains(conflict.Merged, "NINE\n") {
		t.Fatalf("the cleanly merged change from the other side is missing:\n%s", conflict.Merged)
	}
}

// Taking a side is git copying its own stage, never this process round-tripping
// the bytes through a panel.
func TestResolveSideTakesTheStageExactly(t *testing.T) {
	ctx := context.Background()
	svc, dir := mergeConflict(t, "shared.txt", "base\n",
		func(dir string) { write(t, dir, "shared.txt", "ours\r\nwith crlf\r\n") },
		func(dir string) { write(t, dir, "shared.txt", "theirs\n") },
	)
	if err := svc.ResolveSide(ctx, "shared.txt", "ours"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "ours\r\nwith crlf\r\n" {
		t.Fatalf("taking ours changed the bytes: %q", content)
	}
	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Conflicted) != 0 {
		t.Fatalf("still conflicted: %v", status.Conflicted)
	}
	// Taking ours leaves the file identical to HEAD, so there is nothing for
	// it to be staged AS. What matters is that the path is resolved in the
	// index, which is what lets the merge be concluded.
	for _, path := range status.Unstaged {
		if path == "shared.txt" {
			t.Fatal("taking a side left an unstaged change behind")
		}
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "merged"}); err != nil {
		t.Fatalf("commit after taking a side: %v", err)
	}
}

func TestResolveSideRefusesASideThatIsNeither(t *testing.T) {
	svc, _ := mergeConflict(t, "shared.txt", "base\n",
		func(dir string) { write(t, dir, "shared.txt", "ours\n") },
		func(dir string) { write(t, dir, "shared.txt", "theirs\n") },
	)
	if err := svc.ResolveSide(context.Background(), "shared.txt", "mine"); !errors.Is(err, ErrNoSuchSide) {
		t.Fatalf("ResolveSide with a made-up side = %v", err)
	}
}

// Delete/modify: one side removed the file, the other edited it. Taking the
// deleting side means removing the file, not writing an empty one — which is
// why the stages' EXISTENCE is reported separately from their content.
func TestResolveSideTakesADeletionAsADeletion(t *testing.T) {
	ctx := context.Background()
	svc, dir := mergeConflict(t, "shared.txt", "base\n",
		func(dir string) {
			if err := os.Remove(filepath.Join(dir, "shared.txt")); err != nil {
				t.Fatal(err)
			}
		},
		func(dir string) { write(t, dir, "shared.txt", "theirs\n") },
	)
	conflict, err := svc.Conflict(ctx, "shared.txt")
	if err != nil {
		t.Fatal(err)
	}
	if conflict.HasOurs {
		t.Fatal("this branch deleted the file; there is no ours stage")
	}
	if !conflict.HasTheirs || conflict.Theirs != "theirs\n" {
		t.Fatalf("theirs stage = %+v", conflict)
	}
	if err := svc.ResolveSide(ctx, "shared.txt", "ours"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "shared.txt")); !os.IsNotExist(err) {
		t.Fatalf("taking the deleting side left the file behind: %v", err)
	}
	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Conflicted) != 0 {
		t.Fatalf("still conflicted: %v", status.Conflicted)
	}
}

// A binary conflict has nothing to hand a textarea, and says so rather than
// offering an edit that would corrupt the file.
func TestBinaryConflictOffersOnlyASide(t *testing.T) {
	ctx := context.Background()
	svc, dir := mergeConflict(t, "logo.bin", "\x00base\x00",
		func(dir string) { write(t, dir, "logo.bin", "\x00ours\x00") },
		func(dir string) { write(t, dir, "logo.bin", "\x00theirs\x00") },
	)
	conflict, err := svc.Conflict(ctx, "logo.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !conflict.Binary {
		t.Fatal("a file with NUL bytes is binary")
	}
	if conflict.Ours != "" || conflict.Theirs != "" || conflict.Merged != "" {
		t.Fatalf("binary content was handed out anyway: %+v", conflict)
	}
	// The stages still have to be reported as existing, or the panel cannot
	// tell a binary conflict from a delete/modify one.
	if !conflict.HasOurs || !conflict.HasTheirs {
		t.Fatalf("both sides have this file: %+v", conflict)
	}
	if err := svc.ResolveSide(ctx, "logo.bin", "theirs"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "logo.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "\x00theirs\x00" {
		t.Fatalf("binary bytes did not survive taking a side: %q", content)
	}
}

// A commit view is drawn from Show and RevPatch: who, when, what the parent
// was, and each file as it changed.
func TestShowAndRevPatchDescribeOneCommit(t *testing.T) {
	svc, dir := newRepo(t)
	ctx := context.Background()
	write(t, dir, "a.txt", "one\ntwo\n")
	if err := svc.Stage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(ctx, CommitInput{Message: "add a\n\nWhy a exists."}); err != nil {
		t.Fatal(err)
	}
	log, err := svc.Log(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	head, root := log[0], log[1]
	if root.Parent != "" {
		t.Fatalf("the first commit has no parent, got %q", root.Parent)
	}

	shown, err := svc.Show(ctx, head.SHA)
	if err != nil {
		t.Fatal(err)
	}
	if shown.Subject != "add a" || shown.Body != "Why a exists." || shown.Parent != root.Short {
		t.Fatalf("show = %+v, want subject, body and parent %s", shown, root.Short)
	}

	patch, err := svc.RevPatch(ctx, head.SHA, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "+one") || !strings.Contains(patch, "+two") {
		t.Fatalf("patch = %q", patch)
	}
}

// A revision reaches git as an argument, so only a hash may.
func TestShowAndRevPatchRefuseAnythingButAHash(t *testing.T) {
	svc, _ := newRepo(t)
	ctx := context.Background()
	for _, rev := range []string{"", "--output=/tmp/x", "HEAD", "main..HEAD", "abc"} {
		if _, err := svc.Show(ctx, rev); err == nil {
			t.Errorf("Show(%q) was allowed", rev)
		}
		if _, err := svc.RevPatch(ctx, rev, "README.md"); err == nil {
			t.Errorf("RevPatch(%q) was allowed", rev)
		}
	}
}
