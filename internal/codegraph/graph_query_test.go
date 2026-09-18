package codegraph

import (
	"fmt"
	"strings"
	"testing"
)

func openWith(t *testing.T, result AnalysisResult) *Service {
	t.Helper()
	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(t.Context(), "/fake/project"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	return svc
}

func method(id string) Node {
	return Node{ID: id, Label: id, Kind: "method", Layer: "service", FilePath: id + ".java", Line: 1, EndLine: 2}
}

// The analyzer can emit the same node ID twice (two declarations it resolves
// to one signature). That used to fail the whole index on the primary key.
func TestReplaceIndexKeepsTheFirstOfDuplicateNodes(t *testing.T) {
	store := newTestStore(t)
	first := method("dup")
	first.Label = "first"
	second := method("dup")
	second.Label = "second"
	result := AnalysisResult{
		Nodes: []Node{first, second, method("other")},
		Edges: []Edge{{From: "other", To: "dup", Kind: "call"}},
	}
	if err := store.ReplaceIndex(t.Context(), result); err != nil {
		t.Fatalf("a duplicate node failed the whole index: %v", err)
	}
	got, err := store.GetNode(t.Context(), "dup")
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "first" {
		t.Fatalf("label = %q, want the first-seen node to win", got.Label)
	}
	edges, err := store.GetInEdges(t.Context(), "dup")
	if err != nil || len(edges) != 1 {
		t.Fatalf("edges into the kept node: %+v %v", edges, err)
	}
}

func TestSearchSymbolTreatsWildcardsLiterallyAndIsCapped(t *testing.T) {
	store := newTestStore(t)
	nodes := []Node{method("get_name"), method("getXname"), method("pct%done")}
	for i := 0; i < maxSymbolMatches+50; i++ {
		nodes = append(nodes, method(fmt.Sprintf("bulk%04d", i)))
	}
	if err := store.ReplaceIndex(t.Context(), AnalysisResult{Nodes: nodes}); err != nil {
		t.Fatal(err)
	}

	matches, err := store.SearchSymbol(t.Context(), "get_name")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].NodeID != "get_name" {
		t.Fatalf("underscore matched as a wildcard: %+v", matches)
	}
	matches, err = store.SearchSymbol(t.Context(), "%")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].NodeID != "pct%done" {
		t.Fatalf("percent matched as a wildcard: %d matches", len(matches))
	}
	matches, err = store.SearchSymbol(t.Context(), "bulk")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != maxSymbolMatches {
		t.Fatalf("matches = %d, want the cap of %d", len(matches), maxSymbolMatches)
	}
}

func TestStoreUsesWALWithABusyTimeout(t *testing.T) {
	store := newTestStore(t)
	var mode string
	if err := store.db.QueryRowContext(t.Context(), `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
	var timeout int
	if err := store.db.QueryRowContext(t.Context(), `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout < 1000 {
		t.Fatalf("busy_timeout = %d, want a real wait", timeout)
	}
}

// A single depth-first pass marked a method visited the first time it was
// reached, however deep. Reached first at the depth limit, its callees were
// cut — and the short route to it was then ignored as already visited.
func TestApiFlowReachesMethodsByTheirShortestRoute(t *testing.T) {
	nodes := []Node{method("root"), method("x"), method("y")}
	edges := []Edge{}
	prev := "root"
	for i := 1; i < maxFlowDepth; i++ { // root -> c1 -> ... -> c31 -> x puts x at depth 32
		id := fmt.Sprintf("c%02d", i)
		nodes = append(nodes, method(id))
		edges = append(edges, Edge{From: prev, To: id, Kind: "call"})
		prev = id
	}
	edges = append(edges,
		Edge{From: prev, To: "x", Kind: "call"},
		Edge{From: "root", To: "x", Kind: "call"}, // ...and also one call from the root
		Edge{From: "x", To: "y", Kind: "call"},
	)
	svc := openWith(t, AnalysisResult{
		Nodes:     nodes,
		Edges:     edges,
		Endpoints: []Endpoint{{ID: "GET /r", HTTPMethod: "GET", Path: "/r", EntryNode: "root"}},
	})
	flow, err := svc.GetApiFlow(t.Context(), "GET /r")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, s := range flow.Steps {
		seen[s.Label]++
	}
	if seen["y"] != 1 {
		t.Fatalf("y, two calls from the endpoint, was dropped or duplicated: %v", seen)
	}
	if len(flow.Steps) != len(nodes) {
		t.Fatalf("steps = %d, want every one of the %d methods exactly once", len(flow.Steps), len(nodes))
	}
}

func TestApiFlowIsCappedInSize(t *testing.T) {
	nodes := []Node{method("root")}
	var edges []Edge
	for i := 0; i < maxFlowSteps+100; i++ {
		id := fmt.Sprintf("leaf%04d", i)
		nodes = append(nodes, method(id))
		edges = append(edges, Edge{From: "root", To: id, Kind: "call"})
	}
	svc := openWith(t, AnalysisResult{
		Nodes:     nodes,
		Edges:     edges,
		Endpoints: []Endpoint{{ID: "GET /r", HTTPMethod: "GET", Path: "/r", EntryNode: "root"}},
	})
	flow, err := svc.GetApiFlow(t.Context(), "GET /r")
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Steps) != maxFlowSteps {
		t.Fatalf("steps = %d, want the cap of %d", len(flow.Steps), maxFlowSteps)
	}
}

