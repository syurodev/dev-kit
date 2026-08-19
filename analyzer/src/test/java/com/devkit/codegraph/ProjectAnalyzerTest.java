package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

import static org.junit.jupiter.api.Assertions.*;

class ProjectAnalyzerTest {

    @Test
    void findsClassAndMethodNodesInSampleProject() throws IOException {
        Path projectRoot = Path.of("src/test/resources/sample-project");
        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        List<Node> classNodes = result.nodes.stream().filter(n -> n.kind.equals("class")).toList();
        assertEquals(3, classNodes.size(), "expected RefundController, RefundService, RefundRepository");

        Node service = classNodes.stream()
                .filter(n -> n.label.equals("com.acme.refund.RefundService"))
                .findFirst().orElseThrow(() -> new AssertionError("RefundService class node not found: " + result.nodes));
        assertEquals("unknown", service.layer, "no Spring annotations in this fixture yet — layer detection is tested separately in LayerClassifierTest");
        assertTrue(service.filePath.endsWith("RefundService.java"), "filePath = " + service.filePath);
        assertTrue(service.line > 0 && service.endLine >= service.line, "line=%d endLine=%d".formatted(service.line, service.endLine));

        List<Node> methodNodes = result.nodes.stream().filter(n -> n.kind.equals("method")).toList();
        boolean hasProcessMethod = methodNodes.stream()
                .anyMatch(n -> n.label.equals("com.acme.refund.RefundService.process"));
        assertTrue(hasProcessMethod, "expected a method node for RefundService.process: " + methodNodes);
    }

    @Test
    void findsCallEdgeAcrossControllerToServiceToRepository() throws IOException {
        Path projectRoot = Path.of("src/test/resources/sample-project");
        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        String controllerCreate = "com.acme.refund.RefundController.create";
        String serviceProcess = "com.acme.refund.RefundService.process";
        String repoSave = "com.acme.refund.RefundRepository.save";

        assertTrue(hasCallEdge(result, controllerCreate, serviceProcess),
                "expected call edge RefundController.create -> RefundService.process, got: " + result.edges);
        assertTrue(hasCallEdge(result, serviceProcess, repoSave),
                "expected call edge RefundService.process -> RefundRepository.save, got: " + result.edges);
    }

    @Test
    void unresolvedCallDoesNotAbortAnalysisOfRestOfFile(@TempDir Path tempDir) throws IOException {
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/broken");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("Broken.java"), """
                package com.acme.broken;
                public class Broken {
                    public void a() {
                        new com.totally.unknown.NotOnClasspath().doSomething();
                    }
                    public void b() {
                        System.out.println("still parsed fine");
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        boolean hasClassNode = result.nodes.stream().anyMatch(n -> n.label.equals("com.acme.broken.Broken"));
        assertTrue(hasClassNode, "class node should still exist despite an unresolvable call inside one of its methods");
        boolean hasMethodB = result.nodes.stream().anyMatch(n -> n.label.equals("com.acme.broken.Broken.b"));
        assertTrue(hasMethodB, "method b() should still be indexed even though method a() has an unresolvable call");
    }

    @Test
    void detectsEndpointFromControllerAnnotations() throws IOException {
        Path projectRoot = Path.of("src/test/resources/sample-project");
        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        assertEquals(1, result.endpoints.size(), "expected exactly the RefundController.create endpoint: " + result.endpoints);
        Endpoint endpoint = result.endpoints.get(0);
        assertEquals("POST", endpoint.httpMethod);
        assertEquals("/refunds", endpoint.path);
        assertEquals("POST /refunds", endpoint.id);
    }

    private boolean hasCallEdge(AnalysisResult result, String fromLabel, String toLabel) {
        return result.edges.stream().anyMatch(e ->
                e.kind.equals("call")
                        && labelOf(result, e.from).equals(fromLabel)
                        && labelOf(result, e.to).equals(toLabel));
    }

    private String labelOf(AnalysisResult result, String nodeId) {
        return result.nodes.stream().filter(n -> n.id.equals(nodeId)).findFirst()
                .map(n -> n.label).orElse("<missing:" + nodeId + ">");
    }
}
