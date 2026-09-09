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
	// while one is in flight. Two builds writing the same output directory
	// produce one corrupt artifact rather than two good ones.
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
// per toolchain: "the runtime is missing" and "the build tool is too old" are
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
	// where it came from — "PATH", an install root variable, an IDE's own copy.
	// Worth showing: on a machine carrying several versions of a toolchain,
	// "which one" is the question the user is about to ask next.
	Path   string `json:"path"`
	Source string `json:"source"`
	Met    bool   `json:"met"`
	// Problem says what is wrong in one phrase, empty when Met. It is written
	// for display, not for parsing.
	Problem string `json:"problem"`
	// InstallHint is the prose: what to know beyond running a command, such as
	// where an IDE already keeps one. No commands in it.
	InstallHint string `json:"installHint"`
	// Install are the commands that would fix it, already narrowed to the
	// platform DevKit is running on.
	//
	// Separate from the hint because a command buried in a sentence is a
	// command nobody can copy — and because the sentence rendered its own
	// backticks, since the panel prints text rather than markdown. Narrowed
	// here rather than in the UI because this process knows its own GOOS and
	// the frontend would only be guessing.
	Install []InstallCommand `json:"install"`

	// How the probe works, not something the UI should render. locate lists
	// where to look, in preference order, given the toolchain's build
	// directory.
	locate         func(dir string) []candidate
	versionArgs    []string
	versionPattern *regexp.Regexp
}

// InstallCommand is one way to install a missing program, with the packaging
// ecosystem it belongs to so the reader can pick the line that is theirs.
type InstallCommand struct {
	Label   string `json:"label"`
	Command string `json:"command"`
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
	// Artifact is where a build of this toolchain writes its output. It is not
	// necessarily where a usable artifact is found: a packaged install ships
	// one and never builds. See Search.
	Artifact string
	// Search lists, in order, every place a usable artifact may already be —
	// what the package shipped, then what a local build produced. The first
	// that exists is what the analyzer is wired at, and its presence is what
	// makes the toolchain Ready. Empty means "only Artifact counts".
	Search []string
	// Dir is the working directory the build command runs in.
	Dir string
	// Build is the argv to run. Not a shell string: no quoting rules, no shell
	// on the path between us and the process.
	//
	// Build[0] names a requirement rather than a binary: it is replaced with
	// whatever path that requirement resolved to, so a build tool found inside
	// an IDE is invoked where it actually lives instead of being looked up on
	// a PATH it is not on.
	Build []string
	// Run is the argv that runs the built analyzer over a project, with the
	// three placeholders below substituted. Run[0] names a requirement too, so
	// the runtime discovered for building is the one used for running — without
	// that, a machine whose only copy of that runtime lives inside an IDE could
	// build the analyzer and then fail to launch it.
	Run []string
	// HomeEnv maps an environment variable to the requirement whose install
	// root should fill it — a build tool that locates its runtime through a
	// *_HOME convention needs this, and the Java toolchain's
	// {"JAVA_HOME": "java"} is the current example. Without it a build tool
	// launched by absolute path has no idea which runtime to use, and fails on
	// exactly the machine this discovery exists to support: one where
	// everything lives inside an IDE and nothing is on PATH.
	HomeEnv map[string]string
	// Markers name the files that identify one module of this language — a
	// build file, in practice. A directory holding one is a module root, which
	// is how a folder of microservices is recognised as several rather than
	// mistaken for one. See Detect.
	Markers []string
	// SourceExt is the language's source-file extension. Used only as the
	// fallback when a folder holds source but no build file at all, which is
	// still a project someone wants to read.
	SourceExt    string
	Requirements []Requirement
}

// The placeholders a Toolchain's Run argv is written in terms of. Substituted
// per invocation: the artifact is whichever copy the search settled on, and the
// project and output paths change every call.
const (
	// ArtifactArg is replaced with the analyzer artifact in use.
	ArtifactArg = "${artifact}"
	// ProjectArg is replaced with the directory to analyze.
	ProjectArg = "${project}"
	// OutputArg is replaced with the file the analyzer must write its index to.
	OutputArg = "${output}"
)

