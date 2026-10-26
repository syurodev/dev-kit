# H3 implementation: runtime harness snapshots and override lifecycle

Date: 2026-10-27. Status: **in progress; H3a–H3c delivered (see Execution record)**.

## Objective and current evidence

Implement H3 of [Harness Phase 2 + TEAM Slice C](2026-10-26-harness-phase2-team-snapshot-implementation.md): one host-owned harness snapshot per opened runtime, safe lifecycle behavior for W/S, explicit stored-versus-applied state, and trustworthy attribution. Preserve the current Agent Hub runtime, permissions, workspace scope and release admission seals.

Sources: [subscription harness spec](../specs/2026-10-26-subscription-harness-tuning-design.md), §4–6; [H2 detailed plan](2026-10-26-harness-h2-storage-approval-implementation.md); [TEAM integration handoff](2026-10-26-team-harness-integration-implementation.md); [customization/recovery guide](../../agent-reference/agent-customization-recovery.md); [coordination guide](../../agent-reference/agent-hub-coordination.md).

Observed before writing:

- H1 fixes in PR345 are merged. PR346 H2a is still OPEN at `516c45fe7e4ff7180c6f54b2b804ee5d8ac0f6f8`; its prior strict-read findings passed independent reproduction checks after the fix. Recheck its merge and CI state rather than assuming it is on main.
- Local `harness/h2c-apply` at `bde0ab4d` already contains H2b proposal code (`b089dda1`) and H2c apply code (`ec10ec33`), plus the H2a fix. These are observed local commits, **not evidence of reviewed/merged H2b/H2c**. Do not overwrite this checkout, duplicate those services or treat their execution notes as review acceptance.
- That branch has `internal/harnessconfig`, app confirmation/directory adapters, preference customization boundary support and `AgentHub.HarnessScope`. Its notes still list gaps: production vendor facts/registry/service composition, W reset/delete and override removal through service, explicit-close S cleanup, executable confirmation UI and public transport.

Before editing, fetch current main, inspect current AGENTS.md and the relevant guides, recheck those branches/PRs and board ownership, and identify the accepted base. A stacked PR is allowed if clearly based on the unmerged prerequisite; never self-merge or silently depend on somebody's dirty working tree. This author changes documentation only; implementation is for the receiving agent under the person's request.

## Scope and activation boundary

H3 is runtime snapshot/lifecycle plumbing. H4 owns live tools/list and direct-call filtering, result budgets, S publication, prompt/pre-approval alignment and vendor refresh. H5 owns the public editor/proposal tools. C1 owns TEAM preflight/package binding. Do not start a dispatcher, add live model experiments or build a second resolver, outbox, metrics collector or permission system.

**No partial activation:** an effective profile cannot be reported as applied while the launch config and its MCP serving policy disagree. H3 may ship inert interfaces/default-compatible plumbing. Non-default groups/budgets require H4's consumer/handshake; until it exists, keep activation disabled or refuse the unsupported profiled launch with an actionable reason. Do not pass a filtered prompt/pre-approved list to a full unfiltered MCP endpoint, or stamp profile usage revisions for configuration never actually applied.

## Deliverable order

| Slice | Deliverable | Gate |
| --- | --- | --- |
| H3a | Snapshot types, host provider and immutable copy/identity tests | Corrected H1; reviewed store/provider contracts |
| H3b | One prepare/open/publish pipeline for all runtime paths | H3a; existing PR343 admission seals retained |
| H3c | Durable permitted launch state and S lifecycle cleanup | H3b; H2 CAS/boundary integration |
| H3d | Applied/pending state and existing attribution wiring | H3b/H3c; actual serving state only |

Use small reviewable PRs. Tests against injected ports can precede the production provider. Mark missing production support explicitly; passing mocks is not grounds to activate a profile.

## H3a — One snapshot contract and trusted provider

Inspect the accepted versions of `internal/harness`, `internal/harnessconfig/ports.go`, app H2 adapters, `internal/agenthub/adapter.go`, `service.go`, `turn_attribution.go`, `internal/mcp/session_host.go` and `internal/usagemetrics`.

