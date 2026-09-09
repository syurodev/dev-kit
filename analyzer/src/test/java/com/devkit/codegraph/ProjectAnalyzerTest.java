package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
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
    void keepsTheCallEdgeWhenAnArgumentIsARecordAccessor() throws IOException {
        // The bug this pins: full resolution needs every argument's type,
        // because that is how an overload is chosen, so one argument the solver
        // cannot follow used to take the whole call with it. Records are that
        // argument — JavaParser does not resolve their implicit accessors — and
        // a Spring project passes records into services constantly, so a
        // controller would sit alone in its own graph with no edges at all.
        //
        // The receiver resolves perfectly well on its own, and that is enough
        // to name the callee.
        Path projectRoot = Path.of("src/test/resources/record-project");
        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        boolean reachesTheService = result.edges.stream().anyMatch(e ->
                e.from.startsWith("com.acme.tx.TransactionController.getTransactions")
                        && e.to.startsWith("com.acme.tx.TransactionQueryService.queryPaged"));
        assertTrue(reachesTheService, "controller should reach the service: " + result.edges);

        // And the record itself is in the index. It is a TypeDeclaration but
        // not a ClassOrInterfaceDeclaration, so the old walk never saw it and
        // every request, response and DTO in a project was missing.
        boolean recordIndexed = result.nodes.stream()
                .anyMatch(n -> n.kind.equals("class") && n.label.equals("com.acme.tx.TransactionQuery"));
        assertTrue(recordIndexed, "the record should be a node: " + result.nodes);
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

    @Test
    void extractsFieldReadsAndWrites(@TempDir Path tempDir) throws IOException {
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/data");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("Counter.java"), """
                package com.acme.data;

                public class Counter {
                    private int count;
                    private String label;

                    public void increment() {
                        count++;
                    }

                    public void reset(String newLabel) {
                        this.count = 0;
                        label = newLabel;
                    }

                    public int getCount() {
                        return count;
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        assertEquals(4, result.fieldAccesses.size(), "field accesses size = " + result.fieldAccesses.size());

        FieldAccess inc = result.fieldAccesses.stream()
                .filter(fa -> fa.fieldName.equals("count") && fa.nodeId.contains("increment"))
                .findFirst().orElseThrow();
        assertEquals("com.acme.data.Counter", inc.classId);
        assertEquals("write", inc.kind);

        FieldAccess resetCount = result.fieldAccesses.stream()
                .filter(fa -> fa.fieldName.equals("count") && fa.nodeId.contains("reset"))
                .findFirst().orElseThrow();
        assertEquals("com.acme.data.Counter", resetCount.classId);
        assertEquals("write", resetCount.kind);

        FieldAccess resetLabel = result.fieldAccesses.stream()
                .filter(fa -> fa.fieldName.equals("label") && fa.nodeId.contains("reset"))
                .findFirst().orElseThrow();
        assertEquals("com.acme.data.Counter", resetLabel.classId);
        assertEquals("write", resetLabel.kind);

        FieldAccess getCount = result.fieldAccesses.stream()
                .filter(fa -> fa.fieldName.equals("count") && fa.nodeId.contains("getCount"))
                .findFirst().orElseThrow();
        assertEquals("com.acme.data.Counter", getCount.classId);
        assertEquals("read", getCount.kind);
    }

    @Test
    void everyEdgeReferencesAnExistingNode(@TempDir Path tempDir) throws IOException {
        // Regression test: calling into a JDK/library method (resolvable via
        // ReflectionTypeSolver, so call.resolve() succeeds) must not produce
        // an edge pointing at a node that was never created — the Go side's
        // AnalysisResult.Validate rejects the whole index if any edge does.
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/jdkcall");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("DurationChecker.java"), """
                package com.acme.jdkcall;
                import java.time.Duration;
                public class DurationChecker {
                    public boolean isNegative(Duration d) {
                        return d.isNegative();
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        Set<String> nodeIds = result.nodes.stream().map(n -> n.id).collect(Collectors.toSet());
        for (Edge e : result.edges) {
            assertTrue(nodeIds.contains(e.from), "edge.from references unknown node: " + e.from);
            assertTrue(nodeIds.contains(e.to), "edge.to references unknown node: " + e.to + " (edges: " + result.edges + ")");
        }
    }

    @Test
    void parsesRecordsAndPatternMatchingSwitch(@TempDir Path tempDir) throws IOException {
        // Regression test: without an explicit LanguageLevel, JavaParser
        // defaults to a level too old for these Java 14+ constructs — the
        // whole file silently fails to parse and vanishes from the index.
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/modern");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("Shape.java"), """
                package com.acme.modern;
                public class Shape {
                    public record Circle(double radius) {}
                    public record Square(double side) {}

                    public double area(Object shape) {
                        if (shape instanceof Circle c) {
                            return Math.PI * c.radius() * c.radius();
                        }
                        return switch (shape) {
                            case Square s -> s.side() * s.side();
                            default -> 0.0;
                        };
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        boolean hasClassNode = result.nodes.stream().anyMatch(n -> n.label.equals("com.acme.modern.Shape"));
        assertTrue(hasClassNode, "class node should exist — file uses records/pattern-matching, must still parse: " + result.nodes);
    }

    @Test
    void parsesSyntaxNewerThanTheAnalyzerOwnBytecode(@TempDir Path tempDir) throws IOException {
        // The language level and maven.compiler.release are different knobs.
        // This file uses an unnamed variable in a catch clause (Java 22) and an
        // unnamed pattern variable (Java 21+), neither of which the analyzer's
        // own release of 21 can compile — but JavaParser is a parser, not
        // javac, so it reads them fine at LanguageLevel.JAVA_25.
        //
        // The failure this guards against is silent: a file the parser rejects
        // is swallowed by parseFile's catch-all and simply vanishes from the
        // index, with no node, no edge and no error anywhere.
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/newer");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("Unnamed.java"), """
                package com.acme.newer;
                public class Unnamed {
                    public int count(java.util.List<String> items) {
                        int total = 0;
                        for (String _ : items) {
                            total++;
                        }
                        try {
                            total += items.size();
                        } catch (RuntimeException _) {
                            total = 0;
                        }
                        return total;
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        boolean hasClassNode = result.nodes.stream().anyMatch(n -> n.label.equals("com.acme.newer.Unnamed"));
        assertTrue(hasClassNode, "a file using Java 22 syntax must still be indexed: " + result.nodes);
    }

    @Test
    void analyzesEveryServiceInAMonorepoAndResolvesAcrossThem(@TempDir Path tempDir) throws IOException {
        // Opening a folder of microservices used to produce an empty index in
        // silence: the analyzer looked only at <root>/src/main/java, which a
        // monorepo does not have, so nothing was found and the UI reported an
        // empty project rather than a misread one.
        //
        // Cross-service resolution is the reason all roots go into one run
        // rather than one subprocess per service: orders calls into a shared
        // module, and that edge only exists if both are solved together.
        Path root = tempDir.resolve("monorepo");
        writeJava(root, "services/orders/src/main/java/com/acme/orders/OrderService.java", """
                package com.acme.orders;
                import com.acme.shared.Ids;
                public class OrderService {
                    public String create() {
                        return Ids.next();
                    }
                }
                """);
        writeJava(root, "services/billing/src/main/java/com/acme/billing/BillingService.java", """
                package com.acme.billing;
                public class BillingService {
                    public int charge() { return 1; }
                }
                """);
        writeJava(root, "libs/shared/src/main/java/com/acme/shared/Ids.java", """
                package com.acme.shared;
                public class Ids {
                    public static String next() { return "id"; }
                }
                """);
        // Build output must not be mistaken for a source root, or every
        // generated copy would be indexed alongside the real thing.
        writeJava(root, "services/orders/target/generated-sources/src/main/java/com/acme/orders/Ghost.java", """
                package com.acme.orders;
                public class Ghost {}
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        Set<String> labels = result.nodes.stream().map(n -> n.label).collect(Collectors.toSet());
        assertTrue(labels.contains("com.acme.orders.OrderService"), "orders service missing: " + labels);
        assertTrue(labels.contains("com.acme.billing.BillingService"), "billing service missing: " + labels);
        assertTrue(labels.contains("com.acme.shared.Ids"), "shared module missing: " + labels);
        assertFalse(labels.contains("com.acme.orders.Ghost"), "build output must not be indexed: " + labels);

        boolean crossService = result.edges.stream().anyMatch(e ->
                e.from.contains("com.acme.orders.OrderService.create") && e.to.contains("com.acme.shared.Ids.next"));
        assertTrue(crossService, "a call from one service into a shared module must resolve: " + result.edges);
    }

    @Test
    void analyzesAPlainFolderOfJavaFilesWithNoBuildLayout(@TempDir Path tempDir) throws IOException {
        // No Maven or Gradle layout at all. Still something a person wants to
        // read, so the folder itself is the source root.
        Path root = tempDir.resolve("loose");
        writeJava(root, "Standalone.java", """
                public class Standalone {
                    public int value() { return 7; }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertTrue(result.nodes.stream().anyMatch(n -> n.label.equals("Standalone")),
                "a bare folder of .java files must still be indexed: " + result.nodes);
    }

    private static void writeJava(Path root, String relativePath, String source) throws IOException {
        Path file = root.resolve(relativePath);
        Files.createDirectories(file.getParent());
        Files.writeString(file, source);
    }

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
    void recordsUnresolvedCallsInsteadOfDroppingThem(@TempDir Path tempDir) throws IOException {
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/walls");
        Files.createDirectories(pkg);
        // Two different walls in one method. String is a class the solver
        // resolves through the JDK but never turns into a node -> "library".
        // List is resolved the same way but is an interface, and the trace
        // reads very differently for the two: a library method has no body we
        // can ever read, an interface method has one somewhere we cannot find.
        Files.writeString(pkg.resolve("Walls.java"), """
                package com.acme.walls;

                import java.util.List;

                public class Walls {
                    public String touchWalls(String raw, List<String> items) {
                        items.add(raw);
                        return raw.trim();
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        Map<String, Long> reasons = result.callSites.stream()
                .filter(c -> !c.unresolved.isEmpty())
                .collect(Collectors.groupingBy(c -> c.unresolved, Collectors.counting()));

        assertEquals(1L, reasons.getOrDefault("library", 0L), "String.trim should be recorded as a library wall");
        assertEquals(1L, reasons.getOrDefault("interface", 0L), "List.add should be recorded as an interface wall");

        for (CallSite c : result.callSites) {
            if (!c.unresolved.isEmpty()) {
                assertTrue(c.to.isEmpty(), "an unresolved call site must not name a callee");
                assertTrue(c.line > 0, "even an unresolved call site must carry its line");
            }
        }
        // The point of the whole task: the old code hit `continue` here and both
        // calls vanished, leaving a trace unable to say why it stopped.
        assertEquals(0, result.edges.size(), "neither wall should produce an edge");
    }

    @Test
    void callSitesAreNotDedupedTheWayEdgesAre(@TempDir Path tempDir) throws IOException {
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/twice");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("Callee.java"), """
                package com.acme.twice;

                public class Callee {
                    public void work() {}
                }
                """);
        Files.writeString(pkg.resolve("Caller.java"), """
                package com.acme.twice;

                public class Caller {
                    private final Callee callee = new Callee();

                    public void run() {
                        callee.work();
                        callee.work();
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        List<Edge> callEdges = result.edges.stream()
                .filter(e -> e.to.contains("Callee.work"))
                .toList();
        List<CallSite> sites = result.callSites.stream()
                .filter(c -> c.to.contains("Callee.work"))
                .toList();

        assertEquals(1, callEdges.size(), "edges collapse the repeated call, as they always have");
        assertEquals(2, sites.size(), "call sites must keep both, or the second line is lost");
        assertNotEquals(sites.get(0).line, sites.get(1).line, "the two call sites must be on different lines");
    }

    private static Path sampleProjectRoot() {
        return Path.of("src/test/resources/sample-project");
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
