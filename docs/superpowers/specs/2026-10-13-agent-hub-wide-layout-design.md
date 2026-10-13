# Agent Hub on wide screens — sidebar, session summary, rich results

Status: approved in conversation 2026-10-13 (mockups in `.superpowers/brainstorm/`,
`sidebar-b-v2.html` and `mainview-v1.html`), awaiting spec review.
Part of the UI/UX refactor on branch `feat/mono-chrome-color-palettes`; builds on
the Mono chrome (`2026-10-13-mono-chrome-color-palettes-design.md`).

## Problem

- The conversation is locked to a 720px column (`max-w-180`) in the middle of the
  document. On a 1440–2560px window most of the width is empty.
- Session context (subagents, tasks, changed repos, commands) is squeezed into the
  composer tray, one panel at a time.
- The sidebar card is two lines at ~52px. It is hard to see which session needs
  the person. The "N older sessions" stack draws two filled layers that read as a
  heavy shadow in Mono.
- Links in agent messages do not open (`<a target="_blank">` is swallowed by the
  Wails WebView).
- DevKit tool results without a registered view fall back to a JSON block. That
  block is collapsed, inside the collapsed "work" fold of the turn. Database,
  Kafka, SSH, PDF and workspace-memory tools have no view.
- Tables cannot be resized: neither a database result in the transcript nor a
  markdown table in an answer.

## Goals

1. Use horizontal space on wide windows without hurting reading width.
2. Make "which session needs me" visible at a glance, and fit more sessions per screen.
3. Show what an agent did as readable results (tables, plans, terminal output),
   never as hidden JSON.
4. Feel calm and smooth: the reference style (quiet monochrome, small grey labels,
   superscript counts, no boxes), motion per `docs/dev-kit/smoothness.mdx`, and
   budgets per `docs/dev-kit/frontend-performance.mdx`.

## Non-goals

- Changing the composer's own layout (`AgentComposer`, `ComposerContextBar`)
  beyond what §3.4 says.
- Changing the shell sidebar column, tab strip or document groups
  (`docs/design/module-shell-integration.md`): Agent Hub keeps the shared sidebar
  and `useWorkbenchTabsStore` tabs.
- New vendors, new MCP tools, or changes to what tools return. Views read the
  `rawInput` / `rawOutput` the transcript already has.

## Delivery

Two phases. Each phase gets its own implementation plan and ships working software.

- **Phase 1 — layout:** §1 sidebar, §2 backend preview, §3 main view and summary
  card, §4 links and paths.
- **Phase 2 — rich results:** §5 resizable tables, §6 tool result views, §7 results
  outside the fold.

---

## 1. Sidebar (`modules/agenthub/sidebar/`)

### 1.1 Row

One row per session, **fixed height 50px** (smoothness 3.1), radius 12px, no
border, no box.

```
[avatar 30px]  Title ·························· age
               service · second line
```

- **Avatar:** a 30px circle on `--surface-base` with a 1px inset hairline
  (`--border-subtle`). It carries the vendor mark (`VendorIcon`, vendor colour from
  `vendors.ts`).
- **Status on the avatar:**
  - running: a 1.5px arc that rotates around the circle (1.4s linear,
    `--text-primary`);
  - waiting on the person (permission or question): a 10px `--status-warning` dot
    at the bottom-right, with a 2px ring in the row's ground colour, and a 2.4s
    opacity "breathe";
  - error: the same dot in `--status-danger`, still;
  - idle or closed: nothing.

  Reduced Motion stops both animations; the glyphs stay.
- **Title:** `text-body-13`, one line, truncated. `--text-primary` for selected,
  running and waiting rows; `--text-secondary` for idle; `--text-tertiary` for
  closed.
