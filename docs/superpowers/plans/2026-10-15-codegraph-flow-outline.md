# CodeGraph Flow Outline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A flow tab gets an Outline mode that reads the call flow in source order, with branches, implementations and the library calls that matter (noise folded automatically, with reasons), and a Graph mode whose cards and edges say what each node does and when it runs.

**Architecture:** The Java analyzer records, per call site, its column, text, callee key, receiver origin, result use and the enclosing control-structure frames. These go into new `call_sites` columns. Go scores each step's visibility at read time (`callimportance.go`) and serves `CodeGraphGetSteps` for the nodes of a flow. The frontend builds the outline from steps with a pure function (`buildOutline.ts`) and derives the graph's labels and hidden helpers from the same steps (`graphView.ts`). Outline and Graph are two `Tabs` modes of one flow document.

**Tech Stack:** Java 21 + JavaParser (analyzer, Maven, JUnit 5), Go 1.25 (`internal/codegraph`, `internal/preferences`, `internal/bridge`, SQLite via modernc), React 19 + TypeScript + Vite + bun test, React Flow (`@xyflow/react`), `@tanstack/react-virtual`.

**Spec:** `docs/superpowers/specs/2026-10-15-codegraph-flow-outline-design.md`

**Branch:** `feat/codegraph-flow-outline` (already created; the spec is committed on it).

---

## File structure

**Analyzer (Java, `analyzer/src/main/java/com/devkit/codegraph/`)**
- `Guards.java` — modify: `catch` becomes a guard shape; `text`/`negate`/`leaves`/`flatten` become package-visible for `Steps`.
- `StepFrame.java` — create: one enclosing control structure (`id`, `kind`, `text`, `exits`).
- `Steps.java` — create: reads a call's column, text, frames, receiver origin, result use and declared receiver type from the AST. No solver calls except one optional `NameExpr.resolve()`.
- `CallSite.java` — modify: new fields (`col`, `text`, `calleeKey`, `receiver`, `resultUse`, `frames`, `role`) + `describe(...)`.
- `ProjectAnalyzer.java` — modify: attach step info and callee key to every call site `addCallEdges` writes; mark dispatch links `role = "override"`.
- Tests: `GuardsTest.java` (modify), `StepsTest.java` (create), `StepRecordingTest.java` (create).

**Go (`internal/codegraph/`)**
- `codegraph.go` — modify: `CallSite` gains the step fields; Store interface gains `GetStepSites`, `CallStats`, `ListEffectsFrom`, `FieldWriters`.
- `steps.go` — create: `Step`, `StepFrame`, `StepEffect`, `StepsResult`, `Service.GetSteps`, the step graph and helper judge.
- `callimportance.go` — create: `CallStats`, `StepFacts`, pure `Visibility`, infrastructure table.
- `store.go` — modify: schema, migration, insert, the four new queries.
- `nilstore.go` — modify: the four new methods.
- Tests: `callimportance_test.go`, `steps_test.go`, `store_steps_test.go` (create).

**Go (other)**
- `internal/preferences/{preferences.go,sqlrepository.go,memory.go}` — modify: call-visibility overrides; test in `preferences_test.go`.
- `internal/bridge/codegraph_steps.go` — create: `CodeGraphGetSteps`, `PreferencesGetCallVisibility`, `PreferencesSetCallVisibility`.
- `internal/bridge/errors.go` — modify: map `codegraph.ErrTooManyNodes`.
- `internal/app/container.go` — modify: grant `preferences:read|write` scope `codegraph_call_visibility` to the UI.
- Test: `internal/bridge/codegraph_steps_test.go`.

**Frontend (`frontend/src/`)**
- `bridge/codegraph.ts` — modify: `CodeGraphEdge.guard`, step types, `getCodeGraphSteps`, `getCallVisibility`, `setCallVisibility`.
- `modules/codegraph/outline/buildOutline.ts` (+ `.test.ts`) — create: pure outline model.
- `modules/codegraph/outline/OutlineView.tsx` — create: the Outline mode.
- `modules/codegraph/graph/graphView.ts` (+ `.test.ts`) — create: hidden helpers, edge labels, card data from steps.
- `modules/codegraph/graph/ApiNode.tsx`, `ConnectorEdge.tsx`, `FlowCanvas.tsx` — modify.
- `modules/codegraph/codegraphStore.ts`, `flowActions.ts` — modify: steps, mode, toggled rows, overrides.
- `modules/codegraph/FlowDocument.tsx` — modify: Tabs, both modes.

**Docs:** `AGENTS.md` (new CodeGraph subsection), spec addendum.

---

## Task 0: Spec addendum for the decisions the plan pins down

**Files:**
- Modify: `docs/superpowers/specs/2026-10-15-codegraph-flow-outline-design.md` (append a section)

- [ ] **Step 1: Append the addendum**

Append at the end of the spec:

```markdown
## Addendum — decisions made while planning (2026-10-15)

- **`role` column.** `call_sites.role` is `""` for a call and `"override"` for the
  dispatch link the analyzer writes from an overridden method to its override.
  `Step.Impls` is built from these rows. They are never returned as steps.
- **Closure frame id.** A closure passed as an argument has the frame id of the
  call it is passed to (`line:col` of that call). "Wraps a project call" (rule 3)
  matches the frame id to the wrapping step's position, and the outline draws
  the closure's steps under that call row instead of under a separate heading.
- **Pure helper, as implemented.** A project callee is a helper when it is
  loaded (≤ 3 levels below the asked methods, ≤ 2000 methods in all), has no
  overrides, writes no field (`field_accesses`), and none of its own steps is
  visible. A cycle or an unloaded method counts as not a helper.
- **Utility on a field.** The utility category (rule 4) does not hide a
  discarded call on a `field` receiver (`cache.put(k, v)`): that falls through
  to rule 5 as a state write.
- **Unresolved Java types.** When the declaring type does not resolve, the
  callee key uses the declared type's simple name (`Counter#increment`), and the
  infrastructure table also matches a few simple names (`Logger`, `Counter`,
  `MeterRegistry`, …). Lombok's `log` is keyed `org.slf4j.Logger#<method>`.
- **`GetSteps` goes through the Service** that `CodeGraphGetNeighbors` uses
  (same `servicePath`), not the on-disk index reader.
- **Click on an outline row** selects it and opens the inspector, as a click on a
  card does. Enter does the same from the keyboard.
- **Windowing** uses the shared `useVirtualRows` threshold (100 rows).
- **Steps are re-fetched** for the whole subgraph after "Load more", a refresh,
  or an override change.
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/specs/2026-10-15-codegraph-flow-outline-design.md
git commit -m "docs: flow outline spec addendum from planning

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 1: `catch` is a guard shape

**Files:**
- Modify: `analyzer/src/main/java/com/devkit/codegraph/Guards.java`
- Test: `analyzer/src/test/java/com/devkit/codegraph/GuardsTest.java`

- [ ] **Step 1: Write the failing test**

Add to `GuardsTest` (it already has `guardOfTarget`):

```java
    @Test
    void aCatchBlockGuardsItsCalls() {
        assertEquals("catch IllegalStateException",
                guardOfTarget("void run() { try { x(); } catch (IllegalStateException e) { target(); } }"));
        assertEquals("", guardOfTarget("void run() { try { target(); } catch (IllegalStateException e) { y(); } }"));
        assertEquals("catch A | B", guardOfTarget("void run() { try { x(); } catch (A | B e) { target(); } }"));
    }
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd analyzer && mvn -q test -Dtest=GuardsTest#aCatchBlockGuardsItsCalls`
Expected: FAIL, `expected: <catch IllegalStateException> but was: <>`.

- [ ] **Step 3: Implement**

In `Guards.java` add the import `import com.github.javaparser.ast.stmt.CatchClause;`. In `collect(...)`, add a branch before the final `else if (parent instanceof BlockStmt block)`:

```java
        } else if (parent instanceof CatchClause clause && child == clause.getBody()) {
            add(out, "catch " + flatten(clause.getParameter().getType().toString()));
```

In the same file, make these four helpers package-visible so `Steps` can reuse them (drop `private`, keep `static`): `leaves(Statement)`, `flatten(String)`. `text(Expression)` and `negate(Expression)` are already package-visible.

Update the class Javadoc sentence "Loops and {@code try} are not conditions here" to: "Loops and the {@code try} block itself are not conditions here; a {@code catch} block is — it runs only when its exception was thrown."

- [ ] **Step 4: Run the Guards tests**

Run: `cd analyzer && mvn -q test -Dtest=GuardsTest`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add analyzer/src/main/java/com/devkit/codegraph/Guards.java analyzer/src/test/java/com/devkit/codegraph/GuardsTest.java
git commit -m "codegraph: a catch block guards the calls inside it

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `StepFrame` and `Steps.frames`

**Files:**
- Create: `analyzer/src/main/java/com/devkit/codegraph/StepFrame.java`
- Create: `analyzer/src/main/java/com/devkit/codegraph/Steps.java`
- Test: `analyzer/src/test/java/com/devkit/codegraph/StepsTest.java`

- [ ] **Step 1: Write the failing test**

```java
package com.devkit.codegraph;

import com.github.javaparser.ParserConfiguration;
import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.expr.MethodCallExpr;
import org.junit.jupiter.api.Test;

import java.util.List;
import java.util.stream.Collectors;

import static org.junit.jupiter.api.Assertions.assertEquals;

class StepsTest {

    static {
        StaticJavaParser.getParserConfiguration().setLanguageLevel(ParserConfiguration.LanguageLevel.JAVA_21);
    }

    private static MethodDeclaration run(String body) {
        CompilationUnit unit = StaticJavaParser.parse("class C { " + body + " }");
        return unit.findAll(MethodDeclaration.class).stream()
                .filter(m -> m.getNameAsString().equals("run")).findFirst().orElseThrow();
    }

    private static MethodCallExpr call(MethodDeclaration run, String name) {
        List<MethodCallExpr> calls = run.findAll(MethodCallExpr.class).stream()
                .filter(c -> c.getNameAsString().equals(name)).toList();
        assertEquals(1, calls.size(), "the snippet must call " + name + "() exactly once");
        return calls.get(0);
    }

    /** "kind:text" per frame, outermost first, with "↩" when the branch leaves. */
    private static String frames(String body, String name) {
        MethodDeclaration run = run(body);
        return Steps.frames(call(run, name), run).stream()
                .map(f -> f.kind + ":" + f.text + (f.exits ? "↩" : ""))
                .collect(Collectors.joining(" / "));
    }

    @Test
    void anUnconditionalCallHasNoFrames() {
        assertEquals("", frames("void run() { target(); }", "target"));
    }

    @Test
    void ifElseAndElseIf() {
        assertEquals("if:if flag", frames("void run() { if (flag) { target(); } }", "target"));
        assertEquals("else:else", frames("void run() { if (flag) { x(); } else { target(); } }", "target"));
        assertEquals("if:else if b", frames("void run() { if (a) { x(); } else if (b) { target(); } }", "target"));
    }

    @Test
    void aGuardClauseIsAFrameForWhatIsInsideItAndNothingAfterIt() {
        assertEquals("if:if skip↩", frames("void run() { if (skip) { target(); return; } y(); }", "target"));
        assertEquals("", frames("void run() { if (skip) { x(); return; } target(); }", "target"));
    }

    @Test
    void tryCatchAndFinally() {
        assertEquals("try:try", frames("void run() { try { target(); } catch (E e) { y(); } }", "target"));
        assertEquals("catch:catch E↩", frames("void run() { try { x(); } catch (E e) { target(); return; } }", "target"));
        assertEquals("finally:finally", frames("void run() { try { x(); } finally { target(); } }", "target"));
    }

    @Test
    void loopsSwitchesAndShortCircuits() {
        assertEquals("loop:for d : domains()", frames("void run() { for (D d : domains()) { target(d); } }", "target"));
        assertEquals("loop:while more()", frames("void run() { while (more()) { target(); } }", "target"));
        assertEquals("case:case A", frames("void run() { switch (m) { case A -> target(); default -> y(); } }", "target"));
        assertEquals("case:default", frames("void run() { switch (m) { case A -> x(); default -> target(); } }", "target"));
        assertEquals("and:ready", frames("boolean run() { return ready && target(); }", "target"));
    }

    @Test
    void aClosurePassedToACallIsFramedAtThatCall() {
        MethodDeclaration run = run("void run() { breaker.executeCallable(() -> { target(); return null; }); }");
        List<StepFrame> got = Steps.frames(call(run, "target"), run);
        assertEquals(1, got.size());
        assertEquals("closure", got.get(0).kind);
        assertEquals("breaker.executeCallable(() -> …)", got.get(0).text);
        MethodCallExpr wrapper = call(run, "executeCallable");
        int line = wrapper.getBegin().orElseThrow().line;
        int col = wrapper.getBegin().orElseThrow().column;
        assertEquals(line + ":" + col, got.get(0).id, "the frame id is the wrapping call's position");
    }

    @Test
    void framesNestOutermostFirst() {
        assertEquals("try:try / if:if ok / closure:items.forEach(i -> …)",
                frames("void run() { try { if (ok) { items.forEach(i -> target(i)); } } catch (E e) { } }", "target"));
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd analyzer && mvn -q test -Dtest=StepsTest`
Expected: compilation FAIL, `cannot find symbol: class Steps`.

- [ ] **Step 3: Create `StepFrame.java`**

```java
package com.devkit.codegraph;

/**
 * One control structure around a call — an {@code if} branch, a {@code catch},
 * a loop, a closure — as {@link CallSite#frames} lists them, outermost first.
 *
 * Language-neutral on purpose (the Go side reads the same shape from every
 * analyzer): {@code kind} is one of if, else, case, catch, finally, try, loop,
 * closure, and, or, ternary, await; {@code id} is the structure's
 * {@code line:col}, so calls in one branch share it; {@code exits} is true when
 * the branch ends by returning, throwing, breaking or continuing.
 */
public class StepFrame {
    public String id;
    public String kind;
    public String text;
    public boolean exits;

    public StepFrame() {}

    public StepFrame(String id, String kind, String text, boolean exits) {
        this.id = id;
        this.kind = kind;
        this.text = Steps.clip(text);
        this.exits = exits;
    }
}
```

- [ ] **Step 4: Create `Steps.java` with `frames`, `text`, `col`, `clip`**

```java
package com.devkit.codegraph;

import com.github.javaparser.ast.Node;
import com.github.javaparser.ast.expr.BinaryExpr;
import com.github.javaparser.ast.expr.ConditionalExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.LambdaExpr;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.stmt.CatchClause;
import com.github.javaparser.ast.stmt.DoStmt;
import com.github.javaparser.ast.stmt.ForEachStmt;
import com.github.javaparser.ast.stmt.ForStmt;
import com.github.javaparser.ast.stmt.IfStmt;
import com.github.javaparser.ast.stmt.Statement;
import com.github.javaparser.ast.stmt.SwitchEntry;
import com.github.javaparser.ast.stmt.TryStmt;
import com.github.javaparser.ast.stmt.WhileStmt;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Optional;

/**
 * What the flow outline needs to know about one call, read off the source
 * around it: where it is, how it is written, and the control structures it
 * sits in. Syntactic like {@link Guards} — nothing here is evaluated.
 *
 * Frames differ from a guard in one deliberate way: a guard clause
 * ({@code if (skip) { …; return; }}) is a frame for the calls INSIDE it and
 * nothing for the calls after it. A guard has to say that everything after
 * runs only when {@code !skip}; an outline says it better by showing the early
 * exit and leaving the rest flat.
 */
final class Steps {

    /** A call's text is a label, not a listing. */
    static final int MAX_TEXT = 120;

    private Steps() {}

    static int col(Node node) {
        return node.getBegin().map(p -> p.column).orElse(0);
    }

    /**
     * The call as written, on one line. A lambda argument is shortened to its
     * head — its body is the outline's next rows, not part of this one.
     */
    static String text(Node node) {
        if (node instanceof MethodCallExpr m && m.getArguments().stream().anyMatch(a -> a instanceof LambdaExpr)) {
            StringBuilder b = new StringBuilder();
            m.getScope().ifPresent(s -> b.append(Guards.flatten(s.toString())).append('.'));
            b.append(m.getNameAsString()).append('(');
            List<String> args = new ArrayList<>();
            for (Expression a : m.getArguments()) {
                args.add(a instanceof LambdaExpr l ? lambdaHead(l) + " -> …" : Guards.flatten(a.toString()));
            }
            b.append(String.join(", ", args)).append(')');
            return clip(b.toString());
        }
        return clip(Guards.flatten(node.toString()));
    }

    static String clip(String s) {
        return s.length() <= MAX_TEXT ? s : s.substring(0, MAX_TEXT - 1) + "…";
    }

    /** The control structures around {@code node} inside {@code scope}, outermost first. */
    static List<StepFrame> frames(Node node, Node scope) {
        List<StepFrame> inner = new ArrayList<>();
        Node child = node;
        Optional<Node> parent = node.getParentNode();
        while (parent.isPresent() && child != scope) {
            Node p = parent.get();
            StepFrame frame = frameOf(p, child);
            if (frame != null) {
                inner.add(frame);
            }
            child = p;
            parent = p.getParentNode();
        }
        Collections.reverse(inner);
        return inner;
    }

    private static StepFrame frameOf(Node parent, Node child) {
        if (parent instanceof IfStmt s) {
            if (child == s.getThenStmt()) {
                boolean elseIf = s.getParentNode()
                        .filter(g -> g instanceof IfStmt outer && outer.getElseStmt().filter(e -> e == s).isPresent())
                        .isPresent();
                return new StepFrame(at(s.getThenStmt()), "if",
                        (elseIf ? "else if " : "if ") + Guards.text(s.getCondition()), Guards.leaves(s.getThenStmt()));
            }
            Optional<Statement> otherwise = s.getElseStmt();
            // `else if` is drawn by the nested if's own frame, so the else adds nothing.
            if (otherwise.isPresent() && otherwise.get() == child && !(child instanceof IfStmt)) {
                return new StepFrame(at(child), "else", "else", Guards.leaves(otherwise.get()));
            }
            return null;
        }
        if (parent instanceof ConditionalExpr t) {
            if (child == t.getThenExpr()) {
                return new StepFrame(at(child), "ternary", Guards.text(t.getCondition()), false);
            }
            if (child == t.getElseExpr()) {
                return new StepFrame(at(child), "ternary", Guards.negate(t.getCondition()), false);
            }
            return null;
        }
        if (parent instanceof BinaryExpr b && child == b.getRight()) {
            if (b.getOperator() == BinaryExpr.Operator.AND) {
                return new StepFrame(at(child), "and", Guards.text(b.getLeft()), false);
            }
            if (b.getOperator() == BinaryExpr.Operator.OR) {
                return new StepFrame(at(child), "or", Guards.negate(b.getLeft()), false);
            }
            return null;
        }
        if (parent instanceof SwitchEntry entry && isStatementOf(entry, child)) {
            List<String> labels = new ArrayList<>();
            entry.getLabels().forEach(l -> labels.add(Guards.flatten(l.toString())));
            String text = labels.isEmpty() ? "default" : "case " + String.join(", ", labels);
            var statements = entry.getStatements();
            boolean exits = !statements.isEmpty() && Guards.leaves(statements.get(statements.size() - 1));
            return new StepFrame(at(entry), "case", text, exits);
        }
        if (parent instanceof CatchClause c && child == c.getBody()) {
            return new StepFrame(at(c), "catch", "catch " + Guards.flatten(c.getParameter().getType().toString()),
                    Guards.leaves(c.getBody()));
        }
        if (parent instanceof TryStmt t) {
            if (child == t.getTryBlock()) {
                return new StepFrame(at(t), "try", "try", false);
            }
            if (t.getFinallyBlock().filter(f -> f == child).isPresent()) {
                return new StepFrame(at(child), "finally", "finally", false);
            }
            return null;
        }
        if (parent instanceof ForEachStmt f && child == f.getBody()) {
            String name = f.getVariable().getVariables().get(0).getNameAsString();
            return new StepFrame(at(f), "loop", "for " + name + " : " + Guards.flatten(f.getIterable().toString()), false);
        }
        if (parent instanceof ForStmt f && child == f.getBody()) {
            return new StepFrame(at(f), "loop", "for " + f.getCompare().map(Guards::text).orElse("ever"), false);
        }
        if (parent instanceof WhileStmt w && child == w.getBody()) {
            return new StepFrame(at(w), "loop", "while " + Guards.text(w.getCondition()), false);
        }
        if (parent instanceof DoStmt d && child == d.getBody()) {
            return new StepFrame(at(d), "loop", "do … while " + Guards.text(d.getCondition()), false);
        }
        if (parent instanceof LambdaExpr l && child == l.getBody()) {
            Optional<Node> owner = l.getParentNode();
            if (owner.isPresent() && owner.get() instanceof MethodCallExpr m
                    && m.getArguments().stream().anyMatch(a -> a == l)) {
                // Framed at the call it is passed to, so a reader (and rule 3
                // on the Go side) can tell which call runs it.
                return new StepFrame(at(m), "closure", text(m), false);
            }
            return new StepFrame(at(l), "closure", lambdaHead(l) + " -> …", false);
        }
        return null;
    }

    /** Identity, not equals: two identical statements in different entries are different places. */
    private static boolean isStatementOf(SwitchEntry entry, Node child) {
        for (Statement s : entry.getStatements()) {
            if (s == child) {
                return true;
            }
        }
        return false;
    }

    private static String lambdaHead(LambdaExpr l) {
        List<String> names = new ArrayList<>();
        l.getParameters().forEach(p -> names.add(p.getNameAsString()));
        return names.size() == 1 && !l.isEnclosingParameters() ? names.get(0) : "(" + String.join(", ", names) + ")";
    }

    private static String at(Node n) {
        return n.getBegin().map(p -> p.line + ":" + p.column).orElse("0:0");
    }
}
```