1. Define distinct model and harness references. A model profile selects vendor/model/effort; a harness snapshot identifies tool groups/budgets, skills/servers and configuration provenance. Never reuse an ambiguous `Profile` field for both.
2. Carry host-resolved workspace/session/vendor identity, nullable W/S revisions, the V revision, versioned effective fingerprint, effective groups/budgets, runtime incarnation and endpoint token. Missing W/S is nil, not synthetic revision 0. Distinguish a positively resolved unprofiled default from an unavailable/corrupt provider.
3. Reuse corrected H1 Resolve/Classify and H2's accepted store/facts contracts. Canonicalize in host code, reuse existing customization validation and derive scope from actual session/workspace/grants. Caller packages and MCP args do not provide authority. Return detached copies: mutating a request, candidate profile or future store row must not mutate an opening/live snapshot.
4. Produce a deterministic full-registry fact source from the same operation/tool declarations as the server; do not create a production registry by copying the test fixture or use a filtered registry as the universe. Coordinate its ownership with H4.
5. Do not fabricate vendor executable facts from `EnvironmentReport` labels, which lack full commands/arguments. A required incomplete fact is unavailable, not an empty list. Preserve today's unprofiled operation where no new profile feature is active, but do not label incomplete executable coverage as verified or supply its fingerprint as a fully checked TEAM configuration.
6. Keep paths, executable arguments, instructions and environment values out of public snapshot summaries, audit and usage metadata. Raw values required only by the authorized launch adapter stay in that private config path; opaque IDs/digests are not permission grants.

Tests: absent versus failed/corrupt provider; true default compatibility; detached slices/maps; same model with different harness; symlink retarget; canonical order; actual workspace scope; stale endpoint token; complete versus incomplete vendor facts. No filesystem/network/runtime action in the pure harness leaf.

## H3b — Prepare, open and publish one consistent runtime

1. Inventory CreateSession, `resumeRuntime`/Load, explicit reload, rewind Open/Fork, dead-runtime replacement and account-driven reopen. Make each use one shared preparation adapter before the launch arguments, prompt and vendor pre-approved list are finalized. Avoid re-resolving mutable current W separately for each consumer.
2. Introduce a backward-compatible endpoint preparation/configuration seam. The endpoint's served groups/budgets, vendor prompt/pre-approval and runtime snapshot must be derived from one prepared value. Retain token/generation validation so a stale socket cannot adopt a newer runtime's identity or metrics. Never temporarily expose default-on tools during endpoint setup.
3. Stage the endpoint snapshot before adapter Open/Load/Fork, so early vendor tool discovery/calls cannot see an unrelated full registry. Define whether early tool calls are safely served under that prepared identity or refused as initializing; do not publish completed runtime/account attribution before successful open.
4. Only after the runtime opens and the host accepts its exact identity publish it as applied together with usage facts. Failure at resolve, endpoint creation, temp preparation, adapter open, ACP-ID/settings restoration or session publication tears down staged endpoint/state and keeps the previous durable applied record honest. Do not report a failed open as applied.
5. Retain PR343's admission coverage for every existing-session start and for rewind/restore. A release seal wins against a late start; no new helper may call adapter Open/Load/Fork around it. No DB/configuration mutex is held across vendor calls, endpoint I/O or a person's response.
6. Account selection still follows the existing policy and actual runtime account resolution. A harness profile does not rotate accounts, raise model/effort, weaken mode/grants or release a managed lease.

Tests: every path above using instrumented mock adapters/endpoints; barriers before and after each preparation/publication boundary; forced open/settings/DB failures; release seal racing invoke/reload/rewind; two simultaneous resume callers; initial endpoint queries before open completes; late old-endpoint callbacks. Assert no unexpected process, prompt, task assignment or partial applied-state publication.

## H3c — Desired, permitted and applied state across lifecycle

| Event | Required behavior |
| --- | --- |
| New top-level session | Resolve current saved W + V and that session's S, if valid |
| Fresh child | Current saved W + V; no parent S and no transcript/history fork |
| W edited while child is running | Its pinned W does not change; mark reload-required |
| Permitted non-executable reload | Prepare a new snapshot and publish only after success |
| Executable proposal changes W | New-session-only; existing session cannot silently adopt it on reload |
| Resume/restart | Preserve stored S and permitted configuration semantics; no startup model calls |
| Rewind continuing the same session | Keep that session's S; not equivalent to a fresh child |
| Explicit close/delete | Remove S through the lifecycle-owned service path; do not recreate it by a late proposal |
| Account move, crash, temporary teardown | Preserve S; generic runtime Close is not explicit session close |
| Override revoke | H4 owns live refusal/publication; H3 must not promise it before H4 exists |

