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
			{ID: "method1", Label: "com.acme.OrderService.create", Kind: "method", Layer: "service", FilePath: "OrderService.java", Line: 10, EndLine: 20},
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
