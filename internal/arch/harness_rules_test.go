package arch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// These paths are accepted package boundaries, even before the production
// packages exist. A policy applies to the named package and its descendants,
// so adding a helper subpackage cannot escape the parent's boundary.
type packageImportPolicy struct {
	Scope      string
	Allowed    []string
	Denied     []string
	StdlibOnly bool
}

var harnessPackageImports = []packageImportPolicy{
	{Scope: pkg("harnessruntime/kernel"), StdlibOnly: true},
	{Scope: pkg("harnessruntime/contracts"), Allowed: []string{subtree(pkg("harnessruntime/kernel"))}, StdlibOnly: true},
	{Scope: pkg("execstore"), Allowed: []string{subtree(pkg("harnessruntime/kernel")), subtree(pkg("harnessruntime/contracts")), pkg("crypto")}},
	{Scope: pkg("harnessruntime/coordinator"), Allowed: []string{subtree(pkg("harnessruntime/kernel")), subtree(pkg("harnessruntime/contracts"))}, Denied: []string{"database/sql", "database/sql/driver"}},
	{Scope: pkg("harnessruntime/contextengine"), Allowed: []string{subtree(pkg("harnessruntime/contracts"))}},
	{Scope: pkg("harnessruntime/modeladapter"), Allowed: []string{subtree(pkg("harnessruntime/contracts"))}},
	{Scope: pkg("harnessruntime/tools"), Allowed: []string{subtree(pkg("harnessruntime/contracts"))}},
	{Scope: pkg("hostapi/gen"), Allowed: []string{subtree(pkg("hostapi/gen"))}},
	{Scope: pkg("hostapi"), Allowed: []string{pkg("app"), subtree(pkg("hostapi/gen"))}},
}

// Literal importer rules make process-root construction reviewable. The app
// receives only the contracts interface; concrete runtime parts are assembled
// at the root. Host API is the sole transport allowed to call the app API.
type packageImporterPolicy struct {
	Target          string
	Allowed         []string
	DeniedImporters []string
}

var harnessPackageImporters = []packageImporterPolicy{
	{Target: pkg("app"), Allowed: []string{"devkit", pkg("bridge"), subtree(pkg("hostapi"))}, DeniedImporters: []string{subtree(pkg("hostapi/gen"))}},
	{Target: pkg("harnessruntime/kernel"), Allowed: []string{subtree(pkg("harnessruntime/contracts")), subtree(pkg("execstore")), subtree(pkg("harnessruntime/coordinator"))}},
	{Target: pkg("harnessruntime/contracts"), Allowed: []string{"devkit", pkg("app"), subtree(pkg("execstore")), subtree(pkg("harnessruntime/coordinator")), subtree(pkg("harnessruntime/contextengine")), subtree(pkg("harnessruntime/modeladapter")), subtree(pkg("harnessruntime/tools"))}},
	{Target: pkg("execstore"), Allowed: []string{"devkit"}},
	{Target: pkg("harnessruntime/coordinator"), Allowed: []string{"devkit"}},
	{Target: pkg("harnessruntime/contextengine"), Allowed: []string{"devkit"}},
	{Target: pkg("harnessruntime/modeladapter"), Allowed: []string{"devkit"}},
	{Target: pkg("harnessruntime/tools"), Allowed: []string{"devkit"}},
	{Target: pkg("hostapi"), Allowed: []string{"devkit"}},
	// Generated messages stay behind the host adapter. Generated packages may
	// refer to sibling generated packages, but no shell/domain package may import
	// generated DTOs directly.
	{Target: pkg("hostapi/gen"), Allowed: []string{subtree(pkg("hostapi"))}},
}

const harnessRuntimePrefix = "devkit/internal/harnessruntime"
const generatedHostAPIPrefix = "devkit/internal/hostapi/gen"

// Keep this list in sync with the provider SDKs selected by model-adapter.md.
// It is intentionally narrow: ordinary libraries and transport SDKs are not
// classified as model-provider SDKs by this rule.
var providerSDKPrefixes = []string{
	"github.com/anthropics/anthropic-sdk-go",
	"github.com/openai/openai-go",
	"github.com/sashabaranov/go-openai",
}

