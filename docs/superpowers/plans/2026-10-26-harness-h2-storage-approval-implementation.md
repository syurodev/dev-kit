# H2 implementation: revisioned harness storage and approval-bound writes

Date: 2026-10-26. Status: **in progress; H2a delivered, H2b in review (see Execution record), H2c not started**.

## Objective and dependencies

Implement H2 of [Harness Phase 2 + TEAM Slice C](2026-10-26-harness-phase2-team-snapshot-implementation.md): store W/S with real revisions, prepare an exact effective before/after diff, ask through the existing host confirmation flow, then revalidate and atomically commit the approved change. No runtime behavior or agent mutation tool is enabled by this document.

References:

- [Subscription harness tuning spec](../specs/2026-10-26-subscription-harness-tuning-design.md), §4.
- [Phase 1b attribution and metrics](2026-10-26-harness-tuning-phase1b-usage-attribution-and-tool-metrics.md).
- [TEAM/harness handoff](2026-10-26-team-harness-integration-implementation.md).
- [Customization/recovery guide](../../agent-reference/agent-customization-recovery.md), [MCP contracts](../../agent-reference/mcp-tools.md).

PR344 H1 is merged at reviewed head `f932cf7e843ea0cd17113b6215556b55c98cbfb0`. The person has assigned its review fixes elsewhere. **Do not assume those fixes are available yet.** At this plan's preparation the latest queried PR list showed #344, with no fix PR observed. Before implementation fetch current main and identify the actual fix commit. Reuse the corrected `internal/harness`; do not edit the same resolver concurrently with its owner.

Entry checks for any H2 approval/classification integration:

1. Removing a deny that exposes an imported skill is `TierExecutable`, not only widening.
2. Retargeting a skill path changes the fingerprint of the resolved executable configuration.
3. Nil/empty server argument and env-key lists remain semantically identical after JSON persistence.
4. Retain other validated H1 fixes, including any registry fingerprint encoding corrections; confirm the full resolver tests and actual diff, not only these three test names.

Schema-only work may proceed independently if it does not expose writes or depend on unfixed semantics. Approval/application integration must wait for those checks. This plan does not authorize code by its author, live model calls, user configuration changes, cross-session messages or PR merges.

## Scope and PR boundaries

| Sub-slice | Deliverable | Activation boundary |
| --- | --- | --- |
| H2a | W/S persistence, CAS, atomic W+S writes, migration and repository tests | Internal only; no UI/MCP write path |
| H2b | Host-scoped proposal preparation, trusted facts, effective diff and stale checks | No public mutation endpoint |
| H2c | Existing confirmation integration, one-use approval binding and adversarial tests | Enable no agent tools until H3/H4/H5 are ready |

The receiving agent may combine small sub-slices if review remains practical. H3 owns runtime/OpenSpec snapshots; H4 owns live MCP filtering, budgets and snapshot publication; H5 owns profile editor, proposal transport and grant wiring. This plan supplies reusable service seams/tests for those owners. TEAM dispatcher, retention cleanup, Linux stop-proof validation and result acceptance stay separate.

## Ownership and seams

Read current AGENTS.md and relevant on-demand guides. Inspect current main after the H1 fix, not the older primary checkout used to prepare this document.

- `internal/harness`: pure resolver/classifier/fingerprint and value types; stdlib-only, no SQL, interaction, filesystem or identity imports. All H2 code calls this implementation.
- Storage: reuse existing migration/SQL repository patterns. `internal/storage` or the currently agreed repository owns SQL transactions; domain code depends on a store interface.
- Preferences: existing `AgentCustomization` V and `preferences.Service.SetAgentCustomization` revision checks remain authoritative. Do not copy V into a second independently writable profile store.
- Host/composition: resolves caller/session/workspace, canonical paths and vendor-defined environment facts; adapts existing customization validation and `interaction.Hub`.
- Confirmation: keep existing prompt queue, caller identity and recovery behavior. Do not create another consent queue or use the D1 scoped-db.query approval store for harness changes.
- MCP/runtime/UI: no alternate runner, no direct profile-store writes from a tool or React, no bypass of capability/confirmation gates.

