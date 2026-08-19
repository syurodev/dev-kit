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
