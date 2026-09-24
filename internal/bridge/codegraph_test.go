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

	if _, err := s.CodeGraphOpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}

	endpoints, err := s.CodeGraphListEndpoints("")
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

	if _, err := s.CodeGraphOpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("first CodeGraphOpenProject: %v", err)
	}
	if _, err := s.CodeGraphOpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("second CodeGraphOpenProject: %v", err)
	}

	if _, err := s.CodeGraphWaitForIndexReady(""); err != nil {
		t.Fatal(err)
	}

	endpoints, err := s.CodeGraphListEndpoints("")
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

	if _, err := s.CodeGraphListEndpoints(""); !errors.Is(err, codegraph.ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestCodeGraphListReadWriteFile(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: codegraph.AnalysisResult{}})
	s.container.ConfigDir = t.TempDir()
	dir := t.TempDir()

	if _, err := s.CodeGraphOpenProject(context.Background(), dir); err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}

	if err := s.CodeGraphWriteFile("notes.md", "# hello", ""); err != nil {
		t.Fatalf("CodeGraphWriteFile: %v", err)
	}
	got, err := s.CodeGraphReadFile("notes.md", "")
	if err != nil {
		t.Fatalf("CodeGraphReadFile: %v", err)
	}
	if got != "# hello" {
		t.Fatalf("CodeGraphReadFile = %q, want %q", got, "# hello")
	}

	entries, err := s.CodeGraphListFiles("")
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

	if _, err := s.CodeGraphOpenProject(context.Background(), dir); err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}

	snippet, err := s.CodeGraphGetCodeSnippet("Foo.java", 2, 2, "")
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
	if err := s.container.CodeGraph.OpenProjectAt(context.Background(), filepath.Join(t.TempDir(), "index.db"), "/fake/project"); err != nil {
		t.Fatalf("OpenProjectAt: %v", err)
	}

	radius, err := s.CodeGraphGetBlastRadius("Service.java", "")
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
	if err := s.container.CodeGraph.OpenProjectAt(context.Background(), filepath.Join(t.TempDir(), "index.db"), "/fake/project"); err != nil {
		t.Fatalf("OpenProjectAt: %v", err)
	}
	t.Cleanup(func() { s.container.CodeGraph.Close() })

	version, err := s.CodeGraphWaitForIndexReady("")
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
	if err := s.container.CodeGraph.OpenProjectAt(context.Background(), filepath.Join(t.TempDir(), "index.db"), "/fake/project"); err != nil {
		t.Fatalf("OpenProjectAt: %v", err)
	}
	t.Cleanup(func() { s.container.CodeGraph.Close() })

	sub, err := s.CodeGraphGetNeighbors("n1", 1, codegraph.NeighborFilter{}, "")
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
	if err := s.container.CodeGraph.OpenProjectAt(context.Background(), filepath.Join(t.TempDir(), "index.db"), "/fake/project"); err != nil {
		t.Fatalf("OpenProjectAt: %v", err)
	}
	t.Cleanup(func() { s.container.CodeGraph.Close() })

	trace, err := s.CodeGraphTraceDataFlow("cls", "total", "")
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
	if err := s.container.CodeGraph.OpenProjectAt(context.Background(), t.TempDir()+"/index.db", t.TempDir()); err != nil {
		t.Fatalf("OpenProjectAt: %v", err)
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

	detected, err := s.CodeGraphOpenProject(context.Background(), root)
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

	_, err := s.CodeGraphOpenProject(context.Background(), root)
	if err == nil {
		t.Fatal("a folder of text files is not a project")
	}
	// Its own code, so the UI offers "choose another folder" rather than a
	// retry that could never work.
	if !strings.HasPrefix(err.Error(), "unsupported_project:") {
		t.Fatalf("expected the unsupported_project code, got %q", err.Error())
	}
}

// A buffer opened under one project must not be saved into another: the
// frontend passes the root it opened the file under, and a switch in between
// (an agent's codegraph.openProject) turns the save into project_changed.
func TestCodeGraphWriteFileRefusesAStaleProjectRoot(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: codegraph.AnalysisResult{}})
	s.container.ConfigDir = t.TempDir()
	t.Cleanup(func() { s.container.CodeGraph.Close() })
	dirA, dirB := t.TempDir(), t.TempDir()
	if _, err := s.CodeGraphOpenProject(context.Background(), dirA); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CodeGraphOpenProject(context.Background(), dirB); err != nil {
		t.Fatal(err)
	}

	err := s.CodeGraphWriteFile("notes.md", "for A", dirA)
	if err == nil || !strings.HasPrefix(err.Error(), "project_changed:") {
		t.Fatalf("expected the project_changed code, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dirB, "notes.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the refused save wrote into B: %v", statErr)
	}
	if err := s.CodeGraphWriteFile("notes.md", "for B", dirB); err != nil {
		t.Fatalf("a save into the open project: %v", err)
	}
}

func TestCodeGraphCancelAnalyzerBuildWithNothingBuilding(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{})
	if err := s.CodeGraphCancelAnalyzerBuild(); err != nil {
		t.Fatalf("cancelling no build must be a no-op: %v", err)
	}
}

// A call naming a folder reads that folder's index, from a resident service,
// without moving the focused one; "" still means the focused project.
func TestCodeGraphServicePathReadsAnotherFolder(t *testing.T) {
	s := newTestService(t)
	focused := codegraph.AnalysisResult{
		Nodes:     []codegraph.Node{{ID: "n1", Label: "A.foo", Kind: "method", Layer: "controller"}},
		Endpoints: []codegraph.Endpoint{{ID: "ep1", HTTPMethod: "GET", Path: "/focused", EntryNode: "n1"}},
	}
	other := codegraph.AnalysisResult{
		Nodes:     []codegraph.Node{{ID: "n2", Label: "B.bar", Kind: "method", Layer: "controller"}},
		Endpoints: []codegraph.Endpoint{{ID: "ep2", HTTPMethod: "POST", Path: "/other", EntryNode: "n2"}},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: focused})
	s.container.ConfigDir = t.TempDir()
	s.container.CodeGraph.SetConfigDir(s.container.ConfigDir)
	s.container.CodeGraphRegistry = codegraph.NewRegistry(s.container.CodeGraph, -1)
	s.container.CodeGraphRegistry.SetAnalyzer(fakeCodeGraphAnalyzer{result: other})
	t.Cleanup(func() { _ = s.container.CodeGraphRegistry.Shutdown() })

	focusDir, otherDir := t.TempDir(), t.TempDir()
	if _, err := s.CodeGraphOpenProject(context.Background(), focusDir); err != nil {
		t.Fatal(err)
	}

	got, err := s.CodeGraphListEndpoints(otherDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "/other" {
		t.Fatalf("named service endpoints = %+v, want /other", got)
	}
	if got, _ := s.CodeGraphListEndpoints(""); len(got) != 1 || got[0].Path != "/focused" {
		t.Fatalf("focused endpoints = %+v, want /focused", got)
	}
	if root := s.container.CodeGraph.ProjectRoot(); root != focusDir {
		t.Fatalf("focused root = %q, want it unmoved at %q", root, focusDir)
	}

	// Focusing the other folder takes it from its resident.
	if _, err := s.CodeGraphOpenProject(context.Background(), otherDir); err != nil {
		t.Fatal(err)
	}
	if n := len(s.container.CodeGraphRegistry.Resident()); n != 0 {
		t.Fatalf("resident = %d, want the focused folder handed over", n)
	}
}
