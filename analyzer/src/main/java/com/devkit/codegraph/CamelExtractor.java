package com.devkit.codegraph;

import com.devkit.codegraph.ConfigExtractor.Owner;
import com.devkit.codegraph.ConfigExtractor.Member;
import com.github.javaparser.ast.body.*;
import com.github.javaparser.ast.expr.*;
import com.github.javaparser.ast.stmt.ReturnStmt;
import java.util.*;

/**
 * Bounded, source-only interpretation of Camel's Java DSL. A route is a runtime
 * callback root, never an invocation by configure(). Bindings carry the AST and
 * its lexical environment across project helpers; no service/config code runs.
 */
final class CamelExtractor {
    private record Bound(Expression expr, Map<String, Bound> env) {}
    private record Method(Owner owner, Member member) {}
    private record Source(String uri, String topic, String group, boolean kafka) {}
    private record Setting(MethodCallExpr call, Map<String, Bound> env, String guard) {}
    private final List<Owner> owners;
    private final ConstantResolver constants;
    private final AnalysisResult result;
    private final Map<CallableDeclaration<?>, Method> methods = new IdentityHashMap<>();
    private final Set<com.github.javaparser.ast.Node> deferred = Collections.newSetFromMap(new IdentityHashMap<>());
    private final Set<String> routeKeys = new HashSet<>();
    private final Map<String, Integer> routeIds = new HashMap<>();
    private final Map<String, List<String>> internalRoutes = new HashMap<>();
    private final Map<String, List<String>> internalSends = new HashMap<>();
    private Owner routeOwner;
    private int helperVisits;
    private static final int LIMIT = 16;

    CamelExtractor(List<Owner> owners, ConstantResolver constants, AnalysisResult result) {
        this.owners = owners;
        this.constants = constants;
        this.result = result;
        for (Owner o : owners) for (Member m : o.methods()) methods.put(m.decl(), new Method(o, m));
    }

    Set<com.github.javaparser.ast.Node> extract() {
        for (Owner owner : owners) {
            if (!(owner.decl() instanceof ClassOrInterfaceDeclaration c) || c.isAbstract()
                    || !inherits(owner, "org.apache.camel.builder.RouteBuilder", new HashSet<>())) continue;
            Method configure = method(owner, "configure", 0, new HashSet<>());
            if (configure != null) visit(configure, Map.of(), owner, configure.member().id(), new HashSet<>(), 0);
        }
        for (var send : internalSends.entrySet()) {
            List<String> targets = internalRoutes.getOrDefault(send.getKey(), List.of());
            if (targets.size() == 1 && !send.getKey().contains("{")) {
                for (String caller : send.getValue()) result.edges.add(new Edge(caller, targets.get(0), "call"));
            } else warn("Camel internal destination is unresolved/ambiguous: " + send.getKey());
        }
        return deferred;
    }

    private void warn(String text) {
        if (!result.entryPointWarnings.contains(text)) result.entryPointWarnings.add(text);
    }

    private void visit(Method method, Map<String, Bound> env, Owner routeOwner, String context,
                       Set<String> stack, int depth) {
        this.routeOwner = routeOwner;
        if (++helperVisits > 10000) { warn("Camel project helper traversal limit reached"); return; }
        if (depth > LIMIT || !stack.add(method.member().id())) {
            warn("Camel helper recursion/depth limit: " + method.member().id()); return;
        }
        List<MethodCallExpr> calls = method.member().decl().findAll(MethodCallExpr.class).stream()
                .filter(c -> c.findAncestor(CallableDeclaration.class).orElse(null) == method.member().decl())
                .filter(c -> c.findAncestor(LambdaExpr.class).isEmpty()).toList();
        int ordinal = 0;
        for (MethodCallExpr call : calls) {
            int position = ordinal++;
            if (camelFrom(call, method.owner())) {
                route(call, env, routeOwner, context + "/from-" + position);
                continue;
            }
            // Only standalone project calls can register routes. Fluent DSL calls
            // and argument factories are handled at their own consumption site.
            if (call.getParentNode().orElse(null) instanceof com.github.javaparser.ast.stmt.ExpressionStmt) {
                Method helper = target(call, env, 0);
                if (helper != null) visit(helper, arguments(helper, call, env), routeOwner,
                        context + "/call-" + position, new HashSet<>(stack), depth + 1);
            }
        }
    }

