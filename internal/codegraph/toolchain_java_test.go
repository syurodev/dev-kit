package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Java-specific tests. Everything here is about analyzer/pom.xml, the JDK and
// Maven — the generic machinery is exercised in toolchain_test.go against
// fixtures that name no language.

func TestJavaToolchainMatchesWhatMainWiresAndThePomDeclares(t *testing.T) {
	toolchain := JavaToolchain("/repo")

	if toolchain.Artifact != filepath.Join("/repo", "analyzer", "target", "codegraph-analyzer.jar") {
		t.Fatalf("artifact path drifted from analyzer/pom.xml's finalName: %q", toolchain.Artifact)
	}
	if toolchain.Dir != filepath.Join("/repo", "analyzer") {
		t.Fatalf("build must run in the analyzer module, got %q", toolchain.Dir)
	}
	// maven.compiler.release in analyzer/pom.xml is 21, so the jar cannot run
	// on anything older. If the pom moves, this is the other place to change.
	var java Requirement
	for _, req := range toolchain.Requirements {
		if req.Program == "java" {
			java = req
		}
	}
	if java.MinVersion != "21" {
		t.Fatalf("java minimum should track maven.compiler.release (21), got %q", java.MinVersion)
	}
}

// TestJavaToolchainBuildsForReal runs the actual Maven build against
// analyzer/. It is the only test that proves the discovered toolchain can
// produce a working jar rather than merely that the plumbing type-checks, and
// the only one that costs minutes and a network — hence the opt-in.
//
//	DEVKIT_ANALYZER_BUILD_E2E=1 go test ./internal/codegraph/ -run BuildsForReal -v -timeout 20m
func TestJavaToolchainBuildsForReal(t *testing.T) {
	if os.Getenv("DEVKIT_ANALYZER_BUILD_E2E") != "1" {
		t.Skip("set DEVKIT_ANALYZER_BUILD_E2E=1 to run the real Maven build")
	}
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains(DefaultToolchains(repoRoot))

	status := service.AnalyzerStatus(context.Background())[0]
	for _, req := range status.Requirements {
		t.Logf("%s: found=%q path=%q source=%q met=%v problem=%q",
			req.Program, req.Found, req.Path, req.Source, req.Met, req.Problem)
	}
	if !status.Buildable {
		t.Skip("this machine has no usable JDK/Maven; nothing to prove here")
	}

	service.OnAnalyzerBuildOutput = func(_, line string) { t.Log(line) }
	if err := service.BuildAnalyzer(context.Background(), "java"); err != nil {
		t.Fatalf("real analyzer build failed: %v", err)
	}
	if !service.AnalyzerStatus(context.Background())[0].Ready {
		t.Fatal("the build reported success but the artifact is not there")
	}

	// The proof that the jar is real: run it over the analyzer's own test
	// fixture project and expect nodes back.
	result, err := service.analyzer.Analyze(context.Background(),
		filepath.Join(repoRoot, "analyzer", "src", "test", "resources", "sample-project"))
	if err != nil {
		t.Fatalf("the freshly built analyzer could not analyze the sample project: %v", err)
	}
	if len(result.Nodes) == 0 {
		t.Fatal("the sample project has classes and methods; an empty index means the jar does not work")
	}
	t.Logf("analyzed the sample project: %d nodes, %d edges", len(result.Nodes), len(result.Edges))
}
