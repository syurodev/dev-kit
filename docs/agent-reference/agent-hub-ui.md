### Agent Hub (`internal/agenthub` + `internal/acp`)

- Team foundations are tracked in `docs/superpowers/plans/2026-10-25-team-foundations.md`.
  `internal/agentwork` is a stdlib-only orchestration leaf with injected operations: task-to-child
  handoff is shared there, while MCP keeps capability checks, caller identity and model gates.
  Failed assignment/branch recording closes only the newly created child using bounded cleanup
  independent of the cancelled request; failed brief delivery keeps the assigned child available.
- Scoped pre-approvals currently support **exact `db.query` only**. A live Agent Hub gate card
  issues a finite, expiring approval through the desktop-only bridge; agents cannot issue one.
  Each query reserves an opaque one-use token and consumes it immediately before the database
  call, after capability authorization and profile/parent/tunnel/env revision rechecks. Environment
  confirmation still wins. Vault lock and restart invalidate approvals; server mode cannot issue
  or consume them. Other operations keep their existing gates. See
  `docs/superpowers/specs/2026-10-25-unattended-policy-design.md` before extending eligibility.
- The peer outbox (migration 47) stores peer bodies as local plaintext like the transcript; never
  log them. A crashed `dispatching` item is ambiguous and is not replayed. Recovered queued items
  remain held until that session's user queue is mirrored; scope and hop/rate guards are rechecked.
- Pending-request recovery (migration 48) stores metadata only. Never persist question/permission
  payloads or restore an old approval as answerable after its vendor process has stopped.
- Claude ACP prompt-result metadata can contain a typed failure even when JSON-RPC succeeded.
  The Claude adapter's `PromptFailure` hook classifies it; do not add vendor branches to the generic
  runtime. `_meta.quota.token_count` counters distinguish input, cache read, cache write and output;
  unknown counters remain unknown, and a refused turn still retains usage/raw metadata. Fixture
  tests verify accounting and refusal handling, not a measured reduction in real vendor quota.

**Partial** v1 surface on this branch: ACP codec/conn, mock + Claude adapters,
session service (SQLite migration 13), bridge/UI multi-tab, MCP `agenthub.*`.
Claude vendor spawn uses `sandbox.DefaultRunner().StartPiped` (fail-closed).
A Codex adapter (`adapter_codex*.go`, `codex_*.go`) drives `codex app-server` 0.147.0
under the same jail with its own `CODEX_HOME` (`<data>/agent-homes/codex`) — design in
`docs/superpowers/specs/2026-09-24-codex-agent-hub-design.md`, state in
`docs/superpowers/HANDOFF-codex.md`. Gemini is **not** shipped. Product design + status:
`syurodev/base` → `/dev-kit/agent-hub`, `/dev-kit/implementation-status`.

- **The ACP runtime is generic; the vendor is a hook** (`internal/agenthub/acp_runtime.go`). `acpRuntime` +
  `acpConnHandler` (ACP over stdio, turn bookkeeping, usage, steering, resume) serve both Claude and opencode, and
  `adapter_claude.go` holds only what Claude adds (jail and `_meta`, `_claude/*`, customization). A vendor adapter
  sets `acpRuntimeHooks{Type, PermissionBefore, PermissionAfter}`; the zero value is Claude. Anything a vendor alone
  needs belongs in its adapter, not here and not behind a new `if` in the runtime.
- **The ACP handshake is shared too** (`internal/agenthub/acp_handshake.go`, B4). `startACPHandshake` wires the
  launched process to an `acpRuntime`, bounds the handshake by the process's life and explains a failed call
  (`fail`/`explain`: auth required, exited during handshake, still running); `initialize`, `acpMCPServers` and
  `session` (`session/new`, or `session/load`/`session/resume` with the id-mismatch check) are the calls both
  adapters make; `probeACP` is the probe `initialize`; `authMethodCache` (`auth.go`) is the `AuthMethods` cache.
  Claude keeps `session/fork`, its `_meta` and customization; opencode keeps its load method choice and extra
  directories. Which of Open and Load to call, and restoring model, effort and mode afterwards, live once in
  `Service.resumeRuntime`; stop classification is `StopFromError` plus `StopHint`. Codex speaks its own protocol
  and keeps its own handshake.

