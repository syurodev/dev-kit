package com.devkit.codegraph;

import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.expr.MethodCallExpr;
import org.junit.jupiter.api.Test;

import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;

class GuardsTest {

    static {
        // Multi-label and arrow switches need a modern language level.
        StaticJavaParser.getParserConfiguration()
                .setLanguageLevel(com.github.javaparser.ParserConfiguration.LanguageLevel.JAVA_21);
    }

    /** The guard of the one call to {@code target()} inside {@code run}, in a class whose body is {@code body}. */
    private static String guardOfTarget(String body) {
        CompilationUnit unit = StaticJavaParser.parse("class C { " + body + " }");
        MethodDeclaration run = unit.findAll(MethodDeclaration.class).stream()
                .filter(m -> m.getNameAsString().equals("run")).findFirst().orElseThrow();
        List<MethodCallExpr> calls = run.findAll(MethodCallExpr.class).stream()
                .filter(c -> c.getNameAsString().equals("target")).toList();
        assertEquals(1, calls.size(), "the snippet must call target() exactly once");
        return Guards.of(calls.get(0), run);
    }

    @Test
    void anUnconditionalCallHasNoGuard() {
        assertEquals("", guardOfTarget("void run() { target(); }"));
    }

    @Test
    void thenAndElseBranchesOfAnIf() {
        assertEquals("flag", guardOfTarget("void run() { if (flag) { target(); } }"));
        assertEquals("!flag", guardOfTarget("void run() { if (flag) { x(); } else { target(); } }"));
        assertEquals("flag", guardOfTarget("void run() { if (flag) target(); }"));
    }

    @Test
    void theConditionItselfIsNotGuarded() {
        assertEquals("", guardOfTarget("void run() { if (target()) { x(); } }"));
    }

    @Test
    void nestedConditionsReadOutermostFirst() {
        assertEquals("a && b", guardOfTarget("void run() { if (a) { if (!b) { x(); } else { target(); } } }"));
        assertEquals("a && !b", guardOfTarget("void run() { if (a) { if (b) { x(); } else { target(); } } }"));
        assertEquals("a && b", guardOfTarget("void run() { if (a) { if (b) { target(); } } }"));
    }

    @Test
    void theSameConditionTwiceIsOneFact() {
        assertEquals("a", guardOfTarget("void run() { if (a) { if (a) { target(); } } }"));
    }

    @Test
    void negationIsWrittenTheWayAPersonWouldWriteIt() {
        assertEquals("x != null", guardOfTarget("void run() { if (x == null) { y(); } else { target(); } }"));
        assertEquals("x == null", guardOfTarget("void run() { if (x != null) { y(); } else { target(); } }"));
        assertEquals("n >= 3", guardOfTarget("void run() { if (n < 3) { y(); } else { target(); } }"));
        assertEquals("!enabled()", guardOfTarget("void run() { if (enabled()) { y(); } else { target(); } }"));
        assertEquals("enabled", guardOfTarget("void run() { if (!enabled) { y(); } else { target(); } }"));
        assertEquals("!(a && b)", guardOfTarget("void run() { if (a && b) { y(); } else { target(); } }"));
        assertEquals("flag", guardOfTarget("void run() { if ((flag)) { target(); } }"));
    }

    @Test
    void theGuardClauseGuardsEverythingAfterIt() {
        assertEquals("enabled", guardOfTarget("void run() { if (!enabled) { return; } target(); }"));
        assertEquals("enabled", guardOfTarget("void run() { if (!enabled) throw new RuntimeException(); target(); }"));
        assertEquals("a && b", guardOfTarget("void run() { if (!a) return; if (!b) return; target(); }"));
    }

    @Test
    void aGuardClauseDoesNotGuardWhatCameBeforeIt() {
        assertEquals("", guardOfTarget("void run() { target(); if (!enabled) { return; } }"));
    }