func TestHarnessPackageImportsMatchAcceptedPolicy(t *testing.T) {
	graph := fullImportGraph(t)
	for _, violation := range validateHarnessImportGraph(graph) {
		t.Error(violation)
	}
}

func TestHarnessPolicyValidatorWithSyntheticGraphs(t *testing.T) {
	allowed := map[string][]string{
		"devkit":                     {pkg("app"), pkg("execstore"), pkg("harnessruntime/contracts"), pkg("harnessruntime/coordinator"), pkg("harnessruntime/contextengine"), pkg("harnessruntime/modeladapter"), pkg("harnessruntime/tools"), pkg("hostapi/service")},
		pkg("bridge"):                {pkg("app")},
		pkg("app"):                   {pkg("harnessruntime/contracts")},
		pkg("harnessruntime/kernel"): {"strings", "time"},
		"devkit/internal/harnessruntime/kernel/decision": {"strings"},
		pkg("execstore"):                    {pkg("harnessruntime/kernel"), pkg("harnessruntime/contracts"), pkg("crypto"), "database/sql"},
		pkg("harnessruntime/contracts"):     {pkg("harnessruntime/kernel")},
		pkg("harnessruntime/coordinator"):   {pkg("harnessruntime/kernel"), pkg("harnessruntime/contracts"), "time"},
		pkg("harnessruntime/contextengine"): {pkg("harnessruntime/contracts"), "strings"},
		pkg("harnessruntime/modeladapter"):  {pkg("harnessruntime/contracts"), "github.com/anthropics/anthropic-sdk-go"},
		pkg("harnessruntime/tools"):         {pkg("harnessruntime/contracts")},
		pkg("hostapi/service"):              {pkg("app"), pkg("hostapi/gen/fugue/session/v1"), "connectrpc.com/connect"},
		pkg("hostapi/gen/fugue/session/v1"): {pkg("hostapi/gen/shared/v1"), "google.golang.org/protobuf/proto"},
		pkg("hostapi/gen/shared/v1"):        {"google.golang.org/protobuf/reflect/protoreflect"},
		pkg("agenthub"):                     {"github.com/openai/openai-go/v2"}, // Existing vendor adapters are outside this new-native-runtime policy.
	}
	if got := validateHarnessImportGraph(allowed); len(got) != 0 {
		t.Fatalf("valid synthetic graph rejected:\n%s", strings.Join(got, "\n"))
	}

	for name, tc := range map[string]struct {
		graph map[string][]string
		want  string
	}{
		"coordinator SQL": {
			graph: map[string][]string{pkg("harnessruntime/coordinator"): {pkg("harnessruntime/kernel"), "database/sql"}},
			want:  "denied by policy scope devkit/internal/harnessruntime/coordinator",
		},
		"coordinator storage": {
			graph: map[string][]string{pkg("harnessruntime/coordinator"): {pkg("harnessruntime/contracts"), pkg("storage")}},
			want:  "may not import devkit/internal/storage",
		},
		"coordinator provider SDK": {
			graph: map[string][]string{pkg("harnessruntime/coordinator"): {pkg("harnessruntime/contracts"), "github.com/openai/openai-go/v2"}},
			want:  "imports provider SDK",
		},
		"coordinator subpackage SQL bypass": {
			graph: map[string][]string{"devkit/internal/harnessruntime/coordinator/queue": {"database/sql"}},
			want:  "denied by policy scope devkit/internal/harnessruntime/coordinator",
		},
		"runtime transport": {
			graph: map[string][]string{pkg("harnessruntime/tools"): {pkg("harnessruntime/contracts"), pkg("mcp")}},
			want:  "may not import devkit/internal/mcp",
		},
		"app concrete runtime": {
			graph: map[string][]string{pkg("app"): {pkg("harnessruntime/contracts"), pkg("execstore")}},
			want:  "construct runtime components at process root",
		},
		"app nested concrete runtime": {
			graph: map[string][]string{"devkit/internal/app/hosted": {pkg("harnessruntime/coordinator")}},
			want:  "construct runtime components at process root",
		},
		"generated proto imported outside hostapi": {
			graph: map[string][]string{"devkit/internal/agenthub": {pkg("hostapi/gen/fugue/session/v1")}},
			want:  "outside hostapi",
		},
		"process root imports generated proto": {
			graph: map[string][]string{"devkit": {pkg("hostapi/gen/fugue/session/v1")}},
			want:  "outside hostapi",
		},
		"cmd imports generated proto": {
			graph: map[string][]string{"devkit/cmd/fugue": {pkg("hostapi/gen/fugue/session/v1")}},
			want:  "outside hostapi",
		},
		"cmd imports concrete runtime": {
			graph: map[string][]string{"devkit/cmd/fugue": {pkg("harnessruntime/coordinator")}},
			want:  "may only be imported by: devkit",
		},
		"generated proto imports app": {
			graph: map[string][]string{pkg("hostapi/gen/fugue/session/v1"): {pkg("app")}},
			want:  "may not import devkit/internal/app",
		},
		"MCP imports app": {
			graph: map[string][]string{pkg("mcp"): {pkg("app")}},
			want:  "may only be imported by",
		},
		"unclassified runtime package": {
			graph: map[string][]string{"devkit/internal/harnessruntime/coordinatorish": {pkg("storage")}},
			want:  "unclassified package under harnessruntime",
		},
		"prefix-like non-child": {
			graph: map[string][]string{"devkit/internal/harnessruntime/kernelish": {pkg("storage")}},
			want:  "unclassified package under harnessruntime",
		},
		"kernel third-party dependency is caught by graph policy": {
			graph: map[string][]string{pkg("harnessruntime/kernel"): {"github.com/google/uuid"}},
			want:  "may use standard-library imports only",
		},
		"contracts third-party dependency": {
			graph: map[string][]string{pkg("harnessruntime/contracts"): {"github.com/google/uuid"}},
			want:  "may use standard-library imports only",
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := validateHarnessImportGraph(tc.graph)
			joined := strings.Join(got, "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("expected violation containing %q, got:\n%s", tc.want, joined)
			}
		})
	}
}