1. An applied revision reference alone cannot reconstruct old W/V after their current rows are replaced. Define an immutable permitted-launch snapshot/version store, or explicitly refuse automatic restoration where the original permitted executable config is unavailable. Never rebuild old references using today's different values. Keep any persisted launch definitions within the repository's sensitive-payload/vault contract; do not persist prompts, skill bodies, env values or transcripts to solve this.
2. Preserve executable proposal intent and the previous permitted launch set for existing sessions. If the host cannot safely retain/reconstruct it, block that reload and direct the person to a new session. Vendor files remain externally mutable: show the spec's fingerprint-mismatch notice and state its limited observed-facts guarantee; do not claim DevKit controls those files.
3. Implement explicit-close cleanup through the H2 owning service/composition, not a UI SQL call. Use stamp/CAS/tombstone behavior. Integrate closing state with H2's final scope check/validation boundary so an approval that checked scope before close cannot commit S after cleanup. Do not hold that boundary while waiting for runtime shutdown.
4. Define close failure and cleanup failure explicitly. A failed durable S cleanup must not be reported as successful removal; a retry must preserve monotonic stamps. A closed/deleted session cannot be resurrected by a delayed pending approval. Resume/rewind/account teardown must not accidentally invoke this cleanup.
5. Restart invalidates pending approvals and staged opens, recovers committed config records only, and never resumes an old permission request as answerable. Managed lease recovery remains conservative; no profile snapshot authorizes writer release/reassignment or ambiguous prompt replay.

Tests: restart after each prepare/open/commit boundary, current W overwritten while old runtime survives, missing historical launch config, explicit close versus reload/account teardown, S CAS change during close, close versus H2 apply commit in both orderings, delayed approval callback after cleanup, cleanup DB failure, delete/recreate tokens and rewind preserving S. Do not conflate H2 configuration stamps with lease generations or board task revisions.

## H3d — Status and trustworthy attribution

1. Expose an internal read model distinguishing saved/desired configuration, prepared configuration, actual applied runtime snapshot, applied-with-incomplete-facts coverage, reload-required and failed/unavailable states. No public UI endpoint is required in this slice; H5 will render it. Empty data must not mean successful application.
2. Feed `RuntimeUsageFacts`/turn attribution with actual applied profile/override revisions at turn start and the actual runtime account generation. Existing historical turns keep their previous NULL/unknown attribution. Do not stamp a pending W revision or backfill old rows from current session state.
3. H4's MCP observer reads the actual per-call serving snapshot, including live S revision changes. A turn's start snapshot and a later tool call's serving snapshot can legitimately differ; keep their meanings distinct. No DB or vendor inspection on the metrics hot path.
4. Add fingerprints/versions to durable metadata only if needed for recovery or diagnostics; extend existing schema/versioning instead of a second usage collector. Missing/partial facts stay unknown. Tool bytes are not vendor tokens or subscription quota.
5. If H4 is not active, no profile edit returns `Live=true` or claims to have revoked a tool. Preserve H2's stored-only result until active publication/refusal is actually wired and verified.

Tests: W changed mid-turn, S changed mid-turn, old endpoint after reload, failed runtime open, unknown account/revision, real normalized report partitions unchanged and no metadata leaks. Verify behavior using actual host facts rather than labels copied from the desired profile.

## Deferred H2 obligations and integration gates

Do not hide unfinished H2 behind snapshot wiring. Before construction/exposure of the real proposal service, identify reviewed implementations for production vendor facts, registry/provider, policy and directory adapters, V-writer boundary participation, and W reset/delete/S removal paths. Executable proposals remain unavailable until their exact diff can be shown through a supported confirmation component and all executable-tier gates pass.

H3 may consume reviewed provider ports while incomplete branches remain stacked, but its PR description must identify that base and list mock-only adapters. No new public mutation API or live filtering is authorized merely because H2b/H2c commits exist locally. No change to TEAM task dependencies or terminal status is implied by this plan.

## Verification and acceptance

Run meaningful lifecycle/race/fault tests for Agent Hub, app composition, harness/harnessconfig, storage/preferences and endpoint plumbing as affected. Re-run the PR343 release/rewind/retention regressions and H2 strict-read/CAS/approval regressions. Run architecture tests for new package boundaries, scoped vet/lint and `go test ./internal/... ./cmd/...`. Build real frontend assets before root server composition checks; do not create placeholder dist files. AppService changes require regenerated bindings; frontend changes require `bun test --isolate` and `bun run build`.

