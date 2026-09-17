package checkpoint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func conflictOn(plan RestorePlan, path string) (Conflict, bool) {
	for _, conflict := range plan.Conflicts {
		if conflict.Path == path {
			return conflict, true
		}
	}
	return Conflict{}, false
}

func actionOn(plan RestorePlan, path string) (Action, bool) {
	for _, action := range plan.Actions {
		if action.Path == path {
			return action, true
		}
	}
	return Action{}, false
}

// A restore has to put the file back as it was, not a re-derived version of it:
// a symlink stays a symlink, an executable stays executable.
func TestRestoreKeepsSymlinksAndExecutableBit(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "target.txt", "payload\n")
	write(t, repo, "run.sh", "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(repo.WorkTree(), "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(repo.WorkTree(), "link")
	if err := os.Symlink("target.txt", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	before := snap(t, repo, "cp1", SlotBefore)

	// The agent flattens the symlink and drops the executable bit.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "link", "not a link any more\n")
	if err := os.Chmod(filepath.Join(repo.WorkTree(), "run.sh"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := snap(t, repo, "cp1", SlotAfter)

	plan, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApplyRestore(context.Background(), plan, nil); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink came back as a regular file")
	}
	if target, err := os.Readlink(link); err != nil || target != "target.txt" {
		t.Errorf("readlink = %q, %v; want target.txt", target, err)
	}
	runInfo, err := os.Stat(filepath.Join(repo.WorkTree(), "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if runInfo.Mode()&0o111 == 0 {
		t.Error("the executable bit was not restored")
	}
}

// A project's own .gitattributes must not touch a checkpoint. `text=auto
// eol=crlf` would rewrite every line ending on the way out, and `filter=lfs`
// would send the content through a filter that is not configured here — either
// one turns "restore what the agent wrote" into "restore something like it".
func TestRestoreIsByteExactUnderProjectGitattributes(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	// Two conversions the project can switch on with attributes alone, needing
	// no config this repository would ever have: eol=crlf rewrites line endings
	// on checkout, and ident expands $Id$ into the blob's hash.
	write(t, repo, ".gitattributes", "* text=auto eol=crlf ident\n*.bin filter=lfs diff=lfs merge=lfs\n")
	sources := map[string]string{
		"text.txt":  "alpha\nbeta\ngamma\n",
		"stamp.txt": "header $Id$ footer\n",
		// Bytes git would call binary either way, so this one pins the plain
		// round trip rather than the attributes.
		"mixed.bin": "alpha\nbeta\r\ngamma\n\x00trailing",
	}
	for path, content := range sources {
		write(t, repo, path, content)
	}
	before := snap(t, repo, "cp1", SlotBefore)

	for path := range sources {
		write(t, repo, path, "clobbered")
	}
	after := snap(t, repo, "cp1", SlotAfter)

	plan, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApplyRestore(context.Background(), plan, nil); err != nil {
		t.Fatal(err)
	}
	for path, want := range sources {
		if got := read(t, repo, path); got != want {
			t.Errorf("%s restored as %q, want %q", path, got, want)
		}
	}
}

// A directory that became a symlink is a change the user made. Following it
// would write through it; git's own answer is to replace the symlink with a
// real directory, which unmakes that change silently. Neither is acceptable, so
// the path becomes a conflict the user cannot override.
func TestRestoreRefusesToWriteThroughASymlinkedDirectory(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "pkg/file.txt", "original\n")
	before := snap(t, repo, "cp1", SlotBefore)
	write(t, repo, "pkg/file.txt", "changed\n")
	after := snap(t, repo, "cp1", SlotAfter)

	outside := t.TempDir()
	if err := os.RemoveAll(filepath.Join(repo.WorkTree(), "pkg")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo.WorkTree(), "pkg")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	plan, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	conflict, ok := conflictOn(plan, "pkg/file.txt")
	if !ok || conflict.Reason != ConflictUnsafePath {
		t.Fatalf("conflicts = %+v, want pkg/file.txt unsafe_path", plan.Conflicts)
	}
	if conflict.Reason.Overridable() {
		t.Error("an unsafe path must not be overridable")
	}
	// Even asked to force it, nothing may be written.
	if _, err := repo.ApplyRestore(context.Background(), plan, []string{"pkg/file.txt"}); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("wrote through the symlink: %v %v", entries, err)
	}
}

// A path no tree holds the content of must never be restored. The tree says
// "absent", and acting on that would delete a file nobody checkpointed.
func TestRestoreNeverTouchesASkippedPath(t *testing.T) {
	_, repo := testEngine(t, Limits{MaxFileBytes: 64})
	write(t, repo, "big.bin", "small\n")
	write(t, repo, "other.txt", "one\n")
	before := snap(t, repo, "cp1", SlotBefore)

	grown := make([]byte, 200)
	for i := range grown {
		grown[i] = 'Z'
	}
	if err := os.WriteFile(filepath.Join(repo.WorkTree(), "big.bin"), grown, 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "other.txt", "two\n")
	after := snap(t, repo, "cp1", SlotAfter)

	plan, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	conflict, ok := conflictOn(plan, "big.bin")
	if !ok || conflict.Reason != ConflictProtected {
		t.Fatalf("conflicts = %+v, want big.bin protected", plan.Conflicts)
	}
	if _, ok := actionOn(plan, "other.txt"); !ok {
		t.Fatalf("actions = %+v, want other.txt", plan.Actions)
	}
	if _, err := repo.ApplyRestore(context.Background(), plan, []string{"big.bin"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "big.bin"); got != string(grown) {
		t.Fatal("the skipped file was rewritten")
	}
	if got := read(t, repo, "other.txt"); got != "one\n" {
		t.Fatalf("other.txt = %q, want the restored content", got)
	}
}

// Deleting a file can empty the directory that held it, and that directory
// should go too — but only that one. A directory still holding anything at all,
// including something the engine never snapshotted, stays.
func TestRestoreRemovesOnlyTheDirectoriesItEmptied(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "keep/kept.txt", "stay\n")
	before := snap(t, repo, "cp1", SlotBefore)

	write(t, repo, "fresh/added.txt", "new\n")
	write(t, repo, "keep/extra.txt", "new\n")
	write(t, repo, "mixed/added.txt", "new\n")
	after := snap(t, repo, "cp1", SlotAfter)
	// An ignored file git never saw, in a directory the restore would otherwise
	// empty.
	write(t, repo, "mixed/.DS_Store", "junk")

	plan, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ApplyRestore(context.Background(), plan, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo.WorkTree(), "fresh")); !os.IsNotExist(err) {
		t.Errorf("the emptied directory survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.WorkTree(), "keep", "kept.txt")); err != nil {
		t.Errorf("a directory with surviving content was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo.WorkTree(), "mixed", ".DS_Store")); err != nil {
		t.Errorf("a directory holding a skipped file was removed: %v", err)
	}
}

// The check that makes a restore safe: a file the user edited since the last
// checkpoint is a conflict, skipped by default and only written when the user
// says so.
func TestRestoreReportsAndCanOverrideAConflict(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "file.txt", "one\n")
	before := snap(t, repo, "cp1", SlotBefore)
	write(t, repo, "file.txt", "two\n")
	after := snap(t, repo, "cp1", SlotAfter)

	// The user's own edit, after the turn, that DevKit never snapshotted.
	write(t, repo, "file.txt", "three\n")

	plan, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	conflict, ok := conflictOn(plan, "file.txt")
	if !ok || conflict.Reason != ConflictChangedSince {
		t.Fatalf("conflicts = %+v, want file.txt changed_since", plan.Conflicts)
	}
	if !plan.Empty() {
		t.Fatalf("actions = %+v, want none", plan.Actions)
	}
	if _, err := repo.ApplyRestore(context.Background(), plan, nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "file.txt"); got != "three\n" {
		t.Fatalf("file.txt = %q, want the user's edit left alone", got)
	}
	if _, err := repo.ApplyRestore(context.Background(), plan, []string{"file.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "file.txt"); got != "one\n" {
		t.Fatalf("file.txt = %q, want the forced restore", got)
	}
}

// A dialog sits between planning and applying. Anything that moved in between
// makes the answer the user read wrong, so the apply stops rather than acting
// on it.
func TestApplyRefusesAStalePlan(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "file.txt", "one\n")
	before := snap(t, repo, "cp1", SlotBefore)
	write(t, repo, "file.txt", "two\n")
	after := snap(t, repo, "cp1", SlotAfter)

	plan, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := actionOn(plan, "file.txt"); !ok {
		t.Fatalf("actions = %+v, want file.txt", plan.Actions)
	}

	// While the dialog was open.
	write(t, repo, "file.txt", "three\n")

	if _, err := repo.ApplyRestore(context.Background(), plan, nil); !errors.Is(err, ErrPlanStale) {
		t.Fatalf("err = %v, want ErrPlanStale", err)
	}
	if got := read(t, repo, "file.txt"); got != "three\n" {
		t.Fatalf("file.txt = %q, want it untouched", got)
	}
}

// macOS hands back decomposed filenames. Without core.precomposeunicode the
// same file enters the index under a different name on each snapshot, so every
// turn reports it as both added and deleted and a restore would delete it.
func TestDecomposedFilenamesRoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("normalisation only differs on darwin")
	}
	_, repo := testEngine(t, Limits{})
	write(t, repo, "café.txt", "one\n") // precomposed é
	before := snap(t, repo, "cp1", SlotBefore)
	write(t, repo, "café.txt", "two\n")
	after := snap(t, repo, "cp1", SlotAfter)

	changes, err := repo.Diff(context.Background(), before.Tree, after.Tree)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Kind != ChangeModified {
		t.Fatalf("changes = %+v, want one modification", changes)
	}
}

