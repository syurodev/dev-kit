package com.devkit.codegraph;

import com.github.javaparser.ParserConfiguration;
import com.github.javaparser.Range;
import com.github.javaparser.JavaParser;
import com.github.javaparser.ParseResult;
import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.body.AnnotationDeclaration;
import com.github.javaparser.ast.body.BodyDeclaration;
import com.github.javaparser.ast.body.CallableDeclaration;
import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import com.github.javaparser.ast.body.ConstructorDeclaration;
import com.github.javaparser.ast.body.FieldDeclaration;
import com.github.javaparser.ast.body.InitializerDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.expr.AssignExpr;
import com.github.javaparser.ast.expr.EnclosedExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.FieldAccessExpr;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.expr.MethodReferenceExpr;
import com.github.javaparser.ast.expr.NameExpr;
import com.github.javaparser.ast.expr.ObjectCreationExpr;
import com.github.javaparser.ast.expr.ThisExpr;
import com.github.javaparser.ast.expr.TypeExpr;
import com.github.javaparser.ast.expr.UnaryExpr;
import com.github.javaparser.ast.nodeTypes.NodeWithExtends;
import com.github.javaparser.ast.nodeTypes.NodeWithImplements;
import com.github.javaparser.ast.stmt.ExplicitConstructorInvocationStmt;
import com.github.javaparser.ast.type.ClassOrInterfaceType;
import com.github.javaparser.resolution.declarations.ResolvedMethodLikeDeclaration;
import com.github.javaparser.resolution.declarations.ResolvedMethodDeclaration;
import com.github.javaparser.resolution.declarations.ResolvedTypeDeclaration;
import com.github.javaparser.resolution.declarations.ResolvedValueDeclaration;
import com.github.javaparser.resolution.types.ResolvedType;
import com.github.javaparser.symbolsolver.JavaSymbolSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.CombinedTypeSolver;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.IdentityHashMap;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.stream.Collectors;

public class ProjectAnalyzer {

    private final Path projectRoot;
    private final List<Path> sourceRoots;

    /** Class-level Lombok annotations that generate getters, and those that generate setters. */
    private static final Set<String> LOMBOK_GETTERS = Set.of("Data", "Value", "Getter");
    private static final Set<String> LOMBOK_SETTERS = Set.of("Data", "Setter");
    /** Lombok's logger annotations, each of which declares a static field named `log`. */
    private static final Set<String> LOMBOK_LOGGERS = Set.of(
            "Slf4j", "XSlf4j", "Log4j", "Log4j2", "Log", "CommonsLog", "JBossLog", "Flogger", "CustomLog");

    /** Property that fixes the number of parse threads; 1 parses on the calling thread, as the analyzer once always did. */
    static final String PARSE_THREADS_PROPERTY = "devkit.analyzer.parseThreads";
    /**
     * More parse threads than this stop paying: the pass is memory-bound long
     * before it is core-bound, and each thread holds a syntax tree in flight
     * inside a heap the launcher caps (-Xmx2g).
     */
    private static final int MAX_DEFAULT_PARSE_THREADS = 8;

    private final int parseThreads;
    private Set<com.github.javaparser.ast.Node> deferredCamelCallbacks = Set.of();

    public ProjectAnalyzer(Path projectRoot) {
        this(projectRoot, defaultParseThreads());
    }

    ProjectAnalyzer(Path projectRoot, int parseThreads) {
        this.projectRoot = projectRoot;
        this.sourceRoots = discoverSourceRoots(projectRoot);
        this.parseThreads = Math.max(1, parseThreads);
    }

    private static int defaultParseThreads() {
        return Integer.getInteger(PARSE_THREADS_PROPERTY,
                Math.min(Runtime.getRuntime().availableProcessors(), MAX_DEFAULT_PARSE_THREADS));
    }

    /**
     * Finds every conventional source root under the project.
     *
     * The analyzer used to resolve exactly one, projectRoot/src/main/java, and
     * that made a whole shape of repository silently unanalyzable: point it at
     * a folder of microservices, where each service has its own
     * &lt;service&gt;/src/main/java, and there is no such directory at the top,
     * so it returned an empty index and the UI reported an empty project rather
     * than a misread one.
     *
     * Discovering them all also keeps cross-service resolution working, which
     * splitting the run into one subprocess per service would have lost: a
     * shared module's types resolve because every root goes into the same
     * CombinedTypeSolver.
     *
     * The fallback is deliberate. A folder holding .java files and no Maven or
     * Gradle layout at all is still something a person wants to read, so the
     * project root itself becomes the source root — walked with the same skip
     * list as everything else (see SourceTree).
     */
    private static List<Path> discoverSourceRoots(Path projectRoot) {
        List<Path> roots = new ArrayList<>();
        if (!Files.isDirectory(projectRoot)) {
            return roots;
        }
        try {
            roots.addAll(SourceTree.conventionalSourceRoots(projectRoot));
        } catch (IOException e) {
            // An unreadable tree is not a crash: fall through to the checks
            // below, which produce an empty index rather than an exception.
        }
        if (!roots.isEmpty()) {
            return roots;
        }
        if (!javaFiles(projectRoot).isEmpty()) {
            roots.add(projectRoot);
        }
        return roots;
    }

    private static List<Path> javaFiles(Path root) {
        try {
            return SourceTree.files(root, p -> p.toString().endsWith(".java"));
        } catch (IOException e) {
            return List.of();
        }
    }

    public AnalysisResult analyze() throws IOException {
        if (!Files.exists(projectRoot) || !Files.isDirectory(projectRoot)) {
            throw new IllegalArgumentException("Project directory does not exist or is not a directory: " + projectRoot);
        }

        AnalysisResult result = new AnalysisResult();
        if (sourceRoots.isEmpty()) {
            return result;
        }

        CombinedTypeSolver typeSolver = TypeSolverFactory.build(projectRoot, sourceRoots);
        // Without an explicit language level, JavaParser defaults to one too
        // old to parse records, pattern-matching instanceof, or multi-label
        // switch cases (all Java 14+) — every such file fails to parse and
        // silently vanishes from the index via parseFile's catch-all below,
        // with no node, no edge, no error.
        //
        // This is the newest syntax the analyzer can READ, and it is a separate
        // knob from maven.compiler.release, which is only the bytecode the
        // analyzer itself is compiled to. They are deliberately different: the
        // release stays at 21 so the jar runs on the JDK people actually have
        // (an IDE's bundled runtime, a distro's openjdk-21), while the language
        // level runs ahead so a project written in newer Java is still indexed.
        // JavaParser is a parser, not javac — it needs no JDK 25 to read Java 25.
        //
        // Raise this, not the release, when a newer Java ships. Verified against
        // the installed javaparser-core 3.28.2 jar that this constant exists.
        ParserConfiguration parserConfiguration = new ParserConfiguration()
                .setLanguageLevel(ParserConfiguration.LanguageLevel.JAVA_25)
                .setSymbolResolver(new JavaSymbolSolver(typeSolver));
        StaticJavaParser.setConfiguration(parserConfiguration);

        // One list across every source root: the passes below need to see
        // the whole repository at once, which is what lets a call from one
        // service into a shared module resolve. Roots and files are both
        // sorted, so which of two same-named classes is "first" — and so
        // keeps the plain id — is the same on every run.
        List<SourceFile> sources = new ArrayList<>();
        Set<Path> seen = new HashSet<>();
        for (Path root : sourceRoots) {
            String module = moduleLabel(root);
            for (Path javaFile : javaFiles(root)) {
                // Nested source roots would otherwise parse a file twice
                // and duplicate every node it declares.
                if (seen.add(javaFile.toAbsolutePath().normalize())) {
                    sources.add(new SourceFile(javaFile, module));
                }
            }
        }
        List<ParsedFile> files = parseAll(sources, parserConfiguration);
        List<CompilationUnit> units = files.stream().map(ParsedFile::cu).toList();
        Annotations annotations = Annotations.of(units);
        ConstantResolver constants = ConstantResolver.of(units);

        // Pass 1 collects every declaration; pass 2 resolves calls against
        // that complete picture, because a call in an early-parsed file may
        // target a class in a file not parsed yet.
        Index index = new Index();
        List<CallScope> scopes = new ArrayList<>();
        for (ParsedFile file : files) {
            collectDeclarations(file, annotations, index, scopes, result);
        }
        for (PendingBody pending : pendingBodies) {
            pending.param().fields = bodyFields(pending.declared(), index);
        }
        linkSupertypes(index);
        Dispatch dispatch = linkOverrides(index, result);
        extractEndpoints(index, dispatch, annotations, constants, result);
        List<ConfigExtractor.Owner> camelOwners = new ArrayList<>();
        for (TypeInfo type : index.all) {
            camelOwners.add(new ConfigExtractor.Owner(type.decl, type.id, type.file.relativePath(),
                    type.methods.stream().map(m -> new ConfigExtractor.Member(m.decl(), m.id())).toList()));
        }
        deferredCamelCallbacks = new CamelExtractor(camelOwners, constants, result).extract();

        // Snapshot of every node id pass 1 actually created — pass 2 uses it
        // to reject edges pointing at a method that resolve() found (e.g. a
        // JDK/library method via ReflectionTypeSolver, or a project method
        // whose own signature failed to resolve) but that never became a
        // node. The Go side's AnalysisResult.Validate rejects the whole
        // index if any edge references an unknown node — this must not let
        // one through.
        Set<String> nodeIds = new HashSet<>();
        // Indexed a second way, by "<declaring type>.<method name>", for the
        // fallback in addCallEdges. Built once here rather than scanned per
        // call: a large project has tens of thousands of each, and matching by
        // prefix inside the loop would be quadratic.
        Map<String, List<String>> methodsByTypeAndName = new HashMap<>();
        for (Node n : result.nodes) {
            nodeIds.add(n.id);
            if (!"method".equals(n.kind)) {
                continue;
            }
            int paren = n.id.indexOf('(');
            if (paren > 0) {
                methodsByTypeAndName
                        .computeIfAbsent(n.id.substring(0, paren), k -> new ArrayList<>())
                        .add(n.id);
            }
        }
        for (CallScope scope : scopes) {
            addCallEdges(scope, index, dispatch, nodeIds, methodsByTypeAndName, result);
            addFieldAccesses(scope, index, result);
        }
        extractOutboundCalls(index, scopes, annotations, constants, result);
        extractConfig(index, scopes, annotations, constants, nodeIds, result);

        for (ParsedFile file : files) {
            addTypeDependencies(file, index, result);
        }
        result.edges = dedupeEdges(result.edges);
        result.fileDependencies = dedupeFileDependencies(result.fileDependencies);
        return result;
    }

