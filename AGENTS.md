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
The Java analyzer is not committed as a jar — it is built from `analyzer/` on the machine
that runs DevKit. When it is missing, CodeGraph now shows a setup panel that can run that
build itself (`internal/codegraph/toolchain.go`), rather than failing an index with
"analyzer not configured".

- `Toolchain` is per language and is the extension point for the next analyzer: a value
  with its own requirements, build command and artifact path. `DefaultToolchains` is the
  registry; `main.go` wires it and hot-swaps the analyzer in on a successful build, so no
  restart is needed.
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

  | Package | Jar lands at | Declared in |
  | --- | --- | --- |
  | deb / rpm / arch | `/usr/local/share/dev-kit/` | `build/linux/nfpm/nfpm.yaml` |
  | macOS `.app` | `Contents/Resources/` | `build/darwin/Taskfile.yml` |
  | Windows NSIS | beside the `.exe` | `build/windows/nsis/project.nsi` |

  **AppImage and MSIX do not carry it.** `wails3 generate appimage` and `wails3 tool msix`
  build their payload from the binary alone and expose no hook for extra files, so those
  two fall back to the setup panel. Fixing that means either replacing those tasks with a
  hand-rolled AppDir/package step or a change upstream in Wails — neither is done here.
- `task dev` and `task build` deliberately do NOT build the analyzer: a contributor
  without a JDK can still work on everything else and gets the setup panel, which is what
  it is for. Only the packaging targets require the toolchain.
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
