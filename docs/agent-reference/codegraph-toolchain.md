### CodeGraph analyzer toolchain
Three languages, three shapes, and `Toolchain` is the thing they differ in.

- **Java** — a jar built from `analyzer/` (or shipped in the package) and launched by a JDK
  the user must have. `Ready` means artifact AND runtime; when either is missing CodeGraph
  shows a setup panel (`internal/codegraph/toolchain.go`).
- **Lua** — `Native`: the analyzer runs in this process (`internal/lua` +
  `analyzer_lua*.go`), so it is ready the moment DevKit starts with nothing installed, on
  every platform including AppImage and MSIX.
- **TypeScript** — `Serve` plus `Run[0] == ArtifactArg`: the artifact IS the runtime.
  `tsgo` is the TypeScript compiler rewritten in Go, so it cross-compiles beside DevKit as
  a self-contained binary and needs nothing installed to RUN. Go is a requirement to BUILD
  it, the way Maven is for Java.

- `Toolchain` is per language and is the extension point for the next analyzer: a value
  with its own requirements, build command and artifact path — or with `Native` set, which
  means the analyzer runs in this process and every build field is unused. A native
  toolchain reports `Ready` without probing anything, and `BuildAnalyzer` refuses it
  (`ErrAnalyzerIsBuiltIn`) rather than reporting a build that never happened.
- `Serve` is for an analyzer that is not a one-shot subprocess. It is handed a `launch`
  closure that resolves argv and environment at call time, and returns the `Analyzer`.
  `Run[0]` naming `ArtifactArg` instead of a requirement means the artifact launches
  itself, so `runtimeMet` is true without probing anything.
- **Service keeps one analyzer per language and closes them on shutdown**
  (`analyzerCloser`). It matters only for TypeScript, and it matters a lot: a language
  server rebuilt on every open is cold on every open, and one left running after the app
  quits is a process the user cannot find.
- **Which analyzer runs is a per-project answer.** `OpenProject` detects the language, then
  takes that language's analyzer out of the registry — a Lua project is never handed the
  Java one. `SetAnalyzer` is an override for an owner who knows better (every test double
  in the package); `NewSubprocessAnalyzer()`'s placeholder does not count as one, which is
  what keeps the tests that pass it from pinning the service to nothing. `main.go` wires
  no analyzer at all now, only the registry.
