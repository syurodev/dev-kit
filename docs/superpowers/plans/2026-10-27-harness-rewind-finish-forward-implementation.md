# O-1: bounded finish-forward after a files rewind loses its S reservation

> Publication update, 2026-10-28: #378 has merged; current main is `a4b64bcc`. Implement this focused finish-forward follow-up from current main, not by modifying the now-merged PR branch. The earlier baseline below is historical. AP1 remains Partial; production stays inert.

Date: 2026-10-27. Status: prepared, not implemented. Scope: the residual O-1 window in PR #378; production stays inert.

## Baseline and goal

PR #378 is open at `44dd9d70c1a14e64fbe4855e8374ef522ff85e88`. Its early `checkServingLaunch` closes the stale-before-restore case, but S can still commit during `applyRestoreHeld`. The files then change and step 7 refuses the transcript/reference commit. Fetch current main and #378 before implementation. If the author is working on that branch, coordinate through the user and use a separate worktree; do not overwrite their uncommitted work.

Chosen design: restore files once, then finish forward through a bounded re-preparation/fork when publication is refused specifically because S advanced. Keep the documented files-before-cut ordering. Do not commit the cut before restore, automatically undo files, or repeat the restore to hide a partial failure.

Read AGENTS.md and relevant vendors/sessions, customization/recovery, coordination, MCP/public error and sandbox guides. Trace `rewind.go`, `harness_runtime.go`, `serving_live.go`, checkpoint restore/safety helpers, SQL RewindSession, bridge/frontend rewind callers before editing. This is a focused backend/recovery fix, not AP3/AP4, account switching redesign or executable proposals.

## State contract

| Phase | Files | Transcript/applied reference | Permitted next action |
| --- | --- | --- | --- |
| Prepared/staged | untouched | unchanged | Existing early checks; refuse safely if stale |
| Restore completed | requested restore completed exactly once | unchanged | Attempt cut/publication; retry only eligible S staleness |
| Retrying | do not restore or snapshot again | unchanged | Dispose abandoned fork, re-prepare and fork the same rewind target |
| Committed | restored | cut + new vendor ID + applied reference committed in one SQL transaction | Publish/notify exactly once; final cleanup |
| Partial terminal outcome | restored; do not claim untouched | no cut committed | Truthful fixed public message + retained safety recovery |
| Ambiguous SQL outcome | restored | not yet proven | Read back using existing durable identities; never blindly replay cut/fork cleanup |

A restore call that errors may already have its own partial-write contract. Inspect and preserve that behavior; this plan does not assume an error means no file changed. New finish-forward retry is for a successfully completed restore, not a retry mechanism for failed file writes.

## 1. Reproduce the actual residual window first

Extend the real Git + SQL rewind fixture from #378. Insert a deterministic barrier inside the existing restore ownership path after the stale-before-restore check, while restore is in progress, and before final publication. Use an injected fixture collaborator or narrowly scoped test seam; no production alternate runner and no sleeps as scheduling proof.

Advance S through the actual coordinator/store service path while blocked, then release restore. On the baseline prove: restore ran, selected file state changed, transcript remains uncut, publication refused. Avoid merely calling the stale check at fork-open again. Record when a test uses an epoch-only seam versus an actual committed S edit; the primary acceptance case must exercise the committed S path.

## 2. Classify retry eligibility under the coordinator

`ErrHarnessLaunchStale` currently conflates S epoch changes, closing/closePending and endpoint loss/replacement. Do not retry by `errors.Is(err, ErrHarnessLaunchStale)` alone.

Introduce a small internal structured refusal/reason or equivalent coordinator-owned decision that distinguishes:

- S committed since the reservation, with the session still the same eligible lifecycle/operation: candidate for retry.
- Close/closePending, deletion, cancellation, another launch/endpoint replacing this operation, unsupported endpoint or unreadable facts: terminal for this rewind.

The classification must be captured at the refused publish boundary or revalidated atomically against a host-owned operation/lifecycle identity. A later read of epoch alone is not proof: S commits and lifecycle replacement can both move it. If the existing state cannot distinguish them, add the minimal internal generation/token needed; do not interpret a replacement endpoint as permission to close it and reopen this rewind.

Preserve lock order. No vendor fork, file restore, process close or wait under the coordinator lock. Do not broaden the stale exception into ignoring the publication fence.

## 3. Refactor the rewind into one restore plus bounded completion attempts

Keep initial preflight before closing the old runtime. Track explicit restore-completed and cut-committed states. Preserve the original held plan, cut sequence/message IDs, source vendor conversation, overwrite consent and safetyID across retries.

After successful restore:

1. Attempt the existing SQL cut/reference and runtime publication under the normal coordinator boundary.
2. If refused before any cut commit specifically for eligible S movement, dispose only this attempt's staged fork/runtime/endpoint using existing incarnation ownership. Ensure every failure path closes staged endpoints even if no runtime was returned.
3. Revalidate session existence, status, operation ownership and cancellation. Read the original previous applied reference; an uncommitted fork is never the new adoption baseline.
4. Re-prepare through the existing launch resolver/gate using current permitted facts, then fork the **same original vendor conversation and rewind target**. Preserve supported vendor fallback semantics; do not fork from the abandoned attempt or send a prompt.
5. Skip restore and safety snapshot creation. Attempt cut/publication again with the new launch reference.