func TestHarnessPolicyCoversDescendantsWithoutMatchingSiblingPrefixes(t *testing.T) {
	if !pathWithin(pkg("harnessruntime/coordinator/queue"), pkg("harnessruntime/coordinator")) {
		t.Fatal("coordinator subpackage did not inherit coordinator policy")
	}
	if pathWithin(pkg("harnessruntime/coordinatorish"), pkg("harnessruntime/coordinator")) {
		t.Fatal("sibling prefix incorrectly matched coordinator policy")
	}
	if !pathWithin(pkg("hostapi/gen/fugue/session/v1"), generatedHostAPIPrefix) {
		t.Fatal("generated package subpath was not recognized")
	}
}

func TestSourceImportGraphIncludesCommandAndAlternateBuildTags(t *testing.T) {
	files := []prodFile{
		parseSyntheticProdFile(t, "cmd/fugue/main.go", `package main
import "devkit/internal/harnessruntime/coordinator"`),
		parseSyntheticProdFile(t, "cmd/fugue/windows_only.go", `//go:build windows
package main
import "devkit/internal/hostapi/gen/fugue/session/v1"`),
	}
	graph := map[string][]string{}
	addSourceImportsToGraph(graph, files)
	if got := validateHarnessImportGraph(graph); !containsViolation(got, "imports generated proto package devkit/internal/hostapi/gen/fugue/session/v1 outside hostapi") {
		t.Fatalf("alternate build-tag import was not rejected:\n%s", strings.Join(got, "\n"))
	}
	if !containsViolation(validateHarnessImportGraph(graph), "devkit/cmd/fugue imports devkit/internal/harnessruntime/coordinator, which may only be imported by: devkit") {
		t.Fatal("cmd package was not checked as an importer of concrete runtime packages")
	}
}