func TestNeighborsStopAtTheVisitBudget(t *testing.T) {
	nodes := []Node{method("root")}
	var edges []Edge
	for i := 0; i < maxNeighborVisit+50; i++ {
		id := fmt.Sprintf("n%05d", i)
		nodes = append(nodes, method(id))
		edges = append(edges, Edge{From: "root", To: id, Kind: "call"})
	}
	svc := openWith(t, AnalysisResult{Nodes: nodes, Edges: edges})

	first, err := svc.GetNeighbors(t.Context(), "root", 1, NeighborFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Truncated || first.Continuation == "" {
		t.Fatalf("first page: truncated=%v continuation=%q", first.Truncated, first.Continuation)
	}
	// The last page of the walk holds its final node and offers no more:
	// the neighborhood stopped at the budget, not at the graph's edge.
	last, err := svc.GetNeighbors(t.Context(), "root", 1, NeighborFilter{Continuation: fmt.Sprint(maxNeighborVisit - 1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Nodes) != 1 || last.Continuation != "" {
		t.Fatalf("walk was not cut at %d nodes: last page %d nodes, continuation %q", maxNeighborVisit, len(last.Nodes), last.Continuation)
	}
	if !last.Truncated {
		t.Fatal("the last page must still say the walk was truncated")
	}
}

// A chain of diamonds whose methods all touch the field: walked once per path,
// the trace doubles with every diamond. Walked once per method, it is linear.
func TestTraceDataFlowExpandsEachMethodOnce(t *testing.T) {
	const diamonds = 3 // two calls per diamond: 6 deep, inside maxDataFlowDepth
	nodes := []Node{{ID: "cls", Label: "Cls", Kind: "class"}}
	var sites []CallSite
	var accesses []FieldAccess
	add := func(id string) {
		nodes = append(nodes, method(id))
		accesses = append(accesses, FieldAccess{ClassID: "cls", NodeID: id, FieldName: "f", Kind: "read", Line: 1})
	}
	add("top0")
	for i := 0; i < diamonds; i++ {
		top, left, right, next := fmt.Sprintf("top%d", i), fmt.Sprintf("l%d", i), fmt.Sprintf("r%d", i), fmt.Sprintf("top%d", i+1)
		add(left)
		add(right)
		add(next)
		sites = append(sites,
			CallSite{From: top, To: left, Line: 2},
			CallSite{From: top, To: right, Line: 3},
			CallSite{From: left, To: next, Line: 2},
			CallSite{From: right, To: next, Line: 2},
		)
	}
	svc := openWith(t, AnalysisResult{Nodes: nodes, FieldAccesses: accesses, CallSites: sites})
	trace, err := svc.TraceDataFlow(t.Context(), "cls", "f")
	if err != nil {
		t.Fatal(err)
	}
	reads, repeats := map[string]int{}, 0
	for _, s := range trace.Steps {
		if s.Kind == "read" {
			reads[s.NodeID]++
		}
		if s.Unresolved == "repeat" {
			repeats++
		}
	}
	for id, n := range reads {
		if n != 1 {
			t.Fatalf("%s traced %d times, want once", id, n)
		}
	}
	if len(reads) != 1+3*diamonds {
		t.Fatalf("traced %d methods, want all %d", len(reads), 1+3*diamonds)
	}
	if repeats != diamonds {
		t.Fatalf("repeat markers = %d, want one per diamond's second arm", repeats)
	}
}
