# DevKit Project Structure and Code Rules

## 1. Purpose

This document defines the repository structure, dependency boundaries, naming rules, code conventions, testing expectations, and review gates for DevKit.

It complements `docs/architecture/devkit-architecture.md`. The architecture document explains what the system is. This document explains how the codebase should be organized and how contributors should write code without weakening security, performance, or future extensibility.

## 2. Design Goals

1. Keep the desktop app modular.
   Each major capability should have clear ownership and a small public surface.

2. Keep security boundaries visible in code.
   Sensitive operations must pass through explicit gateways, not hidden helper calls.

3. Keep plugin contracts stable.
   Internal implementation may change, but Extension API contracts must be versioned and reviewed.

4. Keep Wails-specific code thin.
   Wails is the desktop transport and shell integration layer. Core domain logic should remain testable without Wails.

5. Keep frontend modules composable.
   Built-in modules should use the same registry and contribution patterns that future plugins will use.

6. Keep migrations safe.
   Local database schema, encrypted envelopes, sync protocol, and plugin manifest format must be versioned from day one.

7. Keep the project reviewable.
   Changes should be small enough to review, with tests that prove the risky part of the change.

## 3. Target Repository Structure

Recommended structure:

```text
.
|-- cmd/
|   `-- devkit/
|       `-- main.go
|-- internal/
|   |-- app/
|   |-- audit/
|   |-- bridge/
|   |-- commands/
|   |-- config/
|   |-- crypto/
|   |-- db/
|   |-- extension/
|   |-- permissions/
|   |-- plugins/
|   |-- services/
|   |-- storage/
|   |-- sync/
|   |-- vault/
|   `-- workbench/
|-- frontend/
|   |-- src/
|   |   |-- app/
|   |   |-- bridge/
|   |   |-- extension/
|   |   |-- modules/
|   |   |-- services/
|   |   |-- state/
|   |   |-- ui/
|   |   `-- workbench/
|   |-- public/
|   `-- package.json
|-- backend/
|   |-- src/
|   |-- migrations/
|   `-- tests/
|-- plugins/
|   |-- examples/
|   `-- sdk/
|-- docs/
|   |-- architecture/
|   |-- security/
|   `-- decisions/
|-- scripts/
|-- build/
|-- Taskfile.yml
|-- go.mod
`-- README.md
```

The current repository is still close to a Wails starter template. The structure above is the target shape for the real product.

## 4. Top-Level Ownership

### 4.1 `cmd/devkit`

Responsibilities:

- Application entrypoint.
- Wails initialization.
- Process-level setup.
- Dependency wiring.
- Build-time metadata injection.

Rules:

- Keep `main.go` small.
- Do not put domain logic here.
- Do not call storage, vault, SSH, DB, or plugin internals directly from UI event handlers.
- Construct services and pass them into the application composition root.

### 4.2 `internal`

Responsibilities:

- All Go application code that is not intended as a public Go library.
- Core services.
- Gateways.
- Local storage.
- Sync engine.
- Plugin runtime.

Rules:

- Use Go's `internal` package boundary to prevent accidental external imports.
- Packages under `internal` must keep clear dependency direction.
- Avoid cyclic dependencies.

### 4.3 `frontend`

Responsibilities:

- React shell.
- Workbench UI.
- Built-in module UIs.
- Plugin view containers.
- Frontend state and frontend-only services.

Rules:

- Frontend code must not receive unrestricted access to Wails runtime APIs.
- Plugin UI must not call generated Wails bindings directly.
- Sensitive calls go through frontend bridge adapters and Go-side Capability Gateway.

### 4.4 `backend`

Responsibilities:

- SaaS API server.
- Auth.
- Device registration.
- Subscription and entitlement.
- Encrypted blob sync.
- Marketplace metadata later.

Rules:

- Backend must not depend on desktop internals.
- Backend must treat encrypted blobs as opaque.
- Backend must not implement plaintext user-data business logic.

### 4.5 `plugins`

Responsibilities:

- Plugin SDK.
- Example plugins.
- Test plugins for compatibility.

Rules:

- Example plugins must use only public Extension API.
- Test plugins should cover permission denied, permission granted, activation, failure, and version mismatch behavior.

### 4.6 `docs`

Responsibilities:

- Architecture.
- Security model.
- Decision records.
- Integration docs.
- Plugin API docs.

Rules:

- Major architectural changes require a doc update.
- Security-sensitive changes require a threat model or security note update.

## 5. Go Package Boundaries

### 5.1 Dependency Direction

Recommended direction:

```text
cmd/devkit
  -> internal/app
      -> internal/bridge
      -> internal/services
      -> internal/extension
      -> internal/storage
      -> internal/config