func parseSyntheticProdFile(t *testing.T, rel, source string) prodFile {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, source, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return prodFile{Rel: rel, Dir: path.Dir(rel), Src: []byte(source), File: file, Fset: fset}
}

func TestAppImporterBoundaryAllowsHostAPIButKeepsMCPForbidden(t *testing.T) {
	for importer, want := range map[string]bool{
		"devkit":                          true,
		pkg("bridge"):                     true,
		pkg("hostapi/session"):            true,
		pkg("hostapi/gen/session/v1"):     false,
		pkg("mcp"):                        false,
		pkg("app/agenthub"):               false,
		pkg("harnessruntime/coordinator"): false,
	} {
		if got := appImporterAllowed(importer); got != want {
			t.Errorf("appImporterAllowed(%q) = %v, want %v", importer, got, want)
		}
	}
}

func TestHarnessKernelSourceGuards(t *testing.T) {
	for _, f := range prodFiles(t) {
		importPath := "devkit/" + f.Dir
		if !pathWithin(importPath, pkg("harnessruntime/kernel")) {
			continue
		}
		for _, problem := range kernelSourceViolations(f.File) {
			t.Errorf("%s: %s", f.Rel, problem)
		}
	}
}

func TestHarnessKernelSourceValidator(t *testing.T) {
	for name, source := range map[string]string{
		"pure standard library": `package kernel
import "strings"
func normalize(s string) string { return strings.TrimSpace(s) }`,
		"time facts": `package kernel
import "time"
func elapsed(value time.Duration) time.Duration { return value + time.Second }`,
		"clock call": `package kernel
import t "time"
func decide() { _ = t.Now() }`,
		"clock function reference": `package kernel
import t "time"
var now = t.Now`,
		"dot time import bypass": `package kernel
import . "time"
func decide() { _ = Now() }`,
		"randomness": `package kernel
import r "crypto/rand"
func decide(b []byte) { _, _ = r.Read(b) }`,
		"filesystem IO": `package kernel
import "os"
func decide() { _, _ = os.ReadFile("state") }`,
		"builtin output": `package kernel
func decide() { println("event") }`,
		"formatted output reference": `package kernel
import f "fmt"
var output = f.Printf`,
		"dot fmt import bypass": `package kernel
import . "fmt"
func decide() { Printf("event") }`,
		"third-party dependency": `package kernel
import "github.com/google/uuid"
func decide() { _ = uuid.New() }`,
	} {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "kernel.go", source, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			problems := kernelSourceViolations(file)
			if (name == "pure standard library" || name == "time facts") && len(problems) != 0 {
				t.Fatalf("pure source rejected: %v", problems)
			}
			if name != "pure standard library" && name != "time facts" && len(problems) == 0 {
				t.Fatal("forbidden source passed")
			}
		})
	}
}

