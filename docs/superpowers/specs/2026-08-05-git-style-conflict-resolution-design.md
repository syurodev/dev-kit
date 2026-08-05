# Git-style conflict resolution — design

Status: Design (not implemented). Target: close the remaining Phase 3 client gap
("minimal conflict UX") using a Git-style model, testable against
`cmd/mocksyncserver`.

## Goal

Handle sync conflicts like Git: never auto-pick a winner. Keep the record in a
`conflict` state holding **both** sides (ours = local, theirs = remote) and
require an explicit user action to resolve. Never silently merge secrets.

## Why the current code cannot converge same-version conflicts

- Server arbitration (`cmd/mocksyncserver`, and the real backend at
  `/dev-kit/saas-backend`) accepts `entity_version == head+1`; a device that
  pushes the same version loses and receives a `Conflict` (outbox
  `StatusConflict`, `internal/sync/runner.go`).
- On pull, the loser's apply path (`internal/storage/localmutation.go` +
  `sync_entity_versions`) rejects the winning remote op because
  `currentVersion >= remoteVersion`. So "theirs" is dropped and the two devices
  never converge without an explicit resolution step.

## Model

1. **Detect**: push conflict marks the record `sync_state = "conflict"`; the
   current local record is **ours**.
2. **Capture theirs**: when a subsequent pull carries the divergent remote op
   for a record that is in `conflict` state, the apply path stores the
   authenticated remote snapshot as **theirs** (a per-record conflict side)
   instead of dropping it. The main record (ours) is left untouched.
3. **Present**: the UI shows ours vs theirs (Git-marker style) with actions:
   - **Use ours** — keep local content.
   - **Use theirs** — take remote content.
   - **Edit/merge** — only for note text; the user produces the merged text.
   - Secrets (db/ssh) offer only Use ours / Use theirs — no silent merge.
4. **Resolve**: write the chosen content as a **new local mutation at
   `remote_head + 1`**, clear the conflict side and `conflict` state, and let the
   normal outbox flow sync it. Both devices converge on the resolved version.

## Data model changes

- New table `sync_conflicts` (per record, account-bound; **no FK**, app-layer
  integrity): `account_id`, `record_id`, `record_type`, `remote_operation_id`,
  `remote_entity_version`, `theirs_snapshot` (authenticated remote record JSON,
  metadata + encrypted payload envelope — never plaintext secret),
  `local_change_key`, `detected_at`. Unique `(account_id, record_id)`.
- `vault_items.sync_state` gains the `conflict` value (already a free-form string
  column today).

## Component changes

- `internal/sync` outbox: add `Conflicts()` to the `Outbox` interface (+
  `MemoryOutbox`, `SQLOutbox`) to list `StatusConflict` changes.
- `internal/storage`:
  - Apply path captures theirs into `sync_conflicts` when the target record is in
    `conflict` state and the remote op is the divergent version (guard otherwise
    unchanged).
  - New atomic op `ResolveConflict(ctx, account, recordID)` used by "Use theirs":
    replace ours with `theirs_snapshot`, advance `sync_entity_versions` to the
    remote version, delete the `sync_conflicts` row and the conflicted outbox row.
  - "Use ours"/"Edit-merge": re-materialize a local mutation at `remote_head + 1`
    with the chosen content, delete the `sync_conflicts` row + conflicted outbox
    row (leaving normal sync to push it).
- `internal/services/syncclient`: `Conflicts()` listing (ours/theirs metadata,
  no plaintext), and `ResolveConflict(ctx, recordID, strategy, mergedPayload?)`.
- `internal/services/vaultitem`: helpers to read "ours" and to apply a resolved
  record (reuses existing seal/open + `sync_state`).
- `internal/bridge`: `SyncConflicts()` + `SyncResolveConflict(recordID, strategy)`
  through the Capability Gateway (new grants `sync:conflicts`, `sync:resolve`).
  Only non-secret metadata + explicitly-requested decrypted field values cross
  the boundary; payloads for compare are opened on demand like other item reads.
- `frontend/src/modules/sync`: conflict list + a resolve view showing ours/theirs
  and the three actions (secrets: two actions).

## Security invariants (bảo mật tuyệt đối)

- `theirs_snapshot` is the already-authenticated remote record (passed the
  OpenPayload AAD check during pull); it is stored the same way as any other
  encrypted record (metadata plaintext, payload envelope encrypted). No plaintext
  secret is persisted.
- Resolution runs in a single account-bound transaction; partial states cannot
  leave a record both dirty and conflicted.
- The existing apply guard is unchanged; conflict capture is additive.
- Bridge exposes conflict metadata + on-demand decrypted field values only inside
  the gateway, audited like other sensitive reads. No tokens/ciphertext leak.

## Test plan (against the mock)

- Unit: `Conflicts()` listing; `ResolveConflict` transitions (use ours / theirs /
  merge); `sync_conflicts` uniqueness; guard unchanged for non-conflict applies.
- End-to-end (two `syncclient` instances sharing `cmd/mocksyncserver`):
  1. Both devices edit the same note offline from version `v-1`.
  2. Device A `SyncNow` → wins (`sent`).
  3. Device B `SyncNow` → conflict; next pull captures theirs.
  4. Resolve B with each strategy; `SyncNow` B → assert convergence: record equals
     the resolved content on both devices; conflict cleared.

## Out of scope (deferred to post-SaaS-BE, see `/dev-kit/saas-backend`)

- Multi-entity/multi-record convergence at scale, latency/error → retry matrices,
  and real Keycloak auth — run against the production backend, not the mock.