internal/bridge
  -> internal/app contracts
  -> internal/commands

internal/extension
  -> internal/permissions
  -> internal/commands
  -> internal/audit

internal/services
  -> internal/vault
  -> internal/crypto
  -> internal/storage
  -> internal/audit

internal/sync
  -> internal/storage
  -> internal/crypto
  -> internal/audit
```

Forbidden direction:

```text
internal/storage -> internal/bridge
internal/crypto -> internal/services
internal/vault -> internal/extension
internal/services -> frontend generated code
internal/plugins -> frontend components
```

Rule:

- Lower-level packages must not import higher-level orchestration packages.

### 5.2 Package Roles

`internal/app`

- Composition root.
- Service wiring.
- Application lifecycle.
- Startup and shutdown.

`internal/bridge`

- Wails-facing Go methods.
- Request/response DTOs for frontend calls.
- Translation from UI commands to app services.

`internal/commands`

- Command registry.
- Command dispatch.
- Command metadata.
- Command permission requirements.

`internal/extension`

- Extension API contracts.
- Extension activation.
- Contribution registry.
- Extension API versioning.

`internal/plugins`

- Plugin installation.
- Plugin package validation.
- Plugin runtime management.
- Plugin diagnostics.

`internal/permissions`

- Permission grants.
- Permission prompts.
- Scoped permission checks.
- Permission diffing on plugin update.

`internal/services`

- Application use cases.
- Notes, tools, DB manager, SSH workflows.
- Coordinates repositories and gateways.

`internal/vault`

- Secret records.
- Vault lock/unlock.
- Secret handles.
- Secret access policy.

`internal/crypto`

- Key derivation.
- Encryption envelopes.
- Key rotation.
- Recovery key helpers.

`internal/storage`

- SQLite connection.
- SQL migrations.
- Repositories.
- Transaction helpers.

`internal/sync`

- Sync protocol client.
- Sync queue.
- Conflict handling.
- Device sync state.

`internal/audit`

- Local audit events.
- Redaction helpers.
- Audit retention.

## 6. Go Code Rules

### 6.1 General Rules

- Use `context.Context` for IO, database calls, sync jobs, SSH sessions, plugin activation, and long-running operations.
- Return structured errors from boundary layers.
- Keep package APIs small.
- Prefer interfaces at package boundaries, not everywhere.
- Do not expose repository types to the frontend bridge.
- Do not expose core service internals to plugins.
- Avoid global mutable state.
- Avoid package-level service singletons.
- Prefer explicit dependency injection through constructors.

### 6.1.1 Formatting and Static Checks

Required checks:

- `gofmt` for all Go files.
- `go test ./...` for Go packages.
- `go vet ./...` before release.
- A stricter linter can be added after the package structure stabilizes.

Rules:

- Do not ignore linter warnings by default.
- If a warning is intentionally ignored, explain why close to the suppression.
- Generated files should be clearly marked and excluded from manual formatting debates.

### 6.2 Naming

Packages:

- short lowercase names,
- no underscores,
- no generic names like `common`, `utils`, or `helpers` unless the package has a very narrow purpose.

Good:

```text
crypto
vault
storage
permissions
commands
extension
```

Avoid:

```text
common
shared
helpers
manager
misc
```

Types:

- `Service` for use-case orchestration.
- `Repository` for persistence.
- `Gateway` for protected cross-boundary access.
- `Registry` for registering commands, modules, plugins, or views.
- `Client` for external API clients.
- `Envelope` for versioned serialized formats.
- `Policy` for rules that answer allow/deny decisions.

Examples:

```go
type VaultService struct {}
type SecretRepository struct {}
type CapabilityGateway struct {}
type CommandRegistry struct {}
type SyncClient struct {}
type EncryptedRecordEnvelope struct {}
type PermissionPolicy struct {}
```

### 6.3 Error Rules

Errors crossing process, plugin, frontend, or network boundaries must be structured.

Required fields:

- code,
- message,
- recoverable,
- details when safe,
- cause internally where useful.

Rules:

- Do not put secrets in error messages.
- Do not expose raw driver errors directly to plugin UI.
- Wrap errors at boundaries with domain context.
- Keep internal cause available for local diagnostics when safe.

Example codes:

```text
vault.locked
permission.denied
plugin.disabled
plugin.versionUnsupported
storage.migrationRequired
crypto.invalidPassword
sync.conflict
db.connectionFailed
ssh.connectionFailed
```

### 6.4 Logging Rules

- Logs must be structured.
- Logs must be redacted by default.
- Logs must not include secret values, plaintext notes, query text marked sensitive, SSH private keys, DB passwords, API tokens, recovery keys, or encryption keys.
- Audit logs and debug logs are separate.
- Debug logs should be opt-in.
- Crash reports must run through a redaction step.

### 6.5 Concurrency Rules

- Long-running jobs run outside UI request handlers.
- Use cancellation for sync, DB queries, SSH sessions, plugin activation, export, import, and migrations.
- Use bounded worker pools for sync and background indexing.
- Avoid unbounded goroutine creation.
- Avoid sharing decrypted secret values across goroutines unless ownership is clear.
- Emit progress events for user-visible long jobs.

### 6.6 Storage Rules

- All schema changes go through migrations.
- Migrations must be idempotent where practical.
- Migrations must be forward-only in production.
- Destructive migrations require a backup step.
- Repositories should accept a transaction or context-aware DB handle.
- Do not construct SQL query strings with untrusted input.
- Use parameterized queries.
- Store schema version in the database.

### 6.7 Crypto Rules

- Do not write custom crypto algorithms.
- Use reviewed libraries.
- Use authenticated encryption.
- Store envelope version, algorithm, key id, nonce, and ciphertext.
- Never reuse nonce/key pairs.
- Store key derivation parameters with the wrapped key envelope.
- Version encrypted formats from the first implementation.
- Prefer secret handles over returning plaintext secret values.
- Do not log cryptographic material.

### 6.8 Security Boundary Rules

All sensitive operations must pass through a named boundary:

- frontend to Go: Wails bridge,
- plugin to Go: Extension API and Capability Gateway,
- Go to local encrypted data: VaultService and CryptoService,
- local app to server: SyncService,
- server to data store: backend authorization layer.

Forbidden:

- plugin calling generated Wails bindings directly,
- frontend importing secret access helpers,
- repository decrypting secrets without VaultService,
- backend decrypting user blobs,
- shell execution without explicit critical permission.

## 7. Frontend Structure

Recommended `frontend/src`:

```text
frontend/src/
|-- app/
|   |-- App.tsx
|   |-- providers/
|   `-- routes/
|-- bridge/
|   |-- client.ts
|   |-- commands.ts
|   |-- errors.ts
|   `-- events.ts
|-- workbench/
|   |-- activity-bar/
|   |-- command-palette/
|   |-- editor-area/
|   |-- side-bar/
|   |-- status-bar/
|   `-- tabs/
|-- modules/
|   |-- notes/
|   |-- vault/
|   |-- tools/
|   |-- database/
|   `-- ssh/
|-- extension/
|   |-- contribution-registry.ts
|   |-- plugin-view-host.tsx
|   `-- permissions.ts
|-- services/
|-- state/
|-- ui/
`-- test/
```

## 8. Frontend Code Rules

### 8.1 React Rules

- Keep workbench layout separate from module content.
- Built-in modules register views and commands through a registry.
- Do not hardcode module-specific navigation inside the app shell.
- Use stable IDs for commands, views, tabs, and modules.
- Keep secret values out of global state unless absolutely required.
- Prefer derived display state over duplicating backend state.
- Long-running operations should display progress and allow cancellation where supported.

### 8.2 Component Rules

- Shared UI components live in `frontend/src/ui`.
- Domain-specific components live inside their module.
- A module should not import another module's internal components.
- Cross-module UI should be promoted to `ui` or `workbench`.
- Avoid deeply nested component trees that mix data fetching, layout, and rendering.

### 8.3 Frontend Bridge Rules

- Only `frontend/src/bridge` imports generated Wails bindings.
- UI modules call bridge functions, not generated bindings.
- Bridge functions translate backend errors into typed frontend errors.
- Plugin UI containers must not expose bridge functions directly.

Allowed:

```text
modules/database -> bridge/commands -> Wails generated binding
```

Forbidden:

```text
modules/database -> Wails generated binding
plugin view -> Wails generated binding
```

### 8.4 Frontend State Rules

- App shell state: layout, active view, tabs, command palette.
- Module state: filters, selections, editor draft state.
- Server/local data: fetched through bridge services.
- Secret state: use handles or masked values whenever possible.
- Do not persist sensitive frontend state to localStorage.

### 8.5 UI/UX Rules

- The first screen is the usable workbench, not a landing page.
- Use dense, tool-oriented layouts.
- Avoid marketing-style hero sections inside the app.
- Keep destructive actions explicit.
- Permission prompts must explain the exact capability and scope.
- Locked vault state must be visually obvious.
- Sensitive values should default to hidden.

### 8.6 Formatting and Static Checks

Required checks:

- TypeScript typecheck.
- Frontend build.
- Formatter check after a formatter is configured.
- Component/unit tests for critical flows.

Rules:

- Use TypeScript strictness where practical.
- Avoid `any` at domain and bridge boundaries.
- Generated Wails binding types should be wrapped by typed bridge adapters when exposed to app code.
- Do not add a state library until module complexity proves it is needed.

## 9. Backend Structure

Recommended `backend/src`:

```text
backend/src/
|-- app.ts
|-- config/
|-- modules/
|   |-- auth/
|   |-- billing/
|   |-- devices/
|   |-- sync/
|   `-- plugins/
|-- db/
|   |-- migrations/
|   `-- schema/
|-- middleware/
|-- errors/
|-- security/
`-- tests/
```

