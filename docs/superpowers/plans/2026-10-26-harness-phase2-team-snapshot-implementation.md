# Harness Phase 2 and TEAM Slice C implementation plan

Date: 2026-10-26. Status: **in progress; H1 delivered (see Execution record), H2–H5 and C1 not started**.

## Goal and starting point

Deliver one workspace harness configuration resolver, safe user-approved edits, session-scoped MCP serving/budgets, and host-owned snapshots that TEAM can consume before dispatch. Model profile (vendor/model/effort) and harness profile (tools/skills/servers/budgets) stay distinct. Do not build a dispatcher, vendor loop, second outbox or Team dashboard in this plan.

Sources:

- [Subscription harness tuning spec](../specs/2026-10-26-subscription-harness-tuning-design.md), §4–8.
- [TEAM integration handoff](2026-10-26-team-harness-integration-implementation.md), especially Slice C and dependencies B/D/E.
- [Canonical TEAM1 contracts](../specs/2026-10-25-team1-contracts.md).
- [Delivered attribution and MCP metrics](2026-10-26-harness-tuning-phase1b-usage-attribution-and-tool-metrics.md).
- [Managed TEAM implementation records](2026-10-25-managed-team-implementation.md).

PR339 (usage alignment), PR340 (stop evidence), PR341 (package references) and PR343 (release seal, rewind/restore fence and retention fixes) are merged according to the preceding GitHub checks. PR343 merge commit: `e4ac467605ec36ce3d5d54d35b49a8dc928faf94`; reviewed head: `c9354509b4a154a742a5976d1f27b8b5cab87c59`. Recheck GitHub/current main and CI before coding: the primary checkout inspected while writing this plan was still `8d909501` and contains an untracked earlier handoff document. Do not overwrite that file or reset another checkout. Fetch and work from current main containing these changes; choose migration versions at implementation time.

The person asked for this plan and a handoff prompt. This author changes documentation only. The receiving agent needs the person's implementation request; the document itself does not authorize account/configuration changes, live model experiments, messaging other sessions or merging PRs.

## Boundaries and ownership

Read current AGENTS.md and only the guides relevant to MCP, customization/recovery, vendors/sessions and coordination. Read the shell integration guide before UI navigation changes.

- Existing Agent Hub owns runtime open/load/fork/resume, adapters, accounts and admission seals. Never bypass the PR343 seal on a reload or invoke path.
- Existing MCP server owns the registry, grants and single budgeting middleware. A profile filters availability; it grants no authority. Denied groups have no shell fallback.
- Existing usage services/`usagemetrics` own measurement; keep unknown revisions NULL and do not introduce another collector.
- `agentwork` stays stdlib-only. TEAM receives a small host interface, not imports of Agent Hub, MCP, preferences, storage or a harness implementation.
- One implementation owner edits the effective resolver/OpenSpec/MCP-budget seams at a time. Inspect the board and concurrent harness work before starting; do not duplicate it.

## Delivery map

| PR slice | Deliverable | Prerequisite / exit |
| --- | --- | --- |
| H1 | Pure effective-config contract and tests | No runtime behavior change; source of truth for all later slices |
| H2 | Revisioned storage and approval-bound writes | H1; no agent mutation exposure until stale-proposal gates pass |
| H3 | OpenSpec/runtime snapshot lifecycle | H1/H2; all open/load/fork paths use one resolver |
| H4 | Session MCP filtering/budgets and matching prompt guidance | H3; hidden tools cannot be called directly; existing grants remain |
| H5 | Profile editor, proposal tools and override controls | H2–H4; real UI/bridge/gateway validation |
| C1 | TEAM snapshot preflight/package binding and attribution | H3/H4; no production dispatch enabled |

H1–H3 can merge inert plumbing separately. Do not activate group filtering before H4's guidance/pre-approval alignment is present. Do not expose proposal mutations before effective-diff confirmation and compare-and-swap are complete. UI is not a prerequisite for pure contract tests. B's Linux proof work and D's retention work can proceed independently under their existing tasks; this plan does not mark them complete.

## H1 — One effective configuration contract

Inspect `internal/preferences/customization.go` (or its current equivalent), `internal/agenthub/customization.go`, `adapter.go`, `service.go`, MCP registry and budget code. Prefer existing contracts; if a pure shared package is necessary, make it a stdlib-only leaf with injected registry facts and architecture tests, not a second service owning runtime state.

