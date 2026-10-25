# Managed team implementation plan

Date: 2026-10-25. Status: phased implementation plan based on [managed team contract](../specs/2026-10-25-managed-team-contract.md). Local tracking references: `OPT-76c3b6` (TEAM1 contract), `OPT-12698e` (ledger), and `OPT-400ff3` (baseline); references are local only and no board update/MCP mutation is part of this plan. This plan does not claim later batches are implemented or verified. The current deliverable is the metadata-only ledger foundation in `internal/storage` and migration 49; shared DTOs, state validation, and requirements digest live in `internal/agentwork`. The validation record below covers this batch only.

The labels below are **implementation batches**, not the TEAM roadmap phases 0–4 in the orchestration design. Mapping: batches 1–2 support roadmap phase 1 (contracted assignment); batches 3–5 cover roadmap phases 1–2 (context/result, recovery, UI and batching); batch 6 covers roadmap phases 3–4 (review/integration and handoff/effectiveness). Roadmap phase 0 (measurement correctness) is tracked separately in team foundations.

## Guardrails for every batch

- Keep `internal/agentwork` a stdlib-only leaf. Reuse the existing B7 handoff service and B5 durable peer delivery substrate; do not add a runtime, task database, generic scheduler, or duplicate outbox.
- Tasks owns task records/status and terminal transition gates. Agent Hub owns session lifecycle, prompt delivery, stops and interactions. Storage owns persistence. Composition root injects adapters. Existing gateway capability/scope checks remain authoritative.
- Scope every operation to the authenticated caller, workspace, board, parent task, task, and session. Keep payloads out of migration-49 metadata. Do not claim vault encryption for vendor transcripts/source files.
- D1 approval remains exact `db.query`; other gated actions use their existing gate, with production/unknown environment confirmation winning. Never grant authority from a group flag or UI field.
- Ambiguous prompt send is `interrupted_unknown`: reconcile, do not replay. Lease expiry is not proof a writer stopped. Stale revisions/fencing generations cannot change current state.
- Verification lists are requirements for each batch. Only the final validation record states checks actually run.

## Batch 1 — Metadata ledger and migration 49 (current foundation)

**Deliverable:** storage persistence methods `CreateWorkGroup`, `UpdateGroupPolicy` CAS, `CreateWorkItem`, `RefreshWorkItemRequirements` CAS, `BeginAttempt` CAS, `TransitionAttempt` CAS, bounded list pages, and per-attempt `MarkAttemptAmbiguous` CAS; shared DTOs/state validation/digest are in `internal/agentwork`. Migration 49 creates three local metadata tables (`agent_work_groups`, `agent_work_items`, `agent_work_attempts`) with composite scope foreign keys. Groups start disabled and paused. Persist explicit task/requirements revisions and attempt generation. Store no prompt, evidence, path, artifact body, secret, or transcript fields. This is storage plumbing only; there is no UI, dispatch, retry, release, or reassignment API.

**Verification required before merge:**

1. Fresh DB, upgrade from migration 48, migration 49 idempotence, and restart persistence tests; group/item/attempt scope FK rejection across workspace/board/parent/task; reject a current-attempt pointer to another item/scope; confirm parent deletion is restricted while child history exists.
2. CAS race tests for group policy, item/attempt transitions, two simultaneous `BeginAttempt` calls (only one reservation wins on the app’s single shared database handle), and stale expected revision. Confirm failed CAS causes no partial write.
3. State-machine tests reject illegal transitions, terminal reopen, and generic `accepted`; stale attempt/item pointer and revision checks have no state effect. Verify that all attempt outcomes leave the current pointer pinned and `BeginAttempt` cannot retry/reassign it in this foundation. End-to-end generation fencing is a later-phase execution requirement because migration 49 is metadata-only.
4. Storage recovery tests call `MarkAttemptAmbiguous(ctx, attemptID, expectedRevision)` explicitly; integration with host liveness reconciliation remains a later-batch requirement. It CAS-marks only that selected `prompt_enqueued`/`running` attempt as `interrupted_unknown`, preserves ownership/scope, and does not invoke delivery or create a child. Calling it for another state, stale revision, or missing ID fails without modifying other attempts. App startup alone does not mutate attempts or dispatch.
5. Validation: `go test ./internal/agentwork ./internal/storage ./internal/arch/ -count=1`; `go vet ./internal/agentwork ./internal/storage`; `go test ./internal/... ./cmd/...` for the repository Go gate when environment supports it. Confirm `internal/agentwork` imports only stdlib and schema has no body/path columns.

**Exit:** ledger survives restart; races/stale state are rejected; ambiguous work is quarantined without replay; architecture boundary stays green. No UI or autonomous workflow claim is implied.