## 10. Backend Code Rules

- Backend stores encrypted blobs as opaque payloads.
- Backend does not parse plaintext user data.
- Every sync request requires authenticated session and registered device.
- Device id from request payload is not trusted without session binding.
- Subscription checks do not replace authorization checks.
- Use structured errors.
- Use request IDs.
- Rate-limit auth, device registration, sync upload, and plugin downloads.
- Do not log encrypted payload bodies unless explicitly needed for debugging, and never log plaintext secrets.
- API routes are versioned from the beginning.

## 11. Extension and Plugin Rules

### 11.1 Plugin Manifest Rules

Every plugin manifest must include:

- id,
- name,
- publisher,
- version,
- compatible DevKit version,
- compatible Extension API version,
- activation events,
- contributions,
- permissions.

Rules:

- Plugin IDs are globally unique.
- Permission scopes are explicit.
- Activation events are reviewed.
- Startup activation is exceptional.
- Unknown manifest fields should be preserved for forward compatibility but ignored safely.

### 11.2 Extension API Rules

- Extension API is versioned independently from app version.
- Breaking changes require a major version.
- Deprecated APIs must have a removal plan.
- Plugin examples must compile against the public API.
- Internal modules should use the same API shape where practical.

### 11.3 Capability Gateway Rules

The Capability Gateway must check:

