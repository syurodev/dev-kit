package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// javaHomeRoot is what stops JAVA_HOME being invented for a shim. macOS ships
// /usr/bin/java as a stub shared by every JDK tool, so <root>/bin/java reads it
// as /usr — and `JAVA_HOME=/usr /usr/bin/java -version` never returns, which is
// how one wedged analyzer subprocess used to hang a whole test run for ten
// minutes and orphan the JVM behind it.
func TestJavaHomeRootAnswersOnlyForARealJDK(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	java := filepath.Join(bin, "java")
	if err := os.WriteFile(java, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := javaHomeRoot(java); got != "" {
		t.Fatalf("a bin/java with no JDK beside it must yield no JAVA_HOME, got %q", got)
	}

	// `release` is the manifest every JDK since 9 ships at its root.
	if err := os.WriteFile(filepath.Join(root, "release"), []byte("JAVA_VERSION=\"21\"\n"), 0o644); err != nil {
		t.Fatalf("write release: %v", err)
	}
	if got := javaHomeRoot(java); got != root {
		t.Fatalf("javaHomeRoot = %q, want %q", got, root)
	}
}

// A toolchain whose runtime has no install root must set no *_HOME at all. An
// absent JAVA_HOME leaves the runtime to work it out; a wrong one hangs it.
func TestEnvironOmitsHomeForARuntimeWithNoRoot(t *testing.T) {
	toolchain := Toolchain{HomeEnv: map[string]string{"JAVA_HOME": "java"}}
	shim := Requirement{
		Program:  "java",
		Path:     "/usr/bin/java",
		homeRoot: func(string) string { return "" },
	}

	for _, entry := range toolchain.environ(map[string]Requirement{"java": shim}) {
		if strings.HasPrefix(entry, "JAVA_HOME=") {
			t.Fatalf("a shim must contribute no JAVA_HOME, got %q", entry)
		}
	}
}

// PATH is searched last so a version tie goes to a directory that can serve as
// JAVA_HOME. Maven needs one; a shim cannot give it.
func TestJavaCandidatesSearchPathLast(t *testing.T) {
	candidates := javaCandidates("")
	if len(candidates) < 2 {
		t.Skipf("machine has %d java candidate(s); ordering is unobservable", len(candidates))
	}
	for i, c := range candidates[:len(candidates)-1] {
		if c.source == "PATH" {
			t.Fatalf("PATH candidate at position %d of %d, want last", i, len(candidates))
		}
	}
}
