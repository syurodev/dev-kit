package com.devkit.codegraph;

import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;

/**
 * Classifies a class's architectural layer from its annotations, matched by
 * simple name only (e.g. "RestController", not
 * "org.springframework.web.bind.annotation.RestController") — this works
 * whether or not the annotation type itself is resolvable via the type
 * solver, which matters because spring-web/spring-context might not be in
 * the user's local jar cache (see TypeSolverFactory's best-effort policy).
 */
public class LayerClassifier {

    public static String classify(ClassOrInterfaceDeclaration decl) {
        if (decl.getAnnotationByName("RestController").isPresent()
                || decl.getAnnotationByName("Controller").isPresent()) {
            return "controller";
        }
        if (decl.getAnnotationByName("Service").isPresent()) {
            return "service";
        }
        if (decl.getAnnotationByName("Repository").isPresent()) {
            return "repository";
        }
        return "unknown";
    }
}
