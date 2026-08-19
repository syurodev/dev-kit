package com.devkit.codegraph;

import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import org.junit.jupiter.api.Test;

import java.util.Optional;

import static org.junit.jupiter.api.Assertions.*;

class EndpointExtractorTest {

    @Test
    void combinesClassAndMethodLevelPathForPostMapping() {
        var cu = StaticJavaParser.parse("""
                @RestController
                @RequestMapping("/refunds")
                class RefundController {
                    @PostMapping
                    void create() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        Optional<EndpointExtractor.HttpMapping> mapping = EndpointExtractor.extract(classDecl, methodDecl);

        assertTrue(mapping.isPresent());
        assertEquals("POST", mapping.get().httpMethod());
        assertEquals("/refunds", mapping.get().path());
    }

    @Test
    void combinesClassAndMethodPathSegments() {
        var cu = StaticJavaParser.parse("""
                @RestController
                @RequestMapping("/api")
                class RefundController {
                    @GetMapping("/refunds")
                    void list() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        Optional<EndpointExtractor.HttpMapping> mapping = EndpointExtractor.extract(classDecl, methodDecl);

        assertTrue(mapping.isPresent());
        assertEquals("GET", mapping.get().httpMethod());
        assertEquals("/api/refunds", mapping.get().path());
    }

    @Test
    void extractsMethodAndPathFromNormalRequestMappingAnnotation() {
        var cu = StaticJavaParser.parse("""
                @RestController
                @RequestMapping("/api")
                class RefundController {
                    @RequestMapping(value = "/refunds", method = RequestMethod.POST)
                    void create() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        Optional<EndpointExtractor.HttpMapping> mapping = EndpointExtractor.extract(classDecl, methodDecl);

        assertTrue(mapping.isPresent());
        assertEquals("POST", mapping.get().httpMethod());
        assertEquals("/api/refunds", mapping.get().path());
    }

    @Test
    void extractsPathFromNormalAnnotationWithPathAttributeAndArray() {
        var cu = StaticJavaParser.parse("""
                @RestController
                @RequestMapping(path = {"/api"})
                class RefundController {
                    @GetMapping(path = {"/refunds", "/all"})
                    void list() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        Optional<EndpointExtractor.HttpMapping> mapping = EndpointExtractor.extract(classDecl, methodDecl);

        assertTrue(mapping.isPresent());
        assertEquals("GET", mapping.get().httpMethod());
        assertEquals("/api/refunds", mapping.get().path());
    }

    @Test
    void methodWithoutHttpMappingAnnotationIsNotAnEndpoint() {
        var cu = StaticJavaParser.parse("""
                @RestController
                class RefundController {
                    void helper() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        assertTrue(EndpointExtractor.extract(classDecl, methodDecl).isEmpty());
    }
}
