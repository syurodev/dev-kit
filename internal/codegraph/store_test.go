// internal/codegraph/store_test.go
package codegraph

import (
	"context"
	"database/sql"
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

func TestFindNodeAtLineReturnsNarrowestEnclosingNode(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "class1", Label: "com.acme.OrderService", Kind: "class", Layer: "service", FilePath: "OrderService.java", Line: 1, EndLine: 50},
			{
				ID: "method1", Label: "com.acme.OrderService.create", Kind: "method", Layer: "service",
				FilePath: "OrderService.java", Line: 10, EndLine: 20,
				Summary: "Creates order.",
				Params:  []Param{{Name: "req", Type: "Request"}},
			},
		},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	node, ok, err := store.FindNodeAtLine(ctx, "OrderService.java", 15)
	if err != nil {
		t.Fatalf("FindNodeAtLine: %v", err)
	}
	if !ok {
		t.Fatal("expected a match")
	}
	if node.ID != "method1" {
		t.Fatalf("node.ID = %q, want method1 (the narrower method, not the enclosing class)", node.ID)
	}
	if node.Summary != "Creates order." {
		t.Fatalf("node.Summary = %q, want 'Creates order.'", node.Summary)
	}
	if len(node.Params) != 1 || node.Params[0].Name != "req" {
		t.Fatalf("unexpected node.Params: %+v", node.Params)
	}
}

func TestFindNodeAtLineNoMatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, AnalysisResult{
		Nodes: []Node{{ID: "n1", Label: "com.acme.Foo", Kind: "class", Layer: "unknown", FilePath: "Foo.java", Line: 1, EndLine: 10}},
	}); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	_, ok, err := store.FindNodeAtLine(ctx, "Foo.java", 100)
	if err != nil {
		t.Fatalf("FindNodeAtLine: %v", err)
	}
	if ok {
		t.Fatal("expected no match for a line outside every node's range")
	}
}

func TestFindNodeAtLineWrongFile(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, AnalysisResult{
		Nodes: []Node{{ID: "n1", Label: "com.acme.Foo", Kind: "class", Layer: "unknown", FilePath: "Foo.java", Line: 1, EndLine: 10}},
	}); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	_, ok, err := store.FindNodeAtLine(ctx, "Bar.java", 5)
	if err != nil {
		t.Fatalf("FindNodeAtLine: %v", err)
	}
	if ok {
		t.Fatal("expected no match for a different file")
	}
}

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

func TestStoreMigratesOldSchemaWithoutSummaryOrParams(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	// Create legacy 7-column schema
	legacySchema := `
CREATE TABLE nodes (
	id TEXT PRIMARY KEY,
	label TEXT NOT NULL,
	kind TEXT NOT NULL,
	layer TEXT NOT NULL,
	file_path TEXT NOT NULL,
	line INTEGER NOT NULL,
	end_line INTEGER NOT NULL
);
CREATE TABLE edges (
	from_id TEXT NOT NULL,
	to_id TEXT NOT NULL,
	kind TEXT NOT NULL
);
CREATE TABLE endpoints (
	id TEXT PRIMARY KEY,
	http_method TEXT NOT NULL,
	path TEXT NOT NULL,
	entry_node TEXT NOT NULL
);
`
	if _, err := db.Exec(legacySchema); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	// Insert legacy row
	if _, err := db.Exec(`INSERT INTO nodes VALUES ('n1', 'Foo.bar', 'method', 'service', 'Foo.java', 1, 10)`); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	db.Close()

	// Open with newSQLiteStore — should migrate missing summary and params columns
	store, err := newSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("newSQLiteStore on legacy db: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// Read existing legacy row
	node, err := store.GetNode(ctx, "n1")
	if err != nil {
		t.Fatalf("GetNode on migrated row: %v", err)
	}
	if node.ID != "n1" || node.Summary != "" || len(node.Params) != 0 {
		t.Fatalf("unexpected node values: %+v", node)
	}

	// Overwrite with new ReplaceIndex
	newResult := AnalysisResult{
		Nodes: []Node{
			{
				ID: "n2", Label: "Foo.baz", Kind: "method", Layer: "service",
				FilePath: "Foo.java", Line: 12, EndLine: 20,
				Summary: "Performs baz operation.",
				Params:  []Param{{Name: "count", Type: "int"}},
			},
		},
	}
	if err := store.ReplaceIndex(ctx, newResult); err != nil {
		t.Fatalf("ReplaceIndex on migrated store: %v", err)
	}

	node2, err := store.GetNode(ctx, "n2")
	if err != nil {
		t.Fatalf("GetNode on new row: %v", err)
	}
	if node2.Summary != "Performs baz operation." || len(node2.Params) != 1 || node2.Params[0].Name != "count" {
		t.Fatalf("unexpected node2: %+v", node2)
	}
}

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

func samplePlan() Plan {
	return Plan{
		Description: "Refund flow",
		State:       PlanStateDraft,
		Nodes: []PlanNode{
			{PlanNodeID: "pn-1", Label: "Nhận yêu cầu hoàn tiền", Description: "controller entry", FQN: "com.acme.refund.RefundController.create", Kind: "method"},
			{PlanNodeID: "pn-2", Label: "Xử lý hoàn tiền", Description: "core logic", FQN: "com.acme.refund.RefundService.process", Kind: "method"},
		},
		Edges: []PlanEdge{
			{From: "pn-1", To: "pn-2", Condition: "chỉ khi type == REFUND"},
		},
	}
}

func TestStoreSavePlanThenGetPlan(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.SavePlan(ctx, samplePlan()); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	got, err := store.GetPlan(ctx)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Description != "Refund flow" || got.State != PlanStateDraft {
		t.Fatalf("plan = %+v", got)
	}
	if len(got.Nodes) != 2 || got.Nodes[0].PlanNodeID != "pn-1" || got.Nodes[1].FQN != "com.acme.refund.RefundService.process" {
		t.Fatalf("nodes = %+v", got.Nodes)
	}
	if len(got.Edges) != 1 || got.Edges[0].Condition != "chỉ khi type == REFUND" {
		t.Fatalf("edges = %+v", got.Edges)
	}
}

func TestStoreGetPlanWhenNoneFails(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.GetPlan(context.Background()); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("GetPlan = %v, want ErrNoPlan", err)
	}
}

