package com.devkit.codegraph;

import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.expr.AnnotationExpr;
import com.github.javaparser.ast.expr.ArrayInitializerExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.NormalAnnotationExpr;
import com.github.javaparser.ast.expr.SingleMemberAnnotationExpr;

import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;

/**
 * Extracts the HTTP routes a handler method serves from its mapping
 * annotations: @GetMapping/@PostMapping/@PutMapping/@DeleteMapping/
 * @PatchMapping, plain @RequestMapping, and Spring 6's @HttpExchange family
 * (@GetExchange, ...) when the class is a controller. Matches by simple
 * annotation name, same reasoning as LayerClassifier, and sees through the
 * project's own meta-annotations (see Annotations).
 *
 * One method can serve several routes, and each becomes its own mapping:
 * every listed path, crossed with every class-level path, for every listed
 * HTTP method. A plain @RequestMapping with no method anywhere matches every
 * method in Spring, and is reported as "ANY" rather than guessed to be GET.
 *
 * Mappings are inherited the way Spring inherits them. A controller that
 * implements an interface (an OpenAPI-generated one, say) takes the route
 * from the interface method it overrides, and a controller whose own class
 * carries no @RequestMapping takes the class-level prefix from its nearest
 * supertype that does. The caller supplies that hierarchy, since only it
 * knows the project's types.
 *
 * A class marked as a declarative HTTP client (@FeignClient, or Spring 6's
 * own @HttpExchange) is skipped entirely, even if its methods carry the same
 * mapping annotations a controller would: those annotations there describe a
 * route this class *calls*, on some other service, not one this project
 * serves. Left unchecked, a client interface declaring the exact route its
 * own controller implements collides on the endpoint's id (method + path)
 * and used to take down the whole index (see ReplaceIndex's dedup, the
 * belt-and-suspenders half of this fix) — this half stops the false
 * "endpoint" from being extracted at all, which a shared spec interface
 * reused by both a controller and a client makes more likely than it sounds.
 * The exception is @HttpExchange on a @RestController/@Controller: Spring
 * 6.1 serves those, so they are routes.
 */
public class EndpointExtractor {

    public record HttpMapping(String httpMethod, String path) {}

    /** What a plain @RequestMapping (or @HttpExchange) with no method matches: all of them. */
    public static final String ANY_METHOD = "ANY";

    private static final Map<String, String> MAPPING_ANNOTATION_TO_HTTP_METHOD = Map.of(
            "GetMapping", "GET",
            "PostMapping", "POST",
            "PutMapping", "PUT",
            "DeleteMapping", "DELETE",
            "PatchMapping", "PATCH",
            "GetExchange", "GET",
            "PostExchange", "POST",
            "PutExchange", "PUT",
            "DeleteExchange", "DELETE",
            "PatchExchange", "PATCH"
    );

    /** Method-level annotations, fixed-method ones first so the order is deterministic. */
    private static final List<String> METHOD_ANNOTATIONS = List.of(
            "GetMapping", "PostMapping", "PutMapping", "DeleteMapping", "PatchMapping",
            "GetExchange", "PostExchange", "PutExchange", "DeleteExchange", "PatchExchange",
            "RequestMapping", "HttpExchange");

    private static final List<String> CLASS_ANNOTATIONS = List.of("RequestMapping", "HttpExchange");

