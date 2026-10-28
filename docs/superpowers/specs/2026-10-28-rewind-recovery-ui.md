# Rewind recovery: a session notice for a rewind that did not finish

Branch `feat/rewind-recovery-ui` (base `6ba80806`). The owner asked for the missing window control for an
interrupted or partial rewind. Frontend only: no Go, no new `AppService` method, no bindings change.
Production stays inert (harness activation is untouched). Not independently reviewed; the native window
and a real interrupted rewind were not exercised (see Limits).

## 1. What the backend does today (read before building)

### What `InterruptedRewinds` returns

`Service.InterruptedRewinds` (`internal/agenthub/rewind.go`) reads `ListInterruptedRewinds`
(`rewind_store.go`): rows with `kind = rewind_safety AND status = pending`, oldest first, mapped to
`CheckpointSummary` (`id, promptMessageId, kind, status, outcome, beforeTree, changed, overlap, createdAt`).

- **Not session scoped, and it carries no session id.** `CheckpointSummary` has no `sessionId`, the call takes
  no argument and the bridge call (`AgentHubInterruptedRewinds`, capability `agenthub:read` on `session:list`)
  does no per-session `authorizeTarget`. A session view cannot tell from this list which rows are its own.
- **Pending is not the same as interrupted.** A rewind that is running right now also holds a pending safety row
  (written at step 4, settled by the cut at step 7). The query cannot tell the two apart.
- A session's own rows are available, scoped and authorised, from `AgentHubListCheckpoints(sessionId)`
  (`ListCheckpoints` -> `authorizeTarget`, `ListCheckpointSummaries`): it returns every kind, including
  `rewind_safety` rows, with the same fields. Filtering that list on `kind = rewind_safety, status = pending`
  is exactly the `InterruptedRewinds` predicate, restricted to the session. **The control uses that list**, so
  the ownership of a row is the backend's, not the window's guess. `useSessionCheckpoints` already reads the
  same call but `indexCheckpoints` keeps turn rows only.

### Which outcomes leave a pending row