    private boolean camelFrom(MethodCallExpr call, Owner owner) {
        if (!call.getNameAsString().equals("from") || call.getArguments().size() != 1) return false;
        if (!inherits(owner, "org.apache.camel.builder.RouteBuilder", new HashSet<>())) return false;
        if (call.getScope().isPresent() && !(call.getScope().get() instanceof ThisExpr)
                && !(call.getScope().get() instanceof SuperExpr)) return false;
        try {
            return call.resolve().declaringType().getQualifiedName().startsWith("org.apache.camel.");
        } catch (RuntimeException | StackOverflowError ignored) {
            // Missing dependency jars are normal. The source-proven Camel base
            // is sufficient only if no project overload shadows from(...).
            return method(owner, "from", 1, new HashSet<>()) == null;
        }
    }

    private void route(MethodCallExpr from, Map<String, Bound> env, Owner owner, String key) {
        if (!routeKeys.add(owner.id() + key)) return;
        routeOwner = owner;
        Source source = source(from.getArgument(0), env, 0);
        List<MethodCallExpr> chain = new ArrayList<>();
        com.github.javaparser.ast.Node n = from;
        while (n.getParentNode().orElse(null) instanceof MethodCallExpr c && c.getScope().orElse(null) == n) {
            chain.add(c); n = c;
        }
        String routeId = "";
        for (MethodCallExpr c : chain) if (c.getNameAsString().equals("routeId") && c.getArguments().size() == 1)
            routeId = value(c.getArgument(0), env, 0);
        // Path + declaring signature + lexical call path: stable across line
        // movement, separates duplicate IDs and helper invocations. Runtime loops
        // remain one template. A synthetic method can use existing flow traversal.
        String id = owner.id() + ".<camel:" + key + ">";
        Node node = new Node();
        node.id = id; node.kind = "method"; node.layer = "controller";
        node.label = owner.id() + ".route " + (routeId.isEmpty() ? source.uri() : routeId);
        // Owners have normalized project-relative paths, even for an inherited helper.
        node.filePath = ownerOf(from).map(Owner::file).orElse(owner.file());
        node.line = from.getBegin().map(p -> p.line).orElse(0);
        node.endLine = n.getEnd().map(p -> p.line).orElse(node.line);
        node.summary = "Camel route template; endpoint/configuration expressions are not runtime values.";
        result.nodes.add(node);
        boolean internal = source.uri().startsWith("direct:") || source.uri().startsWith("seda:");
        if (internal) internalRoutes.computeIfAbsent(source.uri(), k -> new ArrayList<>()).add(id);
        if (!internal) {
            EntryPoint ep = new EntryPoint(); ep.id = id; ep.entryNode = id;
            ep.kind = source.kafka() ? "message" : "camel"; ep.verb = source.kafka() ? "MESSAGE" : "ROUTE";
            ep.target = source.kafka() ? "kafka:" + source.topic() : source.uri();
            ep.owner = owner.id(); ep.group = owner.decl().getNameAsString();
            ep.routeId = routeId; ep.topic = source.topic(); ep.consumerGroup = source.group();
            result.entryPoints.add(ep);
            if (!source.kafka()) warn("Camel source component is not classified: " + source.uri());
        }
        if (!routeId.isEmpty() && routeIds.merge(routeId, 1, Integer::sum) > 1)
            warn("Duplicate Camel route ID/template: " + routeId + " (entries retain separate source identities)");
        if (source.kafka()) {
            KafkaAccess access = new KafkaAccess(); access.from = id; access.topic = source.topic();
            access.groupId = source.group(); access.role = "consume"; access.client = "camel-kafka";
            access.file = node.filePath; access.line = node.line; result.kafkaAccesses.add(access);
        }
        int handlers = 0;
        for (MethodCallExpr c : chain) {
            if (c.getNameAsString().equals("process") && c.getArguments().size() == 1 && camelDsl(c)) {
                handlers += handler(c.getArgument(0), env, id, 0);
            } else if (Set.of("to", "toD").contains(c.getNameAsString()) && c.getArguments().size() == 1 && camelDsl(c)) {
                Source dest = source(c.getArgument(0), env, 0);
                if (dest.uri().startsWith("direct:") || dest.uri().startsWith("seda:")) {
                    internalSends.computeIfAbsent(dest.uri(), k -> new ArrayList<>()).add(id);
                    handlers++;
                }
                if (dest.kafka()) {
                    KafkaAccess a = new KafkaAccess(); a.from = id; a.topic = dest.topic(); a.role = "produce";
                    a.client = "camel-kafka"; a.file = node.filePath; a.line = c.getBegin().map(p -> p.line).orElse(0);
                    result.kafkaAccesses.add(a);
                }
            }
        }
        if (handlers == 0) {
            warn("Camel route handler is unresolved or absent: " + node.label);
            result.callSites.add(new CallSite(id, "", node.line, "unresolved"));
        }
    }