- **Age:** `text-caption-11`, `--text-tertiary` (as the mockup's `--t4`), tabular
  numbers. On hover or focus it gives its place to the `⋯` menu (the existing
  `DropdownMenu`; the context menu is unchanged).
- **Second line** (`text-body-12`, `--text-tertiary`, one line, truncated), first
  match wins:
  1. A pending permission: its title (the request's tool title), in
     `--status-warning`.
  2. A pending question: the elicitation's message, in `--status-warning`.
  3. Status `error`: the session preview (§2), in `--status-danger`.
  4. Otherwise the session preview (§2).

  When the session's workspace has more than one folder, the line is prefixed with
  the folder label (`projectLabel(session.projectPath)`), then `·`, in
  `--text-tertiary` at 70% opacity. With no preview and nothing pending, the line
  shows only the folder label, or stays empty. The row keeps its 50px height
  either way.
- **Selected:** `bg-surface-raised`. Hover: `bg-state-soft`, `transition-colors`
  `--motion-fast`.
- **Subagent avatars, pin and child marker** move into the tooltip summary and the
  `⋯` menu. The row stays two lines with no extra icons.

### 1.2 Groups and workspace

Top to bottom:

1. **"Needs you"** (`Cần bạn` in the Vietnamese copy; the app's UI language decides
   the string). It lists every session, in **any** workspace, that waits on a
   permission or question or is in `error`, ordered by `updatedAt` descending. It is
   hidden when empty. A session listed here does not also appear in its own group.
2. **The current workspace** (the title-bar workspace switcher,
   `useCurrentWorkspaceServices` / `currentFirst`). Its group is open by default.
3. **Other workspaces and Scratch.** Each is collapsed to one 32px line:
   `name ⁿ` plus `● N running` when N > 0. Clicking expands it in place.

Group label: `text-caption-11`, `--text-tertiary`, with the count as a superscript
in `--text-tertiary` at 70%. The `＋` (new session in this group) appears on hover
or focus of the label line.

Inside a group the existing rules stay (`sidebar/grouping.ts`): the 5 most recent
are shown, plus the selected session and pinned sessions; the rest fold into the
stack.

### 1.3 Stack row

Replace the two filled layers with text and a hairline:
`17 older sessions ─────────── ⌄`, at 32px, `--text-tertiary`. Opening is a grid
`0fr → 1fr` transition (180ms; close 120ms; no JS height measurement).

### 1.4 Motion

| What | Motion |
|---|---|
| Row hover/selected | `transition-colors` `--motion-fast` |
| Session moves between groups (enters/leaves "Needs you", changes status order) | `@formkit/auto-animate` on each group's list, 180ms, `--ease-out-strong` |
| Group expand/collapse, stack open | grid track `0fr→1fr`, 180ms open / 120ms close |
| Running arc, waiting breathe | CSS keyframes on `transform` / `opacity` only |

Nothing animates on first paint: the rail has its final shape in the module's
first frame (smoothness 2.5).

### 1.5 Performance

- Each row subscribes to its own session and its own pending/elicitation entries
  by id (`useShallow` / per-id selectors). It never subscribes to the whole list.
- The second line of a running, open session must not change per token: it reads
  the persisted preview, which changes at turn end.
- Over 200 sessions in one expanded group, the list virtualizes
  (`@tanstack/react-virtual`, fixed 50px rows).

## 2. Backend: session preview

A short, persisted line saying where a session got to, so a session that is not
open still has a second line.

- **Storage:** migration 41 adds `agent_sessions.preview TEXT NOT NULL DEFAULT ''`
  (forward-only, no outer transaction; `internal/storage`).
- **Writer:** `agenthub.Service` sets it when a turn ends:
  - completed: the first sentence of the agent's final message, whitespace folded
    to one line, clipped to 140 runes with `…`;
  - failed (`ErrTurnFailed`, `ErrAgentUsageLimit`, process failure): the vendor's
    own sentence (`VendorError.Message`) or the error's user message.

  It is written in the same update that changes `status`, so `updatedAt` moves
  once.
- **Wire:** `AgentSession.preview?: string` (Go struct, bridge, bindings,
  `frontend/src/bridge/agenthub.ts`).
- **Privacy:** the preview is agent output and is stored like the transcript (plain
  SQLite, outside the vault). It is not sent over MCP, and audit lines never carry
  it.
- **Tests:** Go tests for clipping (runes, not bytes), sentence cut, failure text,
  one write per turn end, and the migration on an existing database.

## 3. Main view (`modules/agenthub/surfaces/AgentHubContent.tsx`, `TranscriptStream.tsx`)

### 3.1 Column

- The conversation column is **760px** (`max-w-190`), replacing `max-w-180`.
- When the summary card is docked (§3.2), the column is offset left by half the
  card's footprint (card 280px + 24px gap), so the column's right edge never runs
  under the card. Otherwise it stays centred.
- The composer region uses the same column width and offset.

### 3.2 Session summary card

A floating card at the top-right of the document area, in the style of the Codex
"Outputs / Sources" card.

- **Look:** 280px wide, radius 16px, `bg-surface-base`, `shadow-canvas-popover`
  plus a 1px `--border-subtle` ring, padding 6px 0. Sections are separated by a
  hairline. Each section header is `text-body-13` `--text-secondary` with a
  superscript count and an optional trailing action (`＋`, `↗`). Each item is a
  30px row.
- **Docked** when the document area's width is ≥ 1200px (container query on the
  document card, not on the window). Below that it collapses to a 32px round
  button at the same corner (an icon, plus a count of items needing attention).
  The button opens the same content as a popover (`PopoverContent`,
  `--motion-base` in, `--motion-fast` out).
- **Height:** follows content. If it would exceed the area, the card scrolls
  inside itself (max height = area height − 32px).
- **Sections**, top to bottom. An empty section is not rendered:

  | Section | Source | Item | Action |
  |---|---|---|---|
  | Đang làm / Plan | the session's latest `plan` item (`turns.ts` keeps one live plan per turn) | check circle (done / current / pending) + step text; done steps in `--text-tertiary` | — |
  | Thay đổi / Changes | union of `ChangedFile`s over the session's checkpoints (`AgentHubListCheckpoints`, `checkpoints.ts`), newest state per path | path + `+a −d` (`--status-success` / `--status-danger`) | row → existing `FileDiffPane` for that checkpoint; header `↗` → the turn-changes view |
  | Nguồn / Sources | (a) the session's PDF documents (new bridge `AgentHubListDocuments(sessionId)` → handle, name, pages, from the existing documents store); (b) DevKit profiles the session's tool calls used (`db.*`, `kafka.*`, `ssh.*`: profile name + count); (c) http(s) URLs in user messages and in `web`/fetch tool inputs, as hosts | glyph + name + count | row → open (PDF: the document; profile: `dispatchIntent` to its module; URL: §4) |
  | Subagent | `useAgentSessionsStore.subagents[sessionId]` + `sessionRunningSubagents` | name + state | row → `openSubagent` |
  | Context | the existing `AgentContextMeter` data (usage, turns, cost) | a 4px meter + `42% · 12 lượt · $0.18` | row → the existing meter popover |

- Each section shows at most 5 items, then a `Xem tất cả (N)` row that expands the
  section in place (grid `0fr→1fr`).
- **Motion:** the card enters with opacity + `translateY(8px)` over `--motion-base`
  the first time a session view shows it, and not again on tab switches (the view
  stays mounted). New items enter via auto-animate (180ms). A plan step turning
  done is `transition-colors` `--motion-fast`. Reduced Motion: none of it.
- **Performance:** the card is its own component tree, subscribed per session. It
  re-derives Sources and Plan from transcript items at most once per transcript
  publish (the transcript already batches to one publish per frame), with the
  derivation memoised on the items array identity. Checkpoints come from TanStack
  Query keyed by session, invalidated on the existing checkpoint event.

### 3.3 Transcript details

- **Attachments** (files and images the person attached) render as cards above the
  user bubble: a 36px tile (file type glyph; PDF in `--status-danger`), the name
  (`text-body-13`) and the kind and size or pages (`text-body-12`
  `--text-tertiary`). Clicking one opens it.
- **The work fold** (`AgentTurn` `step-group`) reads `Đã làm trong 49s ›` (elapsed
  time of the turn's work), followed by a 1px hairline under it across the column.

### 3.4 Composer tray

When the card is docked, the tray strip drops the **subagents** chip (the card
shows the same information). The tasks, repos and commands chips stay. When the
card is collapsed, the tray is unchanged.

## 4. Links and paths

- **Markdown links** in `AgentMessageRich` render as a `<button>` styled as a link
  (`text-text-primary`, underline in `--text-tertiary`, offset 3px; hover underline
  `--text-primary`). On click: http(s) and mailto open with
  `Browser.OpenURL(href)` from `@wailsio/runtime`, which opens the system browser.
  `safeHref` keeps rejecting every other scheme. Hover shows the full URL in a
  tooltip.
- **Bare URLs** in text are linkified the same way (marked GFM autolinks; verify
  they arrive as `link` tokens).
- **Paths:** inline code that is a workspace-relative or absolute path, optionally
  with `:line` or `:line:col`, becomes clickable. It calls the existing
  `onOpenFile(path, line)` (CodeGraph editor). The match is conservative: a
  `/`-containing token with a known source extension, or a path that
  `AgentHubListContextFiles` already reported. Anything else stays plain code.
- **Server mode:** `Browser.OpenURL` may be unavailable. On failure, copy the URL
  and show a toast "Link copied".

---

## 5. Resizable tables (shared)

- Extract the column-width rules from `modules/database/grid/gridColumns.ts`
  (`DEFAULT_COLUMN_WIDTH_PX`, `MIN_COLUMN_WIDTH_PX`, `resizeColumn`,
  `measureColumns`) into `components/ui/data-table/columns.ts`. `ResultGrid`
  imports from there, with no behaviour change; its tests stay green.
- New `components/ui/data-table/ResizableTable.tsx`:
  - `table-layout: fixed`, with a `<colgroup>` driven by widths;
  - a 4px `cursor-col-resize` handle on each header cell's right edge, using
    pointer capture: the width updates on pointer move through a rAF-throttled
    local state, and only the `<colgroup>` re-renders;
  - double-click on the handle auto-fits that column (`measureColumns` on the
    rendered cells);
  - keyboard: a focused handle moves 16px per ←/→ (Shift ×4);
  - horizontal scroll when the total exceeds the container;
  - rows over 100 virtualize via the existing `useVirtualRows`;
  - widths live in component state, keyed by the table's identity (tool call id or
    message id + table index). They survive re-renders and tab switches (the view
    stays mounted). They are not persisted.
- **Used by** the `db.query` view (§6) and markdown tables in `AgentMessageRich`
  (replaces the plain `<table>`, keeping its typography and first-column emphasis).

## 6. Tool result views (Phase 2)

Registered with `registerToolResultView` from the owning module's manifest (as
git, api and codegraph already do). Each reads only `rawInput` / `rawOutput`. If
the output does not parse as expected, the view returns `null`, so the generic
block shows: never a broken card.

