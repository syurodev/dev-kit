# AGENTS.md

## Cursor Cloud specific instructions

This repo is a **Wails v3** desktop app (Go backend + React/TypeScript/Vite frontend).
The product is "DevKit" (see `docs/architecture/`). The Go core under `internal/` implements
the vault (mandatory Master Password, Argon2id KDF + XChaCha20-Poly1305 AEAD), the vault-item
modules (notes, database, ssh, kafka), and a local-first, zero-knowledge **sync client** with
Git-style conflict resolution (`internal/syncclient`, `internal/syncconflict`,
`internal/syncstore`). A throwaway in-memory mock sync server lives at
`cmd/mocksyncserver`. Architecture layering is enforced by `go test ./internal/arch/`.

### Module UI: shared sidebar and document tabs

Before adding or changing a module sidebar or document navigation, read
[`docs/design/module-shell-integration.md`](docs/design/module-shell-integration.md).
The app shell owns the sidebar column and workbench tab strip. Use a lazy
`ModuleManifest.sidebar` for module rails (including when no document is open),
or `useSidebarContent` for document-specific rails. Do not embed a fixed-width
`aside`, duplicate sidebar toggle/resize/material, or maintain a second document
strip / `openIds` / `selectedId` in a module view/store. Enable `tabs: true` and
open documents through `useWorkbenchTabsStore.openTab` with stable ID params;
provide a working `newTab` action. Shadcn `Tabs` may switch modes inside one
document, such as SSH Terminal/Commands/Tunnels/Details. Keep drafts/resource
lifetimes separate from view mounting, derive selection from the focused group,
and gate document dialogs with `useModuleVisibility`. The shared sidebar has no
fill/border/shadow of its own; controls keep their theme colors. SSH's manifest,
sidebar and `documents.ts` are the current reference implementation.

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
  (no hang). Server mode sets `interaction.Hub.Headless` explicitly (`main.go`): its bridge
  is unauthenticated loopback HTTP, so any local process could otherwise answer a prompt.
  The hub is shared with the UI, so in server mode **every** prompt fails fast — active-target
  MCP tools on any env (`db.query`, `ssh.runCommand`, `kafka.*`) and UI actions that confirm
  (prod `git push`, guarded commits, prod `api.send`, task gates). Accepted for now; the plan is
  to ask through OS notifications instead (Codex spec §8.1). Non-gated tools (e.g.
  `vault.status`, notes when unlocked in-process) still work.
- `workspace.list` (read-only) tells an agent its workspace: the folders (services),
  primary first, its own `cwd`, and which folders its session may write (`granted`:
  cwd plus the session's extra read-write paths). An Agent Hub session sees only the
  workspaces its folders belong to; a terminal CLI on the shared socket sees them all.
  The primary-first order comes from `workspace.Service.ListServices` — the SQL
  repository alone sorts by name.
- **Git tools act on the caller's repository, not only the one open in the app**
  (`internal/mcp/git_repo.go`). Every `git.*` tool takes an optional `repo`: a folder name
  from `workspace.list` / `git.repos`, an absolute path, or `./…`/`../…` from the cwd. With
  none, an Agent Hub session gets its own `cwd`'s repository (the workspace folder holding
  it — a subfolder commits in its service); the person's terminal keeps the repository
  DevKit has open. An agent may READ any folder of its own workspaces and WRITE (stage,
  commit, branch, checkout, stash, resolve, push) only folders its session is `granted`
  (`git.repoNotGranted`); anything else is `git.repoOutsideWorkspace`. `git.repos` lists the
  workspace's repositories with branch/ahead/behind/changed, `cwd` and `granted`, and
  `git.status` answers with the `repo` it read. It works through `gitservice.Service.At` —
  a view sharing guards, remotes, credentials and the confirmation gate, keyed by the
  workspace folder like the Git view, so a guarded branch is guarded whichever way it is
  reached — and never moves the app's open repository.
- **History tools never need a force push** (`internal/git/history.go`). `git.amend`,
  `git.squash` and `git.rebase` rewrite only commits no remote-tracking ref reaches
  (`git.pushedHistory` otherwise); `git.cherryPick` / `git.revert` add commits, so pushed
  history is fine. Rebase/pick/revert stop on a conflict with the paths (`stopped`,
  `conflicted`) and take `action: continue|abort`. All go through the branch guard and
  commit as the account identity. `git.fetch` / `git.pull` (ff-only) use the remote's bound
  account and answer ahead/behind. The Claude hook also refuses shell `git pull`,
  `rebase`, `cherry-pick` and `revert`.
- Profile-scoped tools that reach a server (`db.explain`, `db.listForeignKeys`,
  `db.testConnection`, `db.schema.get`, `ssh.testConnection`) go through `GateProfileMCP`:
  they prompt on every call when the profile's env requires confirmation, and pass otherwise.

