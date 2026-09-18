package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeRecords stands in for Phase D's store. Maintenance is the one place the
// engine has to agree with the table about what still exists, so what is being
// pinned here is that agreement, not SQL.
type fakeRecords struct {
	rows    []Record
	deleted []string
}

func (f *fakeRecords) ListCheckpoints(context.Context) ([]Record, error) {
	return append([]Record(nil), f.rows...), nil
}

func (f *fakeRecords) DeleteCheckpoints(_ context.Context, ids []string) error {
	f.deleted = append(f.deleted, ids...)
	gone := map[string]bool{}
	for _, id := range ids {
		gone[id] = true
	}
	kept := f.rows[:0]
	for _, row := range f.rows {
		if !gone[row.ID] {
			kept = append(kept, row)
		}
	}
	f.rows = kept
	return nil
}

func refExists(t *testing.T, repo *Repo, id, slot string) bool {
	t.Helper()
	raw, err := repo.git(context.Background(), nil, "for-each-ref", "--format=%(refname)", "refs/devkit/cp/")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(raw), RefName(id, slot))
}

// The three retention rules, each on its own record, decided purely on the
// stored rows.
func TestExpiredCheckpointsAppliesEveryRule(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	limits := Limits{RetentionDays: 30, MaxPerSession: 2, MaxProjectBytes: 1 << 40}.withDefaults()

	rows := []Record{
		{ID: "old", SessionID: "s1", ProjectKey: "p", Kind: "turn", CreatedAt: now.Add(-31 * 24 * time.Hour)},
		{ID: "stale-safety", SessionID: "s1", ProjectKey: "p", Kind: "restore_safety", CreatedAt: now.Add(-8 * 24 * time.Hour)},
		{ID: "fresh-safety", SessionID: "s1", ProjectKey: "p", Kind: "rewind_safety", CreatedAt: now.Add(-1 * time.Hour)},
		{ID: "turn-1", SessionID: "s1", ProjectKey: "p", Kind: "turn", CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "turn-2", SessionID: "s1", ProjectKey: "p", Kind: "turn", CreatedAt: now.Add(-2 * time.Hour)},
		// s2 has its own budget: the per-session cap is per session.
		{ID: "other-1", SessionID: "s2", ProjectKey: "p", Kind: "turn", CreatedAt: now.Add(-5 * time.Hour)},
		{ID: "other-2", SessionID: "s2", ProjectKey: "p", Kind: "turn", CreatedAt: now.Add(-4 * time.Hour)},
	}
	got := expiredCheckpoints(rows, now, t.TempDir(), limits)
	sort.Strings(got)
	want := []string{"old", "stale-safety", "turn-1"} // turn-1 is the third-newest of s1's three survivors
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expired = %v, want %v", got, want)
	}
}

// A ref is what stops gc collecting a tree, so the store and the refs have to
// stay in step: an expired checkpoint's refs go, a surviving one's stay, and a
// gc afterwards must leave the surviving tree readable.
func TestMaintainDropsOrphanRefsAndKeepsLiveTrees(t *testing.T) {
	engine, repo := testEngine(t, Limits{})
	ctx := context.Background()

	write(t, repo, "a.txt", "one\n")
	keep := snap(t, repo, "keep", SlotBefore)
	write(t, repo, "a.txt", "two\n")
	snap(t, repo, "drop", SlotBefore)

	now := time.Now()
	records := &fakeRecords{rows: []Record{
		{ID: "keep", SessionID: "s1", ProjectKey: repo.Key(), Kind: "turn", CreatedAt: now},
		{ID: "drop", SessionID: "s1", ProjectKey: repo.Key(), Kind: "turn", CreatedAt: now.Add(-90 * 24 * time.Hour)},
	}}

	report, err := engine.Maintain(ctx, records)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Expired) != 1 || report.Expired[0] != "drop" {
		t.Fatalf("expired = %v, want [drop]", report.Expired)
	}
	if refExists(t, repo, "drop", SlotBefore) {
		t.Error("the expired checkpoint's ref survived")
	}
	if !refExists(t, repo, "keep", SlotBefore) {
		t.Error("the surviving checkpoint's ref was dropped")
	}
	// The ref is the only thing holding the tree; prove it holds.
	if _, err := repo.git(ctx, nil, "gc", "--prune=now", "--quiet"); err != nil {
		t.Fatal(err)
	}
	if paths := treePaths(t, repo, keep.Tree); !contains(paths, "a.txt") {
		t.Fatalf("the live tree did not survive gc: %v", paths)
	}
}

// Disk only comes back when the directory does. A project with no rows left has
// nothing to show and nothing to restore, so its shadow repository goes.
func TestMaintainRemovesAShadowRepositoryWithNoRowsLeft(t *testing.T) {
	engine, repo := testEngine(t, Limits{})
	write(t, repo, "a.txt", "one\n")
	snap(t, repo, "only", SlotBefore)
	shadow := repo.GitDir()
	if _, err := os.Stat(shadow); err != nil {
		t.Fatal(err)
	}

	records := &fakeRecords{}
	report, err := engine.Maintain(context.Background(), records)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.RemovedProjects) != 1 || report.RemovedProjects[0] != repo.Key() {
		t.Fatalf("removed = %v, want [%s]", report.RemovedProjects, repo.Key())
	}
	if _, err := os.Stat(shadow); !os.IsNotExist(err) {
		t.Fatalf("the shadow repository survived: %v", err)
	}
	// And a project that comes back gets a working repository, not a handle to
	// the directory that was just deleted.
	reopened, err := engine.Open(context.Background(), repo.WorkTree())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Snapshot(context.Background(), "again", SlotBefore); err != nil {
		t.Fatalf("snapshot after the directory was reclaimed: %v", err)
	}
}

