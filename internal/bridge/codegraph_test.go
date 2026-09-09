// internal/bridge/codegraph_test.go
package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devkit/internal/app"
	"devkit/internal/codegraph"
	"devkit/internal/terminal"
)

// fakeCodeGraphAnalyzer lets these tests drive Service without a real Java
// CLI, same as internal/codegraph's own tests.
type fakeCodeGraphAnalyzer struct {
	result codegraph.AnalysisResult
	err    error
}

func (f fakeCodeGraphAnalyzer) Analyze(ctx context.Context, path string) (codegraph.AnalysisResult, error) {
	return f.result, f.err
}

func TestCodeGraphOpenProjectThenListEndpoints(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		Nodes:     []codegraph.Node{{ID: "n1", Label: "A.foo", Kind: "method", Layer: "controller"}},
		Endpoints: []codegraph.Endpoint{{ID: "ep1", HTTPMethod: "GET", Path: "/a", EntryNode: "n1"}},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	s.container.ConfigDir = t.TempDir()

	if _, err := s.CodeGraphOpenProject("/fake/project"); err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}

	endpoints, err := s.CodeGraphListEndpoints()
	if err != nil {
		t.Fatalf("CodeGraphListEndpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Path != "/a" {
		t.Fatalf("unexpected endpoints: %+v", endpoints)
	}
}

func TestCodeGraphOpenProjectTwiceReusesSameStore(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		Nodes:     []codegraph.Node{{ID: "n1", Label: "A.foo", Kind: "method", Layer: "controller"}},
		Endpoints: []codegraph.Endpoint{{ID: "ep1", HTTPMethod: "GET", Path: "/a", EntryNode: "n1"}},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	s.container.ConfigDir = t.TempDir()

	if _, err := s.CodeGraphOpenProject("/fake/project"); err != nil {
		t.Fatalf("first CodeGraphOpenProject: %v", err)
	}
	if _, err := s.CodeGraphOpenProject("/fake/project"); err != nil {
		t.Fatalf("second CodeGraphOpenProject: %v", err)
	}

	endpoints, err := s.CodeGraphListEndpoints()
	if err != nil {
		t.Fatalf("CodeGraphListEndpoints: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint after re-index, got %d: %+v", len(endpoints), endpoints)
	}
}

func TestCodeGraphListEndpointsBeforeOpenProjectFails(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{})

	if _, err := s.CodeGraphListEndpoints(); !errors.Is(err, codegraph.ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestCodeGraphListReadWriteFile(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: codegraph.AnalysisResult{}})
	s.container.ConfigDir = t.TempDir()
	dir := t.TempDir()

	if _, err := s.CodeGraphOpenProject(dir); err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}

	if err := s.CodeGraphWriteFile("notes.md", "# hello"); err != nil {
		t.Fatalf("CodeGraphWriteFile: %v", err)
	}
	got, err := s.CodeGraphReadFile("notes.md")
	if err != nil {
		t.Fatalf("CodeGraphReadFile: %v", err)
	}
	if got != "# hello" {
		t.Fatalf("CodeGraphReadFile = %q, want %q", got, "# hello")
	}

	entries, err := s.CodeGraphListFiles()
	if err != nil {
		t.Fatalf("CodeGraphListFiles: %v", err)
	}
	if len(entries) != 1 || entries[0].Path != "notes.md" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestCodeGraphGetCodeSnippet(t *testing.T) {
	s := newTestService(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Foo.java"), []byte("class Foo {\n    void bar() {\n    }\n}\n"), 0o644); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	result := codegraph.AnalysisResult{
		Nodes: []codegraph.Node{
			{ID: "m1", Label: "com.acme.Foo.bar", Kind: "method", Layer: "unknown", FilePath: "Foo.java", Line: 2, EndLine: 3},
		},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	s.container.ConfigDir = t.TempDir()

	if _, err := s.CodeGraphOpenProject(dir); err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}

	snippet, err := s.CodeGraphGetCodeSnippet("Foo.java", 2, 2)
	if err != nil {
		t.Fatalf("CodeGraphGetCodeSnippet: %v", err)
	}
	if snippet.FQN != "com.acme.Foo.bar" {
		t.Fatalf("snippet.FQN = %q, want com.acme.Foo.bar", snippet.FQN)
	}
}

func TestCodeGraphGetBlastRadius(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		FileDependencies: []codegraph.FileDependency{
			{FilePath: "Controller.java", DependsOnFilePath: "Service.java"},
		},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	if err := s.container.CodeGraph.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	radius, err := s.CodeGraphGetBlastRadius("Service.java")
	if err != nil {
		t.Fatalf("CodeGraphGetBlastRadius: %v", err)
	}
	if len(radius) != 1 || radius[0] != "Controller.java" {
		t.Fatalf("unexpected radius: %+v", radius)
	}
}

func TestCodeGraphWaitForIndexReady(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: codegraph.AnalysisResult{}})
	if err := s.container.CodeGraph.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { s.container.CodeGraph.Close() })

	version, err := s.CodeGraphWaitForIndexReady()
	if err != nil {
		t.Fatalf("CodeGraphWaitForIndexReady: %v", err)
	}
	if version != 1 {
		t.Fatalf("version = %d, want 1", version)
	}
}

