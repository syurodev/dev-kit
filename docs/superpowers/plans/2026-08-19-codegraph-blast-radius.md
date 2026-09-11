# CodeGraph File Dependencies & Blast Radius Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the `file_dependencies` table and `GetBlastRadius` from the ["Incremental re-index (add-on v1)"](base-doc `content/projects/dev-kit/codegraph.mdx:507-603`) design — "which files depend on this file" as a real, queryable feature, grounded in data the Java analyzer already resolves and currently throws away.

**Scope decision — split from the rest of "Incremental re-index":** that design section bundles three distinct things: (1) the `file_dependencies` table + `GetBlastRadius` query, (2) `fsnotify`-based automatic re-index on file save with debounce/cancel, and (3) `WaitForIndexReady` as a sync point for callers racing that background trigger. This plan ships **only (1)**. Reason: `WaitForIndexReady` only has a real purpose once *something* makes re-index asynchronous — today `CodeGraphOpenProject` is fully synchronous (the bridge call blocks until `Analyze`+`ReplaceIndex` return), so there is no "pending" window yet for a caller to usefully wait out. Bundling `fsnotify` watcher lifecycle (debounce, cancel-on-newer-event, goroutine cleanup on `Close`) into this plan would also make it the riskiest, hardest-to-test piece in the whole CodeGraph backend — it deserves its own focused plan rather than being rushed alongside a much simpler, independently-valuable feature. `GetBlastRadius` needs none of that: it is a synchronous read against data `ReplaceIndex` already writes transactionally. The `fsnotify` auto-trigger + `WaitForIndexReady` is the natural next plan after this one.

**Architecture:** The Java analyzer's `JavaSymbolSolver` already resolves which file each import/reference points at while building the call graph — this plan just keeps that byproduct instead of discarding it after `index.json` is written (per the design doc: "hiện đang bị bỏ qua sau khi xuất `index.json`, chỉ cần giữ lại thay vì tính lại"). Go side: extend `AnalysisResult` with a `FileDependencies` slice, persist it in the same transactional `ReplaceIndex` swap as `nodes`/`edges`/`endpoints`, add one reverse-query `Store` method, and a bounded BFS in `Service.GetBlastRadius` for the transitive closure (mirrors `walkFlow`'s existing depth-bounded-BFS style in `service.go:118-154`).

