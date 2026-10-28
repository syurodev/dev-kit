package checkpoint

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A restore that fails says whether the folder had begun to change. A caller that has to
// tell a person whether files may differ relies on this, so each case is pinned.

func lockedRestore(t *testing.T, withWrite bool) (Applied, string, error) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("a read-only directory does not stop root")
	}
	_, repo := testEngine(t, Limits{})
	write(t, repo, "kept.txt", "original\n")
	before := snap(t, repo, "cp1", SlotBefore)
	if withWrite {
		if err := os.Remove(filepath.Join(repo.WorkTree(), "kept.txt")); err != nil {
			t.Fatal(err)
		}
	}
	write(t, repo, "locked/added.txt", "new\n")
	after := snap(t, repo, "cp1", SlotAfter)

	plan, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repo.WorkTree(), "locked")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	applied, err := repo.ApplyRestore(context.Background(), plan, nil)
	return applied, repo.WorkTree(), err
}

func TestAFailedDeleteAfterAWriteIsReportedAsChanged(t *testing.T) {
	applied, work, err := lockedRestore(t, true)
	if err == nil {
		t.Fatal("the delete in a read-only directory must fail")
	}
	if !Changed(err) {
		t.Fatalf("a file was written before the failure, so the error must say the folder changed: %v", err)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("the cause must stay reachable: %v", err)
	}
	if len(applied.Written) != 1 {
		t.Fatalf("the partial result must name what was written: %+v", applied)
	}
	if _, serr := os.Stat(filepath.Join(work, "kept.txt")); serr != nil {
		t.Fatalf("the written file is there: %v", serr)
	}
}

func TestAFirstDeleteThatIsRefusedLeavesTheFolderAsItWas(t *testing.T) {
	_, work, err := lockedRestore(t, false)
	if err == nil {
		t.Fatal("the delete in a read-only directory must fail")
	}
	if Changed(err) {
		t.Fatalf("nothing was written or removed, so the folder is as it was: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(work, "locked", "added.txt")); serr != nil {
		t.Fatalf("the file must still be there: %v", serr)
	}
}

func TestAStalePlanIsRefusedBeforeAnythingChanges(t *testing.T) {
	_, repo := testEngine(t, Limits{})
	write(t, repo, "a.txt", "one\n")
	before := snap(t, repo, "cp1", SlotBefore)
	write(t, repo, "a.txt", "two\n")
	write(t, repo, "b.txt", "new\n")
	after := snap(t, repo, "cp1", SlotAfter)
	plan, err := repo.PlanRestore(context.Background(), before.Tree, after.Tree, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	write(t, repo, "b.txt", "edited after the plan\n")

	_, err = repo.ApplyRestore(context.Background(), plan, nil)
	if !errors.Is(err, ErrPlanStale) {
		t.Fatalf("want a stale plan, got %v", err)
	}
	if Changed(err) {
		t.Fatalf("a stale plan wrote nothing: %v", err)
	}
}