| Outcome | Safety row | Listed? |
| --- | --- | --- |
| Crash after the safety row, before the cut | `pending` | yes |
| The file restore stopped part-way (`checkpoint.ChangedError`, PR #384, `RestoreIncomplete`) | `pending` | yes |
| Commit outcome not provable (`RewindUnknownError`) | `pending` (kept on purpose) | yes |
| Restore done, cut refused (S moved, budget exhausted, refused publication: plain `RewindPartialError`) | `failed` (`abandonSafety`) | **no** |
| Restore refused before its first write, launch gate refusal, vendor fork failure | `failed` | no |
| Cut committed | `done`, in the same transaction as the cut (`rewind_store.go`) | no |

Consequences used by the control:

- A pending row means the cut did **not** commit (the transaction that cuts the conversation is the one that
  marks the row `done`). So "the conversation was not rewound" is true of every pending row once no rewind is
  running, including the unknown outcome after the fact.
- The files, on the other hand, may be untouched, half restored or fully restored. The notice says so.
- **A gap this change does not close:** the "restore done, cut refused" outcomes settle their row as `failed`,
  so no notice appears for them. The row (and its snapshot) exists and is listed by `AgentHubListCheckpoints`,
  but `failed` alone cannot be told from "the restore was refused and nothing changed", and settling it
  differently is a backend change. For those, the public sentence ("rewind the same message again can finish
  it") stays the only path. Said here so nobody reads this control as covering them.

### What undoing needs: `RestoreUndoRestore` (preview, token, confirmation, overwrite list)

`AgentHubPlanRestore(RestoreRequest{sessionId, mode: "undoRestore", checkpointId})` then
`AgentHubApplyRestore(token, overwrite)` (`checkpoint_restore.go`):

- `PlanRestore` is read only. It refuses an agent caller (`refuseAgentCaller`), a session that is mid-turn or
  claimed (`restorableNow` -> `ErrSessionBusy`; a running rewind holds that claim), and a checkpoint of another
  session or not a safety row (`ErrCheckpointNotFound`). A missing `beforeTree` is `ErrUnavailable`.
  `undoRestore` accepts both safety kinds (`IsSafety`), so the interrupted `rewind_safety` row works directly.
- It returns `RestorePreview{token, sessionId, mode, actions[{path, kind write|delete}], conflicts[{path, kind,
  reason, overridable}], warnings}`. The target is the row's `beforeTree`, scope is every path, `protected` is
  the row's skipped paths. Conflicts are measured against the session's newest full tree
  (`NewestCheckpointTree`): the rewind's own file restore writes a `restore_safety` row with an after tree, so
  after a rewind that got as far as the restore, only files changed since show up as conflicts. When the
  process died inside the restore (no after tree yet) the files the restore wrote may show as overridable
  conflicts: the preview says so and nothing is forced without the person ticking it.
- The token is opaque, server-side, **single use** (`restorePlans.take` deletes it) and expires after 15
  minutes; a second apply on it is `ErrRestorePlanStale`.
- `ApplyRestore` takes its own safety snapshot first, then writes. Only conflicts marked `overridable` and
  named in `overwrite` are forced. It returns `RestoreResult{written, deleted, safetyCheckpointId}`. A failure
  part-way marks that snapshot `failed` but keeps it usable; the public error does not say whether files moved.
- **It does not settle the interrupted row.** After a successful undo the `rewind_safety` row is still
  `pending`, so a restart would list it again.

### Settling the row: `DismissInterruptedRewind` already exists

`AgentHubDismissInterruptedRewind(checkpointId)` (`rewind.go`, bridge capability `agenthub:execute`, listed in
`internal/bridge/testdata/legacy_ops_methods.json`, binding and `agentHubDismissInterruptedRewind` wrapper in
`frontend/src/bridge/agenthub.ts` present). It refuses an agent caller, accepts only a `rewind_safety` row
that is still `pending` (otherwise `ErrCheckpointNotFound`) and marks it `done` without touching a file. So
the backend **does** have a way to settle a row; nothing is faked.

### Who may call these

All of `AgentHubPlanRestore`, `AgentHubApplyRestore`, `AgentHubDismissInterruptedRewind` and the rewind itself
call `refuseAgentCaller` in Go (`ErrRestoreNotPermitted`): the person's, never an agent's. None is an MCP tool.
`AgentHubInterruptedRewinds` and `AgentHubListCheckpoints` are reads. The window adds no authority.

### How the window handles restore and rewind today

- There is **no restore dialog component**. `restore.ts` holds the rules (`restoreTitle`, `restoreSummary`,
  `restoreCounts`, `conflictReason`, `canApplyRestore`, `forcedPaths`, `toggleForced`) for
  `RestoreMode.RestoreUndoRestore` among others, with tests, but nothing renders them except
  `RewindDialog`, which uses `conflictReason`, `forcedPaths` and `toggleForced` for its file half.
- `RewindDialog` (`surfaces/RewindDialog.tsx`): plans on open (`agentHubPlanRewind`), shows the action list with
  a checkbox per overridable conflict (non-overridable ones disabled), confirm is disabled until the plan can be
  applied, applies with `forcedPaths`, and sorts a failure by published code (`attemptRewind`,
  `rewindFailureSpendsThePlan`): a partial or unknown outcome keeps its own warning, clears the plan, never calls
  `onRewound`. A new plan needs reopening.
- Errors reach the window as `Error` with a `code`; stale-plan and checkpoint-not-found have no published code
  (they surface as the generic message), so the window cannot recognise them and must not guess: after any failed
  call the notice reloads the list from the backend instead.

### Verdict

The backend fits a safe control without change: a scoped list (existing), a preview with token and conflicts
(existing), a confirmation with an overwrite list (existing), a settle operation (existing), UI-only
enforcement in Go (existing). Nothing needs a new method, so the work is the frontend only.

## 2. What was built

`InterruptedRewindNotice` sits above the composer in the Agent Hub content and in the rail (the session view; no
navigation, no sidebar, no tab). It renders nothing unless the session has an interrupted rewind.

| Piece | File | Role |
| --- | --- | --- |
| Rules | `frontend/src/modules/agenthub/interruptedRewind.ts` | Which row counts (`rewind_safety`, `pending`, newest of the session's), the fixed words, the actions on offer (`noticeActions`), one undo attempt (`attemptUndo`), settling (`settleInterruptedRewind`) |
| Reader | `frontend/src/modules/agenthub/useInterruptedRewind.ts` | Reads the session's own `AgentHubListCheckpoints`, again on this session's `sessionRewound` event, on demand and when a rewind dialog closes; generation-guarded so a stale answer never lands |
| Notice | `surfaces/InterruptedRewindNotice.tsx` | Pure view plus the container that owns the state |
| Undo | `surfaces/UndoRewindFilesDialog.tsx` | The existing preview, token and overwrite confirmation for `undoRestore` |
| Shared list | `surfaces/RestoreFileList.tsx` | The action and conflict list, extracted unchanged from `RewindDialog` (same test ids), used by both |

What the person sees: "A rewind did not finish. The files in this project may have been put back, or only partly. The
conversation was not rewound. DevKit kept a copy of the files as they were before the rewind." Actions:

1. **Put the files back as they were**: opens the dialog, which calls `AgentHubPlanRestore` (read only) and shows the
   writes and deletes, the conflicts (overridable ones can be ticked, the rest are shown with the reason) and the
   backend's warnings; **Put files back** calls `AgentHubApplyRestore(token, ticked)` once. Success: the notice says what
   was put back and offers **Close this notice**; undo stays available for what was left alone. Failure: the host's
   sentence plus "DevKit cannot tell whether any file changed before it stopped. Check again to see what is left."
   (the plan is spent; **Check again** makes a new plan). The window never says "nothing changed" about an apply.
2. **Keep the files as they are**: asks first ("This closes the notice and does not change any file. The copy is no longer
   offered here."), then `AgentHubDismissInterruptedRewind`. A refused settle is reported ("... The notice is still
   open.") and the list is read again.

Not offered, on purpose: any retry, "finish the rewind", or a second restore without a new preview. Undo is disabled while
the agent is running (the backend refuses then) and while anything is in flight. The notice is hidden while a rewind dialog
is open: a rewind in flight holds the same pending row and is not an interruption. When the dialog closes the list is read
again, so a rewind that stopped part-way raises the notice immediately.

Why the session's checkpoint list and not `AgentHubInterruptedRewinds`: see section 1. The binding wrapper stays unused by
the window; nothing was added to it.

Stale list, handled without recognising error codes (the backend publishes none for a stale plan or a missing row): any
failed plan, apply or settle reloads the list from the backend; if the row is gone the notice goes with it, if not it stays
with the host's sentence. A session switch drops the previous session's row and any in-flight answer. Hub tab and rail can
show the same session: settling in one leaves the other notice until its next read or failed call, which then reloads.

## 3. The fixed public sentences

Unchanged. They say "rewinding the same message again can finish it / shows what is left to put back" and that the
safety checkpoint is kept. Those stay true for every outcome, and the new control only exists for part of them (pending
rows: a restore that stopped part-way, an unproven commit, a crash). The restore-done, cut-refused outcomes settle their
row `failed` and have no notice; a sentence that sent the person to a notice would be false for them. Codes
(`agenthub.rewindFilesRestored`, `agenthub.rewindOutcomeUnknown`), the partial/unknown/refused distinction and the
no-cause rule are untouched. Only the Go comment above `rewindOutcomeRefusal` was corrected ("the window has no control
for it today" was no longer true).

## 4. Tests (bun, `--isolate`, repo style)

| Case asked | Test |
| --- | --- |
| Nothing shown when there is no interrupted rewind | `interruptedRewind.test.ts` "is nothing for a session without a pending rewind safety row" (turn, done, failed, restore-safety rows); `useInterruptedRewind.test.ts` "shows nothing when the session has no pending rewind row" |
| Shown for a pending row | `interruptedRewind.test.ts` "is the pending rewind safety row"; `useInterruptedRewind.test.ts` "shows the pending row of this session"; `InterruptedRewindNotice.test.tsx` "says what is true, in plain words" |
| The action goes through the existing flow | `undoRequest` is exactly `{sessionId, mode: undoRestore, checkpointId, promptMessageId}` (the existing `RestoreRequest`); the dialog calls only `agentHubPlanRestore` and `agentHubApplyRestore`; `attemptUndo` resolves with the result; `settleInterruptedRewind` calls the dismiss wrapper then reloads |
| A failure is reported truthfully | `attemptUndo` failure keeps the host sentence, adds "cannot tell whether any file changed", never "nothing changed"; `UndoRewindFilesBody` renders the failed apply as an alert with no file list; a refused settle says "The notice is still open." and keeps both actions |
| Stale list after the session changed | `useInterruptedRewind.test.ts` "a stale answer never lands after the session moved on", "an older read that finishes after a newer one does not bring back a settled row", "keeps what it showed when a reload cannot be read", rewound-event scoping |
| No action the backend cannot do | `interruptedRewind.test.ts` `noticeActions` (undo and keep only; no undo without a snapshot; no retry/finish/complete/resume/again in any label); `InterruptedRewindNotice.test.tsx` "offers to put the files back or to keep them, and nothing that would finish the rewind" (exactly two buttons) |

Failing-then-passing: two mutations were applied and reverted. Counting any non-`done` row as interrupted failed
"is nothing for a session without a pending rewind safety row"; dropping the generation check in `reload` failed "an older
read that finishes after a newer one ...". Both pass on the real code.

## 5. Validation

All commands run from the worktree, each under `systemd-run --user --scope -q -p MemoryMax=3G timeout N`:

- `cd frontend && bun install --frozen-lockfile` (904 packages).
- `bun test --isolate` on the five affected files (interrupted rewind rules, reader, notice SSR, `rewind.test.ts`,
  `restore.test.ts`): 64 pass, 0 fail.
- `bun test --isolate src/modules/agenthub` (114 files): 1504 pass, 2 fail on the first run under a machine load average of
  27 (`AgentPermissionRequest` deadline text "56s" vs "60s", `Meta Inspector` 7 s case); `surfaces/surfaces.test.tsx` re-run
  alone on this branch: 93 pass, 0 fail, and 93 pass on a clean `origin/main` checkout. Timing flakes, not this change.
- `bunx tsc --noEmit`: clean. `bunx eslint` on the touched files: clean.
- `bun run build` once: built, performance budget passed (initial JS 1245.7 KiB, CSS 282.8 KiB).
- `gofmt -l internal/bridge/errors.go`: clean. The Go change is a comment only; no Go test was run.

## 6. Limits (not verified)

- **The native window and a real interrupted rewind.** Nothing here ran in the Wails window or against a real vendor or
  project. Rendering is checked by SSR markup only; the dialog's effects (plan on open, apply, recheck) are not exercised
  by a test because the repo has no jsdom or browser harness for dialogs (`RewindDialog` has none either).
- A pending row whose restore died inside the restore (no after tree recorded) may preview some files the restore already
  wrote as overridable conflicts; the dialog shows them unticked, and ticking is a decision.
- A pending row of a rewind that is running in another window would show the notice there; the backend refuses a restore
  while it runs (`ErrSessionBusy`), and a settle during it only marks the row done earlier than the cut would. Single
  window app, so not handled further.
- The "restore done, cut refused" outcomes (row `failed`) have no window control (section 1).
- Several pending rows: the newest is offered; after it is settled the next one appears.

## 7. PR description

**feat(agenthub): a notice and an undo for a rewind that did not finish**

A rewind that stops after the files were restored, or part-way through the restore, leaves a pending safety row and a
kept snapshot. The window had bindings for this and no control. This adds a notice in the session view, shown only while
the session has such a row, that says what is true (files may be put back or partly, the conversation was not rewound, a
copy is kept) and offers the two things the backend supports: put the files back through the existing `undoRestore`
preview and confirmation, or settle the row without touching a file (after asking). No retry and nothing that implies the
rewind finishes by itself. Frontend only, no backend, bindings or public-sentence change; the O-1 record and the
rewind-honesty doc are updated, including what is still not covered. See sections 1 to 6.
