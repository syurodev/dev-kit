// internal/codegraph/graph_query.go
package codegraph

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// maxFlowDepth caps how many calls deep GetApiFlow follows, so a
	// pathological call graph can never turn into an unbounded response — same
	// "bounded, no full-graph dump" contract the design doc requires everywhere
	// else in this module.
	maxFlowDepth = 32
	// maxFlowSteps caps the total nodes one flow returns. Depth alone does not
	// bound a flow: a controller fanning out to a service layer that calls
	// everything can reach thousands of methods within a few hops.
	maxFlowSteps = 500
)

func (s *Service) ListEntryPoints(ctx context.Context) ([]EntryPoint, error) {
	store, release, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	defer release()
	entries, err := store.ListEntryPoints(ctx)
	if err != nil {
		return nil, err
	}
	entries = completeEntryPoints(entries)
	sortEntryPoints(entries)
	return entries, nil
}

func (s *Service) SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error) {
	store, release, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	defer release()
	return store.SearchSymbol(ctx, query)
}

// GetApiFlow walks the call graph from the endpoint's entry node, in Go
// (not SQL), so the traversal — including cycle protection — is testable
// without a database.
func (s *Service) GetApiFlow(ctx context.Context, entryPointID string) (ApiFlow, error) {
	store, release, err := s.activeStore()
	if err != nil {
		return ApiFlow{}, err
	}
	defer release()
	ep, err := store.GetEntryPoint(ctx, entryPointID)
	if err != nil {
		return ApiFlow{}, err
	}
	steps, err := walkFlow(ctx, store, ep.EntryNode)
	if err != nil {
		return ApiFlow{}, err
	}
	return ApiFlow{EntryPointID: entryPointID, Steps: steps}, nil
}

