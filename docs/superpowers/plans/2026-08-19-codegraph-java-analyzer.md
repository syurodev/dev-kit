# CodeGraph Java Analyzer CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the standalone Java CLI (JavaParser + JavaSymbolSolver) that walks a Java project's source, detects Spring-style layers/endpoints, resolves the call graph best-effort, and writes an `index.json` that `internal/codegraph`'s Go layer (already built — see `docs/superpowers/plans/2026-08-18-codegraph-core-go.md`) can consume. Ends with wiring `internal/codegraph`'s `subprocessAnalyzer` (currently a stub returning `ErrAnalyzerNotConfigured`) to actually invoke this jar.

**Architecture:** A new standalone Maven project at `analyzer/` — not a Go module, not inside `internal/` — built as a single self-contained "fat jar" via `maven-shade-plugin` so it can be invoked as `java -jar codegraph-analyzer.jar <projectPath> <outputPath>` without a runtime classpath setup. Internally: `CombinedTypeSolver` (`ReflectionTypeSolver` for the JDK + a `JavaParserTypeSolver` per discovered source root + a `JarTypeSolver` per `.jar` found under `~/.m2/repository` and `~/.gradle/caches`) wrapped in `JavaSymbolSolver`, set on a `ParserConfiguration` — this is "best-effort" classpath discovery by design (NFR-1: never runs `mvn`/`gradle` to resolve the real dependency tree). Each `.java` file is parsed into a `CompilationUnit`; classes get a `Node` (layer from `@RestController`/`@Service`/`@Repository`/none found); methods get a `Node` (fully-qualified label, from `ResolvedMethodDeclaration.getQualifiedName()`); method-call expressions get resolved via `MethodCallExpr.resolve()` into call `Edge`s, with `UnsolvedSymbolException` caught per-call (not fatal — becomes a gap in the graph, not a crash, per FR-8). HTTP-mapping annotations on controller-layer methods become `Endpoint`s.

**Tech Stack:** Java 21+ (this machine has OpenJDK 25 already), Maven (**not currently installed on this machine** — Task 1 Step 0 installs it), `com.github.javaparser:javaparser-symbol-solver-core:3.28.2` (pulls `javaparser-core` transitively), JUnit 5, `maven-shade-plugin` for the fat jar, Jackson (`com.fasterxml.jackson.core:jackson-databind`) for JSON output — chosen over hand-rolled JSON since the schema has nested lists of records and Jackson's the de facto standard for exactly this shape in the Java ecosystem, not because DevKit uses it anywhere else (it doesn't; this is a new, isolated Java project with its own dependency set, unrelated to any Go or frontend dependency choices elsewhere in this repo).