Inspect actual file names at implementation time: `internal/preferences/preferences.go`, `sqlrepository.go`, `internal/interaction/hub.go`, `internal/agenthub/environment.go`/`customization.go`, `internal/harness/*`, storage migration runner and composition wiring. These are seams to extend, not a requirement to put all H2 logic into one of them.

## H2a — Persistent records and transaction contract

1. Add local `workspace_harness_profiles` and `session_harness_overrides` using the current migration number. Persist schema version, host identity/scope, real monotonically increasing revisions, JSON and required timestamps. Profiles store no credentials or env values; environment keys/references only. Workspace instructions remain in their existing table.
2. Store/reload H1's absent-vs-present-empty fields correctly. A missing W/S is an explicit absent result, not revision 0 and not an empty document. Unknown stored schema/invalid JSON is an error, never fallback to default-on tools.
3. Define expected-version tokens that distinguish absence and a concrete revision. Creation uses insert-if-absent; update/reset/delete use CAS. Do not allow delete/recreate to reuse an old revision token; preserve a monotonic revision/tombstone or equivalent generation mechanism. Document how its token maps to H1's nullable revisions.
4. Resolve workspace/session existence and association through their owning services. Use foreign keys only where the actual schema supports them; do not invent a session/workspace relation or silently transfer an override when a session changes scope.
5. Provide one transaction for save-to-workspace: write W and S for the requesting session or neither. Session-only changes write S alone. Include both expected versions and scope conditions in that transaction. Do not implement W-success/S-failure as a successful partial change.
6. Expose low-level SQL methods only to the owning service/composition and tests. All user-facing edits, reset/delete and later override removal go through the service's validation/CAS contract.
7. Define S cleanup for future lifecycle integration: explicit close/delete removes S; reload, resume, rewind continuation, account movement and process teardown do not. H2 supplies the scoped delete/CAS operation; H3 owns hooking it into the exact lifecycle path. Avoid placing cleanup in a generic CloseRuntime callback.

Tests: fresh/upgrade/idempotent migration, JSON round-trip, missing/unknown schema, create/update/delete races, deletion/recreation ABA, cross-workspace session rejection, SQL failure between W and S, reopen persistence, and preservation of earlier usage/TEAM rows. Test rollback by fault injection, not just the happy path.

## H2b — Proposal identity, trusted inputs and effective diff

1. Derive the requesting session/workspace from authenticated host context. Agent proposals target only that session's workspace. A shared-terminal caller without a trustworthy session must not provide an arbitrary workspace. A UI caller uses the UI's current selected workspace plus explicit target-session validation; neither caller provides authoritative grants, V revisions or environment data.
2. Fetch current V/W/S and current trusted vendor-defined configuration; resolve canonical paths through existing validation. Deep-copy proposal/snapshot values so mutation of request slices/maps cannot alter a prepared diff or its later comparison.
3. Today's `EnvironmentReport` does not contain full server command/argument facts. Supply a host-only bounded facts adapter or mark the required facts unavailable. A proposal whose classification/diff/fingerprint depends on missing facts must fail closed, especially deny removal, fallback executables and executable edits. Do not treat missing server definitions as an empty environment or run a server to discover its definition. Pure safe proposals that need none of those facts need not wait for unrelated discovery work.
4. Apply the candidate patch to copies, then use corrected H1 Resolve/Classify for both before and after. Validate target, schema, known groups, exemptions, body/list bounds and existing customization rules. Do not classify the patch itself or create another merge algorithm.
5. Build a Go-authored confirmation view with actual effective changes and scope. Label the agent rationale as untrusted text; it never controls command, target, tier or UI markup. Show executable command/args/resolved paths/env names only in the authorized view. Audit metadata and errors contain bounded codes/IDs/revisions/tier, not those definitions or rationale text.
6. Bind the proposal to exact caller/session/workspace/vendor identity, target choice, normalized candidate patch, schema and policy revisions, V/W/S versions, before/after fingerprints and canonical effective diff. Include complete resolved-path/executable facts required by H1; do not rely on a revision-only hash when external resolution can change.
7. Prefer a bounded in-memory pending proposal record with an opaque host-generated ID. Its ID is a lookup key, not an approval. Do not expose an `approved=true`, caller-supplied fingerprint or self-issued receipt path. Define cancellation/single-use semantics without introducing a new product timeout policy.

