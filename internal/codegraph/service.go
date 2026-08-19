// internal/codegraph/service.go
package codegraph

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// maxFlowDepth caps the BFS walk in GetApiFlow so a pathological call graph
// (very deep chain, or a cycle missed by the visited-set for some reason)
// can never turn into an unbounded response — same "bounded, no full-graph
// dump" contract the design doc requires everywhere else in this module.
const maxFlowDepth = 32

// Service is the CodeGraph domain service. It owns exactly one active
// project's index at a time (FR-1: no multi-project registry in v1).
type Service struct {
	mu          sync.RWMutex
	store       Store
	analyzer    Analyzer
	active      bool
	projectRoot string

	indexVersion   int
	reindexing     bool
	reindexPending bool
	// debounceInterval is the quiet period after the last fsnotify event
	// before a re-index actually runs. 750ms in production; tests shorten it
	// (this is an unexported field set directly in-package, same pattern
	// internal/cloudconfig.RefreshOptions uses for its own test-shortenable
	// PollInterval, just without needing an exported options struct here
	// since Service already owns its own lifecycle).
	debounceInterval time.Duration

	watchCancel context.CancelFunc
	watchDone   chan struct{}
}

func NewService(store Store, analyzer Analyzer) *Service {
	return &Service{store: store, analyzer: analyzer, debounceInterval: 750 * time.Millisecond}
}

// SetProjectPath points the service at a new project's on-disk index file,
// opening (creating if needed) a fresh sqliteStore for it and discarding the
// previous one. Call this once per OpenProject, before running the analyzer,
// so a fresh analyze always starts from a clean per-project store.
func (s *Service) SetProjectPath(dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return fmt.Errorf("codegraph: create project store dir: %w", err)
	}
	store, err := newSQLiteStore(dbPath)
	if err != nil {
		return fmt.Errorf("codegraph: open project store: %w", err)
	}
	s.mu.Lock()
	old := s.store
	s.store = store
	s.active = false
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

// OpenProject runs the analyzer over projectPath and replaces the active
// index. A failed analyze leaves the service with no active project rather
// than a stale or partial one.
func (s *Service) OpenProject(ctx context.Context, projectPath string) error {
	result, err := s.analyzer.Analyze(ctx, projectPath)
	if err != nil {
		s.stopWatcher()
		return fmt.Errorf("codegraph: analyze %q: %w", projectPath, err)
	}
	s.mu.Lock()
	if err := s.store.ReplaceIndex(ctx, result); err != nil {
		s.active = false
		s.mu.Unlock()
		s.stopWatcher()
		return fmt.Errorf("codegraph: replace index: %w", err)
	}
	s.active = true
	s.projectRoot = projectPath
	s.indexVersion++
	s.mu.Unlock()

	_ = s.restartWatcher(projectPath) // non-fatal: see restartWatcher's doc comment
	return nil
}

// activeProject returns the current store and project root under the read
// lock, so neither field is ever read concurrently with the writes
// SetProjectPath/OpenProject make to them under s.mu.Lock(). Both are used
// after the lock is released — *sql.DB (what sqliteStore wraps) and a plain
// string are both safe to use unsynchronized once captured.
func (s *Service) activeProject() (Store, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active {
		return nil, "", ErrNoActiveProject
	}
	return s.store, s.projectRoot, nil
}

func (s *Service) activeStore() (Store, error) {
	store, _, err := s.activeProject()
	return store, err
}

func (s *Service) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	return store.ListEndpoints(ctx)
}

func (s *Service) SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	return store.SearchSymbol(ctx, query)
}

// GetApiFlow walks the call graph from the endpoint's entry node, in Go
// (not SQL), so the traversal — including cycle protection — is testable
// without a database. Cycles are broken by the visited-set; maxFlowDepth is
// a second, independent safety bound.
func (s *Service) GetApiFlow(ctx context.Context, endpointID string) (ApiFlow, error) {
	store, err := s.activeStore()
	if err != nil {
		return ApiFlow{}, err
	}
	ep, err := store.GetEndpoint(ctx, endpointID)
	if err != nil {
		return ApiFlow{}, err
	}
	steps, err := walkFlow(ctx, store, ep.EntryNode)
	if err != nil {
		return ApiFlow{}, err
	}
	return ApiFlow{EndpointID: endpointID, Steps: steps}, nil
}