func TestStoreSavePlanReplacesPrevious(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SavePlan(ctx, samplePlan()); err != nil {
		t.Fatalf("first SavePlan: %v", err)
	}

	second := Plan{
		Description: "Something else",
		State:       PlanStateDraft,
		Nodes:       []PlanNode{{PlanNodeID: "pn-9", Label: "Only node", FQN: "com.acme.Other.run", Kind: "method"}},
	}
	if err := store.SavePlan(ctx, second); err != nil {
		t.Fatalf("second SavePlan: %v", err)
	}

	got, err := store.GetPlan(ctx)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Description != "Something else" || len(got.Nodes) != 1 || got.Nodes[0].PlanNodeID != "pn-9" {
		t.Fatalf("plan = %+v", got)
	}
	if len(got.Edges) != 0 {
		t.Fatalf("expected the previous plan's edges cleared, got %+v", got.Edges)
	}
}

func TestStoreSetPlanState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SavePlan(ctx, samplePlan()); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	if err := store.SetPlanState(ctx, PlanStateApproved); err != nil {
		t.Fatalf("SetPlanState: %v", err)
	}
	got, err := store.GetPlan(ctx)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.State != PlanStateApproved {
		t.Fatalf("state = %q, want approved", got.State)
	}
}

func TestStoreSetPlanNodeFQN(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SavePlan(ctx, samplePlan()); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	if err := store.SetPlanNodeFQN(ctx, "pn-2", "com.acme.refund.RefundService.handle"); err != nil {
		t.Fatalf("SetPlanNodeFQN: %v", err)
	}
	got, err := store.GetPlan(ctx)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Nodes[1].FQN != "com.acme.refund.RefundService.handle" {
		t.Fatalf("fqn = %q", got.Nodes[1].FQN)
	}

	if err := store.SetPlanNodeFQN(ctx, "does-not-exist", "com.acme.X.y"); !errors.Is(err, ErrPlanNodeNotFound) {
		t.Fatalf("SetPlanNodeFQN on unknown node = %v, want ErrPlanNodeNotFound", err)
	}
}

func TestStoreResolveFQNPrefersIDWhenParamsGiven(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.A.foo(java.lang.String)", Label: "com.acme.A.foo", Kind: "method", Layer: "service", FilePath: "A.java", Line: 1, EndLine: 2},
			{ID: "com.acme.A.foo(int)", Label: "com.acme.A.foo", Kind: "method", Layer: "service", FilePath: "A.java", Line: 4, EndLine: 5},
		},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	// With parameter types: exact id match, unambiguous.
	node, ok, err := store.ResolveFQN(ctx, "com.acme.A.foo(int)")
	if err != nil {
		t.Fatalf("ResolveFQN: %v", err)
	}
	if !ok || node.ID != "com.acme.A.foo(int)" {
		t.Fatalf("node = %+v, ok = %v", node, ok)
	}

	// Without parameter types: label match, lowest id wins for determinism.
	node, ok, err = store.ResolveFQN(ctx, "com.acme.A.foo")
	if err != nil {
		t.Fatalf("ResolveFQN: %v", err)
	}
	if !ok || node.ID != "com.acme.A.foo(int)" {
		t.Fatalf("expected the lowest-sorted overload id, got %+v", node)
	}

	if _, ok, err = store.ResolveFQN(ctx, "com.acme.Nope.bar"); err != nil || ok {
		t.Fatalf("expected no match, got ok=%v err=%v", ok, err)
	}
}

func TestCallSitesRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	result := AnalysisResult{
		Nodes: []Node{
			{ID: "a.B.c()", Label: "B.c", Kind: "method", FilePath: "B.java", Line: 3},
			{ID: "a.D.e()", Label: "D.e", Kind: "method", FilePath: "D.java", Line: 9},
		},
		CallSites: []CallSite{
			{From: "a.B.c()", To: "a.D.e()", Line: 5},
			{From: "a.B.c()", To: "", Line: 6, Unresolved: "library"},
			{From: "a.B.c()", To: "a.D.e()", Line: 7},
		},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	got, err := store.GetCallSites(ctx, "a.B.c()")
	if err != nil {
		t.Fatalf("GetCallSites: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (repeated calls must not be deduped)", len(got))
	}
	for i, want := range []int{5, 6, 7} {
		if got[i].Line != want {
			t.Errorf("got[%d].Line = %d, want %d — call sites must come back in line order", i, got[i].Line, want)
		}
	}
	if got[1].Unresolved != "library" {
		t.Errorf("got[1].Unresolved = %q, want %q", got[1].Unresolved, "library")
	}
	if got[1].To != "" {
		t.Errorf("got[1].To = %q, want empty", got[1].To)
	}
}

func TestReplaceIndexClearsCallSites(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	first := AnalysisResult{
		Nodes:     []Node{{ID: "a.B.c()", Label: "B.c", Kind: "method"}},
		CallSites: []CallSite{{From: "a.B.c()", To: "", Line: 5, Unresolved: "library"}},
	}
	if err := store.ReplaceIndex(ctx, first); err != nil {
		t.Fatalf("ReplaceIndex(first): %v", err)
	}
	second := AnalysisResult{
		Nodes: []Node{{ID: "a.B.c()", Label: "B.c", Kind: "method"}},
	}
	if err := store.ReplaceIndex(ctx, second); err != nil {
		t.Fatalf("ReplaceIndex(second): %v", err)
	}

	// A re-index that finds no calls must leave none behind, or a stale row
	// would send the trace walking into a method the code no longer calls.
	got, err := store.GetCallSites(ctx, "a.B.c()")
	if err != nil {
		t.Fatalf("GetCallSites: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0 — re-indexing must clear the old call sites", len(got))
	}
}

func TestStoreListFieldsByClassAndByMethod(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	result := sampleResult()
	result.FieldAccesses = []FieldAccess{
		{ClassID: "n1", NodeID: "n1", FieldName: "total", Kind: "write", Line: 12},
		{ClassID: "n1", NodeID: "n2", FieldName: "total", Kind: "read", Line: 22},
		{ClassID: "n1", NodeID: "n2", FieldName: "status", Kind: "write", Line: 25},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	// By class: every field the class has, with both accesses of "total" counted.
	byClass, err := store.ListFields(ctx, "n1")
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(byClass) != 2 {
		t.Fatalf("byClass = %+v, want 2 fields", byClass)
	}
	if byClass[0].FieldName != "status" || byClass[1].FieldName != "total" {
		t.Fatalf("byClass = %+v, want alphabetical order", byClass)
	}
	if byClass[1].Reads != 1 || byClass[1].Writes != 1 {
		t.Fatalf("total = %+v, want 1 read 1 write", byClass[1])
	}
	if byClass[0].ClassID != "n1" {
		t.Fatalf("status.ClassID = %q, want n1 — the trace needs it to call TraceDataFlow", byClass[0].ClassID)
	}

	// By method: only the fields that method touches.
	byMethod, err := store.ListFields(ctx, "n2")
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(byMethod) != 2 {
		t.Fatalf("byMethod = %+v, want 2", byMethod)
	}
	if byMethod[0].Reads != 0 || byMethod[0].Writes != 1 {
		t.Fatalf("status seen from n2 = %+v, want 0 read 1 write", byMethod[0])
	}
}

func TestStoreListFieldsNoAccesses(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.ReplaceIndex(ctx, sampleResult()); err != nil {
		t.Fatalf("ReplaceIndex: %v", err)
	}

	fields, err := store.ListFields(ctx, "n1")
	if err != nil {
		t.Fatalf("ListFields: %v", err)
	}
	if len(fields) != 0 {
		t.Fatalf("expected no fields, got %+v", fields)
	}
}