Use the existing `launchAttempts = 3` as the total budget for finish-forward launch rounds, including the initial round. Account explicitly for `prepareLaunch`'s existing internal stale retry loop so nested retries do not silently multiply the intended budget. Vendor message fallback candidates remain their own existing finite mechanism; auth/not-installed failures still stop immediately. Document and test the actual upper bound for launches/forks.

Re-preparation may see newer W/V. It must pass the normal executable-adoption, scope/grant, sandbox/account and endpoint rules; never freeze stale permissions or bypass a gate to force success. If safe re-preparation is impossible, return the partial outcome. No account migration, group self-enable, policy changes or parent-S inheritance.

Keep the safety row pending across eligible retries; create it exactly once. Success updates it with the cut transaction exactly as before. Do not delete the old source vendor session until the final cut/reference commit is known successful. Attach updates, publish runtime ownership, usage facts and emit OnSessionRewound once for the winning attempt only. Abandoned attempts must not append transcript data or be counted as admitted user turns.

## 4. Truthful failure, cancellation and crash behavior

After files have restored, exhaustion, Close, cancellation, denied launch, fork failure or SQL failure cannot return the generic implication "nothing changed". Deliver a fixed public partial-rewind refusal that says files were restored and the conversation was not rewound **only when that non-commit is proven**. Direct the person to the existing safety recovery UI, not an automatic repeat of the restore.

Trace the actual Wails/bridge/frontend error path. Prefer a typed internal partial error with a published fixed public code/message if that existing path can communicate the required state. If structured result state is necessary, update every consumer so partial is never treated as success or triggers a success-only UI refresh. Tests must cover what the user actually receives, not just `errors.Is` in Go. Do not leak raw git/vendor errors, prompts, paths or secret profile contents in the new error/audit metadata. Publish new codes in bridge.keptCodes or document deliberate redaction.

Preserve the usable safety snapshot after terminal partial failure. Cleanup and safety bookkeeping use a bounded host cleanup context when the caller is cancelled; do not make recovery depend on an already-dead request context. Mark the safety row failed/partial using the supported contract, retain its pre-restore tree and ensure the existing recovery UI can find it. If persistence fails, surface/report the bookkeeping failure without claiming cleanup succeeded.

A crash during retry keeps the pending safety record and original before-tree so restart can show uncertainty. Never restore old answerable permissions. Any fresh native approval needed by recovery stays subject to existing gates.

Do not retry a failed/ambiguous RewindSession transaction as if it were a pre-commit S refusal. Inspect the SQL store contract and existing read-back facilities. Where commit ambiguity is possible, establish cut/vendor ID/reference status before destructive cleanup; otherwise report uncertainty and retain recovery state. No fake success, duplicate cut, source deletion or deletion of a fork that may already be the durable winner.

## 5. Regression matrix

Use real SQL/checkpoints and fixture Git repositories; scripted vendor processes/answers only. No personal files, credentials or billed calls.

- S commits during restore once: restore count one, safety snapshot count one; retry succeeds; expected file contents and transcript cut; stored vendor ID/reference and live endpoint all refer to final attempt.
- S commits on every completion round: bounded attempts, no additional restore/cut, accurate terminal partial state, retained safety recovery.
- Early stale at fork/check: original behavior remains safe before file changes; conversation-only and code-only modes retain their contracts.
- Close/delete or unrelated endpoint replacement between restore and retry: no revival, no stealing/closing the replacement endpoint, no automatic second fork. Cancellation likewise stops boundedly.
- W/V changes before retry to an unresolvable or executable-adopting configuration: gate refuses, no executable launch, partial result is truthful.
- Fork failure, endpoint staging failure and failure after an endpoint but before a runtime: no orphan resources; original vendor source remains until success.
- SQL transaction rollback: transcript/vendor ID/reference remain together; partial result keeps file/safety facts accurate. Exercise ambiguous commit handling if the owning store can produce it.
- Crash/restart during retry: pending safety evidence survives, no automatic replay or old permission resurrection.
- Winning attempt: success event/settings/usage publication once; history attribution is not rewritten by retries.
- Public bridge/frontend: partial versus unknown versus success are distinct, sanitized and recoverable. No automatic retry button that repeats file restore without a new user decision.

Add only tests that prove these distinct invariants. Read and retain #378's existing preflight/task callback regressions.

## 6. Delivery, validation and stop point

Implement as focused commits after #378's actual state is checked: append to its branch only if coordinated with its owner, otherwise use a separate dependent PR with an explicit base. Do not reimplement its other fixes.

Run affected tests with `-race`, including agenthub/app/bridge and any frontend consumer changes; then repository full Go package tests, architecture, frontend tests/build when affected, server vet/build and CI lint. Build actual frontend assets before architecture/embed checks. Regenerate bindings using the pinned Wails version if AppService/DTO bindings change. No test weakening or global Git changes.

Update the rewind sequence comments and #376/#378 ledger with exact fixed behavior, retry bounds, final SHA and any remaining partial/uncertain outcomes. Do not call filesystem + SQL atomic: finish-forward is bounded recovery, not a transaction spanning both. Preserve honest failure states.

Open/update PR using terminal gh and attach it to the Codex task; do not auto-merge. Stop for independent review. AP1 stays Partial until this fix and the other required rows have independent evidence. R2/R3/AP4 and TEAM work remain outside this delivery.
