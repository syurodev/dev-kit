### MCP (agent tools)
**Implemented runtime** — desktop hosts tools over a Unix socket; `cmd/devkit-mcp` is the
stdio proxy agents spawn. Resources / Streamable HTTP are deferred. Do **not** treat this
as production-verified against a real Claude Code session in cloud CI.

- Desktop (GUI, unlocked vault) required for the socket host + selection/confirmation
  dialogs. `task mcp` proxies stdio ↔ socket (`go run ./cmd/devkit-mcp`).
- Install only writes local agent config (no network, no grant changes):
  `task mcp:install AGENT=claude-code|codex SCOPE=global|project PROJECT=.`
  (also Settings → MCP panel / `task mcp:uninstall`).
- Confirmation policy: `allow` / `allow_always` / `deny`. When env
  `requiresConfirmation` (incl. fail-closed / `prod`), **env wins** — every gated call
  re-prompts; `allow_always` is ignored and not offered.
- Headless / `task run:server` / nil Prompter: selection and confirmation **fail fast**
  (no hang). Server mode sets `interaction.Hub.Headless` explicitly (`main.go`): its bridge
  is unauthenticated loopback HTTP, so any local process could otherwise answer a prompt.
  The hub is shared with the UI, so in server mode **every** prompt fails fast — active-target
  MCP tools on any env (`db.query`, `ssh.runCommand`, `kafka.*`) and UI actions that confirm
  (prod `git push`, guarded commits, prod `api.send`, task gates). Accepted for now; the plan is
  to ask through OS notifications instead (Codex spec §8.1). Non-gated tools (e.g.
  `vault.status`, notes when unlocked in-process) still work.
- `workspace.list` (read-only) tells an agent its workspace: the folders (services),
  primary first, its own `cwd`, and which folders its session may write (`granted`:
  cwd plus the session's extra read-write paths). An Agent Hub session sees only the
  workspaces its folders belong to; a terminal CLI on the shared socket sees them all.
  The primary-first order comes from `workspace.Service.ListServices` — the SQL
  repository alone sorts by name.
- `workspace.requestWrite` (gate-owned, `workspace` harness group) asks the person for write access to one folder of
  the caller's own workspace: input `{folder}` (a service name as `workspace.list` shows it; no path, no session id).
  The owner is `agenthub.Service` (`PrepareWorkspaceWrite`, then the confirmation here with `EnvBlocks`, so it is asked
  every time and never remembered, then `CommitWorkspaceWrite`, which checks again). Capability `ws:grant` on
  `workspace:requestWrite` (`internal/app/container.go`); the service refuses any caller but an agent session. Codes
  `workspace.grantRefused` (not an agent, a sub-session, no workspace, an unknown name, a closed session, moved grants),
  `workspace.grantDeclined` and `workspace.grantUnavailable` (nobody can answer), all in `bridge.keptCodes`; they are named
  through constants, so `TestWorkspaceGrantCodesReachTheAgent` covers them (the source scan does not). The answer says when
  git, new sub-sessions and the session's shell and file tools see the folder (`now`, `after_this_turn`, `after_reopen`).
  `git.repoNotGranted` now says to ask with this tool. Its definition lifted the `workspace` group ceiling in
  `tool_budget_test.go` (5300 to 5900).
- **Git tools act on the caller's repository, not only the one open in the app**
  (`internal/mcp/git_repo.go`). Every `git.*` tool takes an optional `repo`: a folder name
  from `workspace.list` / `git.repos`, an absolute path, or `./…`/`../…` from the cwd. With
  none, an Agent Hub session gets its own `cwd`'s repository (the workspace folder holding
  it — a subfolder commits in its service); the person's terminal keeps the repository
  DevKit has open. An agent may READ any folder of its own workspaces and WRITE (stage,
  commit, branch, checkout, stash, resolve, push) only folders its session is `granted`
  (`git.repoNotGranted`); anything else is `git.repoOutsideWorkspace`. `git.repos` lists the
  workspace's repositories with branch/ahead/behind/changed, `cwd` and `granted`, and
  `git.status` answers with the `repo` it read. It works through `gitservice.Service.At` —
  a view sharing guards, remotes, credentials and the confirmation gate, keyed by the
  workspace folder like the Git view, so a guarded branch is guarded whichever way it is
  reached — and never moves the app's open repository.
