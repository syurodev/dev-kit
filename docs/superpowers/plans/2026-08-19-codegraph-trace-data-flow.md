# CodeGraph TraceDataFlow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `CodeGraphTraceDataFlow`, the last piece of ["Data flow analysis & BRD (add-on v1)"](base-doc `content/projects/dev-kit/codegraph.mdx:457-505`) — given a class and a field name, list every method (already in the index) that reads or writes that field, with `file:line`.

**Scope reading, made explicit:** the design doc's sequence diagram (`codegraph.mdx:468-478`) shows a single round trip — "lọc method đọc/ghi fieldName" → "danh sách method + file:line, đọc hay ghi" → "chuỗi trace". Read literally, this is a **filtered list query**, not a call-graph walk (that's what `GetApiFlow`/`GetNeighbors`/`GetBlastRadius` already do, each for a different graph). `TraceDataFlow` answers "where is this field touched", not "who eventually calls the code that touches it" — an agent that also wants the latter already has `GetNeighbors`(rootID=one of these methods) or `GetBlastRadius`(that method's file) to compose with, matching this architecture's consistent "small focused tools, agent composes them" shape rather than one tool trying to do everything. No pagination/ceiling machinery is added — unlike `GetNeighbors`'s potentially-huge fan-out, the number of methods that legitimately access one field is naturally small (Java field access is normally scoped to the declaring class, occasionally a subclass) and the doc's own "đã bounded, rẻ" ("already bounded, cheap") framing supports treating this as inherently small, not something needing truncation.

**Architecture:** A new data dimension the index has never captured before — which methods read/write which field — persisted in a new `field_accesses` table, populated by the (not yet implemented) Java analyzer's symbol resolution the same way `file_dependencies` was in `2026-08-19-codegraph-blast-radius.md`. `Service.TraceDataFlow` is a thin wrapper: validate the class exists, run the filtered query, resolve each accessing method's `Node` for its label. No caching (per the design doc, explicitly) — it's cheap enough to recompute every call.

**Not built here, flagged for the Java analyzer plan:** detecting whether a field access is a *read* or a *write* is real static-analysis work, not a data-shape change — `2026-08-19-codegraph-java-analyzer.md` doesn't do this today (it only resolves method calls for the existing `call` edges). Verified against JavaParser's real AST shapes before writing this note (not guessed): a field write shows up as the **target** of an `AssignExpr` (`AssignExpr{target, value, operator}` — a `NameExpr`/`FieldAccessExpr` resolving to the field, sitting in `target`, not `value`, is a write; the same expression appearing as `value`, or anywhere else, is a read) or as the operand of a `UnaryExpr` with `PREFIX_INCREMENT`/`POSTFIX_INCREMENT`/`PREFIX_DECREMENT`/`POSTFIX_DECREMENT` (`field++`/`--field` are writes too, and also reads for a compound case like `field += 1`, but "counts as a write" is the useful classification for this feature — a caller wanting more nuance already has `GetCodeSnippet` to read the real line). This plan's Go side is fully testable via `fakeAnalyzer` regardless, same pattern as every other Go-only plan in this sequence.

**Tech Stack:** Go only.

---

## File Structure

Modify:
- `internal/codegraph/codegraph.go` — `FieldAccess`, `DataFlowStep`, `DataFlowTrace` types; extend `AnalysisResult` and `Store`.
- `internal/codegraph/store.go` — `field_accesses` table + index, `ReplaceIndex` persists it, `GetFieldAccesses`.
- `internal/codegraph/store_test.go` — tests for the new table/query.
- `internal/codegraph/service.go` — `TraceDataFlow`, `nilStore.GetFieldAccesses` stub.
- `internal/codegraph/service_test.go` — tests for `TraceDataFlow`.
- `internal/bridge/codegraph.go` — `CodeGraphTraceDataFlow`.
- `internal/bridge/codegraph_test.go` — test for it.
- `internal/mcp/server.go` — extend `CodeGraphAPI`.
- `internal/mcp/tools_codegraph.go` — register `codegraph.traceDataFlow`.
- `internal/mcp/tools_codegraph_test.go` — extend `fakeCodeGraphAPI`, test for the new tool.

---

## Task 1: `FieldAccess` type; extend `AnalysisResult` and `Store`

**Files:**
- Modify: `internal/codegraph/codegraph.go`

- [ ] **Step 1: Add the type**

In `internal/codegraph/codegraph.go`, add near `FileDependency`:

```go
// FieldAccess is one "method X reads/writes field Y" fact, scoped to the
// class that declares the field (classID is that class's Node.ID) — Java
// field access is normally limited to the declaring class (or a subclass),
// so accesses are always looked up together with the class they belong to,
// never by field name alone across the whole project.
type FieldAccess struct {
	ClassID   string `json:"classId"`
	NodeID    string `json:"nodeId"` // the method Node.ID that performs the access
	FieldName string `json:"fieldName"`
	Kind      string `json:"kind"` // "read" | "write"
	Line      int    `json:"line"`
}
```

Extend `AnalysisResult` (already has `FileDependencies` from `2026-08-19-codegraph-blast-radius.md` — if that plan hasn't landed yet, add just the `FieldAccesses` line to whatever the current struct looks like):

```go
type AnalysisResult struct {
	Nodes            []Node            `json:"nodes"`
	Edges            []Edge            `json:"edges"`
	Endpoints        []Endpoint        `json:"endpoints"`
	FileDependencies []FileDependency  `json:"fileDependencies"`
	FieldAccesses    []FieldAccess     `json:"fieldAccesses"`
}
```

Extend the `Store` interface:

```go
	GetFieldAccesses(ctx context.Context, classID, fieldName string) ([]FieldAccess, error)
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

Expected: fails until Task 2 adds the `sqliteStore`/`nilStore` implementations — that's expected, this task alone leaves the package non-compiling for one step, same "types first, implementation next" split every other multi-task plan in this sequence uses. If your workflow prefers a green build after every task, merge Task 1 and Task 2 into one commit instead.

- [ ] **Step 3: Commit (after Task 2's implementations exist, so the build is green)**

Hold this commit until Task 2 Step 6 — commit both together as noted there.

---

## Task 2: `field_accesses` table and `GetFieldAccesses`

**Files:**
- Modify: `internal/codegraph/store.go`
- Modify: `internal/codegraph/store_test.go`
- Modify: `internal/codegraph/service.go` (`nilStore` stub)

- [ ] **Step 1: Write the failing test**

Add to `internal/codegraph/store_test.go`:

```go
func TestStoreGetFieldAccesses(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	result := sampleResult()
	result.FieldAccesses = []FieldAccess{
		{ClassID: "n1", NodeID: "n1", FieldName: "total", Kind: "write", Line: 12},
		{ClassID: "n1", NodeID: "n2", FieldName: "total", Kind: "read", Line: 22},
		{ClassID: "n1", NodeID: "n2", FieldName: "status", Kind: "write", Line: 25}, // different field — must not show up
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	accesses, err := store.GetFieldAccesses(ctx, "n1", "total")
	if err != nil {
		t.Fatalf("GetFieldAccesses: %v", err)
	}
	if len(accesses) != 2 {
		t.Fatalf("accesses = %+v, want 2", accesses)
	}
	if accesses[0].Line != 12 || accesses[0].Kind != "write" {
		t.Fatalf("accesses[0] = %+v, want line 12 write (ordered by line)", accesses[0])
	}
	if accesses[1].Line != 22 || accesses[1].Kind != "read" {
		t.Fatalf("accesses[1] = %+v, want line 22 read", accesses[1])
	}
}

func TestStoreGetFieldAccessesNoMatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, sampleResult()); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	accesses, err := store.GetFieldAccesses(ctx, "n1", "nonexistentField")
	if err != nil {
		t.Fatalf("GetFieldAccesses: %v", err)
	}
	if len(accesses) != 0 {
		t.Fatalf("expected no accesses, got: %+v", accesses)
	}
}

func TestStoreReplaceIndexClearsOldFieldAccesses(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first := sampleResult()
	first.FieldAccesses = []FieldAccess{{ClassID: "n1", NodeID: "n1", FieldName: "total", Kind: "write", Line: 12}}
	if err := store.ReplaceIndex(ctx, first); err != nil {
		t.Fatalf("first ReplaceIndex: %v", err)
	}

	if err := store.ReplaceIndex(ctx, sampleResult()); err != nil { // no FieldAccesses
		t.Fatalf("second ReplaceIndex: %v", err)
	}

	accesses, err := store.GetFieldAccesses(ctx, "n1", "total")
	if err != nil {
		t.Fatalf("GetFieldAccesses: %v", err)
	}
	if len(accesses) != 0 {
		t.Fatalf("expected accesses cleared after replace, got: %+v", accesses)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run "TestStoreGetFieldAccesses|TestStoreReplaceIndexClearsOldFieldAccesses" -v
```

Expected: FAIL — compile error, `GetFieldAccesses` doesn't exist and `ReplaceIndex` doesn't persist `FieldAccesses` yet.

- [ ] **Step 3: Add the table**

In `internal/codegraph/store.go`, extend the `schema` constant:

```go
CREATE TABLE IF NOT EXISTS field_accesses (
	class_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	field_name TEXT NOT NULL,
	kind TEXT NOT NULL,
	line INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_field_accesses_lookup ON field_accesses(class_id, field_name);
```

(Same "no migration path" acceptance as `2026-08-19-codegraph-brd-enrichment.md`'s new columns — a brand new table via `CREATE TABLE IF NOT EXISTS` only affects fresh per-project DB files; no real deployed data exists yet for this module.)

- [ ] **Step 4: Persist it in `ReplaceIndex` and implement the query**

In `internal/codegraph/store.go`, `ReplaceIndex`: add `"field_accesses"` to the cleared-tables loop, and an insert loop:

```go
	for _, table := range []string{"nodes", "edges", "endpoints", "file_dependencies", "field_accesses"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return fmt.Errorf("codegraph: clear %s: %w", table, err)
		}
	}
```

(If `2026-08-19-codegraph-blast-radius.md` hasn't landed yet, this table list will just be `{"nodes", "edges", "endpoints", "field_accesses"}` — add `field_accesses` to whichever list is already there.)

```go
	for _, fa := range result.FieldAccesses {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO field_accesses (class_id, node_id, field_name, kind, line) VALUES (?, ?, ?, ?, ?)`,
			fa.ClassID, fa.NodeID, fa.FieldName, fa.Kind, fa.Line); err != nil {
			return fmt.Errorf("codegraph: insert field access %q.%q: %w", fa.ClassID, fa.FieldName, err)
		}
	}
```

Add below `GetOutEdges`/`GetInEdges`:

```go
func (s *sqliteStore) GetFieldAccesses(ctx context.Context, classID, fieldName string) ([]FieldAccess, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT class_id, node_id, field_name, kind, line FROM field_accesses WHERE class_id = ? AND field_name = ? ORDER BY line`,
		classID, fieldName)
	if err != nil {
		return nil, fmt.Errorf("codegraph: GetFieldAccesses: %w", err)
	}
	defer rows.Close()

	var out []FieldAccess
	for rows.Next() {
		var fa FieldAccess
		if err := rows.Scan(&fa.ClassID, &fa.NodeID, &fa.FieldName, &fa.Kind, &fa.Line); err != nil {
			return nil, fmt.Errorf("codegraph: scan field access: %w", err)
		}
		out = append(out, fa)
	}
	return out, rows.Err()
}
```

- [ ] **Step 5: Add the `nilStore` stub**

In `internal/codegraph/service.go`:

```go
func (nilStore) GetFieldAccesses(context.Context, string, string) ([]FieldAccess, error) {
	return nil, ErrNoActiveProject
}
```

- [ ] **Step 6: Run the tests and build**

```bash
go test ./internal/codegraph/... -run "TestStoreGetFieldAccesses|TestStoreGetFieldAccessesNoMatch|TestStoreReplaceIndexClearsOldFieldAccesses" -v
go build ./...
```

Expected: all 3 pass; build succeeds (`sqliteStore`/`nilStore` both satisfy `Store` again).

- [ ] **Step 7: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 8: Commit (Task 1 + Task 2 together)**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/store.go internal/codegraph/store_test.go internal/codegraph/service.go
git commit -m "feat(codegraph): field_accesses table + Store.GetFieldAccesses"
```

---

## Task 3: `Service.TraceDataFlow`

**Files:**
- Modify: `internal/codegraph/codegraph.go`
- Modify: `internal/codegraph/service.go`
- Modify: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/codegraph/service_test.go`:

```go
func dataFlowFixture() AnalysisResult {
	return AnalysisResult{
		Nodes: []Node{
			{ID: "cls", Label: "com.acme.Order", Kind: "class", Layer: "unknown", FilePath: "Order.java", Line: 1, EndLine: 50},
			{ID: "m1", Label: "com.acme.Order.setTotal", Kind: "method", Layer: "unknown", FilePath: "Order.java", Line: 10, EndLine: 12},
			{ID: "m2", Label: "com.acme.Order.getTotal", Kind: "method", Layer: "unknown", FilePath: "Order.java", Line: 14, EndLine: 16},
		},
		FieldAccesses: []FieldAccess{
			{ClassID: "cls", NodeID: "m1", FieldName: "total", Kind: "write", Line: 11},
			{ClassID: "cls", NodeID: "m2", FieldName: "total", Kind: "read", Line: 15},
		},
	}
}

func TestServiceTraceDataFlow(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: dataFlowFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	trace, err := svc.TraceDataFlow(context.Background(), "cls", "total")
	if err != nil {
		t.Fatalf("TraceDataFlow: %v", err)
	}
	if trace.SymbolID != "cls" || trace.FieldName != "total" {
		t.Fatalf("trace = %+v", trace)
	}
	if len(trace.Steps) != 2 {
		t.Fatalf("steps = %+v, want 2", trace.Steps)
	}
	if trace.Steps[0].NodeID != "m1" || trace.Steps[0].Kind != "write" || trace.Steps[0].Label != "com.acme.Order.setTotal" {
		t.Fatalf("steps[0] = %+v", trace.Steps[0])
	}
	if trace.Steps[1].NodeID != "m2" || trace.Steps[1].Kind != "read" || trace.Steps[1].Label != "com.acme.Order.getTotal" {
		t.Fatalf("steps[1] = %+v", trace.Steps[1])
	}
}

func TestServiceTraceDataFlowNoAccessesReturnsEmpty(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: dataFlowFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	trace, err := svc.TraceDataFlow(context.Background(), "cls", "nonexistentField")
	if err != nil {
		t.Fatalf("TraceDataFlow: %v", err)
	}
	if len(trace.Steps) != 0 {
		t.Fatalf("steps = %+v, want none", trace.Steps)
	}
}

func TestServiceTraceDataFlowUnknownSymbolFails(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: dataFlowFixture()})
	if err := svc.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	if _, err := svc.TraceDataFlow(context.Background(), "does-not-exist", "total"); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("expected ErrNodeNotFound, got %v", err)
	}
}

