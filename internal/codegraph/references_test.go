// internal/codegraph/references_test.go
package codegraph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// --- resolveSymbol -----------------------------------------------------------

// The cursor sitting on a symbol's own declaration must resolve to THAT
// symbol even though another node elsewhere shares its simple name — the
// opposite of FindDefinitions' fallback tier, which deliberately drops the
// self-declaration when other candidates exist (see
// TestFindDefinitionsFallbackDropsSelfDeclarationWhenOthersExist). For "find
// references"/"find implementations", landing on the declaration IS the
// ordinary case, not a degenerate one.
func TestResolveSymbolDeclarationWinsOverByName(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.Foo", Label: "com.acme.Foo", Kind: "class", FilePath: "Foo.java", Line: 5, EndLine: 20},
			{ID: "com.other.Foo", Label: "com.other.Foo", Kind: "class", FilePath: "Other.java", Line: 1, EndLine: 10},
		},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatal(err)
	}

	syms, err := resolveSymbol(ctx, store, "Foo.java", 5, "Foo")
	if err != nil {
		t.Fatalf("resolveSymbol: %v", err)
	}
	if len(syms) != 1 || syms[0].Node == nil || syms[0].Node.ID != "com.acme.Foo" {
		t.Fatalf("syms = %+v, want exactly the declaration at the cursor", syms)
	}
}

// A node's Line is where its declaration starts, which under an annotation is
// the annotation — so the cursor on the method's NAME is a line or more below
// it and misses the exact-line tier. The enclosing node named by the
// identifier must still be the symbol; otherwise the by-name tier answers
// with every same-named method in the project and their references are
// unioned in.
func TestResolveSymbolAnnotatedDeclarationResolvesToEnclosingMethod(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.DeviceService", Label: "com.acme.DeviceService", Kind: "class", FilePath: "DeviceService.java", Line: 10, EndLine: 40},
			// @Override on 17, `public List<Device> list(...)` on 18.
			{ID: "com.acme.DeviceService.list(Command)", Label: "com.acme.DeviceService.list", Kind: "method", FilePath: "DeviceService.java", Line: 17, EndLine: 22},
			{ID: "com.acme.OrderService.list()", Label: "com.acme.OrderService.list", Kind: "method", FilePath: "OrderService.java", Line: 5, EndLine: 9},
		},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatal(err)
	}

	syms, err := resolveSymbol(ctx, store, "DeviceService.java", 18, "list")
	if err != nil {
		t.Fatalf("resolveSymbol: %v", err)
	}
	if len(syms) != 1 || syms[0].Node == nil || syms[0].Node.ID != "com.acme.DeviceService.list(Command)" {
		t.Fatalf("syms = %+v, want only the method whose name the cursor is on", syms)
	}
}

// Inside list(), a line that calls ANOTHER method named list names the callee.
// The call-site tier has to win over the enclosing-node tier, or every
// delegating method would resolve to itself.
func TestResolveSymbolCallInsideSameNamedMethodResolvesToCallee(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.DeviceService.list(Command)", Label: "com.acme.DeviceService.list", Kind: "method", FilePath: "DeviceService.java", Line: 17, EndLine: 22},
			{ID: "com.acme.DeviceRepository.list()", Label: "com.acme.DeviceRepository.list", Kind: "method", FilePath: "DeviceRepository.java", Line: 8, EndLine: 8},
		},
		CallSites: []CallSite{
			{From: "com.acme.DeviceService.list(Command)", To: "com.acme.DeviceRepository.list()", Line: 19},
		},
	}
	if err := store.ReplaceIndex(ctx, result); err != nil {
		t.Fatal(err)
	}

	syms, err := resolveSymbol(ctx, store, "DeviceService.java", 19, "list")
	if err != nil {
		t.Fatalf("resolveSymbol: %v", err)
	}
	if len(syms) != 1 || syms[0].Node == nil || syms[0].Node.ID != "com.acme.DeviceRepository.list()" {
		t.Fatalf("syms = %+v, want the callee, not the enclosing method", syms)
	}
}

// --- FindImplementations -----------------------------------------------------

