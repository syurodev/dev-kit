# DevKit

DevKit is a local-first developer toolkit built with **Wails v3** — a Go backend
(`internal/`, `main.go`) and a React + TypeScript + Vite frontend (`frontend/`).
Secrets live in a zero-knowledge vault (mandatory Master Password); built-in
modules cover notes, database connections, SSH, Kafka, and hashing tools, plus a
local-first encrypted sync client with Git-style conflict resolution. Design
specs live under `docs/`.

## Quickstart

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
| Frontend build + typecheck | `cd frontend && bun run build` |
| Frontend-only dev server (Vite, no Go backend) | `cd frontend && bun run dev` |
| Regenerate Wails bindings (after Go bridge changes) | `wails3 generate bindings` |
| MCP stdio proxy (needs desktop + unlocked vault) | `task mcp` |
| Install DevKit MCP into an agent | `task mcp:install AGENT=claude-code\|codex SCOPE=global\|project PROJECT=.` |
| Uninstall DevKit MCP from an agent | `task mcp:uninstall AGENT=claude-code\|codex SCOPE=global\|project PROJECT=.` |

Cloud/CI setup and non-obvious run caveats are documented in `AGENTS.md`.

### MCP (agent tools)

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

### Remote config + OIDC (Sync login)

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

## Code organization

DevKit follows the **standard Go module layout** described in the official guide
[Organizing a Go module](https://go.dev/doc/modules/layout). The conventions we
hold ourselves to:

### Directory layout

- `cmd/<binary>/` — one directory per executable, each declaring `package main`
  (e.g. `cmd/mocksyncserver`, `cmd/devkit-mcp` stdio proxy + install CLI).
- `internal/` — all application logic. The `internal/` path is import-restricted
  by the Go toolchain, so nothing here is a public API and we can refactor freely.
- `main.go` (repo root) — the Wails entry point and composition wiring.
- `frontend/`, `docs/`, `build/` — non-Go assets.
- Keep server logic in `internal/`; only extract a separate Go module if code
  becomes genuinely useful to other projects.

### Packages

- One package per directory; the package name is the directory name.
- Group a package's files by concept (e.g. `aead.go`, `kdf.go`, `envelope.go`) —
  a package may span many files, all declaring the same `package`.
- Keep packages small and focused on a single responsibility; add sub-packages
  only when a distinct responsibility emerges. Prefer flat over deeply nested.

### Naming

Per the Go blog [Package names](https://go.dev/blog/package-names) and
[Effective Go](https://go.dev/doc/effective_go#names):

- Package names are short, lowercase, single words — no `under_scores`, no
  `camelCase`. Prefer singular. Established collection/registry packages may stay
  plural (`permissions`, `capabilities`, `commands`, `modules`). The name should
  describe what the package provides so call sites read well (`crypto.SealV2`,
  `vault.Service`).
- Avoid catch-all packages: `util`, `common`, `helpers`, `misc`, `base`.
- Avoid stutter — prefer `bridge.AppService` over `bridge.Bridge`.
- Name files after the concept they hold; co-locate tests as `<file>_test.go`.
  Bridge DTOs live next to their handlers (not in a shared `dto.go`).
- Name adapters by role and prefix concrete implementations by their technology,
  applying the **same** convention everywhere (e.g. an in-memory store in
  `memory.go`, a SQLite implementation named `SQL…`).

### Dependencies

- Dependencies stay acyclic and flow one way: lower-level packages (`crypto`,
  `sync`) never import higher-level ones (`app`, `bridge`).
- "Accept interfaces, return structs" — define narrow interfaces in the consumer
  package, not the implementer.
- Enforced by `go test ./internal/arch/` (`TestLayering`, `TestNoImportCycles`).
  Any production import that breaks the layering fails CI.

> The community `golang-standards/project-layout` repository (with `pkg/`, etc.)
> is **not** an official Go standard and the Go team does not endorse it. We
> follow the `go.dev` guidance above.

## Current layout

```text
main.go              Wails entry point + dependency wiring
cmd/
  mocksyncserver/    In-memory zero-knowledge mock sync server (dev + E2E testing)
internal/
  app/               Composition root: DI container, module/command registries, permission grants
  bridge/            Wails boundary: DTOs, capability-gated calls, error redaction, opaque IDs
  cloudconfig/       Remote desktop config fetch, disk cache, semver gate, reconnect refresh
  auth/              OIDC PKCE browser flow + OS-keychain refresh-token rotation
  capabilities/      Capability gateway — authorizes every privileged call, emits audit events
  permissions/       Permission policy + request model
  audit/             Audit event sink + field redaction
  commands/          Command registry
  modules/           Module manifest registry
  crypto/            Primitives: XChaCha20-Poly1305 AEAD, Argon2id KDF, versioned envelope
  vault/             Master-password keyring + in-memory DEK (seal/open)
  vaultitem/         Vault-item substrate: Record + Repository (memory/SQL) + seal/open + schema
  environments/      Env tag registry (dev/stg/prod + custom) + Repository (memory/SQL)
  notes/             Notes module
  database/          Database-connection module (pgx)
  ssh/               SSH module (profiles, host keys, port forwarding)
  kafka/             Kafka module (profiles, connect, topics, peek, groups, publish)
  mcp/               MCP server (Unix socket host, tools, active-target gate) + install/
  interaction/       Selection + env confirmation hub (MCP/UI; headless fail-fast)
  tools/             Hashing tools module
  syncclient/        Sync application service (service / connect / syncnow / conflicts)
  syncconflict/      Conflict-resolution application service (detail + resolve strategies)
  sync/              Replication protocol: contract, envelope, HTTP transport, runner, outbox port
  storage/           SQLite kernel: open / KV / migrations / schema (owns all table names)
  syncstore/         Sync persistence adapters: outbox, cursors, remote apply, conflicts, identity, local mutations
  arch/              Architecture-enforcement tests (no runtime code)
  servermode/        Loopback-only bind guard for server mode
  apperrors/         Structured, redactable application errors
frontend/            React + TypeScript + Vite UI (generated Go↔JS bindings under frontend/bindings/)
docs/                Design specs and plans
build/               Wails/platform build configuration
```

The security spine is `permissions` + `capabilities` + `audit`: every privileged
call from the `bridge` passes through the capability gateway with an explicit
`(subject, capability, scope)`, and sensitive fields are redacted before audit.