func TestServiceTraceDataFlowBeforeOpenProjectFails(t *testing.T) {
	svc := NewService(nilStore{}, fakeAnalyzer{})
	if _, err := svc.TraceDataFlow(context.Background(), "cls", "total"); !errors.Is(err, ErrNoActiveProject) {
		t.Fatalf("expected ErrNoActiveProject, got %v", err)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/codegraph/... -run TestServiceTraceDataFlow -v
```

Expected: FAIL — compile error, `TraceDataFlow`/`DataFlowStep`/`DataFlowTrace` don't exist yet.

- [ ] **Step 3: Add the types**

In `internal/codegraph/codegraph.go`, add near `ApiFlow`:

```go
// DataFlowStep is one method that reads or writes the traced field.
type DataFlowStep struct {
	NodeID   string `json:"nodeId"`
	Label    string `json:"label"`
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"` // "read" | "write"
}

// DataFlowTrace is TraceDataFlow's result: every already-indexed method that
// touches one field on one class.
type DataFlowTrace struct {
	SymbolID  string         `json:"symbolId"`
	FieldName string         `json:"fieldName"`
	Steps     []DataFlowStep `json:"steps"`
}
```

- [ ] **Step 4: Implement `Service.TraceDataFlow`**

In `internal/codegraph/service.go`, add below `GetBlastRadius` (or `GetNeighbors`, wherever the most recently added read method sits):

```go
// TraceDataFlow lists every method already in the index that reads or
// writes fieldName on the class symbolID — a filtered lookup over data the
// analyzer already resolved, not a fresh call-graph walk (an agent wanting
// "who calls the method that writes this field" already has GetNeighbors or
// GetBlastRadius to compose with, once it has one of these NodeIDs). Not
// cached — per the design doc, cheap enough to recompute every call.
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

	steps := make([]DataFlowStep, 0, len(accesses))
	for _, a := range accesses {
		node, err := store.GetNode(ctx, a.NodeID)
		if err != nil {
			if errors.Is(err, ErrNodeNotFound) {
				continue // index drifted between the two reads; skip rather than fail the whole trace
			}
			return DataFlowTrace{}, err
		}
		steps = append(steps, DataFlowStep{
			NodeID:   a.NodeID,
			Label:    node.Label,
			FilePath: node.FilePath,
			Line:     a.Line,
			Kind:     a.Kind,
		})
	}
	return DataFlowTrace{SymbolID: symbolID, FieldName: fieldName, Steps: steps}, nil
}
```

- [ ] **Step 5: Run the tests**

```bash
go test ./internal/codegraph/... -run TestServiceTraceDataFlow -v
```

Expected: all 4 pass.

- [ ] **Step 6: Full package regression**

```bash
go test ./internal/codegraph/... -race
```

- [ ] **Step 7: Commit**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): Service.TraceDataFlow"
```

---

## Task 4: Bridge method

**Files:**
- Modify: `internal/bridge/codegraph.go`
- Modify: `internal/bridge/codegraph_test.go`

No new capability grant — `codegraph:read`/`project:active`, already granted.

- [ ] **Step 1: Write the failing test**

Add to `internal/bridge/codegraph_test.go`:

```go
func TestCodeGraphTraceDataFlow(t *testing.T) {
	s := newTestService(t)
	result := codegraph.AnalysisResult{
		Nodes: []codegraph.Node{
			{ID: "cls", Label: "com.acme.Order", Kind: "class", Layer: "unknown", FilePath: "Order.java", Line: 1, EndLine: 50},
			{ID: "m1", Label: "com.acme.Order.setTotal", Kind: "method", Layer: "unknown", FilePath: "Order.java", Line: 10, EndLine: 12},
		},
		FieldAccesses: []codegraph.FieldAccess{
			{ClassID: "cls", NodeID: "m1", FieldName: "total", Kind: "write", Line: 11},
		},
	}
	s.container.CodeGraph = codegraph.NewService(codegraph.NewNilStore(), fakeCodeGraphAnalyzer{result: result})
	if err := s.container.CodeGraph.SetProjectPath(t.TempDir() + "/index.db"); err != nil {
		t.Fatalf("SetProjectPath: %v", err)
	}
	if err := s.container.CodeGraph.OpenProject(context.Background(), "/fake/project"); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	trace, err := s.CodeGraphTraceDataFlow("cls", "total")
	if err != nil {
		t.Fatalf("CodeGraphTraceDataFlow: %v", err)
	}
	if len(trace.Steps) != 1 || trace.Steps[0].Kind != "write" {
		t.Fatalf("unexpected trace: %+v", trace)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/bridge/... -run TestCodeGraphTraceDataFlow -v
```

Expected: FAIL — compile error.

- [ ] **Step 3: Implement**

In `internal/bridge/codegraph.go`:

```go
func (s *AppService) CodeGraphTraceDataFlow(symbolID, fieldName string) (codegraph.DataFlowTrace, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"action":     "trace_data_flow",
		"symbol_id":  symbolID,
		"field_name": fieldName,
	}, func(ctx context.Context) (any, error) {
		return s.container.CodeGraph.TraceDataFlow(ctx, symbolID, fieldName)
	})
	if err != nil {
		return codegraph.DataFlowTrace{}, err
	}
	return result.(codegraph.DataFlowTrace), nil
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
git commit -m "feat(codegraph): CodeGraphTraceDataFlow bridge method"
```

---

## Task 5: MCP tool

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/tools_codegraph.go`
- Modify: `internal/mcp/tools_codegraph_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/mcp/tools_codegraph_test.go` (extend `fakeCodeGraphAPI` with a `dataFlowTrace codegraph.DataFlowTrace` field):

```go
func TestToolCodeGraphTraceDataFlow(t *testing.T) {
	deps := Deps{
		Caller: &fakeCaller{},
		CodeGraph: fakeCodeGraphAPI{dataFlowTrace: codegraph.DataFlowTrace{
			SymbolID: "cls", FieldName: "total",
			Steps: []codegraph.DataFlowStep{{NodeID: "m1", Kind: "write"}},
		}},
	}
	s := NewServer(deps)
	_, v, err := s.toolCodeGraphTraceDataFlow(context.Background(), nil, codeGraphTraceDataFlowInput{SymbolID: "cls", FieldName: "total"})
	if err != nil {
		t.Fatalf("toolCodeGraphTraceDataFlow: %v", err)
	}
	trace, ok := v.(codegraph.DataFlowTrace)
	if !ok || len(trace.Steps) != 1 || trace.Steps[0].Kind != "write" {
		t.Fatalf("unexpected result: %#v", v)
	}
}
```

```go
func (f fakeCodeGraphAPI) TraceDataFlow(ctx context.Context, symbolID, fieldName string) (codegraph.DataFlowTrace, error) {
	return f.dataFlowTrace, f.err
}
```

- [ ] **Step 2: Run to confirm failure**

```bash
go test ./internal/mcp/... -run TestToolCodeGraphTraceDataFlow -v
```

Expected: FAIL — compile error.

- [ ] **Step 3: Extend the interface and register the tool**

In `internal/mcp/server.go`, add to `CodeGraphAPI`:

```go
	TraceDataFlow(ctx context.Context, symbolID, fieldName string) (codegraph.DataFlowTrace, error)
```

In `internal/mcp/tools_codegraph.go`:

```go
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "codegraph.traceDataFlow",
		Description: "List methods that read or write a field on a class, using the already-resolved index",
		Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true},
	}, s.toolCodeGraphTraceDataFlow)
