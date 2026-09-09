package codegraph

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		found, minimum string
		want           bool
	}{
		{"21", "21", true},
		{"21.0.10", "21", true},
		{"25.0.1", "21", true},
		{"17.0.9", "21", false},
		// Java's legacy scheme needs no special case: it reads as 1.8.0.402,
		// and 1 < 21.
		{"1.8.0_402", "21", false},
		{"3.9.11", "3.6", true},
		{"3.6.3", "3.6", true},
		{"3.5.4", "3.6", false},
		// A version with fewer components than the minimum only satisfies it
		// when the components it is missing are required to be zero.
		{"3", "3.6", false},
		{"3", "3.0", true},
		{"", "21", false},
	}
	for _, tc := range cases {
		if got := versionAtLeast(tc.found, tc.minimum); got != tc.want {
			t.Errorf("versionAtLeast(%q, %q) = %v, want %v", tc.found, tc.minimum, got, tc.want)
		}
	}
}

// echoRequirement is a requirement that is always satisfiable on a unix box,
// so a test can exercise everything around the probe without depending on a
// JDK being installed.
func echoRequirement(minVersion string) Requirement {
	return Requirement{
		Program:        "echo",
		Label:          "Echo",
		MinVersion:     minVersion,
		locate:         onPath("echo"),
		versionArgs:    []string{"version 9.9.9"},
		versionPattern: regexp.MustCompile(`version ([0-9.]+)`),
		InstallHint:    "install echo",
	}
}

// onPath is the simplest possible locator: whatever the shell would run.
func onPath(program string) func(string) []candidate {
	return func(string) []candidate {
		path, err := exec.LookPath(program)
		if err != nil {
			return nil
		}
		return []candidate{{path: path, source: "PATH"}}
	}
}

// atPaths is a locator over a fixed list, for testing preference order without
// depending on what the machine happens to have installed.
func atPaths(paths ...string) func(string) []candidate {
	return func(string) []candidate {
		found := make([]candidate, 0, len(paths))
		for _, path := range paths {
			found = append(found, candidate{path: path, source: "test"})
		}
		return executable(found)
	}
}

func TestRequirementProbeReportsMissingProgram(t *testing.T) {
	req := Requirement{
		Program: "devkit-no-such-program-anywhere",
		Label:   "Nothing",
		locate:  onPath("devkit-no-such-program-anywhere"),
	}
	got := req.probe(context.Background(), "")

	if got.Met {
		t.Fatal("a program found nowhere must not report Met")
	}
	if !strings.Contains(got.Problem, "not found") {
		t.Fatalf("Problem should say what is wrong, got %q", got.Problem)
	}
}

func TestRequirementProbeComparesVersions(t *testing.T) {
	met := echoRequirement("9").probe(context.Background(), "")
	if !met.Met || met.Found != "9.9.9" {
		t.Fatalf("expected a met requirement at 9.9.9, got %+v", met)
	}

	tooOld := echoRequirement("10").probe(context.Background(), "")
	if tooOld.Met {
		t.Fatal("9.9.9 must not satisfy a minimum of 10")
	}
	if !strings.Contains(tooOld.Problem, "older than 10") {
		t.Fatalf("Problem should name the shortfall, got %q", tooOld.Problem)
	}
}

func TestRequirementProbeAcceptsUnreadableVersionBanner(t *testing.T) {
	// A wrapper script, an unusual locale, a JVM whose banner we have not
	// seen: the program is there, so let the build try rather than blocking
	// on a string we failed to parse.
	req := Requirement{
		Program:        "echo",
		Label:          "Echo",
		MinVersion:     "21",
		locate:         onPath("echo"),
		versionArgs:    []string{"nothing version-shaped here"},
		versionPattern: regexp.MustCompile(`version "([0-9.]+)"`),
	}
	got := req.probe(context.Background(), "")

	if !got.Met {
		t.Fatalf("an unparseable banner must not block the build, got %+v", got)
	}
	if got.Found != "" {
		t.Fatalf("Found should stay empty when nothing parsed, got %q", got.Found)
	}
}

// writingToolchain builds a fake analyzer: a shell command that prints two
// lines and writes the artifact. It stands in for Maven everywhere the test
// cares about the machinery around the build rather than the build itself.
func writingToolchain(t *testing.T, script string) Toolchain {
	t.Helper()
	dir := t.TempDir()
	return Toolchain{
		Language:     "fake",
		Label:        "Fake",
		Artifact:     filepath.Join(dir, "analyzer.jar"),
		Dir:          dir,
		Build:        []string{"sh", "-c", script},
		Requirements: []Requirement{echoRequirement("1")},
	}
}

