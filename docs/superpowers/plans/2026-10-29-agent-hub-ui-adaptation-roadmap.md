# Agent Hub UI adaptation to what Claude actually sends: roadmap

Status: roadmap-level plan. It orders the slices and fixes their scope; **each slice gets its own short spec and
implementation plan when it is started** (the spec's open questions are marked "owner decision"). Base: `main` at
`80c97fbd` (#396 merged: `bgwork.go` and the folder-grant request exist). Nothing here is implemented.

## 1. Evidence

A survey of the owner's database on 2026-10-29 (read-only; event kinds, field names, counts and times only, no
conversation text): 55 Claude sessions, 7,048 tool calls, 15 event kinds stored. Section 3 names the numbers each slice rests on.
Limits: one machine, one organisation; samples are small for autonomous turns (5 ended), `limit` failures (2) and the
rarer tools; DevKit stores only the events it already normalises (the database has no `unknown` row, and whether
DevKit drops unknown vendor events was not checked); Claude only (16 opencode and 1 Codex session were not
surveyed). The repeatable token report is in `/home/ptvu3/Downloads/devkit-usage/session-token-report.sh`.

## 2. How the window gets its data today (verified by reading the code)

- Session status in the window store (`useAgentSessionsStore`) changes on Go events: a turn we started sets it,
  `handleAgentTurnFinished` (`frontend/src/workbench/AgentSessionsSubscriber.tsx`) sets `idle`/`error`/`closed` for a
  turn started elsewhere and refetches the row for the preview only. Any session update only sets the **tab** status
  to `running` (`handleSessionUpdateTabs`). Nothing in the store reacts to an update that arrives with no turn in flight.
- The sidebar card draws `running` from `session.status === "running"` (`sidebar/AgentSessionCard.tsx`,
  `AgentSessionGroup.tsx`, `SessionBranch.tsx`).
- `turns.ts` already words the live label of a turn from the last update, including `async_task_*`
  ("<name> in the background").
- Tool rows: `stepLabel` (`turns.ts`) words any `mcp__<server>__<tool>` generically ("Tasks transition" over the server name)
  and has verbs for Read/Write/Edit/Fetch/Search/Find and shell. Rich result cards come from a registry,
  `registerToolResultView(name, renderer)` (`workbench/toolResultViews.ts`), matched by the tool's dotted or wire name, and
  drawn after the turn's fold by `transcript/ToolResultCard.tsx` (at most 3 per turn).
- On the backend, `bgwork.go` (#396) already knows, per session, which vendor tasks and agent-started turns are running; the
  window cannot see it.

## 3. The slices

Order is by value over effort; dependencies are stated. "Spec first" means the slice has a design choice the owner
should see before code.

### U1. Session activity during background work and agent-started turns (spec first; owner decision)

**Status: done on branch `feat/session-activity` (option B, spec `docs/superpowers/specs/2026-10-29-session-activity-design.md`),
2026-10-29, not yet merged or reviewed.** Built: `SessionActivity` from `bgwork` on `AgentSession.Activity` (derived, never stored,
blanked for MCP), a coalescing notifier and the `agenthub:activity` event, the preview of an agent-started turn, `isWorking` in the
window (sidebar, tab, Stop, composer) and the tasks-only quiet line, and the latest-turn rule of `settleToolState` (which also
closes the "cancelled newest turn keeps spinning" gap U5 noted). Verified by tests and mutations only; nothing was run in the native
window or against a real vendor. Details: `docs/agent-reference/agent-hub-vendors-sessions.md` (Session activity), `agent-hub-ui.md`,
`agent-hub-transcript-composer.md`.

- **Problem.** After a vendor task finishes, Claude starts a turn on its own (`async_task_spawned` with `taskType: "turn"`,
  id `autonomous-turn-*`, ended by `async_task_state_update` `completed`). DevKit keeps the session `idle`: in the
  owner's data one such turn ran 41 s with 11 tool calls and the database said `idle` throughout; the sidebar showed no
  activity and the preview kept the previous sentence. At survey time five autonomous turns had ended, all with `completed`
  (mean 67 s, longest 207 s), and a sixth was running. Background tasks overall: 45 spawns (40 shell, 5 turns); their state
  updates were 38 `completed`, 2 `failed`, 19 `stopped`.
- **Options for the owner.**
  - A. Window-only: derive "working" for a session from the live `async_task_*` stream (spawn adds, terminal state removes) and draw it
    like `running` without touching the stored status. No backend change; the window is the only reader.
  - B. Backend `Activity` on the session DTO from `bgwork` (running vendor work, not persisted), pushed with an event; the window
    draws it. One truth shared by the window, MCP callers and peers; touches the DTO and the bindings.
  - C. Make the stored status `running` for an agent-started turn. Rejected unless asked: `Invoke` and peer delivery read the status,
    and a stored `running` with no claim would need its own recovery.
- **Recommendation: B** (the data exists in `bgwork`; A would duplicate the rule in the frontend, as `RUNNING_TASK_STATES` already does).
- **Scope.** The activity state; a sidebar and tab indicator distinct from a turn ("working in the background"); the preview
  refreshed when an autonomous turn completes (today it only moves at a turn's end); the live label for the tab.
- **Acceptance.** Replay of the 04:22:39 to 04:23:20 sequence from the owner's data (as a fixture, not the data) shows the indicator
  on at the spawn and off at `completed`; a task that never ends clears after the 12 h valve; no change to stored status.
- **Size:** medium. **Depends on:** #396 (done).

### U2. Make a granted folder visible, and say when it takes effect (small)

- **Problem.** `handleAgentTurnFinished` refetches only the preview, so a folder added by `workspace.requestWrite` during a turn does not
  appear in the session until the next list refresh; the Environment view of a live session shows the process's own grants, so it shows the
  folder only after the reopen (documented in #396).
- **Scope.** (1) After a turn finishes, take the session's folder list from the refetched row. (2) Invalidate the Environment query on that
  event, and make `Service.Environment` for a live session also list each folder of the stored row that the process was not opened with,
  flagged "Granted. Applies after this session reopens." (decision in section 5; no DTO field, no bindings change unless an `AppService`
  signature moves).
- **Acceptance.** A grant during a turn shows in the session's folders when that turn ends; the Environment view lists it as pending until the process is reopened, then as granted.
- **Size:** small. **Depends on:** #396.

### U3. Prompt suggestions as ghost text (decided by the owner on 2026-10-29)

- **Evidence.** `session_info_update` rows carry `_meta.promptSuggestions.suggestions` 312 times (177 with exactly one suggestion
  `{text}`, 135 empty). Neither the Go parser nor the window reads them.
- **Behaviour (the owner's words: "ghost text, not a button; Tab inserts the text into the prompt, like a code suggestion in VS Code").**
  When the composer's draft is empty and the session can take a prompt, the latest suggestion shows as dimmed text where the caret is, as an
  inline suggestion does in an editor. **Tab** inserts it into the draft (it becomes ordinary text the person can edit, and the caret ends after it);
  it never sends. **Escape**, any typing, a paste, a click elsewhere, a new turn, or a session switch dismisses it. A non-empty draft shows nothing
  in this slice (completing a half-typed sentence is not offered by the vendor). A hint for the key ("Tab") is a small muted label at the end of the
  line, shown once the ghost has been on screen for a moment, not a button.
- **Rules.** The text is model output: plain text only, length-capped, single paragraph (a suggestion with newlines is shown up to the first
  line break and inserted whole). The composer's IME and external-value rules (#393 `externalValue.ts`, `ComposerEditorBoundary`) must hold:
  no ghost and no Tab capture while a composition is in progress; Tab keeps its normal meaning (focus move, list indent) when there is no
  ghost; screen readers get the suggestion through `aria-description` on the editor, not as extra text in the document.
- **Scope.** Parse the field into `SessionUpdate` (Go table test of the wire shape: empty array, one item, missing key); the window keeps the latest
  suggestion per session (cleared by the rules above); the editor draws the ghost as a decoration that is not part of the document (check how the
  composer editor is built, `composerEditorKit.ts`, before choosing a decoration over an overlay); a setting to turn it off.
- **Acceptance.** Frontend tests: ghost appears for an empty idle draft only, Tab inserts and clears, Escape clears, typing/paste/IME composition clears or
  suppresses it, a new turn or session switch clears it, empty suggestion shows nothing, Tab without a ghost is unchanged.
- **Size:** medium. **Depends on:** nothing.

### U4. Tool rows and result cards for the long tail (several small slices, parallel)

- **Evidence (calls in the owner's data).** Bash 3,928; built-in Edit 279, ToolSearch 274, Read 259, Write 130, AskUserQuestion 40,
  SubagentHandback 23, Skill 8, Monitor 5, SendMessage 3, TaskStop 2. `mcp__devkit__*`: `api.sendAdhoc` 189, `tasks.transition` 113,
  `git.stage` 112, `git.commit` 100, `tasks.setFields` 92, `tasks.create` 92, `tasks.comment.add` 74, `workspace.memory.get` 59,
  `agenthub.sendMessage` 54, `git.status` 52, `tasks.update` 48, `git.branch.create` 45, `api.saveRequest` 43, `agenthub.createAgentSession` 41,
  `git.push` 29, `agenthub.closeSession` 23, `pr.create` 18.
- **Already covered by a result view:** `tasks.create`, `tasks.get`, `tasks.board.list`, `agenthub.sendMessage`, `api.send`, `git.diff`,
  `db.*`, `kafka.*`, `pdf.*`, `codegraph.searchSymbol|listEndpoints|getApiFlow`, `ssh.runCommand`, `notes.set_organization` and the memory views.
  Check each against the table above before building (the registry matches dotted and wire names).
- **U4a: step wording for built-ins and the tool-search noise** (frontend, small). Add verbs to `stepLabel` for `Skill`, `Monitor`, `TaskStop`,
  `SendMessage`, `EnterPlanMode`, `SubagentHandback`; fold `ToolSearch` rows (274, 4 % of calls, mostly `select:` loads) into one quiet
  "Loaded tools" row per run, expandable. Owner decision: hide by default or keep a muted row.
- **U4b: result cards owned by their modules** (one slice per module; each registers views from its own manifest, as `kafka` does):
  tasks (`transition`, `setFields`, `comment.add`, `update`, `claim`: a one-line status/field change card), git (`stage`, `commit`, `status`,
  `push`, `branch.create`: files staged, commit subject and short hash, ahead/behind), api (`sendAdhoc`, `saveRequest`: reuse the `api.send` card), agenthub
  (`createAgentSession`, `closeSession`: the session as a chip that opens it), pr (`create`: title and link), `workspace.requestWrite` (folder, granted or declined).
  Each view must degrade to the existing generic row when the output cannot be parsed (the registry returns null).
- **Acceptance.** Per view: renders from a recorded output sample (taken from the survey's shapes, scrubbed), returns null on malformed input,
  keeps the three-per-turn cap, passes the module's tests and the Tailwind/token rules (no inline styles).
- **Size:** U4a small; each U4b slice small. **Depends on:** nothing.

### U5. Tool calls that never finished, and failures (small, frontend first)

- **Evidence.** Five `tool_call` rows are still `pending` in sessions that are idle and have 30 to 900 later rows (four Bash, one
  AskUserQuestion; oldest 2026-09-21). They are interrupted calls that never got an update. 249 calls (3.5 %) ended `failed`.
- **Scope.** (1) `toToolState` (`AgentToolCall.tsx`) treats `pending`/`running` as live; add a "didn't finish" state when the session is not
  running and the call is not the last row of an open turn, with muted wording and no spinner. (2) Show the first line of a failed call's output
  in its collapsed row. (3) Optional backend follow-up: finalise `pending` calls as `interrupted` when a turn ends; only after the owner agrees to
  rewriting stored rows (it changes history).
- **Acceptance.** A recorded transcript with a stale pending call shows no spinner in an idle session; a still-running turn's pending call keeps its spinner.
- **Size:** small. **Depends on:** nothing.

### U6. Usage-limit and connection failures (inspect first, then small)

- **Evidence.** `session_info_update.failure`: connection 5 ("Reconnecting to Claude, attempt n of 10", handled by `providerRetry.ts`), limit 2
  (an organisation's monthly spend limit with the vendor's long sentence and a reset time), interrupted 1.
- **Scope.** First read `surfaces/AgentSessionFailure.tsx` and `accounts/accountModel.ts` to see what a `limit` failure shows today (it uses
  `failure.details` as plain text). Then, if needed: a structured line with the reset time and the account actions DevKit already has (switch account), and
  the vendor's text kept as a quoted detail. Do not parse the vendor sentence beyond what the structured fields already give.
- **Size:** small after the inspection. **Depends on:** nothing.

### U8. The right-hand panel of Agent Hub: one header, no wasted width, nothing dropped (added by the owner on 2026-10-29)

Applies to the two panels Agent Hub opens beside the conversation through `useSplitPanelActions().openPanel` (`session/useAgentHubEffects.tsx`):
`SubagentPanel` (`subagents.tsx`, a subagent's work, opened from the sidebar) and `AgentActivityPanel` (the follow feed of the active session).

- **U8a. One header. Done** (spec `docs/superpowers/specs/2026-10-30-panel-single-header-design.md`; contract in `docs/agent-reference/agent-hub-ui.md`; U8b and U8c are open; the look of the single header and its truncation at 420 px were not seen in a native window). Today the shell draws a panel header (module icon, the text "Agent Hub", a layout button, close) and the panel draws a second one
  inside (`SubagentPanel`: Bot icon, the subagent's name, its state, close). Two rows of chrome, two close buttons, and the text "Agent Hub" says which
  module the person is already in. Make it one: `openPanel` options gain a header description (title text, optional icon, a status, optional actions), the shell draws it in
  its single header in place of the module label, and the panels stop drawing their own. Content of the header for `SubagentPanel`: the subagent's name
  (its task as the tooltip), the state (`AgentState`), and a **Stop** while it runs; for `AgentActivityPanel`: the session title, its state and the existing stop-following action.
  The shell's close and layout buttons stay. Read `docs/design/module-shell-integration.md` and `docs/agent-reference/core-development.md` first (the shell owns the sidebar and
  the panel chrome; a module must not add a second strip); the change belongs in the shell's panel slot (`workbench/slots/slotModel.ts` and the split-panel host), with the
  option optional so other modules (`kafka` message panel and the like) are unchanged.
  - **Stop for a subagent: owner decision needed in the slice's spec.** DevKit has no per-subagent cancel for a Task-tool subagent; `Service.Cancel` ends the session's whole runtime
    (and with it every background task, see the vendors doc). A background *task* the vendor reports with `canStop` can be stopped on its own (`AgentAsyncTask`). Proposal:
    show **Stop** only where it means what it says: for an async task with `canStop`, stop that task; for a Task-tool subagent show "Stop session turn" with a one-line
    confirmation that it ends the whole turn; never a bare Stop that silently does the larger thing.
- **U8b. Width.** The panel is 420 px wide and `SubagentActivity` indents its content (`ml-1.5 border-l pl-4`, the nesting rule meant for a fold inside the transcript) inside a `p-3`
  container, so a quarter of the width is gutter, as the owner's screenshot shows (the commands and their output start well inside the panel and long lines wrap early). Add
  a flat variant of `SubagentActivity` for the panel: no rule, no indent, 8 px padding; tool output boxes use the full width. Check `AgentActivityPanel` for the same gutter.
- **U8c. Audit of the subagent's events.** Find out, with tests and not by reading alone, which of a subagent's events the panel draws, folds or drops. Known from the code and the
  owner's data (2026-10-29): the panel reads the owning session's transcript and groups rows by `origin` (`splitFlow` in `turns.ts`, `runningSubagents`), draws text, plans and steps
  (`SubagentActivity` / `workSegments`), and shows an empty state "Nothing to show yet" when a subagent's rows are in a page that is not loaded. In the stored rows of the owner's database
  the kinds that carry an `origin` are `agent_message_chunk` (333), `agent_thought_chunk` (28), `async_task_*` (24) and `subagent_*` (4); **no stored `tool_call` row carries an origin**
  (7,048 tool calls, none), although the live panel shows a subagent's commands. So after a reload or for a subagent that ran in an earlier session load, the panel may show only the
  words and drop the steps. First step of the slice: reproduce it with a fixture of the wire shape (a tool call streamed with `_devkit.origin`, recorded, reloaded through `initTranscript`),
  and find where the origin is lost (`ParseSessionUpdate`, `Recorder.recordToolCall`/the payload, the page path). Then list every kind a subagent can emit against what the panel draws:
  `agent_thought_chunk` (is "Thinking" shown?), `async_task_*` of the subagent (its background tasks), nested `subagent_*`, permission requests raised by the subagent (does the card
  say which subagent asked, and can the person answer from the panel?), `plan`, usage, failures. Fix what is dropped; list what is deliberately not shown.
- **Acceptance.** One header row per panel with title, state and the right Stop; no second close button; content starts at the panel's left padding and uses the full width (a layout
  test on the class set, since there is no native window to measure); a reloaded subagent shows its steps as the live one did; a table in the PR of every subagent event kind and what
  the panel does with it.
- **Size:** medium (U8a touches the shell slot; U8b small; U8c an investigation plus fixes). **Depends on:** nothing; U8c may need a Go change if the origin is lost on the way into storage.
  Spec first: the header API and the Stop semantics.

### U7. Measure it (not UI code)

After U1 to U5, rerun `/home/ptvu3/Downloads/devkit-usage/session-token-report.sh` and compare with
`baseline-before-harness-2026-10-29.txt`. The slices change the window, not the prompt, so no token change is expected from them; the report is for the harness
and the new `workspace` tool (about 570 bytes of definition per session). A repeat survey of event kinds (same queries) shows whether new vendor events appeared.

## 4. Order

1. **U2 (1) and (2)** and **U5**: frontend only, no design choice, the quickest visible fixes.
2. **U1** (needs the A/B/C decision), with U2 (3) if the DTO is touched anyway.
3. **U4a**, then **U4b** slices in parallel by module (tasks, git, api, agenthub, pr), each its own pull request.
4. **U8** (spec first for the header API and Stop; U8c can start as an investigation at once).
5. **U3** (decided: ghost text, Tab inserts).
6. **U6** after its inspection.

Each slice: spec if marked, then a plan in the style of `2026-10-29-agent-write-grant-implementation.md` (failing test first, mutation checks, independent review,
the coordinator pushes, the owner merges). Frontend slices use `cd frontend && bun test --isolate` and `bun run build`; a change to an `AppService` method regenerates
bindings with `wails3 generate bindings -clean=true -ts -i`.

## 5. Decisions

The owner delegated these on 2026-10-29 ("decide for the best UX"). They are the coordinator's choices, recorded so a later spec can
challenge them.

| Slice | Decision | Why |
| --- | --- | --- |
| U1 | **B**: a non-persisted `Activity` on the session DTO from `bgwork`, pushed with an event. | One rule for the window, MCP callers and peers; the data already exists on the backend; no stored-status semantics change. |
| U2 | The session's folder list is refreshed at turn end. The **Environment view lists a granted folder the live process does not have yet**, marked "applies after this session reopens" (Go change in `environment.go`, reading the stored row). No new DTO field and no separate notice. | The place a person checks "what can this session write" tells the truth, including the pending part; a banner would duplicate it. |
| U3 | **Ghost text** in the empty composer like an editor's inline suggestion: **Tab inserts it into the prompt** (never sends), Escape or typing dismisses; a **setting to turn it off**. Confirmed by the owner. | Quietest way to offer a next message; a chip competes with the tray. |
| U4a | `ToolSearch` rows are **folded into one muted "Loaded tools" row per run**, expandable, not hidden. | Hiding what the agent did costs trust; folding removes the noise. |
| U4b | Order: **tasks and api first** (most calls), then git, agenthub, pr, `workspace.requestWrite`. | Call counts in section 3. |
| U5 | **Frontend-only** "didn't finish" state; stored rows are not rewritten. | Rewriting history is not worth a spinner fix. |

## 6. Not planned

A second tab strip or sidebar (AGENTS.md); changes to the harness or the permission cards; other vendors' events (a survey of opencode and Codex sessions is a separate
step if the owner wants parity); surfacing the vendor's `air.agentFileChangeReport` (already used by checkpoints).
