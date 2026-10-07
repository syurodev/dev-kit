package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.stream.Collectors;

import static org.junit.jupiter.api.Assertions.*;

class ConfigTest {

    private static void write(Path root, String relative, String content) throws IOException {
        Path file = root.resolve(relative);
        Files.createDirectories(file.getParent());
        Files.writeString(file, content);
    }

    private static ConfigKey key(List<ConfigKey> keys, String name, String profile) {
        return keys.stream().filter(k -> k.key.equals(name) && k.profile.equals(profile)).findFirst()
                .orElseThrow(() -> new AssertionError("no key " + name + " [" + profile + "] in "
                        + keys.stream().map(k -> k.key + "[" + k.profile + "]").collect(Collectors.toList())));
    }

    // --- config files ---------------------------------------------------------------------------

    @Test
    void yamlIsFlattenedWithLinesValuesAndTheEnvVariablesBehindThem(@TempDir Path root) throws IOException {
        write(root, "svc/src/main/resources/application.yaml", """
                fsap:
                  access:
                    module-based-enabled: ${FSAP_ACCESS_MODULE_BASED_ENABLED:false}
                    modules:
                      - billing
                      - orders
                server:
                  port: 8080
                """);
        List<ConfigKey> keys = ConfigFiles.read(root);

        ConfigKey flag = key(keys, "fsap.access.module-based-enabled", "");
        assertEquals("svc/src/main/resources/application.yaml", flag.file);
        assertEquals(3, flag.line);
        assertEquals("${FSAP_ACCESS_MODULE_BASED_ENABLED:false}", flag.value);
        assertEquals(List.of("FSAP_ACCESS_MODULE_BASED_ENABLED"), flag.envVars);
        assertEquals("billing", key(keys, "fsap.access.modules[0]", "").value);
        assertEquals("orders", key(keys, "fsap.access.modules[1]", "").value);
        assertEquals("8080", key(keys, "server.port", "").value);
    }

    @Test
    void theProfileComesFromTheFileNameOrTheDocument(@TempDir Path root) throws IOException {
        write(root, "src/main/resources/application-prod.yml", "feature:\n  on: true\n");
        write(root, "src/main/resources/application.yml", """
                feature:
                  on: false
                ---
                spring:
                  config:
                    activate:
                      on-profile: staging
                feature:
                  on: maybe
                """);
        List<ConfigKey> keys = ConfigFiles.read(root);
        assertEquals("true", key(keys, "feature.on", "prod").value);
        assertEquals("false", key(keys, "feature.on", "").value);
        assertEquals("maybe", key(keys, "feature.on", "staging").value);
    }

    @Test
    void propertiesFilesAreReadToo(@TempDir Path root) throws IOException {
        write(root, "src/main/resources/application.properties", """
                # a comment
                server.port=9090
                app.greeting: hello \\
                  world
                app.flag = ${APP_FLAG:true}
                bare.key
                """);
        List<ConfigKey> keys = ConfigFiles.read(root);
        assertEquals("9090", key(keys, "server.port", "").value);
        assertEquals(3, key(keys, "app.greeting", "").line);
        assertEquals("hello world", key(keys, "app.greeting", "").value);
        assertEquals(List.of("APP_FLAG"), key(keys, "app.flag", "").envVars);
        assertEquals("", key(keys, "bare.key", "").value);
    }

    @Test
    void aSecretIsKeptAsAKeyAndNeverAsAValue(@TempDir Path root) throws IOException {
        write(root, "src/main/resources/application.yml", """
                spring:
                  datasource:
                    url: jdbc:postgresql://db/app
                    password: ${DB_PASSWORD:hunter2}
                jwt:
                  signing-key: abc123
                client:
                  Api-Token: xyz
                """);
        List<ConfigKey> keys = ConfigFiles.read(root);
        ConfigKey password = key(keys, "spring.datasource.password", "");
        assertTrue(password.redacted);
        assertEquals("", password.value);
        assertEquals(List.of("DB_PASSWORD"), password.envVars, "the variable's name is not the secret");
        assertTrue(key(keys, "jwt.signing-key", "").redacted);
        assertTrue(key(keys, "client.Api-Token", "").redacted);
        assertFalse(key(keys, "spring.datasource.url", "").redacted);
        keys.forEach(k -> assertFalse(k.value.contains("hunter2") || k.value.contains("abc123") || k.value.contains("xyz")));
    }

    @Test
    void testResourcesBuildOutputAndBrokenFilesAreNotConfig(@TempDir Path root) throws IOException {
        write(root, "src/test/resources/application.yml", "only.in.tests: 1\n");
        write(root, "target/classes/application.yml", "only.in.target: 1\n");
        write(root, "src/main/resources/application.yml", "broken: [unclosed\n");
        write(root, "src/main/resources/application-ok.yml", "fine: 1\n");
        List<ConfigKey> keys = ConfigFiles.read(root);
        assertEquals(List.of("fine"), keys.stream().map(k -> k.key).toList());
    }