    /**
     * The properties the project's config files set, and every place the code
     * reads or switches on one. Both are text: no value is looked up, so what
     * the index holds is "this key is set to X in this file" and "this field
     * is bound to that key", and joining the two is a question for the reader.
     */
    private void extractConfig(Index index, List<CallScope> scopes, Annotations annotations,
                               ConstantResolver constants, Set<String> nodeIds, AnalysisResult result) {
        result.configKeys = ConfigFiles.read(projectRoot);
        List<ConfigExtractor.Owner> owners = new ArrayList<>();
        for (TypeInfo type : index.all) {
            List<ConfigExtractor.Member> methods = new ArrayList<>();
            for (MethodInfo method : type.methods) {
                methods.add(new ConfigExtractor.Member(method.decl(), method.id()));
            }
            owners.add(new ConfigExtractor.Owner(type.decl, type.id, type.file.relativePath(), methods));
        }
        List<ConfigExtractor.Scope> configScopes = new ArrayList<>();
        for (CallScope scope : scopes) {
            configScopes.add(new ConfigExtractor.Scope(scope.node(), scope.ownerIds(), scope.owner().file.relativePath()));
        }
        result.configUsages = ConfigExtractor.extract(owners, configScopes, annotations, constants, nodeIds);

        // Which class a call's receiver is, asked of the solver — the one thing
        // table access needs that the annotations cannot say.
        IdentityHashMap<com.github.javaparser.ast.Node, String> moduleOfScope = new IdentityHashMap<>();
        for (CallScope scope : scopes) {
            moduleOfScope.put(scope.node(), scope.owner().file.module());
        }
        TableExtractor.Result tables = TableExtractor.extract(owners, configScopes, annotations, constants, nodeIds,
                (scope, call) -> call.getScope()
                        .map(receiver -> receiverTypeId(receiver, index, moduleOfScope.getOrDefault(scope.node(), "")))
                        .orElse(null));
        result.entityTables = tables.entities();
        result.tableAccesses = tables.accesses();

        result.kafkaAccesses.addAll(KafkaExtractor.extract(owners, configScopes, annotations, constants, nodeIds));

        // Redis keys are read from the same calls.
        for (CallScope scope : scopes) {
            for (MethodCallExpr expr : scope.node().findAll(MethodCallExpr.class)) {
                if (!belongsTo(expr, scope.node())) {
                    continue;
                }
                RedisExtractor.access(expr, constants).ifPresent(access -> {
                    for (String owner : scope.ownerIds()) {
                        if (!nodeIds.contains(owner)) {
                            continue;
                        }
                        RedisAccess copy = new RedisAccess();
                        copy.from = owner;
                        copy.key = access.key;
                        copy.access = access.access;
                        copy.command = access.command;
                        copy.line = access.line;
                        copy.file = scope.owner().file.relativePath();
                        result.redisAccesses.add(copy);
                    }
                });
            }
        }
    }

    /**
     * What a source root is called when two of them declare the same class:
     * its path under the project with the conventional src/main/java dropped,
     * so services/billing/src/main/java reads "services/billing". Empty for
     * the project root itself.
     */
    private String moduleLabel(Path sourceRoot) {
        Path relative = projectRoot.toAbsolutePath().normalize().relativize(sourceRoot.toAbsolutePath().normalize());
        String label = relative.toString().replace('\\', '/');
        if (label.equals("src/main/java")) {
            return "";
        }
        if (label.endsWith("/src/main/java")) {
            label = label.substring(0, label.length() - "/src/main/java".length());
        }
        return label;
    }

    private record SourceFile(Path path, String module) {}

    /**
     * Parses every file, on {@code parseThreads} threads, and returns them in
     * the order given — which is what keeps the index the same on every run
     * whatever order the threads finish in: the passes below take "the first
     * class of a name" from this list's order.
     *
     * Only this pass is parallel. Parsing reads nothing but its own file and
     * the (read-only) configuration; every later pass shares the symbol
     * solver's caches, which are not safe to use from several threads.
     *
     * The threads do not use StaticJavaParser: its configuration is
     * thread-local, so a worker would parse with the default language level —
     * too old for records — and every such file would vanish from the index.
     */
    private List<ParsedFile> parseAll(List<SourceFile> sources, ParserConfiguration configuration) {
        List<ParsedFile> parsed = new ArrayList<>(sources.size());
        if (parseThreads == 1 || sources.size() < 2) {
            JavaParser parser = new JavaParser(configuration);
            for (SourceFile source : sources) {
                parseFile(parser, source).ifPresent(parsed::add);
            }
            return parsed;
        }

        int threads = Math.min(parseThreads, sources.size());
        AtomicInteger threadNumber = new AtomicInteger();
        // Daemon threads: a run that dies must not be kept alive by its pool.
        ExecutorService pool = Executors.newFixedThreadPool(threads, task -> {
            Thread thread = new Thread(task, "parse-" + threadNumber.incrementAndGet());
            thread.setDaemon(true);
            return thread;
        });
        try {
            // A JavaParser per thread: it is cheap, and not documented as shareable.
            ThreadLocal<JavaParser> parsers = ThreadLocal.withInitial(() -> new JavaParser(configuration));
            List<Future<Optional<ParsedFile>>> futures = new ArrayList<>(sources.size());
            for (SourceFile source : sources) {
                futures.add(pool.submit(() -> parseFile(parsers.get(), source)));
            }
            for (Future<Optional<ParsedFile>> future : futures) {
                future.get().ifPresent(parsed::add);
            }
            return parsed;
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("interrupted while parsing", e);
        } catch (ExecutionException e) {
            // parseFile absorbs a bad file; what gets here is a real error
            // (an OutOfMemoryError Main reports by name), which must not be
            // wrapped into something it no longer recognises.
            if (e.getCause() instanceof Error error) {
                throw error;
            }
            if (e.getCause() instanceof RuntimeException runtime) {
                throw runtime;
            }
            throw new IllegalStateException(e.getCause());
        } finally {
            pool.shutdownNow();
        }
    }

    private Optional<ParsedFile> parseFile(JavaParser parser, SourceFile source) {
        // IOException: the file is unreadable. A parse that fails comes back
        // as a result without a unit (a ParseProblemException from the static
        // API); deeply nested expressions can overflow the parser's stack.
        // Either way, one bad file must not abort indexing the rest of the
        // project (FR-8's "unresolved, not fatal" principle applied one level
        // up: unparseable, not just unresolvable). Real errors (OOM etc.)
        // still propagate.
        try {
            ParseResult<CompilationUnit> result = parser.parse(source.path());
            if (!result.isSuccessful() || result.getResult().isEmpty()) {
                return Optional.empty();
            }
            return Optional.of(new ParsedFile(result.getResult().get(),
                    projectRoot.relativize(source.path()).toString(), source.module()));
        } catch (IOException | RuntimeException | StackOverflowError e) {
            return Optional.empty();
        }
    }