- [ ] **Step 5: Run the tests**

Run: `cd analyzer && mvn -q test -Dtest=StepsTest`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add analyzer/src/main/java/com/devkit/codegraph/StepFrame.java analyzer/src/main/java/com/devkit/codegraph/Steps.java analyzer/src/test/java/com/devkit/codegraph/StepsTest.java
git commit -m "codegraph: read the control structures around a call

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Receiver origin, result use and declared type

**Files:**
- Modify: `analyzer/src/main/java/com/devkit/codegraph/Steps.java`
- Test: `analyzer/src/test/java/com/devkit/codegraph/StepsTest.java`

- [ ] **Step 1: Write the failing tests**

Add to `StepsTest`:

```java
    private static String receiver(String body, String name) {
        MethodDeclaration run = run(body);
        return Steps.receiver(call(run, name), run);
    }

    private static String use(String body, String name) {
        MethodDeclaration run = run(body);
        return Steps.resultUse(call(run, name));
    }

    @Test
    void whereTheReceiverComesFrom() {
        String fields = "private final java.util.List<String> sent = null; ";
        assertEquals("param", receiver("void run(Exchange exchange) { exchange.target(); }", "target"));
        assertEquals("local-from-param",
                receiver("void run(Exchange exchange) { Message in = exchange.getIn(); in.target(); }", "target"));
        assertEquals("local-from-param", receiver("void run(Exchange exchange) { exchange.getIn().target(); }", "target"));
        assertEquals("field", receiver(fields + "void run() { sent.target(); }", "target"));
        assertEquals("field", receiver(fields + "void run() { this.sent.target(); }", "target"));
        assertEquals("local", receiver("void run() { Foo f = new Foo(); f.target(); }", "target"));
        assertEquals("static", receiver("void run() { Counter.target(); }", "target"));
        assertEquals("this", receiver("void run() { target(); }", "target"));
        assertEquals("local", receiver("void run() { items.forEach(i -> i.target()); }", "target"));
    }

    @Test
    void whereTheResultGoes() {
        assertEquals("discarded", use("void run() { target(); }", "target"));
        assertEquals("assigned", use("void run() { int x = target(); }", "target"));
        assertEquals("assigned", use("void run() { x = target(); }", "target"));
        assertEquals("returned", use("int run() { return target(); }", "target"));
        assertEquals("argument", use("void run() { other(target()); }", "target"));
        assertEquals("argument", use("void run() { target().increment(); }", "target"));
        assertEquals("condition", use("void run() { if (target()) { x(); } }", "target"));
        assertEquals("condition", use("void run() { if (!target() && ok) { x(); } }", "target"));
        assertEquals("condition", use("void run() { while (target() != null) { x(); } }", "target"));
    }

    @Test
    void theDeclaredTypeOfAReceiver() {
        MethodDeclaration run = run("Counter counter(String n) { return null; } "
                + "private java.util.Map<String, Integer> cache; "
                + "void run(ProducerTemplate template) { Message in = null; template.a(); in.b(); cache.c(); counter(\"x\").d(); }");
        assertEquals("ProducerTemplate", Steps.declaredType(call(run, "a").getScope().orElseThrow(), run));
        assertEquals("Message", Steps.declaredType(call(run, "b").getScope().orElseThrow(), run));
        assertEquals("java.util.Map", Steps.declaredType(call(run, "c").getScope().orElseThrow(), run));
        assertEquals("Counter", Steps.declaredType(call(run, "d").getScope().orElseThrow(), run));
    }
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd analyzer && mvn -q test -Dtest=StepsTest`
Expected: compilation FAIL, `cannot find symbol: method receiver(...)`.

- [ ] **Step 3: Implement**

Add these imports to `Steps.java`:

```java
import com.github.javaparser.ast.body.CallableDeclaration;
import com.github.javaparser.ast.body.FieldDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.body.Parameter;
import com.github.javaparser.ast.body.RecordDeclaration;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.body.VariableDeclarator;
import com.github.javaparser.ast.expr.AssignExpr;
import com.github.javaparser.ast.expr.CastExpr;
import com.github.javaparser.ast.expr.EnclosedExpr;
import com.github.javaparser.ast.expr.FieldAccessExpr;
import com.github.javaparser.ast.expr.InstanceOfExpr;
import com.github.javaparser.ast.expr.NameExpr;
import com.github.javaparser.ast.expr.ObjectCreationExpr;
import com.github.javaparser.ast.expr.SuperExpr;
import com.github.javaparser.ast.expr.SwitchExpr;
import com.github.javaparser.ast.expr.ThisExpr;
import com.github.javaparser.ast.expr.UnaryExpr;
import com.github.javaparser.ast.stmt.ExplicitConstructorInvocationStmt;
import com.github.javaparser.ast.stmt.ExpressionStmt;
import com.github.javaparser.ast.stmt.ReturnStmt;
import com.github.javaparser.ast.stmt.SwitchStmt;
```

Add to the class:

```java
    /** Everything {@link CallSite#describe} records about one call. */
    record Info(int col, String text, String receiver, String resultUse, List<StepFrame> frames) {}

    static Info of(Node call, Node scope) {
        String receiver = call instanceof MethodCallExpr m ? receiver(m, scope) : "unknown";
        return new Info(col(call), text(call), receiver, resultUse(call), frames(call, scope));
    }

    /**
     * Where the receiver of {@code call} comes from: param, field,
     * local-from-param (a local or a chain built from a parameter), local,
     * static (a type name), this, or unknown. A call that changes something
     * the method was handed — `in.setHeader(...)` with `in = exchange.getIn()` —
     * is a step a reader wants to see; the same call on a temporary is not.
     */
    static String receiver(MethodCallExpr call, Node scope) {
        return call.getScope().map(s -> origin(s, call, scope, false)).orElse("this");
    }

    private static String origin(Expression e, Node at, Node scope, boolean derived) {
        Expression x = unwrap(e);
        if (x instanceof ThisExpr || x instanceof SuperExpr) {
            return "this";
        }
        if (x instanceof FieldAccessExpr f) {
            return f.getScope() instanceof ThisExpr ? "field" : origin(f.getScope(), at, scope, true);
        }
        if (x instanceof MethodCallExpr m) {
            return m.getScope().map(s -> origin(s, at, scope, true)).orElse("local");
        }
        if (x instanceof NameExpr n) {
            String kind = nameKind(n.getNameAsString(), at, scope);
            if (kind.equals("unknown")) {
                try {
                    if (n.resolve().isField()) {
                        kind = "field"; // inherited: declared on a superclass
                    }
                } catch (RuntimeException | StackOverflowError ignored) {
                    // stays unknown
                }
            }
            return derived && kind.equals("param") ? "local-from-param" : kind;
        }
        return "unknown";
    }

    private static String nameKind(String name, Node at, Node scope) {
        for (Node n = at; n != null && n != scope; n = n.getParentNode().orElse(null)) {
            if (n instanceof LambdaExpr l && l.getParameters().stream().anyMatch(p -> p.getNameAsString().equals(name))) {
                return "local";
            }
            if (n instanceof CatchClause c && c.getParameter().getNameAsString().equals(name)) {
                return "local";
            }
            if (n instanceof ForEachStmt f
                    && f.getVariable().getVariables().stream().anyMatch(v -> v.getNameAsString().equals(name))) {
                return "local";
            }
        }
        List<String> params = parameterNames(scope);
        for (VariableDeclarator v : scope.findAll(VariableDeclarator.class)) {
            if (!v.getNameAsString().equals(name) || v.getParentNode().filter(p -> p instanceof FieldDeclaration).isPresent()) {
                continue;
            }
            boolean fromParam = v.getInitializer()
                    .filter(init -> init.findAll(NameExpr.class).stream().anyMatch(ref -> params.contains(ref.getNameAsString())))
                    .isPresent();
            return fromParam ? "local-from-param" : "local";
        }
        if (params.contains(name)) {
            return "param";
        }
        if (fieldType(name, scope) != null) {
            return "field";
        }
        if (!name.isEmpty() && Character.isUpperCase(name.charAt(0))) {
            return "static";
        }
        return "unknown";
    }

    private static List<String> parameterNames(Node scope) {
        List<String> names = new ArrayList<>();
        if (scope instanceof CallableDeclaration<?> c) {
            for (Parameter p : c.getParameters()) {
                names.add(p.getNameAsString());
            }
        }
        return names;
    }

    /**
     * Where the value of {@code call} goes: discarded (a statement of its
     * own), assigned, returned, argument (into another call, or as the
     * receiver of the next call in a chain), condition (decides a branch),
     * or unknown.
     */
    static String resultUse(Node call) {
        Node child = call;
        Node p = call.getParentNode().orElse(null);
        while (p instanceof EnclosedExpr || p instanceof CastExpr) {
            child = p;
            p = p.getParentNode().orElse(null);
        }
        if (p == null) {
            return "unknown";
        }
        if (p instanceof ExpressionStmt) {
            return "discarded";
        }
        if (p instanceof VariableDeclarator || p instanceof AssignExpr) {
            return "assigned";
        }
        if (p instanceof ReturnStmt || p instanceof LambdaExpr) {
            return "returned";
        }
        if (p instanceof MethodCallExpr || p instanceof ObjectCreationExpr || p instanceof ExplicitConstructorInvocationStmt) {
            return "argument";
        }
        if (inCondition(child, p)) {
            return "condition";
        }
        if (p instanceof BinaryExpr || p instanceof UnaryExpr || p instanceof InstanceOfExpr || p instanceof ConditionalExpr) {
            Node c = p;
            Node q = p.getParentNode().orElse(null);
            while (q instanceof BinaryExpr || q instanceof UnaryExpr || q instanceof EnclosedExpr || q instanceof InstanceOfExpr) {
                c = q;
                q = q.getParentNode().orElse(null);
            }
            return q != null && inCondition(c, q) ? "condition" : "argument";
        }
        return "unknown";
    }

    private static boolean inCondition(Node child, Node p) {
        return (p instanceof IfStmt s && s.getCondition() == child)
                || (p instanceof WhileStmt w && w.getCondition() == child)
                || (p instanceof DoStmt d && d.getCondition() == child)
                || (p instanceof ForStmt f && f.getCompare().filter(c -> c == child).isPresent())
                || (p instanceof ConditionalExpr t && t.getCondition() == child)
                || (p instanceof SwitchStmt s2 && s2.getSelector() == child)
                || (p instanceof SwitchExpr s3 && s3.getSelector() == child);
    }

    /**
     * The receiver's declared type as written (generics dropped), for a callee
     * key when the solver could not resolve the call: a parameter, a local, a
     * field, or the return type of a method of the same class.
     */
    static String declaredType(Expression receiver, Node scope) {
        Expression x = unwrap(receiver);
        if (x instanceof MethodCallExpr m && m.getScope().isEmpty()) {
            return scope.findAncestor(TypeDeclaration.class)
                    .flatMap(td -> ((TypeDeclaration<?>) td).getMethodsByName(m.getNameAsString()).stream().findFirst())
                    .map(md -> simple(((MethodDeclaration) md).getType().toString()))
                    .orElse(null);
        }
        if (!(x instanceof NameExpr n)) {
            return null;
        }
        String name = n.getNameAsString();
        if (scope instanceof CallableDeclaration<?> c) {
            for (Parameter p : c.getParameters()) {
                if (p.getNameAsString().equals(name)) {
                    return simple(p.getType().toString());
                }
            }
        }
        for (VariableDeclarator v : scope.findAll(VariableDeclarator.class)) {
            if (v.getNameAsString().equals(name) && !v.getType().isVarType()) {
                return simple(v.getType().toString());
            }
        }
        return fieldType(name, scope);
    }

    private static String fieldType(String name, Node scope) {
        Optional<TypeDeclaration> owner = scope.findAncestor(TypeDeclaration.class);
        if (owner.isEmpty()) {
            return null;
        }
        TypeDeclaration<?> td = owner.get();
        Optional<FieldDeclaration> field = td.getFieldByName(name);
        if (field.isPresent()) {
            return simple(field.get().getVariable(0).getType().toString());
        }
        if (td instanceof RecordDeclaration r) {
            for (Parameter p : r.getParameters()) {
                if (p.getNameAsString().equals(name)) {
                    return simple(p.getType().toString());
                }
            }
        }
        return null;
    }

    private static String simple(String type) {
        int lt = type.indexOf('<');
        return lt >= 0 ? type.substring(0, lt) : type;
    }

    private static Expression unwrap(Expression e) {
        Expression x = e;
        while (true) {
            if (x instanceof EnclosedExpr en) {
                x = en.getInner();
            } else if (x instanceof CastExpr c) {
                x = c.getExpression();
            } else {
                return x;
            }
        }
    }
```

Note: `nameKind` previously checked fields with a helper; it now uses `fieldType(name, scope) != null`, so there is one field lookup.

- [ ] **Step 4: Run the tests**

Run: `cd analyzer && mvn -q test -Dtest=StepsTest`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add analyzer/src/main/java/com/devkit/codegraph/Steps.java analyzer/src/test/java/com/devkit/codegraph/StepsTest.java
git commit -m "codegraph: read a call's receiver origin, result use and declared type

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Record steps on every call site

**Files:**
- Modify: `analyzer/src/main/java/com/devkit/codegraph/CallSite.java`
- Modify: `analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java` (`addCallEdges` ~907–1030, `recordCall` ~1074, dispatch ~700)
- Test: `analyzer/src/test/java/com/devkit/codegraph/StepRecordingTest.java`

- [ ] **Step 1: Write the failing test**

```java
package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.stream.Collectors;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

class StepRecordingTest {

    private static final String SOURCE = """
            package acme;
            import java.util.ArrayList;
            import java.util.List;
            import java.util.Optional;
            abstract class Base {
                protected final List<String> sent = new ArrayList<>();
                public final void process(String message) {
                    String target;
                    try {
                        target = decide(message);
                    } catch (IllegalStateException e) {
                        quarantine(message);
                        return;
                    }
                    if (target == null) {
                        return;
                    }
                    Optional.of(target).ifPresent(t -> deliver(t, message));
                    sent.add(message);
                }
                protected abstract String decide(String message);
                protected abstract void deliver(String target, String message);
                private void quarantine(String message) { sent.add("q:" + message); }
            }
            class Impl extends Base {
                protected String decide(String message) { return message.isEmpty() ? null : "t"; }
                protected void deliver(String target, String message) { sent.add(target); }
            }
            """;

    private static List<CallSite> sitesOf(Path root, String fromPrefix) throws IOException {
        Path file = root.resolve("src/main/java/acme/Base.java");
        Files.createDirectories(file.getParent());
        Files.writeString(file, SOURCE);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();
        return r.callSites.stream().filter(c -> c.from.startsWith(fromPrefix)).collect(Collectors.toList());
    }

    private static CallSite byText(List<CallSite> sites, String textPrefix) {
        return sites.stream().filter(c -> c.text.startsWith(textPrefix)).findFirst()
                .orElseThrow(() -> new AssertionError("no site starting " + textPrefix + " in " + sites.stream().map(c -> c.text).toList()));
    }

    private static String frames(CallSite c) {
        return c.frames.stream().map(f -> f.kind + ":" + f.text + (f.exits ? "↩" : "")).collect(Collectors.joining(" / "));
    }

    @Test
    void everyCallOfProcessCarriesItsStep(@TempDir Path root) throws IOException {
        List<CallSite> sites = sitesOf(root, "acme.Base.process(");

        CallSite decide = byText(sites, "decide(message)");
        assertEquals("try:try", frames(decide));
        assertEquals("assigned", decide.resultUse);
        assertEquals("acme.Base#decide", decide.calleeKey);

        CallSite quarantine = byText(sites, "quarantine(message)");
        assertEquals("catch:catch IllegalStateException↩", frames(quarantine));
        assertEquals("discarded", quarantine.resultUse);

        CallSite ifPresent = byText(sites, "Optional.of(target).ifPresent(");
        assertEquals("java.util.Optional#ifPresent", ifPresent.calleeKey);
        assertEquals("Optional.of(target).ifPresent(t -> …)", ifPresent.text);
        assertEquals("", frames(ifPresent), "the guard clause above leaves no frame behind");

        CallSite deliver = byText(sites, "deliver(t, message)");
        assertEquals("closure", deliver.frames.get(0).kind);
        assertEquals(ifPresent.line + ":" + ifPresent.col, deliver.frames.get(0).id);

        CallSite add = sites.stream().filter(c -> c.text.equals("sent.add(message)")).findFirst().orElseThrow();
        assertEquals("java.util.List#add", add.calleeKey);
        assertEquals("field", add.receiver);
        assertEquals("discarded", add.resultUse);
        assertTrue(add.col > 0);
    }

    @Test
    void aDispatchLinkIsMarkedAsAnOverride(@TempDir Path root) throws IOException {
        List<CallSite> sites = sitesOf(root, "acme.Base.decide(");
        assertEquals(1, sites.size());
        assertEquals("override", sites.get(0).role);
        assertTrue(sites.get(0).to.startsWith("acme.Impl.decide("));
    }
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd analyzer && mvn -q test -Dtest=StepRecordingTest`
Expected: compilation FAIL, `cannot find symbol: variable text`.

- [ ] **Step 3: Extend `CallSite.java`**

Add imports `import java.util.ArrayList;` and `import java.util.List;`, then add to the class (after `unresolved`):

```java
    // What the flow outline reads (see Steps). Empty on a site the analyzer
    // has nothing to say about, which is how every older index reads too.
    public int col;
    public String text = "";
    public String calleeKey = "";  // "<declaring type>#<member>"
    public String receiver = "";   // param | field | local-from-param | local | static | this | unknown
    public String resultUse = "";  // discarded | assigned | returned | argument | condition | unknown
    public List<StepFrame> frames = new ArrayList<>();
    public String role = "";       // "" for a call, "override" for a dispatch link to an override

    CallSite describe(Steps.Info info, String calleeKey) {
        this.col = info.col();
        this.text = info.text();
        this.receiver = info.receiver();
        this.resultUse = info.resultUse();
        this.frames = info.frames();
        this.calleeKey = calleeKey == null ? "" : calleeKey;
        return this;
    }
```

- [ ] **Step 4: Wire `ProjectAnalyzer.addCallEdges`**

1. At the top of the `for (MethodCallExpr call : …)` loop body, right after `int line = …;`, add:

```java
            Steps.Info info = Steps.of(call, scope.node());
```

2. Replace the unresolved branch's loop

```java
                    for (String owner : scope.ownerIds()) {
                        result.callSites.add(new CallSite(owner, "", line, reason));
                    }
```

with

