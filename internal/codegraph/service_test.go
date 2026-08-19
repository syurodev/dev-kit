// internal/codegraph/service_test.go
package codegraph

import (
	"context"
	"errors"
	"os"
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

func TestServiceOpenProjectSetsProjectRoot(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	_, root, err := svc.activeProject()
	if err != nil {
		t.Fatalf("activeProject: %v", err)
	}
	if root != "/fake/project" {
		t.Fatalf("projectRoot = %q, want /fake/project", root)
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

func TestServiceListFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("seed a.txt: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "b.java"), []byte("class B {}"), 0o644); err != nil {
		t.Fatalf("seed b.java: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0o644); err != nil {
		t.Fatalf("seed .git/HEAD: %v", err)
	}

	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	entries, err := svc.ListFiles(context.Background())
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	byPath := make(map[string]FileEntry, len(entries))
	for _, e := range entries {
		byPath[e.Path] = e
	}
	if e, ok := byPath["a.txt"]; !ok || e.IsDir {
		t.Fatalf("expected file entry a.txt, got %+v ok=%v", e, ok)
	}
	if e, ok := byPath["src"]; !ok || !e.IsDir {
		t.Fatalf("expected dir entry src, got %+v ok=%v", e, ok)
	}
	if e, ok := byPath[filepath.ToSlash(filepath.Join("src", "b.java"))]; !ok || e.IsDir {
		t.Fatalf("expected file entry src/b.java, got %+v ok=%v", e, ok)
	}
	if _, ok := byPath[".git"]; ok {
		t.Fatal(".git should be skipped")
	}
}

func TestServiceListFilesBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	if _, err := svc.ListFiles(context.Background()); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestServiceReadWriteFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if err := svc.WriteFile(context.Background(), "notes.md", "# hello"); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := svc.ReadFile(context.Background(), "notes.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got != "# hello" {
		t.Fatalf("ReadFile = %q, want %q", got, "# hello")
	}
}

func TestServiceReadFileRejectsPathOutsideProject(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if _, err := svc.ReadFile(context.Background(), "../../etc/passwd"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("ReadFile = %v, want ErrPathOutsideProject", err)
	}
}

func TestServiceWriteFileRejectsPathOutsideProject(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if err := svc.WriteFile(context.Background(), "../../etc/passwd", "pwned"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("WriteFile = %v, want ErrPathOutsideProject", err)
	}
}

func TestServiceGetCodeSnippetSnapsToEnclosingNode(t *testing.T) {
	dir := t.TempDir()
	javaSrc := "package com.acme;\n\nclass OrderService {\n    void create() {\n        // line 5\n        // line 6\n    }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "OrderService.java"), []byte(javaSrc), 0o644); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "method1", Label: "com.acme.OrderService.create", Kind: "method", Layer: "service", FilePath: "OrderService.java", Line: 4, EndLine: 7},
		},
	}
	svc := NewService(nilStore{}, fakeAnalyzer{result: result})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// Ask for line 5 (mid-method) — the real answer should snap to the whole
	// method's 4-7 range, not just line 5.
	snippet, err := svc.GetCodeSnippet(context.Background(), "OrderService.java", 5, 5)
	if err != nil {
		t.Fatalf("GetCodeSnippet: %v", err)
	}
	if snippet.Line != 4 || snippet.EndLine != 7 {
		t.Fatalf("snippet bounds = %d-%d, want 4-7 (snapped to enclosing method)", snippet.Line, snippet.EndLine)
	}
	if snippet.FQN != "com.acme.OrderService.create" {
		t.Fatalf("snippet.FQN = %q, want com.acme.OrderService.create", snippet.FQN)
	}
	if snippet.FilePath != "OrderService.java" {
		t.Fatalf("snippet.FilePath = %q, want OrderService.java", snippet.FilePath)
	}
	if snippet.Language != "java" {
		t.Fatalf("snippet.Language = %q, want java", snippet.Language)
	}
	wantSource := "    void create() {\n        // line 5\n        // line 6\n    }"
	if snippet.Source != wantSource {
		t.Fatalf("snippet.Source = %q, want %q", snippet.Source, wantSource)
	}
}

func TestServiceGetCodeSnippetFallsBackToInputWhenNoNodeMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// README.md is never in the index (only .java files are analyzed), so no
	// node covers it — GetCodeSnippet must fall back to the input range as-is.
	snippet, err := svc.GetCodeSnippet(context.Background(), "README.md", 1, 2)
	if err != nil {
		t.Fatalf("GetCodeSnippet: %v", err)
	}
	if snippet.Line != 1 || snippet.EndLine != 2 {
		t.Fatalf("snippet bounds = %d-%d, want 1-2 (input, unchanged)", snippet.Line, snippet.EndLine)
	}
	if snippet.FQN != "" {
		t.Fatalf("snippet.FQN = %q, want empty (no node matched)", snippet.FQN)
	}
	if snippet.Language != "markdown" {
		t.Fatalf("snippet.Language = %q, want markdown", snippet.Language)
	}
	if snippet.Source != "line1\nline2" {
		t.Fatalf("snippet.Source = %q, want %q", snippet.Source, "line1\nline2")
	}
}

func TestServiceGetCodeSnippetRejectsPathOutsideProject(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if _, err := svc.GetCodeSnippet(context.Background(), "../../etc/passwd", 1, 1); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("GetCodeSnippet = %v, want ErrPathOutsideProject", err)
	}
}
