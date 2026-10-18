# Running and configuring DevKit

The full command table, MCP setup notes, sync-login configuration and server-mode rules.
Moved out of `README.md` unchanged; the README keeps only the minimum to get started.

## Commands

Prerequisites: Go (>= 1.25), [bun](https://bun.sh), `task`
([go-task](https://taskfile.dev)), and — only to compile the Wails CLI / native
GUI (cgo) — `libgtk-4-dev` + `libwebkitgtk-6.0-dev`.

| Task | Command |
| --- | --- |
| Desktop dev (native window) | `task dev` |
| Headless / server mode (HTTP, loopback only) | `task run:server` |
| Production desktop build | `task build` |
| Go tests | `go test ./internal/... ./cmd/...` |
| Architecture layering gate | `go test ./internal/arch/` |
| Two-device sync E2E | `go test ./cmd/mocksyncserver/` |
| Deployed sync matrix (opt-in Compose) | `set -a; . .superpowers/sdd/deployed-e2e.env; set +a; go test ./cmd/synce2e/ -count=1 -timeout 15m` |
| Go format check | `gofmt -l internal main.go` |
| Go vet | `go vet -tags server ./...` |
| Go lint (errcheck off; see `.golangci.yml`) | `golangci-lint run` |
| Frontend tests (incl. module boundary + design-system ratchets) | `cd frontend && bun run test` |
| Frontend build + typecheck | `cd frontend && bun run build` |
| Frontend-only dev server (Vite, no Go backend) | `cd frontend && bun run dev` |
| Regenerate Wails bindings (after Go bridge changes) | `wails3 generate bindings` |
| MCP stdio proxy (needs desktop + unlocked vault) | `task mcp` |
| Install DevKit MCP into an agent | `task mcp:install AGENT=claude-code\|codex SCOPE=global\|project PROJECT=.` |
| Uninstall DevKit MCP from an agent | `task mcp:uninstall AGENT=claude-code\|codex SCOPE=global\|project PROJECT=.` |
| Agent Hub (Partial: Claude + Codex; see `AGENTS.md`) | `go test ./internal/acp/ ./internal/codexapp/ ./internal/agenthub/` |

Cloud/CI setup and non-obvious run caveats are documented in `AGENTS.md`.

## MCP (agent tools)

**Implemented runtime** (stdio tools + install; Resources / Streamable HTTP deferred).
Not claimed as production-tested end-to-end against a real Claude Code / Codex session
in CI — verify locally on a desktop build.

- The **desktop app** hosts the MCP tool server over a Unix domain socket (`mcp.sock`
  under the app config dir) after unlock. Agents do not talk to that socket directly.
- `task mcp` / `cmd/devkit-mcp` is a **stdio ↔ socket proxy** that MCP clients spawn.
- Install writes local agent config only (no network):
  `task mcp:install AGENT=claude-code|codex SCOPE=global|project PROJECT=.`
  (`claude-code` / `codex` only in v1; Cursor deferred).
- Gated tools (SSH / Database / Kafka) use sticky active targets plus env-tag
  confirmation. Dialog choices are `allow` / `allow_always` / `deny`. When the env
  tag `requiresConfirmation` (including fail-closed / locked `prod`), **env wins**:
  prompt every call, ignore prior `allow_always`, and do not offer Allow always.
- Headless / server mode / no GUI prompter: selection and confirmation **fail fast**
  (`selection_required` / `confirmation_required`) — they do not hang waiting for UI.

## Remote config + OIDC (Sync login)

Local-first: vault and local modules work with no network. Sync login needs OIDC
settings from the pinned API host (`https://api.synx.io.vn/v1/desktop/config`) or
from disk cache after a successful fetch. `internal/cloudconfig` fetches on
startup, caches under app-private storage, and refreshes when connectivity returns.

`DEVKIT_OIDC_*` env vars **override** remote/cache when set (local dev / alternate IdP).
OIDC protocol code lives in `internal/auth` (wired from `main.go`).

| Variable | Required | Description |
| --- | --- | --- |
| `DEVKIT_OIDC_ISSUER` | for Sync auth | Keycloak realm issuer, e.g. `https://auth.example/realms/devkit` |
| `DEVKIT_OIDC_CLIENT_ID` | no | Default `devkit-desktop` |
| `DEVKIT_OIDC_SCOPES` | no | Default `openid profile email roles` |
| `DEVKIT_OIDC_AUTH_URL` | no | Override authorize URL |
| `DEVKIT_OIDC_TOKEN_URL` | no | Override token URL |

Example (local Keycloak from companion backend Compose):

`export DEVKIT_OIDC_ISSUER=https://localhost:…/realms/devkit`  
(use the real HTTPS issuer your realm publishes; loopback callback stays `http://127.0.0.1:62364/callback`.)

## Server mode security

`go build -tags server` creates a local headless convenience mode that binds
only to `127.0.0.1` (or another loopback address). It is not a remote API or a
production web-server deployment option. Public binding, TLS, authentication,
and authorization are unsupported until a separate server design provides
those controls.

Because the bridge has no authentication, server mode cannot tell a person's
"Allow" from any other local process's. So in server mode:

- **Agent Hub does not start sessions** (`ErrUnavailableInServerMode`); use the
  desktop app.
- **Every confirmation fails fast**: MCP tools on the active target (any env), and
  actions that need a confirmation from the UI (push to a prod remote, commit on a
  guarded branch, sending to a prod API environment, task gates).

Planned: ask for these through the OS's notifications instead, answered outside the
HTTP bridge.