func TestCodeGraphGetNeighbors(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		Nodes: []codegraph.Node{
			{ID: "n1", Label: "A", Kind: "class", Layer: "unknown", FilePath: "A.java", Line: 1, EndLine: 1},
			{ID: "n2", Label: "B", Kind: "class", Layer: "unknown", FilePath: "B.java", Line: 1, EndLine: 1},
		},
		Edges: []codegraph.Edge{{From: "n1", To: "n2", Kind: "call"}},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	if err := s.container.CodeGraph.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { s.container.CodeGraph.Close() })

	sub, err := s.CodeGraphGetNeighbors("n1", 1, codegraph.NeighborFilter{})
	if err != nil {
		t.Fatalf("CodeGraphGetNeighbors: %v", err)
	}
	if len(sub.Nodes) != 2 {
		t.Fatalf("unexpected subgraph: %+v", sub)
	}
}

func TestCodeGraphTraceDataFlow(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		Nodes: []codegraph.Node{
			{ID: "cls", Label: "com.acme.Order", Kind: "class", Layer: "unknown", FilePath: "Order.java", Line: 1, EndLine: 50},
			{ID: "m1", Label: "com.acme.Order.setTotal", Kind: "method", Layer: "unknown", FilePath: "Order.java", Line: 10, EndLine: 12},
		},
		FieldAccesses: []codegraph.FieldAccess{
			{ClassID: "cls", NodeID: "m1", FieldName: "total", Kind: "write", Line: 11},
		},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	if err := s.container.CodeGraph.SetProjectPath(filepath.Join(t.TempDir(), "index.db")); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	t.Cleanup(func() { s.container.CodeGraph.Close() })

	trace, err := s.CodeGraphTraceDataFlow("cls", "total")
	if err != nil {
		t.Fatalf("CodeGraphTraceDataFlow: %v", err)
	}
	if len(trace.Steps) != 1 || trace.Steps[0].Kind != "write" {
		t.Fatalf("unexpected trace: %+v", trace)
	}
}

func TestCodeGraphStartTerminal(t *testing.T) {
	s := newTestService(t)
	reg := terminal.NewRegistry(nil)
	s.container.Terminal = reg
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: codegraph.AnalysisResult{}})
	s.container.CodeGraph.SetTerminalRegistry(reg)
	if err := s.container.CodeGraph.SetProjectPath(t.TempDir() + "/index.db"); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sessionID, err := s.CodeGraphStartTerminal(24, 80)
	if err != nil {
		t.Fatalf("CodeGraphStartTerminal: %v", err)
	}
	defer reg.Close(sessionID)

	owner, ok := reg.Owner(sessionID)
	if !ok || owner != "builtin.codegraph" {
		t.Fatalf("Owner = %q, %v", owner, ok)
	}
}

func TestCodeGraphProjectRootEmptyWithoutProject(t *testing.T) {
	// No project open is the ordinary case, not a failure: callers offer the
	// root as a default, and "nothing to offer" must not surface as an error.
	svc := NewAppService(app.NewContainer())
	root, err := svc.CodeGraphProjectRoot()
	if err != nil {
		t.Fatalf("no open project reported as error: %v", err)
	}
	if root != "" {
		t.Fatalf("root = %q, want empty", root)
	}
}

func TestCodeGraphOpenProjectReportsEveryModuleItFound(t *testing.T) {
	// A monorepo indexes as one graph. Without a module count on the way back,
	// four services are indistinguishable on screen from one project that
	// happens to have a lot of endpoints.
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{
		result: codegraph.AnalysisResult{
			Nodes:     []codegraph.Node{{ID: "n1", Label: "A.foo", Kind: "method", Layer: "controller"}},
			Endpoints: []codegraph.Endpoint{{ID: "ep1", HTTPMethod: "GET", Path: "/a", EntryNode: "n1"}},
		},
	})
	s.container.ConfigDir = t.TempDir()
	s.container.CodeGraph.SetToolchains([]codegraph.Toolchain{codegraph.JavaToolchain(".")})

	root := t.TempDir()
	for _, service := range []string{"services/orders", "services/billing", "libs/shared"} {
		dir := filepath.Join(root, filepath.FromSlash(service))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	detected, err := s.CodeGraphOpenProject(root)
	if err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}
	if detected.Language != "java" {
		t.Fatalf("expected the Java toolchain to claim it, got %q", detected.Language)
	}
	if len(detected.Modules) != 3 {
		t.Fatalf("expected three modules, got %v", detected.Modules)
	}
}

func TestCodeGraphOpenProjectRefusesAFolderThatIsNotAProject(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{
		result: codegraph.AnalysisResult{
			Nodes:     []codegraph.Node{{ID: "n1", Label: "A.foo", Kind: "method", Layer: "controller"}},
			Endpoints: []codegraph.Endpoint{{ID: "ep1", HTTPMethod: "GET", Path: "/a", EntryNode: "n1"}},
		},
	})
	s.container.ConfigDir = t.TempDir()
	s.container.CodeGraph.SetToolchains([]codegraph.Toolchain{codegraph.JavaToolchain(".")})

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := s.CodeGraphOpenProject(root)
	if err == nil {
		t.Fatal("a folder of text files is not a project")
	}
	// Its own code, so the UI offers "choose another folder" rather than a
	// retry that could never work.
	if !strings.HasPrefix(err.Error(), "unsupported_project:") {
		t.Fatalf("expected the unsupported_project code, got %q", err.Error())
	}
}
