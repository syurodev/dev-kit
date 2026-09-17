package checkpoint

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Every test here runs a real git against a real directory. There is no fake:
// what is being pinned is what git does with an index, an exclude pattern and a
// symlink, and a stub of git would only pin what this package believes about it.

func testEngine(t *testing.T, limits Limits) (*Engine, *Repo) {
	t.Helper()
	git := Locate(context.Background())
	if !git.OK() {
		t.Skip("checkpoint: " + git.Reason)
	}
	engine := New(filepath.Join(t.TempDir(), "checkpoints"), git, limits)
	repo, err := engine.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return engine, repo
}

func write(t *testing.T, repo *Repo, rel, content string) {
	t.Helper()
	full := filepath.Join(repo.WorkTree(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, repo *Repo, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repo.WorkTree(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func snap(t *testing.T, repo *Repo, id, slot string) Snapshot {
	t.Helper()
	out, err := repo.Snapshot(context.Background(), id, slot)
	if err != nil {
		t.Fatalf("snapshot %s/%s: %v", id, slot, err)
	}
	return out
}

func treePaths(t *testing.T, repo *Repo, tree string) []string {
	t.Helper()
	raw, err := repo.git(context.Background(), nil, "ls-tree", "-r", "-z", "--name-only", tree)
	if err != nil {
		t.Fatalf("ls-tree: %v", err)
	}
	return splitNUL(raw)
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func skipReason(snapshot Snapshot, path string) SkipReason {
	for _, skip := range snapshot.Skipped {
		if skip.Path == path {
			return skip.Reason
		}
	}
	return ""
}

// The deny list is requirement (3): a project whose .gitignore says nothing
// about node_modules — or actively re-includes it — still does not get
// node_modules in a snapshot.
//
// Re-including takes two lines, because git will not un-ignore a file whose
// parent directory is ignored; the two-line form is the strongest thing a
// project can say, and `-x` has to beat it.
func TestDenyListBeatsGitignoreNegation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		gitignore string
	}{
		{"silent", "*.log\n"},
		{"re-includes the directory", "node_modules/\n!node_modules/\n!node_modules/keep.js\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, repo := testEngine(t, Limits{})
			write(t, repo, ".gitignore", tc.gitignore)
			write(t, repo, "node_modules/keep.js", "module.exports = 1\n")
			write(t, repo, "node_modules/deep/other.js", "x\n")
			write(t, repo, "src/app.ts", "export const a = 1\n")

			first := snap(t, repo, "cp1", SlotBefore)
			paths := treePaths(t, repo, first.Tree)
			if !contains(paths, "src/app.ts") {
				t.Fatalf("source file missing from tree: %v", paths)
			}
			for _, path := range paths {
				if strings.HasPrefix(path, "node_modules/") {
					t.Errorf("node_modules leaked into the tree: %s", path)
				}
			}
			// Not merely absent from the tree — absent from the listing. An
			// excluded directory is one git never descends into, and that is
			// where the 0.04s per turn comes from: filtering in Go afterwards
			// would give the same tree at the cost of walking every dependency
			// on every snapshot. A path that reached the Go filter shows up as
			// a skip, so an empty skip list is the proof git stopped at the
			// directory.
			if len(first.Skipped) != 0 {
				t.Errorf("git walked into an excluded directory: %+v", first.Skipped)
			}
		})
	}
}

// A path that reached the index before it was denied — or by a route -x does
// not cover, since -x only filters untracked files — has to be forced back out,
// not merely left un-added.
func TestDeniedPathAlreadyTrackedIsRemoved(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "node_modules/x.js", "old\n")
	// Put it in the index behind the filter's back, the way an older build of
	// the engine would have.
	if _, err := repo.git(context.Background(), joinNUL([]string{"node_modules/x.js"}),
		"update-index", "-z", "--add", "--stdin"); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "node_modules/x.js", "new\n")

	out := snap(t, repo, "cp1", SlotBefore)
	if contains(treePaths(t, repo, out.Tree), "node_modules/x.js") {
		t.Fatal("a tracked denied path stayed in the tree")
	}
	if got := skipReason(out, "node_modules/x.js"); got != SkipExcluded {
		t.Fatalf("skip reason = %q, want %q", got, SkipExcluded)
	}
}

// The size ceiling has to hold for a file that was small when it was first
// checkpointed, or a restore would put the old small version back over a large
// file nobody ever snapshotted.
func TestOversizedTrackedFileLeavesTheTree(t *testing.T) {
	_, repo := testEngine(t, Limits{MaxFileBytes: 64})
	write(t, repo, "data.bin", "small\n")
	first := snap(t, repo, "cp1", SlotBefore)
	if !contains(treePaths(t, repo, first.Tree), "data.bin") {
		t.Fatal("small file should be in the first tree")
	}

	write(t, repo, "data.bin", strings.Repeat("A", 200))
	second := snap(t, repo, "cp2", SlotBefore)
	if contains(treePaths(t, repo, second.Tree), "data.bin") {
		t.Fatal("a file that grew past the ceiling stayed in the tree")
	}
	if got := skipReason(second, "data.bin"); got != SkipTooLarge {
		t.Fatalf("skip reason = %q, want %q", got, SkipTooLarge)
	}
}

