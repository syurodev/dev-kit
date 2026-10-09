package codegraph

import (
	"context"
	"sync"
	"testing"
	"time"
)

// perFolderAnalyzer counts analyses per folder and can hold one open.
type perFolderAnalyzer struct {
	mu      sync.Mutex
	runs    map[string]int
	block   chan struct{}
	started chan string
	result  AnalysisResult
}

func newPerFolderAnalyzer() *perFolderAnalyzer {
	return &perFolderAnalyzer{runs: map[string]int{}, started: make(chan string, 32), result: sampleResult()}
}

func (a *perFolderAnalyzer) Analyze(ctx context.Context, path string) (AnalysisResult, error) {
	key := ProjectKey(path)
	a.mu.Lock()
	a.runs[key]++
	block := a.block
	a.mu.Unlock()
	select {
	case a.started <- key:
	default:
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return AnalysisResult{}, ctx.Err()
		}
	}
	return a.result, nil
}

func (a *perFolderAnalyzer) count(path string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runs[ProjectKey(path)]
}

func TestReindexAnalysesAFolderThatIsAlreadyOpenWhereEnqueueOnlyWaits(t *testing.T) {
	an := newPerFolderAnalyzer()
	r, _ := newTestRegistry(t, 2, an)
	log := newStatusLog(r)
	dir := t.TempDir()
	if _, err := r.Open(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Service(dir); !ok {
		t.Fatal("resident service missing")
	}
	// The open itself analysed once (and a stored-index refresh may add one).
	before := an.count(dir)

	r.Enqueue(dir)
	log.wait(t, dir)
	if an.count(dir) != before {
		t.Fatalf("Enqueue re-analysed an open folder: %d -> %d", before, an.count(dir))
	}

	log.mu.Lock()
	delete(log.done, ProjectKey(dir))
	log.mu.Unlock()
	r.Reindex(dir)
	got := log.wait(t, dir)[ProjectKey(dir)]
	if got.State != IndexIndexed {
		t.Fatalf("state = %+v", got)
	}
	if an.count(dir) != before+1 {
		t.Fatalf("Reindex must analyse an open folder again: %d -> %d", before, an.count(dir))
	}
}

func TestReindexOfSeveralFoldersKeepsGoingPastAFailure(t *testing.T) {
	bad, ok1, ok2 := t.TempDir(), t.TempDir(), t.TempDir()
	r, _ := newTestRegistry(t, 1, byPathAnalyzer{fail: map[string]error{bad: errBoom}})
	log := newStatusLog(r)
	r.Reindex(ok1, bad, ok2)
	got := log.wait(t, ok1, bad, ok2)
	if got[ProjectKey(ok1)].State != IndexIndexed || got[ProjectKey(ok2)].State != IndexIndexed {
		t.Fatalf("a failing folder stopped the batch: %+v", got)
	}
	if got[ProjectKey(bad)].State != IndexFailed {
		t.Fatalf("bad = %+v", got[ProjectKey(bad)])
	}
}

type boom struct{}

func (boom) Error() string { return "boom" }

var errBoom error = boom{}

func TestCancelIndexWithdrawsQueuedFoldersAndStopsTheRunningOne(t *testing.T) {
	an := newPerFolderAnalyzer()
	an.block = make(chan struct{})
	r, _ := newTestRegistry(t, 1, an)
	log := newStatusLog(r)
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()

	r.Reindex(a, b, c)
	select {
	case first := <-an.started:
		if first != ProjectKey(a) {
			t.Fatalf("first run = %s, want the first folder", first)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing started")
	}

	// Withdraw the one running and one still queued; the other carries on.
	r.CancelIndex(a, b)
	close(an.block)
	got := log.wait(t, c)
	if got[ProjectKey(c)].State != IndexIndexed {
		t.Fatalf("the folder that was not cancelled must finish: %+v", got[ProjectKey(c)])
	}
	if an.count(b) != 0 {
		t.Fatal("a withdrawn queued folder was still analysed")
	}
	for _, p := range []string{a, b} {
		if st := r.IndexStatus(p); st.State == IndexPending || st.State == IndexIndexing {
			t.Fatalf("%s is still %s after CancelIndex", p, st.State)
		}
	}
}

func TestBuildOutlineNestsByRangeAndKeepsOverlapsAsSiblings(t *testing.T) {
	nodes := []Node{
		{ID: "C", Label: "a.C", Kind: "class", Line: 1, EndLine: 40},
		{ID: "m1", Label: "a.C.one", Kind: "method", Line: 3, EndLine: 10},
		{ID: "m2", Label: "a.C.two", Kind: "method", Line: 12, EndLine: 30},
		{ID: "inner", Label: "a.C.two.lambda", Kind: "method", Line: 14, EndLine: 16},
		{ID: "D", Label: "a.D", Kind: "class", Line: 41, EndLine: 60},
		{ID: "skip", Label: "no line", Kind: "method", Line: 0, EndLine: 0},
		{ID: "partial", Label: "a.D.partial", Kind: "method", Line: 55, EndLine: 70},
	}
	tree := BuildOutline(nodes)
	if len(tree) != 3 || tree[0].ID != "C" || tree[1].ID != "D" || tree[2].ID != "partial" {
		t.Fatalf("top level = %+v", tree)
	}
	c := tree[0]
	if len(c.Children) != 2 || c.Children[1].ID != "m2" || len(c.Children[1].Children) != 1 || c.Children[1].Children[0].ID != "inner" {
		t.Fatalf("class children = %+v", c.Children)
	}
	if len(tree[1].Children) != 0 {
		t.Fatalf("a node that only partly overlaps D must not be its child: %+v", tree[1].Children)
	}
}

func TestStoredViewsReadTheIndexOnDiskWithoutOpeningAService(t *testing.T) {
	an := newPerFolderAnalyzer()
	r, focus := newTestRegistry(t, 2, an)
	log := newStatusLog(r)
	dir := t.TempDir()
	r.Enqueue(dir)
	log.wait(t, dir)

	if n := len(r.Resident()); n != 0 {
		t.Fatalf("resident = %d", n)
	}
	view, err := r.EntryPointsOf(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if view.State != ViewIndexed || len(view.Entries) != 1 || view.Entries[0].Target != "/orders" || view.IndexedAt == 0 {
		t.Fatalf("view = %+v", view)
	}
	outline, err := r.OutlineOf(context.Background(), dir, "Order.java")
	if err != nil {
		t.Fatal(err)
	}
	if outline.State != ViewIndexed || len(outline.Symbols) != 1 || outline.Symbols[0].ID != "n1" {
		t.Fatalf("outline = %+v", outline)
	}
	if n := len(r.Resident()); n != 0 {
		t.Fatalf("reading the views opened a service: resident = %d", n)
	}

	never := t.TempDir()
	view, _ = r.EntryPointsOf(context.Background(), never)
	if view.State != ViewNotIndexed || len(view.Entries) != 0 {
		t.Fatalf("an unindexed folder = %+v", view)
	}
	_ = focus
	if _, err := r.OutlineOf(context.Background(), dir, "../escape.java"); err == nil {
		t.Fatal("an outline path may not leave the folder")
	}
}

func TestSymbolsOfReadsTheStoredIndexOfEachFolderWithoutOpeningAny(t *testing.T) {
	an := newPerFolderAnalyzer()
	r, _ := newTestRegistry(t, 2, an)
	log := newStatusLog(r)
	a, b := t.TempDir(), t.TempDir()
	r.Enqueue(a)
	log.wait(t, a)

	got, err := r.SymbolsOf(context.Background(), a, "process")
	if err != nil || got.State != ViewIndexed || len(got.Matches) != 1 || got.Matches[0].NodeID != "n2" {
		t.Fatalf("indexed folder = %+v, %v", got, err)
	}
	none, err := r.SymbolsOf(context.Background(), b, "process")
	if err != nil || none.State != ViewNotIndexed || len(none.Matches) != 0 {
		t.Fatalf("unindexed folder = %+v, %v", none, err)
	}
	if n := len(r.Resident()); n != 0 {
		t.Fatalf("a symbol search opened %d services", n)
	}
}