func walkFlow(ctx context.Context, store Store, rootNodeID string) ([]FlowStep, error) {
	var steps []FlowStep
	visited := make(map[string]bool)

	var walk func(nodeID string, depth int) error
	walk = func(nodeID string, depth int) error {
		if visited[nodeID] || depth > maxFlowDepth {
			return nil
		}
		visited[nodeID] = true

		node, err := store.GetNode(ctx, nodeID)
		if err != nil {
			return err
		}
		steps = append(steps, FlowStep{Label: node.Label, FilePath: node.FilePath, Line: node.Line})

		edges, err := store.GetOutEdges(ctx, nodeID)
		if err != nil {
			return err
		}
		for _, e := range edges {
			if e.Kind != "call" {
				continue
			}
			if err := walk(e.To, depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(rootNodeID, 0); err != nil {
		return nil, err
	}
	return steps, nil
}

// maxBlastRadiusDepth bounds GetBlastRadius's BFS the same way maxFlowDepth
// bounds walkFlow — the visited set alone already guarantees termination
// even on a cyclic dependency graph (Java allows mutual class references),
// this is purely a latency/result-size ceiling for a pathologically
// deep/wide dependency chain in a very large project.
const maxBlastRadiusDepth = 32

// GetBlastRadius returns every file that transitively depends on filePath —
// "if I change this file, what else might be affected" — via repeated
// one-hop Store.GetDependents lookups. filePath itself is never included in
// the result even if a cycle routes back to it.
func (s *Service) GetBlastRadius(ctx context.Context, filePath string) ([]string, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}

	visited := map[string]bool{filePath: true}
	var result []string
	frontier := []string{filePath}
	for depth := 0; len(frontier) > 0 && depth <= maxBlastRadiusDepth; depth++ {
		var next []string
		for _, f := range frontier {
			dependents, err := store.GetDependents(ctx, f)
			if err != nil {
				return nil, err
			}
			for _, d := range dependents {
				if visited[d] {
					continue
				}
				visited[d] = true
				result = append(result, d)
				next = append(next, d)
			}
		}
		frontier = next
	}
	sort.Strings(result)
	return result, nil
}

const (
	// maxNeighborDepth bounds how many hops GetNeighbors will walk, same
	// defensive-ceiling reasoning as maxFlowDepth/maxBlastRadiusDepth.
	maxNeighborDepth = 6
	// maxNeighborNodes is the hard per-page node ceiling frontend-performance.mdx
	// requires ("Ceiling node/edge cứng"). Chosen as a reasonable single-screen
	// subgraph size for v1 — no UI exists yet to tune this against real usage.
	maxNeighborNodes = 200
)

// GetNeighbors returns a bounded subgraph around rootID: every node
// reachable within depth hops in EITHER edge direction (unlike GetApiFlow's
// directed-only walk — this is "what's connected to this node", not a call
// chain), filtered by filter.Layers, paginated to at most maxNeighborNodes
// per call.
//
// Pagination: the full depth-bounded neighborhood is computed first (bounded
// by the capped depth, same as GetBlastRadius's transitive closure), then
// sorted by node ID for a stable, deterministic order, then sliced by
// filter.Continuation's offset. This keeps Continuation a plain string
// offset — no server-side cursor/session state — at the cost of a stale
// offset silently skipping/duplicating a node if the index changes between
// pages (e.g. a background re-index lands mid-pagination); accepted for v1,
// same class of tradeoff the design doc's "layout cache invalidates on
// re-index" note already expects callers to handle.
func (s *Service) GetNeighbors(ctx context.Context, rootID string, depth int, filter NeighborFilter) (Subgraph, error) {
	store, err := s.activeStore()
	if err != nil {
		return Subgraph{}, err
	}

	root, err := store.GetNode(ctx, rootID)
	if err != nil {
		return Subgraph{}, err
	}

	if depth < 0 {
		depth = 0
	}
	if depth > maxNeighborDepth {
		depth = maxNeighborDepth
	}
	allowedLayer := func(n Node) bool {
		if len(filter.Layers) == 0 {
			return true
		}
		for _, l := range filter.Layers {
			if l == n.Layer {
				return true
			}
		}
		return false
	}

	visited := map[string]Node{rootID: root}
	edgeSeen := map[string]bool{}
	var allEdges []Edge
	frontier := []string{rootID}

	for d := 0; d < depth && len(frontier) > 0; d++ {
		sort.Strings(frontier)
		var next []string
		for _, id := range frontier {
			outEdges, err := store.GetOutEdges(ctx, id)
			if err != nil {
				return Subgraph{}, err
			}
			inEdges, err := store.GetInEdges(ctx, id)
			if err != nil {
				return Subgraph{}, err
			}
			for _, e := range append(outEdges, inEdges...) {
				key := e.From + "->" + e.To
				if !edgeSeen[key] {
					edgeSeen[key] = true
					allEdges = append(allEdges, e)
				}
				other := e.To
				if other == id {
					other = e.From
				}
				if _, ok := visited[other]; ok {
					continue
				}
				n, err := store.GetNode(ctx, other)
				if err != nil {
					if errors.Is(err, ErrNodeNotFound) {
						continue
					}
					return Subgraph{}, err
				}
				if !allowedLayer(n) {
					continue
				}
				visited[n.ID] = n
				next = append(next, n.ID)
			}
		}
		frontier = next
	}

	ids := make([]string, 0, len(visited))
	for id := range visited {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	offset := 0
	if filter.Continuation != "" {
		parsed, convErr := strconv.Atoi(filter.Continuation)
		if convErr != nil || parsed < 0 {
			return Subgraph{}, fmt.Errorf("codegraph: invalid continuation %q", filter.Continuation)
		}
		offset = parsed
	}
	if offset > len(ids) {
		offset = len(ids)
	}
	end := offset + maxNeighborNodes
	truncated := end < len(ids)
	if end > len(ids) {
		end = len(ids)
	}
	pageIDs := ids[offset:end]

	nodes := make([]Node, 0, len(pageIDs))
	pageSet := make(map[string]bool, len(pageIDs))
	for _, id := range pageIDs {
		nodes = append(nodes, visited[id])
		pageSet[id] = true
	}

	var edges []Edge
	for _, e := range allEdges {
		if pageSet[e.From] && pageSet[e.To] {
			edges = append(edges, e)
		}
	}

	continuation := ""
	if truncated {
		continuation = strconv.Itoa(end)
	}
	return Subgraph{Nodes: nodes, Edges: edges, Truncated: truncated, Continuation: continuation}, nil
}

// ListFiles walks the active project's root and returns every file and
// directory (except .git) as a flat, path-sorted list. Symlinked entries are
// listed but not followed further — WalkDir does not descend into a symlink
// target, matching the same "don't silently trust symlinks" stance resolvePath
// enforces for individual reads/writes.
func (s *Service) ListFiles(ctx context.Context) ([]FileEntry, error) {
	_, root, err := s.activeProject()
	if err != nil {
		return nil, err
	}

	var entries []FileEntry
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, FileEntry{Path: filepath.ToSlash(rel), IsDir: d.IsDir()})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("codegraph: list files: %w", walkErr)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// ignoredDirs lists directories that should never be watched or indexed.
var ignoredDirs = map[string]bool{
	".git":         true,
	"target":       true,
	"build":        true,
	"node_modules": true,
	".idea":        true,
	".gradle":      true,
}

// addRecursive watches root and every subdirectory under it (fsnotify.Add
// is non-recursive — each directory must be added individually), skipping
// ignored build/vcs directories.
func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if ignoredDirs[d.Name()] {
			return filepath.SkipDir
		}
		return w.Add(path)
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
	s.reindexPending = false
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
func (s *Service) restartWatcher(root string) error {
	s.stopWatcher()

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("codegraph: create watcher: %w", err)
	}
	if err := addRecursive(w, root); err != nil {
		w.Close()
		return fmt.Errorf("codegraph: watch %q: %w", root, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.mu.Lock()
	s.watchCancel = cancel
	s.watchDone = done
	s.mu.Unlock()

	go s.watchLoop(ctx, w, done)
	return nil
}

// watchLoop owns w for its entire lifetime (closes it on exit) and debounces
// fsnotify events into background re-index runs. A newer event cancels a
// still-running re-index rather than letting it overwrite the index with
// stale results once a fresher analyze has already started.
func (s *Service) watchLoop(ctx context.Context, w *fsnotify.Watcher, done chan struct{}) {
	defer close(done)
	defer w.Close()

	var timer *time.Timer
	var timerC <-chan time.Time
	var reindexCancel context.CancelFunc

	resetDebounce := func() {
		s.mu.Lock()
		s.reindexPending = true
		interval := s.debounceInterval
		s.mu.Unlock()
		if timer == nil {
			timer = time.NewTimer(interval)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(interval)
		}
		timerC = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			if reindexCancel != nil {
				reindexCancel()
			}
			s.mu.Lock()
			s.reindexPending = false
			s.mu.Unlock()
			return

		case event, ok := <-w.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Create) {
				if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() && !ignoredDirs[filepath.Base(event.Name)] {
					_ = w.Add(event.Name) // best-effort: a new package/dir created after watching started
				}
			}
			resetDebounce()

		case <-timerC:
			timerC = nil
			if reindexCancel != nil {
				reindexCancel() // a slower-than-debounce previous run is still in flight — cancel it, its result would already be stale
			}
			reindexCtx, cancel := context.WithCancel(ctx)
			reindexCancel = cancel
			go s.runReindex(reindexCtx)

		case _, ok := <-w.Errors:
			if !ok {
				return
			}
			// best-effort: a single watcher error (e.g. a transient OS-level
			// read error) doesn't stop watching — the loop keeps running.
		}
	}
}

// runReindex is the background counterpart to OpenProject's analyze+replace
// — deliberately more lenient on failure. OpenProject (an explicit, foreground
// user action) marks the project inactive on any error, per its own existing
// contract. A background trigger must not do that: a transient failure (e.g.
// catching a file mid-save with temporarily invalid syntax, or ctx cancelled
// by a newer event) should silently leave the last known-good index in
// place, not blow away an already-working project out from under the user.
// The next debounced trigger (or a manual re-index) gets another chance.
func (s *Service) runReindex(ctx context.Context) {
	s.mu.Lock()
	s.reindexing = true
	s.reindexPending = false
	root := s.projectRoot
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.reindexing = false
		s.mu.Unlock()
	}()

	result, err := s.analyzer.Analyze(ctx, root)
	if err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || s.projectRoot != root {
		return
	}
	if err := s.store.ReplaceIndex(ctx, result); err != nil {
		return
	}
	s.indexVersion++
}

// Close stops the background file watcher, if one is running, and waits for
// its goroutine to fully exit. Safe to call even if no project was ever
// opened (watchCancel is nil then — a no-op). Does not close the Store; the
// container that constructed this Service owns that lifecycle separately
// (see internal/app/container.go's existing Database/SSH/Kafka Close calls).
func (s *Service) Close() error {
	s.stopWatcher()
	return nil
}

// WaitForIndexReady blocks until no background re-index is pending or
// running, then returns the current index version. Callers that just wrote
// a file (or otherwise expect a re-index) should call this before trusting
// any other query's result — see this plan's "Known limitation" note for the
// one narrow timing edge this does not close.
//
// Implemented as a short poll rather than a sync.Cond: the debounce interval
// is hundreds of milliseconds, so a 20ms poll is imperceptible, and it composes
// cleanly with ctx cancellation (sync.Cond.Wait does not, without extra
// goroutine plumbing this doesn't otherwise need).
func (s *Service) WaitForIndexReady(ctx context.Context) (int, error) {
	for {
		s.mu.RLock()
		active := s.active
		busy := s.reindexing || s.reindexPending
		version := s.indexVersion
		s.mu.RUnlock()

		if !active {
			return 0, ErrNoActiveProject
		}
		if !busy {
			return version, nil
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (s *Service) ReadFile(ctx context.Context, relativePath string) (string, error) {
	_, root, err := s.activeProject()
	if err != nil {
		return "", err
	}
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("codegraph: read file: %w", err)
	}
	return string(data), nil
}

// WriteFile re-validates relativePath against the *current* project root
// (not whatever was active when the file was opened for editing) — the
// active project can change between a UI open and a Ctrl+S save.
func (s *Service) WriteFile(ctx context.Context, relativePath, content string) error {
	_, root, err := s.activeProject()
	if err != nil {
		return err
	}
	resolved, err := resolvePath(root, relativePath)
	if err != nil {
		return err
	}
	if fi, err := os.Lstat(resolved); err == nil && (fi.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("codegraph: refusing to write through symlink %q: %w", relativePath, ErrPathOutsideProject)
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return fmt.Errorf("codegraph: write file: %w", err)
	}
	return nil
}

// GetCodeSnippet returns the real source text for filePath, snapped to the
// enclosing method/class's actual boundaries when the index has one covering
// line — the caller does not need to already know a method's exact line
// range, e.g. clicking anywhere inside a graph node's body should still show
// the whole method. Falls back to the input line/endLine verbatim when
// nothing in the index covers that line (a non-Java file, or an unindexed
// location) — still useful for arbitrary text files' File editing preview.
func (s *Service) GetCodeSnippet(ctx context.Context, filePath string, line, endLine int) (CodeSnippet, error) {
	store, root, err := s.activeProject()
	if err != nil {
		return CodeSnippet{}, err
	}
	resolved, err := resolvePath(root, filePath)
	if err != nil {
		return CodeSnippet{}, err
	}

	useLine, useEndLine, fqn := line, endLine, ""
	if node, ok, err := store.FindNodeAtLine(ctx, filePath, line); err != nil {
		return CodeSnippet{}, err
	} else if ok {
		useLine, useEndLine, fqn = node.Line, node.EndLine, node.Label
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return CodeSnippet{}, fmt.Errorf("codegraph: read file: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	if useLine < 1 || useEndLine > len(lines) || useLine > useEndLine {
		return CodeSnippet{}, fmt.Errorf("codegraph: line range %d-%d out of bounds for %q (%d lines)", useLine, useEndLine, filePath, len(lines))
	}
	source := strings.Join(lines[useLine-1:useEndLine], "\n")

	return CodeSnippet{
		FilePath: filePath,
		Line:     useLine,
		EndLine:  useEndLine,
		Source:   source,
		Language: languageForPath(filePath),
		FQN:      fqn,
	}, nil
}

func languageForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".java":
		return "java"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".md":
		return "markdown"
	default:
		return ""
	}
}

// nilStore is the container's placeholder Store before any project has been
// opened. Every method is unreachable in practice because Service checks
// s.active first — but a safe zero-value beats a nil-pointer panic if that
// invariant is ever broken by a future change.
type nilStore struct{}

func (nilStore) ReplaceIndex(context.Context, AnalysisResult) error { return ErrNoActiveProject }
func (nilStore) HasIndex(context.Context) (bool, error)             { return false, nil }
func (nilStore) ListEndpoints(context.Context) ([]Endpoint, error)  { return nil, ErrNoActiveProject }
func (nilStore) GetEndpoint(context.Context, string) (Endpoint, error) {
	return Endpoint{}, ErrNoActiveProject
}
func (nilStore) GetNode(context.Context, string) (Node, error)       { return Node{}, ErrNoActiveProject }
func (nilStore) GetOutEdges(context.Context, string) ([]Edge, error) { return nil, ErrNoActiveProject }
func (nilStore) GetInEdges(context.Context, string) ([]Edge, error)  { return nil, ErrNoActiveProject }
func (nilStore) SearchSymbol(context.Context, string) ([]SymbolMatch, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) FindNodeAtLine(context.Context, string, int) (Node, bool, error) {
	return Node{}, false, ErrNoActiveProject
}
func (nilStore) GetDependents(context.Context, string) ([]string, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) Close() error { return nil }

// NewNilStore returns a placeholder Store that always reports no active
// project. internal/app/container.go uses this to construct Service before
// any CodeGraph project has been opened, since container.go is a different
// package and cannot construct the unexported nilStore type directly.
func NewNilStore() Store { return nilStore{} }
