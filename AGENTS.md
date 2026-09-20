# AGENTS.md

## Cursor Cloud specific instructions

This repo is a **Wails v3** desktop app (Go backend + React/TypeScript/Vite frontend).
The product is "DevKit" (see `docs/architecture/`). The Go core under `internal/` implements
the vault (mandatory Master Password, Argon2id KDF + XChaCha20-Poly1305 AEAD), the vault-item
modules (notes, database, ssh, kafka), and a local-first, zero-knowledge **sync client** with
Git-style conflict resolution (`internal/syncclient`, `internal/syncconflict`,
`internal/syncstore`). A throwaway in-memory mock sync server lives at
`cmd/mocksyncserver`. Architecture layering is enforced by `go test ./internal/arch/`.

### Toolchain (preinstalled in the environment)
- Go (auto-downloaded via `GOTOOLCHAIN=auto` from `go.mod`; needs >= 1.25).
- `bun` (frontend package manager — `bun.lock` is the lockfile; the `Taskfile` default
  `PACKAGE_MANAGER` is `bun`). On `PATH` via `~/.bun/bin`.
- `task` (go-task) and `wails3` CLI on `PATH` via `~/go/bin`.
- System libs `libgtk-4-dev` + `libwebkitgtk-6.0-dev` are installed. They are **required
  just to compile the `wails3` CLI** and any native GUI build (cgo), even though the GUI
  itself can't run headless.

`PATH` additions (`~/.bun/bin`, `~/go/bin`) are persisted in `~/.bashrc`.

### Running the app (headless / cloud)
The native GUI dev flow (`task dev`, `task run`, `task build`, `wails3 dev`) opens a real
desktop window and **cannot run in the headless cloud VM** (no display). Use **server
mode** instead, which serves the same Go services + frontend over HTTP:

- `task run:server` — builds frontend (regenerates bindings, runs `bun run build`) and
  runs the HTTP server. Or, for a quick run without re-running the full task:
  `go build -tags server -o bin/dev-kit-server .` then `./bin/dev-kit-server`.
- Set the bind address with `WAILS_SERVER_HOST` and `WAILS_SERVER_PORT` (e.g.
  `WAILS_SERVER_HOST=127.0.0.1 WAILS_SERVER_PORT=34115`). Server mode only binds loopback
  (`internal/servermode.ValidateHost`).
- Frontend bindings are committed under `frontend/bindings/`; `go build -tags server`
  requires `frontend/dist/` to exist first (it is `//go:embed`-ed), so build the frontend
  before building the server binary if you skip the `task` targets.

Backend method calls from JS go to `POST /wails/runtime`. The vault is **locked on every
process start** (the wrapped keyring persists, but the DEK is in-memory only); the UI shows
a first-run "Create Master Password" or an "Unlock" gate before the workbench.

### Tests / checks
- Go (real code): `go test ./internal/... ./cmd/...`. Note: `go test ./...` will **fail** on
  the root `devkit` package (needs `frontend/dist` and gtk4/webkit for the default GUI
  build) and on `build/ios` scaffolding — that's expected, not a regression. Scope Go tests
  to `./internal/...` (and `./cmd/...`).
- Architecture: `go test ./internal/arch/` fails on forbidden production imports (e.g.
  `bridge` → `storage`/`syncstore`/`sync`/`crypto`). Keep this green on every change.
- Two-device sync E2E: `go test ./cmd/mocksyncserver/` runs a real two-device convergence
  test (two full device stacks over HTTP against the in-memory mock). Run the mock
  standalone for manual testing with `go run ./cmd/mocksyncserver` (loopback, default
  `127.0.0.1:8787`); point the app's sync endpoint at it (plain HTTP is allowed on loopback).
- Deployed sync matrix: `go test ./cmd/synce2e/` skips unless
  `DEVKIT_DEPLOYED_E2E_URL` and `DEVKIT_DEPLOYED_E2E_TOKEN` are set; targets the Compose
  Gateway at `http://127.0.0.1:8082` typically. Source
  `.superpowers/sdd/deployed-e2e.env` (never commit tokens) then run
  `go test ./cmd/synce2e/ -count=1 -timeout 15m`. Each gate resets Compose sync
  tables via `docker exec devkit-db` so G1–G4 pass in one command. Regression:
  `go test ./internal/arch/ ./internal/sync ./internal/syncclient ./cmd/mocksyncserver/ -count=1`.