// validateHarnessImportGraph is also exercised with synthetic graphs so these
// boundaries fail closed before their corresponding production packages land.
func validateHarnessImportGraph(graph map[string][]string) []string {
	var problems []string
	for importer, imports := range graph {
		policy, hasPolicy := mostSpecificPackagePolicy(importer, harnessPackageImports)
		if pathWithin(importer, harnessRuntimePrefix) && !hasPolicy {
			problems = append(problems, fmt.Sprintf("%s: unclassified package under harnessruntime", importer))
		}
		for _, imported := range imports {
			if hasPolicy && isInModule(imported) && !matchesAnyPath(imported, policy.Allowed) {
				problems = append(problems, fmt.Sprintf("%s may not import %s (policy scope %s allows: %s)", importer, imported, policy.Scope, allowedList(policy.Allowed)))
			}
			if hasPolicy && policy.StdlibOnly && !isInModule(imported) && !isStandardLibraryImport(imported) {
				problems = append(problems, fmt.Sprintf("%s may use standard-library imports only; found %s", importer, imported))
			}
			if hasPolicy && matchesAnyPath(imported, policy.Denied) {
				problems = append(problems, fmt.Sprintf("%s may not import %s (denied by policy scope %s)", importer, imported, policy.Scope))
			}
			if inNewHarnessSurface(importer) && isProviderSDK(imported) && !pathWithin(importer, pkg("harnessruntime/modeladapter")) {
				problems = append(problems, fmt.Sprintf("%s imports provider SDK %s outside modeladapter", importer, imported))
			}
			if pathWithin(imported, generatedHostAPIPrefix) && !pathWithin(importer, pkg("hostapi")) {
				problems = append(problems, fmt.Sprintf("%s imports generated proto package %s outside hostapi", importer, imported))
			}
			if pathWithin(importer, pkg("app")) && (pathWithin(imported, harnessRuntimePrefix) || pathWithin(imported, pkg("execstore"))) && !pathWithin(imported, pkg("harnessruntime/contracts")) {
				problems = append(problems, fmt.Sprintf("%s may import only harnessruntime/contracts, not %s; construct runtime components at process root", importer, imported))
			}
		}
	}
	for importer, imports := range graph {
		for _, imported := range imports {
			policy, ok := mostSpecificImporterPolicy(imported, harnessPackageImporters)
			if ok && !packageImporterAllowed(importer, policy) {
				problems = append(problems, fmt.Sprintf("%s imports %s, which may only be imported by: %s", importer, imported, allowedList(policy.Allowed)))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

func mostSpecificPackagePolicy(importer string, policies []packageImportPolicy) (packageImportPolicy, bool) {
	var best packageImportPolicy
	found := false
	for _, policy := range policies {
		if pathWithin(importer, policy.Scope) && (!found || len(policy.Scope) > len(best.Scope)) {
			best, found = policy, true
		}
	}
	return best, found
}

func mostSpecificImporterPolicy(imported string, policies []packageImporterPolicy) (packageImporterPolicy, bool) {
	var best packageImporterPolicy
	found := false
	for _, policy := range policies {
		if pathWithin(imported, policy.Target) && (!found || len(policy.Target) > len(best.Target)) {
			best, found = policy, true
		}
	}
	return best, found
}

func packageImporterAllowed(importer string, policy packageImporterPolicy) bool {
	if matchesAnyPath(importer, policy.DeniedImporters) {
		return false
	}
	return matchesAnyPath(importer, policy.Allowed)
}

func pathWithin(candidate, prefix string) bool {
	return candidate == prefix || strings.HasPrefix(candidate, strings.TrimSuffix(prefix, "/")+"/")
}

func subtree(prefix string) string { return strings.TrimSuffix(prefix, "/") + "/..." }

func matchesAnyPath(candidate string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.HasSuffix(pattern, "/...") && pathWithin(candidate, strings.TrimSuffix(pattern, "/...")) || candidate == pattern {
			return true
		}
	}
	return false
}

func isInModule(importPath string) bool { return strings.HasPrefix(importPath, modulePrefix) }

func isStandardLibraryImport(importPath string) bool {
	if importPath == "C" || isInModule(importPath) {
		return false
	}
	first := strings.SplitN(importPath, "/", 2)[0]
	return !strings.Contains(first, ".")
}

func inNewHarnessSurface(importer string) bool {
	return pathWithin(importer, harnessRuntimePrefix) || pathWithin(importer, pkg("execstore")) || pathWithin(importer, pkg("hostapi"))
}

func isProviderSDK(importPath string) bool {
	for _, prefix := range providerSDKPrefixes {
		if pathWithin(importPath, prefix) {
			return true
		}
	}
	return false
}

func appImporterAllowed(importer string) bool {
	policy, ok := mostSpecificImporterPolicy(pkg("app"), harnessPackageImporters)
	return ok && packageImporterAllowed(importer, policy)
}

func containsViolation(problems []string, want string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, want) {
			return true
		}
	}
	return false
}

// addSourceImportsToGraph unions imports from all production source files into
// go list's current-platform graph. prodFiles includes cmd and every build-tag
// variant, so package boundaries cannot change by moving an import into a
// Windows-only, Linux-only, or otherwise inactive source file.
func addSourceImportsToGraph(graph map[string][]string, files []prodFile) {
	for _, file := range files {
		importer := "devkit"
		if file.Dir != "." {
			importer += "/" + file.Dir
		}
		seen := map[string]bool{}
		for _, imported := range graph[importer] {
			seen[imported] = true
		}
		for _, spec := range file.File.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil || seen[imported] {
				continue
			}
			graph[importer] = append(graph[importer], imported)
			seen[imported] = true
		}
	}
}

