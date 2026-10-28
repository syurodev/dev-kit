# R1b independent review: PR #379 (O-1 finish-forward) and #376 ledger items 4-8

Date: 2026-10-28. Branch `r1b-review` (local only; nothing pushed, no PR, no PR comments).

- Reviewed base: `8c5e64d097540173874b429eb9737196fab44116` (origin/main after #381). The coordinator's first brief named `65cde9b5`;
  main moved to `8c5e64d0` with #381 (idle-release setting) while the review was running. #381 is out of scope; the only lines of it that
  touch the reviewed files are one `s.markUsedLocked(...)` in `rewindRun.publishRuntime` and one `ErrIdleReleaseInvalid` mapping in
  `internal/bridge/errors.go`, both read and judged harmless to this review.
- PR #379 merge SHA: `eca9c10ff513215079fa04ee6ad24c8cce0ea9df` (commits `7e4dbe98` backend, `6f182c85` frontend, `392548e4` docs).
- Reviewer: **another instance of the same model family** (an AI coding session, "Claude Sonnet 5.5"). It did not write #379, H4c, #363,
  #367, #353, H5a/H5b, F-C or #375. It is not a human reviewer and this is not a human sign-off; the coordinator and owner decide what it counts for.
- Production stayed inert: no production file was changed. No real vendor, account, paid request, workspace, vault or credential file was used.
  The only repo change besides this report is a test (`internal/app/harness_r1b_probe_test.go`), commit `14a90949`,
  **written by the reviewer, pending another reviewer**.
- Machine note: the first run of this task was killed by an out-of-memory event, and the coordinator later killed a test process that had reached
  16 GB. Both are explained under finding F2 (a reviewer mutation made a retry loop unbounded). All later runs used `-p 1`, `GOMEMLIMIT=2GiB`,
  hard `-test.timeout` and one pattern at a time; no process was left running.

## Verdict in a few lines

- No defect found in the *new* code of #379 that I could demonstrate. The classification (S vs Close/lifecycle/endpoint), the shared launch budget,
  the read-back of an ambiguous commit and the bridge/frontend outcome sorting hold up under reading and four mutations.
- One honesty gap that #379 does not cover (F1, medium, **read only, not reproduced**): a file restore that fails *part-way* inside a rewind is reported as
  a plain refusal and the rewind's safety row is marked `failed`, although files may have changed.
- One test-hygiene finding (F2, medium for developers, none for production): the budget tests have no cap, so a regression in the budget fails as a
  non-terminating, memory-hungry test (observed, 16 GB) rather than as an assertion.
- Three low items (F3-F5) and some limits. AP1 stays Partial; nothing here is "accepted".

## Files read

`internal/agenthub/{rewind.go (all), rewind_store.go (all), serving_live.go (lines 60-540), harness_runtime.go (60-290), recovery.go (100-160),
serving_status.go (40-160), checkpoint_restore.go (290-360), account_switch.go (295-390), service.go (2720-2860)}`,
`internal/checkpoint/restore.go (175-300)`, `internal/bridge/errors.go` (#379 and #381 hunks, `rewindOutcomeRefusal`, `keptCodes`),
`internal/harnessconfig/apply.go` (`commit`, read-back), `internal/app/{harness_public.go (requireUI, uiSession, all setters), harness_public_budgets.go, wiring.go (agentLiveness)}`,
`internal/mcp/git_repo.go` (all), `internal/tasks/{transition.go (Claim/Release/writeClaim), service.go (mutate)}`,
`frontend/src/modules/agenthub/{rewind.ts, surfaces/RewindDialog.tsx (diff), summary/SessionToolGroups.tsx, summary/toolGroupsController.ts}`,
`frontend/src/workbench/workspaceBudgetModel.ts (120-204)`, `internal/harnessconfig/patch.go (normalizeBudgetEdits, applyBudgetEdits start)`,
tests: `harness_rewind_files_test.go` (rig and the S-move and budget tests), `internal/app/harness_public_test.go` (non-window test), `harness_stale_repair_test.go`.
The ledger (`2026-10-27-harness-ap1-completion.md`), the R1 plan section and `harness-composition-activation.md` were read for scope; current code wins over them.

## Commands and results (read-vs-exercised is per item below)

| Command | Result |
| --- | --- |
| `go test -race -count=1 -timeout 30m ./internal/agenthub/ -run 'Rewind\|SChange\|Ambiguous\|Budget\|LostEndpoint\|Cancelled\|CrashDuring\|LaunchGate\|CannotBeStaged'` (the exact pattern in the brief) | ok, 65.9 s, no race report (run once, before the memory problems) |
| `go test -count=1 ./internal/bridge/ ./internal/app/ -run 'Rewind\|Partial\|Unproven\|Unfinished\|CommittedSChange'` | ok, ok |
| `cd frontend && bun install --frozen-lockfile && bun test --isolate src/modules/agenthub/rewind.test.ts` | 16 pass, 0 fail |
| `go test -p 1 ./internal/mcp/ -run <13 live-publication/reconcile/stale-launch tests>` | ok |
| `go test -p 1 ./internal/harnessconfig/` (whole package) | ok |
| `go test -p 1 -v ./internal/app/ -run 'TestACrashBetween\|TestADeadRuntime\|TestCloseTombstones\|TestDeleteRemoves\|TestShutdownIs\|TestMeasurementUses\|TestResumeAfterRestart\|TestAReloadThat\|TestAChildGets\|TestARealApproval\|TestARewindWhose'` | 13 PASS |
| `go test -p 1 -race ./internal/agenthub/ -run 'TestAccountMoveSendsAStaleResume\|TestLaunchStaleReasons'` | ok |
| `go test -p 1 -race ./internal/tasks/ -run 'TestTwoConcurrentClaims\|TestAReleaseDoesNotErase\|TestAnAgentDoesNotRelease\|TestClaimRefusesALive\|TestAssignRefuses\|TestOnChangedRuns'` | ok |
| `go test -p 1 ./internal/mcp/ -run 'TestALinkRepointed\|Git'`, `... -run 'Budget\|Metrics\|Cutter\|Cut'` and `./internal/app/ -run Budget` | ok |
| `bun test --isolate` on `toolGroupsController.test.ts`, `budgetsController.test.ts` | 28 pass |
| my probe `TestNoBudgetOrScopeSetterAnswersACallerThatIsNotTheWindow` | passes; **fails when `requireUI` is removed from `PrepareBudgetEdits`** (the agent then received a review, `got <nil>`), tree restored |

Not run: the full `go test ./internal/... ./cmd/...`, `bun run build`, `golangci-lint`, `go test ./internal/arch/`, anything with `-race` over a whole package.
(The memory budget did not allow it; CI and the #379 PR report these, which I did not re-verify.)

## Mutations on #379 (each reverted at once; `git status` clean afterwards)

| # | Mutation in `internal/agenthub/rewind.go` | Outcome |
| --- | --- | --- |
| M1 | `canFinishForward` stops checking `stale.reason == launchStaleEpoch` | **caught**, but only by `TestAnSChangeTogetherWithALostEndpointIsNotRetriedAsS`. `TestACloseBetweenRetries...` and `TestRefusalsOtherThanSAreNotRetried` still pass because the session-status read and `ctx.Err()` are independent backstops (not a defect; see F5). |
| M1c | accept `launchStaleLifecycle` as retryable **and** drop the durable `Status != Closed` check | **caught** by `TestACloseBetweenRetriesEndsTheRewindWithoutAnotherAttempt` |
| M2 | `cutOutcome`: a failed read-back counts as `cutNotCommitted` (so the fork would be deleted) | **caught** by `TestAnAmbiguousCutCommitIsReadBackBeforeAnythingIsDeleted` and `TestAnUnprovableCutOfAConversationOnlyRewindClaimsNoFileChange` |
| M3 | each `openForked` gets a fresh budget of 3 instead of the run's shared `launchBudget` | **caught, but not as an assertion**: `TestRepeatedSChangesAreRetriedAFiniteNumberOfTimes` never terminates (60 s `-test.timeout` hit) and memory grows without bound. The coordinator killed one such process at 16 GB, about 300 MB/s. Recorded as "caught: the loop became unbounded and exhausted memory". See F2. |

## #379 review against the brief's list

Read and exercised unless marked read-only.

- **Retry classification.** `launchStaleLocked` decides the reason under the coordinator in a fixed order (Close in progress or pending, lifecycle, endpoint, S last), so S never
  masks a Close or a lost endpoint; `stageServing` and the reservation carry the lifecycle. `canFinishForward` then needs reason == epoch, a live caller context, budget left, and a
  durable session that is not closed. Exercised (M1, M1c, tests above). No misclassification found. One subtlety I checked and found sound: the stale reason is decided *before* `commit()`
  runs, so a retry never follows a refusal that happened after a write.
- **Budget.** `prepareLaunchWithin` decrements before each preparation, re-prepares only while budget remains, and refuses at 0 with `ErrHarnessLaunchStale`. A rewind passes one counter
  through `openForked`, so at most 3 launches per rewind; forks <= 3 x (1 + fallbacks). Exercised (M3 proves the counter is what bounds it). I found no way to exceed it by reading. The
  vendor fallback loop in `openForked` is separately finite.
- **Ambiguous commit.** Only `tx.Commit` failure yields `ErrRewindCommitUnknown`; every earlier failure is a proven rollback (database/sql rolls the transaction back on a cancelled context).
  `cutOutcome` reads the session's vendor id (written by the same transaction), else the safety row, on a host context; anything unreadable or unmatched is unknown, and the unknown path
  closes the fork, deletes neither fork nor source, and leaves the safety row pending. A proven non-commit with files restored becomes `RewindPartialError`, a proven commit
  is finished as success (`publishCommitted`). I looked for a path that deletes the possible winner: `discardFork` is reached only after `cutNotCommitted`, a pre-commit stale refusal, or a restore failure. None found. Exercised (M2).
- **Cleanup on a dead caller context.** Settling the safety row, discarding the fork and the read-back use `hostContext` (caller values, no cancellation, 15 s). Exercised
  (`TestACancelledCallerStill...`). Remaining uses of the caller context after the point of no return: see F3.
- **Lock order.** The only nesting is coordinator `c.mu` -> `Service.mu` (`publishRuntime`, `noteServedFacts`), consistent with the documented order. Under `c.mu`, `commit` does a local
  SQL transaction (`RewindSession`) and map writes: no vendor call, no callback (the documented O-4 allowance). `discardFork`, `closeFork`, `checkServingLaunch` and the retry preparation all run
  outside the coordinator. I grepped the callers of the `c.mu`-taking readers (`servedFor`, `servingStatus`, `ReplacementGuidanceFor`) and found none under `Service.mu`; a full proof that nothing
  holds `Service.mu` while calling them was not done (read-only, spot check).
- **Races.** `-race` run above is clean for that pattern. The shared `launchBudget` is touched by one goroutine (the rewind), so no race.
- **Messages.** `rewindOutcomeRefusal` is first in `publicError`, fixed sentences, no cause text, codes published in `keptCodes`. The conversation-only unknown sentence says "No files were changed",
  which is true only because that branch is reached when `files == nil` (no restore ran in this run). I checked every post-restore return in `run`: all are partial or unknown. The safety row's
  `Detail` holds the cause text but `CheckpointSummary` does not carry `Detail` (grep), so it does not reach the window.
- **Frontend.** `rewindFailure` keys on the published code; partial/unknown set `outcome`, disable confirm, hide the stale plan and never call `onRewound`. Exercised: the 16 logic tests.
  **Read only:** `RewindDialog.tsx` itself has no render test (no test references it), so the disabled confirm, the "Close" label and the alert are verified by reading the diff, not by running.

## Findings

### F1 (medium; honesty gap; not reproduced) - a file restore that fails part-way is reported as a plain refusal

- Where: `internal/agenthub/rewind.go:414-421` (a restore error is returned as is, after `discardFork` and `abandonSafety`, with the comment "Nothing has been committed");
  `internal/checkpoint/restore.go:192-250` (`ApplyRestore` writes with `checkout-index` first, then deletes path by path, and returns the partial `applied` *with* the error; it is not atomic);
  `internal/agenthub/checkpoint_restore.go:349-358` (the inner restore-safety row is set `failed` and returns `RestoreResult{SafetyCheckpointID}, applyErr`).
- Consequence: after any failure inside `ApplyRestore` that follows a successful write (a failed delete, a cancelled caller killing a git subprocess mid-`checkout-index`), files may have changed, yet the person
  gets the ordinary refusal sentence (code is neither `agenthub.rewindFilesRestored` nor `...OutcomeUnknown`), the dialog treats it as `refused` (message shown, confirm stays enabled, but the token is spent), and the
  rewind's own safety row is marked `failed`, which `InterruptedRewinds` (pending only) never lists. A way back does exist (the inner restore-safety row keeps `BeforeTree`) but the window has no control for it (documented limit).
- Why not claimed as broken by #379: this path predates it and #379's claim is about the cut after a successful restore. But its title promises truthful reporting, and this is the one place after the first file write where the answer is not truthful.
- Minimal repro idea (not run): in the files rig, make one delete in the plan fail after a write succeeded (needs a plan with a write and a delete, e.g. delete `kept.txt` then add a second new file, with the directory made read-only for the delete). Proving line: `restore.go` `if err := r.deletePath(path); err != nil { return err }` after `applied.Written = writes`.
- Suggested handling (not applied, owner decision): treat any `applyRestoreHeld` error whose `RestoreResult.SafetyCheckpointID != ""` (the restore ran) as `RewindUnknownError`-class "files may have changed" and leave the rewind safety row pending.

### F2 (medium for the test suite, none for production) - the budget regression tests fail by hanging and exhausting memory

- Where: `internal/agenthub/harness_rewind_files_test.go:563` (`TestRepeatedSChangesAreRetriedAFiniteNumberOfTimes`) and `:795` (`TestEveryPreparationOfARewindSharesOneLaunchBudget`): the hook calls `r.moveS()` on every
  preparation/fork unconditionally.
- Evidence: M3 (unshared budget) made the test run until the 60 s timeout; an earlier identical run grew to 16 GB (about 300 MB/s) until the coordinator killed it, and the previous attempt of this task died from the same cause. The tests *do* catch the regression, but as a machine-killing hang on any developer or CI host.
- Suggested fix: stop moving S after a small cap (for example after 6 preparations) and `t.Errorf` there, so the failure is an assertion. Not applied: it edits merged test code and this is a review-only delivery.

### F3 (low; not reproduced) - after the commit, the settings re-application and old-session deletion still use the caller's context

- `rewind.go:484-501`: `reapplySessionSettings(ctx, ...)` and `deleteOldVendorSession(ctx)` run with the caller's context. A caller that cancels after the cut committed gets a successful result, the UI is told (`OnSessionRewound`), but the
  mode and model options may silently not be re-applied (the fork "comes up in the vendor's default mode", spike S2) and the old vendor session stays. Both are logged, neither is reported. Low: the cut itself is correct; the effect is a quiet mode change.

### F4 (low; read only) - the resume path's refused-publication cleanup uses the caller's context

- `service.go` (resume, after `publishServingLaunch` fails): `_ = rt.Close(ctx)`. Rewind was fixed to use a host context; this sibling was not. Whether a cancelled context leaves the vendor process running depends on the adapter's `Close`, which I did not read.

### F5 (info) - retry classification is pinned by one test, with backstops

M1 shows that only the "lost endpoint together with S" test pins the reason check on its own; Close and refusal tests pass without it because the durable status check and `ctx.Err()` also stop the retry.
That is defense in depth, not a defect, but a future edit that removes a backstop is the only thing that would then be caught by those tests.

### Observations

- The "unknown" outcome does not call `onRewound` or refresh the transcript; the sentence tells the person to reopen the session. Consistent, but the window stays stale until then.
- `git_repo.go`: a write by an agent is authorised by the *requested path* being inside a granted root; the repository that then opens only has to *contain* that path (`Toplevel` check). A session granted a
  sub-directory of a larger checkout therefore acts on the whole checkout by design of git. Whether that matches the grant model is a product question for the owner, not a defect I can demonstrate.
  The `Toplevel` check is an optional interface assertion, so a `GitAPI` test double without it silently skips it; the production `*git.Service` implements it. (Directory-swap threat boundary deliberately not assessed.)

## Ledger items 4-8: read vs exercised, and whether independent evidence now exists

| # | Item | Read | Exercised | Independent evidence now? |
| --- | --- | --- | --- | --- |
| 4 | Coordinator/reconcile, crash between commit and publish, account-move reopen, replacement and late completion, incarnation checks at side effects | `servingCoord` (stage, publish, check, detach, close, reconcile, `ServingTxn`), `commit` in `harnessconfig/apply.go` (quarantine, read-back, mark pending, publish on host context), `completeResume`, `moveSession`, resume head after the barrier (row re-read, `fillAccountSpec`) | mcp live-publication and reconcile tests (13), `harnessconfig` package, app `TestACrashBetweenTheCommitAndThePublicationIsRecoveredByTheNextLaunch`, `TestADeadRuntimeIsRecovered...`, `TestDeleteRemoves...LatePublication...`, `TestAccountMoveSendsAStaleResumeToTheNewAccount` (-race) | **Yes, for what was read and run.** Incarnation is checked at stage (epoch+lifecycle), at publication (close, lifecycle, endpoint incarnation, epoch) and at `detachServing`; endpoint `Install`/`Restore` ticket checks were read in #376, not re-read here. `moveSession` does not use `beginServingClose` (no lifecycle bump); it is excluded from racing launches by the `invoking` claim and the `resuming` barrier, which I read, not stress-tested. Nothing is checked at the moment the vendor process spawns; by design the endpoint is staged first so S changes reach it. |
| 5 | The SQL rewind transaction, child/fork-at-create, parent S not inherited, failed staged launches leave no admitted endpoint | `RewindSession` in full: vendor id, cut, kept-state renumbering, turns and checkpoints of dropped prompts, safety row done, applied reference set or deleted, all in one `BeginTx`; only `Commit` failure is the unknown sentinel. `Snapshot(ctx, sessionID,...)` reads S by the *child's own* id | `TestAChildGetsItsOwnProfileAndNeverItsParentsS`, `TestARewindWhoseReferenceWriteFailsCutsNothingAndDiscardsTheStagedEndpoint`, `TestARewindWhoseSChangesWhileTheForkOpens...`, `TestAnEndpointThatCannotBeStagedOnTheRetryLeavesNothingBehind`, and the whole #379 pattern under `-race` | **Yes.** Transcript cut and applied reference commit together (read; reference-write-failure test exercises the rollback). Parent S is not inherited structurally (S is keyed by session id) and by test. Not proven: a crash between files and SQL (documented limit; F1 is the in-restore variant). |
| 6 | Public controls, error and DTO paths, `SessionToolGroups`, setters refuse non-UI callers in Go | all `harnessControls` methods: every setter, `Apply`, `Cancel`, `Reconcile`, the stale-repair methods and both budget methods call `requireUI` first; `Profile`/`ProfileWithBudgets` go through `agentScope` (agent-only, session-scoped); `SessionToolGroups.tsx` and `toolGroupsController.ts` in full | existing `TestOnlyTheAgentItselfCanAskAndOnlyTheWindowCanEdit` and `...OnlyTheWindowCanDiagnoseOrPrepareARepair...`; **new probe** covering budgets, groups, revoke, apply, cancel, reconcile for agent, terminal, remote-human and unidentified callers (mutation-checked); controller tests (28) | **Yes for Go-side refusal.** Before the probe nothing exercised the two budget methods with a non-window caller. Frontend: the controller drops late answers (epoch), single-flights reads, withdraws a viewed review on re-target and does not cancel one being applied; no finding. `SessionToolGroups.tsx` has no render test; read only. |
| 7 | Budget model, policy setting, measurement attribution | `normalizeBudgetEdits` (set and reset of one field refused, empty edit refused, bounds), `applyBudgetEdits`, `workspaceBudgetModel.ts` (three intents: omitted, set, reset; edits built exactly as left), exemption refusal (`ErrBudgetExempt`, `BudgetExempt` in the registry fingerprint), `noteServedFacts` (replaces, never mutates, only for future turns) | app and mcp budget tests, `budgetsController` tests, `TestMeasurementUsesTheEntrySnapshotAcrossAMidTurnChange` | **Partly.** I read the independent-field semantics and the attribution seam and ran their tests. I did not read `usagemetrics`/storage rows, the stale-epoch handling inside `WorkspaceBudgetsPanel.tsx`, or the policy setter beyond the facade. No finding. |
| 8 | #375: git canonical-path binding, Claim/Release serialization | `gitFor` and helpers in full (one resolution of the requested path, sealed folders and roots, all decisions on resolved paths, `Toplevel` containment), `Claim`, `Assign`, `Release`, `claimable`, `writeClaim`, `mutateLocked` | tasks claim/release tests under `-race`, mcp git scope tests | **Not from #375 itself.** `gh pr view 375 --comments` shows a CodeRabbit automated review (commit `8dfd4014...`, state COMMENTED) and the author's own fix replies at `8dfd4014` and a later head; there is no review by anyone else. That is not an independent sign-off and I did not treat it as one. My own reading found the claim check is repeated under the `writeMu` write lock, `Release` refuses a live other holder, and `OnChanged` runs after the lock is released (later commit `daa43bf5`); the liveness callback (`agentLiveness.Alive`) runs under `writeMu` and only reads the store (no path back into tasks). Directory-swap boundary and O-2 not assessed, as instructed. |

## What I could not verify

- Any native GUI, real vendor runtime, or the `RewindDialog` rendering (no component test exists).
- F1 and F3 by reproduction; F4 beyond reading.
- That nothing holds `Service.mu` while calling a coordinator-taking reader (spot check only).
- Whole-package `-race` suites, lint, `bun run build`, the arch test (not run for memory reasons).
- Endpoint `Install`/`Restore` ticket internals (not re-read), `usagemetrics` storage, the budgets panel internals.
- CodeRabbit's actual findings on #375 beyond the two the author replied to.

## Commits on `r1b-review`

- `73a8be77` draft skeleton of this report
- `14a90949` probe test (written by the reviewer, pending another reviewer)
- the commit containing this final report (see `git log`)

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>