- Go format/vet: `gofmt -l internal main.go` / `go vet -tags server ./...`. (`build/ios/*.go`
  and `internal/crypto/keyring.go` are not gofmt-clean upstream; ignore them.)
- Frontend typecheck + build: `cd frontend && bun run build` (runs `tsc` then `vite build`).
- Frontend dev server (Vite only, no Go): `task dev:frontend` (port 9245).

### MCP (agent tools)
**Implemented runtime** — desktop hosts tools over a Unix socket; `cmd/devkit-mcp` is the
stdio proxy agents spawn. Resources / Streamable HTTP are deferred. Do **not** treat this
as production-verified against a real Claude Code session in cloud CI.

- Desktop (GUI, unlocked vault) required for the socket host + selection/confirmation
  dialogs. `task mcp` proxies stdio ↔ socket (`go run ./cmd/devkit-mcp`).
- Install only writes local agent config (no network, no grant changes):
  `task mcp:install AGENT=claude-code|codex SCOPE=global|project PROJECT=.`
  (also Settings → MCP panel / `task mcp:uninstall`).
- Confirmation policy: `allow` / `allow_always` / `deny`. When env
  `requiresConfirmation` (incl. fail-closed / `prod`), **env wins** — every gated call
  re-prompts; `allow_always` is ignored and not offered.
- Headless / `task run:server` / nil Prompter: selection and confirmation **fail fast**
  (no hang). Non-gated tools (e.g. `vault.status`, notes when unlocked in-process) can
  still work when the host is up.

### Agent Hub (`internal/agenthub` + `internal/acp`)
**Partial** v1 surface on this branch: ACP codec/conn, mock + Claude adapters,
session service (SQLite migration 13), bridge/UI multi-tab, MCP `agenthub.*`.
Claude vendor spawn uses `sandbox.DefaultRunner().StartPiped` (fail-closed).
Codex/Gemini adapters are **not** shipped. Product design + status:
`syurodev/base` → `/dev-kit/agent-hub`, `/dev-kit/implementation-status`.

```bash
go test ./internal/acp/ ./internal/agenthub/ ./internal/bridge/ ./internal/mcp/ -count=1
```

### Agent sandbox (`internal/sandbox`)
**Implemented** leaf package for OS-isolated subprocesses. Linux: **bubblewrap**
(`apt install bubblewrap`). macOS: Seatbelt / `sandbox-exec`. Landlock helper
exists but is **not** wired into bwrap Start/StartPiped.

```bash
go test ./internal/sandbox/ ./internal/arch/ -count=1
# meaningful smoke when bwrap is installed:
go test ./internal/sandbox/ -run 'TestSmokeAgentShapedProcess|TestBwrapCanWriteWorkDirUnderHome|TestStartPiped' -v
```

- `Start` — capture-and-wait helpers (buffered stdout/stderr).
- `StartPiped` — ACP / interactive stdio (Claude adapter path).
- Fail closed if `bwrap` missing / unsupported GOOS (`ErrUnavailable` / `ErrUnsupportedPlatform`).
- Private home default: tmpfs on home + forced `HOME`/`TMPDIR`; WorkDir under `$HOME` is re-bound after tmpfs.
- Default denies host `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.config/gcloud`; symlink binds into deny → `ErrInvalidPolicy`.
- Product docs: `syurodev/base` → `/dev-kit/sandbox` (ADR-009).

### Linux desktop (GTK4 / WebKitGTK)
The shell was written macOS-first and now branches in exactly two places; nothing else
in the frontend knows what OS it is on (`frontend/src/lib/platform.ts` is the only
platform module).

