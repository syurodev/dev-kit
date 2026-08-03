# Sync Security Gates Design

## Goal

Close the pre-remote-transport security gates on the local sync foundation so
the cryptographic contract matches the architecture before a network transport
exists. Also close the related medium/low findings from the same review:
idempotency silent loss, outbox `syncing` stickiness, Go TLS vuln toolchain,
frontend audit advisories, and unbounded KDF params.

## Decisions (approved during brainstorming)

- **Scope:** One foundation pass covering all six findings.
- **Local identity:** Persist generated `account_id` and `device_id` on first DB
  open; `protocol_version` is a code constant.
- **Operation identity:** Every mutation has an independent `operation_id`.
  Idempotency key = hash(namespace + operation_id). Retries with the same
  operation id must match the full immutable identity, including `key_id` and
  plaintext content digest.
- **Outbox recovery:** Lease via `syncing_since` (default 5 minutes), reclaim
  stale `syncing` in `Pending`, plus startup recovery.
- **Frontend:** Bump audited packages; move `shadcn` to `devDependencies`.
- **Packaging:** Single cohesive change set (sync + storage migration + Go pin
  + frontend + KDF), not split supply-chain into a later PR.

## Out of scope

- Real HTTP/SaaS transport and auth session tokens.
- Pull/apply remote changes, conflict UI, sync status UI.
- Device revocation against a remote registry (local contract only prepares the
  fields the backend will enforce later).
- Non-reachable advisories (for example openpgp / GO-2026-4970) unless they
  remain after the Go pin.

## 1. Sync cryptographic context

### Local identity store

New migration creates a dedicated single-row identity table:

```sql
CREATE TABLE IF NOT EXISTS sync_identity (
  id TEXT PRIMARY KEY,          -- constant row id: "local"
  account_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);
```

On first open:

1. If no row exists, generate UUIDs for `account_id` and `device_id`, persist.
2. Later opens load the same row; identity must not rotate silently.

`SyncProtocolVersion` remains a Go constant (`1` initially). It is not stored
in SQLite because upgrades are code-driven and must be explicit.

### SyncContext

Required on seal / queue / push paths:

| Field | Source | Role |
|---|---|---|
| `account_id` | `sync_identity` | Bind ciphertext to account |
| `device_id` | `sync_identity` | Bind ciphertext to device |
| `protocol_version` | code constant | Reject upgrade/downgrade mismatches |
| `operation_id` | per mutation (UUID); required | Replay/idempotency unit |
| `entity_version` | caller-supplied; must be `>= 1` | Rollback / version binding |

`entity_version` is separate from crypto `key_version`. Until vault-item sync
versioning lands, callers may pass `1` for the first queued mutation of a
record and increment on later mutations. `NewOperationID()` is a helper for
callers; `QueuePayload` does not invent an id when omitted.

### Replication envelope (v2)

Bump `ReplicationEnvelopeVersion` to `2`. Version `1` is rejected (greenfield;
no production v1 replication traffic).

Public envelope fields:

- existing: `record_id`, `record_type`, `key_version`, `envelope_version`, `ciphertext`
- new: `account_id`, `device_id`, `protocol_version`, `operation_id`, `entity_version`

Nested payload remains a serialized `crypto.Envelope`. Plaintext never appears
in public fields.

### AAD binding

Replication AAD must authenticate the full sync context, not only record
identity. Canonical associated data includes:

- replication namespace + envelope/protocol version
- `account_id`, `device_id`
- `record_type`, `record_id`
- `key_id`, `key_version`
- `operation_id`, `entity_version`

Any swap of account, device, protocol, operation, or entity version fails open
through AEAD authentication.

### Change + Transport

`Change` carries the same sync context fields (plus existing outbox state).
`Transport.Push` continues to accept `[]Change`; each change already embeds
account/device/protocol/operation identity. Session tokens are deferred until
auth exists; device/account/protocol are the mandatory pre-transport gate.

`MemoryTransport` duplicate detection compares the expanded immutable identity
(including context fields and opaque payload bytes).

## 2. Idempotency

### Key derivation

```text
idempotency_key = "dev-kit/sync/queue/v2:" + hex(sha256(
  "dev-kit/sync/queue/v2" || 0x00 || operation_id
))
```

Namespace bumps to `v2` because the key material changes from
record/operation/timestamp to operation id.

### Queue behavior

`QueuePayload`:

1. Require vault, outbox, sync context (including required `operation_id`),
   entity version (`>= 1`), key id, plaintext.
2. Compute plaintext content digest (`sha256` of plaintext bytes).
3. If an existing change with the same idempotency key is found:
   - compare immutable identity: record id/type, operation, key version,
     key id, content digest, account/device/protocol, operation id, entity
     version
   - `created_at` is scheduling metadata only and is not part of the
     conflict compare
   - match → return existing change (no re-seal; nonce safety preserved)
   - mismatch → `ErrIdempotencyConflict`