    private boolean camelDsl(MethodCallExpr call) {
        try {
            var declaring = call.resolve().declaringType().getQualifiedName();
            return declaring.startsWith("org.apache.camel.");
        } catch (RuntimeException | StackOverflowError ignored) {
            // The chain is rooted in a verified Camel from(...); unresolved
            // fluent calls are normal when dependency jars are unavailable.
            return true;
        }
    }

    private int handler(Expression expr, Map<String, Bound> env, String route, int depth) {
        if (depth > LIMIT) return 0;
        if (unstable(expr)) return 0;
        Bound b = binding(expr, env);
        if (b != null) return handler(b.expr(), b.env(), route, depth + 1);
        if (expr instanceof EnclosedExpr e) return handler(e.getInner(), env, route, depth + 1);
        if (expr instanceof CastExpr e) return handler(e.getExpression(), env, route, depth + 1);
        if (expr instanceof LambdaExpr lambda) {
            deferred.add(lambda);
            int count = 0;
            for (MethodCallExpr call : lambda.findAll(MethodCallExpr.class)) {
                Method m = target(call, env, 0);
                if (m != null) {
                    result.edges.add(new Edge(route, m.member().id(), "call", Guards.of(call, lambda)));
                    result.callSites.add(new CallSite(route, m.member().id(), call.getBegin().map(p -> p.line).orElse(0), ""));
                    count++;
                }
            }
            for (MethodReferenceExpr ref : lambda.findAll(MethodReferenceExpr.class)) count += handler(ref, env, route, depth + 1);
            for (ObjectCreationExpr creation : lambda.findAll(ObjectCreationExpr.class)) {
                Owner type = type(creation.getType().asString(), creation);
                if (type == null) continue;
                var constructors = type.decl().getConstructors().stream()
                        .filter(c -> c.getParameters().size() == creation.getArguments().size()).toList();
                if (constructors.size() != 1) continue;
                int line = constructors.get(0).getBegin().map(p -> p.line).orElse(-1);
                List<Node> nodes = result.nodes.stream().filter(candidate -> candidate.kind.equals("method")
                        && candidate.id.startsWith(type.id() + "." + type.decl().getNameAsString() + "(")
                        && candidate.filePath.equals(type.file()) && candidate.line == line).toList();
                if (nodes.size() == 1) {
                    result.edges.add(new Edge(route, nodes.get(0).id, "call", Guards.of(creation, lambda)));
                    result.callSites.add(new CallSite(route, nodes.get(0).id, creation.getBegin().map(p -> p.line).orElse(0), ""));
                    count++;
                }
            }
            return count;
        }
        if (expr instanceof MethodReferenceExpr ref) {
            deferred.add(ref);
            Owner type = typeOf(ref.getScope(), env, depth + 1);
            Method m = uniqueMethod(type, ref.getIdentifier());
            if (m != null) { edge(route, m, ref); return 1; }
            return 0;
        }
        if (expr instanceof MethodCallExpr call) {
            Method factory = target(call, env, depth + 1);
            if (factory != null) {
                var returns = returns(factory);
                if (returns.size() == 1) return handler(returns.get(0), arguments(factory, call, env), route, depth + 1);
            }
        }
        Owner type = typeOf(expr, env, depth + 1);
        if (type != null && inherits(type, "org.apache.camel.Processor", new HashSet<>())) {
            Method process = method(type, "process", 1, new HashSet<>());
            if (process != null) { edge(route, process, expr); return 1; }
        }
        return 0;
    }

