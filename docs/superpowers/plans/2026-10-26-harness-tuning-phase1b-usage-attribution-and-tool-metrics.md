# Harness tuning Phase 1b: account attribution and MCP result measurements

Date: 2026-10-26. Status: **implemented; validation notes below**.

**Goal:** Extend the Phase 1a report with the account that actually ran each recorded turn, measured MCP text-result sizes,
and a bounded view of recorded account moves and subsequent cache writes. Collect evidence before changing profiles or failover.

**Baseline:** Phase 1a merged in PR #336 (`origin/main` was `326140ff` when this plan was written). Start implementation from
current `main`, not the old `docs/subscription-harness-tuning-spec` checkout: main also contains the independent Claude CLI
fix in #335. Recheck the migration number and touched seams before editing.

**Spec:** [Subscription-path harness tuning](../specs/2026-10-26-subscription-harness-tuning-design.md), §3. This plan implements
measurement that can ship now. Profile and override revisions remain SQL `NULL` until Phase 2 supplies real snapshots. No
profile store, proposal tool, group filtering, new truncation mode, quota inference or account-selection change is included.
Vendor token accounting remains Claude-only. DevKit-owned MCP byte measurements may cover all Hub vendors because they are
measured by the host; this plan does not add Codex/opencode token normalization or claim provider quota/cost data.

## Decisions and reporting contract

### Account attribution is historical data

- Capture the runtime's resolved account **after** `switchAccountForTurn` and `runtimeForTurn`, before calling the vendor.
  `sess.AccountID` alone is insufficient: `fillAccountSpec` can fall back to the built-in account when an added account is gone.
- Reuse the runtime-binding seam (`fillAccountSpec` / `recordSessionAccount`), extended with immutable usage facts. Do not resolve
  the account from the current session row after the prompt finishes. A successful prompt and a failed prompt with billed metadata
  use the same captured facts. Limit follow-ups enter `invoke` again and get their own snapshot.
- Old turns keep unknown attribution. Do not backfill them from today's session account or from a nearby switch notice.
  Built-in accounts are recorded canonically as `<vendor>-default`; unknown is not an empty spelling of the built-in account.
- Account ids are reusable slugs (`newAccountHome` / `accountIDCandidate`). Give added account rows a stable generation id,
  preserve it on updates, and stamp it on turns. Deleting and recreating the same slug must not merge two account lifetimes.
  This identifies a local account configuration, not a provider identity or a person; signing in differently does not prove identity.
- Labels are resolved in the UI only when both account id and generation match. A deleted/recreated account gets a historical
  label with a short recorded id, never the new account's name. Handle loading/error separately from “removed”.
- Preserve Phase 1a totals, per-metric coverage and unknown/zero distinctions. Account grouping is another partition of the same
  recorded turns: including the unknown bucket, account sums and coverage must reconcile with overall sums and coverage.

### MCP sizes describe what DevKit serves

- Count only `tools/call` on identity-bound Agent Hub sockets, including subagents using that same socket. Shared terminal
  clients have no reliable vendor/session attribution and are excluded. The UI says this explicitly.
- The metric is **UTF-8 text-content bytes**, before and after the existing `ResultBudget` transformation. Sum `len(Text)` across
  text blocks. Do not count JSON envelope bytes, `structuredContent` a second time, image/base64 bytes, or vendor-internal tools.
  Do not estimate tokens or claim these are the bytes ultimately included in a vendor's model request.
- Observe immediately around the actual mutation inside the budgeting middleware. Separate nested middlewares would depend on
  SDK middleware ordering and may measure the same side of the cut twice. Keep existing budget decisions and exemptions unchanged.
- Track calls, result-bearing calls, text-bearing calls, non-text calls, tool-error results, handler errors, exempt calls, cuts,
  total text bytes before/after, and max text bytes before/after. A Go error with no result has unknown bytes, not a zero-sized result.
  An image-only result has a known text size of zero and a non-text flag. Structured-only results are reported as non-text/unsupported
  by this metric, not as evidence that their model context cost is zero.
- Use actual before/after counts to mark a cut; do not infer it from `before > ResultBudget`. Exempt tools, images and errors may
  exceed the budget without being cut. A truncation marker can add bytes: display “byte reduction”, which may be negative, rather
  than clamping to fabricated savings. Persist the measurement version and cut mode (`head` in this phase).
- Capture workspace/vendor/profile facts from a host-owned immutable runtime/turn snapshot at call start. Never accept attribution
  in MCP args. This snapshot lookup must use memory only; no SQLite call or account-status probe on the MCP hot path.

