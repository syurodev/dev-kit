package com.devkit.codegraph;

import com.github.javaparser.ast.body.CallableDeclaration;
import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import com.github.javaparser.ast.body.FieldDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.body.Parameter;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.body.VariableDeclarator;
import com.github.javaparser.ast.expr.AnnotationExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.FieldAccessExpr;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.ast.expr.NameExpr;
import com.github.javaparser.ast.expr.NormalAnnotationExpr;
import com.github.javaparser.ast.expr.ObjectCreationExpr;
import com.github.javaparser.ast.expr.SingleMemberAnnotationExpr;
import com.github.javaparser.ast.expr.ThisExpr;
import com.github.javaparser.ast.expr.VariableDeclarationExpr;
import com.github.javaparser.ast.stmt.BlockStmt;
import com.github.javaparser.ast.type.Type;

import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;

/**
 * Finds the outbound HTTP calls a Spring service makes to another one.
 *
 * AST-only, like EndpointExtractor, so the workspace's manifest tier can run
 * it without the symbol solver. That rules out asking "is this receiver a
 * RestTemplate?" of the type system, and it does not need to: the receiver
 * is a name, and the name's declaration — a field, a parameter, a local —
 * says its type in the source. Matching is by simple type name, for the
 * reason LayerClassifier gives.
 *
 * Five clients, the ones Spring code actually uses:
 * <ul>
 * <li>{@code @FeignClient} interfaces — every mapped method is a call to
 *     the service the annotation names, at its route (same mapping
 *     annotations EndpointExtractor reads, read as the route called).</li>
 * <li>Spring HTTP interfaces ({@code @HttpExchange}, {@code @GetExchange}…) —
 *     the same, but bound to a base URL elsewhere, so no service is named.</li>
 * <li>{@code RestTemplate} — the first argument of getForObject/exchange/...
 *     is the URL; exchange and execute take the HTTP method as the second.</li>
 * <li>{@code WebClient} and {@code RestClient} — {@code client.get().uri(x)},
 *     or {@code .method(HttpMethod.X).uri(x)}.</li>
 * </ul>
 * A client built with a base URL — {@code builder.baseUrl(x).build()},
 * {@code WebClient.create(x)}, a RestTemplateBuilder's {@code rootUri(x)} —
 * has it prefixed, when the field or local the call goes through shows where
 * it was built. A client injected ready-made from some @Bean elsewhere keeps
 * the relative URL, which the resolver reports as dynamic rather than guess.
 */
public final class OutboundCallExtractor {

    private OutboundCallExtractor() {}

    /** A found call and the method or constructor it was found in. */
    public record Found(CallableDeclaration<?> in, OutboundCall call) {}

    private static final Map<String, String> REST_TEMPLATE_METHODS = Map.ofEntries(
            Map.entry("getForObject", "GET"),
            Map.entry("getForEntity", "GET"),
            Map.entry("headForHeaders", "HEAD"),
            Map.entry("postForObject", "POST"),
            Map.entry("postForEntity", "POST"),
            Map.entry("postForLocation", "POST"),
            Map.entry("put", "PUT"),
            Map.entry("patchForObject", "PATCH"),
            Map.entry("delete", "DELETE"),
            Map.entry("optionsForAllow", "OPTIONS"),
            Map.entry("exchange", ""),
            Map.entry("execute", ""));

    private static final Set<String> FLUENT_METHODS = Set.of("get", "post", "put", "patch", "delete", "head", "options");

    private static final Map<String, String> CLIENT_TYPES = Map.of(
            "RestTemplate", "rest-template",
            "RestOperations", "rest-template",
            "WebClient", "web-client",
            "RestClient", "rest-client");

    private static final Set<String> BASE_URL_METHODS = Set.of("baseUrl", "rootUri");