- **Black window on first run.** WebKitGTK's DMABUF renderer fails silently on hosts
  that cannot share the buffer (VMs, containers, some Wayland/driver combinations):
  the page loads and the DOM is live in the Web Inspector, but nothing paints. It looks
  total rather than merely wrong because `.native` leaves html/body transparent, so what
  shows through is the window's near-black `BackgroundColour`. `native_webview_linux.go`
  now sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` before GTK comes up unless the variable is
  already in the environment — export it yourself (any value) to take the decision back.
- **Frameless window.** GTK would otherwise stack its own decoration above DevKit's 40px
  title bar. `Frameless` is on for Linux only (`framelessWindow`); Wails' JS runtime keeps
  drag (`--wails-draggable`) and re-implements edge resize for this case. The frontend
  draws minimise/maximise/close itself (`components/ui/window-controls.tsx`) because a
  frameless window otherwise has none, and `Titlebar` drops the 84px macOS traffic-light
  reservation everywhere except macOS. Wails skips `gtk_window_set_title` on a frameless
  window, so `applyWindowTitle` puts WM_NAME back by hand.
- **Verifying the GUI headlessly.** `Xvfb :99 -screen 0 1400x900x24` + `DISPLAY=:99
  ./bin/dev-kit`, then `import -window root shot.png` and drive it with `xdotool`. Needs
  `dbus-x11` on PATH (single-instance uses `dbus-launch`). A bare Xvfb has no window
  manager, so maximise/minimise do nothing there — close and the layout are testable.

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
- The UI's setup panel is gated per language (`frontend/src/modules/codegraph/analyzerGate.ts`):
  the open project's language decides, and with nothing open it only takes over when NO
  analyzer is ready. Before Lua it took the whole canvas whenever anything was unbuilt,
  which would now block Lua work on a missing JDK.
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
  it is for. Only the packaging targets require the toolchain.
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
- The build is unsandboxed by design: it is the developer's own repository, built with
  their own toolchain, and Maven needs the network and `~/.m2`.
- End-to-end (real Maven, minutes, network):
  `DEVKIT_ANALYZER_BUILD_E2E=1 go test ./internal/codegraph/ -run BuildsForReal -v -timeout 20m`

### CodeGraph: the Lua analyzer
`internal/lua` is a hand-written Lua parser (lexer + recursive descent). It reads 5.1
through 5.4 plus the LuaJIT literals, because one Kong tree holds all of it: the plugin
runs on LuaJIT (5.1, `1LL`), the tooling beside it is plain 5.4 (`//`, bitwise, `<const>`).
It never returns an error — a statement it cannot read becomes a `Diagnostic` and the parse
resynchronises at the next statement boundary, so a file being edited keeps every function
around the broken one. (The Java analyzer swallows an unparseable file whole; that is the
failure this deliberately does not copy.)

`internal/codegraph/analyzer_lua*.go` turns that tree into an `AnalysisResult`:

- `analyzer_lua.go` — file discovery, the require-path index, module nodes, doc comments.
- `analyzer_lua_declare.go` — what each file declares and which of it is reachable. Lua has
  no export keyword, so all six ways of putting a function on a module table are read
  (`function M.f`, `function M:f`, `M.f = function`, `local M = { f = ... }`,
  `return { f = ... }`, a bare global).
- `analyzer_lua_resolve.go` — call resolution. **There are no types, so resolution is by
  provenance**: a value is followable when it traces back to a `require` of a file in this
  project, the table the module returns, or a method's `self`. Everything else is a call
  site with a reason and no target — `library` (stdlib, `ngx`, `kong`, a require with no
  file here) or `unresolved` (a value the analyzer lost). A wrong edge is worse than a
  missing one. A constructor (`new`, `create`, `extend`, …) carries its module through;
  an ordinary method does not, because `db:query()` returns data, not an instance.
- `analyzer_lua_kong.go` — everything about what a Lua project is FOR. Entry points are
  Kong plugin phases (`ACCESS · plugin-name`, since a plugin serves no route of its own),
  Kong Admin API route tables (`["/path"] = { GET = ... }`) and Lapis `app:get` routes.
  Layers follow: endpoints → `controller`, `resty.redis`/`kong.db`/a `dao`-ish name →
  `repository`, and what a controller requires → `service` unless it already said otherwise.