### Collection is bounded and honest about gaps

- Add a stdlib-only `internal/usagemetrics` leaf: value types, aggregation, a bounded collector and a `database/sql` adapter.
  It imports no other `internal` package. `mcp`, `agenthub` and the composition root may import it; enforce this in `internal/arch`.
- Observation only copies identifiers/enums/counters; retain neither SDK results nor contexts, argument objects, JSON payloads,
  transcript bodies, credential data or vendor errors. Validate bounded identifier lengths. Failed unknown-tool requests collapse
  into one fixed bucket, rather than persisting arbitrary tool names chosen by a caller.
- `Record` merges under a short mutex into at most 4,096 dimension keys, with no database I/O. Flush every two seconds on one
  worker; a detached in-flight batch and the active batch each have this bound. Additional new keys increment dropped counters.
  The writer has a two-second DB timeout and never holds the collector mutex while writing or waiting for SQLite.
- Each flush is one transaction of additive UPSERTs plus a per-run committed batch-sequence watermark. A retry checks that
  watermark before applying the same immutable batch, so a commit whose acknowledgement was lost cannot double its counters.
  On a proven rollback, merge the detached batch back if capacity allows and count anything that cannot be retained. On an
  ambiguous commit outcome retain the bounded in-flight batch until its watermark can be checked; do not merge it into a different
  batch. Do not add a second SQLite connection to work around `MaxOpenConns(1)`.
- Track collector runs durably (start, last committed flush, graceful close, dropped samples, write failures, unclean recovery).
  Opening a new run marks abandoned active runs unclean. Reports expose possible collection gaps and the collection start time;
  absence of a row is not proof that a tool was never used. A killed process can lose buffered observations.
- Reading a report does not synchronously flush. Show persisted aggregates and “saved through” / collector health. Clean shutdown
  first stops new MCP accepts and drains in-flight observations, then closes/flushes the collector before the database closes.
  Test the timeout path and report incomplete collection instead of hanging shutdown.

### Account moves are observations, not causal cost estimates

- Reuse account notices in `agent_transcript`; do not copy transcripts into a second event store. Confirmed seam: `AccountNotice`
  has typed `kind/from/to`; `Recorder.recordPlain` writes an individual row with `id`, `seq` and `CreatedAt`. `UpdateSessionInfo`
  is not one of the upserted session-state kinds, so a later notice does not overwrite the earlier timestamp.
- `noteAccountSwitch` also runs when reopening and rollback both fail. Existing `kind="switch"` therefore does **not** prove a
  runtime reopened. Add a host-only optional runtime-opened flag and resolved target generation to newly recorded notices.
  The successful reopen path stamps true, binding-only fallback stamps false, failed-and-undone notices remain failures.
  Old notices with no flag remain unverified. Vendor-supplied `_meta.devkit` text must not manufacture the host-only flag.
- Capture `started_at` for new recorded turns. A finish timestamp alone can attach the preceding account's long-running turn
  to a later move. Keep `created_at` as the existing completion/day grouping timestamp for Phase 1a compatibility.
- Match a confirmed notice to the first recorded turn starting after it in the same session and resolved account generation,
  before another move in that session. Tied/contradictory times, absent start times, missing generation or missing usage are marked
  unknown. No match across sessions, restart guesses or fallback to the current account. Zero cache writes remain a known zero.
- Label the value “cache writes on the first recorded turn after the move”. Earlier failed/cancelled work may have no accounting
  row and may already have warmed the cache. Do not call this value cache invalidation cost or subtract it from an unrelated turn.
- Bound this view to a caller-selected UTC window (default 30 days, maximum 90), pages of at most 100 notices, with a stable
  `(created_at, id)` cursor tied to vendor/window filters. Fetch only projected account-notice fields, ids and times, never full
  transcript payloads or reasons. Include failed, binding-only and legacy-unverified counts separately.

## Storage and API shape

Migration **51 if still free**; otherwise use the next version. Use a focused migration helper called from `storage.Migrate`.
Do not rewrite existing migrations or wrap all migration `Up` functions in an outer transaction.

