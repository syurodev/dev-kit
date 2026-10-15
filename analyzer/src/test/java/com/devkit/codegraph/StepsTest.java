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
}
