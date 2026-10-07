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
    void keepsMethodsAndEndpointsWithMissingLibraryParameters(@TempDir Path root) throws IOException {
        Files.createDirectories(root.resolve("example"));
        Files.writeString(root.resolve("example/Controller.java"), """
                package example;
                import org.springframework.security.oauth2.jwt.Jwt;
                import jakarta.servlet.http.HttpServletRequest;
                import org.springframework.web.bind.annotation.PostMapping;
                class Controller {
                    @PostMapping("/push") void push(Jwt jwt, String id) { Helper.run(); }
                    void request(HttpServletRequest request) {}
                    void request(String request) {}
                }
                """);
        Files.writeString(root.resolve("example/Helper.java"), "package example; class Helper { static void run() {} }");
        AnalysisResult result = new ProjectAnalyzer(root).analyze();
        assertTrue(result.nodes.stream().anyMatch(n -> n.id.equals(
                "example.Controller.push(org.springframework.security.oauth2.jwt.Jwt, java.lang.String)")));
        assertEquals(3, result.nodes.stream().filter(n -> n.label.startsWith("example.Controller.")).count());
        assertEquals(1, result.endpoints.size());
        assertTrue(result.edges.stream().anyMatch(e -> e.to.equals("example.Helper.run()")));
    }

    @Test
    void edgesCarryTheConditionTheCallRunsUnder(@TempDir Path root) throws IOException {
        Files.createDirectories(root.resolve("example"));
        Files.writeString(root.resolve("example/AccessService.java"), """
                package example;
                class AccessService {
                    private boolean moduleBased;
                    boolean isAvailable(String org) {
                        if (moduleBased) {
                            return byModule(org);
                        }
                        audit(org);
                        return byRole(org);
                    }
                    boolean byModule(String org) { return true; }
                    boolean byRole(String org) { return true; }
                    void audit(String org) {}
                    void twice(String org) {
                        if (moduleBased) { audit(org); }
                        if (!moduleBased) { audit(org); }
                    }
                    void always(String org) {
                        audit(org);
                        if (moduleBased) { audit(org); }
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        String isAvailable = "example.AccessService.isAvailable(java.lang.String)";
        assertEquals("moduleBased", guardOf(result, isAvailable, "example.AccessService.byModule(java.lang.String)"));
        // The guard clause: byRole runs only past the `return`, audit too.
        assertEquals("!moduleBased", guardOf(result, isAvailable, "example.AccessService.byRole(java.lang.String)"));
        assertEquals("!moduleBased", guardOf(result, isAvailable, "example.AccessService.audit(java.lang.String)"));
        // One edge for two calls: it runs under either condition.
        assertEquals("moduleBased || !moduleBased",
                guardOf(result, "example.AccessService.twice(java.lang.String)", "example.AccessService.audit(java.lang.String)"));
        // One unconditional call makes the edge unconditional.
        assertEquals("", guardOf(result, "example.AccessService.always(java.lang.String)", "example.AccessService.audit(java.lang.String)"));
    }

    private static String guardOf(AnalysisResult result, String from, String to) {
        List<Edge> matching = result.edges.stream().filter(e -> e.from.equals(from) && e.to.equals(to)).toList();
        assertEquals(1, matching.size(), "edges " + from + " -> " + to + ": " + matching.size());
        return matching.get(0).guard;
    }

    /** A project of enough files, in two modules sharing a class name, that threads finish in a different order than they started. */
    private static void writeManyFiles(Path root) throws IOException {
        for (String module : List.of("billing", "orders")) {
            Path dir = root.resolve(module + "/src/main/java/acme/" + module);
            Files.createDirectories(dir);
            for (int i = 0; i < 40; i++) {
                int next = (i + 1) % 40;
                Files.writeString(dir.resolve("C" + i + ".java"), """
                        package acme.%1$s;
                        class C%2$d {
                            private final C%3$d next = new C%3$d();
                            boolean flag;
                            String run(String s) {
                                if (flag) { return next.run(s); }
                                return s.trim();
                            }
                        }
                        """.formatted(module, i, next));
            }
            // The same class name in both modules: which one keeps the plain
            // id depends on the order files are handed to the passes.
            Files.writeString(dir.resolve("Shared.java"), "package acme." + module + "; class Shared { void go() {} }");
        }
    }

    private static String indexOf(Path root, int parseThreads) throws IOException {
        return new com.fasterxml.jackson.databind.ObjectMapper()
                .writeValueAsString(new ProjectAnalyzer(root, parseThreads).analyze());
    }

    @Test
    void parsingOnSeveralThreadsGivesTheSameIndexAsOneThread(@TempDir Path root) throws IOException {
        writeManyFiles(root);
        String sequential = indexOf(root, 1);
        assertEquals(sequential, indexOf(root, 1), "the sequential index must itself be stable");
        for (int threads : List.of(2, 4, 8)) {
            assertEquals(sequential, indexOf(root, threads), threads + " parse threads changed the index");
        }
        assertTrue(sequential.contains("acme.billing.C0"), "the project was actually indexed");
    }

    @Test
    void workerThreadsParseWithTheNewestLanguageLevel(@TempDir Path root) throws IOException {
        // A record is Java 16: the parser's default level cannot read it, and a
        // worker that fell back to the default would drop the file silently.
        Files.createDirectories(root.resolve("example"));
        for (int i = 0; i < 12; i++) {
            Files.writeString(root.resolve("example/R" + i + ".java"),
                    "package example; record R" + i + "(String a) { String twice() { return a + a; } }");
        }
        for (int threads : List.of(1, 4)) {
            AnalysisResult result = new ProjectAnalyzer(root, threads).analyze();
            long records = result.nodes.stream().filter(n -> n.kind.equals("class") && n.label.startsWith("example.R")).count();
            assertEquals(12, records, "with " + threads + " parse thread(s)");
        }
    }

    @Test
    void aFileThatDoesNotParseIsSkippedWhateverTheThreadCount(@TempDir Path root) throws IOException {
        Files.createDirectories(root.resolve("example"));
        for (int i = 0; i < 10; i++) {
            Files.writeString(root.resolve("example/Ok" + i + ".java"), "package example; class Ok" + i + " { void run() {} }");
        }
        Files.writeString(root.resolve("example/Broken.java"), "package example; class Broken { void run( {");
        for (int threads : List.of(1, 4)) {
            AnalysisResult result = new ProjectAnalyzer(root, threads).analyze();
            assertEquals(10, result.nodes.stream().filter(n -> n.kind.equals("class")).count(), "with " + threads + " parse thread(s)");
        }
    }

    @Test
    void recordsOutboundCallsAgainstTheMethodMakingThem(@TempDir Path root) throws IOException {
        Files.createDirectories(root.resolve("example"));
        Files.writeString(root.resolve("example/BillingClient.java"), """
                package example;
                import org.springframework.cloud.openfeign.FeignClient;
                import org.springframework.web.bind.annotation.GetMapping;
                @FeignClient(name = "billing-service")
                interface BillingClient {
                    @GetMapping("/invoices")
                    void list();
                }
                """);
        Files.writeString(root.resolve("example/RefundService.java"), """
                package example;
                import org.springframework.web.client.RestTemplate;
                class RefundService {
                    private final RestTemplate rest = new RestTemplate();
                    void notifyLedger(String id) {
                        rest.postForObject("http://ledger-service/entries/" + id, null, Void.class);
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertEquals(2, result.outboundCalls.size(), "outbound calls: " + result.outboundCalls.size());
        OutboundCall feign = result.outboundCalls.stream().filter(c -> c.client.equals("feign")).findFirst().orElseThrow();
        assertEquals("example.BillingClient.list()", feign.from);
        assertEquals("billing-service", feign.serviceName);
        assertTrue(feign.file.endsWith("BillingClient.java"), feign.file);
        OutboundCall rest = result.outboundCalls.stream().filter(c -> c.client.equals("rest-template")).findFirst().orElseThrow();
        assertEquals("example.RefundService.notifyLedger(java.lang.String)", rest.from);
        assertEquals("http://ledger-service/entries/{id}", rest.url);
        assertEquals(6, rest.line);
    }

    @Test
    void feignClientDeclaringTheSameRouteAsItsControllerIsNotADuplicateEndpoint(@TempDir Path root) throws IOException {
        Files.createDirectories(root.resolve("example"));
        Files.writeString(root.resolve("example/ProductsController.java"), """
                package example;
                import org.springframework.web.bind.annotation.RestController;
                import org.springframework.web.bind.annotation.GetMapping;
                @RestController
                class ProductsController {
                    @GetMapping("/v1/internal/tenants/{orgId}/products")
                    void list() {}
                }
                """);
        Files.writeString(root.resolve("example/ProductsClient.java"), """
                package example;
                import org.springframework.cloud.openfeign.FeignClient;
                import org.springframework.web.bind.annotation.GetMapping;
                @FeignClient(name = "products-service")
                interface ProductsClient {
                    @GetMapping("/v1/internal/tenants/{orgId}/products")
                    void list();
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertEquals(1, result.endpoints.size(),
                "the Feign client's declared route must not be counted as a second endpoint: " + result.endpoints);
        assertEquals("example.ProductsController.list()", result.endpoints.get(0).entryNode);
    }

    @Test
    void indexesTypeDependenciesWithoutCalls(@TempDir Path root) throws IOException {
        Files.writeString(root.resolve("Model.java"), "package model; public class Model {}");
        Files.writeString(root.resolve("Consumer.java"), "package consumer; import model.Model; class Consumer { Model value; }");
        AnalysisResult result = new ProjectAnalyzer(root).analyze();
        assertTrue(result.fileDependencies.stream().anyMatch(d ->
                d.filePath.equals("Consumer.java") && d.dependsOnFilePath.equals("Model.java")));
    }

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
    void methodReferencesAreCallsToTheMethodTheyName(@TempDir Path tempDir) throws IOException {
        // The shape that was invisible: `.map(Controller::toResponse)` runs
        // toResponse for every element, but it is not a MethodCallExpr, so
        // there was no edge — the flow stopped short of it, and "find
        // references" on toResponse answered "nowhere".
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/ref");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("Controller.java"), """
                package com.acme.ref;
                import java.util.List;
                import java.util.function.Supplier;
                public class Controller {
                    private final Mapper mapper = new Mapper();
                    public List<String> list(List<Integer> ids) {
                        return ids.stream()
                                .map(Controller::toResponse)
                                .map(mapper::decorate)
                                .map(this::trim)
                                .map(String::valueOf)
                                .toList();
                    }
                    public Supplier<Mapper> factory() {
                        return Mapper::new;
                    }
                    private static String toResponse(Integer id) { return "#" + id; }
                    private String trim(String s) { return s.trim(); }
                }
                """);
        Files.writeString(pkg.resolve("Mapper.java"), """
                package com.acme.ref;
                public class Mapper {
                    public Mapper() {}
                    public String decorate(String s) { return "[" + s + "]"; }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        String list = "com.acme.ref.Controller.list";
        assertTrue(hasCallEdge(result, list, "com.acme.ref.Controller.toResponse"), "Type::staticMethod: " + result.edges);
        assertTrue(hasCallEdge(result, list, "com.acme.ref.Mapper.decorate"), "instance::method: " + result.edges);
        assertTrue(hasCallEdge(result, list, "com.acme.ref.Controller.trim"), "this::method: " + result.edges);
        assertTrue(hasCallEdge(result, "com.acme.ref.Controller.factory", "com.acme.ref.Mapper.Mapper"),
                "Type::new: " + result.edges);

        // The call site sits on the line the reference is written on — that is
        // what "find references" shows, and the line it jumps to.
        boolean siteOnItsOwnLine = result.callSites.stream().anyMatch(cs ->
                labelOf(result, cs.from).equals(list)
                        && labelOf(result, cs.to).equals("com.acme.ref.Controller.toResponse")
                        && cs.line == 8);
        assertTrue(siteOnItsOwnLine, "expected the toResponse site on line 8: " + result.callSites);

        // String::valueOf resolves, to a library method. A CALL to one records
        // a "library" wall; a reference to one records nothing, or every
        // stream pipeline in a project would be a row of walls. So list() has
        // a wall for each of its six library calls — stream, four maps, toList
        // — and not a seventh.
        long wallsInList = result.callSites.stream()
                .filter(cs -> labelOf(result, cs.from).equals(list) && cs.to.isEmpty())
                .count();
        assertEquals(6, wallsInList, "the library reference must not add a wall");
    }

    @Test
    void anOverloadedMethodReferenceTheSolverCannotFollowIsLeftOut(@TempDir Path tempDir) throws IOException {
        // Inference by name needs exactly one method of that name. Two
        // overloads and no resolution means no way to choose, and a wrong edge
        // is worse than a missing one.
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/ref");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("Caller.java"), """
                package com.acme.ref;
                public class Caller {
                    public void run(com.totally.unknown.Registry registry) {
                        registry.register(Handlers::handle);
                    }
                }
                """);
        Files.writeString(pkg.resolve("Handlers.java"), """
                package com.acme.ref;
                public class Handlers {
                    static void handle(String s) {}
                    static void handle(Integer i) {}
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();
        boolean anyEdgeToHandle = result.edges.stream()
                .anyMatch(e -> labelOf(result, e.to).equals("com.acme.ref.Handlers.handle"));
        assertTrue(!anyEdgeToHandle, "ambiguous reference must not guess: " + result.edges);
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

        // count++ reads the old value as well as writing a new one.
        assertEquals(5, result.fieldAccesses.size(), "field accesses size = " + result.fieldAccesses.size());

        List<String> incKinds = result.fieldAccesses.stream()
                .filter(fa -> fa.fieldName.equals("count") && fa.nodeId.contains("increment"))
                .peek(fa -> assertEquals("com.acme.data.Counter", fa.classId))
                .map(fa -> fa.kind).sorted().toList();
        assertEquals(List.of("read", "write"), incKinds);

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

    // --- Unique ids ------------------------------------------------------------

    private static void assertIdsUniqueAndReferenced(AnalysisResult result) {
        Set<String> ids = new java.util.HashSet<>();
        for (Node n : result.nodes) {
            assertTrue(ids.add(n.id), "duplicate node id " + n.id);
        }
        for (Edge e : result.edges) {
            assertTrue(ids.contains(e.from) && ids.contains(e.to), "edge to unknown node: " + e.from + " -> " + e.to);
        }
        for (Endpoint ep : result.endpoints) {
            assertTrue(ids.contains(ep.entryNode), "endpoint to unknown node: " + ep.entryNode);
        }
        for (CallSite c : result.callSites) {
            assertTrue(ids.contains(c.from) && (c.to.isEmpty() || ids.contains(c.to)), "call site to unknown node: " + c.to);
        }
        Set<String> endpointIds = new java.util.HashSet<>();
        for (Endpoint ep : result.endpoints) {
            assertTrue(endpointIds.add(ep.id), "duplicate endpoint id " + ep.id);
        }
    }

    @Test
    void twoServicesDeclaringTheSameClassGetDistinctIds(@TempDir Path tempDir) throws IOException {
        // Every service in a folder of services has its own
        // com.acme.Application. Both became one node id, the store's primary
        // key refused the second, and nothing at all was indexed.
        Path root = tempDir.resolve("services");
        for (String service : List.of("billing", "orders")) {
            writeJava(root, service + "/src/main/java/com/acme/Application.java", """
                    package com.acme;
                    public class Application {
                        public static void main(String[] args) { new Application().start(); }
                        void start() { Health.check(); }
                    }
                    """);
            writeJava(root, service + "/src/main/java/com/acme/Health.java", """
                    package com.acme;
                    @RestController
                    public class Health {
                        @GetMapping("/health")
                        public static String check() { return "ok"; }
                    }
                    """);
        }

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertIdsUniqueAndReferenced(result);
        Set<String> classIds = result.nodes.stream().filter(n -> n.kind.equals("class"))
                .map(n -> n.id).collect(Collectors.toSet());
        assertEquals(Set.of("com.acme.Application", "com.acme.Application [orders]",
                "com.acme.Health", "com.acme.Health [orders]"), classIds);

        // Each service's call lands in its own copy, not the first one the
        // solver happened to see.
        assertTrue(result.edges.stream().anyMatch(e -> e.from.equals("com.acme.Application [orders].start()")
                && e.to.equals("com.acme.Health [orders].check()")), "orders must call its own Health: " + result.edges);
        assertTrue(result.edges.stream().anyMatch(e -> e.from.equals("com.acme.Application.start()")
                && e.to.equals("com.acme.Health.check()")), "billing must call its own Health: " + result.edges);

        // The same route served twice: the first keeps the plain id the UI
        // addresses it by, the second is told apart by its service.
        Set<String> endpointIds = result.endpoints.stream().map(ep -> ep.id).collect(Collectors.toSet());
        assertEquals(Set.of("GET /health", "GET /health [orders]"), endpointIds);
        Endpoint orders = result.endpoints.stream().filter(ep -> ep.id.endsWith("[orders]")).findFirst().orElseThrow();
        assertEquals("/health", orders.path);
        assertEquals("com.acme.Health [orders].check()", orders.entryNode);
    }

    @Test
    void localClassesOfOneNameInTwoMethodsGetDistinctIdsAndTheirOwnCalls(@TempDir Path tempDir) throws IOException {
        Path root = tempDir.resolve("project");
        writeJava(root, "src/main/java/com/acme/Outer.java", """
                package com.acme;
                public class Outer {
                    static void target() {}
                    void first() {
                        class Helper { void go() { target(); } }
                        new Helper().go();
                    }
                    void second() {
                        class Helper { void go() {} }
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertIdsUniqueAndReferenced(result);
        Set<String> classIds = result.nodes.stream().filter(n -> n.kind.equals("class"))
                .map(n -> n.id).collect(Collectors.toSet());
        assertEquals(Set.of("com.acme.Outer", "com.acme.Outer.first$Helper", "com.acme.Outer.second$Helper"), classIds);

        // target() is called inside Helper.go, and only there: counting it
        // against first() as well put it in the graph twice.
        List<CallSite> toTarget = result.callSites.stream().filter(c -> c.to.equals("com.acme.Outer.target()")).toList();
        assertEquals(1, toTarget.size(), "one call, one call site: " + toTarget.stream().map(c -> c.from).toList());
        assertEquals("com.acme.Outer.first$Helper.go()", toTarget.get(0).from);
        assertTrue(result.edges.stream().anyMatch(e -> e.from.equals("com.acme.Outer.first()")
                && e.to.equals("com.acme.Outer.first$Helper.go()")), "first() calls the local class: " + result.edges);
    }

    // --- Interfaces and inherited routes ------------------------------------------

    @Test
    void callsThroughAServiceInterfaceReachTheImplementation(@TempDir Path tempDir) throws IOException {
        Path root = tempDir.resolve("project");
        writeJava(root, "src/main/java/com/acme/OrderService.java", """
                package com.acme;
                public interface OrderService { String create(String id); }
                """);
        writeJava(root, "src/main/java/com/acme/OrderServiceImpl.java", """
                package com.acme;
                public class OrderServiceImpl implements OrderService {
                    public String create(String id) { return Ids.next(); }
                }
                """);
        writeJava(root, "src/main/java/com/acme/Ids.java", """
                package com.acme;
                public class Ids { public static String next() { return "x"; } }
                """);
        writeJava(root, "src/main/java/com/acme/Notifier.java", """
                package com.acme;
                public interface Notifier { void send(String message); }
                """);
        writeJava(root, "src/main/java/com/acme/OrderController.java", """
                package com.acme;
                public class OrderController {
                    private final OrderService orders;
                    private final Notifier notifier;
                    OrderController(OrderService orders, Notifier notifier) { this.orders = orders; this.notifier = notifier; }
                    String post(String id) {
                        notifier.send(id);
                        return orders.create(id);
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertIdsUniqueAndReferenced(result);
        assertTrue(hasCallEdge(result, "com.acme.OrderController.post", "com.acme.OrderService.create"), result.edges.toString());
        assertTrue(hasCallEdge(result, "com.acme.OrderService.create", "com.acme.OrderServiceImpl.create"),
                "the interface method must lead on to its implementation: " + result.edges);
        assertTrue(hasCallEdge(result, "com.acme.OrderServiceImpl.create", "com.acme.Ids.next"), result.edges.toString());

        // Notifier has no implementation anywhere in the project: the flow
        // cannot go on from it, and the call site says so.
        CallSite send = result.callSites.stream()
                .filter(c -> c.from.startsWith("com.acme.OrderController.post") && c.line == 7)
                .findFirst().orElseThrow(() -> new AssertionError(result.callSites.toString()));
        assertEquals("interface", send.unresolved);
        CallSite create = result.callSites.stream()
                .filter(c -> c.from.startsWith("com.acme.OrderController.post") && c.line == 8)
                .findFirst().orElseThrow();
        assertEquals("", create.unresolved, "an interface with an implementation is not a wall");
    }

    @Test
    void routesDeclaredOnAnImplementedInterfaceEnterAtTheController(@TempDir Path tempDir) throws IOException {
        // The shape an OpenAPI generator produces: the routes live on an
        // interface, the controller only implements it.
        Path root = tempDir.resolve("project");
        writeJava(root, "src/main/java/com/acme/api/UsersApi.java", """
                package com.acme.api;
                @RequestMapping("/v1")
                public interface UsersApi {
                    @GetMapping(Paths.USERS + "/{id}")
                    String getUser(String id);
                    @PostMapping(value = {Paths.USERS, "/people"})
                    String createUser(String body);
                }
                """);
        writeJava(root, "src/main/java/com/acme/api/Paths.java", """
                package com.acme.api;
                public final class Paths {
                    public static final String ROOT = "/users";
                    public static final String USERS = ROOT;
                }
                """);
        writeJava(root, "src/main/java/com/acme/web/UsersController.java", """
                package com.acme.web;
                import com.acme.api.UsersApi;
                @RestController
                public class UsersController implements UsersApi {
                    @Override public String getUser(String id) { return id; }
                    @Override public String createUser(String body) { return body; }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertIdsUniqueAndReferenced(result);
        Map<String, String> entryById = result.endpoints.stream()
                .collect(Collectors.toMap(ep -> ep.id, ep -> ep.entryNode));
        assertEquals(Map.of(
                "GET /v1/users/{id}", "com.acme.web.UsersController.getUser(java.lang.String)",
                "POST /v1/users", "com.acme.web.UsersController.createUser(java.lang.String)",
                "POST /v1/people", "com.acme.web.UsersController.createUser(java.lang.String)"), entryById);
    }

    @Test
    void aControllerInheritsItsBaseClassPrefixAndHandlers(@TempDir Path tempDir) throws IOException {
        Path root = tempDir.resolve("project");
        writeJava(root, "src/main/java/com/acme/CrudController.java", """
                package com.acme;
                @RequestMapping("/api")
                public abstract class CrudController {
                    @GetMapping("/{id}")
                    public String get(String id) { return id; }
                    @DeleteMapping("/{id}")
                    public void delete(String id) {}
                }
                """);
        writeJava(root, "src/main/java/com/acme/OrderController.java", """
                package com.acme;
                @RestController
                public class OrderController extends CrudController {
                    @GetMapping("/orders")
                    public String list() { return ""; }
                    @Override public void delete(String id) {}
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertIdsUniqueAndReferenced(result);
        Map<String, String> entryById = result.endpoints.stream()
                .collect(Collectors.toMap(ep -> ep.id, ep -> ep.entryNode));
        assertEquals(Map.of(
                "GET /api/orders", "com.acme.OrderController.list()",
                "DELETE /api/{id}", "com.acme.OrderController.delete(java.lang.String)",
                "GET /api/{id}", "com.acme.CrudController.get(java.lang.String)"), entryById);
    }

    @Test
    void customStereotypeAnnotationsAreSeenThrough(@TempDir Path tempDir) throws IOException {
        Path root = tempDir.resolve("project");
        writeJava(root, "src/main/java/com/acme/ApiController.java", """
                package com.acme;
                @RestController
                @RequestMapping("/api")
                public @interface ApiController {}
                """);
        writeJava(root, "src/main/java/com/acme/Ping.java", """
                package com.acme;
                @ApiController
                public class Ping {
                    @GetMapping("/ping") public String ping() { return "pong"; }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        Node ping = result.nodes.stream().filter(n -> n.id.equals("com.acme.Ping")).findFirst().orElseThrow();
        assertEquals("controller", ping.layer);
        assertEquals(List.of("GET /api/ping"), result.endpoints.stream().map(ep -> ep.id).toList());
    }

    // --- Walking the tree -----------------------------------------------------------

    @Test
    void aFolderWithNoBuildLayoutStillSkipsBuildOutput(@TempDir Path tempDir) throws IOException {
        Path root = tempDir.resolve("loose");
        writeJava(root, "app/Real.java", "package app; public class Real {}");
        writeJava(root, "target/classes/app/Real.java", "package app; public class Real {}");
        writeJava(root, "node_modules/pkg/Vendored.java", "public class Vendored {}");
        writeJava(root, "build/generated/Generated.java", "public class Generated {}");

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertEquals(List.of("app.Real"), result.nodes.stream().map(n -> n.id).toList());
    }

    @Test
    void findsSourceRootsNestedDeeperThanSixLevels(@TempDir Path tempDir) throws IOException {
        Path root = tempDir.resolve("mono");
        writeJava(root, "a/b/c/d/e/f/g/service/src/main/java/com/acme/Deep.java", "package com.acme; public class Deep {}");

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertTrue(result.nodes.stream().anyMatch(n -> n.id.equals("com.acme.Deep")), result.nodes.toString());
    }

    // --- Constructors, initializers, field accesses, Lombok ----------------------------

    @Test
    void indexesConstructorsAndTheCallsThatRunThem(@TempDir Path tempDir) throws IOException {
        Path root = tempDir.resolve("project");
        writeJava(root, "src/main/java/com/acme/Clock.java", """
                package com.acme;
                public class Clock {
                    public Clock(String zone) {}
                    static long now() { return 0; }
                }
                """);
        writeJava(root, "src/main/java/com/acme/Scheduler.java", """
                package com.acme;
                public class Scheduler {
                    private final long started = Clock.now();
                    static { Clock.now(); }
                    public Scheduler() {}
                    public Scheduler(int n) { this(); }
                    Clock make() { return new Clock("UTC"); }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertIdsUniqueAndReferenced(result);
        Node ctor = result.nodes.stream().filter(n -> n.id.equals("com.acme.Clock.Clock(java.lang.String)"))
                .findFirst().orElseThrow(() -> new AssertionError("constructor node missing: " + result.nodes));
        assertEquals("method", ctor.kind);
        assertTrue(result.edges.stream().anyMatch(e -> e.from.equals("com.acme.Scheduler.make()")
                && e.to.equals("com.acme.Clock.Clock(java.lang.String)")), "new Clock(...) calls its constructor: " + result.edges);
        assertTrue(result.edges.stream().anyMatch(e -> e.from.equals("com.acme.Scheduler.Scheduler(int)")
                && e.to.equals("com.acme.Scheduler.Scheduler()")), "this() calls the other constructor: " + result.edges);
        // The field initializer runs in the constructor that does not
        // delegate; the static block runs once, for the class.
        Set<String> nowCallers = result.edges.stream().filter(e -> e.to.equals("com.acme.Clock.now()"))
                .map(e -> e.from).collect(Collectors.toSet());
        assertEquals(Set.of("com.acme.Scheduler.Scheduler()", "com.acme.Scheduler"), nowCallers);
    }

    @Test
    void compoundAssignmentReadsAndWrites(@TempDir Path tempDir) throws IOException {
        Path root = tempDir.resolve("project");
        writeJava(root, "src/main/java/com/acme/Total.java", """
                package com.acme;
                public class Total {
                    private int sum;
                    void add(int n) { sum += n; }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        assertEquals(List.of("read", "write"), result.fieldAccesses.stream()
                .filter(fa -> fa.fieldName.equals("sum")).map(fa -> fa.kind).sorted().toList());
    }

    @Test
    void lombokAccessorsAndLoggerAreNotUnresolvedNoise(@TempDir Path tempDir) throws IOException {
        Path root = tempDir.resolve("project");
        writeJava(root, "src/main/java/com/acme/Order.java", """
                package com.acme;
                import lombok.Data;
                @Data
                public class Order {
                    private String status;
                }
                """);
        writeJava(root, "src/main/java/com/acme/OrderFlow.java", """
                package com.acme;
                import lombok.extern.slf4j.Slf4j;
                @Slf4j
                public class OrderFlow {
                    void run(Order order) {
                        log.info("start");
                        String s = order.getStatus();
                        order.setStatus("done");
                        mystery.call();
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();

        Map<Integer, String> reasonByLine = result.callSites.stream()
                .filter(c -> c.from.startsWith("com.acme.OrderFlow.run"))
                .collect(Collectors.toMap(c -> c.line, c -> c.unresolved));
        assertEquals(Map.of(6, "lombok", 7, "lombok", 8, "lombok", 9, "unresolved"), reasonByLine);
        // The accessors are the field accesses they stand for.
        List<String> statusAccesses = result.fieldAccesses.stream()
                .filter(fa -> fa.classId.equals("com.acme.Order") && fa.fieldName.equals("status")
                        && fa.nodeId.startsWith("com.acme.OrderFlow.run"))
                .map(fa -> fa.line + ":" + fa.kind).sorted().toList();
        assertEquals(List.of("7:read", "8:write"), statusAccesses);
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