| Store | Additions / contract |
| --- | --- |
| `agent_accounts` | Nullable `usage_generation` text. Seed existing added rows once with opaque random ids; built-in generation is the fixed marker `builtin`. `SQLAccountStore.Put` preserves an existing generation on conflict and creates one only on insert. A row written by an older binary without a generation is unknown until safely initialized; never guess from its label. |
| `agent_turns` | Nullable `account_id`, `account_generation`, `started_at`, `usage_workspace_id`, `usage_context_version`, `harness_profile_revision`, `harness_override_revision`. Historical rows remain NULL. Context version 1 distinguishes a new deliberately unbound workspace (NULL id) from legacy rows whose workspace was not captured. No FK to mutable account/workspace/profile rows. |
| `agent_tool_usage_daily` | Key: UTC day, vendor, workspace key, tool, profile key, override key, measurement version and cut mode. Store counts, sums, maxima and nullable profile/override revisions. No content and no per-call rows. |
| `agent_tool_usage_runs` | Run id, committed batch sequence, started/last-flushed/closed times, dropped/write-failure counters and unclean marker. An incomplete run is visible evidence of possible gaps. This is collector health, not an audit of tool actions. |
| Transcript query index | Add a suitable session/kind/time index only after checking existing indexes and `EXPLAIN QUERY PLAN`. Restrict the query to notice-bearing `session_info_update` rows and the requested window. Guard JSON extraction of damaged payloads with `CASE WHEN json_valid(payload) ...`. |

SQLite does not make NULLs equal for composite uniqueness. For daily UPSERT keys use explicit non-null key spellings
(`profile_key='none'`, `override_key='none'`, workspace key `''` for a known unbound workspace). Revisions themselves stay NULL,
not 0 or -1; future versioned keys use `v:<revision>`. Validate key/value consistency. Never collapse revision 0 into “none”.

Accounts and tokens are not one-to-one: a local account can sign in again, and built-in accounts can exist without an account
row. The built-in runtime snapshot uses canonical id plus `builtin` without creating a row or probing authentication.
Historical turn/account-generation references survive account deletion. Daily aggregates survive session deletion because they
contain no session ids; their workspace ids can become historical. Deleting a workspace must not relabel old usage as a new one.

Use optional store interfaces as in Phase 1a so unrelated fake stores do not acquire every metrics method. Suggested seams:

```go
// Names may follow existing conventions; semantics are required.
type RuntimeUsageFacts struct {
    Vendor, WorkspaceID, AccountID, AccountGeneration string
    ProfileRevision, OverrideRevision *int64
}

type TurnAttribution struct {
    ContextVersion int
    StartedAt time.Time
    Facts RuntimeUsageFacts
}

// usagemetrics owns its independent Scope/Observation/DailyRow/RunHealth types.
// Service exposes memory-only facts; main adapts them into the leaf types.
// No bridge -> storage or mcp -> storage/app/bridge import is introduced.
```

Extend `UsageReport.ByAccount` with account id, generation and `UsageTotals`; the empty-id group is unknown. Add UI-only
`AgentToolUsageReport` and `AgentAccountMoveUsageReport` methods rather than enlarging the token report with unrelated rows.
The latter accepts the validated date window, limit and cursor. All lists are non-null, and all methods use `requireWindow` plus
`agentAccountsCall` / `uiOnly` and `agenthub:read` on explicit `usage:*` scopes. Extend `legacy_ops_methods.json`; no MCP tool is added.

`AgentAccountInfo` adds the opaque generation so the UI can resolve names safely. It does not expose a home directory, login,
credential or provider account id. Report DTOs return numbers, opaque ids, UTC times, fixed status enums and a bounded cursor.
New invalid-query / unavailable errors need deliberate bridge mappings; never return SQL text or a stored payload in an error.

## Files and layering

