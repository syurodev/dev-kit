# Vault Item Repository Design

## Goal

Close the Phase 1 gap where the vault-item repository is still *Design only*.
Unify the three separate built-in item stores (`notes`, `connection_profiles`,
`ssh_profiles`) into a single `vault_items` store with a shared repository +
store layer that owns storage, crypto (seal/open), and cross-type search. The
public service signatures for notes/database/ssh stay unchanged so the bridge,
frontend adapters, UI, and per-type capability grants are not affected.

DevKit is greenfield: there is no production data to preserve, so no backfill
migration is written.

## Decisions (approved during brainstorming)

- **Item model:** Approach A — a shared `vaultitem` store owns
  storage/crypto/search; thin per-type services (notes, database, ssh) sit on
  top and keep their own connection behavior (DB pool/TLS, SSH host keys / run).
  Not the fully-generic CRUD + type registry (that is a Phase 2 concern).
- **Data:** Greenfield. Migration creates `vault_items` and drops the three
  legacy tables. No backfill / re-encrypt.
- **Schema:** Promote `type` to a real SQL column; keep `metadata` inside the
  JSON blob (search filters metadata in Go — fine at local scale).
- **AAD:** Keep the per-type `key_id` binding (`note-body`, `db-password`,
  `ssh-secret`) so the anti-swap guarantee is unchanged; only the call path is
  unified through the shared store.
- **Search:** Cross-type search runs on plaintext metadata only and works while
  the vault is locked (never decrypts payloads). One new bridge family
  `VaultSearch`; existing `NotesSearch` becomes a type-scoped search and keeps
  its contract.
- **Capability:** Keep all existing per-type grants. Add one subject
  `builtin.search` with capability `vault:search`, scope `item:search`.
- **Errors:** No new error codes. `vault_locked` on `OpenPayload` while locked;
  search does not return `vault_locked`.

## Storage & schema (`internal/storage`)

New table replaces the three legacy KV tables:

```sql
CREATE TABLE vault_items (
  id   TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  data TEXT NOT NULL          -- JSON blob (vaultitem.Record)
);
```

JSON blob shape (`vaultitem.Record`):

```json
{
  "id": "itm_ab12",
  "type": "note",
  "metadata": { "title": "Deploy" },
  "payload": { "version": 2, "algorithm": "xchacha20-poly1305", "key_id": "note-body", "nonce": "…", "ciphertext": "…" },
  "created_at": "…",
  "updated_at": "…",
  "key_version": 1,
  "sync_state": "local"
}
```

- `type` is duplicated as a SQL column for `ListByType` and indexed lookups; the
  authoritative copy still lives in the blob.
- `metadata` is plaintext, non-sensitive, and shaped by the item type.
- `payload` is the encrypted-secret envelope (V2), or `null` for types without a
  secret.

### Migration (forward-only, greenfield)

- `m5: create vault_items` — `CREATE TABLE IF NOT EXISTS vault_items (id TEXT
  PRIMARY KEY, type TEXT NOT NULL, data TEXT NOT NULL)`.
- `m6: drop legacy item tables` — `DROP TABLE IF EXISTS notes;
  DROP TABLE IF EXISTS connection_profiles; DROP TABLE IF EXISTS ssh_profiles;`.
- `vault_keyring` is unchanged.
- Migrations stay idempotent and versioned via `schema_migrations`.

The generic `storage.KV` (id + data only) is left unchanged. `vaultitem`
provides its own SQLite-backed repository that writes both the `type` column and
the `data` blob and filters on the column in `ListByType`. This keeps the
existing `storage.KV` contract stable while giving the item store the one extra
indexed column it needs.

## Shared layer (`internal/services/vaultitem`)

New package. Knows nothing about note/db/ssh specifics.

```go
type Record struct {
    ID, Type   string
    Metadata   json.RawMessage
    Payload    *crypto.Envelope   // nil when the type has no secret
    CreatedAt, UpdatedAt time.Time
    KeyVersion int
    SyncState  string
}

type Repository interface {
    List(ctx) ([]Record, error)
    ListByType(ctx, itemType string) ([]Record, error)
    Get(ctx, id string) (Record, error)
    Put(ctx, Record) error
    Delete(ctx, id string) error
}

type Vault interface {
    SealV2(ad crypto.AssociatedData, plaintext []byte) (crypto.Envelope, error)
    OpenWithAD(env crypto.Envelope, ad crypto.AssociatedData) ([]byte, error)
}

type Store struct { repo Repository; vault Vault }
```

`Store` API used by the thin services:

- `Create(itemType string, metadata any, keyID string, secret []byte) (Record, error)`
  — generate id, seal `secret` with AAD `{RecordType: itemType, RecordID: id,
  KeyID: keyID}`, set timestamps / `key_version` / `sync_state="local"`, `Put`.
  When `secret == nil`, `Payload` stays nil.
- `OpenPayload(rec Record, keyID string) ([]byte, error)` — `OpenWithAD` with the
  matching AAD; returns `vault.ErrLocked` (mapped to `vault_locked`) while locked.