func interfaceAndImpls() AnalysisResult {
	return AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderService", Label: "com.acme.OrderService", Kind: "class", FilePath: "OrderService.java", Line: 1, EndLine: 20},
			{ID: "com.acme.OrderService.create(Request)", Label: "com.acme.OrderService.create", Kind: "method", FilePath: "OrderService.java", Line: 5, EndLine: 5},
			{ID: "com.acme.OrderServiceImpl", Label: "com.acme.OrderServiceImpl", Kind: "class", FilePath: "OrderServiceImpl.java", Line: 1, EndLine: 30},
			{ID: "com.acme.OrderServiceImpl.create(Request)", Label: "com.acme.OrderServiceImpl.create", Kind: "method", FilePath: "OrderServiceImpl.java", Line: 10, EndLine: 15},
			{ID: "com.acme.OrderServiceImpl2", Label: "com.acme.OrderServiceImpl2", Kind: "class", FilePath: "OrderServiceImpl2.java", Line: 1, EndLine: 30},
			{ID: "com.acme.OrderServiceImpl2.create(Request)", Label: "com.acme.OrderServiceImpl2.create", Kind: "method", FilePath: "OrderServiceImpl2.java", Line: 8, EndLine: 12},
		},
		// Dispatch links: ProjectAnalyzer.linkOverrides records these at the
		// INTERFACE method's own declaration line (5), one per implementor.
		CallSites: []CallSite{
			{From: "com.acme.OrderService.create(Request)", To: "com.acme.OrderServiceImpl.create(Request)", Line: 5},
			{From: "com.acme.OrderService.create(Request)", To: "com.acme.OrderServiceImpl2.create(Request)", Line: 5},
		},
	}
}

func TestFindImplementationsOfInterfaceMethodTwoImpls(t *testing.T) {
	svc := openWith(t, interfaceAndImpls())

	defs, err := svc.FindImplementations(context.Background(), "OrderService.java", 5, "create")
	if err != nil {
		t.Fatalf("FindImplementations: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("defs = %+v, want both implementations", defs)
	}
	if !containsID(defs, "com.acme.OrderServiceImpl.create(Request)") || !containsID(defs, "com.acme.OrderServiceImpl2.create(Request)") {
		t.Fatalf("defs = %+v, missing an implementation", defs)
	}
}

// Asking for implementations of a method that IS an implementation (nothing
// dispatches FROM it) must return nothing — not the method itself.
func TestFindImplementationsOnImplementationReturnsEmpty(t *testing.T) {
	svc := openWith(t, interfaceAndImpls())

	defs, err := svc.FindImplementations(context.Background(), "OrderServiceImpl.java", 10, "create")
	if err != nil {
		t.Fatalf("FindImplementations: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("defs = %+v, want none: an implementation is not itself an interface", defs)
	}
}

func TestFindImplementationsOfInterfaceClassLevel(t *testing.T) {
	svc := openWith(t, interfaceAndImpls())

	defs, err := svc.FindImplementations(context.Background(), "OrderService.java", 1, "OrderService")
	if err != nil {
		t.Fatalf("FindImplementations: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("defs = %+v, want the two classes owning an implementing method", defs)
	}
	if !containsID(defs, "com.acme.OrderServiceImpl") || !containsID(defs, "com.acme.OrderServiceImpl2") {
		t.Fatalf("defs = %+v, want OrderServiceImpl and OrderServiceImpl2", defs)
	}
	for _, d := range defs {
		if d.Kind != "class" {
			t.Fatalf("defs = %+v, want class-kind results", defs)
		}
	}
}

func TestFindImplementationsNoActiveProject(t *testing.T) {
	svc := NewService(NewNilStore(), fakeAnalyzer{})
	if _, err := svc.FindImplementations(context.Background(), "Foo.java", 1, "Foo"); err != ErrNoActiveProject {
		t.Fatalf("err = %v, want ErrNoActiveProject", err)
	}
}

// --- FindReferences: methods --------------------------------------------------

func TestFindReferencesMethodCalledDirectly(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderService.compute()", Label: "com.acme.OrderService.compute", Kind: "method", FilePath: "OrderService.java", Line: 10, EndLine: 12},
			{ID: "com.acme.OrderController.handle()", Label: "com.acme.OrderController.handle", Kind: "method", FilePath: "OrderController.java", Line: 5, EndLine: 8},
		},
		CallSites: []CallSite{
			{From: "com.acme.OrderController.handle()", To: "com.acme.OrderService.compute()", Line: 6},
		},
	}
	svc := openWith(t, result)

	refs, err := svc.FindReferences(context.Background(), "OrderService.java", 10, "compute")
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want exactly the one call site", refs)
	}
	r := refs[0]
	if r.NodeID != "com.acme.OrderController.handle()" || r.Kind != "call" || r.FilePath != "OrderController.java" || r.Line != 6 {
		t.Fatalf("refs[0] = %+v, unexpected", r)
	}
}

