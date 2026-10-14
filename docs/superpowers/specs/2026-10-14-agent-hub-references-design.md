# Agent Hub phase 1b — references that open, preview and explain themselves

Status: approved in conversation 2026-10-14. Follows
`2026-10-13-agent-hub-wide-layout-design.md` (Phase 1). Branch
`feat/mono-chrome-color-palettes`.

## Problem

An agent writes `handler.lua:27`, `[session.go](internal/auth/session.go#L88)`,
`SessionTTL` or `a1b2c3d`. Today:

- **Inline-code file refs** are sent verbatim to `codegraph.reveal`. That intent
  resolves the path against the **title-bar** workspace's folders, not the
  session's, and joins a bare name onto `projectPath`. When the file is not
  there, it opens a tab that shows a read error, and nobody is told.
- **Markdown links to files** (no scheme, or `file:`) go to `openExternal`, which
  only allows `http(s)` and `mailto`, so clicking does nothing.
- **Previews:** there is no way to look at a reference without opening a tab.
- **Context menus:** there is none.
- **Symbols and commit hashes** are plain code.

## Goals

1. Every file reference opens the right file at the right line, or says why it
   cannot.
2. A short look is possible without leaving the conversation: a shadcn
   `HoverCard` preview.
3. Right-click gives the usual verbs.
4. Symbols and commits written in code spans become references when they really
   exist.

Out of scope (Phase 2): DB tables and Kafka topics as references. They are only
reliable once tool results are known.

## 1. Parsing (`transcript/fileRef.ts`, extended)

`fileRef(text)` accepts:

- `a/b.go`, `./a/b.go`, `/abs/a/b.go`, `a/b.go:27`, `a/b.go:27:3`, `a/b.go#L27`,
  `a/b.go#L27-L30` (line = 27);
- `Dockerfile`, `Makefile`, `Taskfile.yml`, `go.mod`, `package.json`: well-known
  extensionless and dot names, through an allow-list.

It rejects:

- URLs (anything with `scheme://`);
- whitespace;
- version-like and identifier-like tokens;
- `..` segments that escape (`../x` is allowed only as relative to the project
  folder; see §2).

`linkTarget(href)` classifies a markdown link `href` as one of:

- `{kind:"url", url}` for `http(s)` and `mailto`;
- `{kind:"file", ref}` for no scheme, or `file://`, where `fileRef` parses the
  path part (`file://` is stripped and the path percent-decoded);
- `{kind:"other"}`.

## 2. Resolution (`transcript/resolveFileRef.ts`, new)

Context comes from the session: `{sessionId, workspaceId, projectPath}`. The
folders are `listWorkspaceServices(workspaceId)`, which gives `{id, path}`.

`resolveFileRef(ref, ctx, deps)` returns one of:

- `{kind:"found", target}`, where
  `target = {workspaceId, folderId, folderName, rel, line?}`;
- `{kind:"ambiguous", targets}`, capped at 10;
- `{kind:"missing"}`;
- `{kind:"unsupported"}`, when the session has no workspace (scratch).

Order of attempts:

1. **Absolute path:** take the deepest folder that contains it, then
   `statFolderFile`. If the file exists, found; otherwise missing.
2. **Relative to the session's project folder** (the folder that contains
   `projectPath`, with `rel = join(projectRel, path)`), then `statFolderFile`.
3. **Relative to each folder root:** `statFolderFile` on each, collecting hits.
4. **By name:** `agentHubListContextFiles(sessionId, basename)` (absolute paths,
   ≤100). Keep the paths ending in `"/" + ref.path`, then map them to folders.

The first step that yields any hits wins. Hits are deduplicated by
`folderId + rel`. One hit is found; more than one is ambiguous; none is
missing.

Results are cached with TanStack Query, key
`["agenthub","fileRef",sessionId,ref.path]`, `staleTime` 30 s. A missing result
is not cached beyond the query's own staleTime.