Tests: foreign caller/target, moved/deleted session/workspace, unavailable/incomplete vendor facts, symlink retarget between reads, mutation of caller-owned values after Prepare, effective deny removal, server fallback and canonical equivalent JSON representations.

## H2c — Confirm, revalidate and commit

Use this ordering; no DB transaction or configuration write lock stays held while the person answers:

1. Under a short host validation boundary, capture inputs and prepare the immutable proposal/diff. Release the boundary.
2. Ask through existing `interaction.Hub` with no allow-always. Prefer its existing non-rememberable confirmation semantics; if adding fields is necessary, keep old callers unchanged and test the real bridge/card integration before exposing them. Headless/server/nil prompt handlers must fail closed before mutation.
3. On a positive answer, reacquire the validation boundary and re-resolve caller scope, session/workspace lifecycle, approval policy, V/W/S, canonical paths and relevant vendor facts. Recompute the effective before and after and compare exactly with what was shown. Do not trust a UI-returned diff or a fingerprint submitted by an agent.
4. If W moved, return `harness.profileConflict`. If V/S/vendor facts, policy, caller/scope or either effective diff changed, refuse with a stale/denied error as appropriate. Do not silently merge, downgrade the tier or carry approval over to another candidate.
5. Serialize all DevKit writers that can affect those checks, including existing V customization edits and future W/S UI paths, through a consistent lock/order or a host revision-reservation mechanism. A mutex used by proposals alone does not protect against the old V setter. Do not invert locks against SQLite/Agent Hub callbacks. External vendor files are outside the transaction: guarantee only the fresh facts observed/revalidated, as the spec states.
6. Commit with expected W/S tokens in one short store transaction. The confirmation binding is single-use; duplicate completion cannot cause a second write. Define failure behavior so DB errors, cancellation and retries cannot apply an old approval to changed inputs. Re-read state for a fresh proposal when needed.
7. Return distinct stored/applied state. H2 has no active runtime publisher, so a committed profile is not described as already applied to a running session. H4 later owns commit-to-memory coherence and revocation delivery; do not claim immediate live revocation from this internal service alone.

Confirmation tiers follow the existing spec: narrowing asks unless the person enabled the UI-only auto-narrowing setting; widening always asks; executable proposals require UI opt-in, one executable change per proposal, and the approve button must not be focused. Executable changes are new-session-only; store that application intent for H3 and do not implement reload activation here. Accounts, sandbox, vault, approval policy and auto-narrowing settings never become fields an agent can patch.

If the full exact executable diff cannot be presented with today's prompt component, keep executable proposals unavailable with a clear reason; do not squeeze them into a truncated Summary and call that approval. H5 may supply the structured view. An optional UI setting alone must not make headless agent mutations possible: tests must show the service preserves the spec's fail-closed server/missing-handler contract, including auto-narrowing.

## Recovery and errors

- Restart cancels pending proposals/approval receipts. Pending-interaction recovery keeps metadata only; an old recovered question is never answerable and cannot authorize a write. A new proposal re-resolves all inputs.
- Cancellation, deny, prompt failure or stale inputs leave W/S unchanged. Remove or invalidate the pending record so a later callback cannot revive it.
- Publish new errors at the layer that exposes them through bridge/MCP, following `keptCodes`/intentional-redaction rules. Use the shared `harness.*` codes, fixed host-authored messages and typed conflict/stale errors; do not leak SQL, paths, raw JSON, commands or vendor error text.
- Validate no allow-always cache or D1 db.query scoped approval can approve a harness mutation. A profile or override is never permission for a tool call.

## Required adversarial matrix

| Scenario | Required result |
| --- | --- |
| V changes while dialog is open, including through existing Settings setter | Stale; no W/S write |
| W CAS loses or profile is deleted/recreated | Conflict; no partial write |
| S changes/revokes or closes with session | Stale/denied; no resurrection |
| Vendor command/fallback or resolved skill target changes | Stale or facts-unavailable; never use old approval |
| Pure serialization nil/empty difference | Same effective shape; no false executable tier |
| Mixed narrowing + executable patch | Strictest tier and executable opt-in |
| Duplicate answer/apply or changed patch with same proposal ID | At most the originally bound single commit |
| Missing/headless prompt handler, denial, malformed answer | Refused; store unchanged |
| Failure writing S after W | Whole transaction rolled back |
| Restart with pending question or receipt | No writes/replay; new proposal required |
| No profile, malformed stored profile, deleted tool/group | Defaults only for absence; malformed data refused with repair guidance |
| Ordinary snapshot/profile write | No lease release, model invocation, account rotation or prompt send |