    @Test
    void anAliasBombCannotFillTheIndex(@TempDir Path root) throws IOException {
        StringBuilder yaml = new StringBuilder("a: &a [x, x, x, x, x, x, x, x, x, x]\n");
        String prev = "a";
        for (char c = 'b'; c <= 'k'; c++) {
            yaml.append(c).append(": &").append(c).append(" [*").append(prev).append(", *").append(prev)
                    .append(", *").append(prev).append(", *").append(prev).append(", *").append(prev)
                    .append(", *").append(prev).append(", *").append(prev).append(", *").append(prev)
                    .append(", *").append(prev).append(", *").append(prev).append("]\n");
            prev = String.valueOf(c);
        }
        write(root, "src/main/resources/application.yml", yaml.toString());
        // Either it is refused or it is capped — never 10^10 entries.
        assertTrue(ConfigFiles.read(root).size() <= ConfigFiles.MAX_KEYS_PER_FILE);
    }

    // --- placeholders -----------------------------------------------------------------------------

    @Test
    void placeholdersAreFoundWithTheirDefaults() {
        var found = ConfigExtractor.placeholders("${a.b:false}");
        assertEquals(1, found.size());
        assertEquals("a.b", found.get(0).key());
        assertEquals("false", found.get(0).defaultValue());

        assertEquals(List.of("base", "path"), ConfigExtractor.placeholders("${base}/${path:/x}").stream().map(p -> p.key()).toList());
        assertEquals(List.of("a", "b"), ConfigExtractor.placeholders("${a:${b}}").stream().map(p -> p.key()).toList());
        assertEquals(List.of("flag"), ConfigExtractor.placeholders("#{'${flag}' == 'on'}").stream().map(p -> p.key()).toList());
        assertTrue(ConfigExtractor.placeholders("no placeholder").isEmpty());
        assertTrue(ConfigExtractor.placeholders("${unbalanced").isEmpty());
    }

    @Test
    void kebabCase() {
        assertEquals("module-based-enabled", ConfigExtractor.kebab("moduleBasedEnabled"));
        assertEquals("port", ConfigExtractor.kebab("port"));
    }

    // --- code ---------------------------------------------------------------------------------------

    private static ConfigUsage usage(AnalysisResult r, String kind, String key) {
        List<ConfigUsage> matching = r.configUsages.stream().filter(u -> u.kind.equals(kind) && u.key.equals(key)).toList();
        assertEquals(1, matching.size(), kind + " " + key + " in " + r.configUsages.stream().map(u -> u.kind + ":" + u.key).toList());
        return matching.get(0);
    }

    @Test
    void valueAnnotationsOnFieldsParametersAndSetters(@TempDir Path root) throws IOException {
        write(root, "src/main/java/acme/AccessService.java", """
                package acme;
                import org.springframework.beans.factory.annotation.Value;
                class AccessService {
                    @Value("${fsap.access.module-based-enabled:false}")
                    private boolean moduleBasedAccessEnabled;
                    private final String base;
                    private String mode;
                    AccessService(@Value("${fsap.access.base-url}") String base) { this.base = base; }
                    @Value("${fsap.access.mode:${fsap.access.fallback-mode}}")
                    void setMode(String mode) { this.mode = mode; }
                    boolean enabled() { return moduleBasedAccessEnabled; }
                }
                """);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();

        ConfigUsage flag = usage(r, "value", "fsap.access.module-based-enabled");
        assertEquals("acme.AccessService", flag.from);
        assertEquals("moduleBasedAccessEnabled", flag.field);
        assertEquals("false", flag.defaultValue);
        assertEquals(4, flag.line);
        assertEquals("src/main/java/acme/AccessService.java", flag.file);

        ConfigUsage ctor = usage(r, "value", "fsap.access.base-url");
        assertEquals("base", ctor.field);
        assertEquals("parameter of AccessService", ctor.detail);

        ConfigUsage setter = usage(r, "value", "fsap.access.mode");
        assertEquals("acme.AccessService.setMode(java.lang.String)", setter.from);
        assertEquals("setter", setter.detail);
        usage(r, "value", "fsap.access.fallback-mode"); // the nested default is a key too
    }

    @Test
    void configurationPropertiesBindsEveryFieldIncludingNestedTypesAndRecords(@TempDir Path root) throws IOException {
        write(root, "src/main/java/acme/AccessProperties.java", """
                package acme;
                import org.springframework.boot.context.properties.ConfigurationProperties;
                @ConfigurationProperties(prefix = "fsap.access")
                class AccessProperties {
                    private boolean moduleBasedEnabled;
                    private int cacheTtlSeconds;
                    private Redis redis = new Redis();
                    private static final String IGNORED = "x";
                    static class Redis { private String keyPrefix; private int database; }
                }
                """);
        write(root, "src/main/java/acme/LimitProperties.java", """
                package acme;
                import org.springframework.boot.context.properties.ConfigurationProperties;
                @ConfigurationProperties("limits")
                record LimitProperties(int maxRequests, String windowSize) {}
                """);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();

        ConfigUsage flag = usage(r, "properties", "fsap.access.module-based-enabled");
        assertEquals("acme.AccessProperties", flag.from);
        assertEquals("moduleBasedEnabled", flag.field);
        usage(r, "properties", "fsap.access.cache-ttl-seconds");
        usage(r, "properties", "fsap.access.redis");
        ConfigUsage nested = usage(r, "properties", "fsap.access.redis.key-prefix");
        assertEquals("keyPrefix", nested.field);
        assertTrue(nested.from.endsWith("Redis"), nested.from);
        usage(r, "properties", "fsap.access.redis.database");
        assertTrue(r.configUsages.stream().noneMatch(u -> u.key.contains("ignored")), "a static field is not bound");
        usage(r, "properties", "limits.max-requests");
        usage(r, "properties", "limits.window-size");
    }