Stat errors with `unreadable` (`binary`, `too_large`) still count as found. The
preview says why it cannot show the file.

## 3. Opening (`codegraph.reveal` widened; `transcript/openReference.ts`)

`EntityRef` gains optional `workspaceId` and `folderId`. For `kind:"file"`, when
both are set, `codegraph.reveal` calls `openFile({workspaceId, folderId, path:
ref.id}, line, {preview:false, beside})` directly (any workspace, as `openFile`
already allows) and switches the Code Graph sidebar to Files. Otherwise the old
behaviour is unchanged.

`openFileRef(ref, ctx, {placement})`:

- **found:** dispatch `codegraph.reveal` with the ids.
- **ambiguous:** open a small picker anchored to the clicked element. It is a
  Popover listing `folderName · rel` and choosing one opens it.
- **missing:** `toast.error` with `Couldn't find <ref.path> in this workspace`.
- **unsupported:** fall back to the old `codegraph.reveal` with `project`.

Placement defaults to `"beside"`, so the session stays on screen.

`AgentHubView.handleOpenFile` and `AgentCompanion.openFile` both go through
`openFileRef`. Tool-call file chips keep using `onOpenFile` with absolute
locations; that path is unchanged.

## 4. Preview (`transcript/ReferencePreview.tsx`, new)

This uses shadcn `HoverCard` with `openDelay` 400 and `closeDelay` 120. It
follows the ModuleSwitcher pattern: `HoverCardTrigger asChild` wrapping
`ContextMenuTrigger asChild`, and the hover card closes when the context menu
opens.

- **File:** a header with `rel` (mono, truncated from the left: file name
  bold, folder dim) and `folderName`.
  - **Body:** ±4 lines around `line`, or the first 9 lines when there is no
    line. There is a line-number gutter and the target line has a raised
    background. Highlighting comes from `useHighlight` + `renderHighlighted`
    (`components/code/CodeBlock.tsx`) on the slice, language from
    `languageForPath`.
  - **Footer:** `Open` (beside) and `Open here`.
  - **Data:** `readFolderFile` through TanStack Query, key
    `["agenthub","fileRefRead",workspaceId,folderId,rel]`, `staleTime` 30 s.
    The read starts only when the card opens.
  - **States:** "Binary file" or "Too large to preview" from stat or read
    errors; "Not found in this workspace"; for ambiguous refs, the candidate
    list.
- **URL:** full URL (wrapping), host, and `Open in browser` and `Copy link`
  buttons. Nothing is fetched.
- **Symbol:** as for a file, at the definition line, with the kind
  (class/method) in the header.
- **Commit:** short sha, and `Open in Git`. Nothing is fetched (§6).
- **Look:** width `w-96`, the existing `HoverCardContent` material (glass-float,
  rounded), `text-code-12`. It enters and leaves with the Radix data-state
  animations already on the component.

## 5. Context menu

- **File:** Open beside · Open here · Copy path · Copy path:line · Reveal in
  Files. "Reveal in Files" opens the file here and focuses the Files tab.
- **URL:** Open in browser · Copy link.
- **Symbol:** Go to definition · Copy name.
- **Commit:** Open in Git · Copy SHA.

## 6. Symbols and commits (inline code only)

- **When.** Only for a message whose turn has finished (not live). Code spans
  of a live message stay plain, so nothing jumps while text streams. Each
  message is evaluated once, then cached.
- **Symbols.** A code span counts if it:
  - matches `/^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)*(\(\))?$/`;
  - is 3 to 80 characters long;
  - is not a reserved word (`null`, `true`, `false`, `nil`, `undefined`,
    `this`, `self`, `return` and similar).

  At most 20 distinct names per message are looked up with
  `searchWorkspaceSymbols(workspaceId, [], last segment)`, three at a time.
  A match is exact: the label equals the span, or the label's last segment
  equals the span's last segment and the span's qualifier is a suffix of the
  label. Exactly one exact match makes the span a reference. Cache key:
  `["agenthub","symbol",workspaceId,name]`, `staleTime` 5 min.
