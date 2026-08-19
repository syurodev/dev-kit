// internal/bridge/codegraph_test.go
package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"devkit/internal/codegraph"
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

	if err := s.CodeGraphOpenProject("/fake/project"); err != nil {
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

	if err := s.CodeGraphOpenProject("/fake/project"); err != nil {
		t.Fatalf("first CodeGraphOpenProject: %v", err)
	}
	if err := s.CodeGraphOpenProject("/fake/project"); err != nil {
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

	if err := s.CodeGraphOpenProject(dir); err != nil {
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

	if err := s.CodeGraphOpenProject(dir); err != nil {
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
