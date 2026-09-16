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
	"unicode/utf8"

	"github.com/fsnotify/fsnotify"

	"devkit/internal/terminal"
)

// maxFlowDepth caps the BFS walk in GetApiFlow so a pathological call graph
// (very deep chain, or a cycle missed by the visited-set for some reason)
// can never turn into an unbounded response — same "bounded, no full-graph
// dump" contract the design doc requires everywhere else in this module.
const maxFlowDepth = 32

// Service is the CodeGraph domain service. It owns exactly one active
// project's index at a time (FR-1: no multi-project registry in v1).
type Service struct {
	openMu      sync.Mutex
	mu          sync.RWMutex
	store       Store
	analyzer    Analyzer
	active      bool
	projectRoot string

	terminalRegistry *terminal.Registry

	// toolchains is what BuildAnalyzer can build; see toolchain.go. Guarded by
	// mu with everything else. buildMu is separate and is held for the whole
	// length of a build — minutes on a cold Maven cache — which mu, taken on
	// every read of the index, could not be.
	toolchains []Toolchain
	buildMu    sync.Mutex

	// detected is what the open folder turned out to hold — which language, and
	// which module roots. Recorded during OpenProject so the answer costs one
	// walk rather than one per caller who wants it, and so the UI can say "4
	// modules" instead of leaving a monorepo looking like a single project.
	detected DetectedProject

	opening      bool
	indexErr     error
	generation   uint64
	request      uint64
	reindexCount int
	sessionPath  string
	indexVersion int
	reindexing   bool

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
	reindexPending        bool
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
	s.openMu.Lock()
	defer s.openMu.Unlock()
	return s.setProjectPath(dbPath)
}