```java
                    String key = reason.equals("lombok") && isLogCall(call)
                            ? "org.slf4j.Logger#" + call.getNameAsString()
                            : calleeKey(call, null, scope.node());
                    for (String owner : scope.ownerIds()) {
                        result.callSites.add(new CallSite(owner, "", line, reason).describe(info, key));
                    }
```

3. Replace the library/interface branch's loop

```java
                for (String owner : scope.ownerIds()) {
                    result.callSites.add(new CallSite(owner, "", line, isInterface ? "interface" : "library"));
                }
```

with

```java
                String key = calleeKey(call, target, scope.node());
                for (String owner : scope.ownerIds()) {
                    result.callSites.add(new CallSite(owner, "", line, isInterface ? "interface" : "library").describe(info, key));
                }
```

4. Change the three `recordCall(...)` calls to pass step info and a key:

```java
            recordCall(scope, toNodeId, line, dangling ? "interface" : "", index, result, Guards.of(call, scope.node()),
                    info, keyOfNodeId(toNodeId));
```

```java
                recordCall(scope, toNodeId, construction.getRange().map(r -> r.begin.line).orElse(0), "", index, result,
                        Guards.of(construction, scope.node()), Steps.of(construction, scope.node()), keyOfNodeId(toNodeId));
```

```java
            recordCall(scope, toNodeId, reference.getRange().map(r -> r.begin.line).orElse(0),
                    dangling ? "interface" : "", index, result, Guards.of(reference, scope.node()),
                    Steps.of(reference, scope.node()), keyOfNodeId(toNodeId));
```

5. Change `recordCall`'s signature and its call-site line:

```java
    private void recordCall(CallScope scope, String toNodeId, int line, String unresolved, Index index,
                            AnalysisResult result, String guard, Steps.Info info, String calleeKey) {
        for (String owner : scope.ownerIds()) {
            // A wall keeps the shape every other wall has: no callee named.
            result.callSites.add(new CallSite(owner, unresolved.isEmpty() ? toNodeId : "", line, unresolved)
                    .describe(info, calleeKey));
            result.edges.add(new Edge(owner, toNodeId, "call", guard));
        }
```

(the rest of `recordCall` is unchanged).

6. Add the helpers next to `simpleNameOf`:

```java
    /**
     * "<declaring type>#<member>": what scores a call and what a person marks
     * "always show". The solver's answer first; then the receiver's resolved
     * type; then the type the receiver was declared with; then a type name used
     * as a receiver; then the receiver as written.
     */
    private static String calleeKey(MethodCallExpr call, ResolvedMethodDeclaration target,
                                    com.github.javaparser.ast.Node scope) {
        String name = call.getNameAsString();
        if (target != null) {
            try {
                return target.declaringType().getQualifiedName() + "#" + name;
            } catch (RuntimeException | StackOverflowError ignored) {
                // falls through
            }
        }
        if (call.getScope().isEmpty()) {
            return "#" + name;
        }
        Expression receiver = call.getScope().get();
        try {
            var type = receiver.calculateResolvedType();
            if (type.isReferenceType()) {
                return type.asReferenceType().getQualifiedName() + "#" + name;
            }
        } catch (RuntimeException | StackOverflowError ignored) {
            // falls through
        }
        String declared = Steps.declaredType(receiver, scope);
        if (declared != null) {
            return declared + "#" + name;
        }
        return receiver.toString().replaceAll("\\s+", " ") + "#" + name;
    }

    /** A node id's key: `acme.Base.decide(java.lang.String)` → `acme.Base#decide`. */
    private static String keyOfNodeId(String id) {
        int paren = id.indexOf('(');
        String head = paren >= 0 ? id.substring(0, paren) : id;
        int dot = head.lastIndexOf('.');
        return dot < 0 ? "#" + head : head.substring(0, dot) + "#" + head.substring(dot + 1);
    }

    private static boolean isLogCall(MethodCallExpr call) {
        return call.getScope().filter(s -> s instanceof NameExpr n && n.getNameAsString().equals("log")).isPresent();
    }
```

Add any missing imports to `ProjectAnalyzer.java`: `com.github.javaparser.ast.expr.Expression`, `com.github.javaparser.ast.expr.NameExpr` (check the import block first; most are already there).

7. Mark the dispatch link (around line 702). Replace

```java
                            result.callSites.add(new CallSite(overridden.id(), method.id(), overridden.line(), ""));
```

with

```java
                            CallSite link = new CallSite(overridden.id(), method.id(), overridden.line(), "");
                            link.role = "override";
                            result.callSites.add(link);
```

- [ ] **Step 5: Run the new test and the whole analyzer suite**

Run: `cd analyzer && mvn -q test -Dtest=StepRecordingTest && mvn -q test`
Expected: all PASS. If `ProjectAnalyzerTest` compares whole `CallSite` JSON, update its expectations to include the new fields, only where they differ.

- [ ] **Step 6: Commit**

```bash
git add analyzer/src/main/java/com/devkit/codegraph/CallSite.java analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java analyzer/src/test/java/com/devkit/codegraph/StepRecordingTest.java
git commit -m "codegraph: record each call's step for the flow outline

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Store the step columns (Go)

**Files:**
- Modify: `internal/codegraph/codegraph.go` (`CallSite` ~325, Store interface ~548)
- Modify: `internal/codegraph/store.go` (schema ~146, migrations ~277, insert ~486, new queries)
- Modify: `internal/codegraph/nilstore.go`
- Create: `internal/codegraph/steps.go` (types only in this task)
- Test: `internal/codegraph/store_steps_test.go`

- [ ] **Step 1: Write the failing test**

```go
package codegraph

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A call site's step survives the store, in source order, with its frames.
func TestStoreKeepsCallSiteSteps(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	err := store.ReplaceIndex(ctx, AnalysisResult{
		Nodes: []Node{method("a"), method("b"), method("c")},
		CallSites: []CallSite{
			{From: "a", To: "", Line: 9, Col: 5, Unresolved: "library", Text: "log.warn(x)", CalleeKey: "org.slf4j.Logger#warn",
				Receiver: "field", ResultUse: "discarded"},
			{From: "a", To: "b", Line: 9, Col: 1, Text: "b()", CalleeKey: "acme#b", Receiver: "this", ResultUse: "discarded",
				Frames: []StepFrame{{ID: "8:3", Kind: "catch", Text: "catch E", Exits: true}}},
			{From: "b", To: "c", Line: 2, Role: RoleOverride},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	sites, err := store.GetStepSites(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 3 || sites[0].To != "b" || sites[1].CalleeKey != "org.slf4j.Logger#warn" || sites[2].Role != RoleOverride {
		t.Fatalf("sites = %+v", sites)
	}
	if f := sites[0].Frames; len(f) != 1 || f[0] != (StepFrame{ID: "8:3", Kind: "catch", Text: "catch E", Exits: true}) {
		t.Fatalf("frames = %+v", f)
	}
	stats, err := store.CallStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Methods != 3 || stats.Callers["org.slf4j.Logger#warn"] != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

// An index written before steps opens and reads its call sites with no step.
func TestStoreAddsStepColumnsToAnOldIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE call_sites (from_id TEXT NOT NULL, to_id TEXT NOT NULL, line INTEGER NOT NULL, unresolved TEXT NOT NULL);
INSERT INTO call_sites VALUES ('a', 'b', 3, '');`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	store, err := newSQLiteStore(path)
	if err != nil {
		t.Fatalf("open old index: %v", err)
	}
	defer store.Close()
	sites, err := store.GetStepSites(t.Context(), []string{"a"})
	if err != nil || len(sites) != 1 || sites[0].Text != "" || sites[0].Frames != nil {
		t.Fatalf("sites = %+v, err = %v", sites, err)
	}
}