    /**
     * Every mapped method of a {@code @FeignClient} interface, each a call to
     * the named service. Empty for any other type.
     */
    public static List<Found> feign(TypeDeclaration<?> type, Annotations annotations, ConstantResolver constants) {
        Optional<AnnotationExpr> feign = annotations.find(type, "FeignClient");
        if (feign.isEmpty()) {
            return List.of();
        }
        String name = attribute(feign.get(), "name")
                .or(() -> attribute(feign.get(), "value"))
                .or(() -> attribute(feign.get(), "serviceId"))
                .map(constants::resolve).orElse("");
        String url = attribute(feign.get(), "url").map(constants::resolve).orElse("");
        String path = attribute(feign.get(), "path").map(constants::resolve).orElse("");

        List<Found> found = new ArrayList<>();
        for (MethodDeclaration method : type.getMethods()) {
            for (EndpointExtractor.HttpMapping mapping :
                    EndpointExtractor.clientMappings(type, method, annotations, constants)) {
                String route = EndpointExtractor.joinPath(path, mapping.path());
                found.add(new Found(method, new OutboundCall("feign", mapping.httpMethod(), name,
                        stripTrailingSlash(url) + route, line(method))));
            }
        }
        return found;
    }

    /** Method annotations that make an interface a Spring 6 HTTP interface. */
    private static final List<String> EXCHANGE_METHODS = List.of(
            "GetExchange", "PostExchange", "PutExchange", "DeleteExchange", "PatchExchange", "HttpExchange");

    /**
     * Every {@code @GetExchange}-style method of a Spring HTTP interface: a call
     * to the route it declares, joined to the interface's own {@code @HttpExchange}
     * prefix. The target service is not named here — an HTTP interface is bound
     * to a base URL where {@code HttpServiceProxyFactory} builds it, which is
     * elsewhere and often a @Bean — so {@code serviceName} stays empty and the
     * caller resolves it by the route alone. Empty for any other type.
     */
    public static List<Found> httpInterface(TypeDeclaration<?> type, Annotations annotations, ConstantResolver constants) {
        boolean controller = annotations.has(type, "RestController") || annotations.has(type, "Controller");
        if (controller || annotations.has(type, "FeignClient") || !(type instanceof ClassOrInterfaceDeclaration decl) || !decl.isInterface()) {
            return List.of();
        }
        boolean declared = annotations.has(type, "HttpExchange")
                || type.getMethods().stream().anyMatch(m -> annotations.findAny(m, EXCHANGE_METHODS).isPresent());
        if (!declared) {
            return List.of();
        }
        List<Found> found = new ArrayList<>();
        for (MethodDeclaration method : type.getMethods()) {
            for (EndpointExtractor.HttpMapping mapping :
                    EndpointExtractor.clientMappings(type, method, annotations, constants)) {
                found.add(new Found(method, new OutboundCall("http-interface", mapping.httpMethod(), "",
                        mapping.path(), line(method))));
            }
        }
        return found;
    }

