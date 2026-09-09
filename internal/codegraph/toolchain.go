// internal/codegraph/toolchain.go
package codegraph

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	// ErrUnknownToolchain is returned by BuildAnalyzer for a language no
	// toolchain has been registered for.
	ErrUnknownToolchain = errors.New("codegraph: unknown analyzer toolchain")
	// ErrToolchainIncomplete is returned by BuildAnalyzer when the machine is
	// missing a program the build needs. The status carries which one and what
	// to install; this is the error the caller turns into that message.
	ErrToolchainIncomplete = errors.New("codegraph: analyzer toolchain is incomplete")
	// ErrBuildAlreadyRunning is returned when a second build is requested
	// while one is in flight. Two Maven runs writing the same target/ produce
	// a corrupt jar rather than two jars.
	ErrBuildAlreadyRunning = errors.New("codegraph: analyzer build already running")
	// ErrArtifactMissing is returned when a build reported success but the
	// artifact is not where the toolchain says it should be — a renamed
	// finalName, a skipped shade plugin. Failing here beats wiring an
	// analyzer at a path that does not exist.
	ErrArtifactMissing = errors.New("codegraph: analyzer build produced no artifact")
)

// Requirement is one external program an analyzer toolchain needs before it
// can be built, together with what was actually found on this machine.
//
// The install hint is deliberately per-requirement rather than one paragraph
// per toolchain: the answer to "Java is missing" and "Maven is too old" are
// different sentences, and a user reads the one that applies to them.
type Requirement struct {
	Program string `json:"program"`
	Label   string `json:"label"`
	// MinVersion is compared component-by-component against Found. Empty
	// means being present is the whole requirement.
	MinVersion string `json:"minVersion"`
	// Found is the version detected on this machine, or "" when nothing was
	// found (or what was found did not answer its version flag).
	Found string `json:"found"`
	// Path is the executable that will actually be used, and Source names
	// where it came from — "PATH", "JAVA_HOME", "IDE-bundled Maven". Worth
	// showing: on a machine with several JDKs, "which one" is the question the
	// user is about to ask next.
	Path   string `json:"path"`
	Source string `json:"source"`
	Met    bool   `json:"met"`
	// Problem says what is wrong in one phrase, empty when Met. It is written
	// for display, not for parsing.
	Problem     string `json:"problem"`
	InstallHint string `json:"installHint"`

	// How the probe works, not something the UI should render. locate lists
	// where to look, in preference order, given the toolchain's build
	// directory.
	locate         func(dir string) []candidate
	versionArgs    []string
	versionPattern *regexp.Regexp
}

// Toolchain is everything needed to turn one language's analyzer source into
// the artifact the Analyzer subprocess runs.
//
// One per language. CodeGraph ships Java today and the module is meant to grow
// others, so the shape here is the extension point: a new language is a new
// Toolchain value with its own requirements and build command, and every
// caller — status probing, the build runner, the UI's setup panel — keeps
// working without knowing which languages exist.
type Toolchain struct {
	Language string
	Label    string
	// Artifact is where the build lands and where the analyzer is wired from.
	Artifact string
	// Dir is the working directory the build command runs in.
	Dir string
	// Build is the argv to run. Not a shell string: no quoting rules, no shell
	// on the path between us and the process.
	//
	// Build[0] names a requirement rather than a binary: it is replaced with
	// whatever path that requirement resolved to, so a Maven found inside
	// IntelliJ is invoked where it actually lives instead of being looked up
	// on a PATH it is not on.
	Build []string
	// HomeEnv maps an environment variable to the requirement whose install
	// root should fill it — {"JAVA_HOME": "java"} for Maven. Without it a
	// bundled Maven launched by absolute path has no idea which JDK to use and
	// fails on exactly the machine this discovery exists to support: one with
	// IntelliJ and nothing on PATH.
	HomeEnv      map[string]string
	Requirements []Requirement
}

// ToolchainStatus is the snapshot the frontend renders. Everything the setup
// panel needs to decide what to say is on it, so the UI never re-derives a
// policy the backend already applied.
type ToolchainStatus struct {
	Language string `json:"language"`
	Label    string `json:"label"`
	// Ready means the artifact exists and the analyzer can run right now.
	Ready bool `json:"ready"`
	// Buildable means every requirement is met, so offering a build button is
	// honest. False with Ready false is the "install these first" case.
	Buildable    bool          `json:"buildable"`
	Artifact     string        `json:"artifact"`
	BuildCommand string        `json:"buildCommand"`
	Requirements []Requirement `json:"requirements"`
}

