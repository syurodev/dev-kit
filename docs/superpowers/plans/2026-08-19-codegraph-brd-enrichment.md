# CodeGraph BRD Data Enrichment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the "BRD" half of ["Data flow analysis & BRD (add-on v1)"](base-doc `content/projects/dev-kit/codegraph.mdx:457-505`) — enrich `Node` (and therefore `GetApiFlow`'s `FlowStep`, `GetNeighbors`'s `Subgraph.Nodes`) with a Javadoc summary and full parameter signatures, so an agent writing a BRD from `getApiFlow`/`getNeighbors` output can infer behavior without opening every file. Per the design doc's own framing: DevKit's job here is enriching data it already has, **not** generating BRD prose itself — no LLM call is added to `internal/codegraph`, matching every other module's "expose data, agent writes the words" pattern.

**Scope decision — split from `TraceDataFlow`:** the same design section also specifies `CodeGraphTraceDataFlow(symbolId, fieldName)` (trace which methods read/write a field, bounded to the already-resolved call graph). That needs a genuinely new analysis dimension — field-level read/write tracking — that today's `Node`/`Edge` schema has zero support for (edges are method-to-method `call` only), plus new Java-analyzer AST work to distinguish a field read from a write (is a `NameExpr` resolving to a field the target of an `AssignExpr`, a compound assignment, `field++`, etc. — real static-analysis work, not a data-shape change). This plan is purely additive metadata on data that already flows end-to-end; `TraceDataFlow` is a separate, larger unit of work and the natural next plan.

**Architecture:** `Node` gains two fields, `Summary` and `Params`. Every place that already materializes a `Node` from SQLite (there is exactly one: `Store.GetNode`) gets them for free — `GetNeighbors` already stores the full `Node` it reads from `GetNode` into its result, so it needs **no code change at all** once `GetNode` carries the new columns. `GetApiFlow`'s `FlowStep` is a separate, smaller struct (not `Node` itself) built by `walkFlow` from a `Node` it already fetched — that's the one place needing an explicit field copy.

**Not built here, flagged for the Java analyzer plan:** `2026-08-19-codegraph-java-analyzer.md` doesn't capture Javadoc or full parameter lists today — its `ProjectAnalyzer` only reads what's needed for `Node.Label`/`Kind`/`Layer`/`FilePath`/`Line`/`EndLine`. This plan's Go side is fully testable via the existing `fakeAnalyzer` regardless (same pattern every prior Go-only CodeGraph plan in this sequence has used), and is documented as a forward-looking integration note rather than editing that already-written plan directly — see "What this plan does NOT do".

**Tech Stack:** Go only. `encoding/json` to persist `Params` as a JSON text column (SQLite has no native array/struct column type) — no new dependency, already in the standard library.

---

## File Structure

Modify:
- `internal/codegraph/codegraph.go` — `Param` type; extend `Node` and `FlowStep`.
- `internal/codegraph/store.go` — 2 new `nodes` columns (`summary`, `params`), `ReplaceIndex`/`GetNode` read/write them.
- `internal/codegraph/store_test.go` — round-trip test for the new columns.
- `internal/codegraph/service.go` — `walkFlow` copies `Summary`/`Params` into each `FlowStep`.
- `internal/codegraph/service_test.go` — test that `GetApiFlow` and `GetNeighbors` both carry the enrichment through.

---

## Task 1: `Param` type; extend `Node` and `FlowStep`

**Files:**
- Modify: `internal/codegraph/codegraph.go`

- [ ] **Step 1: Add `Param` and extend the two structs**

In `internal/codegraph/codegraph.go`, add near `Node`:

```go
// Param is one method parameter's name and type, e.g. {Name: "request", Type:
// "RefundRequest"}. Populated by the analyzer for method nodes; always empty
// for class nodes and for any node the analyzer hasn't resolved a signature
// for.
type Param struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
```

Extend `Node` (lines 24-32):

