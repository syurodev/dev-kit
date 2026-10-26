# Managed team implementation plan

Date: 2026-10-25. Status: phased implementation plan based on [managed team contract](../specs/2026-10-25-managed-team-contract.md). Local tracking references: `OPT-76c3b6` (TEAM1 contract), `OPT-12698e` (ledger), and `OPT-400ff3` (baseline); task progress is recorded through the DevKit UI when its MCP transport is unavailable. This plan does not claim later batches are implemented or verified. The current deliverable is the metadata-only ledger foundation in `internal/storage` and migration 49; shared DTOs, state validation, and requirements digest live in `internal/agentwork`. The validation record below covers this batch only.

The labels below are **implementation batches**, not the TEAM roadmap phases 0–4 in the orchestration design. Mapping: batches 1–2 support roadmap phase 1 (contracted assignment); batches 3–5 cover roadmap phases 1–2 (context/result, recovery, UI and batching); batch 6 covers roadmap phases 3–4 (review/integration and handoff/effectiveness). Roadmap phase 0 (measurement correctness) is tracked separately in team foundations.

## Guardrails for every batch

- Keep `internal/agentwork` a stdlib-only leaf. Reuse the existing B7 handoff service and B5 durable peer delivery substrate; do not add a runtime, task database, generic scheduler, or duplicate outbox.
- Tasks owns task records/status and terminal transition gates. Agent Hub owns session lifecycle, prompt delivery, stops and interactions. Storage owns persistence. Composition root injects adapters. Existing gateway capability/scope checks remain authoritative.
- Scope every operation to the authenticated caller, workspace, board, parent task, task, and session. Keep payloads out of migration-49 metadata. Do not claim vault encryption for vendor transcripts/source files.
- D1 approval remains exact `db.query`; other gated actions use their existing gate, with production/unknown environment confirmation winning. Never grant authority from a group flag or UI field.
- Ambiguous prompt send is `interrupted_unknown`: reconcile, do not replay. Lease expiry is not proof a writer stopped. Stale revisions/fencing generations cannot change current state.
- Verification lists are requirements for each batch. Only the final validation record states checks actually run.

## Batch 1 — Metadata ledger and migration 49 (current foundation)

**Deliverable:** storage persistence methods `CreateWorkGroup`, `UpdateGroupPolicy` CAS, `CreateWorkItem`, `RefreshWorkItemRequirements` CAS, `BeginAttempt` CAS, `TransitionAttempt` CAS, bounded list pages, and per-attempt `MarkAttemptAmbiguous` CAS; shared DTOs/state validation/digest are in `internal/agentwork`. Migration 49 creates three local metadata tables (`agent_work_groups`, `agent_work_items`, `agent_work_attempts`) with composite scope foreign keys. Groups start disabled and paused. Persist explicit task/requirements revisions and attempt generation. The requirements digest is v2 over host-resolved workspace/board/task identity, description, sorted blockedBy IDs, folder scope and branch. It excludes title, progress and dependency revision/result bindings; prior v1 values require reconciliation. Store no prompt, evidence, path, artifact body, secret, or transcript fields. This is storage plumbing only; there is no UI, dispatch, retry, release, or reassignment API.

**Verification required before merge:**

1. Fresh DB, upgrade from migration 48, migration 49 idempotence, and restart persistence tests; group/item/attempt scope FK rejection across workspace/board/parent/task; reject a current-attempt pointer to another item/scope; confirm parent deletion is restricted while child history exists. Digest tests cover identity, description, sorted dependencies, folder scope and branch; title/progress are ignored and input slices are not mutated.
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