    // --- Pass 1: declarations -------------------------------------------------

    private void collectDeclarations(ParsedFile file, Annotations annotations, Index index,
                                     List<CallScope> scopes, AnalysisResult result) {
        // Every type declaration, not only ClassOrInterfaceDeclaration. A record
        // is a TypeDeclaration but not a ClassOrInterfaceDeclaration, so the
        // narrower walk skipped every one of them — and a Spring project keeps
        // its requests, responses and DTOs in records, which meant they were
        // missing from the graph and from the file list entirely.
        //
        // findAll is pre-order, so an enclosing type always has its id before
        // a local class inside it asks for it.
        for (TypeDeclaration<?> classDecl : file.cu().findAll(TypeDeclaration.class)) {
            String layer = LayerClassifier.classify(classDecl, annotations);
            TypeInfo type = index.declare(classDecl, file, layer);
            result.nodes.add(classNode(classDecl, type.id, layer, file.relativePath(), classDecl.getRange()));

            List<String> constructorIds = new ArrayList<>();
            List<String> initializingConstructorIds = new ArrayList<>();
            for (ConstructorDeclaration ctor : classDecl.getConstructors()) {
                MethodInfo info = addMethodNode(ctor, type, layer, file.relativePath(), index,
                        annotations, result);
                constructorIds.add(info.id());
                scopes.add(new CallScope(ctor, List.of(info.id()), type));
                // A constructor that starts with this(...) hands off to another
                // one, which is where the initializers actually run.
                var statements = ctor.getBody().getStatements();
                boolean delegates = !statements.isEmpty()
                        && statements.get(0) instanceof ExplicitConstructorInvocationStmt e && e.isThis();
                if (!delegates) {
                    initializingConstructorIds.add(info.id());
                }
            }
            for (MethodDeclaration methodDecl : classDecl.getMethods()) {
                MethodInfo info = addMethodNode(methodDecl, type, layer, file.relativePath(), index,
                        annotations, result);
                type.methods.add(info);
                scopes.add(new CallScope(methodDecl, List.of(info.id()), type));
            }

            // Field initializers and initializer blocks run code too. Instance
            // ones run as part of every constructor that does not delegate, so
            // their calls belong to those; static ones, and instance ones in a
            // class with only the implicit constructor, belong to the class.
            List<String> instanceOwners = initializingConstructorIds.isEmpty()
                    ? List.of(type.id) : initializingConstructorIds;
            for (BodyDeclaration<?> member : classDecl.getMembers()) {
                if (member instanceof InitializerDeclaration init) {
                    scopes.add(new CallScope(init, init.isStatic() ? List.of(type.id) : instanceOwners, type));
                } else if (member instanceof FieldDeclaration field
                        && field.getVariables().stream().anyMatch(v -> v.getInitializer().isPresent())) {
                    scopes.add(new CallScope(field, field.isStatic() ? List.of(type.id) : instanceOwners, type));
                }
            }
        }
    }

    /** A @RequestBody parameter whose type cannot be looked up until pass 1 ends. */
    private record PendingBody(Param param, com.github.javaparser.ast.body.Parameter declared) {}

    private final List<PendingBody> pendingBodies = new ArrayList<>();

    /**
     * The fields of a @RequestBody parameter's type, when that type is in this
     * project's own source.
     *
     * Resolution is tried first because it is the right answer. When the
     * classpath cannot supply the type — which is the ordinary case for a
     * project analyzed without its dependencies — the simple name is looked up
     * in the index, and ONLY an unambiguous match counts. Two classes named
     * RefundRequest is exactly when a guess sends a draft with the wrong
     * fields, and an empty body the UI can explain is better than a confident
     * wrong one.
     */
    private List<Field> bodyFields(com.github.javaparser.ast.body.Parameter p, Index index) {
        String qualified = null;
        try {
            qualified = p.getType().resolve().asReferenceType().getQualifiedName();
        } catch (RuntimeException | StackOverflowError e) {
            // No classpath for this type; fall through to the index.
        }
        TypeInfo found = null;
        if (qualified != null) {
            found = index.oneByQualifiedName(qualified);
        }
        if (found == null) {
            found = index.oneBySimpleName(simpleTypeName(p.getType().asString()));
        }
        return found == null ? null : emptyToNull(ParamBinding.fieldsOf(found.decl));
    }

    private static List<Field> emptyToNull(List<Field> fields) {
        return fields.isEmpty() ? null : fields;
    }

    /** "java.util.List<Refund>" -> "Refund"; "RefundRequest" -> "RefundRequest". */
    private static String simpleTypeName(String written) {
        String name = written;
        int generic = name.indexOf('<');
        if (generic >= 0) {
            name = name.substring(0, generic);
        }
        int dot = name.lastIndexOf('.');
        if (dot >= 0) {
            name = name.substring(dot + 1);
        }
        return name.trim();
    }

    private MethodInfo addMethodNode(CallableDeclaration<?> decl, TypeInfo type, String layer, String relativePath,
                                     Index index, Annotations annotations, AnalysisResult result) {
        String signature;
        try {
            ResolvedMethodLikeDeclaration resolved = decl instanceof MethodDeclaration m ? m.resolve()
                    : ((ConstructorDeclaration) decl).resolve();
            // Note: getSignature() can lazily trigger type resolution for
            // parameters. If a parameter type is not on the classpath, it
            // throws UnsolvedSymbolException. Fall back to the declaration AST (FR-8).
            signature = resolved.getSignature();
        } catch (RuntimeException | StackOverflowError e) {
            // Declaration discovery must not depend on the target project's classpath.
            // Resolve each parameter independently; preserve source spelling when
            // even imports cannot identify a missing type unambiguously.
            signature = decl.getNameAsString() + "(" + decl.getParameters().stream()
                    .map(p -> signatureType(p.getType()) + (p.isVarArgs() ? "[]" : ""))
                    .collect(Collectors.joining(", ")) + ")";
        }

        Node node = new Node();
        // The type's id, not its qualified name: two services declaring the
        // same class must not produce one method id between them.
        node.id = index.uniqueId(type.id + "." + signature, "");
        node.label = type.id + "." + decl.getNameAsString();
        node.kind = "method"; // constructors too: to the graph they are callables like any other
        node.layer = layer;
        node.filePath = relativePath;
        node.summary = decl.getJavadoc()
                .map(j -> j.getDescription().toText().trim())
                .orElse("");
        for (com.github.javaparser.ast.body.Parameter p : decl.getParameters()) {
            Param param = new Param(p.getNameAsString(), p.getType().asString());
            // Only a controller's methods carry wire bindings. Reading them
            // everywhere would resolve a DTO for every parameter of every
            // service in the project to answer a question nothing asks.
            if ("controller".equals(layer)) {
                ParamBinding.describe(param, p, annotations);
                if ("body".equals(param.source)) {
                    // Resolved after every file is declared, not here: the
                    // DTO may live in a file this pass has not reached, and
                    // looking now would find nothing for half the project
                    // depending on the order the directory was walked.
                    pendingBodies.add(new PendingBody(param, p));
                }
            }
            node.params.add(param);
        }
        Range range = decl.getRange().orElse(null);
        if (range != null) {
            node.line = range.begin.line;
            node.endLine = range.end.line;
        }
        result.nodes.add(node);
        return new MethodInfo(decl, node.id, signature, type, node.line);
    }

    private String signatureType(com.github.javaparser.ast.type.Type type) {
        try {
            return type.resolve().describe();
        } catch (RuntimeException | StackOverflowError ignored) {
            if (type.isArrayType()) {
                return signatureType(type.asArrayType().getComponentType()) + "[]";
            }
            if (type.isClassOrInterfaceType()) {
                var ref = type.asClassOrInterfaceType();
                String name = ref.getNameWithScope();
                var cu = type.findCompilationUnit();
                if (cu.isPresent()) {
                    for (var imp : cu.get().getImports()) {
                        if (!imp.isAsterisk() && !imp.isStatic()
                                && imp.getName().getIdentifier().equals(name.split("\\.")[0])) {
                            name = imp.getNameAsString() + name.substring(name.split("\\.")[0].length());
                            break;
                        }
                    }
                }
                if (ref.getTypeArguments().isPresent()) {
                    name += "<" + ref.getTypeArguments().get().stream()
                            .map(this::signatureType).collect(Collectors.joining(", ")) + ">";
                }
                return name;
            }
            return type.asString();
        }
    }

    // --- Hierarchy --------------------------------------------------------------

