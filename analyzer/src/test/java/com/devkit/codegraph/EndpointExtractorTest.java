package com.devkit.codegraph;

import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import org.junit.jupiter.api.Test;

import java.util.List;

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

        List<EndpointExtractor.HttpMapping> mappings = EndpointExtractor.extract(classDecl, methodDecl);

        assertFalse(mappings.isEmpty());
        assertEquals("POST", mappings.get(0).httpMethod());
        assertEquals("/refunds", mappings.get(0).path());
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

        List<EndpointExtractor.HttpMapping> mappings = EndpointExtractor.extract(classDecl, methodDecl);

        assertFalse(mappings.isEmpty());
        assertEquals("GET", mappings.get(0).httpMethod());
        assertEquals("/api/refunds", mappings.get(0).path());
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

        List<EndpointExtractor.HttpMapping> mappings = EndpointExtractor.extract(classDecl, methodDecl);

        assertFalse(mappings.isEmpty());
        assertEquals("POST", mappings.get(0).httpMethod());
        assertEquals("/api/refunds", mappings.get(0).path());
    }

    @Test
    void emitsOneMappingPerListedMethodInRequestMappingArray() {
        var cu = StaticJavaParser.parse("""
                @RestController
                @RequestMapping("/api")
                class RefundController {
                    @RequestMapping(value = "/sync", method = {RequestMethod.POST, RequestMethod.GET})
                    void sync() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        List<EndpointExtractor.HttpMapping> mappings = EndpointExtractor.extract(classDecl, methodDecl);

        // Spring registers one route per listed method, and so does this, in
        // the order they are listed.
        assertEquals(List.of(
                new EndpointExtractor.HttpMapping("POST", "/api/sync"),
                new EndpointExtractor.HttpMapping("GET", "/api/sync")), mappings);
    }

    @Test
    void emitsOneMappingPerPathCrossedWithEveryClassPath() {
        // Only the first path of an array used to survive, so /all was a
        // route the graph never knew existed.
        var cu = StaticJavaParser.parse("""
                @RestController
                @RequestMapping(path = {"/api", "/v2"})
                class RefundController {
                    @GetMapping(path = {"/refunds", "/all"})
                    void list() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        List<String> paths = EndpointExtractor.extract(classDecl, methodDecl).stream()
                .map(EndpointExtractor.HttpMapping::path).toList();

        assertEquals(List.of("/api/refunds", "/api/all", "/v2/refunds", "/v2/all"), paths);
    }

    @Test
    void plainRequestMappingWithNoMethodMatchesAnyMethod() {
        var cu = StaticJavaParser.parse("""
                @RestController
                class HookController {
                    @RequestMapping("/hook")
                    void hook() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        assertEquals(List.of(new EndpointExtractor.HttpMapping("ANY", "/hook")),
                EndpointExtractor.extract(classDecl, methodDecl));
    }

    @Test
    void plainRequestMappingTakesTheClassLevelMethodWhenItHasNone() {
        var cu = StaticJavaParser.parse("""
                @RestController
                @RequestMapping(value = "/hooks", method = RequestMethod.PUT)
                class HookController {
                    @RequestMapping("/one")
                    void hook() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        assertEquals(List.of(new EndpointExtractor.HttpMapping("PUT", "/hooks/one")),
                EndpointExtractor.extract(classDecl, methodDecl));
    }

    @Test
    void resolvesStringConstantsOfTheSameClassAndConcatenations() {
        var cu = StaticJavaParser.parse("""
                @RestController
                @RequestMapping(UserController.API)
                class UserController {
                    static final String API = "/api";
                    private static final String USERS = "/users";
                    @GetMapping(USERS + "/{id}")
                    void get() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        assertEquals("/api/users/{id}", EndpointExtractor.extract(classDecl, methodDecl).get(0).path());
    }

    @Test
    void anUnresolvableConstantIsAVisiblePlaceholderNotItsSourceText() {
        // Printing the expression made "/Paths.USERS" — a route that looks
        // real. A placeholder is plainly a hole, and still says where from.
        var cu = StaticJavaParser.parse("""
                @RestController
                class UserController {
                    @GetMapping(Paths.USERS)
                    void list() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        assertEquals("/{Paths.USERS}", EndpointExtractor.extract(classDecl, methodDecl).get(0).path());
    }

    @Test
    void httpExchangeOnAControllerIsServedNotCalled() {
        var cu = StaticJavaParser.parse("""
                @RestController
                @HttpExchange("/tenants")
                class TenantsController {
                    @GetExchange("/{orgId}")
                    void get() {}
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        assertEquals(List.of(new EndpointExtractor.HttpMapping("GET", "/tenants/{orgId}")),
                EndpointExtractor.extract(classDecl, methodDecl));
    }

    @Test
    void feignClientInterfaceIsNotAnEndpoint() {
        var cu = StaticJavaParser.parse("""
                @FeignClient(name = "products-service")
                interface ProductsClient {
                    @GetMapping("/v1/internal/tenants/{orgId}/products")
                    List<Product> list(@PathVariable String orgId);
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        assertTrue(EndpointExtractor.extract(classDecl, methodDecl).isEmpty());
    }

    @Test
    void httpExchangeClientInterfaceIsNotAnEndpoint() {
        var cu = StaticJavaParser.parse("""
                @HttpExchange("/v1/internal/tenants")
                interface TenantsClient {
                    @GetMapping("/{orgId}/products")
                    List<Product> list(@PathVariable String orgId);
                }
                """);
        ClassOrInterfaceDeclaration classDecl = cu.findFirst(ClassOrInterfaceDeclaration.class).orElseThrow();
        MethodDeclaration methodDecl = cu.findFirst(MethodDeclaration.class).orElseThrow();

        assertTrue(EndpointExtractor.extract(classDecl, methodDecl).isEmpty());
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
