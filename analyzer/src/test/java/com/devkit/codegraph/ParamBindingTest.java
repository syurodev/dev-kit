package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import java.util.stream.Collectors;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Where a handler parameter's value comes from, and what a body type holds.
 * This is what a drafted API request is built out of; getting it wrong sends
 * the value in the wrong place, which is worse than sending nothing.
 */
class ParamBindingTest {

    private static Map<String, Param> paramsOf(AnalysisResult result, String labelSuffix) {
        return result.nodes.stream()
                .filter(n -> n.label.endsWith(labelSuffix))
                .flatMap(n -> n.params.stream())
                .collect(Collectors.toMap(p -> p.name, p -> p));
    }

    @Test
    void readsEachSpringBindingAndTheNameOnTheWire(@TempDir Path root) throws IOException {
        Files.createDirectories(root.resolve("example"));
        Files.writeString(root.resolve("example/RefundController.java"), """
                package example;
                import org.springframework.web.bind.annotation.*;
                @RestController
                class RefundController {
                    @PostMapping("/refunds/{id}")
                    String create(@PathVariable String id,
                                  @RequestParam("page_size") int pageSize,
                                  @RequestParam int page,
                                  @RequestHeader(name = "X-Tenant") String tenant,
                                  @RequestBody RefundRequest body,
                                  jakarta.servlet.http.HttpServletRequest request) { return ""; }
                }
                """);
        Files.writeString(root.resolve("example/RefundRequest.java"), """
                package example;
                class RefundRequest {
                    public String reference;
                    public java.math.BigDecimal amount;
                    static final String KIND = "refund";
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();
        Map<String, Param> params = paramsOf(result, "RefundController.create");

        assertEquals("path", params.get("id").source);
        assertNull(params.get("id").wireName, "a name that is not renamed carries no wire name");

        assertEquals("query", params.get("pageSize").source);
        assertEquals("page_size", params.get("pageSize").wireName);
        assertEquals("query", params.get("page").source);
        assertNull(params.get("page").wireName);

        assertEquals("header", params.get("tenant").source);
        assertEquals("X-Tenant", params.get("tenant").wireName);

        // An unannotated parameter in a handler is the request, the principal
        // or a model — not a value to put in a draft.
        assertEquals("other", params.get("request").source);
    }

    @Test
    void resolvesABodyTypeToItsOwnFields(@TempDir Path root) throws IOException {
        Files.createDirectories(root.resolve("example"));
        Files.writeString(root.resolve("example/RefundController.java"), """
                package example;
                import org.springframework.web.bind.annotation.*;
                @RestController
                class RefundController {
                    @PostMapping("/refunds")
                    String create(@RequestBody RefundRequest body) { return ""; }
                }
                """);
        Files.writeString(root.resolve("example/RefundRequest.java"), """
                package example;
                class RefundRequest {
                    public String reference;
                    public java.math.BigDecimal amount;
                    static final String KIND = "refund";
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();
        Param body = paramsOf(result, "RefundController.create").get("body");
        assertEquals("body", body.source);
        assertNotNull(body.fields);
        List<String> names = body.fields.stream().map(f -> f.name).toList();
        assertEquals(List.of("reference", "amount"), names);
        // A constant on the DTO is not part of the request.
        assertFalse(names.contains("KIND"));
    }

    @Test
    void readsARecordsComponentsAsItsFields(@TempDir Path root) throws IOException {
        Files.createDirectories(root.resolve("example"));
        Files.writeString(root.resolve("example/RefundController.java"), """
                package example;
                import org.springframework.web.bind.annotation.*;
                @RestController
                class RefundController {
                    @PostMapping("/refunds")
                    String create(@RequestBody RefundRequest body) { return ""; }
                }
                """);
        Files.writeString(root.resolve("example/RefundRequest.java"),
                "package example; record RefundRequest(String reference, long amount) {}");

        AnalysisResult result = new ProjectAnalyzer(root).analyze();
        Param body = paramsOf(result, "RefundController.create").get("body");
        assertNotNull(body.fields);
        assertEquals(List.of("reference", "amount"), body.fields.stream().map(f -> f.name).toList());
        assertEquals(List.of("String", "long"), body.fields.stream().map(f -> f.type).toList());
    }

    @Test
    void leavesTheBodyEmptyWhenTheTypeIsAmbiguousOrOutsideTheProject(@TempDir Path root) throws IOException {
        Files.createDirectories(root.resolve("a"));
        Files.createDirectories(root.resolve("b"));
        Files.createDirectories(root.resolve("example"));
        Files.writeString(root.resolve("a/RefundRequest.java"),
                "package a; public class RefundRequest { public String fromA; }");
        Files.writeString(root.resolve("b/RefundRequest.java"),
                "package b; public class RefundRequest { public String fromB; }");
        Files.writeString(root.resolve("example/RefundController.java"), """
                package example;
                import org.springframework.web.bind.annotation.*;
                @RestController
                class RefundController {
                    @PostMapping("/refunds")
                    String create(@RequestBody RefundRequest body,
                                  @RequestBody com.stripe.Charge charge) { return ""; }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();
        Map<String, Param> params = paramsOf(result, "RefundController.create");
        // Two classes with that simple name is exactly when a guess sends the
        // wrong fields. An empty body the UI can explain beats a confident
        // wrong one.
        assertEquals("body", params.get("body").source);
        assertNull(params.get("body").fields);
        // A library type has no source here to read at all.
        assertNull(params.get("charge").fields);
    }

    @Test
    void leavesBindingsOffMethodsThatAreNotHandlers(@TempDir Path root) throws IOException {
        Files.createDirectories(root.resolve("example"));
        Files.writeString(root.resolve("example/RefundService.java"), """
                package example;
                import org.springframework.stereotype.Service;
                @Service
                class RefundService {
                    String process(String requestId) { return requestId; }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(root).analyze();
        Param param = paramsOf(result, "RefundService.process").get("requestId");
        // Empty, not "other": the service says nothing about the wire because
        // it is not on it.
        assertNull(param.source);
    }
}
