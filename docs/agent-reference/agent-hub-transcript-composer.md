- **References in agent prose resolve, preview and open** (spec
  `docs/superpowers/specs/2026-10-14-agent-hub-references-design.md`, last section = what changed;
  files under `frontend/src/modules/agenthub/transcript/`).
  - Parsing is pure: `fileRef.ts` takes `a/b.go`, `./`, absolute, `:27`, `:27-30`, `:27:3`, `#L27(-L30)`
    and an allow-list of known names (`Dockerfile`, `go.mod`, …), never URLs, versions or escaping `..`;
    a segment that is only `...` or `…` is an **elision** (`core/.../DispatchFunction.java`, how an answer fits a long path
    in a table cell): `hasElision` / `elisionPattern`, and the resolver (step 0) never stats it as a path — it searches the
    file name and keeps the files whose path fits the written ends, any number of folders between;
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
- **A ```mermaid fence in an agent's answer is drawn as the diagram** (`transcript/AgentMermaidBlock.tsx`, shared
  renderer `components/mermaid/useMermaid.ts`, also Notes' blocks). It is the ordinary code block until the message has
  settled (`CodeRefsContext.settled`: half a diagram never renders) and whenever Mermaid refuses the source or fails to
  load — the same look as before diagrams existed; once drawn, Source / Diagram toggles and Copy copies the source.
  Renders run one at a time (Mermaid is configured globally per render), are cached by family/theme/source, and use
  the app font and theme tokens. `useOptionalTheme` lets a surface with no `ThemeProvider` draw in the dark palette
  instead of throwing. A file reference chip has no native `title` (the hover card says where it goes, and the
  tooltip drew a second one over the text); the card header's full path is a title only when it is cut off.
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
  (Go: `file_drop.go`, `EnableFileDrop` + `data-file-drop-target` on the composer) and become the same chips
  as `@` makes (next bullet).
- **A file, an image and a reference are chips in the draft, and nowhere else** (spec
  `docs/superpowers/specs/2026-10-19-composer-attachments-design.md`; `modules/agenthub/composerDraft.ts`,
  `surfaces/composer/attachments.tsx`, `AttachmentChip.tsx`). There is no strip of attachments and no
  `attachments` / `images` / `refs` state in `AgentHubView` or `AgentCompanion`: `promptBySession` is still a
  string, and a chip is written into it as a token of private-use characters (`U+E000 kind:key|label U+E001`,
  `chipToken`). `parseDraft` gives the text the agent reads (`@label` per chip — what `@` always put in the
  text) and `turnFromDraft` the lists a turn carries; the turn pipeline (`TurnInput`, `queuedPrompt`,
  `retryTurnFor`, `shouldSteer`) still takes arrays and did not change. So the Hub and the rail composers of
  one session share everything, a refused first turn puts the whole draft (chips included) back in the field,
  and a queued prompt keeps its `draft` so "edit queued" restores chips. Image bytes wait in
  `stores/composerImages.ts` by id (in memory; a chip whose bytes are gone is dropped at send). Everything that
  adds something — `@`, the `@` button, `+ → Attach files…`, an OS drop, a pasted image, `agent.attach` refs —
  inserts a chip at the caret (`insertAttachmentChips`; `appendChips` on the string when no editor is mounted).
  A long name keeps its end (`chipName.ts`); the hover card asks Go what the file is
  (`AgentHubDescribeFiles` → `DescribeAttachment`, `attachment_info.go`: size, kind, why it cannot be sent;
  `TestDescribeAgreesWithTheRefusals` keeps it in step with `attachmentBlock`). **The composer kit has no
  MarkdownPlugin**: a paste is read by `deserializeComposerValue` and comes back exactly as written (the
  Markdown reader turned `# T\n- a` into `Ta`); copy puts the prompt as the agent reads it on the clipboard as
  text and keeps the chips in the editor's own fragment. Slate takes a multi-format paste from `beforeinput`,
  not the `paste` event, so a browser check of it has to dispatch that (`?gallery#agenthub-composer-layout`,
  scenario "attachments").

- **A tool call its turn left pending or in progress is not drawn as live** (`transcript/toolSettle.tsx`). `TranscriptStream`
  gives each turn an `EarlierTurnProvider` (`earlier` is every turn but the newest, so a later user message exists); in an
  earlier turn a `pending`, `in_progress` or `awaiting_permission` call draws as "didn't finish" (hollow dot, no spinner;
  the activity card says "Didn't finish" instead of its present-participle verb; no command clock), whatever the session
  is doing now. The newest turn is left as its status says: a vendor-started turn or a background subagent produces updates
  with no turn event while the stored status stays idle, so the status cannot settle it. A cancelled newest turn whose call
  stays `in_progress` therefore keeps spinning until the roadmap's U1 (session Activity) can settle it. Stored rows are not
  rewritten. A failed row with no error line of its own shows the first line of what it said (`failureLine`).
