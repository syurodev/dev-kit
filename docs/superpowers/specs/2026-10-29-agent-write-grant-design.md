# An agent asks for write access to a folder of its workspace

Status: design agreed with the owner in a brainstorming session on 2026-10-29. Not implemented, not reviewed
independently. Base `cf5a884a` (main after #395, harness active in production).

## 1. Problem

A session's write grant is fixed when the session is created: its working directory (`project_path`) plus
`extra_rw_paths` (`agent_sessions`). A folder that is a service of the session's workspace but is not in that
list is read-only to the session, and every `git.*` write on it answers `git.repoNotGranted` ("Ask the person to
add it to the session", `internal/mcp/git_repo.go`). Nothing in the product can add it afterwards:
`UpdateSession` does not write `extra_rw_paths` (`internal/agenthub/store.go`), and the only path setter is the
create-time `cleanExtraReadWritePaths`.

Evidence (owner's database, 2026-10-29): workspace `fsap-event` has six services including `fsap-audit-service`;
the root session "FSAP-3932 và các subtask" (`1e37cca8`) was created with the working directory plus four extras
and no `fsap-audit-service`, so `git_repos` answered `granted: false` and the agent could only ask the person to
change the workspace configuration. The only way out was a new session, or editing the database by hand (done
once, with a backup; this design replaces that).

## 2. Decisions

| # | Decision | Why |
| --- | --- | --- |
| D1 | A new MCP tool `workspace.requestWrite` lets an agent session ask for write access to **one folder of its own workspace**, by service name as `workspace.list` shows it. | The owner's wording: "a repo in the workspace". No arbitrary paths. |
| D2 | The person approves in the window every time. No allow-always, no scoped approval. | The same rule as `peerConfirm` (`internal/mcp/tools_agenthub.go`): `EnvBlocks: true`. |
| D3 | An approved folder is **stored on the session** (`extra_rw_paths`) and lasts as long as the session. | Owner's choice. Resume, rewind and reload already rebuild the jail, `workspace.list` and `git.*` from the stored row. |
| D4 | Git tools, `workspace.list` and sessions created afterwards see the grant at once. The session's own shell and file tools see it after DevKit reopens its process **at the end of the current turn**, when that is safe. | The jail is bound when the process starts. |
| D5 | Only a top-level session (no parent) can ask. A child is refused. | "A child session may write what its parent may write, never more" (`agent-hub-vendors-sessions.md`). A child already holds a copy of everything its parent had when it was created; widening a child alone would break the rule, widening the parent does not change existing children. |
| D6 | Out of scope: revoking, expiry or use limits, folders outside the workspace, granting from the window without an agent request, changing what existing children hold. | YAGNI. |

## 3. The tool

`workspace.requestWrite`, registered beside `workspace.list` in `internal/mcp/tools_workspace.go`, owned by the
gate (`gateOwnedTool`, so a vendor wrapper delegates the confirmation to DevKit and the person sees one card, not
two). Annotations: not read-only, not open-world.

Input: `{ "folder": "<service name>" }`. Nothing else: no path, no workspace id, no session id (the caller's
identity names the session).

Output (small, fixed):

```json
{ "folder": "fsap-audit-service", "granted": true, "alreadyGranted": false,
  "effective": { "git": "now", "newSessions": "now", "shellAndFiles": "after_this_turn" } }
```

`shellAndFiles` is `now` when the folder was already writable, `after_this_turn` when DevKit will reopen the
process, and `after_reopen` when it will not (see section 6). The text for the model says what each means and
does not promise more.

## 4. Where the checks live

The tool handler only parses the input and calls the owner, `agenthub.Service`, in two steps around the
confirmation: `PrepareWorkspaceWrite` (steps 1 to 4) and `CommitWorkspaceWrite` (step 6). The confirmation (step 5)
stays in the MCP layer, as `peerConfirm` does, because `agenthub` owns no confirmation transport for a tool call.
Every check is in Go, in the order below. The first failure ends the call and nothing is stored.

1. **Capability.** `Caller.Call(ctx, "builtin.workspace", "ws:grant", "workspace:requestWrite", ...)` with a new
   grant in `internal/app/container.go`, given to agent sessions only. A terminal caller or any non-agent is
   refused (`workspace.grantRefused`).
2. **Session.** Read the row from the store (never a cached copy). It must exist, not be closed, and have no
   `ParentSessionID` (D5).
3. **Workspace.** `WorkspaceID` non-empty. `ListServices(WorkspaceID)` must contain `folder` by name. The path is
   the registered service path, cleaned; `/` and the user's home are refused (the rules of
   `cleanExtraReadWritePaths`).
4. **Already writable.** If `workspace.Covered(path, project_path + extras)` is true, answer
   `alreadyGranted: true` without asking.
5. **Confirmation.** `Hub.Confirm` with `Kind: KindAgent`, `Tool: "workspace.requestWrite"`, `EnvBlocks: true`, a
   summary naming the folder, the workspace and the full path, and the plain consequence ("this session will be
   able to write here for as long as it lasts"). Denied: `workspace.grantDeclined`. No one can answer (server
   mode, nil handler, locked vault, `Hub.Headless`): `workspace.grantUnavailable`. A cancelled context ends the
   call without a record.
6. **Re-check and store.** The wait may take minutes. After the answer, read the session and the service list
   again and repeat steps 2 to 4. Then write with a compare-and-set:
   `UPDATE agent_sessions SET extra_rw_paths = <new> WHERE id = ? AND extra_rw_paths = <value read>`. No row
   changed means another writer moved the grants: the store re-reads and tries again, up to ten times (each
   failed try means another writer succeeded, so the loop ends), and the call is refused with
   `workspace.grantRefused` if it never lands. The checks of steps 2 to 4 run once, right before the write. The
   storage method is a new optional store interface (`ExtraPathStore`), not a change to `UpdateSession`.

Nothing in this method reads a path, a session id or an approval from the tool input except the folder name, and
the value stored is always the registered service path.

## 5. What sees the new grant, and when

Verified in code (2026-10-29):

| Reader | Reads | Sees it |
| --- | --- | --- |
| `workspace.list` `granted` | `AgentHub.GetStatus` on every call (`tools_workspace.go`) | at once |
| `git.*` scope (`internal/mcp/git_repo.go`) | session row on every call | at once |
| `CreateSession` of a child | the parent's stored row (`inheritedExtraPaths`) | at once, new children only |
| Existing children | their own stored copy | never changes (D5) |
| The session's shell and file tools | the jail built when the process started | after a reopen |

Not verified, to confirm first in the plan: that no cache of the session's grants exists between the store and
the `git.*` scope for an already running session.

## 6. Reopening the process

D4 says DevKit reopens the process once the turn ends. The mechanism reuses idle release
(`internal/agenthub/idle_release.go`): `releaseRuntime` tears the process down and keeps the session, and the next
prompt, peer message or settings call reopens it through `resumeRuntime`, which rebuilds the jail from the stored
row.

- The tool marks the session in memory (`reopenForGrant[id]`). At the end of `Invoke`, after the status returns
  to `idle` and the conversation is flushed, if the mark is set the service attempts a release with the same
  guards idle release uses, minus the time threshold: the session is `idle` or `error`; no running or waiting
  child; no pending permission or question; no queued or dispatching peer message; no resume in flight; the vendor
  can reopen it (`reopenable`).
- **One guard is added and is required: running background work.** A shell task started by the agent lives inside
  the process, and releasing the process kills it. DevKit has no backend notion of it today (the idle-release gap
  found on 2026-10-29: tasks are visible only to the window, from `async_task_*` transcript rows). This design
  adds the smallest piece it needs: a per-session, in-memory set of task ids built in `handleUpdate` from
  `async_task_spawned` and `async_task_state_update` (spawned adds; `completed`, `failed`, `stopped` remove;
  dropped with the runtime). It is in memory on purpose: a task dies with its process, so a record that survives a
  restart would only be wrong. An agent-initiated turn (`taskType: "turn"`, id `autonomous-turn-*`) counts as work
  for the same reason.
- A session that fails any guard is not reopened. The tool's answer already told the agent that the shell and
  file tools get the folder "after this turn, or after the session is reopened if DevKit cannot do it now". Idle
  release or a restart then applies it. Nothing is retried in a loop. A window notice for this state is left to
  the UI plan (it needs a new session field).
- A failed release does not undo the stored grant. The stored row is the truth; the process is only late.
- The same background-work set must be consulted by idle release (`ReleaseIdle`), which closes the gap that
  started this section. That change is small and shared, so it ships in the same plan, in its own commit, and is
  also listed in the UI plan item "session state from autonomous turns and background tasks".

## 7. Errors and audit

New public codes, each published in `bridge.keptCodes` (`internal/bridge/errors.go`) with a message that names no
path and no other folder:

| Code | When |
| --- | --- |
| `workspace.grantRefused` | not an agent session, a child session, no workspace, the name is not a service of the workspace, grants moved twice, the session is closed |
| `workspace.grantDeclined` | the person declined or closed the card |
| `workspace.grantUnavailable` | no one can answer the card |

Audit goes through the capability call (`builtin.workspace`, `ws:grant`, scope `workspace:requestWrite`) with
metadata `origin`, `action: request_workspace_write` and the service name; the gateway records the outcome. No
path, no summary text, no prompt. A storage error after approval returns `internal_error`; it is never reported as
declined.

## 8. Window

- The confirmation card is the existing agent permission card; nothing new beyond the summary text.
- The Environment view lists each writable folder with "Granted when this session was created."
  (`internal/agenthub/environment.go`). The wording becomes "Granted to this session." because a folder can now
  be added later.
- The agent learns the tool from the refusal it already meets: the `git.repoNotGranted` message (two copies,
  `internal/mcp/tools_git.go` and `internal/mcp/git_repo.go`) now says "Ask for it with `workspace.requestWrite`
  (a sub-session asks its parent), or work in <cwd>." instead of "Ask the person to add it to the session". The
  prompt brief is not changed: that keeps the six `legacy_*.golden` prompt files and the per-vendor tool naming
  out of this change. If the tool is not served (its harness group is off) the agent gets the usual unknown-tool
  or `harness.groupOff` answer.
- A window notice for "applies after this session reopens" is left to the UI plan (it needs a new session field).

## 9. Tests

- Refusals, each leaving the stored row unchanged: terminal caller; child session; no workspace; unknown name;
  `/` and home; closed session.
- `alreadyGranted` asks nothing.
- Declined, closed card, cancelled context, no handler, server mode and `Hub.Headless` each end with the right
  code and no write.
- A service removed from the workspace during the wait is refused after the answer (the re-check).
- Two concurrent requests for the same folder: one write, the other answers `alreadyGranted` or retries once.
- After a grant: `git_repos` reports `granted: true`; a child created afterwards inherits the folder; a child
  created before it does not.
- Reopen: the mark is set and the process is released after the turn; each guard (running child, pending
  question, queued peer message, running background task, autonomous turn, vendor cannot reopen) leaves the process
  alone and answers `after_reopen`; a failed release keeps the grant.
- Background work set: spawn then complete, failed, stopped and a runtime release each clear it; `ReleaseIdle`
  skips a session while it holds a task.
- A mutation test per guard (remove it, expect a failure, restore), as in earlier slices.
- `internal/arch` green; the new codes are in `keptCodes`; the tool is in the harness inventory (see section 10).

## 10. To confirm at the start of the plan

1. **Harness group (resolved).** `harness.Group` is the part of a tool name before its first dot, so
   `workspace.requestWrite` is in group `workspace`, with `workspace.list` and `workspace.memory.*`. A session
   with that group off gets `harness.groupOff` for it (`internal/mcp/serving.go`); no new group is needed. The plan
   adds a test that proves it.
2. **No grant cache** between the store and the running session's `git.*` scope (section 5): confirm in the plan.
3. **Reopen at turn end.** Confirm that the deferred block of `Invoke` is a safe place to call the release
   without racing the caller's own result being delivered, and that the tool's answer reaches the model before the
   process goes. If it does not, release on the next idle sweep instead and keep the `after_reopen` wording, and
   tell the owner.
4. **Vendors.** Only Claude is measured. For Codex and opencode, check that `resumeRuntime` rebuilds the jail from
   the stored row as it does for Claude, or answer `after_reopen` for them.
5. **Events (default chosen).** The window refetches the session row when a turn finishes (`OnTurnFinished`), and
   the grant is made inside a turn, so the Environment view refreshes at the end of that turn without a new event.
   The plan confirms that the window really refetches there; if it does not, one event is added.

## 11. Docs to update with the change

`docs/agent-reference/agent-hub-vendors-sessions.md` (grant rule, reopen), `docs/agent-reference/mcp-tools.md`
(new tool, capability, codes), `docs/agent-reference/agent-hub-coordination.md` (a child cannot ask).
`AGENTS.md` stays under 10 KiB and is not changed.

## 12. Limits

- The design reasons from reading the code and from one owner's database. Nothing was run.
- The background-work set sees only what the vendor reports. Claude reports shell tasks and autonomous turns (in
  the owner's data: 45 task spawns, and each of the 5 autonomous turns seen so far ended with a `completed`
  update); other vendors report nothing, and for them the guard cannot protect a task.
- Editing the database directly stays possible for the owner and is not an approval path: it skips the card, the
  audit and the re-checks.
