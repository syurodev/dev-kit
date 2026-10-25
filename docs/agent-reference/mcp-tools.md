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
  branch. `agenthub.createAgentSession` accepts `worktree` (a branch name), creates it before
  the child, starts the child there and records the branch on `taskId` when supplied.
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

