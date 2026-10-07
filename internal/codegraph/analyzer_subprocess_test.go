// internal/codegraph/analyzer_subprocess_test.go
package codegraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// builtJavaAnalyzer returns a Java toolchain whose artifact is actually on
// disk. Tests that need a real analyzer skip (not fail) when it has not been
// built, so this package's suite still passes in an environment that only has
// the Go toolchain — the jar comes from a different toolchain (Maven), not
// something `go test` can produce itself.
func builtJavaAnalyzer(t *testing.T) Toolchain {
	t.Helper()
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	toolchain := JavaToolchain(repoRoot)
	if toolchain.installedArtifact() == "" {
		t.Skipf("analyzer not built (run `cd analyzer && mvn package` first); searched %v", toolchain.Search)
	}
	return toolchain
}

func TestSubprocessAnalyzerAnalyzesSampleProject(t *testing.T) {
	toolchain := builtJavaAnalyzer(t)

	projectPath, err := filepath.Abs("../../analyzer/src/test/resources/sample-project")
	if err != nil {
		t.Fatalf("resolve sample project path: %v", err)
	}

	// No LookPath guard: the toolchain finds its own runtime wherever it lives,
	// which is the whole point — a JDK inside an IDE is not on PATH.
	analyzer := toolchain.Analyzer()
	result, err := analyzer.Analyze(context.Background(), projectPath)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("analyzer produced an inconsistent graph: %v", err)
	}
	if len(result.EntryPoints) != 1 || result.EntryPoints[0].Target != "/refunds" {
		t.Fatalf("unexpected endpoints: %+v", result.EntryPoints)
	}

	// The analyzer derives file-level dependencies from resolved call edges:
	// Controller -> Service -> Repository, two cross-file deps, no self-deps.
	want := map[string]string{
		"src/main/java/com/acme/refund/RefundController.java": "src/main/java/com/acme/refund/RefundService.java",
		"src/main/java/com/acme/refund/RefundService.java":    "src/main/java/com/acme/refund/RefundRepository.java",
	}
	if len(result.FileDependencies) != len(want) {
		t.Fatalf("expected %d file dependencies, got %+v", len(want), result.FileDependencies)
	}
	for _, fd := range result.FileDependencies {
		if want[fd.FilePath] != fd.DependsOnFilePath {
			t.Fatalf("unexpected file dependency %+v (want set %+v)", fd, want)
		}
	}

	// Verify params are emitted for methods
	var foundCreateMethod bool
	for _, n := range result.Nodes {
		if n.Kind == "method" && n.Label == "com.acme.refund.RefundController.create" {
			foundCreateMethod = true
			if len(n.Params) != 1 || n.Params[0].Name != "requestId" || n.Params[0].Type != "String" {
				t.Fatalf("unexpected params for create method: %+v", n.Params)
			}
		}
	}
	if !foundCreateMethod {
		t.Fatalf("RefundController.create method node not found in result: %+v", result.Nodes)
	}

	// Verify field accesses are emitted
	var foundRefundRepoAccess bool
	for _, fa := range result.FieldAccesses {
		if fa.ClassID == "com.acme.refund.RefundService" && fa.FieldName == "refundRepository" && fa.Kind == "read" {
			foundRefundRepoAccess = true
		}
	}
	if !foundRefundRepoAccess {
		t.Fatalf("RefundService.refundRepository field access not found in result: %+v", result.FieldAccesses)
	}
}

func TestSubprocessAnalyzerWithoutAToolchainReturnsNotConfigured(t *testing.T) {
	analyzer := NewSubprocessAnalyzer()
	_, err := analyzer.Analyze(context.Background(), "/some/project")
	if !errors.Is(err, ErrAnalyzerNotConfigured) {
		t.Fatalf("expected ErrAnalyzerNotConfigured, got %v", err)
	}
}

func TestToolchainAnalyzerWithoutABuiltArtifactReturnsNotConfigured(t *testing.T) {
	// A toolchain that knows how to build an analyzer but has not yet is not
	// the same as no analyzer at all — but from a caller's side it fails the
	// same way, which is what keeps the setup panel's trigger a single error.
	_, err := JavaToolchain(t.TempDir()).Analyzer().Analyze(context.Background(), "/some/project")
	if !errors.Is(err, ErrAnalyzerNotConfigured) {
		t.Fatalf("expected ErrAnalyzerNotConfigured, got %v", err)
	}
}

// The guard a call runs under survives the jar's JSON and arrives on the Go
// Edge — the one place the Java and Go halves of the feature meet.
func TestSubprocessAnalyzerCarriesEdgeGuards(t *testing.T) {
	toolchain := builtJavaAnalyzer(t)
	dir := t.TempDir()
	source := `package acme;
class AccessService {
    private boolean moduleBasedAccessEnabled;
    boolean isAvailable(String org) {
        if (moduleBasedAccessEnabled) { return byModule(org); }
        return byRole(org);
    }
    boolean byModule(String org) { return true; }
    boolean byRole(String org) { return org != null; }
}
`
	if err := os.WriteFile(filepath.Join(dir, "AccessService.java"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := toolchain.Analyzer().Analyze(t.Context(), dir)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	guards := map[string]string{}
	for _, e := range result.Edges {
		guards[e.To] = e.Guard
	}
	if got := guards["acme.AccessService.byModule(java.lang.String)"]; got != "moduleBasedAccessEnabled" {
		t.Fatalf("byModule guard = %q, edges %+v", got, result.Edges)
	}
	if got := guards["acme.AccessService.byRole(java.lang.String)"]; got != "!moduleBasedAccessEnabled" {
		t.Fatalf("byRole guard = %q, edges %+v", got, result.Edges)
	}
}
