package codegraph

import (
	"context"
	"errors"
	"os"
	"sync"
)

// IndexState is where one workspace service's index stands
// (docs/dev-kit/workspace.mdx § "Trạng thái index của một service").
type IndexState string

const (
	// IndexUnknown: nothing queued, and no index on disk for this folder.
	IndexUnknown IndexState = ""
	// IndexPending: queued, waiting for the worker.
	IndexPending IndexState = "pending"
	// IndexIndexing: the worker is analysing it now.
	IndexIndexing IndexState = "indexing"
	// IndexIndexed: an index for exactly this folder is on disk.
	IndexIndexed IndexState = "indexed"
	// IndexFailed: the last run failed; Error says why.
	IndexFailed IndexState = "failed"
	// IndexUnsupported: no analyzer claims the folder (ErrUnsupportedProject).
	IndexUnsupported IndexState = "unsupported"
)

// ServiceIndexStatus is one service's index state as the Registry sees it.
type ServiceIndexStatus struct {
	Path  string     `json:"path"`
	State IndexState `json:"state"`
	// Error is the failure, for IndexFailed and IndexUnsupported.
	Error string `json:"error,omitempty"`
	// MissingToolchain separates "install or build the analyzer" from a real
	// failure — the setup panel's HasArtifact/Ready distinction, for a
	// service nobody has opened.
	MissingToolchain bool `json:"missingToolchain,omitempty"`
	// IndexedAt is when the stored index was written, Unix ms, for
	// IndexIndexed; 0 when unknown.
	IndexedAt int64 `json:"indexedAt,omitempty"`
}

// indexQueue is the Registry's index-on-add queue: folders indexed once, in
// the background, without taking a resident slot. One worker: the queue
// takes at most one of the Host's analyze slots, so a workspace of 89
// services never starves the folder the user is actually opening.
type indexQueue struct {
	mu      sync.Mutex
	pending []string
	states  map[string]ServiceIndexStatus
	running bool
	ctx     context.Context
	cancel  context.CancelFunc
	// forced are queued folders that must be analysed again even when a
	// Service already has them open (Registry.Reindex), as opposed to the
	// index-on-add run, which leaves an open folder to its own watcher.
	forced map[string]bool
	// current is the folder being indexed now and the function that cancels
	// just that run (Registry.CancelIndex).
	current       string
	cancelCurrent context.CancelFunc
}

// Enqueue queues folders to be indexed once in the background and returns
// at once. A folder already queued or being indexed is not queued twice; one
// open as the focused or a resident service is skipped — it keeps its own
// index. Everything else runs, including a folder already indexed: the run
// refreshes it (and costs only the reopen when nothing changed).
func (r *Registry) Enqueue(paths ...string) { r.enqueue(false, paths) }

// Reindex queues folders to be analysed again, whether or not a Service has
// them open: an open folder is re-indexed in place (Service.Refresh), any other
// is indexed on a throwaway Service. It is what "Reindex" in the workspace
// means — the answer to "this index looks wrong", not to "this folder is new".
// Same queue, same single worker, same bound on concurrent analyses; folders
// run in the order given, and a failing one does not stop the rest.
func (r *Registry) Reindex(paths ...string) { r.enqueue(true, paths) }

// CancelIndex withdraws folders from the queue and stops the run in flight if
// it is one of them. A folder withdrawn goes back to whatever is on disk: it
// reports indexed with its previous index, or nothing. Folders not queued are
// ignored. Cancelling does not touch a Service's own watcher re-index.
func (r *Registry) CancelIndex(paths ...string) {
	q := r.queue()
	var reverted []string
	q.mu.Lock()
	drop := map[string]bool{}
	for _, p := range paths {
		drop[ProjectKey(p)] = true
	}
	kept := q.pending[:0]
	for _, key := range q.pending {
		if drop[key] {
			delete(q.states, key)
			delete(q.forced, key)
			reverted = append(reverted, key)
			continue
		}
		kept = append(kept, key)
	}
	q.pending = kept
	if q.current != "" && drop[q.current] && q.cancelCurrent != nil {
		q.cancelCurrent()
	}
	q.mu.Unlock()
	for _, key := range reverted {
		r.reportIndexStatus(r.IndexStatus(key))
	}
}

func (r *Registry) enqueue(force bool, paths []string) {
	q := r.queue()
	var queued []ServiceIndexStatus
	q.mu.Lock()
	for _, path := range paths {
		key := ProjectKey(path)
		if st := q.states[key].State; st == IndexPending || st == IndexIndexing {
			if force && st == IndexPending {
				q.forced[key] = true
			}
			continue
		}
		q.pending = append(q.pending, key)
		if force {
			q.forced[key] = true
		}
		q.states[key] = ServiceIndexStatus{Path: key, State: IndexPending}
		queued = append(queued, q.states[key])
	}
	start := !q.running && len(q.pending) > 0
	if start {
		q.running = true
	}
	ctx := q.ctx
	q.mu.Unlock()
	// Reported outside the lock: a listener that asks IndexStatus back
	// would otherwise deadlock on it.
	for _, st := range queued {
		r.reportIndexStatus(st)
	}
	if start {
		go r.drainQueue(ctx)
	}
}

