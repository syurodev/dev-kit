// internal/codegraph/definitions_test.go
package codegraph

import (
	"context"
	"testing"
)

// definitionIDs is a small assertion helper: FindDefinitions' contract only
// promises a set (dedup by NodeID), so tests compare IDs rather than order
// except where ranking itself is under test.
func definitionIDs(defs []Definition) []string {
	ids := make([]string, len(defs))
	for i, d := range defs {
		ids[i] = d.NodeID
	}
	return ids
}

func containsID(defs []Definition, id string) bool {
	for _, d := range defs {
		if d.NodeID == id {
			return true
		}
	}
	return false
}

func TestFindDefinitionsCallSiteHit(t *testing.T) {
	// Real id/label shapes, per ProjectAnalyzer.addMethodNode: a method's id is
	// "<classId>.<signature>" (parens and params live only in the id), its
	// label is "<classId>.<methodName>" (no signature) — matchesIdentifier
	// relies on exactly this split.
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderService", Label: "com.acme.OrderService", Kind: "class", Layer: "service", FilePath: "OrderService.java", Line: 1, EndLine: 50},
			{ID: "com.acme.OrderService.create(Request)", Label: "com.acme.OrderService.create", Kind: "method", Layer: "service", FilePath: "OrderService.java", Line: 10, EndLine: 20},
			{ID: "com.acme.OrderService.validate(Request)", Label: "com.acme.OrderService.validate", Kind: "method", Layer: "service", FilePath: "OrderService.java", Line: 40, EndLine: 42},
		},
		CallSites: []CallSite{
			{From: "com.acme.OrderService.create(Request)", To: "com.acme.OrderService.validate(Request)", Line: 15},
		},
	}
	svc := openWith(t, result)

	defs, err := svc.FindDefinitions(context.Background(), "OrderService.java", 15, "validate")
	if err != nil {
		t.Fatalf("FindDefinitions: %v", err)
	}
	if len(defs) != 1 || defs[0].NodeID != "com.acme.OrderService.validate(Request)" {
		t.Fatalf("defs = %+v, want exactly the call site's target", defs)
	}
	if defs[0].Kind != "method" || defs[0].FilePath != "OrderService.java" || defs[0].Line != 40 {
		t.Fatalf("defs[0] = %+v, want the target's own location", defs[0])
	}
}

func TestFindDefinitionsTwoCallsOnOneLineBothReturned(t *testing.T) {
	// "helper(1); helper(\"x\");" on one line: two call_sites rows, two
	// distinct overloads, same line. Both must come back.
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderService", Label: "com.acme.OrderService", Kind: "class", FilePath: "OrderService.java", Line: 1, EndLine: 50},
			{ID: "com.acme.OrderService.create(Request)", Label: "com.acme.OrderService.create", Kind: "method", FilePath: "OrderService.java", Line: 10, EndLine: 20},
			{ID: "com.acme.OrderService.helper(int)", Label: "com.acme.OrderService.helper", Kind: "method", FilePath: "OrderService.java", Line: 25, EndLine: 26},
			{ID: "com.acme.OrderService.helper(String)", Label: "com.acme.OrderService.helper", Kind: "method", FilePath: "OrderService.java", Line: 27, EndLine: 28},
		},
		CallSites: []CallSite{
			{From: "com.acme.OrderService.create(Request)", To: "com.acme.OrderService.helper(int)", Line: 15},
			{From: "com.acme.OrderService.create(Request)", To: "com.acme.OrderService.helper(String)", Line: 15},
		},
	}
	svc := openWith(t, result)

	defs, err := svc.FindDefinitions(context.Background(), "OrderService.java", 15, "helper")
	if err != nil {
		t.Fatalf("FindDefinitions: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("defs = %+v, want both overloads called on that line", defs)
	}
	if !containsID(defs, "com.acme.OrderService.helper(int)") || !containsID(defs, "com.acme.OrderService.helper(String)") {
		t.Fatalf("defs = %+v, missing an overload", defs)
	}
}

