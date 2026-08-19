// internal/codegraph/service_test.go
package codegraph

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

// fakeAnalyzer is the test double every other test in this package (and the
// bridge/MCP tests in later tasks) uses instead of a real Java subprocess.
type fakeAnalyzer struct {
	result AnalysisResult
	err    error
}

func (f fakeAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	return f.result, f.err
}

func newTestService(t *testing.T, analyzer Analyzer) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	store, err := newSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("newSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewService(store, analyzer)
}

func TestServiceQueriesFailBeforeOpenProject(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{})
	ctx := context.Background()

	if _, err := svc.ListEndpoints(ctx); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestServiceOpenProjectThenListEndpoints(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	ctx := context.Background()

	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	endpoints, err := svc.ListEndpoints(ctx)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Path != "/orders" {
		t.Fatalf("unexpected endpoints: %+v", endpoints)
	}
}

func TestServiceOpenProjectSurfacesAnalyzerError(t *testing.T) {
	analyzerErr := errors.New("boom")
	svc := newTestService(t, fakeAnalyzer{err: analyzerErr})

	err := svc.OpenProject(context.Background(), "/fake/project")
	if !errors.Is(err, analyzerErr) {
		t.Fatalf("expected wrapped analyzer error, got %v", err)
	}
	// A failed OpenProject must not leave a stale/partial index queryable.
	if _, err := svc.ListEndpoints(context.Background()); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject after failed OpenProject, got %v", err)
	}
}

func TestServiceGetApiFlowWalksCallChain(t *testing.T) {
	// n1 (controller) --call--> n2 (service) --call--> n3 (repository)
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "n1", Label: "OrderController.create", Kind: "method", Layer: "controller", FilePath: "OrderController.java", Line: 10},
			{ID: "n2", Label: "OrderService.process", Kind: "method", Layer: "service", FilePath: "OrderService.java", Line: 20},
			{ID: "n3", Label: "OrderRepository.save", Kind: "method", Layer: "repository", FilePath: "OrderRepository.java", Line: 30},
		},
		Edges: []Edge{
			{From: "n1", To: "n2", Kind: "call"},
			{From: "n2", To: "n3", Kind: "call"},
		},
		Endpoints: []Endpoint{{ID: "ep1", HTTPMethod: "POST", Path: "/orders", EntryNode: "n1"}},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	ctx := context.Background()
	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	flow, err := svc.GetApiFlow(ctx, "ep1")
	if err != nil {
		t.Fatalf("GetApiFlow: %v", err)
	}
	if len(flow.Steps) != 3 {
		t.Fatalf("expected 3 steps (controller, service, repository), got %d: %+v", len(flow.Steps), flow.Steps)
	}
	wantLabels := []string{"OrderController.create", "OrderService.process", "OrderRepository.save"}
	for i, want := range wantLabels {
		if flow.Steps[i].Label != want {
			t.Fatalf("step %d: got %q, want %q (full flow: %+v)", i, flow.Steps[i].Label, want, flow.Steps)
		}
	}
}

func TestServiceGetApiFlowHandlesCycleWithoutHanging(t *testing.T) {
	// n1 -> n2 -> n1 (a real call cycle — mutual recursion happens in Java).
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "n1", Label: "A.foo", Kind: "method", Layer: "unknown"},
			{ID: "n2", Label: "B.bar", Kind: "method", Layer: "unknown"},
		},
		Edges: []Edge{
			{From: "n1", To: "n2", Kind: "call"},
			{From: "n2", To: "n1", Kind: "call"},
		},
		Endpoints: []Endpoint{{ID: "ep1", HTTPMethod: "GET", Path: "/x", EntryNode: "n1"}},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	ctx := context.Background()
	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	flow, err := svc.GetApiFlow(ctx, "ep1")
	if err != nil {
		t.Fatalf("GetApiFlow: %v", err)
	}
	if len(flow.Steps) != 2 {
		t.Fatalf("expected exactly 2 steps (cycle must not revisit), got %d: %+v", len(flow.Steps), flow.Steps)
	}
}

func TestServiceSearchSymbol(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	ctx := context.Background()
	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	matches, err := svc.SearchSymbol(ctx, "OrderService")
	if err != nil {
		t.Fatalf("SearchSymbol: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("unexpected matches: %+v", matches)
	}
}

func TestNewServiceWithNilStorePlaceholderIsSafe(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	if _, err := svc.ListEndpoints(context.Background()); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject from nilStore-backed service, got %v", err)
	}
}

func TestServiceSetProjectPathSwapsStore(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := svc.SetProjectPath(dbPath); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	ctx := context.Background()
	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if _, err := svc.ListEndpoints(ctx); err != nil {
		t.Fatalf("ListEndpoints after SetProjectPath+OpenProject: %v", err)
	}
}

func TestServiceSetProjectPathCreatesParentDir(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	nested := filepath.Join(t.TempDir(), "does", "not", "exist", "yet", "index.db")
	if err := svc.SetProjectPath(nested); err != nil {
		t.Fatalf("SetProjectPath with nested new dir: %v", err)
	}
}

// TestServiceConcurrentAccessIsRaceFree pits SetProjectPath/OpenProject
// (writers of s.store/s.active) against ListEndpoints (a reader) running at
// the same time. Before activeStore() existed, ListEndpoints read s.store
// directly after releasing the lock from a separate requireActive() call —
// `go test -race` flags that as a data race under this exact access pattern.
// Errors from the concurrent calls are expected and ignored (a reader can
// legitimately see ErrNoActiveProject mid-swap, or the old store closed out
// from under it); this test only asserts the race detector stays quiet.
func TestServiceConcurrentAccessIsRaceFree(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	ctx := context.Background()
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("initial SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("initial OpenProject: %v", err)
	}

	const iterations = 500
	dbPath := filepath.Join(t.TempDir(), "index.db")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_, _ = svc.ListEndpoints(ctx)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if err := svc.SetProjectPath(dbPath); err != nil {
				continue
			}
			_ = svc.OpenProject(ctx, "/fake/project")
		}
	}()
	wg.Wait()
}
