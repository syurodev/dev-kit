// internal/codegraph/service_test.go
package codegraph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"devkit/internal/terminal"
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

func TestServiceReadFileRejectsBinaryContent(t *testing.T) {
	dir := t.TempDir()
	// A compiled .class file's magic bytes (0xCAFEBABE) are not valid UTF-8.
	if err := os.WriteFile(filepath.Join(dir, "Foo.class"), []byte{0xCA, 0xFE, 0xBA, 0xBE, 0x00, 0x34}, 0o644); err != nil {
		t.Fatalf("seed binary file: %v", err)
	}
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if _, err := svc.ReadFile(context.Background(), "Foo.class"); !errors.Is(err, ErrBinaryFile) {
		t.Fatalf("ReadFile = %v, want ErrBinaryFile", err)
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

func TestServiceWriteFileRejectsDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	pwnedPath := filepath.Join(outside, "pwned.txt")
	if err := os.Symlink(pwnedPath, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	if err := svc.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if err := svc.WriteFile(context.Background(), "link.txt", "pwned"); !errors.Is(err, ErrPathOutsideProject) {
		t.Fatalf("WriteFile = %v, want ErrPathOutsideProject", err)
	}
	if _, err := os.Stat(pwnedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pwned.txt should not have been created, stat returned: %v", err)
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

func TestServiceGetBlastRadiusTransitive(t *testing.T) {
	// Controller depends on Service, Service depends on Repository.
	// Editing Repository.java should report both Service.java (direct) and
	// Controller.java (transitive, via Service).
	result := AnalysisResult{
		Nodes: []Node{{ID: "n1", Label: "Controller.handle", Kind: "method", Layer: "controller", FilePath: "Controller.java", Line: 1, EndLine: 5}},
		FileDependencies: []FileDependency{
			{FilePath: "Controller.java", DependsOnFilePath: "Service.java"},
			{FilePath: "Service.java", DependsOnFilePath: "Repository.java"},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	radius, err := svc.GetBlastRadius(context.Background(), "Repository.java")
	if err != nil {
		t.Fatalf("GetBlastRadius: %v", err)
	}
	if len(radius) != 2 {
		t.Fatalf("radius = %v, want 2 entries", radius)
	}
	want := map[string]bool{"Controller.java": true, "Service.java": true}
	for _, f := range radius {
		if !want[f] {
			t.Fatalf("unexpected file in radius: %q (full: %v)", f, radius)
		}
	}
}

func TestServiceGetBlastRadiusNoDependents(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	radius, err := svc.GetBlastRadius(context.Background(), "Nobody.java")
	if err != nil {
		t.Fatalf("GetBlastRadius: %v", err)
	}
	if len(radius) != 0 {
		t.Fatalf("expected empty radius, got: %v", radius)
	}
}

func TestServiceGetBlastRadiusHandlesCycle(t *testing.T) {
	// A depends on B, B depends on A — a real possibility with Java's mutual
	// class references. The visited-set must stop this from looping forever.
	result := AnalysisResult{
		FileDependencies: []FileDependency{
			{FilePath: "A.java", DependsOnFilePath: "B.java"},
			{FilePath: "B.java", DependsOnFilePath: "A.java"},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	done := make(chan struct{})
	var radius []string
	var err error
	go func() {
		radius, err = svc.GetBlastRadius(context.Background(), "A.java")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GetBlastRadius did not return — likely an infinite loop on the cycle")
	}
	if err != nil {
		t.Fatalf("GetBlastRadius: %v", err)
	}
	if len(radius) != 1 || radius[0] != "B.java" {
		t.Fatalf("radius = %v, want exactly [B.java]", radius)
	}
}

func TestServiceGetBlastRadiusBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	if _, err := svc.GetBlastRadius(context.Background(), "Foo.java"); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestAddRecursiveWatchesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src", "main", "java"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "target", "classes"), 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	defer w.Close()

	if err := addRecursive(w, dir); err != nil {
		t.Fatalf("addRecursive: %v", err)
	}

	watched := w.WatchList()
	has := func(p string) bool {
		for _, w := range watched {
			if w == p {
				return true
			}
		}
		return false
	}
	if !has(dir) {
		t.Fatalf("expected root %q watched, got %v", dir, watched)
	}
	if !has(filepath.Join(dir, "src", "main", "java")) {
		t.Fatalf("expected nested dir watched, got %v", watched)
	}
	if has(filepath.Join(dir, ".git")) {
		t.Fatalf(".git should not be watched, got %v", watched)
	}
	if has(filepath.Join(dir, "target")) || has(filepath.Join(dir, "target", "classes")) {
		t.Fatalf("target should not be watched, got %v", watched)
	}
	if has(filepath.Join(dir, "node_modules")) || has(filepath.Join(dir, "node_modules", "pkg")) {
		t.Fatalf("node_modules should not be watched, got %v", watched)
	}
}

func TestServiceOpenProjectStartsWatcherNonFatally(t *testing.T) {
	// A project path that doesn't exist on disk can't be watched (fsnotify.Add
	// fails), but OpenProject must still succeed — auto-trigger degrades
	// gracefully, it never blocks the project from opening.
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	endpoints, err := svc.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(endpoints) == 0 {
		t.Fatal("expected the project to still be usable despite the watcher failing to start")
	}
}

func TestServiceWaitForIndexReadyReturnsImmediatelyWhenIdle(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	version, err := svc.WaitForIndexReady(ctx)
	if err != nil {
		t.Fatalf("WaitForIndexReady: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1 (one successful OpenProject)", version)
	}
}

func TestServiceWaitForIndexReadyBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	if _, err := svc.WaitForIndexReady(context.Background()); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestServiceWaitForIndexReadyRespectsContextCancellation(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	svc.mu.Lock()
	svc.reindexing = true // simulate a reindex that never finishes
	svc.mu.Unlock()
	defer func() {
		svc.mu.Lock()
		svc.reindexing = false
		svc.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := svc.WaitForIndexReady(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestServiceCloseStopsWatcher(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	dir := t.TempDir()
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	svc.mu.RLock()
	done := svc.watchDone
	svc.mu.RUnlock()

	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-done:
		// watchLoop goroutine exited, as expected
	case <-time.After(time.Second):
		t.Fatal("watchLoop did not exit after Close")
	}
}

type countingAnalyzer struct {
	result AnalysisResult
	calls  *atomic.Int32
}

func (c countingAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	c.calls.Add(1)
	return c.result, nil
}

func TestServiceAutoReindexOnFileChange(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Foo.java"), []byte("class Foo {}"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	var calls atomic.Int32
	svc := newTestService(t, countingAnalyzer{result: sampleResult(), calls: &calls})
	svc.debounceInterval = 20 * time.Millisecond
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	initialCalls := calls.Load()

	if err := os.WriteFile(filepath.Join(dir, "Foo.java"), []byte("class Foo { void bar() {} }"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}
	// Give fsnotify time to actually deliver the OS-level event before
	// polling — WaitForIndexReady only observes state that has already been
	// set by the watch loop, it cannot wait for an event that hasn't arrived
	// yet (see this plan's "Known limitation"). 50ms is generous headroom
	// over real fsnotify delivery latency (typically sub-5ms locally) while
	// staying well under the debounce interval's own margin below.
	time.Sleep(50 * time.Millisecond)

	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := svc.WaitForIndexReady(waitCtx); err != nil {
		t.Fatalf("WaitForIndexReady: %v", err)
	}

	if calls.Load() <= initialCalls {
		t.Fatalf("expected the analyzer to be called again after the file change, calls = %d (was %d)", calls.Load(), initialCalls)
	}
}

func TestServiceReopeningStopsWatchingOldProject(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirA, "Foo.java"), []byte("class Foo {}"), 0o644); err != nil {
		t.Fatalf("seed A: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "Bar.java"), []byte("class Bar {}"), 0o644); err != nil {
		t.Fatalf("seed B: %v", err)
	}

	var calls atomic.Int32
	svc := newTestService(t, countingAnalyzer{result: sampleResult(), calls: &calls})
	svc.debounceInterval = 20 * time.Millisecond
	if err := svc.OpenProject(context.Background(), dirA); err != nil {
		t.Fatalf("OpenProject A: %v", err)
	}
	if err := svc.OpenProject(context.Background(), dirB); err != nil {
		t.Fatalf("OpenProject B: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	callsAfterBothOpens := calls.Load()

	// Modify a file in the now-abandoned project A — must NOT trigger a
	// reindex; the watcher should have moved entirely to B.
	if err := os.WriteFile(filepath.Join(dirA, "Foo.java"), []byte("class Foo { void bar() {} }"), 0o644); err != nil {
		t.Fatalf("modify A: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // comfortably longer than debounceInterval

	if calls.Load() != callsAfterBothOpens {
		t.Fatalf("expected no reindex from changes in the abandoned project, calls = %d (was %d)", calls.Load(), callsAfterBothOpens)
	}
}

func TestServiceWaitForIndexReadyWaitsDuringDebounce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Foo.java"), []byte("class Foo {}"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	var calls atomic.Int32
	svc := newTestService(t, countingAnalyzer{result: sampleResult(), calls: &calls})
	// Use a 150ms debounce interval to ensure the debounce window is comfortably observable
	svc.debounceInterval = 150 * time.Millisecond
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	initialCalls := calls.Load()

	if err := os.WriteFile(filepath.Join(dir, "Foo.java"), []byte("class Foo { void bar() {} }"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}
	// Give fsnotify 10ms to deliver event and set reindexPending = true
	time.Sleep(10 * time.Millisecond)

	start := time.Now()
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := svc.WaitForIndexReady(waitCtx); err != nil {
		t.Fatalf("WaitForIndexReady: %v", err)
	}
	elapsed := time.Since(start)

	// Must have waited at least ~100ms (debounce remaining) and reindexed
	if elapsed < 80*time.Millisecond {
		t.Fatalf("WaitForIndexReady returned too quickly (%v), likely skipped debounce window", elapsed)
	}
	if calls.Load() <= initialCalls {
		t.Fatalf("expected analyzer to be called after file change, calls = %d (was %d)", calls.Load(), initialCalls)
	}
}

type funcAnalyzer struct {
	analyzeFunc func(ctx context.Context, path string) (AnalysisResult, error)
}

func (f funcAnalyzer) Analyze(ctx context.Context, path string) (AnalysisResult, error) {
	return f.analyzeFunc(ctx, path)
}

func TestServiceStaleReindexDoesNotClobberNewProject(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	resultA := AnalysisResult{
		Nodes: []Node{{ID: "nA", Label: "A.foo", Kind: "method", Layer: "controller"}},
	}
	resultB := AnalysisResult{
		Nodes: []Node{{ID: "nB", Label: "B.bar", Kind: "method", Layer: "service"}},
	}

	analyzingA := make(chan struct{})
	unblockA := make(chan struct{})

	// Only the SECOND analyze of dirA — the background reindex's, not
	// OpenProject's initial one — may block; gating every dirA analyze would
	// deadlock OpenProject(A) itself before the test even reaches the
	// interesting interleaving.
	var aCalls atomic.Int32
	customAnalyzer := funcAnalyzer{
		analyzeFunc: func(ctx context.Context, path string) (AnalysisResult, error) {
			if path == dirA {
				if aCalls.Add(1) == 2 {
					close(analyzingA)
					<-unblockA
				}
				return resultA, nil
			}
			return resultB, nil
		},
	}

	svc := newTestService(t, customAnalyzer)
	if err := svc.OpenProject(context.Background(), dirA); err != nil {
		t.Fatalf("OpenProject A: %v", err)
	}

	// Start runReindex for project A in a background goroutine
	doneReindex := make(chan struct{})
	go func() {
		svc.runReindex(context.Background())
		close(doneReindex)
	}()

	// Wait until runReindex is inside Analyze for dirA
	<-analyzingA

	// Now open project B while project A's reindex is in-flight
	if err := svc.OpenProject(context.Background(), dirB); err != nil {
		t.Fatalf("OpenProject B: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	// Unblock project A reindex
	close(unblockA)
	<-doneReindex

	// Project B index must NOT be overwritten by project A
	nodes, err := svc.SearchSymbol(context.Background(), "B.bar")
	if err != nil {
		t.Fatalf("SearchSymbol: %v", err)
	}
	if len(nodes) != 1 || nodes[0].NodeID != "nB" {
		t.Fatalf("expected project B index to remain intact, got: %+v", nodes)
	}
	nodesA, err := svc.SearchSymbol(context.Background(), "A.foo")
	if err != nil {
		t.Fatalf("SearchSymbol: %v", err)
	}
	if len(nodesA) != 0 {
		t.Fatalf("expected no symbols from project A in project B, got: %+v", nodesA)
	}
}

func neighborsFixture() AnalysisResult {
	// n1(controller) -> n2(service) -> n3(repository); n4(controller) is
	// isolated, unreachable from n1 within any depth.
	return AnalysisResult{
		Nodes: []Node{
			{ID: "n1", Label: "Controller.handle", Kind: "method", Layer: "controller", FilePath: "C.java", Line: 1, EndLine: 5},
			{ID: "n2", Label: "Service.process", Kind: "method", Layer: "service", FilePath: "S.java", Line: 1, EndLine: 5},
			{ID: "n3", Label: "Repository.save", Kind: "method", Layer: "repository", FilePath: "R.java", Line: 1, EndLine: 5},
			{ID: "n4", Label: "Other.handle", Kind: "method", Layer: "controller", FilePath: "O.java", Line: 1, EndLine: 5},
		},
		Edges: []Edge{
			{From: "n1", To: "n2", Kind: "call"},
			{From: "n2", To: "n3", Kind: "call"},
		},
	}
}

func TestServiceGetNeighborsDepthOne(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: neighborsFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sub, err := svc.GetNeighbors(context.Background(), "n2", 1, NeighborFilter{})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	ids := nodeIDs(sub.Nodes)
	// n2 itself, plus n1 (in-edge) and n3 (out-edge) — n4 is unreachable.
	wantIDs := map[string]bool{"n1": true, "n2": true, "n3": true}
	if len(ids) != 3 {
		t.Fatalf("nodes = %v, want exactly n1,n2,n3", ids)
	}
	for _, id := range ids {
		if !wantIDs[id] {
			t.Fatalf("unexpected node %q in result: %v", id, ids)
		}
	}
	if len(sub.Edges) != 2 {
		t.Fatalf("edges = %+v, want 2 (n1->n2, n2->n3)", sub.Edges)
	}
	if sub.Truncated {
		t.Fatal("did not expect truncation for a 3-node result under any reasonable ceiling")
	}
}

func TestServiceGetNeighborsDepthZeroReturnsOnlyRoot(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: neighborsFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sub, err := svc.GetNeighbors(context.Background(), "n2", 0, NeighborFilter{})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	if len(sub.Nodes) != 1 || sub.Nodes[0].ID != "n2" {
		t.Fatalf("nodes = %v, want exactly [n2]", nodeIDs(sub.Nodes))
	}
	if len(sub.Edges) != 0 {
		t.Fatalf("edges = %+v, want none at depth 0", sub.Edges)
	}
}

func TestServiceGetNeighborsFiltersByLayer(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: neighborsFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sub, err := svc.GetNeighbors(context.Background(), "n2", 2, NeighborFilter{Layers: []string{"service"}})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	// Only n2 itself matches "service" — n1 (controller) and n3 (repository)
	// are excluded even though they're within depth 2.
	if len(sub.Nodes) != 1 || sub.Nodes[0].ID != "n2" {
		t.Fatalf("nodes = %v, want exactly [n2]", nodeIDs(sub.Nodes))
	}
}

func TestServiceGetNeighborsTruncatesAndResumes(t *testing.T) {
	// Star graph: root n0 directly connected to 5 others. Force the ceiling
	// down via a tiny artificial limit isn't possible without exporting it,
	// so this test instead drives a large-enough fixture with the real
	// constant and asserts the offset/resume contract, not an exact count —
	// see Step 3's maxNeighborNodes value before finalizing the fixture size.
	nodes := []Node{{ID: "root", Label: "Root", Kind: "class", Layer: "unknown", FilePath: "Root.java", Line: 1, EndLine: 1}}
	var edges []Edge
	const spokes = maxNeighborNodes + 10
	for i := 0; i < spokes; i++ {
		id := fmt.Sprintf("leaf%03d", i)
		nodes = append(nodes, Node{ID: id, Label: id, Kind: "class", Layer: "unknown", FilePath: id + ".java", Line: 1, EndLine: 1})
		edges = append(edges, Edge{From: "root", To: id, Kind: "call"})
	}
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{Nodes: nodes, Edges: edges}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	first, err := svc.GetNeighbors(context.Background(), "root", 1, NeighborFilter{})
	if err != nil {
		t.Fatalf("GetNeighbors (first page): %v", err)
	}
	if !first.Truncated {
		t.Fatalf("expected truncation with %d spokes against maxNeighborNodes=%d", spokes, maxNeighborNodes)
	}
	if len(first.Nodes) != maxNeighborNodes {
		t.Fatalf("first page = %d nodes, want exactly maxNeighborNodes=%d", len(first.Nodes), maxNeighborNodes)
	}
	if first.Continuation == "" {
		t.Fatal("expected a non-empty Continuation when truncated")
	}

	second, err := svc.GetNeighbors(context.Background(), "root", 1, NeighborFilter{Continuation: first.Continuation})
	if err != nil {
		t.Fatalf("GetNeighbors (second page): %v", err)
	}

	firstIDs := nodeIDs(first.Nodes)
	secondIDs := nodeIDs(second.Nodes)
	overlap := map[string]bool{}
	for _, id := range firstIDs {
		overlap[id] = true
	}
	for _, id := range secondIDs {
		if overlap[id] {
			t.Fatalf("node %q appeared in both pages — resumption is not skipping already-returned nodes", id)
		}
	}
	if len(firstIDs)+len(secondIDs) == 0 {
		t.Fatal("both pages empty")
	}
}

func TestServiceGetNeighborsHandlesCycle(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "a", Label: "A", Kind: "class", Layer: "unknown", FilePath: "A.java", Line: 1, EndLine: 1},
			{ID: "b", Label: "B", Kind: "class", Layer: "unknown", FilePath: "B.java", Line: 1, EndLine: 1},
		},
		Edges: []Edge{
			{From: "a", To: "b", Kind: "call"},
			{From: "b", To: "a", Kind: "call"},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	done := make(chan struct{})
	var sub Subgraph
	var err error
	go func() {
		sub, err = svc.GetNeighbors(context.Background(), "a", 5, NeighborFilter{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GetNeighbors did not return — likely an infinite loop on the cycle")
	}
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	if len(sub.Nodes) != 2 {
		t.Fatalf("nodes = %v, want exactly [a, b]", nodeIDs(sub.Nodes))
	}
}

// The design doc requires a hard ceiling on BOTH nodes and edges per page
// ("Ceiling node/edge cứng"). A dense cluster fits under the node ceiling
// but its intra-page edge count must still be capped.
func TestServiceGetNeighborsCapsEdgesPerPage(t *testing.T) {
	// root + 60 nodes, every pair connected: 61*60/2 = 1830 edges — one
	// 61-node page (under maxNeighborNodes) carrying 1830 intra-page edges.
	const others = 60
	ids := make([]string, 0, others+1)
	ids = append(ids, "root")
	nodes := []Node{{ID: "root", Label: "Root", Kind: "class", Layer: "unknown", FilePath: "Root.java", Line: 1, EndLine: 1}}
	var edges []Edge
	for i := 0; i < others; i++ {
		id := fmt.Sprintf("n%03d", i)
		ids = append(ids, id)
		nodes = append(nodes, Node{ID: id, Label: id, Kind: "class", Layer: "unknown", FilePath: id + ".java", Line: 1, EndLine: 1})
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			edges = append(edges, Edge{From: ids[i], To: ids[j], Kind: "call"})
		}
	}
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{Nodes: nodes, Edges: edges}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// Depth 2: level 0 collects root's edges, level 1 walks every leaf's
	// in/out edges — only then does the BFS see the leaf↔leaf edges of the
	// dense cluster. All 61 nodes still land in a single under-ceiling page.
	sub, err := svc.GetNeighbors(context.Background(), "root", 2, NeighborFilter{})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	if len(sub.Nodes) != others+1 {
		t.Fatalf("nodes = %d, want all %d in one page (under the node ceiling)", len(sub.Nodes), others+1)
	}
	if len(sub.Edges) > maxNeighborEdges {
		t.Fatalf("edges = %d, want at most maxNeighborEdges=%d", len(sub.Edges), maxNeighborEdges)
	}
	if len(sub.Edges) < maxNeighborEdges {
		t.Fatalf("edges = %d, expected the full cap to be exercised (fixture has %d intra-page edges)", len(sub.Edges), len(edges))
	}
	if !sub.Truncated {
		t.Fatal("expected Truncated=true when the edge ceiling is hit, even though all nodes fit in one page")
	}
	if sub.Continuation != "" {
		t.Fatalf("continuation = %q, want empty (no further node pages exist; the truncation is edge-side)", sub.Continuation)
	}
}

func TestServiceGetNeighborsUnknownRootFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: neighborsFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if _, err := svc.GetNeighbors(context.Background(), "does-not-exist", 1, NeighborFilter{}); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func nodeIDs(nodes []Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}

func TestServiceGetApiFlowCarriesSummaryAndParams(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{
				ID: "n1", Label: "OrderController.create", Kind: "method", Layer: "controller",
				FilePath: "Order.java", Line: 10, EndLine: 15,
				Summary: "Handles order creation requests.",
				Params:  []Param{{Name: "request", Type: "CreateOrderRequest"}},
			},
		},
		Endpoints: []Endpoint{{ID: "ep1", HTTPMethod: "POST", Path: "/orders", EntryNode: "n1"}},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	flow, err := svc.GetApiFlow(context.Background(), "ep1")
	if err != nil {
		t.Fatalf("GetApiFlow: %v", err)
	}
	if len(flow.Steps) != 1 {
		t.Fatalf("steps = %+v, want 1", flow.Steps)
	}
	step := flow.Steps[0]
	if step.Summary != "Handles order creation requests." {
		t.Fatalf("Summary = %q", step.Summary)
	}
	if len(step.Params) != 1 || step.Params[0] != (Param{Name: "request", Type: "CreateOrderRequest"}) {
		t.Fatalf("Params = %+v", step.Params)
	}
}

func TestServiceGetNeighborsCarriesSummaryAndParams(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{
				ID: "n1", Label: "OrderService.process", Kind: "method", Layer: "service",
				FilePath: "OrderService.java", Line: 1, EndLine: 5,
				Summary: "Processes a validated order.",
				Params:  []Param{{Name: "order", Type: "Order"}},
			},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sub, err := svc.GetNeighbors(context.Background(), "n1", 1, NeighborFilter{})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	if len(sub.Nodes) != 1 {
		t.Fatalf("nodes = %+v, want 1", sub.Nodes)
	}
	if sub.Nodes[0].Summary != "Processes a validated order." {
		t.Fatalf("Summary = %q", sub.Nodes[0].Summary)
	}
	if len(sub.Nodes[0].Params) != 1 || sub.Nodes[0].Params[0] != (Param{Name: "order", Type: "Order"}) {
		t.Fatalf("Params = %+v", sub.Nodes[0].Params)
	}
}

func dataFlowFixture() AnalysisResult {
	return AnalysisResult{
		Nodes: []Node{
			{ID: "cls", Label: "com.acme.Order", Kind: "class", Layer: "unknown", FilePath: "Order.java", Line: 1, EndLine: 50},
			{ID: "m1", Label: "com.acme.Order.setTotal", Kind: "method", Layer: "unknown", FilePath: "Order.java", Line: 10, EndLine: 12},
			{ID: "m2", Label: "com.acme.Order.getTotal", Kind: "method", Layer: "unknown", FilePath: "Order.java", Line: 14, EndLine: 16},
		},
		FieldAccesses: []FieldAccess{
			{ClassID: "cls", NodeID: "m1", FieldName: "total", Kind: "write", Line: 11},
			{ClassID: "cls", NodeID: "m2", FieldName: "total", Kind: "read", Line: 15},
		},
	}
}

func TestServiceTraceDataFlow(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: dataFlowFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	trace, err := svc.TraceDataFlow(context.Background(), "cls", "total")
	if err != nil {
		t.Fatalf("TraceDataFlow: %v", err)
	}
	if trace.SymbolID != "cls" || trace.FieldName != "total" {
		t.Fatalf("trace = %+v", trace)
	}
	if len(trace.Steps) != 2 {
		t.Fatalf("steps = %+v, want 2", trace.Steps)
	}
	if trace.Steps[0].NodeID != "m1" || trace.Steps[0].Kind != "write" || trace.Steps[0].Label != "com.acme.Order.setTotal" {
		t.Fatalf("steps[0] = %+v", trace.Steps[0])
	}
	if trace.Steps[1].NodeID != "m2" || trace.Steps[1].Kind != "read" || trace.Steps[1].Label != "com.acme.Order.getTotal" {
		t.Fatalf("steps[1] = %+v", trace.Steps[1])
	}
}

func TestServiceTraceDataFlowNoAccessesReturnsEmpty(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: dataFlowFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	trace, err := svc.TraceDataFlow(context.Background(), "cls", "nonexistentField")
	if err != nil {
		t.Fatalf("TraceDataFlow: %v", err)
	}
	if len(trace.Steps) != 0 {
		t.Fatalf("steps = %+v, want none", trace.Steps)
	}
}

func TestServiceTraceDataFlowUnknownSymbolFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: dataFlowFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if _, err := svc.TraceDataFlow(context.Background(), "does-not-exist", "total"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestServiceTraceDataFlowBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	if _, err := svc.TraceDataFlow(context.Background(), "cls", "total"); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestServiceStartTerminalUsesProjectRootAsCwd(t *testing.T) {
	dir := t.TempDir()
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	reg := terminal.NewRegistry(nil)
	svc.SetTerminalRegistry(reg)
	if err := svc.OpenProject(context.Background(), dir); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sessionID, err := svc.StartTerminal(24, 80)
	if err != nil {
		t.Fatalf("StartTerminal: %v", err)
	}
	defer reg.Close(sessionID)

	owner, ok := reg.Owner(sessionID)
	if !ok || owner != "builtin.codegraph" {
		t.Fatalf("Owner = %q, %v", owner, ok)
	}
}

func TestServiceStartTerminalBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	svc.SetTerminalRegistry(terminal.NewRegistry(nil))
	if _, err := svc.StartTerminal(24, 80); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestServiceStartTerminalWithoutRegistryFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if _, err := svc.StartTerminal(24, 80); !errors.Is(err, ErrTerminalNotConfigured) {
		t.Fatalf("expected ErrTerminalNotConfigured, got %v", err)
	}
}

func planFixtureAnalysis() AnalysisResult {
	// Real code that already contains RefundController.create calling
	// RefundService.process — used to exercise the "plan is already
	// complete the moment it is proposed" case.
	return AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.RefundController.create(com.acme.Req)", Label: "com.acme.RefundController.create", Kind: "method", Layer: "controller", FilePath: "C.java", Line: 1, EndLine: 5},
			{ID: "com.acme.RefundService.process(com.acme.Req)", Label: "com.acme.RefundService.process", Kind: "method", Layer: "service", FilePath: "S.java", Line: 1, EndLine: 5},
		},
		Edges: []Edge{
			{From: "com.acme.RefundController.create(com.acme.Req)", To: "com.acme.RefundService.process(com.acme.Req)", Kind: "call"},
		},
	}
}

func TestServiceProposeFlowStoresDraftAndReportsProgress(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	progress, err := svc.ProposeFlow(context.Background(), Plan{
		Description: "Refund flow",
		Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "Entry", FQN: "com.acme.RefundController.create", Kind: "method"},
			{PlanNodeID: "pn-2", Label: "Core", FQN: "com.acme.RefundService.process", Kind: "method"},
		},
		Edges: []PlanEdge{{From: "pn-1", To: "pn-2", Condition: "always"}},
	})
	if err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}
	if progress.State != PlanStateDraft {
		t.Fatalf("state = %q, want draft", progress.State)
	}
	// Index is empty, so nothing resolves.
	if progress.Aggregate != PlanAggregateNotStarted {
		t.Fatalf("aggregate = %q, want not_started", progress.Aggregate)
	}
	if len(progress.Nodes) != 2 || progress.Nodes[0].Status != PlanStatusNotImplemented {
		t.Fatalf("nodes = %+v", progress.Nodes)
	}
}

func TestServiceProposeFlowCompleteImmediatelyWhenCodeAlreadyMatches(t *testing.T) {
	// The doc's explicit special case: every planned method already exists
	// and the flow already matches reality, so the plan is complete the
	// moment it is proposed — no approve/implement cycle needed.
	svc := newTestService(t, fakeAnalyzer{result: planFixtureAnalysis()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	progress, err := svc.ProposeFlow(context.Background(), Plan{
		Description: "Already built",
		Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "Entry", FQN: "com.acme.RefundController.create", Kind: "method"},
			{PlanNodeID: "pn-2", Label: "Core", FQN: "com.acme.RefundService.process", Kind: "method"},
		},
		Edges: []PlanEdge{{From: "pn-1", To: "pn-2"}},
	})
	if err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}
	if progress.Aggregate != PlanAggregateComplete {
		t.Fatalf("aggregate = %q, want complete; nodes = %+v", progress.Aggregate, progress.Nodes)
	}
	if progress.Nodes[0].RealNodeID != "com.acme.RefundController.create(com.acme.Req)" {
		t.Fatalf("RealNodeID = %q", progress.Nodes[0].RealNodeID)
	}
}

func TestServiceProposeFlowReplacesPreviousPlan(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	first := Plan{Description: "First", Nodes: []PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"}}}
	if _, err := svc.ProposeFlow(context.Background(), first); err != nil {
		t.Fatalf("first ProposeFlow: %v", err)
	}

	second := Plan{Description: "Second", Nodes: []PlanNode{{PlanNodeID: "pn-9", Label: "B", FQN: "com.acme.B.b", Kind: "method"}}}
	if _, err := svc.ProposeFlow(context.Background(), second); err != nil {
		t.Fatalf("second ProposeFlow: %v", err)
	}

	flow, err := svc.GetPlannedFlow(context.Background())
	if err != nil {
		t.Fatalf("GetPlannedFlow: %v", err)
	}
	if flow.Description != "Second" || len(flow.Nodes) != 1 || flow.Nodes[0].PlanNodeID != "pn-9" {
		t.Fatalf("plan = %+v", flow)
	}
}

func TestServiceProposeFlowRejectsInvalidPlans(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	cases := []struct {
		name string
		plan Plan
	}{
		{"no nodes", Plan{Description: "empty"}},
		{"duplicate node id", Plan{Description: "dup", Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"},
			{PlanNodeID: "pn-1", Label: "B", FQN: "com.acme.B.b", Kind: "method"},
		}}},
		{"node without fqn", Plan{Description: "nofqn", Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "A", Kind: "method"},
		}}},
		{"edge to unknown node", Plan{Description: "badedge", Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"},
		}, Edges: []PlanEdge{{From: "pn-1", To: "pn-404"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.ProposeFlow(context.Background(), tc.plan); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("ProposeFlow = %v, want ErrInvalidPlan", err)
			}
		})
	}
}

func TestServiceProposeFlowBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	plan := Plan{Description: "x", Nodes: []PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"}}}
	if _, err := svc.ProposeFlow(context.Background(), plan); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestServiceGetPlanProgressReflectsNewlyIndexedCode(t *testing.T) {
	// The heart of "auto-diff": propose against an empty index (nothing
	// implemented), then re-index with the code written, and the very next
	// GetPlanProgress call reports it — no recompute hook, no manual
	// refresh, because status is computed on read.
	analyzer := &swappableAnalyzer{result: AnalysisResult{}}
	svc := newTestService(t, analyzer)
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	plan := Plan{
		Description: "Refund flow",
		Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "Entry", FQN: "com.acme.RefundController.create", Kind: "method"},
			{PlanNodeID: "pn-2", Label: "Core", FQN: "com.acme.RefundService.process", Kind: "method"},
		},
		Edges: []PlanEdge{{From: "pn-1", To: "pn-2"}},
	}
	before, err := svc.ProposeFlow(context.Background(), plan)
	if err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}
	if before.Aggregate != PlanAggregateNotStarted {
		t.Fatalf("before = %q, want not_started", before.Aggregate)
	}

	// The agent writes the code; a re-index picks it up.
	analyzer.result = planFixtureAnalysis()
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("re-OpenProject: %v", err)
	}

	// This call succeeding at all is load-bearing: ReplaceIndex clears the
	// index tables but must NOT touch plan_meta/plan_nodes/plan_edges, or a
	// routine background re-index would silently destroy the user's approved
	// plan. A wiped plan surfaces here as ErrNoPlan, not a wrong status.
	after, err := svc.GetPlanProgress(context.Background())
	if err != nil {
		t.Fatalf("GetPlanProgress: %v", err)
	}
	if after.Aggregate != PlanAggregateComplete {
		t.Fatalf("after = %q, want complete; nodes = %+v", after.Aggregate, after.Nodes)
	}
}

func TestServiceApprovePlan(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	plan := Plan{Description: "x", Nodes: []PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"}}}
	if _, err := svc.ProposeFlow(context.Background(), plan); err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}

	if err := svc.ApprovePlan(context.Background()); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	progress, err := svc.GetPlanProgress(context.Background())
	if err != nil {
		t.Fatalf("GetPlanProgress: %v", err)
	}
	if progress.State != PlanStateApproved {
		t.Fatalf("state = %q, want approved", progress.State)
	}
}

func TestServiceApprovePlanWithoutPlanFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if err := svc.ApprovePlan(context.Background()); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("ApprovePlan = %v, want ErrNoPlan", err)
	}
}

func TestServiceRelinkPlanNodeRestoresMatchAfterRename(t *testing.T) {
	// The documented escape hatch for the FQN-matching tradeoff: the real
	// method exists under a different name than planned, so the node reads
	// not_implemented until the plan is pointed at the new name.
	analysis := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.RefundService.handle(com.acme.Req)", Label: "com.acme.RefundService.handle", Kind: "method", Layer: "service", FilePath: "S.java", Line: 1, EndLine: 5},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: analysis})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	plan := Plan{Description: "x", Nodes: []PlanNode{
		{PlanNodeID: "pn-1", Label: "Core", FQN: "com.acme.RefundService.process", Kind: "method"},
	}}
	progress, err := svc.ProposeFlow(context.Background(), plan)
	if err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}
	if progress.Nodes[0].Status != PlanStatusNotImplemented {
		t.Fatalf("status = %q, want not_implemented before relink", progress.Nodes[0].Status)
	}

	if err := svc.RelinkPlanNode(context.Background(), "pn-1", "com.acme.RefundService.handle"); err != nil {
		t.Fatalf("RelinkPlanNode: %v", err)
	}

	progress, err = svc.GetPlanProgress(context.Background())
	if err != nil {
		t.Fatalf("GetPlanProgress: %v", err)
	}
	if progress.Nodes[0].Status != PlanStatusImplementedMatching {
		t.Fatalf("status = %q, want implemented_matching after relink", progress.Nodes[0].Status)
	}
}

func TestServiceRelinkUnknownPlanNodeFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	plan := Plan{Description: "x", Nodes: []PlanNode{{PlanNodeID: "pn-1", Label: "A", FQN: "com.acme.A.a", Kind: "method"}}}
	if _, err := svc.ProposeFlow(context.Background(), plan); err != nil {
		t.Fatalf("ProposeFlow: %v", err)
	}
	if err := svc.RelinkPlanNode(context.Background(), "pn-404", "com.acme.X.y"); !errors.Is(err, ErrPlanNodeNotFound) {
		t.Fatalf("RelinkPlanNode = %v, want ErrPlanNodeNotFound", err)
	}
}

func TestServiceGetPlanProgressWithoutPlanFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if _, err := svc.GetPlanProgress(context.Background()); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("GetPlanProgress = %v, want ErrNoPlan", err)
	}
}

// swappableAnalyzer lets a test change what the next OpenProject indexes,
// simulating "the agent wrote the code, then it got re-indexed".
type swappableAnalyzer struct {
	result AnalysisResult
}

func (a *swappableAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	return a.result, nil
}