// JavaToolchain describes the bundled Java analyzer.
//
// The paths are relative, which ties this to a process started from the repo
// root — the same dev-time assumption main.go has always made for the jar, and
// the same one a packaged build will have to replace (see
// NewSubprocessAnalyzerWithJar's doc comment). Passing the root in rather than
// resolving it here keeps that decision at the wiring layer where it belongs.
//
// Java 21 is not a guess: analyzer/pom.xml sets maven.compiler.release to 21,
// so the jar's own bytecode requires a 21+ runtime. A newer JDK is fine to
// build and run with — what the analyzer can PARSE is fixed by
// ParserConfiguration.LanguageLevel in ProjectAnalyzer.java, not by the JDK it
// happens to run on.
func JavaToolchain(repoRoot string) Toolchain {
	return Toolchain{
		Language: "java",
		Label:    "Java",
		Artifact: filepath.Join(repoRoot, "analyzer", "target", "codegraph-analyzer.jar"),
		Dir:      filepath.Join(repoRoot, "analyzer"),
		// Build[0] is the requirement name, swapped for the Maven that was
		// actually found. -B keeps the output line-oriented instead of
		// redrawing a progress bar with carriage returns, which is what makes
		// it readable when streamed into a log view a line at a time.
		Build: []string{"mvn", "-B", "-DskipTests", "package"},
		// Maven finds its JDK through JAVA_HOME. Set from whichever java the
		// probe settled on, so a Maven bundled inside an IDE builds with the
		// IDE's JDK without either being on PATH.
		HomeEnv: map[string]string{"JAVA_HOME": "java"},
		Requirements: []Requirement{
			{
				Program:    "java",
				Label:      "Java Development Kit",
				MinVersion: "21",
				locate:     javaCandidates,
				// java writes its version to stderr, hence CombinedOutput in
				// the probe. The quoted form covers both the modern "21.0.10"
				// and the legacy "1.8.0_402", which compares correctly against
				// 21 without special-casing: 1 < 21.
				versionArgs:    []string{"-version"},
				versionPattern: regexp.MustCompile(`version "([0-9][0-9._]*)"`),
				InstallHint:    "Install a JDK 21 or newer — `sudo apt install openjdk-21-jdk` on Ubuntu, `brew install openjdk@21` on macOS. IntelliJ users already have one: File → Project Structure → SDKs, or let the IDE download one into ~/.jdks.",
			},
			{
				Program:    "mvn",
				Label:      "Apache Maven",
				MinVersion: "3.6",
				locate:     mavenCandidates,
				// maven-shade-plugin 3.6.0 in analyzer/pom.xml needs 3.6.3+;
				// 3.6 is the floor this checks, close enough to catch the
				// genuinely ancient ones without failing a working machine.
				versionArgs:    []string{"-version"},
				versionPattern: regexp.MustCompile(`Apache Maven ([0-9][0-9.]*)`),
				InstallHint:    "Install Maven — `sudo apt install maven` on Ubuntu, `brew install maven` on macOS. IntelliJ bundles one; DevKit uses it automatically if it can find the IDE.",
			},
		},
	}
}

// DefaultToolchains is the registry: every analyzer DevKit knows how to build.
func DefaultToolchains(repoRoot string) []Toolchain {
	return []Toolchain{JavaToolchain(repoRoot)}
}

// SetToolchains registers what BuildAnalyzer can build and AnalyzerStatus can
// report on. A Service with none — every test double, the in-memory container
// — reports an empty list rather than failing, which is the truth: it has no
// analyzer to build.
func (s *Service) SetToolchains(toolchains []Toolchain) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolchains = toolchains
}

func (s *Service) toolchainList() []Toolchain {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.toolchains
}

// AnalyzerStatus probes the machine once per registered toolchain and reports
// what is ready, what is buildable, and what is missing.
//
// Probing shells out to `java -version` and `mvn -version`, so this is not
// free — call it when the user is looking at the setup panel, not on a timer.
func (s *Service) AnalyzerStatus(ctx context.Context) []ToolchainStatus {
	toolchains := s.toolchainList()
	statuses := make([]ToolchainStatus, 0, len(toolchains))
	for _, tc := range toolchains {
		statuses = append(statuses, tc.status(ctx))
	}
	return statuses
}

func (t Toolchain) status(ctx context.Context) ToolchainStatus {
	requirements, buildable := t.resolve(ctx)
	return ToolchainStatus{
		Language:     t.Language,
		Label:        t.Label,
		Ready:        t.artifactExists(),
		Buildable:    buildable,
		Artifact:     t.Artifact,
		BuildCommand: strings.Join(t.Build, " "),
		Requirements: requirements,
	}
}