- The UI reports a missing analyzer **per folder**, not for the whole module
  (`frontend/src/modules/codegraph/workspace/indexHealth.ts`, the Files root's dot and the
  header's index summary): a folder whose language has no ready analyzer says so with the build
  action, and every other folder keeps working. The old whole-canvas gate (`analyzerGate.ts`) is gone.
- A local Java jar older than `analyzer/pom.xml` or `analyzer/src/main` is stale:
  the setup panel offers a rebuild instead of analyzing with outdated semantics.
  Real-jar Go tests fail with the Maven rebuild command when a stale jar exists;
  they still skip when no jar was built. Bundled/external artifacts are unaffected.
- **Discovery is not `PATH`** (`internal/codegraph/locate.go`). Plenty of Java developers
  have IntelliJ and nothing else: the IDE ships its own JetBrains Runtime, bundles a full
  Maven under `plugins/maven/lib/maven3`, and downloads project JDKs into `~/.jdks` —
  none of which reach `PATH`. The probe searches `JAVA_HOME`/`MAVEN_HOME`, `PATH`,
  `~/.jdks`, SDKMAN, JetBrains Toolbox and app bundles, and the system JDK directories,
  then picks the NEWEST version that clears the minimum. The build runs the resolved
  paths with `JAVA_HOME` set and both `bin` directories prepended to `PATH`.
- A `mvnw` in `analyzer/` would be preferred over everything else, which would drop the
  Maven requirement entirely. There is no wrapper today; `mavenCandidates` already looks
  for one.
- **The jar ships inside the package.** `common:build:analyzer` builds it and the
  packaging tasks depend on it, so a released app never shows the setup panel — that is
  the from-source path. Each platform puts it where `installedResourcePaths`
  (`internal/codegraph/artifact.go`) looks, and the two must be changed together:

  | Package | Jar and `tsgo` land at | Declared in |
  | --- | --- | --- |
  | deb / rpm / arch | `/usr/local/share/dev-kit/` | `build/linux/nfpm/nfpm.yaml` |
  | macOS `.app` | `Contents/Resources/` | `build/darwin/Taskfile.yml` |
  | Windows NSIS | beside the `.exe` | `build/windows/nsis/project.nsi` |

  `common:build:analyzer:ts` builds `tsgo` the same way, and the packaging tasks depend on
  both. On macOS the universal package builds it for each architecture and lipos them
  (`build:analyzer:ts:universal`): a universal `.app` with a single-architecture analyzer
  analyzes nothing on half the Macs it installs on, and says so only when somebody opens a
  TypeScript project.

  **AppImage and MSIX do not carry it.** `wails3 generate appimage` and `wails3 tool msix`
  build their payload from the binary alone and expose no hook for extra files, so those
  two fall back to the setup panel. Fixing that means either replacing those tasks with a
  hand-rolled AppDir/package step or a change upstream in Wails — neither is done here;
  noted alongside the JRE gap in syurodev/dev-kit#28.
- `task dev` and `task build` deliberately do NOT build the analyzer: a contributor
  without a JDK can still work on everything else and gets the setup panel, which is what
  it is for. Only the packaging targets require the toolchain. Build one by hand with
  `task build:analyzer:ts` (TypeScript) or `task build:analyzer` (Java) — thin root-level
  wrappers over the `common:` tasks above, for opening a real project in `task dev`/
  `task run:server` without going through packaging.
- **A JRE is still required to RUN the analyzer, on every path.** Shipping the jar removes
  Maven and the build, not Java: `Toolchain.Run` is `java -jar …`. `Ready` therefore means
  artifact AND runtime, and `HasArtifact` separates the two failures so the panel can say
  "build this" or "install a runtime" rather than offering a build that would not help.
  Removing the last requirement means bundling a JRE per platform — measured at 57 MB with
  `jlink --add-modules java.se` (a trimmed module set saves only 2 MB and breaks
  `ReflectionTypeSolver` for target projects using `java.sql`, `java.net.http` and the
  rest, so it would have to be the full one). Not done — tracked in
  syurodev/dev-kit#28, which carries the measurements and the work items.
- **Two Java versions, deliberately different.** `analyzer/pom.xml` sets
  `maven.compiler.release` to 21 — the bytecode the analyzer itself is compiled to, so it
  runs on the JDK people actually have (an IDE's bundled runtime, a distro's
  `openjdk-21`). `ParserConfiguration.LanguageLevel` in `ProjectAnalyzer.java` is
  `JAVA_25` — the newest syntax it can READ in a target project. They are independent:
  JavaParser is a parser, not javac, so reading Java 25 needs no JDK 25. When a newer Java
  ships, raise the language level, never the release. A file whose syntax the parser
  rejects is swallowed by `parseFile`'s catch-all and vanishes from the index with no node,
  no edge and no error — `parsesSyntaxNewerThanTheAnalyzerOwnBytecode` guards that.
- **Parsing runs on several threads** (`ProjectAnalyzer.parseAll`): a fixed pool of
  `min(cores, 8)` platform threads, one `JavaParser` per thread, results gathered in
  submission order so the index is byte-identical to a single-threaded run (tests compare
  1/2/4/8 threads). Never use `StaticJavaParser` from a worker: its configuration is
  thread-local, so a worker parses at the default language level and every record/pattern
  file silently vanishes (`workerThreadsParseWithTheNewestLanguageLevel` has teeth).
  `-Ddevkit.analyzer.parseThreads=1` restores the old behaviour (reach the jar's JVM through
  `JAVA_TOOL_OPTIONS`; `Toolchain.Run` is a fixed argv). Only parsing is parallel: every later
  pass shares the symbol solver's caches, which are not thread-safe.
- **Preparing for Java 25.** Nothing in the analyzer needs 25, and the floor stays 21 because
  people run it on an IDE's JDK or a distro's `openjdk-21`; Locate already picks the NEWEST JDK
  that clears the floor, so a machine with 25 runs on 25. Measured on a synthetic 1500-file
  project (4 cores, JDK 21; 21k methods): 55s cold, of which `collectDeclarations` ≈ 38%
  (almost all `addMethodNode`'s `resolve()` + `getSignature()` per method), `parseFile` ≈ 34%,
  `addCallEdges` ≈ 16% (mostly `resolve()`), GC ≈ 8%, peak heap ≈ 1 GB. Parallel parsing took
  parse 5.6s → 2.1s and the whole run to ≈ 46s. What that says about 25:
  - **Virtual threads do not help** — indexing is CPU-bound, and they have been final since 21.
    A virtual-thread pool measured the same as 4 platform threads (it runs on the same cores).
  - **The real next win is the solver passes**, not the JDK: `addMethodNode` and `addCallEdges`
    are ≈ 54% together and are shared-solver work. Options, in order of risk: resolve a
    signature only when a parameter type needs qualifying (most do not); audit
    `JavaParserFacade`'s caches and parallelise per source root; one `JavaSymbolSolver` per
    thread (N× the type-solver memory under a 2g heap). Verify any of them the way parsing was:
    byte-identical output on the big project (`cmp`) before and after.
  - **Possible 25-only gain, unmeasured**: `-XX:+UseCompactObjectHeaders` (product in 25) should
    cut the memory of millions of tiny AST nodes. It does not exist on 21 (the JVM refuses to
    start), so it would be added together with `-XX:+IgnoreUnrecognizedVMOptions` in
    `JavaToolchain.Run` — and only after measuring on a real project with JDK 25. JDK 25 was not
    available when this was written.
  - **Raising the floor to 25 means changing together**: `maven.compiler.release` in
    `analyzer/pom.xml`; `javaRequirement().MinVersion` and `javaInstallCommands()` in
    `internal/codegraph/toolchain_java.go` (plus its doc comment); the precondition message in
    `build/Taskfile.yml`; the comment in `.github/workflows/build-prod.yml` (CI already builds on
    JDK 25); this file. After that a machine with only JDK 21 gets the setup panel — decide that
    on purpose. Add a test that the jar is `class file version 69`.
- The build is unsandboxed by design: it is the developer's own repository, built with
  their own toolchain, and Maven needs the network and `~/.m2`.
- End-to-end (real Maven, minutes, network):
  `DEVKIT_ANALYZER_BUILD_E2E=1 go test ./internal/codegraph/ -run BuildsForReal -v -timeout 20m`