- **Agent-origin Git is confined and hardened** (`internal/mcp/git_repo.go` `confineToScope`,
  `internal/git/harden.go`). `gitFor` reads the `.git` at the top of an agent's checkout: a directory is
  the repository; a file or link must name the git directory git found, inside the workspace or granted
  scope (a linked worktree is also accepted when its worktree entry names the checkout back, so a child
  granted only its checkout still works), and a `commondir` must be in scope too; refusal is
  `git.repoOutsideWorkspace`. The handle is then pinned with `GIT_DIR`/`GIT_WORK_TREE`. For a caller of kind
  agent git runs with `core.fsmonitor`, hooks (`core.hooksPath=/dev/null`), `core.sshCommand`, pager, askpass,
  credential helpers, signing programs and the named filters/diff/merge drivers neutralised, so the
  repository's hooks, LFS filters and credential helpers do not run for agents; the person's terminal is
  unchanged. It is a denylist, not a sandbox. Spec:
  `docs/superpowers/specs/2026-10-28-git-agent-scope-hardening.md`.
- **Agent-origin Git runs in a directory it holds open** (`internal/git/bound*.go`, `internal/fchdirexec`).
  `Service.At` opens the folder without following a link, holds it, the checkout top and the git directory by
  descriptor and starts git there with no path lookup (`/proc/<pid>/fd/N` as the working directory and
  `GIT_DIR=/proc/self/fd/N` on Linux; elsewhere the app binary re-executed as an `argv[0]` helper that does
  `fchdir` + `execve`). Identity (dev, ino) of the folder, top and git directory is checked before and after each
  command, and on Linux a watcher refuses a command whose process leaves the directory; refusal is
  `git.repoOutsideWorkspace`. Handles are closed with `closeGit` after each tool call. Do not add a path-based
  `-C <path>` or an `os.ReadFile(<work tree path>)` on an agent handle. **Residual, not closed:** git itself
  changes into the work tree by *name* for a command that needs it, so a name rebound in that instant sends that
  one command into another directory (measured: 13 of 20,000 status calls under a continuous
  `RENAME_EXCHANGE` flipper read the other directory's files; the repository's refs, objects, config and index
  stayed the granted ones on Linux). Also path-resolved: `git.worktree.add` targets, local-path remotes,
  `git.repos`, the PR tools (`internal/prwatch`) and the managed-writer resource key; guard keys stay path strings
  (synced rows). macOS was compiled but not run; the helper costs about 24 ms per command. This is not a boundary
  against a hostile agent. Spec: `docs/superpowers/specs/2026-10-28-git-descriptor-bound-execution.md`.
- **History tools never need a force push** (`internal/git/history.go`). `git.amend`,
  `git.squash` and `git.rebase` rewrite only commits no remote-tracking ref reaches
  (`git.pushedHistory` otherwise); `git.cherryPick` / `git.revert` add commits, so pushed
  history is fine. Rebase/pick/revert stop on a conflict with the paths (`stopped`,
  `conflicted`) and take `action: continue|abort`. All go through the branch guard and
  commit as the account identity. `git.fetch` / `git.pull` (ff-only) use the remote's bound
  account and answer ahead/behind. The Claude hook also refuses shell `git pull`,
  `rebase`, `cherry-pick` and `revert`.
- **Git worktrees give code-editing children separate checkouts** (`internal/git/worktree.go`,
  `internal/mcp/tools_git.go`). `git.worktree.add|list|remove` use the same `repo` argument and
  granted-folder rule as other Git tools; worktrees live under
  `.superpowers/.worktrees/<branch-slug>`. Removal refuses dirty worktrees and never deletes a
  branch. `agenthub.createAgentSession` accepts `worktree` (a branch name): `cwd` selects
  the repository, or an empty `cwd` inherits the parent's. The repository root and checkout
  must both be writable by the caller; a refused target never falls back to the parent's repo.
  It creates the checkout before the child, starts the child there and records the branch on
  `taskId` when supplied. Removal skips closed sessions. Missing ordinary cwd suffixes are
  checked against their resolved existing ancestor: an unrelated deleted project does not
  block cleanup, but a missing subdirectory inside the worktree still protects it. Broken
  aliases, cycles and other unresolved errors fail closed.