func TestAnalyzerStatusReportsReadyOnceTheArtifactExists(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	toolchain := writingToolchain(t, "true")
	service.SetToolchains([]Toolchain{toolchain})

	statuses := service.AnalyzerStatus(context.Background())
	if len(statuses) != 1 {
		t.Fatalf("expected one status, got %d", len(statuses))
	}
	if statuses[0].Ready {
		t.Fatal("no artifact on disk yet — Ready must be false")
	}
	if !statuses[0].Buildable {
		t.Fatalf("every requirement is met, so Buildable must be true: %+v", statuses[0])
	}
	if statuses[0].BuildCommand != "sh -c true" {
		t.Fatalf("BuildCommand should be the argv joined for display, got %q", statuses[0].BuildCommand)
	}

	if err := os.WriteFile(toolchain.Artifact, []byte("jar"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !service.AnalyzerStatus(context.Background())[0].Ready {
		t.Fatal("artifact exists — Ready must be true")
	}
}

func TestAnalyzerStatusIgnoresAnEmptyArtifact(t *testing.T) {
	// A zero-byte jar is what a killed build leaves behind. Wiring the
	// analyzer at it would trade a clear "not built" for an obscure crash
	// inside java.
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	toolchain := writingToolchain(t, "true")
	service.SetToolchains([]Toolchain{toolchain})

	if err := os.WriteFile(toolchain.Artifact, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if service.AnalyzerStatus(context.Background())[0].Ready {
		t.Fatal("a zero-byte artifact must not count as ready")
	}
}

func TestBuildAnalyzerStreamsOutputAndInstallsTheResult(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	toolchain := writingToolchain(t, "echo building; echo done >&2; printf jar > analyzer.jar")
	service.SetToolchains([]Toolchain{toolchain})

	var mu sync.Mutex
	var lines []string
	service.OnAnalyzerBuildOutput = func(language, line string) {
		mu.Lock()
		defer mu.Unlock()
		if language != "fake" {
			t.Errorf("output tagged with the wrong language: %q", language)
		}
		lines = append(lines, line)
	}

	if err := service.BuildAnalyzer(context.Background(), "fake"); err != nil {
		t.Fatalf("build should succeed: %v", err)
	}

	mu.Lock()
	got := strings.Join(lines, "\n")
	mu.Unlock()
	// Both streams, through one pipe, so the order they happened in survives.
	if !strings.Contains(got, "building") || !strings.Contains(got, "done") {
		t.Fatalf("stdout and stderr should both reach the listener, got %q", got)
	}

	// Installed without a restart: the analyzer is no longer the unconfigured
	// one, so OpenProject gets past ErrAnalyzerNotConfigured.
	if _, err := service.analyzer.Analyze(context.Background(), t.TempDir()); errors.Is(err, ErrAnalyzerNotConfigured) {
		t.Fatal("a successful build must install the analyzer it produced")
	}
}

func TestBuildAnalyzerRejectsAnUnknownLanguage(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains([]Toolchain{writingToolchain(t, "true")})

	err := service.BuildAnalyzer(context.Background(), "cobol")
	if !errors.Is(err, ErrUnknownToolchain) {
		t.Fatalf("expected ErrUnknownToolchain, got %v", err)
	}
}

func TestBuildAnalyzerRefusesWhenARequirementIsMissing(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	toolchain := writingToolchain(t, "printf jar > analyzer.jar")
	toolchain.Requirements = []Requirement{{
		Program: "devkit-no-such-program-anywhere",
		Label:   "Nothing",
		locate:  onPath("devkit-no-such-program-anywhere"),
	}}
	service.SetToolchains([]Toolchain{toolchain})

	err := service.BuildAnalyzer(context.Background(), "fake")
	if !errors.Is(err, ErrToolchainIncomplete) {
		t.Fatalf("expected ErrToolchainIncomplete, got %v", err)
	}
	if _, statErr := os.Stat(toolchain.Artifact); statErr == nil {
		t.Fatal("the build must not run at all when a requirement is missing")
	}
}

func TestBuildAnalyzerReportsAFailedBuild(t *testing.T) {
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains([]Toolchain{writingToolchain(t, "echo boom >&2; exit 1")})

	var lines []string
	service.OnAnalyzerBuildOutput = func(_, line string) { lines = append(lines, line) }

	err := service.BuildAnalyzer(context.Background(), "fake")
	if err == nil {
		t.Fatal("a build that exits non-zero must be an error")
	}
	// The failure reason has to reach the user, and it is in the output — the
	// exit status alone says nothing.
	if !strings.Contains(strings.Join(lines, "\n"), "boom") {
		t.Fatalf("a failing build's output must still be streamed, got %v", lines)
	}
}

func TestBuildAnalyzerRejectsABuildThatProducesNothing(t *testing.T) {
	// Maven can exit 0 having skipped the shade plugin, or write the jar under
	// a different finalName. Wiring the analyzer at a path that does not exist
	// would turn that into an obscure failure at the next index instead.
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains([]Toolchain{writingToolchain(t, "true")})

	err := service.BuildAnalyzer(context.Background(), "fake")
	if !errors.Is(err, ErrArtifactMissing) {
		t.Fatalf("expected ErrArtifactMissing, got %v", err)
	}
}

func TestBuildAnalyzerRefusesAConcurrentBuild(t *testing.T) {
	// Two Maven runs writing the same target/ produce one corrupt jar.
	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	started := make(chan struct{})
	release := make(chan struct{})
	toolchain := writingToolchain(t, "printf jar > analyzer.jar")
	service.SetToolchains([]Toolchain{toolchain})
	service.OnAnalyzerBuildOutput = func(string, string) {}

	// Hold the first build open by blocking its output listener.
	service.OnAnalyzerBuildOutput = func(string, string) {
		close(started)
		<-release
	}
	toolchain.Build = []string{"sh", "-c", "echo hold; printf jar > analyzer.jar"}
	service.SetToolchains([]Toolchain{toolchain})

	done := make(chan error, 1)
	go func() { done <- service.BuildAnalyzer(context.Background(), "fake") }()

	<-started
	if err := service.BuildAnalyzer(context.Background(), "fake"); !errors.Is(err, ErrBuildAlreadyRunning) {
		t.Fatalf("expected ErrBuildAlreadyRunning, got %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("the first build should still succeed: %v", err)
	}
}

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

// fakeProgram writes an executable shell script that prints a version banner,
// standing in for a JDK sitting somewhere PATH has never heard of.
func fakeProgram(t *testing.T, dir, name, banner string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho '" + banner + "' >&2\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProbePicksTheNewestVersionThatClearsTheMinimum(t *testing.T) {
	// ~/.jdks routinely holds a 17 next to a 21. Taking the first match would
	// fail a machine that has exactly what the build needs.
	root := t.TempDir()
	old := fakeProgram(t, filepath.Join(root, "jdk-17"), "java", `openjdk version "17.0.9" 2023-10-17`)
	newer := fakeProgram(t, filepath.Join(root, "jdk-21"), "java", `openjdk version "21.0.10" 2026-01-20`)
	newest := fakeProgram(t, filepath.Join(root, "jdk-25"), "java", `openjdk version "25.0.1" 2026-07-15`)

	req := Requirement{
		Program:        "java",
		Label:          "Java Development Kit",
		MinVersion:     "21",
		locate:         atPaths(old, newer, newest),
		versionArgs:    []string{"-version"},
		versionPattern: regexp.MustCompile(`version "([0-9][0-9._]*)"`),
	}
	got := req.probe(context.Background(), "")

	if !got.Met {
		t.Fatalf("a 21 and a 25 are both present, so the requirement is met: %+v", got)
	}
	if got.Path != newest {
		t.Fatalf("expected the newest JDK at %q, got %q (%s)", newest, got.Path, got.Found)
	}
}

func TestProbeNamesTheClosestMissWhenNothingQualifies(t *testing.T) {
	// "Java 17.0.9 is older than 21" is actionable. "java was not found" on a
	// machine that plainly has java is not.
	root := t.TempDir()
	eight := fakeProgram(t, filepath.Join(root, "jdk-8"), "java", `openjdk version "1.8.0_402"`)
	seventeen := fakeProgram(t, filepath.Join(root, "jdk-17"), "java", `openjdk version "17.0.9" 2023-10-17`)

	req := Requirement{
		Program:        "java",
		Label:          "Java Development Kit",
		MinVersion:     "21",
		locate:         atPaths(eight, seventeen),
		versionArgs:    []string{"-version"},
		versionPattern: regexp.MustCompile(`version "([0-9][0-9._]*)"`),
	}
	got := req.probe(context.Background(), "")

	if got.Met {
		t.Fatal("neither JDK clears 21")
	}
	if got.Found != "17.0.9" {
		t.Fatalf("the message should name the closest miss, got %q", got.Found)
	}
	if !strings.Contains(got.Problem, "older than 21") {
		t.Fatalf("Problem should say what is short, got %q", got.Problem)
	}
}

func TestBuildRunsTheResolvedProgramWithItsToolchainInTheEnvironment(t *testing.T) {
	// The machine this matters for has IntelliJ and nothing on PATH: the build
	// must run the Maven that was found, with JAVA_HOME pointing at the JDK
	// that was found, and with both on PATH so Maven's own launcher and its
	// forked compiler can see them.
	root := t.TempDir()
	java := fakeProgram(t, filepath.Join(root, "ide", "jbr", "bin"), "java", `openjdk version "21.0.10"`)

	dir := t.TempDir()
	// Stands in for a bundled mvn: it records what it was handed.
	mvn := filepath.Join(root, "ide", "plugins", "maven", "lib", "maven3", "bin", "mvn")
	if err := os.MkdirAll(filepath.Dir(mvn), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-version\" ]; then echo 'Apache Maven 3.9.11'; exit 0; fi\n" +
		"echo \"JAVA_HOME=$JAVA_HOME\"\n" +
		"echo \"PATH=$PATH\"\n" +
		"printf jar > analyzer.jar\n"
	if err := os.WriteFile(mvn, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	service := NewService(nilStore{}, NewSubprocessAnalyzer())
	service.SetToolchains([]Toolchain{{
		Language: "fake",
		Label:    "Fake",
		Artifact: filepath.Join(dir, "analyzer.jar"),
		Dir:      dir,
		Build:    []string{"mvn", "package"},
		HomeEnv:  map[string]string{"JAVA_HOME": "java"},
		Requirements: []Requirement{
			{
				Program: "java", Label: "JDK", MinVersion: "21", locate: atPaths(java),
				versionArgs: []string{"-version"}, versionPattern: regexp.MustCompile(`version "([0-9][0-9._]*)"`),
			},
			{
				Program: "mvn", Label: "Maven", MinVersion: "3.6", locate: atPaths(mvn),
				versionArgs: []string{"-version"}, versionPattern: regexp.MustCompile(`Apache Maven ([0-9][0-9.]*)`),
			},
		},
	}})

	var mu sync.Mutex
	var lines []string
	service.OnAnalyzerBuildOutput = func(_, line string) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, line)
	}

	if err := service.BuildAnalyzer(context.Background(), "fake"); err != nil {
		t.Fatalf("build should succeed: %v", err)
	}

	mu.Lock()
	got := strings.Join(lines, "\n")
	mu.Unlock()

	// JAVA_HOME is the install root, one level above bin.
	wantHome := filepath.Dir(filepath.Dir(java))
	if !strings.Contains(got, "JAVA_HOME="+wantHome) {
		t.Fatalf("JAVA_HOME should point at the JDK that was found (%s), got:\n%s", wantHome, got)
	}
	if !strings.Contains(got, filepath.Dir(java)) {
		t.Fatalf("the JDK's bin should be on PATH so Maven can fork javac, got:\n%s", got)
	}
}

func TestJavaAndMavenAreDiscoverableOffPath(t *testing.T) {
	// A regression guard for the whole point of locate.go: the candidate lists
	// must consider more than PATH. Asserting on the shape rather than on this
	// machine's contents, since CI may have neither an IDE nor a JDK.
	javaHome := t.TempDir()
	java := fakeProgram(t, filepath.Join(javaHome, "bin"), "java", `openjdk version "21.0.10"`)
	t.Setenv("JAVA_HOME", javaHome)

	var found bool
	for _, c := range javaCandidates("") {
		if c.path == java && c.source == "JAVA_HOME" {
			found = true
		}
	}
	if !found {
		t.Fatal("JAVA_HOME must be searched — a JDK the user configured is the one they meant")
	}

	// The Maven Wrapper wins over everything: if the project ships one, it is
	// by definition the Maven this build was meant to use.
	dir := t.TempDir()
	wrapper := fakeProgram(t, dir, "mvnw", "Apache Maven 3.9.11")
	candidates := mavenCandidates(dir)
	if len(candidates) == 0 || candidates[0].path != wrapper {
		t.Fatalf("a project's own mvnw should come first, got %+v", candidates)
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
