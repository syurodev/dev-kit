// internal/codegraph/service.go
package codegraph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"devkit/internal/terminal"
)

// Service is the CodeGraph domain service. It owns exactly one active
// project's index at a time (FR-1: no multi-project registry in v1).
//
// Three locks, each with one job:
//
//   - openMu serialises the SHORT state transitions of opening, switching and
//     closing a project. It is never held across an analysis — minutes on a
//     cold monorepo — because Close and the next open both need it, and a
//     quit that waits for someone else's index is a hang.
//   - mu guards every field below. Queries take it for a read, long enough to
//     capture a store and a root, never for the query itself.
//   - writeMu serialises index writes, so an older run can never land its
//     ReplaceIndex after a newer one's. It is not mu: queries keep reading the
//     previous snapshot while a write is in flight.
type Service struct {
	// workTree is the Git module, when one is wired: the single source of
	// what changed in the project. Nil falls back to this package's own
	// reader — see SetWorkTreeStatus.
	workTree WorkTreeStatus

	openMu  sync.Mutex
	mu      sync.RWMutex
	writeMu sync.Mutex

	// store is the open project's index, and lease is how it is borrowed: a
	// switch retires the old lease rather than closing the store out from
	// under a query that is still reading it. See storeLease.
	store Store
	lease *storeLease
	// analyzer is what the active project is indexed with. It is set per open,
	// from the toolchain registered for the language detection found, so two
	// projects in two languages are each read by their own analyzer.
	analyzer Analyzer
	// analyzerOverride is an analyzer installed by whoever owns this Service
	// (SetAnalyzer), and it wins over the registry. That is what a test double
	// is, and what a Service with no toolchains has instead of a registry; a
	// production Service sets none, so the registry decides.
	analyzerOverride Analyzer
	// analyzers is one analyzer per language, built from the registry on first
	// use and kept. Kept rather than rebuilt because an analyzer may hold a
	// process — the TypeScript one is a language server whose whole advantage
	// is that it outlives a single analysis — and a fresh instance per open
	// would throw that away every time.
	analyzers   map[string]Analyzer
	active      bool
	projectRoot string
	// announcedRoot is the last root OnProjectChanged was told about, so
	// reopening the same folder is not reported as a switch.
	announcedRoot string

	// host is what this Service shares with every other one in the app:
	// toolchains and their build, the config directory, the terminal
	// registry (host.go). Set at construction and never replaced.
	host *Host

	// detected is what the open folder turned out to hold — which language, and
	// which module roots. Recorded during OpenProject so the answer costs one
	// walk rather than one per caller who wants it, and so the UI can say "4
	// modules" instead of leaving a monorepo looking like a single project.
	detected DetectedProject

	index       indexState
	sessionPath string

	// projectCtx lives exactly as long as the open project. Every analysis
	// started for it — the open itself, the refresh after a cached open, each
	// watcher re-index — is cancelled with it, so a switch or a Close stops the
	// old project's JVM instead of leaving it to finish for nobody.
	projectCtx    context.Context
	projectCancel context.CancelFunc

	// OnIndexUpdated fires after every re-index that actually changed the
	// index, on the background goroutine that performed it. Non-blocking
	// implementations only: a slow listener stalls the file watcher. nil means
	// nobody is listening, which is the normal case in tests.
	OnIndexUpdated func(version int)
	// OnAnalyzerBuildOutput fires for every line an analyzer build writes, on
	// the goroutine draining the build's output. Same contract as
	// OnIndexUpdated: non-blocking implementations only, nil means nobody is
	// listening. Set once at wiring time, before any build starts.
	OnAnalyzerBuildOutput func(language, line string)
	// OnProjectChanged fires when the active project becomes a different
	// folder — whoever asked for it: the UI, an agent over MCP, or the restore
	// at startup. Its payload is the new root. An editor holding a buffer from
	// the old folder needs this to know its save would land somewhere else.
	// Same contract as OnIndexUpdated.
	OnProjectChanged func(root string)

	// debounceInterval is the quiet period after the last relevant fsnotify
	// event before a re-index actually runs. 750ms in production; tests shorten
	// it (an unexported field set directly in-package, same pattern
	// internal/cloudconfig.RefreshOptions uses for its PollInterval).
	debounceInterval time.Duration
	// maxDebounceWait caps how long a burst of events can keep postponing the
	// re-index. Without it a tool that writes continuously — a build
	// regenerating sources, an IDE autosaving — resets the timer forever and
	// the index never settles.
	maxDebounceWait time.Duration

	watchCancel context.CancelFunc
	watchDone   chan struct{}
}

