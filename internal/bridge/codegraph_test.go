// internal/bridge/codegraph_test.go
package bridge

import (
	"context"
	"errors"
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
	if err := s.container.CodeGraph.SetProjectPath(t.TempDir() + "/index.db"); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}

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

func TestCodeGraphListEndpointsBeforeOpenProjectFails(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{})

	if _, err := s.CodeGraphListEndpoints(); !errors.Is(err, codegraph.ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}