### Agent Hub (`internal/agenthub` + `internal/acp`)
**Partial** v1 surface on this branch: ACP codec/conn, mock + Claude adapters,
session service (SQLite migration 13), bridge/UI multi-tab, MCP `agenthub.*`.
Claude vendor spawn uses `sandbox.DefaultRunner().StartPiped` (fail-closed).
A Codex adapter (`adapter_codex*.go`, `codex_*.go`) drives `codex app-server` 0.147.0
under the same jail with its own `CODEX_HOME` (`<data>/agent-homes/codex`) — design in
`docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md`, state in
`docs/superpowers/HANDOFF-codex.md`. Gemini is **not** shipped. Product design + status:
`syurodev/base` → `/dev-kit/agent-hub`, `/dev-kit/implementation-status`.

- **The frontend reads a vendor table** (`frontend/src/modules/agenthub/vendors.ts`:
  name, runtime id, how it signs in, colour). Do not compare against `"claude"`; an
  unknown vendor gets its own name and nothing to install or sign in to.
- **Both vendors pick model and effort before the session exists.** `VendorConfigCatalog`
  (`ConfigCatalog`) is implemented by Codex (`codex_config.go`) and Claude (`claude_config.go`):
  a short-lived agent in a scratch folder, no prompt. Claude only lists the effort levels of the
  model it is on, so the catalog switches to each model and reads them back from
  `session/set_config_option`. The draft picker calls effort `reasoning_effort` for every vendor;
  `claudeRuntime.SetConfigOption` maps it to Claude's own `effort`.
- **Codex signs in inside DevKit, UI only.** Bridge `AgentHubSignInStatus` /
  `AgentHubStart{Browser,Device}SignIn` / `AgentHubCancelSignIn` / `AgentHubSignOut`
  (`auth:codex`), event `agenthub:signInCompleted`. There is no MCP tool for it
  (`TestNoToolSignsAgentsIn`), and `Service` refuses a caller stamped as an agent or a
  user-terminal MCP client. Audit lines carry the method and account id, never the
  login page, code or key.
- **Codex API key = an Accounts token, by id.** `preferences` stores only the id
  (`agent_codex_api_key_account`); `app.CodexAPIKey` resolves it at spawn and Accounts
  refuses to delete it while chosen. A successful ChatGPT sign-in adds a `KindExternal`
  "Codex (ChatGPT)" row to Accounts; signing out removes it.
- `agent_not_installed` / `agent_auth_required` name the vendor: adapters wrap those
  sentinels in `agenthub.VendorError`, and `bridge/errors.go` words the message per
  vendor. A chosen API key that is gone or refused also carries
  `ErrAgentAPIKeyUnusable` → `agent_api_key_unusable`, which the UI sends to Settings
  rather than to a sign-in (the key wins at every spawn). `app.CodexAPIKey.Choose`
  refuses agent / user-terminal callers like sign-in does.
- **A turn the vendor refuses is not a crash.** Codex's `usageLimitExceeded` →
  `ErrAgentUsageLimit` (`agent_usage_limit`); a full context window, a spent budget or
  any other failed turn → `ErrTurnFailed` (`agent_turn_failed`). Both carry Codex's own
  sentence (`VendorError.Message` — the only place that says when a limit lifts), leave
  the session `idle` rather than `error`, and are also reported as a session failure
  (`codexTurnFailure`), the banner Claude's air `sessionFailure` fills. The banner lasts
  one turn: the next prompt retires it (`transcript.ts`). `ErrProcessFailed` is for a
  process that actually died.

- **The prompt DevKit appends is rendered per vendor and per session**
  (`internal/agenthub/devkit_prompt.go`). `{{tool.name}}` placeholders become the names the
  model actually sees — `mcp__devkit__git_commit` for Claude, DevKit's own name plus a
  "your host may reformat it" note for Codex — and `TestPromptNamesOnlyRealTools` fails if
  one names no MCP tool. It says which vendor defaults DevKit replaces (git writes, code
  structure, saved connections, the task board) and that this takes precedence: the old,
  descriptive version lost to Claude Code's own prompt every time. `Service.SessionBrief`
  (`app.AgentSessionBrief`) puts the session's workspace folders and which it may write
  in the prompt instead of asking the agent to go and look.
