# CodeGraph Core Go (Phase 1a: backend without analyzer CLI or frontend) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up `internal/codegraph`'s Go service, SQLite-backed index storage, bridge methods, and MCP tools for the four base CodeGraph operations (open project, list endpoints, get API flow, search symbol) — fully working and tested end-to-end against a fake analyzer, with zero dependency on the real Java analyzer CLI or any frontend UI.

**Architecture:** `internal/codegraph` defines an `Analyzer` interface (`Analyze(ctx, projectPath) (AnalysisResult, error)`) so the entire Go layer — SQLite storage, graph traversal, capability-gated bridge/MCP surface — can be built and tested against a `fakeAnalyzer` test double today. A `subprocessAnalyzer` implementation exists too, but it only proves the wiring: since no Java CLI jar exists yet, it returns a clear `ErrAnalyzerNotConfigured` error rather than actually shelling out. Each project's index lives in its own SQLite file (`modernc.org/sqlite`, same pure-Go driver already used by `internal/storage`) keyed by a hash of the project path, replaced wholesale on every (re-)index — this is derived/cache data, not a vault secret, so it never touches `vaultitem`. `Service.GetApiFlow` walks the call graph in Go (BFS over `Store.GetOutEdges`), not in SQL, so the traversal logic is unit-testable without a database.

**Tech Stack:** Go, `modernc.org/sqlite` (already a dependency), `database/sql`. No new external dependencies. Follows this repo's real bridge pattern (`internal/bridge/ssh.go`) and MCP pattern (`internal/mcp/tools_ssh.go`) exactly — see file citations in each task below.

**Explicit scope boundary (read this before questioning why X isn't here):** This plan ships `internal/codegraph`, its SQLite store, its bridge methods, and its MCP tools — a complete, `go test`-verified backend slice. It does **not** include: the real Java analyzer CLI (a separate Java/Maven project using JavaParser + JavaSymbolSolver — a different toolchain, its own plan), the frontend module (`frontend/src/modules/codegraph/`, React UI — this repo's frontend has no unit test infra per `AGENTS.md`, so it's cleaner as its own plan once there's a real backend to point it at), and every add-on described in the design doc (File editing, Terminal, Graph UI, Data flow/BRD, Incremental re-index, Plan-first flow) — each depends on this phase and is its own follow-up plan. Full design source of truth: `base-doc` repo, `content/projects/dev-kit/codegraph.mdx` (BRD, MCP tool surface, Bridge & capability tables) and `content/projects/dev-kit/frontend-performance.mdx#code-graph` (bounded-API contract this plan already respects: no unbounded full-index responses, summary/detail split).

---

## File Structure

Create:
- `internal/codegraph/codegraph.go` — `Node`, `Edge`, `Endpoint`, `FlowStep`, `ApiFlow`, `SymbolMatch`, `AnalysisResult` types; `Analyzer` and `Store` interfaces; `ErrAnalyzerNotConfigured`, `ErrNoActiveProject`, `ErrNodeNotFound`.
- `internal/codegraph/store.go` — `sqliteStore` (implements `Store`) backed by one SQLite file per project.
- `internal/codegraph/store_test.go` — tests for `sqliteStore`.
- `internal/codegraph/service.go` — `Service` (`NewService`, `OpenProject`, `ListEndpoints`, `GetApiFlow`, `SearchSymbol`) and the BFS flow-walk.
- `internal/codegraph/service_test.go` — tests for `Service` against a `fakeAnalyzer` + real `sqliteStore` on a temp file.
- `internal/codegraph/analyzer_subprocess.go` — `subprocessAnalyzer`, returns `ErrAnalyzerNotConfigured` (real subprocess invocation is a follow-up plan).
- `internal/codegraph/analyzer_subprocess_test.go` — tests for the stub's error behavior.
- `internal/bridge/codegraph.go` — `AppService.CodeGraph*` bridge methods.
- `internal/bridge/codegraph_test.go` — bridge tests, mirroring `internal/bridge/ssh_test.go`'s shape.
- `internal/mcp/tools_codegraph.go` — `registerCodeGraph()` + tool handlers.

Modify:
- `internal/app/container.go:72` (right after the `SSH *sshservice.Service` field) — add `CodeGraph *codegraph.Service` field; `container.go:213` area — construct it; capability grants list (~`container.go:157-163`, next to the `builtin.ssh` grants) — add `builtin.codegraph` grants.
- `internal/app/app.go:15-70` — add a `codegraph` entry to the `builtins` slice (descriptive `ModuleManifest`, mirrors the `database`/`ssh` entries already there).
- `internal/mcp/server.go:75-127` — add a `CodeGraphAPI` interface next to `SSHAPI`/`DatabaseAPI`, add `CodeGraph CodeGraphAPI` to `Deps`, call `s.registerCodeGraph()` in `NewServer`.

---

## Task 1: Core types, errors, and the `Analyzer`/`Store` interfaces

**Files:**
- Create: `internal/codegraph/codegraph.go`
- Test: `internal/codegraph/codegraph_test.go`

No behavior to test yet in this task beyond "the package compiles and the zero-value types behave sanely" — the real behavioral tests land in Tasks 2–4 once there's a `Store`/`Service` to exercise. This task's own test just locks down the one piece of actual logic here: `AnalysisResult.Validate()`, a cheap sanity check the real analyzer's output must pass before `Service.OpenProject` accepts it (an edge referencing a node ID that doesn't exist in the same result is a bug worth catching immediately at the boundary, not three layers later as a confusing SQL foreign-key failure).

- [x] **Step 1: Write the failing test**

```go
// internal/codegraph/codegraph_test.go
package codegraph

import "testing"

func TestAnalysisResultValidateCatchesDanglingEdge(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{{ID: "n1", Label: "A.foo", Kind: "method"}},
		Edges: []Edge{{From: "n1", To: "n2", Kind: "call"}}, // n2 does not exist
	}
	if err := result.Validate(); err == nil {
		t.Fatal("expected error for edge referencing unknown node, got nil")
	}
}

func TestAnalysisResultValidateAcceptsConsistentGraph(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "n1", Label: "A.foo", Kind: "method"},
			{ID: "n2", Label: "B.bar", Kind: "method"},
		},
		Edges: []Edge{{From: "n1", To: "n2", Kind: "call"}},
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/codegraph/... -run TestAnalysisResultValidate -v`
Expected: FAIL — `codegraph.AnalysisResult` (and its `Validate` method) don't exist yet, compile error.

