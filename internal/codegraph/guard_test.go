package codegraph

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// The guard on an edge makes it through the store, in both directions of
// reading, and an unguarded edge stays unguarded.
func TestStoreKeepsEdgeGuards(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	err := store.ReplaceIndex(ctx, AnalysisResult{
		Nodes: []Node{method("a"), method("b"), method("c")},
		Edges: []Edge{
			{From: "a", To: "b", Kind: "call", Guard: "moduleBased"},
			{From: "a", To: "c", Kind: "call"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := store.GetOutEdges(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	guards := map[string]string{}
	for _, e := range out {
		guards[e.To] = e.Guard
	}
	if guards["b"] != "moduleBased" || guards["c"] != "" || len(guards) != 2 {
		t.Fatalf("out edges = %+v", out)
	}
	in, _ := store.GetInEdges(ctx, "b")
	if len(in) != 1 || in[0].Guard != "moduleBased" {
		t.Fatalf("in edges = %+v", in)
	}
	adjacent, _ := store.GetAdjacentEdges(ctx, []string{"b"})
	if len(adjacent) != 1 || adjacent[0].Guard != "moduleBased" {
		t.Fatalf("adjacent edges = %+v", adjacent)
	}
}

// An index written before edges had a guard opens, reads as unguarded, and
// takes a guarded index on the next re-index.
func TestStoreAddsTheGuardColumnToAnOldIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE edges (from_id TEXT NOT NULL, to_id TEXT NOT NULL, kind TEXT NOT NULL);
INSERT INTO edges VALUES ('a', 'b', 'call');`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	store, err := newSQLiteStore(path)
	if err != nil {
		t.Fatalf("open old index: %v", err)
	}
	defer store.Close()
	edges, err := store.GetOutEdges(t.Context(), "a")
	if err != nil || len(edges) != 1 || edges[0].Guard != "" {
		t.Fatalf("edges = %+v, err = %v", edges, err)
	}
	err = store.ReplaceIndex(t.Context(), AnalysisResult{
		Nodes: []Node{method("a"), method("b")},
		Edges: []Edge{{From: "a", To: "b", Kind: "call", Guard: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	edges, _ = store.GetOutEdges(t.Context(), "a")
	if len(edges) != 1 || edges[0].Guard != "x" {
		t.Fatalf("edges after re-index = %+v", edges)
	}
}

// The flow says, per step, how deep it is and what the caller's call to it
// depends on, so the flat list reads as a tree with its branches marked.
func TestApiFlowShowsDepthAndGuards(t *testing.T) {
	svc := openWith(t, AnalysisResult{
		Nodes: []Node{method("root"), method("byModule"), method("byRole"), method("audit"), method("leaf")},
		Edges: []Edge{
			{From: "root", To: "byModule", Kind: "call", Guard: "moduleBased"},
			{From: "root", To: "byRole", Kind: "call", Guard: "!moduleBased"},
			{From: "byModule", To: "audit", Kind: "call"},
			{From: "audit", To: "leaf", Kind: "call", Guard: "audit.enabled"},
		},
		EntryPoints: []EntryPoint{{ID: "GET /r", Verb: "GET", Target: "/r", EntryNode: "root"}},
	})
	flow, err := svc.GetApiFlow(t.Context(), "GET /r")
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		label string
		depth int
		guard string
	}
	var got []row
	for _, s := range flow.Steps {
		got = append(got, row{s.Label, s.Depth, s.Guard})
	}
	want := []row{
		{"root", 0, ""},
		{"byModule", 1, "moduleBased"},
		{"audit", 2, ""},
		{"leaf", 3, "audit.enabled"},
		{"byRole", 1, "!moduleBased"},
	}
	if len(got) != len(want) {
		t.Fatalf("steps = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d = %+v, want %+v (all: %+v)", i, got[i], want[i], got)
		}
	}
}