// What extractors saw in a method comes back keyed by the line it is on.
func TestStoreListsEffectsAndFieldWriters(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	err := store.ReplaceIndex(ctx, AnalysisResult{
		Nodes:         []Node{method("a"), method("b")},
		KafkaAccesses: []KafkaAccess{{From: "a", Topic: "orders", Role: KafkaProduce, Client: KafkaTemplate, File: "a.java", Line: 4}},
		RedisAccesses: []RedisAccess{{From: "a", Key: "k:{id}", Access: "write", Command: "set", File: "a.java", Line: 5}},
		FieldAccesses: []FieldAccess{{ClassID: "C", NodeID: "b", FieldName: "state", Kind: "write", Line: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := store.ListEffectsFrom(ctx, []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(effects) != 2 || effects[0].Line != 4 || effects[0].Effect != (StepEffect{Kind: "kafka", Role: "produce", Target: "orders"}) ||
		effects[1].Effect != (StepEffect{Kind: "redis", Role: "write", Target: "k:{id}"}) {
		t.Fatalf("effects = %+v", effects)
	}
	writers, err := store.FieldWriters(ctx, []string{"a", "b"})
	if err != nil || !writers["b"] || writers["a"] {
		t.Fatalf("writers = %v, err = %v", writers, err)
	}
}
```

Before running, check the field names of `FieldAccess` in `codegraph.go` (`grep -n "type FieldAccess struct" -A8 internal/codegraph/codegraph.go`) and adjust the literal if they differ.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/codegraph/ -run 'TestStoreKeepsCallSiteSteps|TestStoreAddsStepColumns|TestStoreListsEffects' -count=1`
Expected: compile FAIL (`unknown field Col in struct literal`).

- [ ] **Step 3: Add the step types (`steps.go`)**

```go
package codegraph

import "errors"

// RoleOverride marks the call site the Java analyzer writes from an overridden
// method to an override: a dispatch link, not a call anyone wrote. Step.Impls
// is built from these; they are never returned as steps.
const RoleOverride = "override"

// ErrTooManyNodes is returned when one GetSteps call asks for more methods
// than MaxStepNodeIDs; the frontend asks in chunks.
var ErrTooManyNodes = errors.New("codegraph: too many node ids")

// StepFrame is one control structure around a call, outermost first. It is
// language-neutral: Kind is one of if, else, case, catch, finally, try, loop,
// closure, and, or, ternary, await (open — a reader shows an unknown kind by
// its Text); ID is the structure's "line:col", shared by every call in it, and
// for a closure passed as an argument it is the position of the call it is
// passed to; Exits is true when the branch ends in return/throw/break/continue.
type StepFrame struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Exits bool   `json:"exits,omitempty"`
}

// StepEffect is what an extractor recognised at a call: a Kafka produce, an
// HTTP call, a Redis or DB access, as recorded (not evaluated).
type StepEffect struct {
	Kind   string `json:"kind"`   // kafka | http | redis | db
	Role   string `json:"role"`   // produce | consume | call | read | write
	Target string `json:"target"` // topic, method + url, key pattern or table
}

// StepEffectAt is a StepEffect with the method and line it was seen at.
type StepEffectAt struct {
	From   string
	Line   int
	Effect StepEffect
}
```

- [ ] **Step 4: Extend `CallSite` and the Store interface (`codegraph.go`)**

Replace the `CallSite` struct with:

```go
type CallSite struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Line       int    `json:"line"`
	Unresolved string `json:"unresolved"`

	// The step the flow outline reads; empty from an analyzer (or an index)
	// that does not record it. See steps.go for what each field means.
	Col       int         `json:"col,omitempty"`
	Text      string      `json:"text,omitempty"`
	CalleeKey string      `json:"calleeKey,omitempty"` // "<module-or-type>#<member>"
	Receiver  string      `json:"receiver,omitempty"`  // param | field | local-from-param | local | static | this | unknown
	ResultUse string      `json:"resultUse,omitempty"` // discarded | assigned | returned | argument | condition | unknown
	Frames    []StepFrame `json:"frames,omitempty"`
	Role      string      `json:"role,omitempty"` // "" | RoleOverride
}
```

Add to the `Store` interface, after `GetCallSitesTo`:

```go
	// GetStepSites returns every call site of the given callers with its step,
	// ordered by caller, line and column — the flow outline's input.
	GetStepSites(ctx context.Context, fromIDs []string) ([]CallSite, error)
	// CallStats counts, per callee key, the distinct methods calling it, and
	// the methods in the index.
	CallStats(ctx context.Context) (CallStats, error)
	// ListEffectsFrom returns what extractors recognised in the given
	// callers' bodies (Kafka, HTTP, Redis, DB), ordered by caller and line.
	ListEffectsFrom(ctx context.Context, fromIDs []string) ([]StepEffectAt, error)
	// FieldWriters reports which of the given methods write a field.
	FieldWriters(ctx context.Context, ids []string) (map[string]bool, error)
```

`CallStats` is defined in Task 6. Add a temporary definition at the bottom of `steps.go` now, and move it in Task 6:

```go
// CallStats is how often each callee is called across the index.
type CallStats struct {
	Callers map[string]int // callee key → distinct calling methods
	Methods int            // method nodes in the index
}
```

- [ ] **Step 5: Schema, migration and insert (`store.go`)**

1. In the schema string, replace the `call_sites` table with:

```sql
CREATE TABLE IF NOT EXISTS call_sites (
	from_id TEXT NOT NULL,
	to_id TEXT NOT NULL,
	line INTEGER NOT NULL,
	unresolved TEXT NOT NULL,
	col INTEGER NOT NULL DEFAULT 0,
	text TEXT NOT NULL DEFAULT '',
	callee_key TEXT NOT NULL DEFAULT '',
	receiver TEXT NOT NULL DEFAULT '',
	result_use TEXT NOT NULL DEFAULT '',
	frames TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT ''
);
```

2. Add the migration after `migrateEdgeGuard`:

```go
// migrateCallSiteSteps adds the step columns to an index written before the
// flow outline. Its call sites read with no step — a flat outline — until the
// next re-index fills them in.
func migrateCallSiteSteps(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(call_sites)")
	if err != nil {
		return fmt.Errorf("codegraph: inspect call_sites table: %w", err)
	}
	have := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt *string
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return fmt.Errorf("codegraph: scan table_info: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("codegraph: table_info rows: %w", err)
	}
	rows.Close()
	columns := []struct{ name, ddl string }{
		{"col", "INTEGER NOT NULL DEFAULT 0"},
		{"text", "TEXT NOT NULL DEFAULT ''"},
		{"callee_key", "TEXT NOT NULL DEFAULT ''"},
		{"receiver", "TEXT NOT NULL DEFAULT ''"},
		{"result_use", "TEXT NOT NULL DEFAULT ''"},
		{"frames", "TEXT NOT NULL DEFAULT ''"},
		{"role", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, c := range columns {
		if have[c.name] {
			continue
		}
		if _, err := db.Exec("ALTER TABLE call_sites ADD COLUMN " + c.name + " " + c.ddl); err != nil {
			return fmt.Errorf("codegraph: add call_sites.%s column: %w", c.name, err)
		}
	}
	return nil
}
```

and call it right after `migrateEdgeGuard(db)`:

```go
	if err := migrateCallSiteSteps(db); err != nil {
		return err
	}
```

3. Replace the call-site insert in `ReplaceIndex`:

```go
	siteStmt, err := insert(`INSERT INTO call_sites (from_id, to_id, line, unresolved, col, text, callee_key, receiver, result_use, frames, role)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer siteStmt.Close()
	for _, c := range result.CallSites {
		frames := ""
		if len(c.Frames) > 0 {
			body, err := json.Marshal(c.Frames)
			if err != nil {
				return fmt.Errorf("codegraph: encode frames of %q: %w", c.From, err)
			}
			frames = string(body)
		}
		if _, err := siteStmt.ExecContext(ctx, c.From, c.To, c.Line, c.Unresolved, c.Col, c.Text, c.CalleeKey,
			c.Receiver, c.ResultUse, frames, c.Role); err != nil {
			return fmt.Errorf("codegraph: insert call site %q->%q: %w", c.From, c.To, err)
		}
	}
```

(`encoding/json` is already imported by `store.go` for `params`; add it if not.)

- [ ] **Step 6: The four queries (`store.go`)**

```go
func (s *sqliteStore) GetStepSites(ctx context.Context, fromIDs []string) ([]CallSite, error) {
	var out []CallSite
	for start := 0; start < len(fromIDs); start += batchSize {
		chunk := fromIDs[start:min(start+batchSize, len(fromIDs))]
		rows, err := s.db.QueryContext(ctx,
			`SELECT from_id, to_id, line, unresolved, col, text, callee_key, receiver, result_use, frames, role
			   FROM call_sites WHERE from_id IN (`+placeholders(len(chunk))+`)
			  ORDER BY from_id, line, col, to_id`, stringArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("codegraph: GetStepSites: %w", err)
		}
		for rows.Next() {
			var c CallSite
			var frames string
			if err := rows.Scan(&c.From, &c.To, &c.Line, &c.Unresolved, &c.Col, &c.Text, &c.CalleeKey,
				&c.Receiver, &c.ResultUse, &frames, &c.Role); err != nil {
				rows.Close()
				return nil, fmt.Errorf("codegraph: scan step site: %w", err)
			}
			if frames != "" {
				// A row that cannot be read keeps its call, without frames:
				// one bad row must not take the method's outline with it.
				if err := json.Unmarshal([]byte(frames), &c.Frames); err != nil {
					c.Frames = nil
				}
			}
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func (s *sqliteStore) CallStats(ctx context.Context) (CallStats, error) {
	stats := CallStats{Callers: map[string]int{}}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE kind = 'method'`).Scan(&stats.Methods); err != nil {
		return CallStats{}, fmt.Errorf("codegraph: count methods: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT callee_key, COUNT(DISTINCT from_id) FROM call_sites WHERE callee_key != '' AND role = '' GROUP BY callee_key`)
	if err != nil {
		return CallStats{}, fmt.Errorf("codegraph: CallStats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return CallStats{}, fmt.Errorf("codegraph: scan call stats: %w", err)
		}
		stats.Callers[key] = n
	}
	return stats, rows.Err()
}

func (s *sqliteStore) ListEffectsFrom(ctx context.Context, fromIDs []string) ([]StepEffectAt, error) {
	var out []StepEffectAt
	for start := 0; start < len(fromIDs); start += batchSize {
		chunk := fromIDs[start:min(start+batchSize, len(fromIDs))]
		in := placeholders(len(chunk))
		args := make([]any, 0, 4*len(chunk))
		for range 4 {
			args = append(args, stringArgs(chunk)...)
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT from_id, line, 'kafka', role, topic FROM kafka_accesses WHERE from_id IN (`+in+`)
			UNION ALL
			SELECT from_id, line, 'http', 'call', TRIM(http_method || ' ' || url) FROM outbound_calls WHERE from_id IN (`+in+`)
			UNION ALL
			SELECT from_id, line, 'redis', access, key FROM redis_accesses WHERE from_id IN (`+in+`)
			UNION ALL
			SELECT from_id, line, 'db', access, CASE WHEN schema_name = '' THEN table_name ELSE schema_name || '.' || table_name END
			  FROM table_accesses WHERE from_id IN (`+in+`)
			ORDER BY 1, 2`, args...)
		if err != nil {
			return nil, fmt.Errorf("codegraph: ListEffectsFrom: %w", err)
		}
		for rows.Next() {
			var e StepEffectAt
			if err := rows.Scan(&e.From, &e.Line, &e.Effect.Kind, &e.Effect.Role, &e.Effect.Target); err != nil {
				rows.Close()
				return nil, fmt.Errorf("codegraph: scan effect: %w", err)
			}
			out = append(out, e)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func (s *sqliteStore) FieldWriters(ctx context.Context, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	for start := 0; start < len(ids); start += batchSize {
		chunk := ids[start:min(start+batchSize, len(ids))]
		rows, err := s.db.QueryContext(ctx,
			`SELECT DISTINCT node_id FROM field_accesses WHERE kind = 'write' AND node_id IN (`+placeholders(len(chunk))+`)`,
			stringArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("codegraph: FieldWriters: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, fmt.Errorf("codegraph: scan field writer: %w", err)
			}
			out[id] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}
```

Check `stringArgs` returns `[]any` (`grep -n "func stringArgs" internal/codegraph/store.go`); if it returns `[]interface{}`, the code above is unchanged.

- [ ] **Step 7: `nilstore.go`**

Add, following the file's pattern for "no project" answers:

```go
func (nilStore) GetStepSites(context.Context, []string) ([]CallSite, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) CallStats(context.Context) (CallStats, error) { return CallStats{}, ErrNoActiveProject }
func (nilStore) ListEffectsFrom(context.Context, []string) ([]StepEffectAt, error) {
	return nil, ErrNoActiveProject
}
func (nilStore) FieldWriters(context.Context, []string) (map[string]bool, error) {
	return nil, ErrNoActiveProject
}
```

(Look at how `nilStore.GetCallSitesTo` answers and use the same error value if it is not `ErrNoActiveProject`.) Then search for any other `Store` implementation: `grep -rn "func (.*) GetCallSitesTo(" internal cmd` — add the same four methods to each one found.

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/codegraph/ -count=1`
Expected: all PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/store.go internal/codegraph/nilstore.go internal/codegraph/steps.go internal/codegraph/store_steps_test.go
git commit -m "codegraph: store each call site's step

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Visibility scoring (`callimportance.go`)

**Files:**
- Create: `internal/codegraph/callimportance.go`
- Modify: `internal/codegraph/steps.go` (move `CallStats` out)
- Test: `internal/codegraph/callimportance_test.go`

- [ ] **Step 1: Write the failing test**

```go
package codegraph

import "testing"

func TestVisibility(t *testing.T) {
	stats := CallStats{Methods: 40, Callers: map[string]int{"acme.Audit#record": 12, "acme.Rare#call": 2}}
	cases := []struct {
		name    string
		facts   StepFacts
		over    map[string]string
		visible bool
		reason  string
	}{
		// The worked example (spec §2): fsap-event-router's process() and quarantine().
		{"setHeader writes to the message", StepFacts{CalleeKey: "org.apache.camel.Message#setHeader", Text: "in.setHeader(X, y)",
			Receiver: "local-from-param", ResultUse: "discarded"}, nil, true, "writes to in"},
		{"producerTemplate.send", StepFacts{CalleeKey: "org.apache.camel.ProducerTemplate#send", Text: "producerTemplate.send(uri, exchange)",
			Receiver: "field", ResultUse: "discarded"}, nil, true, "writes to producerTemplate"},
		{"executeCallable wraps deliver", StepFacts{CalleeKey: "io.github.resilience4j.circuitbreaker.CircuitBreaker#executeCallable",
			Receiver: "local", ResultUse: "discarded", WrapsProjectCall: true}, nil, true, "wraps a project call"},
		{"log.warn", StepFacts{CalleeKey: "org.slf4j.Logger#warn", Receiver: "unknown", ResultUse: "discarded"}, nil, false, "logging"},
		{"counter increment, unresolved type", StepFacts{CalleeKey: "Counter#increment", Receiver: "local", ResultUse: "discarded"}, nil, false, "metrics"},
		{"exchange.getIn", StepFacts{CalleeKey: "org.apache.camel.Exchange#getIn", Receiver: "param", ResultUse: "assigned"}, nil, false, "accessor"},
		{"in.getHeader", StepFacts{CalleeKey: "org.apache.camel.Message#getHeader", Receiver: "local-from-param", ResultUse: "argument"}, nil, false, "accessor"},
		{"circuitBreaker(...) is assigned", StepFacts{CalleeKey: "io.github.resilience4j.circuitbreaker.CircuitBreakerRegistry#circuitBreaker",
			Receiver: "field", ResultUse: "assigned"}, nil, false, "value"},
		{"a project call", StepFacts{CalleeKey: "acme.P#quarantine", Project: true}, nil, true, "project"},
		{"a project helper", StepFacts{CalleeKey: "acme.P#metricPrefix", Project: true, Helper: true}, nil, false, "helper"},

		// One row per remaining rule.
		{"override show beats infra", StepFacts{CalleeKey: "org.slf4j.Logger#warn"}, map[string]string{"org.slf4j.Logger#warn": "show"}, true, "you chose"},
		{"type-wide override", StepFacts{CalleeKey: "org.apache.camel.Message#setHeader", Receiver: "local-from-param", ResultUse: "discarded"},
			map[string]string{"org.apache.camel.Message#*": "hide"}, false, "you chose"},
		{"method override beats type override", StepFacts{CalleeKey: "org.apache.camel.Message#setHeader"},
			map[string]string{"org.apache.camel.Message#*": "hide", "org.apache.camel.Message#setHeader": "show"}, true, "you chose"},
		{"an effect", StepFacts{CalleeKey: "org.springframework.kafka.core.KafkaTemplate#send", Effect: &StepEffect{Kind: "kafka", Role: "produce", Target: "t"}},
			nil, true, "Kafka produce"},
		{"decides a branch", StepFacts{CalleeKey: "acme.Lib#ready", ResultUse: "condition"}, nil, true, "decides a branch"},
		{"utility on a temporary", StepFacts{CalleeKey: "java.util.List#add", Receiver: "local", ResultUse: "discarded"}, nil, false, "utility"},
		{"utility on a field is a state write", StepFacts{CalleeKey: "java.util.Map#put", Text: "cache.put(k, v)", Receiver: "field", ResultUse: "discarded"},
			nil, true, "writes to cache"},
		{"builder", StepFacts{CalleeKey: "com.x.Request.Builder#header", Receiver: "local", ResultUse: "argument"}, nil, false, "builder"},
		{"common in this project", StepFacts{CalleeKey: "acme.Audit#record", Receiver: "local", ResultUse: "discarded"}, nil, false, "common (12 methods)"},
		{"rare side effect", StepFacts{CalleeKey: "acme.Rare#call", Receiver: "local", ResultUse: "discarded"}, nil, true, "side effect"},
		{"unknown receiver skips the write rule", StepFacts{CalleeKey: "acme.Rare#call", Receiver: "unknown", ResultUse: "discarded"}, nil, true, "side effect"},
		{"legacy row: no key, no use", StepFacts{}, nil, false, "value"},
		{"lua logging", StepFacts{CalleeKey: "ngx#log", ResultUse: "discarded"}, nil, false, "logging"},
		{"ts console", StepFacts{CalleeKey: "console#log", ResultUse: "discarded"}, nil, false, "logging"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			visible, reason := Visibility(c.facts, stats, c.over)
			if visible != c.visible || reason != c.reason {
				t.Fatalf("Visibility = (%v, %q), want (%v, %q)", visible, reason, c.visible, c.reason)
			}
		})
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/codegraph/ -run TestVisibility -count=1`
Expected: compile FAIL (`undefined: StepFacts`).

- [ ] **Step 3: Implement `callimportance.go`** (and delete the temporary `CallStats` from `steps.go`)

```go
package codegraph

import (
	"fmt"
	"strings"
)

// Which calls a flow outline shows and which it folds away, decided at read
// time so a rule can change without a re-index. The first rule that has an
// answer wins, and every answer comes with the reason the outline shows for
// it. A signal whose input an analyzer did not record ("" or "unknown") is
// skipped, so a language that records less still gets an answer.

// CallStats is how often each callee is called across the index.
type CallStats struct {
	Callers map[string]int // callee key → distinct calling methods
	Methods int            // method nodes in the index
}

// StepFacts is what Visibility judges a call by.
type StepFacts struct {
	CalleeKey string
	Text      string
	Receiver  string
	ResultUse string
	// Project is true when the call lands on a node of this project.
	Project bool
	// Helper: a project callee none of whose own steps is visible (steps.go).
	Helper bool
	// WrapsProjectCall: a closure passed to this call holds a visible project call.
	WrapsProjectCall bool
	Effect           *StepEffect
}

// A callee called from at least commonMin methods that are at least
// commonShare of the index is plumbing: logging, auditing, a shared helper.
// First guesses, to tune on real projects.
const (
	commonShare = 0.15
	commonMin   = 8
)

// infraPrefixes match a callee key's start; one table, rows for every language.
var infraPrefixes = []struct{ prefix, reason string }{
	{"org.slf4j.", "logging"},
	{"org.apache.logging.", "logging"},
	{"org.apache.commons.logging.", "logging"},
	{"java.util.logging.", "logging"},
	{"ch.qos.logback.", "logging"},
	{"io.micrometer.", "metrics"},
	{"io.opentelemetry.", "metrics"},
	{"io.prometheus.", "metrics"},
	{"java.lang.String#", "utility"},
	{"java.lang.StringBuilder#", "utility"},
	{"java.lang.Math#", "utility"},
	{"java.util.Objects#", "utility"},
	{"java.util.Optional#", "utility"},
	{"java.util.List#", "utility"},
	{"java.util.Map#", "utility"},
	{"java.util.Set#", "utility"},
	{"java.util.Collection#", "utility"},
	{"java.util.Collections#", "utility"},
	{"java.util.Arrays#", "utility"},
	{"java.util.stream.", "utility"},
	{"java.time.", "utility"},
	// Lua: OpenResty and Kong.
	{"ngx#log", "logging"},
	{"kong.log", "logging"},
	{"string#", "utility"},
	{"table#", "utility"},
	// JavaScript / TypeScript.
	{"console#", "logging"},
	{"JSON#", "utility"},
	{"Math#", "utility"},
	{"Object#", "utility"},
	{"Array#", "utility"},
}

// infraSimpleTypes catch a Java key whose declaring type did not resolve and
// fell back to the declared simple name ("Counter#increment").
var infraSimpleTypes = map[string]string{
	"Logger": "logging", "Log": "logging",
	"Counter": "metrics", "MeterRegistry": "metrics", "Timer": "metrics", "Gauge": "metrics",
	"Tracer": "metrics", "Span": "metrics",
	"Optional": "utility", "Objects": "utility", "String": "utility", "StringBuilder": "utility",
}

// Visibility decides whether a step is shown, and why.
func Visibility(f StepFacts, stats CallStats, overrides map[string]string) (bool, string) {
	if mode, ok := overrideFor(f.CalleeKey, overrides); ok {
		return mode == "show", "you chose"
	}
	if f.Project {
		if f.Helper {
			return false, "helper"
		}
		return true, "project"
	}
	if f.WrapsProjectCall {
		return true, "wraps a project call"
	}
	if f.Effect != nil {
		return true, effectLabel(*f.Effect)
	}
	if f.ResultUse == "condition" {
		return true, "decides a branch"
	}
	if reason := infraReason(f.CalleeKey); reason != "" {
		// A collection on a field is the object's state, not plumbing.
		if !(reason == "utility" && f.Receiver == "field" && f.ResultUse == "discarded") {
			return false, reason
		}
	}
	if writesState(f.Receiver) && f.ResultUse == "discarded" {
		return true, "writes to " + receiverName(f.Text)
	}
	if n := stats.Callers[f.CalleeKey]; f.CalleeKey != "" && stats.Methods > 0 &&
		n >= commonMin && float64(n)/float64(stats.Methods) >= commonShare {
		return false, fmt.Sprintf("common (%d methods)", n)
	}
	switch {
	case f.ResultUse == "discarded":
		return true, "side effect"
	case isAccessor(memberOf(f.CalleeKey)) && f.ResultUse != "" && f.ResultUse != "unknown":
		return false, "accessor"
	default:
		return false, "value"
	}
}

// overrideFor looks the key up, then its type with "#*".
func overrideFor(key string, overrides map[string]string) (string, bool) {
	if key == "" || len(overrides) == 0 {
		return "", false
	}
	if mode, ok := overrides[key]; ok && (mode == "show" || mode == "hide") {
		return mode, true
	}
	if i := strings.IndexByte(key, '#'); i >= 0 {
		if mode, ok := overrides[key[:i]+"#*"]; ok && (mode == "show" || mode == "hide") {
			return mode, true
		}
	}
	return "", false
}

func infraReason(key string) string {
	if key == "" {
		return ""
	}
	for _, p := range infraPrefixes {
		if strings.HasPrefix(key, p.prefix) {
			return p.reason
		}
	}
	typ, member := key, ""
	if i := strings.IndexByte(key, '#'); i >= 0 {
		typ, member = key[:i], key[i+1:]
	}
	if !strings.Contains(typ, ".") {
		if reason, ok := infraSimpleTypes[typ]; ok {
			return reason
		}
	}
	simple := typ[strings.LastIndexByte(typ, '.')+1:]
	if member == "builder" || member == "build" || strings.HasSuffix(simple, "Builder") {
		return "builder"
	}
	return ""
}

func writesState(receiver string) bool {
	return receiver == "param" || receiver == "field" || receiver == "local-from-param"
}

// receiverName is the identifier a call is made on, read off its text.
func receiverName(text string) string {
	end := 0
	for end < len(text) {
		c := text[end]
		if c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || end > 0 && c >= '0' && c <= '9' {
			end++
			continue
		}
		break
	}
	if end == 0 || end >= len(text) || text[end] != '.' {
		return "its receiver"
	}
	return text[:end]
}

func memberOf(key string) string {
	if i := strings.IndexByte(key, '#'); i >= 0 {
		return key[i+1:]
	}
	return ""
}

func isAccessor(member string) bool {
	for _, p := range []string{"get", "is", "has"} {
		if member == p {
			return true
		}
		if strings.HasPrefix(member, p) && len(member) > len(p) {
			if c := member[len(p)]; c >= 'A' && c <= 'Z' {
				return true
			}
		}
	}
	return false
}

func effectLabel(e StepEffect) string {
	kind := map[string]string{"kafka": "Kafka", "http": "HTTP", "redis": "Redis", "db": "DB"}[e.Kind]
	if kind == "" {
		kind = e.Kind
	}
	return kind + " " + e.Role
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/codegraph/ -run TestVisibility -count=1 -v`
Expected: all subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codegraph/callimportance.go internal/codegraph/callimportance_test.go internal/codegraph/steps.go
git commit -m "codegraph: score which calls a flow outline shows

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: `Service.GetSteps`

**Files:**
- Modify: `internal/codegraph/steps.go`
- Test: `internal/codegraph/steps_test.go`

- [ ] **Step 1: Write the failing test**

```go
package codegraph

import (
	"errors"
	"strings"
	"testing"
)

// The shape of fsap-event-router's process(): an abstract decide with two
// overrides, a quarantine that writes a header, helpers that only count.
func stepsFixture() AnalysisResult {
	nodes := []Node{method("process"), method("decide"), method("implA"), method("implB"), method("deliver"),
		method("kafkaDeliver"), method("quarantine"), method("counter"), method("metricPrefix")}
	return AnalysisResult{
		Nodes: nodes,
		CallSites: []CallSite{
			{From: "process", To: "decide", Line: 3, Col: 9, Text: "decide(exchange)", CalleeKey: "acme#decide", ResultUse: "assigned",
				Frames: []StepFrame{{ID: "2:5", Kind: "try", Text: "try"}}},
			{From: "process", To: "quarantine", Line: 5, Col: 9, Text: "quarantine(exchange, R)", CalleeKey: "acme#quarantine", ResultUse: "discarded",
				Frames: []StepFrame{{ID: "4:7", Kind: "catch", Text: "catch AclLookupException", Exits: true}}},
			{From: "process", To: "counter", Line: 8, Col: 9, Text: "counter(metricPrefix())", CalleeKey: "acme#counter", ResultUse: "argument"},
			{From: "process", To: "metricPrefix", Line: 8, Col: 17, Text: "metricPrefix()", CalleeKey: "acme#metricPrefix", ResultUse: "argument"},
			{From: "process", Line: 8, Col: 9, Unresolved: "library", Text: "counter(metricPrefix()).increment()", CalleeKey: "Counter#increment",
				Receiver: "local", ResultUse: "discarded"},
			{From: "process", Line: 11, Col: 9, Unresolved: "library", Text: "breaker.executeCallable(() -> …)",
				CalleeKey: "io.github.resilience4j.circuitbreaker.CircuitBreaker#executeCallable", Receiver: "local", ResultUse: "discarded"},
			{From: "process", To: "deliver", Line: 12, Col: 13, Text: "deliver(proceed, exchange)", CalleeKey: "acme#deliver", ResultUse: "discarded",
				Frames: []StepFrame{{ID: "11:9", Kind: "closure", Text: "breaker.executeCallable(() -> …)"}}},
			{From: "decide", To: "implA", Line: 1, Role: RoleOverride},
			{From: "decide", To: "implB", Line: 1, Role: RoleOverride},
			{From: "deliver", To: "kafkaDeliver", Line: 1, Role: RoleOverride},
			{From: "kafkaDeliver", Line: 2, Unresolved: "library", Text: "producerTemplate.send(uri, exchange)",
				CalleeKey: "org.apache.camel.ProducerTemplate#send", Receiver: "field", ResultUse: "discarded"},
			{From: "quarantine", Line: 2, Unresolved: "library", Text: "in.setHeader(H, r)", CalleeKey: "org.apache.camel.Message#setHeader",
				Receiver: "local-from-param", ResultUse: "discarded"},
			{From: "quarantine", Line: 3, Unresolved: "lombok", Text: "log.warn(m)", CalleeKey: "org.slf4j.Logger#warn", Receiver: "unknown", ResultUse: "discarded"},
			{From: "counter", Line: 2, Unresolved: "library", Text: "Counter.builder(n)", CalleeKey: "Counter#builder", Receiver: "static", ResultUse: "returned"},
		},
		KafkaAccesses: []KafkaAccess{},
	}
}

func stepByText(t *testing.T, steps []Step, text string) Step {
	t.Helper()
	for _, s := range steps {
		if strings.HasPrefix(s.Text, text) {
			return s
		}
	}
	t.Fatalf("no step %q in %+v", text, steps)
	return Step{}
}

func TestGetStepsOfAFlow(t *testing.T) {
	svc := openWith(t, stepsFixture())
	got, err := svc.GetSteps(t.Context(), []string{"process", "quarantine", "decide"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	process := got.Steps["process"]
	if len(process) != 7 {
		t.Fatalf("process steps = %+v", process)
	}
	if s := stepByText(t, process, "decide("); !s.Visible || strings.Join(s.Impls, ",") != "implA,implB" {
		t.Fatalf("decide = %+v", s)
	}
	if s := stepByText(t, process, "quarantine("); !s.Visible || s.Frames[0].Kind != "catch" {
		t.Fatalf("quarantine = %+v", s)
	}
	for _, helper := range []string{"counter(metricPrefix())", "metricPrefix()"} {
		if s := stepByText(t, process, helper); s.Visible || s.Reason != "helper" {
			t.Fatalf("%s = %+v, want a hidden helper", helper, s)
		}
	}
	if s := stepByText(t, process, "counter(metricPrefix()).increment()"); s.Visible || s.Reason != "metrics" {
		t.Fatalf("increment = %+v", s)
	}
	if s := stepByText(t, process, "breaker.executeCallable"); !s.Visible || s.Reason != "wraps a project call" {
		t.Fatalf("executeCallable = %+v", s)
	}
	if s := stepByText(t, got.Steps["quarantine"], "in.setHeader"); !s.Visible || s.Reason != "writes to in" {
		t.Fatalf("setHeader = %+v", s)
	}
	if s := stepByText(t, got.Steps["quarantine"], "log.warn"); s.Visible {
		t.Fatalf("log.warn = %+v", s)
	}
	if len(got.Steps["decide"]) != 0 {
		t.Fatalf("an abstract method has no steps of its own, got %+v", got.Steps["decide"])
	}
	if got.Legacy {
		t.Fatal("an index with step text is not legacy")
	}
}

func TestGetStepsHonoursOverrides(t *testing.T) {
	svc := openWith(t, stepsFixture())
	got, err := svc.GetSteps(t.Context(), []string{"quarantine"}, map[string]string{"org.slf4j.Logger#warn": "show"})
	if err != nil {
		t.Fatal(err)
	}
	if s := stepByText(t, got.Steps["quarantine"], "log.warn"); !s.Visible || s.Reason != "you chose" {
		t.Fatalf("log.warn = %+v", s)
	}
}

func TestGetStepsAttachesEffects(t *testing.T) {
	result := stepsFixture()
	result.KafkaAccesses = []KafkaAccess{{From: "kafkaDeliver", Topic: "out", Role: KafkaProduce, Client: KafkaTemplate, File: "k.java", Line: 2}}
	svc := openWith(t, result)
	got, err := svc.GetSteps(t.Context(), []string{"kafkaDeliver"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := got.Steps["kafkaDeliver"][0]
	if s.Effect == nil || *s.Effect != (StepEffect{Kind: "kafka", Role: "produce", Target: "out"}) || s.Reason != "Kafka produce" {
		t.Fatalf("step = %+v", s)
	}
}

func TestGetStepsCapsAndRefuses(t *testing.T) {
	sites := make([]CallSite, 0, maxStepsPerMethod+5)
	for i := range maxStepsPerMethod + 5 {
		sites = append(sites, CallSite{From: "big", Line: i + 1, Unresolved: "library", Text: "x()", CalleeKey: "acme.Lib#x"})
	}
	svc := openWith(t, AnalysisResult{Nodes: []Node{method("big")}, CallSites: sites})
	got, err := svc.GetSteps(t.Context(), []string{"big"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Steps["big"]) != maxStepsPerMethod || len(got.Truncated) != 1 {
		t.Fatalf("steps = %d, truncated = %v", len(got.Steps["big"]), got.Truncated)
	}
	ids := make([]string, MaxStepNodeIDs+1)
	if _, err := svc.GetSteps(t.Context(), ids, nil); !errors.Is(err, ErrTooManyNodes) {
		t.Fatalf("err = %v, want ErrTooManyNodes", err)
	}
}

func TestGetStepsOfALegacyIndex(t *testing.T) {
	svc := openWith(t, AnalysisResult{Nodes: []Node{method("a"), method("b")},
		CallSites: []CallSite{{From: "a", To: "b", Line: 2}, {From: "a", Line: 3, Unresolved: "library"}}})
	got, err := svc.GetSteps(t.Context(), []string{"a"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Legacy || len(got.Steps["a"]) != 2 {
		t.Fatalf("got = %+v", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/codegraph/ -run TestGetSteps -count=1`
Expected: compile FAIL (`svc.GetSteps undefined`).

- [ ] **Step 3: Implement in `steps.go`**

Change the import line to `import ("context"; "errors"; "fmt")` and add:

```go
const (
	// MaxStepNodeIDs bounds one GetSteps call; the frontend asks in chunks.
	MaxStepNodeIDs = 200
	// maxStepsPerMethod bounds one method's steps; the rest are reported as truncated.
	maxStepsPerMethod = 300
	// helperDepth is how far below the asked methods callees are read to tell
	// a helper from a step that matters.
	helperDepth = 3
	// maxStepGraph bounds how many methods that reading may load in all.
	maxStepGraph = 2000
)

// Step is one call in a method body, as the flow outline draws it.
type Step struct {
	Line      int         `json:"line"`
	Col       int         `json:"col"`
	Text      string      `json:"text"`
	CalleeKey string      `json:"calleeKey"`
	To        string      `json:"to,omitempty"`    // the project node it lands on
	Impls     []string    `json:"impls,omitempty"` // overrides of To, when To is overridden
	Frames    []StepFrame `json:"frames"`
	Visible   bool        `json:"visible"`
	Reason    string      `json:"reason"`
	Effect    *StepEffect `json:"effect,omitempty"`
}

// StepsResult is GetSteps' answer.
type StepsResult struct {
	Steps     map[string][]Step `json:"steps"`     // node id → steps in source order
	Truncated []string          `json:"truncated"` // node ids whose steps were capped
	Legacy    bool              `json:"legacy"`    // the index predates steps: a flat outline
}

// GetSteps returns the steps of each asked method, scored for visibility.
// overrides are the person's "always show / hide" choices
// ({"<key>": "show"|"hide"}, "<Type>#*" for a whole type).
func (s *Service) GetSteps(ctx context.Context, nodeIDs []string, overrides map[string]string) (StepsResult, error) {
	if len(nodeIDs) > MaxStepNodeIDs {
		return StepsResult{}, fmt.Errorf("%w: %d asked, at most %d", ErrTooManyNodes, len(nodeIDs), MaxStepNodeIDs)
	}
	store, release, err := s.activeStore()
	if err != nil {
		return StepsResult{}, err
	}
	defer release()
	return buildSteps(ctx, store, nodeIDs, overrides)
}

func buildSteps(ctx context.Context, store Store, nodeIDs []string, overrides map[string]string) (StepsResult, error) {
	out := StepsResult{Steps: make(map[string][]Step, len(nodeIDs)), Truncated: []string{}}
	if len(nodeIDs) == 0 {
		return out, nil
	}
	stats, err := store.CallStats(ctx)
	if err != nil {
		return StepsResult{}, err
	}
	g, err := loadStepGraph(ctx, store, nodeIDs)
	if err != nil {
		return StepsResult{}, err
	}
	j := &stepJudge{g: g, stats: stats, overrides: overrides, helper: map[string]int8{}}
	anySite, anyText := false, false
	for _, id := range nodeIDs {
		steps := []Step{}
		for _, c := range g.sites[id] {
			if c.Role == RoleOverride {
				continue
			}
			anySite = true
			anyText = anyText || c.Text != ""
			if len(steps) == maxStepsPerMethod {
				out.Truncated = append(out.Truncated, id)
				break
			}
			steps = append(steps, j.step(id, c))
		}
		out.Steps[id] = steps
	}
	out.Legacy = anySite && !anyText
	return out, nil
}

// stepGraph is the asked methods and the callees below them, as far as
// telling a helper apart needs.
type stepGraph struct {
	loaded  map[string]bool
	sites   map[string][]CallSite
	impls   map[string][]string
	effects map[string][]StepEffect // effectKey(from, line) → effects
	writers map[string]bool
}

func effectKey(from string, line int) string { return fmt.Sprintf("%s\x00%d", from, line) }

func loadStepGraph(ctx context.Context, store Store, roots []string) (*stepGraph, error) {
	g := &stepGraph{loaded: map[string]bool{}, sites: map[string][]CallSite{}, impls: map[string][]string{},
		effects: map[string][]StepEffect{}, writers: map[string]bool{}}
	frontier := roots
	for level := 0; level <= helperDepth && len(frontier) > 0 && len(g.loaded) < maxStepGraph; level++ {
		var ask []string
		for _, id := range frontier {
			if !g.loaded[id] && len(g.loaded) < maxStepGraph {
				g.loaded[id] = true
				ask = append(ask, id)
			}
		}
		if len(ask) == 0 {
			break
		}
		sites, err := store.GetStepSites(ctx, ask)
		if err != nil {
			return nil, err
		}
		var next []string
		for _, c := range sites {
			g.sites[c.From] = append(g.sites[c.From], c)
			if c.Role == RoleOverride {
				g.impls[c.From] = append(g.impls[c.From], c.To)
			}
			if c.To != "" && !g.loaded[c.To] {
				next = append(next, c.To)
			}
		}
		effects, err := store.ListEffectsFrom(ctx, ask)
		if err != nil {
			return nil, err
		}
		for _, e := range effects {
			k := effectKey(e.From, e.Line)
			g.effects[k] = append(g.effects[k], e.Effect)
		}
		writers, err := store.FieldWriters(ctx, ask)
		if err != nil {
			return nil, err
		}
		for id := range writers {
			g.writers[id] = true
		}
		frontier = next
	}
	return g, nil
}

// stepJudge scores steps, remembering which callees are helpers.
type stepJudge struct {
	g         *stepGraph
	stats     CallStats
	overrides map[string]string
	helper    map[string]int8 // 1 helper, 2 not, 3 being judged (a cycle counts as not)
}

func (j *stepJudge) step(from string, c CallSite) Step {
	st := Step{Line: c.Line, Col: c.Col, Text: c.Text, CalleeKey: c.CalleeKey, To: c.To, Frames: c.Frames}
	if st.Frames == nil {
		st.Frames = []StepFrame{}
	}
	if c.To != "" {
		st.Impls = j.g.impls[c.To]
	}
	f := j.facts(from, c, 0)
	st.Effect = f.Effect
	st.Visible, st.Reason = Visibility(f, j.stats, j.overrides)
	return st
}

func (j *stepJudge) facts(from string, c CallSite, depth int) StepFacts {
	f := StepFacts{CalleeKey: c.CalleeKey, Text: c.Text, Receiver: c.Receiver, ResultUse: c.ResultUse}
	if c.To != "" {
		f.Project = true
		f.Helper = len(j.g.impls[c.To]) == 0 && j.isHelper(c.To, depth)
		return f
	}
	if effs := j.g.effects[effectKey(from, c.Line)]; len(effs) > 0 {
		e := effs[0]
		f.Effect = &e
	}
	f.WrapsProjectCall = j.wraps(from, c, depth)
	return f
}

// wraps reports whether a closure passed to c holds a project call that matters.
func (j *stepJudge) wraps(from string, c CallSite, depth int) bool {
	at := fmt.Sprintf("%d:%d", c.Line, c.Col)
	for _, o := range j.g.sites[from] {
		if o.To == "" || o.Role == RoleOverride {
			continue
		}
		for _, fr := range o.Frames {
			if fr.Kind == "closure" && fr.ID == at && (len(j.g.impls[o.To]) > 0 || !j.isHelper(o.To, depth)) {
				return true
			}
		}
	}
	return false
}

// isHelper: loaded, not overridden, writes no field, and none of its own
// steps is visible. Anything not known for sure is not a helper — hiding a
// step that matters is worse than showing one that does not.
func (j *stepJudge) isHelper(id string, depth int) bool {
	switch j.helper[id] {
	case 1:
		return true
	case 2, 3:
		return false
	}
	if depth >= helperDepth || !j.g.loaded[id] || j.g.writers[id] || len(j.g.impls[id]) > 0 {
		return false
	}
	j.helper[id] = 3
	helper := true
	for _, c := range j.g.sites[id] {
		if c.Role == RoleOverride {
			continue
		}
		if visible, _ := Visibility(j.facts(id, c, depth+1), j.stats, j.overrides); visible {
			helper = false
			break
		}
	}
	if helper {
		j.helper[id] = 1
	} else {
		j.helper[id] = 2
	}
	return helper
}
```

Remove the now-unused `errors` import if `ErrTooManyNodes` moved; it stays in this file, so keep it.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/codegraph/ -count=1`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/codegraph/steps.go internal/codegraph/steps_test.go
git commit -m "codegraph: serve a flow's steps with their visibility

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Call-visibility overrides in preferences

**Files:**
- Modify: `internal/preferences/preferences.go`, `sqlrepository.go`, `memory.go`
- Test: `internal/preferences/preferences_test.go`

- [ ] **Step 1: Write the failing test**

Add to `preferences_test.go` (it already builds a `Service` over a `MemoryRepository`; follow the existing constructor it uses, e.g. `NewService(&MemoryRepository{})`):

```go
func TestCallVisibilityOverrides(t *testing.T) {
	svc := NewService(&MemoryRepository{})
	got, err := svc.CallVisibility()
	if err != nil || len(got) != 0 {
		t.Fatalf("unset = %v, %v", got, err)
	}
	if err := svc.SetCallVisibility("org.apache.camel.Message#setHeader", "show"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetCallVisibility("org.slf4j.Logger#*", "hide"); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.CallVisibility()
	if got["org.apache.camel.Message#setHeader"] != "show" || got["org.slf4j.Logger#*"] != "hide" {
		t.Fatalf("got = %v", got)
	}
	if err := svc.SetCallVisibility("org.slf4j.Logger#*", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.CallVisibility()
	if _, ok := got["org.slf4j.Logger#*"]; ok {
		t.Fatalf("reset kept the key: %v", got)
	}
	for _, bad := range []struct{ key, mode string }{
		{"no-hash", "show"},
		{"a#b", "maybe"},
		{strings.Repeat("x", 301) + "#y", "show"},
	} {
		if err := svc.SetCallVisibility(bad.key, bad.mode); err == nil {
			t.Fatalf("SetCallVisibility(%q, %q) accepted", bad.key, bad.mode)
		}
	}
}
```

Add `"strings"` to the test file's imports if missing. Also add the SQL round trip to `sqlrepository_test.go`, following how it tests `SetAgentSandbox` there (open the repository the same way, then):

```go
	if err := repo.SetCallVisibility(map[string]string{"a#b": "show"}); err != nil {
		t.Fatal(err)
	}
	got, found, err := repo.GetCallVisibility()
	if err != nil || !found || got["a#b"] != "show" {
		t.Fatalf("got = %v, found = %v, err = %v", got, found, err)
	}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/preferences/ -count=1`
Expected: compile FAIL (`svc.CallVisibility undefined`).

- [ ] **Step 3: Implement**

`preferences.go` — add to the `Repository` interface:

```go
	GetCallVisibility() (map[string]string, bool, error)
	SetCallVisibility(overrides map[string]string) error
```

and to the file:

```go
// Limits on CodeGraph's call-visibility overrides: a key is a callee key
// ("<type>#<member>", or "<type>#*"), a mode is show or hide.
const (
	maxCallVisibilityKey     = 300
	maxCallVisibilityEntries = 500
)

// ErrInvalidCallVisibility rejects a key or mode the overrides cannot hold.
var ErrInvalidCallVisibility = errors.New("preferences: invalid call visibility")

// CallVisibility is the person's "always show / always hide" choices for
// CodeGraph's flow outline. Local to this machine, never synced, the same
// for every project: a library method behaves the same everywhere.
func (s *Service) CallVisibility() (map[string]string, error) {
	got, found, err := s.repo.GetCallVisibility()
	if err != nil {
		return nil, err
	}
	if !found || got == nil {
		return map[string]string{}, nil
	}
	return got, nil
}

// SetCallVisibility sets one key to "show" or "hide", or removes it with "".
func (s *Service) SetCallVisibility(key, mode string) error {
	if !strings.Contains(key, "#") || len(key) > maxCallVisibilityKey {
		return fmt.Errorf("%w: key %q", ErrInvalidCallVisibility, key)
	}
	if mode != "show" && mode != "hide" && mode != "" {
		return fmt.Errorf("%w: mode %q", ErrInvalidCallVisibility, mode)
	}
	current, err := s.CallVisibility()
	if err != nil {
		return err
	}
	next := make(map[string]string, len(current)+1)
	for k, v := range current {
		next[k] = v
	}
	if mode == "" {
		delete(next, key)
	} else {
		next[key] = mode
	}
	if len(next) > maxCallVisibilityEntries {
		return fmt.Errorf("%w: more than %d entries", ErrInvalidCallVisibility, maxCallVisibilityEntries)
	}
	return s.repo.SetCallVisibility(next)
}
```

Add `errors`, `fmt`, `strings` to the imports if missing.

`sqlrepository.go` — add the key constant beside `updateChecksKey`:

```go
	callVisibilityKey     = "codegraph_call_visibility"
```

and the methods:

```go
// GetCallVisibility reads a value it cannot parse as unset: losing the
// overrides shows the defaults, which is safe.
func (r *SQLRepository) GetCallVisibility() (map[string]string, bool, error) {
	var value string
	err := r.db.QueryRowContext(context.Background(),
		"SELECT value FROM app_preferences WHERE key = ?", callVisibilityKey).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var overrides map[string]string
	if jsonErr := json.Unmarshal([]byte(value), &overrides); jsonErr != nil {
		return nil, false, nil
	}
	return overrides, true, nil
}

func (r *SQLRepository) SetCallVisibility(overrides map[string]string) error {
	body, err := json.Marshal(overrides)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(context.Background(),
		`INSERT INTO app_preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, callVisibilityKey, string(body))
	return err
}
```

`memory.go` — add fields `callVisibility map[string]string` and `hasCallVisibility bool` to `MemoryRepository`, and:

```go
func (r *MemoryRepository) GetCallVisibility() (map[string]string, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.callVisibility))
	for k, v := range r.callVisibility {
		out[k] = v
	}
	return out, r.hasCallVisibility, nil
}

func (r *MemoryRepository) SetCallVisibility(overrides map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callVisibility = make(map[string]string, len(overrides))
	for k, v := range overrides {
		r.callVisibility[k] = v
	}
	r.hasCallVisibility = true
	return nil
}
```

Then check for other `preferences.Repository` implementations: `grep -rn "func (.*) SetCodexAPIKeyAccount(" internal cmd` — add the two methods to any found outside `internal/preferences`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/preferences/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/preferences/
git commit -m "preferences: CodeGraph call-visibility overrides

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: Bridge methods, grants and bindings

**Files:**
- Create: `internal/bridge/codegraph_steps.go`
- Modify: `internal/bridge/errors.go` (codegraph switch ~388)
- Modify: `internal/app/container.go` (grant list ~374)
- Test: `internal/bridge/codegraph_steps_test.go`
- Regenerate: `frontend/bindings/`

- [ ] **Step 1: Write the failing test**

Look at `internal/bridge/codegraph_test.go` for how it builds an `AppService` with an opened CodeGraph service (the helper it uses for `CodeGraphGetNeighbors`), and at `updates_test.go` for one with preferences. Write, using those same helpers (their names replace `newCodeGraphBridge` / `newPreferencesBridge` below):

```go
package bridge

import (
	"testing"

	"devkit/internal/codegraph"
)

func TestCodeGraphGetStepsAppliesSavedOverrides(t *testing.T) {
	service := newCodeGraphBridge(t, codegraph.AnalysisResult{
		Nodes: []codegraph.Node{{ID: "a", Label: "a", Kind: "method", Layer: "service", FilePath: "a.java", Line: 1, EndLine: 2}},
		CallSites: []codegraph.CallSite{{From: "a", Line: 2, Unresolved: "library", Text: "log.warn(x)",
			CalleeKey: "org.slf4j.Logger#warn", ResultUse: "discarded"}},
	})
	got, err := service.CodeGraphGetSteps([]string{"a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if steps := got.Steps["a"]; len(steps) != 1 || steps[0].Visible {
		t.Fatalf("steps = %+v", got.Steps)
	}
	if err := service.PreferencesSetCallVisibility("org.slf4j.Logger#warn", "show"); err != nil {
		t.Fatal(err)
	}
	overrides, err := service.PreferencesGetCallVisibility()
	if err != nil || overrides["org.slf4j.Logger#warn"] != "show" {
		t.Fatalf("overrides = %v, err = %v", overrides, err)
	}
	got, _ = service.CodeGraphGetSteps([]string{"a"}, "")
	if !got.Steps["a"][0].Visible {
		t.Fatalf("override not applied: %+v", got.Steps["a"])
	}
}
```

If no single helper gives both CodeGraph and preferences, build the container the way `codegraph_test.go` does and set `container.Preferences = preferences.NewService(&preferences.MemoryRepository{})` before constructing the service.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/bridge/ -run TestCodeGraphGetSteps -count=1`
Expected: compile FAIL (`service.CodeGraphGetSteps undefined`).

- [ ] **Step 3: Implement `codegraph_steps.go`**

```go
package bridge

import (
	"context"

	"devkit/internal/codegraph"
)

// CodeGraphGetSteps returns the steps of the given methods of a flow — the
// Outline mode's rows and the Graph mode's labels — scored with the person's
// call-visibility overrides. Read-only, like CodeGraphGetNeighbors, and asked
// of the same service.
func (s *AppService) CodeGraphGetSteps(nodeIDs []string, servicePath string) (codegraph.StepsResult, error) {
	result, err := s.call(context.Background(), "builtin.codegraph", "codegraph:read", "project:active", map[string]any{
		"service_path": servicePath,
		"action":       "get_steps",
		"node_count":   len(nodeIDs),
	}, func(ctx context.Context) (any, error) {
		overrides := map[string]string{}
		if s.container.Preferences != nil {
			got, err := s.container.Preferences.CallVisibility()
			if err != nil {
				return nil, err
			}
			overrides = got
		}
		svc, err := s.codeGraphService(ctx, servicePath)
		if err != nil {
			return nil, err
		}
		return svc.GetSteps(ctx, nodeIDs, overrides)
	})
	if err != nil {
		return codegraph.StepsResult{}, err
	}
	return result.(codegraph.StepsResult), nil
}

// PreferencesGetCallVisibility returns the "always show / hide" choices.
func (s *AppService) PreferencesGetCallVisibility() (map[string]string, error) {
	result, err := s.call(context.Background(), "builtin.settings", "preferences:read", "codegraph_call_visibility", map[string]any{
		"action": "get_call_visibility",
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return s.container.Preferences.CallVisibility()
	})
	if err != nil {
		return nil, err
	}
	return result.(map[string]string), nil
}

// PreferencesSetCallVisibility sets one callee key to "show" or "hide", or
// clears it with "". UI only: no MCP tool changes what a person sees.
func (s *AppService) PreferencesSetCallVisibility(calleeKey, mode string) error {
	_, err := s.call(context.Background(), "builtin.settings", "preferences:write", "codegraph_call_visibility", map[string]any{
		"action": "set_call_visibility",
		"mode":   mode,
	}, func(context.Context) (any, error) {
		if s.container.Preferences == nil {
			return nil, errPreferencesUnavailable
		}
		return nil, s.container.Preferences.SetCallVisibility(calleeKey, mode)
	})
	return err
}
```

- [ ] **Step 4: Errors and grants**

`internal/bridge/errors.go`, inside the codegraph `switch` (next to `ErrInvalidPlan`):

```go
		case errors.Is(err, codegraph.ErrTooManyNodes):
			return apperrors.Wrap(codeValidationFailed, "Too many CodeGraph nodes in one request.", true, codegraph.ErrTooManyNodes)
```

Find where `preferences` errors are mapped (`grep -n "preferences\.Err" internal/bridge/errors.go`) and add, in the same style:

```go
		case errors.Is(err, preferences.ErrInvalidCallVisibility):
			return apperrors.Wrap(codeValidationFailed, "That call cannot be marked.", true, preferences.ErrInvalidCallVisibility)
```

(If preferences errors are not mapped anywhere, add this case next to the codegraph cases and import `devkit/internal/preferences`.)

`internal/app/container.go`, after the two `update_checks` grants:

```go
		{SubjectID: "builtin.settings", Capability: "preferences:read", Scope: "codegraph_call_visibility"},
		{SubjectID: "builtin.settings", Capability: "preferences:write", Scope: "codegraph_call_visibility"},
```

- [ ] **Step 5: Run the Go tests**

Run: `go test ./internal/bridge/ ./internal/app/ ./internal/codegraph/ ./internal/preferences/ ./internal/arch/ -count=1`
Expected: all PASS.

- [ ] **Step 6: Regenerate bindings**

Run (from the repo root): `wails3 generate bindings -clean=true -ts -i`
Expected: `frontend/bindings/devkit/internal/bridge/appservice.ts` gains `CodeGraphGetSteps`, `PreferencesGetCallVisibility`, `PreferencesSetCallVisibility`; `frontend/bindings/devkit/internal/codegraph/models.ts` gains `Step`, `StepFrame`, `StepEffect`, `StepsResult`, and `CallSite` its new fields. If `wails3` is not on PATH, install it into a throwaway module as AGENTS.md describes.

- [ ] **Step 7: Commit**

```bash
git add internal/bridge/codegraph_steps.go internal/bridge/codegraph_steps_test.go internal/bridge/errors.go internal/app/container.go frontend/bindings/
git commit -m "bridge: CodeGraphGetSteps and call-visibility preferences

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Frontend bridge types and calls

**Files:**
- Modify: `frontend/src/bridge/codegraph.ts`

- [ ] **Step 1: Extend the types and add the calls**

Replace `CodeGraphEdge` with:

```ts
export type CodeGraphEdge = {
  from: string;
  to: string;
  kind: string;
  /** The condition the call runs under, as written (Go Edge.Guard); absent when it always runs. */
  guard?: string;
};
```

Add after `getCodeGraphNeighbors`:

```ts
/** One control structure around a call (Go StepFrame). `kind` is open: render an unknown one by its text. */
export type CodeGraphStepFrame = { id: string; kind: string; text: string; exits?: boolean };

/** What an extractor recognised at a call (Go StepEffect). */
export type CodeGraphStepEffect = { kind: string; role: string; target: string };

/** One call in a method body, as the outline draws it (Go Step). */
export type CodeGraphStep = {
  line: number;
  col: number;
  text: string;
  calleeKey: string;
  /** The project node it lands on. */
  to?: string;
  /** Overrides of `to`, when `to` is abstract or overridden. */
  impls?: string[];
  frames: CodeGraphStepFrame[];
  visible: boolean;
  reason: string;
  effect?: CodeGraphStepEffect;
};

export type CodeGraphSteps = {
  steps: Record<string, CodeGraphStep[]>;
  truncated: string[];
  legacy: boolean;
};

/** Go's MaxStepNodeIDs: one request's limit, so longer lists go in chunks. */
export const MAX_STEP_NODE_IDS = 200;

function toStep(s: StepModel): CodeGraphStep {
  return {
    line: s.line,
    col: s.col,
    text: s.text,
    calleeKey: s.calleeKey,
    to: s.to || undefined,
    impls: s.impls && s.impls.length ? [...s.impls] : undefined,
    frames: (s.frames ?? []).flatMap((f) => (f ? [{ id: f.id, kind: f.kind, text: f.text, exits: f.exits || undefined }] : [])),
    visible: s.visible,
    reason: s.reason,
    effect: s.effect ? { kind: s.effect.kind, role: s.effect.role, target: s.effect.target } : undefined,
  };
}

/** The steps of a flow's methods, scored with the person's overrides. */
export async function getCodeGraphSteps(nodeIds: string[], servicePath = ""): Promise<CodeGraphSteps> {
  const merged: CodeGraphSteps = { steps: {}, truncated: [], legacy: false };
  for (let i = 0; i < nodeIds.length; i += MAX_STEP_NODE_IDS) {
    const chunk = nodeIds.slice(i, i + MAX_STEP_NODE_IDS);
    const result = await callBridge(() => AppService.CodeGraphGetSteps(chunk, servicePath));
    for (const [id, list] of Object.entries(result.steps ?? {})) {
      merged.steps[id] = (list ?? []).flatMap((s) => (s ? [toStep(s)] : []));
    }
    merged.truncated.push(...(result.truncated ?? []));
    merged.legacy = merged.legacy || result.legacy;
  }
  return merged;
}

export type CallVisibilityMode = "show" | "hide" | "";

/** The person's "always show / hide" choices, by callee key. */
export async function getCallVisibility(): Promise<Record<string, string>> {
  const result = await callBridge(() => AppService.PreferencesGetCallVisibility());
  return { ...(result ?? {}) };
}

export async function setCallVisibility(calleeKey: string, mode: CallVisibilityMode): Promise<void> {
  await callBridge(() => AppService.PreferencesSetCallVisibility(calleeKey, mode));
}
```

Add the model import at the top:

```ts
import type { Step as StepModel } from "../../bindings/devkit/internal/codegraph/models";
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.app.json` (or `bun run build` if there is no app tsconfig — check `frontend/package.json`'s `build` script for the tsc invocation and run that part).
Expected: no errors. If the generated model's nullability differs (for example `frames: (StepFrame | null)[]` versus `StepFrame[]`), the `?? []` and `flatMap` above already accept both; fix only what tsc reports.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/bridge/codegraph.ts
git commit -m "frontend: bridge types and calls for flow steps

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: The outline model (`buildOutline.ts`)

**Files:**
- Create: `frontend/src/modules/codegraph/outline/buildOutline.ts`
- Test: `frontend/src/modules/codegraph/outline/buildOutline.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "bun:test";
import { buildOutline, type OutlineRow } from "./buildOutline";
import type { CodeGraphNode, CodeGraphStep, CodeGraphSteps } from "@/bridge/codegraph";

const node = (id: string, label = `acme.P.${id}`): CodeGraphNode => ({
  id, label, kind: "method", layer: "", filePath: "P.java", line: 1, endLine: 9, summary: "",
});
const step = (over: Partial<CodeGraphStep>): CodeGraphStep => ({
  line: 1, col: 1, text: "x()", calleeKey: "#x", frames: [], visible: true, reason: "side effect", ...over,
});
const nodes = (...ids: string[]) => new Map(ids.map((id) => [id, node(id)]));
/** Rows as "depth kind text", the way a reader sees them. */
const show = (rows: OutlineRow[]) =>
  rows.map((r) => {
    const label =
      r.kind === "method" ? r.title
      : r.kind === "frame" ? r.text + (r.exits ? " ↩" : "")
      : r.kind === "call" ? r.title + (r.ref ? ` [${r.ref.reason}]` : "")
      : r.kind === "fold" ? `+${r.count} · ${r.reasons}`
      : "more";
    return `${r.depth} ${r.kind} ${label}`;
  });

describe("buildOutline", () => {
  it("groups steps under their frames and folds hidden runs inside one group", () => {
    const steps: CodeGraphSteps = {
      legacy: false,
      truncated: [],
      steps: {
        process: [
          step({ text: "decide(exchange)", to: "decide", frames: [{ id: "2:5", kind: "try", text: "try" }] }),
          step({ text: "log.error(e)", visible: false, reason: "logging", frames: [{ id: "4:7", kind: "catch", text: "catch E", exits: true }] }),
          step({ text: "quarantine(x)", to: "quarantine", frames: [{ id: "4:7", kind: "catch", text: "catch E", exits: true }] }),
          step({ text: "log.debug(a)", visible: false, reason: "logging", frames: [{ id: "8:5", kind: "if", text: "if Skip", exits: true }] }),
          step({ text: "counter(a)", visible: false, reason: "helper", to: "counter", frames: [{ id: "8:5", kind: "if", text: "if Skip", exits: true }] }),
          step({ text: "Counter.increment()", visible: false, reason: "metrics", frames: [{ id: "8:5", kind: "if", text: "if Skip", exits: true }] }),
        ],
        decide: [],
        quarantine: [],
        counter: [],
      },
    };
    const rows = buildOutline({ rootId: "process", nodes: nodes("process", "decide", "quarantine", "counter"), steps, toggled: {} });
    expect(show(rows)).toEqual([
      "0 method P.process",
      "1 frame try",
      "2 call decide(exchange)",
      "1 frame catch E ↩",
      "2 fold +1 · logging",
      "2 call quarantine(x)",
      "1 frame if Skip ↩",
      "2 fold +3 · logging, helper, metrics",
    ]);
  });

  it("opens a fold in place", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "log.a()", visible: false, reason: "logging" }), step({ text: "send()" })] },
    };
    const closed = buildOutline({ rootId: "a", nodes: nodes("a"), steps, toggled: {} });
    const fold = closed.find((r) => r.kind === "fold")!;
    const open = buildOutline({ rootId: "a", nodes: nodes("a"), steps, toggled: { [fold.key]: true } });
    expect(show(open)).toEqual(["0 method P.a", "1 fold +1 · logging", "2 call log.a()", "1 call send()"]);
    expect(open.find((r) => r.kind === "call" && r.title === "log.a()")).toMatchObject({ hidden: true });
  });

  it("draws a closure's steps under the call that runs it, not under a heading", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        a: [
          step({ line: 11, col: 9, text: "breaker.executeCallable(() -> …)" }),
          step({ line: 12, col: 13, text: "deliver(p)", to: "deliver", frames: [{ id: "11:9", kind: "closure", text: "breaker.executeCallable(() -> …)" }] }),
        ],
        deliver: [],
      },
    };
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a", "deliver"), steps, toggled: {} }))).toEqual([
      "0 method P.a",
      "1 call breaker.executeCallable(() -> …)",
      "2 call deliver(p)",
    ]);
  });

  it("lists implementations under an abstract call and expands each", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "decide(x)", to: "decide", impls: ["implA", "implB"] })], decide: [], implA: [step({ text: "acl.find()" })], implB: [] },
    };
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a", "decide", "implA", "implB"), steps, toggled: {} }))).toEqual([
      "0 method P.a",
      "1 call decide(x)",
      "2 method P.implA",
      "3 call acl.find()",
      "2 method P.implB",
    ]);
  });

  it("draws a method once and points back to it, and marks recursion", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: {
        a: [step({ text: "q(1)", to: "q" }), step({ text: "q(2)", to: "q" }), step({ text: "a()", to: "a" })],
        q: [step({ text: "send()" })],
      },
    };
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a", "q"), steps, toggled: {} }))).toEqual([
      "0 method P.a",
      "1 call q(1)",
      "2 call send()",
      "1 call q(2) [shown-above]",
      "1 call a() [recursion]",
    ]);
  });

  it("opens three levels of project calls by default", () => {
    const steps: CodeGraphSteps = {
      legacy: false, truncated: [],
      steps: { a: [step({ text: "b()", to: "b" })], b: [step({ text: "c()", to: "c" })], c: [step({ text: "d()", to: "d" })], d: [step({ text: "e()" })] },
    };
    const rows = buildOutline({ rootId: "a", nodes: nodes("a", "b", "c", "d"), steps, toggled: {} });
    expect(show(rows)).toEqual(["0 method P.a", "1 call b()", "2 call c()", "3 call d()"]);
    expect(rows.at(-1)).toMatchObject({ kind: "call", expandable: true, open: false });
  });

  it("says where a method's steps were cut", () => {
    const steps: CodeGraphSteps = { legacy: false, truncated: ["a"], steps: { a: [step({ text: "x()" })] } };
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a"), steps, toggled: {} }))).toEqual(["0 method P.a", "1 call x()", "1 more more"]);
  });

  it("names a step with no text by its target", () => {
    const steps: CodeGraphSteps = { legacy: true, truncated: [], steps: { a: [step({ text: "", to: "b" })], b: [] } };
    expect(show(buildOutline({ rootId: "a", nodes: nodes("a", "b"), steps, toggled: {} }))).toEqual(["0 method P.a", "1 call P.b"]);
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && bun test src/modules/codegraph/outline`
Expected: FAIL, `Cannot find module './buildOutline'`.

- [ ] **Step 3: Implement `buildOutline.ts`**

```ts
import type { CodeGraphNode, CodeGraphStep, CodeGraphStepFrame, CodeGraphSteps } from "@/bridge/codegraph";
import { shortLabel } from "../graph/labels";

/**
 * The flow outline as rows: the root method, then its steps in source order,
 * nested under the control structures they sit in, with a project call
 * expanding into the callee's own steps. Pure — the view windows these rows
 * and keeps the open/closed choices; nothing here fetches.
 *
 * Keys are paths ("root/3/fold@5"), so a row keeps its open state while the
 * rows around it change.
 */
export type OutlineRef = { reason: "shown-above" | "recursion"; key: string };

export type OutlineRow =
  | { kind: "method"; key: string; depth: number; nodeId: string; title: string; summary: string; line: number; open: boolean; expandable: boolean; impl: boolean; ref?: OutlineRef }
  | { kind: "frame"; key: string; depth: number; frameKind: string; text: string; exits: boolean }
  | { kind: "call"; key: string; depth: number; ownerId: string; step: CodeGraphStep; title: string; targetId?: string; open: boolean; expandable: boolean; hidden: boolean; ref?: OutlineRef }
  | { kind: "fold"; key: string; depth: number; count: number; reasons: string; open: boolean }
  | { kind: "more"; key: string; depth: number; nodeId: string };

export type OutlineInput = {
  rootId: string;
  nodes: ReadonlyMap<string, CodeGraphNode>;
  steps: CodeGraphSteps;
  /** Rows whose open state the person changed, by key. */
  toggled: Readonly<Record<string, boolean>>;
  /** Levels of methods open by default, the root included. */
  depth?: number;
};

export const DEFAULT_OUTLINE_DEPTH = 3;

/** "logging, metrics" — the most frequent reasons first, three at most. */
export function summarizeReasons(reasons: string[]): string {
  const counts = new Map<string, number>();
  for (const r of reasons) counts.set(r, (counts.get(r) ?? 0) + 1);
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1] || reasons.indexOf(a[0]) - reasons.indexOf(b[0]))
    .slice(0, 3)
    .map(([r]) => r)
    .join(", ");
}