Module ids are require paths (`kong/plugins/x/handler.lua` → `kong.plugins.x.handler`,
`foo/init.lua` → `foo`, a leading `lua/` or `src/` stripped). A require that names nothing
exactly falls back to a **unique** suffix match, which is what makes
`require "kong.plugins.mine.util"` find `util.lua` in a plugin repository checked out at
its own root; an ambiguous suffix resolves to nothing rather than to a guess.

`analyzer_lua_e2e_test.go` is the one that matters when changing any of this: it drives a
Lua index through the flow walker, blast radius, go-to-definition, find-references, the
data-flow trace and the snippet reader — all written against the Java analyzer's id and
label contract, all of which a second analyzer can break without failing a single one of
its own tests.

```bash
go test ./internal/lua/ ./internal/codegraph/ -count=1
```

### CodeGraph: the TypeScript analyzer
A language server, driven. `internal/lsp` is the client — not an editor client: no
diagnostics, no completion, no document sync beyond opening a file. An indexer opens
everything once and asks structural questions, tens of thousands of them, so calls are
matched by id and safe to make concurrently.

TypeScript cannot be in-process like Lua: `microsoft/typescript-go` keeps its parser,
binder and checker under `internal/`, so there is no library to import — only the `tsgo`
binary, which `analyzer-ts/` (a Go module of its own) pins and builds.

`internal/codegraph/analyzer_ts*.go`:

- `analyzer_ts.go` — the analyzer. Owns the long-lived connection, walks the project,
  orchestrates the LSP, assembles the `AnalysisResult`.
- `analyzer_ts_model.go` — the language-level model: files, declarations, decorators,
  imports, and **call expressions** (receiver, name, arguments as written).
  **It knows nothing about any framework.** Calls are here because a call whose meaning
  is in its ARGUMENTS is a language-level fact: the checker resolves
  `app.get('/users', h)` and `client.send({ cmd: 'x' })` to library methods and is right
  to, but the route and the pattern are in the source either way.
- `analyzer_ts_read.go` / `analyzer_ts_scan.go` — what the server does not say. tsgo
  returns an empty `detail`, so signatures come from the source, and no LSP request
  exposes decorators. Bounded text work: the server has already said where each
  declaration starts and where its name is, so **a decorator is whatever sits between
  those two points** and a parameter list is whatever is inside the parentheses after the
  name. Nothing searches for a declaration.
- `analyzer_ts_framework.go` — **the seam**. A framework is a reading of the model, not a
  fork of it: five methods (`Name`, `Detect`, `Endpoints`, `Layer`, `Edges`). Adding one
  is a new file plus a line in `tsFrameworks`; it may not touch the model or the analyzer.
  If it needs something the model does not carry, the model grows a *language-level* fact,
  never a framework-shaped one.
- `analyzer_ts_nest.go` — NestJS, read almost exactly the way the Java analyzer reads
  Spring, because Nest copied the model. One place reads more than a decorator: Nest marks
  every provider `@Injectable`, so the decorator alone cannot tell a service from a
  repository — what the class injects can.
  Nest's entry points are not only HTTP: `@MessagePattern`/`@EventPattern` (every
  transport — TCP, Redis, NATS, Kafka, RabbitMQ, MQTT), gRPC, `@SubscribeMessage`,
  GraphQL resolvers, and `@Cron`/`@Interval` — the last of which are gated on nothing,
  because a scheduled job lives on a provider, never a controller.
  **`client.send(pattern)` is linked to the `@MessagePattern(pattern)` that serves it**,
  as a `call` edge, so a flow walks across the service boundary the way a reader does.
  The checker cannot produce that edge and is right not to: what the call reaches is
  decided at run time by a broker, and the only thing connecting the two is the pattern,
  written on both sides. Patterns are canonicalised (object keys sorted) so a caller and
  a handler written by different people still meet; an ambiguous pattern resolves to
  nothing rather than to one of two handlers.
- `analyzer_ts_next.go` — Next.js App Router. It exists to prove the seam: an interface
  with one implementation is a guess about what varies. Next shares nothing with Nest — no
  decorators, no classes, no injection; a route is a DIRECTORY and a handler is an exported
  function whose name is the HTTP method.
