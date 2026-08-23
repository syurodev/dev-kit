# CodeGraph Def-Use Trace (backend) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn `CodeGraphTraceDataFlow` from an arbitrarily-ordered list of field accesses into a call-graph-ordered trace that says, per method, where the field is read and written and which call the flow continues into — and that says out loud where it cannot continue.

**Architecture:** The Java analyzer already walks method bodies and resolves symbols; it gains one new output, `callSites`, which is the un-deduped, line-carrying record of every call expression **including the ones it currently discards silently**. Go stores that in a new `call_sites` table and assembles the trace: interleave a method's field accesses and its call sites by source line, then walk the call graph depth-first from the methods that touch the field.

**Tech Stack:** Java 17 + JavaParser/JavaSymbolSolver 3.28.2 (Maven), Go + SQLite (`modernc` driver via the existing store), `go test`, JUnit 5.

---

## Why this shape, and what it is not

Read this before Task 1. Two of these were settled by reading the library, not by preference.

**JavaSymbolSolver does no data-flow.** It resolves symbols and types — "what does this name refer to, what type is it". Every def-use conclusion in this plan is our own code over its output. There is no library call that returns a def-use chain.

**Bodies exist only for project classes.** `JavaParserMethodDeclaration` (source-backed) overrides `toAst()` and exposes `getWrappedNode()`; `JavassistMethodDeclaration` (jar-backed) does not override it and inherits `AssociableToAST.toAst()`, whose entire body is `return Optional.empty()`. So a call into Spring, the JDK, or any jar can be *named* but never *entered*. This is a hard boundary, not a to-do.

**There is no API for subtypes.** `ResolvedReferenceTypeDeclaration` has `getAncestors`/`getAllAncestors` — upward only. A call through an interface resolves to the *interface* method, which has no body. Finding the implementation would mean scanning every type in the project ourselves, and would still be wrong for a Spring proxy created at runtime.

So the trace stops at three kinds of wall, and **the whole point of this plan is that it says which wall it hit** rather than going quiet. `addCallEdges` currently does the opposite: an unresolved call hits `continue` and vanishes.

**This is not reaching-definitions.** Within a method the events are ordered by **source line**, not by execution order. Branches, loops and early returns are not modelled — a write inside an `else` still sorts before a read on a later line. Building a CFG from the AST is a much larger piece of work and is deliberately not in this plan; the ceiling is recorded in the payload (`orderedBy: "source"`) so no consumer can mistake it for execution order.

---

## File Structure

**Java — created**

| File | Responsibility |
|---|---|
| `analyzer/src/main/java/com/devkit/codegraph/CallSite.java` | One call expression: caller, callee (may be empty), line, and why the callee is empty. |

**Java — modified**

| File | Change |
|---|---|
| `analyzer/src/main/java/com/devkit/codegraph/AnalysisResult.java` | Adds `callSites`. |
| `analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java` | `addCallEdges` also emits a `CallSite` per call expression, including unresolved ones. |
| `analyzer/src/test/java/com/devkit/codegraph/ProjectAnalyzerTest.java` | Tests for the new output. |

**Go — modified**

| File | Change |
|---|---|
| `internal/codegraph/codegraph.go` | `CallSite` type, `AnalysisResult.CallSites`, reshaped `DataFlowTrace`/`DataFlowStep`. |
| `internal/codegraph/store.go` | `call_sites` table, insert, `GetCallSites`, and the clear list. |
| `internal/codegraph/service.go` | `TraceDataFlow` rewritten to assemble and walk. |
| `internal/codegraph/service_test.go` | Tests for ordering, walking and the three walls. |
| `internal/codegraph/store_test.go` | Round-trip test for the new table. |

Nothing in `internal/bridge` changes: `CodeGraphTraceDataFlow` already returns whatever `DataFlowTrace` is.

---

## The payload this produces

```go
type DataFlowTrace struct {
    SymbolID  string         `json:"symbolId"`
    FieldName string         `json:"fieldName"`
    OrderedBy string         `json:"orderedBy"` // always "source" in v1 — see the ceiling above
    Steps     []DataFlowStep `json:"steps"`
}

type DataFlowStep struct {
    NodeID     string `json:"nodeId"`
    Label      string `json:"label"`
    FilePath   string `json:"filePath"`
    Line       int    `json:"line"`
    Kind       string `json:"kind"`       // "read" | "write" | "call"
    Depth      int    `json:"depth"`      // 0 for a root method, +1 per call followed
    TargetID   string `json:"targetId"`   // kind=="call": the callee node, else ""
    Unresolved string `json:"unresolved"` // "" | "library" | "interface" | "unresolved" | "depth" | "cycle"
}
```