// A concrete implementation is called only through its interface — the
// common Spring-style shape. FindReferences on the impl method must still
// surface the real call site, reached via the dispatch link into the impl
// and then the interface method's own real callers. The dispatch link
// itself (recorded on the interface method's own declaration line) must
// never surface as a reference.
func TestFindReferencesImplReachedOnlyThroughInterface(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderService.create(Request)", Label: "com.acme.OrderService.create", Kind: "method", FilePath: "OrderService.java", Line: 5, EndLine: 5},
			{ID: "com.acme.OrderServiceImpl.create(Request)", Label: "com.acme.OrderServiceImpl.create", Kind: "method", FilePath: "OrderServiceImpl.java", Line: 10, EndLine: 15},
			{ID: "com.acme.OrderController.handle()", Label: "com.acme.OrderController.handle", Kind: "method", FilePath: "OrderController.java", Line: 3, EndLine: 8},
		},
		CallSites: []CallSite{
			// Dispatch link: recorded at the interface method's own line (5).
			{From: "com.acme.OrderService.create(Request)", To: "com.acme.OrderServiceImpl.create(Request)", Line: 5},
			// The real call, through the interface type.
			{From: "com.acme.OrderController.handle()", To: "com.acme.OrderService.create(Request)", Line: 4},
		},
	}
	svc := openWith(t, result)

	refs, err := svc.FindReferences(context.Background(), "OrderServiceImpl.java", 10, "create")
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want exactly the one real call reached through the interface", refs)
	}
	r := refs[0]
	if r.NodeID != "com.acme.OrderController.handle()" || r.FilePath != "OrderController.java" || r.Line != 4 {
		t.Fatalf("refs[0] = %+v, want the controller's real call site", r)
	}
	for _, ref := range refs {
		if ref.Line == 5 {
			t.Fatalf("refs = %+v, the dispatch link (interface's own declaration line) must never appear as a reference", refs)
		}
	}
}

// A dispatch link the analyzer marked as one (Role: RoleOverride) is
// recognised by its role, not only by sitting on the abstract method's own
// declaration line: here the method's node starts on its annotation (line 4)
// while the link is recorded at line 5. The line rule stays as the fallback
// for indexes written before roles (the tests above).
func roleMarkedDispatch() AnalysisResult {
	return AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderService.create(Request)", Label: "com.acme.OrderService.create", Kind: "method", FilePath: "OrderService.java", Line: 4, EndLine: 5},
			{ID: "com.acme.OrderServiceImpl.create(Request)", Label: "com.acme.OrderServiceImpl.create", Kind: "method", FilePath: "OrderServiceImpl.java", Line: 10, EndLine: 15},
			{ID: "com.acme.OrderController.handle()", Label: "com.acme.OrderController.handle", Kind: "method", FilePath: "OrderController.java", Line: 3, EndLine: 8},
		},
		CallSites: []CallSite{
			{From: "com.acme.OrderService.create(Request)", To: "com.acme.OrderServiceImpl.create(Request)", Line: 5, Role: RoleOverride},
			{From: "com.acme.OrderController.handle()", To: "com.acme.OrderService.create(Request)", Line: 4},
		},
	}
}

func TestFindImplementationsReadsTheOverrideRole(t *testing.T) {
	svc := openWith(t, roleMarkedDispatch())

	defs, err := svc.FindImplementations(context.Background(), "OrderService.java", 4, "create")
	if err != nil {
		t.Fatalf("FindImplementations: %v", err)
	}
	if len(defs) != 1 || defs[0].NodeID != "com.acme.OrderServiceImpl.create(Request)" {
		t.Fatalf("defs = %+v, want the implementation the role-marked link names", defs)
	}
}