**Tech Stack:** Go only — no new dependencies. (The Java analyzer CLI itself, `docs/superpowers/plans/2026-08-19-codegraph-java-analyzer.md`, is a separate not-yet-implemented plan; this plan's Go-side work is fully testable today via the existing `fakeAnalyzer` test double, same as every other CodeGraph Go plan so far — nothing here blocks on the Java CLI existing.)

---

## File Structure

Modify:
- `internal/codegraph/codegraph.go` — add `FileDependency` type, extend `AnalysisResult`, extend `Store` interface.
- `internal/codegraph/store.go` — `file_dependencies` table, extend `ReplaceIndex`, implement `GetDependents`.
- `internal/codegraph/store_test.go` — tests for the extended `ReplaceIndex` and new `GetDependents`.
- `internal/codegraph/service.go` — `GetBlastRadius`, `nilStore.GetDependents` stub.
- `internal/codegraph/service_test.go` — tests for `GetBlastRadius`.
- `internal/bridge/codegraph.go` — `CodeGraphGetBlastRadius`.
- `internal/bridge/codegraph_test.go` — test for the new bridge method.
- `internal/mcp/server.go` — extend `CodeGraphAPI`.
- `internal/mcp/tools_codegraph.go` — register `codegraph.getBlastRadius`.
- `internal/mcp/tools_codegraph_test.go` — extend `fakeCodeGraphAPI`, test for the new tool.

---

## Task 1: `FileDependency` type

**Files:**
- Modify: `internal/codegraph/codegraph.go`

- [ ] **Step 1: Add the type and extend `AnalysisResult`**

In `internal/codegraph/codegraph.go`, add near `Endpoint`:

```go
// FileDependency is one file-level "depends on" edge — filePath imports or
// otherwise references a symbol declared in dependsOnFilePath. Produced as a
// byproduct of the analyzer's symbol resolution (it already knows this while
// resolving Node/Edge — see AnalysisResult's doc comment); kept for
// GetBlastRadius rather than recomputed later.
type FileDependency struct {
	FilePath          string `json:"filePath"`
	DependsOnFilePath string `json:"dependsOnFilePath"`
}
```

Extend `AnalysisResult` (lines 72-76):

```go
// AnalysisResult is everything an Analyzer produces for one project.
type AnalysisResult struct {
	Nodes            []Node            `json:"nodes"`
	Edges            []Edge            `json:"edges"`
	Endpoints        []Endpoint        `json:"endpoints"`
	FileDependencies []FileDependency  `json:"fileDependencies"`
}
```

`Validate()` (lines 81-100) does not need changes — a `FileDependency` references file paths, not `Node.ID`s, so there's nothing to cross-check against `Nodes` (a file can have zero indexed nodes, e.g. a `pom.xml` or `.properties` file referenced only by path, and still legitimately appear here for a future non-Java dependency kind).

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: succeeds — this is a pure additive struct field; every existing `AnalysisResult{...}` literal in the codebase (tests, the not-yet-implemented Java analyzer plan) omits `FileDependencies` and gets its zero value (`nil`), which is valid.

- [ ] **Step 3: Commit**

```bash
git add internal/codegraph/codegraph.go
git commit -m "feat(codegraph): add FileDependency type to AnalysisResult"
```

---

## Task 2: `file_dependencies` table

**Files:**
- Modify: `internal/codegraph/store.go`
- Modify: `internal/codegraph/store_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/codegraph/store_test.go`:

```go
func TestStoreReplaceIndexPersistsFileDependencies(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	result := sampleResult()
	result.FileDependencies = []FileDependency{
		{FilePath: "Order.java", DependsOnFilePath: "OrderService.java"},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	dependents, err := store.GetDependents(ctx, "OrderService.java")
	if err != nil {
		t.Fatalf("GetDependents: %v", err)
	}
	if len(dependents) != 1 || dependents[0] != "Order.java" {
		t.Fatalf("unexpected dependents: %+v", dependents)
	}
}

func TestStoreReplaceIndexClearsOldFileDependencies(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first := sampleResult()
	first.FileDependencies = []FileDependency{
		{FilePath: "Order.java", DependsOnFilePath: "OrderService.java"},
	}
	if err := store.ReplaceIndex(ctx, first); err != nil {
		t.Fatalf("first ReplaceIndex: %v", err)
	}

	// Second index has no file dependencies at all — must fully replace, not merge.
	second := AnalysisResult{
		Nodes:     []Node{{ID: "n9", Label: "Health.check", Kind: "method", Layer: "controller"}},
		Endpoints: []Endpoint{{ID: "ep9", HTTPMethod: "GET", Path: "/health", EntryNode: "n9"}},
	}
	if err := store.ReplaceIndex(ctx, second); err != nil {
		t.Fatalf("second ReplaceIndex: %v", err)
	}

	dependents, err := store.GetDependents(ctx, "OrderService.java")
	if err != nil {
		t.Fatalf("GetDependents: %v", err)
	}
	if len(dependents) != 0 {
		t.Fatalf("expected no dependents after replace, got: %+v", dependents)
	}
}

func TestStoreGetDependentsNoMatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, sampleResult()); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	dependents, err := store.GetDependents(ctx, "Nobody.java")
	if err != nil {
		t.Fatalf("GetDependents: %v", err)
	}
	if len(dependents) != 0 {
		t.Fatalf("expected no dependents, got: %+v", dependents)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run "TestStoreReplaceIndexPersistsFileDependencies|TestStoreReplaceIndexClearsOldFileDependencies|TestStoreGetDependentsNoMatch" -v
```

Expected: FAIL — compile error, `GetDependents` doesn't exist and `ReplaceIndex` doesn't persist `FileDependencies` yet.

- [ ] **Step 3: Add the table to the schema**

In `internal/codegraph/store.go`, extend the `schema` constant (lines 12-34):

```go
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
CREATE TABLE IF NOT EXISTS file_dependencies (
	file_path TEXT NOT NULL,
	depends_on_file_path TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_file_deps_depends_on ON file_dependencies(depends_on_file_path);
`
```

(Indexed on `depends_on_file_path` — that's the reverse-query column `GetDependents` filters on, same reasoning as `idx_edges_from` existing for `edges.from_id`, the column `GetOutEdges` filters on.)

- [ ] **Step 4: Persist it in `ReplaceIndex`**

In `internal/codegraph/store.go`, `ReplaceIndex` (lines 57-97): add `"file_dependencies"` to the tables cleared in the loop, and add an insert loop for `result.FileDependencies` alongside the existing three:

```go
	for _, table := range []string{"nodes", "edges", "endpoints", "file_dependencies"} {
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
	for _, fd := range result.FileDependencies {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO file_dependencies (file_path, depends_on_file_path) VALUES (?, ?)`,
			fd.FilePath, fd.DependsOnFilePath); err != nil {
			return fmt.Errorf("codegraph: insert file dependency %q->%q: %w", fd.FilePath, fd.DependsOnFilePath, err)
		}
	}
```

- [ ] **Step 5: Implement `GetDependents`**

In `internal/codegraph/store.go`, add below `SearchSymbol`:

```go
// GetDependents returns every file that directly depends on filePath (i.e.
// filePath appears as their depends_on_file_path) — one hop, not transitive.
// GetBlastRadius (Service) walks this repeatedly for the transitive closure.
func (s *sqliteStore) GetDependents(ctx context.Context, filePath string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT file_path FROM file_dependencies WHERE depends_on_file_path = ? ORDER BY file_path`,
		filePath)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetDependents: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("codegraph: scan dependent: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
```

- [ ] **Step 6: Add to the `Store` interface**

In `internal/codegraph/codegraph.go`, extend the `Store` interface (lines 113-122):

```go
type Store interface {
	ReplaceIndex(ctx context.Context, result AnalysisResult) error
	HasIndex(ctx context.Context) (bool, error)
	ListEndpoints(ctx context.Context) ([]Endpoint, error)
	GetEndpoint(ctx context.Context, endpointID string) (Endpoint, error)
	GetNode(ctx context.Context, nodeID string) (Node, error)
	GetOutEdges(ctx context.Context, nodeID string) ([]Edge, error)
	SearchSymbol(ctx context.Context, query string) ([]SymbolMatch, error)
	GetDependents(ctx context.Context, filePath string) ([]string, error)
	Close() error
}
```

- [ ] **Step 7: Add the `nilStore` stub**

In `internal/codegraph/service.go`, add next to `nilStore`'s other methods:

```go
func (nilStore) GetDependents(context.Context, string) ([]string, error) { return nil, ErrNoActiveProject }
```

- [ ] **Step 8: Run the tests**

```bash
go test ./internal/codegraph/... -run "TestStoreReplaceIndexPersistsFileDependencies|TestStoreReplaceIndexClearsOldFileDependencies|TestStoreGetDependentsNoMatch" -v
go build ./...
```

Expected: all 3 pass; build succeeds (confirms `sqliteStore` and `nilStore` both still satisfy `Store`).

- [ ] **Step 9: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 10: Commit**

```bash
git add internal/codegraph/store.go internal/codegraph/store_test.go internal/codegraph/codegraph.go internal/codegraph/service.go
git commit -m "feat(codegraph): file_dependencies table + Store.GetDependents"
```

---

## Task 3: `Service.GetBlastRadius`

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/codegraph/service_test.go`:

```go
func TestServiceGetBlastRadiusTransitive(t *testing.T) {
	// Controller depends on Service, Service depends on Repository.
	// Editing Repository.java should report both Service.java (direct) and
	// Controller.java (transitive, via Service).
	result := AnalysisResult{
		Nodes: []Node{{ID: "n1", Label: "Controller.handle", Kind: "method", Layer: "controller", FilePath: "Controller.java", Line: 1, EndLine: 5}},
		FileDependencies: []FileDependency{
			{FilePath: "Controller.java", DependsOnFilePath: "Service.java"},
			{FilePath: "Service.java", DependsOnFilePath: "Repository.java"},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	radius, err := svc.GetBlastRadius(context.Background(), "Repository.java")
	if err != nil {
		t.Fatalf("GetBlastRadius: %v", err)
	}
	if len(radius) != 2 {
		t.Fatalf("radius = %v, want 2 entries", radius)
	}
	want := map[string]bool{"Controller.java": true, "Service.java": true}
	for _, f := range radius {
		if !want[f] {
			t.Fatalf("unexpected file in radius: %q (full: %v)", f, radius)
		}
	}
}

func TestServiceGetBlastRadiusNoDependents(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: sampleResult()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	radius, err := svc.GetBlastRadius(context.Background(), "Nobody.java")
	if err != nil {
		t.Fatalf("GetBlastRadius: %v", err)
	}
	if len(radius) != 0 {
		t.Fatalf("expected empty radius, got: %v", radius)
	}
}

func TestServiceGetBlastRadiusHandlesCycle(t *testing.T) {
	// A depends on B, B depends on A — a real possibility with Java's mutual
	// class references. The visited-set must stop this from looping forever.
	result := AnalysisResult{
		FileDependencies: []FileDependency{
			{FilePath: "A.java", DependsOnFilePath: "B.java"},
			{FilePath: "B.java", DependsOnFilePath: "A.java"},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	done := make(chan struct{})
	var radius []string
	var err error
	go func() {
		radius, err = svc.GetBlastRadius(context.Background(), "A.java")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GetBlastRadius did not return — likely an infinite loop on the cycle")
	}
	if err != nil {
		t.Fatalf("GetBlastRadius: %v", err)
	}
	if len(radius) != 1 || radius[0] != "B.java" {
		t.Fatalf("radius = %v, want exactly [B.java]", radius)
	}
}

func TestServiceGetBlastRadiusBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	if _, err := svc.GetBlastRadius(context.Background(), "Foo.java"); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}
```

Add `"time"` to `internal/codegraph/service_test.go`'s imports (currently `"context"`, `"errors"`, `"path/filepath"`, `"sync"`, `"testing"`).

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestServiceGetBlastRadius -v
```

Expected: FAIL — compile error, `GetBlastRadius` doesn't exist yet.

- [ ] **Step 3: Implement**

In `internal/codegraph/service.go`, add below `GetApiFlow`/`walkFlow`:

```go
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
```

Add `"sort"` to `internal/codegraph/service.go`'s imports (currently `"context"`, `"fmt"`, `"sync"`).

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceGetBlastRadius -v
```

Expected: all 4 pass.

- [ ] **Step 5: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 6: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): Service.GetBlastRadius"
```

---

## Task 4: Bridge method

**Files:**
- Modify: `internal/bridge/codegraph.go`
- Modify: `internal/bridge/codegraph_test.go`

No new capability grant needed — `CodeGraphGetBlastRadius` uses `codegraph:read`/`project:active`, the same grant `CodeGraphListEndpoints` etc. already use (`internal/app/container.go:167`, unchanged since Phase 1a).

- [ ] **Step 1: Write the failing test**

Add to `internal/bridge/codegraph_test.go`:

```go
func TestCodeGraphGetBlastRadius(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		FileDependencies: []codegraph.FileDependency{
			{FilePath: "Controller.java", DependsOnFilePath: "Service.java"},
		},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	if err := s.container.CodeGraph.SetProjectPath(t.TempDir() + "/index.db"); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	radius, err := s.CodeGraphGetBlastRadius("Service.java")
	if err != nil {
		t.Fatalf("CodeGraphGetBlastRadius: %v", err)
	}
	if len(radius) != 1 || radius[0] != "Controller.java" {
		t.Fatalf("unexpected radius: %+v", radius)
	}
}
```

(This test builds the service with `container.CodeGraph.SetProjectPath`+`OpenProject` directly rather than going through `CodeGraphOpenProject`/`ConfigDir`, matching the style of the pre-existing `TestCodeGraphListEndpointsBeforeOpenProjectFails` — either approach works since `GetBlastRadius` doesn't touch the filesystem-path derivation logic the other tests exercise.)

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/bridge/... -run TestCodeGraphGetBlastRadius -v
```

Expected: FAIL — compile error, `CodeGraphGetBlastRadius` doesn't exist yet.

- [ ] **Step 3: Implement**

In `internal/bridge/codegraph.go`, add below `CodeGraphSearchSymbol`:

```go
func (s *AppService) CodeGraphGetBlastRadius(filePath string) ([]string, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":    "get_blast_radius",
		"file_path": filePath,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.GetBlastRadius(ctx, filePath)
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/bridge/... -run TestCodeGraph -v
```

Expected: all pass, including every pre-existing `TestCodeGraph*` test.

- [ ] **Step 5: Commit**

```bash
git add internal/bridge/codegraph.go internal/bridge/codegraph_test.go
git commit -m "feat(codegraph): CodeGraphGetBlastRadius bridge method"
```

---

## Task 5: MCP tool

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/tools_codegraph.go`
- Modify: `internal/mcp/tools_codegraph_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/mcp/tools_codegraph_test.go` (extend `fakeCodeGraphAPI` with a `blastRadius []string` field alongside its existing fields):

```go
func TestToolCodeGraphGetBlastRadius(t *testing.T) {
	deps := Deps{
		Caller:    &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{blastRadius: []string{"Controller.java"}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphGetBlastRadius(context.Background(), nil, codeGraphGetBlastRadiusInput{FilePath: "Service.java"})
	if err != nil {
		t.Fatalf("toolCodeGraphGetBlastRadius: %v", err)
	}
	radius, ok := v.([]string)
	if !ok || len(radius) != 1 || radius[0] != "Controller.java" {
		t.Fatalf("unexpected result: %#v", v)
	}
}
```

Add the field and method to `fakeCodeGraphAPI`:

```go
type fakeCodeGraphAPI struct {
	endpoints   []codegraph.Endpoint
	flow        codegraph.ApiFlow
	matches     []codegraph.SymbolMatch
	blastRadius []string
	err         error
}
```

```go
func (f fakeCodeGraphAPI) GetBlastRadius(ctx context.Context, filePath string) ([]string, error) {
	return f.blastRadius, f.err
}
```

(If Task 1/10 of `2026-08-19-codegraph-file-editing.md` has already landed by the time this plan is implemented, `fakeCodeGraphAPI` will already have `files`/`fileContent`/`snippet` fields too — add `blastRadius` alongside them rather than redefining the whole struct.)

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/mcp/... -run TestToolCodeGraphGetBlastRadius -v
```

Expected: FAIL — compile error, `toolCodeGraphGetBlastRadius`/`codeGraphGetBlastRadiusInput` don't exist, and `CodeGraphAPI` doesn't require `GetBlastRadius` yet.

- [ ] **Step 3: Extend the interface**

In `internal/mcp/server.go`, add to `CodeGraphAPI`:

```go
	GetBlastRadius(ctx context.Context, filePath string) ([]string, error)
```

(If `2026-08-19-codegraph-file-editing.md` has already landed, this interface will already have `ListFiles`/`ReadFile`/`GetCodeSnippet` too — add this line alongside them.)

- [ ] **Step 4: Register the tool**

In `internal/mcp/tools_codegraph.go`, add to `registerCodeGraph`:

```go
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.getBlastRadius",
		Description: "List files that depend (import/reference) on a given file, transitively",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphGetBlastRadius)
```

And the handler:

```go
type codeGraphGetBlastRadiusInput struct {
	FilePath string `json:"filePath"`
}

func (s *Server) toolCodeGraphGetBlastRadius(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphGetBlastRadiusInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("get_blast_radius", map[string]any{"file_path": in.FilePath}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.GetBlastRadius(ctx, in.FilePath)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
```

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/mcp/... -run TestToolCodeGraph -v
```

Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools_codegraph.go internal/mcp/tools_codegraph_test.go
git commit -m "feat(codegraph): expose getBlastRadius via MCP"
```

---

## Task 6: Full regression

**Files:** none (verification only)

- [ ] **Step 1: Run the repo's documented standard check**

```bash
go test ./internal/... ./cmd/... -count=1
go build ./...
```

- [ ] **Step 2: Run with `-race`**

```bash
go test ./internal/... ./cmd/... -race -count=1
```

Expected: everything passes.

---

## What this plan does NOT do

**`fsnotify` auto-trigger + `WaitForIndexReady` + `Service.Close()`** — the rest of the "Incremental re-index" design section — is the natural next plan, now unblocked by this one (`indexVersion`-style state tracking and the reindex-in-progress flag that plan needs are new work there, not something this plan had to pre-build). **True partial re-parse** (Java CLI only re-analyzing the affected file set instead of the whole project) is a further-out optimization *that plan* will also deliberately defer — full re-analyze on every trigger, just automatically triggered instead of requiring a manual button press; see that design doc's own "Rủi ro thật: sai affected set" callout (`codegraph.mdx:597-603`) for why partial-reparse correctness is inherently approximate and not worth building until a real project proves full re-analyze too slow. No UI changes — `GetBlastRadius` is immediately useful to an MCP-calling agent today ("what else might break if I change this file") even with zero frontend work; a "show blast radius" UI affordance can follow whenever the Graph UI canvas plan lands.
