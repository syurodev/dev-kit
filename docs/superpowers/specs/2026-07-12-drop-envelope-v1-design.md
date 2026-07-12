# Drop Envelope V1 Design

## Goal

DevKit is a new project with no production V1 ciphertext to preserve. Remove
V1 envelope create/read paths entirely so the only supported encrypted-record
format is V2 with AAD binding. Keep existing API names (`SealV2`,
`OpenWithAD`). Update PERSONAL/base DevKit docs to match.

## Decision

- **Data policy:** Reject V1 envelopes on open. No migrate tool.
- **API style:** Minimal — keep `SealV2` / `OpenWithAD` names (no rename).
- **Repos:** V2 + XChaCha20-Poly1305 + AAD
  (`record_type + NUL + record_id + NUL + key_id`).

## Code changes (`/Users/syuro/Workspace/dev-kit`)

### `internal/crypto`

- Remove `Seal` (V1 writer).
- Remove or replace `Open` so there is no nil-AAD decrypt path for secret
  records. Callers that need decrypt use `OpenWithAD` only.
- `OpenWithAD`: accept only `EnvelopeVersionV2` with matching AAD; V1 returns a
  clear unsupported-version error (no silent nil-AAD fallback).
- `Envelope.Validate`: accept only version `2`.
- `EnvelopeVersionV1`: remove if unused, or keep solely for explicit reject
  tests — prefer remove once tests no longer need the constant.
- Update `aead_test.go` / `envelope_test.go`: delete V1 readability tests;
  add reject-V1 cases; keep AAD mismatch / NUL rejection coverage.

### `internal/vault`

- Remove `Service.Seal` and `Service.Open` (only used by vault unit tests for
  the V1 path today). Services already use `SealV2` / `OpenWithAD`.
- Update `OpenWithAD` comments: no V1 compatibility claim.
- Rewrite `service_test.go` seal/open cases onto `SealV2` / `OpenWithAD`.

### Services

- `notes`, `database`, `ssh`: no call-site changes expected (already V2).
- Re-run their tests after crypto/vault changes.

### Out of scope

- SQLCipher / full-database encryption
- Algorithm change
- Renaming `SealV2` → `Seal`
- Bulk re-encrypt / migrate CLI
- Sync runtime

## Docs changes (`/Users/syuro/Workspace/PERSONAL/base`)

Update Project-state and related knowledge pages so they no longer claim V1
readability:

| Page | Change |
|---|---|
| `architecture-decisions.mdx` ADR-005 | Rewrite to **Accepted: V2-only**. Note V1 dropped because greenfield; no backward-read. |
| `technical-contracts.mdx` Encryption envelope | Valid version = `2` only; remove Open/V1 compatibility bullets. |
| `security-controls.mdx` | Control row: V2 AAD only; remaining risk no longer “legacy V1 readable”. |
| Other V1 mentions | Grep and fix any leftover “V1 readability / Open reads V1” claims in DevKit docs. |

Do not reintroduce status noise on knowledge pages.

## Verification

In `dev-kit`:

```bash
GOCACHE=/private/tmp/devkit-go-cache go test ./internal/crypto/... ./internal/vault/... ./internal/services/notes/... ./internal/services/database/... ./internal/services/ssh/...
GOCACHE=/private/tmp/devkit-go-cache go test ./...
GOCACHE=/private/tmp/devkit-go-cache go vet ./...
```

In `PERSONAL/base` (after doc edits):

```bash
bun run types:check
bun run lint
```

## Success criteria

1. No public API creates or decrypts V1 envelopes.
2. Presenting a V1 envelope to `OpenWithAD` fails with unsupported version.
3. Existing V2 service flows and tests pass.
4. Docs (ADR-005, contracts, security-controls) describe V2-only; no stale V1
   compatibility claims.