// IndexStatus reports where path's index stands: the queue's own state while
// it is queued or ran this session, else what is on disk.
func (r *Registry) IndexStatus(path string) ServiceIndexStatus {
	key := ProjectKey(path)
	q := r.queue()
	q.mu.Lock()
	st, ok := q.states[key]
	q.mu.Unlock()
	if ok && st.State != IndexIndexed {
		return st
	}
	var configDir string
	if r.focus != nil {
		configDir = r.focus.Host().ConfigDir()
	}
	if at, found := StoredIndex(configDir, key); found {
		return ServiceIndexStatus{Path: key, State: IndexIndexed, IndexedAt: at}
	}
	return ServiceIndexStatus{Path: key, State: IndexUnknown}
}

func (r *Registry) queue() *indexQueue {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.indexQ == nil {
		ctx, cancel := context.WithCancel(context.Background())
		r.indexQ = &indexQueue{states: map[string]ServiceIndexStatus{}, forced: map[string]bool{}, ctx: ctx, cancel: cancel}
	}
	return r.indexQ
}

func (r *Registry) drainQueue(ctx context.Context) {
	q := r.queue()
	for {
		q.mu.Lock()
		if len(q.pending) == 0 || ctx.Err() != nil {
			q.running = false
			q.mu.Unlock()
			return
		}
		key := q.pending[0]
		q.pending = q.pending[1:]
		force := q.forced[key]
		delete(q.forced, key)
		q.states[key] = ServiceIndexStatus{Path: key, State: IndexIndexing}
		runCtx, cancelRun := context.WithCancel(ctx)
		q.current, q.cancelCurrent = key, cancelRun
		q.mu.Unlock()
		r.reportIndexStatus(ServiceIndexStatus{Path: key, State: IndexIndexing})

		st := r.indexOnce(runCtx, key, force)
		cancelled := runCtx.Err() != nil
		cancelRun()
		q.mu.Lock()
		q.current, q.cancelCurrent = "", nil
		q.mu.Unlock()
		if ctx.Err() != nil {
			// Stopped, not failed: the app is quitting. Nothing is reported,
			// so no "failed" is persisted for a run that never finished.
			q.mu.Lock()
			q.states[key] = ServiceIndexStatus{Path: key, State: IndexPending}
			q.running = false
			q.mu.Unlock()
			return
		}
		if cancelled {
			// Withdrawn by CancelIndex: neither a failure nor a success. The
			// folder is what it was before the run.
			q.mu.Lock()
			delete(q.states, key)
			q.mu.Unlock()
			r.reportIndexStatus(r.IndexStatus(key))
			continue
		}

		q.mu.Lock()
		q.states[key] = st
		q.mu.Unlock()
		r.reportIndexStatus(st)
	}
}

// indexOnce indexes one folder on a throwaway Service: open, wait for the
// index (a stored one is refreshed, not just served), shut down. Not through
// Open — 89 services queued would spin the LRU and evict exactly what the
// user has open. A folder already open is left to its own Service.
func (r *Registry) indexOnce(ctx context.Context, key string, force bool) ServiceIndexStatus {
	if open, ok := r.Service(key); ok {
		if force {
			// Re-index the open folder in place. Its own watcher keeps
			// working; this is the same run it would start on a change.
			if err := open.Refresh(ctx); err != nil && !errors.Is(err, ErrNoActiveProject) {
				return indexFailure(key, err)
			}
		}
		// Its own Service keeps its index; report what that index is.
		if _, err := open.WaitForIndexReady(ctx); err != nil {
			return indexFailure(key, err)
		}
		info, _ := open.IndexInfo(ctx)
		return ServiceIndexStatus{Path: key, State: IndexIndexed, IndexedAt: info.IndexedAt}
	}
	r.mu.Lock()
	svc := r.newService()
	r.mu.Unlock()
	defer func() { _ = svc.Shutdown() }()

	err := svc.OpenProjectDir(ctx, key)
	if err == nil {
		_, err = svc.WaitForIndexReady(ctx)
	}
	if err != nil {
		return indexFailure(key, err)
	}
	info, _ := svc.IndexInfo(ctx)
	return ServiceIndexStatus{Path: key, State: IndexIndexed, IndexedAt: info.IndexedAt}
}

// indexFailure names why a run failed the way the UI acts on it.
func indexFailure(key string, err error) ServiceIndexStatus {
	st := ServiceIndexStatus{Path: key, State: IndexFailed, Error: err.Error()}
	switch {
	case errors.Is(err, ErrUnsupportedProject):
		st.State = IndexUnsupported
	case errors.Is(err, ErrAnalyzerNotConfigured), errors.Is(err, ErrToolchainIncomplete):
		st.MissingToolchain = true
	}
	return st
}

func (r *Registry) reportIndexStatus(st ServiceIndexStatus) {
	if notify := r.OnIndexStatus; notify != nil {
		notify(st)
	}
}

// StoredIndex reports whether configDir holds an index written from exactly
// projectPath, and when. Reads the file without opening a Service: this is
// what a workspace panel asks for every service it lists.
func StoredIndex(configDir, projectPath string) (indexedAt int64, ok bool) {
	if configDir == "" {
		return 0, false
	}
	dbPath := ProjectDBPath(configDir, projectPath)
	if _, err := os.Stat(dbPath); err != nil {
		return 0, false
	}
	store, err := newSQLiteStore(dbPath)
	if err != nil {
		return 0, false
	}
	defer store.Close()
	ctx := context.Background()
	if !storedIndexMatches(ctx, store, projectPath) {
		return 0, false
	}
	at, _ := store.IndexedAt(ctx)
	return at, true
}
