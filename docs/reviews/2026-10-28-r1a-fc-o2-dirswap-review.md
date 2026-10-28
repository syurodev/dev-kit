# R1a independent review: F-C (#377), O-2, #375 directory swap

Date: 2026-10-28. Reviewed base: `origin/main` `8c5e64d097540173874b429eb9737196fab44116` (contains #375-#381; #377 merged at `c2d3074f`, final head `f941aaf7`).

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



## 2. O-2: sessions that have no applied reference (decision document, nothing implemented)

### 2.1 Inventory (actual code at `8c5e64d0`)
Read: `internal/agenthub/harness_runtime.go` (all), `store_harness.go`, `harness_status.go`, `rewind_store.go:125-140`, `internal/harness/snapshot.go` (`SnapshotRef`, `AdoptsNewExecutables`, `Effective.WorkspaceOwned/Suppressed/ExecutableEntries`), `internal/harness/types.go` (`Profile`, `Override`), `internal/harnessconfig/patch.go` (`ProfilePatch`), `service.go:195-210`, `apply.go:160-166`.

- **Executable-bearing stored fields.** Only the workspace profile W (`harness.Profile.Servers`, `SkillFolders`; `DisabledMCP`/`DisabledSkills` only narrow). The session override S is group names only. The vendor customization V (Settings) was always applied at every legacy launch. The durable applied reference (`agent_session_harness_applied`: `ref` JSON, `enforced`, `applied_at`) holds hashes only: `ExecutableEntries` (hash(name) -> hash(definition)), `WorkspaceOwned`, `WorkspaceSuppressed`, `ExecutableDigest`, revisions, fingerprint; no definitions, paths or commands.
- **Rule.** `harnessGate` refuses with `ErrHarnessNewSessionRequired` only if `previous != nil && snap.Ref.AdoptsNewExecutables(previous.Ref)`. `AdoptsNewExecutables` counts an entry that is new or altered versus `previous` **and** workspace-owned (now, before, or un-suppressed). Vendor-owned changes never count. With `previous == nil` the gate cannot fire.
- **Where `previous` comes from.** `previousHarness`: in-memory record, else the durable row; a row that is unreadable (bad JSON, schema mismatch, wrong session id) is `ErrHarnessUnavailable` (fail closed, not "absent"). Create passes `nil` on purpose (`account_create.go:265`). Resume/reload/account reopen (`service.go:2753`), the rewind forks (`rewind.go:763`) and the three preflights (`recovery.go:79`, `account_switch.go:190`, `rewind.go:317`) use `previousHarness`.
- **How a session ends up with no reference.** (a) It was created while `Service.Harness == nil` or with no workspace: `recordHarnessApplied` with `snapshot == nil` **deletes** the row (`harness_runtime.go:330-338`). (b) **Any later launch by an inert build deletes the reference of a session that had one**, and so does a rewind commit with `HarnessRef == nil` (`rewind_store.go:137`); this is asserted as intended by the `DELETE` case of `TestRewindReferenceWriteFailureRollsBackConversation`. (c) A session with no workspace never gets one (correct: W cannot apply).
- **What the read model says.** `HarnessStatus` already has `HarnessNotApplied` for `applied == nil`; it is not mapped to "new session required".
- **Reachability of W executables.** `ProfilePatch` has `Servers`/`SkillFolders`, but `Prepare` and the commit step refuse tier `executable` unconditionally: `ErrExecutableDenied` if the policy forbids, else `ErrExecutableNotReady` (`service.go:202-208`, `apply.go:160-166`); the window facades (`PrepareWorkspaceGroups`, `PrepareBudgetEdits`, `PrepareRevoke`, `PrepareStaleRepair`) construct group/budget patches only; the MCP harness tool schema has no server/skill field (grep of `internal/mcp/tools_harness.go`); the F-C repair preserves other fields. W is not synchronised (no sync code references the table). So today a W with executables can exist only if written outside the product (database edit, a different build).

### 2.2 Exercised (probes in `repros/o2_legacy_adoption_and_rollback_erasure.go.txt`, overlay in `internal/agenthub`, removed afterwards; real SQL store, scripted vendor/launch env)
`go test -p 1 ./internal/agenthub/ -run TestR1aProbe -count=1 -v`:
1. `TestR1aProbeLegacySessionFirstReopenAdoptsWorkspaceExecutable` (passes, logs): a session created with the harness unwired (no row) is reopened after a harness is wired and W owns server `db`: **no error, `db` is in the launch spec, a reference is written with `workspaceOwned=1`.** The new executable is adopted silently and from then on is the baseline.
2. `TestR1aProbeRollbackBuildErasesTheAppliedReference` (**fails by design of the probe**): control with a reference present and W adding a server is refused (`harness.newSessionRequired`); after a restart with `Harness = nil` and one reopen the row is gone (`found=false`); on re-activation with W owning the server the session reopens and **adopts `db` without a refusal**. So the O-2 gap is not only "created while inert": a rollback (or any inert-build run) after activation recreates it for sessions that had a reference.
Limit: the probes use the agenthub test launch environment, not the composed app or a real vendor; "inert build" is simulated by `Harness = nil` on a restarted service.

### 2.3 Safety criterion check
"An old session must not silently gain a new workspace executable merely because its prior reference is missing." Today (code as merged): **violated in the code path** (probe 1), **not reachable through the product** (2.1). It becomes reachable the day any path can write `Servers`/`SkillFolders`, or by a manual/other-build write, and the rollback route (probe 2) means "sessions created while inert" is not the only affected set.

### 2.4 Options
| | Option | Safety | Data | Cost / risk |
|---|---|---|---|---|
| A | Accept, on the recorded precondition that no product path writes W executables | Holds only while executable proposals stay unshipped; a hand-edited W or a rollback still adopts silently | none | Zero code; the precondition is a convention enforced by one unconditional refusal (`ErrExecutableNotReady`); easy to lose when executable proposals are built |
| B | **Stateless gate rule**: on any *reopen* (resume, reload, rewind forks, account move, and their preflights), if there is no stored reference and the resolved snapshot has any workspace-owned executable (`len(Ref.WorkspaceOwned) > 0`), refuse with `ErrHarnessNewSessionRequired`. Create is unchanged (it passes "new"). | Closes probe 1 and probe 2 (absence is treated the same however it came about). States only a code fact about legacy launches: they never merged W. Claims nothing about W being approved. | Reads only; writes no row; the session, transcript and every reference stay as they are. The person opens a new session to use the workspace server, as for any changed executable. | Small gate change plus an explicit "reopen" flag at the callers (create must not be refused). A no-reference session in a workspace whose W owns an executable can never reopen (history stays readable; same UX as the existing refusal). Needs owner review: it changes launch behaviour of existing sessions on activation |
| C | Stamp a baseline reference from today's W at activation (or lazily on first reopen) | **Rejected.** Treats the current W as the prior approval and invents history; for a rollback case it would also bless whatever W became meanwhile | writes rows to every old session | Migration plus an irreversible claim |
| D | Persist a "legacy, no workspace executables" baseline for each old session (empty `WorkspaceOwned`/entries) instead of a gate rule | Equivalent to B for sessions that exist at activation, but not for sessions created later by an inert/rolled-back build, and it needs a sweep | writes rows to all sessions | Migration; weaker than B; not needed if B ships |
| E | Make "not applied" a user-visible state (map `HarnessNotApplied` plus owned executables to `HarnessNewSessionRequired` in `HarnessStatus`) | Presentation only; no protection by itself | none | Add alongside B so the window tells the truth before the launch refuses |

Not chosen, as the task says: enabling executable proposals to make the problem go away.

### 2.5 Recommendation (for the owner; nothing implemented)
1. **AP4 may proceed on O-2 only if** (i) the precondition "no product path writes workspace executables" is recorded as an activation condition, **and** (ii) Option B (with E) is either implemented first or recorded as a hard prerequisite of *any* change that lets W carry executables. If the owner wants no reliance on the precondition, B must ship before AP4: it is small, stateless and covers rollback.
2. Separately decide whether inert builds should keep deleting the reference (`harness_runtime.go:336`, `rewind_store.go:137`). Not deleting on `Harness == nil` preserves existing data and keeps the old guard through a rollback; deleting is the current, tested behaviour, so changing it needs owner review and test changes. B makes this decision less urgent, because absence is judged conservatively.
3. Tests to write with B (reviewer's list, not written): legacy session (row deleted) + W with a server => refused on **each** reopen path (resume, reload, rewind forks, account move, the three preflights) and the running session is left alone; same session + W with only groups/budgets, or with no owned executable => reopens and writes its reference; create with a W-owned server still succeeds; unreadable row still `ErrHarnessUnavailable`; a vendor-only change with no reference does not refuse; the two probes above inverted.
4. Evidence status: independent evidence now exists for the inventory and for the two behaviours (read and exercised at probe level); the decision itself is the owner's and is open.

## 3. #375 directory swap: threat boundary, activation criterion, design

### 3.1 What the code does (read)
`internal/mcp/git_repo.go` `gitFor`: resolves the requested path once (`realPath`, `EvalSymlinks`), decides scope on that value, opens the Git service at it (`GitRepos.At`) and checks once that the opened checkout's `Toplevel()` contains it. `internal/git/git.go:92,103`: every later command is `exec.CommandContext(git, "--no-optional-locks", "-C", r.work, ...)`, i.e. the **path is re-resolved by the kernel for each command**. The package comment states that commands run "in the work tree with the repository's own configuration". The Git tools are served by the host's MCP server, so this git runs in the DevKit host process tree, **not** inside the agent's jail (AGENTS.md: the agent is untrusted).

### 3.2 Exercised (probes in `repros/git_tools_host_boundary.go.txt`; overlay in `internal/mcp`, removed afterwards; real `git` 2.x on linux/amd64, real filesystem, no jail)
`go test -p 1 ./internal/mcp/ -run TestR1aProbe -count=1 -v`, all four **fail** (each failing = the weakness reproduces):
1. `...DirectorySwapAfterOpenRunsInAnotherRepository`: after `gitFor` opened a granted repo, renaming it away and symlinking another repository into its place made the **same handle** answer `"VICTIM-REPO-OUTSIDE-THE-GRANT"` (deterministic, no race).
2. `...DirectorySwapRaceIsWinnable`: a flipper using `renameat2(RENAME_EXCHANGE)` between the real dir and a link, 400 tool calls: 376 refused at open (check works most of the time), **13 answered from the outside repository**, 11 from the granted one (about 3% of calls won with a naive loop; 891,932 flips).
3. `...GitdirFileRedirectsAGrantedFolderToAnotherRepository` (**not a race and not the directory swap; a separate defect**): a granted folder whose `.git` is a *file* `gitdir: <other repo>/.git` passes the `Toplevel` check (the work tree root is the granted folder) and the handle reads the other repository.
4. `...RepositoryConfigRunsInTheHostsGit` (adjacent, separate): a granted repository's own `core.fsmonitor` command ran in the host's git on `Status` (marker file created). The service deliberately honours repository configuration (see 3.1); an agent can write `.git/config` (and hooks) in its own work tree.
Not exercised: any real jail (no seatbelt or Linux sandbox run), macOS behaviour, writes through a redirected handle (the earlier F1 test shows `git.branch.create` writes through a repointed link), credential helpers on a redirected remote, hooks.

### 3.3 Who can exploit what
- Attacker: an agent session (untrusted, per AGENTS.md) that can write inside its granted folders and call `git.*`. Creating links, renaming directories, writing a `.git` file or `.git/config` are ordinary file writes in its own work tree; the seatbelt profile allows `file-write*` on the session's writable subpaths (read in the earlier pass; not re-exercised).
- Effects: reading (`log`, `diff`, `show`) and the guarded writes (branch create, commit, push gates) against a repository the session was never granted, with the host's identity and credentials; and, via repository configuration, code execution in the host's git outside the jail. The branch/task guards judge the repository **as checked**, so they do not cover the redirected one.
- The earlier ledger's statement that the swap "needs a deliberate race" is true for probe 2 only; probes 1 (post-open) and 3 (gitdir file) need no timing.

### 3.4 Activation criterion
Literal criterion of the plan: *does activation rely on containing an agent that can exploit the race?*
- The harness's group filter is independent of path containment and the Git group is on by default today (`resolve.go` "absent is on"), so activation neither adds nor removes Git exposure; it adds an off switch (a person can turn the `git` group off per workspace or session). On that reading it is **not an AP4 blocker**.
- But the product-level claim in AGENTS.md ("Preserve Git branch/repository grant guards") and the harness's own item 8 ("no new authority") are claims that the owner guards hold under an untrusted agent. They do **not** hold: three independent routes (two with no race) reach a repository outside the grant, and one reaches host execution. So: **Git scope must not be described as a boundary against a hostile agent, and I recommend the owner treat it as a release-security blocker to decide explicitly before AP4**, rather than as a footnote. This is the reviewer's recommendation; the blocker/not-blocker call is the owner's. The fixed alias re-resolution bug in #375 is **not** descriptor-bound execution and is not equated with it here.

### 3.5 Focused design (not implemented; descriptor-bound execution needs owner review)
Split into separately shippable steps, smallest first:
1. **`.git` indirection (probe 3).** In `gitFor`, after resolving the work tree, lstat `<top>/.git`: if it is a directory, accept; if it is a file (linked worktree/submodule), parse `gitdir:` and require the target (and `commondir`) to resolve inside the same granted/workspace scope; otherwise refuse. Also pass `GIT_DIR` and `GIT_WORK_TREE` explicitly for agent callers so discovery cannot re-select another repository. Smallest change, no descriptor work; closes a no-race hole.
2. **Host-exec (probe 4).** For agent-origin calls run git with repository-supplied execution neutralised: `-c core.fsmonitor=false -c core.hooksPath=<empty dir> -c core.sshCommand= -c core.pager= -c credential.helper=`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_ATTR_NOSYSTEM`, clear `GIT_EXTERNAL_DIFF`/filters (`filter.*.process`, `diff.*.command`, `textconv`), keep the person's terminal path unchanged. This contradicts the package's "repository's own configuration" principle for agent calls, which is a product decision (LFS filters, user hooks). The stronger alternative is to run agent-origin git **under the session's own sandbox profile**, so whatever the repo config executes has no more authority than the agent; then step 3 becomes defence in depth. Owner decision.
3. **Descriptor-bound execution (probes 1 and 2).** On open, walk the path component by component from a trusted root fd with `openat(O_DIRECTORY|O_NOFOLLOW)` (Linux can use `openat2` with `RESOLVE_NO_SYMLINKS|RESOLVE_BENEATH`; macOS has no `openat2`, so the walk is manual); record `st_dev/st_ino` of the top and of `.git`; keep the fd for the life of the handle. Execute git with that directory as cwd without a path lookup: a small re-exec helper (the app's own binary with a hidden mode, or a tiny helper) that does `fchdir(fd)` and then `execve(git, ...)`, with `GIT_DIR`/`GIT_WORK_TREE` set to `.`-relative paths verified through the fd. (`cmd.Dir=/proc/self/fd/N` does not work: Go chdir happens before `ExtraFiles` are installed; `/proc/<pid>/fd/N` works on Linux only; macOS `/dev/fd` directory handling is not reliable.) Re-verify `fstat` identity before and after each command as a tripwire (narrows but does not replace the fd). Handles must be closed on repository/session close; fd lifetime and the per-service cache keyed by path in `repos.go` need an owner look.
4. **Where scope guards read identity.** Key the branch/repository grant guards on the verified `(dev, ino)` plus resolved top, not on the path string.

Tests the design needs (none exist): inversions of probes 1-4 expecting refusal or the granted repository; a stress test using `RENAME_EXCHANGE` (Linux) and a rename/symlink flipper (macOS) that must produce **zero** foreign answers over a fixed large iteration count, run under `-race`; unit tests of the helper (fchdir, environment, cancellation, large output); mutation checks (remove the fd and expect the stress test to fail); regression for linked worktrees and submodules (legitimate `.git` files must still work); Windows is out of scope for the app's Git tools and was not considered.

### 3.6 Evidence status
Independent evidence now exists that the limit is real, larger than documented (three routes, two without a race) and not mitigated; no evidence exists that any step of the design works (design only).

## 4. Commands run (all on `8c5e64d0`, linux/amd64, Go 1.27.1, Bun 1.4.3, headless)
| Command | Result |
|---|---|
| `go test ./internal/harnessconfig/ ./internal/harness/ -count=1` | ok |
| `go test ./internal/app/ -run 'Stale\|Repair\|OffSettingForAnUnwired' -count=1 -v` | 14 PASS |
| `go test ./internal/app/ -run TestR1aProbeStaleSessionOverrideGroup -count=1 -v` (overlay) | FAIL as the probe intends: resume `harness.snapshotUnavailable`, revoke `harness.groupUnknown`; after Close, resume ok |
| `go test ./internal/harnessconfig/ -run R1aProbeRepairSummaryBound... -v` (overlay, two variants) | `ErrTooLargeToShow` at 48-64 entries with 128-char names; at 64 group-budget rules with names >= 40 chars |
| `cd frontend && bun install --frozen-lockfile` | no changes |
| `bun test --isolate src/workbench/staleRepairController.test.ts src/workbench/HarnessDialogs.test.tsx src/workbench/workspaceHarnessModel.test.ts` | 40 pass, 0 fail |
| `go test -p 1 ./internal/agenthub/ -run TestR1aProbe -count=1 -v` (overlay) | probe 1 logs adoption; probe 2 FAIL as intended |
| `go test -p 1 ./internal/mcp/ -run TestR1aProbe -count=1 -v` (overlay) | 4 FAIL as intended (3.2) |
| vite + `google-chrome --headless=new --dump-dom` layout matrix | table in section 1; stopped afterwards (`pgrep` shows none of mine) |
Overlays were deleted after each run; no production file changed; no reviewer fix was written. **Not run:** full `go test ./internal/... ./cmd/...`, `-race`, `go vet`, `bun run build`, `golangci-lint`, the arch test (no tracked file changed, so none of them is evidence for this review); mutation checks of the F-C guards; any real vendor, native window or jail.

## 5. Read only versus exercised
- **Exercised:** F-C focused Go and frontend tests; stale-S, summary-bound, O-2 and Git probes; the browser layout matrix.
- **Only read (traced, not run):** `applyRepair` ordering and Cancel/commit races beyond the author's tests; the scope/CAS binding digest contents; `harnessGate` call sites on every reopen path; the sandbox profile; `HarnessStatus`; the MCP schema (grep).
- **Not verified:** native window and focus (R3); real bindings (stub used); Git behaviour under a real jail; macOS; a rollback of an actual release; whether any shipped data already contains a W with executables (none can be authored through the product; real user databases were not touched).

## 6. Findings summary
| ID | Severity | Item | Status |
|---|---|---|---|
| R1a-1 | medium | Stale S (session override) names an unregistered group: the session cannot resume, and the window cannot revoke it; Close clears S and recovers; F-C repairs W only | reproduced; owner decision |
| R1a-2 | low | `MaxStaleEntries` 64 not reachable under the 8 KiB summary cap; misleading "split it" error; diagnosis still offers repair | reproduced |
| R1a-3 | low | Limits-tab Close is enabled while a repair apply waits for confirmation (dialog closes, outcome not shown) | reproduced in browser |
| R1a-4 | low | Action row hangs 16 px below the card at max height (F-D) | measured |
| O-2a | medium (conditional) | No-reference reopen adopts a new workspace executable silently | reproduced in test env; unreachable through the product today |
| O-2b | medium (conditional) | An inert (rolled-back) build deletes references, recreating O-2 for sessions that had one | reproduced in test env |
| G-1 | high | Git scope: directory swap after the check (deterministic) and race (13/400) | reproduced |
| G-2 | high | Git scope: `.git` file redirects a granted folder to another repository, no race | reproduced |
| G-3 | high | Repository config (e.g. `core.fsmonitor`) executes in the host's git, outside the jail | reproduced |

Independent evidence per item: F-C backend and UI: yes, with R1a-1..4 open. O-2: inventory and behaviour yes, decision no (owner). Directory swap: threat boundary yes, activation call is the owner's with my recommendation above, design written, no implementation. AP1 should stay Partial; AP3/AP4 were not run.
