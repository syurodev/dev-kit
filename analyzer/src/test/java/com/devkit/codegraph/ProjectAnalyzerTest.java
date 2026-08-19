package com.devkit.codegraph;

import org.junit.jupiter.api.Test;

import java.io.IOException;
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
}
