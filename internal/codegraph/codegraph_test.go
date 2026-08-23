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

func TestValidateIgnoresUnresolvedCallSites(t *testing.T) {
	// An unresolved call site has an empty To on purpose. Validate rejects
	// edges pointing at unknown nodes; it must not apply that rule to call
	// sites, or every project that calls the JDK would fail to index.
	result := AnalysisResult{
		Nodes: []Node{{ID: "a.B.c()", Label: "B.c", Kind: "method"}},
		CallSites: []CallSite{
			{From: "a.B.c()", To: "", Line: 12, Unresolved: "library"},
		},
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsCallSitePointingAtUnknownNode(t *testing.T) {
	// A call site that DOES name a callee is held to the same standard as an
	// edge — a dangling id would make the trace walk into nothing.
	result := AnalysisResult{
		Nodes: []Node{{ID: "a.B.c()", Label: "B.c", Kind: "method"}},
		CallSites: []CallSite{
			{From: "a.B.c()", To: "a.Ghost.x()", Line: 12},
		},
	}
	if err := result.Validate(); err == nil {
		t.Fatal("Validate() = nil, want an error about the unknown node")
	}
}

func TestValidateRejectsCallSiteFromUnknownNode(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{{ID: "a.B.c()", Label: "B.c", Kind: "method"}},
		CallSites: []CallSite{
			{From: "a.Ghost.x()", To: "a.B.c()", Line: 12},
		},
	}
	if err := result.Validate(); err == nil {
		t.Fatal("Validate() = nil, want an error about the unknown caller")
	}
}