func NewService(store Store, analyzer Analyzer) *Service {
	return NewServiceWithHost(NewHost(), store, analyzer)
}

// NewServiceWithHost is NewService on a Host shared with other Services —
// how the workspace Registry gives N services one toolchain registry, one
// analyzer build and one config directory (workspace.mdx § Registry).
func NewServiceWithHost(host *Host, store Store, analyzer Analyzer) *Service {
	if host == nil {
		host = NewHost()
	}
	service := &Service{
		host:             host,
		store:            store,
		lease:            newStoreLease(store),
		analyzer:         analyzer,
		debounceInterval: 750 * time.Millisecond,
		maxDebounceWait:  10 * time.Second,
	}
	if isAnalyzerOverride(analyzer) {
		service.analyzerOverride = analyzer
	}
	return service
}

// isAnalyzerOverride reports whether an analyzer is an explicit choice rather
// than a placeholder.
//
// The placeholder is the one NewSubprocessAnalyzer returns — an analyzer with
// nothing behind it, which is what the app constructs Service with and what
// half this package's tests pass when the analyzer is beside the point. It
// means "nothing here", so it cannot mean "use this and ignore the registry".
func isAnalyzerOverride(analyzer Analyzer) bool {
	if analyzer == nil {
		return false
	}
	_, placeholder := analyzer.(unconfiguredAnalyzer)
	return !placeholder
}

// SetConfigDir records the OS config directory project indexes live under, so
// every caller that opens a project by folder — the bridge, MCP, the restore
// at startup — derives the same index path from the same source.
func (s *Service) SetConfigDir(configDir string) {
	s.host.SetConfigDir(configDir)
}

// Host is what this Service shares with the rest of the app.
func (s *Service) Host() *Host {
	return s.host
}

// setProjectPath points the service at a project's on-disk index file,
// opening (creating if needed) a sqliteStore for it. The previous store is
// retired, not closed: a query still reading it finishes first. Caller holds
// openMu.
func (s *Service) setProjectPath(dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return fmt.Errorf("codegraph: create project store dir: %w", err)
	}
	store, err := newSQLiteStore(dbPath)
	if err != nil {
		return fmt.Errorf("codegraph: open project store: %w", err)
	}
	s.mu.Lock()
	old := s.lease
	s.store = store
	s.lease = newStoreLease(store)
	s.mu.Unlock()
	if old != nil {
		old.retire()
	}
	return nil
}

// SetTerminalRegistry wires the shared Terminal primitive's Registry into
// this Service — called once from container construction (see main.go),
// separately from NewService so NewService's signature (used by every
// existing test in this package) doesn't need to change.
func (s *Service) SetTerminalRegistry(reg *terminal.Registry) {
	s.host.SetTerminalRegistry(reg)
}

// SetAnalyzer replaces the Analyzer used by OpenProject/runReindex. Each
// indexing request captures its analyzer under mu, so a running request keeps
// the instance it started with.
//
// It is an override, and it wins: a Service told explicitly what to analyze
// with is a Service whose owner knows something the registry does not — every
// test double, and an embedder wiring its own. Left unset, which is what the
// app does, the analyzer is chosen per project from the toolchain registered
// for the language detection found (see analyzerFor).
func (s *Service) SetAnalyzer(analyzer Analyzer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.analyzer = analyzer
	if isAnalyzerOverride(analyzer) {
		s.analyzerOverride = analyzer
	}
}

