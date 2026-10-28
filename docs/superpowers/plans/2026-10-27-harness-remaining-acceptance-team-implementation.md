# Remaining harness acceptance, activation and TEAM handoff

> Publication update, 2026-10-28: #375–#378 have merged; current main is `a4b64bcc`. Historical baseline/status rows below are preserved. The next focused R1 delivery is `2026-10-27-harness-rewind-finish-forward-implementation.md`; AP1 remains Partial and AP3/AP4 are not accepted. Recheck repository state before execution.

Date: 2026-10-27. Status: prepared, not executed. This is the ordered continuation after the F-C repair; it does not declare acceptance or activate production.

## Current evidence and boundaries

Fetched main: `eccc7c6c0e3ab268bb6fdf25e16e7ca9b84b0048` (#376 report present). #375 merged; AP2 focused Tasks/Git controls delivery is complete. #376 explicitly leaves AP1 Partial. #377 is open at `43f18027ebac55c7ab09b967973173f43550bbd4`; F-C repair is under review and its long-list layout finding is being fixed by another agent. Do not edit that agent's worktree or duplicate its fix. Verify updated heads/merge status before execution.

Read AGENTS.md and the relevant on-demand guides. Parent plans are the acceptance/activation plan, AP1 completion plan and F-C repair plan in this directory. Some are local worktree artifacts or on open docs PR #374, so transfer the actual files if absent on main; do not infer their contents from chat. Current integrated evidence on main takes precedence over old status rows. Work in separate worktrees and preserve unrelated local changes.

| Delivery | Depends on | Output and stop point |
| --- | --- | --- |
| R1: finish AP1 | #377 final head/fix available | Independent gap review + exact-SHA ledger, focused regression/fix PRs if needed |
| R2: AP3 runtime fixtures | Fixture discovery may begin during R1; acceptance needs an agreed candidate | Reproducible real-process protocol evidence for Claude/Codex/opencode |
| R3: AP3 native candidate | R1 clear; R2 fixture infrastructure usable | Isolated unmerged production-path candidate + actual native approval/recovery evidence |
| R4: AP4 activation | R1, R2, R3 evidence accepted; O-2 decision recorded | Focused production wiring PR, ordinary owner review/merge |
| R5: C1 TEAM snapshot binding | Harness accepted; inspect existing TEAM foundation first | Preflight + immutable package snapshot binding PR; no production dispatcher |

R2/R3 together are AP3, split here into delivery units. Do not describe them as additional product features. Later TEAM dispatcher, result acceptance and dashboard need their own scoped plans; they are not implemented by this document.

## R1 — close AP1 gaps honestly

### 1. Re-review #377 final head

Inspect the actual long-list layout correction in diagnosis and review states: bounded scroll, actions reachable at small window sizes and up to the maximum supported entries, text wrapping, focus/Cancel and normal editor isolation. Use a browser layout check where feasible; controller/SSR output alone does not prove viewport behavior. Native behavior remains R3.

Independently trace the F-C backend and UI implementation: UI-only identity at every owning boundary, exact server-derived removals, no permissive normal resolver, always-confirm widening, profile/registry/V/policy binding, CAS, cancelled/stale/replayed requests, ambiguous write outcomes, preserved S/runtime/applied refs/executable guards and truthful next-open behavior. Read tests and run focused regressions. Do not mark F-C accepted merely because its PR merged.

### 2. Finish the #376 review limits

Use its ledger as a concrete checklist, not a request to repeat the entire audit:

- Item 4: coordinator/reconcile and crash-between-commit-and-publish recovery; trace account-move reopen past prepareLaunch and replacement/late completion paths. Confirm endpoint incarnation checks apply at side effects, not only at preparation.
- Item 5: read the actual SQL rewind transaction and rollback boundaries; trace child/fork-at-create paths. Verify transcript cut and applied reference commit together, parent S is not inherited and failed staged launches leave no admitted endpoint.
- Item 6: complete public controls/error/DTO paths and SessionToolGroups frontend interactions not read in #376. Scope setters must refuse non-UI callers in Go.
- Item 7: review workspace budget model, policy setting and measurement attribution touched by the harness. Verify independent omitted/set/reset semantics, stale UI epochs, fixed exemptions and call/turn-start facts without historical relabelling. Limit accounts/vendors review to actual lifecycle dependencies.
- Item 8: use the independent #375 reviews already performed as evidence with their exact SHA/repros, then recheck later diffs. If those artifacts are unavailable to the receiving agent, review current Git canonical-path binding and Claim/Release serialization independently; do not invent an external sign-off.

A code path being read is not automatically inadequate evidence, and a test being run is not a substitute for tracing it. Add deterministic probes only where needed to establish a suspected interleaving or an otherwise unsupported invariant. Document both what was read and what was exercised. A reviewer cannot independently approve their own new fix; leave that row pending another reviewer.

### 3. Resolve remaining safety/product questions

O-1: investigate the file-restoring rewind interleaving identified in #376. Determine the actual file/transcript/checkpoint outcome on stale S; reproduce if the invariant is uncertain. If files can change on a refused rewind in a way that is unsafe or violates the product contract, deliver a focused fix and review it. Do not dismiss it merely as retryable, or promise transactional filesystem rollback without implementation evidence.

O-2: sessions created while the harness was inert have no applied reference. Inventory actual stored executable fields and launch/adoption rules. Write an explicit activation decision before R4. Proposed safety criterion: an old session must not silently gain a new workspace executable merely because its prior reference is missing. Any baseline/migration or new-session refusal must distinguish legacy vendor behavior from new workspace executables and preserve existing data. Do not invent historical facts, stamp current W as proof of prior approval, or solve this by enabling executable proposals. If an implementation change is necessary, isolate it and obtain owner review before R4.

#375 directory-swap limit: assess its actual threat boundary and whether current isolation/guards meet activation requirements. Keep it explicit. If activation relies on containing an agent that can exploit the remaining path-based execution race, treat the missing protection as a blocker and write a focused design/fix; do not silently equate the fixed alias re-resolution bug with descriptor-bound Git execution.

Update `docs/reviews/2026-10-27-harness-ap1-completion.md` and the canonical activation record. Preserve historical evidence, add reviewed SHA and item-level closure/limits. AP1 becomes complete only when every required item has independent evidence and no unresolved activation blockers. Stop for review of this evidence/fix delivery; do not activate production.

## R2 — real-runtime protocol evidence

Discover installed runtime versions and existing fixture infrastructure first. Use actual Claude/Codex/opencode processes under the supported launcher/jail, isolated HOME/config/data, synthetic workspace and local model/protocol fixtures. A stub executable or scripted in-process adapter is not a real-runtime acceptance pass.

Before sending prompts, prove the process can run without the person's credentials or a billed endpoint. Do not read/copy credential files, sign in, invoke real subscriptions or permit a fallback to provider endpoints. If a runtime needs an unavailable account/provider feature, record the precise blocker and continue independent fixtures; do not mark it unsupported or passed without evidence. Paid/account use requires separate explicit user authorization.

For each runtime, record exact versions, candidate SHA/diff, isolation, commands and sanitized outcomes for:

1. Default/no-profile launch compatibility and narrowed-profile discovery.
2. Hidden/cached direct tool call refusal before the real handler/gateway side effect.
3. Actual wire delegation for gate-owned harness.profile.propose, shared confirmation, and no accidental fixed launch pre-approval for newly enabled tools.
4. Live group enable/revoke and result budgets observed from the server; call-start snapshot under a mid-turn change where the runtime permits controlled reproduction.
5. Reload/restart plus failed-publication/recovery behavior available through the actual adapter path.
6. tools/list_changed delivery versus actual runtime discovery refresh, measured separately. If refresh is absent/unmeasured, retain DiscoveryReloadRequired and explicit Reload; do not claim automatic discovery success.

Use bounded sanitized protocol captures/counters, not full transcript bodies or secrets. Keep fixture-only transport configuration outside production flows. No second runner, fake session IDs or guard bypass. Store repeatable fixtures/evidence in a PR and list blocked runtime rows exactly. R2 is incomplete until mandatory real-runtime gates are satisfied.

## R3 — isolated native candidate and approval evidence

Build an unmerged candidate in its own worktree and disposable app data directory using the same production ComposeHarness path intended for R4. Do not enable the installed personal app or its vault/workspaces. Read the app's supported data-root setup before launching; if isolation cannot be established, stop the dependent native run and report why.

The candidate may contain the necessary production wiring for testing but is labelled acceptance-only. Keep the existing inert-main tripwire on main until R4. Disclose any candidate-only expected tripwire failure; do not delete a failing test and claim the candidate passed. Candidate SHA/diff and intended final startup path must be recorded.

Run the real desktop components connected to the shared Go hub:

- Locked startup and unlock in disposable data; scope selection without a selected default, focused Cancel, denial and approved enable.
- Group revoke, stale/late approval after session navigation, failed publication and reconcile with truthful saved/live states.
- Exact budget review, default-off auto-narrowing and always-confirm repair; F-C diagnosis plus long-list diagnosis/review, Cancel and approved repair.
- Reload/restart preserves the contracted durable state and invalidates old answerable approvals; no live session adopts another incarnation's state.
- Headless/nil-prompter/server checks fail closed, bridge remains loopback, server-mode sessions remain refused. Native and server results are separate evidence rows.

Native automation may observe/click only the isolated fixture. It is not authorized to answer an unrelated permission dialog or change a personal workspace. No need to use real paid agents to prove native dialogs; use supported fixture peers while retaining R2 real-process evidence separately.

Fix discovered bugs in focused changes, rebase/rebuild the candidate and rerun affected evidence. Evidence from an older candidate needs an explicit diff assessment; a relevant wiring/approval change requires rerunning the affected gate.

## R4 — production activation after evidence

Use the detailed AP4 checklist in `2026-10-27-harness-acceptance-activation-implementation.md`. The final PR must:

1. Register the two public harness tools by passing the existing port before inventory freezes.
2. Build and attach the actual assembly to the actual Agent Hub/session host before any listener/session/UI admits work; reorder the current early ServeUnix start as needed.
3. Fail startup closed and clean staged resources on any graph/attach/listener failure. No feature flag, second registry or Harness=nil fallback.
4. Replace the production-inert tripwire with meaningful positive startup-order, real-construction and failure-path coverage, not just inverted text matching.
5. Include accepted R1–R3 evidence and the O-2 decision. Verify no-profile/old-data/optional-module/corrupt-profile/locked-vault paths and existing account reopen behavior.
6. Preserve shutdown semantics, bounded metric draining and socket ownership; shutdown must not masquerade as explicit session Close.
7. Document rollback as code/deployment revert preserving profiles/S/policy/metrics/schema, never destructive reset or runtime fallback.

Run the parent's full validation: frontend tests/build, architecture, prescribed package race suite, `go test ./internal/... ./cmd/...`, server vet/build, current CI lint and pinned binding generation when needed. Review the exact final head and material candidate diff. Open PR with terminal gh, attach it to the current Codex task, and stop for normal human review/merge. No auto-merge or automatic release.

## R5 — TEAM C1 after harness acceptance

Continue the existing C1 design in `2026-10-26-harness-phase2-team-snapshot-implementation.md` and inspect `2026-10-26-team-harness-integration-implementation.md`, the managed-team plan and current code before writing a detailed patch plan. Do not assume all TEAM foundations remain unimplemented or replay old tasks.

The bounded C1 deliverable is:

- Host-owned intended fresh-child configuration plus actual child's HarnessSnapshotRef, through narrow interfaces/composition adapters.
- Preflight intended W/V before creation and actual child identity/scope/grants/mode/required groups/snapshot before enqueue eligibility. Missing groups return configuration-needed; no parent-S inheritance, automatic self-enable, account/model changes or shell fallback.
- Schema-versioned snapshot binding on immutable packages/dispatch inputs, separate from model profile. Configuration staleness is distinct from v2 task requirements digest. Rebuild through supported new package/attempt semantics; never overwrite immutable historical packages.
- Retain historical turn/call attribution from actual admission snapshots; no new collector or backfill from current settings.

Tests cover parent S isolation, absent required group, changed W/V during child open, same model/different harness, stale endpoint/package, revoked tools, config-read failure and package/lease/seal preservation. Preflight passing authorizes no result acceptance, push or merge.

Stop at a C1 PR. Production dispatch belongs to a later slice (including persisted operation IDs, task assignment, enqueue and recovery); result acceptance and the dashboard need their own reviewed implementation plans. Plan those from actual repository state after C1, rather than declaring a fixed number of unknown future PRs now.

## Execution and evidence rules

Each delivery records reviewed base/head SHA, authorship/independence, changed paths, commands/results, actual versus scripted process/native evidence, unresolved blockers and next step. Do not label a gate passed because a PR merged or CI passed. Missing board state on another machine does not block reading/review/local fixtures; use legitimate task/session ownership for guarded actions, never fabricated IDs or verifier bypasses.

This document authorizes planning only. Implement the chosen next delivery when instructed; approval to write this plan is not authorization to launch account-dependent runtimes, manipulate personal app data or merge activation.

## O-1 follow-up after #378

For the residual S-during-restore window, use the focused [bounded finish-forward plan](2026-10-27-harness-rewind-finish-forward-implementation.md). It preserves files-before-cut, limits retry to identified S movement, and retains truthful partial/uncertain recovery outcomes. This is an R1 follow-up, not approval to start activation.
