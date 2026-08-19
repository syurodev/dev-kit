package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Set;
import java.util.stream.Collectors;

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
    void unresolvedParameterTypeDoesNotAbortAnalysisOfRestOfFile(@TempDir Path tempDir) throws IOException {
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/missingparam");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("ControllerWithMissingParam.java"), """
                package com.acme.missingparam;
                public class ControllerWithMissingParam {
                    public void process(com.totally.unknown.MissingDto dto) {
                        System.out.println("missing param type");
                    }
                    public void validMethod(String input) {
                        System.out.println("valid method: " + input);
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        boolean hasClassNode = result.nodes.stream().anyMatch(n -> n.label.equals("com.acme.missingparam.ControllerWithMissingParam"));
        assertTrue(hasClassNode, "class node should still exist despite unresolved parameter type in one method");
        boolean hasValidMethod = result.nodes.stream().anyMatch(n -> n.label.equals("com.acme.missingparam.ControllerWithMissingParam.validMethod"));
        assertTrue(hasValidMethod, "validMethod() should still be indexed even though process() has an unresolved parameter type");
    }

    @Test
    void duplicateMethodCallsDoNotProduceDuplicateEdges(@TempDir Path tempDir) throws IOException {
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/dup");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("Caller.java"), """
                package com.acme.dup;
                public class Caller {
                    private Callee callee;
                    public void doWork() {
                        callee.help();
                        callee.help();
                    }
                }
                """);
        Files.writeString(pkg.resolve("Callee.java"), """
                package com.acme.dup;
                public class Callee {
                    public void help() {}
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();
        long callEdgeCount = result.edges.stream()
                .filter(e -> labelOf(result, e.from).equals("com.acme.dup.Caller.doWork")
                        && labelOf(result, e.to).equals("com.acme.dup.Callee.help"))
                .count();
        assertEquals(1, callEdgeCount, "expected duplicate calls to be deduped to 1 edge");
    }

    @Test
    void nonExistentProjectPathThrowsException() {
        Path nonExistent = Path.of("/tmp/non-existent-path-for-test-xyz-987654");
        assertThrows(IllegalArgumentException.class, () -> new ProjectAnalyzer(nonExistent).analyze());
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

    @Test
    void derivesFileDependenciesFromCrossFileCalls() throws IOException {
        Path projectRoot = Path.of("src/test/resources/sample-project");
        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        Set<String> deps = result.fileDependencies.stream()
                .map(fd -> fd.filePath + " -> " + fd.dependsOnFilePath)
                .collect(Collectors.toSet());
        Set<String> want = Set.of(
                "src/main/java/com/acme/refund/RefundController.java -> src/main/java/com/acme/refund/RefundService.java",
                "src/main/java/com/acme/refund/RefundService.java -> src/main/java/com/acme/refund/RefundRepository.java");
        assertEquals(want, deps, "file dependencies = %s".formatted(deps));
    }

    @Test
    void extractsJavadocSummaryAndMethodParameters(@TempDir Path tempDir) throws IOException {
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/doc");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("OrderService.java"), """
                package com.acme.doc;

                import java.util.List;

                /**
                 * Manages orders in the system.
                 *
                 * @author Alice
                 */
                public class OrderService {

                    /**
                     * Processes a refund request.
                     *
                     * @param orderId the ID of the order
                     * @param items list of item keys
                     * @return status
                     */
                    public boolean processRefund(String orderId, List<String> items) {
                        return true;
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        Node classNode = result.nodes.stream()
                .filter(n -> n.label.equals("com.acme.doc.OrderService"))
                .findFirst().orElseThrow(() -> new AssertionError("OrderService class not found"));
        assertTrue(classNode.summary.contains("Manages orders in the system."), "class summary = " + classNode.summary);
        assertTrue(classNode.params.isEmpty(), "class params must be empty");

        Node methodNode = result.nodes.stream()
                .filter(n -> n.label.equals("com.acme.doc.OrderService.processRefund"))
                .findFirst().orElseThrow(() -> new AssertionError("processRefund method not found"));
        assertTrue(methodNode.summary.contains("Processes a refund request."), "method summary = " + methodNode.summary);
        assertEquals(2, methodNode.params.size());
        assertEquals("orderId", methodNode.params.get(0).name);
        assertEquals("String", methodNode.params.get(0).type);
        assertEquals("items", methodNode.params.get(1).name);
        assertEquals("List<String>", methodNode.params.get(1).type);
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
