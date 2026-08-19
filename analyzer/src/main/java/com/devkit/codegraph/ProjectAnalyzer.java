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
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
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
        if (!Files.exists(projectRoot) || !Files.isDirectory(projectRoot)) {
            throw new IllegalArgumentException("Project directory does not exist or is not a directory: " + projectRoot);
        }

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
        result.edges = dedupeEdges(result.edges);
        return result;
    }

    // The same target called repeatedly in one body yields identical edges;
    // keep the first of each from/to/kind triple, preserving order. One pass
    // at the end instead of a contains-check per added edge keeps large
    // projects linear rather than quadratic.
    private static List<Edge> dedupeEdges(List<Edge> edges) {
        Map<String, Edge> byKey = new LinkedHashMap<>();
        for (Edge e : edges) {
            byKey.putIfAbsent(e.from + "\n" + e.to + "\n" + e.kind, e);
        }
        return new ArrayList<>(byKey.values());
    }

    private void analyzeFile(Path javaFile, AnalysisResult result) throws IOException {
        CompilationUnit cu;
        // ParseProblemException is a RuntimeException; deeply nested
        // expressions can overflow the parser's stack. Either way, one bad
        // file must not abort indexing the rest of the project (FR-8's
        // "unresolved, not fatal" principle applied one level up:
        // unparseable, not just unresolvable). Real errors (OOM etc.)
        // still propagate.
        try {
            cu = StaticJavaParser.parse(javaFile);
        } catch (RuntimeException | StackOverflowError e) {
            return;
        }

        String relativePath = projectRoot.relativize(javaFile).toString();

        for (ClassOrInterfaceDeclaration classDecl : cu.findAll(ClassOrInterfaceDeclaration.class)) {
            String className;
            try {
                className = classDecl.resolve().getQualifiedName();
            } catch (RuntimeException | StackOverflowError e) {
                // If resolving class declaration fails, fallback to AST fully-qualified name or simple name
                className = classDecl.getFullyQualifiedName().orElse(classDecl.getNameAsString());
            }
            String layer = LayerClassifier.classify(classDecl);

            result.nodes.add(classNode(className, layer, relativePath, classDecl.getRange()));

            for (MethodDeclaration methodDecl : classDecl.getMethods()) {
                Optional<String> methodId = addMethodNode(methodDecl, layer, relativePath, result);
                if (methodId.isPresent()) {
                    EndpointExtractor.extract(classDecl, methodDecl).ifPresent(mapping -> {
                        Endpoint endpoint = new Endpoint();
                        endpoint.id = mapping.httpMethod() + " " + mapping.path();
                        endpoint.httpMethod = mapping.httpMethod();
                        endpoint.path = mapping.path();
                        endpoint.entryNode = methodId.get();
                        result.endpoints.add(endpoint);
                    });
                }
            }
        }
    }

    private Optional<String> addMethodNode(MethodDeclaration methodDecl, String layer, String relativePath, AnalysisResult result) {
        String methodId;
        String methodLabel;
        try {
            ResolvedMethodDeclaration resolved = methodDecl.resolve();
            // Note: getSignature() and getQualifiedName() can lazily trigger type resolution
            // for parameters and return types. If a parameter type is not on the classpath,
            // getSignature() throws UnsolvedSymbolException. We catch Throwable to handle this safely (FR-8).
            methodId = resolved.declaringType().getQualifiedName() + "." + resolved.getSignature();
            methodLabel = resolved.getQualifiedName();
        } catch (RuntimeException | StackOverflowError e) {
            return Optional.empty(); // can't resolve this method's declaration or signature — skip it, not fatal
        }

        Node node = new Node();
        node.id = methodId;
        node.label = methodLabel;
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
        return Optional.of(node.id);
    }

    private void addCallEdges(MethodDeclaration methodDecl, String fromNodeId, AnalysisResult result) {
        for (com.github.javaparser.ast.expr.MethodCallExpr call : methodDecl.findAll(com.github.javaparser.ast.expr.MethodCallExpr.class)) {
            String toNodeId;
            try {
                ResolvedMethodDeclaration target = call.resolve();
                // target.getSignature() can also lazily throw UnsolvedSymbolException if its params are missing
                toNodeId = target.declaringType().getQualifiedName() + "." + target.getSignature();
            } catch (RuntimeException | StackOverflowError e) {
                // Unresolved call (missing jar in the local cache, dynamic
                // dispatch the solver can't follow, unresolved parameter types, etc.)
                // — FR-8: this is a gap in the graph, not a fatal error for the whole project.
                continue;
            }
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