// analyzerFor is the analyzer registered for a language, from the toolchain
// registry. A project is analyzed by its own language's analyzer, which is the
// whole reason the registry is keyed by language: handing a Lua project to the
// Java analyzer would start a JVM, walk a folder holding no Java, and report
// the empty result as an empty project.
func (s *Service) analyzerFor(language string) (Analyzer, bool) {
	if language == "" {
		return nil, false
	}
	s.mu.RLock()
	existing, ok := s.analyzers[language]
	s.mu.RUnlock()
	if ok {
		return existing, true
	}

	var built Analyzer
	for _, toolchain := range s.toolchainList() {
		if toolchain.Language == language {
			built = toolchain.Analyzer()
			break
		}
	}
	if built == nil {
		return nil, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.analyzers[language]; ok {
		// Another open got there first; keep theirs so the process is shared.
		return existing, true
	}
	if s.analyzers == nil {
		s.analyzers = map[string]Analyzer{}
	}
	s.analyzers[language] = built
	return built, true
}

// analyzerCloser is implemented by an Analyzer that holds something between
// analyses — a process, a connection — and needs telling when there will not
// be another. Nothing requires it: Java's analyzer and Lua's hold nothing, and
// an analyzer that does not implement it is simply dropped.
type analyzerCloser interface {
	CloseAnalyzer() error
}

// closeAnalyzers shuts down every analyzer that holds resources. Called from
// Close; a language server left running after the app quits is a process the
// user has no way to find.
func (s *Service) closeAnalyzers() {
	s.mu.Lock()
	analyzers := s.analyzers
	s.analyzers = nil
	s.mu.Unlock()
	for _, analyzer := range analyzers {
		if closer, ok := analyzer.(analyzerCloser); ok {
			_ = closer.CloseAnalyzer()
		}
	}
}

// overrideAnalyzer is the explicitly installed analyzer, if there is one.
func (s *Service) overrideAnalyzer() (Analyzer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.analyzerOverride, s.analyzerOverride != nil
}

// adoptAnalyzerFor installs an analyzer only when it is the open project's
// language — what a finished build calls so the next index uses what was just
// built, without a project of another language being handed it. With nothing
// open, it installs: there is no project for it to be wrong about.
func (s *Service) adoptAnalyzerFor(language string, analyzer Analyzer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.analyzerOverride != nil {
		return
	}
	if s.detected.Language == "" || s.detected.Language == language {
		s.analyzer = analyzer
	}
}

// StartTerminal opens a local shell ($SHELL, falling back to /bin/sh) at
// the active project's root and registers it with the shared Terminal
// Registry, returning its session ID. Matches the design doc's own
// sequence: Service itself creates the PTY and registers it — the bridge
// layer only handles the capability check and delegates here.
func (s *Service) StartTerminal(rows, cols int) (string, error) {
	s.mu.RLock()
	active := s.active
	root := s.projectRoot
	s.mu.RUnlock()
	reg := s.host.TerminalRegistry()

	if !active {
		return "", ErrNoActiveProject
	}
	if reg == nil {
		return "", ErrTerminalNotConfigured
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	pty, err := terminal.NewLocalPTY(shell, root, rows, cols)
	if err != nil {
		return "", fmt.Errorf("codegraph: start terminal: %w", err)
	}
	return reg.Register("builtin.codegraph", pty), nil
}

// OpenProjectDir opens a project folder with its index under the configured
// config directory (SetConfigDir). It is OpenProjectAt for callers that have
// a folder and nothing else — MCP — so they cannot derive a different index
// path than the bridge does.
func (s *Service) OpenProjectDir(ctx context.Context, projectPath string) error {
	configDir := s.host.ConfigDir()
	if configDir == "" {
		return fmt.Errorf("codegraph: no config directory configured for project indexes")
	}
	return s.OpenProjectAt(ctx, ProjectDBPath(configDir, projectPath), projectPath)
}

// OpenProjectAt makes a folder the active project, with its index in dbPath,
// and returns once the index is ready (or the open failed). A failed analyze
// leaves the service with no active project rather than a stale or partial
// one.
//
// Three things happen before any analyzer runs, and each exists because the
// version without it was worse.
//
// It refuses a folder no toolchain recognises. Analyzing anyway meant starting
// a JVM, walking a stranger's Downloads folder and reporting the empty result
// as an empty project rather than as the wrong folder.
//
// It reuses an index this project already has. The index is persisted per
// project (see ProjectDBPath), and re-deriving it on every open made reopening
// a large repository cost exactly as much as indexing it the first time, with
// the UI blocked throughout for a result almost always identical to the one
// already on disk.
//
// And having reused it, it re-indexes in the background, through the same
// path the file watcher uses. Queries are gated until that refresh commits;
// persisted rows alone are not evidence that the current analyzer has run.
//
// Cancelling ctx abandons the open. So does a newer open or a Close, which
// report ErrProjectChanged to this caller.
func (s *Service) OpenProjectAt(ctx context.Context, dbPath, projectPath string) error {
	run, err := s.beginOpen(dbPath, projectPath)
	if err != nil {
		return err
	}
	return run(ctx)
}

// beginOpen does the part of an open that must happen before anyone can
// observe the new project — cancel the old project's work, swap the store,
// mark the index as opening — and returns the slow part to run. Splitting the
// two is what lets RestoreLastProject hand the slow part to a goroutine while
// readiness already reports "opening", and what keeps openMu from being held
// across an analysis. An empty dbPath keeps the current store.
func (s *Service) beginOpen(dbPath, projectPath string) (func(context.Context) error, error) {
	root := filepath.Clean(projectPath)

	s.openMu.Lock()
	s.cancelProject()
	s.stopWatcher()
	if dbPath != "" {
		if err := s.setProjectPath(dbPath); err != nil {
			s.mu.Lock()
			s.index.reset()
			s.active = false
			s.mu.Unlock()
			s.openMu.Unlock()
			return nil, err
		}
	}
	projectCtx, projectCancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.index.reset()
	s.index.opening = true
	generation := s.index.generation
	s.active = false
	s.projectRoot = root
	s.detected = DetectedProject{}
	s.projectCtx, s.projectCancel = projectCtx, projectCancel
	analyzer, lease := s.analyzer, s.lease
	notify := s.OnProjectChanged
	announce := s.announcedRoot != root
	s.announcedRoot = root
	s.mu.Unlock()
	s.openMu.Unlock()

	if announce && notify != nil {
		notify(root)
	}
	return func(ctx context.Context) error {
		return s.runOpen(ctx, generation, root, analyzer, lease, projectCtx)
	}, nil
}

func (s *Service) runOpen(ctx context.Context, generation uint64, root string, analyzer Analyzer, lease *storeLease, projectCtx context.Context) (openErr error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(projectCtx, cancel)
	defer stop()

	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.index.generation != generation {
			// Somebody else's project now; its state is not ours to touch.
			openErr = fmt.Errorf("%w: %s was replaced before it finished opening", ErrProjectChanged, root)
			return
		}
		s.index.opening = false
		if openErr != nil {
			s.index.err = openErr
			s.active = false
		}
	}()

	store, release, ok := lease.acquire()
	if !ok {
		return ErrProjectChanged
	}
	defer release()

	// Only where there is something to detect with. A Service with no
	// toolchains registered — an ephemeral one, a test with its own analyzer —
	// has no vocabulary for recognising a project, and refusing every folder on
	// the strength of an empty registry would be a gate that knows nothing
	// standing in front of an analyzer that does.
	var detected DetectedProject
	if len(s.toolchainList()) > 0 {
		found, err := s.DetectProject(root)
		if errors.Is(err, ErrUnsupportedProject) {
			return s.openForBrowsing(generation, root)
		}
		if err != nil {
			return fmt.Errorf("codegraph: open %q: %w", root, err)
		}
		detected = found
	}
	// Detection decides which analyzer runs. It has to happen here rather than
	// in beginOpen: beginOpen captures the analyzer before anything has walked
	// the folder, and with more than one language registered the answer to
	// "which analyzer" is not knowable until the folder has been looked at.
	if _, overridden := s.overrideAnalyzer(); !overridden {
		if resolved, ok := s.analyzerFor(detected.Language); ok {
			analyzer = resolved
		}
	}
	s.mu.Lock()
	if s.index.generation == generation {
		s.detected = detected
		// Recorded, not just used, so every later re-index of this project —
		// the refresh below, each watcher run — reaches for the same one.
		if analyzer != nil {
			s.analyzer = analyzer
		}
	}
	s.mu.Unlock()

	// Ask before analyzing. Both halves are required: the store must hold an
	// index AND that index must be this project's. A store holding rows is a
	// weaker claim than a store holding the right rows, and acting on the
	// weaker one hands one project's index to another.
	if storedIndexMatches(ctx, store, root) {
		// Served while it refreshes only when it was written by the analyzer
		// that would refresh it — the same code over the same project, so the
		// refresh can change what the files changed and nothing else. Anything
		// else (an index from an older jar, an older DevKit, or an analyzer
		// that cannot name itself) stays unready until the refresh commits:
		// persisted rows alone are not evidence that the current analyzer has
		// run.
		serveStored := storedAnalyzerMatches(ctx, store, analyzer)
		s.mu.Lock()
		if s.index.generation != generation {
			s.mu.Unlock()
			return nil // the deferred check reports the switch
		}
		s.active = true
		s.index.stale = serveStored
		s.mu.Unlock()
		s.startWatcherFor(generation, root)
		// Reserve the refresh before returning, and before opening clears.
		// Unless the stored index is being served (stale, above), neither
		// queries nor readiness may observe it as ready before this commits.
		run := s.prepareReindex(projectCtx)
		go run()
		return s.saveProjectFor(generation, root)
	}

	release, err := s.host.acquireJob(ctx)
	if err != nil {
		return fmt.Errorf("codegraph: analyze %q: %w", root, err)
	}
	result, err := analyzer.Analyze(ctx, root)
	release()
	if err != nil {
		return fmt.Errorf("codegraph: analyze %q: %w", root, err)
	}
	version, err := s.writeIndex(ctx, generation, nil, store, result)
	if err != nil {
		return fmt.Errorf("codegraph: replace index: %w", err)
	}

	// Stamp the index with the project it describes, so the next open can
	// recognise it. Best-effort: failing to record it costs one re-index, which
	// is not worth failing an otherwise successful open over.
	_ = store.SetIndexedProject(ctx, root)
	// And with the analyzer that wrote it, so a reopen knows whether it may
	// serve this index while it refreshes. Best-effort for the same reason:
	// a missing stamp only means the next reopen waits, as it always did.
	_ = store.SetIndexedAnalyzer(ctx, analyzerIdentity(analyzer))

	s.mu.Lock()
	if s.index.generation != generation {
		s.mu.Unlock()
		return nil
	}
	s.active = true
	s.index.version = version
	notify := s.OnIndexUpdated
	s.mu.Unlock()
	// Said for an open too, not only a re-index: an open that nobody is
	// waiting on — the restore at startup — has no other way to tell the UI
	// that the index it is showing as "opening" has arrived.
	if notify != nil {
		notify(version)
	}

	s.startWatcherFor(generation, root)
	return s.saveProjectFor(generation, root)
}

// openForBrowsing opens a folder no toolchain recognises for its files alone.
//
// Refusing it outright kept the analyzer away from folders that are not code,
// which is still the point — but it also took away the file tree, the editor
// and git status, none of which need an analyzer. An infrastructure repository
// (Terraform, Helm charts, Kubernetes manifests) is somebody's real project;
// "choose another folder" was the wrong answer to it.
//
// So nothing that analyzes runs: no analyzer, and no watcher, whose only job
// is to re-run one. The project is active for files and the store it owns
// (plans, comments); graph queries answer ErrUnsupportedProject.
func (s *Service) openForBrowsing(generation uint64, root string) error {
	s.mu.Lock()
	if s.index.generation != generation {
		s.mu.Unlock()
		return nil // runOpen's deferred check reports the switch
	}
	s.detected = DetectedProject{}
	s.index.unsupported = true
	s.active = true
	version := s.index.version
	notify := s.OnIndexUpdated
	s.mu.Unlock()
	// The open is done, like any other: a restore at startup has nobody
	// waiting on it, and the UI showing "opening" learns it finished here.
	if notify != nil {
		notify(version)
	}
	return s.saveProjectFor(generation, root)
}

// startWatcherFor starts the watcher for root unless the project has changed
// since generation — a late open must not leave a watcher on a folder nobody
// has open any more.
func (s *Service) startWatcherFor(generation uint64, root string) {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if s.currentGeneration() != generation {
		return
	}
	_ = s.restartWatcher(root) // non-fatal: see restartWatcher's doc comment
}

func (s *Service) saveProjectFor(generation uint64, root string) error {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if s.currentGeneration() != generation {
		return nil
	}
	return s.saveProject(root)
}

func (s *Service) currentGeneration() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index.generation
}