- plugin installed,
- plugin enabled,
- plugin version supported,
- plugin signing identity valid when applicable,
- permission requested in manifest,
- permission granted by user,
- requested scope allowed,
- vault state,
- input payload valid.

The Gateway must not rely on frontend checks.

### 11.4 Plugin Runtime Rules

- Plugin runtime failure should not crash the app.
- Plugin activation must be cancellable or timeout-bound.
- Plugin calls should have request IDs.
- Slow plugin calls should be visible in diagnostics.
- Plugin updates that add permissions require user confirmation.
- Plugin UI communicates through message passing.

## 12. Module Rules

Each built-in module should have:

- module manifest,
- commands,
- views,
- service layer,
- storage/repository layer if needed,
- tests,
- security notes if it touches sensitive data.

Recommended module shape:

```text
frontend/src/modules/<module>/
|-- index.ts
|-- manifest.ts
|-- commands.ts
|-- routes.ts
|-- views/
|-- components/
|-- state/
`-- tests/

internal/services/<module>/
|-- service.go
|-- dto.go
|-- errors.go
`-- service_test.go
```

Rules:

- Modules do not depend on each other's internals.
- Shared capabilities move into core services.
- Module commands must use stable IDs.
- Module storage must follow migration rules.

## 13. Testing Rules

### 13.1 Required Test Levels

Go:

- unit tests for services,
- unit tests for permission policies,
- unit tests for crypto envelopes,
- repository tests for migrations and queries,
- integration tests for sync and capability gateway.

