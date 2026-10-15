package com.devkit.codegraph;

import com.github.javaparser.ParserConfiguration;
import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.expr.ObjectCreationExpr;
import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.List;
import java.util.stream.Collectors;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

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

    /** Each frame's exit kind, outermost first ("-" when the branch does not leave). */
    private static String exitKinds(String body, String name) {
        MethodDeclaration run = run(body);
        return Steps.frames(call(run, name), run).stream()
                .map(f -> f.exit.isEmpty() ? "-" : f.exit)
                .collect(Collectors.joining(" / "));
    }

    @Test
    void aBranchThatLeavesSaysHow() {
        assertEquals("return", exitKinds("void run() { if (skip) { target(); return; } y(); }", "target"));
        assertEquals("throw", exitKinds("void run() { try { x(); } catch (E e) { target(); throw new R(e); } }", "target"));
        assertEquals("- / break", exitKinds("void run() { while (more()) { if (done) { target(); break; } } }", "target"));
        assertEquals("- / continue", exitKinds("void run() { for (D d : ds) { if (skip(d)) { target(); continue; } } }", "target"));
        assertEquals("return", exitKinds("int run() { switch (m) { case A: target(); return 1; default: return 2; } }", "target"));
        assertEquals("-", exitKinds("void run() { if (flag) { target(); } }", "target"));
    }

    @Test
    void aBranchEndingInAnIfElseThatBothLeaveTakesTheFirstKind() {
        assertEquals("throw", exitKinds("void run() { if (a) { target(); if (b) { throw new X(); } else { return; } } y(); }", "target"));
        assertEquals("return", exitKinds("void run() { if (a) { target(); if (b) { return; } else { return; } } y(); }", "target"));
        // Only one side leaves: the branch can fall out of its end.
        assertEquals("-", exitKinds("void run() { if (a) { target(); if (b) { return; } } y(); }", "target"));
    }

    @Test
    void exitsStaysTheBooleanOfExit() {
        MethodDeclaration run = run("void run() { if (skip) { target(); return; } if (go) { other(); } }");
        assertTrue(Steps.frames(call(run, "target"), run).get(0).exits);
        assertEquals(false, Steps.frames(call(run, "other"), run).get(0).exits);
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
        int col = wrapper.getName().getBegin().orElseThrow().column;
        assertEquals(line + ":" + col, got.get(0).id, "the frame id is the wrapping call's position");
    }

    @Test
    void framesNestOutermostFirst() {
        assertEquals("try:try / if:if ok / closure:items.forEach(i -> …)",
                frames("void run() { try { if (ok) { items.forEach(i -> target(i)); } } catch (E e) { } }", "target"));
    }

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

    @Test
    void aCallInAChainIsPlacedAtItsName() {
        // getBegin() of `a().b(...)` is where `a()` starts, so both calls would
        // share one column and c's closure could belong to either.
        MethodDeclaration run = run("void run() { a().b(x -> c()); }");
        MethodCallExpr a = call(run, "a");
        MethodCallExpr b = call(run, "b");
        assertNotEquals(Steps.col(a), Steps.col(b));
        assertEquals(b.getName().getBegin().orElseThrow().column, Steps.col(b));
        int line = b.getBegin().orElseThrow().line;
        assertEquals(line + ":" + Steps.col(b), Steps.position(b));
        List<StepFrame> got = Steps.frames(call(run, "c"), run);
        assertEquals(Steps.position(b), got.get(0).id, "c's closure is the one passed to b");
    }

    @Test
    void anExpressionLambdaReturnsItsCall() {
        assertEquals("returned", use("void run() { items.map(o -> o.getId()); }", "getId"));
        assertEquals("discarded", use("void run() { items.forEach(o -> { o.go(); }); }", "go"));
    }

    @Test
    void anEmptyInfoSaysNothingAndFramesAreCopied() {
        MethodDeclaration run = run("void run() { a().b(); }");
        MethodCallExpr b = call(run, "b");
        Steps.Info empty = Steps.Info.empty(b);
        assertEquals(Steps.col(b), empty.col());
        assertEquals("", empty.text());
        assertEquals("unknown", empty.receiver());
        assertEquals("unknown", empty.resultUse());
        assertTrue(empty.frames().isEmpty());

        List<StepFrame> frames = new ArrayList<>(List.of(new StepFrame("1:1", "if", "if a", false)));
        Steps.Info info = new Steps.Info(1, "x()", "this", "discarded", frames);
        frames.clear();
        assertEquals(1, info.frames().size());
        assertThrows(UnsupportedOperationException.class, () -> info.frames().clear());
    }

    @Test
    void aLocalIsFromAParamOnlyWhenItsValueIsReachedFromOne() {
        assertEquals("local-from-param", receiver(
                "void run(Exchange exchange) { Body b = exchange.getIn().getBody(); b.target(); }", "target"));
        assertEquals("local-from-param", receiver("void run(Req req) { Req r = (Req) req; r.target(); }", "target"));
        assertEquals("local-from-param", receiver(
                "void run(Exchange exchange) { Message in = exchange.getIn(); Body b = in.getBody(); b.target(); }",
                "target"));
        assertEquals("local", receiver("void run(Req req) { Resp r = new Resp(req.id()); r.target(); }", "target"));
        assertEquals("local", receiver("void run(Req req) { Resp r = foo(req); r.target(); }", "target"));
    }

    @Test
    void lambdasAndAnonymousClassesAreShortenedAnywhereInTheText() {
        MethodDeclaration run = run("void run() { list.stream().filter(i -> { return i.ok(); })"
                + ".map(i -> i.name()).forEach(x -> c(x)); }");
        assertEquals("list.stream().filter(i -> …).map(i -> …).forEach(x -> …)", Steps.text(call(run, "forEach")));

        MethodDeclaration anon = run("void run() { exec.submit(new Runnable() { public void run() { work(); } }); }");
        assertEquals("exec.submit(new Runnable() {…})", Steps.text(call(anon, "submit")));
        ObjectCreationExpr creation = anon.findFirst(ObjectCreationExpr.class).orElseThrow();
        assertEquals("new Runnable() {…}", Steps.text(creation));
        assertTrue(creation.getAnonymousClassBody().isPresent(), "the source tree is left alone");
    }

    @Test
    void anAnonymousClassIsAClosure() {
        MethodDeclaration run = run("void run() { exec.submit(new Runnable() { public void run() { work(); } }); }");
        List<StepFrame> got = Steps.frames(call(run, "work"), run);
        assertEquals(1, got.size());
        assertEquals("closure", got.get(0).kind);
        assertEquals("exec.submit(new Runnable() {…})", got.get(0).text);
        assertEquals(Steps.position(call(run, "submit")), got.get(0).id);

        MethodDeclaration held = run("void run() { Runnable r = new Runnable() { public void run() { work(); } }; }");
        List<StepFrame> heldFrames = Steps.frames(call(held, "work"), held);
        assertEquals("closure:new Runnable() {…}", heldFrames.get(0).kind + ":" + heldFrames.get(0).text);
        assertEquals(Steps.position(held.findFirst(ObjectCreationExpr.class).orElseThrow()), heldFrames.get(0).id);

        assertEquals("local", receiver(
                "void run() { exec.submit(new Handler() { public void handle(Job job) { job.go(); } }); }", "go"));
    }

    @Test
    void anAlwaysTrueConditionStillReadsAsOne() {
        assertEquals("loop:while true", frames("void run() { while (true) { target(); } }", "target"));
        assertEquals("if:if true", frames("void run() { if (true) { target(); } }", "target"));
        assertEquals("loop:do … while true", frames("void run() { do { target(); } while (true); }", "target"));
    }

    @Test
    void everyCallOfAMultiLineChainHasItsOwnPosition() {
        MethodDeclaration run = run("void run() {\n  repo.findAll()\n      .stream()\n      .forEach(x -> deliver(x));\n}");
        List<String> positions = List.of(
                Steps.position(call(run, "findAll")), Steps.position(call(run, "stream")),
                Steps.position(call(run, "forEach")));
        assertEquals(3, positions.stream().distinct().count(), positions.toString());
        assertEquals(call(run, "stream").getName().getBegin().orElseThrow().line + ":" + Steps.col(call(run, "stream")),
                Steps.position(call(run, "stream")));
        assertEquals(Steps.position(call(run, "forEach")), Steps.frames(call(run, "deliver"), run).get(0).id);
    }

    @Test
    void manyCallsInOneClosureAreReadInLinearTime() {
        StringBuilder body = new StringBuilder("void run() { tx.execute(s -> {\n");
        for (int i = 0; i < 1500; i++) {
            body.append("  s.step").append(i).append("(a").append(i).append(", b.get(").append(i).append("));\n");
        }
        body.append("  return null; }); }");
        MethodDeclaration run = run(body.toString());
        Steps.Context ctx = Steps.context(run);
        long start = System.nanoTime();
        int read = 0;
        for (MethodCallExpr c : run.findAll(MethodCallExpr.class)) {
            read += Steps.of(c, ctx).frames().size();
        }
        long ms = (System.nanoTime() - start) / 1_000_000;
        assertTrue(read >= 3000, "every call inside the closure is framed by it");
        assertTrue(ms < 3000, "reading 3001 calls took " + ms + " ms; the wrapping call is being printed per call again");
    }
}
