# DevKit Architecture Design

## 1. Product Goal

DevKit is a cross-platform desktop application for developers. It combines daily developer utilities, encrypted personal knowledge, secure connection management, and an extensible plugin platform.

The product should run on macOS, Windows, and Linux with a small memory footprint, strong offline behavior, and a security model suitable for storing sensitive developer data such as notes, snippets, SSH profiles, database credentials, API tokens, and private connection metadata.

The long-term product direction is a local-first developer workspace with optional SaaS sync, subscription entitlement, and a controlled extension ecosystem.

## 2. Core Principles

1. Local-first by default.
   The app must remain useful without network access. Notes, tools, vault data, connection profiles, and core workflows should work offline.

2. Zero-knowledge sync.
   The server stores encrypted blobs and account metadata. It must not need plaintext access to user notes, secrets, SSH keys, database passwords, or private plugin data.

3. Stable extension contracts.
   Plugins must call a public Extension API instead of internal Go services. The internal implementation can change without breaking plugin authors.

4. Least privilege.
   Plugins must declare required capabilities. The host grants, denies, audits, and can revoke these permissions.

5. Lazy activation.
   Modules and plugins should activate only when needed. Startup should load the shell, local vault state, command registry, and currently visible views only.

6. Built-in modules before external plugins.
   The first product versions should ship core modules as internal modules. The external plugin SDK should be opened only after the internal Extension API is proven stable.

7. Security boundaries before marketplace.
   Remote plugin distribution must not exist before manifest validation, permission enforcement, integrity checks, signing, sandboxing strategy, and plugin isolation are designed.

8. Threat model before implementation.
   Every sensitive module must define what it protects, who it protects against, and what it explicitly does not protect against.

9. Compatibility as a product feature.
   Local database schema, sync protocol, encrypted blob format, and Extension API versions must be migration-friendly from the first release.

10. Performance budgets.
   Startup, search, sync, plugin activation, database queries, and SSH sessions should have explicit budgets so the app stays fast as modules grow.

## 2.1 Terminology

This design uses the following terms intentionally:

- Extension API: the public API surface that plugins can call.
- Capability API: the permission-protected part of the Extension API that reaches sensitive Go services.
- Capability Gateway: the Go-side security boundary that validates plugin identity, permissions, vault state, and request payloads before calling core services.
- Hook: an event or lifecycle callback, such as `onVaultUnlocked`, `onCommand`, or `onConfigChanged`.

Hooks should not expose internal services directly. They are used to notify plugins that something happened. When a plugin wants to perform an action, it must call the Extension API and pass through the Capability Gateway.

Good hook usage:

```ts
devkit.events.onVaultUnlocked(async () => {
  await devkit.commands.execute("postgres.refreshConnections");
});
```

Bad hook usage:

```ts
devkit.internal.vaultService.onUnlock((vaultService) => {
  vaultService.rawDecryptEverything();
});
```

## 2.2 Non-Goals for the First Release

The first release should not attempt to ship all platform features.

Non-goals:

- Public plugin marketplace.
- Remote third-party plugin loading.
- Team vault sharing.
- Real-time collaboration.
- Cross-device conflict-free collaborative editing.
- Complex enterprise policy management.
- Running untrusted plugins with broad machine access.

These features can be designed for, but they should not block the local-first MVP.

## 2.3 Security Baseline

The app should be designed against a written security baseline rather than ad hoc rules.

Reference baselines:

- OWASP Cryptographic Storage Cheat Sheet for encryption design, key handling, and algorithm choices.
- OWASP Application Security Verification Standard for authentication, session, access control, logging, and secure architecture review.
- OWASP Mobile Application Security Verification Standard for local storage, platform integration, and client-side secret handling patterns that also apply to desktop apps.

Security gates before shipping sensitive features:

1. Threat model exists.
2. Data classification exists.
3. Encryption and key lifecycle are documented.
4. Permission boundaries are enforced in code.
5. Sensitive logs are reviewed.
6. Migration and rollback behavior are defined.
7. Recovery and lockout flows are tested.
8. Security tests cover the main abuse paths.

## 2.4 Threat Model

Protected assets:

- Master Password and derived keys.
- Data Encryption Keys.
- Recovery keys.
- Notes and snippets.
- SSH private keys and passphrases.
- Database credentials and connection metadata.
- API tokens.
- Query history and command history.
- Sync payloads.
- Plugin permission grants.

Primary attackers:

- Malicious or compromised plugin.
- Compromised sync server.
- Network attacker between client and server.
- Local malware or another local user account.
- Stolen laptop with the app database.
- Malicious marketplace package.
- Accidental sensitive data leakage through logs, crash reports, or telemetry.

Security goals:

- A compromised server cannot decrypt user data.
- A plugin cannot use capabilities it was not granted.
- A locked vault blocks sensitive access.
- A stolen encrypted database is not useful without the required key material.
- Sync does not upload plaintext sensitive data.
- Logs, analytics, and crash reports never include secrets.
- Plugin updates cannot silently replace trusted code with unverified code.

Explicit non-goals:

- Protecting against a fully compromised operating system while the vault is unlocked.
- Protecting plaintext visible on screen from screen recording malware.
- Preventing a user from intentionally exporting their own data.
- Guaranteeing perfect memory zeroization in a garbage-collected runtime.

These non-goals must be documented in user-facing security claims. The product should avoid overpromising "absolute" security.

## 3. Technology Decisions

### 3.1 Desktop Runtime

Decision: Wails v3 with Go backend and React frontend.

Rationale:

