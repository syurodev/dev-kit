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