4. Otherwise seal with context-bound AAD, enqueue serialized envelope.

Retries must reuse the same `operation_id`. A new mutation always gets a new
id from the caller via `NewOperationID()`.

### Persisted immutable fields

Outbox rows store enough identity to compare without decrypting:

- `account_id`, `device_id`, `protocol_version`
- `operation_id`, `entity_version`
- `key_id`, `content_digest`

## 3. Outbox lease & recovery

### Schema (migration)

Migration `m8` creates `sync_identity` and alters `sync_outbox` with
`ALTER TABLE ... ADD COLUMN` (SQLite-safe defaults) for:

- `syncing_since TEXT NOT NULL DEFAULT ''`
- `account_id`, `device_id`, `protocol_version`
- `operation_id`, `entity_version`
- `key_id`, `content_digest`

Rows created before this contract that lack context fields fail validation on
read/push rather than being silently coerced into a sync context.

Default lease duration: **5 minutes**. `MemoryOutbox` / `SQLOutbox` take the
lease in constructor/config so `Pending(now, limit)` keeps its current
signature. Runner `SyncingLease` should match the outbox lease (tests inject
one clock + one lease value).

### Semantics

- `MarkSyncing`: set `status=syncing`, `syncing_since=now`.
- `Pending(now, limit)` returns ready rows where:
  - `status=pending AND next_attempt_at <= now`, or
  - `status=syncing AND syncing_since != '' AND syncing_since <= now - lease`
- Reclaiming a stale syncing row goes through `MarkSyncing` again (lease
  refresh).
- `RecoverStaleSyncing(now)` (startup / open): reset stale `syncing` rows to
  `pending` and clear `syncing_since`. Safe because transports are idempotent
  on `operation_id`.
- `MarkSent` / `MarkConflict` / `MarkFailed` clear `syncing_since`.
- `MarkRetry` returns to `pending`, sets `next_attempt_at`, clears
  `syncing_since`.

## 4. Go toolchain

- Set `go 1.26.5` and `toolchain go1.26.5` in `go.mod` to pick up the
  `crypto/tls` fix for GO-2026-5856.
- Re-run `govulncheck ./...` after the pin. The previously reachable path via
  `application.App.Run` must be cleared.
- Leave unreachable advisories alone unless they still appear as reachable
  after the upgrade.

## 5. Frontend dependency tree

- Run `bun update` (or targeted upgrades) so `postcss`, `fast-uri`,
  `brace-expansion`, `@hono/node-server`, and any other audited packages move
  to fixed versions in `bun.lock`.
- Move `shadcn` from `dependencies` to `devDependencies` (CLI/codegen only;
  not needed in the desktop runtime bundle).
- Verify with `bun audit` and `bun run build`.

## 6. KDF parameter bounds

`DeriveKey` continues to require algorithm `argon2id` and non-empty salt, and
additionally rejects unsafe numeric params:

| Param | Min | Max |
|---|---:|---:|
| Memory (KiB) | 1024 | 2097152 |
| Iterations | 1 | 10 |
| Parallelism | 1 | 16 |
| Key length | 16 | 64 |
| Salt length | 8 | 64 |

`DefaultKDFParams` (64 MiB, 3 iterations, 4 lanes, 32-byte key, 16-byte salt)
stays inside the window. Malicious or corrupted keyring rows that request
extreme memory/time fail closed at unlock instead of allocating unbounded
resources.

## Verification

Must pass before claiming completion:

- Sync unit tests:
  - AAD/context mismatch fails open
  - envelope v1 rejected; v2 round-trips
  - idempotency returns existing change only when digest/key/context match
  - idempotency conflict when plaintext or key id differs under same
    operation id
  - stale `syncing` reclaimed by `Pending` / `RecoverStaleSyncing`
- Storage migration tests for new columns / identity table
- Crypto tests for KDF bound rejection and default success
- `go test ./internal/sync/... ./internal/storage/... ./internal/crypto/...`
- `govulncheck ./...`
- Frontend: `bun audit` clean (or documented residual only) and `bun run build`

## Compatibility notes

- Local sync foundation has not shipped a remote protocol; bumping replication
  envelope and queue idempotency namespace to v2 is acceptable without a
  ciphertext migration tool.
- Migration `m8` forward-extends the schema. Pre-context outbox rows are
  invalid for the new contract and fail closed on validate/push; no silent
  backfill of account/device/operation fields.
- No bridge/UI surface changes in this design; identity load and queue wiring
  into vault item writes can follow in a later sync-client plan once this
  contract lands.