| Tool | View | Owner |
|---|---|---|
| `db.query` | header: profile, env tag chip, row count, duration. A `ResizableTable` of the first 20 rows. Actions: **Mở rộng** (all returned rows in place, virtualized), **Copy CSV**, **Mở trong Database** (`dispatchIntent` to a Database console tab with the SQL and profile). Shows `truncated` when the stored row was previewed (8 KiB transcript preview) and offers the existing load-full-row path | database |
| `db.explain` | the Database module's `PlanDiagram`, compact height, plus **Mở trong Database** | database |
| `db.schema.get`, `db.listForeignKeys` | a table list; expanding a table shows its columns (name, type, PK/FK marks in `--hue-yellow` / `--hue-blue`) | database |
| `kafka.peekMessages` | message rows (partition, offset, key, value preview ≤ 200 chars, timestamp), expand for the full value | kafka |
| `kafka.listTopics`, `kafka.describeTopic`, `kafka.listConsumerGroups` | compact tables via `ResizableTable` | kafka |
| `ssh.runCommand` | a terminal-styled block: command, output (mono, max 20 lines, expand), and the exit code (`--status-success` for 0, `--status-danger` otherwise) | ssh |
| `pdf.viewPages` | page thumbnails (the image blocks the tool returned), click → full size | agenthub |
| `pdf.search`, `pdf.readText` | hits / text excerpt with page numbers | agenthub |
| `workspace.memory.*` | an entry card: kind, title, summary, revision | agenthub |

