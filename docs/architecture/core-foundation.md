# Core Foundation

This document tracks the implemented DevKit core foundation (Phase 0).

## Implemented Packages

- `internal/app`: application composition root and core service wiring.
- `internal/apperrors`: structured application errors.
- `internal/audit`: redaction helpers for logs and audit events.
- `internal/storage`: storage interfaces and migration runner skeleton.
- `internal/crypto`: encrypted record envelope, Argon2id KDF, XChaCha20-Poly1305 AEAD, and the DEK/KEK keyring with recovery key.
- `internal/vault`: vault service with a mandatory Master Password (setup/unlock/lock, recovery-key unlock) and keyring persistence (memory + JSON file store).
- `internal/commands`: command registry.
- `internal/modules`: built-in module registry.
- `internal/permissions`: permission grants and policy checks.
- `internal/capabilities`: permission-aware capability gateway.
- `internal/services/tools`: hash/encoding services.
- `internal/bridge`: Wails-facing app service, vault methods, and DTOs.

## Current Rules

- Frontend code calls `frontend/src/bridge`, not generated Wails bindings directly.
- A Master Password is mandatory and set once at first-run setup; the vault holds the decrypted DEK in memory only while unlocked and zeroizes it on lock.
- Sensitive plugin-style calls must pass through the Capability Gateway.
- Encrypted record formats are versioned from the first implementation.
- Long-running future operations must accept cancellation.

## How It Runs

- Desktop: `task dev` / `wails3 dev` (needs a display + gtk4/webkitgtk).
- Headless local use: server mode — `task run:server` or `go build -tags server` then run the binary; it serves HTTP on `127.0.0.1:8080` only. The app is gated behind the mandatory vault: first run shows "Create Master Password", later runs show "Unlock". This mode is not a remote API or deployment surface: public binding, TLS, authentication, and authorization are unsupported until a separate server design supplies them.

## Next Modules

1. Notes/snippets with encrypted storage.
2. Persistent encrypted vault item repository.
3. Local search after vault unlock.
4. SSH profile storage.
5. PostgreSQL connection profile and query runner.
