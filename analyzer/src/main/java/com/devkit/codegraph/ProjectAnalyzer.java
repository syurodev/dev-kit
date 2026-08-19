package com.devkit.codegraph;

import com.github.javaparser.ParserConfiguration;
import com.github.javaparser.Range;
import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.resolution.declarations.ResolvedMethodDeclaration;
import com.github.javaparser.symbolsolver.JavaSymbolSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.CombinedTypeSolver;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.stream.Stream;

public class ProjectAnalyzer {

    private final Path projectRoot;
    private final Path sourceRoot;

    public ProjectAnalyzer(Path projectRoot) {
        this.projectRoot = projectRoot;
        // v1 assumes the conventional Maven/Gradle layout. A project using a
        // different layout just won't have any of its files found below —
        // it produces an empty (not crashing) index, which is a known
        // limitation to document, not a special case to handle here.
        this.sourceRoot = projectRoot.resolve("src/main/java");
    }

    public AnalysisResult analyze() throws IOException {
        AnalysisResult result = new AnalysisResult();
        if (!Files.isDirectory(sourceRoot)) {
            return result;
        }

        CombinedTypeSolver typeSolver = TypeSolverFactory.build(sourceRoot);
        StaticJavaParser.setConfiguration(new ParserConfiguration().setSymbolResolver(new JavaSymbolSolver(typeSolver)));

        try (Stream<Path> walk = Files.walk(sourceRoot)) {
            for (Path javaFile : walk.filter(p -> p.toString().endsWith(".java")).toList()) {
                analyzeFile(javaFile, result);
            }
        }
        return result;
    }

    private void analyzeFile(Path javaFile, AnalysisResult result) throws IOException {
        CompilationUnit cu;
        try {
            cu = StaticJavaParser.parse(javaFile);
        } catch (Exception e) {
            // A single file with a parse error must not abort indexing the
            // rest of the project (FR-8's "unresolved, not fatal" principle
            // applied one level up: unparseable, not just unresolvable).
            return;
        }

        String relativePath = projectRoot.relativize(javaFile).toString();

        for (ClassOrInterfaceDeclaration classDecl : cu.findAll(ClassOrInterfaceDeclaration.class)) {
            String className = classDecl.resolve().getQualifiedName();
            String layer = LayerClassifier.classify(classDecl);

            result.nodes.add(classNode(className, layer, relativePath, classDecl.getRange()));

            for (MethodDeclaration methodDecl : classDecl.getMethods()) {
                addMethodNode(methodDecl, layer, relativePath, result);
                EndpointExtractor.extract(classDecl, methodDecl).ifPresent(mapping -> {
                    Endpoint endpoint = new Endpoint();
                    endpoint.id = mapping.httpMethod() + " " + mapping.path();
                    endpoint.httpMethod = mapping.httpMethod();
                    endpoint.path = mapping.path();
                    endpoint.entryNode = className + "." + methodDecl.resolve().getSignature();
                    result.endpoints.add(endpoint);
                });
            }
        }
    }

    private void addMethodNode(MethodDeclaration methodDecl, String layer, String relativePath, AnalysisResult result) {
        ResolvedMethodDeclaration resolved;
        try {
            resolved = methodDecl.resolve();
        } catch (Exception e) {
            return; // can't resolve this method's own declaration — skip it, not fatal
        }

        Node node = new Node();
        node.id = resolved.declaringType().getQualifiedName() + "." + resolved.getSignature();
        node.label = resolved.getQualifiedName();
        node.kind = "method";
        node.layer = layer;
        node.filePath = relativePath;
        Range range = methodDecl.getRange().orElse(null);
        if (range != null) {
            node.line = range.begin.line;
            node.endLine = range.end.line;
        }
        result.nodes.add(node);

        addCallEdges(methodDecl, node.id, result);
    }

    private void addCallEdges(MethodDeclaration methodDecl, String fromNodeId, AnalysisResult result) {
        for (com.github.javaparser.ast.expr.MethodCallExpr call : methodDecl.findAll(com.github.javaparser.ast.expr.MethodCallExpr.class)) {
            ResolvedMethodDeclaration target;
            try {
                target = call.resolve();
            } catch (Exception e) {
                // Unresolved call (missing jar in the local cache, dynamic
                // dispatch the solver can't follow, etc.) — FR-8: this is a
                // gap in the graph, not a fatal error for the whole project.
                continue;
            }
            String toNodeId = target.declaringType().getQualifiedName() + "." + target.getSignature();
            result.edges.add(new Edge(fromNodeId, toNodeId, "call"));
        }
    }

    private Node classNode(String qualifiedName, String layer, String relativePath, Optional<Range> range) {
        Node node = new Node();
        node.id = qualifiedName;
        node.label = qualifiedName;
        node.kind = "class";
        node.layer = layer;
        node.filePath = relativePath;
        range.ifPresent(r -> {
            node.line = r.begin.line;
            node.endLine = r.end.line;
        });
        return node;
    }
}