    /** Each type's direct supertypes that are declared in this project. */
    private void linkSupertypes(Index index) {
        for (TypeInfo type : index.all) {
            List<ClassOrInterfaceType> declared = new ArrayList<>();
            if (type.decl instanceof NodeWithExtends<?> ext) {
                declared.addAll(ext.getExtendedTypes());
            }
            if (type.decl instanceof NodeWithImplements<?> impl) {
                declared.addAll(impl.getImplementedTypes());
            }
            for (ClassOrInterfaceType ref : declared) {
                TypeInfo supertype = null;
                try {
                    supertype = ref.resolve().asReferenceType().getTypeDeclaration()
                            .map(d -> index.typeFor(d, type.file.module()))
                            .orElse(null);
                } catch (RuntimeException | StackOverflowError e) {
                    // A supertype whose own supertypes live in a missing jar
                    // can fail to resolve; its simple name is still a strong hint.
                    supertype = index.bySimpleName(ref.getNameAsString(), type);
                }
                if (supertype != null && supertype != type && !type.supertypes.contains(supertype)) {
                    type.supertypes.add(supertype);
                }
            }
        }
    }

    /** All project supertypes, nearest first. */
    private static List<TypeInfo> ancestors(TypeInfo type) {
        List<TypeInfo> out = new ArrayList<>();
        Set<TypeInfo> seen = new HashSet<>();
        seen.add(type);
        List<TypeInfo> frontier = new ArrayList<>(type.supertypes);
        while (!frontier.isEmpty()) {
            List<TypeInfo> next = new ArrayList<>();
            for (TypeInfo t : frontier) {
                if (seen.add(t)) {
                    out.add(t);
                    next.addAll(t.supertypes);
                }
            }
            frontier = next;
        }
        return out;
    }

    /**
     * Connects each interface or abstract method to the project methods that
     * implement it.
     *
     * A controller calls OrderService.create; the body is in
     * OrderServiceImpl.create, and nothing statically links the two, so every
     * flow used to stop at the interface. The link is an edge of kind "call"
     * — deliberately not a new kind: dispatch through an interface is, to
     * someone following a flow, the next call, and the flow walkers on the
     * Go side follow "call" edges and nothing else. A matching call site is
     * recorded on the abstract method, at its own line, for the same reason.
     *
     * Also records what each method overrides, nearest first, which is where
     * an inherited route mapping comes from.
     */
    private Dispatch linkOverrides(Index index, AnalysisResult result) {
        Dispatch dispatch = new Dispatch();
        for (TypeInfo type : index.all) {
            List<TypeInfo> ancestors = ancestors(type);
            for (MethodInfo method : type.methods) {
                MethodDeclaration decl = (MethodDeclaration) method.decl();
                if (decl.isStatic() || decl.isPrivate()) {
                    continue;
                }
                for (TypeInfo ancestor : ancestors) {
                    for (MethodInfo overridden : overriddenIn(ancestor, method)) {
                        dispatch.overridden.computeIfAbsent(method.id(), k -> new ArrayList<>()).add(overridden);
                        if (isDispatchTarget(overridden)) {
                            dispatch.implementations.computeIfAbsent(overridden.id(), k -> new ArrayList<>()).add(method.id());
                            result.edges.add(new Edge(overridden.id(), method.id(), "call"));
                            CallSite link = new CallSite(overridden.id(), method.id(), overridden.line(), "");
                            link.role = "override";
                            result.callSites.add(link);
                        }
                    }
                }
            }
        }
        for (TypeInfo type : index.all) {
            for (MethodInfo method : type.methods) {
                MethodDeclaration decl = (MethodDeclaration) method.decl();
                if (decl.getBody().isEmpty() && !decl.isNative()) {
                    dispatch.abstractMethods.add(method.id());
                }
            }
        }
        return dispatch;
    }

    /**
     * The methods in {@code ancestor} that {@code method} overrides: the one
     * with the same signature, or — because a generic parameter reads "T" in
     * the interface and "Order" in the implementation — the only one with the
     * same name and parameter count.
     */
    private static List<MethodInfo> overriddenIn(TypeInfo ancestor, MethodInfo method) {
        List<MethodInfo> sameShape = new ArrayList<>();
        for (MethodInfo candidate : ancestor.methods) {
            MethodDeclaration decl = (MethodDeclaration) candidate.decl();
            if (decl.isStatic() || decl.isPrivate()
                    || !decl.getNameAsString().equals(method.decl().getNameAsString())
                    || decl.getParameters().size() != method.decl().getParameters().size()) {
                continue;
            }
            if (candidate.signature().equals(method.signature())) {
                return List.of(candidate);
            }
            sameShape.add(candidate);
        }
        return sameShape.size() == 1 ? sameShape : List.of();
    }

    /** A method whose body is chosen at run time from the project's implementations. */
    private static boolean isDispatchTarget(MethodInfo method) {
        MethodDeclaration decl = (MethodDeclaration) method.decl();
        boolean inInterface = method.owner().decl instanceof ClassOrInterfaceDeclaration c && c.isInterface();
        return inInterface || decl.isAbstract();
    }

    // --- Endpoints ----------------------------------------------------------------

    /**
     * Routes, per concrete class: its own handler methods and the ones it
     * inherits from project superclasses, each mapped with whatever the
     * method or anything it overrides declares, under the class's own
     * prefix or its nearest supertype's. The entry node is always a method
     * with a body — a route declared on an interface enters at the
     * controller method implementing it, which is where the flow starts.
     */
    private void extractEndpoints(Index index, Dispatch dispatch, Annotations annotations,
                                  ConstantResolver constants, AnalysisResult result) {
        Map<String, TypeInfo> endpointOwner = new HashMap<>();
        for (TypeInfo type : index.all) {
            if (type.decl instanceof AnnotationDeclaration
                    || (type.decl instanceof ClassOrInterfaceDeclaration c && (c.isInterface() || c.isAbstract()))) {
                continue; // Spring registers neither
            }
            List<TypeInfo> ancestors = ancestors(type);
            List<TypeDeclaration<?>> ancestorDecls = ancestors.stream().<TypeDeclaration<?>>map(t -> t.decl).toList();

            List<MethodInfo> handlers = new ArrayList<>();
            Set<String> overriddenByNearer = new HashSet<>();
            List<TypeInfo> chain = new ArrayList<>();
            chain.add(type);
            for (TypeInfo ancestor : ancestors) {
                if (!(ancestor.decl instanceof ClassOrInterfaceDeclaration c) || !c.isInterface()) {
                    chain.add(ancestor);
                }
            }
            for (TypeInfo owner : chain) {
                for (MethodInfo method : owner.methods) {
                    MethodDeclaration decl = (MethodDeclaration) method.decl();
                    boolean inherited = owner != type;
                    if (decl.getBody().isEmpty() || overriddenByNearer.contains(method.id())
                            || (inherited && (decl.isStatic() || decl.isPrivate()))) {
                        continue;
                    }
                    handlers.add(method);
                    for (MethodInfo o : dispatch.overridden.getOrDefault(method.id(), List.of())) {
                        overriddenByNearer.add(o.id());
                    }
                }
            }

            for (MethodInfo handler : handlers) {
                List<MethodDeclaration> methodChain = new ArrayList<>();
                methodChain.add((MethodDeclaration) handler.decl());
                for (MethodInfo o : dispatch.overridden.getOrDefault(handler.id(), List.of())) {
                    methodChain.add((MethodDeclaration) o.decl());
                }
                for (EndpointExtractor.HttpMapping mapping : EndpointExtractor.extract(
                        type.decl, ancestorDecls, methodChain, annotations, constants)) {
                    addEndpoint(mapping, handler.id(), type, endpointOwner, result);
                }
            }
        }
    }

    /**
     * Endpoint ids are "METHOD path", and the UI addresses an endpoint by
     * that string, so the first class to serve a route keeps exactly that.
     * A second class serving the same route — the same /health in two
     * services of one folder — used to be dropped, silently; it now gets
     * the same id with its service (or, within one service, its class) in
     * brackets. Deterministic, since the classes are visited in path order.
     * The id stays an opaque string; nothing parses it.
     */
    private static void addEndpoint(EndpointExtractor.HttpMapping mapping, String entryNode, TypeInfo type,
                                    Map<String, TypeInfo> endpointOwner, AnalysisResult result) {
        String base = mapping.httpMethod() + " " + mapping.path();
        String id = base;
        TypeInfo existing = endpointOwner.get(id);
        if (existing != null) {
            if (existing == type) {
                return; // one class mapping one route twice: Spring would refuse to start
            }
            String qualifier = !type.file.module().isEmpty() && !type.file.module().equals(existing.file.module())
                    ? type.file.module() : type.decl.getNameAsString();
            id = base + " [" + qualifier + "]";
            for (int n = 2; endpointOwner.containsKey(id); n++) {
                if (endpointOwner.get(id) == type) {
                    return;
                }
                id = base + " [" + qualifier + " #" + n + "]";
            }
        }
        endpointOwner.put(id, type);
        Endpoint endpoint = new Endpoint();
        endpoint.id = id;
        endpoint.httpMethod = mapping.httpMethod();
        endpoint.path = mapping.path();
        endpoint.entryNode = entryNode;
        result.endpoints.add(endpoint);
    }