func TestFindDefinitionsCallSiteSkipsExternalUnresolvedTarget(t *testing.T) {
	// A call resolved to a library method the analyzer has no source for: the
	// node exists (so Validate is happy) but carries no file/line. That must
	// not surface as a definition — there is nowhere to jump to — and,
	// because nothing else matches "call" either, the whole lookup is empty.
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderService", Label: "com.acme.OrderService", Kind: "class", FilePath: "OrderService.java", Line: 1, EndLine: 50},
			{ID: "com.acme.OrderService.create(Request)", Label: "com.acme.OrderService.create", Kind: "method", FilePath: "OrderService.java", Line: 10, EndLine: 20},
			{ID: "java.util.List.of()", Label: "java.util.List.call", Kind: "method", FilePath: "", Line: 0},
		},
		CallSites: []CallSite{
			{From: "com.acme.OrderService.create(Request)", To: "java.util.List.of()", Line: 15},
		},
	}
	svc := openWith(t, result)

	defs, err := svc.FindDefinitions(context.Background(), "OrderService.java", 15, "call")
	if err != nil {
		t.Fatalf("FindDefinitions: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("defs = %+v, want none — the only match has no location", defs)
	}
}

func TestFindDefinitionsFieldAccess(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderService", Label: "com.acme.OrderService", Kind: "class", FilePath: "OrderService.java", Line: 1, EndLine: 50},
			{ID: "com.acme.OrderService.compute()", Label: "com.acme.OrderService.compute", Kind: "method", FilePath: "OrderService.java", Line: 30, EndLine: 35},
		},
		FieldAccesses: []FieldAccess{
			{ClassID: "com.acme.OrderService", NodeID: "com.acme.OrderService.compute()", FieldName: "total", Kind: "write", Line: 32},
		},
	}
	svc := openWith(t, result)

	defs, err := svc.FindDefinitions(context.Background(), "OrderService.java", 32, "total")
	if err != nil {
		t.Fatalf("FindDefinitions: %v", err)
	}
	if len(defs) != 1 || defs[0].NodeID != "com.acme.OrderService" {
		t.Fatalf("defs = %+v, want the declaring class (no field nodes in the index)", defs)
	}
	if defs[0].Kind != "class" {
		t.Fatalf("defs[0].Kind = %q, want class", defs[0].Kind)
	}
}

func TestFindDefinitionsFieldAccessSkipsExternalClass(t *testing.T) {
	// class_id points at a library type field_accesses does not require to
	// exist in Nodes (only edges/call sites/endpoints are Validate-checked) —
	// the shape an inherited-field access from an unindexed superclass takes.
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderService.compute()", Label: "com.acme.OrderService.compute", Kind: "method", FilePath: "OrderService.java", Line: 30, EndLine: 35},
		},
		FieldAccesses: []FieldAccess{
			{ClassID: "javax.persistence.AbstractEntity", NodeID: "com.acme.OrderService.compute()", FieldName: "id", Kind: "read", Line: 32},
		},
	}
	svc := openWith(t, result)

	defs, err := svc.FindDefinitions(context.Background(), "OrderService.java", 32, "id")
	if err != nil {
		t.Fatalf("FindDefinitions: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("defs = %+v, want none — the declaring class is external/unindexed", defs)
	}
}

func TestFindDefinitionsFallbackByNameRanking(t *testing.T) {
	// Three classes named DeviceListResponse: one in the cursor's own file,
	// one the cursor's file depends on, one unrelated. Same-file must win,
	// then the dependency, then the rest.
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.DeviceController", Label: "com.acme.DeviceController", Kind: "class", FilePath: "DeviceController.java", Line: 1, EndLine: 30},
			// A same-file candidate (contrived, but exercises the rank-0 path).
			{ID: "com.acme.DeviceController.DeviceListResponse", Label: "com.acme.DeviceController.DeviceListResponse", Kind: "class", FilePath: "DeviceController.java", Line: 25, EndLine: 29},
			{ID: "com.acme.dto.DeviceListResponse", Label: "com.acme.dto.DeviceListResponse", Kind: "class", FilePath: "DeviceListResponse.java", Line: 1, EndLine: 10},
			{ID: "com.other.DeviceListResponse", Label: "com.other.DeviceListResponse", Kind: "class", FilePath: "Unrelated.java", Line: 1, EndLine: 5},
		},
		FileDependencies: []FileDependency{
			{FilePath: "DeviceController.java", DependsOnFilePath: "DeviceListResponse.java"},
		},
	}
	svc := openWith(t, result)

	// Line 12: not a call site, not a field access, not the declaration line
	// of anything — forces the fallback-by-name tier.
	defs, err := svc.FindDefinitions(context.Background(), "DeviceController.java", 12, "DeviceListResponse")
	if err != nil {
		t.Fatalf("FindDefinitions: %v", err)
	}
	want := []string{
		"com.acme.DeviceController.DeviceListResponse", // same file
		"com.acme.dto.DeviceListResponse",              // a file DeviceController.java depends on
		"com.other.DeviceListResponse",                 // unrelated, ranked last
	}
	got := definitionIDs(defs)
	if len(got) != len(want) {
		t.Fatalf("defs = %+v, want %d ranked results", defs, len(want))
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("defs[%d].NodeID = %q, want %q (order = %v)", i, got[i], id, got)
		}
	}
}