- **Commits.** A code span counts if it:
  - matches `/^[0-9a-f]{7,40}$/`;
  - contains at least one digit and at least one letter.

  Clicking pins Git to the session's project folder (`pinWorkFolder("git",
  projectFolderPath)`), then dispatches `git.reveal {kind:"commit", id: sha}`.
  Nothing is fetched on hover; pinning is a side effect, so it happens only on
  click.
- **Look.** A resolved symbol or commit has a dotted underline
  (`decoration-dotted`). A file ref has a solid underline.

## 7. Phase-1 visual fixes folded in

1. **Asks on the second line:** strip markdown code ticks in the sidebar's ask
   text (`secondLine`), so `` Run `rm -rf build`? `` reads as
   `Run rm -rf build?`.
2. **Plan shown twice when docked:** at `@6xl/agent-conversation`, the
   transcript shows the **latest** plan card as a single quiet line,
   `Plan · 2 of 4 — in the summary`, because the summary card shows it in
   full. Older turns' plans are untouched.
3. **Wide windows:** at `@7xl/agent-conversation` (80rem) the column grows to
   `max-w-220` (880px), the top of the reading range, so the gap before the
   card narrows.

## Performance

- Parsing happens in render. It is cheap, a regex per code span.
- Resolution and reads happen on hover or click only, except the symbol batch,
  which runs once per finished message and is capped. Everything is cached
  with TanStack Query and nothing is refetched on focus.
- HoverCards mount their content only when open.

## Testing

- **Unit:** `fileRef` v2, `linkTarget`, `resolveFileRef` (fake deps: absolute,
  project-relative, folder-relative, by-name unique/ambiguous/missing,
  unsupported), symbol and commit matchers, exact-match rule, the ask cleaner.
- **Render (renderToString):**
  - each reference kind gets its testid and underline style;
  - a live message has no symbol links;
  - the HoverCard trigger wraps the reference;
  - context-menu items are built from a pure helper, tested directly.
- **Codegraph:** `codegraph.reveal` with ids calls `openFile` with those ids.
- **Visual:** the dev page used in Phase 1 (scratch, not committed), plus a
  hover and a right-click screenshot.

## Decisions while building

What shipped differs from the sections above as follows (commits
`9c3d8b70..be66ff49`).

**Parsing (§1)**
- `fileRef` also reads `:27-30` and `:27:3` (line 27), and drops a leading
  `./`. Prose names like `Node.js` are never files. The known names include
  `go.sum`, `tsconfig.json`, `Cargo.toml`, `pom.xml`, `build.gradle(.kts)` and
  `.env*`.
- `linkTarget` reads `file://localhost/…` as `/…`. A malformed escape is
  `other`. A link DevKit will not open renders as plain text, with its href in
  a tooltip.

**Resolution (§2)**
- **Path-only cache.** The resolver never puts a line on a target. The query
  (`fileRefQuery`) is keyed and fetched on `ref.path` alone, and `withLine`
  applies the reference's own line afterwards (`select` in the hook, and again
  in `openFileRef`). One resolution serves `a.go:10` and `a.go:90`.
- **Failures are not cached.** A failed folder lookup throws, so the query
  retries and no wrong "missing" is stored. A stat that rejects counts as
  "not here" for that candidate only.
- **By name probes little.** Candidates are deduplicated and the collection
  stops at one more than the ambiguous cap, so at most 11 stats, never 100.
  A leading `../` is dropped for the suffix match. A path that climbs out of
  a folder root is not tried against the roots.
- `not_regular` is carried as `unreadable` alongside `binary` and `too_large`.