- **`pr.*` tools work with the pull request of an agent's branch** (`internal/mcp/tools_pr.go`; the host client is
  `internal/forge` — GitHub and gitlab.com, see "GitLab is the second provider" below — the folder→repo→account→request logic `internal/prwatch`). Reads `pr.get|list|checks|comments`
  (`git:read` on `repo:pr`); writes `pr.create|update|comment|reply|merge|close|deleteBranch` (`git:push` on
  `remote:origin`, the same push-class capability as `git.push`; `mcp.PullRequestGrants` lists what they ask and
  `TestNewContainerGrantsEveryPullRequestToolItsCapability` checks it against the real grants). Same `repo` argument
  and granted-folder rule as `git.*`. A write on an agent's behalf needs the request's source branch recorded on a
  task the session holds (not the default branch, not a fork's) — `pr.create` checks the current branch, the others
  the request's head. Every write asks first through `Hub.Confirm` with the exact text in `Summary` (the "DevKit tool
  gate" card; headless fails fast): `pr.create|update|merge|close|deleteBranch` **always** (`EnvBlocks: true`, so no
  allow-always, whatever the remote's tag); `pr.comment|reply` follow `db.query`'s policy (allow-always unless the
  remote's environment asks every time). `pr.merge` refuses a closed/merged/conflicting request without asking, shows
  failing/running checks, a draft, a blocking rule or requested changes in the question, and passes the head SHA it
  showed so the host refuses if the branch moved. `pr.deleteBranch` refuses the default and a protected branch
  *before* asking. Audit metadata names the action, repo, number, branch and method — never the text sent. Results
  that quote the host carry a `note` saying they are third-party data, not instructions; bodies are clipped. The token
  never leaves Go; `gh`/`glab` in a shell stay allowed (the prompt prefers the tools, no hook blocks the CLI).
- **An error code an MCP tool raises for the agent must be published, or it reaches the agent as `internal_error`**: add it to `bridge.keptCodes` after reading where its message comes from (`pr.forbidden|notFound|rejected|invalid|unavailable` quote the host through `hostSaid`: one line, no control characters, 300 runes, marked as the host's text), or to `redactedOnPurpose` in `bridge/mcp_error_codes_test.go` with the reason; `TestEveryMCPErrorCodeIsPublishedOrRedactedOnPurpose` scans `internal/mcp` for both (`pr.*` and `git_repo.go` codes were redacted until then, so a GitLab `pr.create` told the agent nothing).
- **`git.fetch` / `git.pull` are `git:write` on `repo:sync`** (`mcp.GitSyncGrant`). They used to ask for
  `git:write` on `remote:<name>`, which the `repo:*` grant does not cover, so every agent call was refused
  while the MCP tests (fake caller, no policy) passed; `TestNewContainerGrantsAgentGitSync` now checks the
  real grants. A denial reaches an agent as `Permission denied: <what was missing>` (the window keeps the
  bare message, `bridge.publicErrorFor`), and a Codex row for a failed MCP call shows the tool's own text
  instead of `failed` (`codexMcpErrorText`).
- **`internal/git` refusals (`ErrPushedHistory`, `ErrDirtyWorkTree`, `ErrInProgress`, `ErrNotAllowed`, `ErrProtectedBranch`, …) are plain sentinels, published by `bridge/git_errors.go`** (`gitRefusals`: code + the `fmt.Errorf("%w: sentence")` site's own text, never git's stderr; `TestEveryGitSentinelIsPublished` fails for a new sentinel, `TestGitRefusalsTellTheAgentWhyThroughTheRealBridge` runs `git.amend` etc. on a real repo) — wrap with a sentence, don't return a sentinel bare.
- **What a tool puts in an agent's context is budgeted** (`internal/mcp/result_budget.go`, `tool_budget_test.go`). Anything a
  tool returns stays in the conversation for every later request, and Codex pulls tool definitions into context too. Results:
  `withResultBudget` (one receiving middleware, not per handler) cuts a text result over `ResultBudget` (24 KB) at item
  boundaries (the largest array keeps the leading items that fit; a lone long string is shortened; plain text is cut at a
  line), adds `_truncated` saying how much was left out and how to get it (`resultHints`), drops a `nextCursor` that would
  skip the cut rows and sets `truncated` on row sets. Never cut: results with an image/non-text block, errors, and the
  tools in `budgetExempt` (`pdf.*` have their own budget, `codegraph.readFile` is "the whole file") — a new exemption needs a
  reason there. Lists and writes return compact shapes (`tasks.*` without history/description except `tasks.get`,
  `workspace.memory` writes without body unless `includeBody`, `git.status` as `"<state> <path>"` lists, `agenthub.listPeers`
  without closed sessions); a ToolResultView must not read a field those dropped — add a shape test beside the view.
  MCP default page sizes are smaller than the window's (`mcpSymbolPage` 25, `mcpEntryPointPage` 50). Definitions:
  `TestToolDefinitionsStayWithinBudget` fails when one description passes 700 bytes, one definition 2200, or the sums grow
  (descriptions 25 KB, with schemas 70 KB); shorten before raising, and never trim a sentence that tells the agent the
  person will be asked (`TestGateOwnedToolsStillSayTheyAsk`). Measured on fixtures in the tests' logs
  (`go test ./internal/mcp -run 'Compact|Measured|Memory|Cut' -v`), not with a real model.
- Profile-scoped tools that reach a server (`db.explain`, `db.listForeignKeys`,
  `db.testConnection`, `db.schema.get`, `ssh.testConnection`) go through `GateProfileMCP`:
  they prompt on every call when the profile's env requires confirmation, and pass otherwise.


### Host text-result measurements (Phase 1b)

`internal/mcp/result_metrics.go` observes UTF-8 `TextContent` bytes immediately around the existing result-budget mutation.
Collection is only on identity-bound Hub sockets; terminal clients are excluded. The scope callback reads host-owned runtime
facts from memory at call entry and checks the endpoint incarnation, so stale sockets cannot adopt a replacement runtime's
workspace. It stores identifiers, dates and counters, never tool arguments, result text, resource/image bytes or duplicate
structured content. Unknown dispatch failures use `dispatch-error`; known text zero and missing result coverage are distinct.
The observer changes no gates, exemptions, limits or response content.

The stdlib-only `internal/usagemetrics` collector aggregates at most 4,096 active keys plus one immutable pending batch of at
most 4,096 keys, flushing every two seconds on one worker. SQLite daily UPSERTs and the run/sequence watermark commit together;
an ambiguously successful commit can be retried without double counting. Full buffers or stale scopes count as dropped
observations; write failures and runs left unclosed are visible in Settings. Shutdown stops accepts, closes collection to new
calls, waits for entered observations within five seconds and drains pending/active batches. A crash can lose the unsaved tail;
there is no durable per-call queue. The report reads persisted data only and never forces a flush. This measures host-served
text bytes, not vendor request tokens, provider cost or subscription quota.
- **A session endpoint can serve a prepared harness policy (H4a; active since R4)** (`internal/mcp/serving.go`, `session_host.go`; plan
  `docs/superpowers/plans/2026-10-27-harness-h4-session-serving-implementation.md`). `Server.Inventory()` reads the registry
  from what the server really registers (name, group, serialized definition bytes, the fixed budget exemption), once; `Registry()`
  hands it to `harness.Resolve`, so validation, serving and the cost tests share one fact set (`TestInventoryIsWhatAClientSees`,
  `TestGroupDefinitionCostsStayWithinMeasuredCeilings`: a new group needs a measured ceiling). `SessionHost.OpenWithPolicy` (the
  `agenthub.PolicyEndpoints` seam) stages an immutable `servingHolder` before the socket accepts anything: the SDK instance
  has the hidden tools **removed** (`RemoveTools`), so no page, cursor or total can name them (tested with a page size of 7), and
  `withMeasuredResultBudget` — the one existing result middleware, not a second layer — loads the holder **once at call entry** and
  uses that snapshot for the group decision, the tool's resolved budget (tool > group > 24 KiB, fixed exemptions kept) and the
  W/S revisions in the observation (the snapshot's, not the host's scope). A registered tool the policy hides answers a tool error
  `harness.groupOff: The <group> tools are turned off for this session.` before any handler or gateway runs; an unknown or empty
  name keeps the SDK's normal answer; `harness.profile.propose` is served whatever its group does (nothing is registered under that
  name yet, so nothing is listed). A policy that does not know a registered tool is refused at open (`harness.notEnforceable`).
  Each endpoint incarnation owns its holder, token and connections: `Close` is idempotent, ends accepted connections (not only the
  listener) and removes the socket and registry entry only if they are still its own. Nothing reads SQLite, preferences or a
  vendor process on that path. The refusals are text results, so `harness.groupOff` / `harness.snapshotUnavailable` are in
  `bridge.keptCodes` (fixed sentences). Pre-approved and gate-owned lists are narrowed to served tools, never widened. Terminal
  (shared-socket) and legacy `Open` endpoints are unchanged. **Active since R4**: `main.go` composes the harness through `app.StartHarness`, so
  `OpenWithPolicy` serves every workspace session's prepared policy; the prompt and launch guidance (H4b), live S publication (H4c) and
  composition (H4d) are in place.
- **Host guidance, pre-approval and the endpoint come from one prepared value (H4b; active since R4)** (`internal/agenthub/devkit_guidance.go`,
  `devkit_prompt.go`, `harness_runtime.go`; plan `docs/superpowers/plans/2026-10-27-harness-h4b-guidance-implementation.md`).
  `SessionHost.OpenWithPolicy` returns `MCPEndpoint.Served`: the tools its inventory registers *and* its policy lets through,
  from the same snapshot that filters discovery. `prepareLaunch` copies it into `OpenSpec.DevKit` (`*DevKitServing`) beside
  the already narrowed `GateOwnedMCP`/`PreApprovedMCP`; an enforcing endpoint that reports no served inventory fails
  preparation (`harness.snapshotUnavailable`, endpoint closed, nothing published) instead of falling back to the full prompt.
  **`nil` is the legacy launch** (no policy prepared, full guidance, byte-identical to before, pinned by captured goldens);
  **non-nil and empty is a positive policy that serves nothing** and renders the safety core only. No second resolver, no read
  of preferences, W, S or a vendor report. The prompt template is split once into chunks; a chunk renders only if every tool it
  names is served (derived from its `{{tool}}` references, so there is no second list of names), `{{?a,b}}…{{/}}` keeps text only
  when those tools are served, `{{+tool}}` requires one without naming it. Hard rules, the headings, "a tool missing from your list
  is not available" and "respond in the user's language" never depend on a group. With git hidden a fixed guard stays ("Git writes
  are guarded… do not run `git commit`… in the shell… tell the person"); a hidden group's other sections are simply omitted, and a
  multi-group section (board delegation: tasks + git + agenthub) needs all of them. The vendor naming note's example is a tool
  that is served, and is omitted when nothing is; the no-brief fallback names `workspace.list` only if served and otherwise asks the
  person; the attached-PDF guide and the peer-message reply line likewise say the tool is unavailable instead of naming it. The brief
  is inserted last and untouched (it used to be spelled like template text; tool-looking text in it is data now).
  `replacementGuidance` is a pure, 4 KiB-bounded helper for H4c's live S result: the same chunks for one newly served group, no
  mutation, no approval, no `Live`. Prompt and vendor pre-approval are fixed at open; a later change never implies the vendor
  saw a refreshed list. Tested against the wire of Claude (`session/new` `systemPrompt.append` and `allowedTools`), Codex
  (`thread/start` and `thread/resume` developer instructions) and opencode (instructions file and config), and against a real
  `SessionHost` + resolver + service (`guidance_integration_test.go`). Bytes only, not tokens: all on 19,219 (Claude) /
  19,572 (Codex) / 19,528 (opencode), git off 16,287 / 16,642 / 16,598, tasks (board) off 15,758 / 16,111 / 16,067, all off 3,437.
  (At H4b this was inert; R4 activated it.)
- **A committed S change is installed on the exact endpoint incarnation (H4c; active since R4)** (`internal/mcp/serving.go`,
  `serving_live.go`, `session_host.go`, `result_metrics.go`; protocol and lock order in
  `agent-customization-recovery.md` "Live publication of S"). `servingHolder` keeps one atomic `admissionState`
  (policy + paused/closed): the call-entry load decides admission, so a call that entered before a quarantine
  finishes under its snapshot and none enters under the old policy after it, with no drain of running handlers.
  While paused `tools/call` answers `harness.snapshotUnavailable` ("being updated", before any handler or the gateway)
  and `tools/list` is refused. `sessionEndpoint` implements `agenthub.ServingEndpoint` (opaque incarnation, tickets
  that supersede each other, `Quarantine`/`Restore`/`Install`); `Install` checks the policy covers the registry, then
  re-registers returning tools through `Server.registerAll` and drops the rest with `RemoveTools` (supported SDK APIs
  only), then swaps the policy and reopens admission in one store; a failed or panicking step leaves the endpoint paused.
  Policy endpoints explicitly set `Capabilities.Tools.ListChanged`, so a configured-empty endpoint advertises `listChanged` and a later enable is
  discoverable; the SDK's debounced list-changed notification is reported as *scheduled*, not as a vendor refresh.
  Result budgets, schemas, the bootstrap exception, host-owned exemptions and the definition ceilings are untouched
  (`registerAll` is the one declaration of the registry). Integration tests run the real H2 `Apply`, SQLite
  `HarnessStore`, Agent Hub service, `SessionHost` and SDK clients with a scripted vendor
  (`serving_integration_test.go`, `serving_live_test.go`, `serving_status_test.go`).
- **`harness.profile.get` / `harness.profile.propose` (H5a; active since R4)** (`internal/mcp/tools_harness.go`; facade and record in
  `harness-composition-activation.md` "H5a"). Registered only when `Deps.Harness` is set, which `main.go` now does (R4), so both exist in
  production once `app.StartHarness` has bound the port. `get` reads the caller's own groups (tool count, definition bytes, W/S, saved vs running;
  `unknown` is not `off`) and follows its group policy; `propose` is the one always-served bootstrap: with no `groups` it
  lists what is off and asks nobody, with `groups` the person picks this session or the workspace through the shared hub
  and the existing `Apply` asks and commits. Neither takes a session or workspace argument (scope is the caller's
  identity; a terminal is refused). Grants `builtin.harness` `harness:read`/`harness:propose` on `profile:self` only,
  authority to *ask*, never to run a tool; no pre-approval is added and the launch permission lists stay fixed. A committed
  change whose publication failed, or whose commit is unknown, is an `isError` result whose `structuredContent` is the same
  `RequestResult` with its `Outcome` (status, commit, publication, stamps, serving identity, `Live`, `ReloadRequired`,
  recovery text); reconcile, never propose again. H5b adds, without a new tool or grant: `get {budgets, group, cursor}` returns a
  bounded page of the workspace's per-tool result limits (explicit rules, effective value and source), and
  `propose {budgets:[{kind, name, bytes|resetBytes, cut|resetCut}]}` — exclusive with `groups`, at most 16 edits, **saved
  workspace profile only** (never a session), taking effect at the next open/reload. The person's opt-in (a UI-only
  preference, off by default) lets only an agent's narrowing budget edit skip the question; the tool schema carries no
  policy, approval or target field. Published codes (`bridge.keptCodes`): `harness.profileDenied`,
  `profileInvalid`, `profileConflict`, `proposalStale`, `groupUnknown`, `factsUnavailable`, `notAvailable`, `noChange`.

