package com.devkit.codegraph;

import com.github.javaparser.ParserConfiguration;
import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.resolution.declarations.ResolvedMethodDeclaration;
import com.github.javaparser.symbolsolver.JavaSymbolSolver;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.nio.file.Path;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;

class TypeSolverFactoryTest {

    @Test
    void resolvesMethodCallAcrossFilesInSourceRoot() throws IOException {
        Path sourceRoot = Path.of("src/test/resources/sample-project/src/main/java");
        var typeSolver = TypeSolverFactory.build(sourceRoot);

        StaticJavaParser.setConfiguration(new ParserConfiguration().setSymbolResolver(new JavaSymbolSolver(typeSolver)));

        CompilationUnit cu = StaticJavaParser.parse(sourceRoot.resolve("com/acme/refund/RefundController.java"));
        List<MethodCallExpr> calls = cu.findAll(MethodCallExpr.class);
        assertEquals(1, calls.size(), "expected exactly one method call in RefundController.create");

        ResolvedMethodDeclaration resolved = calls.get(0).resolve();
        assertEquals("com.acme.refund.RefundService.process", resolved.getQualifiedName());
    }
}
