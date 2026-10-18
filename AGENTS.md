# AGENTS.md

## Cursor Cloud specific instructions

This repo is a **Wails v3** desktop app (Go backend + React/TypeScript/Vite frontend).
The product is "DevKit" (see `docs/architecture/`). The Go core under `internal/` implements
the vault (mandatory Master Password, Argon2id KDF + XChaCha20-Poly1305 AEAD), the vault-item
modules (notes, database, ssh, kafka), and a local-first, zero-knowledge **sync client** with
Git-style conflict resolution (`internal/syncclient`, `internal/syncconflict`,
`internal/syncstore`). A throwaway in-memory mock sync server lives at
`cmd/mocksyncserver`. Architecture layering is enforced by `go test ./internal/arch/`.

### Module UI: shared sidebar and document tabs

Before adding or changing a module sidebar or document navigation, read
[`docs/design/module-shell-integration.md`](docs/design/module-shell-integration.md).
The app shell owns the sidebar column and workbench tab strip. Use a lazy
`ModuleManifest.sidebar` for module rails (including when no document is open),
or `useSidebarContent` for document-specific rails. Do not embed a fixed-width
`aside`, duplicate sidebar toggle/resize/material, or maintain a second document
strip / `openIds` / `selectedId` in a module view/store. Enable `tabs: true` and
open documents through `useWorkbenchTabsStore.openTab` with stable ID params;
provide a working `newTab` action. Close a tab whose document is gone (deleted,
its folder removed, deleted on disk, a draft saved as a new document) with
`closeTab(id, { remember: false })` so ⌘⇧T does not offer it back. Shadcn `Tabs` may switch modes inside one
document, such as SSH Terminal/Commands/Tunnels/Details. Keep drafts/resource
lifetimes separate from view mounting, derive selection from the focused group,
and gate document dialogs with `useModuleVisibility`. The shared sidebar has no
fill/border/shadow of its own; controls keep their theme colors. SSH's manifest,
sidebar and `documents.ts` are the current reference implementation.

### Theme: Mono chrome + Color palette
The chrome is one monochrome design (`frontend/src/index.css`, light and dark); nothing
the person picks changes it. The theme family (`theme_family`, Settings → "Color palette")
supplies only `--hue-*` (8 per mode), `--syntax-*` and `--ansi-*`: `theme/families.ts`
declares hues (+ optional syntax) and `resolvePalette` derives the rest; `applyThemeFamily`
writes only palette tokens. Status (`--status-*`) belongs to the chrome — use it for how
safe something is; use `--hue-*` for which kind it is. `bun test src/theme` floors every
palette colour at 4.5:1 on every Mono ground. Design:
`docs/superpowers/specs/2026-10-13-mono-chrome-color-palettes-design.md`.

### Toolchain (preinstalled in the environment)
- Go (auto-downloaded via `GOTOOLCHAIN=auto` from `go.mod`; needs >= 1.25).
- `bun` (frontend package manager — `bun.lock` is the lockfile; the `Taskfile` default
  `PACKAGE_MANAGER` is `bun`). On `PATH` via `~/.bun/bin`.
- `task` (go-task) and `wails3` CLI on `PATH` via `~/go/bin`.
- System libs `libgtk-4-dev` + `libwebkitgtk-6.0-dev` are installed. They are **required
  just to compile the `wails3` CLI** and any native GUI build (cgo), even though the GUI
  itself can't run headless.

`PATH` additions (`~/.bun/bin`, `~/go/bin`) are persisted in `~/.bashrc`.

### Running the app (headless / cloud)
The native GUI dev flow (`task dev`, `task run`, `task build`, `wails3 dev`) opens a real
desktop window and **cannot run in the headless cloud VM** (no display). Use **server
mode** instead, which serves the same Go services + frontend over HTTP:

- `task run:server` — builds frontend (regenerates bindings, runs `bun run build`) and
  runs the HTTP server. Or, for a quick run without re-running the full task:
  `go build -tags server -o bin/dev-kit-server .` then `./bin/dev-kit-server`.
- Set the bind address with `WAILS_SERVER_HOST` and `WAILS_SERVER_PORT` (e.g.
  `WAILS_SERVER_HOST=127.0.0.1 WAILS_SERVER_PORT=34115`). Server mode only binds loopback
  (`internal/servermode.ValidateHost`).
- Frontend bindings are committed under `frontend/bindings/`; `go build -tags server`
  requires `frontend/dist/` to exist first (it is `//go:embed`-ed), so build the frontend
  before building the server binary if you skip the `task` targets.

Backend method calls from JS go to `POST /wails/runtime`. The vault is **locked on every
process start** (the wrapped keyring persists, but the DEK is in-memory only); the UI shows
a first-run "Create Master Password" or an "Unlock" gate before the workbench.

### Tests / checks
- Go (real code): `go test ./internal/... ./cmd/...`. Note: `go test ./...` will **fail** on
  the root `devkit` package (needs `frontend/dist` and gtk4/webkit for the default GUI
  build) and on `build/ios` scaffolding — that's expected, not a regression. Scope Go tests
  to `./internal/...` (and `./cmd/...`).
- Architecture: `go test ./internal/arch/` fails on forbidden production imports (e.g.
  `bridge` → `storage`/`syncstore`/`sync`/`crypto`). Keep this green on every change.
- Two-device sync E2E: `go test ./cmd/mocksyncserver/` runs a real two-device convergence
  test (two full device stacks over HTTP against the in-memory mock). Run the mock
  standalone for manual testing with `go run ./cmd/mocksyncserver` (loopback, default
  `127.0.0.1:8787`); point the app's sync endpoint at it (plain HTTP is allowed on loopback).
- Deployed sync matrix: `go test ./cmd/synce2e/` skips unless
  `DEVKIT_DEPLOYED_E2E_URL` and `DEVKIT_DEPLOYED_E2E_TOKEN` are set; targets the Compose
  Gateway at `http://127.0.0.1:8082` typically. Source
  `.superpowers/sdd/deployed-e2e.env` (never commit tokens) then run
  `go test ./cmd/synce2e/ -count=1 -timeout 15m`. Each gate resets Compose sync
  tables via `docker exec devkit-db` so G1–G4 pass in one command. Regression:
  `go test ./internal/arch/ ./internal/sync ./internal/syncclient ./cmd/mocksyncserver/ -count=1`.
- Go format/vet: `gofmt -l internal main.go` / `go vet -tags server ./...`. (`build/ios/*.go`
  and `internal/crypto/keyring.go` are not gofmt-clean upstream; ignore them.)
- Frontend typecheck + build: `cd frontend && bun run build` (runs `tsc` then `vite build`).
- Frontend tests: `cd frontend && bun run test` (= `bun test --isolate`, every file in its own global). Without
  `--isolate`, `mock.module` leaks across files: `codegraph/fileActions.test.ts` + `flowActions.test.ts` in one run
  spin forever (CI's frontend job then died at its 20-minute limit), and a file whose mock omits an export can pass only
  because another file loaded the real module first. Spread the real module in a mock (`...(await import(...))`).
- Frontend dev server (Vite only, no Go): `task dev:frontend` (port 9245).

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

### Agent Hub (`internal/agenthub` + `internal/acp`)
**Partial** v1 surface on this branch: ACP codec/conn, mock + Claude adapters,
session service (SQLite migration 13), bridge/UI multi-tab, MCP `agenthub.*`.
Claude vendor spawn uses `sandbox.DefaultRunner().StartPiped` (fail-closed).
A Codex adapter (`adapter_codex*.go`, `codex_*.go`) drives `codex app-server` 0.147.0
under the same jail with its own `CODEX_HOME` (`<data>/agent-homes/codex`) — design in
`docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md`, state in
`docs/superpowers/HANDOFF-codex.md`. Gemini is **not** shipped. Product design + status:
`syurodev/base` → `/dev-kit/agent-hub`, `/dev-kit/implementation-status`.

- **The rail is collapsible groups of cards, and a child session draws under its parent**
  (`frontend/src/modules/agenthub/sidebar/`; `AgentHubSidebar` is the container, `AgentSessionSidebar` the presentation). Groups keep the old membership (`sidebar/grouping.ts`: scratch → workspace id → folder; a
  child stays in its parent's group, whatever folder it was opened in). Inside a group the sessions are a **forest**
  (`sidebar/tree.ts`, pure: `buildForest`, `partitionForest`, `branchSignals`; `SessionBranch.tsx` draws it). A child
  hangs under its parent, indented 28px a level with a guide line leaving the parent's face and an elbow into each row
  (`TREE_GUIDE` from `components/tree-guides.tsx`); only two levels are indented (`INDENT_LEVELS`), deeper ones keep
  the last indent and draw no line, and `aria-level` still says the real depth. The markup is nested `list` /
  `listitem` (the parent's list item holds the card and the children's list), not `role="tree"`: a card holds buttons
  and menus, which a tree widget's rules do not allow.
  - A group draws its 5 most recent **trees** plus exceptions — a tree is as recent as its newest session, counts
    as one entry however many sessions it holds, and is kept when it holds the session on screen or a pinned one — and
    folds the rest into a stack. Order is `AgentSession.updatedAt` (bridge: the row time, not touched by transcript chunks; children newest first) then id.
    "Waiting" reads `pendingQueue`/`elicitationQueue` as well as `status`. A child whose parent is missing
    from the group (deleted, another group, closed and hidden, waiting in Needs you) is a root; a cycle in the data
    puts its newest member at the top and loses nobody.
  - A parent with children draws a button (branch glyph, the count of sessions under it, a chevron; Left/Right on the
    row's title also fold/open, Left on a child goes to its parent). Children are **open by default**; what the person
    folded is remembered per session id (`agentPins.foldedBranches`, `foldedBranchIds` / `onToggleBranch`). A branch
    cannot be folded while the session on screen or a pinned one is in it (`heldBranches`).
  - A parent carries one small dot for what is below it (`branchHint`): a waiting or failed child **always** (those
    are drawn in Needs you, never under the parent, so the parent is where they are marked — with no fold button if
    nothing else is under it), a running child or unread messages (`peerUnread`, the only unread mark Agent Hub has)
    only while the branch is folded. Signals are counted over every session, not just the drawn ones.
  - The rail's Subagents (the vendor's own) are a different thing and stay in the row menu.
  `surfaces/AgentSidebarTree` is no longer rendered by Agent Hub (gallery and tests only).
  Gallery: `?gallery#agenthub-session-rail` (the "branches-folded" rail shows the folded dots).
- **The rail and the main view are a quiet list and a column with a summary card**
  (spec `docs/superpowers/specs/2026-10-13-agent-hub-wide-layout-design.md`, its last section lists
  what Phase 1 changed from it).
  - Rows are 50px: the vendor face (`sidebar/SessionAvatar.tsx`) with its state on it (running arc,
    waiting/error dot), title and age, then a second line: the pending ask (the first permission wins),
    else `AgentSession.preview`, which Go writes at turn end (migration 41, `agenthub/preview.go`). MCP
    blanks the preview. Subagents are in the row menu.
  - "Needs you" lists waiting/failed sessions across workspaces (`needsYou`, through `groupSessions`'
    keep filter, so a child stays with its parent's group).
  - The title-bar workspace comes first and open; other groups fold to one line with a running count
    (`agentPins.expanded`), except one holding the selected session.
  - A turn this window did not send refreshes only `preview` + `updatedAt`
    (`AgentSessionsSubscriber`, latest-read guard).
  - At `@6xl/agent-conversation` the session summary card (`summary/`: Plan, Changes, Sources,
    Subagents, Context) docks top-right and the column (`max-w-190`; the rail keeps `max-w-180`) moves
    left (`pr-80`). Below that size a corner button opens it. The data is derived once per view.
  - Web links open via `bridge/browser.ts` `openExternal`; file refs, symbols and commits: next bullet.
  - The docked summary replaces the latest plan's card with one line (`Plan · 2 of 4 — in the summary`)
    only in the Hub (`planDocked`; the rail has no dock). At `@7xl/agent-conversation` the column grows to
    `max-w-220` (880px). A row's ask is `plainAsk` (ticks and word-wrapping `**` dropped, paths intact).
- **The summary card is filled by what each vendor reports, not by one layout.** A section is drawn only
  when the session has data for it — never empty, never a placeholder — so Claude and Codex differ on purpose.
  Both: Environment (agent, model, effort, mode, workspace, folder, branch; `environmentRows`), Goal, Plan,
  Changes (with a +/− total), Sources, Subagents, Context. Claude only (its ACP stream has them): Background
  tasks (`AsyncTask`), cost, and one plan window from `_claude/rateLimit`. Codex only: the token split
  (`Usage.tokens`), the plan's windows (`account/rateLimits/updated`) and Connectors (`mcpServer/startupStatus/updated`,
  `UpdateConnectors`, a session-state kind kept as one row). Claude's context split by category and its MCP status
  are **not** on the ACP wire (`getContextUsage` / `mcpServerStatus` exist in the SDK, the adapter does not forward
  them) — adding them means an adapter extension, not a guess. `Usage` replaces whole on every update, so the Codex
  translator keeps tokens, limits and the last meter together and re-sends them (`usageUpdateLocked`). Extras
  travel on the wire's `_devkit` key (`wireDevKit`). Every section is a shadcn `Collapsible` (`SectionShell`); what the person
  folded is remembered across sessions in `localStorage` (`summary/summaryFold.ts`, shared by the docked card and the
  corner popover). Gallery: `?gallery#agenthub-summary`.
- **A session's pull request is tracked, not guessed** (`internal/forge`, `internal/prwatch`, bridge
  `PullRequestState|Attach|Detach`, frontend `summary/usePullRequests.ts`, `pullRequestModel.ts`,
  `PullRequestCards.tsx`; section "Pull requests" beside Changes). `forge` is a leaf: a platform-neutral `Provider`
  (`ChangeRequest`, `Check`, `Comment`; `Terms` carries "PR"/"MR" and "checks"/"pipelines") with GitHub as the first
  implementation — GitLab is a second `Provider` and a line in `NewRegistry`, no UI or data-shape change.
  `prwatch` reads a folder through `Source` (git: branch, push remote, origin, default branch), a token through
  `Credentials`, and finds the request by `head=<owner>:<branch>` in origin's repo (a fork's branch uses the fork's
  owner). The account is the one bound to the remote if it is a token, else `Accounts.ForRemote(host, owner, …, https)`;
  the background poll never asks the person (two fitting accounts → "ambiguous", say so), an agent's tool call may.
  Every status without a card is stated (`no_account`, `unauthorized`, `rate_limited`, …) except the quiet ones
  (no request yet, detached, default branch, not a repo, unsupported host). Polling is the window's: `State` returns
  `nextPollMs` (15 s while a check runs or mergeability is unknown, 60 s settled, 90 s with no request, -1 when merged
  or closed or when it needs a person); react-query pauses while the window is hidden; a turn ending, window focus
  and the `prwatch:changed` event (pr.create, attach) refresh at once. Reads are conditional (ETag/`If-None-Match`,
  a 304 costs no rate limit, the cache key holds a digest of the token, never the token); two calls within 3 s are
  one round; 4 requests in flight. The unresolved-thread count (GraphQL, no ETag) is re-read when the request
  changed or every 3 min. `session_change_requests` (migration 42) holds requests attached by hand or by `pr.create`.
  Not done: GitHub Enterprise and a self-hosted GitLab (no host→account mapping: `Registry.For` picks a provider from a
  bare host, and the account registry knows only github.com / gitlab.com — `forge.WithGitLabHosts` is where a mapping
  would plug in, nothing calls it), waking a session when CI fails, an entry point to attach by address when the
  branch lookup found nothing (the bridge method exists).