1. Represent V (vendor customization), W (workspace profile) and S (session override) with explicit versions. Preserve absent vs present-empty fields using optional fields/presence tracking; do not use `omitempty`/zero values as merge semantics.
2. Resolve exactly the spec: unknown tool groups rejected, absent group on, S only enables groups; deny lists V union W; servers keyed by name with W replacement and duplicate/vendor-environment collisions rejected; skill folders canonical absolute-path union. No profile yields current behavior. Keep workspace instructions in their current store.
3. Resolve budgets per tool, then group, then 24 KiB; validate 2–64 KiB and fixed exemptions. Publish no fake support for `headTail`: until the phase-3 implementation exists, reject it clearly rather than silently using head. Structured results keep the existing shrink/cursor semantics.
4. Classify the effective before/after configuration per tool/field: executable > widening > narrowing. Deleting an override can widen access or expose a fallback executable; budget comparisons use the actual previous effective value. A cut-mode-only change is neutral and uses the narrowing tier.
5. Compute a canonical versioned fingerprint from the inputs relevant to that diff: V/W/S revisions and canonical vendor-environment server/skill facts. Array/set ordering must not cause spurious changes; resolve symlinks in trusted host code. No secret/env values or vendor error text in fingerprint metadata/audit.
6. Reserve `harness.profile.propose` as always served even when its ordinary group is off. Its compact request/response surface must identify off groups and their measured definition cost. Do not create a catch-all exemption that serves other hidden tools.

Tests: absent/empty, deny precedence, server replacement/fallback/collisions, canonical paths and aliases, unknown group, deterministic fingerprint, effective budget deletion/overrides, mixed-tier changes and no-profile equivalence. Reuse registry budget tests; do not raise ceilings to fit the new tools.

## H2 — Storage, CAS and proposal approval

1. Add `workspace_harness_profiles` (workspace ID, schema version, revision, JSON) and `session_harness_overrides` (session ID, revision, enabled groups and lifecycle metadata). Use current migration sequence, foreign/scope checks and idempotent upgrades. Overrides must have a revision even though the spec's abbreviated table lists only groups/time: proposals and usage refer to S revision.
2. Profiles contain no secret values; server environment configuration stores names/references only. Reuse current customization validation and credential handling. Keep sensitive definition data out of logs, audit fields and generic errors.
3. Resolve proposals from authenticated workspace/session identity, never caller-provided parent/workspace/grant claims. Produce a Go-authored effective before/after diff; agent rationale is separately labelled untrusted text. Bind the exact target (session-only vs workspace plus requesting session), V/W/S and vendor-environment fingerprint to the proposal.
4. Show confirmation without holding a DB transaction or service write lock. On approval reacquire the serialization boundary, re-read all relevant inputs, recompute before/after, and CAS the write. W movement is `harness.profileConflict`; other changed inputs/diff are `harness.proposalStale`. Couple V/W/S writers through the same in-process validation boundary; handle external vendor-file changes per the spec's observed-fingerprint guarantee, not a fictitious filesystem transaction.
5. Session-only writes S; save-to-workspace atomically commits W and the requesting session's S. Publish in-memory snapshots only after commit. Failure changes neither store nor active serving state. Other running sessions retain their previous W snapshot.
6. Narrowing asks by default; only a UI-owned setting may permit automatic narrowing. Widening always asks; executable proposals require UI opt-in, one executable change per proposal, exact host-resolved command/args/path/env-name diff, and no focused approve button. No allow-always. Missing prompt handler/server mode fail closed. Policy settings, accounts, sandbox and vault stay UI-only.
7. Every write path, including UI reset/delete and removing overrides, must obey CAS/classification/current authorization. The UI being trusted does not justify an alternate store mutation that bypasses consistency checks.

Tests: concurrent proposals, stale V/W/S and vendor commands, deny removal exposing vendor server, cancellation/denial, headless/nil handler, save-W-plus-S rollback, deleted/moved workspace/session and no executable side effect before approval. No commands/servers are launched to validate a proposal.

## H3 — Effective snapshots at runtime open and reload

Inspect `OpenSpec`, `applySandboxExtras`, session host endpoint creation, `RuntimeUsageFacts`, resume/rewind/account-switch paths and PR343's admission guards.

1. Add explicit `ModelProfileRef` and `HarnessSnapshotRef` concepts. The host snapshot minimally carries workspace/session identity, nullable W/S revisions, V revision, fingerprint and effective groups/budget facts. A resolved default is distinguishable from a failed/unavailable resolver. Never synthesize revision zero for historical data.
2. Resolve before launch arguments, appended prompt and pre-approved lists are constructed. Use the same immutable launch snapshot across the adapter and its identity-bound MCP endpoint; publish it with the actual runtime only after successful open. A failed open tears down the endpoint and does not publish fictional applied revisions.
3. W edits mark running sessions as needing reload; they do not mutate those sessions' W snapshot. Session S enable/revoke changes serving live as H4 specifies. Skills/servers/folders/prompt/pre-approval remain fixed until a permitted open/reload.
4. Preserve S on resume/reload/restart, remove it on explicit close/delete, and preserve it for a rewind continuing the same session. A fresh child never inherits the parent's S. Distinguish explicit closure from temporary process teardown during account moves/reload; do not delete S in a generic runtime-close helper.
5. Executable proposals are effective in new sessions only under the stricter tier rule. An existing session must not adopt such a change merely because it reloads. If the implementation cannot retain its permitted launch configuration, refuse that reload with actionable 'open a new session' guidance; do not silently activate executables. Vendor files can change externally: retain the spec's mismatch notice and do not describe the fingerprint as control over those files.
6. Reload/resume/fork retain managed lease, attempt and admission semantics. No profile change releases ownership, creates another child or sends/replays a prompt. Refuse an unavailable snapshot; do not fall back to unfiltered defaults on a resolver/storage error.

