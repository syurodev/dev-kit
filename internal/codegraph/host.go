package codegraph

import (
	"context"
	"fmt"
	"sync"
	"time"

	"devkit/internal/terminal"
)

// Host is what every Service in the app shares: the analyzer toolchains and
// their build, where project indexes live, and the terminal registry shells
// are registered with. None of it belongs to one project.
//
// Split out of Service for the workspace Registry (workspace.mdx § Registry):
// N services each holding their own copy would be N build states for the
// same artifact on disk — two setup panels racing one Maven build — and N
// places to set a config directory that has to be the same everywhere. A
// Service built with NewService gets a Host of its own, which is today's app
// exactly; NewServiceWithHost is how several share one.
type Host struct {
	mu sync.RWMutex

	toolchains       []Toolchain
	configDir        string
	terminalRegistry *terminal.Registry

	// jobs is the analyze semaphore: one token per analysis running anywhere
	// in the app — an open, a watcher re-index, the workspace's index queue.
	// A Java analyze is a JVM for its whole length, so this is the ceiling on
	// JVMs at once; without it a `git checkout` touching several open
	// services would start one per service at the same moment.
	jobs chan struct{}

	// buildMu is held for the whole length of a build — minutes on a cold
	// Maven cache — which mu, taken on every read, could not be. buildCancel
	// stops that build.
	buildMu     sync.Mutex
	buildCancel context.CancelFunc
}

// NewHost returns a Host with nothing registered: no toolchains, no config
// directory, no terminal registry. What an ephemeral container and every
// test double start from.
func NewHost() *Host {
	return &Host{jobs: make(chan struct{}, DefaultJobLimit)}
}

// DefaultJobLimit is how many analyses run at once across the app
// (workspace.mdx § Registry, "Hai trần"). With one project open that is its
// open and one refresh or re-index — what a single Service could already
// have in flight — so today's app is not held up by it.
const DefaultJobLimit = 2

// SetJobLimit changes how many analyses may run at once. Only before any
// analysis starts: a limit changed under a running one would lose track of
// its token.
func (h *Host) SetJobLimit(n int) {
	if n < 1 {
		n = 1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jobs = make(chan struct{}, n)
}

// acquireJob waits for an analyze slot, or for ctx to end. release must be
// called exactly once when the analysis is over.
func (h *Host) acquireJob(ctx context.Context) (release func(), err error) {
	h.mu.RLock()
	jobs := h.jobs
	h.mu.RUnlock()
	select {
	case jobs <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-jobs }) }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// SetToolchains registers what builds can build and AnalyzerStatus reports on.
func (h *Host) SetToolchains(toolchains []Toolchain) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.toolchains = toolchains
}

// Toolchains is the registered toolchain list.
func (h *Host) Toolchains() []Toolchain {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.toolchains
}

// SetConfigDir records the OS config directory project indexes live under.
func (h *Host) SetConfigDir(configDir string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.configDir = configDir
}

// ConfigDir is where project indexes live; "" when none was configured.
func (h *Host) ConfigDir() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.configDir
}

// SetTerminalRegistry wires the shared Terminal primitive's Registry.
func (h *Host) SetTerminalRegistry(reg *terminal.Registry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.terminalRegistry = reg
}

// TerminalRegistry is the registry shells are registered with, or nil.
func (h *Host) TerminalRegistry() *terminal.Registry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.terminalRegistry
}

// AnalyzerStatus probes the machine once per registered toolchain and reports
// what is ready, what is buildable, and what is missing.
//
// Probing shells out to each required program's version flag, so this is not
// free — call it when the user is looking at the setup panel, not on a timer.
func (h *Host) AnalyzerStatus(ctx context.Context) []ToolchainStatus {
	toolchains := h.Toolchains()
	statuses := make([]ToolchainStatus, 0, len(toolchains))
	for _, tc := range toolchains {
		statuses = append(statuses, tc.status(ctx))
	}
	return statuses
}

// BuildAnalyzer builds one language's analyzer artifact, streaming each line
// of the build's output to onLine (nil drops it), and returns the toolchain
// it built so the caller can adopt its analyzer. One build at a time for the
// whole app: a second call while one runs gets ErrBuildAlreadyRunning.
//
// The build runs unsandboxed, as the same user, exactly as typing the command
// in a terminal would: it is the developer's own repository being built with
// the developer's own toolchain, and a build tool needs the network and its
// own package cache to do its job at all.
func (h *Host) BuildAnalyzer(ctx context.Context, language string, onLine func(line string)) (Toolchain, error) {
	var toolchain Toolchain
	found := false
	for _, tc := range h.Toolchains() {
		if tc.Language == language {
			toolchain, found = tc, true
			break
		}
	}
	if !found {
		return Toolchain{}, fmt.Errorf("%w: %s", ErrUnknownToolchain, language)
	}
	if toolchain.Native != nil {
		return Toolchain{}, fmt.Errorf("%w: %s", ErrAnalyzerIsBuiltIn, language)
	}

	if !h.buildMu.TryLock() {
		return Toolchain{}, ErrBuildAlreadyRunning
	}
	defer h.buildMu.Unlock()

	// Bounded, and cancellable from outside the call: CancelAnalyzerBuild (the
	// setup panel's Cancel) and Close (quitting the app) both stop it.
	if _, ok := ctx.Deadline(); !ok {
		var cancelTimeout context.CancelFunc
		ctx, cancelTimeout = context.WithTimeout(ctx, buildTimeout)
		defer cancelTimeout()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	h.mu.Lock()
	h.buildCancel = cancel
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.buildCancel = nil
		h.mu.Unlock()
	}()

	resolved, buildable := toolchain.resolve(ctx)
	if !buildable {
		for _, req := range resolved {
			if !req.Met {
				return Toolchain{}, fmt.Errorf("%w: %s", ErrToolchainIncomplete, req.Problem)
			}
		}
	}

	if err := toolchain.run(ctx, resolved, func(line string) {
		if onLine != nil {
			onLine(line)
		}
	}); err != nil {
		return Toolchain{}, err
	}
	// The build's own output, not whatever the search finds: a build that
	// silently produced nothing must not read as success because a stale
	// bundled copy happens to be lying around.
	if firstExisting([]string{toolchain.Artifact}) == "" {
		return Toolchain{}, fmt.Errorf("%w: expected %s", ErrArtifactMissing, toolchain.Artifact)
	}
	return toolchain, nil
}

// CancelAnalyzerBuild stops the analyzer build in flight, if there is one —
// Maven, its JVM and everything they forked. BuildAnalyzer then returns the
// cancellation. A no-op when nothing is building.
func (h *Host) CancelAnalyzerBuild() {
	h.mu.Lock()
	cancel := h.buildCancel
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

const (
	// probeTimeout bounds one version probe.
	probeTimeout = 15 * time.Second
	// buildTimeout bounds an analyzer build when the caller set no deadline.
	// A cold Maven cache downloads the analyzer's dependencies, which is
	// minutes on a slow network; past this it is not downloading any more.
	buildTimeout = 30 * time.Minute
)