    /** The outbound call this expression makes, if it is one. */
    public static Optional<OutboundCall> call(MethodCallExpr call, ConstantResolver constants) {
        String restMethod = REST_TEMPLATE_METHODS.get(call.getNameAsString());
        if (restMethod != null && call.getScope().isPresent()) {
            Optional<Client> client = client(call.getScope().get(), constants);
            if (client.isPresent() && client.get().kind().equals("rest-template") && call.getArguments().size() > 0) {
                String httpMethod = restMethod;
                String url;
                if (call.getArguments().size() == 1 && restMethod.isEmpty()) {
                    // exchange(RequestEntity): method and URL are inside the entity.
                    url = "{" + call.getArgument(0) + "}";
                    httpMethod = "ANY";
                } else {
                    url = url(call.getArgument(0), constants);
                    if (restMethod.isEmpty()) {
                        httpMethod = httpMethodConstant(call.getArgument(1)).orElse("ANY");
                    }
                }
                return Optional.of(new OutboundCall("rest-template", httpMethod, "",
                        client.get().baseUrl() + url, line(call)));
            }
        }

        if (call.getNameAsString().equals("uri") && call.getArguments().size() > 0
                && call.getScope().orElse(null) instanceof MethodCallExpr verb && verb.getScope().isPresent()) {
            String httpMethod;
            if (FLUENT_METHODS.contains(verb.getNameAsString()) && verb.getArguments().isEmpty()) {
                httpMethod = verb.getNameAsString().toUpperCase();
            } else if (verb.getNameAsString().equals("method") && verb.getArguments().size() == 1) {
                httpMethod = httpMethodConstant(verb.getArgument(0)).orElse("ANY");
            } else {
                return Optional.empty();
            }
            Optional<Client> client = client(verb.getScope().get(), constants);
            if (client.isPresent() && !client.get().kind().equals("rest-template")) {
                Expression arg = call.getArgument(0);
                // uri(uriBuilder -> ...) builds the URL at run time.
                String url = arg.isLambdaExpr() ? "{" + arg + "}" : url(arg, constants);
                return Optional.of(new OutboundCall(client.get().kind(), httpMethod, "",
                        client.get().baseUrl() + url, line(call)));
            }
        }
        return Optional.empty();
    }

    private record Client(String kind, String baseUrl) {}

    /**
     * The client a receiver expression names, with the base URL it was built
     * with ("" when none is visible). An inline construction —
     * {@code WebClient.create(x).get()} — counts as well as a name.
     */
    private static Optional<Client> client(Expression receiver, ConstantResolver constants) {
        if (receiver instanceof NameExpr || (receiver instanceof FieldAccessExpr access && access.getScope() instanceof ThisExpr)) {
            String name = receiver instanceof NameExpr n ? n.getNameAsString() : ((FieldAccessExpr) receiver).getNameAsString();
            Optional<Declared> declared = declaration(receiver, name, !(receiver instanceof NameExpr));
            if (declared.isEmpty()) {
                return Optional.empty();
            }
            String kind = CLIENT_TYPES.get(simpleName(declared.get().type()));
            Optional<Expression> built = declared.get().value();
            if (kind == null && built.isPresent()) {
                kind = builtKind(built.get()); // var client = WebClient.create(...)
            }
            if (kind == null) {
                return Optional.empty();
            }
            return Optional.of(new Client(kind, built.map(b -> baseUrl(b, constants)).orElse("")));
        }
        String kind = builtKind(receiver);
        return kind == null ? Optional.empty() : Optional.of(new Client(kind, baseUrl(receiver, constants)));
    }

    /** A declaration's type as written and the expression it is set to, if any. */
    private record Declared(Type type, Optional<Expression> value) {}

    /**
     * Where a name used at {@code at} is declared: a local or a parameter of
     * the enclosing code (unless {@code fieldOnly}, for {@code this.x}), else a
     * field of an enclosing type. A field's value is its initializer or the
     * one expression its constructors assign it.
     */
    private static Optional<Declared> declaration(Expression at, String name, boolean fieldOnly) {
        Optional<com.github.javaparser.ast.Node> current = at.getParentNode();
        while (current.isPresent()) {
            com.github.javaparser.ast.Node node = current.get();
            if (!fieldOnly && node instanceof BlockStmt block) {
                for (VariableDeclarationExpr decl : block.findAll(VariableDeclarationExpr.class)) {
                    for (VariableDeclarator v : decl.getVariables()) {
                        if (v.getNameAsString().equals(name) && v.getBegin().isPresent() && at.getBegin().isPresent()
                                && v.getBegin().get().isBefore(at.getBegin().get())) {
                            return Optional.of(new Declared(v.getType(), v.getInitializer()));
                        }
                    }
                }
            } else if (!fieldOnly && node instanceof CallableDeclaration<?> callable) {
                for (Parameter p : callable.getParameters()) {
                    if (p.getNameAsString().equals(name)) {
                        return Optional.of(new Declared(p.getType(), Optional.empty()));
                    }
                }
            } else if (node instanceof TypeDeclaration<?> type) {
                for (FieldDeclaration field : type.getFields()) {
                    for (VariableDeclarator v : field.getVariables()) {
                        if (v.getNameAsString().equals(name)) {
                            Optional<Expression> value = v.getInitializer()
                                    .or(() -> ConstantResolver.assignedInConstructor(v));
                            return Optional.of(new Declared(v.getType(), value));
                        }
                    }
                }
            }
            current = node.getParentNode();
        }
        return Optional.empty();
    }