    /**
     * Outbound HTTP calls to other services (OutboundCallExtractor), each
     * recorded against the method it is made from — the same scopes the call
     * edges use, so a call inside an initializer lands on the constructor
     * that runs it. AST-only: nothing here asks the solver.
     */
    private void extractOutboundCalls(Index index, List<CallScope> scopes, Annotations annotations,
                                      ConstantResolver constants, AnalysisResult result) {
        for (TypeInfo type : index.all) {
            List<OutboundCallExtractor.Found> declarative = new ArrayList<>(OutboundCallExtractor.feign(type.decl, annotations, constants));
            declarative.addAll(OutboundCallExtractor.httpInterface(type.decl, annotations, constants));
            for (OutboundCallExtractor.Found found : declarative) {
                OutboundCall call = found.call();
                call.from = type.methods.stream().filter(m -> m.decl() == found.in())
                        .map(MethodInfo::id).findFirst().orElse(type.id);
                call.file = type.file.relativePath();
                result.outboundCalls.add(call);
            }
        }
        for (CallScope scope : scopes) {
            for (MethodCallExpr expr : scope.node().findAll(MethodCallExpr.class)) {
                if (!belongsTo(expr, scope.node())) {
                    continue;
                }
                OutboundCallExtractor.call(expr, constants).ifPresent(call -> {
                    call.from = scope.ownerIds().get(0);
                    call.file = scope.owner().file.relativePath();
                    result.outboundCalls.add(call);
                });
            }
        }
    }

    // --- Pass 2: calls and field accesses -------------------------------------------

    // Imports and type uses are dependencies even when no method call resolves.
    private void addTypeDependencies(ParsedFile file, Index index, AnalysisResult result) {
        Set<String> targets = new LinkedHashSet<>();
        for (var imp : file.cu().getImports()) {
            if (!imp.isAsterisk() && !imp.isStatic()) {
                targets.add(imp.getNameAsString());
            } else if (imp.isStatic()) {
                String name = imp.getNameAsString();
                targets.add(imp.isAsterisk() ? name : name.substring(0, name.lastIndexOf('.')));
            }
        }
        for (var type : file.cu().findAll(ClassOrInterfaceType.class)) {
            try {
                targets.add(type.resolve().asReferenceType().getQualifiedName());
            } catch (RuntimeException | StackOverflowError ignored) {
                // An unresolved external type has no project file to depend on.
            }
        }
        for (String target : targets) {
            TypeInfo targetType = index.byQualifiedName(target, file.module());
            if (targetType != null && !targetType.file.relativePath().equals(file.relativePath())) {
                result.fileDependencies.add(new FileDependency(file.relativePath(), targetType.file.relativePath()));
            }
        }
    }

    private void addCallEdges(CallScope scope, Index index, Dispatch dispatch, Set<String> nodeIds,
                              Map<String, List<String>> methodsByTypeAndName, AnalysisResult result) {
        String module = scope.owner().file.module();
        Steps.Context steps = Steps.context(scope.node());
        for (MethodCallExpr call : scope.node().findAll(MethodCallExpr.class)) {
            if (deferredCamel(call)) continue;
            if (!belongsTo(call, scope.node())) {
                continue; // a local class's method records its own calls
            }
            // The line of the call's NAME, as its column is: in a chain spread
            // over lines every call would otherwise be on the chain's first.
            int line = Steps.line(call);
            Steps.Info info = stepsOf(call, steps);
            ResolvedMethodDeclaration target = null;
            String toNodeId;
            try {
                target = call.resolve();
                // target.getSignature() can also lazily throw UnsolvedSymbolException if its params are missing
                toNodeId = index.typeIdFor(target.declaringType(), module) + "." + target.getSignature();
            } catch (RuntimeException | StackOverflowError e) {
                // Full resolution needs every argument's type, because that is
                // how an overload is chosen — so one argument the solver cannot
                // follow takes the whole call with it. That is how a controller
                // ends up alone in its own graph: `service.query(request.a(),
                // request.b())` is lost not because `service` is a mystery but
                // because `request` is a record, whose implicit accessors this
                // JavaParser cannot resolve.
                //
                // The receiver usually resolves perfectly well on its own, so
                // ask it what type it is and look for that type's method by
                // name. One match is an answer; several means overloads we have
                // no way to choose between, and none means the callee is not
                // ours. Only methods declared on the type itself, not inherited
                // ones — that is the price of not resolving properly.
                toNodeId = inferTarget(call, index, module, methodsByTypeAndName);
                if (toNodeId == null) {
                    // Lombok writes getters, setters and a `log` field into
                    // the bytecode, never the source, so a call to one cannot
                    // resolve here — but it is not a mystery either, and
                    // calling it "unresolved" would bury the real gaps.
                    String reason = lombokCall(call, scope, index, line, result) ? "lombok" : "unresolved";
                    // Still a gap in the graph, not a fatal error (FR-8).
                    // Recorded rather than dropped: a trace that runs into this
                    // has to be able to say the flow was lost here, not just end.
                    // Every Lombok logger is keyed as slf4j on purpose, @Log4j2 and
                    // @CommonsLog too: the key decides "this is logging", and what
                    // the annotation generates cannot be read from here.
                    String key = reason.equals("lombok") && isLogCall(call)
                            ? "org.slf4j.Logger#" + call.getNameAsString()
                            : calleeKey(call, target, steps);
                    for (String owner : scope.ownerIds()) {
                        result.callSites.add(new CallSite(owner, "", line, reason).describe(info, key));
                    }
                    continue;
                }
            }
            if (!nodeIds.contains(toNodeId)) {
                // Resolved successfully but no node exists for it. Two different
                // walls, and the trace reads very differently depending on which:
                // an interface method has a body somewhere we cannot find, a
                // library method has no body we can ever read.
                boolean isInterface = false;
                if (target != null) {
                    try {
                        isInterface = target.declaringType().isInterface();
                    } catch (RuntimeException | StackOverflowError e) {
                        isInterface = false;
                    }
                }
                String key = calleeKey(call, target, steps);
                for (String owner : scope.ownerIds()) {
                    result.callSites.add(new CallSite(owner, "", line, isInterface ? "interface" : "library").describe(info, key));
                }
                continue;
            }
            // An abstract method nothing in the project implements is the
            // same wall as an interface from a library: the edge to it is
            // real, but the flow cannot go past it.
            boolean dangling = dispatch.abstractMethods.contains(toNodeId)
                    && !dispatch.implementations.containsKey(toNodeId);
            recordCall(scope, toNodeId, line, dangling ? "interface" : "", index, result, Guards.of(call, scope.node()),
                    info, keyOfNodeId(toNodeId));
        }

        // `new Foo(...)`, `this(...)` and `super(...)` run a constructor. Only
        // a constructor that is a node is worth recording: a JDK one (new
        // ArrayList<>()) or a class's implicit one would only add noise.
        List<com.github.javaparser.ast.Node> constructions = new ArrayList<>();
        constructions.addAll(scope.node().findAll(ObjectCreationExpr.class));
        constructions.addAll(scope.node().findAll(ExplicitConstructorInvocationStmt.class));
        constructions.sort((a, b) -> Integer.compare(
                a.getRange().map(r -> r.begin.line).orElse(0), b.getRange().map(r -> r.begin.line).orElse(0)));
        for (com.github.javaparser.ast.Node construction : constructions) {
            if (deferredCamel(construction)) continue;
            if (!belongsTo(construction, scope.node())) {
                continue;
            }
            String toNodeId;
            try {
                var ctor = construction instanceof ObjectCreationExpr oce ? oce.resolve()
                        : ((ExplicitConstructorInvocationStmt) construction).resolve();
                toNodeId = index.typeIdFor(ctor.declaringType(), module) + "." + ctor.getSignature();
            } catch (RuntimeException | StackOverflowError e) {
                continue;
            }
            if (nodeIds.contains(toNodeId)) {
                recordCall(scope, toNodeId, construction.getRange().map(r -> r.begin.line).orElse(0), "", index, result,
                        Guards.of(construction, scope.node()), stepsOf(construction, steps), keyOfNodeId(toNodeId));
            }
        }

        // `Foo::bar` names a method that WILL run — `.map(Mapper::toDto)` is
        // where the flow goes next, and the place someone asking "who uses
        // toDto" is looking for. It was invisible: not a MethodCallExpr, so no
        // edge and no call site. Recorded like a constructor, and for the same
        // reason: only when the target is a node. `String::valueOf` and
        // `Objects::nonNull` are everywhere, and a wall for each would bury the
        // gaps that matter; so would one for every reference the solver cannot
        // follow, which it cannot more often than it can a call.
        for (MethodReferenceExpr reference : scope.node().findAll(MethodReferenceExpr.class)) {
            if (deferredCamel(reference)) continue;
            if (!belongsTo(reference, scope.node())) {
                continue;
            }
            String toNodeId = methodReferenceTarget(reference, index, module, methodsByTypeAndName);
            if (toNodeId == null || !nodeIds.contains(toNodeId)) {
                continue;
            }
            boolean dangling = dispatch.abstractMethods.contains(toNodeId)
                    && !dispatch.implementations.containsKey(toNodeId);
            recordCall(scope, toNodeId, reference.getRange().map(r -> r.begin.line).orElse(0),
                    dangling ? "interface" : "", index, result, Guards.of(reference, scope.node()),
                    stepsOf(reference, steps), keyOfNodeId(toNodeId));
        }
    }