| Files | Responsibility |
| --- | --- |
| `internal/storage/sqlite.go`, new `usage_metrics_migration.go`, tests | Forward migration and version registration; no business aggregation. |
| `internal/agenthub/accounts.go`, `accounts_store.go`, account tests | Stable account generation; preserve existing account lifecycle behavior. |
| `internal/agenthub/types.go`, `store.go`, store tests | Nullable turn attribution round-trip; both existing InsertTurn paths preserved. |
| New `internal/agenthub/turn_attribution.go`, tests; `service.go`, `account_switch.go`, `rewind.go` | Runtime facts, capture at prompt boundary, cleanup and host-confirmed move metadata. |
| `internal/agenthub/usage_source.go`, `usage_report.go`, tests | Historical account grouping and captured-workspace precedence. |
| New `internal/usagemetrics/{types,aggregate,collector,sqlstore}.go`, tests | Bounded observation, daily persistence and collection health; no in-module dependencies. |
| `internal/mcp/result_budget.go`, new `result_metrics.go`, tests; `server.go` | Measurements at the actual cut boundary, optional callback wiring, caller isolation. |
| New `internal/agenthub/tool_usage_report.go`, `account_move_usage.go`, tests | UI-only metrics reads; bounded projected notice query and conservative correlation. |
| `internal/agenthub/update.go`, transcript/account-switch tests | Optional trusted host metadata; preserve transcript notice/card semantics. |
| `internal/bridge/agenthub_usage.go`, `agenthub_accounts.go`, tests | DTOs, read guards, query validation, safe account generation. |
| `internal/app/container.go`, grant tests; `internal/bridge/errors.go`, `testdata/legacy_ops_methods.json` | Read scopes, public errors and bridge contract. Reuse the existing usage read grant where sufficient. |
| `main.go`, `internal/arch/arch_test.go` | Collector lifetime and injections; enforce the new leaf boundary. |
| `frontend/bindings/...` | Regenerate, never hand-edit. |
| `frontend/src/bridge/agentUsage.ts`, `agentAccounts.ts`, tests | Additive typed wrapper/DTO fields and separate report calls. |
| `frontend/src/modules/agenthub/usage/` | Account-name model, metrics/move tables, hooks and connected panel. Reuse shared `DataTable`. |
| Account UI hooks and fixtures touched by a new required field | Generation propagation, loading vs historical naming; no sign-in/account actions. |
| Reference docs and this spec/plan | Exact measurement scope, coverage, buffering, unknown attribution and remaining phases. |

## Task 0 — Prepare against merged main

- [x] Read `AGENTS.md`, `docs/agent-reference/{agent-hub-accounts,mcp-tools,agent-customization-recovery}.md` and the Phase 1a code.
- [x] Start an implementation branch from current `origin/main` using the repo's normal Git workflow; preserve unrelated work.
- [x] Recheck latest migration, both production `TurnRecord` constructions, runtime-open/close paths and current grant/error tests.
- [x] Confirm `frontend/dist` exists for architecture checks (or build the frontend). Do not start the native GUI in a headless host.
- [x] Record baseline Go tests and frontend tests/build; distinguish pre-existing failures from changes made by this plan.

```bash
go test ./internal/storage/ ./internal/agenthub/ ./internal/mcp/ ./internal/bridge/ ./internal/app/ ./internal/arch/ -count=1
cd frontend && bun test --isolate && bun run build
```

## Task 1 — Add nullable storage and stable account generations

**Depends on:** Task 0.

- [x] Write migration tests against a pre-migration fixture: existing usage JSON/raw bytes and aggregate totals remain identical;
  historical turn attribution is NULL, not the current session account; migration can be reopened without regenerating identities.
- [x] Implement the schema above; verify uniqueness of unprofiled daily buckets despite nullable revisions.
- [x] Extend `TurnRecord`, `InsertTurn`, `ListTurns` and `TurnUsageRows` scan paths using `sql.Null*`/pointers. Update SQL column
  counts together. A fake/legacy writer with no context still inserts a valid unknown-attribution turn.
- [x] Extend `AccountRecord` and `SQLAccountStore` with an opaque generation. Account rename, usage/sign-in refresh and Put do
  not change it. Delete/recreate of a freed slug does. Preserve the built-in no-row case and refuse callers' attempts to replace
  an existing generation. Store generates/trusts host ids, not a UI or MCP field.
- [x] Test account deletion/recreation, unknown generation from a legacy writer, deliberate unbound workspace vs legacy, revision
  NULL vs 0, and no cascading deletion of historical usage on account removal.
- [x] Update explicit migration-version assertions only; leave managed-ledger behavior unchanged.

**Check:** `go test ./internal/storage/ ./internal/agenthub/ -run 'Usage|Account|Migration|Migrate' -count=1`.

## Task 2 — Capture actual runtime facts at the prompt boundary

**Depends on:** Task 1.

- [x] Add immutable runtime facts beside the existing `sessionAccount` binding. Publish them on initial CreateSession, resume,
  reopened-after-move and rewind's `openForked`; clear/replace them when runtimes close, die or are evicted. Failed opens must
  not publish live facts. Include canonical built-in id/generation even when no account book is configured.
- [x] `invoke` captures a local attribution value after runtime resolution and immediately before the vendor prompt begins.
  Pass it to both successful and metadata-bearing failed `TurnRecord` constructions. Do not read mutable account/workspace state
  again when persisting. Capturing attribution must neither manufacture usage nor create a turn for a refusal that ran nothing.