## 7. Results outside the fold (Phase 2)

A tool call whose result has a registered view (`renderToolResult` returns
non-null) renders **after** the turn's work fold, in call order, as a standalone
result card. Inside the fold it is still listed as a one-line row. All other tool
calls stay inside the fold, as today.

Cap: at most 3 surfaced results per turn. The rest stay in the fold, with a line
`+N kết quả khác trong phần đã làm`.

---

## Accessibility

- Rows keep the existing button/`aria-describedby` structure. Status is spoken in
  the description, never by colour alone.
- The summary card is a `<section aria-label="Session summary">`. The collapsed
  button has `aria-expanded`.
- Column resize handles are focusable `role="separator"` with
  `aria-orientation="vertical"` and `aria-valuenow` (width).
- Focus rings use `--border-focus` (Mono rules).

## Testing

- **Unit (bun):**
  - second-line precedence;
  - the "Needs you" membership rule;
  - workspace collapse rule;
  - summary section derivations (plan, changes union, sources: profiles, URLs,
    PDFs);
  - path matcher;
  - link click calls `Browser.OpenURL`;
  - `columns.ts` (moved tests);
  - each tool view: renders for a fixture, returns null on malformed output;
  - surfacing cap.
- **SSR first-frame tests** (smoothness §9): the sidebar renders final rows with a
  populated store; the summary card renders no empty section.
- **Go:** migration 41, preview writer (§2).
- **Ratchets stay green:** design-system conformance, smoothness, store
  subscription, module boundary (tool views live in their owning module; shared
  table code in `components/ui`).
- **Performance:** `transcriptPerformance.test.ts` and
  `TranscriptStream.performance.test.tsx` budgets unchanged. A new test checks
  that a 1,000-token burst produces at most one summary-card derivation per
  publish.
- **Visual:** server-mode screenshots, light and dark, at window widths 1280,
  1600 and 2200:
  - the card docked versus collapsed;
  - the sidebar with "Needs you";
  - a `db.query` result with a resized column.

## Risks

- **Summary card and transcript compete for width between 1200 and ~1350px.** The
  container-query threshold is the knob; verify in screenshots.
- **The preview is only as good as the agent's first sentence.** Clipping keeps it
  short. A poor sentence is still better than a blank line.
- **Surfacing results can lengthen a turn.** The cap of 3 limits that, and each
  card is compact (20-row preview).