1. Schema/size tests for missing fields, oversized UTF-8, excess references, duplicate IDs, invalid outcomes, wrong producer, stale revision and cross-scope references. Requirements digest remains v2 as specified: description, sorted blockedBy IDs, folder scope, branch and defensive workspace/board/task identity. Role contract and dependency requirements/result revision bindings are later immutable context/result inputs and are not part of this digest.
2. Provenance tests distinguish agent claims, host-run verify, reviewer verdict and CI; head/tree changes stale the verdict/evidence. Dirty worktree without host digest cannot be host-verified.
3. Vault tests show locked vault blocks payload read/dispatch; associated-data or scope mismatch fails; plaintext is absent from DB, logs, audit metadata, errors, tool descriptions and event deltas.
4. A later payload phase must define and approve per-artifact/body limits before adding writes; the earlier draft's 10 MiB/artifact and summary/brief/reference caps are proposals, not TEAM1 decisions. The approved 30-day/200 MB per-group values govern manual cleanup of ended groups, not write quotas. Preserve active or referenced data; no workspace quota or automatic cleanup is specified.
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

Only internal metadata persistence and offline diagnostics are delivered. No group host API, leases/writer quiescence, encrypted context/artifact store, dispatch scheduler, result acceptance, Team/Timeline UI, or native/live pilot is included. Attempt pointers remain pinned; retry/release is intentionally unavailable until host reconciliation exists. At PR333 implementation time, this batch supplied only baseline tooling. Subsequent board review found `OPT-400ff3` already accepted from another session: fresh Codex measurements plus existing Claude data, with OpenCode excluded by the person and tracked in issue #331. Preserve that Done status; do not repeat model runs merely to exercise the CLI.

## Batch 2 delivery record — 2026-10-25

Tasks: `OPT-76c3b6` (contract reconciliation) and `OPT-12698e` (host-backed ledger). Branch: `codex/team-host`, based on merged main `09463fba`. The source spec on `opt/team1-contracts` is preserved; `2026-10-25-team1-contracts.md` is the reconciled canonical policy/interface proposal and the smaller managed-team contract describes the delivered foundation. Approved policy wins over conflicting earlier draft defaults. Payload sizes, public `team.*` schemas and later workflow features remain proposals.

Delivered:

- `agentwork.ManagedService` uses injected host/store interfaces; real `app.WireManagedTeams` composition uses SQL storage, Tasks, vault, Agent Hub session metadata, workspace registry and existing folder grants. There is no alternate vendor runner or startup model call.
- Migration 50 adds explicit status mappings, independent group/item/attempt schema versions, tombstones and unique live task slots. Legacy rows lacking a map start disabled/paused. Unknown versions fail closed. Tombstones are fixed codes, with no task bodies, prompts, paths or free-form reasons persisted in the ledger.
- Requirements digest v2 follows the approved description/dependencies/folder-and-branch scope contract, binding host identity. Title, progress, comments, status and result evidence do not change it. The stored task counter advances only on a requirements-digest change; task `updatedAt` is not used as a revision token. Old v1 snapshots require reconciliation.
- Current UI identity and workspace scope gate group reads/writes; vault lock blocks task-body/digest reads but allows scoped metadata reads. Explicit opt-in metadata needs a non-headless UI and the exact managed terminal policy installed. Enabling records intent only; no scheduler is connected.
- Board status mapping is explicit and category-validated, supports custom IDs and an explicitly shared active column, and never turns board progress or terminal status into execution/acceptance evidence. Only confirmed repository absence may create a deletion tombstone; transient reads and a missing session host fail without changing the ledger. Deletion/terminal sync creates retained tombstones; old attempts keep their task slot until future confirmed quiescence/release. A human can end metadata while locked or after operational configuration breaks, without releasing a pinned writer.
- A Tasks-declared terminal-policy seam blocks managed agent closure through ordinary transition, confirmation completion and asynchronous verifier completion. Lookup includes parent tasks and historical membership across workspace rebinds. Human task moves keep their current path and are never labeled as passing managed review. The public refusal is `tasks.managedTerminalNotReady`.

Validation:

- `TMPDIR=/private/tmp go test ./internal/... ./cmd/...`: all **69 packages** passed.
- Domain/storage and app/tasks suites passed under `-race`. Final recovery/session-host and verifier test synchronization changes were additionally checked with focused race runs; command success is not treated as completion of the post-verify policy check.
- Scoped `go vet`, architecture tests, and the root composition check `TMPDIR=/private/tmp go test -tags server .` passed.
- Golangci-lint on Go 1.27.1 passed with **0 issues** against staged changes. Frontend TypeScript/Vite/performance build passed; no frontend source or AppService method changed, so bindings are unchanged.

Not delivered in this batch: public managed APIs/MCP capabilities or UI controls, writer leases/quiescence/reassignment, encrypted packages/artifacts, managed dispatch/event batching, review/evidence/acceptance flow, or Team/Timeline UI. The terminal seam is a conservative refusal, not completion of `OPT-f3831a`. Async verification still has the existing source-status semantics; this batch adds current scope/target/policy rechecks, not a new verified-target or source-status CAS protocol. No native managed UI or live model smoke was run and no token savings are claimed.

DevKit MCP returned `Transport closed`; branch/progress comments were recorded through the normal UI. External Luna agents are not DevKit sessions, so no session IDs or task claims were fabricated. Task closure remains subject to board acceptance after review/merge.


## Batch 3 delivery scope — 2026-10-26

Tasks: `OPT-b7e5f6` (TEAM-A1 ownership) and `OPT-2e96ed` (context/artifact backend). Branch: `codex/team-ownership`, integrated with main after PR334 and the account-usage migrations. Existing migration 51 remains unchanged; migrations 52 and 53 add writer ownership and encrypted payload records. Work used external Codex Luna subagents; they are not DevKit sessions and no task-session claim is invented.

The ownership foundation supplies atomic task/session/worktree claims, durable generations, CAS heartbeats, and pre-invoke/pre-steer/pre-resume guards on the existing Agent Hub runtime. The resource key comes from the canonical checkout root and per-worktree Git directory: symlinks and nested folders share a resource, while linked worktrees are distinct. Repository branch guards still resolve the shared policy repository. Broad extra-write grants outside the checkout are refused. Expiry fences new turns without releasing any writer slot; historical managed sessions remain fenced. Lease lookup rechecks the current attempt and requirements/task revisions.

**Production release and reassignment remain unavailable.** The current ACP close path does not prove that the writer and descendants stopped or became isolated. The production proof adapter therefore returns unknown and release fails closed; idle state, elapsed TTL, a missing runtime entry and cancellation are not release evidence. Positive release tests use an injected host proof and do not demonstrate a working production proof path. TEAM-A1 stays open until that host capability is implemented and verified. No background heartbeat loop, dispatcher or model invocation is introduced by this batch. Pause blocks subsequent authorization checks; it does not atomically stop an already-authorized or running turn. A runtime start/pause barrier and stop/isolation proof remain prerequisites for stronger scheduler semantics.

Context packages are immutable encrypted snapshots built before lease acquisition. Artifacts require the exact active lease generation and retain `agent_claim` provenance. Bodies use the existing vault AEAD with authenticated scope, attempt/generation and artifact classification; sensitive bodies and paths do not enter ledger metadata. Reads and lists are bounded. Artifact writes recheck current scope, group state and lease in the insertion transaction. Migration and regression tests cover expired/stale owners, concurrent claims, restart retention, current requirements, paused groups, cross-scope decryption, metadata tampering and bounded reads.

Implementation bounds are 64 KiB for packages, at most 1,000,000 plaintext bytes per artifact, 64 KiB per artifact read and 100 records per list. These are conservative implementation limits within the existing global crypto ceiling, not a newly approved retention or quota policy. The approved retention proposal remains manual cleanup of ended groups, initially 30 days / 200 MB; cleanup enforcement is not delivered here.

There are no public managed AppService/MCP methods, scheduler, automatic task completion, review/acceptance workflow or Team/Timeline UI in this batch. Context selection currently uses the task and role contract, with no selected decision/dependency-reference population yet. The future dispatcher must revalidate current requirements at use time. No native/live model pilot or measured token-saving claim is included.


