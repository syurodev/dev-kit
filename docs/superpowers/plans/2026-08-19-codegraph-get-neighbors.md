# CodeGraph GetNeighbors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Phase 4's backend half — `GetNeighbors`, from the ["Graph UI (node-link, add-on v1)"](base-doc `content/projects/dev-kit/codegraph.mdx:336-393`) design: a bounded, paginated subgraph query around one node, returning both nodes and edges within a depth limit and a hard size ceiling.

**Scope decision:** this plan is Go-only. The actual Canvas/WebGL renderer — viewport culling, layout in a Worker, level-of-detail/clustering, a fallback tree view for weak machines/accessibility (all explicitly required by `frontend-performance.mdx:370-384`, "Graph UI tương lai") — is a materially larger, higher-risk frontend engineering effort than everything built so far in this sequence, and deserves its own dedicated plan(s) rather than being rushed alongside the backend query. `GetNeighbors` is independently valuable before any canvas exists: an MCP-calling agent can already use `codegraph.getNeighbors` for structured subgraph exploration (e.g. "what's directly connected to this class") the same way `getApiFlow`/`getBlastRadius` are already useful without a UI.

**Architecture:** A depth-bounded BFS from `rootID` over **both** edge directions (this is "what's around this node", not `GetApiFlow`'s directed call-chain walk) — needs a new `Store.GetInEdges` alongside the existing `GetOutEdges`. Depth-bounded exploration always fully computes the neighborhood within `depth` hops (bounded by a capped `depth`, same reasoning `GetBlastRadius` already uses); the node/edge **ceiling** is then applied as a stable, deterministic (sorted-ID) pagination slice over that set — this makes `Continuation` a plain offset string rather than encoded frontier state, avoiding any server-side session/cursor storage while staying resumable and correct across calls (as long as the index doesn't change mid-pagination — an accepted, documented limitation, same class as the design doc's own "layout cache invalidates on re-index" note).

**Tech Stack:** Go only — no new dependencies.

---

## File Structure

Modify:
- `internal/codegraph/codegraph.go` — `NeighborFilter`, `Subgraph` types; extend `Store` interface with `GetInEdges`.
- `internal/codegraph/store.go` — `idx_edges_to` index, `GetInEdges` implementation.
- `internal/codegraph/store_test.go` — tests for `GetInEdges`.
- `internal/codegraph/service.go` — `GetNeighbors`, `nilStore.GetInEdges` stub.
- `internal/codegraph/service_test.go` — tests for `GetNeighbors` (depth, filter, truncation/resumption, cycles).
- `internal/bridge/codegraph.go` — `CodeGraphGetNeighbors`.
- `internal/bridge/codegraph_test.go` — test for it.
- `internal/mcp/server.go` — extend `CodeGraphAPI`.
- `internal/mcp/tools_codegraph.go` — register `codegraph.getNeighbors`.
- `internal/mcp/tools_codegraph_test.go` — extend `fakeCodeGraphAPI`, test for the new tool.

---

## Task 1: `Store.GetInEdges`

**Files:**
- Modify: `internal/codegraph/codegraph.go`
- Modify: `internal/codegraph/store.go`
- Modify: `internal/codegraph/store_test.go`
- Modify: `internal/codegraph/service.go` (`nilStore` stub)

The existing `edges` table only indexes `from_id` (`idx_edges_from`, used by `GetOutEdges`). A reverse "who points at this node" query needs its own index — without one, this becomes a full table scan on every call, unacceptable given `frontend-performance.mdx`'s own stated scale concern ("hàng chục nghìn symbol/call edge").

- [ ] **Step 1: Write the failing test**

Add to `internal/codegraph/store_test.go`:

```go
func TestStoreGetInEdges(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, sampleResult()); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	// sampleResult has one edge: n1 -> n2.
	edges, err := store.GetInEdges(ctx, "n2")
	if err != nil {
		t.Fatalf("GetInEdges: %v", err)
	}
	if len(edges) != 1 || edges[0].From != "n1" {
		t.Fatalf("unexpected edges: %+v", edges)
	}

	none, err := store.GetInEdges(ctx, "n1")
	if err != nil {
		t.Fatalf("GetInEdges: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no in-edges for n1, got: %+v", none)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestStoreGetInEdges -v
```

Expected: FAIL — compile error, `GetInEdges` doesn't exist.

- [ ] **Step 3: Add the index and implement**

In `internal/codegraph/store.go`, extend the `schema` constant — add below `idx_edges_from`:

```go
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_id);
```

Add below `GetOutEdges`:

```go
// GetInEdges returns every edge pointing at nodeID — the reverse of
// GetOutEdges. Used by GetNeighbors, which explores both directions around a
// node (unlike walkFlow's directed-only call-chain walk).
func (s *sqliteStore) GetInEdges(ctx context.Context, nodeID string) ([]Edge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT from_id, to_id, kind FROM edges WHERE to_id = ? ORDER BY from_id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetInEdges: %w", err)
	}
	defer rows.Close()

	var out []Edge
	for rows.Next() {
		var e Edge
		if err := rows.Scan(&e.From, &e.To, &e.Kind); err != nil {
			return nil, fmt.Errorf("codegraph: scan edge: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Add to the `Store` interface**

In `internal/codegraph/codegraph.go`, extend `Store`:

```go
	GetInEdges(ctx context.Context, nodeID string) ([]Edge, error)
```

- [ ] **Step 5: Add the `nilStore` stub**

In `internal/codegraph/service.go`:

```go
func (nilStore) GetInEdges(context.Context, string) ([]Edge, error) { return nil, ErrNoActiveProject }
```

- [ ] **Step 6: Run the tests**

```bash
go test ./internal/codegraph/... -run TestStoreGetInEdges -v
go build ./...
```

Expected: passes; build confirms `sqliteStore`/`nilStore` still satisfy `Store`.

- [ ] **Step 7: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 8: Commit**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/store.go internal/codegraph/store_test.go internal/codegraph/service.go
git commit -m "feat(codegraph): Store.GetInEdges (reverse edge lookup)"
```

---

## Task 2: `NeighborFilter` and `Subgraph` types

**Files:**
- Modify: `internal/codegraph/codegraph.go`

- [ ] **Step 1: Add the types**

In `internal/codegraph/codegraph.go`, add near `ApiFlow`:

```go
// NeighborFilter narrows a GetNeighbors query. Layers, when non-empty,
// restricts results to nodes in one of the listed layers — the doc's own
// "màu theo layer" framing is the only filter dimension asked for in v1;
// add Kind/edge-kind filters later only if a real UI need shows up (YAGNI).
//
// Continuation carries pagination state: empty to start a fresh query from
// rootID, or the exact string a previous truncated Subgraph.Continuation
// returned to resume. It is an opaque decimal offset into a deterministic
// (sorted-by-ID) ordering of the full depth-bounded neighborhood — see
// GetNeighbors's doc comment for why an offset is enough (no server-side
// cursor/session state needed).
type NeighborFilter struct {
	Layers       []string
	Continuation string
}

// Subgraph is one bounded page of a GetNeighbors result.
type Subgraph struct {
	Nodes        []Node
	Edges        []Edge
	Truncated    bool
	Continuation string // pass back as NeighborFilter.Continuation to fetch the next page; empty when Truncated is false
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/codegraph/codegraph.go
git commit -m "feat(codegraph): NeighborFilter and Subgraph types"
```

---

## Task 3: `Service.GetNeighbors`

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/codegraph/service_test.go`:

```go
func neighborsFixture() AnalysisResult {
	// n1(controller) -> n2(service) -> n3(repository); n4(controller) is
	// isolated, unreachable from n1 within any depth.
	return AnalysisResult{
		Nodes: []Node{
			{ID: "n1", Label: "Controller.handle", Kind: "method", Layer: "controller", FilePath: "C.java", Line: 1, EndLine: 5},
			{ID: "n2", Label: "Service.process", Kind: "method", Layer: "service", FilePath: "S.java", Line: 1, EndLine: 5},
			{ID: "n3", Label: "Repository.save", Kind: "method", Layer: "repository", FilePath: "R.java", Line: 1, EndLine: 5},
			{ID: "n4", Label: "Other.handle", Kind: "method", Layer: "controller", FilePath: "O.java", Line: 1, EndLine: 5},
		},
		Edges: []Edge{
			{From: "n1", To: "n2", Kind: "call"},
			{From: "n2", To: "n3", Kind: "call"},
		},
	}
}

func TestServiceGetNeighborsDepthOne(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: neighborsFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sub, err := svc.GetNeighbors(context.Background(), "n2", 1, NeighborFilter{})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	ids := nodeIDs(sub.Nodes)
	// n2 itself, plus n1 (in-edge) and n3 (out-edge) — n4 is unreachable.
	wantIDs := map[string]bool{"n1": true, "n2": true, "n3": true}
	if len(ids) != 3 {
		t.Fatalf("nodes = %v, want exactly n1,n2,n3", ids)
	}
	for _, id := range ids {
		if !wantIDs[id] {
			t.Fatalf("unexpected node %q in result: %v", id, ids)
		}
	}
	if len(sub.Edges) != 2 {
		t.Fatalf("edges = %+v, want 2 (n1->n2, n2->n3)", sub.Edges)
	}
	if sub.Truncated {
		t.Fatal("did not expect truncation for a 3-node result under any reasonable ceiling")
	}
}

func TestServiceGetNeighborsDepthZeroReturnsOnlyRoot(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: neighborsFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sub, err := svc.GetNeighbors(context.Background(), "n2", 0, NeighborFilter{})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	if len(sub.Nodes) != 1 || sub.Nodes[0].ID != "n2" {
		t.Fatalf("nodes = %v, want exactly [n2]", nodeIDs(sub.Nodes))
	}
	if len(sub.Edges) != 0 {
		t.Fatalf("edges = %+v, want none at depth 0", sub.Edges)
	}
}

func TestServiceGetNeighborsFiltersByLayer(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: neighborsFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sub, err := svc.GetNeighbors(context.Background(), "n2", 2, NeighborFilter{Layers: []string{"service"}})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	// Only n2 itself matches "service" — n1 (controller) and n3 (repository)
	// are excluded even though they're within depth 2.
	if len(sub.Nodes) != 1 || sub.Nodes[0].ID != "n2" {
		t.Fatalf("nodes = %v, want exactly [n2]", nodeIDs(sub.Nodes))
	}
}

func TestServiceGetNeighborsTruncatesAndResumes(t *testing.T) {
	// Star graph: root n0 directly connected to 5 others. Force the ceiling
	// down via a tiny artificial limit isn't possible without exporting it,
	// so this test instead drives a large-enough fixture with the real
	// constant and asserts the offset/resume contract, not an exact count —
	// see Step 3's maxNeighborNodes value before finalizing the fixture size.
	nodes := []Node{{ID: "root", Label: "Root", Kind: "class", Layer: "unknown", FilePath: "Root.java", Line: 1, EndLine: 1}}
	var edges []Edge
	const spokes = maxNeighborNodes + 10
	for i := 0; i < spokes; i++ {
		id := fmt.Sprintf("leaf%03d", i)
		nodes = append(nodes, Node{ID: id, Label: id, Kind: "class", Layer: "unknown", FilePath: id + ".java", Line: 1, EndLine: 1})
		edges = append(edges, Edge{From: "root", To: id, Kind: "call"})
	}
	svc := newTestService(t, fakeAnalyzer{result: AnalysisResult{Nodes: nodes, Edges: edges}})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	first, err := svc.GetNeighbors(context.Background(), "root", 1, NeighborFilter{})
	if err != nil {
		t.Fatalf("GetNeighbors (first page): %v", err)
	}
	if !first.Truncated {
		t.Fatalf("expected truncation with %d spokes against maxNeighborNodes=%d", spokes, maxNeighborNodes)
	}
	if len(first.Nodes) != maxNeighborNodes {
		t.Fatalf("first page = %d nodes, want exactly maxNeighborNodes=%d", len(first.Nodes), maxNeighborNodes)
	}
	if first.Continuation == "" {
		t.Fatal("expected a non-empty Continuation when truncated")
	}

	second, err := svc.GetNeighbors(context.Background(), "root", 1, NeighborFilter{Continuation: first.Continuation})
	if err != nil {
		t.Fatalf("GetNeighbors (second page): %v", err)
	}

	firstIDs := nodeIDs(first.Nodes)
	secondIDs := nodeIDs(second.Nodes)
	overlap := map[string]bool{}
	for _, id := range firstIDs {
		overlap[id] = true
	}
	for _, id := range secondIDs {
		if overlap[id] {
			t.Fatalf("node %q appeared in both pages — resumption is not skipping already-returned nodes", id)
		}
	}
	if len(firstIDs)+len(secondIDs) == 0 {
		t.Fatal("both pages empty")
	}
}

func TestServiceGetNeighborsHandlesCycle(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "a", Label: "A", Kind: "class", Layer: "unknown", FilePath: "A.java", Line: 1, EndLine: 1},
			{ID: "b", Label: "B", Kind: "class", Layer: "unknown", FilePath: "B.java", Line: 1, EndLine: 1},
		},
		Edges: []Edge{
			{From: "a", To: "b", Kind: "call"},
			{From: "b", To: "a", Kind: "call"},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	done := make(chan struct{})
	var sub Subgraph
	var err error
	go func() {
		sub, err = svc.GetNeighbors(context.Background(), "a", 5, NeighborFilter{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GetNeighbors did not return — likely an infinite loop on the cycle")
	}
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	if len(sub.Nodes) != 2 {
		t.Fatalf("nodes = %v, want exactly [a, b]", nodeIDs(sub.Nodes))
	}
}

func TestServiceGetNeighborsUnknownRootFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: neighborsFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if _, err := svc.GetNeighbors(context.Background(), "does-not-exist", 1, NeighborFilter{}); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func nodeIDs(nodes []Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}
```

Add `"fmt"` and `"time"` to `internal/codegraph/service_test.go`'s imports if not already present from earlier plans in this sequence.

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestServiceGetNeighbors -v
```

Expected: FAIL — compile error, `GetNeighbors`/`maxNeighborNodes` don't exist yet.

- [ ] **Step 3: Implement**

In `internal/codegraph/service.go`, add:

```go
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
```

Add `"sort"` and `"strconv"` to `internal/codegraph/service.go`'s imports if not already present from earlier plans in this sequence (`"errors"` too, if `ErrNodeNotFound` handling isn't already imported — check first with `grep -n '"errors"' internal/codegraph/service.go`).

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceGetNeighbors -v
```

Expected: all 6 pass.

- [ ] **Step 5: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 6: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): Service.GetNeighbors"
```

---

## Task 4: Bridge method

**Files:**
- Modify: `internal/bridge/codegraph.go`
- Modify: `internal/bridge/codegraph_test.go`

No new capability grant — `codegraph:read`/`project:active`, already granted since Phase 1a.

- [ ] **Step 1: Write the failing test**

Add to `internal/bridge/codegraph_test.go`:

```go
func TestCodeGraphGetNeighbors(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		Nodes: []codegraph.Node{
			{ID: "n1", Label: "A", Kind: "class", Layer: "unknown", FilePath: "A.java", Line: 1, EndLine: 1},
			{ID: "n2", Label: "B", Kind: "class", Layer: "unknown", FilePath: "B.java", Line: 1, EndLine: 1},
		},
		Edges: []codegraph.Edge{{From: "n1", To: "n2", Kind: "call"}},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	if err := s.container.CodeGraph.SetProjectPath(t.TempDir() + "/index.db"); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sub, err := s.CodeGraphGetNeighbors("n1", 1, codegraph.NeighborFilter{})
	if err != nil {
		t.Fatalf("CodeGraphGetNeighbors: %v", err)
	}
	if len(sub.Nodes) != 2 {
		t.Fatalf("unexpected subgraph: %+v", sub)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/bridge/... -run TestCodeGraphGetNeighbors -v
```

Expected: FAIL — compile error.

- [ ] **Step 3: Implement**

In `internal/bridge/codegraph.go`:

```go
func (s *AppService) CodeGraphGetNeighbors(rootID string, depth int, filter codegraph.NeighborFilter) (codegraph.Subgraph, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":  "get_neighbors",
		"root_id": rootID,
		"depth":   depth,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetNeighbors(ctx, rootID, depth, filter)
	})
	if err != nil {
		return codegraph.Subgraph{}, err
	}
	return result.(codegraph.Subgraph), nil
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/bridge/... -run TestCodeGraph -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/bridge/codegraph.go internal/bridge/codegraph_test.go
git commit -m "feat(codegraph): CodeGraphGetNeighbors bridge method"
```

---

## Task 5: MCP tool

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/tools_codegraph.go`
- Modify: `internal/mcp/tools_codegraph_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/mcp/tools_codegraph_test.go` (extend `fakeCodeGraphAPI` with a `subgraph codegraph.Subgraph` field):

```go
func TestToolCodeGraphGetNeighbors(t *testing.T) {
	deps := Deps{
		Caller: &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{subgraph: codegraph.Subgraph{
			Nodes: []codegraph.Node{{ID: "n1", Label: "A"}},
		}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphGetNeighbors(context.Background(), nil, codeGraphGetNeighborsInput{RootID: "n1", Depth: 1})
	if err != nil {
		t.Fatalf("toolCodeGraphGetNeighbors: %v", err)
	}
	sub, ok := v.(codegraph.Subgraph)
	if !ok || len(sub.Nodes) != 1 || sub.Nodes[0].ID != "n1" {
		t.Fatalf("unexpected result: %#v", v)
	}
}
```

```go
func (f fakeCodeGraphAPI) GetNeighbors(ctx context.Context, rootID string, depth int, filter codegraph.NeighborFilter) (codegraph.Subgraph, error) {
	return f.subgraph, f.err
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/mcp/... -run TestToolCodeGraphGetNeighbors -v
```

Expected: FAIL — compile error.

- [ ] **Step 3: Extend the interface and register the tool**

In `internal/mcp/server.go`, add to `CodeGraphAPI`:

```go
	GetNeighbors(ctx context.Context, rootID string, depth int, filter codegraph.NeighborFilter) (codegraph.Subgraph, error)
```

In `internal/mcp/tools_codegraph.go`:

```go
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.getNeighbors",
		Description: "Get a bounded subgraph (nodes + edges) around one symbol, both call directions, paginated",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetNeighbors)
```

```go
type codeGraphGetNeighborsInput struct {
	RootID       string   `json:"rootId"`
	Depth        int      `json:"depth"`
	Layers       []string `json:"layers,omitempty"`
	Continuation string   `json:"continuation,omitempty"`
}

func (s *Server) toolCodeGraphGetNeighbors(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetNeighborsInput) (*sdk.CallToolResult, any, error) {
	filter := codegraph.NeighborFilter{Layers: in.Layers, Continuation: in.Continuation}
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("get_neighbors", map[string]any{"root_id": in.RootID, "depth": in.Depth}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.GetNeighbors(ctx, in.RootID, in.Depth, filter)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/mcp/... -run TestToolCodeGraph -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools_codegraph.go internal/mcp/tools_codegraph_test.go
git commit -m "feat(codegraph): expose getNeighbors via MCP"
```

---

## Task 6: Full regression

**Files:** none (verification only)

- [ ] **Step 1: Standard check**

```bash
go test ./internal/... ./cmd/... -count=1
go build ./...
```

- [ ] **Step 2: Race**

```bash
go test ./internal/... ./cmd/... -race -count=1
```

Expected: everything passes.

---

## What this plan does NOT do

**No Canvas/WebGL renderer, no viewport culling, no layout Worker, no level-of-detail/clustering, no fallback tree view** — the entire "Graph UI tương lai" contract in `frontend-performance.mdx:370-384` is a separate, larger, dedicated frontend plan. This plan only makes `GetNeighbors` real and queryable (via bridge and MCP) so that plan has a working backend to build against, and so an agent can already use `codegraph.getNeighbors` for structured exploration today. **No early-termination BFS** — the full depth-bounded neighborhood is always computed before the ceiling is applied as a pagination slice, same tradeoff `GetBlastRadius` already accepts; a pathologically high-fan-out node at low depth could still be slow to compute even though the *response* stays bounded. Revisit only if a real project's index proves this a real bottleneck, not preemptively. **No `Kind`/edge-kind filter dimensions** on `NeighborFilter` beyond `Layers` — add only if a real UI need shows up.