    private boolean deferredCamel(com.github.javaparser.ast.Node node) {
        for (var n = node; n != null; n = n.getParentNode().orElse(null)) {
            if (deferredCamelCallbacks.contains(n)) return true;
        }
        return false;
    }

    /**
     * The node id a method reference names, or null when it cannot be told.
     *
     * Asked of the solver first. It fails on a reference more readily than on
     * a call — which overload `Foo::bar` means depends on the functional
     * interface it is being converted to, and that is the inference JavaParser
     * is weakest at — so the fallback is the one a call has: the type on the
     * left, and a method of that name declared on it, if there is exactly one.
     * `Foo::new` is the same question about Foo's constructors, which are
     * method nodes named after their class.
     */
    private String methodReferenceTarget(MethodReferenceExpr reference, Index index, String module,
                                         Map<String, List<String>> methodsByTypeAndName) {
        boolean constructor = reference.getIdentifier().equals("new");
        if (!constructor) {
            try {
                ResolvedMethodDeclaration target = reference.resolve();
                return index.typeIdFor(target.declaringType(), module) + "." + target.getSignature();
            } catch (RuntimeException | StackOverflowError e) {
                // Inferred below.
            }
        }
        String declaringType = receiverTypeId(reference.getScope(), index, module);
        if (declaringType == null) {
            return null;
        }
        String name = constructor ? simpleNameOf(declaringType) : reference.getIdentifier();
        List<String> candidates = methodsByTypeAndName.get(declaringType + "." + name);
        return candidates != null && candidates.size() == 1 ? candidates.get(0) : null;
    }

    /** A type id's simple name: past the last dot, before any " [module]" the index added to tell two apart. */
    private static String simpleNameOf(String typeId) {
        int suffix = typeId.indexOf(" [");
        String qualified = suffix >= 0 ? typeId.substring(0, suffix) : typeId;
        return qualified.substring(qualified.lastIndexOf('.') + 1);
    }

    /**
     * A call's step, or only its position when reading it failed: the outline
     * is extra, and one odd expression must not cost the index its call graph.
     */
    static Steps.Info stepsOf(com.github.javaparser.ast.Node node, Steps.Context steps) {
        return stepsOf(node, n -> Steps.of(n, steps));
    }

    /** {@link #stepsOf(com.github.javaparser.ast.Node, Steps.Context)} with the reader passed in, for a test to break. */
    static Steps.Info stepsOf(com.github.javaparser.ast.Node node,
                              java.util.function.Function<com.github.javaparser.ast.Node, Steps.Info> reader) {
        try {
            return reader.apply(node);
        } catch (RuntimeException | StackOverflowError e) {
            return Steps.Info.empty(node);
        }
    }

    /**
     * "<declaring type>#<member>": what scores a call and what a person marks
     * "always show". The solver's answer first; then the receiver's resolved
     * type; then the type the receiver was declared with; then a name or field
     * as written. A call with no receiver is keyed by the class it is written
     * in; one whose receiver is itself an expression (`make().bar()`) has no
     * key, since nothing says whose method it is.
     */
    private static String calleeKey(MethodCallExpr call, ResolvedMethodDeclaration target, Steps.Context steps) {
        String name = call.getNameAsString();
        if (target != null) {
            try {
                return target.declaringType().getQualifiedName() + "#" + name;
            } catch (RuntimeException | StackOverflowError ignored) {
                // falls through
            }
        }
        if (call.getScope().isEmpty()) {
            return Steps.enclosingType(steps) + "#" + name;
        }
        Expression receiver = call.getScope().get();
        try {
            var type = receiver.calculateResolvedType();
            if (type.isReferenceType()) {
                return type.asReferenceType().getQualifiedName() + "#" + name;
            }
        } catch (RuntimeException | StackOverflowError ignored) {
            // falls through
        }
        String declared = Steps.declaredType(receiver, steps);
        if (declared != null) {
            return declared + "#" + name;
        }
        Expression written = Steps.unwrap(receiver);
        if (written instanceof NameExpr || written instanceof FieldAccessExpr) {
            return Steps.clip(Guards.flatten(written.toString())) + "#" + name;
        }
        return "";
    }

    /**
     * A node id's key: `acme.Base.decide(java.lang.String)` → `acme.Base#decide`.
     * The " [..]" the index adds to tell two types apart is dropped, so a
     * project key reads like the solver's.
     */
    static String keyOfNodeId(String id) {
        int paren = id.indexOf('(');
        String head = (paren >= 0 ? id.substring(0, paren) : id).replaceAll(" \\[[^\\]]*\\]", "");
        int dot = head.lastIndexOf('.');
        return dot < 0 ? "#" + head : head.substring(0, dot) + "#" + head.substring(dot + 1);
    }

    private static boolean isLogCall(MethodCallExpr call) {
        return call.getScope().filter(s -> s instanceof NameExpr n && n.getNameAsString().equals("log")).isPresent();
    }

    private void recordCall(CallScope scope, String toNodeId, int line, String unresolved, Index index,
                            AnalysisResult result, String guard, Steps.Info info, String calleeKey) {
        for (String owner : scope.ownerIds()) {
            // A wall keeps the shape every other wall has: no callee named.
            result.callSites.add(new CallSite(owner, unresolved.isEmpty() ? toNodeId : "", line, unresolved)
                    .describe(info, calleeKey));
            result.edges.add(new Edge(owner, toNodeId, "call", guard));
        }

        // File-level dependency: only when the callee is declared in this
        // project's own source (a dependency on an external file is
        // meaningless for blast radius). Same-file calls are skipped — a file
        // never depends on itself.
        int dot = toNodeId.lastIndexOf('.', toNodeId.indexOf('('));
        TypeInfo targetType = dot > 0 ? index.byId.get(toNodeId.substring(0, dot)) : null;
        String from = scope.owner().file.relativePath();
        if (targetType != null && !targetType.file.relativePath().equals(from)) {
            result.fileDependencies.add(new FileDependency(from, targetType.file.relativePath()));
        }
    }

    /**
     * True when {@code node} runs as part of {@code scope} itself, not as
     * part of some other indexed member nested inside it. A local class's
     * methods are their own nodes and record their own calls; counting those
     * calls against the enclosing method as well, as a plain findAll did,
     * put every one of them in the graph twice. An anonymous class's methods
     * are not nodes, so their calls stay with the method that creates it.
     */
    private static boolean belongsTo(com.github.javaparser.ast.Node node, com.github.javaparser.ast.Node scope) {
        Optional<com.github.javaparser.ast.Node> current = node.getParentNode();
        while (current.isPresent()) {
            com.github.javaparser.ast.Node n = current.get();
            if (n == scope) {
                return true;
            }
            if (n instanceof TypeDeclaration<?>) {
                return false;
            }
            boolean member = n instanceof MethodDeclaration || n instanceof ConstructorDeclaration
                    || n instanceof InitializerDeclaration || n instanceof FieldDeclaration;
            if (member && n.getParentNode().filter(p -> p instanceof TypeDeclaration<?>).isPresent()) {
                return false;
            }
            current = n.getParentNode();
        }
        return false;
    }