- **GitLab is the second provider** (`internal/forge/gitlab*.go`, `NewRegistry(NewGitHub(), NewGitLab())` in
  `app/prwatch.go`; nothing above `Provider` changed, no frontend change). **Written against GitLab's documented REST v4
  and a fake (`httptest`); no real gitlab.com call has been made** — not by the tests, not by the author. What is
  therefore unverified: every real response shape, `detailed_merge_status` values on the person's GitLab version, the
  GraphQL stats query, and which endpoints honour `If-None-Match`.
  - Serves `gitlab.com` only. A project is addressed by its whole path in one segment (`acme%2Fplatform%2Frepo`, nested
    groups work, `Repo.Path` keeps all segments); a request is its project-scoped `iid` ("!7"), found by `source_branch`
    in the target project (a fork's branch is told apart by `source_project_id ≠ target_project_id`; `HeadRepo` is the
    fork's path from one extra `GET /projects/:id`, and `project:<id>` when that fails — never empty, so `prAgentOwns`
    still sees a fork). Auth is the `PRIVATE-TOKEN` header only (a PAT, project or group token; an OAuth access token
    would need `Bearer` and is not handled). Redirects are never followed (Go forwards a custom header across hosts) and
    a paging link to another host is refused; an error message has the token replaced if a host echoes it.
  - Mapping: `Terms` = GitLab/MR/merge request/pipelines. `detailed_merge_status` → clean (`mergeable`), conflicting
    (`conflict`), behind (`need_rebase`), unknown (`checking|unchecked|preparing|approvals_syncing`), anything else
    blocked; an older host falls back to `merge_status`/`has_conflicts`/`blocking_discussions_resolved`. Draft =
    `draft`/`work_in_progress`; `locked` counts as open. Checks = the jobs of `head_pipeline` (a manual or
    allowed-to-fail job counts as skipped=passed; a pipeline with no jobs is one check named `pipeline`, so "starting"
    and "failed before any job" are not silence). Review decision = `requested_changes`, else non-empty `approved_by`
    (a tier without approvals answers 403/404, read as "no decision"). Comments = `/discussions`, system notes dropped,
    ≤ 3 pages of 100; a resolvable discussion (on a diff line or on the request) is a `review` thread with `resolved`;
    unresolved count = discussions with an open resolvable note. A `reply` takes a note id (from `pr.comments`) or a
    discussion id; a plain conversation comment cannot be replied to (use `pr.comment`).
  - Errors → `forge.Error`: 401 unauthorized, 403 forbidden (403 + `RateLimit-Remaining: 0` rate limited), 404 not
    found, 429 rate limited (`Retry-After`, else `RateLimit-Reset` as Unix time), 400/405/406/409/422 validation (a
    merge of a moved branch is 409 "SHA does not match HEAD"), the rest unavailable.
  - Differences a caller can see: **line counts** are not in REST, so `Get` asks GraphQL `diffStatsSummary` once per head
    commit (cached by sha) and, if that fails, leaves 0 with a note "line counts unavailable"; **`rebase` is refused**
    (GitLab merges by the project's own method; `forge.MergeMethodChecker` → `Handle.CheckMergeMethod` makes `pr.merge`
    refuse it before asking the person) and `squash` is the request's `squash`; `should_remove_source_branch` and
    `merge_when_pipeline_succeeds` are never sent (deleting the branch is `pr.deleteBranch`); a draft is the title's
    `Draft:` prefix (added on create, kept by `pr.update`); creating from a fork is refused; `Branch` also checks the
    project's `default_branch`, so a host that omits `default` cannot make the default branch deletable.
  - Polling cost: the same `nextPollMs` rules. A settled GitLab poll is MR + jobs + approvals (+ discussions every 3 min
    or when `updated_at` moves) — about three requests per round against gitlab.com's documented 2,000 authenticated
    requests per minute per user. Conditional requests are sent only when the host itself issued an `ETag` for that address and token; whether
    gitlab.com does for these endpoints is unknown, and nothing depends on it.
  - Manual check with the person's own token (read-only first): add a GitLab token in Accounts (a PAT with `read_api`;
    `api` for the writes), open a session in a repository whose branch has an MR, and look at the "Pull requests"
    card (MR, pipelines, unresolved count, +/− lines); then `pr.get`, `pr.checks`, `pr.comments` from the agent. Only
    after those match GitLab's page, try `pr.comment` (asks first), then `pr.create`/`pr.merge`. The fake-host tests:
    `go test ./internal/forge/ ./internal/prwatch/ ./internal/mcp/ -run 'GitLab' -count=1`.
  - A GitLab error's own text reaches an agent only once `internal/bridge/errors.go` `keptCodes` keeps the `pr.*` codes
    (a separate change); until then the agent sees the code's generic message.
- **References in agent prose resolve, preview and open** (spec
  `docs/superpowers/specs/2026-10-14-agent-hub-references-design.md`, last section = what changed;
  files under `frontend/src/modules/agenthub/transcript/`).
  - Parsing is pure: `fileRef.ts` takes `a/b.go`, `./`, absolute, `:27`, `:27-30`, `:27:3`, `#L27(-L30)`
    and an allow-list of known names (`Dockerfile`, `go.mod`, …), never URLs, versions or escaping `..`;
    `linkTarget(href)` splits a markdown link into url (`http(s)`/`mailto`), file (no scheme / `file://`)
    or other (plain text, href in a tooltip).
  - `resolveFileRef.ts` resolves in the **session's** workspace (`RefContext` = session id, workspace id,
    project path): absolute → project-relative → each folder root → by name (`agentHubListContextFiles`,
    suffix match, ≤ 11 stats); first step with hits wins; found / ambiguous (≤ 10) / missing / unsupported
    (scratch). Cached per **path** (`fileRefQuery`, key `["agenthub","fileRef",session,path]`, 30 s);
    `withLine` puts the reference's own line back. Errors are thrown, never cached as "missing".
  - `openReference.ts`: `openFileRef` dispatches `codegraph.reveal` with `workspaceId` + `folderId`
    (`codegraph/revealFile.ts`; any workspace, sidebar → Files); ambiguous opens `RefPicker.tsx` anchored
    at the reference (focus returns to it, Home/End); missing, a throw, or a reveal that answers
    `"unavailable"` (an intent may return it instead of throwing) each toast; scratch falls back to
    `project`. `refOpener` + `TranscriptActions.refPlacement`: the Hub opens beside, the rail in place.
  - `ReferencePreview.tsx`: shadcn HoverCard 400/120 ms, opened by a resting pointer only (not focus,
    a press or an open context menu), trigger wrapped in `ContextMenuTrigger` the ModuleSwitcher way. The
    file body is always 9 rows (±4 around the line, else the first 9), cut from one highlight of the whole
    file or ±200 lines past the highlight limit; reads (`readFolderFile`) start on open, `staleTime` 5 s /
    `gcTime` 30 s. URL cards fetch nothing. Menus (`referenceMenu.ts`, rows built when a menu opens): Open
    beside / Open here / Copy path / Copy path:line / Reveal in Files; Open in browser / Copy link; Go to
    definition / Copy name; Open in Git / Copy SHA.
  - Symbols and commits (`codeRefs.ts`, `useCodeRefs.ts`, `CodeReference.tsx`) only in **settled**
    messages, never inside a link's text. Symbols: identifier shape, 3–80 chars, not a keyword, and
    `worthLookingUp` (last segment ≥ 4, or qualified, a call or camelCase); ≤ 12 names per message,
    `searchWorkspaceSymbols` on the last segment, 3 in flight app-wide (`createLimiter`, aborts drop
    queued work), exactly one exact match or nothing; cached 5 min, 15 s while a folder is still
    indexing. Commits (`[0-9a-f]{7,40}`, a digit and a letter) fetch nothing; a click pins Git to the
    session's project folder and dispatches `git.reveal`. Dotted underline; file refs solid.
  - The summary's Sources count only data-touching tools (`summaryModel.ts` `DATA_TOOLS`: queries,
    schema reads, Kafka reads/publishes, `ssh.runCommand`) and say what was done ("2 queries").
- **Tool results read as themselves** (Phase 2; spec `docs/superpowers/specs/2026-10-13-agent-hub-wide-layout-design.md`
  §5–§7 and its "Phase 2 decisions"; `docs/dev-kit/tool-result-view.mdx`).
  - One payload whatever the vendor sends (`workbench/toolPayload.ts`): `toolPayloadOf` → `{payload}` /
    `{error}` / null, `toolNameOf` (a Codex call has no name, only the title `devkit.<tool>`), `isDevkitTool`.
    Claude's answer is `content[].content.text` (no `rawOutput`); Codex's is `rawOutput`
    `{content, structuredContent}` (structuredContent wins), `{error}`, or a 64 KiB stub `{truncated, bytes}` →
    null. A fenced `code: message` is an error only for a DevKit tool (a shell may print one). An 8 KiB cut
    preview that does not parse is null and the full row is loaded (`transcript/toolCallDetail.ts`, one TanStack
    query per call, shared with "Show full output"); the stored page-image marker (`transcript_slim.go`) is not
    an answer. Views get only successful payloads; a failed row stays the failed row.
  - The tool's owner registers its view: `db.*` in `modules/database/toolViews`, `kafka.*` in
    `modules/kafka/toolViews`, `ssh.runCommand` in `modules/ssh/toolViews`, `pdf.*` in `agenthub/toolViews`
    (a PDF belongs to the session), `workspace.memory.*` in `workbench/registerViews.ts` (no module owns it).
  - Cards stand after the run's fold (`transcript/ToolResultCard.tsx`): `resultCandidate` takes only
    completed, non-activity calls whose view will draw (or a cut preview waiting on its row), 3 per turn in
    call order; the rest are counted in a "+N more" button that opens the fold at the first hidden step.
    A card shown after the fold stays a one-line row inside it. Views render inside an error boundary: a
    card that throws, or a lazy chunk that fails, falls back to the row / generic content, not a blank app.
  - Every table is `components/ui/data-table/DataTable` (markdown tables in prose too): column resize by
    drag, arrows or double-click auto-fit, widths in component state only; a clickable row opens through a
    button in its first cell (the row keeps its role); windowed past 100 rows.
  - `database.reveal` (`{kind:"table", id:"schema.table", profileId?}`, a schema with a dot in SQL double
    quotes, opened on `profileId` through the normal selection path, "unavailable" for an unknown profile; or
    `{kind:"console"}`) and `kafka.reveal` (`{kind:"topic"}`: shows Kafka first, refreshes from Go, gives up
    after 12 s) are how agenthub opens them. Open in Database never runs SQL, is not offered for writes,
    titles the console "Agent query", and never writes over a saved console (`database/consoleHydration.ts`
    reads the console files once per window, shared with the rail). `db.query` results carry `profile_id`
    (the id the gate confirmed; MCP-only `dbQueryResult`, the console's `QueryResult` is unchanged): the card
    names the connection, and Open in Database (query, and `db.explain` by its input's `profileId`) selects it
    first through `selectProfileById` (shared with `database.reveal`; not on this device → toast, nothing opens;
    the reload before it does not auto-select). A switch that would end an open transaction or stop a running
    console query asks first (`workbench/confirmRequest` → `ConfirmHost`, the shared `ConfirmDialog`; no host →
    no); Cancel opens nothing, without a toast. A query's rows show as the console's result without running
    (`showAgentResult`, `ResultTab.origin: "agent"`, in memory only, "Agent's result · N rows — run to
    refresh"), only into a console holding that statement with nothing of the person's (no result, error or
    run; earlier agent rows refresh); a run replaces them. Writes get no Open, by shape or by statement
    (`isWriteStatement`: RETURNING, a data-modifying CTE). An older transcript's query (no `profile_id`) opens
    on the selected connection with a notice, rows labelled "connection unknown".
  - Tables and topics the session's own results named are references (`transcript/entityRefs.ts`): from
    `db.schema.get`, `db.listForeignKeys`, `kafka.listTopics` and the topic of an answered peek / describe /
    publish. Settled messages only, never inside a link. Order: entity (unless the span has a `/` and reads
    as a path) → file → commit → symbol. A bare table only when one schema has it; a name that is both a
    table and a topic, or two tables spelling one qualified name, links nothing. Up to 4 cut answers per
    session are loaded in full to read their names. A table reference carries the `profileId` its
    `db.schema.get` / `db.listForeignKeys` input named (`EntityRef.profileId`), so it opens on the agent's
    connection, as `db.query`'s card does by its result's `profile_id`; Open in Kafka uses the connection
    selected here (its picker already connects the shared one).
  - Go: the bridge keeps the message of only the listed `pdf.*` / `memory.*` codes (`bridge/errors.go`
    `keptCodes`; anything else is `internal_error`). PDF engine text reaches only the log.
- **The composer is one cluster in its own layout region** (`surfaces/AgentComposer.tsx`,
  `composer/`; `AgentHubContent` and `AgentRail` give it `agent-composer-region` below the transcript).
  Nothing floats over the transcript: no `--composer-inset`, no measuring observer; `TranscriptStream`
  keeps the end in view when its viewport changes height only while following. A compact context header
  (`ComposerContextBar`, built by `useAgentTray`) frames an inset input using the shared FramedCard
  surface/radius tokens (raised frame, base inset, 4px rim). Panel area (`ComposerPanelArea`) grows
  above that header; inside the input inset: field → tools and Send. `chooseTrayPanel` / `repoSummary` (`composer/trayModel.ts`) are the pure rules: a
  permission beats a question beats a list the person opened; a repo chip says the total and, as a
  separate badge, how many need attention. Heights are capped with `cqh` classes in `index.css` against
  `.agent-conversation`. Browser checks (focus, scroll, short and narrow panes) live in the dev
  gallery's "composer cluster" section (`?gallery#agenthub-composer-layout`, bridge-free through
  `TrayStatusOverride`); the bun tests only read markup and pure rules.
- **Lists and emphasis in the composer are plain markdown text, not document nodes**
  (`surfaces/composer/listEditing.ts`, pure functions; wiring in `AgentComposer.tsx`). The prompt must leave
  exactly as typed (`composerSerialization.ts`), so nothing about a list can be rewritten on the way out. What
  the composer adds is editing: Shift+Enter inside a `- ` / `1. ` line starts the next item (an empty item ends
  the list; Enter still sends), Cmd/Ctrl+Shift+8 / +Shift+7 toggle bullet / numbered markers on the selected
  lines, Cmd/Ctrl+B / +I wrap the selection in `**` / `*`. Not inside a code block, not while composing, and
  not on a line that begins after an `@` chip. Files dragged in from the OS arrive as `agenthub:filesDropped`
  (Go: `file_drop.go`, `EnableFileDrop` + `data-file-drop-target` on the composer) and become the same chips.
- **The frontend reads a vendor table** (`frontend/src/modules/agenthub/vendors.ts`:
  name, runtime id, how it signs in, colour). Do not compare against `"claude"`; an
  unknown vendor gets its own name and nothing to install or sign in to.
- **The window's pre-session reads need their own grants** (`catalog:*` for `AgentHubConfigCatalog`, `environment:*` for
  `AgentHubEnvironment` without a session). They were asked for since #30fedee3 / #231 with no grant covering them, so the
  gateway refused every call with a bare "Permission denied." — the draft picker's "Models unavailable" and the empty model
  list in Settings → Agents → "Models by job" (which then fell back to typing a model id). The service still answers only
  the window. `TestNewContainerGrantsAgentHubScopes` and `TestAgentHubPreSessionReadsAreNotRefusedByTheGateway` pin it;
  when a bridge method asks for a new scope, add its grant and a line to the former.
- **Both vendors pick model and effort before the session exists.** `VendorConfigCatalog`
  (`ConfigCatalog`) is implemented by Codex (`codex_config.go`) and Claude (`claude_config.go`):
  a short-lived agent in a scratch folder, no prompt. Claude only lists the effort levels of the
  model it is on, so the catalog switches to each model and reads them back from
  `session/set_config_option`. The draft picker calls effort `reasoning_effort` for every vendor;
  `claudeRuntime.SetConfigOption` maps it to Claude's own `effort`.
- **Codex signs in inside DevKit, UI only.** Bridge `AgentHubSignInStatus` /
  `AgentHubStart{Browser,Device}SignIn` / `AgentHubCancelSignIn` / `AgentHubSignOut`
  (`auth:codex`), event `agenthub:signInCompleted`. There is no MCP tool for it
  (`TestNoToolSignsAgentsIn`), and `Service` refuses a caller stamped as an agent or a
  user-terminal MCP client. Audit lines carry the method and account id, never the
  login page, code or key.
- **Codex API key = an Accounts token, by id.** `preferences` stores only the id
  (`agent_codex_api_key_account`); `app.CodexAPIKey` resolves it at spawn and Accounts
  refuses to delete it while chosen. A successful ChatGPT sign-in adds a `KindExternal`
  "Codex (ChatGPT)" row to Accounts; signing out removes it.
- `agent_not_installed` / `agent_auth_required` name the vendor: adapters wrap those
  sentinels in `agenthub.VendorError`, and `bridge/errors.go` words the message per
  vendor. A chosen API key that is gone or refused also carries
  `ErrAgentAPIKeyUnusable` → `agent_api_key_unusable`, which the UI sends to Settings
  rather than to a sign-in (the key wins at every spawn). `app.CodexAPIKey.Choose`
  refuses agent / user-terminal callers like sign-in does.
- **A session nobody has prompted has no conversation to resume** (`Service.neverPrompted`, `resumeRuntime`).
  Codex writes none before the first turn (measured with the real app-server 0.163.0: `thread/resume` of such a
  thread answers "no rollout found for thread id"); Claude Code keeps its session file from the first message
  (not measured here — reopening is harmless either way, since nothing is lost). So a session with
  no turn row and no user message is reopened with `adapter.Open`, not `Load`, and its vendor id updated
  (`UpdateSessionACPID`); the model/effort chosen at creation come back from the stored `config_option_update`.
  Before this, every settings call on such a session after a restart failed, which the picker showed as
  "Models unavailable". Tests of the resume path must say the session was prompted (`markPrompted`).
- **A fresh Claude session carries its model list from the handshake.** claude-agent-acp sends the settings once,
  in the answer to `session/new` / `session/load`, and never a `config_option_update` until one changes — and
  DevKit used to drop that answer (only the catalog read it). `claudeRuntime` now keeps it (`options`), folds
  every `set_config_option` answer into it, announces it to the first update handler (as `codexRuntime` does)
  and re-publishes it on `PrepareSession`. A refused model list reaches the picker as
  `agenthub.modelsUnavailable` ("Codex didn't report which models it offers…"), not "Agent process failed.".
- **A child session may write what its parent may write, never more** (`Service.CreateSession`, `inheritedExtraPaths`,
  `resolveCwd`, `checkChildFolderInWorkspace`). An agent-created child gets its parent's stored `ExtraReadWritePaths`
  (cleaned; read from the parent's row, never from the tool input — `agentHubCreateInput` has no path field,
  `TestCreateAgentSessionHasNoPathGrantInput`, and an agent caller's own `CreateInput.ExtraReadWritePaths` is still
  ignored) and persists them on its own row, so resume/rewind/reload rebuild the same jail, the brief and
  `workspace.list`/`git.*` `granted` (all read project path + stored extras) list them, and a grandchild inherits from
  its own parent: authority only narrows or stays equal along a chain. Its cwd may be inside the parent's project **or**
  inside one of the parent's extra folders (symlinks resolved on both sides; else `ErrCwdOutsideParent`), and a cwd in an
  extra folder must also be a folder of the parent's workspace when the workspace and a lookup exist. The child does not
  receive the parent's *project* folder as an extra (a child opened in an extra folder cannot write the parent's cwd; one
  opened in a subfolder stays in it). Before this a child had no extras, so a Codex child refused work in folders its
  parent could write. A person-created session is unchanged. Not run against the real vendors' sandboxes: the tests read
  the `OpenSpec` a fake adapter received.