Flat with a `Depth`, not nested: it renders as an indented list, and a flat slice keeps the JSON bridge and the existing frontend type shallow.

---

## Task 1: `CallSite` in the analyzer's output contract

**Files:**
- Create: `analyzer/src/main/java/com/devkit/codegraph/CallSite.java`
- Modify: `analyzer/src/main/java/com/devkit/codegraph/AnalysisResult.java`

- [ ] **Step 1: Create the record**

Create `analyzer/src/main/java/com/devkit/codegraph/CallSite.java`:

```java
package com.devkit.codegraph;

/**
 * One call expression in one method body, kept un-deduped and with its line.
 *
 * Deliberately not an Edge. Edges are deduped by (from, to, kind) so the graph
 * stays clean, which throws away both the line and the repetition — and a
 * def-use trace needs exactly those, to interleave calls with field accesses in
 * source order.
 *
 * `to` is empty when the callee could not be turned into a node, and `unresolved`
 * says why. Recording that at all is the point: the old code dropped these calls
 * silently, so a trace could stop dead with no way to tell the reader whether the
 * flow ended or the analyzer simply lost it.
 */
public class CallSite {
    public String from;
    public String to;         // "" when unresolved
    public int line;
    public String unresolved; // "" | "library" | "interface" | "unresolved"

    public CallSite() {}

    public CallSite(String from, String to, int line, String unresolved) {
        this.from = from;
        this.to = to;
        this.line = line;
        this.unresolved = unresolved;
    }
}
```

- [ ] **Step 2: Add it to the result**

In `analyzer/src/main/java/com/devkit/codegraph/AnalysisResult.java`, change:

```java
    public List<FieldAccess> fieldAccesses = new ArrayList<>();
}
```

to:

```java
    public List<FieldAccess> fieldAccesses = new ArrayList<>();
    public List<CallSite> callSites = new ArrayList<>();
}
```

- [ ] **Step 3: Compile**

Run: `cd dev-kit-app/analyzer && mvn -q -DskipTests compile`
Expected: exits 0, no output.

- [ ] **Step 4: Commit**

```bash
cd dev-kit-app
git add analyzer/src/main/java/com/devkit/codegraph/CallSite.java analyzer/src/main/java/com/devkit/codegraph/AnalysisResult.java
git commit -m "feat(analyzer): call sites carry their line and why they are unresolved"
```

---

## Task 2: Emit call sites, including the ones that used to vanish

`addCallEdges` currently has two `continue` statements that discard a call. Both become a recorded `CallSite` with a reason. The `Edge` behaviour does not change at all — same edges, same dedupe.

**Files:**
- Modify: `analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java`
- Test: `analyzer/src/test/java/com/devkit/codegraph/ProjectAnalyzerTest.java`

- [ ] **Step 1: Write the failing tests**

Append these to `analyzer/src/test/java/com/devkit/codegraph/ProjectAnalyzerTest.java`, inside the existing test class:

```java
    @Test
    void recordsCallSiteWithLineForAResolvedProjectCall() throws Exception {
        AnalysisResult result = new ProjectAnalyzer(sampleProjectRoot()).analyze();

        List<CallSite> resolved = result.callSites.stream()
                .filter(c -> c.unresolved.isEmpty())
                .toList();

        assertFalse(resolved.isEmpty(), "expected at least one resolved call site");
        for (CallSite c : resolved) {
            assertFalse(c.from.isEmpty(), "from must be set");
            assertFalse(c.to.isEmpty(), "a resolved call site must name its callee");
            assertTrue(c.line > 0, "a call site must carry the line it appears on");
        }
    }

    @Test
    void recordsUnresolvedCallsInsteadOfDroppingThem() throws Exception {
        AnalysisResult result = new ProjectAnalyzer(sampleProjectRoot()).analyze();

        // The sample project calls into the JDK (e.g. collections/strings), which
        // resolves to a method that never becomes a node. The old code hit
        // `continue` here and the call disappeared, which is what made a trace
        // unable to say why it stopped.
        List<CallSite> unresolved = result.callSites.stream()
                .filter(c -> !c.unresolved.isEmpty())
                .toList();

        assertFalse(unresolved.isEmpty(), "expected at least one unresolved call site");
        for (CallSite c : unresolved) {
            assertTrue(c.to.isEmpty(), "an unresolved call site must not name a callee");
            assertTrue(c.line > 0, "even an unresolved call site must carry its line");
            assertTrue(
                    List.of("library", "interface", "unresolved").contains(c.unresolved),
                    "unexpected reason: " + c.unresolved);
        }
    }

    @Test
    void callSitesAreNotDedupedTheWayEdgesAre() throws Exception {
        AnalysisResult result = new ProjectAnalyzer(sampleProjectRoot()).analyze();

        // Edges collapse repeated calls to the same target; call sites must not,
        // or a method that calls the same thing twice loses one of its two lines.
        assertTrue(
                result.callSites.size() >= result.edges.size(),
                "call sites (" + result.callSites.size() + ") should not be fewer than edges ("
                        + result.edges.size() + ")");
    }
```

The existing tests in that class do not share a fixture helper — each one writes `Path.of("src/test/resources/sample-project")` inline. Add this helper once and use it in the three new tests only; leave the existing tests alone:

```java
    private static Path sampleProjectRoot() {
        return Path.of("src/test/resources/sample-project");
    }
```

Add any missing imports at the top of the file: `java.nio.file.Path`, `java.util.List`, and the JUnit assertions `assertFalse`/`assertTrue`.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd dev-kit-app/analyzer && mvn -q test -Dtest=ProjectAnalyzerTest`
Expected: FAIL — the three new tests fail because `result.callSites` is empty.

- [ ] **Step 3: Emit call sites**

In `analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java`, replace the whole body of `addCallEdges` with:

```java
    private void addCallEdges(MethodCallSite site, Map<String, String> classFile, Set<String> nodeIds, AnalysisResult result) {
        for (com.github.javaparser.ast.expr.MethodCallExpr call : site.methodDecl().findAll(com.github.javaparser.ast.expr.MethodCallExpr.class)) {
            int line = call.getRange().map(r -> r.begin.line).orElse(0);
            ResolvedMethodDeclaration target;
            String targetClass;
            String toNodeId;
            try {
                target = call.resolve();
                // target.getSignature() can also lazily throw UnsolvedSymbolException if its params are missing
                targetClass = target.declaringType().getQualifiedName();
                toNodeId = targetClass + "." + target.getSignature();
            } catch (RuntimeException | StackOverflowError e) {
                // Unresolved call (missing jar in the local cache, dynamic
                // dispatch the solver can't follow, unresolved parameter types, etc.)
                // — FR-8: this is a gap in the graph, not a fatal error for the whole project.
                // Recorded rather than dropped: a trace that runs into this has to be
                // able to say the flow was lost here, not just end.
                result.callSites.add(new CallSite(site.methodId(), "", line, "unresolved"));
                continue;
            }
            if (!nodeIds.contains(toNodeId)) {
                // Resolved successfully but no node exists for it. Two different
                // walls, and the trace reads very differently depending on which:
                // an interface method has a body somewhere we cannot find, a
                // library method has no body we can ever read.
                boolean isInterface;
                try {
                    isInterface = target.declaringType().isInterface();
                } catch (RuntimeException | StackOverflowError e) {
                    isInterface = false;
                }
                result.callSites.add(new CallSite(
                        site.methodId(), "", line, isInterface ? "interface" : "library"));
                continue;
            }
            result.callSites.add(new CallSite(site.methodId(), toNodeId, line, ""));
            result.edges.add(new Edge(site.methodId(), toNodeId, "call"));

            // File-level dependency: only when the callee is declared in this
            // project's own source (classFile has no entry for JDK/library
            // types, and a dependency on an external file is meaningless for
            // blast radius). Same-file calls are skipped — a file never
            // depends on itself.
            String targetFile = classFile.get(targetClass);
            if (targetFile != null && !targetFile.equals(site.filePath())) {
                result.fileDependencies.add(new FileDependency(site.filePath(), targetFile));
            }
        }
    }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd dev-kit-app/analyzer && mvn -q test -Dtest=ProjectAnalyzerTest`
Expected: PASS, all tests in the class.

- [ ] **Step 5: Run the whole analyzer suite**

Run: `cd dev-kit-app/analyzer && mvn -q test`
Expected: PASS. Nothing else should have moved — `Edge` emission is byte-for-byte what it was.

- [ ] **Step 6: Commit**

```bash
cd dev-kit-app
git add analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java analyzer/src/test/java/com/devkit/codegraph/ProjectAnalyzerTest.java
git commit -m "feat(analyzer): record every call site, including the ones that resolve nowhere"
```

---

## Task 3: The Go side of the contract

**Files:**
- Modify: `internal/codegraph/codegraph.go`
- Test: `internal/codegraph/codegraph_test.go`

- [ ] **Step 1: Write the failing test**

Append to the existing `internal/codegraph/codegraph_test.go`:

```go
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
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd dev-kit-app && go test ./internal/codegraph/ -run TestValidate`
Expected: FAIL to compile — `undefined: CallSite`.

- [ ] **Step 3: Add the type, the field and the validation**

In `internal/codegraph/codegraph.go`, add next to the other analyzer types:

```go
// CallSite is one call expression, un-deduped and with its source line —
// everything Edge throws away for the sake of a clean graph. To is empty when
// the callee could not be turned into a node, and Unresolved says which wall
// the analyzer hit: "library" (bytecode, no body to read), "interface" (the
// implementation is not knowable statically) or "unresolved" (the solver could
// not resolve the call at all).
type CallSite struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Line       int    `json:"line"`
	Unresolved string `json:"unresolved"`
}
```

Add the field to `AnalysisResult`:

```go
	FieldAccesses    []FieldAccess    `json:"fieldAccesses"`
	CallSites        []CallSite       `json:"callSites"`