    private void edge(String route, Method method, com.github.javaparser.ast.Node at) {
        result.edges.add(new Edge(route, method.member().id(), "call"));
        result.callSites.add(new CallSite(route, method.member().id(), at.getBegin().map(p -> p.line).orElse(0), ""));
    }

    private Source source(Expression expr, Map<String, Bound> env, int depth) {
        if (depth > LIMIT) return new Source("{" + expr + "}", "", "", false);
        Bound b = binding(expr, env);
        if (b != null) {
            Source initial = source(b.expr(), b.env(), depth + 1);
            return isKafkaEndpoint(expr, env, depth + 1) ? configured(expr, env, initial, depth + 1) : initial;
        }
        if (expr instanceof MethodCallExpr call) {
            Method helper = target(call, env, depth + 1);
            if (helper != null) {
                List<Expression> returns = returns(helper);
                if (returns.size() == 1) {
                    Map<String, Bound> args = arguments(helper, call, env);
                    Source initial = source(returns.get(0), args, depth + 1);
                    // Setter overrides apply only to the returned KafkaEndpoint's
                    // own getConfiguration() alias, never another endpoint nearby.
                    if (isKafkaEndpoint(returns.get(0), args, depth + 1)) {
                        return configured(returns.get(0), args, initial, depth + 1);
                    }
                    return initial;
                }
                warn("Camel endpoint factory has multiple/unknown returns: " + helper.member().id());
            }
            if (call.getNameAsString().equals("endpoint") && !call.getArguments().isEmpty()
                    && ownerOf(call).filter(o -> inherits(o, "org.apache.camel.builder.RouteBuilder", new HashSet<>())).isPresent())
                return uri(value(call.getArgument(0), env, depth + 1));
        }
        return uri(value(expr, env, depth + 1));
    }

    private Source configured(Expression endpoint, Map<String, Bound> env, Source initial, int depth) {
        var callable = endpoint.findAncestor(CallableDeclaration.class);
        if (callable.isEmpty()) return initial;
        String topic = initial.topic(), group = initial.group();
        List<Setting> settings = new ArrayList<>();
        boolean complete = collectSettings(callable.get(), env, new Bound(endpoint, env), "", settings, new HashSet<>(), 0);
        if (!complete) return new Source(initial.uri(), "{" + expression(endpoint, env, 0) + ".getConfiguration().topic}",
                "{" + expression(endpoint, env, 0) + ".getConfiguration().groupId}", true);
        List<Setting> topics = settings.stream().filter(s -> s.call().getNameAsString().equals("setTopic")).toList();
        List<Setting> groups = settings.stream().filter(s -> s.call().getNameAsString().equals("setGroupId")).toList();
        if (topics.size() == 1 && topics.get(0).guard().isEmpty()) topic = value(topics.get(0).call().getArgument(0), topics.get(0).env(), depth + 1);
        else if (!topics.isEmpty()) { topic = unresolvedSettings(topics); warn("Camel KafkaEndpoint topic has conditional/multiple setters"); }
        if (groups.size() == 1 && groups.get(0).guard().isEmpty()) group = value(groups.get(0).call().getArgument(0), groups.get(0).env(), depth + 1);
        else if (!groups.isEmpty()) { group = unresolvedSettings(groups); warn("Camel KafkaEndpoint consumer group has conditional/multiple setters"); }
        return new Source(initial.uri(), topic, group, true);
    }

    private String unresolvedSettings(List<Setting> settings) {
        return "{" + String.join(" | ", settings.stream().map(s -> (s.guard().isEmpty() ? "" : s.guard() + " => ")
                + expression(s.call().getArgument(0), s.env(), 0)).toList()) + "}";
    }

