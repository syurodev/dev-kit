# Harness AP1 completion review

Date: 2026-10-27. Reviewed SHA: `782a63c5b4672bff8df9a48b9a60bf8ad1b0d711` (main after #375). Plan:
`docs/superpowers/plans/2026-10-27-harness-ap1-completion-implementation.md` (supplied from a Codex worktree; it is not on main),
parent plan `2026-10-27-harness-acceptance-activation-implementation.md` AP1 items 1-8. Production is inert: `main.go` is unchanged and
`TestProductionDoesNotWireTheHarnessYet` is intact. AP3 and AP4 were not run. No account, sign-in, paid request, real workspace or
vault was used; probes ran on temporary databases with scripted vendors.

**Outcome: AP1 stays Partial.** One finding (F-C) blocks activation until the owner decides how a stale stored profile is repaired.
Two rows cannot be signed by this reviewer (see Independence). Nothing here is a human sign-off.

## Independence

The reviewer is an AI coding session. It did not write H4c, #363, #367, #353, H5a or H5b. It **did** write, in #375, the git scope
fixes (`internal/mcp/git_repo.go`, `internal/git` `Toplevel`) and the task claim lock (`internal/tasks`), and an earlier partial AP1 review
at `e39d9060`. Those areas are listed below as **author-reviewed, pending another reviewer** and are not counted as independent evidence.
A separate AI session is review input, not an ordinary merge approval.

## Method and what was actually read

Code was read, not only run: each file below was read in full unless a range is given. Where an existing test was relied on it is named,
and it was run on this head (see Validation). `git diff e39d9060 782a63c5` shows #375 touched no harness-owned package, so the earlier
reading of `internal/mcp/{serving,session_host}.go` still describes this SHA.

| Area | Files read |
| --- | --- |
| Approval, CAS, publication | `internal/harnessconfig/{service,apply,live,snapshot,patch,view}.go`, `internal/app/harness_config.go`, `internal/app/harness_live.go`, `internal/preferences/harnesspolicy.go` (+ `HoldAgentCustomization`) |
| Pinned basis, resolver | `internal/harness/{serving_basis,resolve,classify}.go`, `AdoptsNewExecutables` in `snapshot.go` |
| Store | `internal/storage/harness_store.go` (`Apply`, `writeHarnessRow`, `ClearOverride`), `internal/agenthub/store_harness.go` |
| Lifecycle, fences | `internal/agenthub/{harness_runtime,serving_live}.go`, `recovery.go` (reload), `CloseSession` and `rewind.go` (steps 2-8, `openForked`) in `service.go`, `internal/app/harness_lifecycle.go`, `internal/mcp/{serving,serving_live,session_host}.go` |
| Facade, DTOs, errors | `internal/app/harness_public.go` (identity, `Request`, `view`, `Apply`/`Reconcile`), `harness_public_budgets.go`, `internal/harnessconfig/public.go` (DTOs, `OutcomeOf`, `OutcomeOfReconcile`), `internal/mcp/tools_harness.go`, `internal/bridge/errors.go` (harness code tables) |
| H5b | `internal/mcp/result_budget.go` (cutter), `internal/harnessconfig/patch.go` budget edits, `internal/storage` policy read |
| Frontend | `frontend/src/bridge/harness.ts`, `workbench/{budgetsController.ts,WorkspaceBudgetsPanel.tsx (render and disabled paths),WorkspaceHarnessDialog.tsx (1-200)}`, `modules/agenthub/summary/toolGroupsController.ts` |

Not read: `internal/harnessconfig/{ports,errors}.go`, `harness_status.go`/`serving_status.go` beyond `noteServedFacts`
and `servingStatus`'s opening, `SessionToolGroups.tsx`, `AgentHarnessPolicySetting.tsx`, `workspaceBudgetModel.ts` internals, the vendor adapters, accounts and usage-metrics storage.
Those parts of items 6 and 7 carry only the existing tests.

## Findings

### F-C (medium; blocks activation until decided) - a stale reference in a stored workspace profile locks the workspace out, with no in-app repair

- Where: `internal/harness/resolve.go:61` (a group in W not in the registry), `:274` (a budget rule for a tool not in the registry),
  `:285` (a budget rule for a group not in the registry); callers `internal/harnessconfig/snapshot.go:85` (launch) and
  `internal/harnessconfig/service.go:399` (the `before` resolve of every proposal); `internal/app/harness_public.go:512` (`validGroups` rejects the
  stale group by name too); the message at `internal/harnessconfig/service.go:489` promises "Repair or reset it in Settings".
- Trigger (deterministic): save a workspace profile that turns the optional `pdf` group off while the PDF module is wired, then rebuild the graph without the
  module over the same database (or ship a release that removes or renames a group or a budgeted tool). Repro: `docs/reviews/2026-10-27-harness-ap1-completion.repros/stale_group_in_stored_profile.go.txt`.
  Observed: a new session in the workspace is refused (`harness.snapshotUnavailable`, "could not be resolved, so it was not started");
  `PrepareWorkspaceGroups` for any other group is refused with `harness.groupUnknown`; so is an edit of the stale group itself.
- Consequence: fail-closed (nothing is served unfiltered), so no capability is widened, but every new session of that workspace is blocked and neither the window nor an
  agent can repair it. The only repair is editing the database. There is no reset or delete operation for W anywhere in the controls, bridge or UI. Today it is unreachable because
  the harness is not wired; after activation it becomes a support problem for any registry drift, and a PDF module that is sometimes not wired makes it plausible.
- Not fixed here, deliberately. The obvious fix, ignoring unknown names when reading W, is unsafe: a group renamed in a release would turn a person's stale "off"
  into "on" without telling them. A correct fix is a UI-only, confirmed repair operation that lists the stale entries it will remove (reviewed as widening, through
  the same Prepare/Apply and CAS), plus a resolver that can classify a profile with stale entries for that one purpose. That adds a bridge method, a dialog and regenerated bindings
  and needs a product decision, so it is left to the owner. Options: (1) build that repair, (2) accept and document, with the message corrected to say what is true.
- Independent of authorship: not code the reviewer wrote.
- **Status update (fix delivered, pending independent review).** The historical finding above is unchanged; the repair was implemented afterwards in `dd8e8150` (diagnosis, preparation, apply
  in `internal/harnessconfig/repair.go`), `d3aceead` (window-only controls, port, bridge, bindings), `952908e1` and `1e94743e` (recovery view in the workspace harness dialog). It is a
  UI-only, confirmed removal of exactly the stale keys in W.ToolGroups, W.ResultBudget.Tools and W.ResultBudget.Groups; the resolver stays strict. Details and tests are in
  `docs/agent-reference/harness-composition-activation.md` ("F-C repair"). The reviewer of AP1 also wrote this fix, so it needs a **different** reviewer; until then F-C is
  "fixed, not independently reviewed" and AP1 stays Partial.

### Observations (no defect claimed)

- **O-1 rewind, file restore before the epoch check.** `internal/agenthub/rewind.go` restores files (step 6) before `publishServingLaunch` (step 7) can refuse a launch
  whose S moved while the fork opened. A file-restoring rewind that meets an S commit in that window ends with the files restored, the conversation uncut and
  `ErrHarnessLaunchStale`; the safety checkpoint stays and a retry completes. Reasoned from the code; not reproduced (the existing test
  `TestARewindWhoseSChangesWhileTheForkOpensIsRefusedAndCutsNothing` is conversation-only). Low; the window is the fork open.
- **O-2 sessions with no applied reference.** `internal/agenthub/harness_runtime.go:315-322` deletes the applied reference when a launch has no snapshot, and
  `previousHarness` returns nil without one, so `AdoptsNewExecutables` does not run. Every session that ran while the harness was inert has no reference; its first
  reopen after activation can adopt workspace-owned executables without the new-session requirement. A fact for AP4 to decide on (accept, or record a baseline at activation), not a bug today.
- **O-3 cut-only changes.** `Classify` marks a cut-mode-only change neutral and narrowing. The spec says the same (`2026-10-26-subscription-harness-tuning-design.md:119-120`:
  "A change of cut mode alone is neutral and takes the narrowing tier"). With the opt-in on, an agent can switch head and headTail without a question, as specified. No product question remains.
- **O-4 `ReconcileServing` and the pending path** hold the session coordinator across a store read and an endpoint install (bounded by the 5 s host context in `commit`). Within the documented lock order.

## Per-item ledger (parent AP1 items)

Independence column: **I** = read by a reviewer who did not write it; **A** = includes code the reviewer wrote (pending another reviewer).

| # | Item | Result at `782a63c5` | Indep. | Traced | Limits |
| --- | --- | --- | --- | --- | --- |
| 1 | Immutable admission snapshot, no DB/vendor on the hot path | No finding | I | `servingHolder.admit/quarantine/restore/install/close` (`mcp/serving.go`); cutter reads the entry snapshot (`result_budget.go`, `result_metrics.go:51-140`) | Usage-store write path not read |
| 2 | Approval under pinned V/W/S; W+S CAS; V serialization; policy revision | No finding | I | `evaluate`, `liveEffectFor`, `ServingBasis.Resolve/Identity`, `Provider.Snapshot`, `Apply/commit`, `harnessBoundary`, policy setter lock, digest contents. Derived by hand: saved W change does not move the pinned basis; an S enable uses the launch W (budgets included); `Epoch`, `Incarnation`, `PinnedVendor/Workspace` are in the digest | `liveGroup` budget ranges read, not recomputed; no mutation experiments beyond the existing tests |
| 3 | Durable vs live; unknown commit; no false Live; no duplicate retry | No finding | I | `commit` (read-back by `Expect+1`, `MarkPending`, `Publish`), `Result.Live == PublicationInstalled`, `OutcomeOf` (unknown and paused force `Live=false`), `Request` and the MCP wrapper (`tools_harness.go` returns `IsError` with the same structured outcome), `HarnessStore.Apply` single transaction incl. closed-session refusal | Concurrent non-harness writers of W are not excluded by the read-back rule (no such writer exists today) |
| 4 | Endpoint incarnations, tickets, tokens, late Close, reload, reopen, crash | No finding in what was read | I | `sessionEndpoint.close/open` under `bindMu`, `Install` ticket checks, `stageServing/publishServingLaunch/detachServing`, reload ordering in `resumeSession`, `ToolUsageScope` token check | Crash-between-commit-and-publish recovery and `ReconcileServing` read, not exercised beyond existing tests; account-move reopen not traced past `prepareLaunch` |
| 5 | #353/#363/#367: Close keeps the reopen guard; resume cannot adopt W executables; rewind atomic; incomplete endpoints refused | No finding; O-1, O-2 | I | `CloseSession` (keeps applied ref, S tombstoned via `ClearOverride` after status closed), `AdoptsNewExecutables`, `prepareLaunch*`, `harnessGate`, `nilHarnessEndpoint`, `RewindSession` spec and `publishServingLaunch` | `RewindSession` SQL body not read; fork-at-create paths not traced |
| 6 | Public facade: scope, UI-only setters, bounds, cursors, errors | No finding in what was read | I | `agentScope`, `requireUI`, `uiSession`, `Request`, `requestBudgets`, `budgetPage`, cursor encode/decode, tool schemas, bridge code tables | `SessionToolGroups.tsx` not read |
| 7 | H5b budget/policy/cutter | No finding | I | `normalizeBudgetEdits`, `applyBudgetEdits`, `resolveBudgets`, `Classify` (bytes, cut, strictest tier), `isBudgetOnly`, `needsApproval`, `cutHeadAndTail` (proved head+tail <= room < budget, so no overlap or duplicate), `shrinkStructured` fallbacks | Measurement rows and `usagemetrics` not read |
| 8 | No new authority | See F-C; #375's two fixes | **A** for git scope and the task claim lock | The harness added no tool, grant, pre-approval or policy field in anything read; the guard-owner defects were found and fixed in #375 | The git scope and claim-lock code is the reviewer's own |

Frontend state (part of item 7 in the plan): `BudgetsController` and `ToolGroupsController` capture an epoch per operation, drop late answers, withdraw a review prepared for a view that moved on,
leave an apply in flight, and read back instead of inferring; the budgets panel replaces the rows with the review while one is open, so the draft cannot diverge from the reviewed edits;
`WorkspaceHarnessDialog` cannot be closed while preparing or applying. No optimistic live success is shown (`out.Live = false` for budget edits, `Live` only from a read). Controller-level
guards are single-flight and epoch-based; native focus and dialogs remain AP3.

Carried forward from #375 with its original SHA (`f8e99e04`, tested at `782a63c5`): the AP2 composed guard matrix, F1 and F2 and their concurrent follow-ups. They are not re-reviewed here.

## Validation

Run on `782a63c5` (this change adds documentation only, so no Go or frontend file differs), Go 1.27.2 darwin/arm64, Bun, 2026-10-27:

| Check | Result |
| --- | --- |
| `bun install --frozen-lockfile`, `bun test --isolate`, `bun run build` | pass (5119 tests, 0 fail; build ok) |
| `go test -race -timeout 40m` over the ten packages in the plan | pass (10 packages ok, no race reports) |
| `go test ./internal/arch/` (after building `frontend/dist`) | pass |
| `go test ./internal/... ./cmd/... -count=1` | pass (72 packages ok) |
| `go vet -tags server ./...`, `go build -tags server` | pass |
| `golangci-lint` | **not run**: the installed binary was built with an older Go and cannot typecheck against Go 1.27.2; CI lint is the evidence. No Go file changed here. |

No baseline failure was met, so none is claimed as environmental. The finding F-C repro was run as an overlay and removed; it is kept as `.txt` under
`docs/reviews/2026-10-27-harness-ap1-completion.repros/`. AppService signatures did not change; bindings were not regenerated.

## Remaining before AP1 can be called complete

1. Independent review of the F-C repair (implemented; see the status update under F-C). The reviewer who wrote it cannot sign it.
2. A reviewer other than this session for: the git scope and claim-lock code from #375, and the rows marked "read, not exercised" in items 4, 5 and 6.
3. Decide O-2 (baseline at activation) before AP4.
4. AP3 (real runtimes, native dialogs, `listChanged` per runtime) and AP4 remain unrun.

---

# R1 closeout (second pass)

Date: 2026-10-27. Base reviewed: `origin/main` `c2d3074f` (merge of #377, whose final head is `f941aaf7`). Plan: `docs/superpowers/plans/2026-10-27-harness-remaining-acceptance-team-implementation.md`
(R1; supplied from a Codex worktree, it is not on main). The text above is the first pass and is unchanged. Production is still inert (`main.go` untouched, `TestProductionDoesNotWireTheHarnessYet` intact).
AP3 and AP4 were not run; no account, sign-in, paid request, real workspace or vault was used; every probe ran on temporary databases, scripted vendors and a scratch browser page.

**Outcome: AP1 stays Partial.** This pass read the rows the first pass left open and found, reproduced and fixed three defects in code this session did not write, plus one latent defect in code it did.
Every fix below was written by the reviewer, so none of them is independently reviewed. Reviewer identity is unchanged from the first pass (an AI session; it wrote #375, #377 and these fixes).

## What was read in this pass

| Area | Files read | Exercised |
| --- | --- | --- |
| Coordinator, reconcile, crash recovery | `internal/agenthub/serving_live.go` (all), `harness_runtime.go` (all), `app/harness_live.go` | existing composed tests listed in the activation record; new probe `TestACrashBetweenTheCommitAndThePublicationIsRecoveredByTheNextLaunch` (passes) |
| Account-move reopen | `agenthub/account_switch.go` (`switchAccountForTurn`, `moveSession`, `abandonMove`), `resumeRuntime`, `liveRuntime` (`service.go`) | `TestAnAccountMoveTheLaunchGateWouldRefuseLeavesTheSessionAlone` (fails on `c2d3074f`) |
| Rewind SQL and run | `agenthub/rewind_store.go` (all), `rewind.go` (`ApplyRewind`, `run`, `openForked`, `discardFork`) | `TestARewindTheLaunchGateRefusesLeavesTheRunningSessionAlone`, `TestAFilesRewindWhoseLaunchGoesStaleChangesNoFile` (both fail on `c2d3074f`); existing rewind and rollback tests |
| Create, child | `CreateSession`, `openNewRuntime` (`account_create.go`) | `TestAChildGetsItsOwnProfileAndNeverItsParentsS` (runs, not skipped) |
| Public controls, policy | every `harnessControls` method (`app/harness_public*.go`), `bridge/harness_controls.go`, `bridge/harness_policy.go`, `preferences` setter callers, `SessionToolGroups.tsx`, `AgentHarnessPolicySetting.tsx` | existing identity tests |
| Budgets, measurement | `workspaceBudgetModel.ts` (all), `agenthub/turn_attribution.go`, `serving_status.go:noteServedFacts`, `mcp/result_metrics.go` call scope | existing tests |
| #375 code, current tree | `internal/mcp/git_repo.go` (`gitFor`), `internal/tasks/transition.go` (`Claim`, `writeClaim`, `Release`), `service.go` (`mutateAs`) | existing tests plus the new `OnChanged` test |
| #377, final head | `StaleRepairPanel.tsx`, `WorkspaceHarnessDialog.tsx` (layout chain), `ui/dialog.tsx` | real browser layout check (below) |

Not read: the vendor adapters, accounts storage and sign-in, `usagemetrics` storage queries other than the revision columns (no `UPDATE` of `harness_profile_revision`/`harness_override_revision` exists; they are written at insert), the checkpoint engine internals.

## Findings

### R-1 (medium) - a rewind closed the running session before the launch gate could refuse the fork

- Where: `internal/agenthub/rewind.go` `ApplyRewind`/`run` (step 2 closes the old runtime and endpoint) versus `harness_runtime.go:harnessGate`, reached only later through `openForked` → `prepareLaunch`. Reload already ran `preflightHarness` first (`recovery.go`).
- Trigger: a running session whose workspace profile gained or changed an executable since it opened (or whose snapshot cannot be resolved), then a conversation or conversation-and-files rewind.
- Consequence: the rewind failed with `harness.newSessionRequired` / `harness.snapshotUnavailable`, and by then the live process and its endpoint were closed; every reopen meets the same gate, so a working session became unreachable until the workspace profile changed back. A session with no harness is unaffected.
- Repro: `TestARewindTheLaunchGateRefusesLeavesTheRunningSessionAlone` (two cases). Failed on `c2d3074f` ("closed the running session's endpoint (0 closes before, 1 after)").
- Fix: `78bfd1e5` runs `preflightHarness` after the claim and before step 2. The launch repeats the check.

### R-2 (medium) - an account move did the same, and blamed the target account

- Where: `internal/agenthub/account_switch.go` `switchAccountForTurn` → `moveSession` (closes the process) → `runtimeFor` (the gate) → `abandonMove` (`MarkCooldown(target)` and a "could not be opened" notice).
- Trigger: the same workspace change, then a turn that the account policy moves to another account.
- Consequence: the working process was closed, the gate refused the reopen, the target account was put on cooldown for a refusal that is about the workspace, and the turn failed.
- Repro: `TestAnAccountMoveTheLaunchGateWouldRefuseLeavesTheSessionAlone` (composed rig). Failed on `c2d3074f` with `harness.newSessionRequired` and the log line "could not open on account work".
- Fix: `908d7d71` runs the same preflight before `moveSession`; a refused preflight skips the move and the turn runs on the process it has.
- Limit: a gate that passes but a launch that then fails for another reason still goes through `abandonMove` as before.

### O-1 resolved to a narrower window (low-medium) - files restored before the stale-launch check

- Reproduced (not only reasoned): `TestAFilesRewindWhoseLaunchGoesStaleChangesNoFile` (real git, real SQL, an S epoch move at the fork). On `c2d3074f` the rewind returned `ErrHarnessLaunchStale` with the transcript uncut and the dropped turn's file already deleted.
- Fix: `786440e0` asks `checkServingLaunch` (the same conditions `publishServingLaunch` applies, nothing published) before step 6, discarding the fork and failing the safety row with the project untouched.
- Residual as found in this pass (the update below handles it; it cannot be closed by holding the coordinator across a file restore, which the lock-order note forbids): an S commit that lands **during** the restore itself still makes step 7 refuse after the files changed. Outcome then: files restored, conversation uncut, the safety checkpoint (marked failed, snapshot kept) is the way back, a new rewind completes. No test existed for that window at this pass; the update below adds them.

- **Update: finished forward (`c9b7543e`, owner's choice; the restore-before-cut order is kept).** The residual above is handled instead of only documented. After the files were restored, a
  publication refused because S moved is not reported as "nothing cut": the staged fork and its endpoint are discarded (through the launch's own close, which detaches only its own incarnation), and
  the launch is prepared again by the ordinary rules from the same session reference, so the executable-adoption gate still applies and nothing is inherited from the discarded endpoint. The files are
  not restored a second time; the rewind target, the cut, the safety row and the vendor-session lookup are the original operation's. At most `launchAttempts` (3) publications in all.
  **Only** a stale epoch of a session that is still open is retried (`launchStaleError`, a typed `ErrHarnessLaunchStale`; the session row is re-read because a completed Close moves the same epoch). A Close in
  progress or whose cleanup failed, a lost endpoint, a closed or deleted session, a cancelled caller, a fork failure and a store failure are final. When it cannot finish, the error is
  `RewindPartialError` (matches `ErrRewindFilesRestored` and its cause) naming the safety checkpoint; the bridge publishes a fixed sentence ("The files were put back, but the conversation was not rewound. Rewind the same
  message again to finish, or undo the file restore from its safety checkpoint.") and never the cause. A conversation-only rewind has no restore and is unchanged (a stale launch is still refused with nothing changed).
  The cut, the turn and checkpoint rows, the safety row and the applied reference still commit in the single `RewindSession` transaction (`TestRefusalsOtherThanSAreNotRetried/the cut transaction fails`: failing the
  reference write rolls the cut back). Tests, in a rig with real git, real SQL and a live-handle endpoint registered with the session coordinator: `TestAnSChangeDuringTheRestoreFinishesTheRewindForward` (S moves inside the restore;
  one restore, two forks, one cut, endpoints 2 opened / 2 closed, new incarnation acknowledged, durable reference equals memory), `TestRepeatedSChangesAreRetriedAFiniteNumberOfTimes` (three attempts, partial error, safety row
  failed and named, nothing staged, the same rewind again completes), `TestACloseBetweenRetriesEndsTheRewindWithoutAnotherAttempt` (completed and fenced Close), `TestRefusalsOtherThanSAreNotRetried` (fork, SQL, cancellation),
  `TestLaunchStaleReasonsAreDistinguishedAndAllMatchTheOldError`, and the bridge mapping tests. Mutation-checked: never retrying, discarding nothing before re-preparing, and retrying a completed Close each fail named tests
  (retrying a fenced Close is stopped by the fence itself). **Limits:** the second fork is a second vendor process (one more `Fork`/`Delete`); the retry bound is the same constant as launch preparation; the rig's endpoint is a
  fake with the real coordinator, not the MCP host (the host's own incarnation checks were read, not driven, here); not exercised against a real vendor. Author-written fix: **pending another reviewer**.

### R-3 (low, latent) - `OnChanged` ran under the task write lock (code from #375)

- Where: `internal/tasks/service.go` `mutateAs`, as merged in #375 (`f8e99e04`): `s.changed(...)` was called before the deferred `writeMu.Unlock`, against the field's contract ("never call this while holding a lock").
  The review bot on #375 raised it at `f8e99e04` as a Minor comment; it holds. Today's only subscriber (`main.go:152`) emits an app event and cannot block, so nothing deadlocks now.
- Consequence if a subscriber ever wrote a task: self-deadlock on the non-reentrant `writeMu`.
- Repro: `TestOnChangedRunsAfterTheWriteLockIsReleased` (deadlocks on the old placement; mutation-checked).
- Fix: `daa43bf5` (`mutateLocked` returns the boards, `mutateAs` notifies after the unlock). Author-written code and an author-written fix: **pending another reviewer**.

### F-D (low, pre-existing, not fixed) - the shared dialog body overflows the card's padding, so action rows hang 16 px below the card

- Where: `frontend/src/components/ui/dialog.tsx` `INHERIT_LAYOUT.maxHeight: "inherit"` with the body class `shrink-0`: the body takes the box's `max-h` (here `85vh`) although the box's content area is 32 px smaller, so a dialog whose content reaches the limit overflows its own bottom padding.
- Measured in a real browser (below): the action row bottom is 16 px past the card's bottom edge, at every size tested. The **existing** editor footer (`Close` / `Review changes`) shows the identical offset (726 vs 710), so #377 did not introduce it and the repair view is not worse than the editor.
- Consequence: cosmetic and not blocking (buttons stay on screen and clickable); every dialog of this shape has it. Fix belongs to the shared component (for example letting the body shrink inside a flex box), which affects other dialogs, so it is left for the owner.

### Observations (no defect claimed)

- `servingCoords` gets an entry for a new session id in `reserveServing` and is dropped only on delete (`service.go:1745`), so a create that fails after the launch was staged leaves one small coordinator behind.
- `rebaseBudgets` (`workspaceBudgetModel.ts`) says it drops intents for tools the server no longer registers but keeps every tool intent (only groups are judged, and only when the group list is non-empty); a stale intent is refused by Go when prepared. Comment accuracy only.
- `AgentHarnessAutoNarrowingSet`'s UI-only check is in the bridge (`uiOnly`), not in the preferences service; its only callers are the bridge and the read in `harness_config.go`. No MCP route reaches it.
- Unchanged from the first pass: O-3 (cut-only change is neutral narrowing by specification), O-4.

## Review of #377 (F-C repair) at its final head

- Final head `f941aaf7`; versus the head a reviewer read (`43f18027`) it changes only `StaleRepairPanel.tsx` (18 lines), `HarnessDialogs.test.tsx` and one doc paragraph.
- Independent evidence that exists: a review relayed in this task's request (not a repository or PR artifact; the PR carries only the automated reviewer's comments) states, for `43f18027`, no new backend defect in UI-only permission, confirmation or CAS, focused repair tests green under `-race`, CI green, and one P2 on the layout (fixed in `f941aaf7`). It said it did not run the native dialog. That is input on `43f18027`, not on the final head.
- This pass did **not** independently review the F-C backend: the reviewer wrote it. It re-ran the focused tests inside the race run below and nothing more. F-C remains "fixed, independently reviewed once at `43f18027` (relayed), not signed by this session".
- Layout, checked in a real browser (Vite page with the bridge calls stubbed, the real `WorkspaceHarnessDialog`, fixed-size iframes; scratch files outside the repository, not committed; **not** the native window):

| Frame | Diagnosis, 64 entries | Review, 64 changes |
| --- | --- | --- |
| 1000 x 700 (min window is 1080 x 700, `main.go:71`) | scroll region 397 px of 3014 px, last item reachable, actions on screen | scroll 397 px of 3846 px, last reachable, actions on screen, focus on `Cancel`, editor and its footer absent |
| 420 x 480 | scroll region 150 px, last reachable, actions on screen, no horizontal overflow | not run |
| 320 x 360 (below the supported window) | scroll region collapses to 8 px; actions stay on screen | not run |

  Long names wrap (no horizontal overflow at any frame). 1 entry: same chrome, actions on screen. The 16 px hang is F-D. Native focus, the OS window and real Wails bindings remain AP3.

## Item 8: #375 evidence, as found

- Artifacts in the repository or on the PR: automated review comments only. At `8dfd4014` the bot raised the double-resolution finding (fixed in `c8975ff6`); at `f8e99e04` it raised the `OnChanged` placement (R-3, fixed here). Nothing on the PR states that the final tree was re-reviewed with no findings.
- Relayed in this task (not artifacts): two reviews of #375 at `05029383` and `8dfd4014` with reproduced findings (symlink repoint, Release erasing a new claim, second resolution), and a statement that the P1 reproduced with real Git and that focused `-race` tests passed. They are real findings that were fixed and are evidence that the shape of the code was reviewed by someone else at those heads; they are not a review of `f8e99e04` or later.
- Re-read in this pass (by the author of that code, so not independent): `gitFor` resolves once and uses that value for the scope decision, the candidate and the `Toplevel` check; `writeClaim` re-applies the holder rule to the metadata read under `writeMu`; `Release` and the branch write refuse a live holder under the same lock; `heldByAnotherLiveSession` calls the liveness check (`agentLiveness.Alive`, a store read through `GetStatus`, no `Service.mu`) under `writeMu`; `internal/agenthub` does not import `tasks` and I found no call into it under `Service.mu` (searched by import and by reading, not exhaustively), so I found no lock cycle. One defect (R-3).
- Item 8 therefore stays **A** (author-reviewed) for the git scope and claim-lock code. It needs a reviewer other than this session.

## #375 directory-swap limit, in the real threat boundary

- What remains: after `gitFor` resolves, checks and opens a canonical path, each later Git command is exec'd with that path as its directory. An actor that can replace a real directory component of the path (rename it away, put a link to another repository in its place) in the instant between the check and an exec, and keep flipping, makes that command run in the other repository. The `Toplevel` check runs once, at open; it does not cover later commands.
- Who can do that: an agent whose jail lets it write the folder. The seatbelt profile allows `file-write*` on the session's writable subpaths (`internal/sandbox/seatbelt_profile.go`), which by its text includes creating links and renaming; this was **not** exercised (no sandbox run here). The agent is the party the git scope exists to contain (AGENTS.md: model output is untrusted), so the threat is in scope, but it needs a deliberate race, not an accident.
- Impact if won: reads (`git.log`, `diff`, `show`) and the guarded writes against a repository the session was never granted; the branch/task guards decide on the repository as checked.
- Does activation depend on it? No. Tool groups are on unless a stored profile turns one off (`internal/harness/resolve.go`: "absent is on"; S can only turn groups on), so the Git tools are on today whether or not the harness is wired. Activation adds no Git exposure and removes none; the group filter never relied on path containment. By the plan's criterion (activation relies on containing an agent that can exploit the race) this is **not an AP4 blocker**.
- It is, however, an open defect in shipped code, not a harness property, and it is **not mitigated**: descriptor-bound execution in `internal/git` (open the directory once, verify device/inode of the top and `.git`, and run Git against that handle; macOS has no `openat2`, so this needs `fchdir` in a helper or a re-verified-before-and-after check, which narrows but does not close the window). That is a design of its own with owner review; it is not designed or implemented here.
- Recorded as: **open pre-existing limit; owner decision to schedule; not an activation blocker; do not describe the Git scope as a hard boundary against a hostile agent**.

## O-2 decision proposal (sessions with no applied reference)

Inventory (read, not invented):

- Stored executable-bearing fields: only the **workspace profile** (`harness.Profile`: `Servers`, `SkillFolders`; `DisabledMCP` and `DisabledSkills` are narrowing). The session override S holds group names only. The applied reference (`SnapshotRef`) stores hashes (`ExecutableEntries`, `WorkspaceOwned`, `WorkspaceSuppressed`, `ExecutableDigest`), not the entries. The vendor customization (V) lives in Settings and was always applied at launch.
- Launch and adoption rules: `harnessGate` calls `AdoptsNewExecutables(previous)` only when a previous reference exists (`harness_runtime.go:340`); `previousHarness` returns nil when none is stored; a session created while the harness was inert has none. Its first reopen after activation therefore runs the **new** merge (V + W) with no adoption check.
- What can be authored today: nothing reaches `Profile.Servers`/`SkillFolders` through the product. `GroupRequest`, `GroupEdit`, `BudgetEdit` and the F-C repair cannot carry executable fields (the repair only removes stale group and budget keys and keeps every other field as it was). Executable proposals are out of scope and unimplemented. A profile with executables can exist only if written outside the product.
- So at AP4 the O-2 gap is **unreachable through the product**: no old session can adopt a workspace executable that nobody can create. It becomes reachable the day any path can write `Servers`/`SkillFolders`.

Proposed decision (for the owner; nothing here is implemented, and no baseline is stamped from the current W):

1. Accept O-2 for AP4 **on the recorded precondition** that no product path writes workspace executables (executable proposals stay unshipped).
2. Before any such path ships, add the conservative rule: a reopen (resume, reload, rewind, account move) of a session that has **no** stored reference and whose resolved snapshot has any workspace-owned executable is refused with `ErrHarnessNewSessionRequired`. It states a fact about the legacy launch path (it had no workspace-owned executables) and does not claim W was ever approved. New sessions always write a reference at creation, so they are not affected; sessions without a workspace have no snapshot and are not affected.
3. Not chosen: recording a baseline from today's W at activation (it would treat current W as prior approval) or enabling executable proposals to solve it.
4. Test to write with the rule: legacy session (reference deleted) + W with a server => refused on every reopen path; same session + W without owned executables => reopens and writes its reference.

## Row status after this pass

| # | Item | Result | Indep. | Pending |
| --- | --- | --- | --- | --- |
| 1-3 | Basis, approval/CAS, durable vs live | unchanged: no finding | I | - |
| 4 | Coordinator, reconcile, crash, account reopen | traced; R-2 found and fixed; crash-between-commit-and-publish probe passes | I for the reading; **A** for the R-2 fix (`908d7d71`) | another reviewer for the fix |
| 5 | Rewind SQL, #353/#363/#367, child | rewind SQL read in full (one transaction: vendor id, cut, turns, checkpoints, safety row and applied reference); child isolation exercised; R-1 and O-1 found and fixed | I for the reading; **A** for `78bfd1e5`, `786440e0` and `c9b7543e` | another reviewer for the fixes (`78bfd1e5`, `786440e0`, `c9b7543e`); the O-1 window during the restore is finished forward, with stated limits |
| 6 | Public facade | every method of `harnessControls` except the agent's own `Profile`/`Request`/`ProfileWithBudgets` calls `requireUI` (`WorkspaceProfile`, `SessionProfile`, `WorkspaceBudgets`, `PrepareWorkspaceGroups`, `PrepareBudgetEdits`, `PrepareRevoke`, `StaleProfile`, `PrepareStaleRepair`, `Apply`, `Cancel`, `Reconcile`) and the workspace-scoped ones `requireWorkspace`; the policy setter is window-only in the bridge; `SessionToolGroups.tsx` read; no finding | I | native focus is AP3 |
| 7 | H5b, policy, measurement | no finding; revision columns are written at insert only and read-only afterwards | I | usagemetrics storage not read |
| 8 | No new authority | R-3 found; git scope and claim lock re-read by the author | **A** | another reviewer for #375 code and `daa43bf5` |
| F-C | #377 | one relayed independent review at `43f18027`; layout checked in a browser at `f941aaf7` | mixed | another reviewer for the backend at the final head; native dialog (AP3) |

AP1 is complete only when every row has independent evidence and nothing blocks activation. Still open: the fixes above and the #375/#377 code (authored by this reviewer) need a different reviewer; F-D is unfixed; the O-2 decision and the directory-swap limit await the owner; AP3 and AP4 are not run.