- Go gives strong native capability for SSH, database drivers, filesystem operations, crypto, background tasks, and concurrency.
- Wails keeps the desktop footprint smaller than Electron-style full browser runtimes.
- React gives mature UI composition, state management options, and a strong ecosystem for complex panels such as database viewers and note editors.

Risk:

- Wails v3 is still newer than Electron and Tauri ecosystems. The project should isolate Wails-specific code behind a bridge layer so the core domain services stay testable and portable.

### 3.2 Frontend

Decision: React + Vite for the host shell and built-in module UI.

Recommended near-term approach:

- Do not use Module Federation as the first plugin mechanism.
- Use an internal module registry for built-in modules.
- Keep the UI composition model compatible with future plugin views.

Potential future use:

- Module Federation or another dynamic bundle loading mechanism may be used later as one possible way to render plugin UI.
- It must sit behind the extension platform, not define the extension platform.

### 3.3 Local Storage

Decision: SQLite with encryption support, preferably SQLCipher or an equivalent encrypted local database strategy.

Storage responsibilities:

- Encrypted vault data.
- Local module data.
- Full-text indexes where safe.
- Sync queue and change log.
- Plugin installation metadata.
- Plugin permission grants.
- Audit events.
- User settings.

Important distinction:

- Sensitive records should be encrypted before persistence.
- Search indexes must be designed carefully. Full-text indexing plaintext secrets defeats the security model. Notes may support encrypted local indexes after unlock, but synced indexes must not reveal plaintext.

### 3.4 SaaS Backend

Decision: Bun + ElysiaJS + better-auth + PostgreSQL.

Backend responsibilities:

- Account identity.
- Session management.
- Passkey/WebAuthn login.
- Subscription and entitlement.
- Device registration.
- Encrypted blob sync.
- Plugin marketplace metadata in the future.
- Server-side audit for account-level actions.

Backend non-responsibilities:

- Reading plaintext user data.
- Decrypting notes, SSH keys, DB credentials, or vault records.
- Applying domain-level business logic to encrypted local user data.

### 3.5 Encryption

Decision: Separate authentication from encryption.

Authentication:

- better-auth manages login sessions.
- Passkey/WebAuthn can provide fast account login.
- Session tokens authorize sync and billing APIs.

Encryption:

- The user sets an app-level Master Password. It is **mandatory and chosen once at first-run setup**; a recovery key is generated at setup to back it up.
- The Master Password derives or unwraps the local Encryption Key.
- Data is encrypted on the client before storage and before sync.
- The server receives encrypted blobs only.

Key rule:

- Losing the Master Password must not be silently recoverable by the server. If account recovery is supported, it must use an explicit recovery key, trusted device approval, or another zero-knowledge-compatible design.

## 4. High-Level Architecture

```text
DevKit Desktop App
  |
  |-- React Shell
  |     |-- navigation
  |     |-- command palette
  |     |-- layout/workbench
  |     |-- built-in module views
  |     |-- plugin view containers
  |
  |-- Wails Bridge
  |     |-- typed frontend-to-Go calls
  |     |-- app events
  |     |-- window/system integration
  |
  |-- Go Core
        |-- Extension Host
        |-- Capability Gateway
        |-- Core Services
        |-- Local Storage
        |-- Sync Engine
        |-- Crypto/Vault
```

The frontend should never directly expose sensitive core operations to arbitrary plugin UI. Calls from plugin UI must be routed through a permission-aware messaging boundary and then through the Go Capability Gateway.

## 5. Main Runtime Components

### 5.1 React Shell

Responsibilities:

- Render the app workbench.
- Manage navigation, tabs, panels, sidebars, and global UI state.
- Provide command palette and keyboard routing.
- Render built-in module views.
- Host plugin view containers.
- Display permission prompts and security warnings.
- Show lock/unlock state.

Non-responsibilities:

- Directly decrypting vault data unless a narrowly scoped frontend crypto flow is explicitly required.
- Granting plugin permissions without Go-side persistence and validation.
- Allowing plugin UI to call Wails services directly.

### 5.2 Wails Bridge

Responsibilities:

- Expose typed methods from Go to the React host.
- Publish Go events to the frontend.
- Keep frontend calls stable and coarse-grained.

Recommended rule:

- Wails methods should represent application-level operations, not raw repositories or low-level services.

Example:

```text
Good:
  AppService.ExecuteCommand(commandId, payload)
  AppService.QueryView(viewId, request)
  AppService.UnlockVault(request)

Avoid:
  SecretRepository.FindById(id)
  SQLite.Exec(rawSql)
  SSHService.RawDial(host, port, user, key)
```

### 5.3 Go Core Services

The Go core owns the real system capabilities.

Suggested service boundaries:

- VaultService: encrypted records, secret storage, unlock/lock state.
- CryptoService: key derivation, encryption, decryption, hashing helpers.
- SSHService: SSH profiles, sessions, terminals, port forwarding.
- DatabaseService: connection profiles, drivers, query execution, schema introspection.
- NoteService: encrypted notes, snippets, tags, local search.
- ToolService: hash, encode/decode, JWT inspect, JSON formatter, regex helper.
- SyncService: encrypted blob upload/download, conflict handling, sync queue.
- PluginService: plugin install, manifest validation, lifecycle.
- PermissionService: capability grants, prompts, revocation, audit.
- CommandService: command registry and dispatch.
- AuditService: security-relevant local events.
- SettingsService: app and plugin settings.

## 6. Extension Platform

The extension platform should follow the VS Code-style model:

```text
Plugin Manifest
  |
  | declares contributions, activation events, permissions
  v
Extension Host
  |
  | activates plugin lazily
  v
Extension API
  |
  | calls capability gateway
  v
Go Core Services
```