```

And in `Validate`, after the `Edges` loop and before the `Endpoints` loop:

```go
	for _, c := range r.CallSites {
		if !ids[c.From] {
			return fmt.Errorf("codegraph: call site references unknown node %q", c.From)
		}
		// An empty To is the recorded shape of an unresolved call, not a defect.
		if c.To != "" && !ids[c.To] {
			return fmt.Errorf("codegraph: call site references unknown node %q", c.To)
		}
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd dev-kit-app && go test ./internal/codegraph/ -run TestValidate`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd dev-kit-app
git add internal/codegraph/codegraph.go internal/codegraph/codegraph_test.go
git commit -m "feat(codegraph): CallSite crosses the analyzer boundary"
```

---

## Task 4: Store call sites

**Files:**
- Modify: `internal/codegraph/store.go`
- Test: `internal/codegraph/store_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/codegraph/store_test.go`:

```go
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
}
```

`newTestStore(t)` already exists at the top of that file and returns an open `*sqliteStore` — use it as-is.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd dev-kit-app && go test ./internal/codegraph/ -run TestCallSitesRoundTrip`
Expected: FAIL to compile — `store.GetCallSites undefined`.

- [ ] **Step 3: Add the table**

In `internal/codegraph/store.go`, in the schema string next to `CREATE TABLE IF NOT EXISTS field_accesses`:

```sql
CREATE TABLE IF NOT EXISTS call_sites (
	from_id TEXT NOT NULL,
	to_id TEXT NOT NULL,
	line INTEGER NOT NULL,
	unresolved TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_call_sites_from ON call_sites(from_id);
```

- [ ] **Step 4: Clear it with the rest**

In the same file, add `"call_sites"` to the table list that gets cleared on replace:

```go
	for _, table := range []string{"nodes", "edges", "endpoints", "file_dependencies", "field_accesses", "call_sites"} {
```

- [ ] **Step 5: Insert on replace**

Next to the `field_accesses` insert, add:

```go
	for _, c := range result.CallSites {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO call_sites (from_id, to_id, line, unresolved) VALUES (?, ?, ?, ?)`,
			c.From, c.To, c.Line, c.Unresolved,
		); err != nil {
			return fmt.Errorf("codegraph: insert call site: %w", err)
		}
	}
```

- [ ] **Step 6: Add the read**

Next to `GetFieldAccesses`:

```go
// GetCallSites returns every call expression in one method, in source order.
// Repeats are preserved: a method that calls the same target twice has two
// lines, and losing one would put the trace's events in the wrong order.
func (s *sqliteStore) GetCallSites(ctx context.Context, fromID string) ([]CallSite, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_id, to_id, line, unresolved FROM call_sites WHERE from_id = ? ORDER BY line`,
		fromID)
	if err != nil {
		return nil, fmt.Errorf("codegraph: query call sites: %w", err)
	}
	defer rows.Close()

	var out []CallSite
	for rows.Next() {
		var c CallSite
		if err := rows.Scan(&c.From, &c.To, &c.Line, &c.Unresolved); err != nil {
			return nil, fmt.Errorf("codegraph: scan call site: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
```

The receiver is `*sqliteStore` (unexported) — the same one `GetFieldAccesses` and `GetNode` use. `newTestStore(t)` in `store_test.go` returns it, and the method that writes an index is `ReplaceIndex`, not `Replace`.

- [ ] **Step 7: Run the test to verify it passes**

Run: `cd dev-kit-app && go test ./internal/codegraph/ -run TestCallSitesRoundTrip`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
cd dev-kit-app
git add internal/codegraph/store.go internal/codegraph/store_test.go
git commit -m "feat(codegraph): store call sites in line order, undeduped"
```

---

## Task 5: Reshape the trace payload

Done before the walk so the walk has something to fill in, and so the compile error points at every caller that needs looking at.

**Files:**
- Modify: `internal/codegraph/codegraph.go`

- [ ] **Step 1: Replace the two types**

In `internal/codegraph/codegraph.go`, replace `DataFlowStep` and `DataFlowTrace` with:

```go
// DataFlowStep is one event in the trace: a read, a write, or a call the flow
// continues into. Depth is how many calls were followed to reach it, so a flat
// slice renders as an indented tree without the JSON having to nest.
type DataFlowStep struct {
	NodeID   string `json:"nodeId"`
	Label    string `json:"label"`
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"` // "read" | "write" | "call"
	Depth    int    `json:"depth"`
	// TargetID is the callee for kind=="call", empty otherwise.
	TargetID string `json:"targetId"`
	// Unresolved is empty when nothing was lost. Otherwise it names the wall the
	// trace hit: "library" (bytecode — there is no body to read, ever),
	// "interface" (the implementation is not knowable statically), "unresolved"
	// (the solver could not resolve the call), "depth" (the ceiling below) or
	// "cycle" (this method is already on the current path).
	Unresolved string `json:"unresolved"`
}

// DataFlowTrace is TraceDataFlow's result: every already-indexed method that
// touches one field on one class, in call-graph order, with the calls between
// them shown.
type DataFlowTrace struct {
	SymbolID  string `json:"symbolId"`
	FieldName string `json:"fieldName"`
	// OrderedBy is always "source" in v1 and is in the payload so no consumer
	// mistakes it for execution order. Events within a method are sorted by
	// line; branches, loops and early returns are not modelled, so a write in
	// an else-branch still sorts before a read on a later line. Modelling that
	// needs a control-flow graph, which JavaParser does not build.
	OrderedBy string         `json:"orderedBy"`
	Steps     []DataFlowStep `json:"steps"`
}
```

- [ ] **Step 2: Confirm what breaks**

Run: `cd dev-kit-app && go build ./internal/... 2>&1 | head`
Expected: errors only in `internal/codegraph/service.go` (the old `TraceDataFlow` body) — Task 6 rewrites it. If anything in `internal/bridge` breaks, stop and read it: the bridge is supposed to pass the struct through untouched.

- [ ] **Step 3: Commit**

```bash
cd dev-kit-app
git add internal/codegraph/codegraph.go
git commit -m "feat(codegraph): trace payload carries depth, call targets and why it stopped"
```

Committing a non-building tree is deliberate here — Task 6 is the other half of one change, and splitting the type from its only consumer keeps each diff readable.

---

## Task 6: Assemble and walk

**Files:**
- Modify: `internal/codegraph/service.go`
- Test: `internal/codegraph/service_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/codegraph/service_test.go`. Build the fixture with the package's existing helpers — if `newTestService(t, fakeAnalyzer{result: ...})` is how the other tests inject an analysis result, use exactly that.

```go
// traceFixture: Controller.create calls Service.process, which writes `status`,
// calls Repository.save (which reads it), and then reads it again. Repository.save
// also calls into a library, which is where the trace has to stop and say so.
func traceFixture() AnalysisResult {
	return AnalysisResult{
		Nodes: []Node{
			{ID: "a.Controller.create()", Label: "Controller.create", Kind: "method", FilePath: "Controller.java", Line: 10},
			{ID: "a.Service.process()", Label: "Service.process", Kind: "method", FilePath: "Service.java", Line: 20},
			{ID: "a.Repository.save()", Label: "Repository.save", Kind: "method", FilePath: "Repository.java", Line: 30},
		},
		Edges: []Edge{
			{From: "a.Controller.create()", To: "a.Service.process()", Kind: "call"},
			{From: "a.Service.process()", To: "a.Repository.save()", Kind: "call"},
		},
		CallSites: []CallSite{
			{From: "a.Controller.create()", To: "a.Service.process()", Line: 12},
			{From: "a.Service.process()", To: "a.Repository.save()", Line: 23},
			{From: "a.Repository.save()", To: "", Line: 32, Unresolved: "library"},
		},
		FieldAccesses: []FieldAccess{
			{ClassID: "a.Refund", NodeID: "a.Service.process()", FieldName: "status", Kind: "write", Line: 22},
			{ClassID: "a.Refund", NodeID: "a.Service.process()", FieldName: "status", Kind: "read", Line: 25},
			{ClassID: "a.Refund", NodeID: "a.Repository.save()", FieldName: "status", Kind: "read", Line: 31},
		},
	}
}

```

Then the tests themselves:

```go
func TestTraceDataFlowOrdersEventsBySourceLineWithinAMethod(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: traceFixture()})
	if err := svc.OpenProject(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	trace, err := svc.TraceDataFlow(context.Background(), "a.Refund", "status")
	if err != nil {
		t.Fatalf("TraceDataFlow: %v", err)
	}
	if trace.OrderedBy != "source" {
		t.Errorf("OrderedBy = %q, want %q", trace.OrderedBy, "source")
	}

	// Service.process: write@22, call@23, read@25 — the call has to sit between
	// the write and the read, or the reader cannot see that the written value is
	// what flows into save().
	var got []string
	for _, s := range trace.Steps {
		if s.NodeID == "a.Service.process()" {
			got = append(got, fmt.Sprintf("%s@%d", s.Kind, s.Line))
		}
	}
	want := []string{"write@22", "call@23", "read@25"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Service.process events = %v, want %v", got, want)
	}
}

func TestTraceDataFlowFollowsCallsAndDeepens(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: traceFixture()})
	if err := svc.OpenProject(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	trace, err := svc.TraceDataFlow(context.Background(), "a.Refund", "status")
	if err != nil {
		t.Fatalf("TraceDataFlow: %v", err)
	}

	depth := map[string]int{}
	for _, s := range trace.Steps {
		if _, seen := depth[s.NodeID]; !seen {
			depth[s.NodeID] = s.Depth
		}
	}
	if depth["a.Service.process()"] != 0 {
		t.Errorf("Service.process depth = %d, want 0 — it is the root, nothing above it touches the field", depth["a.Service.process()"])
	}
	if depth["a.Repository.save()"] != 1 {
		t.Errorf("Repository.save depth = %d, want 1 — reached by following one call", depth["a.Repository.save()"])
	}
	// Controller.create never touches `status` and is not called from a method
	// that does, so it must not appear at all.
	if _, present := depth["a.Controller.create()"]; present {
		t.Error("Controller.create is in the trace, but it never touches the field")
	}
}

func TestTraceDataFlowSaysWhereItLostTheFlow(t *testing.T) {
	svc := newTestService(t, fakeAnalyzer{result: traceFixture()})
	if err := svc.OpenProject(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	trace, err := svc.TraceDataFlow(context.Background(), "a.Refund", "status")
	if err != nil {
		t.Fatalf("TraceDataFlow: %v", err)
	}

	// The whole reason unresolved calls are recorded: silence here would read as
	// "the flow ends", when the truth is "we cannot see any further".
	var found bool
	for _, s := range trace.Steps {
		if s.Kind == "call" && s.Unresolved == "library" && s.Line == 32 {
			found = true
		}
	}
	if !found {
		t.Error("no step marks the library call at Repository.java:32 as a wall")
	}
}

func TestTraceDataFlowStopsOnACycle(t *testing.T) {
	result := traceFixture()
	result.CallSites = append(result.CallSites,
		CallSite{From: "a.Repository.save()", To: "a.Service.process()", Line: 33})
	result.Edges = append(result.Edges,
		Edge{From: "a.Repository.save()", To: "a.Service.process()", Kind: "call"})

	svc := newTestService(t, fakeAnalyzer{result: result})
	if err := svc.OpenProject(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("OpenProject: %v", err)
	}

	// Must terminate, and must say why rather than silently truncating.
	trace, err := svc.TraceDataFlow(context.Background(), "a.Refund", "status")
	if err != nil {
		t.Fatalf("TraceDataFlow: %v", err)
	}
	var cycles int
	for _, s := range trace.Steps {
		if s.Unresolved == "cycle" {
			cycles++
		}
	}
	if cycles == 0 {
		t.Error("a recursive call produced no step marked \"cycle\"")
	}
}
```

Add `fmt` and `reflect` to that file's imports if they are not already there.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd dev-kit-app && go test ./internal/codegraph/ -run TestTraceDataFlow`
Expected: FAIL to compile — the old `TraceDataFlow` still builds the removed `DataFlowStep` shape.

- [ ] **Step 3: Rewrite `TraceDataFlow`**

In `internal/codegraph/service.go`, replace the whole `TraceDataFlow` method with:

```go
// maxDataFlowDepth bounds how many calls the trace will follow, the same
// defensive ceiling as maxNeighborDepth/maxFlowDepth. A trace that hits it says
// so in the step rather than just stopping.
const maxDataFlowDepth = 6

// TraceDataFlow walks the field's accesses in call-graph order.
//
// Two passes on purpose. First, find every method that touches the field —
// those are the only methods worth walking. Then depth-first from the ones that
// nothing else in that set calls, so the trace starts where the value starts
// rather than in the middle.
//
// Within a method, field accesses and outgoing call sites are merged and sorted
// by line, which is what puts "the write, then the call it flows into" next to
// each other. That ordering is SOURCE order, not execution order — see
// DataFlowTrace.OrderedBy.
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

	touching := make(map[string]bool, len(accesses))
	byMethod := make(map[string][]FieldAccess, len(accesses))
	for _, a := range accesses {
		touching[a.NodeID] = true
		byMethod[a.NodeID] = append(byMethod[a.NodeID], a)
	}

	// A root is a touching method that no other touching method calls. If every
	// one is called by another (a cycle among them), fall back to all of them so
	// the trace is never empty just because the graph loops.
	calledByTouching := make(map[string]bool)
	for id := range touching {
		sites, err := store.GetCallSites(ctx, id)
		if err != nil {
			return DataFlowTrace{}, err
		}
		for _, c := range sites {
			if c.To != "" && touching[c.To] {
				calledByTouching[c.To] = true
			}
		}
	}
	roots := make([]string, 0, len(touching))
	for id := range touching {
		if !calledByTouching[id] {
			roots = append(roots, id)
		}
	}
	if len(roots) == 0 {
		for id := range touching {
			roots = append(roots, id)
		}
	}
	sort.Strings(roots) // stable output for the same index

	trace := DataFlowTrace{SymbolID: symbolID, FieldName: fieldName, OrderedBy: "source"}
	onPath := make(map[string]bool)

	var walk func(nodeID string, depth int) error
	walk = func(nodeID string, depth int) error {
		node, err := store.GetNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, ErrNodeNotFound) {
				return nil // index drifted between reads; skip rather than fail the whole trace
			}
			return err
		}
		sites, err := store.GetCallSites(ctx, nodeID)
		if err != nil {
			return err
		}

		type event struct {
			line       int
			kind       string
			targetID   string
			unresolved string
		}
		events := make([]event, 0, len(byMethod[nodeID])+len(sites))
		for _, a := range byMethod[nodeID] {
			events = append(events, event{line: a.Line, kind: a.Kind})
		}
		for _, c := range sites {
			// Calls that lead nowhere useful are still worth a step when they
			// were lost; a resolved call to a method that never touches the
			// field is just noise.
			if c.Unresolved == "" && !touching[c.To] {
				continue
			}
			events = append(events, event{line: c.Line, kind: "call", targetID: c.To, unresolved: c.Unresolved})
		}
		sort.SliceStable(events, func(i, j int) bool { return events[i].line < events[j].line })

		onPath[nodeID] = true
		defer delete(onPath, nodeID)

		for _, e := range events {
			step := DataFlowStep{
				NodeID:     nodeID,
				Label:      node.Label,
				FilePath:   node.FilePath,
				Line:       e.line,
				Kind:       e.kind,
				Depth:      depth,
				TargetID:   e.targetID,
				Unresolved: e.unresolved,
			}
			if e.kind == "call" && e.unresolved == "" {
				switch {
				case onPath[e.targetID]:
					step.Unresolved = "cycle"
				case depth+1 > maxDataFlowDepth:
					step.Unresolved = "depth"
				}
			}
			trace.Steps = append(trace.Steps, step)

			if e.kind == "call" && step.Unresolved == "" {
				if err := walk(e.targetID, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, root := range roots {
		if err := walk(root, 0); err != nil {
			return DataFlowTrace{}, err
		}
	}
	return trace, nil
}
```

Add `sort` to that file's imports if it is not already there.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd dev-kit-app && go test ./internal/codegraph/ -run TestTraceDataFlow -v`
Expected: PASS, four tests.

- [ ] **Step 5: Run the package suite**

Run: `cd dev-kit-app && go test ./internal/codegraph/`
Expected: PASS, including the five pre-existing trace tests, **unchanged**. That is not luck and it is worth checking rather than assuming: `TestServiceTraceDataFlow`, `...NoAccessesReturnsEmpty`, `...UnknownSymbolFails`, `...BeforeOpenProjectFails` (`service_test.go:1242-1297`) and `TestCodeGraphTraceDataFlow` (`internal/bridge/codegraph_test.go:210`) all use fixtures with **no call sites**, so every touching method is a root, roots sort by id, and the emitted steps keep the same order and the same `NodeID`/`Kind`/`Label` fields they assert on. If any of them goes red, the walk is wrong — do not edit the test to match the new output.

- [ ] **Step 6: Commit**

```bash
cd dev-kit-app
git add internal/codegraph/service.go internal/codegraph/service_test.go
git commit -m "feat(codegraph): trace the field through the call graph, and name the walls"
```

---

## Task 7: Full regression

**Files:** none — verification only.

- [ ] **Step 1: Java**

Run: `cd dev-kit-app/analyzer && mvn -q test`
Expected: PASS.

- [ ] **Step 2: Go**

Run:

```bash
cd dev-kit-app && go build $(go list ./... | grep -v '/build/ios') && go test $(go list ./... | grep -v '/build/ios')
```

Expected: all `ok`. (`build/ios` is an iOS-only Wails template that has never built on the host toolchain; it fails identically on `main`.)

- [ ] **Step 3: Prove the index still round-trips end to end**

Run: `cd dev-kit-app && go test ./internal/bridge/ -run TestCodeGraph`
Expected: PASS. This is what catches a `Validate` change that rejects real analyzer output.

- [ ] **Step 4: Commit anything the pass turned up**

If steps 1–3 are green there is nothing to commit. If one fails, fix it, re-run all three, and commit the fix on its own.

---

## What this plan does NOT do

**No frontend.** `frontend/src/bridge/codegraph.ts` has no `traceDataFlow` at all, and the Figma design (`Node Data Flow`, page 14 CodeGraph) draws a flat list with no depth, no call steps and no wall markers — it predates this payload. Both need a follow-up plan; the design needs an indent level and a visual for a step whose `unresolved` is set, which is the part that carries this feature's honesty.

**No control-flow graph.** Ordering within a method is source order. A write in an `else` still sorts before a read on a later line, and a loop is one pass. `OrderedBy: "source"` is in the payload so this cannot be mistaken for execution order.

**No inter-procedural value tracking.** The trace shows that a method writes the field and then calls something; it does not claim the written value is what the callee receives. That needs parameter/return propagation, and the three walls in "Why this shape" mean it would be unsound wherever it mattered most.

**No `codegraph.mdx` update.** The doc's data-flow section still describes the old flat list and calls the result a "chuỗi trace" without saying what chains it. It should gain: the `callSites` output, the three wall reasons, and the source-order ceiling. Separate pass, but do it before this ships or the doc will describe a payload that no longer exists.
