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
        return sitesOf(root, "Base.java", SOURCE, fromPrefix);
    }

    private static List<CallSite> sitesOf(Path root, String fileName, String source, String fromPrefix) throws IOException {
        Path file = root.resolve("src/main/java/acme/" + fileName);
        Files.createDirectories(file.getParent());
        Files.writeString(file, source);
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

    @Test
    void aCallNothingResolvesIsKeyedByWhatTheSourceSays(@TempDir Path root) throws IOException {
        List<CallSite> sites = sitesOf(root, "U.java", """
                package acme;
                class U {
                    void go(Unknown u) {
                        helper();
                        u.foo();
                        a.b.c();
                        make().bar();
                    }
                }
                """, "acme.U.go(");
        assertEquals("acme.U#helper", byText(sites, "helper()").calleeKey);
        assertEquals("Unknown#foo", byText(sites, "u.foo()").calleeKey);
        assertEquals("a.b#c", byText(sites, "a.b.c()").calleeKey);
        assertEquals("acme.U#bar", byText(sites, "make().bar()").calleeKey,
                "a receiver that is itself a call is not a name to key by");
    }

    @Test
    void aNodeIdKeyDropsTheDisambiguationSuffix() {
        assertEquals("acme.Base#decide", ProjectAnalyzer.keyOfNodeId("acme.Base.decide(java.lang.String)"));
        assertEquals("acme.Base#decide", ProjectAnalyzer.keyOfNodeId("acme.Base [mod].decide(java.lang.String)"));
        assertEquals("acme.Base#decide", ProjectAnalyzer.keyOfNodeId("acme.Base [mod #2].decide()"));
        assertEquals("acme.Base#decide", ProjectAnalyzer.keyOfNodeId("acme.Base [#2].decide()"));
        assertEquals("acme.Base.Inner#go", ProjectAnalyzer.keyOfNodeId("acme.Base [#2].Inner.go()"));
        assertEquals("acme.Base#decide", ProjectAnalyzer.keyOfNodeId("acme.Base.decide(int) [#2]"));
    }

    @Test
    void readingAStepNeverTakesTheAnalysisDown() {
        com.github.javaparser.ast.expr.MethodCallExpr call =
                com.github.javaparser.StaticJavaParser.parseExpression("a().b()");
        Steps.Info info = ProjectAnalyzer.stepsOf(call, null);
        assertEquals("", info.text());
        assertEquals("unknown", info.receiver());
        assertTrue(info.frames().isEmpty());
    }
}