## Batch 2 — Host service adapters and opt-in configuration

**Deliverable:** composition-root adapter that derives identity and scope from authenticated host context; creates group against an existing workspace/board/parent task; updates policy with CAS; exposes bounded, metadata-only reads. Add custom board-status mapping by board status IDs/categories, with unknown/deleted mapping surfaced as configuration-needed. Group creation remains disabled and paused until an explicit authorized user action.

**Verification required:**

1. Adapter tests prove wrong caller, workspace, board, parent, task, or session scope is denied and UI-supplied role/coordinator/path/grant values are ignored.
2. Custom workflow tests use nonstandard status names and changed/deleted IDs; no literal `todo`/`review`/`done` assumption, and unmapped terminal status never becomes accepted.
3. Authorization tests exercise real capability grants, loopback/server-mode restrictions, missing confirmation handler, production and unknown environment tags, and exact D1 `db.query` scope; no other operation inherits that approval.
4. Add/publish any new public error codes through `bridge.keptCodes` or document intentional redaction. `go test ./internal/arch/` remains green.

**Exit:** caller-scoped API reads/writes metadata only; no dispatch is possible while disabled/paused; board mapping cannot bypass the managed acceptance policy.

## Batch 3 — Context/result package and artifact references

**Deliverable:** immutable versioned context package and result envelope with opaque IDs, task/requirements revision, attempt/generation, producer, provenance and evidence references. Host bounds and validates content. If payload persistence is introduced, use vault codec with associated-data binding; artifact content is immutable and referenced through opaque IDs. Keep large output in existing artifact/document mechanisms until quotas are implemented.

**Verification required:**

1. Schema/size tests for missing fields, oversized UTF-8, excess references, duplicate IDs, invalid outcomes, wrong producer, stale revision and cross-scope references. Requirements digest tests bind task requirements, role contract, scope, dependency IDs, and each dependency's validated requirements/result revision; missing, duplicate, or mismatched `DependencyInputs` must fail.
2. Provenance tests distinguish agent claims, host-run verify, reviewer verdict and CI; head/tree changes stale the verdict/evidence. Dirty worktree without host digest cannot be host-verified.
3. Vault tests show locked vault blocks payload read/dispatch; associated-data or scope mismatch fails; plaintext is absent from DB, logs, audit metadata, errors, tool descriptions and event deltas.
4. Quota/retention tests enforce 10 MiB/item, 50 MiB/group, 100 MiB/workspace; summary/brief/reference caps; reject rather than truncate; active/pending/referenced data is never removed; expired terminal body cleanup preserves metadata/hash audit.
5. Path traversal and arbitrary path inputs are rejected; every artifact lookup rechecks workspace/group/task/attempt scope.

**Exit:** results are revision-bound and auditable; artifacts are encrypted/scoped/quota-limited; no green result from a different revision can be accepted.

## Batch 4 — Dispatch integration and managed recovery

**Deliverable:** expand/reuse B7 handoff saga with stable operation ID and persisted checkpoints; use existing Agent Hub session and B5 delivery/outbox APIs. Add managed result/event consumer with independent type/dedupe/watermark, without replacing peer delivery. No auto-reassign on lease expiry.

**Verification required:**

1. Fault injection before/after reserve, session creation, assignment, enqueue and receipt. Retry reconciles the same operation and never creates a duplicate child.
2. Crash after possible prompt send yields an unresolved candidate; only host session/turn reconciliation can call per-attempt `MarkAttemptAmbiguous`, and no automatic resend occurs. Live old writer keeps ownership. A later retry API must be separately designed and require confirmed quiescence; it is not in migration 49.
3. Lock vault, pause group, pending user prompt/queue, pending permission/elicitation, closed session, production confirmation and unknown post-restart interaction state; each blocks dispatch/wake with a durable reason.
4. Confirm typed vendor failure/stop and usage-limit signals use current Agent Hub stop/account policy; do not retry a user refusal or rotate accounts beyond configured policy.
5. Dedupe/reordering tests prove only acknowledged prompt-included events advance watermark; progress-only events do not wake coordinator; events arriving during a turn remain for a later batch.

**Exit:** no duplicate child, no ambiguous replay, no stale result application, no bypass of existing gate/session lifecycle.

## Batch 5 — Team/Timeline UI and actionable batching

**Deliverable:** one Agent Hub group document with Team/Timeline modes and scoped read model. Sidebar stays `ModuleManifest.sidebar`; tabs/focus/split/pin/close use workbench; stable `groupId` params. Close tab does not stop work. Timeline uses persisted intervals/events; summary does not load transcripts or call a model. Add digest batching only after pause/queue/interaction guards are proven.