Tests: each create/load/reload/rewind/account path, startup without a profile, failure before/after endpoint creation, current-W edits vs pinned runtime-W, S lifecycle, child non-inheritance and concurrent seal/reload. Use mocks; no live vendor calls required.

## H4 — Session-specific serving, budgets and prompt consistency

Inspect `internal/mcp/session_host.go`, `server.go`, `result_budget.go`, measured budgeting code, `devkit_prompt.go` and vendor adapters' allowed/pre-approved configuration.

1. Keep the full registry as the deterministic source for group inventory/cost, and expose a filtered per-session `tools/list`. Refuse direct calls to off-group tools with published `harness.groupOff` even if the vendor cached an old list. Preserve capability, caller/workspace scope and environment gates on every on-group call.
2. Evaluate an immutable session serving snapshot at call entry using memory only; do not read SQLite/environment reports on the hot path. Use one coherent snapshot for group decision, budget and measurement attribution. A later revocation refuses subsequent calls; it is not described as rolling back an already-running tool.
3. Atomically publish S changes and increment its revision; retain pinned W for other active sessions. A stale endpoint token cannot adopt a new runtime's snapshot. If publication is interrupted after a durable write, fail affected serving closed until reconciled; do not return successful revocation while still allowing later calls through an obsolete allow snapshot.
4. Extend the existing budgeting middleware to resolve effective budgets. Keep image/non-text/error behavior and fixed exemptions, preserve `_truncated`/cursor handling and all measured before/after counters. Byte reduction can be negative and is not a token estimate. Do not add a second truncation middleware or a second collector.
5. Send `tools/list_changed` where supported. No claim of vendor refresh without a real spike: otherwise display reload-required state while server-side refusal/availability already follows S. A newly served tool is not newly pre-approved; normal permission flow applies until an allowed reload.
6. Ship the minimum group-aware guidance split needed to safely activate filtering: hard safety rules always remain, sections with several dependencies require all groups, and off-group rules name the safe user path. With git off, keep the shell-command prohibition. Construct prompt/pre-approved lists from the same launch snapshot. Broader prompt editing and `headTail` remain phase-3 work.

Tests: list vs direct-call parity, unknown/stale clients, live enable/revoke, S isolation between siblings, permission still required, token mismatch, failed publication/recovery, budget precedence at 2/24/64 KiB, UTF-8 and escaped JSON, exemptions/non-text/errors, truncation cursors and exact metrics attribution. Extend existing definition/per-group budget and hard-rule prompt tests.

## H5 — UI, bridge and proposal tools

1. Add a workspace-scoped profile editor under the existing Agents settings surface, using shared shell primitives. Show inherited/effective values, measured group definition bytes, changed/reload-required state and active session overrides. Do not add another sidebar or tab strip.
2. Present Go-authored proposal diff and scope choices: this session only vs save-to-workspace. Show executable opt-in and automatic-narrowing policy only in UI. Revocation states distinguish server revoked, vendor list awaiting reload and gone after reload/close.
3. Add thin `harness.profile.get` and `.propose` adapters through the existing capability registry/gateway. Agent scope comes from its authenticated session workspace; an unaffiliated/shared-terminal caller must not choose an arbitrary workspace. Define its supported behavior explicitly and fail closed if identity cannot resolve the target. Do not expand the read tool into account/sandbox/permission APIs.
4. Publish every introduced code in `bridge.keptCodes` or deliberately document redaction. Spec codes: `harness.profileConflict`, `profileDenied`, `groupUnknown`, `budgetInvalid`, `proposalStale`, `groupOff` under the harness namespace. Keep all tool definitions/results compact, bounded and free of secret data; command details belong in the authorized confirmation, not audit metadata.
5. Regenerate bindings after AppService changes. Test the real container grants, bridge calls and confirmation component as well as service units. Loading/errors must not render as an empty profile or as successful revocation.

Exit: no agent-accessible path bypasses approval tiers, scope or stale-proposal checks; UI and active runtime clearly distinguish desired, stored and applied configuration. Browser fixtures/gallery are not native vendor verification.

