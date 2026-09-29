// internal/codegraph/watch.go
package codegraph

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

// indexState is everything that says whether the index can be trusted right
// now. It lives on Service and is guarded by Service.mu; the methods assume
// the caller holds it.
//
// The model: a query may read the index only when nothing that could change it
// is in flight — no open still analyzing, no re-index running, no relevant file
// change waiting out its debounce — and the last attempt succeeded. Persisted
// rows alone are not evidence that the current source has been analyzed.
type indexState struct {
	// opening covers an open from the moment it is requested until its
	// analysis (or its cached-index check) is done.
	opening bool
	// reindexCount is how many re-index runs have been reserved and have not
	// finished. Reserved, not started: readiness must include queued work.
	reindexCount int
	// pending is a relevant file change that has not yet turned into a run.
	pending bool
	// err is the outcome of the latest open or current re-index; non-nil keeps
	// the index unready until a later run succeeds.
	err error
	// generation changes whenever the project does (open, close). A run that
	// started under another generation must never publish.
	generation uint64
	// request is bumped by every reserved re-index. Only the newest request
	// publishes, so a slow older run cannot overwrite a newer one.
	request uint64
	// version is the persistent index revision last published.
	version int
	// stale says queries may read the stored index while the reopen's refresh
	// runs: it was written by the same analyzer over the same project
	// (storedAnalyzerMatches). Cleared when that refresh finishes, either
	// way — after it, readiness is the ordinary rule again.
	stale bool
	// unsupported says the open project is one no toolchain recognised, open
	// for its files alone (Service.openForBrowsing): there is no index and
	// there will not be one, so graph queries get ErrUnsupportedProject — a
	// reason — rather than an empty answer an agent would take for a fact.
	unsupported bool
}

// reset starts a new generation with nothing in flight: a project switch or
// Close. Every older run is thereby barred from publishing.
func (st *indexState) reset() {
	st.generation++
	st.opening = false
	st.reindexCount = 0
	st.pending = false
	st.err = nil
	st.stale = false
	st.unsupported = false
}

func (st *indexState) busy() bool {
	return st.opening || st.reindexCount > 0 || st.pending
}

// readyErr is nil when queries may read the index.
func (st *indexState) readyErr() error {
	if st.unsupported {
		return ErrUnsupportedProject
	}
	if st.err != nil || (st.busy() && !st.stale) {
		return ErrIndexNotReady
	}
	return nil
}

// reserve registers a re-index before its goroutine starts and returns the
// (generation, request) pair that run must still match to publish.
func (st *indexState) reserve() (uint64, uint64) {
	st.reindexCount++
	st.pending = false
	st.request++
	return st.generation, st.request
}

// current reports whether a run reserved as (generation, request) is still the
// newest work for the open project.
func (st *indexState) current(generation, request uint64) bool {
	return st.generation == generation && st.request == request
}

// finish records a reserved run's outcome. published says whether this run's
// write is the index now being served; only then does its error (or success)
// decide readiness.
func (st *indexState) finish(generation, request uint64, published bool, version int, err error) {
	if st.generation != generation {
		return // a new project reset the count; this run is not part of it
	}
	if st.request == request {
		if published {
			st.version = version
		}
		st.err = err
		st.stale = false
	}
	st.reindexCount--
}

// errStaleRun reports that a run's result was not written because newer work
// had already replaced it. Never surfaced: the newer run publishes instead.
var errStaleRun = errors.New("codegraph: superseded by newer index work")

// writeIndex replaces the index with result and returns the new revision —
// unless the run is no longer current, which is checked under writeMu so an
// older run can never land its write after a newer one's. request is nil for
// an open, which has no request of its own; generation alone guards it.
//
// Deliberately not under mu: a ReplaceIndex on a large project takes a while,
// and queries keep reading the previous snapshot (WAL) while it runs. The
// caller re-checks currency under mu before publishing what it wrote.
func (s *Service) writeIndex(ctx context.Context, generation uint64, request *uint64, store Store, result AnalysisResult) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.RLock()
	ok := s.index.generation == generation && (request == nil || s.index.request == *request)
	s.mu.RUnlock()
	if !ok {
		return 0, errStaleRun
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		return 0, err
	}
	return store.IndexVersion(ctx)
}

// addRecursive watches root and every subdirectory under it (fsnotify.Add
// is non-recursive — each directory must be added individually), skipping
// the shared ignored directories.
func addRecursive(w *fsnotify.Watcher, root string) error {
	return addTree(w, root, nil)
}

// addTree is addRecursive that also records each directory it watched, so the
// loop can tell a removed directory — which carries no extension to judge by
// — from a removed file.
func addTree(w *fsnotify.Watcher, root string, watched map[string]bool) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && ignoredDir(d.Name()) {
			return filepath.SkipDir
		}
		if err := w.Add(path); err != nil {
			return err
		}
		if watched != nil {
			watched[path] = true
		}
		return nil
	})
}