    /**
     * The callee of a call whose arguments would not resolve, found from the
     * receiver instead: its node id, or null when the receiver has no type we
     * can name or the name is not uniquely a method of it.
     */
    private String inferTarget(MethodCallExpr call, Index index, String module,
                               Map<String, List<String>> methodsByTypeAndName) {
        Optional<Expression> scope = call.getScope();
        if (scope.isEmpty()) {
            return null; // an unqualified call — this method's own class, already resolvable or already lost
        }
        String declaringType = receiverTypeId(scope.get(), index, module);
        if (declaringType == null) {
            return null;
        }
        List<String> candidates = methodsByTypeAndName.get(declaringType + "." + call.getNameAsString());
        if (candidates == null || candidates.size() != 1) {
            return null;
        }
        return candidates.get(0);
    }

    /**
     * The index's id for the type of a receiver — the `service` of
     * `service.query(...)`, the `Mapper` of `Mapper::toDto` — or null when it
     * has no type we can name. A type written out (the left of `Mapper::toDto`)
     * is resolved as the type it is; anything else is an expression, and is
     * asked what it evaluates to.
     */
    private String receiverTypeId(Expression receiver, Index index, String module) {
        try {
            ResolvedType type = receiver instanceof TypeExpr written
                    ? written.getType().resolve()
                    : receiver.calculateResolvedType();
            if (!type.isReferenceType()) {
                return null;
            }
            var typeDecl = type.asReferenceType().getTypeDeclaration();
            return typeDecl.isPresent()
                    ? index.typeIdFor(typeDecl.get(), module)
                    : type.asReferenceType().getQualifiedName();
        } catch (RuntimeException | StackOverflowError e) {
            return null; // the receiver is a mystery too; nothing to infer from
        }
    }

    /**
     * True when an unresolvable call is one Lombok generates: `log.x(...)` in
     * a class with a Lombok logger annotation, or a getter/setter of a field
     * of a project class Lombok generates accessors for. An accessor is
     * recorded as the field access it stands for, so a data-flow trace over
     * that field still sees it.
     */
    private boolean lombokCall(MethodCallExpr call, CallScope scope, Index index, int line, AnalysisResult result) {
        Optional<Expression> receiver = call.getScope();
        if (receiver.isPresent() && receiver.get() instanceof NameExpr name && name.getNameAsString().equals("log")) {
            Optional<com.github.javaparser.ast.Node> current = call.getParentNode();
            while (current.isPresent()) {
                if (current.get() instanceof TypeDeclaration<?> t
                        && t.getAnnotations().stream().anyMatch(a -> LOMBOK_LOGGERS.contains(Annotations.simpleName(a)))) {
                    return true;
                }
                current = current.get().getParentNode();
            }
            return false;
        }

        String method = call.getNameAsString();
        String prefix = method.startsWith("get") ? "get" : method.startsWith("is") ? "is"
                : method.startsWith("set") ? "set" : null;
        if (prefix == null || method.length() == prefix.length()) {
            return false;
        }
        boolean setter = prefix.equals("set");
        if (setter ? call.getArguments().size() != 1 : !call.getArguments().isEmpty()) {
            return false;
        }
        String property = method.substring(prefix.length());
        String fieldName = Character.toLowerCase(property.charAt(0)) + property.substring(1);

        TypeInfo receiverType = null;
        if (receiver.isEmpty() || receiver.get() instanceof ThisExpr) {
            receiverType = scope.owner();
        } else {
            try {
                ResolvedType type = receiver.get().calculateResolvedType();
                if (type.isReferenceType()) {
                    receiverType = type.asReferenceType().getTypeDeclaration()
                            .map(d -> index.typeFor(d, scope.owner().file.module()))
                            .orElse(null);
                }
            } catch (RuntimeException | StackOverflowError e) {
                return false;
            }
        }
        if (receiverType == null) {
            return false;
        }
        List<TypeInfo> lookIn = new ArrayList<>();
        lookIn.add(receiverType);
        lookIn.addAll(ancestors(receiverType));
        for (TypeInfo type : lookIn) {
            for (FieldDeclaration field : type.decl.getFields()) {
                if (field.getVariables().stream().noneMatch(v -> v.getNameAsString().equals(fieldName))) {
                    continue;
                }
                Set<String> generators = setter ? LOMBOK_SETTERS : LOMBOK_GETTERS;
                boolean generated = type.decl.getAnnotations().stream()
                        .anyMatch(a -> generators.contains(Annotations.simpleName(a)))
                        || field.getAnnotations().stream()
                        .anyMatch(a -> Annotations.simpleName(a).equals(setter ? "Setter" : "Getter"));
                if (!generated) {
                    return false;
                }
                for (String owner : scope.ownerIds()) {
                    result.fieldAccesses.add(new FieldAccess(type.id, owner, fieldName, setter ? "write" : "read", line));
                }
                return true;
            }
        }
        return false;
    }

    private void addFieldAccesses(CallScope scope, Index index, AnalysisResult result) {
        for (FieldAccessExpr fae : scope.node().findAll(FieldAccessExpr.class)) {
            if (!belongsTo(fae, scope.node())) {
                continue;
            }
            try {
                recordFieldAccess(fae.resolve(), fae, scope, index, result);
            } catch (RuntimeException | StackOverflowError ignored) {
            }
        }
        for (NameExpr ne : scope.node().findAll(NameExpr.class)) {
            if (!belongsTo(ne, scope.node())) {
                continue;
            }
            try {
                recordFieldAccess(ne.resolve(), ne, scope, index, result);
            } catch (RuntimeException | StackOverflowError ignored) {
            }
        }
    }

    private void recordFieldAccess(ResolvedValueDeclaration resolved, Expression expr, CallScope scope, Index index,
                                   AnalysisResult result) {
        if (resolved != null && resolved.isField()) {
            var field = resolved.asField();
            String classId;
            try {
                classId = index.typeIdFor(field.declaringType(), scope.owner().file.module());
            } catch (RuntimeException | StackOverflowError e) {
                return;
            }
            String fieldName = field.getName();
            int line = expr.getRange().map(r -> r.begin.line).orElse(0);
            for (String kind : accessKinds(expr)) {
                for (String owner : scope.ownerIds()) {
                    result.fieldAccesses.add(new FieldAccess(classId, owner, fieldName, kind, line));
                }
            }
        }
    }

    /**
     * How an expression uses the field it names. `x = 1` only writes; `x += 1`
     * and `x++` read the old value and write a new one, so they are both — a
     * def-use trace that saw only the write would claim the previous value
     * was never used.
     */
    private static List<String> accessKinds(Expression expr) {
        com.github.javaparser.ast.Node current = expr;
        while (current.getParentNode().isPresent() && current.getParentNode().get() instanceof EnclosedExpr) {
            current = current.getParentNode().get();
        }
        Optional<com.github.javaparser.ast.Node> parentOpt = current.getParentNode();
        if (parentOpt.isPresent()) {
            com.github.javaparser.ast.Node parent = parentOpt.get();
            if (parent instanceof AssignExpr assign && assign.getTarget() == current) {
                return assign.getOperator() == AssignExpr.Operator.ASSIGN
                        ? List.of("write") : List.of("read", "write");
            }
            if (parent instanceof UnaryExpr unary && unary.getExpression() == current) {
                switch (unary.getOperator()) {
                    case PREFIX_INCREMENT, POSTFIX_INCREMENT, PREFIX_DECREMENT, POSTFIX_DECREMENT -> {
                        return List.of("read", "write");
                    }
                    default -> {}
                }
            }
        }
        return List.of("read");
    }

    // The same target called repeatedly in one body yields identical edges;
    // keep one of each from/to/kind triple, preserving order. One pass
    // at the end instead of a contains-check per added edge keeps large
    // projects linear rather than quadratic. The guards of the calls it
    // stands for are merged (Guards.merge): called once unconditionally and
    // once under a flag, the edge is unconditional.
    private static List<Edge> dedupeEdges(List<Edge> edges) {
        Map<String, Edge> byKey = new LinkedHashMap<>();
        Map<String, List<String>> guards = new HashMap<>();
        for (Edge e : edges) {
            String key = e.from + "\n" + e.to + "\n" + e.kind;
            byKey.putIfAbsent(key, e);
            guards.computeIfAbsent(key, k -> new ArrayList<>()).add(e.guard);
        }
        byKey.forEach((key, e) -> e.guard = Guards.merge(guards.get(key)));
        return new ArrayList<>(byKey.values());
    }

    private static List<FileDependency> dedupeFileDependencies(List<FileDependency> deps) {
        Map<String, FileDependency> byKey = new LinkedHashMap<>();
        for (FileDependency fd : deps) {
            byKey.putIfAbsent(fd.filePath + "\n" + fd.dependsOnFilePath, fd);
        }
        return new ArrayList<>(byKey.values());
    }