- **An agent chooses a child's model and effort, and the person's profiles guide it**
  (`agenthub/model_catalog.go`, `model_choice.go`, `mcp/tools_agenthub.go`, `preferences/modelprofiles.go`,
  `bridge/agenthub_modelprofiles.go`, Settings → Agents → "Models by job" in `settings/ModelProfilesEditor.tsx`).
  - `agenthub.createAgentSession` takes `initialConfig {model, effort}` (effort is `reasoning_effort` inside)
    and `profile` (a profile's name or id). Nothing said → the child inherits its parent's model and effort
    when it is the same vendor (a value the runtime refuses is dropped). Values are checked against the
    vendor's catalog; a wrong one is refused with what the vendor offers. The parent-only, parent-folder and
    parent-workspace rules are untouched (`ErrCwdOutsideParent`, `ErrWorkspaceMismatch`).
  - **Cost policy** (`PlanModelChoice`): a profile is the person's own choice and asks nothing. By hand, a model
    other than the parent's asks the person — DevKit has no reliable price order for models, so cheaper asks
    too — and an effort above the parent's asks (efforts are ranked: none < minimal < low < medium < high <
    xhigh < max; "default" or an unknown name is unranked and asks). The question is the "DevKit tool gate"
    (`KindAgent`, `EnvBlocks: true`: every time, no allow-always). `CreateSession` repeats the check and refuses
    with `ErrModelChoiceNeedsApproval` unless `CreateInput.ModelChoiceApproved`, which only the tool handler
    sets after the answer (its input struct has no field for it).
  - `agenthub.listModels` / `agenthub.listModelProfiles` are read-only (`agenthub:read` on `models:list` /
    `profiles:list`; `mcp.AgentHubModelsGrant` / `AgentHubProfilesGrant`, checked against the real policy in
    `TestNewContainerGrantsTheModelReadTools`). The catalog is cached per vendor (`Service.ModelCatalog`: 15 min,
    a failure 1 min, one throw-away agent at a time, detached from the asking call); the window's own
    `ConfigCatalog` stays uncached and refreshes it. A vendor that cannot answer comes back with a status
    (`signInRequired`, `notInstalled`, `unavailable`) instead of failing the call.
  - **A long list is paged, a name is resolved** (`model_catalog.go` `pageModels`, `model_match.go`). `agenthub.listModels`
    takes `query` (words from the id or name, case/punctuation ignored), `limit` (default 40, max 100, per vendor) and
    `offset`; a vendor with hundreds of models (opencode) used to blow the 24 KB result budget and be cut blindly. A page
    carries `total`, `nextOffset` and a `hint`, names equal to the id and long descriptions are dropped. `PlanModelChoice`
    resolves what the agent wrote (`VendorModels.Resolve`: id, id in another case, display name, same letters/digits, a
    fragment naming exactly one model) to the vendor's id, and the cost comparison, the question to the person and the
    session use that id. A guess is never taken: an unknown or ambiguous name is a `ModelChoiceError` (an
    `ErrInvalidConfigOption`) whose sentence lists the whole list (≤ 12 models) or the closest few, and the bridge
    publishes it (`ModelChoiceDetail`) instead of "Choose a valid agent setting.". opencode lists efforts only for the model
    it is on, so an effort asked for another opencode model is refused as "takes no effort" — not fixed here.
  - **Profiles** are the preference `agent_model_profiles` (revision/CAS like `AgentCustomization`; ≤ 24, name 60,
    description 400, notes 800 bytes; unique names; ids are slugs of the name). Get/set/check are UI-only
    bridge methods (`AgentHubGetModelProfiles` / `SetModelProfiles` / `CheckModelProfiles`, grants
    `preferences:* agent_model_profiles`); no MCP tool edits them (`TestPromptTeachesModelChoiceAndNoToolEditsProfiles`).
    A profile whose model the vendor dropped is listed with `status` + `problem` and refused when chosen;
    its text reaches an agent as the person's configuration (`ModelProfilesNote`), never an instruction. The
    audit line of a child session names the profile, model and effort — never a prompt.
  - Not verified against the real vendors in CI: Claude/Codex model lists come from fakes
    (`TestFreshClaudeSession*`, `TestCodexSessionWithoutATurn*`, `choiceAdapter`); the Codex "no rollout" answer
    was measured with the real app-server 0.163.0 under a throw-away `CODEX_HOME`. Gallery:
    `?gallery#agenthub-model-profiles`.