// stopWatcher stops any running background file watcher and waits for its
// goroutine to fully exit.
func (s *Service) stopWatcher() {
	s.mu.Lock()
	cancel := s.watchCancel
	done := s.watchDone
	s.watchCancel = nil
	s.watchDone = nil
	s.index.pending = false
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// restartWatcher stops any watcher from a previously opened project (waiting
// for its goroutine to fully exit, so its *fsnotify.Watcher is closed before
// a new one opens — avoids two watchers racing on the same underlying OS
// resources across a rapid re-open) and starts a fresh one for root.
//
// Failing to watch is not fatal to an open: the project is still usable, it
// just does not re-index by itself.
func (s *Service) restartWatcher(root string) error {
	s.stopWatcher()

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("codegraph: create watcher: %w", err)
	}
	watched := map[string]bool{}
	if err := addTree(w, root, watched); err != nil {
		w.Close()
		return fmt.Errorf("codegraph: watch %q: %w", root, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.mu.Lock()
	s.watchCancel = cancel
	s.watchDone = done
	s.mu.Unlock()
	relevant := watchFilter(s.host.Toolchains())

	go s.watchLoop(ctx, w, done, watched, relevant)
	return nil
}

// watchFilter says which changed files can change the index: the source files
// and build files of the registered toolchains — derived from Toolchain, so a
// second language brings its own. Everything else a project folder churns
// through (editor swap files, logs, .DS_Store, the analyzer's own temp files)
// is noise, and treating it as a change used to hold the index unready for no
// reason. With no toolchain registered there is no vocabulary to judge by, and
// every change counts — the same stance detection takes.
func watchFilter(toolchains []Toolchain) func(name string) bool {
	var exts, markers []string
	for _, tc := range toolchains {
		if tc.SourceExt != "" {
			exts = append(exts, tc.SourceExt)
		}
		markers = append(markers, tc.Markers...)
	}
	if len(exts) == 0 && len(markers) == 0 {
		return func(string) bool { return true }
	}
	return func(name string) bool {
		base := filepath.Base(name)
		for _, ext := range exts {
			if strings.HasSuffix(base, ext) {
				return true
			}
		}
		for _, marker := range markers {
			if matched, _ := filepath.Match(marker, base); matched {
				return true
			}
		}
		return false
	}
}

// underIgnoredDir reports whether path sits inside an ignored directory below
// root — a build writing into target/ must not look like a source change.
func underIgnoredDir(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		if part != "." && ignoredDir(part) {
			return true
		}
	}
	return false
}

// watchLoop owns w for its entire lifetime (closes it on exit) and debounces
// relevant fsnotify events into background re-index runs.
//
// Three rules keep it settling under a steady stream of writes:
//
//   - Only relevant changes count (watchFilter). An irrelevant one neither
//     marks the index pending nor delays anything.
//   - The debounce restarts on each relevant event, but never past
//     maxDebounceWait from the first one of the burst.
//   - A run in flight is never cancelled by a newer change. The change is
//     queued instead and runs when the current one finishes, so a re-index
//     slower than the event rate still completes and publishes rather than
//     being thrown away forever.
func (s *Service) watchLoop(ctx context.Context, w *fsnotify.Watcher, done chan struct{}, watched map[string]bool, relevant func(string) bool) {
	defer close(done)
	defer w.Close()

	s.mu.RLock()
	interval, maxWait, root := s.debounceInterval, s.maxDebounceWait, s.projectRoot
	s.mu.RUnlock()
	if maxWait < interval {
		maxWait = interval
	}

	var (
		timer       *time.Timer
		timerC      <-chan time.Time
		burstStart  time.Time
		running     bool
		queued      bool
		runDone     = make(chan struct{}, 1)
		cancelRun   context.CancelFunc
		markPending = func() {
			s.mu.Lock()
			s.index.pending = true
			s.mu.Unlock()
		}
	)

	startRun := func() {
		runCtx, cancel := context.WithCancel(ctx)
		cancelRun = cancel
		running = true
		run := s.prepareReindex(runCtx)
		go func() {
			run()
			cancel()
			runDone <- struct{}{}
		}()
	}

	arm := func() {
		now := time.Now()
		if timerC == nil {
			burstStart = now
		}
		wait := interval
		if deadline := burstStart.Add(maxWait); now.Add(wait).After(deadline) {
			wait = deadline.Sub(now)
		}
		if timer == nil {
			timer = time.NewTimer(wait)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(wait)
		}
		timerC = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			if cancelRun != nil {
				cancelRun()
			}
			s.mu.Lock()
			s.index.pending = false
			s.mu.Unlock()
			return

		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if underIgnoredDir(root, event.Name) {
				continue
			}
			isDir := false
			if event.Has(fsnotify.Create) {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
					isDir = true
					if !ignoredDir(filepath.Base(event.Name)) {
						// best-effort: a new package/dir created after watching
						// started. Anything moved in with it arrived without
						// events of its own, so the directory itself counts.
						_ = addTree(w, event.Name, watched)
					} else {
						continue
					}
				}
			}
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				if watched[event.Name] {
					isDir = true // a package deleted or moved away
					delete(watched, event.Name)
				}
			}
			// Chmod alone is metadata — Spotlight, backup tools and `touch`
			// fire it on files that did not change.
			if event.Op == fsnotify.Chmod {
				continue
			}
			if !isDir && !relevant(event.Name) {
				continue
			}
			markPending()
			arm()

		case <-timerC:
			timerC = nil
			if running {
				queued = true // stays pending; runs when the current one ends
				continue
			}
			startRun()

		case <-runDone:
			running = false
			if queued {
				queued = false
				startRun()
			}

		case _, ok := <-w.Errors:
			if !ok {
				return
			}
			// best-effort: a single watcher error (e.g. a transient OS-level
			// read error) doesn't stop watching — the loop keeps running.
		}
	}
}