// cancelProject cancels every analysis started for the current project.
// Caller holds openMu.
func (s *Service) cancelProject() {
	s.mu.Lock()
	cancel := s.projectCancel
	s.projectCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ActiveDetection reports what the open project turned out to hold. Zero-valued
// when nothing is open, or when no toolchain was registered to detect with.
func (s *Service) ActiveDetection() DetectedProject {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active {
		return DetectedProject{}
	}
	return s.detected
}

// storedIndexMatches reports whether store already holds an index built from
// this exact project.
func storedIndexMatches(ctx context.Context, store Store, projectPath string) bool {
	has, err := store.HasIndex(ctx)
	if err != nil || !has {
		return false
	}
	indexed, err := store.IndexedProject(ctx)
	if err != nil || indexed == "" {
		// Unknown provenance — an index written before the stamp existed. One
		// re-index costs less than showing the wrong project's graph.
		return false
	}
	return indexed == filepath.Clean(projectPath)
}

// storedAnalyzerMatches reports whether the stored index was written by the
// analyzer about to refresh it. Unknown on either side is no.
func storedAnalyzerMatches(ctx context.Context, store Store, analyzer Analyzer) bool {
	identity := analyzerIdentity(analyzer)
	if identity == "" {
		return false
	}
	stored, err := store.IndexedAnalyzer(ctx)
	return err == nil && stored == identity
}

// ProjectRoot returns the directory the active project was opened from — or
// is still opening, since the folder is known the moment it is chosen — or ""
// when no project is open. Callers use it to offer that directory as a default
// elsewhere in the app, so "no project" is an empty string rather than an
// error — there is nothing wrong, there is simply nothing to offer.
func (s *Service) ProjectRoot() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active && !s.index.opening {
		return ""
	}
	return s.projectRoot
}