```

```go
type codeGraphTraceDataFlowInput struct {
	SymbolID  string `json:"symbolId"`
	FieldName string `json:"fieldName"`
}

func (s *Server) toolCodeGraphTraceDataFlow(ctx context.Context, _ *sdk.CallToolRequest, in codeGraphTraceDataFlowInput) (*sdk.CallToolResult, any, error) {
	v, err := s.deps.Caller.Call(ctx, "builtin.codegraph", "codegraph:read", "project:active", meta("trace_data_flow", map[string]any{"symbol_id": in.SymbolID, "field_name": in.FieldName}), func(context.Context) (any, error) {
		return s.deps.CodeGraph.TraceDataFlow(ctx, in.SymbolID, in.FieldName)
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
git commit -m "feat(codegraph): expose traceDataFlow via MCP"
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

**Does not modify the Java analyzer CLI** to actually detect field reads/writes — see this plan's "Not built here" section above for the verified `AssignExpr`/`UnaryExpr`/`FieldAccessExpr` shapes whoever extends that plan should ground the real implementation against. **No transitive caller walk baked into `TraceDataFlow` itself** — deliberately scoped to direct accesses only, per the "Scope reading" section; compose with `GetNeighbors`/`GetBlastRadius` for that. **No pagination/ceiling** — the natural result size (methods touching one field on one class) doesn't need it; revisit only if a real project proves otherwise. **Field access across subclasses** (a `protected` field accessed from a subclass method) is whatever the Java analyzer's eventual symbol resolution naturally produces — this plan doesn't add special-case logic for inheritance, it just persists and queries whatever `ClassID`/`FieldName` pairs show up in `AnalysisResult.FieldAccesses`.

This closes out "Data flow analysis & BRD (add-on v1)" entirely (combined with `2026-08-19-codegraph-brd-enrichment.md`). Remaining CodeGraph phases: the Canvas UI (deferred from `2026-08-19-codegraph-get-neighbors.md`), Terminal cục bộ (Phase 6), Plan-first flow (Phase 7).
