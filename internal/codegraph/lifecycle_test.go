package codegraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// blockingAnalyzer parks in Analyze until released or its context ends, and
// reports each entry — the shape of a JVM analyzing a large project.
type blockingAnalyzer struct {
	entered   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
	result    AnalysisResult
}

func newBlockingAnalyzer() blockingAnalyzer {
	return blockingAnalyzer{
		entered:   make(chan struct{}, 8),
		cancelled: make(chan struct{}, 8),
		release:   make(chan struct{}),
		result:    sampleResult(),
	}
}

func (b blockingAnalyzer) Analyze(ctx context.Context, _ string) (AnalysisResult, error) {
	b.entered <- struct{}{}
	select {
	case <-b.release:
		return b.result, nil
	case <-ctx.Done():
		b.cancelled <- struct{}{}
		return AnalysisResult{}, ctx.Err()
	}
}

func TestWriteFileRefusesASaveMeantForAnotherProject(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(t.Context(), dirA); err != nil {
		t.Fatal(err)
	}
	// The editor opened notes.md under A; an agent then switches to B.
	if err := svc.OpenProject(t.Context(), dirB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })

	err := svc.WriteFile(t.Context(), "notes.md", "meant for A", dirA)
	if !errors.Is(err, ErrProjectChanged) {
		t.Fatalf("save into a switched project = %v, want ErrProjectChanged", err)
	}
	if _, statErr := os.Stat(filepath.Join(dirB, "notes.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the refused save still created a file in B: %v", statErr)
	}

	// The same save against the project that IS open goes through, whether the
	// root is spelled with a trailing separator or not.
	if err := svc.WriteFile(t.Context(), "notes.md", "meant for B", dirB+string(filepath.Separator)); err != nil {
		t.Fatalf("save into the open project: %v", err)
	}
	// No expectedRoot keeps the old behaviour.
	if err := svc.WriteFile(t.Context(), "other.md", "x", ""); err != nil {
		t.Fatalf("save without expectedRoot: %v", err)
	}
}

func TestWriteFileIsAtomicAndKeepsTheFileMode(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })

	if err := svc.WriteFile(t.Context(), "run.sh", "new", ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want the file's own 0755 kept", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "run.sh" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteFileWritesThroughASymlinkInsideTheProject(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })

	if err := svc.WriteFile(t.Context(), "link.txt", "new", ""); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filepath.Join(dir, "link.txt")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced by a file: %v %v", info, err)
	}
	if data, _ := os.ReadFile(target); string(data) != "new" {
		t.Fatalf("target = %q, want the write to land on it", data)
	}
}

func TestReadFileRefusesAFileTooLargeForTheEditor(t *testing.T) {
	dir := t.TempDir()
	big := make([]byte, maxReadFileBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	if err := os.WriteFile(filepath.Join(dir, "huge.log"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })

	if _, err := svc.ReadFile(t.Context(), "huge.log"); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("ReadFile = %v, want ErrFileTooLarge", err)
	}
}

func TestListFilesSkipsTheSharedIgnoredDirectories(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"src/A.java", "out/gen/B.java", "dist/app.js", "vendor/x/y.go", ".github/workflows/ci.yml"} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })

	entries, err := svc.ListFiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Path] = true
	}
	for _, skipped := range []string{"out", "out/gen/B.java", "dist", "vendor"} {
		if got[skipped] {
			t.Fatalf("%s listed; ignored directories must not be: %v", skipped, entries)
		}
	}
	// Hidden is not ignored: a CI workflow is something people edit.
	for _, kept := range []string{"src/A.java", ".github/workflows/ci.yml"} {
		if !got[kept] {
			t.Fatalf("%s missing from %v", kept, entries)
		}
	}
}

func TestGetCodeSnippetFindsTheNodeForAnAbsoluteOrDotPath(t *testing.T) {
	dir := t.TempDir()
	src := "class Foo {\n  void bar() {\n    x();\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "Foo.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{Nodes: []Node{
		{ID: "Foo.bar()", Label: "Foo.bar", Kind: "method", FilePath: "Foo.java", Line: 2, EndLine: 4},
	}}})
	if err := svc.OpenProject(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })

	for _, path := range []string{filepath.Join(dir, "Foo.java"), "./Foo.java"} {
		snippet, err := svc.GetCodeSnippet(t.Context(), path, 3, 3)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if snippet.FQN != "Foo.bar" || snippet.Line != 2 || snippet.EndLine != 4 {
			t.Fatalf("%s did not snap to the indexed method: %+v", path, snippet)
		}
	}
}