    /** The client kind an expression constructs: new RestTemplate(), WebClient.create(), X.builder()...build(). */
    private static String builtKind(Expression expr) {
        Expression e = expr;
        while (e instanceof MethodCallExpr m && m.getScope().isPresent()) {
            e = m.getScope().get();
        }
        if (e instanceof ObjectCreationExpr creation) {
            return CLIENT_TYPES.get(creation.getType().getNameAsString());
        }
        if (e instanceof NameExpr n) {
            return CLIENT_TYPES.get(n.getNameAsString());
        }
        return null;
    }

    /**
     * The base URL a client-building chain sets: {@code baseUrl(x)} /
     * {@code rootUri(x)} anywhere in it, or {@code WebClient.create(x)}.
     * "" when the chain sets none.
     */
    private static String baseUrl(Expression built, ConstantResolver constants) {
        Expression e = built;
        while (e instanceof MethodCallExpr m) {
            if (m.getArguments().size() == 1 && (BASE_URL_METHODS.contains(m.getNameAsString())
                    || (m.getNameAsString().equals("create") && m.getScope().orElse(null) instanceof NameExpr n
                        && CLIENT_TYPES.containsKey(n.getNameAsString())))) {
                return stripTrailingSlash(constants.resolve(m.getArgument(0)));
            }
            if (m.getScope().isEmpty()) {
                break;
            }
            e = m.getScope().get();
        }
        return "";
    }

    /** A URL argument folded; {@code URI.create(x)} is seen through, since it only wraps the string. */
    private static String url(Expression arg, ConstantResolver constants) {
        if (arg instanceof MethodCallExpr m && m.getNameAsString().equals("create") && m.getArguments().size() == 1
                && m.getScope().orElse(null) instanceof NameExpr n && n.getNameAsString().equals("URI")) {
            return constants.resolve(m.getArgument(0));
        }
        return constants.resolve(arg);
    }

    /** HttpMethod.POST (or a static import of it) as "POST". */
    private static Optional<String> httpMethodConstant(Expression expr) {
        String text = expr.toString();
        String last = text.substring(text.lastIndexOf('.') + 1).toUpperCase();
        return Set.of("GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE").contains(last)
                ? Optional.of(last) : Optional.empty();
    }

    private static Optional<Expression> attribute(AnnotationExpr annotation, String name) {
        if (annotation instanceof SingleMemberAnnotationExpr single) {
            return name.equals("value") ? Optional.of(single.getMemberValue()) : Optional.empty();
        }
        if (annotation instanceof NormalAnnotationExpr normal) {
            for (var pair : normal.getPairs()) {
                if (name.equals(pair.getNameAsString())) {
                    return Optional.of(pair.getValue());
                }
            }
        }
        return Optional.empty();
    }

    private static String simpleName(Type type) {
        String written = type.asString();
        int generic = written.indexOf('<');
        if (generic >= 0) {
            written = written.substring(0, generic);
        }
        return written.substring(written.lastIndexOf('.') + 1);
    }

    private static String stripTrailingSlash(String s) {
        return s.endsWith("/") ? s.substring(0, s.length() - 1) : s;
    }

    private static int line(com.github.javaparser.ast.Node node) {
        return node.getBegin().map(p -> p.line).orElse(0);
    }
}