// A directory with its own .git belongs to that repository's history. Snapshot
// must report it rather than either recursing into it or silently dropping it.
func TestNestedRepositoryIsSkippedAndReported(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "top.txt", "hi\n")
	sub := filepath.Join(repo.WorkTree(), "vendored")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", sub).CombinedOutput(); err != nil {
		t.Fatalf("git init sub: %v: %s", err, out)
	}
	write(t, repo, "vendored/inner.txt", "inner\n")

	out := snap(t, repo, "cp1", SlotBefore)
	for _, path := range treePaths(t, repo, out.Tree) {
		if strings.HasPrefix(path, "vendored") {
			t.Errorf("nested repository leaked into the tree: %s", path)
		}
	}
	if got := skipReason(out, "vendored"); got != SkipNestedRepo {
		t.Fatalf("skip reason = %q, want %q (skips: %+v)", got, SkipNestedRepo, out.Skipped)
	}
}

// The ceiling that says "this project is too large" must be a named error the
// UI can render, not a git failure.
func TestFileCeilingReportsProjectTooLarge(t *testing.T) {
	_, repo := testEngine(t, Limits{MaxFiles: 3})
	for _, name := range []string{"a", "b", "c", "d"} {
		write(t, repo, name+".txt", name)
	}
	_, err := repo.Snapshot(context.Background(), "cp1", SlotBefore)
	if err == nil || !strings.Contains(err.Error(), ErrProjectTooLarge.Error()) {
		t.Fatalf("err = %v, want %v", err, ErrProjectTooLarge)
	}
}

// Nothing the app inherited may point git at another repository. A GIT_DIR left
// in the environment by the terminal the app was launched from is the realistic
// version of this, and it would write to the user's own index.
func TestInheritedGitEnvDoesNotLeakIn(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "a.txt", "one\n")
	snap(t, repo, "cp0", SlotBefore)

	decoy := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", decoy).CombinedOutput(); err != nil {
		t.Fatalf("git init decoy: %v: %s", err, out)
	}
	decoyIndex := filepath.Join(decoy, ".git", "index")
	t.Setenv("GIT_DIR", filepath.Join(decoy, ".git"))
	t.Setenv("GIT_WORK_TREE", decoy)
	t.Setenv("GIT_INDEX_FILE", decoyIndex)

	write(t, repo, "a.txt", "two\n")
	out := snap(t, repo, "cp1", SlotAfter)
	if !contains(treePaths(t, repo, out.Tree), "a.txt") {
		t.Fatal("snapshot did not see the project")
	}
	if _, err := os.Stat(decoyIndex); !os.IsNotExist(err) {
		t.Fatalf("the inherited GIT_INDEX_FILE was written: %v", err)
	}
}

// A command that runs past its deadline has to die and report, not hold the
// turn open. The caller's own deadline is what turn boundaries will use.
func TestSnapshotHonoursTheCallerDeadline(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "a.txt", "one\n")

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	started := time.Now()
	_, err := repo.Snapshot(ctx, "cp1", SlotBefore)
	if err == nil {
		t.Fatal("expected the expired deadline to fail the snapshot")
	}
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Fatalf("snapshot took %s after its deadline passed", elapsed)
	}
}

// Snapshots for one project share an index file, so two at once would
// interleave update-index runs. The mutex is what makes concurrent turns in one
// project safe; without it this test corrupts the index or races write-tree.
func TestConcurrentSnapshotsSerialise(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	for i := range 20 {
		write(t, repo, "f"+string(rune('a'+i))+".txt", "content")
	}
	errs := make(chan error, 8)
	for i := range 8 {
		go func() {
			_, err := repo.Snapshot(context.Background(), "cp"+string(rune('a'+i)), SlotBefore)
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent snapshot: %v", err)
		}
	}
}

// Two sessions may be pointed at the same project spelled differently — the
// path as typed and the path with symlinks resolved. One shadow repository, or
// each would miss the other's writes.
func TestProjectKeyResolvesSymlinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	direct, _, err := ProjectKey(real)
	if err != nil {
		t.Fatal(err)
	}
	viaLink, resolved, err := ProjectKey(link)
	if err != nil {
		t.Fatal(err)
	}
	if direct != viaLink {
		t.Fatalf("keys differ: %s vs %s", direct, viaLink)
	}
	if want, _ := filepath.EvalSymlinks(real); resolved != want {
		t.Fatalf("resolved = %s, want %s", resolved, want)
	}
}