### 6.0 Scope Decision: Internal Modules Now, Untrusted Plugins Later

Status (locked decision):

- **In scope now:** an internal module platform only. Built-in modules ship compiled with the app and call the Extension API through the Capability Gateway.
- **Deferred to a far, non-committed vision:** external/third-party plugin loading, the plugin marketplace, plugin signing/supply-chain, and untrusted-code sandboxing (§6.8 Phases 2–4, §6.9, §16 Phases 4–5). These are not committed and must not drive near-term implementation.

Rationale: the hardest and least certain part of the VS Code-style vision is isolating untrusted code inside a Wails/Go app (there is no built-in separate extension-host process; JS-in-webview, goja, WASM, and iframe sandboxes each have serious limits). Building the marketplace/permission/signing surface before that isolation exists would be premature (see §18 Risk #1).

Consequence: the near-term work still builds the Extension API, permission model, and Capability Gateway — but only as the boundary that **internal** modules use. The requirement is that this internal design stays extensible so an untrusted runtime can be inserted later **without rearchitecting**. The invariants that guarantee this are in §6.10.

### 6.1 Why Not Module Federation as the Platform

Module Federation solves runtime UI bundle loading. It does not solve:

- permission grants,
- plugin lifecycle,
- activation events,
- crash isolation,
- secret access control,
- extension API versioning,
- marketplace trust,
- plugin signing,
- audit logs,
- offline installation,
- rollback,
- capability-based access to Go services.

Therefore, dynamic UI loading can be a later rendering implementation detail, but the platform must be based on manifests, activation, permissions, extension APIs, and capability gateways.

### 6.2 Plugin Manifest

Each plugin should include a manifest file.

Example:

```json
{
  "id": "devkit.postgres-tools",
  "name": "PostgreSQL Tools",
  "publisher": "devkit",
  "version": "1.0.0",
  "engines": {
    "devkit": "^1.0.0"
  },
  "main": "dist/extension.js",
  "activationEvents": [
    "onCommand:postgres.open",
    "onView:postgres.connections"
  ],
  "contributes": {
    "commands": [
      {
        "id": "postgres.open",
        "title": "Open PostgreSQL Manager"
      }
    ],
    "views": [
      {
        "id": "postgres.connections",
        "title": "PostgreSQL",
        "location": "sidebar"
      }
    ],
    "settings": [
      {
        "id": "postgres.maxRows",
        "type": "number",
        "default": 500
      }
    ]
  },
  "permissions": [
    "db:read",
    "db:write",
    "vault:read",
    "network"
  ]
}
```

### 6.3 Activation Events

Plugins should not run at startup by default.

Supported activation event examples:

- `onStartupFinished`
- `onCommand:<commandId>`
- `onView:<viewId>`
- `onLanguage:<languageId>`
- `onUri:<scheme>`
- `onVaultUnlocked`
- `onConnectionType:<type>`
- `onSchedule:<interval>`

Startup activation should require explicit review because it affects performance and security.

### 6.4 Contribution Points

The host should support controlled contribution points:

- commands,
- menu items,
- sidebar views,
- editor panels,
- settings,
- themes,
- snippets,
- tool definitions,
- connection providers,
- note renderers,
- command palette actions.

Contribution points should be declarative. A plugin should not mutate the shell UI directly.

### 6.5 Extension API

Plugins call stable APIs exposed by the host.

Example TypeScript shape:

```ts
export interface DevKitAPI {
  commands: CommandsAPI;
  views: ViewsAPI;
  vault: VaultAPI;
  db: DatabaseAPI;
  ssh: SSHAPI;
  fs: FileSystemAPI;
  network: NetworkAPI;
  crypto: CryptoAPI;
  settings: SettingsAPI;
  events: EventsAPI;
}
```

Example calls:

```ts
devkit.commands.registerCommand("postgres.open", async () => {
  await devkit.views.open("postgres.connections");
});

const secret = await devkit.vault.getSecret("connection/postgres/main");
const result = await devkit.db.query(connectionId, "select now()", []);
```

The plugin should not import or call Go internals. It only sees the Extension API.

### 6.5.1 Extension API Versioning

The Extension API should be versioned separately from the app version.

Recommended rules:

- Each plugin declares a compatible DevKit engine range in its manifest.
- Breaking API changes require a new major API version.
- Deprecated APIs should remain available for at least one major app release cycle.
- The host should reject plugins with unsupported engine ranges before activation.
- Runtime feature detection should be available for optional capabilities.

Example:

```json
{
  "engines": {
    "devkit": "^1.2.0",
    "extensionApi": "^1.0.0"
  }
}
```

This allows the desktop app, backend, and plugin API to evolve at different speeds.

### 6.6 Capability Gateway

The Capability Gateway is the security boundary between plugins and core services.

Responsibilities:

- Check plugin identity.
- Check requested capability.
- Check granted permissions.
- Check vault lock state.
- Validate input payloads.
- Normalize errors.
- Rate-limit sensitive operations if needed.
- Log audit events.
- Call the relevant Go core service.

Example flow:

```text
Plugin calls devkit.vault.getSecret(id)
  |
Extension Host attaches plugin identity
  |
Capability Gateway checks:
  - plugin is installed and enabled
  - plugin requested vault:read in manifest
  - user granted vault:read
  - vault is unlocked
  - secret scope allows this plugin
  |
VaultService reads and decrypts secret
  |
Gateway returns scoped result
```

### 6.7 Permission Model

Suggested permission categories:

- `vault:read:<scope>`
- `vault:write:<scope>`
- `db:connect:<connectionId>`
- `db:query:<connectionId>`
- `db:write:<connectionId>`
- `ssh:connect:<profileId>`
- `ssh:forward:<profileId>`
- `filesystem:read:<pathScope>`
- `filesystem:write:<pathScope>`
- `network:<hostScope>`
- `shell:execute:<commandScope>`
- `clipboard:read`
- `clipboard:write`
- `sync:read`
- `sync:write`
- `settings:read:<scope>`
- `settings:write:<scope>`

Dangerous permissions:

- `shell:execute`
- broad filesystem access,
- broad vault read,
- SSH forwarding,
- arbitrary network.

Dangerous permissions should require strong UI prompts and should be visible in plugin details.

Permission rules:

- Prefer scoped permissions over broad permissions.
- A plugin should not receive all vault items by default.
- User grants should be stored per plugin id, plugin publisher, plugin signing identity, permission, and scope.
- Permission grants should survive app restarts but be revocable.
- Permission grants should be re-confirmed when a plugin update adds new permissions.
- Permission grants should be re-confirmed when a plugin signing identity changes.
- Permission prompts must show what data or system capability the plugin is asking for.

Example scoped permission:

```json
{
  "pluginId": "devkit.postgres-tools",
  "permission": "vault:read",
  "scope": "secret:connection/postgres/*",
  "grantedBy": "user",
  "createdAt": "2026-06-28T00:00:00Z"
}
```

### 6.7.1 Capability Classes

Capabilities should be grouped by risk.

Low risk:

- command registration,
- theme contribution,
- local plugin settings,
- non-sensitive UI views.

Medium risk:

- clipboard write,
- reading non-sensitive metadata,
- opening a plugin webview,
- network access to declared hosts.

High risk:

- vault read/write,
- database query execution,
- SSH connection,
- filesystem read/write,
- clipboard read,
- sync access.

Critical risk:

- shell command execution,
- broad filesystem access,
- broad vault access,
- arbitrary network access,
- SSH port forwarding.

Critical permissions should be avoided in the first external plugin release unless there is a strong product reason and a clear sandboxing strategy.

### 6.8 Plugin Isolation Options

The platform can evolve through isolation levels.

Phase 1: Internal modules only.

- Built-in modules compiled with the app.
- Use the same Extension API internally.
- Validate API design before public plugins.

Phase 2: Local trusted plugins.

- Plugins installed from local disk.
- Manifest validation.
- Permission prompts.
- No remote marketplace yet.
- Prefer a separate plugin runtime process when possible.

Phase 3: Signed marketplace plugins.

- Plugin signing.
- Integrity checks.
- Version pinning.
- Rollback.
- Review process.
- Security metadata.

Phase 4: Stronger sandboxing.

- Separate process runtime.
- WASM plugin runtime for selected plugin categories.
- Webview/iframe isolation for plugin UI.
- Strict message passing.

Recommended long-term isolation model:

```text
React Shell
  |
  | message passing only
  v
Plugin UI Sandbox
  |
  | message passing only
  v
Plugin Runtime Process
  |
  | authenticated RPC
  v
Capability Gateway
  |
  v
Go Core Services
```

The host should assume plugin UI and plugin runtime code are not trusted. The Capability Gateway must enforce permissions even if the plugin runtime is compromised.

### 6.9 Plugin Supply Chain

Before marketplace distribution, the platform needs a supply-chain model.

Required controls:

- Plugin packages include a manifest, code bundle, checksums, and signature.
- The app validates signatures before install and before activation.
- Installed plugin versions are pinned.
- Updates are downloaded to a staging area before activation.
- Rollback is available if activation fails.
- Plugin updates that add permissions require a fresh user prompt.
- Marketplace metadata is not trusted as the only source of truth; local signature validation is still required.
- The app stores plugin publisher identity separately from display name.

MVP rule:

- No remote marketplace or remote plugin auto-update until signing, integrity checks, permission diffing, and rollback are implemented.

### 6.10 Extensibility Invariants (build now, extend later)

Even though only internal modules ship first, the current design MUST obey the
following invariants so that an out-of-process or WASM untrusted-plugin runtime
can be added later without rearchitecting. These are the "design it vững để mở
rộng" contract.

1. **Single choke point.** Every sensitive operation goes through the Capability
   Gateway. Internal modules are trusted but MUST NOT bypass it or call core
   services directly. If a trusted module takes a shortcut today, you cannot
   insert isolation tomorrow.

2. **One Extension API contract for everyone.** Internal modules call the exact
   same Extension API shape that a future external plugin would. Modules MUST NOT
   import core Go services or generated Wails bindings directly. The API is a
   contract, not a convenience wrapper.

3. **Versioned API.** The Extension API is versioned independently of the app
   (§6.5.1). Internal modules declare the API version they target, so the same
   compatibility gate works for external plugins later.

4. **Caller identity is first-class.** Every capability call carries a subject
   identity (`builtin.tools` now, `plugin.<id>` later). The Gateway keys every
   allow/deny decision on subject + capability + scope. Nothing infers trust from
   "it's internal".

5. **Manifest-driven, declarative contributions.** Internal modules register
   commands/views/permissions through a manifest/registry; they never mutate the
   shell directly. The contribution model is identical for future plugins.

6. **Transport-agnostic boundary (the key one for isolation).** The Extension API
   crossing point MUST be serializable: request/response DTOs are plain data, no
   live Go pointers, callbacks, or in-memory handles cross the boundary. Secrets
   cross as opaque handles/IDs, not values (§9.5). Today the call is in-process;
   because it is serializable, the same call can later be marshaled over IPC/RPC
   to a plugin process or over the host↔WASM boundary.

7. **Permissions and scopes from day one.** Grants are stored per subject +
   capability + scope even for built-in modules. Broadening to external plugins
   reuses the same machinery; no new permission system is introduced later.

8. **Lazy-activation-capable registry.** The registry stores module metadata
   without executing module code at registration. Eager activation now is fine,
   but the registry MUST support activation events so future lazy/untrusted
   activation fits.

9. **Auditable by construction.** The Gateway can emit an audit event for every
   capability call, including internal ones (§14). Isolation later changes who
   calls the Gateway, not whether calls are audited.

10. **UI goes through the bridge, never the raw runtime.** Frontend modules call
    typed bridge adapters, not generated Wails bindings. A future sandboxed
    plugin UI (iframe/WASM) reuses the same message-passing contract instead of
    touching the runtime directly.

Litmus test for any near-term change: "If external plugins shipped tomorrow,
would this code need to be rewritten, or just have a new subject + isolation
layer plugged into the existing Gateway/API?" If the answer is "rewritten", the
invariant above is being violated.

## 7. Built-In Modules

Built-in modules should use the same architecture that future plugins will use. This keeps the extension API honest.

Recommended MVP modules:

1. Notes and snippets.
   - Markdown notes.
   - Code snippets.
   - Tags.
   - Local search.
   - Encrypted storage.

2. Vault and connection profiles.
   - SSH profiles.
   - Database connection profiles.
   - API tokens.
   - Environment variables.
   - Secret references usable by other modules.

3. Hash and encoding tools.
   - SHA-256, SHA-512, MD5 for non-security compatibility use.
   - HMAC.
   - Base64 encode/decode.
   - URL encode/decode.
   - JWT inspect without sending data to a server.

4. Database manager.
   - PostgreSQL first.
   - Connection test.
   - Schema browser.
   - Query runner.
   - Result table.
   - Query history stored locally and encrypted when sensitive.

5. SSH tool.
   - SSH session profiles.
   - Terminal session.
   - Key/passphrase stored in vault.
   - Optional port forwarding later.

Non-MVP modules:

- Kubernetes manager.
- Redis UI.
- HTTP API client.
- Git client.
- Log viewer.
- Team shared vault.
- Plugin marketplace.

## 8. Local Data Model

The exact schema can evolve, but the storage model should separate concerns.

Suggested logical tables:

```text
app_settings
vault_items
vault_item_metadata
notes
note_indexes
connection_profiles
sync_entities
sync_operations
plugins
plugin_permissions
plugin_settings
audit_events
command_history
```

### 8.1 Data Classification

Every persisted field should be classified before implementation.

Suggested classes:

1. Public metadata.
   Safe to show without unlock and safe to sync as plaintext. Example: plugin id, plugin version, app theme choice.

2. Private metadata.
   Not a secret by itself, but may reveal user behavior. Example: note title, tag names, connection display name, last opened timestamp.

3. Sensitive content.
   Must be encrypted at rest and in sync. Example: note body, snippets, SSH usernames when considered private, DB names, query history.

4. Secret material.
   Must be encrypted, hidden from logs, and exposed only through scoped APIs. Example: SSH private keys, DB passwords, API tokens, passphrases.

Recommended MVP default:

- Treat note bodies, snippets, connection payloads, query history, and all credentials as encrypted sensitive data.
- Treat plugin ids, versions, and permission grants as local metadata.
- Do not sync plaintext private metadata until the product explicitly accepts that privacy trade-off.

### 8.2 Vault Items

Vault records should contain:

- stable id,
- type,
- encrypted payload,
- encrypted or non-sensitive metadata,
- created timestamp,
- updated timestamp,
- sync state,
- key version.

Sensitive values must live in encrypted payloads.

### 8.3 Search

Search requires a careful security decision.

Options:

1. Plain local index after unlock.
   Fast, but increases local exposure if database encryption is weak or unlocked.

2. Encrypted local index.
   More secure, harder to implement, weaker ranking.

3. Metadata-only search when locked, full search after unlock.
   Good default for MVP.

Recommended MVP:

- Search titles, tags, and non-sensitive metadata while locked if metadata is not secret.
- Full note/snippet search only after vault unlock.
- Never sync plaintext search indexes to the server.

## 9. Encryption Design

### 9.1 Key Hierarchy

Recommended model:

```text
Master Password
  |
  | key derivation
  v
Key Encryption Key
  |
  | unwraps
  v
Data Encryption Key
  |
  | encrypts records
  v
Encrypted Vault Items
```

The Data Encryption Key can be rotated and rewrapped without forcing every record to depend directly on the Master Password.

Recommended cryptographic defaults:

- Use a memory-hard password key derivation function such as Argon2id where platform support allows.
- Use authenticated encryption such as XChaCha20-Poly1305 or AES-256-GCM.
- Use random nonces according to the selected algorithm requirements.
- Never reuse nonce/key pairs.
- Include associated data for record type, record id, user id, device id, and key version where applicable.
- Store key derivation parameters with the encrypted key envelope.
- Store encryption algorithm and key version with each encrypted record.
- Avoid custom cryptographic algorithms.

Example encrypted record envelope:

```json
{
  "version": 1,
  "algorithm": "xchacha20-poly1305",
  "keyId": "dek_2026_06",
  "nonce": "base64...",
  "associatedData": {
    "entityType": "note",
    "entityId": "note_123"
  },
  "ciphertext": "base64..."
}
```

### 9.1.1 Key Lifecycle

Key lifecycle states:

- created,
- active,
- rotated,
- retired,
- revoked.

Rules:

- New records use the active Data Encryption Key.
- Old records can remain encrypted with older keys until touched or re-encrypted by a background migration.
- The app must be able to decrypt older supported envelope versions.
- Key rotation must be resumable and safe if the app closes mid-process.
- A key cannot be deleted while records still require it.
- Revoked device keys must be blocked from future sync operations.

### 9.2 Device Keys

Each device should have a device key pair or device secret.

Uses:

- device registration,
- trusted device approval,
- local key wrapping,
- sync identity,
- future sharing flows.

### 9.3 Recovery

Recovery must be explicit.

Possible options:

- User-generated recovery key.
- Trusted device approval.
- Emergency kit file.

Avoid:

- Server-side password reset that restores encrypted user data without a recovery key.

### 9.3.1 Backup and Export

Backup/export must preserve security properties.

Rules:

- Encrypted backup export should be supported before plaintext export.
- Plaintext export should require vault unlock and explicit confirmation.
- Export files should include format version, encryption metadata, and integrity checks.
- Import should validate format version before mutating local data.
- Backup restore should not require the sync server.

### 9.4 App Lock

The app should support:

- manual lock,
- auto-lock on sleep,
- auto-lock after idle timeout,
- lock on logout,
- lock on device change if needed.

When locked:

- decrypted keys should be cleared from memory where practical,
- plugin access to sensitive capabilities is denied,
- UI should hide sensitive values,
- sync may continue only for already encrypted blobs that do not require plaintext.

### 9.5 Secret Handling Rules

Rules:

- Secrets must not be included in errors, logs, analytics, telemetry, or crash reports.
- Secret values should be passed through short-lived structs and cleared when practical.
- Frontend state should store references to secrets, not secret values, unless the user is actively viewing or editing them.
- Plugin APIs should prefer opaque secret handles over plaintext secret values.
- Long-running sessions such as SSH and DB connections should receive credentials through core services, not through plugin UI.

Preferred flow:

```text
Plugin asks to connect using connectionId
  |
Capability Gateway checks permission
  |
DatabaseService retrieves credential from VaultService
  |
DatabaseService opens connection
  |
Plugin receives connection handle, not password
```

## 10. Sync Architecture

The sync system should be append/change oriented.

Local flow:

```text
User changes note
  |
NoteService updates encrypted local record
  |
SyncService writes sync operation
  |
Background worker uploads encrypted blob
```

Server flow:

```text
Client uploads encrypted entity
  |
Server authenticates session
  |
Server checks subscription/device entitlement
  |
Server stores encrypted blob and version metadata
```

Server data should include:

- user id,
- device id,
- entity id,
- entity type,
- encrypted payload,
- version,
- updated timestamp,
- deletion marker,
- conflict metadata.

Server data should not include:

- plaintext note bodies,
- plaintext SSH usernames/passwords/private keys,
- plaintext DB credentials,
- decrypted snippets,
- decrypted plugin private data.

### 10.1 Conflict Handling

MVP conflict strategy:

- Last writer wins for low-risk settings.
- Create conflict copies for notes/snippets.
- Never silently merge secrets.

Future:

- CRDT or structured merge for notes if collaborative editing becomes a priority.

### 10.2 Sync Protocol Versioning

The sync protocol must be versioned independently from the desktop app.

Rules:

- Each sync request includes client app version, sync protocol version, device id, and last known server cursor.
- The server can reject unsupported protocol versions with a structured upgrade-required error.
- Encrypted entity payload format is versioned separately from HTTP API version.
- Clients must tolerate unknown entity types by storing or skipping them safely according to policy.
- Delete operations should use tombstones so deletion can sync across offline devices.
- Sync cursors should be opaque to clients.

### 10.3 Sync Idempotency

Sync operations must be idempotent.

Rules:

- Each local mutation gets an operation id.
- Retrying the same upload must not create duplicate records.
- Server writes should use entity id plus version or operation id for deduplication.
- Download application must be transactional locally.
- Failed sync batches must be resumable.

### 10.4 Multi-Device Consistency

Required behavior:

- A device can be revoked from the account.
- Revoked devices cannot upload new changes.
- Existing encrypted blobs from a revoked device remain stored unless the user deletes them.
- Device revocation does not magically revoke data already downloaded to that device.

This limitation must be understood before offering team or enterprise features.

## 11. SaaS Backend Architecture

Suggested backend services:

- AuthService: better-auth session and passkey integration.
- AccountService: profile, devices, account settings.
- BillingService: subscription, plan, entitlement.
- SyncService: encrypted blob storage.
- DeviceService: device registration and revocation.
- PluginRegistryService: marketplace metadata in future.

Suggested PostgreSQL tables:

```text
users
sessions
accounts
devices
subscriptions
sync_entities
sync_entity_versions
plugin_registry
plugin_versions
server_audit_events
```

The backend API should be versioned from the beginning:

```text
/v1/auth
/v1/devices
/v1/sync
/v1/billing
/v1/plugins
```

### 11.1 Backend Security Rules

Rules:

- All sync APIs require authenticated user and registered device.
- Device id from request body is not trusted without session/device binding.
- Subscription checks must not be the only authorization layer.
- Server audit logs must avoid encrypted payload contents and client secrets.
- Rate limits should exist for login, device registration, sync upload, and plugin downloads.
- Passkey login should be paired with a recovery and device-management story.

### 11.2 Backend Scalability Rules

The backend should stay simple until usage requires more.

Near-term:

- PostgreSQL single database.
- Blob payloads can live in PostgreSQL while volume is low.
- Background jobs for cleanup and billing reconciliation.

Scale-up path:

- Move encrypted blobs to object storage when payload volume grows.
- Keep metadata and indexes in PostgreSQL.
- Partition sync entity versions by user id or time only after measuring table growth.
- Add queue-based fanout for future team/collaboration features.

Avoid early:

- Month-based sharding before real usage data.
- Domain-specific server parsing of encrypted user data.
- Complex distributed systems before sync correctness is proven.

## 12. UI Architecture

### 12.1 Workbench Layout

The app should feel like a tool workspace, not a landing page.

Primary regions:

- left activity bar,
- sidebar,
- editor/panel area,
- bottom panel for terminal/log/output,
- command palette,
- status bar.

This layout naturally supports tools such as notes, SSH sessions, database query tabs, and plugin views.

### 12.2 Module Registry

The React shell should use a registry:

```ts
type ModuleDefinition = {
  id: string;
  title: string;
  icon: string;
  routes: RouteDefinition[];
  commands: CommandDefinition[];
  views: ViewDefinition[];
};
```

Built-in modules register through this interface. Future plugins can contribute through a compatible manifest-driven path.

### 12.3 Plugin UI

Recommended progression:

1. Built-in React components for first-party modules.
2. Sandboxed plugin panels with message passing.
3. Dynamic plugin UI bundles only after signing, integrity, cache, and rollback are ready.

Plugin UI must not receive direct access to the Wails bridge.

### 12.4 Performance Budgets

Initial budgets should be treated as targets and refined after measurement.

Desktop startup:

- Cold start to first usable shell: under 2 seconds on a modern developer laptop.
- Shell render should not wait for plugin activation.
- Vault unlock can happen after shell load.

Plugin activation:

- No plugin activates during startup unless explicitly allowed.
- Command-triggered activation should show progress if it takes over 300 ms.
- Slow activation should be measured and reported in plugin diagnostics.

Search:

- Metadata search should return first results under 150 ms for typical local datasets.
- Full encrypted note search after unlock should stream or paginate results for large datasets.

Database manager:

- Query results should be paginated or streamed.
- Default query result limit should be conservative.
- Large exports should run as background jobs.
- Query cancellation must be supported.

SSH:

- Long-running SSH sessions should be managed by Go services, not React state.
- Terminal output should be backpressured to avoid freezing the UI.

Sync:

- Sync should run in background workers.
- Upload/download batches should be bounded.
- UI should show sync status without blocking local edits.

### 12.5 Resource Management

Rules:

- Core services should accept `context.Context` for cancellation.
- Long-running operations should expose progress events.
- Database queries and SSH sessions should support cancellation.
- Plugin runtimes should have lifecycle state: inactive, activating, active, deactivating, failed.
- Failed plugins should be isolated and disabled without crashing the app.
- The app should expose diagnostics for slow commands, slow plugins, and failed sync jobs.

## 13. Error Handling

All cross-boundary calls should return structured errors.

Suggested error shape:

```json
{
  "code": "permission.denied",
  "message": "Plugin does not have vault:read permission.",
  "details": {
    "pluginId": "devkit.postgres-tools",
    "permission": "vault:read"
  },
  "recoverable": true
}
```

Important error categories:

- `auth.required`
- `vault.locked`
- `permission.denied`
- `plugin.disabled`
- `plugin.versionUnsupported`
- `sync.conflict`
- `network.unavailable`
- `db.connectionFailed`
- `ssh.connectionFailed`
- `crypto.invalidPassword`
- `storage.corrupt`

### 13.1 Compatibility and Migrations

Migration compatibility must be designed from day one.

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

- Migrations are forward-only in production.
- Destructive migrations require backup creation first.
- Local migration steps must be idempotent.
- The app should refuse to start if the local database was opened by a newer incompatible version.
- Extension API compatibility should be tested with sample plugins.
- Backend API deprecation should have a defined support window.
- Encrypted record migrations must be resumable.

### 13.2 Feature Flags

Use feature flags for risky platform features.

Good candidates:

- external plugin install,
- plugin marketplace,
- remote sync,
- new encryption envelope,
- new database driver,
- shell execution capability,
- AI features if added later.

Feature flags should not bypass security checks. They only control availability.

## 14. Observability and Audit

Local audit events should record security-relevant actions:

- vault unlocked,
- vault locked,
- plugin installed,
- plugin enabled/disabled,
- permission granted/revoked,
- secret read by plugin,
- shell command requested,
- SSH connection opened,
- database connection opened,
- sync device added/revoked.

Audit logs must avoid plaintext secrets.

## 15. Testing Strategy

### 15.1 Go Tests

Focus areas:

- encryption and key management,
- encrypted record envelope compatibility,
- vault lock/unlock,
- permission checks,
- capability gateway,
- plugin manifest validation,
- plugin package signature validation,
- sync queue behavior,
- sync idempotency,
- local database migrations,
- database driver abstraction,
- SSH profile handling,
- cancellation for long-running operations.

### 15.2 Frontend Tests

Focus areas:

- shell navigation,
- command palette,
- module registry,
- permission prompt UI,
- locked state UI,
- database result rendering,
- note editor behavior.

### 15.3 Integration Tests

Focus areas:

- frontend command to Go capability flow,
- plugin permission denial,
- plugin permission grant,
- vault locked behavior,
- sync upload/download of encrypted blobs,
- sync retry/idempotency behavior,
- migration from previous local schema,
- database connection test with local containers where available,
- plugin activation and failure isolation.

### 15.4 Security Tests

Focus areas:

- plugin cannot call ungranted capabilities,
- plugin cannot bypass Wails bridge,
- locked vault blocks sensitive calls,
- plaintext secrets are not written to logs,
- synced payloads are encrypted,
- permission changes after plugin update require confirmation,
- revoked devices cannot upload new sync operations,
- exported backup stays encrypted unless explicitly exported as plaintext.

### 15.5 Architecture Review Gates

Before a feature touches secrets, plugins, sync, auth, or filesystem access, require:

- threat model update,
- data classification update,
- permission model review,
- migration impact review,
- abuse-case tests,
- rollback behavior,
- logging review.

## 16. Development Roadmap

Scope note: Phases 4 (Local Trusted Plugins) and 5 (Marketplace) are a **far,
non-committed vision** per §6.0. They are kept here for direction only; Phases
0–3 must satisfy the extensibility invariants in §6.10 so those later phases
remain possible without rearchitecting.

### Phase 0: Architecture Foundation

Deliverables:

- App module layout.
- Typed Wails bridge conventions.
- Go service boundaries.
- Local storage package.
- Crypto package spike.
- Threat model.
- Data classification registry.
- Migration framework.
- Structured error package.
- Internal command registry.
- Internal module registry.

### Phase 1: Local MVP

Deliverables:

- Workbench UI.
- Vault unlock/lock.
- Encrypted SQLite storage.
- Notes/snippets module.
- Hash/encoding tools.
- SSH profile storage.
- PostgreSQL connection profile and query runner.
- No external plugins yet.
- No SaaS sync yet.

### Phase 2: Internal Extension API

Deliverables:

- Manifest format for internal modules.
- Command contribution points.
- View contribution points.
- Capability Gateway.
- Permission model.
- Extension API versioning.
- Plugin diagnostics model.
- Internal modules migrated to Extension API.

### Phase 3: SaaS Sync

Deliverables:

- Auth with better-auth.
- Device registration.
- Encrypted blob sync.
- Conflict handling.
- Subscription entitlement.
- Recovery key flow.

### Phase 4: Local Trusted Plugins

Deliverables:

- Local plugin install.
- Manifest validation.
- Plugin lifecycle.
- Permission prompts.
- Sandboxed UI panel strategy.
- Plugin API docs.

### Phase 5: Marketplace

Deliverables:

- Plugin signing.
- Marketplace metadata.
- Version compatibility.
- Review process.
- Integrity checks.
- Rollback.
- Paid plugin or subscription integration if needed.

## 17. Suggested Repository Layout

Recommended future layout:

```text
.
|-- cmd/
|   `-- devkit/
|-- internal/
|   |-- app/
|   |-- bridge/
|   |-- commands/
|   |-- crypto/
|   |-- db/
|   |-- extension/
|   |-- permissions/
|   |-- plugins/
|   |-- services/
|   |-- storage/
|   |-- sync/
|   `-- vault/
|-- frontend/
|   |-- src/
|   |   |-- app/
|   |   |-- workbench/
|   |   |-- modules/
|   |   |-- extension/
|   |   |-- services/
|   |   `-- ui/
|-- docs/
|   `-- architecture/
|-- plugins/
|   `-- examples/
`-- backend/
    |-- src/
    |-- migrations/
    `-- tests/
```

The backend can live in a separate repository later. Keeping it under `backend/` early is useful while the product shape is still changing.

## 18. Key Risks

1. Plugin system too early.
   Building external plugin support before proving built-in modules can slow the product down significantly.

2. E2EE recovery complexity.
   Account recovery, device migration, and lost password flows must be designed carefully.

3. Search versus encryption.
   Full-text search and zero-knowledge sync are naturally in tension.

4. Dynamic UI loading risk.
   Loading third-party UI code into a desktop developer tool increases supply-chain risk.

5. Scope creep.
   "All tools a developer needs" can become too broad. The app should first win as a secure local dev vault plus a few excellent daily tools.

6. Weak plugin isolation.
   If external plugin code runs in the same trusted runtime as the host, a single malicious plugin can compromise the trust model.

7. Unversioned encrypted data.
   If encrypted records do not carry algorithm, key, and envelope versions, future migration becomes dangerous.

8. Overbroad permissions.
   If early permissions are broad, later tightening will break plugins or upset users.

9. Recovery mismatch.
   If users expect account reset to recover encrypted data, but the product is zero-knowledge, onboarding and recovery UX can become a trust problem.

## 19. Recommended Immediate Decisions

1. Treat the first release as local-first and single-user.
2. Build internal modules first.
3. Design the Extension API now, but do not open external plugins yet.
4. Use a Capability Gateway from the beginning.
5. Implement encrypted local storage before SaaS sync.
6. Keep backend sync blob-oriented and zero-knowledge.
7. Avoid Module Federation until plugin signing, permissioning, and isolation are ready.
8. Version every persisted and cross-process contract.
9. Use scoped permissions from the first plugin design.
10. Prefer secret handles over plaintext secret values in plugin APIs.
11. Define performance budgets before building DB manager and SSH terminal.
12. Do not promise public plugin marketplace until signing, sandboxing, and rollback are implemented.

## 20. Open Questions

1. Should the first MVP include both SSH and DB manager, or choose one to make excellent first?
2. Should notes support rich text, Markdown only, or Markdown with code-focused extensions?
3. Should SQLCipher be the default local encryption layer, or should records be encrypted at the application layer before SQLite?
4. Should plugins eventually run as JavaScript, WASM, external processes, or a combination?
5. What is the intended first paid feature: sync, pro tools, team vault, or plugin marketplace?
6. What recovery model is acceptable for users who lose the Master Password?
