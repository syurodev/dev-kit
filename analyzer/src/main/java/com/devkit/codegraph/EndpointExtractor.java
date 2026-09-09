package com.devkit.codegraph;

import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.expr.AnnotationExpr;
import com.github.javaparser.ast.expr.ArrayInitializerExpr;
import com.github.javaparser.ast.expr.Expression;
import com.github.javaparser.ast.expr.NormalAnnotationExpr;
import com.github.javaparser.ast.expr.SingleMemberAnnotationExpr;

import java.util.Map;
import java.util.Optional;

/**
 * Extracts an HTTP method + path from a method's mapping annotation
 * (@GetMapping/@PostMapping/@PutMapping/@DeleteMapping/@PatchMapping, or
 * plain @RequestMapping which defaults to GET when it carries no method
 * detail this best-effort extractor bothers parsing). Matches by simple
 * annotation name, same reasoning as LayerClassifier.
 */
public class EndpointExtractor {

    public record HttpMapping(String httpMethod, String path) {}

    private static final Map<String, String> MAPPING_ANNOTATION_TO_HTTP_METHOD = Map.of(
            "GetMapping", "GET",
            "PostMapping", "POST",
            "PutMapping", "PUT",
            "DeleteMapping", "DELETE",
            "PatchMapping", "PATCH"
    );

    public static Optional<HttpMapping> extract(TypeDeclaration<?> classDecl, MethodDeclaration methodDecl) {
        for (var entry : MAPPING_ANNOTATION_TO_HTTP_METHOD.entrySet()) {
            Optional<AnnotationExpr> annotation = methodDecl.getAnnotationByName(entry.getKey());
            if (annotation.isPresent()) {
                String methodPath = annotationPath(annotation.get());
                return Optional.of(new HttpMapping(entry.getValue(), joinPath(classPath(classDecl), methodPath)));
            }
        }
        Optional<AnnotationExpr> requestMapping = methodDecl.getAnnotationByName("RequestMapping");
        if (requestMapping.isPresent()) {
            String httpMethod = annotationMethod(requestMapping.get()).orElse("GET");
            return Optional.of(new HttpMapping(httpMethod, joinPath(classPath(classDecl), annotationPath(requestMapping.get()))));
        }
        return Optional.empty();
    }

    private static String classPath(TypeDeclaration<?> classDecl) {
        return classDecl.getAnnotationByName("RequestMapping")
                .map(EndpointExtractor::annotationPath)
                .orElse("");
    }

    private static String annotationPath(AnnotationExpr annotation) {
        if (annotation instanceof SingleMemberAnnotationExpr single) {
            return extractStringFromExpr(single.getMemberValue());
        }
        if (annotation instanceof NormalAnnotationExpr normal) {
            for (var pair : normal.getPairs()) {
                String name = pair.getNameAsString();
                if ("value".equals(name) || "path".equals(name)) {
                    return extractStringFromExpr(pair.getValue());
                }
            }
        }
        return "";
    }

    private static Optional<String> annotationMethod(AnnotationExpr annotation) {
        if (annotation instanceof NormalAnnotationExpr normal) {
            for (var pair : normal.getPairs()) {
                if ("method".equals(pair.getNameAsString())) {
                    return httpMethodOf(pair.getValue());
                }
            }
        }
        return Optional.empty();
    }

    // The value is RequestMethod.CONSTANT, possibly in an array when several
    // methods are mapped. Spring registers one route per listed method; this
    // extractor records the first-listed one. The constant name is the last
    // dotted segment, so this also tolerates fully-qualified references.
    private static Optional<String> httpMethodOf(Expression expr) {
        if (expr instanceof ArrayInitializerExpr array && !array.getValues().isEmpty()) {
            expr = array.getValues().get(0);
        }
        String name = expr.toString();
        int lastDot = name.lastIndexOf('.');
        if (lastDot >= 0) {
            name = name.substring(lastDot + 1);
        }
        name = name.trim().toUpperCase();
        return MAPPING_ANNOTATION_TO_HTTP_METHOD.containsValue(name) ? Optional.of(name) : Optional.empty();
    }

    private static String extractStringFromExpr(Expression expr) {
        if (expr instanceof ArrayInitializerExpr array && !array.getValues().isEmpty()) {
            return stripQuotes(array.getValues().get(0).toString());
        }
        return stripQuotes(expr.toString());
    }

    private static String stripQuotes(String literal) {
        return literal.replaceAll("^\"|\"$", "");
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
