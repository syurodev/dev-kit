# R3 native evidence record (PARTIAL)

Status: **partial and owner-deferred.** The owner asked on 2026-10-28 to close what can be closed now and to finish the remaining rows
during normal use of the candidate. This record states which rows have evidence, which were not run, and which cannot be run. It does
not say AP3 is complete, and it does not by itself satisfy R4's "accepted R1–R3 evidence" gate (see "What this does and does not allow").

The candidate is **acceptance-only and unmerged**. Its branch `candidate/harness-ap3-native` exists only on the owner's machine (not
pushed). Its build, launcher, scripted fixture peer and the full click-by-click checklist are in that branch's
`docs/reviews/2026-10-28-r3-candidate.md`; this file is the evidence only.

## Candidate and fixture

| Field | Value |
| --- | --- |
| Candidate | branch `candidate/harness-ap3-native`, head `384a95d4`, base `69078684` (main at #390). Wiring: `Harness: container.Harness` in `mcp.Deps`, early `ServeUnix` removed, `app.StartHarness` (compose, attach, bind, then listen) after `application.New`, Telegram moved after it |
| Build | Go 1.27.1, Wails v3.0.0, Linux 7.0.0-38, GNOME on Wayland (WebKitGTK 6.0); desktop build `-tags production`, server build `-tags server` |
| Fixture | Real desktop window and real Go hub. Agent sessions are the scripted `r3-peer` (build tag `r3peer`, reached only through `DEVKIT_CLAUDE_ACP_COMMAND`), shown as a "Claude" session; **not a real vendor**. A marker `claude-acp` install (`current.json`, `install.json`, `/usr/bin/true` as its node) is written into the disposable data dir so the draft composer is not locked. Real-vendor behaviour stays R2's evidence |
| Data | Disposable root made by `scripts/r3-native-candidate.sh` (`HOME` and all `XDG_*` under a fresh `mktemp -d`); the launcher refuses a data dir outside it and `~/.config/DevKit` is never used |
| Safety | No account, sign-in, paid request or network model. Evidence below is from three sources, named per row: **owner** (clicked and reported), **DB** (read-only `sqlite3` of the disposable database: audit rows, transcript, harness tables), **scripted** (a terminal MCP client and an HTTP client for the server build, run by the coordinator) |
| Review | None independent yet. The candidate was written and mutation-checked by agents and the coordinator; the owner clicked. Independent review of this record is still needed |

## Rows

Result: **Pass** has evidence named in the Source column; **Partial** says what is missing; **Not run** was not observed in this run;
**N/A** cannot be produced with the shipped binary.

| Row | Gate | Result | Source and what was observed |
| --- | --- | --- | --- |
| 1 | Locked startup, unlock, harness entry point | Pass | owner + DB: app starts through `StartHarness`; `devkit.db` and `mcp.sock` (mode 0600) only under the disposable root; the workspace menu offers "Agent tool groups…" |
| 2a(a) | Scope dialog, Cancel (agent-initiated) | Pass | owner + DB: `harness.profileDenied: No scope was chosen`, nothing saved. Preselection and focus were reported by the owner, not machine-checked |
| 2a(b) | Denial of the permission card | Pass | DB: session B `/propose vault` waited 5.5 s then `harness.profileDenied`; no S row |
| 2a(c) | Approved enable, this session only | Pass | DB: waited 7.3 s; `publication:"installed"`; S row `groups:["vault"]`; `vault.status` then runs |
| 2a(d) | Save to workspace | Not run | |
| 2b | Workspace review and approved save | Pass (save) | DB: two `prepare_workspace_groups` + `apply`, each with one approved confirmation, `vault:false` at revision 1 on both workspaces. Back and Deny legs leave no trace; not evidenced |
| 3 | Group revoke | Pass (Allow once) | DB: two `prepare_revoke` + `apply` with approved confirmations; afterwards `vault.status` returned `harness.groupOff` and `/list` had no `vault(`. Back and Deny legs not evidenced. Workspace-level revoke not separately run |
| 4(a) | Late approval after navigating | Partial | DB: A waited 37 s on approval; during that time B's `/get` showed `vault` off, `overrides:[]`, and B's `/list` had no `vault(`; A ended with S `vault`. B was read **before** A's Allow, so non-leakage after the Allow is not proven |
| 4(b),(c) | Stop with a card open; `/hold` across navigation | Not run | |
| 4(d) | Does a dialog allow leaving the session? | Pass (recorded) | owner: the scope dialog ("Where should this be saved?") is **modal**; the page behind is blurred and navigation is impossible while it is open. The permission card was not recorded |
| 5 | Failed publication, Reconcile | **N/A** | A publication fails only inside the coordinator-held install step; no fault hook exists in the shipped binary. Evidence stays the scripted Go tests (`TestACommittedChangeWhoseEndpointCannotBePublishedKeepsItsFactsThroughTheSDKAndReconciles`, `TestAnUnknownCommitIsNeitherARollbackNorASuccess`) and R2 rows 5b–5f. Recorded as "not run natively" |
| 5(a) | Adjacent: peer crash, session reopened | Pass (backend) | DB + log: `/exit 3` gave `agenthub.processFailed`; the next prompt reopened with `runtime=resume`; S still stored (`groups:["vault"]`); `/get` showed `"overrides":["vault"]`. The card's wording after the crash was **not observed** (the window went blank, see F1) |
| 6a | Exact budget review in the window | Not run | The owner observed the size field is in **KiB** with a 2–64 KiB range, and that out-of-range input stays red. A valid value was not recorded |
| 6b | Default-off auto-narrowing | Pass | DB timing + transcript: (1) switch off, narrowing to 4096 B waited 7.9 s and was denied; (2) switch on, narrowing to 3072 B was applied in 0.05 s with no question; (3) widening to 65536 B waited 13.2 s and was applied after approval. Policy stored as `{"revision":1,"autoNarrowing":true}`. Persistence across a restart is not yet shown |
| 6c | Always-confirm repair | Partial | (2) widening always asked (above). (1) stale repair: Not run |
| 7a | F-C diagnosis, Cancel, approved repair | Not run | `test2`'s profile is seeded with `{"gone.group.1":false}` (revision 2) and waits for the owner to open its dialog |
| 7b | Long list diagnosis and review | Not run | |
| 8 | Terminal identity cannot ask or read as a session | Pass | scripted: a stdio MCP client through `bin/devkit-mcp` against the running disposable root, caller `user-terminal`. `harness.profile.get` and `harness.profile.propose` both returned `harness.profileDenied: This caller cannot propose a harness change for that target.` immediately; no prompt appeared; no S or profile row changed |
| 9 | Reload and restart | Not run | Only one relaunch in the same root (row 1) and the crash reopen in 5(a). The pending-confirmation kill (`TERM`, `KILL`) and the saved-limit persistence checks were not run |
| 10a | Server mode: loopback only | Pass | scripted: `dev-kit-server` listened on `127.0.0.1:18931` only; `curl` to the machine's LAN address was refused; `WAILS_SERVER_HOST=0.0.0.0` was refused by the launcher and by the binary itself (`server host "0.0.0.0" must be a loopback address`); `mcp.sock` mode 0600 under the disposable root |
| 10b | Server mode: confirmation cannot be shown, fails closed | Pass | scripted over the bridge's HTTP transport (not a browser): `HarnessApply` returned `harness.profileDenied: No one can approve a harness change right now.` in 0.00 s; the workspace has no profile row afterwards |
| 10c | Server mode: sessions refused | Pass | scripted: `AgentHubCreateSession` returned 422 `validation_failed: Agent Hub needs the desktop app. In server mode an agent could answer its own permission prompts.` |

## Findings (none is a harness defect that blocks the rows above)

| # | Finding | Evidence | Severity and scope |
| --- | --- | --- | --- |
| F1 | **The whole window goes blank on a render error, and the prompt box can trigger one.** With a Vietnamese input method on, typing, deleting, switching the method off, losing the caret while focus stayed, typing, deleting and pressing Enter blanked the window. The process and WebKit stayed alive; the UI's 4-second polling stopped at the moment of the blank. There is no root error boundary (only three transcript cards have one). The saved prompt read `/get /get /`, i.e. the editor's content diverged from what was shown | owner's account, DB (polling stop, transcript), source (`ErrorBoundary` only in transcript cards) | Present on `main`: the candidate changes no frontend file. Cause not confirmed (no JS error captured; production builds have no inspector). Needs its own change: a root boundary with a reload action and an editor boundary |
| F2 | The UI's reads are audited at a high rate and the session id is lost: `interaction:respond pending_list` about twice a second (1,943 rows in 23 minutes) and `harness:read session_profile` every 4 s; `session_id` is always `"invalid"` in the audit metadata because `ops.RecordID` accepts only 24 hex characters and sessions use UUIDs (the same call exists in `agenthub.go`) | audit table | Low. Not compared with `main`, so not known whether the candidate adds to it |
| F3 | A request that changes nothing still asks the person for a scope first, then answers `harness.noChange` (`/propose vault` when S already has it, `/propose notes` when it is already on) | transcript timing (3.8 s and 4.3 s waits with a completed dialog) | Low, UX |
| F4 | `harness.profile.get` from a terminal is refused with the wording "cannot propose a harness change", which is wrong for a read | row 8 | Low, wording |
| F5 | The earlier checklist assumption that the "runtime not installed" state would not appear with the adapter override was wrong: `runtimeBlocked` reads only the managed install markers and locks the draft composer | owner | Fixture issue, fixed in the launcher (marker install) |

## Behaviour differences the candidate introduced (R4 must decide each)

1. A bind failure of the shared MCP socket is now fatal at startup (it was logged and ignored).
2. Telegram is started after the harness is attached (it can create and prompt sessions).
3. `HarnessAssemblyConfig.Audit` is nil in the candidate, so proposal outcomes reach no audit sink; the plan does not name one.
4. `StartHarness` sits after `application.New`, so a second launch still exits in `SingleInstance` before binding anything.
5. `OnShutdown` is unchanged and never issues a session Close.

## Limits

- Not run by anyone: 2a(d), 2b Back/Deny legs, 3 Back/Deny legs, 4(b)(c), 6a, 6c(1), 7a, 7b, 9, and the browser UI for 10b (the apply refusal was driven over HTTP).
- The scripted `r3-peer` is a fixture, not a vendor, and has no vendor-side permission flow, model picker or usage.
- Row 5 cannot be produced natively. Whether the permission card is modal was not recorded.
- Evidence comes from one machine, one candidate head and one disposable root. The restart rows depend on that root being reused.
- Record the candidate head with every later result; a wiring or approval change after `384a95d4` needs an explicit diff assessment and a rerun of the affected rows.

## What this does and does not allow

This record shows the wiring starts, the two public tools reach the real hub, the ordinary approval and refusal paths behave as the
design says on the rows above, and server mode and the terminal identity fail closed. It leaves the rows listed under Limits open. R4's
gate (R1–R3 evidence accepted, O-2 decided, ordinary owner review) is therefore **not met by this file alone**. Either the open rows are
completed and added here, or the owner records in the R4 pull request that they accept R4 with those rows open.