- [x] Implement a memory-only `ToolUsageScope(sessionID)` (name illustrative) that returns a copied runtime/active-turn snapshot,
  or “unavailable”. The active turn's snapshot takes precedence. No arbitrary session id accepted from a tool argument.
- [x] Keep account-generation provenance from the account actually resolved by `fillAccountSpec`. Missing selected account →
  built-in runtime must be recorded as built-in even if the session row still names the missing added account.
- [x] Test with scripted runtimes: A then switch to B; success and billed failure; disappeared account fallback; ordinary no-policy
  built-in; cancellation without metadata; independent limit follow-up; rename during a turn; delete/recreate same account slug.
  Assert old rows remain unchanged after all later account/session mutations.
- [x] Test scope cleanup/replacement on reload, resume, restart, rewind/fork and dead-runtime eviction. Old socket callbacks must
  not adopt a newly opened runtime's snapshot: bind a runtime generation when creating its observation callback, or otherwise
  detect that the endpoint is stale. Calls in flight keep the snapshot they captured at entry.

**Check:** `go test ./internal/agenthub/ -run 'Attribution|UsageScope|Account|Invoke|Rewind|Resume' -count=1`.

## Task 3 — Extend token aggregation by recorded account

**Depends on:** Tasks 1–2.

- [x] Add id/generation account groups and propagate them through `UsageReport` and its DTO conversion.
- [x] For new context-versioned turns use their captured workspace. For legacy rows preserve Phase 1a's session-workspace fallback
  only in the workspace view; account attribution stays unknown. Document the fallback instead of claiming historical precision.
- [x] Reuse `UsageTotals.add` and `Known` coverage. Include unknown account/generation buckets and deterministic tie ordering.
- [x] Test reconciliation across all account groups for each metric and its coverage, including malformed/unknown usage, genuine
  zeros and helper-model splits. The by-model split remains separate and is never added to the overall/account totals.
- [x] Use account id plus generation as a structured pair, not ambiguous string concatenation in Go or UI keys.

**Check:** `go test ./internal/agenthub/ -run 'BuildUsageReport|TurnUsageRows|ServiceUsageReport|DeriveTurnUsage' -count=1`.

## Task 4 — Build the bounded metrics leaf and durable daily store

**Depends on:** Task 1; may be implemented independently of Tasks 2–3.

- [x] Define small `Scope`, `Observation`, `DailyRow`, `RunHealth` and query types. Use `int64` counters/byte sums and deterministic
  aggregation. Validate counts/byte sizes, string bounds and future profile-key consistency at the host boundary.
- [x] Implement pure aggregation with a fixed key bound and a collector with an injectable clock, ticker and store. No sleeps in
  unit tests. `Record` is memory-only; slow/failing persistence cannot hold up an MCP response or change its return value.
- [x] Implement transactional UPSERT plus the per-run batch watermark, rollback, merge-back, known drop counts and run health.
  Test both definite rollback and an error returned after the transaction actually committed. Avoid exactly-once claims for
  observations across process death: committed batches are durable; active buffered samples are best effort and unclean runs mark gaps.
- [x] A report reads only requested UTC-day/vendor/workspace ranges (default 30 days, max 90) and a bounded top-tool list. Return
  collection start, last flush, drop/failure/unclean status. There is no report-query write or implicit account lookup.
- [x] Test 4,096-key saturation, concurrent calls during a blocked flush, rollback and retry without double counting, shutdown
  while writes fail, reopening after an unclean run, UTC boundaries, sum/max merge rules, and `MaxOpenConns(1)` without deadlock.
- [x] Add an architecture assertion that production `internal/usagemetrics` imports no other in-module package.

**Check:** `go test -race ./internal/usagemetrics/ -count=1` and `go test ./internal/arch/ -count=1`.

## Task 5 — Instrument the existing MCP budget boundary

**Depends on:** Tasks 2 and 4.

- [x] Keep `withResultBudget(ResultBudget)` valid for existing callers/tests. Add a sibling helper or options-based constructor
  with an optional observer; a nil observer preserves today's behavior with negligible extra work.
- [x] Snapshot attribution at call entry; measure response text directly before/after the actual `limitResult` call and emit one
  value-only observation on return. This path also observes uncut, exempt and tool-error results without changing them.
- [x] `sdkServerFor(caller)` captures the identity-bound caller directly for the observer. Do not depend on whether `withCaller`
  happens to wrap this middleware inside or outside. Only host-registered dispatches keep their actual tool names; unknown/malformed
  dispatch errors use a bounded fixed bucket. `tools/list`, notifications and shared-terminal requests emit no Hub measurements.