**Opening (§3)**
- **An intent may return `"unavailable"` instead of throwing.**
  `dispatchIntent` passes it on. `codegraph.reveal` answers it when no folder
  holds a project path (`revealFile.ts`, `revealFileRef`). Returning keeps the
  many callers that `void` a dispatch free of unhandled rejections.
  `openFileRef` toasts it as "Couldn't find …", and toasts a throw as
  "Couldn't open …: <reason>".
- Ambiguous with no transcript on screen to anchor a picker toasts "Several
  files match …". The picker (`RefPicker.tsx`) anchors at a wrapped
  reference's first line, is labelled by its heading, takes Home/End, and
  returns focus to the reference (or the scroller if it re-rendered away).
- **Placement per surface.** `TranscriptActions.refPlacement` is `beside` in
  the Hub and `replace` in the companion rail, through `refOpener`. The card's
  Open and a picked candidate use it. Only the menu's Open beside / Open here
  name a place.

**Preview (§4)**
- **Pointer-only hover cards.** Only a resting pointer opens one: not focus,
  not a press, not while its context menu is open. A click closes it.
  Keyboard users open a reference with Enter (Space too on a code span) and
  get its menu from the context-menu key or Shift+F10; the trigger announces
  the menu (`aria-haspopup`).
- **A fixed nine-row body.** Every state fills the same box (code, skeleton,
  notes, the candidate list) above a footer that is always there, so the card
  never resizes. The header shows the path as written until the resolved one
  arrives. A line past the end raises the last line and says `line N of M`.
  CRLF reads as LF, tabs are two wide.
- **Highlighting** covers the whole file once, memoised, then the slice is
  cut. Past the highlighter's size limit, it covers ±200 lines around the
  target, so a comment or string opened above the slice keeps its colour.
- **Reads** go stale after 5 s (not 30 s) and leave memory 30 s after the
  card closes (`gcTime`). A preview of a file being edited stays current.
- The footer is `Open here` and `Open`. An unsupported (scratch) ref offers
  `Open in Code Graph`, the project fallback. The commit card's button says
  which folder it switches Git to.
- Menu rows are built only when a menu opens.

**Symbols and commits (§6)**
- **Cap 12, limiter 3.** At most 12 distinct names per message, not 20. The
  three-at-a-time limit is one queue for the whole app (`createLimiter`), not
  per message. A query whose message unmounts aborts and leaves the queue
  before it runs.
- **Short-name rule.** A bare name is looked up only if its last segment is
  at least 4 characters, or it is qualified, a call (`()`), or camelCase
  (`worthLookingUp`). `get` or `run` matches half a codebase and names none
  of it. File refs and commit-shaped spans are never looked up as symbols.
- **Not inside links.** Code in a link's text is the link's text: no file ref,
  symbol or commit is made there, and it is not counted against the cap.
- **While indexing.** A miss while any folder is `indexing`, `not-indexed` or
  `failed` is kept 15 s, not 5 min, so the reference appears once the index
  is ready.
- An exact match ignores a label's parameter list (`invoke(String)`).
  Workspace folders for the symbol header and the commit pin are one shared
  query (30 s).
- A commit becomes a reference only when the session has a project path to
  pin Git to.

**Summary sources**
- The summary's Sources count only tools that touched a target's data. The
  allow-list is `DATA_TOOLS`: `db.query`, `db.explain`, `db.schema.get`,
  `db.listForeignKeys`, the Kafka reads and `publishMessage`, and
  `ssh.runCommand`. Listing profiles, testing a connection or reading history
  is not a source.
- Each source says what was done: "2 queries", "read the schema",
  "published 1", "peeked 2 topics", "ran 3 commands". Codex's untitled MCP
  calls are read from their `devkit.<tool>` title.

**Phase-1 fixes (§7)**
- The ask cleaner (`plainAsk`) drops code ticks. It drops `**`/`__` only where
  they wrap words, so a path or glob containing them stays intact.
- The docked plan line appears only where the summary docks: the Hub, for a
  non-draft session (`planDocked`). The rail has the container query but no
  dock, so its plan stays whole. An empty plan keeps its card.