Validation record: the integrated main snapshot passed `go test ./internal/... ./cmd/...` across 70 packages, followed by affected domain/storage/app/bridge/architecture suites after review fixes. Ownership/artifact suites and Agent Hub/Git guard checks passed under the race detector; the final physical-containment change was checked with focused app race tests. Scoped `go vet`, root server composition tests, frontend TypeScript/Vite/performance build and golangci-lint (0 issues, Go 1.27.1, diff against origin/main) passed. Upgrade coverage includes schema 48/49 and 51 to 53, preserving existing usage attribution. No frontend source or AppService method changed in this batch, so bindings did not change. The normal board was updated through DevKit MCP after connectivity recovered; both tasks remain open pending review and the explicitly outstanding host proof.

## Batch 4 — writer stop proof and admission barrier (2026-10-26)

Slice B of [the harness integration plan](2026-10-26-team-harness-integration-implementation.md). Branch `team/writer-stop-proof`; `OPT-b7e5f6` stays In progress and is not closed.

Delivered: `sandbox.Proc` exit observation (`Exited`/`ExitedAt`) and a backend containment flag (bwrap only); `acpRuntime.Close` reaps what it kills; `agenthub.Service.WriterStopProof` over every runtime incarnation opened in this process (a failed open or a runtime without a witness makes it unknown); `managedOwnershipHost.CheckManagedWriterQuiescence` positive only on that proof, for a lease created after the process started; an admission barrier (`enterWriterAdmission`/`DrainWriterAdmission`) with a final fence recheck right before the prompt leaves. Detail: `docs/agent-reference/agent-hub-coordination.md`.

Acceptance gaps that remain: (1) no positive proof exists on the macOS development host, and the Linux/bwrap path was not exercised on real Linux (its smoke test skips elsewhere); (2) Codex has no witness; (3) the proof is process-local, so a restart keeps ownership pinned and a person-driven resolution path is not built; (4) nothing calls `DrainWriterAdmission` yet because no pause API is exposed; (5) unmanaged writers of the same worktree are not covered; (6) tests with an injected `WriterStopProof` establish host logic, not real vendor support. Release and reassignment stay unavailable wherever the evidence is missing.

## Batch 5 — host-selected dependency references (2026-10-26)

Slice D (internal part) of [the harness integration plan](2026-10-26-team-harness-integration-implementation.md), `OPT-2e96ed`; the task stays In progress.

Delivered: group-scoped `GetWorkItemByTask`; `PackageHost.ResolvePackageReference` with typed missing/foreign/stale/unsupported errors; build-time re-resolution and canonical ordering/duplicate rejection of references; `CheckPackageReferences` for unsent-package rebuild decisions; the app host selects tracked `blockedBy` tasks bound to their requirements digest. Detail: `docs/agent-reference/agent-hub-coordination.md`.

Remaining acceptance gaps: (1) **decisions** — no durable decision record exists, so packages carry none (needs a person-written, sealed, scope-bound record with its own migration, tied to the result/decision flow); (2) no explicit artifact or document selection; (3) no public transport or budget-aware range reads (`OPT-59dfcb`); (4) retention cleanup is separate; (5) nothing yet calls `CheckPackageReferences` because the dispatcher (slice E) does not exist.

## Batch 6 — review fixes for the writer stop proof (2026-10-26)

Review of #340 found two P1 holes and one P2. Fixed on `fix/writer-stop-review`: (1) release now holds an admission seal across the proof and the commit (`ManagedReleaseBarrier`, `SealWriterAdmission`), and `resumeRuntime` — the single start point — registers an admission, so a start that passed an earlier check cannot open a runtime after a positive proof; (2) `ApplyRewind` and `ApplyRestore` are admitted and fenced, so a session that lost ownership can no longer fork a runtime or restore files; (3) the proof table keeps evidence only (no runtime references) and sweeps reaped entries on every open. The acceptance gaps recorded in Batch 4 are unchanged.