    private boolean collectSettings(CallableDeclaration<?> method, Map<String, Bound> env, Bound endpoint,
                                 String guard, List<Setting> out,
                                 Set<CallableDeclaration<?>> stack, int depth) {
        if (depth > LIMIT || !stack.add(method)) { warn("Camel Kafka configuration helper recursion/depth limit"); return false; }
        boolean complete = true;
        for (MethodCallExpr call : method.findAll(MethodCallExpr.class)) {
            if (call.findAncestor(CallableDeclaration.class).orElse(null) != method || call.findAncestor(LambdaExpr.class).isPresent()) continue;
            String ownGuard = Guards.of(call, method);
            String condition = guard.isEmpty() ? ownGuard : ownGuard.isEmpty() ? guard : guard + " && " + ownGuard;
            if (Set.of("setTopic", "setGroupId").contains(call.getNameAsString()) && call.getArguments().size() == 1
                    && call.getScope().isPresent() && configurationOf(call.getScope().get(), env, endpoint)) {
                out.add(new Setting(call, env, condition));
                continue;
            }
            if (call.getParentNode().orElse(null) instanceof com.github.javaparser.ast.stmt.ExpressionStmt
                    && !Set.of("from", "routeId", "process", "to", "toD").contains(call.getNameAsString())
                    && call.getArguments().stream().anyMatch(a -> configurationOf(a, env, endpoint)
                    || dereference(a, env).expr() == dereference(endpoint.expr(), endpoint.env()).expr())) {
                Method helper = target(call, env, depth + 1);
                if (helper != null) complete &= collectSettings(helper.member().decl(), arguments(helper, call, env), endpoint,
                        condition, out, new HashSet<>(stack), depth + 1);
                else { complete = false; warn("Camel Kafka configuration helper is unresolved: " + call); }
            }
        }
        return complete;
    }

    private Bound dereference(Expression expr, Map<String, Bound> env) {
        Bound current = new Bound(expr, env);
        for (int i = 0; i < LIMIT; i++) {
            Bound b = binding(current.expr(), current.env());
            if (b == null) break;
            current = b;
        }
        return current;
    }

    private boolean configurationOf(Expression expr, Map<String, Bound> env, Bound endpoint) {
        Bound receiver = dereference(expr, env);
        if (!(receiver.expr() instanceof MethodCallExpr get) || !get.getNameAsString().equals("getConfiguration") || get.getScope().isEmpty()) return false;
        return dereference(get.getScope().get(), receiver.env()).expr() == dereference(endpoint.expr(), endpoint.env()).expr();
    }

    private boolean isKafkaEndpoint(Expression expr, Map<String, Bound> env, int depth) {
        if (depth > LIMIT) return false;
        Bound b = binding(expr, env);
        if (b != null) return isKafkaEndpoint(b.expr(), b.env(), depth + 1);
        if (expr instanceof MethodCallExpr call && call.getNameAsString().equals("endpoint")
                && call.getArguments().size() == 2 && call.getArgument(1) instanceof ClassExpr c)
            return qualified(c.getType().asString(), c).equals("org.apache.camel.component.kafka.KafkaEndpoint");
        if (expr instanceof ObjectCreationExpr c)
            return qualified(c.getType().asString(), c).equals("org.apache.camel.component.kafka.KafkaEndpoint");
        return false;
    }

    private static Source uri(String uri) {
        if (!uri.startsWith("kafka:")) return new Source(uri, "", "", false);
        int q = uri.indexOf('?');
        String topic = uri.substring(6, q < 0 ? uri.length() : q), group = "";
        if (q >= 0) for (String option : uri.substring(q + 1).split("&"))
            if (option.startsWith("groupId=")) group = option.substring(8);
        return new Source(uri, topic, group, true);
    }

    private String value(Expression expr, Map<String, Bound> env, int depth) {
        if (depth > LIMIT) return "{" + expr + "}";
        if (unstable(expr)) return "{" + expr + "}";
        Bound b = binding(expr, env);
        if (b != null) return value(b.expr(), b.env(), depth + 1);
        if (expr instanceof EnclosedExpr e) return value(e.getInner(), env, depth + 1);
        if (expr instanceof BinaryExpr e && e.getOperator() == BinaryExpr.Operator.PLUS)
            return value(e.getLeft(), env, depth + 1) + value(e.getRight(), env, depth + 1);
        if (expr instanceof MethodCallExpr call) {
            Method helper = target(call, env, depth + 1);
            if (helper != null && returns(helper).size() == 1)
                return value(returns(helper).get(0), arguments(helper, call, env), depth + 1);
            return "{" + expression(expr, env, depth + 1) + "}";
        }
        return constants.resolve(expr);
    }