/** The first sentence of a doc comment, for a row or a card. */
export function firstSentence(summary: string): string {
  const text = summary.trim();
  const end = text.search(/[.!?](\s|$)/);
  return end >= 0 ? text.slice(0, end + 1) : text;
}

export function buildOutline(input: OutlineInput): OutlineRow[] {
  const limit = input.depth ?? DEFAULT_OUTLINE_DEPTH;
  const rows: OutlineRow[] = [];
  const firstAt = new Map<string, string>();
  const truncated = new Set(input.steps.truncated);
  // Something to expand into: a method whose steps were read and are not empty.
  const hasSteps = (id: string) => (input.steps.steps[id]?.length ?? 0) > 0;
  const isOpen = (key: string, fallback: boolean) => input.toggled[key] ?? fallback;

  const root = input.nodes.get(input.rootId);
  if (!root) return rows;
  const rootOpen = isOpen("root", true);
  rows.push(methodRow(root, "root", 0, rootOpen, hasSteps(root.id), false));
  if (rootOpen) {
    firstAt.set(root.id, "root");
    body(root.id, "root", 1, 0, new Set([root.id]));
  }
  return rows;

  function methodRow(n: CodeGraphNode, key: string, depth: number, open: boolean, expandable: boolean, impl: boolean, ref?: OutlineRef): OutlineRow {
    return { kind: "method", key, depth, nodeId: n.id, title: shortLabel(n.label, n.kind), summary: firstSentence(n.summary), line: n.line, open, expandable, impl, ref };
  }

  function body(ownerId: string, parentKey: string, depth: number, level: number, path: ReadonlySet<string>) {
    const list = input.steps.steps[ownerId] ?? [];
    const usedKeys = new Set<string>();
    let stack: CodeGraphStepFrame[] = [];
    let pending: { index: number; step: CodeGraphStep }[] = [];
    let lastCallAt = "";

    const unique = (key: string) => {
      let k = key;
      for (let n = 2; usedKeys.has(k); n++) k = `${key}~${n}`;
      usedKeys.add(k);
      return k;
    };
    const flush = () => {
      if (pending.length === 0) return;
      const key = `${parentKey}/fold@${pending[0].index}`;
      const open = isOpen(key, false);
      const d = depth + stack.length;
      rows.push({ kind: "fold", key, depth: d, count: pending.length, reasons: summarizeReasons(pending.map((p) => p.step.reason)), open });
      if (open) for (const p of pending) callRow(ownerId, p.step, `${parentKey}/${p.index}`, d + 1, level, path, true);
      pending = [];
    };

    list.forEach((step, index) => {
      const frames = step.frames;
      let common = 0;
      while (common < stack.length && common < frames.length && stack[common].id === frames[common].id) common++;
      const sameGroup = common === stack.length && common === frames.length;
      if (!sameGroup || step.visible) flush();
      for (let i = common; i < frames.length; i++) {
        const f = frames[i];
        // A closure passed to the call just drawn is that call's body.
        if (f.kind === "closure" && f.id === lastCallAt) continue;
        rows.push({ kind: "frame", key: unique(`${parentKey}/frame@${f.id}`), depth: depth + i, frameKind: f.kind, text: f.text, exits: !!f.exits });
      }
      stack = frames;
      if (!step.visible) {
        pending.push({ index, step });
        return;
      }
      callRow(ownerId, step, `${parentKey}/${index}`, depth + frames.length, level, path, false);
      lastCallAt = `${step.line}:${step.col}`;
    });
    flush();
    if (truncated.has(ownerId)) rows.push({ kind: "more", key: `${parentKey}/more`, depth, nodeId: ownerId });
  }

  function callRow(ownerId: string, step: CodeGraphStep, key: string, depth: number, level: number, path: ReadonlySet<string>, hidden: boolean) {
    const target = step.to ? input.nodes.get(step.to) : undefined;
    const title = step.text || (target ? shortLabel(target.label, target.kind) : step.calleeKey || "call");
    const impls = step.impls ?? [];
    let ref: OutlineRef | undefined;
    if (target && path.has(target.id)) ref = { reason: "recursion", key: firstAt.get(target.id) ?? "root" };
    const expandable = !!target && !ref && (impls.length > 0 || hasSteps(target.id));
    const open = expandable && isOpen(key, level + 1 < limit);
    if (target && open && impls.length === 0 && firstAt.has(target.id)) ref = { reason: "shown-above", key: firstAt.get(target.id)! };
    rows.push({ kind: "call", key, depth, ownerId, step, title, targetId: target?.id, open: open && !ref, expandable: expandable && !ref, hidden, ref });
    if (!open || ref || !target) return;

    if (impls.length > 0) {
      for (const implId of impls) {
        const impl = input.nodes.get(implId);
        if (!impl) continue;
        const implKey = `${key}/impl:${implId}`;
        const seen = firstAt.get(implId);
        const implRef: OutlineRef | undefined = path.has(implId)
          ? { reason: "recursion", key: seen ?? "root" }
          : seen ? { reason: "shown-above", key: seen } : undefined;
        const implOpen = !implRef && hasSteps(implId) && isOpen(implKey, level + 1 < limit);
        rows.push(methodRow(impl, implKey, depth + 1, implOpen, !implRef && hasSteps(implId), true, implRef));
        if (implOpen) {
          firstAt.set(implId, implKey);
          body(implId, implKey, depth + 2, level + 1, new Set([...path, implId]));
        }
      }
      return;
    }
    firstAt.set(target.id, key);
    body(target.id, key, depth + 1, level + 1, new Set([...path, target.id]));
  }
}
```

- [ ] **Step 4: Run the tests**

Run: `cd frontend && bun test src/modules/codegraph/outline`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/codegraph/outline/
git commit -m "codegraph: the flow outline model

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: The graph view model (`graphView.ts`)

**Files:**
- Create: `frontend/src/modules/codegraph/graph/graphView.ts`
- Test: `frontend/src/modules/codegraph/graph/graphView.test.ts`

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "bun:test";
import { cardData, graphView } from "./graphView";
import type { CodeGraphNode, CodeGraphStep, CodeGraphSteps } from "@/bridge/codegraph";

const node = (id: string, over: Partial<CodeGraphNode> = {}): CodeGraphNode => ({
  id, label: `com.acme.Processor.${id}`, kind: "method", layer: "", filePath: "src/Processor.java", line: 7, endLine: 9, summary: "", ...over,
});
const step = (over: Partial<CodeGraphStep>): CodeGraphStep => ({
  line: 1, col: 1, text: "x()", calleeKey: "#x", frames: [], visible: true, reason: "project", ...over,
});

describe("graphView", () => {
  const nodes = ["process", "decide", "impl", "quarantine", "metricPrefix"].map((id) => node(id));
  const edges = [
    { from: "process", to: "decide", kind: "call" },
    { from: "process", to: "quarantine", kind: "call", guard: "!(decision instanceof Skip)" },
    { from: "process", to: "metricPrefix", kind: "call" },
    { from: "decide", to: "impl", kind: "call" },
  ];
  const steps: CodeGraphSteps = {
    legacy: false, truncated: [],
    steps: {
      process: [
        step({ text: "decide(e)", to: "decide", impls: ["impl"], frames: [{ id: "2:5", kind: "try", text: "try" }] }),
        step({ text: "log.x()", visible: false, reason: "logging" }),
        step({ text: "quarantine(e)", to: "quarantine", frames: [{ id: "4:7", kind: "catch", text: "catch AclLookupException", exits: true }] }),
        step({ text: "metricPrefix()", to: "metricPrefix", visible: false, reason: "helper" }),
      ],
      decide: [], impl: [], metricPrefix: [],
      quarantine: [step({ text: "send()", visible: true, reason: "Kafka produce", effect: { kind: "kafka", role: "produce", target: "q" } })],
    },
  };

  it("leaves out helpers, numbers calls and labels them with their innermost frame", () => {
    const view = graphView("process", nodes, edges, steps);
    expect(view.nodes.map((n) => n.id)).toEqual(["process", "decide", "impl", "quarantine"]);
    expect(view.edges.map((e) => `${e.from}->${e.to}:${e.kind}`)).toEqual([
      "process->decide:call", "process->quarantine:call", "decide->impl:implements",
    ]);
    const label = (from: string, to: string) => view.edgeLabel(view.edges.find((e) => e.from === from && e.to === to)!);
    expect(label("process", "decide")).toBe("1 · try");
    expect(label("process", "quarantine")).toBe("2 · catch AclLookupException");
    expect(label("decide", "impl")).toBe("implements");
    expect(view.edgeTitle(view.edges.find((e) => e.to === "quarantine")!)).toBe("!(decision instanceof Skip)");
    expect(view.hiddenCalls.get("process")).toBe(2);
    expect(view.effects.get("quarantine")).toEqual(["Kafka produce"]);
  });

  it("passes the graph through untouched without steps", () => {
    const view = graphView("process", nodes, edges, null);
    expect(view.nodes).toBe(nodes);
    expect(view.edges).toBe(edges);
    expect(view.edgeLabel(edges[0])).toBeUndefined();
  });
});

describe("cardData", () => {
  it("puts the method first, the class above it and the summary instead of the file", () => {
    const data = cardData(node("quarantine", { summary: "Forwards the record untouched. Then counts it." }), 6, ["Kafka produce"]);
    expect(data).toMatchObject({
      title: "quarantine", caption: "Processor", summary: "Forwards the record untouched.",
      detail: "Processor.java:7", hiddenCalls: 6, effects: ["Kafka produce"],
    });
  });

  it("keeps a class's own name as the title", () => {
    expect(cardData(node("X", { kind: "class", label: "com.acme.Processor" }), 0, [])).toMatchObject({ title: "Processor", caption: "" });
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && bun test src/modules/codegraph/graph/graphView`
Expected: FAIL, `Cannot find module './graphView'`.