// "Undo this turn" and "go back to before this turn" target the same tree and
// check against the same expectation. The only difference is which paths are in
// scope, so narrowing has to be exact: a path outside the scope must be left
// alone even though it differs from the target.
func TestPlanRestoreNarrowsToTheGivenPaths(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "mine.txt", "one\n")
	write(t, repo, "theirs.txt", "one\n")
	before := snap(t, repo, "cp1", SlotBefore)

	write(t, repo, "mine.txt", "two\n")
	write(t, repo, "theirs.txt", "two\n")
	after := snap(t, repo, "cp1", SlotAfter)

	narrowed, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, []string{"mine.txt"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(narrowed.Actions) != 1 || narrowed.Actions[0].Path != "mine.txt" {
		t.Fatalf("actions = %+v, want only mine.txt", narrowed.Actions)
	}

	// nil is every path that differs — what "go back to before" asks for.
	wide, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(wide.Actions) != 2 {
		t.Fatalf("actions = %+v, want both", wide.Actions)
	}

	if _, err := repo.ApplyRestore(context.Background(), narrowed, nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "mine.txt"); got != "one\n" {
		t.Errorf("mine.txt = %q, want restored", got)
	}
	if got := read(t, repo, "theirs.txt"); got != "two\n" {
		t.Errorf("theirs.txt = %q, want untouched", got)
	}
}