- **Workspace instructions are read each turn, sent when they change.** Create/Edit workspace
  stores `instructions` locally (migration 34, UTF-8, max 32 KiB) together with its name and
  folders. `Service` reads them by the session's persisted `WorkspaceID` before invoking the
  runtime and builds a context block (instructions, memory index, session brief). The block is
  prepended to the prompt only when `workspaceContextDue` says the agent needs it: it has
  never received one, its fingerprint (SHA-256 of the text) differs from the last delivered
  one, or `workspaceContextRefreshTurns` (20) unchanged turns have passed (a vendor that
  compacts its conversation may have dropped it). A failed turn records nothing, so the next
  one carries the block again. The record is in memory and forgotten wherever the process
  holding the conversation is replaced: close, dead-runtime eviction, resume, rewind. Claude
  and Codex receive the same content; the user echo stays unchanged. Empty/unbound context
  retires earlier workspace instructions; DevKit's rules retain precedence. User-authored
  instructions are never passed through the tool-name placeholder renderer. The per-vendor
  prompt (`devkit_prompt.go`) tells the agent that a request without a block means the last
  one still holds.
- **Shared workspace memory is local and versioned** (`internal/workspacememory`,
  migration 35). `workspace.memory.*` tools scope Hub agents by their persisted
  workspace ID; terminal callers must supply an explicit ID. Update/delete/restore
  require `expectedRevision`; CAS and immutable history snapshots commit atomically.
  Source attribution comes from host identity, never tool input. The context block
  carries an index of at most 8 KiB (`Service.Context`): one `- id [kind] title — summary`
  line per entry (summary clipped to 140 runes, whitespace folded to one line, no bodies, no
  revisions), as reference data rather than instructions. The agent reads an entry with
  `workspace.memory.get`. The render is deterministic because agenthub fingerprints it;
  an empty workspace renders as "".
  Soft-deleted entries remain readable/restorable; deleting the workspace cascades
  entries and history. Plaintext SQLite storage is outside vault/sync, like workspace
  instructions. Audit excludes memory text and query. Workspace menu → Workspace memory
  opens the lazy manager: bounded summary pages, CAS edits and paged revision metadata
  with bodies loaded on selection. Migration 36 adds scoped contentless SQLite FTS5
  (`unicode61 remove_diacritics 2`) and materialized summary read models. Search is literal
  word-prefix AND, not substring; MCP list/search accept filter-bound cursors (legacy
  offset remains supported). `BenchmarkMemory50K` covers rare/common search, deep cursor
  pages and large revision histories. Broad searches still scale with matching postings.
- **Claude's PreToolUse hook refuses `git commit` / `git push` in Bash**
  (`cmd/devkit-mcp/hook.go`, `shellGitWrite`) with a reason naming the DevKit tool to call:
  the shell skips branch guards, the task trailer and the push rule. Other git commands
  are left alone. It reads plain spellings (`-C`, env prefixes, chains), not `sh -c`.

**Not in server mode**: `main.go` registers `agenthub.ServerModeAdapter` stubs there, and
every session fails with `ErrUnavailableInServerMode` — the jail shares the host loopback
and server mode's bridge has no authentication, so a session could approve itself.

