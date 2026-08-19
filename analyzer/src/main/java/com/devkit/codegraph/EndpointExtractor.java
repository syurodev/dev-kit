package com.devkit.codegraph;

import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.expr.AnnotationExpr;
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

    public static Optional<HttpMapping> extract(ClassOrInterfaceDeclaration classDecl, MethodDeclaration methodDecl) {
        for (var entry : MAPPING_ANNOTATION_TO_HTTP_METHOD.entrySet()) {
            Optional<AnnotationExpr> annotation = methodDecl.getAnnotationByName(entry.getKey());
            if (annotation.isPresent()) {
                String methodPath = annotationPath(annotation.get());
                return Optional.of(new HttpMapping(entry.getValue(), joinPath(classPath(classDecl), methodPath)));
            }
        }
        Optional<AnnotationExpr> requestMapping = methodDecl.getAnnotationByName("RequestMapping");
        if (requestMapping.isPresent()) {
            return Optional.of(new HttpMapping("GET", joinPath(classPath(classDecl), annotationPath(requestMapping.get()))));
        }
        return Optional.empty();
    }

    private static String classPath(ClassOrInterfaceDeclaration classDecl) {
        return classDecl.getAnnotationByName("RequestMapping")
                .map(EndpointExtractor::annotationPath)
                .orElse("");
    }

    private static String annotationPath(AnnotationExpr annotation) {
        if (annotation instanceof SingleMemberAnnotationExpr single) {
            return stripQuotes(single.getMemberValue().toString());
        }
        return "";
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