Frontend:

- component tests for critical UI,
- state tests for command and module registries,
- bridge adapter tests,
- permission prompt tests.

Backend:

- route tests,
- auth/device authorization tests,
- sync idempotency tests,
- migration tests,
- rate-limit tests where practical.

Plugin:

- sample plugin compatibility tests,
- permission denied tests,
- activation tests,
- failure isolation tests.

### 13.2 Security Tests

Required for sensitive code:

- plugin cannot call ungranted capability,
- plugin cannot bypass scope,
- locked vault blocks secrets,
- secrets are redacted from logs,
- encrypted records include version and algorithm,
- sync payload is encrypted,
- revoked device cannot upload new changes,
- migration preserves encrypted data.

### 13.3 Test Naming

Go tests should describe behavior:

```go
func TestCapabilityGateway_DeniesVaultReadWithoutGrant(t *testing.T) {}
func TestVaultService_DoesNotReturnSecretWhenLocked(t *testing.T) {}
func TestSyncService_RetryUploadIsIdempotent(t *testing.T) {}
```

Avoid vague names:

```go
func TestGateway(t *testing.T) {}
func TestVault(t *testing.T) {}
```

## 14. Migration Rules

Versioned surfaces:

- local database schema,
- encrypted record envelope,
- sync protocol,
- Wails bridge API,
- Extension API,
- plugin manifest schema,
- plugin package format,
- backend HTTP API.

Rules:

- Every migration has a version.
- Every migration has tests.
- Migrations are forward-only in production.
- Destructive migrations require backup.
- Encrypted record migrations are resumable.
- App refuses to open a database from a newer incompatible version.
- Sync protocol rejects unsupported clients with structured errors.

## 15. Configuration Rules

Configuration classes:

- build-time config,
- app config,
- user settings,
- plugin settings,
- secret config.

Rules:

- Secret config belongs in vault, not plain settings.
- Plugin settings are namespaced by plugin id.
- Settings schema should be versioned.
- Defaults should be defined close to the owning module.
- Environment variables are for development and backend deployment, not desktop secret storage.

## 16. Documentation Rules

Required docs:

- architecture overview,
- project structure and code rules,
- threat model,
- crypto design,
- plugin API,
- backend API,
- migration policy,
- release checklist.

Decision records:

- Use `docs/decisions/YYYY-MM-DD-title.md`.
- Record context, decision, alternatives, and consequences.
- Required for choices that affect security, plugin API, storage, sync, or backend protocol.

## 17. Review Checklist

Before merging a change, check:

- Does it respect package dependency direction?
- Does it expose new Wails methods?
- Does it expose new Extension API methods?
- Does it touch secrets, vault, crypto, auth, sync, or plugins?
- Does it need a threat model update?
- Does it need a migration?
- Does it add or widen plugin permissions?
- Does it log sensitive data?
- Does it introduce long-running work without cancellation?
- Does it need performance measurement?
- Are tests included for the risky behavior?
- Are docs updated?

## 18. Phase 0 Implementation Order

Recommended order before building feature-heavy modules:

1. Move starter app into target structure.
2. Add application composition root.
3. Add structured error package.
4. Add logging and redaction package.
5. Add local storage package and migration runner.
6. Add crypto envelope package.
7. Add vault lock/unlock skeleton.
8. Add command registry.
9. Add frontend bridge adapter layer.
10. Add workbench shell.
11. Add internal module registry.
12. Add permission model skeleton.
13. Add Capability Gateway skeleton.
14. Build first simple module through the registry.

Recommended first module:

- Hash/encoding tools, because it exercises command registration and workbench layout without touching secrets.

Recommended second module:

- Notes/snippets with encrypted storage, because it validates vault, storage, crypto envelope, and local search decisions.

## 19. Hard Rules

These rules should not be bypassed without a written decision record:

1. Plugin code cannot call Wails generated bindings directly.
2. Plugin capability calls must go through Capability Gateway.
3. Backend cannot decrypt user vault data.
4. Secret values cannot be logged.
5. Encrypted records must be versioned.
6. Database schema changes require migrations.
7. Dangerous plugin permissions require explicit user confirmation.
8. Long-running operations require cancellation.
9. Public Extension API changes require compatibility review.
10. Remote plugin marketplace requires signing, integrity checks, permission diffing, and rollback.