func TestFindReferencesSkipsARoleMarkedDispatchLink(t *testing.T) {
	svc := openWith(t, roleMarkedDispatch())

	refs, err := svc.FindReferences(context.Background(), "OrderServiceImpl.java", 10, "create")
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	if len(refs) != 1 || refs[0].NodeID != "com.acme.OrderController.handle()" || refs[0].Line != 4 {
		t.Fatalf("refs = %+v, want only the controller's call reached through the interface", refs)
	}
}

// --- FindReferences: fields ---------------------------------------------------

func TestFindReferencesFieldReadAndWrite(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.Order", Label: "com.acme.Order", Kind: "class", FilePath: "Order.java", Line: 1, EndLine: 50},
			{ID: "com.acme.Order.setTotal()", Label: "com.acme.Order.setTotal", Kind: "method", FilePath: "Order.java", Line: 5, EndLine: 7},
			{ID: "com.acme.OrderService.compute()", Label: "com.acme.OrderService.compute", Kind: "method", FilePath: "OrderService.java", Line: 8, EndLine: 10},
		},
		FieldAccesses: []FieldAccess{
			{ClassID: "com.acme.Order", NodeID: "com.acme.Order.setTotal()", FieldName: "total", Kind: "write", Line: 6},
			{ClassID: "com.acme.Order", NodeID: "com.acme.OrderService.compute()", FieldName: "total", Kind: "read", Line: 9},
		},
	}
	svc := openWith(t, result)

	// Cursor on the write site resolves the field symbol via the
	// field-access tier (there is no field node to land on directly).
	refs, err := svc.FindReferences(context.Background(), "Order.java", 6, "total")
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs = %+v, want both the read and the write", refs)
	}
	var sawRead, sawWrite bool
	for _, r := range refs {
		switch {
		case r.Kind == "write" && r.FilePath == "Order.java" && r.Line == 6:
			sawWrite = true
		case r.Kind == "read" && r.FilePath == "OrderService.java" && r.Line == 9:
			sawRead = true
		}
	}
	if !sawRead || !sawWrite {
		t.Fatalf("refs = %+v, missing the read or the write", refs)
	}
}

// --- FindReferences: classes (DI) ---------------------------------------------

func TestFindReferencesClassViaDependencyInjection(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderRepository", Label: "com.acme.OrderRepository", Kind: "class", FilePath: "OrderRepository.java", Line: 1, EndLine: 10},
			{ID: "com.acme.OrderService", Label: "com.acme.OrderService", Kind: "class", FilePath: "OrderService.java", Line: 1, EndLine: 40},
		},
		Edges: []Edge{
			{From: "com.acme.OrderService", To: "com.acme.OrderRepository", Kind: "di"},
		},
	}
	svc := openWith(t, result)

	refs, err := svc.FindReferences(context.Background(), "OrderRepository.java", 1, "OrderRepository")
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want the one injecting class", refs)
	}
	r := refs[0]
	if r.NodeID != "com.acme.OrderService" || r.Kind != "inject" || r.FilePath != "OrderService.java" || r.Line != 1 {
		t.Fatalf("refs[0] = %+v, unexpected", r)
	}
}

// --- FindReferences: constructors ---------------------------------------------

// Constructors are indexed as ordinary method nodes (see
// ProjectAnalyzer.collectDeclarations: addMethodNode runs over every
// ConstructorDeclaration), labelled "<classId>.<ClassSimpleName>" — a call
// to `new Order(...)` must surface as a reference to the class.
func TestFindReferencesClassConstructorCall(t *testing.T) {
	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.Order", Label: "com.acme.Order", Kind: "class", FilePath: "Order.java", Line: 1, EndLine: 40},
			{ID: "com.acme.Order.Order()", Label: "com.acme.Order.Order", Kind: "method", FilePath: "Order.java", Line: 2, EndLine: 4},
			{ID: "com.acme.OrderFactory.build()", Label: "com.acme.OrderFactory.build", Kind: "method", FilePath: "OrderFactory.java", Line: 5, EndLine: 8},
		},
		CallSites: []CallSite{
			{From: "com.acme.OrderFactory.build()", To: "com.acme.Order.Order()", Line: 6},
		},
	}
	svc := openWith(t, result)

	refs, err := svc.FindReferences(context.Background(), "Order.java", 1, "Order")
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	var sawCtorCall bool
	for _, r := range refs {
		if r.NodeID == "com.acme.OrderFactory.build()" && r.Kind == "call" && r.Line == 6 {
			sawCtorCall = true
		}
	}
	if !sawCtorCall {
		t.Fatalf("refs = %+v, want the constructor call surfaced as a reference to the class", refs)
	}
}

