# Code organization

How the Go code and the repository are laid out, and the conventions we hold ourselves to.
Moved out of `README.md` unchanged; `go test ./internal/arch/` enforces the layering.
The "Current layout" listing at the end predates Agent Hub, CodeGraph, Git, Tasks and the API client; `ls internal` is the full list.

DevKit follows the **standard Go module layout** described in the official guide
[Organizing a Go module](https://go.dev/doc/modules/layout). The conventions we
hold ourselves to:

## Directory layout

- `cmd/<binary>/` — one directory per executable, each declaring `package main`
  (e.g. `cmd/mocksyncserver`, `cmd/devkit-mcp` stdio proxy + install CLI).
- `internal/` — all application logic. The `internal/` path is import-restricted
  by the Go toolchain, so nothing here is a public API and we can refactor freely.
- `main.go` (repo root) — the Wails entry point and composition wiring.
- `frontend/`, `docs/`, `build/` — non-Go assets.
- Keep server logic in `internal/`; only extract a separate Go module if code
  becomes genuinely useful to other projects.

## Packages

- One package per directory; the package name is the directory name.
- Group a package's files by concept (e.g. `aead.go`, `kdf.go`, `envelope.go`) —
  a package may span many files, all declaring the same `package`.
- Keep packages small and focused on a single responsibility; add sub-packages
  only when a distinct responsibility emerges. Prefer flat over deeply nested.

## Naming

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

## Dependencies

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