```go
// Node is a class or method discovered during indexing.
type Node struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"` // fully-qualified, e.g. "com.acme.OrderService.create"
	Kind     string  `json:"kind"`  // "class" | "method"
	Layer    string  `json:"layer"` // "controller" | "service" | "repository" | "unknown"
	FilePath string  `json:"filePath"`
	Line     int     `json:"line"`
	EndLine  int     `json:"endLine"`
	Summary  string  `json:"summary"` // Javadoc summary text, empty if none/not a doc comment
	Params   []Param `json:"params"`  // full parameter signature, empty for class nodes
}
```

Extend `FlowStep` (lines 50-54):

```go
// FlowStep is one hop in an API flow.
type FlowStep struct {
	Label    string  `json:"label"` // matches the Node.Label it came from
	FilePath string  `json:"filePath"`
	Line     int     `json:"line"`
	Summary  string  `json:"summary"`
	Params   []Param `json:"params"`
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: succeeds — both changes are purely additive struct fields; every existing literal (tests, the not-yet-implemented Java analyzer plan's JSON contract) omits them and gets the zero value.

- [ ] **Step 3: Commit**

```bash
git add internal/codegraph/codegraph.go
git commit -m "feat(codegraph): add Param type; Node/FlowStep gain Summary and Params"
```

---

## Task 2: Persist `summary`/`params` in SQLite

**Files:**
- Modify: `internal/codegraph/store.go`
- Modify: `internal/codegraph/store_test.go`

**No migration path** — the 2 new columns are added directly to `CREATE TABLE IF NOT EXISTS nodes (...)`, which only affects a **brand new** per-project SQLite file. Every project gets its own DB file keyed by a path hash (`ProjectDBPath`, `2026-08-19-codegraph-project-path-wiring.md`), created fresh on first open — there is no existing deployed data to migrate, and no migration machinery exists anywhere in this package today. If some other plan in this sequence is implemented and used against a real project *before* this one lands, that project's DB file would predate these columns and `GetNode`/`ReplaceIndex` would fail with "no such column" until the project is re-opened fresh (which recreates nothing — the file already exists) — acceptable for pre-release software with no real deployed data; revisit with an actual `ALTER TABLE` migration only if this codebase ever needs to preserve real users' existing index files across a schema change.

- [ ] **Step 1: Write the failing test**

Add to `internal/codegraph/store_test.go`:

```go
func TestStoreGetNodePersistsSummaryAndParams(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	result := AnalysisResult{
		Nodes: []Node{
			{
				ID: "n1", Label: "OrderService.create", Kind: "method", Layer: "service",
				FilePath: "OrderService.java", Line: 10, EndLine: 20,
				Summary: "Creates a new order for the given request.",
				Params:  []Param{{Name: "request", Type: "OrderRequest"}, {Name: "userId", Type: "String"}},
			},
			// A class node with no summary/params — must round-trip as empty, not error.
			{ID: "n2", Label: "OrderService", Kind: "class", Layer: "service", FilePath: "OrderService.java", Line: 1, EndLine: 40},
		},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	got, err := store.GetNode(ctx, "n1")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Summary != "Creates a new order for the given request." {
		t.Fatalf("Summary = %q", got.Summary)
	}
	if len(got.Params) != 2 || got.Params[0] != (Param{Name: "request", Type: "OrderRequest"}) || got.Params[1] != (Param{Name: "userId", Type: "String"}) {
		t.Fatalf("Params = %+v", got.Params)
	}

	gotClass, err := store.GetNode(ctx, "n2")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if gotClass.Summary != "" || len(gotClass.Params) != 0 {
		t.Fatalf("expected empty Summary/Params for a class node, got %+v", gotClass)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestStoreGetNodePersistsSummaryAndParams -v
```

Expected: FAIL — the `nodes` table has no `summary`/`params` columns yet, and `GetNode`'s `SELECT` doesn't fetch them, so `got.Summary`/`got.Params` will be zero values regardless of what was inserted (in fact `ReplaceIndex`'s current `INSERT` statement doesn't even have placeholders for these two new struct fields yet, so this won't compile-fail — it'll assert-fail once the columns are added to the schema in Step 3 but not yet wired into the INSERT/SELECT statements; run this in two sub-steps if that's clearer: first confirm a compile pass with zero-value assertion failures, then implement).

- [ ] **Step 3: Add the columns**

In `internal/codegraph/store.go`, extend the `schema` constant's `nodes` table:

```go
CREATE TABLE IF NOT EXISTS nodes (
	id TEXT PRIMARY KEY,
	label TEXT NOT NULL,
	kind TEXT NOT NULL,
	layer TEXT NOT NULL,
	file_path TEXT NOT NULL,
	line INTEGER NOT NULL,
	end_line INTEGER NOT NULL,
	summary TEXT NOT NULL DEFAULT '',
	params TEXT NOT NULL DEFAULT '[]'
);
```

(`params` is a JSON-encoded `[]Param` — SQLite has no native array column type; `DEFAULT '[]'` keeps an empty-params row trivially parseable as an empty slice rather than needing NULL-handling.)

- [ ] **Step 4: Wire `ReplaceIndex`'s insert**

In `internal/codegraph/store.go`, `ReplaceIndex`'s node insert loop:

```go
	for _, n := range result.Nodes {
		paramsJSON, err := json.Marshal(n.Params)
		if err != nil {
			return fmt.Errorf("codegraph: marshal params for node %q: %w", n.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO nodes (id, label, kind, layer, file_path, line, end_line, summary, params) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.Label, n.Kind, n.Layer, n.FilePath, n.Line, n.EndLine, n.Summary, string(paramsJSON)); err != nil {
			return fmt.Errorf("codegraph: insert node %q: %w", n.ID, err)
		}
	}
```

(`json.Marshal(nil)` — a `Node` with no `Params` set — produces `"null"`, not `"[]"`; that's fine to store, but `json.Unmarshal` of `"null"` back into a `[]Param` also correctly yields `nil`/empty, so this round-trips without special-casing. Verify this holds in Step 8's test run rather than assuming it.)

- [ ] **Step 5: Wire `GetNode`'s select**

```go
func (s *sqliteStore) GetNode(ctx context.Context, nodeID string) (Node, error) {
	var n Node
	var paramsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, label, kind, layer, file_path, line, end_line, summary, params FROM nodes WHERE id = ?`, nodeID,
	).Scan(&n.ID, &n.Label, &n.Kind, &n.Layer, &n.FilePath, &n.Line, &n.EndLine, &n.Summary, &paramsJSON)
	if err == sql.ErrNoRows {
		return Node{}, ErrNodeNotFound
	}
	if err != nil {
		return Node{}, fmt.Errorf("codegraph: GetNode: %w", err)
	}
	if err := json.Unmarshal([]byte(paramsJSON), &n.Params); err != nil {
		return Node{}, fmt.Errorf("codegraph: unmarshal params for node %q: %w", nodeID, err)
	}
	return n, nil
}
```

Add `"encoding/json"` to `internal/codegraph/store.go`'s imports.

- [ ] **Step 6: Run the test**

```bash
go test ./internal/codegraph/... -run TestStoreGetNodePersistsSummaryAndParams -v
```

Expected: passes. If the `json.Marshal(nil)` → `"null"` → `json.Unmarshal` → `nil` round-trip from Step 4's note doesn't hold for some reason, the class-node assertion (`len(gotClass.Params) != 0`) will catch it here — fix by normalizing `nil` to `[]Param{}` before marshaling if so, rather than assuming.

- [ ] **Step 7: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

Expected: passes, including every pre-existing `Store`/`GetNode` test — `sampleResult()`'s nodes never set `Summary`/`Params`, so they exercise exactly the "empty" path Step 6 already validated.

- [ ] **Step 8: Commit**

```bash
git add internal/codegraph/store.go internal/codegraph/store_test.go
git commit -m "feat(codegraph): persist Node.Summary and Node.Params in SQLite"
```

---

## Task 3: `GetApiFlow` carries the enrichment through `FlowStep`

**Files:**
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

`GetNeighbors` needs **no change** for this task — it already stores the full `Node` (from `store.GetNode`, updated in Task 2) directly into its result, so `Subgraph.Nodes[i].Summary`/`.Params` are already populated once Task 2 lands. Only `walkFlow` (which builds the smaller `FlowStep` struct, not `Node` itself) needs an explicit copy.

- [ ] **Step 1: Write the failing tests**

Add to `internal/codegraph/service_test.go`:

```go
func TestServiceGetApiFlowCarriesSummaryAndParams(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{
				ID: "n1", Label: "OrderController.create", Kind: "method", Layer: "controller",
				FilePath: "Order.java", Line: 10, EndLine: 15,
				Summary: "Handles order creation requests.",
				Params:  []Param{{Name: "request", Type: "CreateOrderRequest"}},
			},
		},
		Endpoints: []Endpoint{{ID: "ep1", HTTPMethod: "POST", Path: "/orders", EntryNode: "n1"}},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	flow, err := svc.GetApiFlow(context.Background(), "ep1")
	if err != nil {
		t.Fatalf("GetApiFlow: %v", err)
	}
	if len(flow.Steps) != 1 {
		t.Fatalf("steps = %+v, want 1", flow.Steps)
	}
	step := flow.Steps[0]
	if step.Summary != "Handles order creation requests." {
		t.Fatalf("Summary = %q", step.Summary)
	}
	if len(step.Params) != 1 || step.Params[0] != (Param{Name: "request", Type: "CreateOrderRequest"}) {
		t.Fatalf("Params = %+v", step.Params)
	}
}