func (t Toolchain) artifactExists() bool {
	info, err := os.Stat(t.Artifact)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// resolve probes every requirement and reports whether all of them are met.
func (t Toolchain) resolve(ctx context.Context) ([]Requirement, bool) {
	requirements := make([]Requirement, 0, len(t.Requirements))
	buildable := true
	for _, req := range t.Requirements {
		resolved := req.probe(ctx, t.Dir)
		buildable = buildable && resolved.Met
		requirements = append(requirements, resolved)
	}
	return requirements, buildable
}

// probe walks the candidate list and settles on the best one: the newest
// version that clears MinVersion.
//
// Newest rather than first, because the candidate list is a list of PLACES and
// a place can hold anything — ~/.jdks routinely has a 17 sitting next to a 21,
// and picking whichever the glob returned first would fail a machine that has
// exactly what the build needs. First-match is only the tiebreak, which is what
// keeps JAVA_HOME and PATH ahead of a stray JDK when both would do.
func (r Requirement) probe(ctx context.Context, dir string) Requirement {
	var candidates []candidate
	if r.locate != nil {
		candidates = r.locate(dir)
	}
	if len(candidates) == 0 {
		r.Problem = fmt.Sprintf("%s was not found on PATH or in the usual install locations", r.Program)
		return r
	}

	best := r
	var newestSeen Requirement
	for _, c := range candidates {
		got := r.inspect(ctx, c)
		if got.Met {
			if !best.Met || compareVersions(got.Found, best.Found) > 0 {
				best = got
			}
			continue
		}
		// Remember the closest miss so the message can name a real version
		// ("Java 17.0.9 is older than 21") instead of a generic absence.
		if got.Found != "" && compareVersions(got.Found, newestSeen.Found) > 0 {
			newestSeen = got
		}
	}
	if best.Met {
		return best
	}
	if newestSeen.Found != "" {
		return newestSeen
	}
	r.Problem = fmt.Sprintf("%s was found but would not report its version", r.Program)
	return r
}

// inspect runs one candidate's version flag and judges it.
func (r Requirement) inspect(ctx context.Context, c candidate) Requirement {
	r.Path = c.path
	r.Source = c.source

	// CombinedOutput because `java -version` writes to stderr, and a version
	// banner is small enough that buffering it is not a concern.
	cmd := exec.CommandContext(ctx, c.path, r.versionArgs...)
	// A bundled Maven needs a JDK to even print its version. Give it the one
	// the user configured; if that is wrong the java requirement reports it
	// separately.
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		r.Problem = fmt.Sprintf("%s could not be run: %v", c.path, err)
		return r
	}
	match := r.versionPattern.FindSubmatch(out)
	if match == nil {
		// There and runnable but its banner is not one we recognise — a
		// wrapper script, an unusual locale, a JVM that prints something new.
		// Let the build try rather than blocking on a string we failed to
		// parse.
		r.Met = true
		return r
	}
	r.Found = string(match[1])
	if r.MinVersion == "" || versionAtLeast(r.Found, r.MinVersion) {
		r.Met = true
		return r
	}
	r.Problem = fmt.Sprintf("%s %s is older than %s", r.Label, r.Found, r.MinVersion)
	return r
}

var versionComponent = regexp.MustCompile(`[0-9]+`)

// compareVersions orders two dotted versions component by component, reading
// every run of digits and ignoring the separators between them. That is what
// makes Java's legacy "1.8.0_402" comparable to "21" without a special case:
// it reads as 1.8.0.402, and 1 < 21.
//
// A missing component counts as zero, so "3" and "3.0" are equal and "3" is
// below "3.6". An empty version sorts below everything, which is what lets the
// probe use it as the starting point for "newest seen".
func compareVersions(a, b string) int {
	left := versionComponent.FindAllString(a, -1)
	right := versionComponent.FindAllString(b, -1)
	for i := 0; i < len(left) || i < len(right); i++ {
		leftN := versionPart(left, i)
		rightN := versionPart(right, i)
		if leftN != rightN {
			if leftN > rightN {
				return 1
			}
			return -1
		}
	}
	return 0
}

func versionPart(components []string, i int) int {
	if i >= len(components) {
		return 0
	}
	n, err := strconv.Atoi(components[i])
	if err != nil {
		return 0
	}
	return n
}

func versionAtLeast(found, minimum string) bool {
	// An absent version is never good enough; without this "" would tie with
	// "0" and satisfy a minimum of "0".
	if found == "" {
		return false
	}
	return compareVersions(found, minimum) >= 0
}