- [x] Wire observer scope through an injected callback on `mcp.Deps`; never import `storage`, `app` or `bridge` into MCP.
- [x] Add real SDK client/server tests, not just direct handler calls: a multi-byte text cut, structured JSON shrink with removed
  cursor, exempt PDF/readFile, mixed image/text, image-only, structured-only, IsError, Go error, nil/empty result, missing scope,
  unknown tool and two concurrent identity-bound sessions in different workspaces. Verify exactly one observation per call.
- [x] Assert the existing response bytes/content/structured content and public error semantics are identical with collection on
  and off. Include a failing collector and a saturated collector; grants/gates still decide the same calls.
- [x] The current SDK exports no public `GetTool`/registry enumeration method. If a dispatch cannot be proven registered from its
  result shape, record it in the fixed dispatch-error bucket (including handler errors with no result). Do not invent an SDK API,
  call `tools/list` on the hot path, or retain unbounded caller-selected names merely to separate these errors.
- [x] Benchmark a small result, a large text cut and an exempt image result. Record observer overhead; do not marshal/copy full
  results for measurement and do not start a goroutine per call.

**Check:** `go test ./internal/mcp/ -run 'Budget|Metrics|Caller|SessionHost' -count=1` and the existing result-budget tests.

## Task 6 — Read and correlate account-move observations conservatively

**Depends on:** Tasks 1–2.

- [x] Extend host-authored move notices with optional resolved target generation and runtime-opened status. Stamp these only in
  the trusted host path after resolving the runtime; ignore matching fields in vendor-supplied update metadata. Keep existing
  notice kind/text and selection/failover behavior intact. Do not persist vendor error/reason text for the measurement report.
- [x] Add a focused optional SQL-store interface for projected, paginated notice fields. Use `CreatedAt` (not `UpdatedAt`) and
  preserve transcript row id/seq as provenance. Test repeated notices remain distinct. Do not call `ReadTranscript` to load bodies.
- [x] Query new turn attribution/start times for the affected sessions/window using a bounded query per page, rather than one
  query per notice. Read only accounting JSON needed by `DeriveTurnUsage`, not messages. Validate window/limit/cursor on the service.
- [x] Implement pure correlation and statuses: confirmed+matched, confirmed without accounting, failed/undone, binding-only,
  legacy-unverified and ambiguous. A NULL start or generation cannot become a confirmed match. Use coverage for cache writes.
- [x] Test a long A turn ending after the B notice, two moves before any recorded usage, switch-back, another session with the same
  account, restart, missing/zero cache write, failed reopen with successful/failed rollback, tied timestamps, deleted/recreated id,
  damaged JSON and a forged vendor `_meta.devkit` flag. Avoid silently upgrading legacy notices to confirmed events.
- [x] Test filter-bound pagination and deterministic ordering; show an empty next cursor at end and never silently drop a page.

**Check:** `go test ./internal/agenthub/ -run 'AccountMoveUsage|AccountSwitch|Transcript|Attribution' -count=1`.

## Task 7 — Wire collector lifetime, guarded report services and bindings

**Depends on:** Tasks 3–6.

- [x] In `main.go`, instantiate the metrics SQL adapter and collector after migration, before accepting MCP traffic. Inject its
  reader into Agent Hub and its value-only observer/scope adapter into MCP. Constructors/tests without it remain valid.
- [x] On shutdown stop new MCP accepts and wait for in-flight observations with a bound before final metrics flush. Preserve the
  existing Agent Hub transcript flush and cancellation semantics. Keep the DB open until both flush paths finish; handle early
  startup failure with cleanup. Reuse the existing MCP context where appropriate; avoid a leaked background worker.
- [x] Add UI-only `Service.ToolUsageReport` / `Service.AccountMoveUsageReport`, corresponding bridge DTOs/methods and the account
  generation field. Existing `AgentUsageReport` gains additive `byAccount` only. Publish safe query/unavailable errors and add
  method names to sorted `legacy_ops_methods.json`. The existing `usage:*` read grant should cover the new scopes; test it rather
  than widening permissions or adding an execute grant.
- [x] Tests use the real gateway: UI succeeds, agent/terminal/remote-human callers fail at both bridge and service, invalid vendor
  or query is refused, collection-disabled is distinguishable from an empty report, every list is `[]`, and account DTOs expose
  no home/credential. Verify no new MCP tool touches accounts or report reads.
- [x] Test in server mode: historical token/account reports use pure normalization; spawning/sign-in remain refused. No unavailable
  adapter may turn existing usage into zeros or perform a live provider request for this page.
