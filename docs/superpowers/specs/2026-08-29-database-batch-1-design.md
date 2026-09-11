# Database module — implementation batch 1

Self-contained brief. You do not need the design conversation that produced it.

## What this batch is

The Database module is `Implemented` for Postgres but only covers 7 of the 19
bridge methods its design calls for. This batch is the **foundation slice**: the
design-system tokens that drifted, the profile-options write path, safe-mode,
and result export. It deliberately stops before the driver abstraction.

Read first: `base-doc/content/projects/dev-kit/database.mdx` — it is the
contract. Where this brief and that page disagree, the page wins; tell the user
rather than silently choosing.

**In scope:** A, B, C1, C2, D below.
**Out of scope — do not start:** SSH tunnel, `Driver`/`Conn` abstraction,
MySQL/MariaDB, query history, schema cache, ERD, `SessionInspector` (activity /
cancel / terminate), `QueryPlanner` (EXPLAIN), new MCP tools.

---

## A · Two design tokens

`dev-kit-app/frontend/src/index.css` already defines `--state-soft`,
`--surface-well`, `--feedback-danger-soft`, `--border-danger`. Two are missing.

Add to **both** the `:root` block and the `:root[data-theme="light"]` block —
same values in both, because the Figma variables alias the same primitive in
Dark and Light (that is the file's existing convention for danger tints, see
`--feedback-danger-soft` above them):

```
--feedback-danger-hover: rgba(216, 120, 134, 0.24);
--feedback-danger-pressed: rgba(216, 120, 134, 0.32);
```

Then repoint the `destructive` variant in
`dev-kit-app/frontend/src/components/ui/button.tsx`. It currently hardcodes
opacity modifiers that have drifted from the design source (code 15/25/30,
design 14/24/32):

```
destructive: "bg-status-danger/15 text-status-danger border border-status-danger/30 hover:bg-status-danger/25 active:bg-status-danger/30",
```

Use `--feedback-danger-soft` / `--feedback-danger-hover` /
`--feedback-danger-pressed` and `--border-danger` so both sides read one source.

## B · Component parity check — verify, do not rewrite

Code is **ahead of** the design file here; nothing should need porting.

- `button.tsx` already has all 7 variants. Confirm only.
- `switch.tsx` exists. Confirm the checked track resolves to the same colour as
  `--action-primary`; fix the mapping if it does not.
- Do **not** add an icon prop to Button. The design file models the trailing
  icon as a component property because Figma has no children; in React it is
  `children`. That asymmetry is intended.

## C1 · `DBUpdateProfileOptions`

Today a profile is immutable after creation. Add a metadata-only write path.

**Fields this batch can change:** `env`, and a new `read_only` (bool, default
false). Nothing else.

> `engine` and `ssh_profile_id` are **not** part of this batch even though the
> design page mentions them. `engine` belongs with the driver abstraction, and
> `ssh_profile_id` with the tunnel. Adding them now creates dead fields whose
> values no code path honours. They join through this same method later.

Files: `internal/database/database.go`, `internal/bridge/database.go`.

1. Add `ReadOnly bool \`json:"read_only"\`` to the `metadata` struct and to
   `ProfileSummary`. Absent in stored records means `false` — do not write a
   migration, the records are JSON metadata.
2. The narrow `Store` interface in `internal/database/database.go` does not
   expose an update. **`vaultitem.Store` already has `UpdateMetadata(id,
   metadata) (Record, error)`** (`internal/vaultitem/store.go:106`) — widen the
   local interface to include it. Do not add a new repository method.
3. `Service.UpdateProfileOptions(id string, opts UpdateProfileOptionsInput)`.

**Validation rules — these are load-bearing, not preferences:**

- Validate `env` exactly as `CreateProfile` does (`s.envs.Exists`). No update
  path may produce a state that create forbids.
- **Validate the incoming value, never the stored one.** A profile whose env tag
  was deleted must still accept a new valid tag. Checking current state first
  would brick it permanently — that state has a UI (`Database / 25 Env
  unresolved`) whose only escape is this method.
- Changing `env` is a **policy** change, not a display option: it decides
  whether `db.query` over MCP must confirm, and `prod` is locked. Emit an audit
  entry for it (`internal/audit`, reached through the capability gateway like
  every other sensitive call).
- Bridge method is **human-only — do not expose it over MCP**, same rule as
  `DBSetActiveProfile`. Lowering `env` from `prod` removes a confirmation
  requirement.

**Pool invalidation — each field costs something different:**

| Field | Action |
|---|---|
| `env` | Nothing. Plaintext metadata, absent from the DSN. |
| `read_only` | **Must drop the pool.** See C2. |

`PoolManager.Get(ctx, profileID, dsn)` (`internal/database/pool.go:24`) caches
**by `profileID` alone and ignores the `dsn` argument on a cache hit**. A
changed DSN or session setting therefore does *not* reach an existing pool.
`PoolManager.Remove(profileID)` is the invalidation path.

## C2 · Safe-mode (`read_only`)

When `read_only` is true, every connection in the profile's pool must carry
`default_transaction_read_only = on`, so **PostgreSQL** rejects writes. DevKit
must not inspect SQL to guess statement types — that is an explicit
non-behaviour of this product, stated in the design page's Known ceiling.

- Mechanism: `pgxpool.Config.AfterConnect`, which runs for every new connection
  the pool creates.
- **`AfterConnect` only runs for new connections.** Toggling `read_only` on a
  profile that already has a live pool leaves the pooled connections writable.
  Call `PoolManager.Remove(profileID)` on toggle. A test that toggles the flag
  and then asserts a write is rejected is the point of this task; without the
  drop it passes only on a cold pool.
- Applies to UI and MCP equally because it lives at the pool, not the caller.
- It does **not** cover cancelling or terminating other sessions
  (`pg_cancel_backend` is a `SELECT`). Out of scope here, but do not write a
  comment or doc line claiming safe-mode is a general write guard.

## D · Export query result — frontend only

`QueryResult{Columns []string, Rows [][]string, RowCount int, Truncated bool}`
is already in hand after a query. **No bridge method, no capability, no Go
change.** Serialise in the frontend and write through the Wails save dialog.

- Same 200-row cap the table shows. There is no "export everything" path;
  widening means editing the SQL.
- CSV: cells are already strings; escape per RFC 4180.
- JSON: **every value serialises as a string** — `{"id": "42"}`, not
  `{"id": 42}`. `Rows` is `[][]string`, so types were lost before export. This
  is a property of `QueryResult`, not a choice export gets to make. Surface it
  in the UI; do not silently coerce numbers, and do not "fix" it by guessing
  types from the text.

UI reference: Figma `15 Database` → `Database / 20 Export result`.

## Acceptance

- [ ] `bun run build` passes, including the performance budget.
- [ ] `go test ./...` passes; new tests cover: env validated against the
      incoming value while the stored tag is unresolvable; toggling `read_only`
      on a **warm** pool causes a subsequent write to be rejected.
- [ ] `DBUpdateProfileOptions` is reachable from the bridge and absent from the
      MCP tool list.
- [ ] Changing `env` produces an audit entry.
- [ ] Export produces a file with 200 rows and all-string JSON values.
- [ ] Nothing from the out-of-scope list has been started.

## Notes

- Cross-compile check for Windows is expected to pass; Linux cross-compile fails
  inside Wails' own GTK cgo files and is a pre-existing condition, not your bug.
- `base-doc` has unrelated uncommitted changes. Leave them alone.