- [x] **Step 3: Write the implementation**

```go
// internal/codegraph/codegraph.go
package codegraph

import (
	"context"
	"errors"
	"fmt"
)

var (
	// ErrAnalyzerNotConfigured is returned by an Analyzer that has no real
	// backing implementation available yet (e.g. the Java CLI jar isn't
	// bundled/found). Service.OpenProject surfaces this unchanged.
	ErrAnalyzerNotConfigured = errors.New("codegraph: analyzer not configured")
	// ErrNoActiveProject is returned by any query method when no project has
	// been opened yet (or the last OpenProject failed).
	ErrNoActiveProject = errors.New("codegraph: no active project")
	// ErrNodeNotFound is returned when a node/endpoint ID does not exist in
	// the active project's index.
	ErrNodeNotFound = errors.New("codegraph: node not found")
)

// Node is a class or method discovered during indexing.
type Node struct {
	ID       string
	Label    string // fully-qualified, e.g. "com.acme.OrderService.create"
	Kind     string // "class" | "method"
	Layer    string // "controller" | "service" | "repository" | "unknown"
	FilePath string
	Line     int
	EndLine  int
}

// Edge is a directed call relationship between two Nodes, by Node.ID.
type Edge struct {
	From string
	To   string
	Kind string // "call" | "di"
}

// Endpoint is one detected HTTP entry point (e.g. a @GetMapping method).
type Endpoint struct {
	ID         string
	HTTPMethod string
	Path       string
	EntryNode  string // Node.ID of the controller method
}

// FlowStep is one hop in an API flow.
type FlowStep struct {
	Label    string // matches the Node.Label it came from
	FilePath string
	Line     int
}

// ApiFlow is the full call chain discovered for one endpoint.
type ApiFlow struct {
	EndpointID string
	Steps      []FlowStep
}

// SymbolMatch is one SearchSymbol result.
type SymbolMatch struct {
	NodeID   string
	Label    string
	Kind     string
	FilePath string
	Line     int
}

// AnalysisResult is everything an Analyzer produces for one project.
type AnalysisResult struct {
	Nodes     []Node
	Edges     []Edge
	Endpoints []Endpoint
}

// Validate checks internal consistency: every Edge and Endpoint must
// reference a Node.ID that is actually present in Nodes. Catches analyzer
// bugs at the boundary instead of as a confusing failure three layers later.
func (r AnalysisResult) Validate() error {
	ids := make(map[string]bool, len(r.Nodes))
	for _, n := range r.Nodes {
		ids[n.ID] = true
	}
	for _, e := range r.Edges {
		if !ids[e.From] {
			return fmt.Errorf("codegraph: edge references unknown node %q", e.From)
		}
		if !ids[e.To] {
			return fmt.Errorf("codegraph: edge references unknown node %q", e.To)
		}
	}
	for _, ep := range r.Endpoints {
		if !ids[ep.EntryNode] {
			return fmt.Errorf("codegraph: endpoint %q references unknown entry node %q", ep.ID, ep.EntryNode)
		}
	}
	return nil
}

// Analyzer builds an AnalysisResult from a project's source on disk. The
// real implementation shells out to a bundled Java CLI (JavaParser +
// JavaSymbolSolver) — see analyzer_subprocess.go; this plan ships a stub
// that returns ErrAnalyzerNotConfigured, plus the fakeAnalyzer test double
// used throughout this package's tests.
type Analyzer interface {
	Analyze(ctx context.Context, projectPath string) (AnalysisResult, error)
}

// Store persists one project's index. Each project gets its own SQLite file;
// ReplaceIndex overwrites it wholesale on every (re-)index.
type Store interface {
	ReplaceIndex(ctx context.Context, result AnalysisResult) error
	HasIndex(ctx context.Context) (bool, error)
	ListEndpoints(ctx context.Context) ([]Endpoint, error)
	GetEndpoint(ctx context.Context, endpointID string) (Endpoint, error)
	GetNode(ctx context.Context, nodeID string) (Node, error)
	GetOutEdges(ctx context.Context, nodeID string) ([]Edge, error)
	SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error)
	Close() error
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/codegraph/... -run TestAnalysisResultValidate -v`
Expected: PASS (both tests).