// An empty (non-nil) scope is "no paths", not "every path". The two spellings
// mean opposite things and a restore that read one as the other would rewrite
// the whole project instead of nothing.
func TestPlanRestoreTreatsAnEmptyScopeAsNothing(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "a.txt", "one\n")
	before := snap(t, repo, "cp1", SlotBefore)
	write(t, repo, "a.txt", "two\n")
	after := snap(t, repo, "cp1", SlotAfter)

	plan, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, []string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Empty() || len(plan.Conflicts) != 0 {
		t.Fatalf("plan = %+v, want nothing", plan)
	}
}

// A file a diff drawer would choke on comes back as "too large", not as half of
// itself: half a file draws a difference at the cut that is not in the file.
// The size is asked for first, so a huge blob is never read to be discarded.
func TestFileContentRefusesAFileOverTheCeiling(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	small := "one\ntwo\n"
	write(t, repo, "small.txt", small)
	write(t, repo, "big.txt", strings.Repeat("x", 4096))
	snapshot := snap(t, repo, "cp1", SlotBefore)

	body, tooLarge, err := repo.FileContent(context.Background(), snapshot.Tree, "small.txt", 1024)
	if err != nil || tooLarge {
		t.Fatalf("small.txt = %v, tooLarge=%v", err, tooLarge)
	}
	if string(body) != small {
		t.Fatalf("small.txt = %q, want %q", body, small)
	}

	body, tooLarge, err = repo.FileContent(context.Background(), snapshot.Tree, "big.txt", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !tooLarge {
		t.Fatal("a file over the ceiling came back as content")
	}
	if len(body) != 0 {
		t.Fatalf("a refused file still returned %d bytes", len(body))
	}

	// A ceiling of zero is no ceiling, for the callers that want the bytes
	// whatever the size.
	body, tooLarge, err = repo.FileContent(context.Background(), snapshot.Tree, "big.txt", 0)
	if err != nil || tooLarge || len(body) != 4096 {
		t.Fatalf("unbounded read = %d bytes, tooLarge=%v, %v", len(body), tooLarge, err)
	}
}