    private Node classNode(TypeDeclaration<?> classDecl, String id, String layer, String relativePath, Optional<Range> range) {
        Node node = new Node();
        node.id = id;
        node.label = id;
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

    // --- The type index ---------------------------------------------------------------

    /**
     * Every project type, and the one place node ids are handed out.
     *
     * A class's id is its qualified name — except that a folder of services
     * routinely holds the same class twice (every service has its own
     * com.acme.Application), and two nodes with one id violated the store's
     * primary key and took the whole index down with nothing indexed. So an
     * id is unique by construction: the first type seen keeps its plain
     * qualified name, a later one with the same name is qualified with its
     * module ("com.acme.Application [services/billing]"), and a numbered
     * suffix settles anything still colliding. A local class has no
     * qualified name at all and is named for the member it is declared in
     * ("com.acme.Outer.run$Helper"), so two local Helpers in two methods no
     * longer share one.
     *
     * References go the other way through typeIdFor: when the solver names a
     * class that two modules declare, the one in the caller's own module is
     * preferred — the solver itself simply returns whichever root it saw
     * first, which for the second service is the wrong one.
     */
    private static final class Index {
        final List<TypeInfo> all = new ArrayList<>();
        final Map<String, TypeInfo> byId = new HashMap<>();
        private final IdentityHashMap<TypeDeclaration<?>, TypeInfo> byDecl = new IdentityHashMap<>();
        private final Map<String, List<TypeInfo>> byQualifiedName = new HashMap<>();
        private final Map<String, List<TypeInfo>> bySimpleName = new HashMap<>();
        private final Set<String> usedIds = new HashSet<>();

        TypeInfo declare(TypeDeclaration<?> decl, ParsedFile file, String layer) {
            String qualifiedName = null;
            String base;
            if (isLocal(decl)) {
                base = localBaseId(decl);
            } else {
                try {
                    qualifiedName = decl.resolve().getQualifiedName();
                } catch (RuntimeException | StackOverflowError e) {
                    // If resolving the declaration fails, fall back to the AST's fully-qualified name
                    qualifiedName = decl.getFullyQualifiedName().orElse(decl.getNameAsString());
                }
                base = qualifiedName;
            }
            TypeInfo info = new TypeInfo(decl, uniqueId(base, file.module()), file, layer);
            all.add(info);
            byId.put(info.id, info);
            byDecl.put(decl, info);
            if (qualifiedName != null) {
                byQualifiedName.computeIfAbsent(qualifiedName, k -> new ArrayList<>()).add(info);
            }
            bySimpleName.computeIfAbsent(decl.getNameAsString(), k -> new ArrayList<>()).add(info);
            return info;
        }

        /** Declared inside a method, constructor or initializer rather than only inside other types. */
        private static boolean isLocal(TypeDeclaration<?> decl) {
            Optional<com.github.javaparser.ast.Node> current = decl.getParentNode();
            while (current.isPresent() && !(current.get() instanceof CompilationUnit)) {
                if (!(current.get() instanceof TypeDeclaration<?>)) {
                    return true;
                }
                current = current.get().getParentNode();
            }
            return false;
        }

        private String localBaseId(TypeDeclaration<?> decl) {
            com.github.javaparser.ast.Node parent = decl.getParentNode().orElse(null);
            if (parent instanceof TypeDeclaration<?> outer && byDecl.containsKey(outer)) {
                return byDecl.get(outer).id + "." + decl.getNameAsString(); // a member of a local class
            }
            String member = "init";
            TypeInfo enclosing = null;
            Optional<com.github.javaparser.ast.Node> current = decl.getParentNode();
            boolean memberFound = false;
            while (current.isPresent()) {
                com.github.javaparser.ast.Node n = current.get();
                if (!memberFound && n instanceof CallableDeclaration<?> callable) {
                    member = callable.getNameAsString();
                    memberFound = true;
                }
                if (n instanceof TypeDeclaration<?> t && byDecl.containsKey(t)) {
                    enclosing = byDecl.get(t);
                    break;
                }
                current = n.getParentNode();
            }
            String prefix = enclosing == null ? "" : enclosing.id + ".";
            return prefix + member + "$" + decl.getNameAsString();
        }

        /** The one type with this qualified name, or null when none or many. */
        TypeInfo oneByQualifiedName(String name) {
            return only(byQualifiedName.get(name));
        }

        /** The one type with this simple name, or null when none or many. */
        TypeInfo oneBySimpleName(String name) {
            return only(bySimpleName.get(name));
        }

        private static TypeInfo only(List<TypeInfo> candidates) {
            return candidates != null && candidates.size() == 1 ? candidates.get(0) : null;
        }

        /**
         * base itself when unused; otherwise base qualified with the module,
         * then numbered. Brackets and the characters the Go side splits ids
         * on ('.' and '(') are kept out of the qualifier, so a method id still
         * reads "<class id>.<name>(<params>)".
         */
        String uniqueId(String base, String module) {
            if (usedIds.add(base)) {
                return base;
            }
            String qualifier = module.replaceAll("[.()\\[\\]]", "_");
            if (!qualifier.isEmpty() && usedIds.add(base + " [" + qualifier + "]")) {
                return base + " [" + qualifier + "]";
            }
            for (int n = 2; ; n++) {
                String candidate = base + " [" + (qualifier.isEmpty() ? "" : qualifier + " ") + "#" + n + "]";
                if (usedIds.add(candidate)) {
                    return candidate;
                }
            }
        }

        /** The project type a resolved declaration refers to, or null for a library type. */
        TypeInfo typeFor(ResolvedTypeDeclaration decl, String callerModule) {
            try {
                Optional<com.github.javaparser.ast.Node> ast = decl.toAst();
                if (ast.isPresent() && ast.get() instanceof TypeDeclaration<?> t && byDecl.containsKey(t)) {
                    return byDecl.get(t);
                }
            } catch (RuntimeException | StackOverflowError ignored) {
                // no AST behind it (a jar or reflection type) — look it up by name
            }
            return byQualifiedName(decl.getQualifiedName(), callerModule);
        }

        /**
         * The node id for a resolved type: a project type's id, or, for a
         * library type, its qualified name — which matches no node, and so
         * reads as "library" wherever it is used.
         */
        String typeIdFor(ResolvedTypeDeclaration decl, String callerModule) {
            TypeInfo type = typeFor(decl, callerModule);
            return type != null ? type.id : decl.getQualifiedName();
        }

        TypeInfo byQualifiedName(String qualifiedName, String callerModule) {
            List<TypeInfo> candidates = byQualifiedName.get(qualifiedName);
            if (candidates == null) {
                return null;
            }
            for (TypeInfo t : candidates) {
                if (t.file.module().equals(callerModule)) {
                    return t;
                }
            }
            return candidates.get(0);
        }

        /** A project type by simple name, when the name alone picks one (own module first). */
        TypeInfo bySimpleName(String simpleName, TypeInfo from) {
            List<TypeInfo> candidates = bySimpleName.getOrDefault(simpleName, List.of()).stream()
                    .filter(t -> t != from).toList();
            if (candidates.size() == 1) {
                return candidates.get(0);
            }
            List<TypeInfo> sameModule = candidates.stream()
                    .filter(t -> t.file.module().equals(from.file.module())).toList();
            return sameModule.size() == 1 ? sameModule.get(0) : null;
        }
    }

    private static final class TypeInfo {
        final TypeDeclaration<?> decl;
        final String id;
        final ParsedFile file;
        final String layer;
        /** Declared methods, constructors excluded (they are never overridden). */
        final List<MethodInfo> methods = new ArrayList<>();
        /** Direct supertypes declared in this project. */
        final List<TypeInfo> supertypes = new ArrayList<>();

        TypeInfo(TypeDeclaration<?> decl, String id, ParsedFile file, String layer) {
            this.decl = decl;
            this.id = id;
            this.file = file;
            this.layer = layer;
        }
    }

    /** signature is "name(params)", the part of the id after the type's id. */
    private record MethodInfo(CallableDeclaration<?> decl, String id, String signature, TypeInfo owner, int line) {}

    private static final class Dispatch {
        /** Method id to the project methods it overrides, nearest first. */
        final Map<String, List<MethodInfo>> overridden = new HashMap<>();
        /** Interface/abstract method id to the ids of the project methods implementing it. */
        final Map<String, List<String>> implementations = new HashMap<>();
        /** Ids of methods with no body (interface and abstract methods). */
        final Set<String> abstractMethods = new HashSet<>();
    }

    private record ParsedFile(CompilationUnit cu, String relativePath, String module) {}

    /**
     * A stretch of code whose calls and field accesses are recorded against
     * ownerIds: a method or constructor against itself, an initializer
     * against the constructors that run it or the class.
     */
    private record CallScope(com.github.javaparser.ast.Node node, List<String> ownerIds, TypeInfo owner) {}
}