// activeProject borrows the current store and returns the project root, for
// queries that read the derived graph: they need the index to be ready. The
// release func must be called once the query is done with the store; until
// then a project switch waits to close it.
func (s *Service) activeProject() (Store, string, func(), error) {
	s.mu.RLock()
	err := s.index.readyErr()
	active := s.active
	root, lease := s.projectRoot, s.lease
	s.mu.RUnlock()
	if err != nil {
		return nil, "", nil, err
	}
	if !active {
		return nil, "", nil, ErrNoActiveProject
	}
	return borrow(lease, root)
}

func (s *Service) activeStore() (Store, func(), error) {
	store, _, release, err := s.activeProject()
	return store, release, err
}

// activeStoreAndDetection is activeStore with what the project was detected
// as, read under the same lock as the lease, so the two describe one project.
func (s *Service) activeStoreAndDetection() (Store, DetectedProject, func(), error) {
	s.mu.RLock()
	err := s.index.readyErr()
	active := s.active
	root, lease, detected := s.projectRoot, s.lease, s.detected
	s.mu.RUnlock()
	if err != nil {
		return nil, DetectedProject{}, nil, err
	}
	if !active {
		return nil, DetectedProject{}, nil, ErrNoActiveProject
	}
	store, _, release, err := borrow(lease, root)
	if err != nil {
		return nil, DetectedProject{}, nil, err
	}
	return store, detected, release, nil
}