- `analyzer_ts_router.go` — the Express family (Express, Fastify, Hono, Koa's router,
  Restify). The third way a route is declared: by CALLING a method at run time, so the
  only trace in the source is the call. One reading covers all of them. The path is the
  guard — every one of `get`/`post`/`delete` is a method on something else too, and a
  route is the only one whose first argument starts with `/`. tsgo reports an inline
  handler as a function symbol of its own, so a flow starts at the handler rather than at
  the file; a named handler resolves like any other identifier. It does NOT follow
  `app.use('/api', subrouter)`: mounting moves every route beneath it, and reporting one
  at the wrong path is worse than reporting it at its own.

Two things this analyzer does that the others cannot:

- **There is no `unresolved` call site.** The checker resolved the call or the code does
  not compile. A target outside the project is `library`, same as everywhere else.
- **Module resolution is the compiler's.** `tsconfig` paths, `baseUrl`, package `exports`,
  extension inference, NodeNext's `.js` meaning `.ts`, barrel re-exports — asked by running
  `textDocument/definition` on each import specifier rather than reimplemented and got
  silently wrong.

**The process is long-lived and that is the design.** A cold index of 722 files is ~1.35s,
almost all of it building the program; a re-index is ~320ms, because `sync` tells the
server only about files whose content hash changed. The file watcher re-indexes on every
save, so a one-shot subprocess would pay the cold cost every time.

Tests skip unless the analyzer has been built:

```bash
cd analyzer-ts && go build -o bin/tsgo github.com/microsoft/typescript-go/cmd/tsgo
go test ./internal/lsp/ ./internal/codegraph/ -count=1
```

### CodeGraph: opening a project
`Service.OpenProject` does three things before any analyzer runs, each because the version
without it was worse.

- **Detection gates the subprocess** (`internal/codegraph/detect.go`) and now also picks the
  analyzer. Without it, opening any folder started a JVM to walk it and reported the empty
  result as an empty project rather than as the wrong folder. A `Toolchain` declares
  `Markers` (build files) and `SourceExt`; a directory holding a marker is a module root —
  for Lua that includes `handler.lua`, so a repository of Kong plugins reads as one module
  per plugin. First toolchain to claim the folder wins, and Java is ahead of Lua because
  its markers are build files while Lua's fallback is "any folder holding a .lua".
  Unrecognised folders get `ErrUnsupportedProject`, which the UI renders as "choose another
  folder" rather than as a failed analysis (its own bridge code, `unsupported_project`, so
  the two are not confused). Detection is skipped when no toolchain is registered — an empty
  registry knows nothing and must not refuse everything.
- **A folder of microservices is one index, not none.** `ProjectAnalyzer` discovers every
  `src/main/java` below the root (skipping `target/`, `node_modules/`, dot-dirs; depth 6),
  falling back to the root itself for a bare folder of `.java` files. All roots go into one
  `CombinedTypeSolver`, which is what keeps a call from one service into a shared module
  resolvable — one subprocess per service would lose that. `filePath` is relative to the
  project root, so roots never collide.
- **A known project reuses its stored index, then re-indexes in the background.** The index
  is persisted per project (`ProjectDBPath`); re-deriving it on every open cost as much as
  the first index with the UI blocked throughout. Reuse requires BOTH `HasIndex` and
  `IndexedProject == this project` — the per-project file is a naming convention, and a
  convention is not something the code can check, so the index records which project it
  describes (`index_meta`). An index written before that table simply re-indexes once.

### Credentials, work and the repository (`accounts`, `tasks`, `git`, `apiclient`)
Four modules added together; the design lives in `docs/dev-kit/{accounts,tasks,git,api-client}.mdx`
and the cross-module rules in `docs/dev-kit/cross-module.mdx`.

```bash
go test ./internal/accounts/ ./internal/tasks/ ./internal/git/ ./internal/apiclient/ ./internal/gitparse/ ./internal/notify/ -count=1
```

