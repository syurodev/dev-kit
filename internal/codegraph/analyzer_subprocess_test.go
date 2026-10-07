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

// The whole path on the feedback's own example: a flag in application.yaml
// fed by an environment variable, injected with @Value, branched on, behind
// an endpoint — from the real jar, through the index, to "what changes".
func TestSubprocessAnalyzerFeedsFindConfigUsage(t *testing.T) {
	toolchain := builtJavaAnalyzer(t)
	dir := t.TempDir()
	files := map[string]string{
		"src/main/resources/application.yaml": "fsap:\n  access:\n    module-based-enabled: ${FSAP_ACCESS_MODULE_BASED_ENABLED:false}\n  db:\n    password: hunter2\n",
		"src/main/java/acme/AccessController.java": `package acme;
import org.springframework.web.bind.annotation.*;
@RestController
class AccessController {
    private final AccessService service = new AccessService();
    @GetMapping("/v1/access/{org}")
    boolean get(@PathVariable String org) { return service.isAvailable(org); }
}
`,
		"src/main/java/acme/AccessService.java": `package acme;
import org.springframework.beans.factory.annotation.Value;
class AccessService {
    @Value("${fsap.access.module-based-enabled:false}")
    private boolean moduleBasedAccessEnabled;
    boolean isAvailable(String org) {
        if (moduleBasedAccessEnabled) { return byModule(org); }
        return byRole(org);
    }
    boolean byModule(String org) { return true; }
    boolean byRole(String org) { return false; }
}
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := toolchain.Analyzer().Analyze(t.Context(), dir)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	svc := openWith(t, result)

	report, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "FSAP_ACCESS_MODULE_BASED_ENABLED"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Keys) != 1 || report.Keys[0].File != "src/main/resources/application.yaml" || report.Keys[0].Line != 3 {
		t.Fatalf("keys = %+v", report.Keys)
	}
	if len(report.Usages) != 1 || report.Usages[0].Kind != ConfigUseValue || report.Usages[0].Field != "moduleBasedAccessEnabled" {
		t.Fatalf("usages = %+v", report.Usages)
	}
	u := report.Usages[0]
	if len(u.Readers) != 1 || len(u.Guarded) != 2 {
		t.Fatalf("readers = %+v, guarded = %+v", u.Readers, u.Guarded)
	}
	if report.EntryPointTotal != 1 || report.EntryPoints[0].Target != "/v1/access/{org}" {
		t.Fatalf("entry points = %+v", report.EntryPoints)
	}

	secret, err := svc.FindConfigUsage(t.Context(), ConfigUsageQuery{Key: "fsap.db.password"})
	if err != nil {
		t.Fatal(err)
	}
	if len(secret.Keys) != 1 || !secret.Keys[0].Redacted || secret.Keys[0].Value != "" {
		t.Fatalf("a secret must come back redacted: %+v", secret.Keys)
	}
}

// The whole path on the feedback's table question: an entity with the default
// table name, a service that saves it and a repository that purges it, two
// endpoints — from the real jar, through the index, to who reads or writes it.
func TestSubprocessAnalyzerFeedsFindTableUsage(t *testing.T) {
	toolchain := builtJavaAnalyzer(t)
	dir := t.TempDir()
	files := map[string]string{
		"src/main/java/acme/OrganizationApiEnablement.java": `package acme;
import jakarta.persistence.*;
@Entity
class OrganizationApiEnablement { @Id Long id; String org; }
`,
		"src/main/java/acme/EnablementRepo.java": `package acme;
import org.springframework.data.jpa.repository.*;
import org.springframework.data.repository.CrudRepository;
interface EnablementRepo extends CrudRepository<OrganizationApiEnablement, Long> {
    @Modifying
    @Query("delete from OrganizationApiEnablement e where e.org = ?1")
    void purge(String org);
}
`,
		"src/main/java/acme/EnablementService.java": `package acme;
class EnablementService {
    private final EnablementRepo repo = null;
    void enable(OrganizationApiEnablement e) { repo.save(e); }
    void reset(String org) { repo.purge(org); }
}
`,
		"src/main/java/acme/Controllers.java": `package acme;
import org.springframework.web.bind.annotation.*;
@RestController
class Controllers {
    private final EnablementService service = new EnablementService();
    @PostMapping("/v1/enable") void enable(@RequestBody OrganizationApiEnablement e) { service.enable(e); }
    @PostMapping("/admin/reset") void reset(@RequestParam String org) { service.reset(org); }
}
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := toolchain.Analyzer().Analyze(t.Context(), dir)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	svc := openWith(t, result)

	report, err := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: "organization_api_enablement"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entities) != 1 || report.Entities[0].Table != "organization_api_enablement" {
		t.Fatalf("entities = %+v", report.Entities)
	}
	if report.AccessTotal != 2 || report.Writes != 2 {
		t.Fatalf("accesses = %+v", report.Accesses)
	}
	vias := map[string]string{}
	for _, a := range report.Accesses {
		vias[a.Via] = a.Label
	}
	if vias[TableViaCrud] != "acme.EnablementService.enable" || vias[TableViaQuery] != "acme.EnablementRepo.purge" {
		t.Fatalf("vias = %v", vias)
	}
	if report.EntryPointTotal != 2 {
		t.Fatalf("entry points = %+v", report.EntryPoints)
	}
	for _, e := range report.EntryPoints {
		if e.Access != "write" {
			t.Fatalf("%s reaches the table by %q, want write", e.Target, e.Access)
		}
	}
}
