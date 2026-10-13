package com.devkit.codegraph;

import java.nio.file.Path;
import java.nio.file.Files;
import java.util.*;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class CamelTest {
    private static AnalysisResult result;
    @BeforeAll static void analyze() throws Exception {
        result = new ProjectAnalyzer(Path.of("src/test/resources/camel-project")).analyze();
    }
    private EntryPoint route(String routeId) {
        return result.entryPoints.stream().filter(e -> e.routeId.equals(routeId)).findFirst()
                .orElseThrow(() -> new AssertionError("No route " + routeId + ": " + result.entryPoints.stream().map(e -> e.routeId).toList()));
    }
    private Set<String> reachable(String root) {
        Set<String> seen = new HashSet<>();
        ArrayDeque<String> queue = new ArrayDeque<>(); queue.add(root);
        while (!queue.isEmpty()) {
            String id = queue.remove(); if (!seen.add(id)) continue;
            result.edges.stream().filter(e -> e.from.equals(id) && e.kind.equals("call")).forEach(e -> queue.add(e.to));
        }
        return seen;
    }
    private void reaches(EntryPoint ep, String method) {
        assertTrue(reachable(ep.entryNode).stream().anyMatch(id -> id.startsWith(method + "(")), ep.routeId + " -> " + reachable(ep.entryNode));
    }

    @Test void webhookEndpointSettersOverrideUriAndPreserveRuntimeExpressions() {
        EntryPoint a = route("webhook-dispatch-{domain.name()}");
        assertEquals("message", a.kind);
        assertEquals("{domain.source().topic()}", a.topic);
        assertEquals("kafka:{domain.source().topic()}", a.target);
        assertEquals("{source.groupId()}.{domain.name()}", a.consumerGroup);
        assertFalse(a.topic.contains("webhook-dispatch"));
        assertEquals("fixture.WebhookDispatchRouteBuilder", a.owner);
        reaches(a, "fixture.WebhookDispatchProcessor.dispatch");
        reaches(a, "fixture.Business.persistWebhook");
        assertFalse(reachable(a.entryNode).stream().anyMatch(id -> id.startsWith("fixture.Business.fanout(")));
        Node node = result.nodes.stream().filter(n -> n.id.equals(a.entryNode)).findFirst().orElseThrow();
        assertEquals("src/main/java/fixture/WebhookDispatchRouteBuilder.java", node.filePath);
        assertEquals(12, node.line);
        assertTrue(result.kafkaAccesses.stream().anyMatch(k -> k.from.equals(a.entryNode) && k.topic.equals(a.topic) && k.groupId.equals(a.consumerGroup)));
    }

    @Test void helperArgumentsConnectConcreteProcessorNotEveryInterfaceImplementation() {
        EntryPoint b = route("fanout-{domain.name()}");
        assertEquals("{domain.source().topic()}", b.topic);
        assertEquals("{domain.source().groupId()}", b.consumerGroup);
        reaches(b, "fixture.OrganizationFanoutProcessor.process");
        reaches(b, "fixture.Business.persistFanout");
        assertFalse(reachable(b.entryNode).stream().anyMatch(id -> id.startsWith("fixture.WebhookDispatchProcessor.")));
    }

    @Test void literalConstantLambdaMethodReferenceAndProcessorObjectHaveIsolatedFlows() {
        assertEquals("literal", route("literal").topic);
        assertEquals("literal-group", route("literal").consumerGroup);
        assertEquals("constants", route("constant").topic);
        assertEquals("constant-group", route("constant").consumerGroup);
        reaches(route("literal"), "fixture.Business.persistLiteral");
        reaches(route("constant"), "fixture.Business.persistConstant");
        reaches(route("reference"), "fixture.Business.persistReference");
        reaches(route("object"), "fixture.Business.persistFanout");
        reaches(route("inherited"), "fixture.Business.persistFanout");
        assertFalse(reachable(route("literal").entryNode).contains(route("constant").entryNode));
    }

    @Test void localEndpointFactoryHelperCallbacksAndInheritedConfigure() {
        EntryPoint ep = route("local-endpoint");
        assertEquals("local-topic", ep.topic);
        assertEquals("local-group", ep.consumerGroup);
        reaches(ep, "fixture.Business.persistFanout");
        reaches(route("kafka:helper-reference"), "fixture.Business.persistReference");
        reaches(route("kafka:helper-lambda"), "fixture.Business.persistLiteral");
        reaches(route("inherited-configure"), "fixture.Business.persistConstant");
        EntryPoint noId = result.entryPoints.stream().filter(e -> e.topic.equals("no-id")).findFirst().orElseThrow();
        assertEquals("", noId.routeId); assertEquals("standalone", noId.consumerGroup);
        reaches(noId, "fixture.Business.persistReference");
        reaches(route("construction"), "fixture.Business.persistLiteral");
        reaches(route("construction"), "fixture.PerMessageProcessor.PerMessageProcessor");
    }

    @Test void runtimeMutationAndUnknownHandlerStayUnresolved() {
        EntryPoint ep = route("runtime-uri");
        assertEquals("{uri}", ep.target);
        assertEquals("camel", ep.kind);
        EntryPoint unknown = route("unresolved");
        assertEquals(1, reachable(unknown.entryNode).size());
        assertTrue(result.callSites.stream().anyMatch(c -> c.from.equals(unknown.entryNode) && c.unresolved.equals("unresolved")));
        assertTrue(result.entryPointWarnings.stream().anyMatch(w -> w.contains("unresolved") && w.contains("route unresolved")));
    }

    @Test void helperInvocationsDuplicateIdsAndInternalSourcesRemainDistinct() {
        List<EntryPoint> duplicates = result.entryPoints.stream().filter(e -> e.routeId.equals("duplicate")).toList();
        assertEquals(2, duplicates.size());
        assertNotEquals(duplicates.get(0).id, duplicates.get(1).id);
        assertEquals(Set.of("helper-one", "helper-two"), new HashSet<>(duplicates.stream().map(e -> e.topic).toList()));
        for (EntryPoint e : duplicates) reaches(e, "fixture.Business.persistFanout");
        assertTrue(result.entryPointWarnings.stream().anyMatch(w -> w.contains("Duplicate Camel route")));
        assertFalse(result.entryPoints.stream().anyMatch(e -> e.target.startsWith("direct:") || e.target.startsWith("seda:")));
        reaches(route("sender"), "fixture.Business.persistInternal");
        assertTrue(result.kafkaAccesses.stream().anyMatch(k -> k.from.equals(route("producer").entryNode) && k.role.equals("produce") && k.topic.equals("outbound")));
    }

    @Test void httpAndNonCamelCallsAreUnaffectedAndRegistrationIsNotHandlerExecution() {
        assertEquals(1, result.endpoints.size()); assertEquals("/http", result.endpoints.get(0).path);
        assertFalse(result.entryPoints.stream().anyMatch(e -> e.topic.contains("fake")));
        for (Node node : result.nodes) if (node.label.equals("fixture.SimpleRoutes.configure")
                || node.label.equals("fixture.AdditionalRoutes.configure")
                || node.label.equals("fixture.WebhookDispatchRouteBuilder.configure")) {
            Set<String> calls = reachable(node.id);
            assertFalse(calls.stream().anyMatch(id -> id.startsWith("fixture.Business.")), calls.toString());
            assertFalse(calls.stream().anyMatch(id -> id.startsWith("fixture.WebhookDispatchProcessor.dispatch(")));
        }
    }

    @Test void routeIdentityIsDeterministicAcrossParserParallelism() throws Exception {
        AnalysisResult next = new ProjectAnalyzer(Path.of("src/test/resources/camel-project"), 1).analyze();
        assertEquals(result.entryPoints.stream().map(e -> e.id).toList(), next.entryPoints.stream().map(e -> e.id).toList());
    }

    @Test void sourceLineMovementDoesNotChangeIdentity(@org.junit.jupiter.api.io.TempDir Path copy) throws Exception {
        Path fixture = Path.of("src/test/resources/camel-project");
        try (var paths = Files.walk(fixture)) {
            for (Path file : paths.filter(Files::isRegularFile).toList()) {
                Path target = copy.resolve(fixture.relativize(file));
                Files.createDirectories(target.getParent());
                if (file.toString().endsWith(".java")) Files.writeString(target, "\n\n\n" + Files.readString(file));
                else Files.copy(file, target);
            }
        }
        AnalysisResult shifted = new ProjectAnalyzer(copy).analyze();
        assertEquals(result.entryPoints.stream().map(e -> e.id).toList(), shifted.entryPoints.stream().map(e -> e.id).toList());
        EntryPoint a = route("webhook-dispatch-{domain.name()}");
        assertEquals(15, shifted.nodes.stream().filter(n -> n.id.equals(a.entryNode)).findFirst().orElseThrow().line);
    }
}
