# Rewind: a restore that fails part-way is reported as what it is

Branch `fix/rewind-partial-restore-honesty` (base `8c5e64d0`). Source: findings F1 to F4 of the independent review
`docs/reviews/2026-10-28-r1b-376-items-and-379-review.md` (branch `r1b-review`). Production stays inert; this changes
no retry classification and no budget logic. Not independently reviewed.

## Summary

- **F1 (medium).** `checkpoint.ApplyRestore` writes with `checkout-index` and then deletes path by path; it is not atomic and returns its
  partial result together with the error. `RewindSession` took any restore error as "nothing committed": it discarded the fork, marked the
  rewind's safety row `failed` (never listed by `InterruptedRewinds`, which reads pending rows) and returned the bare cause, so the person
  was told an ordinary refusal although files had changed. Now:
  - `checkpoint.ChangedError` / `checkpoint.Changed(err)` mark a restore failure that happened after the folder began to change
    (the call to `checkout-index`, or any delete after a file was removed). A failure before that (stale plan, refused path, unreadable
    snapshot or index, a first delete that is refused) carries no mark: the folder is as it was.
  - When the restore error is marked, the rewind returns the existing `RewindPartialError` (published code `agenthub.rewindFilesRestored`),
    discards the fork and endpoint, and leaves the rewind's safety row **pending**, so `InterruptedRewinds` lists it and its snapshot (kept) is
    the way back. The cut is never attempted.
  - Otherwise the restore changed nothing and stays the plain refusal, row `failed`, as before.
  - `RewindPartialError.RestoreIncomplete` selects a second fixed host sentence under the same code: "The files were only partly put back,
    and the conversation was not rewound. Some files in the project may differ from before. ..." The existing sentence says the files "were put
    back", which is false for a half restore. No cause text reaches either sentence. The frontend sorts by code only, so partial, unknown and
    refused stay distinct and nothing changed there but a test.
- **F2 (test suite).** The S-moving hooks of `TestRepeatedSChangesAreRetriedAFiniteNumberOfTimes` and
  `TestEveryPreparationOfARewindSharesOneLaunchBudget` are capped (`filesRig.cappedMoveS`): past the cap the test reports with `t.Errorf` and
  stops moving S, so a broken budget fails in well under a second instead of looping and exhausting memory.
- **F3 (low).** After the cut commits, `reapplySessionSettings` and `deleteOldVendorSession` run on `hostContext` (bounded, no caller
  cancellation), so a caller that cancels right after the commit still gets the mode and model back and the source vendor session deleted.
- **F4 (low).** A resume that fails after its process is up closes that process on `hostContext`: both failure points in `resumeRuntime`
  (the new vendor id cannot be stored, the launch is refused at publication). Closing with a cancelled context left the process running.

## Test plan (all reproduced with a failing test first)

| Item | Test | Before | After |
| --- | --- | --- | --- |
| F1 | `TestARestoreThatFailsAfterWritingIsAPartialRewindAndKeepsTheSafetyRowPending` (turn 2 deletes `kept.txt` and creates `locked/a.txt`; the directory is made read-only after the plan; the restore writes `kept.txt` then fails to delete) | fails: error is the bare `remove ...: permission denied`, row `failed`, not listed | pass |
| F1 | `TestARestoreRefusedBeforeItWritesIsAPlainRefusal` (plan drifts before the apply) | pass (pins the unchanged path) | pass |
| F1 | `internal/checkpoint` `TestAFailedDeleteAfterAWriteIsReportedAsChanged`, `TestAFirstDeleteThatIsRefusedLeavesTheFolderAsItWas`, `TestAStalePlanIsRefusedBeforeAnythingChanges` | n/a (new API) | pass |
| F1 | `internal/bridge` `TestAPartialRestoreHasItsOwnSentenceUnderTheSameCode` (same code, own sentence, no cause, survives a second funnel pass) | n/a | pass |
| F1 | frontend `rewind.test.ts`: the new sentence under the same code is `partial`, spends the plan, is neither `unknown` nor `refused` | n/a | pass |
| F2 | the two budget tests with the mutation "each preparation draws its own budget" (`openForked` passes a fresh `launchAttempts` counter) | uncapped (as reviewed, not re-run here): non-terminating, 16 GB | capped: both fail in about 0.2 s; `TestRepeatedS...` reports the S-move cap ("S was moved for more than 5 launches"), `TestEveryPreparation...` "prepared 4 launches"; mutation reverted, both pass |
| F3 | `TestACallerThatCancelsAfterTheCutStillGetsTheModeBackAndTheSourceDeleted` (cut commits, caller cancels) | fails: no mode applied, source deletion saw a cancelled context | pass |
| F4 | `TestAResumeThatFailsClosesItsProcessForACancelledCaller` (`never prompted`, `refused at publication`) | both fail (Close refused with context canceled) | pass |

Commands (each under `systemd-run --user --scope -q -p MemoryMax=4G timeout N env GOMEMLIMIT=2GiB`, `-p 1 -count=1`, one at a time):
`go vet ./internal/agenthub/ ./internal/app/ ./internal/bridge/ ./internal/checkpoint/`; `go test` of `./internal/agenthub/`, `./internal/app/`,
`./internal/bridge/` with `-run 'Rewind|SChange|Ambiguous|Budget|LostEndpoint|Cancelled|CrashDuring|LaunchGate|CannotBeStaged|Partial|Unproven|Unfinished'`
(agenthub also `|Restore|Resume`), all of `./internal/checkpoint/`, `./internal/arch/`, `gofmt -l internal/`,
`cd frontend && bun install --frozen-lockfile && bun test --isolate src/modules/agenthub/rewind.test.ts`.

## Limits

- Files and SQL are still not one transaction. A failed or interrupted restore leaves files in an in-between state; this change only makes the
  report and the pending row truthful. The host never repeats or undoes a restore by itself.
- Window recovery is unchanged: the pending row is listed by `InterruptedRewinds`, but no component uses the bindings
  (`AgentHubInterruptedRewinds`, `RestoreUndoRestore`) yet, so the sentence points only at rewinding the same message again, which makes a new plan
  (files the failed restore already wrote show up there as conflicts to confirm).
- `ChangedError` is conservative on a `checkout-index` that fails or is killed: git may or may not have written, so it counts as changed.
  A delete of a file that is already absent does not count as a change.
- The existing partial outcomes (S moved and the budget ran out, a refused publication) still settle their safety row as `failed`, so they are
  not listed by `InterruptedRewinds`; only the new restore-failure path keeps it pending. Not changed here (out of the findings' scope); the
  two behaviours are now inconsistent and the owner may want one rule.
- The plain `ApplyRestore` (code-only rewind and "restore files") returns the same wrapped error and is unchanged in what it publishes;
  `Changed` is available to it but the public sentence for that path was not reworked.
- Other `rt.Close(ctx)` calls with a caller context (for example the reload path, `service.go` around the reopen) were not touched: no failing
  test was written for them.
- Not verified: a real vendor, a native window, `RewindDialog` rendering (no component test exists), a full-suite or `-race` run, `bun run build`,
  lint. `internal/arch` was run with a stub `frontend/dist` (the worktree has no build), then the stub was removed.
