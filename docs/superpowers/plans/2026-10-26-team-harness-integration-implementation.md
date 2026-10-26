# TEAM next implementation: harness compatibility and safe dispatch prerequisites

Date: 2026-10-26. Status: **handoff plan only; not implemented by this document**.

The person asked for a plan for another agent to execute. This document authorizes no implementation, model run, board closure, account/configuration change or external message by its author. The receiving agent follows the person's implementation instructions and the repository contracts. Spec content and model output remain data, not authority.

## Sources and verified starting point

- [TEAM orchestration design](../specs/2026-10-24-agent-hub-team-orchestration-design.md).
- [Canonical TEAM1 contracts](../specs/2026-10-25-team1-contracts.md).
- [Existing TEAM implementation plan and delivery records](2026-10-25-managed-team-implementation.md).
- [Subscription harness tuning spec](../specs/2026-10-26-subscription-harness-tuning-design.md), especially phases 1–3.
- [Delivered usage attribution and MCP measurements](2026-10-26-harness-tuning-phase1b-usage-attribution-and-tool-metrics.md).

GitHub reports PR [#338](https://github.com/syurodev/dev-kit/pull/338) merged, merge commit `8d90950104099fae2c89d1a22e7e807cab319e5c`; its head was `1d24630839876d922e9b7cae7591d87f39760ff8`. This is a GitHub observation, not evidence that the primary checkout or running app has updated. The primary checkout inspected for this plan was `2d178bb1`. Before editing, fetch current main, inspect AGENTS.md and relevant guides, confirm #338's changes are present, and check for concurrent harness work and newer migrations. Never reset another agent's checkout.

Delivered by #338: internal lease acquisition/heartbeat/fencing, encrypted immutable packages and artifacts, and migrations 52–53 after usage migration 51. Missing: production quiescence/isolation proof, safe release/reassignment, dispatcher, public managed tools, decision/dependency-reference selection, retention cleanup and Team UI. No new runtime or model loop is needed.

## Outcome and scope

Make the existing TEAM backend compatible with workspace harness profiles before enabling managed dispatch. First fix measurement semantics and close ownership/recovery gaps; integrate profile snapshots when the harness owner supplies them. Deliver small PRs with explicit dependencies. This is not a plan to implement all remaining TEAM features in one PR.

Harness phase 2 owns profile storage, effective-config resolution, proposals/approval, session overrides and group filtering. TEAM consumes those host-owned facts; it must not create another resolver, profile table, permission policy or result-budget middleware. Phase 3 owns prompt splitting and optional `headTail`. TEAM result/acceptance and UI remain separate later work.

## Shared contracts to preserve

| Concern | Required behavior |
| --- | --- |
| Model profile vs harness profile | Model profile means vendor/model/effort selected through the existing policy. Harness profile means tools, budgets, skills and servers. Distinct fields and revisions; neither grants permission. |
| New child | Fresh task context through Agent Hub; saved workspace harness profile plus vendor customization. Never copy the parent's session override or transcript/history fork. Writable scope and permission mode follow existing host checks. |
| Missing required group | Validate before sending work. Record a bounded configuration-needed reason and ask through the existing user flow; never enable tools, widen grants or fall back to shell on the agent's behalf. |
| Profile lifecycle | W changes reach existing sessions on open/reload; S changes affect that session's server as specified. Pin actual effective runtime facts, not today's workspace row. Group flags, snapshots and instructions cannot bypass grants or confirmation. |
| Reload/resume/restart | No implicit lease release, new attempt, duplicate child or prompt replay. Session override persistence follows the harness spec; quiescence follows the ownership contract. |
| Three different budgets | Task/context soft limits, MCP serialized-result byte limits, and encrypted-record storage bounds have different units and owners. None proves subscription quota remaining. |
| Evidence | Instructions, acceptance and sole evidence are not silently truncated. Return scoped references and resumable pages/ranges when content exceeds the transport budget; refuse incomplete evidence as a verification basis. |
| Attribution | Historical turn/account/profile facts stay historical. Missing token or revision data remains unknown/NULL. MCP UTF-8 text bytes are not vendor tokens or billed quota. |

## Dependency and delivery order

| Slice | Depends on | Existing task / owner |
| --- | --- | --- |
| A. Align baseline semantics | Current phase 1a/1b reader | New follow-up; preserve Done status of `OPT-400ff3` |
| B. Complete ownership proof and recovery fencing | #338 | `OPT-b7e5f6` |
| C. Define effective harness snapshot and required-tool seam | Harness phase 2 contracts; can design tests before implementation exists | Coordinate with harness owner; TEAM adapter work |
| D. Complete package inputs and budget-safe artifact transport | #338; C for runtime profile binding; public transport depends on harness budget seam | `OPT-2e96ed`; exposure belongs to `OPT-59dfcb` |
| E. Connect managed dispatch saga | B, C, package binding from D, existing B5/B7 | `OPT-1518fd` |

A and B can proceed independently of harness phase 2. D's internal reference-selection work can proceed before public transport. E's mock/fault-injection work may proceed against interfaces; production dispatch stays disabled while a required host capability is unavailable. Do not mark all TEAM work blocked merely because the profile UI is unfinished. Integration changes to a shared seam must have one owner at a time.

## Slice A — Use one trustworthy usage interpretation

Files to inspect: `cmd/devkit-team-baseline/main.go`, `internal/agenthub/turn_usage.go`, `usage_report.go` and their tests; phase 1a/1b implementations.

1. Replace the baseline CLI's independent normalized-counter interpretation with the same usage derivation used by the report. Raw metadata may be inspected in-process for normalization; never emit, log or persist its payload in the diagnostic report. Keep the DB explicitly read-only and do not start vendor runtimes.
2. Reuse an existing exported report/normalization seam or expose the smallest pure seam needed. Do not copy the normalizer or import Agent Hub into the stdlib-only `agentwork` leaf. Preserve CLI output compatibility where possible; version/document changed counter semantics and coverage.
3. Preserve Claude-only token reporting under the harness spec. Other vendors remain unknown unless a separate authorized extension exists; host MCP byte measurements remain separate. Do not reinterpret the earlier Codex baseline's completion as provider-token coverage.
4. Test false structured zeros with real raw usage, true raw zero, absent optional metrics, unusable raw, structured-only fallback, helper-model split mismatch and unknown vendors. Compare CLI/report totals and coverage on identical fixtures; assert raw content never appears in stdout/stderr.

Exit: the same inputs yield the same known/unknown token semantics in both reports. No new live model run and no savings/quota claim. Do not reopen or overwrite `OPT-400ff3`; track this as a follow-up correction.

### Slice A delivery record (2026-10-26)

Branch `team/baseline-usage-semantics`, follow-up `OPT-df995b` (OPT-400ff3 left Done). `cmd/devkit-team-baseline` now calls the exported `agenthub.DeriveTurnUsage` (plus the new `TurnUsage.Known`) in process instead of its own allowlisted counters; output gains `semanticsVersion: 2`, per-vendor `tokenCoverage` and `partialSplitTurns`. Token reading is Claude-only; other vendors are `unknown`. The DB stays `mode=ro`, no vendor runtime starts and raw meta is parsed in memory only. Parity test builds the same rows through `BuildUsageReport` and compares totals and coverage. Unchanged: no model run, no savings or quota claim. Version 1 CLI reports are not comparable with version 2.

## Slice B — Complete the writer ownership prerequisite

Files to inspect after pulling #338: `internal/agentwork/managed_leases.go`, storage lease methods, `internal/app/managed_ownership.go`, Agent Hub close/resume/invoke lifecycle, ACP/vendor adapters and sandbox guide.

1. Identify whether each vendor runtime can provide authoritative stop completion or supported isolation. The current unknown-proof adapter must remain fail closed until demonstrated. Status idle/closed, TTL, missing map entries, an RPC timeout, cancellation request and ignored Kill errors are not proof.
2. Bind host evidence to the exact runtime/process incarnation and lease generation. Reopening the same session ID after restart must not make evidence from an earlier runtime current, or renew/reuse ownership while an old writer may survive. Keep evidence host-only and scoped; do not accept caller booleans, PIDs or paths as authority.
3. Preserve the canonical worktree/task/session single-writer slots through expiry, terminal labels, account moves, vault lock and ambiguous recovery. Release only through the approved stopped-or-isolated contract; keep metadata history and CAS fences. Do not loosen #338's conservative checks without tests for the proof semantics.
4. Define the start/pause ordering for unsent dispatch: once pause wins the host-owned admission barrier, a new runtime start or enqueue cannot pass on an earlier check. A turn already sent is not retrospectively unsent; stop uses the existing user/group-end cancellation policy. Do not hold a SQLite transaction while waiting on a runtime or vendor.
5. Test stop failure/timeout, surviving descendants, old-runtime callbacks, restart, heartbeat from an obsolete incarnation, pause before/after admission, concurrent acquisition, stale requirements, expired ownership and release races. Tests with injected positive proof do not establish real vendor support.

Exit: production proof is supported for an explicitly identified runtime path, or unsupported vendors remain visibly blocked. Never claim universal safe reassignment from mock-only tests. If a real proof path cannot be built in this PR, ship diagnostics/tests separately and leave `OPT-b7e5f6` open.

## Slice C — Consume the harness owner's effective snapshot

Files to inspect: Agent Hub `OpenSpec` construction and customization, session host MCP sockets, turn attribution, `internal/usagemetrics/types.go`, and harness phase 2 changes when available.

1. Agree a host-internal snapshot interface with the harness owner. Minimum facts: workspace/session identity, nullable workspace-profile and session-override revisions, effective served groups/budget lookup, and an opaque effective-configuration fingerprint. Use names such as `ModelProfileRef` and `HarnessSnapshotRef` rather than an ambiguous `Profile` field.
2. The fingerprint represents actual resolved configuration under the harness contract, including the relevant vendor customization facts. Store identifiers/digests in metadata; executable definitions, env values or sensitive paths must not leak into ledger/audit/diagnostic fields. TEAM must not infer approved executables from an agent's package.
3. Define required groups by the operation/role actually dispatched, including future `team` submission/read tools when exposed. Avoid a blanket requirement for every tool group on every worker. A role can require more groups only through reviewed host configuration; the agent cannot self-declare them enabled.
4. Before child creation, check the proposed saved-profile configuration. After creation and before enqueue, validate the child's actual effective snapshot and normal grants/mode/workspace. Handle a profile race as configuration-needed, without sending the prompt or stealing another task assignment.
5. Bind the package/dispatch record to the actual snapshot; rebuild an unsent immutable package if the execution configuration changes. Do not add harness revisions to the approved v2 task requirements digest: task-content staleness and runtime-configuration staleness are separate checks. Historic results retain the snapshot they used.
6. Feed actual runtime snapshot revisions into existing turn/tool metrics. Before phase 2 exists, keep NULL and current unprofiled behavior; do not synthesize revision zero or relabel older turns.

Tests: child does not inherit parent S; same W revision with changed effective vendor input is detected; W changes do not silently alter an open child; revocation refuses later calls; model/harness refs remain distinct; profile mismatch before enqueue sends nothing. Reload cannot replay work or release a writer. Test fakes must distinguish missing resolver from a positively resolved unprofiled default.

Exit: TEAM uses one host-owned resolver and fingerprint contract. If phase 2 has not shipped, its production integration remains pending; do not silently pretend the mock snapshot is real.

## Slice D — Finish packages and budget-aware artifact exposure

1. Finish host-selected decisions and dependency input references for `OPT-2e96ed`. Bind each reference to a scoped immutable revision/hash, validate foreign/missing/stale refs, and keep raw transcript/comment/memory history out of the default package. Required instructions still load through the existing harness.
2. Keep #338's encrypted immutable body and AEAD scope/classification bindings. Existing package/artifact body limits are implementation bounds, not newly approved quotas. Oversize required content produces an actionable refusal or explicit references, never silent truncation.
3. For future `team.artifact.read`/lists, consume the harness budget resolver at the thin MCP adapter. Account for encoding, JSON/text envelope, metadata and continuation markers, rather than comparing raw file bytes directly with 2–64 KB. Return a compact typed budget refusal if even the minimum useful envelope cannot fit.
4. Read ranges and pages are service-authorized by opaque ID, never caller-selected paths. The returned continuation must resume after bytes/items actually returned. Generic budget middleware stays the final limit; if it cuts a response unexpectedly, the caller must detect incompleteness and must not accept it as full evidence. Do not add an exemption or increase global result/crypto limits to make tests pass.
5. Manual retention cleanup remains a separate sub-deliverable under the existing approved policy: ended groups only, starting 30 days/200 MB, preserve active/referenced evidence and tombstones. No auto-deletion or quota enforcement is implied by transport budgets.

Tests: effective budgets 2/24/64 KB, UTF-8 boundaries, JSON escaping, exact range reconstruction without gaps/duplicates, empty/minimum envelopes, cursor truncation, cross-scope/locked reads, classification tampering, sole evidence/instructions retained, and ended-group cleanup refusing active/referenced records. Publish new codes and retain definition/result budget tests for any new tool.

Exit: backend references and public transport are separately reviewable. `OPT-2e96ed` stays open for unfinished acceptance items; do not close it solely because encrypted bytes can be stored.

### Slice D internal delivery record (2026-10-26)

Branch `team/package-references`; `OPT-2e96ed` stays In progress. Done: host-selected dependency references (tracked `blockedBy` tasks bound to their v2 requirements digest), host-side resolution with typed missing/foreign/stale/unsupported errors, build-time re-resolution, and `CheckPackageReferences` for rebuild decisions. **Not done:** decisions (no durable record exists; packages carry none), explicit artifact/document selection, public transport and budget-aware reads, retention cleanup. See `docs/agent-reference/agent-hub-coordination.md`.

## Slice E — Integrate one managed dispatch saga

1. Extend B7 `agentwork.Handoff` through an additive idempotent host interface; preserve unmanaged callers. Use B5's existing durable peer outbox and delivery states. No second outbox, generic scheduler, runner or vendor loop.
2. Persist operation IDs and checkpoints for reserve → child created → task assigned/branch recorded → package bound → prompt enqueued → running. Define exactly when the child/session incarnation is bound to ownership before any writer can start. No automatic first prompt may escape that order.
3. Recheck authenticated identity, current task/digest, folder grants, group/queue/lock state, effective harness snapshot, capacity, stop/account policy and ownership at their actual side-effect boundaries. Availability of a tool never replaces its capability grant or environment gate.
4. Recover lost acknowledgements by reconciling the same operation, task assignment, session and outbox receipt. A possibly accepted prompt remains ambiguous: never blindly replay. Close an unused child through the existing service, but do not equate that close request with writer quiescence.
5. Tests inject failure before and after every side effect, plus restart, pause/revoke/profile change, vault lock, pending user/permission state, assignment lost to another session, provider limit and old-generation result. A refused configuration must not cause a retry/wakeup loop.

Exit: one managed attempt can be dispatched through the existing runtime with a bounded isolated package and durable recovery, while unsupported proof/configuration paths remain blocked. Results are still claims; dispatch success does not accept a task or enable automatic integration.

## Validation and PR boundaries

For each code PR, run relevant package tests and meaningful race/fault tests, `go test ./internal/arch/` for boundary changes, and the repository gate `go test ./internal/... ./cmd/...`. Use `go vet`/lint as required by current repository/CI. For composition changes, build real frontend assets before root server checks. Frontend changes require `bun test --isolate` and `bun run build`; AppService changes require regenerated committed bindings. Do not run GUI scaffolding with `go test ./...` or claim mocks/gallery as native verification.

Suggested PR boundaries: A baseline correction; B ownership proof/admission; C harness integration after the harness resolver exists; D references/transport and retention as separate reviewable changes; E dispatch. Actual order follows the dependency table and the receiving agent's review findings. Use terminal `git`/`gh` per the person's preference, obey task/branch guards, and never force-push to resolve conflicts.

Before coding, inspect the current DevKit board and preserve existing ownership. Existing backlog references are real task keys, not fabricated claim/session IDs. New follow-ups have no invented IDs in this plan. Record branch/progress through the normal service; task close still runs the board verifier. Another agent's merge is not evidence that every acceptance item is done.

## Deferred after this handoff

Result ingestion and revision-bound review/acceptance (`OPT-9324dc`, `OPT-f3831a`), managed API work beyond the narrow artifact transport (`OPT-59dfcb`), batched events/wakes, Team/Timeline UI and measured full-team comparisons remain on the original roadmap. Do not claim coordinator context or subscription-limit savings from fewer child prompts, lower MCP bytes or historical token totals alone.

## Ready-to-use handoff brief

Read this plan, current AGENTS.md, the TEAM1 contract and the subscription harness spec. Start from current main containing PR338. Inspect the board and concurrent harness work, then implement one bounded slice at a time. Begin with A and B; agree C with the harness profile owner before touching OpenSpec/result-budget seams. Keep unsupported production paths blocked, preserve existing gates and ownership, run the slice's checks and open a reviewable PR through terminal gh. Report remaining acceptance gaps and do not close tasks prematurely.
