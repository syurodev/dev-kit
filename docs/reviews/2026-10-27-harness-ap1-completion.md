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
  UI-only, confirmed removal of exactly the stale keys in W.Groups, W.ToolBudgets and W.GroupBudgets; the resolver stays strict. Details and tests are in
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