```bash
go test ./internal/acp/ ./internal/agenthub/ ./internal/bridge/ ./internal/mcp/ -count=1
# Codex in the real jail, signed out (no quota): DEVKIT_CODEX_COMMAND is the native
# binary, e.g. <data>/runtimes/codex/0.147.0/node_modules/@openai/codex-<platform>/vendor/<triple>/bin/codex
DEVKIT_CODEX_COMMAND=/abs/path/to/codex go test ./internal/agenthub/ -run CodexRealBinary -v
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
- **Agent Hub asks for more by default**: the real `$HOME` (`ShareHome`) and the Docker
  socket found via DOCKER_HOST / current docker context / OrbStack / Docker Desktop
  (`AllowDocker`, `agenthub.DetectDocker`). DevKit's data dir (except `runtimes/`) is always
  hidden (`agenthub.ProtectDataDir`). Settings → Agents → Sandbox turns either off.
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

### CodeGraph: workspace of folders (not one project)
CodeGraph works on the **workspace**; a folder is neither a service nor "the" project. Design:
`docs/design/codegraph-workspace.md` (identity, scope, lifecycle, API, limits).

- **Identity** is `workspaceId + folderId + normalizedRelPath` (`workspace/identity.ts`:
  `docKey`, `flowKey`, `treeFileId`). `folderId` is `workspace_services.id`; re-adding a path gives a
  new id, so a draft never adopts a folder that merely has the same path. Paths are never case-folded.
  The same relative path in two folders is two buffers, two tabs, two cache entries.
- **Primary is read from `workspaces.root`**, never copied (`primaryId` is derived in `loadFolders`).
  `WorkspaceSetPrimary` only moves where agents are spawned: no reindex, no tab/draft/history reset.
  Nothing in CodeGraph keys off it. There is no `switchProject`: change workspace
  (title bar), change primary, change discovery scope (header picker), focus a document and
  add/remove folders (workspace management) are five different actions.
- **Go resolves folder→path from the workspace's membership**; the frontend never sends a path to
  read or write a file. `internal/app/workspacecode.go` is the seam; `internal/codegraph/rootfiles.go`
  does root-scoped I/O (`ReadFileAt`/`WriteFileAt`/`StatFileAt`/`ListFilesAt`/`GitInfoAt`) with no
  "active project". Reads of entry points / outline / symbols come from the on-disk index
  (`Registry.EntryPointsOf`/`OutlineOf`/`SymbolsOf`) and never open a Service.
- **Save is a revision check + atomic rename.** Revision = SHA-256 prefix of the bytes; a save sends
  the revision the buffer is based on and gets `file_conflict` if the disk moved. Saves of one path are
  serialised in-process; a foreign process writing inside the check→rename window can still be
  overwritten (detection, not isolation). `bufferModel.ts` is the pure state machine (base / disk
  revision / external / ack / orphan); "Keep mine" quiets the banner, Save stays guarded.
- **Find in workspace** is `internal/wssearch` (RE2, `git ls-files` or walk, bounded ordered worker
  pool, UTF-16 columns, cancel by request id). Unsaved buffers are not searched, and the UI says so.
- **Open documents are watched** by `internal/docwatch` (parent directories of open tabs and dirty
  buffers only, debounced). Missed events are covered by a reconcile on window focus.
- **Reindex** goes through `Registry`'s bounded single-worker queue; `WorkspaceReindex(folderIds,
  cancel)` scopes it. One folder failing never stops the batch; results of a previous workspace are
  dropped by generation.
- **Cross-folder intelligence is deliberately thin.** Definitions/references/outline come from the
  index of the folder that owns the file; nothing matches names across folders and text search is never
  presented as references. HTTP/Kafka/Redis links stay in `internal/crossservice`.
- Tests: `go test ./internal/wssearch/ ./internal/docwatch/ ./internal/codegraph/ ./internal/app/
  ./internal/bridge/ ./internal/arch/ -count=1`; frontend `bun test src/modules/codegraph`
  (`fileActions.test.ts`, `workspaceSync.test.ts`, `searchActions.test.ts` use a fake bridge).
  Benchmarks: `go test ./internal/wssearch/ -run xxx -bench WorkspaceSearch` and
  `go test ./internal/codegraph/ -run xxx -bench ExplorerFolder`.

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

Tests skip unless the analyzer has been built (`task build:analyzer:ts`, or directly:
`cd analyzer-ts && go build -o bin/tsgo github.com/microsoft/typescript-go/cmd/tsgo`):

```bash
task build:analyzer:ts
go test ./internal/lsp/ ./internal/codegraph/ -count=1
```

### CodeGraph: entry points
What the sidebar lists is `codegraph.EntryPoint` (`internal/codegraph/entrypoints.go`), not
"endpoints": where a flow starts because something outside the code calls in. `Kind` says
what calls in — `http`, `graphql`, `rpc`, `message`, `socket`, `schedule`, `plugin` (a Kong
phase) — `Verb`/`Target` are the chip and what it answers to (`GET /users`,
`ACCESS rate-limit`, `CRON 0 * * * *`), and `Service`/`Owner`/`Group`/`Order` place it in
the list. The UI groups kind → service → owner and does no guessing of its own.

- **An analyzer sets what it knows; `completeEntryPoints` fills the rest on every read**
  (`Service.ListEntryPoints`): kind from the verb, owner/group from a Java node id, the Java
  service from the package segment after the shared prefix. So an index written before
  these fields existed reads the same as a new one. The store moved `endpoints` into
  `entry_points` in place (rows copied, not dropped — a reopen serves the stored index).
- **Kong phases are `plugin` entries under their plugin, `Order`ed as Kong runs them**; a
  plugin's Admin API is `http` under the same plugin name.
- **The Java jar's wire format is unchanged** (`endpoints`, `httpMethod`, `path`):
  `analyzer_subprocess.go` translates it, so an installed jar keeps working with any DevKit.
- **Kept stable on purpose**: the MCP tool names (`codegraph.listEndpoints`, `getApiFlow`'s
  `endpointId` input — agents' allowlists name them), the entity-ref kind `"endpoint"`
  (agents and saved transcripts resolve it), and the API Client's `EndpointDraft` (an HTTP
  concept). Only `http` entries offer "Try in API Client". Tool-result cards read both the
  new (`verb`/`target`, `entryPointId`) and the old field names, since transcripts outlive builds.

### CodeGraph: guards on call edges
`Edge.Guard` is the condition a call runs under, read from the Java source around it
(`analyzer/.../Guards.java`): `if`/`else`, `? :`, `switch` entries, the right side of `&&`/`||`
and the guard clause (`if (!enabled) return;` guards what follows). **Syntactic, never evaluated** —
a guard names a flag, not its value. One edge per `(from, to, kind)`: one unconditional call makes
the edge unconditional, otherwise guards are joined with `||` (`Guards.merge`). `edges.guard` is a
migrated column (`migrateEdgeGuard`); old indexes read as unguarded until a re-index, which a new
DevKit build triggers anyway. `FlowStep` carries `Depth` (the flat DFS list is a tree) and `Guard`.
`call_sites` does not carry it; Lua and TypeScript do not set it. Design:
`docs/dev-kit/codegraph.mdx` § Guard trên cạnh gọi.

### CodeGraph: configuration
Java only. The analyzer records what config files SET (`ConfigKey`: `ConfigFiles.java`, YAML via
SnakeYAML `compose()` — never constructed, alias-bomb capped — plus `.properties`) and what the code
USES (`ConfigUsage`: `ConfigExtractor.java`). Two tables, `config_keys` and `config_usages`, joined
at read time on `CanonicalConfigKey` (letters and digits only, lower-cased — Spring's relaxed
binding), so an env var, a kebab key and a camelCase key are one setting.
- **Secrets never enter the index as values**: a key whose separator-stripped name contains
  `password|secret|token|apikey|…` is stored `redacted` with no value (its env var names are kept).
  `FindConfigUsage` and every other reader inherit that; do not add a path that returns raw file text.
- `Service.FindConfigUsage` (MCP `codegraph.findConfigUsage`): per use, readers (`field_accesses`, or
  a `@ConfigurationProperties` field's getter/record accessor), gated methods (conditions), calls whose
  `Edge.Guard` names the field; then a bounded reverse walk over call edges to the entry points.
  It reads source, never evaluates: "what depends on the flag", not "is it on".
- Known gaps, all noted in the answer or in `docs/dev-kit/codegraph.mdx` § Cấu hình là một loại node:
  Lombok/implicit record accessors are unresolvable so `properties` readers can be missing;
  `@ConfigurationProperties` on a `@Bean` method is not read; Lua and TypeScript record no config.

### CodeGraph: database tables
Java only, same shape as configuration: the analyzer records `EntityTable` (`@Entity`/`@Table`, Spring
Boot's snake_case default, secondary/join/collection tables) and `TableAccess` (`TableExtractor.java`,
SQL read by `SqlTables.java`), two tables `entity_tables` / `table_accesses`, joined with the call graph at
read time by `Service.FindTableUsage` (MCP `codegraph.findTableUsage`).
- **SQL is read by tokens, not parsed**: FROM/JOIN/USING read, UPDATE/INSERT INTO/DELETE/MERGE/TRUNCATE write.
  Anything that looks like a clause but is not (`EXTRACT(.. FROM ..)`, `FOR UPDATE`, CTE names, string
  literals) has a test in `SqlTablesTest`; add one before adding a rule.
- **Inherited Spring Data CRUD has no node and no edge** — `repo.save(x)` is recorded at the CALL
  (`repository-crud`), recognised by the receiver's type from the solver. A method the repository
  declares is attributed to that method, and its callers come from the call edges, never twice.
- **A name that is also one of an entity's tables asks about that table** (`FindTableUsage`): with the
  default naming class, entity and table are one word. An entity name that differs brings all its tables.
- Not read: `@NamedQuery`, `EntityManager.persist/merge`, tables named by a variable, triggers/cascades/views,
  Liquibase. Gaps are in `docs/dev-kit/codegraph.mdx` § Truy vấn nối với bảng DB.

### CodeGraph: calls between services
`internal/crossservice` joins what each service's index says — the routes it serves, the HTTP calls it
makes (`OutboundCall`, table `outbound_calls`) — into edges, **when asked**; nothing stores them.
`codegraph.ServiceSnapshot` is what one service offers the join: from a resident service, or read from
its index on disk with no Service opened (`Registry.Snapshot`; no watcher, no resident slot).
- Callers: Java (`OutboundCallExtractor`: Feign, Spring HTTP interfaces, RestTemplate, WebClient,
  RestClient) and Lua (`analyzer_lua_http.go`: `resty.http`, recognised by provenance of
  `require "resty.http"`, never by a method name). A `foldString` hole is `{expr}`, the shape Java uses.
- `workspace.ResolveTarget` resolves a host against the CALLER's config. With no host (a plugin's
  `{conf.url}`, an HTTP interface) the route alone links it, as `path-unique`, only when it has ≥ 2
  literal segments and exactly one other service serves it; several is `ambiguous-path`. Never pick one.
- An unreadable service is named, not skipped (`not-indexed`), and every edge carries how old the
  other end's index is. A service nothing watches can be wrong without saying so.
- Layering: `crossservice` imports `codegraph` and `workspace`; `codegraph` still does not import
  `workspace` (arch test). The MCP tool resolves "the asker's workspace" from `workspace.list`'s logic.

### CodeGraph: shared Redis keys
Same shape as the HTTP join: each service records `RedisAccess` (table `redis_accesses`; Java
`RedisExtractor`, Lua `recordRedis`), `crossservice.RunRedis` joins at read time (MCP
`codegraph.findRedisKeyUsage`). A key is a PATTERN (`fsap:access:{org}:{api}`); two are one resource
when some string matches both (`overlaps`, a DP over `*`), and a pattern needs ≥ 3 fixed leading
characters to count — `*` and `{key}` overlap everything, so they are dropped and a query by one is refused.
- Redis is recognised by the receiver (an `opsFor*()` chain, a declared template/client/command type,
  Lua provenance of `require "resty.redis"`), never by a method name like `get`.
- Not seen: keys built elsewhere and passed in, `@Cacheable`, Lua `EVAL`, pub/sub, Redisson. Say so in
  the answer's notes instead of widening the match.

### CodeGraph: Kafka topics
Same shape as Redis (table `kafka_accesses`; Java `KafkaExtractor`, Lua `recordKafka`;
`crossservice.RunKafka`, MCP `codegraph.findKafkaTopicUsage`), with two differences:
- **The topic name is folded against the config of the service that WROTE it** (`Service.Config.Resolve`),
  never against another's — two services spell one topic by different properties. A property the
  service's config does not set is `unresolved`, listed, not dropped.
- **Topics have a direction**, so the answer carries `gap` (`no-consumer` / `no-producer`). It is a gap in
  THIS workspace only; say so (the notes do) instead of calling the topic dead.
- Kafka is recognised by annotation, constructor or declared receiver type, never by a method name. Kafka's
  own `Producer`/`Consumer` interfaces are NOT in the type sets on purpose: a project's own `Producer.send`
  would become a topic. Not seen: Spring Cloud Stream bindings, `@SendTo`, `sendDefault`, `@RetryableTopic`.

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
  An unrecognised folder (Terraform, Helm, manifests…) still opens, **for its files only**
  (`Service.openForBrowsing`): no analyzer and no watcher run, the tree/editor/git status
  work, `IndexInfo.Unsupported` is true, and graph queries and `WaitForIndexReady` answer
  `ErrUnsupportedProject` (bridge code `unsupported_project`). That is a state, not a
  failure: the UI labels it "Not supported" (neutral tone), hides the Entry points tab,
  and never shows an error card or a retry for it; its messages name no language. Detection is skipped when no
  toolchain is registered — an empty registry knows nothing and must not refuse everything.
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
  ran. A red result refuses and does **not** fall back to asking. Changing a
  board (workflow, verify, binding) is `tasks:admin` and UI-only, so an agent
  can trigger the command and never choose it. An agent may *create* a board
  (`tasks.board.create`, `tasks:write` `board:new`): it gets no verify command
  — its gate is the person's confirmation — and is bound only into one of the
  session's own workspaces (`tasks.Service.CreateBoardFor`). Runs are background, one per project (they share a work tree),
  queued durably in `task_verify_runs`; `Recover` puts an interrupted run back
  to `queued` and never infers a result, and a request older than 24h goes
  stale rather than grading code it was never made against.
- **Every rule in `tasks` reads a status's `category`, never its name** — there
  is a test with a board whose terminal column is called "Đã ship".
- **Auth in `apiclient` is an account binding, not a string.** Rotation is a
  one-place edit and Copy as cURL emits `$DEVKIT_TOKEN`. The redirect policy
  compares the whole **origin**: comparing hostnames passed under httptest,
  where both servers are 127.0.0.1, and the test caught the credential
  following a cross-service redirect. A blocked redirect comes back as the 3xx
  itself (`redirectBlocked` names where it pointed), not as an error.
- **API client variables are Postman's three scopes**: globals (`api-globals`,
  one item; two devices' copies merge and fold back into one on the next edit)
  < collection, root folder first (both halves in the `api-collection` sealed
  payload — its metadata keeps the old shape, because an older DevKit validates
  metadata field by field and would refuse the whole pull) < environment.
  `api-globals` is a new item type, so an older DevKit on another device stops
  pulling once one exists, like every item type added before it. Secrets are write-only everywhere: an update names the
  secrets to KEEP (`VarsUpdate.KeepSecrets`) and only new values cross the
  bridge. Send still requires an environment — its tag is the gate — even when
  every variable comes from elsewhere.
- **A request is edited, not just sent.** `RequestInput` (no timestamps) is what
  the bridge takes; query/headers/form are ordered `KVList` rows with `disabled`
  (a stored map still decodes), `:name` path segments are `pathVars` on the
  request, and `bodyMode` sets Content-Type unless a header row does.
  `ApiSendRequest` sends the editor's unsaved state; `ApiCancelSend` stops it.
  Plain `http://` is allowed to loopback always, elsewhere only with no bound
  account on a tag that does not ask first (`ErrPlainHTTP`). Pasting curl
  (`ParseCurl`) opens a draft; it is not an import.