    private static final Set<String> HTTP_METHODS = Set.of(
            "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "TRACE");

    private static boolean isDeclarativeClient(TypeDeclaration<?> classDecl, Annotations annotations) {
        boolean client = annotations.has(classDecl, "FeignClient") || annotations.has(classDecl, "HttpExchange");
        boolean controller = annotations.has(classDecl, "RestController") || annotations.has(classDecl, "Controller");
        return client && !controller;
    }

    /** The method's own mapping under its own class's, with nothing inherited and no project index. */
    public static List<HttpMapping> extract(TypeDeclaration<?> classDecl, MethodDeclaration methodDecl) {
        return extract(classDecl, List.of(), List.of(methodDecl), Annotations.NONE, ConstantResolver.NONE);
    }

    /**
     * @param classDecl      the class serving the routes
     * @param classAncestors its project supertypes, nearest first
     * @param methodChain    the handler method, then the methods it overrides, nearest first
     */
    public static List<HttpMapping> extract(TypeDeclaration<?> classDecl, List<TypeDeclaration<?>> classAncestors,
                                            List<MethodDeclaration> methodChain, Annotations annotations,
                                            ConstantResolver constants) {
        if (isDeclarativeClient(classDecl, annotations)) {
            return List.of();
        }
        Optional<Annotations.Match> methodMapping = Optional.empty();
        for (MethodDeclaration m : methodChain) {
            methodMapping = annotations.findAny(m, METHOD_ANNOTATIONS);
            if (methodMapping.isPresent()) {
                break;
            }
        }
        if (methodMapping.isEmpty()) {
            return List.of();
        }
        Optional<Annotations.Match> classMapping = annotations.findAny(classDecl, CLASS_ANNOTATIONS);
        for (int i = 0; classMapping.isEmpty() && i < classAncestors.size(); i++) {
            classMapping = annotations.findAny(classAncestors.get(i), CLASS_ANNOTATIONS);
        }

        List<String> httpMethods = httpMethods(methodMapping.get(), classMapping, constants);
        List<String> classPaths = classMapping.map(m -> paths(m, constants)).orElse(List.of(""));
        List<String> methodPaths = paths(methodMapping.get(), constants);

        Set<HttpMapping> mappings = new LinkedHashSet<>();
        for (String httpMethod : httpMethods) {
            for (String classPath : classPaths) {
                for (String methodPath : methodPaths) {
                    mappings.add(new HttpMapping(httpMethod, joinPath(classPath, methodPath)));
                }
            }
        }
        return new ArrayList<>(mappings);
    }

    private static List<String> httpMethods(Annotations.Match methodMapping, Optional<Annotations.Match> classMapping,
                                            ConstantResolver constants) {
        String fixed = MAPPING_ANNOTATION_TO_HTTP_METHOD.get(methodMapping.name());
        if (fixed != null) {
            return List.of(fixed);
        }
        // @RequestMapping / @HttpExchange: the method's own method= wins, then
        // the class-level one, and with neither Spring matches every method.
        List<String> own = declaredMethods(methodMapping, constants);
        if (!own.isEmpty()) {
            return own;
        }
        List<String> inherited = classMapping.map(m -> declaredMethods(m, constants)).orElse(List.of());
        return inherited.isEmpty() ? List.of(ANY_METHOD) : inherited;
    }

    // The value is RequestMethod.CONSTANT (or, for @HttpExchange, a string),
    // possibly in an array when several methods are mapped. Spring registers
    // one route per listed method, and so does this. The constant name is the
    // last dotted segment, so fully-qualified references are fine too.
    private static List<String> declaredMethods(Annotations.Match match, ConstantResolver constants) {
        Optional<Expression> value = attribute(match.written(), "method")
                .or(() -> attribute(match.annotation(), "method"));
        if (value.isEmpty()) {
            return List.of();
        }
        List<String> methods = new ArrayList<>();
        for (Expression e : elements(value.get())) {
            String name = e.isStringLiteralExpr() || e.isBinaryExpr() ? constants.resolve(e) : e.toString();
            int lastDot = name.lastIndexOf('.');
            if (lastDot >= 0) {
                name = name.substring(lastDot + 1);
            }
            name = name.trim().toUpperCase();
            if (HTTP_METHODS.contains(name) && !methods.contains(name)) {
                methods.add(name);
            }
        }
        return methods;
    }

    /**
     * Every path the annotation lists, or [""] when it lists none. A path
     * written on the annotation as used wins over one on the meta-annotation
     * it came through (the @AliasFor pattern custom mapping annotations use).
     */
    private static List<String> paths(Annotations.Match match, ConstantResolver constants) {
        Optional<Expression> value = pathAttribute(match.written());
        if (value.isEmpty() && match.written() != match.annotation()) {
            value = pathAttribute(match.annotation());
        }
        if (value.isEmpty()) {
            return List.of("");
        }
        List<String> paths = new ArrayList<>();
        for (Expression e : elements(value.get())) {
            String path = constants.resolve(e);
            if (!paths.contains(path)) {
                paths.add(path);
            }
        }
        return paths.isEmpty() ? List.of("") : paths;
    }

    private static Optional<Expression> pathAttribute(AnnotationExpr annotation) {
        if (annotation instanceof SingleMemberAnnotationExpr single) {
            return Optional.of(single.getMemberValue());
        }
        return attribute(annotation, "value")
                .or(() -> attribute(annotation, "path"))
                .or(() -> attribute(annotation, "url"));
    }

    private static Optional<Expression> attribute(AnnotationExpr annotation, String name) {
        if (annotation instanceof NormalAnnotationExpr normal) {
            for (var pair : normal.getPairs()) {
                if (name.equals(pair.getNameAsString())) {
                    return Optional.of(pair.getValue());
                }
            }
        }
        return Optional.empty();
    }

    private static List<Expression> elements(Expression expr) {
        if (expr instanceof ArrayInitializerExpr array) {
            return array.getValues();
        }
        return List.of(expr);
    }

    private static String joinPath(String classPath, String methodPath) {
        // Collapse doubled slashes from concatenation (classPath or
        // methodPath may be "") down to one, then drop a trailing slash
        // unless the whole path collapsed to just "/".
        String combined = ("/" + classPath + "/" + methodPath).replaceAll("/+", "/");
        if (combined.length() > 1 && combined.endsWith("/")) {
            combined = combined.substring(0, combined.length() - 1);
        }
        return combined;
    }
}
