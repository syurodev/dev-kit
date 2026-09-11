# Database module — implementation batch 2

Self-contained brief. You do not need the design conversation that produced it.

## What this batch is

Batch 1 landed profile options, safe-mode and export. This batch adds **query
history and saved queries**, and the shared plumbing they need. It stops before
the driver abstraction and before the SSH tunnel.

Read first: `base-doc/content/projects/dev-kit/database.mdx`, section
"Query history và Saved query". It is the contract. Where this brief and that
page disagree, the page wins; say so rather than silently choosing.

**In scope:** T1–T6 below.
**Out of scope — do not start:** SSH tunnel, `Driver`/`Conn` abstraction,
MySQL/MariaDB, schema cache, ERD, `SessionInspector`, `QueryPlanner`, and the
`ListByTypeAndMetaField` helper that Notes version history will eventually want.

---

## The one thing to understand before writing code

History and saved queries look like one feature with a `kind` flag. They are
not, and an earlier draft of the design page said they were.

`vault_items` is **sync-bound by construction**: `SQLMutationRepository.Put`
runs `validateMutationRecord`, which rejects any record type absent from
`IsSupportedItemType` (`internal/vaultitem/sqlmutationrepository.go:67,198`),
and the same transaction writes an outbox operation. There is no local-only
record in that table.

Saved queries must sync. History must not — it is a trace of what happened on
this machine, and its SQL carries literals. One type with a `kind` field would
force both to the same answer. So they get different storage, and "history never
leaves the device" becomes structural rather than a filter someone can remove by
accident.

## T1 · `db-query` record type — saved queries only

`internal/vaultitem/schema.go`. Add `db-query` to `IsSupportedItemType` and
`ValidateRecordSchema`. This is the sixth type.

This file is the gate sync uses to decide what it will accept, for live
snapshots and tombstones alike. Treat it as core, not as a local edit inside
`internal/database`.

- `metadata`: `{"profile_id":"<24hex>","name":"..."}`, plaintext, **never SQL**.
- `payload`: envelope V2 of the SQL text, AAD `(db-query, <id>, db-query-body)`.
- There is **no `kind` field**. Storage decides the kind.

## T2 · `db_query_history` table

New migration (m13) in `internal/storage/sqlite.go`, plus a table constant next
to the existing ones.

Minimum columns: `id`, `profile_id`, `sql_envelope` (blob), `created_at`. **No
column holds plaintext SQL.**

- Seal and open with `vault.SealV2` / `vault.OpenWithAD`
  (`internal/vault/service.go:156,166`) directly — not through `vaultitem`.
- Distinct AAD: `(db-query-history, <row id>, db-query-body)`. An envelope from
  this table must not open anywhere else.
- Sync must never learn this table exists. Do not register it anywhere near
  `internal/syncstore` or the outbox.

## T3 · Automatic history + prune

Write one row after **every successful `DBQuery`**. Failed queries are not
history.

- Prune to the 50 newest per `profile_id`, by `created_at`, in plain SQL. Do not
  reach for a metadata-field lister; there is no vault item involved.
- **A history failure must not fail the query.** The statement already ran and
  the user already has rows. If sealing or inserting fails, log it and return
  the result. Turning a successful query into an error because bookkeeping
  failed is the wrong trade.
- `DeleteProfile` must delete that profile's history. It lives outside
  `vault_items`, so it does not ride any vault-item cascade.

## T4 · Saved queries

`DBSaveQuery(profileID, name, sql)`, `DBListSavedQueries(profileID)`,
`DBDeleteSavedQuery(queryID)`.

- Never pruned.
- Delete is a hard delete. There is no trash for queries, unlike Notes.
- Listing: `ListByType("db-query")` then filter by `profile_id` in Go is fine at
  this scale — saved queries are tens per profile, not thousands. Do not build
  an index for it in this batch.

## T5 · Vault-locked is an error, not an empty list

`DBListQueryHistory` and `DBListSavedQueries` must return an error when the
vault is locked, not an empty slice. An empty list reads as "you have never run
a query here", which is false and unrecoverable-looking. This applies to both
storage mechanisms.

## T6 · MCP

Add `db.history.list` and `db.savedQuery.list`, both `read`, both scoped
`profile:<id>`.

Do **not** expose `db.saveQuery` or `db.deleteSavedQuery`. Deciding what is
worth keeping is a human act, same rule that keeps `DBSetActiveProfile` and
`DBUpdateProfileOptions` off the tool list.

## Acceptance

- [ ] `go test ./...` and `bun run build` pass.
- [ ] A saved query round-trips through `vault_items` and appears in the sync
      outbox; a history row does **not** appear in the outbox.
- [ ] Writing the 51st history row for a profile leaves 50, and does not touch
      another profile's rows.
- [ ] A history envelope fails to open under the saved-query AAD.
- [ ] `DBQuery` still returns its result when history insertion fails (inject a
      failure; do not skip this one).
- [ ] Deleting a profile removes its history rows.
- [ ] Both list methods error while the vault is locked.
- [ ] `db.saveQuery` and `db.deleteSavedQuery` are absent from the MCP tool
      list, proven by enumerating the real list — see
      `TestDatabaseWriteMethodsAreAbsentFromMCPTools` in
      `internal/bridge/database_test.go` for the pattern.

## Testing rules for this batch

**A negative test must be shown to fail.** Batch 1 shipped a test named
`...IsAbsentFromMCPTools` whose body only asserted the server was non-nil — it
would have stayed green while the method was exposed. When you write an
assertion that something is absent, temporarily make it present, watch it fail,
then restore. Say in your report that you did this.

The same applies to the outbox assertions above: prove the history test fails if
you write history through `vaultitem` instead of the local table.

## Notes

- Linux cross-compile fails inside Wails' own GTK cgo files. Pre-existing, not
  yours.
- `base-doc` has unrelated uncommitted changes. Leave them alone.
