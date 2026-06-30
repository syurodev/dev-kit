# AGENTS.md

## Cursor Cloud specific instructions

This repo is a **Wails v3** desktop app (Go backend + React/TypeScript/Vite frontend).
The product is "DevKit" (see `docs/architecture/`); the current code is still close to the
Wails starter template with a single `GreetService` plus some core Go packages under
`internal/`.

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
- The server listens on **port 8080** by default. Override with `WAILS_SERVER_PORT` and
  `WAILS_SERVER_HOST` (e.g. `WAILS_SERVER_HOST=127.0.0.1 WAILS_SERVER_PORT=8080`).
- Frontend bindings are committed under `frontend/bindings/`; `go build -tags server`
  requires `frontend/dist/` to exist first (it is `//go:embed`-ed), so build the frontend
  before building the server binary if you skip the `task` targets.

Backend method calls from JS go to `POST /wails/runtime` with a JSON body
`{"object":0,"method":0,"args":{"call-id":"<id>","methodID":<id>,"args":[...]}}`. This is
useful for scripting an end-to-end backend check without a browser.

### Tests / checks
- Go (real code): `go test ./internal/...`. Note: `go test ./...` will **fail** on the
  root `changeme` package (needs `frontend/dist` and gtk4/webkit for the default GUI
  build) and on `build/ios` scaffolding — that's expected, not a regression. Scope Go
  tests to `./internal/...`.
- Go format/vet: `gofmt -l internal main.go` / `go vet -tags server ./...`. (The
  `build/ios/*.go` template files are not gofmt-clean upstream; ignore them.)
- Frontend typecheck + build: `cd frontend && bun run build` (runs `tsc` then `vite build`).
- Frontend dev server (Vite only, no Go): `task dev:frontend` (port 9245).

### Notes / gotchas
- `bin/`, `frontend/dist/`, and `frontend/node_modules/` are gitignored — don't commit them.
- `frontend/.npmrc` sets `minimum-release-age=10080`; honoured by bun/pnpm, ignored by npm.
- `wails3 generate bindings` is slow (~13s, scans all packages) and regenerates
  `frontend/bindings/`; the committed output is already current, so it normally produces
  no diff.
