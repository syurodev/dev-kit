package com.devkit.codegraph;

import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

class LayerClassifierTest {

    private ClassOrInterfaceDeclaration parseClass(String source) {
        return StaticJavaParser.parse(source).findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
    }

    @Test
    void classifiesRestController() {
        var decl = parseClass("@org.springframework.web.bind.annotation.RestController class C {}");
        assertEquals("controller", LayerClassifier.classify(decl));
    }

    @Test
    void classifiesController() {
        var decl = parseClass("@org.springframework.stereotype.Controller class C {}");
        assertEquals("controller", LayerClassifier.classify(decl));
    }

    @Test
    void classifiesService() {
        var decl = parseClass("@org.springframework.stereotype.Service class C {}");
        assertEquals("service", LayerClassifier.classify(decl));
    }

    @Test
    void classifiesRepository() {
        var decl = parseClass("@org.springframework.stereotype.Repository class C {}");
        assertEquals("repository", LayerClassifier.classify(decl));
    }

    @Test
    void classifiesUnannotatedAsUnknown() {
        var decl = parseClass("class C {}");
        assertEquals("unknown", LayerClassifier.classify(decl));
    }

    @Test
    void matchesBySimpleAnnotationNameNotFullyQualifiedImport() {
        // Matching by simple annotation name (not requiring symbol resolution
        // of the annotation's own import) means this works even when the
        // Spring annotation itself can't be resolved because spring-web
        // isn't in the local jar cache — matches this analyzer's overall
        // best-effort philosophy: layer/endpoint detection degrades
        // gracefully independent of whether full symbol resolution succeeds.
        var decl = parseClass("@RestController class C {}");
        assertEquals("controller", LayerClassifier.classify(decl));
    }

    @Test
    void classifiesAnUnannotatedSpringDataRepositoryInterface() {
        // Spring finds these by what they extend, and most carry no annotation.
        var decl = parseClass("interface OrderRepository extends JpaRepository<Order, Long> {}");
        assertEquals("repository", LayerClassifier.classify(decl));
        var mongo = parseClass("interface EventRepository extends org.springframework.data.mongodb.repository.MongoRepository<Event, String> {}");
        assertEquals("repository", LayerClassifier.classify(mongo));
    }

    @Test
    void anInterfaceExtendingSomethingElseIsNotARepository() {
        var decl = parseClass("interface Named extends Comparable<Named> {}");
        assertEquals("unknown", LayerClassifier.classify(decl));
    }

    @Test
    void seesThroughTheProjectsOwnMetaAnnotations() {
        // @UseCase is the project's own wrapper around @Service, one level
        // further wrapped by @ApplicationService.
        var cu = StaticJavaParser.parse("""
                @Service @interface UseCase {}
                @UseCase @interface ApplicationService {}
                @ApplicationService class PlaceOrder {}
                """);
        var decl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        assertEquals("service", LayerClassifier.classify(decl, Annotations.of(java.util.List.of(cu))));
        assertEquals("unknown", LayerClassifier.classify(decl), "without the project index only direct annotations count");
    }
}