## C1 — Bind TEAM preflight to actual harness snapshots

1. Add a small host-owned interface returning the intended fresh-child config and the actual opened child's `HarnessSnapshotRef`; keep concrete resolver/Agent Hub imports in composition adapters. Roles declare only the groups their actual operations require; review those declarations rather than enabling every tool globally.
2. Validate intended saved W plus V before creation; validate actual child identity, scope, grants/mode, required groups and fingerprint again before enqueue. Missing groups create a bounded configuration-needed reason; the dispatcher must not inherit parent S, self-enable groups, change model/account policy or fall back to shell.
3. Extend immutable package/dispatch bindings with schema-versioned harness snapshot references separately from model profile. Rebuild an unsent package through the supported new package/attempt protocol if config changes; do not overwrite immutable rows or assume the current one-package-per-attempt store permits replacement. Keep v2 task requirements digest unchanged: configuration staleness and requirements staleness are distinct.
4. Preserve the snapshot used by historical results. Populate existing turn metrics with the actual turn-start runtime facts, and MCP metrics with actual call-start serving facts; later live S changes do not relabel earlier turns/calls. No new collector or historical backfill from current session/profile rows.
5. This PR provides preflight interfaces/tests and package binding only. Slice E later owns persisted operation IDs, task assignment, enqueue, recovery and production dispatch activation. Absent proof/profile dependencies remain explicit; mock support cannot enable production behavior.

Tests: parent S not inherited, missing group blocks before prompt, W/V changed between intended and actual open, same model/different harness, stale endpoint, stale package snapshot, no overwrite of old package, revoked tools, config-read failure and seal/lease preservation. No result acceptance or push/merge is authorized by preflight success.

## Validation and acceptance

For each bounded PR run the affected Go tests, meaningful race/fault tests, `go vet`/lint as required and `go test ./internal/arch/` for package changes. Final Go gate is `go test ./internal/... ./cmd/...`; build real frontend assets before root server composition checks. Frontend UI/bridge changes require `bun test --isolate`, `bun run build` and regenerated committed bindings where needed. Test provider discovery with safe fixtures first; live vendor/account experiments require an explicit separate user request.

Completion requires: no-profile compatibility; correct effective merge/classification; atomic stale-safe approvals; correct child/override lifecycle; direct-call refusal even with stale vendor lists; aligned prompt/pre-approval; actual nullable attribution; TEAM snapshot validation; and real container security tests. Linux/bwrap proof, Codex stop witness, manual retention, dispatcher, review/acceptance and Team UI remain independently open. Do not infer quota savings from MCP bytes or token totals.

## Execution and handoff

Create/claim appropriate tasks through DevKit MCP under the existing roadmap after inspecting current ownership. H1–H5 may need new harness tasks; no IDs are invented here. Keep existing TEAM task dependencies; do not close `OPT-b7e5f6` or `OPT-2e96ed` for completing only this plan. Use one branch/PR per reviewable slice and record the branch on its task. The person prefers terminal git/gh for commits/push/PR; retain task/branch guards and never force-push or bypass a failed board verifier. Do not merge automatically.

Start with H1, then H2/H3; activate behavior only as the delivery table allows. If another agent already owns harness phase 2, consume its contracts and implement only the missing TEAM adapter/tests, with the person's authorization before messaging that agent. Report each PR's tests, limitations and unfinished acceptance items, and update the board through normal MCP services.

## Execution record

Tasks (board OPT, blocked chain H1 → H2 → H3 → H4 → H5/C1): OPT-e3df87 (H1), OPT-984269 (H2), OPT-ca55c2 (H3), OPT-79901a (H4), OPT-f0224f (H5), OPT-b4be56 (C1). `tasks.claim` was refused (`validation_failed`) so the tasks were moved and their branch recorded through `setFields`/`transition` without a claim. No second harness implementation existed on `main` (PR339–343 merged, checked on 2026-10-26).

### H1 (branch `harness/h1-effective-config`)

Delivered: `internal/harness` (types, `Resolve`, `Classify`, fingerprint) as a stdlib-only leaf with an architecture rule; `internal/mcp/harness_registry_test.go` resolves the real registry (17 groups, 67,157 definition bytes, equal to the spec's measurement) and pins `ResultBudget`. Nothing calls the package yet.

Decisions: tiers are `None < Narrowing < Widening < Executable`; a deny removal that exposes a vendor-defined server or skill is executable (the spec's "gains one" rule), any other deny removal widens; each budget field (size, cut) falls back separately; a stored profile that names a vanished tool or group fails `Resolve`.

Open for later slices: command/args facts for vendor-defined servers (the environment report has none), the repair path for profiles naming removed tools, `harness.profileInvalid` publication in `bridge.keptCodes`, and everything in H2–H5/C1.