**Verification required:**

1. `cd frontend && bun test --isolate` and `cd frontend && bun run build`; shell checks for one sidebar/tab strip, stable identity, correct focused group, hidden-dialog visibility, stale selection, pagination and close/reopen lifecycle.
2. UI regression/gallery cases cover empty/loading/locked/denied/stale/offline, many items, custom statuses, pending permission/question, two attempts, reconnect/gap, split groups, keyboard, accessibility, light/dark and narrow layout.
3. Wake tests: progress-only gives zero coordinator calls; one completion batch gives at most one call; a user prompt or queue pause blocks auto-wake; no event is lost across restart or stream gap.
4. Open task/session/evidence actions target the selected stable ID through existing document helpers. UI cannot answer a prior permission/question or directly transition a task to accepted.
5. Native/live vendor smoke is separately labeled; a browser gallery or mock does not count as native, recovery, or vendor verification.

**Exit:** dashboard derives state from host read model, uses shared shell, and adds no model calls for visualization. Batching does not change manual peer behavior.

## Batch 6 — Review, integration, handoff, and effectiveness

**Deliverable:** separate reviewer/integrator work items, bounded review loops, verified-target binding, coordinator checkpoint transfer and capacity tuning. Keep review/acceptance and push/merge under existing gates. Any full-team effectiveness claim needs comparative evidence.

**Verification required:**

1. Review binds to requirements revision + exact head/tree digest; head change stales verdict. Host verification runs only configured commands on configured snapshots/worktrees, never agent-selected paths/commands.
2. Integration head gets its own verification; board cwd mismatch is visible and cannot be called worker-branch verification. Terminal task transition goes through Tasks policy plus existing gate.
3. Coordinator transfer preserves decisions, blockers, requirements revision and evidence references, opens a new session via existing Agent Hub document helper, and does not resurrect old pending asks.
4. Fixed workload comparison includes read-only research, independent patches, multi-branch integration, review rework, restart and long idle. Report coordinator context p50/p95/max, wakeups, full-group usage and unknown coverage, quality/rework, recovery ambiguity, and user wait time. Do not claim savings without comparable baseline and quality evidence.

**Exit:** revision-safe integrated result; all required gates and recovery cases pass; observed full-group quality/cost support any performance claim. Otherwise ship as an unmeasured workflow and report that limitation.


## Batch 1 validation and delivery record — 2026-10-25

Implemented with Luna subagents and parent review on `codex/team-managed`, starting from main `85a06259`. Local tracking: `OPT-76c3b6` contract, `OPT-12698e` ledger foundation, `OPT-400ff3` baseline tooling. DevKit MCP reads and task transitions returned `Transport closed`; no task was claimed, moved, or closed, and the app database was not edited to bypass its service. These are local delivery references, not accepted board-task statuses.

- `TMPDIR=/private/tmp go test ./internal/... ./cmd/...` passed all **69 packages**.
- `TMPDIR=/private/tmp go test -race ./internal/agentwork ./internal/storage ./cmd/devkit-team-baseline -count=1` passed, including fresh DB, upgrade from schema 48, idempotent migration, restart, CAS, scope/FK, stale requirements, bounded pagination, explicit ambiguity, and real normalized-usage serialization regressions.
- `TMPDIR=/private/tmp go vet ./internal/agentwork ./internal/storage ./cmd/devkit-team-baseline` passed.
- `TMPDIR=/private/tmp GOTOOLCHAIN=go1.27.1 golangci-lint run --new-from-rev=HEAD ./internal/... ./cmd/...` passed with **0 issues** against the staged changes before commit. Architecture tests passed as part of the full Go gate.
- `cd frontend && bun install --frozen-lockfile && bun run build` passed, including TypeScript and the performance budget. Build assets were needed by the Go embed; no frontend source or bindings changed.

The baseline CLI opens an explicit existing DB with SQLite `mode=ro`, emits allowlisted aggregate counters, and keeps unavailable metrics unknown. Fixtures use `json.Marshal(agenthub.NormalizedUsage)` and assert both embedded `RawMeta` and the separate `raw_meta` column stay out of the report. A read-only check for the earlier greeting session ID found no matching session in the current DB; it is not a new context/usage measurement. No vendor was invoked and no token saving is claimed.

Only internal metadata persistence and offline diagnostics are delivered. No group host API, leases/writer quiescence, encrypted context/artifact store, dispatch scheduler, result acceptance, Team/Timeline UI, or native/live pilot is included. Attempt pointers remain pinned; retry/release is intentionally unavailable until host reconciliation exists. `OPT-400ff3` still needs a comparable clean-session pilot for measurement acceptance.