// The marker file is how a shadow directory names its project — without it
// maintenance cannot open the repository to sweep its refs.
func TestShadowDirectoryRecordsItsProject(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	raw, err := os.ReadFile(filepath.Join(repo.GitDir(), projectFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != repo.WorkTree() {
		t.Fatalf("marker = %q, want %q", strings.TrimSpace(string(raw)), repo.WorkTree())
	}
	info, err := os.Stat(repo.GitDir())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("shadow directory mode = %v, want 0700", info.Mode().Perm())
	}
}

// The per-project soft cap deletes what it must and no more.
//
// The share subtracted per deletion has to be a share of what is actually on
// disk. Using a share of the cap instead made the amount of history deleted
// depend on the limit rather than on the usage: a directory only just over the
// line with many checkpoints lost nearly all of them.
func TestSoftCapDeletesOnlyWhatItMustGo(t *testing.T) {
	root := t.TempDir()
	const key = "project-key"
	dir := filepath.Join(root, key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// 2 MiB on disk against a 1 MiB cap: half has to go, not nearly all of it.
	if err := os.WriteFile(filepath.Join(dir, "pack"), make([]byte, 2<<20), 0o600); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	limits := Limits{MaxProjectBytes: 1 << 20}.withDefaults()
	rows := make([]Record, 0, 20)
	for i := range 20 {
		rows = append(rows, Record{
			ID:         "cp" + strconv.Itoa(i),
			SessionID:  "s1",
			ProjectKey: key,
			Kind:       "turn",
			CreatedAt:  now.Add(-time.Duration(20-i) * time.Minute),
		})
	}

	expired := expiredCheckpoints(rows, now, root, limits)
	// A range, not a count: each deletion frees an estimated twentieth of 2 MiB
	// and integer division leaves a remainder, so the exact number is an
	// artefact of the arithmetic rather than the behaviour. What is being
	// pinned is the proportion — halving the directory takes about half the
	// checkpoints. Subtracting a share of the CAP instead needs nineteen of the
	// twenty, which is what this range refuses.
	if len(expired) < 8 || len(expired) > 12 {
		t.Fatalf("expired %d of 20, want about half: %v", len(expired), expired)
	}
	// And they are the oldest, so the undo history the user is most likely to
	// reach for survives.
	if expired[0] != "cp0" {
		t.Fatalf("expired = %v, want the oldest first", expired)
	}
	for _, id := range expired {
		if id == "cp19" {
			t.Fatal("the newest checkpoint was deleted for size")
		}
	}
}

// Under the cap, nothing is deleted for size at all.
func TestSoftCapLeavesAProjectUnderTheLineAlone(t *testing.T) {
	root := t.TempDir()
	const key = "small-project"
	if err := os.MkdirAll(filepath.Join(root, key), 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	rows := []Record{
		{ID: "a", SessionID: "s1", ProjectKey: key, Kind: "turn", CreatedAt: now.Add(-time.Hour)},
		{ID: "b", SessionID: "s1", ProjectKey: key, Kind: "turn", CreatedAt: now},
	}
	if expired := expiredCheckpoints(rows, now, root, DefaultLimits()); len(expired) != 0 {
		t.Fatalf("expired = %v, want none", expired)
	}
}

// However far over the line a project is, it keeps one checkpoint.
//
// The estimate can be wrong — objects are shared between trees, so deleting a
// checkpoint may free nothing — and a wrong estimate must not be able to empty
// a project's undo history entirely. Something to go back to beats an accurate
// directory size.
func TestSoftCapNeverDeletesEveryCheckpointOfAProject(t *testing.T) {
	root := t.TempDir()
	const key = "enormous"
	dir := filepath.Join(root, key)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Two hundred times the cap: no number of deletions brings the estimate
	// under it, so only the guard stops the loop.
	if err := os.WriteFile(filepath.Join(dir, "pack"), make([]byte, 8<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	limits := Limits{MaxProjectBytes: 40 << 10}.withDefaults()

	rows := []Record{
		{ID: "old", SessionID: "s1", ProjectKey: key, Kind: "turn", CreatedAt: now.Add(-2 * time.Minute)},
		{ID: "mid", SessionID: "s1", ProjectKey: key, Kind: "turn", CreatedAt: now.Add(-time.Minute)},
		{ID: "newest", SessionID: "s1", ProjectKey: key, Kind: "turn", CreatedAt: now},
	}
	expired := expiredCheckpoints(rows, now, root, limits)
	if len(expired) != 2 {
		t.Fatalf("expired = %v, want all but one", expired)
	}
	for _, id := range expired {
		if id == "newest" {
			t.Fatal("the last checkpoint a project has was deleted for size")
		}
	}
}