// prepareReindex reserves a re-index and returns the work to run. Register
// work synchronously, before launching its goroutine: readiness must include
// queued work, and older runs must never publish over newer requests.
//
// The run is tied to the project it was reserved for as well as to ctx: a
// project switch or Close cancels it. Failed refreshes keep the stored graph
// for recovery but never expose it as ready; a later watcher event or an
// explicit open retries the analysis.
func (s *Service) prepareReindex(ctx context.Context) func() {
	s.mu.Lock()
	generation, request := s.index.reserve()
	root, lease, analyzer, projectCtx := s.projectRoot, s.lease, s.analyzer, s.projectCtx
	s.mu.Unlock()
	return func() {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		if projectCtx != nil {
			stop := context.AfterFunc(projectCtx, cancel)
			defer stop()
		}

		// Waits for an analyze slot shared with every other Service on the
		// Host (host.go); cancelled with the project like the analysis itself.
		var result AnalysisResult
		release, err := s.host.acquireJob(ctx)
		if err == nil {
			result, err = analyzer.Analyze(ctx, root)
			release()
		}
		if err == nil {
			err = ctx.Err()
		}
		published, version := false, 0
		if err == nil {
			store, release, ok := lease.acquire()
			if !ok {
				err = errStaleRun
			} else {
				version, err = s.writeIndex(ctx, generation, &request, store, result)
				if err == nil {
					// Every write stamps who wrote it, a refresh included: a
					// newer analyzer's refresh rewrites the whole index.
					_ = store.SetIndexedAnalyzer(ctx, analyzerIdentity(analyzer))
				}
				release()
				published = err == nil
			}
		}

		s.mu.Lock()
		published = published && s.index.current(generation, request) && s.active
		if errors.Is(err, errStaleRun) {
			err = nil // the newer run owns readiness
		}
		s.index.finish(generation, request, published, version, err)
		notify := s.OnIndexUpdated
		s.mu.Unlock()
		if published && notify != nil {
			notify(version)
		}
	}
}

func (s *Service) runReindex(ctx context.Context) { s.prepareReindex(ctx)() }

// WaitForIndexReady blocks through cold analysis, cached startup refresh,
// and queued/running re-indexes, then returns the persistent project revision.
// Callers that just wrote a file (or otherwise expect a re-index) should call
// this before trusting any other query's result — see this plan's "Known
// limitation" note for the one narrow timing edge this does not close.
//
// Implemented as a short poll rather than a sync.Cond: the debounce interval
// is hundreds of milliseconds, so a 20ms poll is imperceptible, and it composes
// cleanly with ctx cancellation (sync.Cond.Wait does not, without extra
// goroutine plumbing this doesn't otherwise need).
func (s *Service) WaitForIndexReady(ctx context.Context) (int, error) {
	for {
		s.mu.RLock()
		active := s.active
		busy := s.index.busy()
		indexErr := s.index.err
		version := s.index.version
		unsupported := s.index.unsupported
		s.mu.RUnlock()

		if unsupported {
			// Open, and as ready as it will get: its files are there, a graph
			// is not. Said as the reason rather than as ErrIndexNotReady, which
			// would promise an index that is coming.
			return 0, ErrUnsupportedProject
		}
		if !busy {
			if indexErr != nil {
				return 0, fmt.Errorf("%w: %v", ErrIndexNotReady, indexErr)
			}
			if !active {
				return 0, ErrNoActiveProject
			}
			return version, nil
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
