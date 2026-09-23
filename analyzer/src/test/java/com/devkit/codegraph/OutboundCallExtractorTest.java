package com.devkit.codegraph;

import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.body.TypeDeclaration;
import com.github.javaparser.ast.expr.MethodCallExpr;
import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.List;

import static org.junit.jupiter.api.Assertions.*;

class OutboundCallExtractorTest {

    private static List<OutboundCall> calls(String source) {
        CompilationUnit cu = StaticJavaParser.parse(source);
        Annotations annotations = Annotations.of(List.of(cu));
        ConstantResolver constants = ConstantResolver.of(List.of(cu));
        List<OutboundCall> out = new ArrayList<>();
        for (TypeDeclaration<?> type : cu.findAll(TypeDeclaration.class)) {
            OutboundCallExtractor.feign(type, annotations, constants).forEach(f -> out.add(f.call()));
        }
        for (MethodCallExpr call : cu.findAll(MethodCallExpr.class)) {
            OutboundCallExtractor.call(call, constants).ifPresent(out::add);
        }
        return out;
    }

    private static OutboundCall only(String source) {
        List<OutboundCall> found = calls(source);
        assertEquals(1, found.size(), "calls found: " + found.size());
        return found.get(0);
    }

    @Test
    void feignClientMethodsCallTheNamedService() {
        List<OutboundCall> found = calls("""
                @FeignClient(name = "billing-service", path = "/api")
                interface BillingClient {
                    @GetMapping("/invoices/{id}")
                    Invoice get(@PathVariable String id);
                    @PostMapping("/invoices")
                    Invoice create(Invoice body);
                    default void helper() {}
                }
                """);
        assertEquals(2, found.size());
        assertEquals("feign", found.get(0).client);
        assertEquals("billing-service", found.get(0).serviceName);
        assertEquals("GET", found.get(0).httpMethod);
        assertEquals("/api/invoices/{id}", found.get(0).url);
        assertEquals("POST", found.get(1).httpMethod);
    }

    @Test
    void feignValueAndUrlPlaceholderAreKeptForTheConfigToFold() {
        OutboundCall call = only("""
                @FeignClient(value = "${clients.billing.name}", url = "${clients.billing.url}/")
                interface BillingClient {
                    @DeleteMapping("/invoices/{id}")
                    void delete(@PathVariable String id);
                }
                """);
        assertEquals("${clients.billing.name}", call.serviceName);
        assertEquals("${clients.billing.url}/invoices/{id}", call.url);
        assertEquals("DELETE", call.httpMethod);
    }

    @Test
    void restTemplateUrlFromValueFieldAndParameterHole() {
        OutboundCall call = only("""
                class RefundGateway {
                    private final RestTemplate restTemplate;
                    @Value("${clients.billing.url}")
                    private String billingUrl;
                    Invoice fetch(String id) {
                        String url = billingUrl + "/invoices/" + id;
                        return restTemplate.getForObject(url, Invoice.class);
                    }
                }
                """);
        assertEquals("rest-template", call.client);
        assertEquals("GET", call.httpMethod);
        assertEquals("${clients.billing.url}/invoices/{id}", call.url);
    }

    @Test
    void restTemplateExchangeTakesTheMethodArgument() {
        OutboundCall call = only("""
                class C {
                    static final String BASE = "http://billing-service";
                    RestTemplate rest = new RestTemplate();
                    void run() {
                        rest.exchange(BASE + "/refunds", HttpMethod.POST, null, Void.class);
                    }
                }
                """);
        assertEquals("POST", call.httpMethod);
        assertEquals("http://billing-service/refunds", call.url);
    }

    @Test
    void constructorInjectedValueIsFollowed() {
        OutboundCall call = only("""
                class C {
                    private final RestTemplate restTemplate;
                    private final String baseUrl;
                    C(RestTemplate restTemplate, @Value("${billing.url:http://billing-service}") String baseUrl) {
                        this.restTemplate = restTemplate;
                        this.baseUrl = baseUrl;
                    }
                    void run() { restTemplate.postForObject(baseUrl + "/refunds", null, Void.class); }
                }
                """);
        assertEquals("${billing.url:http://billing-service}/refunds", call.url);
    }

    @Test
    void webClientBaseUrlFromTheBuilderChain() {
        OutboundCall call = only("""
                class C {
                    private final WebClient client;
                    C(WebClient.Builder builder, @Value("${billing.url}") String url) {
                        this.client = builder.baseUrl(url).build();
                    }
                    Mono<Invoice> get(String id) {
                        return client.get().uri("/invoices/{id}", id).retrieve().bodyToMono(Invoice.class);
                    }
                }
                """);
        assertEquals("web-client", call.client);
        assertEquals("GET", call.httpMethod);
        assertEquals("${billing.url}/invoices/{id}", call.url);
    }

    @Test
    void restClientMethodCallAndInlineCreate() {
        List<OutboundCall> found = calls("""
                class C {
                    void run(RestClient restClient) {
                        restClient.method(HttpMethod.PATCH).uri("http://billing-service/x").retrieve();
                        RestClient.create("http://refund-service").post().uri("/refunds").retrieve();
                    }
                }
                """);
        assertEquals(2, found.size());
        assertEquals("PATCH", found.get(0).httpMethod);
        assertEquals("http://billing-service/x", found.get(0).url);
        assertEquals("rest-client", found.get(1).client);
        assertEquals("http://refund-service/refunds", found.get(1).url);
    }

    @Test
    void injectedClientWithoutVisibleBaseUrlStaysRelative() {
        OutboundCall call = only("""
                class C {
                    private final WebClient billingClient;
                    C(WebClient billingClient) { this.billingClient = billingClient; }
                    void run() { billingClient.get().uri("/invoices").retrieve(); }
                }
                """);
        assertEquals("/invoices", call.url);
    }

    @Test
    void uriBuilderLambdaIsAHole() {
        OutboundCall call = only("""
                class C {
                    WebClient client = WebClient.create("http://billing-service");
                    void run() { client.get().uri(b -> b.path("/x").build()).retrieve(); }
                }
                """);
        assertTrue(call.url.startsWith("http://billing-service{"), call.url);
    }

    @Test
    void otherReceiversAreNotCalls() {
        assertTrue(calls("""
                class C {
                    Map<String, String> map;
                    List<String> list;
                    void run() {
                        map.put("a", "b");
                        list.get(0);
                        builder.get().uri("/x");
                    }
                }
                """).isEmpty());
    }

    @Test
    void reassignedLocalIsAHole() {
        OutboundCall call = only("""
                class C {
                    RestTemplate rest;
                    void run(boolean prod) {
                        String url = "http://billing-service";
                        if (prod) { url = "http://billing.example.com"; }
                        rest.getForObject(url + "/x", String.class);
                    }
                }
                """);
        assertEquals("{url}/x", call.url);
    }
}