func TestServiceGetNeighborsCarriesSummaryAndParams(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{
				ID: "n1", Label: "OrderService.process", Kind: "method", Layer: "service",
				FilePath: "OrderService.java", Line: 1, EndLine: 5,
				Summary: "Processes a validated order.",
				Params:  []Param{{Name: "order", Type: "Order"}},
			},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	sub, err := svc.GetNeighbors(context.Background(), "n1", 1, NeighborFilter{})
	if err != nil {
		t.Fatalf("GetNeighbors: %v", err)
	}
	if len(sub.Nodes) != 1 {
		t.Fatalf("nodes = %+v, want 1", sub.Nodes)
	}
	if sub.Nodes[0].Summary != "Processes a validated order." {
		t.Fatalf("Summary = %q", sub.Nodes[0].Summary)
	}
	if len(sub.Nodes[0].Params) != 1 || sub.Nodes[0].Params[0] != (Param{Name: "order", Type: "Order"}) {
		t.Fatalf("Params = %+v", sub.Nodes[0].Params)
	}
}
```

- [ ] **Step 2: Run**

```bash
go test ./internal/codegraph/... -run "TestServiceGetApiFlowCarriesSummaryAndParams|TestServiceGetNeighborsCarriesSummaryAndParams" -v
```

Expected: `TestServiceGetNeighborsCarriesSummaryAndParams` **already passes** (confirms the "no code change needed" claim above — if it doesn't, something about `GetNeighbors`/`GetNode`'s wiring from Task 2 is incomplete, go back and check before proceeding). `TestServiceGetApiFlowCarriesSummaryAndParams` FAILS — `walkFlow` doesn't copy the 2 new fields into `FlowStep` yet.

- [ ] **Step 3: Implement**

In `internal/codegraph/service.go`, `walkFlow`'s `FlowStep` construction:

```go
		steps = append(steps, FlowStep{
			Label:    node.Label,
			FilePath: node.FilePath,
			Line:     node.Line,
			Summary:  node.Summary,
			Params:   node.Params,
		})