- [x] Regenerate `wails3 generate bindings -clean=true -ts -i`; review the generated diff for additive methods/DTO fields only.

**Checks:** bridge/app/architecture/MCP suites; generated frontend typecheck.

## Task 8 — Add account, tool-size and move tables to the existing page

**Depends on:** Task 7.

- [x] Extend typed wrappers and report fixtures. Older/nullable generated lists become `[]`; absent attribution is unknown, not
  built-in. Preserve Phase 1a's coverage rules and fixed warning that tokens do not measure subscription quota.
- [x] Add a “By account” token table. Resolve labels using id **and** generation; display unknown, historical/removed and loading
  cases deliberately. A failed account-name query must not call all accounts removed. No provider identity is inferred.
- [x] Add a separate “DevKit MCP text results” table in the existing usage panel: tool, calls, result/text coverage, before/after
  text bytes, byte reduction, max size and cut rate. State the UTC window, scope exclusions and collection-health information.
  Use bytes/KiB/MiB, not token units. Maxima and sums are not p50/p90; do not manufacture percentiles from daily aggregates.
- [x] Add the paginated move table with UTC time, from/to, confirmation status, first recorded-turn time and cache-write value
  with coverage. Show unknowns as dashes with a reason/status and the non-causal wording above. Limit the date window and retain
  a visible filter state; changing vendor/window resets pagination and discards late replies for the old filters.
- [x] Extend query keys with vendor/window/cursor and explicitly enable stale focus refresh as in Phase 1a. Fetch account names
  with the existing account hook; do not trigger refresh-status/sign-in or quota probes. Give each report an independent error
  state so one failure does not hide token totals. Do not poll or flush synchronously on focus.
- [x] Use the shared `DataTable`; do not add raw unvirtualized tables or bypass smoothness/performance ratchets. Test narrow panels,
  long historical labels, no data, partial data, real zero, drops/unclean recovery, paginated end and one failed subreport.
- [x] Add hook tests with real QueryClient defaults for focus refresh, filter separation and stale response isolation; render tests
  for account-id reuse and model metrics staying unknown. Add Settings search keywords only if the existing ones do not find it.

**Check:** focused Bun tests, `bun test --isolate`, `bun run build`; browser preview at wide and narrow panel widths.

## Task 9 — Reconcile, document and verify

**Depends on:** Tasks 0–8.

- [x] On a temporary backup of a real database, upgrade and compare Phase 1a totals/coverage before and after. Existing turns stay
  in the unknown-account bucket. Never migrate/write the live database for a test, log raw metadata, or commit the copied DB.
- [x] Exercise a synthetic real two-account session stack with known switch and result fixtures. Compare recorded account ids,
  generations, byte counts and move correlation with independent expected values; failure paths must preserve the same outcome.
- [x] Exercise the collector under a blocked SQLite writer and under forced process exit on a disposable DB. After restart, durable
  counters remain and unclean collection is visible; do not claim recovery of samples that were only buffered.
- [x] Recheck fixtures and generated DTO mappings for new required account fields; update only the affected callers. No real account
  login, exhaustion or expensive provider traffic is required for acceptance.
- [x] Update `docs/agent-reference/{agent-hub-accounts,mcp-tools}.md`, spec delivery/status and this plan's execution notes with what
  actually passed. Explicitly record if the native desktop page was not exercised. No automatic budget/failover tuning is enabled.

```bash
gofmt -l internal/usagemetrics internal/agenthub internal/mcp internal/bridge internal/app internal/storage main.go
go vet ./internal/usagemetrics/ ./internal/agenthub/ ./internal/mcp/ ./internal/bridge/ ./internal/app/ ./internal/storage/
go test ./internal/usagemetrics/ ./internal/storage/ ./internal/agenthub/ ./internal/mcp/ ./internal/bridge/ ./internal/app/ ./internal/arch/ -count=1
go test -race ./internal/usagemetrics/ -count=1
cd frontend && bun test --isolate && bun run build
```

Run from the root separately after the frontend build: `go build -tags server -o bin/dev-kit-server .` and `git diff --check`.
Existing unrelated formatting debt is not permission to reformat whole packages. Keep the changes reviewable.

## Acceptance gates