// projectFiles is for operations on the selected directory, not the derived
// graph — reading and writing files. They remain available while the watcher
// rebuilds after an editor save, and while a project is still opening: the
// folder is known the moment it is chosen.
func (s *Service) projectFiles() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active && !s.index.opening {
		return "", ErrNoActiveProject
	}
	return s.projectRoot, nil
}

// projectStore borrows the store for plan and comment operations. Those are
// metadata the project owns, not facts derived from the index, so they need
// the store open — not the index ready. A plan comment must not bounce off a
// re-index triggered by the save the user just made.
func (s *Service) projectStore() (Store, func(), error) {
	s.mu.RLock()
	usable := s.active || s.index.opening
	root, lease := s.projectRoot, s.lease
	s.mu.RUnlock()
	if !usable {
		return nil, nil, ErrNoActiveProject
	}
	store, _, release, err := borrow(lease, root)
	return store, release, err
}

func borrow(lease *storeLease, root string) (Store, string, func(), error) {
	store, release, ok := lease.acquire()
	if !ok {
		// Retired between reading the lease and borrowing it: the project
		// switched under this call.
		return nil, "", nil, ErrProjectChanged
	}
	return store, root, release, nil
}

// Close cancels everything in flight for the open project — the open itself,
// refreshes, watcher re-indexes and an analyzer build — and stops the file
// watcher. It returns without waiting for an analyzer to exit: cancellation
// kills the subprocess, and whatever it was doing can no longer publish.
// Safe to call more than once, and before any project was opened. Does not
// close the Store; the container that constructed this Service owns that
// lifecycle separately (see internal/app/container.go's existing
// Database/SSH/Kafka Close calls).
func (s *Service) Close() error {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	s.CancelAnalyzerBuild()
	s.closeProjectLocked()
	return nil
}