// BuildAnalyzer builds one language's analyzer and, on success, installs it —
// the caller does not have to restart the app or re-wire anything.
//
// Every line the build writes goes to OnAnalyzerBuildOutput as it arrives, so
// the UI can show a real build rather than a spinner: a Maven run that
// downloads its dependencies takes minutes on a cold cache, and a progress bar
// that cannot say why is indistinguishable from a hang.
//
// The build runs unsandboxed, as the same user, exactly as typing the command
// in a terminal would: it is the developer's own repository being built with
// the developer's own toolchain, and Maven needs the network and ~/.m2 to do
// its job at all.
func (s *Service) BuildAnalyzer(ctx context.Context, language string) error {
	var toolchain Toolchain
	found := false
	for _, tc := range s.toolchainList() {
		if tc.Language == language {
			toolchain, found = tc, true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrUnknownToolchain, language)
	}

	if !s.buildMu.TryLock() {
		return ErrBuildAlreadyRunning
	}
	defer s.buildMu.Unlock()

	resolved, buildable := toolchain.resolve(ctx)
	if !buildable {
		for _, req := range resolved {
			if !req.Met {
				return fmt.Errorf("%w: %s", ErrToolchainIncomplete, req.Problem)
			}
		}
	}

	if err := toolchain.run(ctx, resolved, func(line string) {
		if s.OnAnalyzerBuildOutput != nil {
			s.OnAnalyzerBuildOutput(language, line)
		}
	}); err != nil {
		return err
	}
	if !toolchain.artifactExists() {
		return fmt.Errorf("%w: expected %s", ErrArtifactMissing, toolchain.Artifact)
	}

	s.SetAnalyzer(NewSubprocessAnalyzerWithJar(toolchain.Artifact))
	return nil
}

func (t Toolchain) run(ctx context.Context, resolved []Requirement, onLine func(string)) error {
	if len(t.Build) == 0 {
		return fmt.Errorf("%w: %s has no build command", ErrUnknownToolchain, t.Language)
	}
	byProgram := make(map[string]Requirement, len(resolved))
	for _, req := range resolved {
		byProgram[req.Program] = req
	}

	// Build[0] names a requirement; run whatever path it resolved to. Without
	// this a bundled Maven would be re-looked-up on a PATH it is not on.
	program := t.Build[0]
	if req, ok := byProgram[program]; ok && req.Path != "" {
		program = req.Path
	}
	cmd := exec.CommandContext(ctx, program, t.Build[1:]...)
	cmd.Dir = t.Dir
	cmd.Env = t.environ(byProgram)

	// One pipe for both streams: Maven writes warnings to stderr and the build
	// log to stdout, and a reader who sees them apart cannot reconstruct the
	// order they happened in — which is the only thing that makes a failing
	// build readable.
	pipeReader, pipeWriter := io.Pipe()
	cmd.Stdout = pipeWriter
	cmd.Stderr = pipeWriter

	if err := cmd.Start(); err != nil {
		pipeWriter.Close()
		pipeReader.Close()
		return fmt.Errorf("codegraph: start %s build: %w", t.Language, err)
	}

	var streamed sync.WaitGroup
	streamed.Add(1)
	go func() {
		defer streamed.Done()
		scanner := bufio.NewScanner(pipeReader)
		// A stack trace or a dependency-resolution line can be far longer than
		// bufio's 64KiB default, and a scanner that overflows stops silently.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			if onLine != nil {
				onLine(scanner.Text())
			}
		}
		// Drain rather than report: the process's own exit status below is the
		// authority on whether the build worked.
		io.Copy(io.Discard, pipeReader)
	}()

	waitErr := cmd.Wait()
	// Close the writer so the scanner sees EOF, then wait for it to finish, so
	// no line arrives after this function has returned.
	pipeWriter.Close()
	streamed.Wait()
	pipeReader.Close()

	if waitErr != nil {
		return fmt.Errorf("codegraph: %s analyzer build failed: %w", t.Language, waitErr)
	}
	return nil
}

// environ hands the build the toolchain it was probed with, rather than
// whatever the app happened to inherit.
//
// Two things go in: the *_HOME variables named by HomeEnv, and the directory of
// every resolved program prepended to PATH. The second matters as much as the
// first — Maven's launcher, its plugins and the forked compiler all look for
// `java` by name, and on the machine this discovery exists for (IntelliJ
// installed, nothing on PATH) they would not find one.
func (t Toolchain) environ(byProgram map[string]Requirement) []string {
	env := os.Environ()
	var pathPrefix []string
	for _, req := range byProgram {
		if req.Path != "" {
			pathPrefix = append(pathPrefix, filepath.Dir(req.Path))
		}
	}
	// Map iteration is unordered and PATH order decides which java wins; sort
	// so the same machine builds the same way twice.
	sort.Strings(pathPrefix)

	for variable, program := range t.HomeEnv {
		req, ok := byProgram[program]
		if !ok || req.Path == "" {
			continue
		}
		// <home>/bin/java -> <home>. Both JAVA_HOME and MAVEN_HOME point at
		// the install root, one level above the bin directory.
		env = append(env, variable+"="+filepath.Dir(filepath.Dir(req.Path)))
	}
	if len(pathPrefix) > 0 {
		env = append(env, "PATH="+strings.Join(append(pathPrefix, os.Getenv("PATH")), string(os.PathListSeparator)))
	}
	return env
}