    private String expression(Expression expr, Map<String, Bound> env, int depth) {
        if (depth > LIMIT) return expr.toString();
        Bound b = binding(expr, env);
        if (b != null) return expression(b.expr(), b.env(), depth + 1);
        if (expr instanceof MethodCallExpr c) return c.getScope().map(s -> expression(s, env, depth + 1) + ".").orElse("")
                + c.getNameAsString() + "(" + String.join(", ", c.getArguments().stream().map(a -> expression(a, env, depth + 1)).toList()) + ")";
        if (expr instanceof BinaryExpr bExpr) return expression(bExpr.getLeft(), env, depth + 1) + " "
                + bExpr.getOperator().asString() + " " + expression(bExpr.getRight(), env, depth + 1);
        return expr.toString();
    }

    private Bound binding(Expression expr, Map<String, Bound> env) {
        if (unstable(expr)) return null;
        if (!(expr instanceof NameExpr) && !(expr instanceof FieldAccessExpr f && f.getScope() instanceof ThisExpr)) return null;
        String name = expr instanceof NameExpr n ? n.getNameAsString() : ((FieldAccessExpr) expr).getNameAsString();
        if (env.containsKey(name)) return env.get(name);
        return OutboundCallExtractor.declaration(expr, name, expr instanceof FieldAccessExpr)
                .flatMap(d -> d.value()).map(e -> new Bound(e, env)).orElse(null);
    }

    private boolean unstable(Expression expr) {
        if (!(expr instanceof NameExpr)) return false;
        var callable = expr.findAncestor(CallableDeclaration.class);
        if (callable.isEmpty()) return false;
        String name = ((NameExpr) expr).getNameAsString();
        for (AssignExpr assign : callable.get().findAll(AssignExpr.class)) {
            if (assign.getTarget() instanceof NameExpr n && n.getNameAsString().equals(name)
                    && assign.getBegin().isPresent() && expr.getBegin().isPresent()
                    && assign.getBegin().get().isBefore(expr.getBegin().get())) {
                warn("Camel binding is assigned at runtime: " + name);
                return true;
            }
        }
        return false;
    }

    private Map<String, Bound> arguments(Method method, MethodCallExpr call, Map<String, Bound> env) {
        Map<String, Bound> args = new HashMap<>();
        for (int i = 0; i < Math.min(method.member().decl().getParameters().size(), call.getArguments().size()); i++)
            args.put(method.member().decl().getParameter(i).getNameAsString(), new Bound(call.getArgument(i), env));
        return args;
    }

    private List<Expression> returns(Method method) {
        return method.member().decl().findAll(ReturnStmt.class).stream()
                .filter(r -> r.findAncestor(CallableDeclaration.class).orElse(null) == method.member().decl())
                .flatMap(r -> r.getExpression().stream()).toList();
    }

    private Method target(MethodCallExpr call, Map<String, Bound> env, int depth) {
        if (depth > LIMIT) return null;
        Owner type = call.getScope().map(s -> typeOf(s, env, depth + 1)).orElseGet(() -> effectiveOwner(call));
        return method(type, call.getNameAsString(), call.getArguments().size(), new HashSet<>());
    }

    private Owner typeOf(Expression expr, Map<String, Bound> env, int depth) {
        if (depth > LIMIT) return null;
        Bound b = binding(expr, env);
        if (b != null) return typeOf(b.expr(), b.env(), depth + 1);
        if (expr instanceof ThisExpr) return effectiveOwner(expr);
        if (expr instanceof SuperExpr) return ownerOf(expr).orElse(null);
        if (expr instanceof ObjectCreationExpr c) return type(c.getType().asString(), c);
        if (expr instanceof NameExpr n || expr instanceof FieldAccessExpr) {
            String name = expr instanceof NameExpr n ? n.getNameAsString() : ((FieldAccessExpr) expr).getNameAsString();
            var declaration = OutboundCallExtractor.declaration(expr, name, expr instanceof FieldAccessExpr);
            if (declaration.isPresent()) return type(declaration.get().type().asString(), expr);
            return type(name, expr); // static method reference/factory
        }
        try {
            return type(expr.calculateResolvedType().describe(), expr);
        } catch (RuntimeException | StackOverflowError ignored) { return null; }
    }

    private Optional<Owner> ownerOf(com.github.javaparser.ast.Node node) {
        TypeDeclaration<?> decl = node.findAncestor(TypeDeclaration.class).orElse(null);
        return owners.stream().filter(o -> o.decl() == decl).findFirst();
    }