- [ ] **Step 3: Implement `graphView.ts`**

```ts
import type { CodeGraphEdge, CodeGraphNode, CodeGraphSteps } from "@/bridge/codegraph";
import { shortLabel } from "./labels";
import { firstSentence } from "../outline/buildOutline";
import type { ApiNodeData } from "./ApiNode";

/**
 * What the Graph mode draws, read off the same steps as the Outline: helpers
 * the outline folds away are not cards; an edge says which call it is (its
 * order among the caller's visible steps) and under which structure; a call
 * into an override is drawn as "implements". Without steps the graph is
 * today's, untouched.
 */
export type GraphView = {
  nodes: CodeGraphNode[];
  edges: CodeGraphEdge[];
  hiddenCalls: ReadonlyMap<string, number>;
  effects: ReadonlyMap<string, string[]>;
  edgeLabel: (edge: CodeGraphEdge) => string | undefined;
  edgeTitle: (edge: CodeGraphEdge) => string | undefined;
};

const FRAME_LABEL_MAX = 40;
const EFFECT_NAMES: Record<string, string> = { kafka: "Kafka", http: "HTTP", redis: "Redis", db: "DB" };

const clipLabel = (text: string) => (text.length <= FRAME_LABEL_MAX ? text : `${text.slice(0, FRAME_LABEL_MAX - 1)}…`);

export function graphView(rootId: string, nodes: CodeGraphNode[], edges: CodeGraphEdge[], steps: CodeGraphSteps | null): GraphView {
  if (!steps) {
    return { nodes, edges, hiddenCalls: new Map(), effects: new Map(), edgeLabel: () => undefined, edgeTitle: (e) => e.guard || undefined };
  }
  const incoming = new Map<string, { visible: boolean; reason: string }[]>();
  const implements_ = new Set<string>();
  const hiddenCalls = new Map<string, number>();
  const effects = new Map<string, string[]>();
  const firstCall = new Map<string, { order: number; frame: string }>();

  for (const [from, list] of Object.entries(steps.steps)) {
    let order = 0;
    let hidden = 0;
    const seen = new Set<string>();
    for (const s of list) {
      if (s.visible) order++;
      else hidden++;
      if (s.to) {
        const arr = incoming.get(s.to) ?? [];
        arr.push({ visible: s.visible, reason: s.reason });
        incoming.set(s.to, arr);
        if (s.visible && !firstCall.has(`${from}->${s.to}`)) {
          firstCall.set(`${from}->${s.to}`, { order, frame: s.frames.at(-1)?.text ?? "" });
        }
        for (const impl of s.impls ?? []) implements_.add(`${s.to}->${impl}`);
      }
      if (s.visible && s.effect) {
        const name = `${EFFECT_NAMES[s.effect.kind] ?? s.effect.kind} ${s.effect.role}`;
        if (!seen.has(name)) {
          seen.add(name);
          effects.set(from, [...(effects.get(from) ?? []), name]);
        }
      }
    }
    if (hidden > 0) hiddenCalls.set(from, hidden);
  }

  const helper = new Set<string>();
  for (const [id, calls] of incoming) {
    if (id !== rootId && calls.length > 0 && calls.every((c) => !c.visible && c.reason === "helper")) helper.add(id);
  }
  const keptNodes = nodes.filter((n) => !helper.has(n.id));
  const keptEdges = edges
    .filter((e) => !helper.has(e.from) && !helper.has(e.to))
    .map((e) => (implements_.has(`${e.from}->${e.to}`) ? { ...e, kind: "implements" } : e));

  return {
    nodes: keptNodes,
    edges: keptEdges,
    hiddenCalls,
    effects,
    edgeLabel: (e) => {
      if (e.kind === "implements") return "implements";
      const call = firstCall.get(`${e.from}->${e.to}`);
      if (!call) return undefined;
      return call.frame ? `${call.order} · ${clipLabel(call.frame)}` : String(call.order);
    },
    edgeTitle: (e) => e.guard || undefined,
  };
}

/** A card's data: the method as the title, its class above it, the doc comment's first sentence. */
export function cardData(n: CodeGraphNode, hiddenCalls: number, effects: string[]): ApiNodeData & { ariaLabel: string } {
  const short = shortLabel(n.label, n.kind);
  const dot = n.kind === "method" ? short.indexOf(".") : -1;
  const file = n.filePath.split("/").pop() ?? n.filePath;
  return {
    layer: n.layer,
    title: dot >= 0 ? short.slice(dot + 1) : short,
    caption: dot >= 0 ? short.slice(0, dot) : "",
    titleTooltip: n.label,
    detail: `${file}:${n.line}`,
    detailTooltip: `${n.filePath}:${n.line}`,
    summary: firstSentence(n.summary),
    hiddenCalls,
    effects,
    ariaLabel: `${n.layer || n.kind} ${n.label}`,
  };
}
```