// The diff is what the turn header counts and what a restore acts on, so each
// kind has to be distinguishable and the line counts real. Nothing in
// info/attributes may turn text files binary.
func TestDiffReportsKindsModesAndLineCounts(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "keep.txt", "one\ntwo\n")
	write(t, repo, "gone.txt", "bye\n")
	before := snap(t, repo, "cp1", SlotBefore)

	write(t, repo, "keep.txt", "one\ntwo\nthree\n")
	if err := os.Remove(filepath.Join(repo.WorkTree(), "gone.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "new.sh", "#!/bin/sh\necho hi\n")
	if err := os.Chmod(filepath.Join(repo.WorkTree(), "new.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.WorkTree(), "blob.bin"), []byte{0, 1, 2, 0, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	after := snap(t, repo, "cp1", SlotAfter)

	changes, err := repo.Diff(context.Background(), before.Tree, after.Tree)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]Change{}
	for _, change := range changes {
		byPath[change.Path] = change
	}
	if got := byPath["keep.txt"]; got.Kind != ChangeModified || got.Added != 1 || got.Removed != 0 {
		t.Errorf("keep.txt = %+v, want modified +1 -0", got)
	}
	if got := byPath["gone.txt"]; got.Kind != ChangeDeleted {
		t.Errorf("gone.txt = %+v, want deleted", got)
	}
	if got := byPath["new.sh"]; got.Kind != ChangeAdded || got.Mode != "100755" {
		t.Errorf("new.sh = %+v, want added mode 100755", got)
	}
	if got := byPath["blob.bin"]; !got.Binary() {
		t.Errorf("blob.bin = %+v, want binary", got)
	}
}

// A directory that becomes a symlink — or the reverse — is a path that exists
// in the index as `pkg/file` and on disk as `pkg`. git refuses to hold both, so
// the stale entries have to be dropped before anything is added; without that
// the snapshot fails and the turn loses its checkpoint entirely.
func TestDirectoryBecomingASymlinkStillSnapshots(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "pkg/file.txt", "inside\n")
	snap(t, repo, "cp1", SlotBefore)

	if err := os.RemoveAll(filepath.Join(repo.WorkTree(), "pkg")); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "elsewhere.txt", "target\n")
	if err := os.Symlink("elsewhere.txt", filepath.Join(repo.WorkTree(), "pkg")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	after := snap(t, repo, "cp1", SlotAfter)
	paths := treePaths(t, repo, after.Tree)
	if contains(paths, "pkg/file.txt") {
		t.Error("the replaced directory's file stayed in the tree")
	}
	if !contains(paths, "pkg") {
		t.Errorf("the new symlink is missing from the tree: %v", paths)
	}

	// And back again.
	if err := os.Remove(filepath.Join(repo.WorkTree(), "pkg")); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "pkg/file.txt", "inside again\n")
	back := snap(t, repo, "cp2", SlotAfter)
	paths = treePaths(t, repo, back.Tree)
	if contains(paths, "pkg") || !contains(paths, "pkg/file.txt") {
		t.Errorf("tree after the reverse type change = %v", paths)
	}
}

// Settings can add patterns of its own — a project with a large generated
// directory the built-in list does not know about. They have to reach git with
// the same top priority as the built-in ones, or a .gitignore that tracks the
// directory would keep putting it in every tree.
func TestExtraExcludesFromSettingsApply(t *testing.T) {
	_, repo := testEngine(t, Limits{ExtraExcludes: []string{"generated/", "*.snapshot"}})
	write(t, repo, "generated/schema.ts", "export type A = 1\n")
	write(t, repo, "src/main.ts", "1\n")
	write(t, repo, "src/big.snapshot", "blob\n")

	out := snap(t, repo, "cp1", SlotBefore)
	paths := treePaths(t, repo, out.Tree)
	if !contains(paths, "src/main.ts") {
		t.Fatalf("source file missing: %v", paths)
	}
	for _, path := range paths {
		if strings.HasPrefix(path, "generated/") || strings.HasSuffix(path, ".snapshot") {
			t.Errorf("a Settings exclude leaked into the tree: %s", path)
		}
	}
}

// The patterns handed to git are gitignore syntax, and a directory pattern has
// to end in a slash or it would also match a file of that name.
func TestDenyPatternsAreDirectoryShaped(t *testing.T) {
	patterns := denyPatterns([]string{" spaced/ ", "", "   "})
	if !contains(patterns, "node_modules/") || !contains(patterns, ".DS_Store") {
		t.Fatalf("patterns = %v", patterns)
	}
	if !contains(patterns, "spaced/") {
		t.Errorf("a Settings pattern was not trimmed: %v", patterns)
	}
	for _, pattern := range patterns {
		if pattern == "" || strings.TrimSpace(pattern) != pattern {
			t.Errorf("empty or padded pattern %q would match everything", pattern)
		}
	}
}

// A turn's opening snapshot has a budget, and another session in the same
// project holding the lock must not spend it. A sync.Mutex cannot be given up,
// so waiting for the lock has to honour the caller's deadline the same way
// running git does.
func TestWaitingForTheProjectLockHonoursTheDeadline(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "a.txt", "one\n")

	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = repo.runLocked(context.Background(), func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := repo.Snapshot(ctx, "cp1", SlotBefore)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("expected the snapshot to give up on the lock")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline error", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("waited %s for a lock it should have given up on", elapsed)
	}
}