- `UpdateMetadata`, `UpdateSecret`, `Get`, `ListByType`, `Delete`.
- `Search(query string, types ...string) ([]Record, error)` — filter on
  plaintext metadata across types; never decrypts payloads; works while locked.

Repository implementations: `MemoryRepository` (tests) and `SQLRepository`
(SQLite KV), mirroring the existing per-service repository pattern.

## Thin services

Public signatures unchanged — only internals move onto `vaultitem.Store`.

- **notes** (`internal/services/notes`): `noteMetadata{ Title }`, `type="note"`,
  `keyID="note-body"`, secret = body. `Create/Get/List/Search` delegate to the
  store. Remove `notes.Repository` / `MemoryRepository` / `SQLRepository`.
- **database** (`internal/services/database`): `dbMetadata{ Host, Port,
  Database, Username, SSLMode }`, `type="db-profile"`, `keyID="db-password"`,
  secret = password. CRUD via store; **keep** `pool.go`, connect/query, TLS
  verify-full logic untouched.
- **ssh** (`internal/services/ssh`): `sshMetadata{ Host, Port, Username,
  AuthMethod, HostKeyFingerprint }`, `type="ssh-profile"`, `keyID="ssh-secret"`,
  secret = key/passphrase material. CRUD via store; **keep** `hostkeys.go`,
  connect / run-command untouched.

### Wiring (`internal/app/container.go`, `main.go`)

- One shared `vaultitem.Store` over `storage` table `vault_items`, injected into
  all three services.
- `NewContainer` (memory) uses a shared `vaultitem.MemoryRepository`.
- `NewContainerWithStores` (SQLite) takes one `vaultitem.Repository` instead of
  three per-type repositories.
- `main.go` replaces the three `NewSQLRepository` lines with one shared store.

## Bridge, capability, errors

- **Bridge:** add family `VaultSearch(query) -> []{ id, type, label, updated_at }`.
  Existing families (`NotesList`, `NotesSearch`, `DBQuery`, `SSHRunCommand`, …)
  are unchanged. `NotesSearch` becomes `Search` scoped to `type="note"`.
- **Capability:** keep every existing grant. Add subject `builtin.search`,
  capability `vault:search`, scope `item:search`. Item ids stay opaque via the
  existing `recordID()` path.
- **Errors:** no new codes. `vault_locked` for `OpenPayload` while locked;
  `validation_failed` for missing metadata; search never returns `vault_locked`.
- **Redaction:** metadata still passes through `audit.RedactMap`; payloads
  (secrets) never enter audit metadata.

## Testing (TDD)

- `vaultitem/store_test.go`: CRUD round-trip; `OpenPayload` fails while locked;
  anti-swap (payload sealed for item A fails to open under item B's id); `Search`
  filters metadata across types; `key_version` / `sync_state` set correctly.
- `vaultitem/repository_test.go`: KV round-trip for memory + SQLite repos.
- Keep and adjust `notes_test.go`, `database_test.go`, `ssh_test.go` so they stay
  green under unchanged public signatures — the safety net proving the refactor
  is behavior-preserving. Remove the old per-service repository tests.
- `bridge/gateway_test.go`: add `VaultSearch` allowed/denied cases; keep existing
  bridge family tests.
- `storage/migrations_test.go`: after migrate, `vault_items` exists and the three
  legacy tables are gone; migration is idempotent.

## Implementation order

Each step keeps tests green before moving on.

1. `internal/crypto` — unchanged (reuse `SealV2` / `OpenWithAD`).
2. `storage`: `vault_items` table + migrations m5/m6 + tests.
3. `vaultitem` package (repository + store) + tests — the foundation, first.
4. Rewrite `notes` service on the store; make its tests green.
5. Rewrite `database` service (keep pool/TLS); make its tests green.
6. Rewrite `ssh` service (keep host keys); make its tests green.
7. `container.go` + `main.go` wiring onto one shared store.
8. Bridge `VaultSearch` + capability grant + tests.
9. Verify: `go test ./...`, `go vet ./...`, `go build -tags server .`,
   `npm --prefix frontend run build`.

## Docs impact (update AFTER code lands)

Contract/evidence docs describe actual runtime, so update them only once the code
is merged:

- `technical-contracts.mdx` — persistence contract (`vault_items` replaces three
  tables), new `VaultSearch` bridge family, AAD binding through the shared store.
- `implementation-status.mdx` — "Vault item repository" *Design only* →
  *Implemented*; update evidence paths and the Verified snapshot commit.
- `data-sync.mdx` — describe `vault_items` with `sync_state` / `key_version` as
  the sync foundation.
- `architecture-decisions.mdx` — new **ADR-008: Unified `vault_items` store for
  built-in item types**.
- `roadmap.mdx` — mark the Phase 1 vault-item repository "Next recommended work"
  item done.

## Out of scope

- Fully-generic item CRUD + type registry (Approach B / Phase 2).
- Backfill or re-encrypt of legacy data (greenfield).
- Encrypting note titles or other plaintext metadata.
- SQLCipher / full-database encryption.
- Sync runtime, MCP runtime, external plugins, marketplace.
- Encrypted search indexes.
