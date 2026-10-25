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
[`docs/design/module-shell-integration.md`](../design/module-shell-integration.md).
The app shell owns the sidebar column and workbench tab strip. Use a lazy
`ModuleManifest.sidebar` for module rails (including when no document is open),
or `useSidebarContent` for document-specific rails. Do not embed a fixed-width
`aside`, duplicate sidebar toggle/resize/material, or maintain a second document
strip / `openIds` / `selectedId` in a module view/store. Enable `tabs: true` and
open documents through `useWorkbenchTabsStore.openTab` with stable ID params;
provide a working `newTab` action. Close a tab whose document is gone (deleted,
its folder removed, deleted on disk, a draft saved as a new document) with
`closeTab(id, { remember: false })` so ⌘⇧T does not offer it back. Shadcn `Tabs` may switch modes inside one
document, such as SSH Terminal/Commands/Tunnels/Details. Keep drafts/resource
lifetimes separate from view mounting, derive selection from the focused group,
and gate document dialogs with `useModuleVisibility`. The shared sidebar has no
fill/border/shadow of its own; controls keep their theme colors. SSH's manifest,
sidebar and `documents.ts` are the current reference implementation.

### Theme: Mono chrome + Color palette
The chrome is one monochrome design (`frontend/src/index.css`, light and dark); nothing
the person picks changes it. The theme family (`theme_family`, Settings → "Color palette")
supplies only `--hue-*` (8 per mode), `--syntax-*` and `--ansi-*`: `theme/families.ts`
declares hues (+ optional syntax) and `resolvePalette` derives the rest; `applyThemeFamily`
writes only palette tokens. Status (`--status-*`) belongs to the chrome — use it for how
safe something is; use `--hue-*` for which kind it is. `bun test src/theme` floors every
palette colour at 4.5:1 on every Mono ground. Design:
`docs/superpowers/specs/2026-10-13-mono-chrome-color-palettes-design.md`.

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
- Frontend tests: `cd frontend && bun run test` (= `bun test --isolate`, every file in its own global). Without
  `--isolate`, `mock.module` leaks across files: `codegraph/fileActions.test.ts` + `flowActions.test.ts` in one run
  spin forever (CI's frontend job then died at its 20-minute limit), and a file whose mock omits an export can pass only
  because another file loaded the real module first. Spread the real module in a mock (`...(await import(...))`).
- Frontend dev server (Vite only, no Go): `task dev:frontend` (port 9245).