    @Test
    void conditionsThatDecideWhetherABeanExists(@TempDir Path root) throws IOException {
        write(root, "src/main/java/acme/Config.java", """
                package acme;
                import org.springframework.boot.autoconfigure.condition.*;
                import org.springframework.context.annotation.*;
                @Configuration
                @Profile({"prod", "!local"})
                class Config {
                    @Bean
                    @ConditionalOnProperty(prefix = "fsap.access", name = "module-based-enabled", havingValue = "true", matchIfMissing = true)
                    Object moduleBased() { return new Object(); }

                    @Bean
                    @ConditionalOnMissingBean(Object.class)
                    Object fallback() { return new Object(); }

                    @Bean
                    @ConditionalOnProperty(name = {"a.one", "a.two"})
                    Object both() { return new Object(); }

                    @Bean
                    @ConditionalOnExpression("${feature.expr:false} and ${feature.other}")
                    Object expr() { return new Object(); }
                }
                """);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();

        ConfigUsage gated = usage(r, "condition", "fsap.access.module-based-enabled");
        assertEquals("acme.Config.moduleBased()", gated.from);
        assertEquals("@ConditionalOnProperty havingValue=true matchIfMissing", gated.detail);
        usage(r, "condition", "a.one");
        usage(r, "condition", "a.two");
        usage(r, "condition", "feature.expr");
        usage(r, "condition", "feature.other");

        ConfigUsage profile = r.configUsages.stream().filter(u -> u.kind.equals("profile")).findFirst().orElseThrow();
        assertEquals("acme.Config", profile.from);
        assertEquals("prod, !local", profile.detail);

        ConfigUsage missing = r.configUsages.stream().filter(u -> u.kind.equals("bean-condition")).findFirst().orElseThrow();
        assertEquals("acme.Config.fallback()", missing.from);
        assertEquals("", missing.key);
        assertTrue(missing.detail.startsWith("@ConditionalOnMissingBean"), missing.detail);
    }

    @Test
    void environmentAndGetenvReadsWithLiteralKeys(@TempDir Path root) throws IOException {
        write(root, "src/main/java/acme/Reader.java", """
                package acme;
                class Reader {
                    private final Env env = null;
                    interface Env { String getProperty(String k); String getProperty(String k, String d); }
                    String a() { return env.getProperty("fsap.read.a"); }
                    String b() { return env.getProperty("fsap.read.b", "dflt"); }
                    String c(String dynamic) { return env.getProperty("prefix." + dynamic); }
                    String d() { return System.getenv("FSAP_READ_D"); }
                }
                """);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();

        assertEquals("acme.Reader.a()", usage(r, "environment", "fsap.read.a").from);
        assertEquals("dflt", usage(r, "environment", "fsap.read.b").defaultValue);
        assertEquals("acme.Reader.d()", usage(r, "env-var", "FSAP_READ_D").from);
        assertTrue(r.configUsages.stream().noneMatch(u -> u.key.startsWith("prefix")), "a key built at runtime is not a key");
    }

    @Test
    void projectConstantsInAnnotationsAreFolded(@TempDir Path root) throws IOException {
        write(root, "src/main/java/acme/Keys.java", "package acme; final class Keys { static final String ENABLED = \"fsap.const.enabled\"; }");
        write(root, "src/main/java/acme/Svc.java", """
                package acme;
                import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
                @ConditionalOnProperty(Keys.ENABLED)
                class Svc {}
                """);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();
        assertEquals("acme.Svc", usage(r, "condition", "fsap.const.enabled").from);
    }

    @Test
    void theIndexCarriesBothHalvesTogether(@TempDir Path root) throws IOException {
        write(root, "src/main/resources/application.yml", "fsap:\n  on: true\n");
        write(root, "src/main/java/acme/S.java", """
                package acme;
                import org.springframework.beans.factory.annotation.Value;
                class S { @Value("${fsap.on}") boolean on; }
                """);
        AnalysisResult r = new ProjectAnalyzer(root, 1).analyze();
        assertEquals(List.of("fsap.on"), r.configKeys.stream().map(k -> k.key).toList());
        assertEquals(List.of("fsap.on"), r.configUsages.stream().map(u -> u.key).toList());
    }
}