// Shutdown ends this Service for good, store included: what the workspace
// Registry calls to evict one (workspace.mdx § Registry).
//
// Not Close. Close leaves the store open — the container owns the one a
// singleton Service is built with — so evicting through it would leak a
// SQLite handle per eviction. And Close cancels the analyzer build, which is
// the Host's: evicting one service must not stop a build the setup panel
// started for the whole app.
//
// The store is retired rather than closed outright: a query still reading it
// finishes first, then it closes (storeLease). Afterwards the Service holds
// the nil store and no project; opening a project on it again works, the
// same as on a new one.
func (s *Service) Shutdown() error {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	s.closeProjectLocked()
	nilStore := NewNilStore()
	s.mu.Lock()
	old := s.lease
	s.store = nilStore
	s.lease = newStoreLease(nilStore)
	s.mu.Unlock()
	if old != nil {
		old.retire()
	}
	return nil
}

// closeProjectLocked is what Close and Shutdown share: stop the project's
// work, its watcher and its analyzers. It returns without waiting for an
// analyzer to exit: cancellation kills the subprocess, and whatever it was
// doing can no longer publish. Caller holds openMu.
func (s *Service) closeProjectLocked() {
	s.cancelProject()
	s.stopWatcher()
	s.mu.Lock()
	s.index.reset() // A late analyzer result cannot publish after shutdown.
	s.active = false
	s.mu.Unlock()
	s.closeAnalyzers()
}
