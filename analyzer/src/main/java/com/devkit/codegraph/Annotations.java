package com.devkit.codegraph;

import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.body.AnnotationDeclaration;
import com.github.javaparser.ast.expr.AnnotationExpr;
import com.github.javaparser.ast.nodeTypes.NodeWithAnnotations;

import java.util.ArrayList;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;

/**
 * Annotation lookup by simple name that also sees through the project's own
 * meta-annotations.
 *
 * A Spring codebase often wraps the stereotypes it uses everywhere:
 * {@code @ApiController} carrying {@code @RestController} and
 * {@code @RequestMapping("/api")}, or {@code @UseCase} carrying
 * {@code @Service}. Spring reads straight through those; a lookup that only
 * checks the annotations written on the class does not, and every class using
 * the wrapper lands in the "unknown" layer with no endpoints. Only annotation
 * types declared in the project's own source can be looked into — a library's
 * meta-annotations live in bytecode this analyzer does not read — and that
 * covers the case that matters, since a team's wrappers are its own code.
 *
 * Matching stays by simple name, for the reason LayerClassifier gives.
 */
public final class Annotations {

    /** No project annotation types: only annotations written directly count. */
    public static final Annotations NONE = new Annotations(Map.of());

    private final Map<String, List<AnnotationDeclaration>> declarationsBySimpleName;

    private Annotations(Map<String, List<AnnotationDeclaration>> declarationsBySimpleName) {
        this.declarationsBySimpleName = declarationsBySimpleName;
    }

    public static Annotations of(List<CompilationUnit> units) {
        Map<String, List<AnnotationDeclaration>> byName = new LinkedHashMap<>();
        for (CompilationUnit cu : units) {
            for (AnnotationDeclaration decl : cu.findAll(AnnotationDeclaration.class)) {
                byName.computeIfAbsent(decl.getNameAsString(), k -> new ArrayList<>()).add(decl);
            }
        }
        return new Annotations(byName);
    }

    public boolean has(NodeWithAnnotations<?> element, String name) {
        return find(element, name).isPresent();
    }

    /**
     * The annotation called {@code name} on the element: written on it
     * directly, or reached through any chain of project annotation types
     * annotated with it. A direct one wins over a meta one, which is also
     * Spring's precedence. The expression returned is the one that carries
     * the attributes — for a meta-annotation, the use on the annotation type.
     */
    public Optional<AnnotationExpr> find(NodeWithAnnotations<?> element, String name) {
        for (AnnotationExpr a : element.getAnnotations()) {
            if (simpleName(a).equals(name)) {
                return Optional.of(a);
            }
        }
        Set<String> visited = new HashSet<>();
        for (AnnotationExpr a : element.getAnnotations()) {
            Optional<AnnotationExpr> meta = findMeta(simpleName(a), name, visited);
            if (meta.isPresent()) {
                return meta;
            }
        }
        return Optional.empty();
    }

    /**
     * The first annotation on the element (direct or meta) whose name is one
     * of {@code names}, paired with which name matched. Used where one of
     * several annotations decides, like the mapping annotations.
     */
    public Optional<Match> findAny(NodeWithAnnotations<?> element, List<String> names) {
        for (AnnotationExpr a : element.getAnnotations()) {
            String simple = simpleName(a);
            if (names.contains(simple)) {
                return Optional.of(new Match(simple, a, a));
            }
        }
        for (AnnotationExpr a : element.getAnnotations()) {
            for (String name : names) {
                Optional<AnnotationExpr> meta = findMeta(simpleName(a), name, new HashSet<>());
                if (meta.isPresent()) {
                    return Optional.of(new Match(name, meta.get(), a));
                }
            }
        }
        return Optional.empty();
    }

    /**
     * One annotation found by findAny. {@code annotation} is the matched
     * annotation itself; {@code written} is what appears on the element,
     * which differs from it when the match came through a meta-annotation.
     */
    public record Match(String name, AnnotationExpr annotation, AnnotationExpr written) {}

    private Optional<AnnotationExpr> findMeta(String annotationType, String name, Set<String> visited) {
        if (!visited.add(annotationType)) {
            return Optional.empty(); // annotation types may annotate each other; stop at a cycle
        }
        for (AnnotationDeclaration decl : declarationsBySimpleName.getOrDefault(annotationType, List.of())) {
            for (AnnotationExpr a : decl.getAnnotations()) {
                if (simpleName(a).equals(name)) {
                    return Optional.of(a);
                }
            }
            for (AnnotationExpr a : decl.getAnnotations()) {
                Optional<AnnotationExpr> deeper = findMeta(simpleName(a), name, visited);
                if (deeper.isPresent()) {
                    return deeper;
                }
            }
        }
        return Optional.empty();
    }

    static String simpleName(AnnotationExpr a) {
        return a.getName().getIdentifier();
    }
}