**Explicit scope boundary (read this before questioning why X isn't here):** This plan does not touch the frontend (no UI to pick a project folder yet — that's `SetProjectPath`'s caller, a separate follow-up plan) and does not implement DI-edge detection (`Edge.Kind = "di"`) — call edges are extracted purely from `MethodCallExpr` resolution, which already reveals "Controller calls Service.method()" without needing to separately model Spring's IoC container; nothing on the Go side consumes `"di"` edges yet, and precisely determining *which concrete bean* satisfies an `@Autowired` field is a materially bigger problem than a "best-effort MVP" analyzer should take on. If a future need for it appears, that's its own plan. This plan also does not add Maven Wrapper (`mvnw`) for portable/CI builds — `brew install maven` unblocks local development today; wrapper-script packaging for CI is a smaller follow-up once there's an actual CI job that needs it, not a blocker for building the analyzer itself.

---

## Data contract: `index.json`

This is the JSON shape both sides must agree on exactly — it maps 1:1 onto the `AnalysisResult`/`Node`/`Edge`/`Endpoint` Go types already defined in `internal/codegraph/codegraph.go`.

```json
{
  "nodes": [
    {"id": "com.acme.refund.RefundService", "label": "com.acme.refund.RefundService", "kind": "class", "layer": "service", "filePath": "src/main/java/com/acme/refund/RefundService.java", "line": 10, "endLine": 40},
    {"id": "com.acme.refund.RefundService.process(com.acme.refund.RefundRequest)", "label": "com.acme.refund.RefundService.process", "kind": "method", "layer": "service", "filePath": "src/main/java/com/acme/refund/RefundService.java", "line": 15, "endLine": 25}
  ],
  "edges": [
    {"from": "com.acme.refund.RefundController.create(com.acme.refund.RefundRequest)", "to": "com.acme.refund.RefundService.process(com.acme.refund.RefundRequest)", "kind": "call"}
  ],
  "endpoints": [
    {"id": "POST /refunds", "httpMethod": "POST", "path": "/refunds", "entryNode": "com.acme.refund.RefundController.create(com.acme.refund.RefundRequest)"}
  ]
}
```

- **Method `id`** = `declaringType().getQualifiedName() + "." + getSignature()` (JavaParser's `ResolvedMethodLikeDeclaration.getSignature()` already renders `name(paramType1, paramType2)`) — guarantees uniqueness across overloads. **Method `label`** = `getQualifiedName()` alone (no params) — matches the plain dotted-FQN style already committed to in `base-doc`'s `codegraph.mdx` Plan-first-flow examples (`"com.acme.refund.RefundService.process"`), at the cost of not disambiguating overloads by label alone — an accepted, already-documented limitation, not something to fix here.
- **Class `id`/`label`** = the qualified class name alone (classes can't overload, no signature suffix needed).
- **Endpoint `id`** = `"<HTTP method> <path>"` (e.g. `"POST /refunds"`) — deterministic, human-readable, stable across re-index as long as the route itself doesn't change.
- `filePath` is relative to the project root passed on the CLI, not absolute — the Go side already treats `Node.FilePath` this way (see `CodeGraphGetCodeSnippet`'s design in `codegraph.mdx`).

---

## File Structure

Create (new Maven project, `analyzer/`):
- `analyzer/pom.xml` — dependencies, `maven-shade-plugin` config for the fat jar.
- `analyzer/src/main/java/com/devkit/codegraph/Main.java` — CLI entrypoint: parses `args[0]` (project path) and `args[1]` (output path), orchestrates the pipeline, writes `index.json`.
- `analyzer/src/main/java/com/devkit/codegraph/model/Node.java`, `Edge.java`, `Endpoint.java`, `AnalysisResult.java` — plain data classes (Jackson-serializable POJOs) mirroring the JSON contract above.
- `analyzer/src/main/java/com/devkit/codegraph/TypeSolverFactory.java` — builds the `CombinedTypeSolver` (reflection + project source roots + discovered jars).
- `analyzer/src/main/java/com/devkit/codegraph/LayerClassifier.java` — maps a class's annotations to `"controller" | "service" | "repository" | "unknown"`.
- `analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java` — the main pipeline: parse all `.java` files, build class/method `Node`s, walk call expressions for `Edge`s, detect `Endpoint`s.
- `analyzer/src/main/java/com/devkit/codegraph/EndpointExtractor.java` — HTTP-mapping annotation → `Endpoint`.
- `analyzer/src/test/java/com/devkit/codegraph/*Test.java` — one test class per production class above (JUnit 5).
- `analyzer/src/test/resources/sample-project/` — a tiny fixture Spring-style Java project (Controller → Service → Repository, 3 classes) used by the end-to-end test in Task 7 and by the acceptance criteria in `codegraph.mdx`.

Modify (Go side, wiring the finished jar in):
- `internal/codegraph/codegraph.go` — add `json:"..."` struct tags to `Node`, `Edge`, `Endpoint`, `AnalysisResult` (currently untagged — they were only ever passed in-process between `Analyzer.Analyze()` and `Store.ReplaceIndex()`, ready for JSON now that a real subprocess analyzer produces it).
- `internal/codegraph/analyzer_subprocess.go` — replace the `ErrAnalyzerNotConfigured` stub with a real implementation that execs the built jar and parses its `index.json` output.
- `internal/codegraph/analyzer_subprocess_test.go` — replace the "always errors" test with real subprocess-invocation tests.

---

## Task 1: Maven project skeleton + CLI entrypoint (walking skeleton)

**Files:**
- Create: `analyzer/pom.xml`
- Create: `analyzer/src/main/java/com/devkit/codegraph/Main.java`
- Test: `analyzer/src/test/java/com/devkit/codegraph/MainTest.java`

- [ ] **Step 0: Install Maven**

This machine has `java` (OpenJDK 25) but not `mvn`. Install it:

```bash
brew install maven
```

Verify:

```bash
mvn -version
```

Expected: prints a Maven version (3.9.x or later) and confirms it's using a Java 21+ runtime.

- [ ] **Step 1: Write the failing test**

```java
// analyzer/src/test/java/com/devkit/codegraph/MainTest.java
package com.devkit.codegraph;

import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

import static org.junit.jupiter.api.Assertions.*;

class MainTest {

    @Test
    void writesEmptyIndexForProjectWithNoJavaFiles(@TempDir Path tempDir) throws IOException {
        Path projectDir = tempDir.resolve("project");
        Files.createDirectories(projectDir);
        Path outputFile = tempDir.resolve("index.json");

        Main.run(projectDir.toString(), outputFile.toString());

        assertTrue(Files.exists(outputFile), "index.json should be created");
        String content = Files.readString(outputFile);
        assertTrue(content.contains("\"nodes\""), "output should have a nodes field: " + content);
        assertTrue(content.contains("\"edges\""), "output should have an edges field: " + content);
        assertTrue(content.contains("\"endpoints\""), "output should have an endpoints field: " + content);
    }
}
```

- [ ] **Step 2: Write `pom.xml`**

```xml
<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>

  <groupId>com.devkit</groupId>
  <artifactId>codegraph-analyzer</artifactId>
  <version>1.0.0</version>
  <packaging>jar</packaging>

  <properties>
    <maven.compiler.source>21</maven.compiler.source>
    <maven.compiler.target>21</maven.compiler.target>
    <project.build.sourceEncoding>UTF-8</project.build.sourceEncoding>
    <javaparser.version>3.28.2</javaparser.version>
    <jackson.version>2.18.2</jackson.version>
    <junit.version>5.11.4</junit.version>
  </properties>

  <dependencies>
    <dependency>
      <groupId>com.github.javaparser</groupId>
      <artifactId>javaparser-symbol-solver-core</artifactId>
      <version>${javaparser.version}</version>
    </dependency>
    <dependency>
      <groupId>com.fasterxml.jackson.core</groupId>
      <artifactId>jackson-databind</artifactId>
      <version>${jackson.version}</version>
    </dependency>
    <dependency>
      <groupId>org.junit.jupiter</groupId>
      <artifactId>junit-jupiter</artifactId>
      <version>${junit.version}</version>
      <scope>test</scope>
    </dependency>
  </dependencies>

  <build>
    <plugins>
      <plugin>
        <groupId>org.apache.maven.plugins</groupId>
        <artifactId>maven-surefire-plugin</artifactId>
        <version>3.5.2</version>
      </plugin>
      <plugin>
        <groupId>org.apache.maven.plugins</groupId>
        <artifactId>maven-shade-plugin</artifactId>
        <version>3.6.0</version>
        <executions>
          <execution>
            <phase>package</phase>
            <goals><goal>shade</goal></goals>
            <configuration>
              <transformers>
                <transformer implementation="org.apache.maven.plugins.shade.resource.ManifestResourceTransformer">
                  <mainClass>com.devkit.codegraph.Main</mainClass>
                </transformer>
              </transformers>
              <finalName>codegraph-analyzer</finalName>
            </configuration>
          </execution>
        </executions>
      </plugin>
    </plugins>
  </build>
</project>
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd analyzer && mvn test -Dtest=MainTest
```

Expected: FAIL — `Main` class doesn't exist yet, compile error.

- [ ] **Step 4: Write the minimal `Main`**

```java
// analyzer/src/main/java/com/devkit/codegraph/Main.java
package com.devkit.codegraph;

import com.fasterxml.jackson.databind.ObjectMapper;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

/**
 * CLI entrypoint. Usage: java -jar codegraph-analyzer.jar <projectPath> <outputPath>
 */
public class Main {

    public static void main(String[] args) throws IOException {
        if (args.length != 2) {
            System.err.println("Usage: codegraph-analyzer <projectPath> <outputPath>");
            System.exit(2);
        }
        run(args[0], args[1]);
    }

    public static void run(String projectPath, String outputPath) throws IOException {
        AnalysisResultWriter.write(Path.of(outputPath), new AnalysisResult());
    }
}
```

```java
// analyzer/src/main/java/com/devkit/codegraph/AnalysisResultWriter.java
package com.devkit.codegraph;

import com.fasterxml.jackson.databind.ObjectMapper;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;

public class AnalysisResultWriter {
    private static final ObjectMapper MAPPER = new ObjectMapper();

    public static void write(Path outputPath, AnalysisResult result) throws IOException {
        Files.writeString(outputPath, MAPPER.writeValueAsString(result));
    }
}
```

```java
// analyzer/src/main/java/com/devkit/codegraph/AnalysisResult.java
package com.devkit.codegraph;

import java.util.ArrayList;
import java.util.List;

public class AnalysisResult {
    public List<Node> nodes = new ArrayList<>();
    public List<Edge> edges = new ArrayList<>();
    public List<Endpoint> endpoints = new ArrayList<>();
}
```

```java
// analyzer/src/main/java/com/devkit/codegraph/Node.java
package com.devkit.codegraph;

public class Node {
    public String id;
    public String label;
    public String kind;   // "class" | "method"
    public String layer;  // "controller" | "service" | "repository" | "unknown"
    public String filePath;
    public int line;
    public int endLine;
}
```

```java
// analyzer/src/main/java/com/devkit/codegraph/Edge.java
package com.devkit.codegraph;

public class Edge {
    public String from;
    public String to;
    public String kind; // "call"

    public Edge() {}

    public Edge(String from, String to, String kind) {
        this.from = from;
        this.to = to;
        this.kind = kind;
    }
}
```

```java
// analyzer/src/main/java/com/devkit/codegraph/Endpoint.java
package com.devkit.codegraph;

public class Endpoint {
    public String id;
    public String httpMethod;
    public String path;
    public String entryNode;
}
```

(Public fields, no getters/setters: Jackson serializes public fields by default with zero configuration — the simplest thing that works for a straight data-transfer shape with no invariants to protect. `Edge` gets a convenience constructor since it's built imperatively in a loop later; the others are populated field-by-field where they're constructed.)

- [ ] **Step 5: Run test to verify it passes**

```bash
cd analyzer && mvn test -Dtest=MainTest
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add analyzer/
git commit -m "feat(codegraph-analyzer): Maven project skeleton, CLI entrypoint writes empty index.json"
```

---

## Task 2: Type solver setup (best-effort classpath, no build tool invocation)

**Files:**
- Create: `analyzer/src/main/java/com/devkit/codegraph/TypeSolverFactory.java`
- Test: `analyzer/src/test/java/com/devkit/codegraph/TypeSolverFactoryTest.java`
- Test fixture: `analyzer/src/test/resources/sample-project/src/main/java/com/acme/refund/RefundController.java`, `RefundService.java`, `RefundRepository.java`, `PaymentGateway.java` (the shared fixture Task 7's end-to-end test also uses)

- [ ] **Step 1: Write the fixture project**

```java
// analyzer/src/test/resources/sample-project/src/main/java/com/acme/refund/RefundController.java
package com.acme.refund;

public class RefundController {
    private RefundService refundService;

    public RefundController(RefundService refundService) {
        this.refundService = refundService;
    }

    public String create(String requestId) {
        return refundService.process(requestId);
    }
}
```

```java
// analyzer/src/test/resources/sample-project/src/main/java/com/acme/refund/RefundService.java
package com.acme.refund;

public class RefundService {
    private RefundRepository refundRepository;

    public RefundService(RefundRepository refundRepository) {
        this.refundRepository = refundRepository;
    }

    public String process(String requestId) {
        return refundRepository.save(requestId);
    }
}
```

```java
// analyzer/src/test/resources/sample-project/src/main/java/com/acme/refund/RefundRepository.java
package com.acme.refund;

public class RefundRepository {
    public String save(String requestId) {
        return "saved:" + requestId;
    }
}
```

These three plain classes (no Spring annotations yet — those get added to a *second*, annotated fixture variant in Task 5/6, kept separate so each task's fixture only has what that task's feature needs) are enough to prove symbol resolution and call-graph extraction across files.

- [ ] **Step 2: Write the failing test**

```java
// analyzer/src/test/java/com/devkit/codegraph/TypeSolverFactoryTest.java
package com.devkit.codegraph;

import com.github.javaparser.ParserConfiguration;
import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.ast.expr.MethodCallExpr;
import com.github.javaparser.resolution.declarations.ResolvedMethodDeclaration;
import com.github.javaparser.symbolsolver.JavaSymbolSolver;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.nio.file.Path;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;

class TypeSolverFactoryTest {

    @Test
    void resolvesMethodCallAcrossFilesInSourceRoot() throws IOException {
        Path sourceRoot = Path.of("src/test/resources/sample-project/src/main/java");
        var typeSolver = TypeSolverFactory.build(sourceRoot);

        StaticJavaParser.setConfiguration(new ParserConfiguration().setSymbolResolver(new JavaSymbolSolver(typeSolver)));

        CompilationUnit cu = StaticJavaParser.parse(sourceRoot.resolve("com/acme/refund/RefundController.java"));
        List<MethodCallExpr> calls = cu.findAll(MethodCallExpr.class);
        assertEquals(1, calls.size(), "expected exactly one method call in RefundController.create");

        ResolvedMethodDeclaration resolved = calls.get(0).resolve();
        assertEquals("com.acme.refund.RefundService.process", resolved.getQualifiedName());
    }
}
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd analyzer && mvn test -Dtest=TypeSolverFactoryTest
```

Expected: FAIL — `TypeSolverFactory` doesn't exist yet.

- [ ] **Step 4: Write the implementation**

```java
// analyzer/src/main/java/com/devkit/codegraph/TypeSolverFactory.java
package com.devkit.codegraph;

import com.github.javaparser.ParserConfiguration;
import com.github.javaparser.symbolsolver.model.resolution.TypeSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.CombinedTypeSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.JarTypeSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.JavaParserTypeSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.ReflectionTypeSolver;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.stream.Stream;

/**
 * Builds a best-effort CombinedTypeSolver: the JDK (reflection), the
 * project's own source (so cross-file resolution works), and every .jar
 * found under the user's local Maven/Gradle caches. This deliberately never
 * runs `mvn`/`gradle` to compute the real dependency tree (NFR-1) — some
 * calls into third-party libraries the project doesn't happen to have
 * locally cached will be unresolved, and that's an accepted v1 limitation,
 * not a bug (see codegraph.mdx's Known ceiling).
 */
public class TypeSolverFactory {

    public static CombinedTypeSolver build(Path projectSourceRoot) throws IOException {
        CombinedTypeSolver typeSolver = new CombinedTypeSolver();
        typeSolver.add(new ReflectionTypeSolver());
        typeSolver.add(new JavaParserTypeSolver(projectSourceRoot));

        addJarsUnder(typeSolver, Path.of(System.getProperty("user.home"), ".m2", "repository"));
        addJarsUnder(typeSolver, Path.of(System.getProperty("user.home"), ".gradle", "caches", "modules-2", "files-2.1"));

        return typeSolver;
    }

    private static void addJarsUnder(CombinedTypeSolver typeSolver, Path root) throws IOException {
        if (!Files.isDirectory(root)) {
            return; // no local Maven/Gradle cache at this path — fine, just fewer resolvable jars
        }
        try (Stream<Path> walk = Files.walk(root)) {
            walk.filter(p -> p.toString().endsWith(".jar"))
                .forEach(jar -> {
                    try {
                        typeSolver.add(new JarTypeSolver(jar));
                    } catch (IOException e) {
                        // A single unreadable/corrupt jar in a huge local cache must not
                        // abort the whole analysis — skip it and keep going.
                    }
                });
        }
    }
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd analyzer && mvn test -Dtest=TypeSolverFactoryTest
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add analyzer/
git commit -m "feat(codegraph-analyzer): best-effort type solver (JDK + project source + local jar caches)"
```

---

## Task 3: Layer classification for classes

**Files:**
- Create: `analyzer/src/main/java/com/devkit/codegraph/LayerClassifier.java`
- Test: `analyzer/src/test/java/com/devkit/codegraph/LayerClassifierTest.java`

- [ ] **Step 1: Write the failing test**

```java
// analyzer/src/test/java/com/devkit/codegraph/LayerClassifierTest.java
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
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd analyzer && mvn test -Dtest=LayerClassifierTest
```

Expected: FAIL — `LayerClassifier` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```java
// analyzer/src/main/java/com/devkit/codegraph/LayerClassifier.java
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
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd analyzer && mvn test -Dtest=LayerClassifierTest
```

Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add analyzer/
git commit -m "feat(codegraph-analyzer): classify class layer from Spring annotations by simple name"
```

---

## Task 4: `ProjectAnalyzer` — class + method `Node`s

**Files:**
- Create: `analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java`
- Test: `analyzer/src/test/java/com/devkit/codegraph/ProjectAnalyzerTest.java`

- [ ] **Step 1: Write the failing test**

```java
// analyzer/src/test/java/com/devkit/codegraph/ProjectAnalyzerTest.java
package com.devkit.codegraph;

import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.nio.file.Path;
import java.util.List;

import static org.junit.jupiter.api.Assertions.*;

class ProjectAnalyzerTest {

    @Test
    void findsClassAndMethodNodesInSampleProject() throws IOException {
        Path projectRoot = Path.of("src/test/resources/sample-project");
        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        List<Node> classNodes = result.nodes.stream().filter(n -> n.kind.equals("class")).toList();
        assertEquals(3, classNodes.size(), "expected RefundController, RefundService, RefundRepository");

        Node service = classNodes.stream()
                .filter(n -> n.label.equals("com.acme.refund.RefundService"))
                .findFirst().orElseThrow(() -> new AssertionError("RefundService class node not found: " + result.nodes));
        assertEquals("unknown", service.layer, "no Spring annotations in this fixture yet — layer detection is tested separately in LayerClassifierTest");
        assertTrue(service.filePath.endsWith("RefundService.java"), "filePath = " + service.filePath);
        assertTrue(service.line > 0 && service.endLine >= service.line, "line=%d endLine=%d".formatted(service.line, service.endLine));

        List<Node> methodNodes = result.nodes.stream().filter(n -> n.kind.equals("method")).toList();
        boolean hasProcessMethod = methodNodes.stream()
                .anyMatch(n -> n.label.equals("com.acme.refund.RefundService.process"));
        assertTrue(hasProcessMethod, "expected a method node for RefundService.process: " + methodNodes);
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd analyzer && mvn test -Dtest=ProjectAnalyzerTest
```

Expected: FAIL — `ProjectAnalyzer` doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```java
// analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java
package com.devkit.codegraph;

import com.github.javaparser.ParserConfiguration;
import com.github.javaparser.Range;
import com.github.javaparser.StaticJavaParser;
import com.github.javaparser.ast.CompilationUnit;
import com.github.javaparser.ast.body.ClassOrInterfaceDeclaration;
import com.github.javaparser.ast.body.MethodDeclaration;
import com.github.javaparser.resolution.declarations.ResolvedMethodDeclaration;
import com.github.javaparser.symbolsolver.JavaSymbolSolver;
import com.github.javaparser.symbolsolver.resolution.typesolvers.CombinedTypeSolver;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Optional;
import java.util.stream.Stream;

public class ProjectAnalyzer {

    private final Path projectRoot;
    private final Path sourceRoot;

    public ProjectAnalyzer(Path projectRoot) {
        this.projectRoot = projectRoot;
        // v1 assumes the conventional Maven/Gradle layout. A project using a
        // different layout just won't have any of its files found below —
        // it produces an empty (not crashing) index, which is a known
        // limitation to document, not a special case to handle here.
        this.sourceRoot = projectRoot.resolve("src/main/java");
    }

    public AnalysisResult analyze() throws IOException {
        AnalysisResult result = new AnalysisResult();
        if (!Files.isDirectory(sourceRoot)) {
            return result;
        }

        CombinedTypeSolver typeSolver = TypeSolverFactory.build(sourceRoot);
        StaticJavaParser.setConfiguration(new ParserConfiguration().setSymbolResolver(new JavaSymbolSolver(typeSolver)));

        try (Stream<Path> walk = Files.walk(sourceRoot)) {
            for (Path javaFile : walk.filter(p -> p.toString().endsWith(".java")).toList()) {
                analyzeFile(javaFile, result);
            }
        }
        return result;
    }

    private void analyzeFile(Path javaFile, AnalysisResult result) throws IOException {
        CompilationUnit cu;
        try {
            cu = StaticJavaParser.parse(javaFile);
        } catch (Exception e) {
            // A single file with a parse error must not abort indexing the
            // rest of the project (FR-8's "unresolved, not fatal" principle
            // applied one level up: unparseable, not just unresolvable).
            return;
        }

        String relativePath = projectRoot.relativize(javaFile).toString();

        for (ClassOrInterfaceDeclaration classDecl : cu.findAll(ClassOrInterfaceDeclaration.class)) {
            String className = classDecl.resolve().getQualifiedName();
            String layer = LayerClassifier.classify(classDecl);

            result.nodes.add(classNode(className, layer, relativePath, classDecl.getRange()));

            for (MethodDeclaration methodDecl : classDecl.getMethods()) {
                addMethodNode(methodDecl, layer, relativePath, result);
            }
        }
    }

    private void addMethodNode(MethodDeclaration methodDecl, String layer, String relativePath, AnalysisResult result) {
        ResolvedMethodDeclaration resolved;
        try {
            resolved = methodDecl.resolve();
        } catch (Exception e) {
            return; // can't resolve this method's own declaration — skip it, not fatal
        }

        Node node = new Node();
        node.id = resolved.declaringType().getQualifiedName() + "." + resolved.getSignature();
        node.label = resolved.getQualifiedName();
        node.kind = "method";
        node.layer = layer;
        node.filePath = relativePath;
        Range range = methodDecl.getRange().orElse(null);
        if (range != null) {
            node.line = range.begin.line;
            node.endLine = range.end.line;
        }
        result.nodes.add(node);
    }

    private Node classNode(String qualifiedName, String layer, String relativePath, Optional<Range> range) {
        Node node = new Node();
        node.id = qualifiedName;
        node.label = qualifiedName;
        node.kind = "class";
        node.layer = layer;
        node.filePath = relativePath;
        range.ifPresent(r -> {
            node.line = r.begin.line;
            node.endLine = r.end.line;
        });
        return node;
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd analyzer && mvn test -Dtest=ProjectAnalyzerTest
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add analyzer/
git commit -m "feat(codegraph-analyzer): extract class and method nodes with layer, file, line range"
```

---

## Task 5: Call graph edges

**Files:**
- Modify: `analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java`
- Modify: `analyzer/src/test/java/com/devkit/codegraph/ProjectAnalyzerTest.java`

- [ ] **Step 1: Write the failing test**

Add to `ProjectAnalyzerTest.java`:

```java
    @Test
    void findsCallEdgeAcrossControllerToServiceToRepository() throws IOException {
        Path projectRoot = Path.of("src/test/resources/sample-project");
        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        String controllerCreate = "com.acme.refund.RefundController.create";
        String serviceProcess = "com.acme.refund.RefundService.process";
        String repoSave = "com.acme.refund.RefundRepository.save";

        assertTrue(hasCallEdge(result, controllerCreate, serviceProcess),
                "expected call edge RefundController.create -> RefundService.process, got: " + result.edges);
        assertTrue(hasCallEdge(result, serviceProcess, repoSave),
                "expected call edge RefundService.process -> RefundRepository.save, got: " + result.edges);
    }

    private boolean hasCallEdge(AnalysisResult result, String fromLabel, String toLabel) {
        return result.edges.stream().anyMatch(e ->
                e.kind.equals("call")
                        && labelOf(result, e.from).equals(fromLabel)
                        && labelOf(result, e.to).equals(toLabel));
    }

    private String labelOf(AnalysisResult result, String nodeId) {
        return result.nodes.stream().filter(n -> n.id.equals(nodeId)).findFirst()
                .map(n -> n.label).orElse("<missing:" + nodeId + ">");
    }
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd analyzer && mvn test -Dtest=ProjectAnalyzerTest#findsCallEdgeAcrossControllerToServiceToRepository
```

Expected: FAIL — no edges are produced yet (`result.edges` is empty).

- [ ] **Step 3: Add call-edge extraction**

Modify `addMethodNode` in `ProjectAnalyzer.java` to also walk the method body for calls, right after adding the method's own node:

```java
    private void addMethodNode(MethodDeclaration methodDecl, String layer, String relativePath, AnalysisResult result) {
        ResolvedMethodDeclaration resolved;
        try {
            resolved = methodDecl.resolve();
        } catch (Exception e) {
            return;
        }

        Node node = new Node();
        node.id = resolved.declaringType().getQualifiedName() + "." + resolved.getSignature();
        node.label = resolved.getQualifiedName();
        node.kind = "method";
        node.layer = layer;
        node.filePath = relativePath;
        Range range = methodDecl.getRange().orElse(null);
        if (range != null) {
            node.line = range.begin.line;
            node.endLine = range.end.line;
        }
        result.nodes.add(node);

        addCallEdges(methodDecl, node.id, result);
    }

    private void addCallEdges(MethodDeclaration methodDecl, String fromNodeId, AnalysisResult result) {
        for (com.github.javaparser.ast.expr.MethodCallExpr call : methodDecl.findAll(com.github.javaparser.ast.expr.MethodCallExpr.class)) {
            ResolvedMethodDeclaration target;
            try {
                target = call.resolve();
            } catch (Exception e) {
                // Unresolved call (missing jar in the local cache, dynamic
                // dispatch the solver can't follow, etc.) — FR-8: this is a
                // gap in the graph, not a fatal error for the whole project.
                continue;
            }
            String toNodeId = target.declaringType().getQualifiedName() + "." + target.getSignature();
            result.edges.add(new Edge(fromNodeId, toNodeId, "call"));
        }
    }
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd analyzer && mvn test -Dtest=ProjectAnalyzerTest
```

Expected: PASS (all `ProjectAnalyzerTest` tests, including the new one).

- [ ] **Step 5: Add a test proving unresolved calls don't crash the whole analysis**

```java
    @Test
    void unresolvedCallDoesNotAbortAnalysisOfRestOfFile(@TempDir Path tempDir) throws IOException {
        Path projectRoot = tempDir.resolve("project");
        Path pkg = projectRoot.resolve("src/main/java/com/acme/broken");
        Files.createDirectories(pkg);
        Files.writeString(pkg.resolve("Broken.java"), """
                package com.acme.broken;
                public class Broken {
                    public void a() {
                        new com.totally.unknown.NotOnClasspath().doSomething();
                    }
                    public void b() {
                        System.out.println("still parsed fine");
                    }
                }
                """);

        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        boolean hasClassNode = result.nodes.stream().anyMatch(n -> n.label.equals("com.acme.broken.Broken"));
        assertTrue(hasClassNode, "class node should still exist despite an unresolvable call inside one of its methods");
        boolean hasMethodB = result.nodes.stream().anyMatch(n -> n.label.equals("com.acme.broken.Broken.b"));
        assertTrue(hasMethodB, "method b() should still be indexed even though method a() has an unresolvable call");
    }
```

(Needs `import org.junit.jupiter.api.io.TempDir;` and `import java.nio.file.Files;` added to the test file's imports if not already present from Task 1.)

- [ ] **Step 6: Run test to verify it passes**

```bash
cd analyzer && mvn test -Dtest=ProjectAnalyzerTest
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add analyzer/
git commit -m "feat(codegraph-analyzer): extract call-graph edges, unresolved calls are non-fatal"
```

---

## Task 6: Endpoint detection

**Files:**
- Create: `analyzer/src/main/java/com/devkit/codegraph/EndpointExtractor.java`
- Test: `analyzer/src/test/java/com/devkit/codegraph/EndpointExtractorTest.java`
- Modify: `analyzer/src/main/java/com/devkit/codegraph/ProjectAnalyzer.java` (call the extractor)
- Test fixture addition: `analyzer/src/test/resources/sample-project/src/main/java/com/acme/refund/RefundController.java` (add Spring annotations)

- [ ] **Step 1: Add Spring annotations to the fixture controller**

```java
// analyzer/src/test/resources/sample-project/src/main/java/com/acme/refund/RefundController.java
package com.acme.refund;

@RestController
@RequestMapping("/refunds")
public class RefundController {
    private RefundService refundService;

    public RefundController(RefundService refundService) {
        this.refundService = refundService;
    }

    @PostMapping
    public String create(String requestId) {
        return refundService.process(requestId);
    }
}
```

Note this deliberately uses **unqualified** annotation names (no `import org.springframework...`, no actual Spring dependency added to `pom.xml`) — proving `LayerClassifier`'s simple-name matching (Task 3) and this task's endpoint extraction both work without Spring itself being resolvable, matching the analyzer's best-effort philosophy end to end.

- [ ] **Step 2: Write the failing test**

```java
// analyzer/src/test/java/com/devkit/codegraph/EndpointExtractorTest.java
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
```

- [ ] **Step 3: Run test to verify it fails**

```bash
cd analyzer && mvn test -Dtest=EndpointExtractorTest
```

Expected: FAIL — `EndpointExtractor` doesn't exist yet.

- [ ] **Step 4: Write the implementation**

```java
// analyzer/src/main/java/com/devkit/codegraph/EndpointExtractor.java
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
```

- [ ] **Step 5: Run test to verify it passes**

```bash
cd analyzer && mvn test -Dtest=EndpointExtractorTest
```

Expected: PASS (3 tests).

- [ ] **Step 6: Wire it into `ProjectAnalyzer`**

In `ProjectAnalyzer.analyzeFile`, after the `for (MethodDeclaration methodDecl : classDecl.getMethods())` loop's `addMethodNode` call, add:

```java
            for (MethodDeclaration methodDecl : classDecl.getMethods()) {
                addMethodNode(methodDecl, layer, relativePath, result);
                EndpointExtractor.extract(classDecl, methodDecl).ifPresent(mapping -> {
                    Endpoint endpoint = new Endpoint();
                    endpoint.id = mapping.httpMethod() + " " + mapping.path();
                    endpoint.httpMethod = mapping.httpMethod();
                    endpoint.path = mapping.path();
                    endpoint.entryNode = className + "." + methodDecl.resolve().getSignature();
                    result.endpoints.add(endpoint);
                });
            }
```

- [ ] **Step 7: Add an end-to-end endpoint test to `ProjectAnalyzerTest`**

```java
    @Test
    void detectsEndpointFromControllerAnnotations() throws IOException {
        Path projectRoot = Path.of("src/test/resources/sample-project");
        AnalysisResult result = new ProjectAnalyzer(projectRoot).analyze();

        assertEquals(1, result.endpoints.size(), "expected exactly the RefundController.create endpoint: " + result.endpoints);
        Endpoint endpoint = result.endpoints.get(0);
        assertEquals("POST", endpoint.httpMethod);
        assertEquals("/refunds", endpoint.path);
        assertEquals("POST /refunds", endpoint.id);
    }
```

- [ ] **Step 8: Run the full analyzer test suite**

```bash
cd analyzer && mvn test
```

Expected: PASS across every test class written so far.

- [ ] **Step 9: Commit**

```bash
git add analyzer/
git commit -m "feat(codegraph-analyzer): detect HTTP endpoints from Spring mapping annotations"
```

---

## Task 7: Build the fat jar, verify it runs standalone, wire into `internal/codegraph`

**Files:**
- Modify: `internal/codegraph/codegraph.go` (JSON tags)
- Modify: `internal/codegraph/analyzer_subprocess.go`
- Modify: `internal/codegraph/analyzer_subprocess_test.go`

- [ ] **Step 1: Build the fat jar and smoke-test it from the command line**

```bash
cd analyzer && mvn package
ls -la target/codegraph-analyzer.jar
java -jar target/codegraph-analyzer.jar src/test/resources/sample-project /tmp/index.json
cat /tmp/index.json
```

Expected: the jar builds, runs without a classpath argument (proving the shade plugin bundled everything), and `/tmp/index.json` contains the 3 sample-project classes, their methods, the 2 call edges, and the 1 endpoint — matching what the JUnit tests already assert, just observed end-to-end from the command line this time.

- [ ] **Step 2: Add JSON tags to the Go types**

In `internal/codegraph/codegraph.go`, update the struct definitions:

```go
// Node is a class or method discovered during indexing.
type Node struct {
	ID       string `json:"id"`
	Label    string `json:"label"` // fully-qualified, e.g. "com.acme.OrderService.create"
	Kind     string `json:"kind"`  // "class" | "method"
	Layer    string `json:"layer"` // "controller" | "service" | "repository" | "unknown"
	FilePath string `json:"filePath"`
	Line     int    `json:"line"`
	EndLine  int    `json:"endLine"`
}

// Edge is a directed call relationship between two Nodes, by Node.ID.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"` // "call" | "di"
}

// Endpoint is one detected HTTP entry point (e.g. a @GetMapping method).
type Endpoint struct {
	ID         string `json:"id"`
	HTTPMethod string `json:"httpMethod"`
	Path       string `json:"path"`
	EntryNode  string `json:"entryNode"` // Node.ID of the controller method
}
```

```go
// AnalysisResult is everything an Analyzer produces for one project.
type AnalysisResult struct {
	Nodes     []Node     `json:"nodes"`
	Edges     []Edge     `json:"edges"`
	Endpoints []Endpoint `json:"endpoints"`
}
```

(`FlowStep`/`SymbolMatch` are Go-internal query-result shapes, never serialized from the analyzer's JSON — leave them untagged.)

- [ ] **Step 3: Write the failing test for the real subprocess analyzer**

```go
// internal/codegraph/analyzer_subprocess_test.go
package codegraph

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// analyzerJarPath locates the fat jar built by Task 7 Step 1. Tests that
// need a real jar skip (not fail) when it hasn't been built yet, so this
// package's test suite still passes in an environment that only has the Go
// toolchain — the jar is a separate, optional build artifact from a
// different toolchain (Maven), not something `go test` can produce itself.
func analyzerJarPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("../../analyzer/target/codegraph-analyzer.jar")
	if err != nil {
		t.Fatalf("resolve analyzer jar path: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("analyzer jar not built at %s (run `cd analyzer && mvn package` first): %v", path, err)
	}
	return path
}

func TestSubprocessAnalyzerAnalyzesSampleProject(t *testing.T) {
	jarPath := analyzerJarPath(t)
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("java not on PATH")
	}

	projectPath, err := filepath.Abs("../../analyzer/src/test/resources/sample-project")
	if err != nil {
		t.Fatalf("resolve sample project path: %v", err)
	}

	analyzer := NewSubprocessAnalyzerWithJar(jarPath)
	result, err := analyzer.Analyze(context.Background(), projectPath)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if err := result.Validate(); err != nil {
		t.Fatalf("analyzer produced an inconsistent graph: %v", err)
	}
	if len(result.Endpoints) != 1 || result.Endpoints[0].Path != "/refunds" {
		t.Fatalf("unexpected endpoints: %+v", result.Endpoints)
	}
}

func TestSubprocessAnalyzerWithoutJarConfiguredReturnsNotConfigured(t *testing.T) {
	analyzer := NewSubprocessAnalyzer()
	_, err := analyzer.Analyze(context.Background(), "/some/project")
	if !errors.Is(err, ErrAnalyzerNotConfigured) {
		t.Fatalf("expected ErrAnalyzerNotConfigured, got %v", err)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

```bash
go test ./internal/codegraph/... -run TestSubprocessAnalyzer -v
```

Expected: FAIL — `NewSubprocessAnalyzerWithJar` doesn't exist yet, compile error.

- [ ] **Step 5: Write the real implementation**

```go
// internal/codegraph/analyzer_subprocess.go
package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// subprocessAnalyzer invokes the bundled Java analyzer CLI (see
// docs/superpowers/plans/2026-08-19-codegraph-java-analyzer.md) as a
// one-shot subprocess: `java -jar <jarPath> <projectPath> <outputPath>`,
// then reads and parses the index.json it writes. An empty jarPath means
// "not configured yet" (e.g. the jar hasn't been built/bundled) — Analyze
// fails clearly rather than trying to exec an empty path.
type subprocessAnalyzer struct {
	jarPath string
}

func NewSubprocessAnalyzer() Analyzer {
	return subprocessAnalyzer{}
}

// NewSubprocessAnalyzerWithJar points the analyzer at a specific built jar.
// Production wiring (internal/app/container.go) will resolve the real
// bundled jar path once packaging/distribution is decided — a separate,
// later concern from getting the subprocess invocation itself correct.
func NewSubprocessAnalyzerWithJar(jarPath string) Analyzer {
	return subprocessAnalyzer{jarPath: jarPath}
}

func (a subprocessAnalyzer) Analyze(ctx context.Context, projectPath string) (AnalysisResult, error) {
	if a.jarPath == "" {
		return AnalysisResult{}, ErrAnalyzerNotConfigured
	}

	outputFile, err := os.CreateTemp("", "codegraph-index-*.json")
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: create temp output file: %w", err)
	}
	outputPath := outputFile.Name()
	outputFile.Close()
	defer os.Remove(outputPath)

	cmd := exec.CommandContext(ctx, "java", "-jar", a.jarPath, projectPath, outputPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: analyzer subprocess failed: %w (output: %s)", err, out)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: read analyzer output: %w", err)
	}

	var result AnalysisResult
	if err := json.Unmarshal(data, &result); err != nil {
		return AnalysisResult{}, fmt.Errorf("codegraph: parse analyzer output: %w", err)
	}
	return result, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

```bash
go test ./internal/codegraph/... -run TestSubprocessAnalyzer -v
```

Expected: PASS — `TestSubprocessAnalyzerAnalyzesSampleProject` actually shells out to `java -jar` and parses real output; `TestSubprocessAnalyzerWithoutJarConfiguredReturnsNotConfigured` still covers the "not configured" path from Phase 1a.

- [ ] **Step 7: Run the full Go suite to check for regressions**

```bash
go test ./internal/... ./cmd/... -count=1
```

Expected: PASS (matches `AGENTS.md`'s documented expected output — only the pre-existing `build/ios` / no-test-files notices, no failures).

- [ ] **Step 8: Commit**

```bash
git add internal/codegraph/codegraph.go internal/codegraph/analyzer_subprocess.go internal/codegraph/analyzer_subprocess_test.go
git commit -m "feat(codegraph): wire subprocessAnalyzer to the real Java analyzer CLI"
```

---

## What this plan does NOT unlock yet

`NewSubprocessAnalyzerWithJar` exists and is tested against a manually-built jar path, but nothing in `internal/app/container.go` wires a *real* production jar path into the container's `CodeGraph` service yet — that's packaging/distribution (where does the jar ship inside the DevKit app bundle, how is its path discovered at runtime), which depends on decisions the frontend/installer work hasn't made yet. Likewise `SetProjectPath` still isn't called with a real user-chosen path anywhere (per Phase 1a's own note) — that's the frontend module plan, next.