// walkFlow returns the call chain below rootNodeID, depth-first in call order.
//
// Two passes. The first is breadth-first and finds each reachable method's
// SHORTEST call depth, bounded by maxFlowDepth and maxFlowSteps. The second
// lays those methods out depth-first, descending into a callee only along a
// shortest path to it.
//
// A single depth-first pass with one global visited set — what this used to
// be — marks a method visited the first time it is reached, however deep. If
// that first route was a long one that ran into maxFlowDepth, everything below
// the method was cut, and the short route reaching it later was ignored
// because it was already "visited": the flow lost methods that were only a
// few calls from the endpoint. Recording shortest depths first means every
// method within the bounds appears exactly once, under a parent that reaches
// it by a shortest route.
func walkFlow(ctx context.Context, store Store, rootNodeID string) ([]FlowStep, error) {
	depth := map[string]int{rootNodeID: 0}
	calls := map[string][]string{}
	order := []string{rootNodeID}
	frontier := []string{rootNodeID}
	for d := 0; d < maxFlowDepth && len(frontier) > 0 && len(depth) < maxFlowSteps; d++ {
		var next []string
		for _, id := range frontier {
			edges, err := store.GetOutEdges(ctx, id)
			if err != nil {
				return nil, err
			}
			for _, e := range edges {
				if e.Kind != "call" {
					continue
				}
				calls[id] = append(calls[id], e.To)
				if _, seen := depth[e.To]; seen || len(depth) >= maxFlowSteps {
					continue
				}
				depth[e.To] = d + 1
				order = append(order, e.To)
				next = append(next, e.To)
			}
		}
		frontier = next
	}

	nodes, err := store.GetNodes(ctx, order)
	if err != nil {
		return nil, err
	}
	if _, ok := nodes[rootNodeID]; !ok {
		return nil, ErrNodeNotFound
	}

	steps := make([]FlowStep, 0, len(depth))
	emitted := make(map[string]bool, len(depth))
	var walk func(id string)
	walk = func(id string) {
		emitted[id] = true
		if node, ok := nodes[id]; ok {
			steps = append(steps, FlowStep{
				Label:    node.Label,
				FilePath: node.FilePath,
				Line:     node.Line,
				Summary:  node.Summary,
				Params:   node.Params,
			})
		}
		for _, callee := range calls[id] {
			if calleeDepth, ok := depth[callee]; ok && !emitted[callee] && calleeDepth == depth[id]+1 {
				walk(callee)
			}
		}
	}
	walk(rootNodeID)
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
	store, root, release, err := s.activeProject()
	if err != nil {
		return nil, err
	}
	defer release()

	filePath, err = projectRelative(root, filePath)
	if err != nil {
		return nil, err
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

const (
	// maxDataFlowDepth bounds how many calls the trace will follow, the same
	// defensive ceiling as maxNeighborDepth/maxFlowDepth. A trace that hits it
	// says so in the step rather than just stopping.
	maxDataFlowDepth = 6
	// maxDataFlowSteps caps the whole trace. The last step before the cut is
	// marked "limit", so a truncated trace never reads as a complete one.
	maxDataFlowSteps = 2000
)

// ListFields returns the fields one node touches — the entry point for a data
// flow trace, since TraceDataFlow needs a field name the caller cannot guess.
func (s *Service) ListFields(ctx context.Context, nodeID string) ([]FieldRef, error) {
	store, release, err := s.activeStore()
	if err != nil {
		return nil, err
	}
	defer release()
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
//
// Each method is expanded once. A second call into a method already traced
// above is shown as a call marked "repeat" and not walked again: without
// that, a diamond of methods that all touch the field was walked once per
// PATH through it, which grows exponentially with the diamonds in a chain.
func (s *Service) TraceDataFlow(ctx context.Context, symbolID, fieldName string) (DataFlowTrace, error) {
	store, root, release, err := s.activeProject()
	if err != nil {
		return DataFlowTrace{}, err
	}
	defer release()
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

	// Call sites are read once per method and reused by both passes.
	sitesOf := make(map[string][]CallSite, len(touching))
	for id := range touching {
		sites, err := store.GetCallSites(ctx, id)
		if err != nil {
			return DataFlowTrace{}, err
		}
		sitesOf[id] = sites
	}

	// A root is a touching method that no other touching method calls. If every
	// one is called by another (a cycle among them), fall back to all of them so
	// the trace is never empty just because the graph loops.
	calledByTouching := make(map[string]bool)
	for _, sites := range sitesOf {
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

	methodIDs := make([]string, 0, len(touching))
	for id := range touching {
		methodIDs = append(methodIDs, id)
	}
	nodes, err := store.GetNodes(ctx, methodIDs)
	if err != nil {
		return DataFlowTrace{}, err
	}

	trace := DataFlowTrace{SymbolID: symbolID, FieldName: fieldName, OrderedBy: "source"}
	onPath := make(map[string]bool)
	expanded := make(map[string]bool)
	full := false

	var walk func(nodeID string, depth int)
	walk = func(nodeID string, depth int) {
		node, ok := nodes[nodeID]
		if !ok {
			return // index drifted between reads; skip rather than fail the whole trace
		}
		expanded[nodeID] = true

		type event struct {
			line       int
			kind       string
			targetID   string
			unresolved string
		}
		sites := sitesOf[nodeID]
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
			if full {
				return
			}
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
				case expanded[e.targetID]:
					step.Unresolved = "repeat"
				case depth+1 > maxDataFlowDepth:
					step.Unresolved = "depth"
				}
			}
			if len(trace.Steps) == maxDataFlowSteps-1 {
				step.Unresolved = "limit"
				full = true
			}
			trace.Steps = append(trace.Steps, step)

			if e.kind == "call" && step.Unresolved == "" {
				walk(e.targetID, depth+1)
			}
		}
	}

	for _, root := range roots {
		if full {
			break
		}
		if expanded[root] {
			continue
		}
		walk(root, 0)
	}
	attachSourceLines(root, trace.Steps)
	return trace, nil
}

// maxDataFlowCodeLen keeps one step's source line to what a panel row shows.
const maxDataFlowCodeLen = 160

// attachSourceLines fills each step's Code with its source line, reading
// every file once. Best effort: a file that moved or cannot be read leaves
// its steps without code rather than failing a trace that is otherwise right.
func attachSourceLines(root string, steps []DataFlowStep) {
	files := make(map[string][]string)
	for i := range steps {
		path := steps[i].FilePath
		lines, seen := files[path]
		if !seen {
			if resolved, err := resolvePath(root, path); err == nil {
				if text, err := readTextFile(resolved); err == nil {
					lines = strings.Split(text, "\n")
				}
			}
			files[path] = lines
		}
		if n := steps[i].Line; n >= 1 && n <= len(lines) {
			steps[i].Code = clipLine(strings.TrimSpace(lines[n-1]), maxDataFlowCodeLen)
		}
	}
}

func clipLine(line string, max int) string {
	runes := []rune(line)
	if len(runes) <= max {
		return line
	}
	return string(runes[:max-1]) + "…"
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
	// maxNeighborVisit bounds the whole walk behind the pages. Depth 6 of a
	// well-connected service layer is most of a large project; the walk stops
	// collecting nodes here and reports the result as truncated, rather than
	// loading the project to serve a 200-node page.
	maxNeighborVisit = 5000
)

// GetNeighbors returns a bounded subgraph around rootID: every node
// reachable within depth hops in EITHER edge direction (unlike GetApiFlow's
// directed-only walk — this is "what's connected to this node", not a call
// chain), filtered by filter.Layers, paginated to at most maxNeighborNodes
// nodes and maxNeighborEdges intra-page edges per call — the design doc's
// "Ceiling node/edge cứng" is a hard ceiling on both dimensions, so a dense
// cluster that fits under the node ceiling cannot balloon the response.
//
// The walk is breadth-first, one batch of queries per level (edges of the
// whole frontier, then the nodes they reach) rather than one query per node,
// and stops collecting at maxNeighborVisit nodes — past that the result is
// marked Truncated.
//
// Pagination: the depth-bounded neighborhood is computed first, then sorted
// by node ID for a stable, deterministic order, then sliced by
// filter.Continuation's offset. This keeps Continuation a plain string
// offset — no server-side cursor/session state — at the cost of a stale
// offset silently skipping/duplicating a node if the index changes between
// pages (e.g. a background re-index lands mid-pagination); accepted for v1,
// same class of tradeoff the design doc's "layout cache invalidates on
// re-index" note already expects callers to handle.
func (s *Service) GetNeighbors(ctx context.Context, rootID string, depth int, filter NeighborFilter) (Subgraph, error) {
	store, release, err := s.activeStore()
	if err != nil {
		return Subgraph{}, err
	}
	defer release()

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
	// rejected remembers nodes the layer filter turned away, so a node
	// reachable along many edges is fetched and judged once.
	rejected := map[string]bool{}
	edgeSeen := map[string]bool{}
	var allEdges []Edge
	frontier := []string{rootID}
	budgetHit := false

	// Class nodes have no call edges of their own. Seed the walk with their
	// declared methods and expose containment so the result stays connected.
	if root.Kind == "class" && depth > 0 {
		methods, err := store.ListClassMethods(ctx, rootID)
		if err != nil {
			return Subgraph{}, err
		}
		for _, child := range methods {
			if !allowedLayer(child) {
				continue
			}
			if len(visited) >= maxNeighborVisit {
				budgetHit = true
				break
			}
			visited[child.ID] = child
			frontier = append(frontier, child.ID)
			allEdges = append(allEdges, Edge{From: rootID, To: child.ID, Kind: "contains"})
			edgeSeen[rootID+"->"+child.ID] = true
		}
	}

	for d := 0; d < depth && len(frontier) > 0 && !budgetHit; d++ {
		sort.Strings(frontier)
		edges, err := store.GetAdjacentEdges(ctx, frontier)
		if err != nil {
			return Subgraph{}, err
		}
		inFrontier := make(map[string]bool, len(frontier))
		for _, id := range frontier {
			inFrontier[id] = true
		}
		var unknown []string
		unknownSeen := map[string]bool{}
		for _, e := range edges {
			key := e.From + "->" + e.To
			if !edgeSeen[key] {
				edgeSeen[key] = true
				allEdges = append(allEdges, e)
			}
			for _, other := range []string{e.From, e.To} {
				if inFrontier[other] {
					continue
				}
				if _, ok := visited[other]; ok || rejected[other] || unknownSeen[other] {
					continue
				}
				unknownSeen[other] = true
				unknown = append(unknown, other)
			}
		}
		fetched, err := store.GetNodes(ctx, unknown)
		if err != nil {
			return Subgraph{}, err
		}
		var next []string
		for _, id := range unknown {
			n, ok := fetched[id]
			if !ok {
				continue // an edge to a node the index does not hold
			}
			if !allowedLayer(n) {
				rejected[id] = true
				continue
			}
			if len(visited) >= maxNeighborVisit {
				budgetHit = true
				break
			}
			visited[id] = n
			next = append(next, id)
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
	// exist; Continuation fetches them), edge-side (this page's edge list
	// was cut at maxNeighborEdges; there is no edge continuation — the caller
	// sees a node-complete but edge-partial page), or walk-side (the
	// neighborhood itself was cut at maxNeighborVisit).
	truncated := end < len(ids) || edgesTruncated || budgetHit
	continuation := ""
	if end < len(ids) {
		continuation = strconv.Itoa(end)
	}
	return Subgraph{Nodes: nodes, Edges: edges, Truncated: truncated, Continuation: continuation}, nil
}