// ToolchainStatus is the snapshot the frontend renders. Everything the setup
// panel needs to decide what to say is on it, so the UI never re-derives a
// policy the backend already applied.
type ToolchainStatus struct {
	Language string `json:"language"`
	Label    string `json:"label"`
	// Ready means the analyzer can run right now: the artifact is there AND so
	// is the runtime that launches it. Both halves matter — a packaged app
	// ships the artifact but not a runtime, and calling that ready would send
	// the user to an opaque failure at their first index instead of to a
	// sentence naming what to install.
	Ready bool `json:"ready"`
	// HasArtifact separates the two ways of not being ready: nothing built yet
	// (which a build fixes) from built but unrunnable (which it does not). The
	// UI says a different thing for each.
	HasArtifact bool `json:"hasArtifact"`
	// Buildable means every requirement is met, so offering a build button is
	// honest. False with Ready false is the "install these first" case.
	Buildable bool `json:"buildable"`
	// Artifact is the artifact in use when Ready, and where a build would put
	// one when not. Either way it is the path worth showing.
	Artifact string `json:"artifact"`
	// Bundled reports that the artifact in use shipped with the app rather
	// than being built here. The setup panel never appears in that case; the
	// field is for a diagnostics view that wants to say which copy is running.
	Bundled      bool          `json:"bundled"`
	BuildCommand string        `json:"buildCommand"`
	Requirements []Requirement `json:"requirements"`
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
// Probing shells out to each required program's version flag, so this is not
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
	installed := t.installedArtifact()
	artifact := t.Artifact
	if installed != "" {
		artifact = installed
	}
	return ToolchainStatus{
		Language:     t.Language,
		Label:        t.Label,
		Ready:        installed != "" && t.runtimeMet(requirements),
		HasArtifact:  installed != "",
		Buildable:    buildable,
		Artifact:     artifact,
		Bundled:      installed != "" && installed != t.Artifact,
		BuildCommand: strings.Join(t.Build, " "),
		Requirements: requirements,
	}
}

// runtimeMet reports whether the program that launches the analyzer is present.
// Only that one: an analyzer needs its runtime to run, never its build tool.
func (t Toolchain) runtimeMet(resolved []Requirement) bool {
	if len(t.Run) == 0 {
		return false
	}
	for _, req := range resolved {
		if req.Program == t.Run[0] {
			return req.Met
		}
	}
	return false
}

// installedArtifact is the usable artifact this toolchain would run, or "".
func (t Toolchain) installedArtifact() string {
	if len(t.Search) > 0 {
		return firstExisting(t.Search)
	}
	return firstExisting([]string{t.Artifact})
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
// keeps a configured install root and PATH ahead of a stray copy found by a
// glob, when both would do.
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
		// ("17.0.9 is older than 21") instead of a generic absence.
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

	// CombinedOutput because a version banner is as likely to go to stderr as
	// to stdout, and it is small enough that buffering it is not a concern.
	cmd := exec.CommandContext(ctx, c.path, r.versionArgs...)
	// A launcher script may need its own runtime just to print a version. Give
	// it the environment the user configured; if that runtime is itself wrong,
	// its own requirement reports that separately.
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
// makes a legacy scheme like "1.8.0_402" comparable to "21" without a special
// case: it reads as 1.8.0.402, and 1 < 21.
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
// the UI can show a real build rather than a spinner: a build that downloads
// its dependencies takes minutes on a cold cache, and a progress bar that
// cannot say why is indistinguishable from a hang.
//
// The build runs unsandboxed, as the same user, exactly as typing the command
// in a terminal would: it is the developer's own repository being built with
// the developer's own toolchain, and a build tool needs the network and its
// own package cache to do its job at all.
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
	// The build's own output, not whatever the search finds: a build that
	// silently produced nothing must not read as success because a stale
	// bundled copy happens to be lying around.
	if firstExisting([]string{toolchain.Artifact}) == "" {
		return fmt.Errorf("%w: expected %s", ErrArtifactMissing, toolchain.Artifact)
	}

	s.SetAnalyzer(toolchain.Analyzer())
	return nil
}