- **Import/export is format-agnostic** (`internal/apiclient/interchange.go`).
  Every format converts to or from one tool-neutral `Bundle` and never touches
  the vault; `Service.Import` / `Service.Export` are the only code that turns a
  Bundle into vault items or back, so the rules live once: no secret VALUE
  leaves (names do, empty), an account binding leaves as placeholder variables
  (`{{<account>_token}}`, named via the optional `AccountNamer`), an imported
  environment needs a tag (the person's choice, else one the file carries that
  this machine knows — else `ErrTagRequired`, before anything is written).
  **Adding a format is a new `format_*.go` implementing `Decoder` and/or
  `Encoder`, plus a line in `formats`**; it may not reach into the model — a
  missing concept grows the Bundle tool-neutrally. Today: `postman` (read +
  write; an environment's tag rides in its name as `Name [tag]`) and `curl`
  (write: a bash script, one function per request, `set -u`, running the file
  alone sends nothing). The window lists formats from `ApiFormats` and knows
  none by name. Export is `api:read` scope `export:*`, UI only — no MCP tool.
  Agents get `api.sendAdhoc`, and every request/response they read has
  credential-named rows masked (`Request.ForAgent`). An ad-hoc send
  (`SendInput.Adhoc`) that carries a bound account or a secret variable asks
  the person every time, on any tag: the agent chose the host, so the tag
  cannot have answered "send this token there".
- **Agents build the API client's contents, through the person**
  (`internal/mcp/tools_apiclient_write.go`): `api.listCollections`,
  `api.{create,update,delete}Collection`, `api.saveRequest`, `api.deleteRequest`,
  `api.{create,update}Environment`. Every write is `confirmAPIWrite` (kind `api`,
  inside the capability check). It asks EVERY time, no allow_always, for an
  environment (it names the hosts and its tag decides whether sending asks), a
  request that `Service.Carries` a credential (bound account, a secret variable under
  any environment, a credential typed into a row — `api.send` fires a saved request on
  the tag's rule alone), a collection's variables or parent, and any delete. Other
  writes offer allow_always. Variable edits are set/remove, the rest kept; a masked
  row sent back keeps the stored value (`Service.RestoreMasked`), and a masked value
  with nothing to keep is refused. Deleting an environment and Globals stay UI-only.
  Every write emits `apiclient:changed` (`Service.OnChanged`, via a wrapped item
  store), which the sidebar follows.
- **Boards bind to workspaces through local `task_board_bindings`** (not synced):
  each machine maintains its own binding of board → workspace, never written into
  the synced board metadata. `workspace_id` is nullable; a row's mere presence
  means "this machine has considered the board", distinguishing never-considered
  from deliberately-unbound. Agents see only boards bound to workspaces in their
  session's selected `workspace_id`, getting `tasks.notFound`
  otherwise—no second-guessing, and no MCP tool may bind. `project_path` is
  read-only fallback for legacy boards; `verify.service` names a workspace service
  instead, traveling through sync unlike paths. `Considered` marker guards
  `MigrateBoardBindings` to avoid re-binding unbound boards on restart.
- **Git over SSH uses the account's key, served by `internal/gitssh`** (a leaf). An `ssh-key`
  account holds the private key and its passphrase packed in one sealed secret; `gitssh.Open`
  serves it from a private in-process agent (0700 socket dir, one key, gone with the command) and
  git runs with `GIT_SSH_COMMAND` pointing at it with `IdentitiesOnly=yes`. `ssh-key` accounts are
  **local by default**: a scoped row whose owner is `local:<id>` (never swept as an orphan, resolved
  by id, never offered to the API client or revealed again), `Promote` is how one starts syncing.
  `accounts.ForRemote(host, owner, env, via)` picks the account for an unbound remote — keys over
  SSH, tokens over HTTPS — and remembers it per `(host, owner, via)`; the app's `remotePicker` asks
  the person through `interaction.Hub.SelectAccount` when two fit. SSH aliases resolve through
  `ssh -G`. A key that cannot be opened stops the command; it never falls back to the machine's own
  key. `~/.ssh/config` `IdentityFile` entries are still tried *after* a refused account key (see the
  pinned test). `internal/git` tests that touch SSH use `code.example.test`, not `github.com`: the
  cloud sandbox injects a `url.insteadOf` that rewrites github.com SSH URLs to HTTPS.
- **Any credential kind can be machine-local, not only keys.** `CreateInput.Local` makes the same
  scoped `local:<id>` row for a token or basic credential (never for `external`, or with an env
  restriction — the row has nowhere to keep one). `Verify` handles local rows (`verifyLocal`): the
  subject and verdict are kept in the row, scopes and expiry are shown once and not stored.
  `Service.CreateGitToken` is the git entry point (GitHub/GitLab token, Bitbucket basic with a
  required username; the provider must have an `SSHHost`, which is how a repository host is
  marked). `accountResolver.ResolveForRemote` gives git a stand-in username (the verified login,
  else `git`) when a token has none; the API client still gets what is stored.
- **Rotating a token is in place** (`Service.RotateToken`, bridge `AccountsRotateToken`): the id stays so
  bindings, remembered choices and the commit identity survive; the username is kept unless a new one is
  given (read with `Reveal`, since `Resolve` with no env refuses an env-restricted account); only
  `token`/`basic` accounts that are shared or machine-local can be rotated (a key is replaced by adding
  the new one; a connection's password changes through its profile). `Rotate` now clears subject, scopes
  and expiry (shared) or the local row's verification: they describe the OLD value.
- **Importing a key from disk is UI-only and path-guarded** (`internal/bridge/accounts_sshimport.go`).
  `gitssh.Discover` lists `~/.ssh` one level deep (no symlinks, ≤64 KB, skips `config`/`known_hosts`/`*.pub`)
  and `ReadAliases` reads `Host`/`IdentityFile`/`Include` as TEXT (`ssh -G` would report default key
  files for every host). The bridge reads a path only if this window was first shown it — listed by
  `AccountsDiscoverSSHKeys` or returned by the native picker — and the whole set of methods answers
  "desktop app only" in server mode (an unauthenticated bridge must not read files by path). No MCP
  tool exists for it. Keys are deduplicated by `(provider, fingerprint)`: one key may serve GitHub and
  GitLab, never one provider twice (`gitssh.QuickInfo` reads an OpenSSH key's public half without the
  passphrase, so listing and dedupe never run the slow KDF). An ssh alias bound to a key account
  (`ChooseForAlias`, stored in `account_remote_choices` as `(alias, "*", ssh)`) answers before the
  repository owner is asked.
- **Commits are written as the account's identity.** An account may carry a name and email
  (`account_identities`, local — not in the account metadata, whose strict decoder would make an
  older DevKit refuse the record). `git.Service.CommitIdentity` decides who the next commit is:
  the repository's own `user.email` wins, else the account of the branch's upstream remote (then
  `origin`, then the only remote) when it has BOTH halves, else git's configuration. It looks the
  account up through `remotePicker.IdentityFor`, which never opens a dialog. `Commit` passes the
  identity as `GIT_AUTHOR_*`/`GIT_COMMITTER_*` (an amend keeps the old author) and never writes
  `.git/config`. Agents commit through the same path, so their commits carry it too.
- **`internal/notify` and `internal/gitparse` are leaves** pinned by
  `internal/arch`, extracted as their own commits before the modules that
  needed them. `notify.Target` is three strings and deliberately not the
  frontend's `EntityRef`.
- **Regenerating bindings**: `wails3` may not be on PATH. Install it into a
  throwaway module (`go install github.com/wailsapp/wails/v3/cmd/wails3@<the
  version in go.mod>` from a temp dir) rather than into this one, then
  `wails3 generate bindings -clean=true -ts -i` — the `-ts` matters, the
  default output is `.js` and this repo commits `.ts`.

### Update notice (`internal/updater`, `internal/buildinfo`)
DevKit **tells** you a newer release exists; it downloads and installs nothing (OTA install
is a later step). `internal/updater` is a leaf that lists `syurodev/dev-kit` GitHub releases;
`buildinfo.Version` is stamped by the packaging build (`-X devkit/internal/buildinfo.Version`,
from `DEVKIT_VERSION`, which `build-prod.yml` sets from the tag). Unstamped builds report
`dev` and never reach the network.

- **Two release kinds, two channels.** `vX.Y.Z` is stable; `vX.Y.Z-beta` is a GitHub
  pre-release. A stable build is offered only stable releases; a beta build is offered the
  highest of either, so a beta tester hears about the next beta and the stable that supersedes
  it. "Newest" is by version precedence (`1.0.0-beta < 1.0.0`), never publish date, and the tag
  suffix decides even if the pre-release flag was forgotten. `/releases/latest` is not used
  because it never returns pre-releases.
- The release page URL comes off the network and is opened in the browser, so it must be
  `https://github.com/<repo>/releases/...` or the check fails rather than linking elsewhere.
- Bridge: `UpdatesCheck(automatic)`, `UpdatesCurrentVersion`, `PreferencesGet/SetUpdateChecks`
  (key `update_checks`, default on). Only the automatic startup check honours the preference
  (enforced in Go); "Check now" always answers. UI: `UpdateNotifier` (one toast per release,
  remembered in localStorage) and Settings → Account → Updates.
- Test: `go test ./internal/updater/ ./internal/bridge/ -run Update`; try a stamped build with
  `DEVKIT_VERSION=0.0.1 task build`.

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