`ApiNodeData` gains `caption`, `summary`, `hiddenCalls`, `effects` in Task 14; until then this file does not typecheck, which is fine because bun test does not typecheck. Run the typecheck after Task 14.

- [ ] **Step 4: Run the tests**

Run: `cd frontend && bun test src/modules/codegraph/graph/graphView`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/codegraph/graph/graphView.ts frontend/src/modules/codegraph/graph/graphView.test.ts
git commit -m "codegraph: derive graph labels and hidden helpers from steps

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: Store and actions for steps, mode and rows

**Files:**
- Modify: `frontend/src/modules/codegraph/codegraphStore.ts` (`FlowDoc` ~23)
- Modify: `frontend/src/modules/codegraph/flowActions.ts`

- [ ] **Step 1: Extend `FlowDoc`**

In `codegraphStore.ts`, import the type and add fields to `FlowDoc`:

```ts
import type { CodeGraphSteps } from "@/bridge/codegraph";
```

```ts
  /** The steps of the subgraph's methods — the Outline's rows, the Graph's labels. */
  steps: CodeGraphSteps | null;
  stepsLoading: boolean;
  stepsError: string;
  /** Which mode the flow tab shows. Memory only; a new flow opens on the outline. */
  mode: "outline" | "graph";
  /** Outline rows whose open state the person changed, by row key. */
  toggled: Record<string, boolean>;
```

Then find every other place a `FlowDoc` is built: `grep -rn "loadingMore: false" frontend/src/modules/codegraph` — besides `EMPTY_FLOW` in `flowActions.ts`, add the same five defaults wherever one is built.

- [ ] **Step 2: Actions in `flowActions.ts`**

Change `EMPTY_FLOW`:

```ts
const EMPTY_FLOW = (folderId: string): FlowDoc => ({
  folderId, subgraph: null, pages: 0, loading: false, loadingMore: false, error: "",
  steps: null, stepsLoading: false, stepsError: "", mode: "outline", toggled: {},
});
```

Extend the bridge import with `getCodeGraphSteps`, `setCallVisibility as saveCallVisibility`, `type CallVisibilityMode`, and add `import { toast } from "sonner";`.

Add:

```ts
/**
 * Fetches the steps of the flow's methods. Dropped if the subgraph it was
 * asked for is no longer the one in that flow (a refresh replaced it).
 */
export async function loadSteps(folderId: string, root: string): Promise<void> {
  const key = flowKey(folderId, root);
  const subgraph = snapshot().flows[key]?.subgraph;
  const servicePath = servicePathOf(folderId);
  if (!subgraph || !servicePath) return;
  const current = begin(`steps:${key}`);
  patchFlow(key, folderId, { stepsLoading: true, stepsError: "" });
  try {
    const steps = await getCodeGraphSteps(subgraph.nodes.map((n) => n.id), servicePath);
    if (current() && snapshot().flows[key]?.subgraph === subgraph) patchFlow(key, folderId, { steps, stepsLoading: false });
  } catch (e) {
    if (current()) patchFlow(key, folderId, { stepsLoading: false, stepsError: errorMessage(e) });
  }
}

export function setFlowMode(folderId: string, root: string, mode: FlowDoc["mode"]) {
  patchFlow(flowKey(folderId, root), folderId, { mode });
}

/** Opens or closes one outline row. */
export function toggleOutlineRow(folderId: string, root: string, rowKey: string, open: boolean) {
  const key = flowKey(folderId, root);
  const toggled = snapshot().flows[key]?.toggled ?? {};
  patchFlow(key, folderId, { toggled: { ...toggled, [rowKey]: open } });
}

/** "Always show / hide" a callee (or a whole type with "<Type>#*"); every open flow re-reads its steps. */
export async function setCallVisibility(calleeKey: string, mode: CallVisibilityMode): Promise<void> {
  try {
    await saveCallVisibility(calleeKey, mode);
  } catch (e) {
    toast.error(errorMessage(e));
    return;
  }
  const open = documentsOf().flatMap(({ doc }) =>
    doc.kind === "flow" && doc.folderId && doc.root ? [[doc.folderId, doc.root] as const] : [],
  );
  await Promise.all([...new Map(open.map((o) => [`${o[0]} ${o[1]}`, o])).values()].map(([f, r]) => loadSteps(f, r)));
}
```

Call `loadSteps` after each place a subgraph lands:
- in `loadFlow`, after `if (current()) patchFlow(key, folderId, { subgraph: sub, pages: 1, loading: false });` add `if (current()) void loadSteps(folderId, root);`
- in `loadMoreNeighbors`, after the merge `patchFlow(...)` add `void loadSteps(folderId, root);`
- in `refreshFolderFlows`, after the success `patchFlow(...)` add `if (current()) void loadSteps(folderId, root);`

- [ ] **Step 3: Typecheck and run the codegraph tests**