- **`internal/accounts` keeps two stores behind one service.** A *shared*
  account is a vault item and syncs; a *scoped* one (a single connection's
  password) goes to the local `account_secrets` table and does not. That is not
  a policy choice — `db_query_history` already wrote down why: everything in
  `vault_items` is sync-bound by construction, so a credential that must stay
  on one machine cannot live there. It is still sealed with the DEK. The link
  from a profile is `scope_owner`, deliberately **not** a stored `account_id`:
  an id pointing at a row that does not sync dangles on the next device, and
  writing a replacement back makes two devices overwrite each other forever.
- **No method anywhere returns a credential to an agent.** `AccountsAPI` has
  two read-only methods and no Resolve; `accounts:reveal` is granted to the
  accounts subject only. Modules resolve inside Go at the point of use.
- **`internal/git` is not `internal/checkpoint`.** checkpoint overrides
  `$GIT_DIR/info/attributes` to disable the project's `.gitattributes` so bytes
  round-trip untouched — right for a snapshot, silently wrong for a commit
  (`* text=auto`, `*.bin filter=lfs`). Never commit the user's work through it.
  What is shared is `Locate` and `internal/gitparse`.
- **Push is gated, commit is not — unless a `git-guard` says so.** Push leaves
  the machine; commit is local and reversible. A guard binds an env tag to a
  branch *pattern*, so it also covers branches that do not exist yet. There is
  no force push: no flag, no parameter, no code path.
- **An agent may push only to a branch recorded on a task it holds**
  (`task.branch`). A list living in the session dies with it; the task does not.
- **Closing a task is the one gate.** A board may declare a `verify` command;
  DevKit runs it and the exit code decides. An agent's own claim that tests
  passed is a sentence with nothing behind it — DevKit cannot see the tools it
  ran. A red result refuses and does **not** fall back to asking. Boards are
  `tasks:admin` and UI-only, so an agent can trigger the command and never
  choose it. Runs are background, one per project (they share a work tree),
  queued durably in `task_verify_runs`; `Recover` puts an interrupted run back
  to `queued` and never infers a result, and a request older than 24h goes
  stale rather than grading code it was never made against.
- **Every rule in `tasks` reads a status's `category`, never its name** — there
  is a test with a board whose terminal column is called "Đã ship".
- **Auth in `apiclient` is an account binding, not a string.** Rotation is a
  one-place edit and Copy as cURL emits `$DEVKIT_TOKEN`. The redirect policy
  compares the whole **origin**: comparing hostnames passed under httptest,
  where both servers are 127.0.0.1, and the test caught the credential
  following a cross-service redirect.
- **`internal/notify` and `internal/gitparse` are leaves** pinned by
  `internal/arch`, extracted as their own commits before the modules that
  needed them. `notify.Target` is three strings and deliberately not the
  frontend's `EntityRef`.
- **Regenerating bindings**: `wails3` may not be on PATH. Install it into a
  throwaway module (`go install github.com/wailsapp/wails/v3/cmd/wails3@<the
  version in go.mod>` from a temp dir) rather than into this one, then
  `wails3 generate bindings -clean=true -ts -i` — the `-ts` matters, the
  default output is `.js` and this repo commits `.ts`.

### Notes / gotchas
- Sync login: production uses `internal/cloudconfig` (pinned `GET /v1/desktop/config`, disk cache, refresh on reconnect). Set `DEVKIT_OIDC_*` to override remote/cache for local dev. Omit all OIDC env + stay offline with no cache for local-only vault/modules.
- Storage is a single SQLite file (`modernc.org/sqlite`, no cgo) opened with
  `MaxOpenConns(1)`; migrations run forward-only from `storage.Migrate`. Because of the
  single connection, migration `Up` funcs must **not** be wrapped in an outer transaction.
- `bin/`, `frontend/dist/`, and `frontend/node_modules/` are gitignored — don't commit them.
- `frontend/.npmrc` sets `minimum-release-age=10080`; honoured by bun/pnpm, ignored by npm.
- `wails3 generate bindings` is slow (~15s, scans all packages) and regenerates
  `frontend/bindings/`; re-run it after changing any bridge (`AppService`) method signature,
  then `bun run build`.
