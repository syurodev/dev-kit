// internal/codegraph/analyzer_subprocess_test.go
package codegraph

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// analyzerJarPath locates the fat jar built by Task 7 Step 1. Tests that
// need a real jar skip (not fail) when it hasn't been built yet, so this
// package's test suite still passes in an environment that only has the Go
// toolchain — the jar is a separate, optional build artifact from a
// different toolchain (Maven), not something `go test` can produce itself.
func analyzerJarPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("../../analyzer/target/codegraph-analyzer.jar")
	if err != nil {
		t.Fatalf("resolve analyzer jar path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("analyzer jar not built at %s (run `cd analyzer && mvn package` first): %v", path, err)
	}
	return path
}

func TestSubprocessAnalyzerAnalyzesSampleProject(t *testing.T) {
	jarPath := analyzerJarPath(t)
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("java not on PATH")
	}

	projectPath, err := filepath.Abs("../../analyzer/src/test/resources/sample-project")
	if err != nil {
		t.Fatalf("resolve sample project path: %v", err)
	}

	analyzer := NewSubprocessAnalyzerWithJar(jarPath)
	result, err := analyzer.Analyze(context.Background(), projectPath)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("analyzer produced an inconsistent graph: %v", err)
	}
	if len(result.Endpoints) != 1 || result.Endpoints[0].Path != "/refunds" {
		t.Fatalf("unexpected endpoints: %+v", result.Endpoints)
	}
}

func TestSubprocessAnalyzerWithoutJarConfiguredReturnsNotConfigured(t *testing.T) {
	analyzer := NewSubprocessAnalyzer()
	_, err := analyzer.Analyze(context.Background(), "/some/project")
	if !errors.Is(err, ErrAnalyzerNotConfigured) {
		t.Fatalf("expected ErrAnalyzerNotConfigured, got %v", err)
	}
}