func TestOnProjectChangedFiresForASwitchNotForAReopen(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	var mu sync.Mutex
	var roots []string
	svc.OnProjectChanged = func(root string) {
		mu.Lock()
		defer mu.Unlock()
		roots = append(roots, root)
	}
	t.Cleanup(func() { svc.Close() })

	for _, dir := range []string{dirA, dirA, dirB} {
		if err := svc.OpenProject(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(roots) != 2 || roots[0] != dirA || roots[1] != dirB {
		t.Fatalf("projectChanged = %v, want [%s %s]", roots, dirA, dirB)
	}
}

// Close used to wait on openMu, which an open held for its whole analysis —
// up to the analyzer's ten-minute timeout, with the app refusing to quit.
func TestCloseDoesNotWaitForAnOpenInFlight(t *testing.T) {
	analyzer := newBlockingAnalyzer()
	svc := newTestService(t, analyzer)
	done := make(chan error, 1)
	go func() { done <- svc.OpenProject(context.Background(), t.TempDir()) }()
	<-analyzer.entered

	closed := make(chan struct{})
	go func() { svc.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked behind an open's analysis")
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrProjectChanged) {
			t.Fatalf("the abandoned open = %v, want ErrProjectChanged", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the open's analysis")
	}
	if _, err := svc.ListEndpoints(t.Context()); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("after Close = %v, want ErrNoActiveProject", err)
	}
}

func TestCancellingTheCallerContextStopsAnOpen(t *testing.T) {
	analyzer := newBlockingAnalyzer()
	svc := newTestService(t, analyzer)
	t.Cleanup(func() { svc.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.OpenProject(ctx, t.TempDir()) }()
	<-analyzer.entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled open = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling the caller's context did not stop the open")
	}
	if _, err := svc.WaitForIndexReady(t.Context()); !errors.Is(err, ErrIndexNotReady) {
		t.Fatalf("a cancelled open left the index %v, want not ready", err)
	}
}

// A newer open replaces an older one still analyzing, instead of queueing
// behind it; the older caller is told the project changed.
func TestANewerOpenSupersedesOneStillAnalyzing(t *testing.T) {
	analyzer := newBlockingAnalyzer()
	svc := newTestService(t, analyzer)
	t.Cleanup(func() { svc.Close() })
	dirA, dirB := t.TempDir(), t.TempDir()
	first := make(chan error, 1)
	go func() { first <- svc.OpenProject(context.Background(), dirA) }()
	<-analyzer.entered

	second := make(chan error, 1)
	go func() { second <- svc.OpenProject(context.Background(), dirB) }()
	select {
	case err := <-first:
		if !errors.Is(err, ErrProjectChanged) {
			t.Fatalf("superseded open = %v, want ErrProjectChanged", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the older open was not cancelled by the newer one")
	}
	<-analyzer.entered
	close(analyzer.release)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if svc.ProjectRoot() != dirB {
		t.Fatalf("root = %q, want %q", svc.ProjectRoot(), dirB)
	}
}

// The refresh a cached open starts belongs to that project: switching away
// must cancel it rather than leave its JVM running for nobody.
func TestSwitchingProjectsCancelsTheOldProjectsRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	dir := t.TempDir()
	seed := reopenService(t, path, fakeAnalyzer{result: sampleResult()})
	if err := seed.OpenProject(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	seed.Close()

	analyzer := newBlockingAnalyzer()
	svc := reopenService(t, path, analyzer)
	if err := svc.OpenProject(t.Context(), dir); err != nil {
		t.Fatal(err)
	}
	<-analyzer.entered // the background refresh is analyzing

	svc.Close()
	select {
	case <-analyzer.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the old project's refresh kept running after Close")
	}
}

func TestRestoreLastProjectReturnsBeforeTheAnalysisFinishes(t *testing.T) {
	config, root := t.TempDir(), t.TempDir()
	first := NewService(NewNilStore(), fakeAnalyzer{result: sampleResult()})
	if err := first.RestoreLastProject(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	if err := first.OpenProjectAt(t.Context(), filepath.Join(t.TempDir(), "other.db"), root); err != nil {
		t.Fatal(err)
	}
	first.Close()
	first.store.Close()

	// A fresh index path for the restore, so it has to analyze cold.
	analyzer := newBlockingAnalyzer()
	next := NewService(NewNilStore(), analyzer)
	t.Cleanup(func() { next.Close(); next.store.Close() })
	var announced []string
	next.OnProjectChanged = func(r string) { announced = append(announced, r) }

	returned := make(chan error, 1)
	go func() { returned <- next.RestoreLastProject(t.Context(), config) }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RestoreLastProject blocked on the analysis — startup would wait for it")
	}
	if len(announced) != 1 || announced[0] != root {
		t.Fatalf("restore announced %v, want [%s]", announced, root)
	}
	if next.ProjectRoot() != root {
		t.Fatalf("root while opening = %q, want %q", next.ProjectRoot(), root)
	}
	<-analyzer.entered
	if _, err := next.ListEndpoints(t.Context()); !errors.Is(err, ErrIndexNotReady) {
		t.Fatalf("queries during the restore = %v, want ErrIndexNotReady", err)
	}
	close(analyzer.release)
	if _, err := next.WaitForIndexReady(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// Plan and comment operations need the store, not a ready index: a comment
// must not bounce off the re-index the user's own save just triggered.
func TestPlanAndCommentsWorkWhileTheIndexRefreshes(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(t.Context(), "/fake/project"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	plan := Plan{Description: "p", Nodes: []PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "OrderController.create", Kind: "method"}}}
	if _, err := svc.ProposeFlow(t.Context(), plan); err != nil {
		t.Fatal(err)
	}

	run := svc.prepareReindex(t.Context()) // a re-index is now in flight
	if _, err := svc.ListEndpoints(t.Context()); !errors.Is(err, ErrIndexNotReady) {
		t.Fatalf("graph queries must still wait: %v", err)
	}
	if err := svc.CommentPlanNode(t.Context(), "pn-1", "use the existing table"); err != nil {
		t.Fatalf("comment during re-index: %v", err)
	}
	if _, err := svc.GetPlannedFlow(t.Context()); err != nil {
		t.Fatalf("plan during re-index: %v", err)
	}
	if err := svc.ApprovePlan(t.Context()); err != nil {
		t.Fatalf("approve during re-index: %v", err)
	}
	run()
}

// Relinking is an agent-callable write; it may change an approved plan, but
// not keep the human's approval of something they did not approve.
func TestRelinkingAnApprovedPlanTakesItBackToDraft(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(t.Context(), "/fake/project"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	plan := Plan{Description: "p", Nodes: []PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.A.run", Kind: "method"}}}
	if _, err := svc.ProposeFlow(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApprovePlan(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Same FQN: nothing changed, approval stands.
	if err := svc.RelinkPlanNode(t.Context(), "pn-1", "com.A.run"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.GetPlannedFlow(t.Context()); got.State != PlanStateApproved {
		t.Fatalf("a no-op relink changed the state to %q", got.State)
	}

	if err := svc.RelinkPlanNode(t.Context(), "pn-1", "com.A.execute"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetPlannedFlow(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.State != PlanStateDraft || got.Nodes[0].FQN != "com.A.execute" {
		t.Fatalf("after relink: state %q fqn %q, want draft and the new fqn", got.State, got.Nodes[0].FQN)
	}
}

func TestStoreLeaseClosesOnlyAfterTheLastBorrowerReturns(t *testing.T) {
	store := &closeCountingStore{Store: nilStore{}}
	lease := newStoreLease(store)
	_, release, ok := lease.acquire()
	if !ok {
		t.Fatal("a fresh lease must lend")
	}
	lease.retire()
	if store.closed != 0 {
		t.Fatal("retire closed the store under a borrower still reading it")
	}
	if _, _, ok := lease.acquire(); ok {
		t.Fatal("a retired lease must not lend again")
	}
	release()
	release() // idempotent
	if store.closed != 1 {
		t.Fatalf("closed %d times, want once when the borrower returned", store.closed)
	}
}

type closeCountingStore struct {
	Store
	closed int
}

func (c *closeCountingStore) Close() error {
	c.closed++
	return nil
}

// MCP opens by folder alone; the index must land where the desktop app's own
// opens put it, derived from the one configured directory.
func TestOpenProjectDirUsesTheConfiguredIndexLocation(t *testing.T) {
	svc := NewService(NewNilStore(), fakeAnalyzer{result: sampleResult()})
	root := t.TempDir()
	if err := svc.OpenProjectDir(t.Context(), root); err == nil {
		t.Fatal("with no config dir there is nowhere agreed to put the index")
	}
	config := t.TempDir()
	svc.SetConfigDir(config)
	if err := svc.OpenProjectDir(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close(); svc.store.Close() })
	if _, err := os.Stat(ProjectDBPath(config, root)); err != nil {
		t.Fatalf("index not at ProjectDBPath(config, root): %v", err)
	}
}