func TestFindDefinitionsFallbackExcludesSelfDeclarationUnlessOnlyResult(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.dto.DeviceListResponse", Label: "com.acme.dto.DeviceListResponse", Kind: "class", FilePath: "DeviceListResponse.java", Line: 1, EndLine: 10},
		},
	}
	svc := openWith(t, result)

	// The cursor sits exactly on the class's own declaration line. It is the
	// only candidate, so it must still be returned rather than nothing.
	defs, err := svc.FindDefinitions(context.Background(), "DeviceListResponse.java", 1, "DeviceListResponse")
	if err != nil {
		t.Fatalf("FindDefinitions: %v", err)
	}
	if len(defs) != 1 || defs[0].NodeID != "com.acme.dto.DeviceListResponse" {
		t.Fatalf("defs = %+v, want the lone self-declaration kept", defs)
	}
}

func TestFindDefinitionsFallbackDropsSelfDeclarationWhenOthersExist(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.dto.DeviceListResponse", Label: "com.acme.dto.DeviceListResponse", Kind: "class", FilePath: "DeviceListResponse.java", Line: 1, EndLine: 10},
			{ID: "com.other.DeviceListResponse", Label: "com.other.DeviceListResponse", Kind: "class", FilePath: "Unrelated.java", Line: 1, EndLine: 5},
		},
	}
	svc := openWith(t, result)

	defs, err := svc.FindDefinitions(context.Background(), "DeviceListResponse.java", 1, "DeviceListResponse")
	if err != nil {
		t.Fatalf("FindDefinitions: %v", err)
	}
	if containsID(defs, "com.acme.dto.DeviceListResponse") {
		t.Fatalf("defs = %+v, self-declaration must be dropped when another candidate exists", defs)
	}
	if len(defs) != 1 || defs[0].NodeID != "com.other.DeviceListResponse" {
		t.Fatalf("defs = %+v, want only the other file's class", defs)
	}
}

func TestFindDefinitionsNothingFoundReturnsEmpty(t *testing.T) {
	svc := openWith(t, sampleResult())

	defs, err := svc.FindDefinitions(context.Background(), "OrderController.java", 10, "NoSuchSymbol")
	if err != nil {
		t.Fatalf("FindDefinitions: %v", err)
	}
	if defs == nil {
		t.Fatal("defs is nil, want a non-nil empty slice")
	}
	if len(defs) != 0 {
		t.Fatalf("defs = %+v, want none", defs)
	}
}

func TestFindDefinitionsNoActiveProject(t *testing.T) {
	svc := NewService(NewNilStore(), fakeAnalyzer{})

	_, err := svc.FindDefinitions(context.Background(), "Foo.java", 1, "Foo")
	if err != ErrNoActiveProject {
		t.Fatalf("err = %v, want ErrNoActiveProject", err)
	}
}

func TestMatchesIdentifier(t *testing.T) {
	cases := []struct {
		label, identifier string
		want              bool
	}{
		{"com.acme.OrderService.create", "create", true},
		{"com.acme.OrderService.create", "Create", false}, // case-sensitive
		{"com.acme.OrderService.listAll", "list", false},  // suffix, not substring
		{"com.acme.OrderService", "OrderService", true},
		{"OrderService", "OrderService", true}, // default-package class: label has no dot at all
		{"com.acme.OrderService.create", "", false},
	}
	for _, c := range cases {
		if got := matchesIdentifier(c.label, c.identifier); got != c.want {
			t.Errorf("matchesIdentifier(%q, %q) = %v, want %v", c.label, c.identifier, got, c.want)
		}
	}
}