func fullImportGraph(t *testing.T) map[string][]string {
	t.Helper()
	// The server-tagged root package is the process composition point for the
	// future runtime. Avoid GUI-only build constraints while still checking its
	// actual imports alongside internal packages.
	out := goList(t, "{{.ImportPath}}\t{{join .Imports \" \"}}", "-tags=server", ".", "./internal/...")
	graph := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			graph[fields[0]] = nil
			continue
		}
		graph[fields[0]] = strings.Fields(fields[1])
	}
	addSourceImportsToGraph(graph, prodFiles(t))
	return graph
}

var kernelForbiddenImports = []string{
	"crypto/rand", "database/sql", "database/sql/driver", "io", "log", "log/slog", "math/rand", "net", "os", "runtime", "syscall", "unsafe",
}

var kernelClockFunctions = map[string]bool{
	"After": true, "AfterFunc": true, "LoadLocation": true, "Local": true,
	"NewTicker": true, "NewTimer": true, "Now": true, "Since": true,
	"Sleep": true, "Tick": true, "Until": true,
}

var fmtEffectCalls = map[string]bool{
	"Fprint": true, "Fprintf": true, "Fprintln": true,
	"Print": true, "Printf": true, "Println": true,
	"Scan": true, "Scanf": true, "Scanln": true,
	"Fscan": true, "Fscanf": true, "Fscanln": true,
}

// kernelSourceViolations is a syntactic guard for common clock, randomness,
// I/O, and third-party dependency paths. It is not a semantic proof of purity;
// reviews still need to consider function effects and nondeterminism.
func kernelSourceViolations(file *ast.File) []string {
	var problems []string
	importedName := map[string]string{}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if importPath == "C" {
			problems = append(problems, "kernel must not use cgo")
		}
		name := path.Base(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
			if name == "." {
				problems = append(problems, "kernel must not use dot imports; they can hide clock or I/O operations")
			}
			if name == "_" {
				problems = append(problems, "kernel must not use side-effect-only imports")
			}
		}
		importedName[name] = importPath
		if isStdlibForbiddenKernelImport(importPath) {
			problems = append(problems, "kernel must not import nondeterministic or I/O package "+importPath)
		}
		if isInModule(importPath) || strings.Contains(strings.SplitN(importPath, "/", 2)[0], ".") {
			problems = append(problems, "kernel must use standard-library imports only; found "+importPath)
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok && (id.Name == "print" || id.Name == "println") {
				problems = append(problems, "kernel must not write output with builtin "+id.Name)
			}
		case *ast.SelectorExpr:
			qualifier, ok := n.X.(*ast.Ident)
			if !ok {
				return true
			}
			importPath := importedName[qualifier.Name]
			if importPath == "time" && kernelClockFunctions[n.Sel.Name] {
				problems = append(problems, "kernel must not read or schedule from the clock with time."+n.Sel.Name)
			}
			if importPath == "fmt" && fmtEffectCalls[n.Sel.Name] {
				problems = append(problems, "kernel must not perform I/O with fmt."+n.Sel.Name)
			}
		case *ast.GoStmt:
			problems = append(problems, "kernel must not start goroutines")
		case *ast.SelectStmt:
			problems = append(problems, "kernel must not use nondeterministic select")
		}
		return true
	})
	sort.Strings(problems)
	return problems
}

func isStdlibForbiddenKernelImport(importPath string) bool {
	for _, prefix := range kernelForbiddenImports {
		if pathWithin(importPath, prefix) {
			return true
		}
	}
	return false
}