| Gate | Evidence required |
| --- | --- |
| Historical correctness | New turns use actual runtime account/generation; legacy stays unknown; account totals and coverage reconcile. |
| Stable identity | Rename preserves generation; delete/recreate of the same slug does not acquire the previous account's usage/name. |
| Measurement correctness | SDK-path tests show one observation at the real cut boundary, UTF-8 byte counts and unchanged tool results/gates. |
| Bounded overhead | Fixed buffers, one worker, no hot-path DB I/O, no per-call goroutines; slow writer and saturation tests. |
| Honest durability | Committed batch not doubled, drops/gaps visible, graceful flush bounded, unclean recovery marked. |
| Conservative moves | Started-at and account-generation checks; binding-only/legacy/ambiguous events never become a causal cache-cost claim. |
| Privacy and authorization | Numbers/ids/times/enums only; no result/argument content; bridge+service guards; no MCP reporting/account tool. |
| Delivery | Focus refresh, independent errors, shared virtualized tables, additive bindings, Go/architecture/Bun/build checks. |

## What follows

Phase 2a adds UI-owned workspace harness profiles and snapshots. It supplies real profile/override revision values to these
already nullable fields and dimensions, without rewriting earlier `none` observations. Tool-group filtering must ship together
with the prompt safety/guidance split; agent proposals, executable/path proposals and mid-session grants follow their vendor spikes.
Actual cache-cost analysis and changes to `accountFlipHold` remain Phase 4 work, based on data rather than this plan's counters.

## Execution notes

Implemented on `feat/harness-usage-metrics`, from merged main `326140ff`; the plan was committed first as `7f49697e`.
Migration 51 adds nullable attribution, stable account generations and daily metrics/run watermarks. Runtime facts publish only
after a successful open and are captured before each prompt; close/replacement invalidates the endpoint token. The two recorded
turn paths carry the same attribution. The result observer wraps the existing cut boundary without storing payloads. Reports are
window-only; profile revisions stay `NULL`. There is no account-selection or result-budget policy change. Existing account reservations still guard removal while a runtime opens; measurement publication is separate. Submillisecond candidate ordering stays ambiguous because SQLite timestamp ordering cannot prove which turn was first.

Validation on 2026-10-26:

- Baseline affected Go packages and the original 5,041 frontend tests passed. The completed frontend suite passed **5,050 tests**;
  typecheck, production build and performance budget passed (initial JS 1,241.7 KiB, CSS 282.8 KiB).
- Affected Go packages (`usagemetrics`, `storage`, `agenthub`, `mcp`, `bridge`, `app`, `arch`) passed. Additional tests cover account
  generation reuse, captured vs legacy workspace, a real Service two-account move, successful/billed failed turn attribution,
  endpoint replacement, preservation of the opening-account removal guard, host-only notice provenance, SQL pagination/correlation, real MCP SDK text/structured/mixed/error paths,
  committed-but-lost-reply retry, 4,096-key saturation, concurrent observations, a blocked SQLite writer, shutdown and subprocess exit.
  Race testing of `usagemetrics` and scoped `go vet` passed. Bindings were regenerated; server-mode binary compilation passed.
- A read-only SQLite backup of the live schema-50 database was migrated in a temporary directory and then deleted. All Phase 1a
  aggregates and coverage were identical before/after: **516 Claude turns**, **962,611,929 total tokens**; all 516 legacy turns stayed
  in the unknown-account bucket. The live database was not migrated or written by this check. No copied database was committed.
- Full `go test ./internal/... ./cmd/... -count=1` passed the affected packages but has unrelated failures in `internal/codegraph`
  (Java analyzer JAR older than source), `internal/crossservice` (analyzer not configured), and `internal/git`
  (`TestResolveSideTakesTheStageExactly`, CRLF normalization). Those paths have no diff from the base. This is not a claim of a
  completely green repository-wide suite.
- Browser fixture preview checked partial coverage, a removed generation, mixed results, known-zero cache writes, unverified
  moves and collection gaps at default, 1,200 px and 420 px widths. DOM page width stayed 420 px at the narrow setting; tables use
  their own horizontal scrollers. Temporary preview files were removed. Native desktop, live provider traffic and real sign-in/
  exhaustion were not exercised. Provider-gated tests keep their normal skip conditions.
- The middleware benchmark (100 ms samples; wrapper callback only, not end-to-end vendor latency) showed small result
  299.5/295.2 ns, large cut 32.7/36.1 µs, exempt image 431.2/439.5 ns for observer off/on respectively. Allocations were unchanged
  (3/10/6); these short samples are descriptive, not a latency guarantee. Tests inject clock/ticks or drive flushes directly.

The checkbox list records implementation and automated/fixture verification; it does not claim native/provider validation.
