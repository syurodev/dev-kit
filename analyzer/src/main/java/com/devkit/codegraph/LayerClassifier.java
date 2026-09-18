package com.devkit.codegraph;

import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import com.github.javaparser.ast.body.TypeDeclaration;

import java.util.Set;

/**
 * Classifies a class's architectural layer from its annotations, matched by
 * simple name only (e.g. "RestController", not
 * "org.springframework.web.bind.annotation.RestController") — this works
 * whether or not the annotation type itself is resolvable via the type
 * solver, which matters because spring-web/spring-context might not be in
 * the user's local jar cache (see TypeSolverFactory's best-effort policy).
 *
 * A Spring Data repository is usually an interface extending JpaRepository
 * with no annotation at all — Spring finds it by what it extends — so those
 * supertypes classify it too, again by simple name.
 */
public class LayerClassifier {

    private static final Set<String> SPRING_DATA_REPOSITORIES = Set.of(
            "Repository", "CrudRepository", "ListCrudRepository",
            "PagingAndSortingRepository", "ListPagingAndSortingRepository",
            "JpaRepository", "JpaSpecificationExecutor", "QuerydslPredicateExecutor",
            "MongoRepository", "ReactiveMongoRepository",
            "ReactiveCrudRepository", "ReactiveSortingRepository", "R2dbcRepository",
            "ElasticsearchRepository", "CassandraRepository", "Neo4jRepository",
            "KeyValueRepository", "RevisionRepository");

    public static String classify(TypeDeclaration<?> decl) {
        return classify(decl, Annotations.NONE);
    }

    public static String classify(TypeDeclaration<?> decl, Annotations annotations) {
        if (annotations.has(decl, "RestController") || annotations.has(decl, "Controller")) {
            return "controller";
        }
        if (annotations.has(decl, "Service")) {
            return "service";
        }
        if (annotations.has(decl, "Repository") || extendsSpringDataRepository(decl)) {
            return "repository";
        }
        return "unknown";
    }

    private static boolean extendsSpringDataRepository(TypeDeclaration<?> decl) {
        if (!(decl instanceof ClassOrInterfaceDeclaration type) || !type.isInterface()) {
            return false;
        }
        return type.getExtendedTypes().stream()
                .anyMatch(t -> SPRING_DATA_REPOSITORIES.contains(t.getNameAsString()));
    }
}
