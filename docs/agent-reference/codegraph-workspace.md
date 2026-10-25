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