    @Test
    void anIfThatDoesNotLeaveTheBlockGuardsNothingAfterIt() {
        assertEquals("", guardOfTarget("void run() { if (!enabled) { x(); } target(); }"));
    }

    @Test
    void ifElseWhereOneSideLeavesGuardsTheRest() {
        assertEquals("!a", guardOfTarget("void run() { if (a) { return; } else { x(); } target(); }"));
        assertEquals("a", guardOfTarget("void run() { if (a) { x(); } else { return; } target(); }"));
    }

    @Test
    void theGuardClauseAndTheBranchBothCount() {
        assertEquals("enabled && mode == 1",
                guardOfTarget("void run() { if (!enabled) return; if (mode == 1) { target(); } }"));
    }

    @Test
    void ternaryBranches() {
        assertEquals("flag", guardOfTarget("int run() { return flag ? target() : 0; }"));
        assertEquals("!flag", guardOfTarget("int run() { return flag ? 0 : target(); }"));
        assertEquals("", guardOfTarget("int run() { return target() > 0 ? 1 : 0; }"));
    }

    @Test
    void theRightSideOfShortCircuitOperators() {
        assertEquals("flag", guardOfTarget("boolean run() { return flag && target(); }"));
        assertEquals("!flag", guardOfTarget("boolean run() { return flag || target(); }"));
        assertEquals("", guardOfTarget("boolean run() { return target() && flag; }"));
        assertEquals("a && b", guardOfTarget("boolean run() { return a && b && target(); }"));
    }

    @Test
    void switchEntries() {
        assertEquals("kind == A", guardOfTarget("void run() { switch (kind) { case A: target(); break; default: x(); } }"));
        assertEquals("kind in (A, B)", guardOfTarget("void run() { switch (kind) { case A, B -> target(); default -> x(); } }"));
        assertEquals("kind == default", guardOfTarget("void run() { switch (kind) { case A -> x(); default -> target(); } }"));
        assertEquals("", guardOfTarget("void run() { switch (target()) { case A: x(); } }"));
        assertEquals("kind == A", guardOfTarget("int run() { return switch (kind) { case A -> target(); default -> 0; }; }"));
    }

    @Test
    void loopsAndTryAreNotConditions() {
        assertEquals("", guardOfTarget("void run() { for (var x : xs) { target(); } }"));
        assertEquals("", guardOfTarget("void run() { try { target(); } catch (RuntimeException e) { x(); } }"));
    }

    @Test
    void anAlwaysTrueConditionAddsNothing() {
        assertEquals("", guardOfTarget("void run() { if (true) { target(); } }"));
    }

    @Test
    void whitespaceInAConditionIsFlattened() {
        assertEquals("a.b(c, d)", guardOfTarget("void run() { if (a.b(c,\n   d)) { target(); } }"));
    }

    @Test
    void aLongGuardIsCut() {
        String condition = "a".repeat(400);
        String guard = guardOfTarget("void run() { if (" + condition + ") { target(); } }");
        assertEquals(Guards.MAX_LENGTH, guard.length());
        assertEquals('…', guard.charAt(guard.length() - 1));
    }

    @Test
    void aCatchBlockGuardsItsCalls() {
        assertEquals("catch IllegalStateException",
                guardOfTarget("void run() { try { x(); } catch (IllegalStateException e) { target(); } }"));
        assertEquals("", guardOfTarget("void run() { try { target(); } catch (IllegalStateException e) { y(); } }"));
        assertEquals("catch A | B", guardOfTarget("void run() { try { x(); } catch (A | B e) { target(); } }"));
    }

    @Test
    void mergingGuards() {
        assertEquals("", Guards.merge(List.of("a", "")), "one unconditional call makes the edge unconditional");
        assertEquals("a", Guards.merge(List.of("a", "a")));
        assertEquals("a || b", Guards.merge(List.of("a", "b")));
        assertEquals("(a && b) || c", Guards.merge(List.of("a && b", "c")));
        assertEquals("a || b", Guards.merge(List.of("a", "b", "a")));
    }
}