    private Owner effectiveOwner(com.github.javaparser.ast.Node node) {
        Owner lexical = ownerOf(node).orElse(null);
        if (routeOwner != null && lexical != null && inherits(routeOwner,
                lexical.decl().getFullyQualifiedName().orElse(""), new HashSet<>())) return routeOwner;
        return lexical;
    }

    private Owner type(String name, com.github.javaparser.ast.Node at) {
        String q = qualified(name, at);
        List<Owner> candidates = owners.stream().filter(o -> o.decl().getFullyQualifiedName().orElse("").equals(q)).toList();
        // Same source/module first; never use an arbitrary global simple-name match.
        String file = ownerOf(at).map(Owner::file).orElse("");
        if (candidates.size() > 1) candidates = candidates.stream().filter(o -> o.file().equals(file)).toList();
        return candidates.size() == 1 ? candidates.get(0) : null;
    }

    private String qualified(String name, com.github.javaparser.ast.Node at) {
        name = name.replaceAll("<.*>", "");
        if (name.contains(".")) {
            int dot = name.indexOf('.');
            if (Character.isUpperCase(name.charAt(0))) return qualified(name.substring(0, dot), at) + name.substring(dot);
            return name;
        }
        var cu = at.findCompilationUnit();
        if (cu.isEmpty()) return name;
        String pkg = cu.get().getPackageDeclaration().map(p -> p.getNameAsString() + ".").orElse("");
        String candidate = pkg + name;
        for (var node = at; node != null; node = node.getParentNode().orElse(null)) {
            if (node instanceof TypeDeclaration<?> type) {
                String nested = type.getFullyQualifiedName().orElse("") + "." + name;
                if (owners.stream().anyMatch(o -> o.decl().getFullyQualifiedName().orElse("").equals(nested))) return nested;
            }
        }
        for (var imp : cu.get().getImports()) if (!imp.isStatic() && !imp.isAsterisk()
                && imp.getName().getIdentifier().equals(name)) return imp.getNameAsString();
        if (owners.stream().anyMatch(o -> o.decl().getFullyQualifiedName().orElse("").equals(candidate))) return candidate;
        List<String> wildcard = new ArrayList<>();
        for (var imp : cu.get().getImports()) if (!imp.isStatic() && imp.isAsterisk()) {
            String q = imp.getNameAsString() + "." + name;
            if (q.equals("org.apache.camel.builder.RouteBuilder") || q.equals("org.apache.camel.Processor")
                    || q.equals("org.apache.camel.component.kafka.KafkaEndpoint")
                    || owners.stream().anyMatch(o -> o.decl().getFullyQualifiedName().orElse("").equals(q))) wildcard.add(q);
        }
        return wildcard.size() == 1 ? wildcard.get(0) : candidate;
    }

    private boolean inherits(Owner owner, String base, Set<String> seen) {
        if (owner == null || !seen.add(owner.id())) return false;
        if (owner.decl().getFullyQualifiedName().orElse("").equals(base)) return true;
        if (!(owner.decl() instanceof ClassOrInterfaceDeclaration c)) return false;
        var supers = new ArrayList<>(c.getExtendedTypes()); supers.addAll(c.getImplementedTypes());
        for (var s : supers) if (qualified(s.asString(), c).equals(base) || inherits(type(s.asString(), c), base, seen)) return true;
        return false;
    }

    private Method method(Owner owner, String name, int arity, Set<String> seen) {
        if (owner == null || !seen.add(owner.id())) return null;
        var candidates = owner.methods().stream().filter(m -> m.decl().getNameAsString().equals(name)
                && m.decl().getParameters().size() == arity).toList();
        if (candidates.size() == 1) return methods.get(candidates.get(0).decl());
        if (!candidates.isEmpty()) return null;
        if (owner.decl() instanceof ClassOrInterfaceDeclaration c) for (var parent : c.getExtendedTypes()) {
            Method inherited = method(type(parent.asString(), c), name, arity, seen);
            if (inherited != null) return inherited;
        }
        return null;
    }

    private Method uniqueMethod(Owner owner, String name) {
        if (owner == null) return null;
        var candidates = owner.methods().stream().filter(m -> m.decl().getNameAsString().equals(name)).toList();
        return candidates.size() == 1 ? methods.get(candidates.get(0).decl()) : null;
    }
}
