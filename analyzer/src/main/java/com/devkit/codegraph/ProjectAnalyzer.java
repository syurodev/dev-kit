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
import java.nio.file.Files;import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
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
        // Without an explicit language level, JavaParser defaults to one too
        // old to parse records, pattern-matching instanceof, or multi-label
        // switch cases (all Java 14+) — every such file fails to parse and
        // silently vanishes from the index via parseFile's catch-all below,
        // with no node, no edge, no error. JAVA_21 matches this project's
        // own maven.compiler.release; verified against the installed
        // javaparser-core 3.28.2 jar that this constant exists.
        StaticJavaParser.setConfiguration(new ParserConfiguration()
                .setLanguageLevel(ParserConfiguration.LanguageLevel.JAVA_21)
                .setSymbolResolver(new JavaSymbolSolver(typeSolver)));

        List<ParsedFile> files = new ArrayList<>();
        try (Stream<Path> walk = Files.walk(sourceRoot)) {
            for (Path javaFile : walk.filter(p -> p.toString().endsWith(".java")).toList()) {
                parseFile(javaFile).ifPresent(files::add);
            }
        }

        // Pass 1 collects every declaration and the class -> file map; pass 2
        // resolves calls against that complete map, because a call in an
        // early-parsed file may target a class in a file not parsed yet.
        Map<String, String> classFile = new HashMap<>();
        List<MethodCallSite> callSites = new ArrayList<>();
        for (ParsedFile file : files) {
            collectDeclarations(file, classFile, callSites, result);
        }
        // Snapshot of every node id pass 1 actually created — pass 2 uses it
        // to reject edges pointing at a method that resolve() found (e.g. a
        // JDK/library method via ReflectionTypeSolver, or a project method
        // whose own signature failed to resolve) but that never became a
        // node. The Go side's AnalysisResult.Validate rejects the whole
        // index if any edge references an unknown node — this must not let
        // one through.
        Set<String> nodeIds = new HashSet<>();
        for (Node n : result.nodes) {
            nodeIds.add(n.id);
        }
        for (MethodCallSite site : callSites) {
            addCallEdges(site, classFile, nodeIds, result);
            addFieldAccesses(site, result);
        }

        result.edges = dedupeEdges(result.edges);
        result.fileDependencies = dedupeFileDependencies(result.fileDependencies);
        return result;
    }

    private Optional<ParsedFile> parseFile(Path javaFile) {
        CompilationUnit cu;
        // IOException: the file is unreadable. ParseProblemException is a
        // RuntimeException; deeply nested expressions can overflow the
        // parser's stack. Either way, one bad file must not abort indexing
        // the rest of the project (FR-8's "unresolved, not fatal" principle
        // applied one level up: unparseable, not just unresolvable). Real
        // errors (OOM etc.) still propagate.
        try {
            cu = StaticJavaParser.parse(javaFile);
        } catch (IOException | RuntimeException | StackOverflowError e) {
            return Optional.empty();
        }
        return Optional.of(new ParsedFile(cu, projectRoot.relativize(javaFile).toString()));
    }

    private void collectDeclarations(ParsedFile file, Map<String, String> classFile, List<MethodCallSite> callSites, AnalysisResult result) {
        for (ClassOrInterfaceDeclaration classDecl : file.cu().findAll(ClassOrInterfaceDeclaration.class)) {
            String className;
            try {
                className = classDecl.resolve().getQualifiedName();
            } catch (RuntimeException | StackOverflowError e) {
                // If resolving class declaration fails, fallback to AST fully-qualified name or simple name
                className = classDecl.getFullyQualifiedName().orElse(classDecl.getNameAsString());
            }
            classFile.putIfAbsent(className, file.relativePath());
            String layer = LayerClassifier.classify(classDecl);

            result.nodes.add(classNode(classDecl, className, layer, file.relativePath(), classDecl.getRange()));

            for (MethodDeclaration methodDecl : classDecl.getMethods()) {
                Optional<String> methodId = addMethodNode(methodDecl, layer, file.relativePath(), result);
                if (methodId.isPresent()) {
                    callSites.add(new MethodCallSite(methodDecl, methodId.get(), file.relativePath()));
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
        node.summary = methodDecl.getJavadoc()
                .map(j -> j.getDescription().toText().trim())
                .orElse("");
        for (com.github.javaparser.ast.body.Parameter p : methodDecl.getParameters()) {
            node.params.add(new Param(p.getNameAsString(), p.getType().asString()));
        }
        Range range = methodDecl.getRange().orElse(null);
        if (range != null) {
            node.line = range.begin.line;
            node.endLine = range.end.line;
        }
        result.nodes.add(node);

        return Optional.of(node.id);
    }

    private void addCallEdges(MethodCallSite site, Map<String, String> classFile, Set<String> nodeIds, AnalysisResult result) {
        for (com.github.javaparser.ast.expr.MethodCallExpr call : site.methodDecl().findAll(com.github.javaparser.ast.expr.MethodCallExpr.class)) {
            int line = call.getRange().map(r -> r.begin.line).orElse(0);
            ResolvedMethodDeclaration target;
            String targetClass;
            String toNodeId;
            try {
                target = call.resolve();
                // target.getSignature() can also lazily throw UnsolvedSymbolException if its params are missing
                targetClass = target.declaringType().getQualifiedName();
                toNodeId = targetClass + "." + target.getSignature();
            } catch (RuntimeException | StackOverflowError e) {
                // Unresolved call (missing jar in the local cache, dynamic
                // dispatch the solver can't follow, unresolved parameter types, etc.)
                // — FR-8: this is a gap in the graph, not a fatal error for the whole project.
                // Recorded rather than dropped: a trace that runs into this has to be
                // able to say the flow was lost here, not just end.
                result.callSites.add(new CallSite(site.methodId(), "", line, "unresolved"));
                continue;
            }
            if (!nodeIds.contains(toNodeId)) {
                // Resolved successfully but no node exists for it. Two different
                // walls, and the trace reads very differently depending on which:
                // an interface method has a body somewhere we cannot find, a
                // library method has no body we can ever read.
                boolean isInterface;
                try {
                    isInterface = target.declaringType().isInterface();
                } catch (RuntimeException | StackOverflowError e) {
                    isInterface = false;
                }
                result.callSites.add(new CallSite(
                        site.methodId(), "", line, isInterface ? "interface" : "library"));
                continue;
            }
            result.callSites.add(new CallSite(site.methodId(), toNodeId, line, ""));
            result.edges.add(new Edge(site.methodId(), toNodeId, "call"));

            // File-level dependency: only when the callee is declared in this
            // project's own source (classFile has no entry for JDK/library
            // types, and a dependency on an external file is meaningless for
            // blast radius). Same-file calls are skipped — a file never
            // depends on itself.
            String targetFile = classFile.get(targetClass);
            if (targetFile != null && !targetFile.equals(site.filePath())) {
                result.fileDependencies.add(new FileDependency(site.filePath(), targetFile));
            }
        }
    }

    private void addFieldAccesses(MethodCallSite site, AnalysisResult result) {
        for (com.github.javaparser.ast.expr.FieldAccessExpr fae : site.methodDecl().findAll(com.github.javaparser.ast.expr.FieldAccessExpr.class)) {
            try {
                recordFieldAccess(fae.resolve(), fae, site, result);
            } catch (RuntimeException | StackOverflowError ignored) {
            }
        }
        for (com.github.javaparser.ast.expr.NameExpr ne : site.methodDecl().findAll(com.github.javaparser.ast.expr.NameExpr.class)) {
            try {
                recordFieldAccess(ne.resolve(), ne, site, result);
            } catch (RuntimeException | StackOverflowError ignored) {
            }
        }
    }

    private void recordFieldAccess(com.github.javaparser.resolution.declarations.ResolvedValueDeclaration resolved, com.github.javaparser.ast.expr.Expression expr, MethodCallSite site, AnalysisResult result) {
        if (resolved != null && resolved.isField()) {
            com.github.javaparser.resolution.declarations.ResolvedFieldDeclaration field = resolved.asField();
            String classId;
            try {
                classId = field.declaringType().getQualifiedName();
            } catch (RuntimeException | StackOverflowError e) {
                return;
            }
            String fieldName = field.getName();
            String kind = determineAccessKind(expr);
            int line = expr.getRange().map(r -> r.begin.line).orElse(0);
            result.fieldAccesses.add(new FieldAccess(classId, site.methodId(), fieldName, kind, line));
        }
    }

    private String determineAccessKind(com.github.javaparser.ast.expr.Expression expr) {
        com.github.javaparser.ast.Node current = expr;
        while (current.getParentNode().isPresent() && current.getParentNode().get() instanceof com.github.javaparser.ast.expr.EnclosedExpr) {
            current = current.getParentNode().get();
        }
        Optional<com.github.javaparser.ast.Node> parentOpt = current.getParentNode();
        if (parentOpt.isPresent()) {
            com.github.javaparser.ast.Node parent = parentOpt.get();
            if (parent instanceof com.github.javaparser.ast.expr.AssignExpr assign) {
                if (assign.getTarget() == current) {
                    return "write";
                }
            } else if (parent instanceof com.github.javaparser.ast.expr.UnaryExpr unary) {
                switch (unary.getOperator()) {
                    case PREFIX_INCREMENT, POSTFIX_INCREMENT, PREFIX_DECREMENT, POSTFIX_DECREMENT -> {
                        if (unary.getExpression() == current) {
                            return "write";
                        }
                    }
                    default -> {}
                }
            }
        }
        return "read";
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

    private static List<FileDependency> dedupeFileDependencies(List<FileDependency> deps) {
        Map<String, FileDependency> byKey = new LinkedHashMap<>();
        for (FileDependency fd : deps) {
            byKey.putIfAbsent(fd.filePath + "\n" + fd.dependsOnFilePath, fd);
        }
        return new ArrayList<>(byKey.values());
    }

    private Node classNode(ClassOrInterfaceDeclaration classDecl, String qualifiedName, String layer, String relativePath, Optional<Range> range) {
        Node node = new Node();
        node.id = qualifiedName;
        node.label = qualifiedName;
        node.kind = "class";
        node.layer = layer;
        node.filePath = relativePath;
        node.summary = classDecl.getJavadoc()
                .map(j -> j.getDescription().toText().trim())
                .orElse("");
        range.ifPresent(r -> {
            node.line = r.begin.line;
            node.endLine = r.end.line;
        });
        return node;
    }

    private record ParsedFile(CompilationUnit cu, String relativePath) {}

    private record MethodCallSite(MethodDeclaration methodDecl, String methodId, String filePath) {}
}