- **A child session starts in its parent's permission mode, never a more permissive one**
  (`agenthub/mode_inherit.go`; `Service.CreateSession`, agent callers with a parent only). The parent's mode is read from
  its stored `current_mode_update` row (seeded from the handshake, moved by every change — the Claude runtime now keeps
  `Modes()` current after `set_mode`, so a resume re-seeds the live mode, not the handshake's), not from the tool input:
  `agenthub.createAgentSession` has no mode field and `TestCreateAgentSessionHasNoModeInput` keeps it so. Same vendor →
  the parent's id. Another vendor → the same id if it advertises it (`auto`, `bypassPermissions`), else the pair Claude
  `default` ↔ Codex `ask`; a parent stricter than `auto` with no match (`plan`, `acceptEdits`) gets the child's asking
  mode, anything else stays on the child's default (logged). A mode the runtime refuses does not fail the creation —
  unless the parent is Ask / `default` / `plan` and the child would be more permissive: then the child is closed and
  `ErrModeNotInherited` returned. A person-created session keeps the vendor default. Not run against the real vendors:
  the tests use fakes.
- **A profile can also be a vendor subagent** (`internal/agenthub/subagents.go`, `claude_subagents.go`,
  `codex_subagents.go`; fields `subagent` + `prompt` on `AgentModelProfile`, off by default, so an old list changes nothing).
  A subagent is the vendor's feature (Claude's Agent tool, Codex's `spawn_agent`) running inside the session — not
  `agenthub.createAgentSession`. `applySubagents` runs beside `applySandboxExtras` at create, resume and rewind (never inside
  it: the catalog read calls that) and puts the vendor's own profiles in `OpenSpec.Subagents`; a running session keeps its set
  until Reload (Environment → Reload session). `planSubagents` leaves a profile out, with the reason, when its model/effort is
  not on the vendor's (cached) list, when Claude's effort is not `low|medium|high|xhigh|max`, or when the person already has a
  subagent of that name (`.claude/agents`, `.codex/agents`, `[agents.<n>]`; the programmatic definition would shadow theirs).
  `preferences.SubagentNames` derives the name from the profile **id** (ASCII slug, unique, never a vendor built-in).
  - Claude: `_meta.claudeCode.options.agents[<name>] = {description, prompt, model?, effort?}` and nothing else — no tools,
    permissionMode, mcpServers or skills (`TestClaudeSubagentsMetaWritesOnlyWhatCarriesNoPermission`). `model: "default"` is omitted.
  - Codex (**schema read from the 0.163.0 binary and run for real**): a role is `agents.<name>` = `{description, config_file}`
    (`AgentRoleToml`, + `nickname_candidates`); the role file needs `developer_instructions` and takes `model`,
    `model_reasoning_effort` (and `sandbox_mode`, which DevKit never writes). DevKit writes the file under
    `<codex home>/devkit-agents/<name>-<hash>.toml` (content-named so a running session keeps its file; jail may not write the
    dir; files unreferenced for 24 h are swept) and registers it per process with `-c agents.<name>.description=… -c
    agents.<name>.config_file=<abs>`, never in the shared config.toml. In the app-server (`multi_agent_v2`) the role appears in
    `collaboration.spawn_agent`'s `agent_type` with the description and "model … effort … cannot be changed". Codex only
    delegates when the person or an AGENTS.md asks for sub-agents — its own rule, stated in the UI.
  - `agenthub.listModelProfiles` also returns `subagent`, `subagentName`, `subagentStatus` (`active|skipped`) and
    `subagentProblem` (never `prompt`; only the window's own list carries it). No new tool or bridge method, so no new grant.
  - Real runtimes, mock model API, fake HOME: `cmd/pdfe2e/subagent_test.go` (skipped unless `DEVKIT_SUBAGENT_E2E_CLAUDE`,
    `_NODE`, `_CODEX` are set — see its header): a new Claude and a new Codex session each show the subagent, the child
    runs on the profile's model/effort/instructions, and a Claude subagent's shell `git commit` is refused by DevKit's hook.
    The harness gateway allows everything, so a gated tool's confirm card is not exercised through a subagent.
- **Codex subagent limits: one cap on how many, no depth** (`internal/preferences/subagentlimits.go`, preference
  `agent_subagent_limits`, `internal/agenthub/codex_subagent_limits.go`, bridge `AgentHubGet|SetSubagentLimits`
  (UI-only, revision/CAS, grants `preferences:* agent_subagent_limits`), Settings → Agents → Models & subagents →
  "Subagent limits", gallery `?gallery#agenthub-subagent-limits`). Unset = DevKit writes nothing. A value (0–16; Go
  rejects more, `codexSubagentLimitArgs` also clamps) is passed per process as `-c
  features.multi_agent_v2.max_concurrent_threads_per_session=N+1 -c agents.max_threads=N` beside the role registration
  (`applySubagentLimits` runs in `applySubagents`, so create, resume, rewind and Reload all re-read it; a running
  session keeps its limit until Reload). Never config.toml. No MCP tool reads or edits it
  (`TestNoToolTouchesSubagentLimits`). Claude: no control (its subagents cannot start subagents; DevKit sets no
  count), said in one line in the UI.
  **Measured with the real `codex app-server` 0.163.0** (`cmd/pdfe2e/subagent_limits_test.go`, scripted mock model,
  fake HOME; env as `subagent_test.go`; `go test ./cmd/pdfe2e/ -run SubagentLimit -v`):
  - **Slots**: the app-server runs multi_agent_v2 (`multi_agent_v2` is a stable, on feature; setting it false changes
    nothing). The root's developer message says "There are N available concurrency slots … including you". Default
    **N = 4** (the main agent + 3 subagents). `agents.max_threads=K` → K+1 slots (K subagents), and so does
    `agents.max_concurrent_threads_per_session=K`; `features.multi_agent_v2.max_concurrent_threads_per_session=M` → M
    slots (root included) and **wins over the agents.* keys when both are set** — hence DevKit sets both (the shared-home
    import carries the user's `features` table, and a `-c` table over a user's bool/table `multi_agent_v2` is
    accepted). Values up to 500 were accepted (no Codex cap; DevKit caps at 16). **`agents.max_threads=0` stops
    Codex starting** ("agents.max_concurrent_threads_per_session must be at least 1"), so "none" is expressed as the
    v2 key = 1 only (every spawn is refused). `agents.max_threads` is not the default 6 the docs suggest in this mode.
  - **A finished subagent frees its slot** (three children answered, then a fourth and fifth spawned fine); one that is
    still working keeps it. Nested spawns draw from the same pool: a chain root→a→b→c with 4 slots refused the c's child.
  - **A refused spawn** reaches the model as the `spawn_agent` call's output, the plain text `collab spawn failed:
    agent thread limit reached` — no error flag, the turn goes on.
  - **`agents.max_depth` does nothing here.** With room for 20 slots, depth 0, 1, 2 and the default all let a chain
    nest 4–7 levels (`/root/l0_w0/l1_w0/l2_w0/…`); `spawn_agent` stays offered to every level. ("Agent depth limit
    reached. Solve the task yourself." is in the binary, but this mode never produces it.) So nesting is bounded only by
    the slot count, and **DevKit offers no depth control** — a limit of 1 is the way to forbid nesting.
  - **`agents.job_max_runtime_seconds`** belongs to Codex's agent-job (CSV batch) tool, which is not among the tools the
    app-server offers (`exec wait … spawn_agent wait_agent`), so it is not exposed. `default_subagent_model` /
    `default_subagent_reasoning_effort` are covered by profiles-as-subagents.
  - Not verified: a real ChatGPT/API account may get different remote feature flags than the keyless runs above.
- **Codex sessions have session commands, DevKit's own** (`internal/agenthub/codex_commands.go`, `codexRuntime.Prompt`;
  `codexapp` `MethodThreadCompactStart` / `MethodReviewStart`). Claude announces its commands (`available_commands_update`)
  and runs them itself; `codex app-server` does neither — the TUI's slash commands are TUI features — so "/compact" used to
  reach the model as a sentence and the composer had no menu and no Compact button. Now the runtime announces `compact` and
  `review` once per process (same wire shape and session-state kind as Claude's, `SetHandlers` beside the model picker and
  connectors) and a prompt that is **exactly** `/name [text]` — one text block, nothing attached, name in `codexCommands` —
  runs through its RPC instead of `turn/start`. `see /compact`, `/etc/hosts`, an unknown `/x`, or a command with an
  attachment are ordinary prompts. Adding `/goal`, `/rename`, `/fork` later is one `codexCommands` entry + its `prepare`.
  - `/compact` → `thread/compact/start`; takes no text (the RPC has no instructions field: `/compact foo` is refused with
    `ErrInvalidPrompt`, not sent to the model). `/review [base <branch> | commit <sha> | instructions]` → `review/start`,
    `delivery: "inline"`; exactly two words `base X` / `commit <hex>` name a target, anything else is the person's own
    instructions (so "base on the style guide" is not a branch), a branch outside `[A-Za-z0-9._/@~^-]` is refused because
    Codex puts the name into a prompt whose model runs it in a shell. Nothing → `uncommittedChanges`.
  - **Busy is refused by DevKit, because Codex does not**: `thread/compact/start` and `review/start` on a thread with a
    running turn *interrupt that turn* and start theirs (measured). `beginTurnLocked` answers `ErrSessionBusy` first, shared
    with `Prompt`; the Compact button is hidden while the session runs (`compactOffered`, `slashCommands.ts`).
  - A command is a turn like any other: `r.turn` holds the thread, Stop → `turn/interrupt`, an app-server refusal of the
    request (bad thread, unreadable params) is `ErrTurnFailed` with Codex's sentence and an idle session, not
    `ErrProcessFailed` (`callRefused`). `thread/compact/start` answers `{}` without a turn id, so the turn is `adopt`ed from
    the next `turn/started` on the thread (`onTurnStarted`); no `turn/started` within `codexAdoptWait` (15 s) is an error,
    not a hang. After a compaction the translator's last token report is republished (`republishUsage`).
  - Transcript: `contextCompaction` → the **same card as Claude's** (`tool_call` name `compact`, kind `think`, title
    "Compact conversation"); `enteredReviewMode` → a `review` card ("Review: current changes"); `exitedReviewMode` → nothing
    (its text is the agent message that follows, measured identical). Before, all three drew the generic unknown-item card.
  - **Workspace context** (`Service.Invoke`): `slashCommandOf` (exactly one text block whose first word is `/name`, not a
    path) means the block is **not** prepended — it would turn "/compact" into a sentence for either vendor (Claude Code reads a
    command from the start of the turn, so this was very likely a latent bug there whenever the block was due: first turn,
    a change, every 20th turn — read from the code, not run against the real Claude) — and a successful `/compact` forgets what the session was sent (`forgetWorkspaceContextLocked`), so the next
    real prompt carries the block again. A failed compaction changes nothing. Not done: auto-compaction (either vendor)
    is not seen by `Service`, so the 20-turn refresh stays the only cover for it.
  - **Measured with the real `codex app-server` 0.163.0**, fake HOME, scripted mock Responses API (raw JSON-RPC probe; the
    DevKit path end to end is `cmd/pdfe2e/codex_commands_test.go`, run with `DEVKIT_SUBAGENT_E2E_CODEX=<binary>`):
    | | `thread/compact/start {threadId}` | `review/start {threadId, target, delivery}` |
    | --- | --- | --- |
    | answer | `{}` in ~50 ms, no turn id | `{turn, reviewThreadId}`; `reviewThreadId` = the thread itself for `inline` |
    | stream | `turn/started` (new turn) → `item/started contextCompaction` → `thread/tokenUsage/updated` → `item/completed` → `turn/completed completed` | `item/started+completed enteredReviewMode {review:"current changes"}` → `turn/started` of a *different inner turn* → userMessage (Codex's own review prompt) → `exitedReviewMode {review:<rendered findings>}` → agentMessage (same text) → `turn/completed` |
    | what ends it | `turn/completed` of the turn named by `turn/started` | `turn/completed` of the turn **in the answer**; the inner turn never completes and reads `interrupted` in `thread/read` |
    | model traffic | one `/responses` request ending in `compaction_trigger` (remote compaction v2); the reply must hold exactly one `compaction` output item or the turn fails ("expected exactly one compaction output item") | one request with a fresh context (developer + environment + Codex's review prompt), not the conversation; the result is appended to the thread afterwards |
    | tokens | `total` unchanged, `last.totalTokens` = ~7000 with 0 input (the new window's estimate; the context meter reads `last`) | an ordinary usage update |
    | busy thread | **interrupts the running turn** (it ends `interrupted`) and compacts | **interrupts the running turn** and reviews |
    | during it | `turn/start` fails `ActiveTurnNotSteerable { turn_kind: Compact }`; a second `thread/compact/start` interrupts the first | — |
    | fresh thread | works, no model request, `last` ≈ 7000 | works |
    | errors | `-32600 invalid thread id` for a bad id | same; `-32600` unknown target variant; `detached` delivery refused ("paginated threads do not support detached review") and deprecated |
    | notifications | **no `thread/compacted`** — the deprecated `ContextCompactedNotification` is not sent; the item is the signal | no `review/*` notification; raw model deltas are **not** streamed in review mode |
    `ReviewTarget`: `{type:"uncommittedChanges"}`, `{type:"baseBranch", branch}`, `{type:"commit", sha, title?}`, `{type:"custom",
    instructions}`. A branch or sha that does not exist is **not** validated by Codex (the review prompt just names it).
    Structured findings (`findings[]`, `overall_*`) render as "<explanation>\n\nReview comment:\n\n- [P1] title — path:lines\n  body".
    Not verified: a real model's review quality or token counts (mock only), 0.147 (the version named in older comments;
    only 0.162.1/0.163.0 were available), a ChatGPT account's remote feature flags.
  - **Deliberately not done**: `thread/fork`, `thread/revert`, `thread/goal/*`, `thread/name/set`, `thread/shellCommand`,
    `thread/inject_items` and `mcpServerStatus/list` have RPCs but no command — none was measured here and each needs its own
    decision (goal and name overlap Agent Hub's own goal/title; fork/revert overlap rewind). `delivery: "detached"` is
    refused by the app-server and deprecated. Codex's own `/model`, `/approvals` and the like are DevKit's pickers.
- **opencode is the third vendor, and the first with no pinned version** (`adapter_opencode.go`, `agentruntime.OpenCodeCLI`,
  `Agent.Unpinned`; measured against opencode V2 2.0.32 with a scripted model, not a real one). Research and plan:
  the conversation that added it; upstream state: `anomalyco/opencode` issues labelled `acp`.
  - **No pin.** `Install(ctx, "opencode", "")` asks npm for `@opencode/cli`'s latest tag (`Manager.resolveLatest`, stable
    versions only, `<NPMRegistry>/@opencode%2Fcli/latest`), installs it and activates it only if `OpenCodeAdapter.Probe` passes
    under the jail (initialize, and the agent can reopen a session). `Status.TestedVersion` is empty, so the UI says
    "Install latest" / "Update to X" and never "untested"; `Previous` is the rollback. DevKit knows what the agent can do from
    what it says (initialize, config options, `-32601` answers), never from a version number — Claude and Codex are meant to
    move to the same rule. `adapter_opencode_live_test.go` is the conformance check to run against a new release
    (`DEVKIT_OPENCODE_COMMAND=<native binary> go test ./internal/agenthub/ -run OpenCodeLive -v`; the binary is
    `node_modules/@opencode/cli-<platform>/bin/opencode`; `DEVKIT_OPENCODE_INSTALL_LIVE=1 … -run OpenCodeInstallLive` installs from npm).
  - **`opencode acp`, one process per session.** opencode registers a session's MCP servers per *directory* on its private
    server, so sessions sharing a process (or a directory) see each other's tools (upstream #40978). The process serves a private
    HTTP API on loopback (`AllowLoopbackListeners`) and ends when stdin closes. It reuses the generic ACP runtime
    (`claudeRuntime`, now carrying `vendor`) and wraps it in `openCodeRuntime`.
  - **Its home is DevKit's, like Codex's**: `<data>/agent-homes/opencode`, handed over as `XDG_{CONFIG,DATA,STATE,CACHE}_HOME`
    (opencode has no variable of its own). The user's `~/.config/opencode` and sign-in are never read. `config/opencode/` is not
    reset (a person may add providers there) but is read-only to the jail, as is `prompts/`. DevKit's settings ride on
    `OPENCODE_CONFIG_CONTENT`, which measurably beats every file: `permissions` (shell and edit **ask**), `update: disable`,
    `share: disabled`, and `instructions` (the DevKit prompt, a content-named file in `prompts/`). A private-home session gets
    `HOME=<home>/home`: opencode resolves `~/.claude` and `~/.agents` when it opens a session and, where the jail hides a real
    `~/.claude`, `realPath` fails and every `session/new` with it. `OPENCODE_DISABLE_CLAUDE_CODE=1`. XDG_CONFIG_HOME moves gh's and
    glab's login, so a shared home sets `GH_CONFIG_DIR`/`GLAB_CONFIG_DIR` back. Sign-in is `opencode acp --login` in a terminal
    tab (`AuthProvider`, the Claude path), run with the same XDG roots.
  - **Permission modes are DevKit's** (`ask`, `auto`, `bypassPermissions`, the ids Codex uses so a child inherits across vendors).
    opencode's own `mode` is an agent (build/plan) and `plan` does **not** stop writes (measured), so that option is dropped from
    the settings and `SetMode` refuses it. opencode is told to ask and `permissionBeforeAsk` answers: Full auto allows everything,
    Auto allows edits and puts commands to the person, Ask puts everything to the person. The person is never offered "always":
    opencode keeps that rule for the whole project, past the session (a later session ran the command unasked), so the option is
    removed from the card and an `always` answer becomes `once`. A refusal ends the turn (`stopReason: cancelled`). MCP tools are not in the
    ask list: DevKit's own gate asks after dispatch.
  - **What opencode does not have, and what DevKit does about it** (each is an interface the runtime/adapter does not implement
    or an error it answers, so nothing errors): steering mid-turn (`Steer` → `ErrUnsupported` → peer messages queue; the PR is
    upstream #54394) and goals; forking at a message (no `RuntimeForker`: a rewind restores files only); subagent profiles
    (`subagentProblem`: skipped with a reason); skills/MCP overrides and the environment report ("unavailable"); compaction cards
    (`session.compaction` is not declared, so opencode sends a plain marker). Unknown methods answer `-32601`.
  - **DevKit's tools are called through opencode's Code Mode**: its system prompt lists MCP tools as
    `tools.devkit.git_status()` (dots → underscores) and they only run inside the `execute` tool; past ~20 tools the catalog is
    cut to ~12 KB and the rest is found with `search` (`opencodeNaming`). It registers a session's MCP servers asynchronously: a
    prompt sent right after `session/new` was built without them (5 of 5), 100 ms after the server answered `tools/list` it had them
    (upstream #53299). `openCodeMCPSettle` (1.5 s) holds the first prompt; waiting for the MCP host to see `tools/list` would be exact.
  - **The jail must let opencode read the directories above the project** (`sandbox.Policy.ReadAncestors`, Seatbelt only): to open
    a session it resolves every ancestor of the project (and `<ancestor>/.claude`, `.agents`, `.opencode`, `opencode.json…`), and a
    stat-only grant on the ancestors made `session/new` fail with `EPERM: lstat '/Users'` — **for every project under /Users**, which
    no test caught because they all used a temp dir under /var (`TestOpenCodeLiveProjectUnderHome` is the one that does). Measured by
    editing the generated profile: `file-read-data` on the `/Users` literal is what it needs (`file-read-metadata`/`-xattr` are not
    enough), so ancestors are listable (names, never contents) for opencode only. A **private home** inside the home folder cannot
    be fixed that way — stat-ing the hidden `~/.claude` needs read access to that directory too, i.e. listing the home — so
    `openCodeNeedsSharedHome` refuses it up front with the reason ("turn on Share home folder in Settings → Agents → Safety, or open a
    project outside your home"). Linux/bwrap: not measured.
  - Catalog: `ConfigCatalog` reads `model`/`effort` from a scratch session and **deletes it** (opencode persists a session when it is
    created); the efforts of the current model only, since every model switch reloads the agent. The scratch directory is not
    resolved through symlinks (`/private/var/…` makes opencode resolve `/private`, which the jail does not show it).
  - Not verified: a real model, `auth_required` as opencode really answers it, a subagent that calls tools (child-session
    updates are declared via `_meta["opencode/child-session-updates"]`, untested), `/compact` with a model that obeys its template,
    bwrap on Linux, Windows, and V1 (ACP is there too; only V2 was run).
- **A person's turn has no time limit; an agent's or terminal's does.** `Service.Invoke` applies
  `DefaultInvokeTimeout` (10 min) only when the caller is not the UI or a remote human
  (`invokeUnbounded`). It used to cut every UI turn at 10:00 exactly, which looked like the vendor
  stopping mid-task (no turn row, idle session, no message). A turn that does hit the limit now
  leaves a `timeout` session failure ("The turn stopped at its time limit").
- **A turn the vendor refuses is not a crash.** Codex's `usageLimitExceeded` →
  `ErrAgentUsageLimit` (`agent_usage_limit`); a full context window, a spent budget or
  any other failed turn → `ErrTurnFailed` (`agent_turn_failed`). Both carry Codex's own
  sentence (`VendorError.Message` — the only place that says when a limit lifts), leave
  the session `idle` rather than `error`, and are also reported as a session failure
  (`codexTurnFailure`), the banner Claude's air `sessionFailure` fills. The banner lasts
  one turn: the next prompt retires it (`transcript.ts`). `ErrProcessFailed` is for a
  process that actually died.

- **The prompt DevKit appends is rendered per vendor and per session**
  (`internal/agenthub/devkit_prompt.go`). `{{tool.name}}` placeholders become the names the
  model actually sees — `mcp__devkit__git_commit` for Claude, DevKit's own name plus a
  "your host may reformat it" note for Codex — and `TestPromptNamesOnlyRealTools` fails if
  one names no MCP tool. Normal CLI, search/LSP and skill workflows are allowed;
  CodeGraph and saving API requests are optional. Git writes still use DevKit's
  guarded tools; saved-connection checks still apply. `Service.SessionBrief`
  (`app.AgentSessionBrief`) puts the session's workspace folders and which it may write
  in the prompt instead of asking the agent to go and look.
- **Workspace instructions are read each turn, sent when they change.** Create/Edit workspace
  stores `instructions` locally (migration 34, UTF-8, max 32 KiB) together with its name and
  folders. `Service` reads them by the session's persisted `WorkspaceID` before invoking the
  runtime and builds a context block (instructions, memory index, session brief). The block is
  prepended to the prompt only when `workspaceContextDue` says the agent needs it: it has
  never received one, its fingerprint (SHA-256 of the text) differs from the last delivered
  one, or `workspaceContextRefreshTurns` (20) unchanged turns have passed (a vendor that
  compacts its conversation may have dropped it). A failed turn records nothing, so the next
  one carries the block again. The record is in memory and forgotten wherever the process
  holding the conversation is replaced: close, dead-runtime eviction, resume, rewind. Claude
  and Codex receive the same content; the user echo stays unchanged. Empty/unbound context
  retires earlier workspace instructions; DevKit's rules retain precedence. User-authored
  instructions are never passed through the tool-name placeholder renderer. The per-vendor
  prompt (`devkit_prompt.go`) tells the agent that a request without a block means the last
  one still holds.
- **Shared workspace memory is local and versioned** (`internal/workspacememory`,
  migration 35). `workspace.memory.*` tools scope Hub agents by their persisted
  workspace ID; terminal callers must supply an explicit ID. Update/delete/restore
  require `expectedRevision`; CAS and immutable history snapshots commit atomically.
  Source attribution comes from host identity, never tool input. The context block
  carries an index of at most 8 KiB (`Service.Context`): one `- id [kind] title — summary`
  line per entry (summary clipped to 140 runes, whitespace folded to one line, no bodies, no
  revisions), as reference data rather than instructions. The agent reads an entry with
  `workspace.memory.get`. The render is deterministic because agenthub fingerprints it;
  an empty workspace renders as "".
  Soft-deleted entries remain readable/restorable; deleting the workspace cascades
  entries and history. Plaintext SQLite storage is outside vault/sync, like workspace
  instructions. Audit excludes memory text and query. Workspace menu → Workspace memory
  opens the lazy manager: bounded summary pages, CAS edits and paged revision metadata
  with bodies loaded on selection. Migration 36 adds scoped contentless SQLite FTS5
  (`unicode61 remove_diacritics 2`) and materialized summary read models. Search is literal
  word-prefix AND, not substring; MCP list/search accept filter-bound cursors (legacy
  offset remains supported). `BenchmarkMemory50K` covers rare/common search, deep cursor
  pages and large revision histories. Broad searches still scale with matching postings.
- **Claude's PreToolUse hook refuses `git commit` / `git push` in Bash**
  (`cmd/devkit-mcp/hook.go`, `shellGitWrite`) with a reason naming the DevKit tool to call:
  the shell skips branch guards, the task trailer and the push rule. Other git commands
  are left alone. It reads plain spellings (`-C`, env prefixes, chains), not `sh -c`.

- **Sessions message each other without blocking** (`internal/agenthub/peer.go`, MCP `agenthub.sendMessage` /
  `agenthub.listPeers`; spec `docs/superpowers/specs/2026-10-16-agent-hub-peer-messages-design.md`). The answer is at
  once `delivery` = `started` (idle: an ordinary `invoke` turn), `steered` (running: Claude `_session/steering` / Codex
  `turn/steer`, the `TurnSteerer` interface), `queued` (in-memory; drained when a turn ends) or `refused` + `code`.
  Reach = own workspace, sub-sessions, parents; another workspace is `not_found` unless the person approves the exact
  text (the tool asks, `CrossWorkspaceApproved` is never a wire field); a closed session is never woken. The row is a
  `user_message_chunk` with `source` (`MessageSource`, stored payload + `_devkit.source`), drawn by `PeerMessage` and
  never as the person's bubble; the model gets an envelope (`peerEnvelope`) saying it is not the person's, framed by
  lines carrying the message id. Loop/storm guards: hop count (replies count even without `replyTo`; the person's
  prompt resets it; cap 6), 5/min per pair, 20/min per target. The window mirrors its own prompt queue to Go
  (`AgentHubSetPromptQueue`) so the person's prompts go first and a pause is honoured. `TestNewContainerGrantsTheMessagingTools`
  checks the real grants. Audit: `OnPeerMessage` (never the text). Not durable across a restart.

- **The window learns of a session someone else created** (`Service.OnSessionCreated` → `agenthub:sessionCreated`, frontend
  `adoptCreatedSession` in `AgentSessionsSubscriber`). The session list is read once at start; before this a child started
  by `agenthub.createAgentSession` (or from the terminal) never reached the rail and its tab said "Session not available"
  until the app restarted. The event carries only the id; the window reads the row (`AgentHubGetStatus`) and upserts it.
  Sessions the window creates itself (UI / remote human callers) do not fire it.

**Not in server mode**: `main.go` registers `agenthub.ServerModeAdapter` stubs there, and
every session fails with `ErrUnavailableInServerMode` — the jail shares the host loopback
and server mode's bridge has no authentication, so a session could approve itself.

```bash
go test ./internal/acp/ ./internal/agenthub/ ./internal/bridge/ ./internal/mcp/ -count=1
# Codex in the real jail, signed out (no quota): DEVKIT_CODEX_COMMAND is the native
# binary, e.g. <data>/runtimes/codex/0.147.0/node_modules/@openai/codex-<platform>/vendor/<triple>/bin/codex
DEVKIT_CODEX_COMMAND=/abs/path/to/codex go test ./internal/agenthub/ -run CodexRealBinary -v
```

### Agent customization and recovery
- **Settings → Agents is its own nav group of six one-topic pages**, not one tab
  (`frontend/src/modules/settings/settingsNavigation.ts` is the model, `AgentsPages.tsx` the pages):
  Runtimes (`agents-runtimes`, + Codex observed sessions), Models & subagents (`agents-models`),
  Skills & MCP (`agents-skills`, the tools/skills/MCP report and `AgentCustomization` overrides,
  `AgentEnvironmentPanel`), Safety (`agents-safety`, sandbox + file checkpoints), Notifications & remote
  (`agents-notifications`, + Telegram) and DevKit as MCP (`agents-mcp`). Runtimes and Skills & MCP carry a shadcn
  `Tabs` Claude | Codex | opencode switch whose last choice is shared and kept in `localStorage`
  (`agentVendorChoice.ts`, guarded); the other pages are vendor-neutral. The old ids `agents`, `remote` and `mcp`
  stay valid for callers (`requestSettings("agents")`): `resolveSettingsTab` maps them to Runtimes, Notifications &
  remote and DevKit as MCP. A new agent setting is a panel on the page whose topic it is, plus keywords on that
  page's `SETTINGS_GROUPS` entry — not a new stacked section.
- Settings → Agents → Tools, skills and MCP manages local imports and per-vendor
  overrides (`preferences.AgentCustomization`, in the existing sandbox preference
  JSON). Dedicated UI-only get/set bridge methods use revision/CAS. General sandbox
  saves preserve this map; they cannot overwrite it from a stale Settings form.
- Imported skill folders are read-only links, never copied or deleted at source.
  Codex uses native skill config (both canonical file and folder forms for runtime
  compatibility); Claude uses a temporary local plugin cleaned up with its process.
  MCP definitions support stdio and HTTP, with environment variable names only.
- Claude's inherited toggles deny skill/MCP tool invocation, not MCP startup.
  Vendor source deny rules remain authoritative. Local source updates are applied
  by Refresh + Reload; no remote installer/update command runs automatically.
- Resume/Reload is UI-only and reuses the persisted native conversation ID without
  replaying a prompt. Reload reserves the session against Invoke and resume while
  closing its idle runtime. A missing native conversation stays an explicit error.
  Startup recovery adds a durable interruption notice; model/effort/mode are restored
  before attaching live handlers. User-requested Retry turn remains separate.

### Prompt attachments (`internal/agenthub/attachment*.go`)
DevKit reads an attached file itself and sends its contents (limit 8 MiB). Text, Markdown and HTML go
as they are; images as image blocks; **`.docx` and `.xlsx` have their text extracted**
(`attachment_docs.go`, `archive/zip` + `encoding/xml`) and ride as a `text/plain` resource, capped at 1 MiB
of text with the cut said aloud. Old binary `.doc`/`.xls` are refused by name with "save it as
.docx/.xlsx". **PDFs are not read into the prompt at all** (next section). Plain text only for Office files: no
layout, images, comments, formulas (the cached result is read) or headers/footnotes; a spreadsheet date is
its serial number. A malformed file panicking a reader is a refused attachment, not a crash.

### PDF documents (`internal/pdfdoc`, `internal/agenthub/documents*.go`, `internal/mcp/tools_pdf.go`)
A PDF attached to a prompt is kept as a **document of the session** and read by the agent through
`pdf.*` MCP tools; the prompt carries only what it is and how to read it. Why not native: ACP's `resource`
can carry a `blob`, but claude-agent-acp 0.78.0 ignores blob resources (`promptToClaude`: "Ignore blob
resources (unsupported)") and Codex has no PDF input at all, so a PDF sent natively would vanish without an
error. Re-check when bumping either runtime.

- **Engine** (`internal/pdfdoc`, a leaf): PDFium compiled to WebAssembly under wazero via
  `klippa-app/go-pdfium` — pure Go, no cgo, nothing to install, one binary on every platform (+13 MB on the
  server-mode binary). The module gets **no file system and no network** (go-pdfium's default mounts `/`;
  `FSConfig` is overridden) and receives the document as bytes. First start compiles the module (~3.5 s),
  cached in `<data>/pdf-engine` so later launches take ~90 ms. Concurrency 2; analysis and renders are cached
  (LRU). Limits, each reported and never applied by quietly doing less: 100 MiB / 3000 pages per file, 20
  documents / 500 MiB per session, 25 pages per text read, 4 images (≤ 6 MiB total) per view, dpi 50–300,
  longest edge ≤ 2400 px, ~48 KiB of text per result (pages that do not fit are named), search covers every
  page within a time budget and returns `resumeFromPage` if it ran out.
- **Page chrome**: a web page printed to PDF carries the site around it. A line that repeats across pages
  in the same place **and** sits in the top/bottom 9 % band or in a column beside the body is chrome
  (`chrome.go`); a table header that repeats mid-page is not. Chrome is left out of `readText`/`search` by
  default, counted and sampled in the result, listed in `pdf.info`, and `includeChrome:true` keeps it. It is a
  heuristic and says so.
- **Scans**: no OCR (a tesseract/wasm engine plus language data was judged not worth the package weight; a
  model reads the page image better). `info` reports `textLayer` full/partial/none and the pages without one;
  `readText` says "No text layer" for them; `search` lists `pagesWithoutText`; `viewPages` shows them.
- **Store**: `agent_documents` (migration 40) + `<data>/agent-documents/<session>/<docN>.pdf` (0600, atomic
  write). Handle `docN` is per session and never reused; same bytes twice in a session = one document.
  `Library.Open` re-hashes the stored copy. Rows go in `DeleteSessions`' transaction, files in
  `Service.DeleteSession`, and `Sweep` at start-up removes folders of gone sessions and files with no row. The
  directory is in `dataDirSecrets`: an agent cannot reach it by path, only through the tools. Plain files in
  the data directory, like the transcript: outside the vault and sync.
- **Import** happens in `Service.Invoke` before the turn is recorded (`importDocuments`): an unreadable,
  password-protected or over-limit PDF refuses the prompt with its name and reason. An agent caller's path is
  held to its project (`resolveAttachmentPath`, symlinks resolved). The workspace context block gains a
  "PDF documents attached to this session" section, so a resumed or compacted conversation is told again.
- **Tools** (`pdf.list|info|search|readText|viewPages`, read-only, registered only when a store is wired).
  The caller's identity *is* its session (the per-session socket): an agent reads only its own documents and
  naming another session gets the same "No such document" as a missing one; a terminal caller must pass
  `sessionId`. They go through the gateway as `builtin.agenthub agenthub:read session:<id>`; audit metadata
  carries a handle, never a file name or query. They are **pre-approved** (`MCPEndpoint.PreApprovedTools` →
  Claude `allowedTools`, Codex MCP elicitation accepted): measured, Claude denies every call otherwise.
  Every result that quotes a document says it is data, not instructions.
- **Images reach the model differently per vendor — measured with the real runtimes, not read off the code**:
  Claude Code passes an MCP `image` block through untouched (byte-identical PNG), **but gives the model
  `structuredContent` instead of `content` when a result has both**, so `readText` returns `content` only.
  Codex 0.147 runs MCP tools from JavaScript inside its `exec` tool (`await tools.mcp__devkit__pdf_viewPages(…)`)
  and an image reaches the model **only if the script calls `image(result.content[i])`**; `codexNaming`
  (`devkit_prompt.go`) and the PDF guide say so, and the e2e test has a control showing an unforwarded image
  never arrives. Page pictures are dropped from the *stored* transcript row (`transcript_slim.go`; the live
  event keeps them for Claude; DevKit's Codex translator already reduces them to a byte count).
- **Verification**: `go test ./internal/pdfdoc ./internal/mcp ./internal/agenthub` (fixtures made by
  reportlab/pypdf in `internal/pdfdoc/testdata`: web-print with sidebar/header/footer and a table over pages
  2–4, scan, scan+text, AES-encrypted, damaged). `cmd/pdfe2e` runs the **real** DevKit Service, MCP socket and
  proxy, bubblewrap sandbox, claude-agent-acp + Claude Code and `codex app-server`, with only the model's HTTP
  API mocked so every request the runtime sends can be read back. Skipped unless
  `DEVKIT_PDF_E2E_CLAUDE=<dir with npm install @agentclientprotocol/claude-agent-acp@0.78.0>` and
  `DEVKIT_PDF_E2E_CODEX=<dir with npm install @openai/codex@0.147.0>` are set;
  `DEVKIT_PDF_E2E_SAMPLE=<a long real PDF>` adds the real-document tests.
  `go test ./cmd/pdfe2e/ -v -timeout 20m`.

### Agent sandbox (`internal/sandbox`)
**Implemented** leaf package for OS-isolated subprocesses. Linux: **bubblewrap**
(`apt install bubblewrap`). macOS: Seatbelt / `sandbox-exec`. Landlock helper
exists but is **not** wired into bwrap Start/StartPiped.

```bash
go test ./internal/sandbox/ ./internal/arch/ -count=1
# meaningful smoke when bwrap is installed:
go test ./internal/sandbox/ -run 'TestSmokeAgentShapedProcess|TestBwrapCanWriteWorkDirUnderHome|TestStartPiped' -v
```

- `Start` — capture-and-wait helpers (buffered stdout/stderr).
- `StartPiped` — ACP / interactive stdio (Claude adapter path).
- Fail closed if `bwrap` missing / unsupported GOOS (`ErrUnavailable` / `ErrUnsupportedPlatform`).
- Private home default: tmpfs on home + forced `HOME`/`TMPDIR`; WorkDir under `$HOME` is re-bound after tmpfs.
- Default denies host `~/.ssh`, `~/.gnupg`, `~/.aws`, `~/.config/gcloud`; symlink binds into deny → `ErrInvalidPolicy`.
- **Agent Hub asks for more by default**: the real `$HOME` (`ShareHome`) and the Docker
  socket found via DOCKER_HOST / current docker context / OrbStack / Docker Desktop
  (`AllowDocker`, `agenthub.DetectDocker`). DevKit's data dir (except `runtimes/`) is always
  hidden (`agenthub.ProtectDataDir`). Settings → Agents → Sandbox turns either off.
- **Environment inspection**: Settings → Agents and the session menu expose a
  UI-only report (`agenthub/environment.go`, `AgentHubEnvironment`). The live
  report retains launch env/grants. Codex reports skills/MCP through bounded
  metadata calls; Claude discovery is configuration-only. Never label file
  discovery as runtime verification or include config values, env secrets or
  skill bodies in the report/audit. Shared-home PATH completion runs after
  vendor/session overrides and adds existing common toolchain bins.
- **Vendor customization is available again**: Claude uses `user`/`project`/`local`
  setting sources. Shared-home Codex sessions import portable user MCP/settings and
  global instructions, link user skills/plugins read-only, and keep DevKit's own
  login/state. `skills/` is persistent and writable for system/managed installs;
  native features are not forcibly disabled. Shared-home sessions inherit the
  host process's CLI environment; Codex provider credentials remain selected by
  DevKit. Codex defaults to Auto, and both adapters allow loopback dev servers.
- Product docs: `syurodev/base` → `/dev-kit/sandbox` (ADR-009).

### Linux desktop (GTK4 / WebKitGTK)
The shell was written macOS-first and now branches in exactly two places; nothing else
in the frontend knows what OS it is on (`frontend/src/lib/platform.ts` is the only
platform module).

- **Black window on first run.** WebKitGTK's DMABUF renderer fails silently on hosts
  that cannot share the buffer (VMs, containers, some Wayland/driver combinations):
  the page loads and the DOM is live in the Web Inspector, but nothing paints. It looks
  total rather than merely wrong because `.native` leaves html/body transparent, so what
  shows through is the window's near-black `BackgroundColour`. `native_webview_linux.go`
  now sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` before GTK comes up unless the variable is
  already in the environment — export it yourself (any value) to take the decision back.
- **Frameless window.** GTK would otherwise stack its own decoration above DevKit's 40px
  title bar. `Frameless` is on for Linux only (`framelessWindow`); Wails' JS runtime keeps
  drag (`--wails-draggable`) and re-implements edge resize for this case. The frontend
  draws minimise/maximise/close itself (`components/ui/window-controls.tsx`) because a
  frameless window otherwise has none, and `Titlebar` drops the 84px macOS traffic-light
  reservation everywhere except macOS. Wails skips `gtk_window_set_title` on a frameless
  window, so `applyWindowTitle` puts WM_NAME back by hand.
- **Scroll boxes in an inert shell panel stop scrolling** (Linux app with overlay scrollbars
  only: `lib/platform.quietsScrollBoxes` marks `<html data-quiet-scroll-boxes>`). WebKitGTK
  drops a clipped scroller's layer and rebuilds it in the frames that reveal it, so while a
  column or the bottom row is inert, boxes that opt in with `data-scroll-box` (and
  `.cm-scroller`) are `overflow: hidden` (`workbench/slots/slots.css`). Give a new scroll box
  inside a pane body `data-scroll-box`.
- **Verifying the GUI headlessly.** `Xvfb :99 -screen 0 1400x900x24` + `DISPLAY=:99
  ./bin/dev-kit`, then `import -window root shot.png` and drive it with `xdotool`. Needs
  `dbus-x11` on PATH (single-instance uses `dbus-launch`). A bare Xvfb has no window
  manager, so maximise/minimise do nothing there — close and the layout are testable.

### CodeGraph: workspace of folders (not one project)
CodeGraph works on the **workspace**; a folder is neither a service nor "the" project. Design:
`docs/design/codegraph-workspace.md` (identity, scope, lifecycle, API, limits).

- **Identity** is `workspaceId + folderId + normalizedRelPath` (`workspace/identity.ts`:
  `docKey`, `flowKey`, `treeFileId`). `folderId` is `workspace_services.id`; re-adding a path gives a
  new id, so a draft never adopts a folder that merely has the same path. Paths are never case-folded.
  The same relative path in two folders is two buffers, two tabs, two cache entries.
- **Primary is read from `workspaces.root`**, never copied (`primaryId` is derived in `loadFolders`).
  `WorkspaceSetPrimary` only moves where agents are spawned: no reindex, no tab/draft/history reset.
  Nothing in CodeGraph keys off it. There is no `switchProject`: change workspace
  (title bar), change primary, change discovery scope (header picker), focus a document and
  add/remove folders (workspace management) are five different actions.
- **Go resolves folder→path from the workspace's membership**; the frontend never sends a path to
  read or write a file. `internal/app/workspacecode.go` is the seam; `internal/codegraph/rootfiles.go`
  does root-scoped I/O (`ReadFileAt`/`WriteFileAt`/`StatFileAt`/`ListFilesAt`/`GitInfoAt`) with no
  "active project". Reads of entry points / outline / symbols come from the on-disk index
  (`Registry.EntryPointsOf`/`OutlineOf`/`SymbolsOf`) and never open a Service.
- **Save is a revision check + atomic rename.** Revision = SHA-256 prefix of the bytes; a save sends
  the revision the buffer is based on and gets `file_conflict` if the disk moved. Saves of one path are
  serialised in-process; a foreign process writing inside the check→rename window can still be
  overwritten (detection, not isolation). `bufferModel.ts` is the pure state machine (base / disk
  revision / external / ack / orphan); "Keep mine" quiets the banner, Save stays guarded.
- **Find in workspace** is `internal/wssearch` (RE2, `git ls-files` or walk, bounded ordered worker
  pool, UTF-16 columns, cancel by request id). Unsaved buffers are not searched, and the UI says so.
- **Open documents are watched** by `internal/docwatch` (parent directories of open tabs and dirty
  buffers only, debounced). Missed events are covered by a reconcile on window focus.
- **Reindex** goes through `Registry`'s bounded single-worker queue; `WorkspaceReindex(folderIds,
  cancel)` scopes it. One folder failing never stops the batch; results of a previous workspace are
  dropped by generation.
- **Nested folders have one owner per file** (the deepest in scope): search and the Files tree leave a nested
  folder's subtree out of the outer one (`wssearch.nestedSubtrees`, `withoutNested`). The same physical file can
  still be opened through two identities; they are two buffers that conflict rather than overwrite. Policy in
  `docs/design/codegraph-workspace.md` § 3.1.
- **Folder order in the UI never depends on the primary** (`displayOrder`), and Back/Forward cross files *and*
  flows within one editor group.
- **Drafts live in memory only.** They survive closing a tab, switching workspace and removing a folder
  (orphaned: Copy / Save a copy / Discard); they do **not** survive a restart or crash. Do not describe them as
  crash recovery (design doc § 10).
- **Every `Workspace*` bridge method must be granted to the UI**: `TestEveryWorkspaceBridgeMethodIsGrantedToTheUI`
  calls them all against the real default grants, and `moduleViewsParity.test.ts` compares the manifest's *commands* and
  views with `internal/app/app.go` (a drift disables the whole module with "Module metadata is unavailable").
- **Verifying in a browser**: `task run:server`, then drive it with Playwright; fixtures are
  `scripts/codegraph-fixture.mjs` (representative workspace), `scripts/codegraph-bench-fixture.mjs` (10k files) and
  `scripts/codegraph-index-fixture.mjs` (real Java/TypeScript analyzer timing). Server mode has no Agent Hub.
- **Markdown and PDF files open in `FileDocument` by their name** (`modules/codegraph/viewers/viewerKind.ts`;
  tab identity is unchanged, so agent references, Back/Forward and the palette reach them for free).
  Markdown stays a text buffer with a **View | Edit** switch (plus a text-editor toggle and, for a jump to a line,
  `source`). View is `MarkdownPreview`: it renders `marked`'s tokens through the agent transcript's parser
  (`agentMarkdown.parseMarkdown`) — no HTML string is built, raw HTML shows as text, images are never fetched, links go
  to the browser, a heading of the file or a file of the same folder (`markdownLinks.ts`). Edit is Plate, the same
  editor as Notes (`MarkdownFileEditor`, `markdownEditorKit.ts`) — **but the file is never written from Plate's
  tree**: Plate re-pads tables, turns hard-wrapped paragraphs into `\`-hard-breaks and rewrites list markers, which
  measured 0/84 of this repo's `.md` files round-tripping. `markdownSplice.ts` applies only what the person changed to
  the original text, block by block (median 2 changed lines over this repo's docs; 6 of 68 fall back to Plate's own
  formatting and the editor says so before saving). A file whose letters/digits Plate would drop or change (HTML it
  cannot read, reference-link definitions, entities — 16 of 84 here, `losesText`) is edited as text instead. Front
  matter is set aside and put back (`markdownFrontMatter.ts`). The buffer stays the source of truth (`editFile`, Save,
  conflicts); `markdownNorm` must call `serialize({value})`, not `setValue` (11× slower). A PDF has **no buffer**
  (`openFile`/`ensureBuffer` skip `isPdfPath`): `PdfDocument` asks Go for `WorkspacePdfInfo` / `Page` / `Search`
  (`internal/app/pdfview.go`, the same `pdfdoc` engine the agent's `pdf.*` tools use — pages are PNG pictures, no
  new frontend library, nothing of the file runs in the webview). Go reads the bytes through `codegraph.ReadBinaryAt`
  (folder-resolved, symlink-safe, 100 MiB) and keeps ≤ 4 documents / 256 MiB, validated by size+mtime; every call
  names the content `revision` from `Info`, so a replaced file answers `pdf_changed` and the view reopens. Layout,
  zoom and which pages are near the viewport are pure (`pdfLayout.ts`); an effect there must depend on ids, never on
  the `owner` object from `useDocumentOwnership` (new every render — it once re-read the file in a loop). Not done:
  highlighting a search hit on the page, selecting/copying PDF text, password-protected PDFs, images in Markdown,
  `#anchor` into another file, Markdown headings in the Outline.
- **Cross-folder intelligence is deliberately thin.** Definitions/references/outline come from the
  index of the folder that owns the file; nothing matches names across folders and text search is never
  presented as references. HTTP/Kafka/Redis links stay in `internal/crossservice`.
- Tests: `go test ./internal/wssearch/ ./internal/docwatch/ ./internal/codegraph/ ./internal/app/
  ./internal/bridge/ ./internal/arch/ -count=1`; frontend `bun test src/modules/codegraph`
  (`fileActions.test.ts`, `workspaceSync.test.ts`, `searchActions.test.ts` use a fake bridge).
  Benchmarks: `go test ./internal/wssearch/ -run xxx -bench WorkspaceSearch` and
  `go test ./internal/codegraph/ -run xxx -bench ExplorerFolder`.

### CodeGraph analyzer toolchain
Three languages, three shapes, and `Toolchain` is the thing they differ in.

- **Java** — a jar built from `analyzer/` (or shipped in the package) and launched by a JDK
  the user must have. `Ready` means artifact AND runtime; when either is missing CodeGraph
  shows a setup panel (`internal/codegraph/toolchain.go`).
- **Lua** — `Native`: the analyzer runs in this process (`internal/lua` +
  `analyzer_lua*.go`), so it is ready the moment DevKit starts with nothing installed, on
  every platform including AppImage and MSIX.
- **TypeScript** — `Serve` plus `Run[0] == ArtifactArg`: the artifact IS the runtime.
  `tsgo` is the TypeScript compiler rewritten in Go, so it cross-compiles beside DevKit as
  a self-contained binary and needs nothing installed to RUN. Go is a requirement to BUILD
  it, the way Maven is for Java.

- `Toolchain` is per language and is the extension point for the next analyzer: a value
  with its own requirements, build command and artifact path — or with `Native` set, which
  means the analyzer runs in this process and every build field is unused. A native
  toolchain reports `Ready` without probing anything, and `BuildAnalyzer` refuses it
  (`ErrAnalyzerIsBuiltIn`) rather than reporting a build that never happened.
- `Serve` is for an analyzer that is not a one-shot subprocess. It is handed a `launch`
  closure that resolves argv and environment at call time, and returns the `Analyzer`.
  `Run[0]` naming `ArtifactArg` instead of a requirement means the artifact launches
  itself, so `runtimeMet` is true without probing anything.
- **Service keeps one analyzer per language and closes them on shutdown**
  (`analyzerCloser`). It matters only for TypeScript, and it matters a lot: a language
  server rebuilt on every open is cold on every open, and one left running after the app
  quits is a process the user cannot find.
- **Which analyzer runs is a per-project answer.** `OpenProject` detects the language, then
  takes that language's analyzer out of the registry — a Lua project is never handed the
  Java one. `SetAnalyzer` is an override for an owner who knows better (every test double
  in the package); `NewSubprocessAnalyzer()`'s placeholder does not count as one, which is
  what keeps the tests that pass it from pinning the service to nothing. `main.go` wires
  no analyzer at all now, only the registry.
- The UI reports a missing analyzer **per folder**, not for the whole module
  (`frontend/src/modules/codegraph/workspace/indexHealth.ts`, the Files root's dot and the
  header's index summary): a folder whose language has no ready analyzer says so with the build
  action, and every other folder keeps working. The old whole-canvas gate (`analyzerGate.ts`) is gone.
- A local Java jar older than `analyzer/pom.xml` or `analyzer/src/main` is stale:
  the setup panel offers a rebuild instead of analyzing with outdated semantics.
  Real-jar Go tests fail with the Maven rebuild command when a stale jar exists;
  they still skip when no jar was built. Bundled/external artifacts are unaffected.
- **Discovery is not `PATH`** (`internal/codegraph/locate.go`). Plenty of Java developers
  have IntelliJ and nothing else: the IDE ships its own JetBrains Runtime, bundles a full
  Maven under `plugins/maven/lib/maven3`, and downloads project JDKs into `~/.jdks` —
  none of which reach `PATH`. The probe searches `JAVA_HOME`/`MAVEN_HOME`, `PATH`,
  `~/.jdks`, SDKMAN, JetBrains Toolbox and app bundles, and the system JDK directories,
  then picks the NEWEST version that clears the minimum. The build runs the resolved
  paths with `JAVA_HOME` set and both `bin` directories prepended to `PATH`.
- A `mvnw` in `analyzer/` would be preferred over everything else, which would drop the
  Maven requirement entirely. There is no wrapper today; `mavenCandidates` already looks
  for one.
- **The jar ships inside the package.** `common:build:analyzer` builds it and the
  packaging tasks depend on it, so a released app never shows the setup panel — that is
  the from-source path. Each platform puts it where `installedResourcePaths`
  (`internal/codegraph/artifact.go`) looks, and the two must be changed together:

  | Package | Jar and `tsgo` land at | Declared in |
  | --- | --- | --- |
  | deb / rpm / arch | `/usr/local/share/dev-kit/` | `build/linux/nfpm/nfpm.yaml` |
  | macOS `.app` | `Contents/Resources/` | `build/darwin/Taskfile.yml` |
  | Windows NSIS | beside the `.exe` | `build/windows/nsis/project.nsi` |

  `common:build:analyzer:ts` builds `tsgo` the same way, and the packaging tasks depend on
  both. On macOS the universal package builds it for each architecture and lipos them
  (`build:analyzer:ts:universal`): a universal `.app` with a single-architecture analyzer
  analyzes nothing on half the Macs it installs on, and says so only when somebody opens a
  TypeScript project.

  **AppImage and MSIX do not carry it.** `wails3 generate appimage` and `wails3 tool msix`
  build their payload from the binary alone and expose no hook for extra files, so those
  two fall back to the setup panel. Fixing that means either replacing those tasks with a
  hand-rolled AppDir/package step or a change upstream in Wails — neither is done here;
  noted alongside the JRE gap in syurodev/dev-kit#28.
- `task dev` and `task build` deliberately do NOT build the analyzer: a contributor
  without a JDK can still work on everything else and gets the setup panel, which is what
  it is for. Only the packaging targets require the toolchain. Build one by hand with
  `task build:analyzer:ts` (TypeScript) or `task build:analyzer` (Java) — thin root-level
  wrappers over the `common:` tasks above, for opening a real project in `task dev`/
  `task run:server` without going through packaging.
- **A JRE is still required to RUN the analyzer, on every path.** Shipping the jar removes
  Maven and the build, not Java: `Toolchain.Run` is `java -jar …`. `Ready` therefore means
  artifact AND runtime, and `HasArtifact` separates the two failures so the panel can say
  "build this" or "install a runtime" rather than offering a build that would not help.
  Removing the last requirement means bundling a JRE per platform — measured at 57 MB with
  `jlink --add-modules java.se` (a trimmed module set saves only 2 MB and breaks
  `ReflectionTypeSolver` for target projects using `java.sql`, `java.net.http` and the
  rest, so it would have to be the full one). Not done — tracked in
  syurodev/dev-kit#28, which carries the measurements and the work items.
- **Two Java versions, deliberately different.** `analyzer/pom.xml` sets
  `maven.compiler.release` to 21 — the bytecode the analyzer itself is compiled to, so it
  runs on the JDK people actually have (an IDE's bundled runtime, a distro's
  `openjdk-21`). `ParserConfiguration.LanguageLevel` in `ProjectAnalyzer.java` is
  `JAVA_25` — the newest syntax it can READ in a target project. They are independent:
  JavaParser is a parser, not javac, so reading Java 25 needs no JDK 25. When a newer Java
  ships, raise the language level, never the release. A file whose syntax the parser
  rejects is swallowed by `parseFile`'s catch-all and vanishes from the index with no node,
  no edge and no error — `parsesSyntaxNewerThanTheAnalyzerOwnBytecode` guards that.
- **Parsing runs on several threads** (`ProjectAnalyzer.parseAll`): a fixed pool of
  `min(cores, 8)` platform threads, one `JavaParser` per thread, results gathered in
  submission order so the index is byte-identical to a single-threaded run (tests compare
  1/2/4/8 threads). Never use `StaticJavaParser` from a worker: its configuration is
  thread-local, so a worker parses at the default language level and every record/pattern
  file silently vanishes (`workerThreadsParseWithTheNewestLanguageLevel` has teeth).
  `-Ddevkit.analyzer.parseThreads=1` restores the old behaviour (reach the jar's JVM through
  `JAVA_TOOL_OPTIONS`; `Toolchain.Run` is a fixed argv). Only parsing is parallel: every later
  pass shares the symbol solver's caches, which are not thread-safe.
- **Preparing for Java 25.** Nothing in the analyzer needs 25, and the floor stays 21 because
  people run it on an IDE's JDK or a distro's `openjdk-21`; Locate already picks the NEWEST JDK
  that clears the floor, so a machine with 25 runs on 25. Measured on a synthetic 1500-file
  project (4 cores, JDK 21; 21k methods): 55s cold, of which `collectDeclarations` ≈ 38%
  (almost all `addMethodNode`'s `resolve()` + `getSignature()` per method), `parseFile` ≈ 34%,
  `addCallEdges` ≈ 16% (mostly `resolve()`), GC ≈ 8%, peak heap ≈ 1 GB. Parallel parsing took
  parse 5.6s → 2.1s and the whole run to ≈ 46s. What that says about 25:
  - **Virtual threads do not help** — indexing is CPU-bound, and they have been final since 21.
    A virtual-thread pool measured the same as 4 platform threads (it runs on the same cores).
  - **The real next win is the solver passes**, not the JDK: `addMethodNode` and `addCallEdges`
    are ≈ 54% together and are shared-solver work. Options, in order of risk: resolve a
    signature only when a parameter type needs qualifying (most do not); audit
    `JavaParserFacade`'s caches and parallelise per source root; one `JavaSymbolSolver` per
    thread (N× the type-solver memory under a 2g heap). Verify any of them the way parsing was:
    byte-identical output on the big project (`cmp`) before and after.
  - **Possible 25-only gain, unmeasured**: `-XX:+UseCompactObjectHeaders` (product in 25) should
    cut the memory of millions of tiny AST nodes. It does not exist on 21 (the JVM refuses to
    start), so it would be added together with `-XX:+IgnoreUnrecognizedVMOptions` in
    `JavaToolchain.Run` — and only after measuring on a real project with JDK 25. JDK 25 was not
    available when this was written.
  - **Raising the floor to 25 means changing together**: `maven.compiler.release` in
    `analyzer/pom.xml`; `javaRequirement().MinVersion` and `javaInstallCommands()` in
    `internal/codegraph/toolchain_java.go` (plus its doc comment); the precondition message in
    `build/Taskfile.yml`; the comment in `.github/workflows/build-prod.yml` (CI already builds on
    JDK 25); this file. After that a machine with only JDK 21 gets the setup panel — decide that
    on purpose. Add a test that the jar is `class file version 69`.
- The build is unsandboxed by design: it is the developer's own repository, built with
  their own toolchain, and Maven needs the network and `~/.m2`.
- End-to-end (real Maven, minutes, network):
  `DEVKIT_ANALYZER_BUILD_E2E=1 go test ./internal/codegraph/ -run BuildsForReal -v -timeout 20m`

### CodeGraph: the Lua analyzer
`internal/lua` is a hand-written Lua parser (lexer + recursive descent). It reads 5.1
through 5.4 plus the LuaJIT literals, because one Kong tree holds all of it: the plugin
runs on LuaJIT (5.1, `1LL`), the tooling beside it is plain 5.4 (`//`, bitwise, `<const>`).
It never returns an error — a statement it cannot read becomes a `Diagnostic` and the parse
resynchronises at the next statement boundary, so a file being edited keeps every function
around the broken one. (The Java analyzer swallows an unparseable file whole; that is the
failure this deliberately does not copy.)

`internal/codegraph/analyzer_lua*.go` turns that tree into an `AnalysisResult`:

- `analyzer_lua.go` — file discovery, the require-path index, module nodes, doc comments.
- `analyzer_lua_declare.go` — what each file declares and which of it is reachable. Lua has
  no export keyword, so all six ways of putting a function on a module table are read
  (`function M.f`, `function M:f`, `M.f = function`, `local M = { f = ... }`,
  `return { f = ... }`, a bare global).
- `analyzer_lua_resolve.go` — call resolution. **There are no types, so resolution is by
  provenance**: a value is followable when it traces back to a `require` of a file in this
  project, the table the module returns, or a method's `self`. Everything else is a call
  site with a reason and no target — `library` (stdlib, `ngx`, `kong`, a require with no
  file here) or `unresolved` (a value the analyzer lost). A wrong edge is worse than a
  missing one. A constructor (`new`, `create`, `extend`, …) carries its module through;
  an ordinary method does not, because `db:query()` returns data, not an instance.
- `analyzer_lua_kong.go` — everything about what a Lua project is FOR. Entry points are
  Kong plugin phases (`ACCESS · plugin-name`, since a plugin serves no route of its own),
  Kong Admin API route tables (`["/path"] = { GET = ... }`) and Lapis `app:get` routes.
  Layers follow: endpoints → `controller`, `resty.redis`/`kong.db`/a `dao`-ish name →
  `repository`, and what a controller requires → `service` unless it already said otherwise.

Module ids are require paths (`kong/plugins/x/handler.lua` → `kong.plugins.x.handler`,
`foo/init.lua` → `foo`, a leading `lua/` or `src/` stripped). A require that names nothing
exactly falls back to a **unique** suffix match, which is what makes
`require "kong.plugins.mine.util"` find `util.lua` in a plugin repository checked out at
its own root; an ambiguous suffix resolves to nothing rather than to a guess.

`analyzer_lua_e2e_test.go` is the one that matters when changing any of this: it drives a
Lua index through the flow walker, blast radius, go-to-definition, find-references, the
data-flow trace and the snippet reader — all written against the Java analyzer's id and
label contract, all of which a second analyzer can break without failing a single one of
its own tests.

```bash
go test ./internal/lua/ ./internal/codegraph/ -count=1
```

### CodeGraph: the TypeScript analyzer
A language server, driven. `internal/lsp` is the client — not an editor client: no
diagnostics, no completion, no document sync beyond opening a file. An indexer opens
everything once and asks structural questions, tens of thousands of them, so calls are
matched by id and safe to make concurrently.

TypeScript cannot be in-process like Lua: `microsoft/typescript-go` keeps its parser,
binder and checker under `internal/`, so there is no library to import — only the `tsgo`
binary, which `analyzer-ts/` (a Go module of its own) pins and builds.

`internal/codegraph/analyzer_ts*.go`:

- `analyzer_ts.go` — the analyzer. Owns the long-lived connection, walks the project,
  orchestrates the LSP, assembles the `AnalysisResult`.
- `analyzer_ts_model.go` — the language-level model: files, declarations, decorators,
  imports, and **call expressions** (receiver, name, arguments as written).
  **It knows nothing about any framework.** Calls are here because a call whose meaning
  is in its ARGUMENTS is a language-level fact: the checker resolves
  `app.get('/users', h)` and `client.send({ cmd: 'x' })` to library methods and is right
  to, but the route and the pattern are in the source either way.
- `analyzer_ts_read.go` / `analyzer_ts_scan.go` — what the server does not say. tsgo
  returns an empty `detail`, so signatures come from the source, and no LSP request
  exposes decorators. Bounded text work: the server has already said where each
  declaration starts and where its name is, so **a decorator is whatever sits between
  those two points** and a parameter list is whatever is inside the parentheses after the
  name. Nothing searches for a declaration.
- `analyzer_ts_framework.go` — **the seam**. A framework is a reading of the model, not a
  fork of it: five methods (`Name`, `Detect`, `Endpoints`, `Layer`, `Edges`). Adding one
  is a new file plus a line in `tsFrameworks`; it may not touch the model or the analyzer.
  If it needs something the model does not carry, the model grows a *language-level* fact,
  never a framework-shaped one.
- `analyzer_ts_nest.go` — NestJS, read almost exactly the way the Java analyzer reads
  Spring, because Nest copied the model. One place reads more than a decorator: Nest marks
  every provider `@Injectable`, so the decorator alone cannot tell a service from a
  repository — what the class injects can.
  Nest's entry points are not only HTTP: `@MessagePattern`/`@EventPattern` (every
  transport — TCP, Redis, NATS, Kafka, RabbitMQ, MQTT), gRPC, `@SubscribeMessage`,
  GraphQL resolvers, and `@Cron`/`@Interval` — the last of which are gated on nothing,
  because a scheduled job lives on a provider, never a controller.
  **`client.send(pattern)` is linked to the `@MessagePattern(pattern)` that serves it**,
  as a `call` edge, so a flow walks across the service boundary the way a reader does.
  The checker cannot produce that edge and is right not to: what the call reaches is
  decided at run time by a broker, and the only thing connecting the two is the pattern,
  written on both sides. Patterns are canonicalised (object keys sorted) so a caller and
  a handler written by different people still meet; an ambiguous pattern resolves to
  nothing rather than to one of two handlers.
- `analyzer_ts_next.go` — Next.js App Router. It exists to prove the seam: an interface
  with one implementation is a guess about what varies. Next shares nothing with Nest — no
  decorators, no classes, no injection; a route is a DIRECTORY and a handler is an exported
  function whose name is the HTTP method.
- `analyzer_ts_router.go` — the Express family (Express, Fastify, Hono, Koa's router,
  Restify). The third way a route is declared: by CALLING a method at run time, so the
  only trace in the source is the call. One reading covers all of them. The path is the
  guard — every one of `get`/`post`/`delete` is a method on something else too, and a
  route is the only one whose first argument starts with `/`. tsgo reports an inline
  handler as a function symbol of its own, so a flow starts at the handler rather than at
  the file; a named handler resolves like any other identifier. It does NOT follow
  `app.use('/api', subrouter)`: mounting moves every route beneath it, and reporting one
  at the wrong path is worse than reporting it at its own.

Two things this analyzer does that the others cannot:

- **There is no `unresolved` call site.** The checker resolved the call or the code does
  not compile. A target outside the project is `library`, same as everywhere else.
- **Module resolution is the compiler's.** `tsconfig` paths, `baseUrl`, package `exports`,
  extension inference, NodeNext's `.js` meaning `.ts`, barrel re-exports — asked by running
  `textDocument/definition` on each import specifier rather than reimplemented and got
  silently wrong.

**The process is long-lived and that is the design.** A cold index of 722 files is ~1.35s,
almost all of it building the program; a re-index is ~320ms, because `sync` tells the
server only about files whose content hash changed. The file watcher re-indexes on every
save, so a one-shot subprocess would pay the cold cost every time.

Tests skip unless the analyzer has been built (`task build:analyzer:ts`, or directly:
`cd analyzer-ts && go build -o bin/tsgo github.com/microsoft/typescript-go/cmd/tsgo`):

```bash
task build:analyzer:ts
go test ./internal/lsp/ ./internal/codegraph/ -count=1
```

### CodeGraph: entry points
What the sidebar lists is `codegraph.EntryPoint` (`internal/codegraph/entrypoints.go`), not
"endpoints": where a flow starts because something outside the code calls in. `Kind` says
what calls in — `http`, `graphql`, `rpc`, `message`, `socket`, `schedule`, `plugin` (a Kong
phase) — `Verb`/`Target` are the chip and what it answers to (`GET /users`,
`ACCESS rate-limit`, `CRON 0 * * * *`), and `Service`/`Owner`/`Group`/`Order` place it in
the list. The UI groups kind → service → owner and does no guessing of its own.

- **An analyzer sets what it knows; `completeEntryPoints` fills the rest on every read**
  (`Service.ListEntryPoints`): kind from the verb, owner/group from a Java node id, the Java
  service from the package segment after the shared prefix. So an index written before
  these fields existed reads the same as a new one. The store moved `endpoints` into
  `entry_points` in place (rows copied, not dropped — a reopen serves the stored index).
- **Kong phases are `plugin` entries under their plugin, `Order`ed as Kong runs them**; a
  plugin's Admin API is `http` under the same plugin name.
- **Java HTTP keeps its wire format** (`endpoints`, `httpMethod`, `path`):
  `analyzer_subprocess.go` translates it, so older jars still read. Camel adds `entryPoints`
  with `kind=message` for Kafka (`camel` when the source component is unresolved), route/topic/group
  source expressions, and `entryPointSupport`/`entryPointWarnings`. A synthetic method per
  source route template owns runtime callback edges; `configure()` does not call the handler.
  `direct:`/`seda:` sources are internal flow nodes, not external entries. Rebuild the Java jar
  before running `go test ./internal/codegraph/ -run TestCamel -count=1`: this test invokes the
  real analyzer on `analyzer/src/test/resources/camel-project`, persists SQLite, and traverses
  Go flows. No Kafka/application process is needed. Details: `docs/design/codegraph-camel.md`.
- **Kept stable on purpose**: the MCP tool names (`codegraph.listEndpoints`, `getApiFlow`'s
  `endpointId` input — agents' allowlists name them), the entity-ref kind `"endpoint"`
  (agents and saved transcripts resolve it), and the API Client's `EndpointDraft` (an HTTP
  concept). Only `http` entries offer "Try in API Client". Tool-result cards read both the
  new (`verb`/`target`, `entryPointId`) and the old field names, since transcripts outlive builds.

### CodeGraph: guards on call edges
`Edge.Guard` is the condition a call runs under, read from the Java source around it
(`analyzer/.../Guards.java`): `if`/`else`, `? :`, `switch` entries, the right side of `&&`/`||`,
a `catch` body (`catch <Type>`, so a call made only when that exception was thrown says so) and the
guard clause (`if (!enabled) return;` guards what follows). Loops and the `try` block itself are not
guards. **Syntactic, never evaluated** — a guard names a flag, not its value. One edge per
`(from, to, kind)`: one unconditional call makes the edge unconditional, otherwise guards are joined
with `||` (`Guards.merge`). `edges.guard` is a migrated column (`migrateEdgeGuard`); old indexes read
as unguarded until a re-index, which a new DevKit build triggers anyway. `FlowStep` carries `Depth`
(the flat DFS list is a tree) and `Guard`, so the `catch` shape also reaches MCP `getApiFlow` output.
`call_sites` does not carry it; Lua and TypeScript do not set it. Design:
`docs/dev-kit/codegraph.mdx` § Guard trên cạnh gọi.

### CodeGraph: flow outline and steps
A flow tab has two modes, **Outline | Graph** (shadcn `Tabs`, Outline first; spec
`docs/superpowers/specs/2026-10-15-codegraph-flow-outline-design.md`, its Addendum lists what changed in the build).
- **Steps are call sites with more columns** (`call_sites.col/text/callee_key/receiver/result_use/frames/target/role`,
  `migrateCallSiteSteps`). Java fills them (`Steps.java`); Lua and TS do not yet and read as a flat outline. A
  library call with neither text nor callee key (what Lua/TS record) is hidden as `unknown call` unless an
  extractor effect lands on it. `frames` are language-neutral (`if, else, case, catch, finally, try, loop,
  closure, and, or, ternary, await`). A guard clause is a frame only for the calls inside it.
- **`Legacy`** (re-index prompt) is decided per `GetSteps` request: some asked method has non-override sites,
  none of them has text, and the detected language is `java`. The frontend asks in chunks (≤ 200 ids) and ORs
  `legacy` across them (`bridge/codegraph.ts`).
- **Positions are the call's NAME**: a method call's `line:col` is where its name starts (`a.b()` points at `b`),
  and call-level effect extractors record that same line and column. A closure's frame id is the `line:col` of
  the call it is passed to. Effect tables (`kafka_accesses`, `redis_accesses`, `outbound_calls`, table accesses)
  gained `col` and join to a call site by `(from, line, col)`; a col-0 effect (old index, declaration-level)
  attaches by line only, to the first library call on it, and only in a method none of whose sites has a column.
  **For agents**: after a re-index, the line reported for a method call — call sites, Kafka/Redis/table/outbound
  rows — is the line of the call's name, so for a multi-line chain the lines from references/definitions,
  def-use and `codegraph.find*Usage` move to the line the name is on, not the line the statement starts on.
- **`target` vs `to`**: a call into a body-less project method (a repository or client interface) has `to` = ""
  (a wall for flow walks) and `target` = the project node it names; `Step.Target` carries it so the outline can
  show its effect. `role = "override"` rows are dispatch links (overridden method → override), never returned as
  steps, only `Step.Impls`; `references.go` reads `role` and keeps the old line rule as a fallback.
- **Visibility is decided at read time** (`callimportance.go`, pure `Visibility`; `steps.go` supplies the facts).
  Rule order: your override → project call (a helper is hidden; else a body-less one — `to` empty, `target` set —
  with an extractor effect shows the effect label; else "project") → wraps a project call (closure) → extractor
  effect at the call → no text and no key (`unknown call`, hidden) → decides a branch → infrastructure table (a
  utility call discarded on a `field` falls through) → "writes to …" (receiver param/field/local-from-param, and
  only when the result is discarded) → common (≥ 8 calling methods and ≥ 15 % of the index's methods,
  `CallStats`) → side effect (result discarded) / accessor (only when the result use is known) / value. Every
  answer has a reason the outline shows.
- **Helper** = a call into a project method that has no overrides, whose call does not pass a closure holding a
  project call that is overridden, body-less or not itself a helper, and that is loaded (≤ 3 levels below the
  asked methods, ≤ 2000 methods in all), writes no field, has no extractor effect and no visible own step.
  Memoised by `(id, depth)`; a cycle (reaches the depth limit), an unloaded method or a body-less project call is
  never a helper.
- **`CallStats`** is cached on the `sqliteStore` instance and dropped by `ReplaceIndex`; a read that overlaps a
  `ReplaceIndex` (the generation moved while it ran) is returned but not kept.
- **Overrides are local preferences** (`codegraph_call_visibility`, `PreferencesGet/SetCallVisibility`, UI only,
  all projects). `preferences.Service.SetCallVisibility` trims the key and requires ≤ 300 bytes,
  `<type>#<member>` with non-blank text on both sides of the first `#` and no control characters (`Type#*` is
  member `*`, the whole type); mode is `show|hide`, `""` clears. The 500-entry cap refuses only adding a new key
  (re-setting or clearing at the cap works). Writes are serialised by a mutex on the Service.
- `CodeGraphGetSteps(nodeIds ≤ 200, servicePath)` asks the same Service as `CodeGraphGetNeighbors`.
- **Frontend** (pure, `bun test src/modules/codegraph`): `outline/buildOutline.ts` builds rows with opaque keys:
  a step's id is `(calleeKey || to || target || text)#n` (n = occurrence of that base in its method), a row's key
  is `parent + "\u0001" + "<kind>:" + id` (kinds call/fold/frame/impl/more, root `"root"`), so a re-fetch keeps
  expansion and selection. Frame keys use the frame's `line:col` (with a `~n` suffix when repeated) and are **not**
  stable across a re-index that moves code. Hidden steps fold into a `fold` row naming their top reasons.
  `outline/outlineNav.ts` is the keyboard/tree-role model; `OutlineView.tsx` draws it with the shared
  `TreeGuides` and row geometry (`components/tree-guides.tsx`, also the sidebar's). Motion classes
  (`outline-row-enter`, `outline-row-flash`, `flow-mode-enter`) are off under `prefers-reduced-motion`.
  `graph/graphView.ts` hides helpers and drops only the cards that hid disconnected.
- **Arrowhead labels are an explicit `FlowCanvas` prop** (`labelAt?: "arrow"`), passed only by `FlowDocument`;
  the plan and database canvases leave it off and keep midpoint labels. With it, `ConnectorEdge` moves a label to
  the arrowhead (as its number alone, the rest in the tooltip) only when the label is numbered (`^\d+( · |$)`,
  `arrowLabel`) and the edge enters its card from the left; anything else stays at the midpoint.

### CodeGraph: configuration
Java only. The analyzer records what config files SET (`ConfigKey`: `ConfigFiles.java`, YAML via
SnakeYAML `compose()` — never constructed, alias-bomb capped — plus `.properties`) and what the code
USES (`ConfigUsage`: `ConfigExtractor.java`). Two tables, `config_keys` and `config_usages`, joined
at read time on `CanonicalConfigKey` (letters and digits only, lower-cased — Spring's relaxed
binding), so an env var, a kebab key and a camelCase key are one setting.
- **Secrets never enter the index as values**: a key whose separator-stripped name contains
  `password|secret|token|apikey|…` is stored `redacted` with no value (its env var names are kept).
  `FindConfigUsage` and every other reader inherit that; do not add a path that returns raw file text.
- `Service.FindConfigUsage` (MCP `codegraph.findConfigUsage`): per use, readers (`field_accesses`, or
  a `@ConfigurationProperties` field's getter/record accessor), gated methods (conditions), calls whose
  `Edge.Guard` names the field; then a bounded reverse walk over call edges to the entry points.
  It reads source, never evaluates: "what depends on the flag", not "is it on".
- Known gaps, all noted in the answer or in `docs/dev-kit/codegraph.mdx` § Cấu hình là một loại node:
  Lombok/implicit record accessors are unresolvable so `properties` readers can be missing;
  `@ConfigurationProperties` on a `@Bean` method is not read; Lua and TypeScript record no config.

### CodeGraph: database tables
Java only, same shape as configuration: the analyzer records `EntityTable` (`@Entity`/`@Table`, Spring
Boot's snake_case default, secondary/join/collection tables) and `TableAccess` (`TableExtractor.java`,
SQL read by `SqlTables.java`), two tables `entity_tables` / `table_accesses`, joined with the call graph at
read time by `Service.FindTableUsage` (MCP `codegraph.findTableUsage`).
- **SQL is read by tokens, not parsed**: FROM/JOIN/USING read, UPDATE/INSERT INTO/DELETE/MERGE/TRUNCATE write.
  Anything that looks like a clause but is not (`EXTRACT(.. FROM ..)`, `FOR UPDATE`, CTE names, string
  literals) has a test in `SqlTablesTest`; add one before adding a rule.
- **Inherited Spring Data CRUD has no node and no edge** — `repo.save(x)` is recorded at the CALL
  (`repository-crud`), recognised by the receiver's type from the solver. A method the repository
  declares is attributed to that method, and its callers come from the call edges, never twice.
- **A name that is also one of an entity's tables asks about that table** (`FindTableUsage`): with the
  default naming class, entity and table are one word. An entity name that differs brings all its tables.
- Not read: `@NamedQuery`, `EntityManager.persist/merge`, tables named by a variable, triggers/cascades/views,
  Liquibase. Gaps are in `docs/dev-kit/codegraph.mdx` § Truy vấn nối với bảng DB.

### CodeGraph: calls between services
`internal/crossservice` joins what each service's index says — the routes it serves, the HTTP calls it
makes (`OutboundCall`, table `outbound_calls`) — into edges, **when asked**; nothing stores them.
`codegraph.ServiceSnapshot` is what one service offers the join: from a resident service, or read from
its index on disk with no Service opened (`Registry.Snapshot`; no watcher, no resident slot).
- Callers: Java (`OutboundCallExtractor`: Feign, Spring HTTP interfaces, RestTemplate, WebClient,
  RestClient) and Lua (`analyzer_lua_http.go`: `resty.http`, recognised by provenance of
  `require "resty.http"`, never by a method name). A `foldString` hole is `{expr}`, the shape Java uses.
- `workspace.ResolveTarget` resolves a host against the CALLER's config. With no host (a plugin's
  `{conf.url}`, an HTTP interface) the route alone links it, as `path-unique`, only when it has ≥ 2
  literal segments and exactly one other service serves it; several is `ambiguous-path`. Never pick one.
- An unreadable service is named, not skipped (`not-indexed`), and every edge carries how old the
  other end's index is. A service nothing watches can be wrong without saying so.
- Layering: `crossservice` imports `codegraph` and `workspace`; `codegraph` still does not import
  `workspace` (arch test). The MCP tool resolves "the asker's workspace" from `workspace.list`'s logic.

### CodeGraph: shared Redis keys
Same shape as the HTTP join: each service records `RedisAccess` (table `redis_accesses`; Java
`RedisExtractor`, Lua `recordRedis`), `crossservice.RunRedis` joins at read time (MCP
`codegraph.findRedisKeyUsage`). A key is a PATTERN (`fsap:access:{org}:{api}`); two are one resource
when some string matches both (`overlaps`, a DP over `*`), and a pattern needs ≥ 3 fixed leading
characters to count — `*` and `{key}` overlap everything, so they are dropped and a query by one is refused.
- Redis is recognised by the receiver (an `opsFor*()` chain, a declared template/client/command type,
  Lua provenance of `require "resty.redis"`), never by a method name like `get`.
- Not seen: keys built elsewhere and passed in, `@Cacheable`, Lua `EVAL`, pub/sub, Redisson. Say so in
  the answer's notes instead of widening the match.

### CodeGraph: Kafka topics
Same shape as Redis (table `kafka_accesses`; Java `KafkaExtractor`, Lua `recordKafka`;
`crossservice.RunKafka`, MCP `codegraph.findKafkaTopicUsage`), with two differences:
- **The topic name is folded against the config of the service that WROTE it** (`Service.Config.Resolve`),
  never against another's — two services spell one topic by different properties. A property the
  service's config does not set is `unresolved`, listed, not dropped.
- **Topics have a direction**, so the answer carries `gap` (`no-consumer` / `no-producer`). It is a gap in
  THIS workspace only; say so (the notes do) instead of calling the topic dead.
- Kafka is recognised by annotation, constructor or declared receiver type, never by a method name. Kafka's
  own `Producer`/`Consumer` interfaces are NOT in the type sets on purpose: a project's own `Producer.send`
  would become a topic. Not seen: Spring Cloud Stream bindings, `@SendTo`, `sendDefault`, `@RetryableTopic`.

### CodeGraph: opening a project
`Service.OpenProject` does three things before any analyzer runs, each because the version
without it was worse.

- **Detection gates the subprocess** (`internal/codegraph/detect.go`) and now also picks the
  analyzer. Without it, opening any folder started a JVM to walk it and reported the empty
  result as an empty project rather than as the wrong folder. A `Toolchain` declares
  `Markers` (build files) and `SourceExt`; a directory holding a marker is a module root —
  for Lua that includes `handler.lua`, so a repository of Kong plugins reads as one module
  per plugin. First toolchain to claim the folder wins, and Java is ahead of Lua because
  its markers are build files while Lua's fallback is "any folder holding a .lua".
  An unrecognised folder (Terraform, Helm, manifests…) still opens, **for its files only**
  (`Service.openForBrowsing`): no analyzer and no watcher run, the tree/editor/git status
  work, `IndexInfo.Unsupported` is true, and graph queries and `WaitForIndexReady` answer
  `ErrUnsupportedProject` (bridge code `unsupported_project`). That is a state, not a
  failure: the UI labels it "Not supported" (neutral tone), hides the Entry points tab,
  and never shows an error card or a retry for it; its messages name no language. Detection is skipped when no
  toolchain is registered — an empty registry knows nothing and must not refuse everything.
- **A folder of microservices is one index, not none.** `ProjectAnalyzer` discovers every
  `src/main/java` below the root (skipping `target/`, `node_modules/`, dot-dirs; depth 6),
  falling back to the root itself for a bare folder of `.java` files. All roots go into one
  `CombinedTypeSolver`, which is what keeps a call from one service into a shared module
  resolvable — one subprocess per service would lose that. `filePath` is relative to the
  project root, so roots never collide.
- **A known project reuses its stored index, then re-indexes in the background.** The index
  is persisted per project (`ProjectDBPath`); re-deriving it on every open cost as much as
  the first index with the UI blocked throughout. Reuse requires BOTH `HasIndex` and
  `IndexedProject == this project` — the per-project file is a naming convention, and a
  convention is not something the code can check, so the index records which project it
  describes (`index_meta`). An index written before that table simply re-indexes once.

### Credentials, work and the repository (`accounts`, `tasks`, `git`, `apiclient`)
Four modules added together; the design lives in `docs/dev-kit/{accounts,tasks,git,api-client}.mdx`
and the cross-module rules in `docs/dev-kit/cross-module.mdx`.

```bash
go test ./internal/accounts/ ./internal/tasks/ ./internal/git/ ./internal/apiclient/ ./internal/gitparse/ ./internal/notify/ -count=1
```

- **`internal/accounts` keeps two stores behind one service.** A *shared*
  account is a vault item and syncs; a *scoped* one (a single connection's
  password) goes to the local `account_secrets` table and does not. That is not
  a policy choice — `db_query_history` already wrote down why: everything in
  `vault_items` is sync-bound by construction, so a credential that must stay
  on one machine cannot live there. It is still sealed with the DEK. The link
  from a profile is `scope_owner`, deliberately **not** a stored `account_id`:
  an id pointing at a row that does not sync dangles on the next device, and
  writing a replacement back makes two devices overwrite each other forever.
- **No method anywhere returns a credential to an agent.** `AccountsAPI` has
  two read-only methods and no Resolve; `accounts:reveal` is granted to the
  accounts subject only. Modules resolve inside Go at the point of use.
- **`internal/git` is not `internal/checkpoint`.** checkpoint overrides
  `$GIT_DIR/info/attributes` to disable the project's `.gitattributes` so bytes
  round-trip untouched — right for a snapshot, silently wrong for a commit
  (`* text=auto`, `*.bin filter=lfs`). Never commit the user's work through it.
  What is shared is `Locate` and `internal/gitparse`.
- **Push is gated, commit is not — unless a `git-guard` says so.** Push leaves
  the machine; commit is local and reversible. A guard binds an env tag to a
  branch *pattern*, so it also covers branches that do not exist yet. There is
  no force push: no flag, no parameter, no code path.
- **An agent may push only to a branch recorded on a task it holds**
  (`task.branch`). A list living in the session dies with it; the task does not.
- **Closing a task is the one gate.** A board may declare a `verify` command;
  DevKit runs it and the exit code decides. An agent's own claim that tests
  passed is a sentence with nothing behind it — DevKit cannot see the tools it
  ran. A red result refuses and does **not** fall back to asking. Changing a
  board (workflow, verify, binding) is `tasks:admin` and UI-only, so an agent
  can trigger the command and never choose it. An agent may *create* a board
  (`tasks.board.create`, `tasks:write` `board:new`): it gets no verify command
  — its gate is the person's confirmation — and is bound only into one of the
  session's own workspaces (`tasks.Service.CreateBoardFor`). Runs are background, one per project (they share a work tree),
  queued durably in `task_verify_runs`; `Recover` puts an interrupted run back
  to `queued` and never infers a result, and a request older than 24h goes
  stale rather than grading code it was never made against.
- **Every rule in `tasks` reads a status's `category`, never its name** — there
  is a test with a board whose terminal column is called "Đã ship".
- **Auth in `apiclient` is an account binding, not a string.** Rotation is a
  one-place edit and Copy as cURL emits `$DEVKIT_TOKEN`. The redirect policy
  compares the whole **origin**: comparing hostnames passed under httptest,
  where both servers are 127.0.0.1, and the test caught the credential
  following a cross-service redirect. A blocked redirect comes back as the 3xx
  itself (`redirectBlocked` names where it pointed), not as an error.
- **API client variables are Postman's three scopes**: globals (`api-globals`,
  one item; two devices' copies merge and fold back into one on the next edit)
  < collection, root folder first (both halves in the `api-collection` sealed
  payload — its metadata keeps the old shape, because an older DevKit validates
  metadata field by field and would refuse the whole pull) < environment.
  `api-globals` is a new item type, so an older DevKit on another device stops
  pulling once one exists, like every item type added before it. Secrets are write-only everywhere: an update names the
  secrets to KEEP (`VarsUpdate.KeepSecrets`) and only new values cross the
  bridge. Send still requires an environment — its tag is the gate — even when
  every variable comes from elsewhere.
- **A request is edited, not just sent.** `RequestInput` (no timestamps) is what
  the bridge takes; query/headers/form are ordered `KVList` rows with `disabled`
  (a stored map still decodes), `:name` path segments are `pathVars` on the
  request, and `bodyMode` sets Content-Type unless a header row does.
  `ApiSendRequest` sends the editor's unsaved state; `ApiCancelSend` stops it.
  Plain `http://` is allowed to loopback always, elsewhere only with no bound
  account on a tag that does not ask first (`ErrPlainHTTP`). Pasting curl
  (`ParseCurl`) opens a draft; it is not an import.
- **Import/export is format-agnostic** (`internal/apiclient/interchange.go`).
  Every format converts to or from one tool-neutral `Bundle` and never touches
  the vault; `Service.Import` / `Service.Export` are the only code that turns a
  Bundle into vault items or back, so the rules live once: no secret VALUE
  leaves (names do, empty), an account binding leaves as placeholder variables
  (`{{<account>_token}}`, named via the optional `AccountNamer`), an imported
  environment needs a tag (the person's choice, else one the file carries that
  this machine knows — else `ErrTagRequired`, before anything is written).
  **Adding a format is a new `format_*.go` implementing `Decoder` and/or
  `Encoder`, plus a line in `formats`**; it may not reach into the model — a
  missing concept grows the Bundle tool-neutrally. Today: `postman` (read +
  write; an environment's tag rides in its name as `Name [tag]`) and `curl`
  (write: a bash script, one function per request, `set -u`, running the file
  alone sends nothing). The window lists formats from `ApiFormats` and knows
  none by name. Export is `api:read` scope `export:*`, UI only — no MCP tool.
  Agents get `api.sendAdhoc`, and every request/response they read has
  credential-named rows masked (`Request.ForAgent`). An ad-hoc send
  (`SendInput.Adhoc`) that carries a bound account or a secret variable asks
  the person every time, on any tag: the agent chose the host, so the tag
  cannot have answered "send this token there".
- **Agents build the API client's contents, through the person**
  (`internal/mcp/tools_apiclient_write.go`): `api.listCollections`,
  `api.{create,update,delete}Collection`, `api.saveRequest`, `api.deleteRequest`,
  `api.{create,update}Environment`. Every write is `confirmAPIWrite` (kind `api`,
  inside the capability check). It asks EVERY time, no allow_always, for an
  environment (it names the hosts and its tag decides whether sending asks), a
  request that `Service.Carries` a credential (bound account, a secret variable under
  any environment, a credential typed into a row — `api.send` fires a saved request on
  the tag's rule alone), a collection's variables or parent, and any delete. Other
  writes offer allow_always. Variable edits are set/remove, the rest kept; a masked
  row sent back keeps the stored value (`Service.RestoreMasked`), and a masked value
  with nothing to keep is refused. Deleting an environment and Globals stay UI-only.
  Every write emits `apiclient:changed` (`Service.OnChanged`, via a wrapped item
  store), which the sidebar follows.
- **Boards bind to workspaces through local `task_board_bindings`** (not synced):
  each machine maintains its own binding of board → workspace, never written into
  the synced board metadata. `workspace_id` is nullable; a row's mere presence
  means "this machine has considered the board", distinguishing never-considered
  from deliberately-unbound. Agents see only boards bound to workspaces in their
  session's selected `workspace_id`, getting `tasks.notFound`
  otherwise—no second-guessing, and no MCP tool may bind. `project_path` is
  read-only fallback for legacy boards; `verify.service` names a workspace service
  instead, traveling through sync unlike paths. `Considered` marker guards
  `MigrateBoardBindings` to avoid re-binding unbound boards on restart.
- **Git over SSH uses the account's key, served by `internal/gitssh`** (a leaf). An `ssh-key`
  account holds the private key and its passphrase packed in one sealed secret; `gitssh.Open`
  serves it from a private in-process agent (0700 socket dir, one key, gone with the command) and
  git runs with `GIT_SSH_COMMAND` pointing at it with `IdentitiesOnly=yes`. `ssh-key` accounts are
  **local by default**: a scoped row whose owner is `local:<id>` (never swept as an orphan, resolved
  by id, never offered to the API client or revealed again), `Promote` is how one starts syncing.
  `accounts.ForRemote(host, owner, env, via)` picks the account for an unbound remote — keys over
  SSH, tokens over HTTPS — and remembers it per `(host, owner, via)`; the app's `remotePicker` asks
  the person through `interaction.Hub.SelectAccount` when two fit. SSH aliases resolve through
  `ssh -G`. A key that cannot be opened stops the command; it never falls back to the machine's own
  key. `~/.ssh/config` `IdentityFile` entries are still tried *after* a refused account key (see the
  pinned test). `internal/git` tests that touch SSH use `code.example.test`, not `github.com`: the
  cloud sandbox injects a `url.insteadOf` that rewrites github.com SSH URLs to HTTPS.
- **Any credential kind can be machine-local, not only keys.** `CreateInput.Local` makes the same
  scoped `local:<id>` row for a token or basic credential (never for `external`, or with an env
  restriction — the row has nowhere to keep one). `Verify` handles local rows (`verifyLocal`): the
  subject and verdict are kept in the row, scopes and expiry are shown once and not stored.
  `Service.CreateGitToken` is the git entry point (GitHub/GitLab token, Bitbucket basic with a
  required username; the provider must have an `SSHHost`, which is how a repository host is
  marked). `accountResolver.ResolveForRemote` gives git a stand-in username (the verified login,
  else `git`) when a token has none; the API client still gets what is stored.
- **Rotating a token is in place** (`Service.RotateToken`, bridge `AccountsRotateToken`): the id stays so
  bindings, remembered choices and the commit identity survive; the username is kept unless a new one is
  given (read with `Reveal`, since `Resolve` with no env refuses an env-restricted account); only
  `token`/`basic` accounts that are shared or machine-local can be rotated (a key is replaced by adding
  the new one; a connection's password changes through its profile). `Rotate` now clears subject, scopes
  and expiry (shared) or the local row's verification: they describe the OLD value.
- **Importing a key from disk is UI-only and path-guarded** (`internal/bridge/accounts_sshimport.go`).
  `gitssh.Discover` lists `~/.ssh` one level deep (no symlinks, ≤64 KB, skips `config`/`known_hosts`/`*.pub`)
  and `ReadAliases` reads `Host`/`IdentityFile`/`Include` as TEXT (`ssh -G` would report default key
  files for every host). The bridge reads a path only if this window was first shown it — listed by
  `AccountsDiscoverSSHKeys` or returned by the native picker — and the whole set of methods answers
  "desktop app only" in server mode (an unauthenticated bridge must not read files by path). No MCP
  tool exists for it. Keys are deduplicated by `(provider, fingerprint)`: one key may serve GitHub and
  GitLab, never one provider twice (`gitssh.QuickInfo` reads an OpenSSH key's public half without the
  passphrase, so listing and dedupe never run the slow KDF). An ssh alias bound to a key account
  (`ChooseForAlias`, stored in `account_remote_choices` as `(alias, "*", ssh)`) answers before the
  repository owner is asked.
- **Commits are written as the account's identity.** An account may carry a name and email
  (`account_identities`, local — not in the account metadata, whose strict decoder would make an
  older DevKit refuse the record). `git.Service.CommitIdentity` decides who the next commit is:
  the repository's own `user.email` wins, else the account of the branch's upstream remote (then
  `origin`, then the only remote) when it has BOTH halves, else git's configuration. It looks the
  account up through `remotePicker.IdentityFor`, which never opens a dialog. `Commit` passes the
  identity as `GIT_AUTHOR_*`/`GIT_COMMITTER_*` (an amend keeps the old author) and never writes
  `.git/config`. Agents commit through the same path, so their commits carry it too.
- **`internal/notify` and `internal/gitparse` are leaves** pinned by
  `internal/arch`, extracted as their own commits before the modules that
  needed them. `notify.Target` is three strings and deliberately not the
  frontend's `EntityRef`.
- **Regenerating bindings**: `wails3` may not be on PATH. Install it into a
  throwaway module (`go install github.com/wailsapp/wails/v3/cmd/wails3@<the
  version in go.mod>` from a temp dir) rather than into this one, then
  `wails3 generate bindings -clean=true -ts -i` — the `-ts` matters, the
  default output is `.js` and this repo commits `.ts`.

### Update notice (`internal/updater`, `internal/buildinfo`)
DevKit **tells** you a newer release exists; it downloads and installs nothing (OTA install
is a later step). `internal/updater` is a leaf that lists `syurodev/dev-kit` GitHub releases;
`buildinfo.Version` is stamped by the packaging build (`-X devkit/internal/buildinfo.Version`,
from `DEVKIT_VERSION`, which `build-prod.yml` sets from the tag). Unstamped builds report
`dev` and never reach the network.

- **Two release kinds, two channels.** `vX.Y.Z` is stable; `vX.Y.Z-beta` is a GitHub
  pre-release. A stable build is offered only stable releases; a beta build is offered the
  highest of either, so a beta tester hears about the next beta and the stable that supersedes
  it. "Newest" is by version precedence (`1.0.0-beta < 1.0.0`), never publish date, and the tag
  suffix decides even if the pre-release flag was forgotten. `/releases/latest` is not used
  because it never returns pre-releases.
- The release page URL comes off the network and is opened in the browser, so it must be
  `https://github.com/<repo>/releases/...` or the check fails rather than linking elsewhere.
- Bridge: `UpdatesCheck(automatic)`, `UpdatesCurrentVersion`, `PreferencesGet/SetUpdateChecks`
  (key `update_checks`, default on). Only the automatic startup check honours the preference
  (enforced in Go); "Check now" always answers. UI: `UpdateNotifier` (one toast per release,
  remembered in localStorage) and Settings → Account → Updates.
- Test: `go test ./internal/updater/ ./internal/bridge/ -run Update`; try a stamped build with
  `DEVKIT_VERSION=0.0.1 task build`.

### Open source licenses (`THIRD_PARTY_LICENSES.md`)
The notices for everything bundled into DevKit live in `THIRD_PARTY_LICENSES.md` at the repository root and in
Settings → Personal → "Open source licenses" (`settings/LicensesSetting.tsx`, fetched on demand as an emitted
`?url` asset, shown as plain text by `thirdPartyLicenses.ts`). The file is **generated**, never edited:
`bun scripts/third-party-licenses.mjs` reads the Go module cache (app, all three OSes, GUI and `-tags server`; the
`analyzer-ts` module), `frontend/node_modules` (the closure of `dependencies`), the Java analyzer's runtime jars
(`mvn dependency:list` + `~/.m2`) and the Inter font. The file is one entry per package (name, version, SPDX id,
source, note, the authors' own text, NOTICE files included), the way attribution notices are usually written. A package
offered under several licenses says which one DevKit uses (Apache-2.0 first, then MIT); a package that ships no license
file gets its text from its repository (cached in `scripts/third-party-licenses.fetched.json`, so the output is
reproducible offline); one that publishes none anywhere (`use-composed-ref`) says so. `--check` fails when the file is
stale; it needs those toolchains installed, so it is not a CI step. Re-run it after changing any dependency (`go.mod`, `package.json`,
`analyzer/pom.xml`, `analyzer-ts/go.mod`) or a bundled font. The Claude / Codex runtimes are downloaded at run
time, not bundled, and are not listed.
- Product identity (name `DevKit`, company `SynX`, id `com.syuro.devkit`) is in `build/config.yml` and the
  generated build assets under `build/` (plists, `desktop`, `info.json`, NSIS, MSIX). The macOS menu-bar name is
  `CFBundleName` in `Info.plist` / `Info.dev.plist`. `build/ios/` is scaffolding and still carries the template name.
- `build/linux/nfpm/nfpm.yaml` still says `license: "MIT"` from the template; DevKit has no LICENSE of its own yet,
  so that field is a placeholder to settle with the project's own license.

### Notes / gotchas
- **Brand marks come from Simple Icons, one import each** (`frontend/src/components/brand-icon/BrandIcon.tsx`,
  `@icons-pack/react-simple-icons`). Add a brand to its `BRANDS` table with a deep import
  (`@icons-pack/react-simple-icons/icons/SiKong`), never from the package's barrel (~7000 modules in the dev server). A mark is one path on
  `currentColor`, decorative (`title=""`, `aria-hidden`); a caller's size/colour wins (a tree row sizes its svgs).
  Used for: Kong on the `plugin` entry points (`EntryPointsTab`), the git host on an account row (`github`/`gitlab`/`bitbucket`
  provider ids, `isBrand`), Kafka on a cluster row (its connection state is the mark's colour). Not for files (that is
  `components/file-icon`, Material Icon Theme with light/dark variants), not for the Database engine marks (Figma wants the
  vendor's colours there, `modules/database/icons`), and not for Claude/Codex (`VendorIcon`, from Figma; OpenAI has no Simple Icons mark).
  The artwork is CC0 but the marks are their owners' trademarks. A new dependency means regenerating `THIRD_PARTY_LICENSES.md`.
  Gallery: `?gallery#brand-icons`.
- Sync login: production uses `internal/cloudconfig` (pinned `GET /v1/desktop/config`, disk cache, refresh on reconnect). Set `DEVKIT_OIDC_*` to override remote/cache for local dev. Omit all OIDC env + stay offline with no cache for local-only vault/modules.
- Storage is a single SQLite file (`modernc.org/sqlite`, no cgo) opened with
  `MaxOpenConns(1)`; migrations run forward-only from `storage.Migrate`. Because of the
  single connection, migration `Up` funcs must **not** be wrapped in an outer transaction.
- `bin/`, `frontend/dist/`, and `frontend/node_modules/` are gitignored — don't commit them.
- `frontend/.npmrc` sets `minimum-release-age=10080`; honoured by bun/pnpm, ignored by npm.
- `wails3 generate bindings` is slow (~15s, scans all packages) and regenerates
  `frontend/bindings/`; re-run it after changing any bridge (`AppService`) method signature,
  then `bun run build`.