- [x] **Step 5: Commit**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/codegraph_test.go
git commit -m "feat(codegraph): add core types, errors, and Analyzer/Store interfaces"
```

---

## Task 2: SQLite-backed `Store`

**Files:**
- Create: `internal/codegraph/store.go`
- Test: `internal/codegraph/store_test.go`

- [x] **Step 1: Write the failing test**

```go
// internal/codegraph/store_test.go
package codegraph

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *sqliteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	store, err := newSQLiteStore(path)
	if err != nil {
		t.Fatalf("newSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func sampleResult() AnalysisResult {
	return AnalysisResult{
		Nodes: []Node{
			{ID: "n1", Label: "OrderController.create", Kind: "method", Layer: "controller", FilePath: "Order.java", Line: 10, EndLine: 15},
			{ID: "n2", Label: "OrderService.process", Kind: "method", Layer: "service", FilePath: "OrderService.java", Line: 20, EndLine: 30},
		},
		Edges: []Edge{{From: "n1", To: "n2", Kind: "call"}},
		Endpoints: []Endpoint{
			{ID: "ep1", HTTPMethod: "POST", Path: "/orders", EntryNode: "n1"},
		},
	}
}

func TestStoreReplaceIndexThenListEndpoints(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if has, err := store.HasIndex(ctx); err != nil || has {
		t.Fatalf("expected HasIndex false before any index, got has=%v err=%v", has, err)
	}

	if err := store.ReplaceIndex(ctx, sampleResult()); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	if has, err := store.HasIndex(ctx); err != nil || !has {
		t.Fatalf("expected HasIndex true after ReplaceIndex, got has=%v err=%v", has, err)
	}

	endpoints, err := store.ListEndpoints(ctx)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Path != "/orders" {
		t.Fatalf("unexpected endpoints: %+v", endpoints)
	}
}

func TestStoreReplaceIndexIsWholesale(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.ReplaceIndex(ctx, sampleResult()); err != nil {
		t.Fatalf("first ReplaceIndex: %v", err)
	}
	// Second index has none of the first's endpoints — replace, not merge.
	second := AnalysisResult{
		Nodes:     []Node{{ID: "n9", Label: "Health.check", Kind: "method", Layer: "controller"}},
		Endpoints: []Endpoint{{ID: "ep9", HTTPMethod: "GET", Path: "/health", EntryNode: "n9"}},
	}
	if err := store.ReplaceIndex(ctx, second); err != nil {
		t.Fatalf("second ReplaceIndex: %v", err)
	}

	endpoints, err := store.ListEndpoints(ctx)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Path != "/health" {
		t.Fatalf("expected only the second index's endpoint to remain, got: %+v", endpoints)
	}
}

func TestStoreGetNodeAndOutEdges(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, sampleResult()); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	node, err := store.GetNode(ctx, "n1")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.Label != "OrderController.create" {
		t.Fatalf("unexpected node: %+v", node)
	}

	edges, err := store.GetOutEdges(ctx, "n1")
	if err != nil {
		t.Fatalf("GetOutEdges: %v", err)
	}
	if len(edges) != 1 || edges[0].To != "n2" {
		t.Fatalf("unexpected edges: %+v", edges)
	}

	if _, err := store.GetNode(ctx, "does-not-exist"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestStoreSearchSymbol(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, sampleResult()); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	matches, err := store.SearchSymbol(ctx, "OrderService")
	if err != nil {
		t.Fatalf("SearchSymbol: %v", err)
	}
	if len(matches) != 1 || matches[0].Label != "OrderService.process" {
		t.Fatalf("unexpected matches: %+v", matches)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/codegraph/... -run TestStore -v`
Expected: FAIL — `newSQLiteStore`/`sqliteStore` don't exist yet, compile error.

- [x] **Step 3: Write the implementation**

```go
// internal/codegraph/store.go
package codegraph

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS nodes (
	id TEXT PRIMARY KEY,
	label TEXT NOT NULL,
	kind TEXT NOT NULL,
	layer TEXT NOT NULL,
	file_path TEXT NOT NULL,
	line INTEGER NOT NULL,
	end_line INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS edges (
	from_id TEXT NOT NULL,
	to_id TEXT NOT NULL,
	kind TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
CREATE TABLE IF NOT EXISTS endpoints (
	id TEXT PRIMARY KEY,
	http_method TEXT NOT NULL,
	path TEXT NOT NULL,
	entry_node TEXT NOT NULL
);
`

// sqliteStore is a Store backed by one SQLite file per project. It is not
// safe for concurrent use by multiple goroutines without external locking —
// Service (Task 3) serializes access with a mutex.
type sqliteStore struct {
	db *sql.DB
}

func newSQLiteStore(path string) (*sqliteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("codegraph: open sqlite store: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("codegraph: create schema: %w", err)
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }

func (s *sqliteStore) ReplaceIndex(ctx context.Context, result AnalysisResult) error {
	if err := result.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("codegraph: begin replace: %w", err)
	}
	defer tx.Rollback()

	for _, table := range []string{"nodes", "edges", "endpoints"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("codegraph: clear %s: %w", table, err)
		}
	}
	for _, n := range result.Nodes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO nodes (id, label, kind, layer, file_path, line, end_line) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.Label, n.Kind, n.Layer, n.FilePath, n.Line, n.EndLine); err != nil {
			return fmt.Errorf("codegraph: insert node %q: %w", n.ID, err)
		}
	}
	for _, e := range result.Edges {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO edges (from_id, to_id, kind) VALUES (?, ?, ?)`,
			e.From, e.To, e.Kind); err != nil {
			return fmt.Errorf("codegraph: insert edge %q->%q: %w", e.From, e.To, err)
		}
	}
	for _, ep := range result.Endpoints {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO endpoints (id, http_method, path, entry_node) VALUES (?, ?, ?, ?)`,
			ep.ID, ep.HTTPMethod, ep.Path, ep.EntryNode); err != nil {
			return fmt.Errorf("codegraph: insert endpoint %q: %w", ep.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("codegraph: commit replace: %w", err)
	}
	return nil
}

func (s *sqliteStore) HasIndex(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("codegraph: HasIndex: %w", err)
	}
	return count > 0, nil
}

func (s *sqliteStore) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, http_method, path, entry_node FROM endpoints ORDER BY path`)
	if err != nil {
		return nil, fmt.Errorf("codegraph: ListEndpoints: %w", err)
	}
	defer rows.Close()

	var out []Endpoint
	for rows.Next() {
		var ep Endpoint
		if err := rows.Scan(&ep.ID, &ep.HTTPMethod, &ep.Path, &ep.EntryNode); err != nil {
			return nil, fmt.Errorf("codegraph: scan endpoint: %w", err)
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetEndpoint(ctx context.Context, endpointID string) (Endpoint, error) {
	var ep Endpoint
	err := s.db.QueryRowContext(ctx,
		`SELECT id, http_method, path, entry_node FROM endpoints WHERE id = ?`, endpointID,
	).Scan(&ep.ID, &ep.HTTPMethod, &ep.Path, &ep.EntryNode)
	if err == sql.ErrNoRows {
		return Endpoint{}, ErrNodeNotFound
	}
	if err != nil {
		return Endpoint{}, fmt.Errorf("codegraph: GetEndpoint: %w", err)
	}
	return ep, nil
}

func (s *sqliteStore) GetNode(ctx context.Context, nodeID string) (Node, error) {
	var n Node
	err := s.db.QueryRowContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line FROM nodes WHERE id = ?`, nodeID,
	).Scan(&n.ID, &n.Label, &n.Kind, &n.Layer, &n.FilePath, &n.Line, &n.EndLine)
	if err == sql.ErrNoRows {
		return Node{}, ErrNodeNotFound
	}
	if err != nil {
		return Node{}, fmt.Errorf("codegraph: GetNode: %w", err)
	}
	return n, nil
}

func (s *sqliteStore) GetOutEdges(ctx context.Context, nodeID string) ([]Edge, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT from_id, to_id, kind FROM edges WHERE from_id = ?`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetOutEdges: %w", err)
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

func (s *sqliteStore) SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, label, kind, file_path, line FROM nodes WHERE label LIKE ? ORDER BY label`,
		"%"+query+"%")
	if err != nil {
		return nil, fmt.Errorf("codegraph: SearchSymbol: %w", err)
	}
	defer rows.Close()

	var out []SymbolMatch
	for rows.Next() {
		var m SymbolMatch
		if err := rows.Scan(&m.NodeID, &m.Label, &m.Kind, &m.FilePath, &m.Line); err != nil {
			return nil, fmt.Errorf("codegraph: scan symbol match: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/codegraph/... -run TestStore -v`
Expected: PASS (all 4 tests).

- [x] **Step 5: Commit**

```bash
git add internal/codegraph/store.go internal/codegraph/store_test.go
git commit -m "feat(codegraph): add SQLite-backed Store, one file per project"
```

---

## Task 3: `Service` — `OpenProject`, `ListEndpoints`, `GetApiFlow` (BFS flow walk), `SearchSymbol`

**Files:**
- Create: `internal/codegraph/service.go`
- Test: `internal/codegraph/service_test.go`

- [x] **Step 1: Write the failing test**

```go
// internal/codegraph/service_test.go
package codegraph

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// fakeAnalyzer is the test double every other test in this package (and the
// bridge/MCP tests in later tasks) uses instead of a real Java subprocess.
type fakeAnalyzer struct {
	result AnalysisResult
	err    error
}

func (f fakeAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	return f.result, f.err
}

func newTestService(t *testing.T, analyzer Analyzer) *Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "index.db")
	store, err := newSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("newSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewService(store, analyzer)
}

func TestServiceQueriesFailBeforeOpenProject(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{})
	ctx := context.Background()

	if _, err := svc.ListEndpoints(ctx); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}

func TestServiceOpenProjectThenListEndpoints(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	ctx := context.Background()

	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	endpoints, err := svc.ListEndpoints(ctx)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Path != "/orders" {
		t.Fatalf("unexpected endpoints: %+v", endpoints)
	}
}

func TestServiceOpenProjectSurfacesAnalyzerError(t *testing.T) {
	analyzerErr := errors.New("boom")
	svc := newTestService(t, fakeAnalyzer{err: analyzerErr})

	err := svc.OpenProject(context.Background(), "/fake/project")
	if !errors.Is(err, analyzerErr) {
		t.Fatalf("expected wrapped analyzer error, got %v", err)
	}
	// A failed OpenProject must not leave a stale/partial index queryable.
	if _, err := svc.ListEndpoints(context.Background()); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject after failed OpenProject, got %v", err)
	}
}

func TestServiceGetApiFlowWalksCallChain(t *testing.T) {
	// n1 (controller) --call--> n2 (service) --call--> n3 (repository)
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "n1", Label: "OrderController.create", Kind: "method", Layer: "controller", FilePath: "OrderController.java", Line: 10},
			{ID: "n2", Label: "OrderService.process", Kind: "method", Layer: "service", FilePath: "OrderService.java", Line: 20},
			{ID: "n3", Label: "OrderRepository.save", Kind: "method", Layer: "repository", FilePath: "OrderRepository.java", Line: 30},
		},
		Edges: []Edge{
			{From: "n1", To: "n2", Kind: "call"},
			{From: "n2", To: "n3", Kind: "call"},
		},
		Endpoints: []Endpoint{{ID: "ep1", HTTPMethod: "POST", Path: "/orders", EntryNode: "n1"}},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	ctx := context.Background()
	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	flow, err := svc.GetApiFlow(ctx, "ep1")
	if err != nil {
		t.Fatalf("GetApiFlow: %v", err)
	}
	if len(flow.Steps) != 3 {
		t.Fatalf("expected 3 steps (controller, service, repository), got %d: %+v", len(flow.Steps), flow.Steps)
	}
	wantLabels := []string{"OrderController.create", "OrderService.process", "OrderRepository.save"}
	for i, want := range wantLabels {
		if flow.Steps[i].Label != want {
			t.Fatalf("step %d: got %q, want %q (full flow: %+v)", i, flow.Steps[i].Label, want, flow.Steps)
		}
	}
}

func TestServiceGetApiFlowHandlesCycleWithoutHanging(t *testing.T) {
	// n1 -> n2 -> n1 (a real call cycle — mutual recursion happens in Java).
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "n1", Label: "A.foo", Kind: "method", Layer: "unknown"},
			{ID: "n2", Label: "B.bar", Kind: "method", Layer: "unknown"},
		},
		Edges: []Edge{
			{From: "n1", To: "n2", Kind: "call"},
			{From: "n2", To: "n1", Kind: "call"},
		},
		Endpoints: []Endpoint{{ID: "ep1", HTTPMethod: "GET", Path: "/x", EntryNode: "n1"}},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	ctx := context.Background()
	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	flow, err := svc.GetApiFlow(ctx, "ep1")
	if err != nil {
		t.Fatalf("GetApiFlow: %v", err)
	}
	if len(flow.Steps) != 2 {
		t.Fatalf("expected exactly 2 steps (cycle must not revisit), got %d: %+v", len(flow.Steps), flow.Steps)
	}
}

func TestServiceSearchSymbol(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	ctx := context.Background()
	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	matches, err := svc.SearchSymbol(ctx, "OrderService")
	if err != nil {
		t.Fatalf("SearchSymbol: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("unexpected matches: %+v", matches)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/codegraph/... -run TestService -v`
Expected: FAIL — `Service`/`NewService` don't exist yet, compile error.

- [x] **Step 3: Write the implementation**

```go
// internal/codegraph/service.go
package codegraph

import (
	"context"
	"fmt"
	"sync"
)

// maxFlowDepth caps the BFS walk in GetApiFlow so a pathological call graph
// (very deep chain, or a cycle missed by the visited-set for some reason)
// can never turn into an unbounded response — same "bounded, no full-graph
// dump" contract the design doc requires everywhere else in this module.
const maxFlowDepth = 32

// Service is the CodeGraph domain service. It owns exactly one active
// project's index at a time (FR-1: no multi-project registry in v1).
type Service struct {
	mu       sync.RWMutex
	store    Store
	analyzer Analyzer
	active   bool
}

func NewService(store Store, analyzer Analyzer) *Service {
	return &Service{store: store, analyzer: analyzer}
}

// OpenProject runs the analyzer over projectPath and replaces the active
// index. A failed analyze leaves the service with no active project rather
// than a stale or partial one.
func (s *Service) OpenProject(ctx context.Context, projectPath string) error {
	result, err := s.analyzer.Analyze(ctx, projectPath)
	if err != nil {
		return fmt.Errorf("codegraph: analyze %q: %w", projectPath, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.store.ReplaceIndex(ctx, result); err != nil {
		s.active = false
		return fmt.Errorf("codegraph: replace index: %w", err)
	}
	s.active = true
	return nil
}

func (s *Service) requireActive() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.active {
		return ErrNoActiveProject
	}
	return nil
}

func (s *Service) ListEndpoints(ctx context.Context) ([]Endpoint, error) {
	if err := s.requireActive(); err != nil {
		return nil, err
	}
	return s.store.ListEndpoints(ctx)
}

func (s *Service) SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error) {
	if err := s.requireActive(); err != nil {
		return nil, err
	}
	return s.store.SearchSymbol(ctx, query)
}

// GetApiFlow walks the call graph from the endpoint's entry node, in Go
// (not SQL), so the traversal — including cycle protection — is testable
// without a database. Cycles are broken by the visited-set; maxFlowDepth is
// a second, independent safety bound.
func (s *Service) GetApiFlow(ctx context.Context, endpointID string) (ApiFlow, error) {
	if err := s.requireActive(); err != nil {
		return ApiFlow{}, err
	}
	ep, err := s.store.GetEndpoint(ctx, endpointID)
	if err != nil {
		return ApiFlow{}, err
	}
	steps, err := s.walkFlow(ctx, ep.EntryNode)
	if err != nil {
		return ApiFlow{}, err
	}
	return ApiFlow{EndpointID: endpointID, Steps: steps}, nil
}

func (s *Service) walkFlow(ctx context.Context, rootNodeID string) ([]FlowStep, error) {
	var steps []FlowStep
	visited := make(map[string]bool)

	var walk func(nodeID string, depth int) error
	walk = func(nodeID string, depth int) error {
		if visited[nodeID] || depth > maxFlowDepth {
			return nil
		}
		visited[nodeID] = true

		node, err := s.store.GetNode(ctx, nodeID)
		if err != nil {
			return err
		}
		steps = append(steps, FlowStep{Label: node.Label, FilePath: node.FilePath, Line: node.Line})

		edges, err := s.store.GetOutEdges(ctx, nodeID)
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
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/codegraph/... -run TestService -v`
Expected: PASS (all 6 tests).

- [x] **Step 5: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): add Service with BFS flow walk, bounded and cycle-safe"
```

---

## Task 4: `subprocessAnalyzer` stub

**Files:**
- Create: `internal/codegraph/analyzer_subprocess.go`
- Test: `internal/codegraph/analyzer_subprocess_test.go`

This is intentionally a stub — it proves `Service.OpenProject` has a real, wireable production `Analyzer` implementation today, and satisfies the design doc's acceptance criterion ("Tắt `java` khỏi PATH → mở project trả lỗi rõ ràng") even before the real Java CLI exists, without pretending to invoke a jar that isn't built yet.

- [x] **Step 1: Write the failing test**

```go
// internal/codegraph/analyzer_subprocess_test.go
package codegraph

import (
	"context"
	"errors"
	"testing"
)

func TestSubprocessAnalyzerReturnsNotConfigured(t *testing.T) {
	analyzer := NewSubprocessAnalyzer()
	_, err := analyzer.Analyze(context.Background(), "/some/project")
	if !errors.Is(err, ErrAnalyzerNotConfigured) {
		t.Fatalf("expected ErrAnalyzerNotConfigured, got %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/codegraph/... -run TestSubprocessAnalyzer -v`
Expected: FAIL — `NewSubprocessAnalyzer` doesn't exist yet, compile error.

- [x] **Step 3: Write the implementation**

```go
// internal/codegraph/analyzer_subprocess.go
package codegraph

import "context"

// subprocessAnalyzer will shell out to the bundled Java analyzer CLI
// (JavaParser + JavaSymbolSolver) once that jar exists — a separate,
// follow-up plan (different toolchain: Java/Maven, not Go). Until then it
// always returns ErrAnalyzerNotConfigured, so OpenProject fails clearly
// instead of silently no-op'ing or panicking on a missing binary.
type subprocessAnalyzer struct{}

func NewSubprocessAnalyzer() Analyzer {
	return subprocessAnalyzer{}
}

func (subprocessAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	return AnalysisResult{}, ErrAnalyzerNotConfigured
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/codegraph/... -run TestSubprocessAnalyzer -v`
Expected: PASS.

- [x] **Step 5: Run the full package test suite**

Run: `go test ./internal/codegraph/... -v`
Expected: PASS — every test from Tasks 1–4 (13 tests total).

- [x] **Step 6: Commit**

```bash
git add internal/codegraph/analyzer_subprocess.go internal/codegraph/analyzer_subprocess_test.go
git commit -m "feat(codegraph): add subprocessAnalyzer stub (real Java CLI is a follow-up plan)"
```

---

## Task 5: Module registration and capability grants

**Files:**
- Modify: `internal/app/app.go:15-70` (the `builtins` slice in `RegisterBuiltins`)
- Modify: `internal/app/container.go:72` (Container struct field), `internal/app/container.go` grants list (next to the `builtin.ssh` block, currently around line 157-163), and wherever `SSH: sshservice.NewService(...)` is constructed (currently `container.go:213`)

- [x] **Step 1: Add the descriptive `ModuleManifest` entry**

In `internal/app/app.go`, inside the `builtins := []modules.ModuleManifest{...}` slice (right after the `kafka` entry, before `agenthub`, to keep connection-profile-style modules grouped — order doesn't matter functionally, this is just for readability):

```go
		{
			ID:           "codegraph",
			Title:        "Code Graph",
			Version:      "1.0.0",
			APIVersion:   modules.CurrentAPIVersion,
			Commands:     []string{"codegraph.openProject"},
			Views:        []string{"codegraph.explorer"},
			Capabilities: []string{"codegraph:index", "codegraph:read"},
		},
```

- [x] **Step 2: Add the `Container` field**

In `internal/app/container.go`, add the import and the field right after `SSH *sshservice.Service` (line 72):

```go
	"devkit/internal/codegraph"
```

(add to the import block, alphabetically among the existing `devkit/internal/...` imports)

```go
	SSH          *sshservice.Service
	CodeGraph    *codegraph.Service
```

- [x] **Step 3: Add the capability grants**

In the grants list, right after the three `builtin.ssh` / before the `builtin.kafka` block (matches the file's existing ordering):

```go
		{SubjectID: "builtin.codegraph", Capability: "codegraph:index", Scope: "project:new"},
		{SubjectID: "builtin.codegraph", Capability: "codegraph:read", Scope: "project:active"},
```

- [x] **Step 4: Give `Service` a swappable store and a safe placeholder**

The `Store` from Task 2 is one SQLite file per project, so it cannot be constructed at container-startup time — no project path is known yet. `Service` needs a way to swap in a real store once `OpenProject` learns a path, and a safe placeholder to hold before that happens (never a bare `nil`, which would panic the first time any method touches it). Go back to `internal/codegraph/service.go` (Task 3) and add both:

```go
// SetProjectPath points the service at a new project's on-disk index file,
// opening (creating if needed) a fresh sqliteStore for it and discarding the
// previous one. Call this once per OpenProject, before running the analyzer,
// so a fresh analyze always starts from a clean per-project store.
func (s *Service) SetProjectPath(dbPath string) error {
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
```

And change `NewService` to accept a nil-safe initial store:

```go
func NewService(store Store, analyzer Analyzer) *Service {
	return &Service{store: store, analyzer: analyzer}
}
```

(unchanged signature — `container.go` now passes an explicit no-op placeholder store instead of `nil`, see next step)

Add a tiny no-op store so the container never holds a truly nil `Store` (every method call before the first real `SetProjectPath` should return `ErrNoActiveProject`, not panic on a nil pointer):

```go
// nilStore is the container's placeholder Store before any project has been
// opened. Every method is unreachable in practice because Service checks
// s.active first — but a safe zero-value beats a nil-pointer panic if that
// invariant is ever broken by a future change.
type nilStore struct{}

func (nilStore) ReplaceIndex(context.Context, AnalysisResult) error         { return ErrNoActiveProject }
func (nilStore) HasIndex(context.Context) (bool, error)                    { return false, nil }
func (nilStore) ListEndpoints(context.Context) ([]Endpoint, error)         { return nil, ErrNoActiveProject }
func (nilStore) GetEndpoint(context.Context, string) (Endpoint, error)     { return Endpoint{}, ErrNoActiveProject }
func (nilStore) GetNode(context.Context, string) (Node, error)             { return Node{}, ErrNoActiveProject }
func (nilStore) GetOutEdges(context.Context, string) ([]Edge, error)       { return nil, ErrNoActiveProject }
func (nilStore) SearchSymbol(context.Context, string) ([]SymbolMatch, error) { return nil, ErrNoActiveProject }
func (nilStore) Close() error                                              { return nil }

// NewNilStore returns a placeholder Store that always reports no active
// project. internal/app/container.go uses this to construct Service before
// any CodeGraph project has been opened, since container.go is a different
// package and cannot construct the unexported nilStore type directly.
func NewNilStore() Store { return nilStore{} }
```

Add a test for `SetProjectPath` and `nilStore` in `internal/codegraph/service_test.go`:

```go
func TestNewServiceWithNilStorePlaceholderIsSafe(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	if _, err := svc.ListEndpoints(context.Background()); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject from nilStore-backed service, got %v", err)
	}
}

func TestServiceSetProjectPathSwapsStore(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{result: sampleResult()})
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := svc.SetProjectPath(dbPath); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	ctx := context.Background()
	if err := svc.OpenProject(ctx, "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}
	if _, err := svc.ListEndpoints(ctx); err != nil {
		t.Fatalf("ListEndpoints after SetProjectPath+OpenProject: %v", err)
	}
}
```

Run: `go test ./internal/codegraph/... -v` — expect PASS on all tests including the two new ones.

- [x] **Step 5: Construct the service in the container**

Find where `SSH: sshservice.NewService(itemStore, envService),` is constructed (`container.go:213`) and add, in the same struct literal:

```go
		CodeGraph: codegraph.NewService(codegraph.NewNilStore(), codegraph.NewSubprocessAnalyzer()),
```

- [x] **Step 6: Update the existing module-count test**

`internal/bridge/app_service_test.go`'s `TestAppServiceListsModules` asserts an exact module count and an exact capability set per module — adding the `codegraph` manifest entry (Step 1) makes it a 9th module, so this existing test needs three edits or it will fail:

```go
	if len(moduleList) != 9 {
		t.Fatalf("len(modules) = %d, want 9", len(moduleList))
	}
	wantCapabilities := map[string][]string{
		"tools":     {"tools:hash"},
		"notes":     {"notes:read", "notes:write"},
		"database":  {"db:query"},
		"ssh":       {"ssh:connect", "ssh:forward"},
		"kafka":     {"kafka:connect", "kafka:produce"},
		"sync":      {"sync:manage"},
		"settings":  {"env:read", "env:write", "preferences:read", "preferences:write"},
		"agenthub":  {"agenthub:read", "agenthub:execute"},
		"codegraph": {"codegraph:index", "codegraph:read"},
	}
```

And the final assertion line:

```go
	if !found["tools"] || !found["notes"] || !found["database"] || !found["ssh"] || !found["kafka"] || !found["sync"] || !found["settings"] || !found["agenthub"] || !found["codegraph"] {
		t.Fatalf("modules = %+v, want tools, notes, database, ssh, kafka, sync, settings, agenthub and codegraph", moduleList)
	}
```

Run: `go test ./internal/bridge/... -run TestAppServiceListsModules -v`
Expected: PASS.

- [x] **Step 7: Verify the container still builds**

Run: `go build ./internal/app/...`
Expected: builds cleanly.

- [x] **Step 8: Verify existing app/bridge tests still pass**

Run: `go test ./internal/app/... ./internal/bridge/... -v`
Expected: PASS — including the updated `TestAppServiceListsModules` from Step 6.

- [x] **Step 9: Commit**

```bash
git add internal/app/app.go internal/app/container.go internal/bridge/app_service_test.go internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): wire module manifest, capability grants, and container"
```

---

## Task 6: Bridge methods

**Files:**
- Create: `internal/bridge/codegraph.go`
- Test: `internal/bridge/codegraph_test.go`

Mirrors `internal/bridge/ssh.go`'s exact shape (`s.call(ctx, subject, capability, scope, meta, fn)`).

- [x] **Step 1: Write the failing test**

This package's real `AppService` test constructor is `newTestService(t)` in `internal/bridge/app_service_test.go` (builds a real `app.Container` via `app.NewContainer()` + `app.RegisterBuiltins`, then `NewAppService(container)`). `AppService.container` is an unexported field (`internal/bridge/app_service.go:13`), reachable directly from `codegraph_test.go` since it's the same `bridge` package. Write:

```go
// internal/bridge/codegraph_test.go
package bridge

import (
	"context"
	"errors"
	"testing"

	"devkit/internal/codegraph"
)

// fakeCodeGraphAnalyzer lets these tests drive Service without a real Java
// CLI, same as internal/codegraph's own tests.
type fakeCodeGraphAnalyzer struct {
	result codegraph.AnalysisResult
	err    error
}

func (f fakeCodeGraphAnalyzer) Analyze(ctx context.Context, path string) (codegraph.AnalysisResult, error) {
	return f.result, f.err
}

func TestCodeGraphOpenProjectThenListEndpoints(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		Nodes:     []codegraph.Node{{ID: "n1", Label: "A.foo", Kind: "method", Layer: "controller"}},
		Endpoints: []codegraph.Endpoint{{ID: "ep1", HTTPMethod: "GET", Path: "/a", EntryNode: "n1"}},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	if err := s.container.CodeGraph.SetProjectPath(t.TempDir() + "/index.db"); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}

	if err := s.CodeGraphOpenProject("/fake/project"); err != nil {
		t.Fatalf("CodeGraphOpenProject: %v", err)
	}

	endpoints, err := s.CodeGraphListEndpoints()
	if err != nil {
		t.Fatalf("CodeGraphListEndpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Path != "/a" {
		t.Fatalf("unexpected endpoints: %+v", endpoints)
	}
}

func TestCodeGraphListEndpointsBeforeOpenProjectFails(t *testing.T) {
	s := newTestService(t)
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{})

	if _, err := s.CodeGraphListEndpoints(); !errors.Is(err, codegraph.ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bridge/... -run TestCodeGraph -v`
Expected: FAIL — `AppService.CodeGraphOpenProject`/`CodeGraphListEndpoints` don't exist yet.

- [x] **Step 3: Write the implementation**

```go
// internal/bridge/codegraph.go
package bridge

import (
	"context"

	"devkit/internal/codegraph"
)

func (s *AppService) CodeGraphOpenProject(path string) error {
	_, err := s.call(context.Background(), "builtin.codegraph", "codegraph:index", "project:new", map[string]any{
		"action": "open_project",
	}, func(ctx context.Context) (any, error) {
		return nil, s.container.CodeGraph.OpenProject(ctx, path)
	})
	return err
}

func (s *AppService) CodeGraphListEndpoints() ([]codegraph.Endpoint, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "list_endpoints",
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.ListEndpoints(ctx)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.Endpoint), nil
}

func (s *AppService) CodeGraphGetApiFlow(endpointID string) (codegraph.ApiFlow, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":      "get_api_flow",
		"endpoint_id": endpointID,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetApiFlow(ctx, endpointID)
	})
	if err != nil {
		return codegraph.ApiFlow{}, err
	}
	return result.(codegraph.ApiFlow), nil
}

func (s *AppService) CodeGraphSearchSymbol(query string) ([]codegraph.SymbolMatch, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action": "search_symbol",
		"query":  query,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.SearchSymbol(ctx, query)
	})
	if err != nil {
		return nil, err
	}
	return result.([]codegraph.SymbolMatch), nil
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bridge/... -run TestCodeGraph -v`
Expected: PASS.

- [x] **Step 5: Run the full bridge test suite to check for regressions**

Run: `go test ./internal/bridge/... -v`
Expected: PASS — every pre-existing bridge test still passes; the two new ones join them.

- [x] **Step 6: Commit**

```bash
git add internal/bridge/codegraph.go internal/bridge/codegraph_test.go
git commit -m "feat(codegraph): add bridge methods (OpenProject, ListEndpoints, GetApiFlow, SearchSymbol)"
```

---

## Task 7: MCP tools

**Files:**
- Create: `internal/mcp/tools_codegraph.go`
- Modify: `internal/mcp/server.go:75-140` (add `CodeGraphAPI` interface, add field to `Deps`, call `registerCodeGraph()`)

- [x] **Step 1: Add the `CodeGraphAPI` interface and wire `Deps`**

In `internal/mcp/server.go`, add next to `SSHAPI`/`DatabaseAPI` (around line 75-90):

```go
// CodeGraphAPI is the codegraph domain surface MCP tools invoke.
type CodeGraphAPI interface {
	ListEndpoints() ([]codegraph.Endpoint, error)
	GetApiFlow(endpointID string) (codegraph.ApiFlow, error)
	SearchSymbol(query string) ([]codegraph.SymbolMatch, error)
}
```

(add `"devkit/internal/codegraph"` to this file's import block)

In the `Deps` struct (line 115-127), add:

```go
	CodeGraph CodeGraphAPI
```

In `NewServer` (line 136+), add a call alongside the existing `s.registerVault()` etc.:

```go
	s.registerCodeGraph()
```

Note `OpenProject` is deliberately **not** part of `CodeGraphAPI`/exposed as an MCP tool in this plan — the design doc's MCP tool surface does list `codegraph.openProject`, but this repo's actual `AppService` bridge signature takes only a `path string` with no per-call human confirmation gate, and opening an arbitrary filesystem path is exactly the kind of action later phases (once File editing's path-validation work is real) should gate more carefully. Exposing the three pure-read tools now and deferring `openProject`'s MCP exposure to whichever follow-up plan adds real path safety is a smaller, safer slice — flag this explicitly rather than wiring it silently.

- [x] **Step 2: Write the failing test**

This package's real `Caller` test double is `fakeCaller` (`internal/mcp/server_test.go:24`) — a pointer-receiver type whose `Call` method just runs the handler and records the call. Write:

```go
// internal/mcp/tools_codegraph_test.go
package mcp

import (
	"context"
	"testing"

	"devkit/internal/codegraph"
)

type fakeCodeGraphAPI struct {
	endpoints []codegraph.Endpoint
	flow      codegraph.ApiFlow
	matches   []codegraph.SymbolMatch
	err       error
}

func (f fakeCodeGraphAPI) ListEndpoints() ([]codegraph.Endpoint, error)          { return f.endpoints, f.err }
func (f fakeCodeGraphAPI) GetApiFlow(id string) (codegraph.ApiFlow, error)       { return f.flow, f.err }
func (f fakeCodeGraphAPI) SearchSymbol(q string) ([]codegraph.SymbolMatch, error) { return f.matches, f.err }

func TestToolCodeGraphListEndpoints(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{endpoints: []codegraph.Endpoint{{ID: "ep1", Path: "/orders"}}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphListEndpoints(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("toolCodeGraphListEndpoints: %v", err)
	}
	endpoints, ok := v.([]codegraph.Endpoint)
	if !ok || len(endpoints) != 1 || endpoints[0].Path != "/orders" {
		t.Fatalf("unexpected result: %#v", v)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/mcp/... -run TestToolCodeGraph -v`
Expected: FAIL — `toolCodeGraphListEndpoints` doesn't exist yet.

- [x] **Step 4: Write the implementation**

```go
// internal/mcp/tools_codegraph.go
package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerCodeGraph() {
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.listEndpoints",
		Description: "List HTTP endpoints detected in the active CodeGraph project",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphListEndpoints)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.getApiFlow",
		Description: "Get the Controller -> Service -> Repository call chain for one endpoint",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetApiFlow)

	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.searchSymbol",
		Description: "Search classes/methods by name in the active CodeGraph project",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphSearchSymbol)
}

func (s *Server) toolCodeGraphListEndpoints(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("list_endpoints", nil), func(context.Context) (any, error) {
		return s.deps.CodeGraph.ListEndpoints()
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type codeGraphGetApiFlowInput struct {
	EndpointID string `json:"endpointId"`
}

func (s *Server) toolCodeGraphGetApiFlow(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetApiFlowInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("get_api_flow", map[string]any{"endpoint_id": in.EndpointID}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.GetApiFlow(in.EndpointID)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}

type codeGraphSearchSymbolInput struct {
	Query string `json:"query"`
}

func (s *Server) toolCodeGraphSearchSymbol(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphSearchSymbolInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("search_symbol", map[string]any{"query": in.Query}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.SearchSymbol(in.Query)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
```

- [x] **Step 5: Run test to verify it passes**

Run: `go test ./internal/mcp/... -run TestToolCodeGraph -v`
Expected: PASS.

- [x] **Step 6: Run the full MCP + codegraph + bridge test suites**

Run: `go test ./internal/mcp/... ./internal/codegraph/... ./internal/bridge/... -v`
Expected: PASS across all three packages.

- [x] **Step 7: Run the repo-wide Go test + arch-layering check**

Run: `go test ./internal/... ./cmd/... -count=1`
Expected: PASS. (Per `AGENTS.md`, `go test ./...` alone will fail on unrelated `build/ios` scaffolding — that's expected and not this plan's concern; the scoped command above is the real check.)

Run: `go test ./internal/arch/ -v`
Expected: PASS — confirms `internal/codegraph` doesn't import anything the architecture layering rules forbid (e.g. it must not import `internal/bridge` or `internal/mcp`; dependencies only flow the other way).

- [x] **Step 8: Commit**

```bash
git add internal/mcp/tools_codegraph.go internal/mcp/tools_codegraph_test.go internal/mcp/server.go
git commit -m "feat(codegraph): add MCP tools (listEndpoints, getApiFlow, searchSymbol)"
```

---

## What this plan does NOT unlock yet

`CodeGraphOpenProject`'s bridge method exists and is tested, but nothing calls `Service.SetProjectPath` with a real per-project file path before invoking it — that plumbing (deriving a project-key hash from the opened path, choosing a real data directory instead of a temp dir) belongs to whichever follow-up plan adds the frontend's "choose a folder" flow, since that's the first point a real project path exists at runtime. Similarly, `codegraph.openProject` is not an MCP tool in this plan (see Task 7's note) — pick that up alongside real path-safety work, not before it.

The next plans, in dependency order per the design doc: real Java analyzer CLI (separate Java/Maven project), frontend module + folder-picker + endpoint list UI, then the six add-ons (File editing, Incremental re-index, Graph UI, Data flow & BRD, Terminal, Plan-first flow) — each already scoped in `base-doc`'s `codegraph.mdx`.