// --- FindReferences: Code, cap, empty -----------------------------------------

func TestFindReferencesCodePopulatedFromRealFile(t *testing.T) {
	root := t.TempDir()
	callerSrc := "class OrderController {\n    void handle() {\n        service.compute();\n    }\n}\n"
	if err := os.WriteFile(filepath.Join(root, "OrderController.java"), []byte(callerSrc), 0o600); err != nil {
		t.Fatal(err)
	}

	result := AnalysisResult{
		Nodes: []Node{
			{ID: "com.acme.OrderService.compute()", Label: "com.acme.OrderService.compute", Kind: "method", FilePath: "OrderService.java", Line: 1, EndLine: 2},
			{ID: "com.acme.OrderController.handle()", Label: "com.acme.OrderController.handle", Kind: "method", FilePath: "OrderController.java", Line: 2, EndLine: 4},
		},
		CallSites: []CallSite{
			{From: "com.acme.OrderController.handle()", To: "com.acme.OrderService.compute()", Line: 3},
		},
	}
	svc := newTestService(t, fakeAnalyzer{result: result})
	dbPath := filepath.Join(t.TempDir(), "index.db")
	if err := svc.OpenProjectAt(context.Background(), dbPath, root); err != nil {
		t.Fatalf("OpenProjectAt: %v", err)
	}
	t.Cleanup(func() { svc.Close() })

	refs, err := svc.FindReferences(context.Background(), "OrderService.java", 1, "compute")
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs = %+v, want exactly one", refs)
	}
	if want := "service.compute();"; refs[0].Code != want {
		t.Fatalf("refs[0].Code = %q, want %q", refs[0].Code, want)
	}
}

func TestFindReferencesCap(t *testing.T) {
	const callers = maxReferences + 50
	nodes := []Node{
		{ID: "com.acme.OrderService.compute()", Label: "com.acme.OrderService.compute", Kind: "method", FilePath: "OrderService.java", Line: 1, EndLine: 2},
	}
	var sites []CallSite
	for i := 0; i < callers; i++ {
		id := fmt.Sprintf("com.acme.Caller%d.run()", i)
		nodes = append(nodes, Node{ID: id, Label: fmt.Sprintf("com.acme.Caller%d.run", i), Kind: "method", FilePath: fmt.Sprintf("Caller%d.java", i), Line: 1, EndLine: 2})
		sites = append(sites, CallSite{From: id, To: "com.acme.OrderService.compute()", Line: 5})
	}
	svc := openWith(t, AnalysisResult{Nodes: nodes, CallSites: sites})

	refs, err := svc.FindReferences(context.Background(), "OrderService.java", 1, "compute")
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	if len(refs) != maxReferences {
		t.Fatalf("len(refs) = %d, want the cap of %d", len(refs), maxReferences)
	}
}

func TestFindReferencesNothingFoundReturnsEmpty(t *testing.T) {
	svc := openWith(t, sampleResult())

	refs, err := svc.FindReferences(context.Background(), "OrderController.java", 10, "NoSuchSymbol")
	if err != nil {
		t.Fatalf("FindReferences: %v", err)
	}
	if refs == nil {
		t.Fatal("refs is nil, want a non-nil empty slice")
	}
	if len(refs) != 0 {
		t.Fatalf("refs = %+v, want none", refs)
	}
}

func TestFindReferencesNoActiveProject(t *testing.T) {
	svc := NewService(NewNilStore(), fakeAnalyzer{})
	if _, err := svc.FindReferences(context.Background(), "Foo.java", 1, "Foo"); err != ErrNoActiveProject {
		t.Fatalf("err = %v, want ErrNoActiveProject", err)
	}
}