```

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/codegraph/... -run "TestServiceGetApiFlowCarriesSummaryAndParams|TestServiceGetNeighborsCarriesSummaryAndParams" -v
```

Expected: both pass.

- [ ] **Step 5: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 6: Commit**

```bash
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): GetApiFlow carries Summary/Params through FlowStep"
```

---

## Task 4: Full regression

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

Expected: everything passes. No bridge or MCP changes were needed in this plan — `CodeGraphGetApiFlow`/`CodeGraphGetNeighbors` and their MCP tools already return the full `ApiFlow`/`Subgraph` structs, so the new `Summary`/`Params` fields flow through automatically once Go's own JSON marshaling serializes them; nothing to wire.

---

## What this plan does NOT do

**Does not modify the Java analyzer CLI** (`2026-08-19-codegraph-java-analyzer.md`, not yet implemented) to actually populate `Summary`/`Params` from real Java source — this plan's Go side is complete and tested via `fakeAnalyzer` regardless, same as every other Go-only plan in this sequence. **Forward-looking integration note for whoever implements or extends that plan:** JavaParser exposes a Javadoc comment on a declaration via its `NodeWithJavadoc<N>` interface (implemented by `MethodDeclaration`/`ClassOrInterfaceDeclaration`, the same declarations `ProjectAnalyzer` already visits to build `Node`s) — verify its exact `getJavadoc()`/`getJavadocComment()` method names and `Javadoc`'s text-extraction API against the real 3.28.2 source before writing that code, same "verify, don't guess" discipline this whole sequence has used for every other JavaParser API touched. Full parameters come from `MethodDeclaration.getParameters()` → `NodeList<Parameter>`, each with `.getType().asString()` and its name.

**`CodeGraphTraceDataFlow`** — the other half of "Data flow analysis & BRD" — is the natural next plan, per the scope decision above: it needs a new field-access data model and new Java-analyzer AST work this plan deliberately doesn't touch.

**No enrichment on `SymbolMatch`** (`SearchSymbol`'s result type) — the design doc only asks for `getApiFlow`/`getNeighbors`, not `searchSymbol`; add it later only if a real need shows up.