## Validation and definition of done

Run tests for the new service/store plus corrected H1 resolver, relevant preferences/interaction tests and meaningful race/fault tests. Run architecture checks for every new package and `go test ./internal/... ./cmd/...` for the repository Go gate. Follow current lint/vet requirements. Composition checks require real frontend assets; any AppService change requires regenerated committed bindings, and any UI change requires `bun test --isolate` and `bun run build`. No GUI-scaffolding test claim or live vendor claim from mocks.

Done for H2 means migration/CAS and atomic W+S semantics work, the real V writer participates in the stale-check contract, trusted facts distinguish unavailable from empty, exact approval binding survives races, and no mutation is public without its security checks. Clearly list any intentionally unavailable executable proposal path. H3/H4/H5 and C1 are still separate; H2's successful unit tests do not enable managed dispatch or close TEAM ownership/package tasks.

## Handoff and board

Inspect existing harness tasks before creating duplicates; H1's known task is `OPT-e3df87`, not an H2 claim. Create/claim suitable H2 subtasks through DevKit MCP under the current plan and preserve other owners. This document invents no task/session IDs and makes no board changes. Record branch/progress and acceptance gaps normally; never bypass task/branch guards or the close verifier.

Use terminal git/gh per the person's preference. Start from current main with reviewed H1 fixes, keep PRs small, include the migration/approval/race evidence and open the PR for review. Do not force-push, self-merge, run live models, rotate accounts or change the person's workspace profile to demonstrate the feature. A complete UI/live rollout requires the later activation prerequisites.

Suggested brief: implement H2a first, then H2b/H2c against the corrected single harness resolver. Keep public mutation transport disabled and preserve no-profile behavior. Report the actual H1 fix commit and any vendor-facts or confirmation-UI limitation before calling H2 complete.

## Execution record

H1 fixes used (merged in #345, `95f2f83f`, verified by running their three tests on main before starting): a denied imported skill is outside the executable set so un-denying is `TierExecutable`; the fingerprint hashes canonical skill-folder and deny targets; server shapes treat nil and empty `Args`/`EnvKeys` as equal. H2a also fixes the registry fingerprint encoding (name and exemption were joined into one string).

### H2a (branch `harness/h2a-storage`, task OPT-984269)

Migration 54, `storage.HarnessStore`, and the persistence contract types in `internal/harness/store.go`. See the "Harness profile storage (H2a)" section of the customization guide for the token, scope and corruption rules. Verified by fresh/upgrade/idempotent migration, create/update/delete CAS, delete/recreate ABA, 16-way concurrent create and update races, W+S rollback by SQL trigger and in-transaction hook, scope and cascade, reopen, corrupt-document and mutation checks.

Not in H2a: the service, proposals, confirmation, the V-writer participation, vendor facts, and any lifecycle hook for S cleanup (H3).

### H2b (branch `harness/h2b-proposals`, stacked on H2a)

`internal/harnessconfig` (ports, patch normalization, `Prepare`/`Cancel`, view, binding) with an architecture rule, and new codes in `internal/harness` (`profileDenied`, `factsUnavailable`, `notAvailable`, `noChange`). Tests use the real `storage.HarnessStore`. Covered: agent cannot choose workspace/session, UI session must belong to the workspace, terminal/remote/no caller denied, gone/moved scope, no-op proposals, vendor facts gate (needed vs not, unreadable), deny removal exposing a vendor server, server fallback, mixed tiers, narrowing vs auto-narrowing vs widening, per-vendor judgement of a workspace change, binding moves with V/W/S/policy/vendor facts/patch, equivalent representations, caller-owned values after Prepare, symlink retarget, corrupt/obsolete stored profile, validator failure, rationale handling, cancel ownership and the pending bound. Mutation checks fail the matching tests.

Not in H2b: confirmation, revalidation and the single commit (H2c); the production adapters (`Vendors` with real facts, `Directory`, `Validator`, `Policy`) and a production registry source, which H2c/H4 own. No production code constructs the service yet.
