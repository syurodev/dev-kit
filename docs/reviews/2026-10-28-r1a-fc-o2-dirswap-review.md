# R1a independent review: F-C (#377), O-2, #375 directory swap

**DRAFT (sections 3 and 4 still being written).** Date: 2026-10-28. Reviewed base: `origin/main` `8c5e64d097540173874b429eb9737196fab44116` (contains #375-#381; #377 merged at `c2d3074f`, final head `f941aaf7`).

## 0. Independence and method

I am an AI coding session (another instance of the Claude model family, not a human reviewer). I did not write #375, #377, the R-1/R-2/O-1 fixes or any code reviewed here; the earlier AP1 author was a different session. This is review input, not an ordinary merge approval and not a sign-off by a person. I changed no production code and wrote no fix.

Production is inert. Nothing was enabled; no real vendor runtime, account, paid request, real workspace or credential file was used. Every probe ran on temporary databases with scripted vendors (existing test rigs) or on a scratch browser page.

An earlier attempt of this task left nine files under `docs/reviews/2026-10-28-r1a-fc-o2-dirswap-review.repros/`. Treated as unverified leads. Re-run by me: `stale_session_override_group.go.txt` (reproduced), the layout scripts (re-run with changes: smooth-scroll fix, shot script rewritten), `o2_legacy_adoption_and_rollback_erasure.go.txt` (see section 3), `git_tools_host_boundary.go.txt` (see section 4). `repair_summary_bound_probe.go.txt` is mine.

## 1. F-C repair (#377) at `8c5e64d0`

### Read (files)
`internal/harnessconfig/repair.go` (all), `apply.go` (Apply, take, drop, beginCommit, mapApplyErr), `service.go` (pending, Cancel, evaluate target check), `internal/app/harness_public.go` (StaleProfile, PrepareStaleRepair, Apply, Cancel), `internal/bridge/harness_controls.go`, `frontend/src/workbench/{staleRepairController.ts,StaleRepairPanel.tsx,WorkspaceHarnessDialog.tsx}`, `internal/harnessconfig/repair_test.go` and `internal/app/harness_stale_repair_test.go` (names and the key cases), `git diff 782a63c5 origin/main --stat` over `internal/harness` (the resolver is not in the diff: it is unchanged by #377).

### Traced, no finding
- UI-only identity: `requireUICaller` in `DiagnoseStale` and `PrepareRepair`; the facade repeats `requireUI` and `requireWorkspace`; `Service.Prepare` refuses any `Target` other than session/workspace (`service.go:275`), so no `Request` carries `TargetRepair`; no MCP tool references repair. `take` and `Cancel` bind the id to `caller.String()`.
- Server-derived removals: the caller passes only a workspace id; `InspectStale` over stored W plus registry; `RepairedProfile` deletes exactly those keys; Apply rescans and requires an equal digest (W stamp, removed entries with values, registry identity, vendor revisions, policy revision, repaired body hash).
- Strict resolver: `validateWhole` runs the ordinary Validator and `harness.Resolve` for every vendor on the repaired candidate; corrupt, wrong schema, other invalid settings, no registry are `unrepairable`, never partially repaired.
- Always confirms: `applyRepair` calls `confirm` unconditionally (tier widening); the auto-narrowing preference is never consulted; no confirmer or no boundary fails closed.
- CAS and ambiguity: single `Store.Apply` with `Expect: sc.stamp` inside `Boundary.Hold`; `storeCommitUnknown` is read back (`Expect+1` committed, `Expect` proven not written, else unknown); never retried.
- Spent-on-use: `take` marks `taken`, `drop` deferred; replay finds nothing. Cancel before `beginCommit` wins; after it, `ErrTooLateToCancel`.
- Preserved: only `ProfileWrite` is written (no S, no applied reference, no executable fields); the review text says running sessions keep their basis and the usual executable checks still apply at next open (true, `harnessGate` is unchanged and W's executable fields are untouched).

### Exercised
- `go test ./internal/harnessconfig/ ./internal/harness/ -count=1`: ok.
- `go test ./internal/app/ -run 'Stale|Repair|OffSettingForAnUnwired' -count=1 -v`: 14 tests PASS (including the 13 F-C cases listed in `harness_stale_repair_test.go`).
- `cd frontend && bun install --frozen-lockfile` (no changes), `bun test --isolate src/workbench/staleRepairController.test.ts src/workbench/HarnessDialogs.test.tsx src/workbench/workspaceHarnessModel.test.ts`: 40 pass, 0 fail.
- Not run by me: mutation checks of each guard (relied on the author's claim and the named tests), `-race`.

### Findings
- **R1a-1 (medium, new, not a fail-open): the repair covers W only; a session override (S) that names a no-longer-registered group locks that one session.** Repro: `repros/stale_session_override_group.go.txt` (overlay in `internal/app`, removed afterwards). Sequence: session enabled `pdf` through S while the module was wired; build restarted without the module; W repaired through the real F-C path (works, new sessions start); then for the existing session: `ResumeSession` returns `harness.snapshotUnavailable` ("could not be resolved, so it was not started"), the window revoke (`PrepareRevoke` of `pdf`) returns `harness.groupUnknown`, and a `Snapshot` call fails. `CloseSession` succeeds, clears the override, and `ResumeSession` after it succeeds. So the way out is closing the session, which discards S; nothing tells the person that. Consequence: after the "repair complete" message a person's existing session still cannot reopen and the error does not name S. The review and dialog text only speak of "new sessions". It is not a widening and no executable is gained. Needs an owner decision: extend the repair to S, document Close as the recovery, or make the message say so.
- **R1a-2 (low, new): the advertised bound of 64 stale entries is not reachable.** The review summary is capped at 8 KiB (`view.go` `maxSummaryBytes`) and `PrepareRepair` returns `ErrTooLargeToShow` ("Split it into smaller changes", which cannot be done for a repair) while `DiagnoseStale` still reports `stale` and the window offers "Review repair". Measured (`repros/repair_summary_bound_probe.go.txt` and an earlier variant): 64 stale groups prepare with names up to 64 chars, but 48 and 64 groups fail with 128-char names; 64 stale group budget rules with 40-char names and a size and cut already fail. Fail-closed (nothing written); the workspace stays unable to start sessions and the message is misleading. Practically needs tampered or very drifted data (real group/tool names are short). Fix options: lower `MaxStaleEntries`, or make the diagnosis compute the summary and report `truncated`/unrepairable.
- **R1a-3 (low): a stale repair's Apply can be abandoned from the "Result limits" tab.** The tab's `Close` button calls `onClose` directly; only the Dialog's `onOpenChange` is guarded by `staleState.busy`. Measured in the browser: while the repair apply waits for confirmation the limits-tab Close is enabled and clicking it closes the dialog (`closeCalls` 0 -> 1). The Go apply continues; the outcome is never shown; the next open re-reads the diagnosis, so nothing is untruthful, but the person gets no result for a write that may still happen. The same pattern exists for the ordinary editor's apply (pre-existing).
- **R1a-4 (low, cosmetic, pre-existing F-D confirmed): the action row sits 16 px below the card bottom whenever the dialog reaches its max height** (measured `actions bottom - card bottom = 16` at every size with 64 entries; `-16` with one short entry). Buttons stay on screen and clickable.

### Layout check (real browser, not the native window)
Method: Vite (`bunx vite`, scratch config outside the committed tree, stubbed `AppService`) serving the real `WorkspaceHarnessDialog`; headless `google-chrome` 156 with `--dump-dom`; measured with `getBoundingClientRect`/`elementFromPoint`. Stub returns diagnosis and review shaped like Go's; 64 entries; "long" variant uses ~330-char unbroken names. Scripts: `repros/layout_check_*.txt`. The stub is not the real bindings; focus is browser focus, not native.

| Viewport (CSS px) | State | Scroll region (client / content) | Last item reachable | Actions on screen and topmost | Notes |
| --- | --- | --- | --- | --- | --- |
| 1080 x 700 (the minimum window, `main.go:71-72`) | diagnosis 64 | 397 / 2662 | yes | yes | no horizontal overflow |
| 1080 x 700 | diagnosis 64 long | 397 / 4710 | yes | yes | names wrap |
| 1080 x 700 | review 64 | 397 / 4310 | yes | yes | focus on Cancel; ordinary editor, its footer and "Review changes" absent |
| 1080 x 700 | review 64 long | 397 / 9686 | yes | yes | same |
| 1080 x 700 | apply waiting | same | yes | buttons disabled | focus kept on (disabled) Cancel |
| 1080 x 644 | 1 entry | 142 / 142 | yes | yes | actions 16 px above card bottom edge |
| 500 x 424 (below the minimum) | diagnosis/review 64 long | 122 / 6458, 14606 | not fully inside (items taller than the region) | yes | actions stay on screen; focus falls to BODY while apply waits |
| 700 x 257 (far below) | 64 long | 20 / 4710 | no (region too short) | yes | region collapses but scrolls |

Limit: "last reachable" requires the whole last item inside the region, so a region shorter than one wrapped item reports false; scrolling still works by definition. Initial focus in the diagnosis state is the "Tool groups" tab, not Cancel (Cancel is focused in the review state). Horizontal widths other than 1080 and 500 were not exercised. Native window, OS focus and real Wails bindings: out of scope (R3).

### Item status F-C
Independent evidence now exists for: UI-only identity, server-derived removals, strict resolver, always-confirm, binding/CAS/ambiguity, spent-on-use, preserved S/refs, and the layout at the minimum window. New findings R1a-1..R1a-3 are not blockers of the repair itself but R1a-1 is a recovery gap that should be decided before activation.

## 2. (reserved)

## 3. O-2 decision document: TO BE WRITTEN

## 4. #375 directory swap: TO BE WRITTEN