Run: `cd frontend && bun test src/modules/codegraph`
Expected: PASS (existing tests that build a `FlowDoc` literal fail to typecheck only under tsc; fix them in Step 1's grep).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/codegraph/codegraphStore.ts frontend/src/modules/codegraph/flowActions.ts
git commit -m "codegraph: load a flow's steps and keep its mode and open rows

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 14: Cards and edges that say something

**Files:**
- Modify: `frontend/src/modules/codegraph/graph/ApiNode.tsx`
- Modify: `frontend/src/modules/codegraph/graph/ConnectorEdge.tsx`
- Modify: `frontend/src/modules/codegraph/graph/FlowCanvas.tsx`

- [ ] **Step 1: `ApiNode.tsx`**

Replace the `ApiNodeData` type and the component body:

```tsx
export type ApiNodeData = {
  layer: string;
  title: string;
  detail: string;
  /** Full text for the native tooltip, since the card truncates both lines. */
  titleTooltip?: string;
  detailTooltip?: string;
  /** The small line above the title: the class a method belongs to. */
  caption?: string;
  /** The doc comment's first sentence; takes the detail well's place when there is one. */
  summary?: string;
  /** What an extractor saw in the body: "Kafka produce", "HTTP call". */
  effects?: string[];
  /** Calls the outline folds away (logging, metrics, helpers), counted since they are not drawn. */
  hiddenCalls?: number;
};

export function ApiNode({ data, selected }: NodeProps & { data: ApiNodeData }) {
  const layer = layerOf(data.layer);
  const visual = LAYERS[layer];
  const Icon = visual.icon;
  const accent = `var(${visual.cssVar})`;
  // "UNKNOWN" on most cards of a Camel or plain-Java service said nothing and
  // took a line; a known layer keeps its colour and caps label.
  const knownLayer = layer !== "unknown";

  return (
    <div
      className={cn(
        "group flex w-67 flex-col gap-2.5 rounded-lg bg-surface-raised p-3 in-focus-visible:ring-2 in-focus-visible:ring-text-primary",
        selected
          ? "border-2 border-text-primary shadow-canvas-node-selected"
          : "border border-border-subtle shadow-canvas-node hover:border-text-tertiary hover:shadow-canvas-node-hover",
      )}
    >
      <Handle id="top" type="target" position={Position.Top} className="!opacity-0" />
      <Handle id="left" type="target" position={Position.Left} className="!opacity-0" />
      <Handle id="bottom" type="source" position={Position.Bottom} className="!opacity-0" />

      <div className="flex items-center gap-2.5">
        <span className={ICON_BADGE}>
          <Icon size={16} strokeWidth={2} style={{ color: accent }} />
        </span>
        <span className="flex min-w-0 flex-col gap-0.5">
          {(knownLayer || data.caption) && (
            <span className="flex min-w-0 items-center gap-1.5">
              {knownLayer && (
                <span className="shrink-0 text-label-11-caps" style={{ color: accent }}>
                  {visual.label}
                </span>
              )}
              {data.caption && (
                <span title={data.titleTooltip} className="truncate text-caption-11 text-text-tertiary">
                  {data.caption}
                </span>
              )}
            </span>
          )}
          <span title={data.titleTooltip ?? data.title} className="truncate text-title-14 text-text-primary">
            {data.title}
          </span>
        </span>
      </div>
      {data.summary ? (
        <span title={data.detailTooltip ?? data.detail} className="line-clamp-2 text-body-12 text-text-secondary">
          {data.summary}
        </span>
      ) : (
        <span
          title={data.detailTooltip ?? data.detail}
          className="truncate rounded-md bg-state-soft px-2.5 py-1.75 text-code-11 text-text-secondary"
        >
          {data.detail}
        </span>
      )}
      {(data.effects?.length || data.hiddenCalls) ? (
        <span className="flex flex-wrap items-center gap-1.5 text-caption-11 text-text-tertiary">
          {data.effects?.map((effect) => (
            <span key={effect} className="rounded-sm border border-border-subtle px-1.5 text-code-11 text-text-secondary">
              {effect}
            </span>
          ))}
          {data.hiddenCalls ? <span>+{data.hiddenCalls} hidden calls</span> : null}
        </span>
      ) : null}
    </div>
  );
}
```

Update the component's doc comment: replace the sentence about "the layer name in caps over the title" with "a known layer's name in caps beside the class, the method as the title, and the doc comment's first sentence (or the file chip when there is none)".

- [ ] **Step 2: `ConnectorEdge.tsx`**

In `ConnectorEdgeData` add:

```ts
  /** The tooltip of the label: the full guard behind a short frame label. */
  title?: string;
```

In `ConnectorEdge`, read it and treat `implements` as dashed:

```tsx
  const title = (data as ConnectorEdgeData | undefined)?.title;
  // An injection and a call into an override are both "not a call you wrote here".
  const dashed = kind === "di" || kind === "implements";
```

and render the pill with:

```tsx
            <EdgeLabelPill kind={kind === "di" ? "di" : "call"} title={title ?? label}>
              {label ?? (kind === "di" ? "injects" : "calls")}
            </EdgeLabelPill>
```

- [ ] **Step 3: `FlowCanvas.tsx`**

Add two props to `FlowCanvasProps`:

```ts
  /** Tooltip of each edge's label (the full guard). Stable, like edgeLabel. */
  edgeTitle?: (edge: CodeGraphEdge) => string | undefined;
  /** Centre this node on the first placement instead of fitting the whole graph — the Outline's selection when switching to Graph. */
  focusNodeId?: string | null;
```

Destructure them in `Canvas` (`edgeTitle, focusNodeId,`). In the `flowEdges` memo, extend `data`:

```ts
            data: {
              kind: e.kind,
              label,
              title: edgeTitle?.(e) ?? label,
              showLabel: e.kind === "di" || !!label,
            },
```

and add `edgeTitle` to that memo's dependency list.

Keep the focus id in a ref so the placement effect does not re-run on selection:

```ts
  const focusRef = useRef(focusNodeId);
  focusRef.current = focusNodeId;
```

In the placement effect, replace `if (nodeSetChanged) void fitView(fitOptions);` with:

```ts
    if (nodeSetChanged) {
      const focus = focusRef.current;
      // A node picked in the Outline is where the reader is; fit around it.
      void fitView(focus && positions.has(focus) ? { nodes: [{ id: focus }], maxZoom: 1, duration: 0 } : fitOptions);
    }
```

- [ ] **Step 4: Typecheck and test**

Run: `cd frontend && bun run build && bun test src/modules/codegraph`
Expected: build succeeds; tests PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/codegraph/graph/ApiNode.tsx frontend/src/modules/codegraph/graph/ConnectorEdge.tsx frontend/src/modules/codegraph/graph/FlowCanvas.tsx
git commit -m "codegraph: cards lead with the method, edges say which call and when

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 15: The Outline view

**Files:**
- Create: `frontend/src/modules/codegraph/outline/OutlineView.tsx`

- [ ] **Step 1: Write the component**

```tsx
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { ChevronDown, ChevronRight, CornerDownLeft, RotateCcw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { useVirtualRows } from "@/components/ui/virtual-rows";
import { cn } from "@/lib/utils";
import type { CodeGraphNode } from "@/bridge/codegraph";
import type { FlowDoc } from "../codegraphStore";
import { loadSteps, setCallVisibility, toggleOutlineRow } from "../flowActions";
import { buildOutline, type OutlineRow } from "./buildOutline";

const ROW_HEIGHT = 26;
const INDENT = 16;

export type OutlineViewProps = {
  folderId: string;
  root: string;
  flow: FlowDoc;
  nodes: CodeGraphNode[];
  selectedNodeId: string | null;
  onInspect: (nodeId: string) => void;
  onShowInGraph: (nodeId: string) => void;
  onOpenAt: (nodeId: string, line: number) => void;
};

/** The node a row is about: the method it names, or the method it calls (else the one it is in). */
function nodeOf(row: OutlineRow): string | null {
  if (row.kind === "method") return row.nodeId;
  if (row.kind === "call") return row.targetId ?? row.ownerId;
  return null;
}

/**
 * The Outline mode of a flow tab: the call flow read top to bottom, as the
 * code runs it. Rows come from buildOutline; this draws them, windows them,
 * and turns clicks, keys and the context menu into store actions.
 */
export function OutlineView({ folderId, root, flow, nodes, selectedNodeId, onInspect, onShowInGraph, onOpenAt }: OutlineViewProps) {
  const byId = useMemo(() => new Map(nodes.map((n) => [n.id, n])), [nodes]);
  const rows = useMemo(
    () => (flow.steps ? buildOutline({ rootId: root, nodes: byId, steps: flow.steps, toggled: flow.toggled }) : []),
    [flow.steps, flow.toggled, root, byId],
  );
  const { scrollRef, indices, padTop, padBottom } = useVirtualRows(rows.length, ROW_HEIGHT);
  const [cursor, setCursor] = useState(0);

  // The selection from the Graph (or the inspector) brings its first row into view.
  useEffect(() => {
    if (!selectedNodeId) return;
    const index = rows.findIndex((r) => nodeOf(r) === selectedNodeId);
    if (index < 0) return;
    setCursor(index);
    scrollToIndex(index);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only when the selection changes
  }, [selectedNodeId]);

  function scrollToIndex(index: number) {
    const el = scrollRef.current;
    if (!el) return;
    const top = index * ROW_HEIGHT;
    if (top < el.scrollTop || top + ROW_HEIGHT > el.scrollTop + el.clientHeight) el.scrollTop = Math.max(0, top - el.clientHeight / 3);
  }

  function toggle(row: OutlineRow, open: boolean) {
    if (row.kind === "fold" || ((row.kind === "call" || row.kind === "method") && row.expandable)) {
      toggleOutlineRow(folderId, root, row.key, open);
    }
  }

  function activate(index: number) {
    const row = rows[index];
    if (!row) return;
    setCursor(index);
    if (row.kind === "fold") return toggle(row, !row.open);
    if ((row.kind === "call" || row.kind === "method") && row.ref) {
      const target = rows.findIndex((r) => r.key === row.ref!.key);
      if (target >= 0) {
        setCursor(target);
        scrollToIndex(target);
      }
      return;
    }
    const id = nodeOf(row);
    if (id) onInspect(id);
  }

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const row = rows[cursor];
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const next = Math.max(0, Math.min(rows.length - 1, cursor + (e.key === "ArrowDown" ? 1 : -1)));
      setCursor(next);
      scrollToIndex(next);
    } else if (e.key === "ArrowRight" && row) {
      e.preventDefault();
      toggle(row, true);
    } else if (e.key === "ArrowLeft" && row) {
      e.preventDefault();
      toggle(row, false);
    } else if (e.key === "Enter") {
      e.preventDefault();
      activate(cursor);
    }
  };

  if (flow.stepsError) {
    return (
      <div className="flex flex-col items-start gap-3 p-5 text-body-13 text-status-danger">
        {flow.stepsError}
        <Button variant="outline" size="xs" className="bg-surface-raised" onClick={() => void loadSteps(folderId, root)}>
          Try again
          <RotateCcw className="size-3.5" />
        </Button>
      </div>
    );
  }
  if (!flow.steps) return <p className="p-5 text-body-13 text-text-tertiary">Reading the flow…</p>;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {flow.steps.legacy && (
        <p className="border-b border-border-subtle px-4 py-2 text-caption-12 text-text-tertiary">
          This index was written before branches were recorded. Re-index to see them.
        </p>
      )}
      <div
        ref={scrollRef}
        data-scroll-box
        tabIndex={0}
        role="tree"
        aria-label="Flow outline"
        onKeyDown={onKeyDown}
        className="min-h-0 flex-1 overflow-auto py-2 font-mono text-code-12 outline-none"
      >
        <div style={{ height: padTop }} />
        {indices.map((index) => (
          <OutlineLine
            key={rows[index].key}
            row={rows[index]}
            selected={nodeOf(rows[index]) !== null && nodeOf(rows[index]) === selectedNodeId}
            focused={index === cursor}
            filePathOf={(id) => byId.get(id)?.filePath ?? ""}
            onActivate={() => activate(index)}
            onToggle={(open) => toggle(rows[index], open)}
            onShowInGraph={onShowInGraph}
            onOpenAt={onOpenAt}
          />
        ))}
        <div style={{ height: padBottom }} />
      </div>
    </div>
  );
}

function OutlineLine({
  row,
  selected,
  focused,
  onActivate,
  onToggle,
  onShowInGraph,
  onOpenAt,
}: {
  row: OutlineRow;
  selected: boolean;
  focused: boolean;
  filePathOf: (id: string) => string;
  onActivate: () => void;
  onToggle: (open: boolean) => void;
  onShowInGraph: (nodeId: string) => void;
  onOpenAt: (nodeId: string, line: number) => void;
}) {
  const expandable = (row.kind === "call" || row.kind === "method") && row.expandable;
  const open = (row.kind === "call" || row.kind === "method") && row.open;
  const line = (
    <div
      role="treeitem"
      aria-selected={selected}
      aria-expanded={expandable ? open : undefined}
      onClick={onActivate}
      style={{ height: ROW_HEIGHT, paddingLeft: 12 + row.depth * INDENT }}
      className={cn(
        "flex cursor-default items-center gap-1.5 pr-4 whitespace-nowrap",
        selected ? "bg-state-selected" : "hover:bg-state-soft",
        focused && "outline-1 -outline-offset-1 outline-border-default",
      )}
    >
      <span
        className="flex size-4 shrink-0 items-center justify-center text-icon-secondary"
        onClick={(e) => {
          if (!expandable) return;
          e.stopPropagation();
          onToggle(!open);
        }}
      >
        {expandable ? open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" /> : null}
      </span>
      <RowBody row={row} />
    </div>
  );

  if (row.kind !== "call" && row.kind !== "method") return line;
  const nodeId = nodeOf(row)!;
  const key = row.kind === "call" ? row.step.calleeKey : "";
  const type = key.includes("#") ? key.slice(0, key.indexOf("#")) : "";
  const member = key.includes("#") ? key.slice(key.indexOf("#") + 1) : "";
  const at = row.kind === "call" ? row.step.line : row.line;
  const ownerId = row.kind === "call" ? row.ownerId : row.nodeId;
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{line}</ContextMenuTrigger>
      <ContextMenuContent>
        {key && type && member && (
          <>
            <ContextMenuLabel className="max-w-80 truncate font-mono">{key}</ContextMenuLabel>
            <ContextMenuItem onSelect={() => void setCallVisibility(key, "show")}>Always show {simpleName(type)}.{member}</ContextMenuItem>
            <ContextMenuItem onSelect={() => void setCallVisibility(key, "hide")}>Always hide {simpleName(type)}.{member}</ContextMenuItem>
            <ContextMenuItem onSelect={() => void setCallVisibility(`${type}#*`, "show")}>Always show every call on {simpleName(type)}</ContextMenuItem>
            <ContextMenuItem onSelect={() => void setCallVisibility(`${type}#*`, "hide")}>Always hide every call on {simpleName(type)}</ContextMenuItem>
            <ContextMenuItem
              onSelect={() => {
                void setCallVisibility(key, "");
                void setCallVisibility(`${type}#*`, "");
              }}
            >
              Reset to default
            </ContextMenuItem>
            <ContextMenuSeparator />
          </>
        )}
        <ContextMenuItem onSelect={() => onOpenAt(ownerId, at)}>Open in editor</ContextMenuItem>
        <ContextMenuItem onSelect={() => onShowInGraph(nodeId)}>Show in Graph</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}

const simpleName = (type: string) => type.slice(type.lastIndexOf(".") + 1);

function RowBody({ row }: { row: OutlineRow }) {
  switch (row.kind) {
    case "method":
      return (
        <>
          {row.impl && <span className="text-text-tertiary">↳</span>}
          <span className="font-semibold text-text-primary">{row.title}</span>
          {row.ref ? <RefTag reason={row.ref.reason} /> : row.summary && <span className="truncate text-text-tertiary">— {row.summary}</span>}
          <span className="ml-auto pl-3 text-text-tertiary">:{row.line}</span>
        </>
      );
    case "frame":
      return (
        <span className="flex items-center gap-1 text-(--hue-blue)">
          {row.text}
          {row.exits && <CornerDownLeft aria-label="leaves" className="size-3 text-text-tertiary" />}
        </span>
      );
    case "fold":
      return (
        <span className="rounded-sm border border-dashed border-border-default px-1.5 text-caption-11 text-text-tertiary" title={row.reasons}>
          {row.open ? "−" : "+"}
          {row.count} {row.count === 1 ? "call" : "calls"} · {row.reasons}
        </span>
      );
    case "more":
      return <span className="text-text-tertiary">… more steps not shown</span>;
    case "call": {
      const project = !!row.targetId;
      return (
        <>
          <span
            title={row.hidden ? `Hidden: ${row.step.reason}` : row.step.reason}
            className={cn("truncate", project ? "font-semibold text-text-primary" : "text-text-secondary", row.hidden && "opacity-60")}
          >
            {row.title}
          </span>
          {row.step.impls?.length ? <span className="text-text-tertiary">↳ {row.step.impls.length} implementations</span> : null}
          {row.step.effect && (
            <span className="rounded-sm border border-border-subtle px-1.5 text-caption-11 text-text-secondary">
              {row.step.reason}
            </span>
          )}
          {row.hidden && <span className="text-caption-11 text-text-tertiary">{row.step.reason}</span>}
          {row.ref && <RefTag reason={row.ref.reason} />}
          <span className="ml-auto pl-3 text-text-tertiary">:{row.step.line}</span>
        </>
      );
    }
  }
}

function RefTag({ reason }: { reason: "shown-above" | "recursion" }) {
  return <span className="text-caption-11 text-text-tertiary">{reason === "recursion" ? "↻ recursion" : "shown above ↑"}</span>;
}
```

Before writing, confirm two class names exist in `frontend/src/index.css`: `bg-state-selected` and `outline-border-default` (`grep -n "state-selected\|--border-default" frontend/src/index.css`). If `--state-selected` is not a token, use `bg-state-soft` for the selected row and `hover:bg-surface-raised` for hover.

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bun run build`
Expected: success.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/codegraph/outline/OutlineView.tsx
git commit -m "codegraph: the Outline mode of a flow

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 16: Two modes in the flow document

**Files:**
- Modify: `frontend/src/modules/codegraph/FlowDocument.tsx`

- [ ] **Step 1: Wire the modes**

Add imports:

```tsx
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { OutlineView } from "./outline/OutlineView";
import { cardData, graphView } from "./graph/graphView";
import { setFlowMode } from "./flowActions";
import type { CodeGraphNode } from "@/bridge/codegraph";
```

(`setFlowMode` joins the existing `./flowActions` import.)

Inside `FlowDocument`, after `const onSelect = …`, add:

```tsx
  const selectedNodeId = useCodeGraphStore((s) => (key && s.inspector?.flow === key ? s.inspector.nodeId : null));
  const mode = flow?.mode ?? "outline";
  const view = useMemo(
    () => (flow?.subgraph ? graphView(root, flow.subgraph.nodes, flow.subgraph.edges, flow.steps) : null),
    [flow?.subgraph, flow?.steps, root],
  );
  const buildData = useCallback(
    (n: CodeGraphNode) => cardData(n, view?.hiddenCalls.get(n.id) ?? 0, view?.effects.get(n.id) ?? []),
    [view],
  );
  const showInGraph = useCallback(
    (nodeId: string) => {
      setFlowMode(folderId, root, "graph");
      void inspectNode(folderId, root, nodeId);
    },
    [folderId, root],
  );
  const openAt = useCallback(
    (nodeId: string, line: number) => {
      const n = flow?.subgraph?.nodes.find((x) => x.id === nodeId);
      if (n?.filePath) void openFile({ workspaceId: ownership.owner?.workspaceId ?? "", folderId, path: n.filePath }, line, { preview: false, beside: true });
    },
    [flow?.subgraph, folderId, ownership.owner?.workspaceId],
  );
```

Move these hooks above the early `return`s so hook order never changes (they only read, and `view` is null without a subgraph).

Build the shared header once, before `if (subgraph)`; it is the current `header={…}` content moved into a variable:

```tsx
  const subgraph = flow?.subgraph;
  const rootNode = subgraph?.nodes.find((n) => n.id === root);
  const header = (
    <>
      <NavButtons />
      {/* Move here, verbatim, the JSX FlowCanvas currently receives after <NavButtons /> in its
          `header={…}` prop: the `entry ? (<>verb chip, title, route/group</>) : (<span>root title</span>)`
          block and the `several ? <Chip …>{ownership.label}</Chip> : null` line. */}
    </>
  );
  const modeSwitch = (
    <Tabs value={mode} onValueChange={(value) => setFlowMode(folderId, root, value as "outline" | "graph")}>
      <TabsList>
        <TabsTrigger value="outline">Outline</TabsTrigger>
        <TabsTrigger value="graph">Graph</TabsTrigger>
      </TabsList>
    </Tabs>
  );
```

Replace the `if (subgraph) { return (…) }` block with:

```tsx
  if (subgraph && view) {
    if (mode === "outline") {
      return (
        <div className="flex h-full w-full flex-col overflow-hidden px-4 pt-3">
          <div className="flex shrink-0 items-center justify-between gap-4 pb-1">
            <div className="flex min-w-0 items-center gap-2.5">{header}</div>
            <div className="flex shrink-0 items-center gap-3.5">
              {flow.stepsLoading && <span className="text-code-11 text-text-tertiary">reading…</span>}
              {modeSwitch}
            </div>
          </div>
          <div className="h-px shrink-0 bg-border-subtle" />
          <OutlineView
            folderId={folderId}
            root={root}
            flow={flow}
            nodes={subgraph.nodes}
            selectedNodeId={selectedNodeId}
            onInspect={onSelect}
            onShowInGraph={showInGraph}
            onOpenAt={openAt}
          />
        </div>
      );
    }
    return (
      <div className="flex h-full w-full flex-col overflow-hidden">
        <FlowCanvas
          key={root}
          rootId={root}
          nodes={view.nodes}
          edges={view.edges}
          onSelect={onSelect}
          buildData={buildData}
          edgeLabel={view.edgeLabel}
          edgeTitle={view.edgeTitle}
          selectedNodeId={selectedNodeId}
          focusNodeId={selectedNodeId}
          header={header}
          meta={(counts: CanvasCounts) => (
            <>
              {/* Move here, verbatim, the body FlowCanvas currently receives as `meta`: the
                  describeCounts(counts) span and the `subgraph.truncated && (…partial… Load more…)` block. */}
              {modeSwitch}
            </>
          )}
          hint={
            <>
              <strong className="text-text-primary">Click</strong> to inspect
            </>
          }
        />
      </div>
    );
  }
```

`flow` is non-null inside `if (subgraph && view)`; if TypeScript does not narrow it, write `flow!` in the two props that need it.

- [ ] **Step 2: Typecheck, test, build**

Run: `cd frontend && bun test src/modules/codegraph && bun run build`
Expected: tests PASS, build succeeds.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/codegraph/FlowDocument.tsx
git commit -m "codegraph: a flow tab switches between Outline and Graph

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```

---

## Task 17: Docs and full verification

**Files:**
- Modify: `AGENTS.md` (new subsection after "CodeGraph: guards on call edges")

- [ ] **Step 1: Add the AGENTS.md subsection**

```markdown
### CodeGraph: flow outline and steps
A flow tab has two modes, **Outline | Graph** (shadcn `Tabs`, Outline first; spec
`docs/superpowers/specs/2026-10-15-codegraph-flow-outline-design.md`).
- **Steps are call sites with more columns** (`call_sites.col/text/callee_key/receiver/result_use/frames/role`,
  `migrateCallSiteSteps`). Java fills them (`Steps.java`); Lua and TS do not yet, and read as a flat
  outline. `frames` are language-neutral (`if, else, case, catch, finally, try, loop, closure, and, or,
  ternary, await`); a closure passed as an argument has the id of the call it is passed to. A guard clause
  is a frame only for the calls inside it. `role = "override"` rows are dispatch links → `Step.Impls`.
- **Visibility is decided at read time** (`internal/codegraph/callimportance.go`, pure `Visibility`):
  override → project (helper hidden) → structural (wraps a project call, extractor effect, decides a
  branch) → infrastructure table → writes to input/state → common in the project → accessor / side
  effect / value. Every answer has a reason the outline shows. Overrides are local preferences
  (`codegraph_call_visibility`, `PreferencesGet/SetCallVisibility`, UI only, all projects).
- `CodeGraphGetSteps(nodeIds ≤ 200, servicePath)` asks the same Service as `CodeGraphGetNeighbors`.
  The frontend builds rows with `outline/buildOutline.ts` and the graph's labels/hidden helpers with
  `graph/graphView.ts`, both pure and tested with `bun test src/modules/codegraph`.
```

- [ ] **Step 2: Run everything**

```bash
cd analyzer && mvn -q test && cd ..
go test ./internal/codegraph/ ./internal/preferences/ ./internal/bridge/ ./internal/app/ ./internal/arch/ -count=1
gofmt -l internal
go vet -tags server ./internal/...
cd frontend && bun test src/modules/codegraph && bun run build && cd ..
```

Expected: every command succeeds; `gofmt -l internal` prints nothing new (ignore `internal/crypto/keyring.go`).

- [ ] **Step 3: Rebuild the analyzer jar and check the worked example by hand**

```bash
task build:analyzer
```

Then run the desktop app (`task dev`), open `/home/ptvu3/Workspace/fsap/fsap-event-router`, let it re-index, open the `organization-fanout-{domain}` entry point and check:
- Outline: `process` → `decide` with two `↳` implementations; `catch AclLookupException ↩` → `+1 · logging` → `quarantine`; `if decision instanceof Skip ↩` → a fold of logging/metrics/helper; `breaker.executeCallable(() -> …)` with `deliver` under it; `quarantine` expands to the `in.setHeader(...)` calls and `producerTemplate.send(...)`.
- Right-click `in.setHeader` → "Always hide Message.setHeader" → it folds; "Reset to default" brings it back.
- Select `quarantine` in Outline → switch to Graph → that card is selected and centred; edges read `1 · try`, `2 · catch AclLookupException`; `decide → OrganizationFanoutProcessor.decide` is dashed "implements"; no `metricPrefix` / `kafkaEndpoint` / `counter` cards.

Record anything that differs in the PR description; fix code only for real defects.

- [ ] **Step 4: Commit**

```bash
git add AGENTS.md
git commit -m "docs: CodeGraph flow outline in AGENTS.md

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>"
```