func (s *Service) setProjectPath(dbPath string) error {
	s.stopWatcher()
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
	s.generation++
	s.reindexCount = 0
	s.reindexing = false
	s.indexVersion = 0
	s.active = false
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

// SetTerminalRegistry wires the shared Terminal primitive's Registry into
// this Service — called once from container construction (see main.go),
// separately from NewService so NewService's signature (used by every
// existing test in this package) doesn't need to change.
func (s *Service) SetTerminalRegistry(reg *terminal.Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminalRegistry = reg
}

// SetAnalyzer replaces the Analyzer used by OpenProject/runReindex — called
// once from main.go's post-construction wiring (mirrors SetTerminalRegistry)
// once the real Java analyzer jar's location is resolved, replacing the
// zero-value stub container.go constructs Service with. Like
// SetTerminalRegistry, this must only be called during startup wiring,
// before the app begins serving requests. Each indexing request captures
// its analyzer under mu, so a running request keeps the instance it started with.
func (s *Service) SetAnalyzer(analyzer Analyzer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.analyzer = analyzer
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
	reg := s.terminalRegistry
	s.mu.RUnlock()

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

// OpenProject makes a folder the active project. A failed analyze leaves the
// service with no active project rather than a stale or partial one.
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
func (s *Service) OpenProject(ctx context.Context, projectPath string) error {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if err := s.openProject(ctx, projectPath); err != nil {
		return err
	}
	return s.saveProject(projectPath)
}

// OpenProjectAt serializes the store switch and open as a single operation.
func (s *Service) OpenProjectAt(ctx context.Context, dbPath, projectPath string) error {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if err := s.setProjectPath(dbPath); err != nil {
		return err
	}
	if err := s.openProject(ctx, projectPath); err != nil {
		return err
	}
	return s.saveProject(projectPath)
}

func (s *Service) openProject(ctx context.Context, projectPath string) (openErr error) {
	s.stopWatcher()
	s.mu.Lock()
	s.generation++
	s.opening = true
	s.active = false
	s.reindexCount = 0
	s.reindexing = false
	s.indexErr = nil
	analyzer := s.analyzer
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.opening = false
		if openErr != nil {
			s.indexErr = openErr
		}
		s.mu.Unlock()
	}()
	// Only where there is something to detect with. A Service with no
	// toolchains registered — an ephemeral one, a test with its own analyzer —
	// has no vocabulary for recognising a project, and refusing every folder on
	// the strength of an empty registry would be a gate that knows nothing
	// standing in front of an analyzer that does.
	var detected DetectedProject
	if len(s.toolchainList()) > 0 {
		found, err := s.DetectProject(projectPath)
		if err != nil {
			s.stopWatcher()
			return fmt.Errorf("codegraph: open %q: %w", projectPath, err)
		}
		detected = found
	}
	s.mu.Lock()
	s.detected = detected
	s.mu.Unlock()

	// Ask before analyzing. Both halves are required: the store must hold an
	// index AND that index must be this project's. A store holding rows is a
	// weaker claim than a store holding the right rows, and acting on the
	// weaker one hands one project's index to another.
	if s.storedIndexMatches(ctx, projectPath) {
		s.mu.Lock()
		s.active = true
		s.projectRoot = projectPath
		s.mu.Unlock()

		_ = s.restartWatcher(projectPath) // non-fatal: see restartWatcher's doc comment
		// Reserve the refresh before returning. Neither queries nor readiness
		// may observe the cached graph as ready before this run commits.
		run := s.prepareReindex(context.Background())
		go run()
		return nil
	}

	result, err := analyzer.Analyze(ctx, projectPath)
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
	version, err := s.store.IndexVersion(ctx)
	if err != nil {
		s.active = false
		s.mu.Unlock()
		return err
	}
	s.indexVersion = version
	s.mu.Unlock()

	// Stamp the index with the project it describes, so the next open can
	// recognise it. Best-effort: failing to record it costs one re-index, which
	// is not worth failing an otherwise successful open over.
	if err := s.store.SetIndexedProject(ctx, filepath.Clean(projectPath)); err != nil {
		// Nothing to do but carry on; the next open re-analyzes.
		_ = err
	}

	_ = s.restartWatcher(projectPath) // non-fatal: see restartWatcher's doc comment
	return nil
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

// storedIndexMatches reports whether the open store already holds an index
// built from this exact project.
func (s *Service) storedIndexMatches(ctx context.Context, projectPath string) bool {
	has, err := s.store.HasIndex(ctx)
	if err != nil || !has {
		return false
	}
	indexed, err := s.store.IndexedProject(ctx)
	if err != nil || indexed == "" {
		// Unknown provenance — an index written before the stamp existed. One
		// re-index costs less than showing the wrong project's graph.
		return false
	}
	return indexed == filepath.Clean(projectPath)
}

// activeProject returns the current store and project root under the read
// lock, so neither field is ever read concurrently with the writes
// SetProjectPath/OpenProject make to them under s.mu.Lock(). Both are used
// after the lock is released — *sql.DB (what sqliteStore wraps) and a plain
// ProjectRoot returns the directory the active project was opened from, or ""
// when no project is open. Callers use it to offer that directory as a default
// elsewhere in the app, so "no project" is an empty string rather than an
// error — there is nothing wrong, there is simply nothing to offer.
func (s *Service) ProjectRoot() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active {
		return ""
	}
	return s.projectRoot
}

// string are both safe to use unsynchronized once captured.
func (s *Service) activeProject() (Store, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.opening || s.reindexing || s.reindexPending || s.indexErr != nil {
		return nil, "", ErrIndexNotReady
	}
	if !s.active {
		return nil, "", ErrNoActiveProject
	}
	return s.store, s.projectRoot, nil
}

// File operations use the selected directory, not the derived graph. They
// remain available while the watcher rebuilds after an editor save.
func (s *Service) projectFiles() (Store, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.opening {
		return nil, "", ErrIndexNotReady
	}
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
		steps = append(steps, FlowStep{
			Label:    node.Label,
			FilePath: node.FilePath,
			Line:     node.Line,
			Summary:  node.Summary,
			Params:   node.Params,
		})

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
	store, root, err := s.activeProject()
	if err != nil {
		return nil, err
	}

	if filepath.IsAbs(filePath) {
		filePath, err = filepath.Rel(root, filePath)
		if err != nil {
			return nil, ErrPathOutsideProject
		}
	}
	filePath = filepath.Clean(filePath)
	if filePath == ".." || strings.HasPrefix(filePath, ".."+string(filepath.Separator)) {
		return nil, ErrPathOutsideProject
	}
	visited := map[string]bool{filePath: true}
	result := []string{}
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

// maxDataFlowDepth bounds how many calls the trace will follow, the same
// defensive ceiling as maxNeighborDepth/maxFlowDepth. A trace that hits it says
// so in the step rather than just stopping.
const maxDataFlowDepth = 6

// ListFields returns the fields one node touches — the entry point for a data
// flow trace, since TraceDataFlow needs a field name the caller cannot guess.
func (s *Service) ListFields(ctx context.Context, nodeID string) ([]FieldRef, error) {
	store, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	return store.ListFields(ctx, nodeID)
}

// TraceDataFlow walks the field's accesses in call-graph order.
//
// Two passes on purpose. First, find every method that touches the field —
// those are the only methods worth walking. Then depth-first from the ones that
// nothing else in that set calls, so the trace starts where the value starts
// rather than in the middle.
//
// Within a method, field accesses and outgoing call sites are merged and sorted
// by line, which is what puts "the write, then the call it flows into" next to
// each other. That ordering is SOURCE order, not execution order — see
// DataFlowTrace.OrderedBy.
func (s *Service) TraceDataFlow(ctx context.Context, symbolID, fieldName string) (DataFlowTrace, error) {
	store, err := s.activeStore()
	if err != nil {
		return DataFlowTrace{}, err
	}
	if _, err := store.GetNode(ctx, symbolID); err != nil {
		return DataFlowTrace{}, err
	}

	accesses, err := store.GetFieldAccesses(ctx, symbolID, fieldName)
	if err != nil {
		return DataFlowTrace{}, err
	}

	touching := make(map[string]bool, len(accesses))
	byMethod := make(map[string][]FieldAccess, len(accesses))
	for _, a := range accesses {
		touching[a.NodeID] = true
		byMethod[a.NodeID] = append(byMethod[a.NodeID], a)
	}

	// A root is a touching method that no other touching method calls. If every
	// one is called by another (a cycle among them), fall back to all of them so
	// the trace is never empty just because the graph loops.
	calledByTouching := make(map[string]bool)
	for id := range touching {
		sites, err := store.GetCallSites(ctx, id)
		if err != nil {
			return DataFlowTrace{}, err
		}
		for _, c := range sites {
			if c.To != "" && touching[c.To] {
				calledByTouching[c.To] = true
			}
		}
	}
	roots := make([]string, 0, len(touching))
	for id := range touching {
		if !calledByTouching[id] {
			roots = append(roots, id)
		}
	}
	if len(roots) == 0 {
		for id := range touching {
			roots = append(roots, id)
		}
	}
	sort.Strings(roots) // stable output for the same index

	trace := DataFlowTrace{SymbolID: symbolID, FieldName: fieldName, OrderedBy: "source"}
	onPath := make(map[string]bool)

	var walk func(nodeID string, depth int) error
	walk = func(nodeID string, depth int) error {
		node, err := store.GetNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, ErrNodeNotFound) {
				return nil // index drifted between reads; skip rather than fail the whole trace
			}
			return err
		}
		sites, err := store.GetCallSites(ctx, nodeID)
		if err != nil {
			return err
		}

		type event struct {
			line       int
			kind       string
			targetID   string
			unresolved string
		}
		events := make([]event, 0, len(byMethod[nodeID])+len(sites))
		for _, a := range byMethod[nodeID] {
			events = append(events, event{line: a.Line, kind: a.Kind})
		}
		for _, c := range sites {
			// An unresolved call has no evidence of touching this field.
			// Only connect methods with indexed accesses to the requested field.
			if !touching[c.To] {
				continue
			}
			events = append(events, event{line: c.Line, kind: "call", targetID: c.To, unresolved: c.Unresolved})
		}
		sort.SliceStable(events, func(i, j int) bool { return events[i].line < events[j].line })

		unique := make(map[event]bool)
		deduped := events[:0]
		for _, e := range events {
			if !unique[e] {
				unique[e] = true
				deduped = append(deduped, e)
			}
		}
		events = deduped
		onPath[nodeID] = true
		defer delete(onPath, nodeID)

		for _, e := range events {
			step := DataFlowStep{
				NodeID:     nodeID,
				Label:      node.Label,
				FilePath:   node.FilePath,
				Line:       e.line,
				Kind:       e.kind,
				Depth:      depth,
				TargetID:   e.targetID,
				Unresolved: e.unresolved,
			}
			if e.kind == "call" && e.unresolved == "" {
				switch {
				case onPath[e.targetID]:
					step.Unresolved = "cycle"
				case depth+1 > maxDataFlowDepth:
					step.Unresolved = "depth"
				}
			}
			trace.Steps = append(trace.Steps, step)

			if e.kind == "call" && step.Unresolved == "" {
				if err := walk(e.targetID, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, root := range roots {
		if err := walk(root, 0); err != nil {
			return DataFlowTrace{}, err
		}
	}
	return trace, nil
}

const (
	// maxNeighborDepth bounds how many hops GetNeighbors will walk, same
	// defensive-ceiling reasoning as maxFlowDepth/maxBlastRadiusDepth.
	maxNeighborDepth = 6
	// maxNeighborNodes is the hard per-page node ceiling frontend-performance.mdx
	// requires ("Ceiling node/edge cứng"). Chosen as a reasonable single-screen
	// subgraph size for v1 — no UI exists yet to tune this against real usage.
	maxNeighborNodes = 200
	// maxNeighborEdges is the other half of that "Ceiling node/edge cứng"
	// requirement: intra-page edges are capped independently of nodes, so a
	// dense cluster that fits under the node ceiling can't balloon the
	// response to O(nodes²) edges.
	maxNeighborEdges = 1000
)

// GetNeighbors returns a bounded subgraph around rootID: every node
// reachable within depth hops in EITHER edge direction (unlike GetApiFlow's
// directed-only walk — this is "what's connected to this node", not a call
// chain), filtered by filter.Layers, paginated to at most maxNeighborNodes
// nodes and maxNeighborEdges intra-page edges per call — the design doc's
// "Ceiling node/edge cứng" is a hard ceiling on both dimensions, so a dense
// cluster that fits under the node ceiling cannot balloon the response.
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

	// Class nodes have no call edges of their own. Seed the walk with their
	// declared methods and expose containment so the result stays connected.
	if root.Kind == "class" && depth > 0 {
		matches, err := store.SearchSymbol(ctx, root.Label+".")
		if err != nil {
			return Subgraph{}, err
		}
		for _, match := range matches {
			if match.Kind != "method" {
				continue
			}
			signature := strings.SplitN(match.NodeID, "(", 2)[0]
			dot := strings.LastIndex(signature, ".")
			if dot < 0 || signature[:dot] != rootID {
				continue
			}
			child, err := store.GetNode(ctx, match.NodeID)
			if err != nil {
				return Subgraph{}, err
			}
			if !allowedLayer(child) {
				continue
			}
			visited[child.ID] = child
			frontier = append(frontier, child.ID)
			allEdges = append(allEdges, Edge{From: rootID, To: child.ID, Kind: "contains"})
			edgeSeen[rootID+"->"+child.ID] = true
		}
	}

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
	edgesTruncated := false
	if len(edges) > maxNeighborEdges {
		edges = edges[:maxNeighborEdges]
		edgesTruncated = true
	}

	// Truncated reports that this page hit a ceiling — node-side (more pages
	// exist; Continuation fetches them) or edge-side (this page's edge list
	// was cut at maxNeighborEdges; there is no edge continuation — the caller
	// sees a node-complete but edge-partial page).
	truncated := end < len(ids) || edgesTruncated
	continuation := ""
	if end < len(ids) {
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
	_, root, err := s.projectFiles()
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
		s.request++
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
			run := s.prepareReindex(reindexCtx)
			go run()

		case _, ok := <-w.Errors:
			if !ok {
				return
			}
			// best-effort: a single watcher error (e.g. a transient OS-level
			// read error) doesn't stop watching — the loop keeps running.
		}
	}
}

// Failed refreshes keep the stored graph for recovery but never expose it
// as ready. A later watcher event or explicit open retries the analysis.
// Register work synchronously, before launching its goroutine. Readiness must
// include queued work, and older runs must never publish over newer requests.
func (s *Service) prepareReindex(ctx context.Context) func() {
	s.mu.Lock()
	s.reindexCount++
	s.reindexing = true
	s.reindexPending = false
	s.request++
	generation, request := s.generation, s.request
	root, store, analyzer := s.projectRoot, s.store, s.analyzer
	s.mu.Unlock()
	return func() {
		result, err := analyzer.Analyze(ctx, root)
		s.mu.Lock()
		if s.generation != generation {
			s.mu.Unlock()
			return
		}
		changed := false
		if s.request == request && s.active {
			if err == nil {
				err = ctx.Err()
			}
			if err == nil {
				err = store.ReplaceIndex(ctx, result)
			}
			if err == nil {
				s.indexVersion, err = store.IndexVersion(ctx)
				changed = err == nil
			}
			s.indexErr = err
		}
		s.reindexCount--
		s.reindexing = s.reindexCount > 0
		notify, version := s.OnIndexUpdated, s.indexVersion
		s.mu.Unlock()
		if changed && notify != nil {
			notify(version)
		}
	}
}

func (s *Service) runReindex(ctx context.Context) { s.prepareReindex(ctx)() }

// Close stops the background file watcher, if one is running, and waits for
// its goroutine to fully exit. Safe to call even if no project was ever
// opened (watchCancel is nil then — a no-op). Does not close the Store; the
// container that constructed this Service owns that lifecycle separately
// (see internal/app/container.go's existing Database/SSH/Kafka Close calls).
func (s *Service) Close() error {
	s.openMu.Lock()
	defer s.openMu.Unlock()
	s.stopWatcher()
	s.mu.Lock()
	s.generation++ // A late analyzer result cannot publish after shutdown.
	s.active = false
	s.reindexing = false
	s.reindexCount = 0
	s.mu.Unlock()
	return nil
}

// WaitForIndexReady blocks through cold analysis, cached startup refresh,
// and queued/running re-indexes, then returns the persistent project revision. Callers that just wrote
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
		busy := s.opening || s.reindexing || s.reindexPending
		indexErr := s.indexErr
		version := s.indexVersion
		s.mu.RUnlock()

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

func (s *Service) ReadFile(ctx context.Context, relativePath string) (string, error) {
	_, root, err := s.projectFiles()
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
	if !utf8.Valid(data) {
		return "", ErrBinaryFile
	}
	return string(data), nil
}

// WriteFile re-validates relativePath against the *current* project root
// (not whatever was active when the file was opened for editing) — the
// active project can change between a UI open and a Ctrl+S save.
func (s *Service) WriteFile(ctx context.Context, relativePath, content string) error {
	_, root, err := s.projectFiles()
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

// validatePlan rejects internally inconsistent plans at the boundary, the
// same way AnalysisResult.Validate rejects bad analyzer output — a bad plan
// caught here beats a confusing failure three layers later.
func validatePlan(plan Plan) error {
	if len(plan.Nodes) == 0 {
		return fmt.Errorf("%w: plan has no nodes", ErrInvalidPlan)
	}
	ids := make(map[string]bool, len(plan.Nodes))
	for _, n := range plan.Nodes {
		if n.PlanNodeID == "" {
			return fmt.Errorf("%w: plan node with empty id", ErrInvalidPlan)
		}
		if ids[n.PlanNodeID] {
			return fmt.Errorf("%w: duplicate plan node id %q", ErrInvalidPlan, n.PlanNodeID)
		}
		if n.FQN == "" {
			return fmt.Errorf("%w: plan node %q has no fqn", ErrInvalidPlan, n.PlanNodeID)
		}
		ids[n.PlanNodeID] = true
	}
	for _, e := range plan.Edges {
		if !ids[e.From] {
			return fmt.Errorf("%w: edge references unknown plan node %q", ErrInvalidPlan, e.From)
		}
		if !ids[e.To] {
			return fmt.Errorf("%w: edge references unknown plan node %q", ErrInvalidPlan, e.To)
		}
	}
	return nil
}

// ProposeFlow replaces the project's active plan with a new draft and
// returns its progress immediately. The returned progress can already be
// `complete` when every planned method exists and the flow already matches
// real code — the doc's explicit special case, where no approve/implement
// cycle is needed at all.
func (s *Service) ProposeFlow(ctx context.Context, plan Plan) (PlanProgress, error) {
	store, err := s.activeStore()
	if err != nil {
		return PlanProgress{}, err
	}
	if err := validatePlan(plan); err != nil {
		return PlanProgress{}, err
	}

	plan.State = PlanStateDraft
	if err := store.SavePlan(ctx, plan); err != nil {
		return PlanProgress{}, err
	}
	return computeProgress(ctx, store, plan)
}

// GetPlannedFlow returns the active plan as proposed, without computing
// statuses — the shape an implementing agent needs (intended FQNs, edges,
// conditions), not a progress report.
func (s *Service) GetPlannedFlow(ctx context.Context) (Plan, error) {
	store, err := s.activeStore()
	if err != nil {
		return Plan{}, err
	}
	return store.GetPlan(ctx)
}

// computeProgress resolves every plan node's FQN against the current index,
// collects each resolved node's real out-edges, and runs the pure status
// functions over the result. Statuses are always computed here, never
// stored — so any re-index (including the background one) is reflected on
// the very next call with no recompute hook anywhere.
func computeProgress(ctx context.Context, store Store, plan Plan) (PlanProgress, error) {
	resolved := make(map[string]Node, len(plan.Nodes))
	realOutTargets := make(map[string][]string, len(plan.Nodes))
	for _, n := range plan.Nodes {
		real, ok, err := store.ResolveFQN(ctx, n.FQN)
		if err != nil {
			return PlanProgress{}, err
		}
		if !ok {
			continue
		}
		resolved[n.PlanNodeID] = real
		if _, seen := realOutTargets[real.ID]; seen {
			continue // two plan nodes resolved to the same real node
		}
		edges, err := store.GetOutEdges(ctx, real.ID)
		if err != nil {
			return PlanProgress{}, err
		}
		targets := make([]string, 0, len(edges))
		for _, e := range edges {
			if e.Kind != "call" {
				continue
			}
			targets = append(targets, e.To)
		}
		realOutTargets[real.ID] = targets
	}

	progress := PlanProgress{
		Description: plan.Description,
		State:       plan.State,
		Nodes:       make([]PlanNodeProgress, 0, len(plan.Nodes)),
	}
	statuses := make([]string, 0, len(plan.Nodes))
	for _, n := range plan.Nodes {
		status := planNodeStatus(n.PlanNodeID, plan.Edges, resolved, realOutTargets)
		statuses = append(statuses, status)
		progress.Nodes = append(progress.Nodes, PlanNodeProgress{
			PlanNode:   n,
			Status:     status,
			RealNodeID: resolved[n.PlanNodeID].ID,
		})
	}
	progress.Aggregate = aggregateStatus(statuses)
	return progress, nil
}

// GetPlanProgress returns per-node status plus the derived whole-plan
// status. Statuses are computed fresh against the current index on every
// call, so progress reflects the latest re-index automatically.
func (s *Service) GetPlanProgress(ctx context.Context) (PlanProgress, error) {
	store, err := s.activeStore()
	if err != nil {
		return PlanProgress{}, err
	}
	plan, err := store.GetPlan(ctx)
	if err != nil {
		return PlanProgress{}, err
	}
	return computeProgress(ctx, store, plan)
}

// ApprovePlan moves the active plan from draft to approved. Human-only by
// design — the bridge exposes it, MCP deliberately does not (see the doc's
// Bridge & MCP table: approving a plan is always a human decision).
func (s *Service) ApprovePlan(ctx context.Context) error {
	store, err := s.activeStore()
	if err != nil {
		return err
	}
	return store.SetPlanState(ctx, PlanStateApproved)
}

// RelinkPlanNode repoints one plan node at a different FQN — the escape
// hatch for the deliberate "matching by FQN breaks on rename" tradeoff.
// Explicit by design: DevKit never guesses that a new name is a renamed old
// one (see the doc's non-goals).
func (s *Service) RelinkPlanNode(ctx context.Context, planNodeID, fqn string) error {
	store, err := s.activeStore()
	if err != nil {
		return err
	}
	if fqn == "" {
		return fmt.Errorf("%w: relink requires a non-empty fqn", ErrInvalidPlan)
	}
	return store.SetPlanNodeFQN(ctx, planNodeID, fqn)
}

// nilStore is the container's placeholder Store before any project has been
// opened. Every method is unreachable in practice because Service checks
// s.active first — but a safe zero-value beats a nil-pointer panic if that
// invariant is ever broken by a future change.
type nilStore struct{}

func (nilStore) IndexVersion(context.Context) (int, error) { return 0, nil }

func (nilStore) ReplaceIndex(context.Context, AnalysisResult) error { return ErrNoActiveProject }
func (nilStore) IndexedProject(context.Context) (string, error)     { return "", nil }
func (nilStore) SetIndexedProject(context.Context, string) error    { return nil }
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
func (nilStore) GetFieldAccesses(context.Context, string, string) ([]FieldAccess, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) GetCallSites(context.Context, string) ([]CallSite, error) {
	return nil, ErrNoActiveProject
}

func (nilStore) AddComment(context.Context, PlanComment) error { return ErrNoActiveProject }
func (nilStore) ListComments(context.Context, string) ([]PlanComment, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) CountComments(context.Context) (map[string]int, error) {
	return nil, ErrNoActiveProject
}

func (nilStore) ListFields(context.Context, string) ([]FieldRef, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) SavePlan(context.Context, Plan) error                 { return ErrNoActiveProject }
func (nilStore) GetPlan(context.Context) (Plan, error)                { return Plan{}, ErrNoActiveProject }
func (nilStore) SetPlanState(context.Context, string) error           { return ErrNoActiveProject }
func (nilStore) SetPlanNodeFQN(context.Context, string, string) error { return ErrNoActiveProject }
func (nilStore) ResolveFQN(context.Context, string) (Node, bool, error) {
	return Node{}, false, ErrNoActiveProject
}
func (nilStore) Close() error { return nil }

// NewNilStore returns a placeholder Store that always reports no active
// project. internal/app/container.go uses this to construct Service before
// any CodeGraph project has been opened, since container.go is a different
// package and cannot construct the unexported nilStore type directly.
func NewNilStore() Store { return nilStore{} }