Acceptance: all runtime paths use one prepared snapshot; no-profile behavior remains compatible; a partially implemented profile is never represented as applied; S lifecycle and close/apply races are correct; pending/executable changes cannot silently enter an existing session; failed/stale opens publish nothing false; metadata is host-owned and actual; admission seals and existing permission policy are preserved. Native/vendor refresh and Linux isolation behavior need separate evidence; mocks do not establish them.

## Handoff and board

Inspect the current DevKit board before claiming/creating tasks, preserve other agent ownership, and record a branch per reviewable slice. No H3 task/session IDs are invented here. H2a is not H2 completion; do not close an H2 or TEAM task merely because this plumbing compiles. Use normal MCP services for task updates and the verifier for any closure.

Use terminal git/gh per the person's preference; keep task/branch guards, no force-push and no automatic merge. No live model/account experiments, real workspace-profile mutation for demonstrations or messages to another session without the person's explicit authorization.

Suggested handoff: first inspect the accepted H2 state and existing local H2b/H2c work; implement H3a, then H3b–H3d in small PRs. Keep profile activation disabled until H4 can enforce the same snapshot. Record exact base commits, checks, runtime paths covered and remaining production limitations in each PR.

## Execution record

Bases: H1 #344/#345 and H2a #346 are merged. H2b is PR #347 (`harness/h2b-prepare`, base main) and H2c is PR #348 (`harness/h2c-confirm-apply`, stacked on #347): fresh branches cherry-picked from the earlier local commits (tree identical to the old `harness/h2c-apply`), both unreviewed and unmerged when H3 started. The old local branches are left untouched.

### H3a (branch `harness/h3a-snapshot`, stacked on #347)

`harness.ModelProfileRef`, `SnapshotRef`, `Snapshot` (+ `Clone`, `Equal`, `ExecutableDigest`, `ServingIsDefault`) and `harnessconfig.Provider`. Tests: default vs present-empty vs unavailable (corrupt W/S, vendor read error, no registry, removed group, nil provider), actual workspace/session scope and no child inheritance, detached copies, facts coverage and no fabricated facts, symlink retarget and order for the executable digest, stale reference, model vs harness separation. Mutation checks fail the matching tests. Mock-only: the provider is exercised through the same fake ports as H2b; no production registry or vendor-facts reader exists.

### H3b (branch `harness/h3b-pipeline`, stacked on H3a #349)

`agenthub.Service.Harness` + `harness_runtime.go`: `prepareLaunch`/`harnessGate`/`preflightHarness`/`publishHarnessLocked`, the optional `PolicyEndpoints` seam, the in-memory `appliedHarness` record and `HarnessApplied`. All five open sites (create, `resumeRuntime` with its Load/Open, rewind Open and Fork) use it. Tests (mock adapter, instrumented endpoints, real resolver snapshots): no harness, default compatibility, refusal before anything opens, unresolvable snapshot, policy staged before adapter open, revisions only when enforced, launch config only when profiled, failed open keeps the previous record, executable gate (moved W vs V-only vs fresh session), release seal vs reload, concurrent resume, workspace-less session, rewind. Mutation checks fail the matching tests.

Not covered / mock-only: the account-switch reopen shares `resumeRuntime` and is not separately exercised; the endpoint fake stands in for the H4 session host; no process, vendor or live account is involved. Durable applied state, S lifecycle and the status read model are H3c/H3d.

### H3c (branch `harness/h3c-lifecycle`, stacked on H3b #350)

Migration 55 (`agent_session_harness_applied`), `agenthub.HarnessAppliedStore` on `SQLStore`, durable `previousHarness`/`recordHarnessApplied` in the create, resume and rewind paths, `HarnessLifecycle` called by `CloseSession`, `ErrHarnessCleanupFailed`, `HarnessStore.ClearOverride`, the closed-session refusal inside `HarnessStore.Apply`, and the app adapter `harnessLifecycle`. Tests: durable reference follows reopen, restart still refuses a changed executable set, unreadable reference, failed durable write (resume and create), close removes S and the reference, reload/crash never run the cleanup, cleanup failure reported and retried, delete subtree, ClearOverride tombstones / idempotence / injected failure, close vs apply in both orders and under contention. Mutation checks fail the matching tests.

Decision: DevKit does not persist launch definitions. The permitted configuration is *not reconstructable*; a reopen that would change executables after W moved is refused and the person is pointed to a new session (the plan's allowed alternative). Mock-only / open: the container does not set `Harness`/`HarnessLifecycle` yet; the close-vs-`Apply` test against the real H2c service needs #348 in the base; a reload that is refused after a restart leaves the session without a runtime until a new session is opened (only the live-runtime reload is preflighted).