// Analyzer returns an Analyzer that runs this toolchain's artifact.
//
// It resolves nothing now: the artifact and the runtime are looked up per
// analysis, so installing the runtime — or building the analyzer — takes
// effect without a restart, and startup pays for no subprocess probes.
func (t Toolchain) Analyzer() Analyzer {
	return toolchainAnalyzer{toolchain: t}
}

// analyzerCommand builds the argv and environment for one analysis, or reports
// why it cannot.
func (t Toolchain) analyzerCommand(ctx context.Context, project, output string) ([]string, []string, error) {
	artifact := t.installedArtifact()
	if artifact == "" || len(t.Run) == 0 {
		return nil, nil, ErrAnalyzerNotConfigured
	}

	// Only the program that actually launches the analyzer, not every build
	// requirement: running an analyzer needs its runtime, never its build tool.
	runtimeReq, ok := t.requirement(t.Run[0])
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s names no requirement %q", ErrUnknownToolchain, t.Language, t.Run[0])
	}
	resolved := runtimeReq.probe(ctx, t.Dir)
	if !resolved.Met {
		return nil, nil, fmt.Errorf("%w: %s", ErrToolchainIncomplete, resolved.Problem)
	}

	replace := strings.NewReplacer(ArtifactArg, artifact, ProjectArg, project, OutputArg, output)
	argv := make([]string, 0, len(t.Run))
	argv = append(argv, resolved.Path)
	for _, arg := range t.Run[1:] {
		argv = append(argv, replace.Replace(arg))
	}
	return argv, t.environ(map[string]Requirement{resolved.Program: resolved}), nil
}

func (t Toolchain) requirement(program string) (Requirement, bool) {
	for _, req := range t.Requirements {
		if req.Program == program {
			return req, true
		}
	}
	return Requirement{}, false
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
	// this a tool found off PATH would be looked up again on a PATH it is not
	// on, and the discovery would have been for nothing.
	program := t.Build[0]
	if req, ok := byProgram[program]; ok && req.Path != "" {
		program = req.Path
	}
	cmd := exec.CommandContext(ctx, program, t.Build[1:]...)
	cmd.Dir = t.Dir
	cmd.Env = t.environ(byProgram)

	// One pipe for both streams: build tools put warnings on stderr and the
	// log on stdout, and a reader who sees them apart cannot reconstruct the
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
// first — a build tool's launcher, its plugins and any compiler it forks all
// look for the runtime by bare name, and on the machine this discovery exists
// for (everything inside an IDE, nothing on PATH) they would not find it.
func (t Toolchain) environ(byProgram map[string]Requirement) []string {
	env := os.Environ()
	var pathPrefix []string
	for _, req := range byProgram {
		if req.Path != "" {
			pathPrefix = append(pathPrefix, filepath.Dir(req.Path))
		}
	}
	// Map iteration is unordered and PATH order decides which copy of a program
	// wins; sort so the same machine builds the same way twice.
	sort.Strings(pathPrefix)

	for variable, program := range t.HomeEnv {
		req, ok := byProgram[program]
		if !ok || req.Path == "" {
			continue
		}
		// <root>/bin/<program> -> <root>. Every *_HOME convention points at the
		// install root, one level above the bin directory.
		env = append(env, variable+"="+filepath.Dir(filepath.Dir(req.Path)))
	}
	if len(pathPrefix) > 0 {
		env = append(env, "PATH="+strings.Join(append(pathPrefix, os.Getenv("PATH")), string(os.PathListSeparator)))
	}
	return env
}