- **Adapter conformance** (`internal/agenthub/conformance_test.go`) runs shared lifecycle and turn cases against the mock and scripted vendor wires. When adding a vendor, reuse its adapter fake, add a `VendorQuirks` entry and `TestConformance_<Vendor>` wrapper, then run `CGO_ENABLED=0 GOFLAGS='-buildvcs=false -tags=server' go test ./internal/agenthub/ -run Conformance -count=2`. Capability flags must name vendor limitations with a concrete reason; do not disable a case because its harness is incomplete. Codex and opencode live checks are opt-in with `DEVKIT_CODEX_COMMAND` and `DEVKIT_OPENCODE_COMMAND`.

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
  - **A branch draws `max(3, running children)` children, not all of them** (`tree.ts` `visibleChildren`,
    `BranchMore.tsx`, spec `docs/superpowers/specs/2026-10-21-agent-hub-many-children-design.md`). Every running child
    is drawn, then the newest idle ones up to three; the session on screen, a pinned one, and the child above them
    are drawn even past that. The rest fold into one row, `+N more · Show all` (it also says how many folded
    sessions are running and which hold unread messages, descendants included, because a folded idle child can
    hold running work and an open parent's dot stays silent). Applies at every level; the "all" choice is state of `BranchChildren` and is forgotten when
    the branch is folded. The parent's count, `branchSignals` and Needs you still read every session. Gallery:
    `rail-many-children`.
- **The rail and the main view are a quiet list and a column with a summary card**
  (spec `docs/superpowers/specs/2026-10-13-agent-hub-wide-layout-design.md`, its last section lists
  what Phase 1 changed from it).
  - Rows are 50px: the vendor face (`sidebar/SessionAvatar.tsx`) with its state on it (running arc,
    waiting/error dot), title and age, then a second line: the pending ask (the first permission wins),
    else `AgentSession.preview`, which Go writes at turn end (migration 41, `agenthub/preview.go`). MCP
    blanks the preview. Subagents are in the row menu.
  - **A session is working** when a turn of ours runs (stored status `running`) **or** the agent runs a turn of its own
    (`activity.agentTurn`, see `agent-hub-vendors-sessions.md` "Session activity"): `isWorking(session)` in
    `modules/agenthub/sessionActivity.ts`. Everything that asked "is this session active" reads it: the card's avatar and strong
    title (`cardIndicator`), the group's and branch's running counts, `branchSignals`/`visibleChildren`, the **Cancel turn**
    item (`Service.Cancel` answers nil when nothing runs, but on a live runtime it can end the **whole process**, background shell
    tasks included: the activity is then cleared at once), the composer's Stop (`streaming`), the dock badge, "Reload session"
    (also disabled while background tasks run, saying why: a reopen would end them),
    the system status count and the tab (`handleAgentActivity`: running while the agent's turn runs, finished and unread when it
    ends; `handleSessionUpdateTabs` no longer paints an idle session's tab running for a background task's update). What
    stays on the **stored status** because it is about DevKit's own turn: `shouldSteer`, the send queue, queue drain, the
    permission bookkeeping, `statusAnnouncement`. **Background tasks alone** (`tasks > 0`, no agent turn, idle) draw no spinner
    and no running count: the second line says "1 background task" quietly (`backgroundTasksOnly`, `secondLine`), in
    place of the older preview. When an agent turn ends the window reads the row once for the new preview, marks the tab
    finished and the result unread, **unless** the event says the turn did not finish (`ended: false`: the process went away)
    or the session is `closed`. The store keeps, of a read and an event of one session, the one with the higher `activity.rev`
    (`newerActivity`), so a read that was in flight when an event landed cannot erase it. Sending a prompt while the vendor
    runs a turn of its own goes straight to `Invoke`; its behaviour with a real vendor is not verified.
  - "Needs you" lists waiting/failed sessions across workspaces (`needsYou`, through `groupSessions`'
    keep filter, so a child stays with its parent's group).
  - **A failed session can be dismissed from Needs you** (`sidebar/dismissed.ts`, `dismissFailure.ts`; X button on the row,
    "Dismiss from Needs you" in the row menu before Close session, "Dismiss" in the open session's failure banner). Only
    the failure: the session is not closed, goes back to its group with the quiet idle look (no red dot, no danger line;
    the tooltip says "Stopped with an error, dismissed"), stops marking its parent (`branchSignals`), and its tab dot and
    "ready to review" mark settle. `agentPins.dismissedFailures` is `sessionId → the row's updatedAt` then; a failure is
    dismissed while the row is not newer, and `watchDismissedFailures` (AgentSessionsSubscriber) forgets an entry once the
    session leaves `error` or is gone (never before the list has loaded), so the next failure is news. A pending permission
    or question is never dismissable (`failureDismissable`). Not seen in the native app, only in the gallery
    (`?gallery#agenthub-session-rail`, "rail-dismissed").
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
  travel on the wire's `_devkit` key (`wireDevKit`). Every section is a shadcn `Collapsible` (`SectionShell`) and **starts
  folded**; what the person *opened* is remembered across sessions in `localStorage` (`summary/summaryFold.ts`, key
  `devkit.agenthub.summary.opened`, shared by the docked card and the corner popover; the old `…closed` list is ignored).
  A folded header carries one dimmed line (`hint`: Environment → model, Goal → objective, Pull requests → "1 open · 6 need
  an account", Context → "19% used"); sections with a count or a +/− total keep that instead. A Changes row is a name
  (stem truncates, extension stays: `splitFileName`), a folder that shrinks first and right-aligned counts; the name is
  capped against the row's own width (`@container`, `100cqw`) because a weighted flex shrink gives up a fraction of a pixel
  and draws a whole name with an ellipsis. Branches reads `main · 15 repos` / `2 branches · 15 repos`, the tooltip grouped
  by branch. Pull-request refusals are **one notice per reason, not per folder** (`pullRequestsView` groups by status +
  `sharedMessage`, the sentence with its parenthesised repository removed): the shared sentence, an `Add token` action
  (Accounts), and the folders behind a disclosure; cards come first. Go already stops polling for a folder that needs a
  person (`nextPoll` → -1 for no account) and asks once for all folders, so nothing changed there.
  Gallery: `?gallery#agenthub-summary` (Open all / Fold all; the opencode card is the 15-folder case).
- **Peer messages and `agenthub.sendMessage` results share one look** (`transcript/peerParts.tsx`: `PeerAvatar`, `PeerParty`,
  `StatusChip`; `PeerMessage.tsx`, `toolViews/message.tsx`). Both are a `FramedCard` (raised frame, inset body); the party is the
  vendor avatar + live session title (`From …` / `Message to …`, opens the session); the delivery is a chip (started/steered
  green dot, queued amber, refused red — a word always accompanies the colour). A received text is clamped to 3 lines with
  an `aria-expanded`/`aria-controls` Show more shown only when the text really overflows (measured; length decides before
  the first measure). The sent card wraps a long reason instead of truncating it. Gallery: `?gallery#agenthub-peer-messages`.
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
  (no request yet, detached, default branch, not a repo, unsupported host). `no_account` for a remote that has an
  SSH-key account but no token is worded as such ("The GitLab key “…” pushes your code but can't read merge
  requests. Add a GitLab token…"): `prCredentials.noToken` returns `prwatch.KeyOnlyError` (still `ErrNoAccount`,
  carries only the key's sanitised label — no parentheses, which `sharedMessage` would strip — never a secret), `pr.*`
  tools say the same under the published `pr.noAccount`, and "Add token" opens Accounts' add-token form on that provider
  (`stores/addTokenRequest.ts`). Polling is the window's: `State` returns
  `nextPollMs` (15 s while a check runs or mergeability is unknown, 60 s settled, 90 s with no request, -1 when merged
  or closed or when it needs a person); react-query pauses while the window is hidden; a turn ending, window focus
  and the `prwatch:changed` event (pr.create, attach) refresh at once. An account change (`accounts:changed`, from `accounts.Service.OnChanged`: create, rotate, delete, promote, a remote/host choice) invalidates every session's card (`watchAccountChanges`) and drops prwatch's 3 s answers (`CredentialsChanged`) — a `no_account` folder polls no more, so nothing else would notice a new token; credentials themselves are never cached in prwatch. Reads are conditional (ETag/`If-None-Match`,
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
- **Interrupted rewind notice** (`surfaces/InterruptedRewindNotice.tsx`, rules in `interruptedRewind.ts`, read in
  `useInterruptedRewind.ts`): above the composer in the content and the rail, only while the session's own checkpoint list
  has a `rewind_safety` row still `pending` and no rewind dialog is open. Actions are the backend's two: put the files back
  through the `undoRestore` preview and confirmation (`UndoRewindFilesDialog`, `RestoreFileList` shared with `RewindDialog`),
  or settle the row (`AgentHubDismissInterruptedRewind`, asked first). No retry, no "finish the rewind". A failed call reloads
  the list rather than guessing. A rewind refused after its restore settles `failed` and raises no notice. See
  `docs/superpowers/specs/2026-10-28-rewind-recovery-ui.md`.
- **The right-hand panel has one header, the shell's** (`SubagentPanel` in `subagents.tsx`, `AgentActivityPanel` in `follow.tsx`;
  opened from `session/useAgentHubEffects.tsx`). `openPanel` takes an optional `header` (`PanelHeader` in
  `components/ui/splitPanelModel.ts`: `title`, `titleHint`, `icon`, `status`, `actions`). Who draws what: the shell
  (`components/ui/pane-slots.tsx` `PaneHeader`) draws icon, truncating title, status, actions, then its own layout and close
  buttons, and the title replaces the module label ("Agent Hub"); the opener describes it; the panel body draws no header
  and no close. A pane opened without `header` is drawn as before (other modules unchanged). The header is the opener's
  state: a new `header` under the same `key` updates the row and keeps the body mounted, so `status` and `actions` are
  elements that read the store themselves (`useSubagentHeader` in `subagentHeader.tsx`, `useActivityHeader` in `follow.tsx`)
  and the header changes only with the title and hint. The shell's Move and Close keep naming the module ("Close Agent Hub"),
  never the header's title. A header stays through the close animation only while its pane goes with it. Subagent: name
  (`"Subagent"` unnamed), tooltip = the whole name then the task, Bot, `AgentState`, Stop. Follow feed: `Following <session title>`,
  tooltip = the whole title, the sidebar card's state (`agentStatusOf`), and "Stop following" as its action (the shell's close
  does the same).
  - **Stop on a subagent is "Stop turn", not Stop.** DevKit has no per-subagent or per-task cancel: `Service.Cancel` ends
    the session's whole runtime, subagents and background tasks with it. So the control shows only while the subagent runs
    and the session `isWorking`; it is icon-only (a pane can be 320 px wide), `aria-label` "Stop turn", tooltip "Stops the whole session's turn, including this
    subagent and background tasks"; it asks first (`AlertDialog`, as the sidebar's delete does) and then calls `cancelSessionTurn`
    (`session/cancelTurn.ts`), the one function behind the sidebar's "Cancel turn" and the composer's stop. Never a bare "Stop". The question is withdrawn when there is no turn left to stop and is not carried to another subagent or
    session; a double confirm cancels once.
    A stop for one subagent or task needs a backend and vendor capability and is not built. See
    `docs/superpowers/specs/2026-10-30-panel-single-header-design.md`.
