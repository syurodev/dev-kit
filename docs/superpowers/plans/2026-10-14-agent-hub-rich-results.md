# Agent Hub Phase 2 — Rich Results Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agent tool results read as what they are (tables, plans, messages, terminal output, pages, memory entries) instead of hidden JSON. Every table can be resized. Results with a view show outside the work fold. DB tables and Kafka topics named in prose become references. This is §5–§7 of `docs/superpowers/specs/2026-10-13-agent-hub-wide-layout-design.md`, plus the item deferred from `docs/superpowers/specs/2026-10-14-agent-hub-references-design.md`.

**Architecture:**
- **Payload layer.** One `toolPayloadOf(call)` turns any vendor's tool call into `{name, payload | error}`, and every result view reads only that.
- **Table primitive.** One shared, resizable `DataTable` lives in `components/ui/data-table/`.
- **Module views.** Each owning module registers its views in its manifest, which keeps the module boundaries intact.
- **Placement.** The transcript draws done calls that have a view as standalone cards after the work fold, capped per turn.
- **Navigation.** New `database.reveal` and `kafka.reveal` intents let agenthub open tables and topics without importing those modules.

**Tech Stack:** React 19, TypeScript, TanStack Query, Radix via `components/ui`, `@tanstack/react-virtual` (`useVirtualRows`), Tailwind v4, bun test (renderToString), Go for one error-mapping fix.

## Findings that change the spec

- **F1. Claude MCP results are in `content`, not `rawOutput`.**
  - claude-agent-acp 0.88 omits `rawOutput` for AIR clients whenever content exists. The payload is `content[].content.text`: the JSON of `structuredContent`, wrapped in a ```` ``` ```` fence on error.
  - Codex puts `{content:[{type:"text",text}], structuredContent}` in `rawOutput`, gives errors as `{error}`, and replaces results over 64 KiB with a stub `{truncated:true, bytes}`.
  - Today's views read only `rawOutput`, so for Claude sessions they never render. That covers `api.send`, `git.*`, `codegraph.*`, `tasks.*` and `notes.*`.
- **F2. Codex MCP calls have no `name`.** The title is `devkit.<tool>`. `renderToolResult({name: call.name ?? ""})` therefore never matches a Codex call.
- **F3. Truncated previews.** Past 8 KiB the stored raw becomes a JSON string of its first bytes, and `ToolCall.truncated` is set. A view needs the full row from `agentHubToolCallDetail` (already used by "Show full output").
- **F4. Outputs carry no profile or env.**
  - `db.query`, `kafka.*` and `ssh.runCommand` results do not name the profile. A card cannot show it, so it does not.
  - `ssh.runCommand` returns `{output}` only: no exit code.
  - `db.query` cells are strings; there are no column types.
- **F5. No `database.*`, `kafka.*` or `ssh.*` intents exist.**
  - Inside its own module a card can call module code directly; `database/documents.ts` `openSavedQuery`-style helpers work, for example.
  - agenthub may not import those modules (`moduleBoundaryRatchet`), so references go through new intents.
- **F6 (Go). `bridge.publicError` turns `pdf.*` and `memory.*` AppErrors into `internal_error`.** It does this when they are raised inside `Caller.Call`, because `publicErrorForCode` only keeps three codes. Fix it, so agents and views see the real reason.

## Conventions (every task)

- Repo `/home/ptvu3/Downloads/dev-kit`, frontend commands run in `frontend/`, branch `feat/mono-chrome-color-palettes`.
- Work TDD: write the failing test, watch it fail, implement, watch it pass, then commit. Each commit message ends with the `Co-Authored-By` line for the model doing the work.
- Stage only the task's files. Never `git stash`, never `git checkout -- <file>`, never `--no-verify`, never push.
- Before each commit, run:
  - `bun test src/modules src/workbench src/components src/designSystemConformance.test.ts src/smoothnessRatchet.test.ts src/storeSubscriptionRatchet.test.ts src/moduleBoundaryRatchet.test.ts src/focusRing.test.ts`
  - `bunx tsc --noEmit -p .`
  - eslint on the changed files: no new errors (the repo baseline is 47).
- Views follow `docs/dev-kit/tool-result-view.mdx` (rules L52–92):
  - read only what the call carries; never fetch more, except the existing full-row load;
  - return `null` to fall back to the generic block;
  - the owning module registers the view.
- Cards use `components/ui/framed-card.tsx` (`FramedCard`, `FramedCardHeader {icon,title,meta,action}`, `FramedCardPreview`, `FramedCardRow`, `FramedCardMore`, `FramedCardAction`), as `modules/apiclient/SendResultCard.tsx` does. Register them with `lazy()` and check the shape synchronously before the chunk loads, as `apiclient/manifest.ts` L125–132 does.
- **Data shapes** (snake_case Go JSON; see F1–F4):
  - `db.query` → `{columns: string[], rows: (string|null)[][], row_count, truncated, rows_affected}`; the default cap is 200 rows.
  - `db.explain` → `{nodes:[{operation, depth, estimated_rows, actual_rows, self_millis, detail, children?}], raw, analyzed}`.
  - `db.schema.get` → `{namespace:{schema:{table:[col]}}, columns:{schema:{table:[{name,data_type,nullable,position}]}}, indexed_at}`.
  - `db.listForeignKeys` → `{result:[{schema,table,column,referenced_schema,referenced_table,referenced_column}]}`.
  - `kafka.peekMessages` → `{result:[{offset,key?,value,timestamp?}]}`; input `{topic, partition, limit?, offset?}`.
  - `kafka.listTopics` → `{result:[{name}]}`.
  - `kafka.describeTopic` → `{name, replication_factor, partitions:[{id}]}`.
  - `kafka.listConsumerGroups` → `{result:[{group_id, lag?}]}`.
  - `kafka.publishMessage` → `{partition, offset, timestamp?}`.
  - `ssh.runCommand` → `{output}`; input `{command}`.
  - `pdf.list` → `{documents:[…]}`.
  - `pdf.info` → `{document, pdf, notes}`.
  - `pdf.search` → `{documentId, query, result:{hits:[{page,snippet,match,zone?}], pagesSearched, complete, resumeFromPage?, truncated?}, notes}`.
  - `pdf.readText` and `pdf.viewPages` → **text content only**. viewPages: `content[0]` is a caption, then a JSON array of `{page,widthPx,heightPx,dpi,mimeType,region?}`; `content[1..]` are images, which the stored transcript drops (`transcript_slim.go`).
  - `workspace.memory.get|create|update` → `Entry` (`id, workspaceId, kind, title, summary, body, revision, deleted, createdAt, updatedAt, source`).
  - `workspace.memory.list|search` → `{entries: Summary[], hasMore, nextCursor?}`.

---

### Task 1: One payload for every vendor (`workbench/toolPayload.ts`)

**Files:**
- Create `frontend/src/workbench/toolPayload.ts` and `toolPayload.test.ts`.
- Modify `frontend/src/workbench/toolResultViews.ts` (`ToolResultInput` gains `content?: ContentBlock[]` and `status?: string`; `renderToolResult` resolves the name), `frontend/src/modules/agenthub/transcript/AgentToolCall.tsx` (call site ~L259) and `transcript/AgentToolActivity.tsx` (done branch).

- [ ] **Step 1: Failing tests.**
  - `toolNameOf(call)`:
    - `{name:"mcp__devkit__db_query"}` → `"mcp__devkit__db_query"` (the registry already resolves the wire name);
    - `{title:"devkit.db.query"}` with no name → `"db.query"`;
    - `{title:"Read file"}` → `""`.
  - `toolPayloadOf(call)` returns `{payload}` or `{error}` or `null`:
    - **Claude:** `content:[{type:"content", content:{type:"text", text:'{"columns":["a"],"rows":[["1"]],"row_count":1}'}}]` → payload object.
    - **Claude error:** text ```` "```\nquery_failed: relation \"x\" does not exist\n```" ```` with status failed → `{error:'query_failed: relation "x" does not exist'}`.
    - **Codex:** `rawOutput:{content:[{type:"text",text:"…"}], structuredContent:{…}}` → `structuredContent`.
    - **Codex error:** `rawOutput:{error:"boom"}` → `{error:"boom"}`.
    - **Codex stub:** `rawOutput:{truncated:true, bytes:70000}` → `null`.
    - **Plain JSON:** a `rawOutput` JSON string → parsed. A `{result:[…]}` envelope is unwrapped, as `parseToolOutput` does.
    - **Not JSON:** non-JSON text such as `pdf.readText` → `{payload: <the text>}` (string).
    - **Truncated preview:** a string that fails `JSON.parse` while `call.truncated` is set → `null`, so the caller loads the full row.
  - `renderToolResult` passes `rawOutput: payload`, `content: call.content blocks` and `status`. Existing views keep working because `parseToolOutput` passes objects through. Test that `api.send`'s `parseSendResult` accepts a Claude-shaped call through `toolPayloadOf`.
- [ ] **Step 2:** Run the tests. They must FAIL.
- [ ] **Step 3: Implement.**
  - Fence stripping: `/^```[a-z]*\n([\s\S]*?)\n```$/`.
  - Error detection:
    - status `failed`;
    - a Codex `{error}` object;
    - an `isError` flag;
    - a fenced text whose first token matches `^[a-z_.]+:` (AppError format).
  - In `AgentToolCall` and `AgentToolActivity`, build the input with `toolPayloadOf(fullCall ?? call)`. Pass `name: toolNameOf(call)`. When the result is an error, keep today's failed-row behaviour; views see only successful payloads.
  - Keep `parseToolOutput` exported, since existing views use it.
- [ ] **Step 4:** Run the tests. They must PASS. This also makes existing views render for Claude and Codex.
- [ ] **Step 5:** Commit `agent results: one payload whatever the vendor sends`.

### Task 2: Shared resizable `DataTable` (§5)

**Files:**
- Create `frontend/src/components/ui/data-table/columns.ts`: move `DEFAULT_COLUMN_WIDTH_PX`, `MIN_COLUMN_WIDTH_PX`, `resizeColumn` and `measureColumns` here from `modules/database/grid/gridColumns.ts`, and re-export them from gridColumns so ResultGrid is unchanged.
- Create `components/ui/data-table/DataTable.tsx` with tests.
- Modify `modules/agenthub/transcript/AgentMessageRich.tsx` (markdown `case "table"`, ~L356–410).

- [ ] **Step 1: Failing tests.**
  - The moved pure functions keep their existing gridColumns tests (move or duplicate the assertions).
  - `DataTable` render:
    - `table-layout: fixed` with a `<colgroup>`;
    - a resize handle per header, `role="separator" aria-orientation="vertical" aria-valuenow`;
    - rows over 100 use `useVirtualRows` (count `<tr>` under SSR: below 100 all are rendered, matching the existing hook);
    - `initialWidths` respected.
  - A pure `nextWidths(widths, index, delta)` (wraps `resizeColumn`) and `keyboardResize(widths, index, key, shift)`: ArrowLeft/Right ±16, ×4 with Shift.
  - A markdown table in prose renders a `DataTable` (`data-testid="agent-table"` kept), with first-column emphasis and alignment kept.
- [ ] **Step 2:** Run the tests. They must FAIL.
- [ ] **Step 3: Implement.**

  `DataTable` props:
  ```ts
  {
    columns: { key: string; header: ReactNode; align?: "left" | "center" | "right"; mono?: boolean }[];
    rows: ReactNode[][] | (string | null)[][];
    initialWidths?: number[];
    maxHeight?: string;   // a token class such as "max-h-80"
    dense?: boolean;
    caption?: string;
    onRowClick?(i: number): void;
  }
  ```
  - Resizing: the handle is a 4px `cursor-col-resize` strip on the right edge of each header cell. Use pointer capture; on move, update a rAF-throttled `useState` of widths. Only the `<colgroup>` depends on widths.
  - Double-click a handle to auto-fit: `measureColumns` on that column's cell strings.
  - Keyboard: a focused handle reacts to arrows.
  - Layout: horizontal scroll when the total is wider than the container; sticky header.
  - Widths are component state. They survive re-renders and are not persisted.
  - Look:
    - header `bg-(--state-soft) text-body-13-medium text-text-secondary`;
    - cells `px-3 py-1.5`, `text-code-12` when `mono`;
    - rows `border-t border-border-subtle`;
    - null cells render `NULL` in `text-text-tertiary italic`.
  - Markdown tables: map their cells to `DataTable` rows. Cell content stays `InlineNodes`, so rows accept ReactNode.
- [ ] **Step 4:** Run the tests. They must PASS, including the smoothness ratchet. `DataTable` virtualizes, so it must not count as an unvirtualized table; check how the ratchet counts and keep the count unchanged.
- [ ] **Step 5:** Commit `ui: a shared resizable data table; agent tables use it`.

### Task 3: Results outside the fold (§7)

**Files:** Modify `modules/agenthub/surfaces/AgentTurn.tsx` (`StepGroup` ~L323–381, `StepRow` ~L384–427) and `transcript/AgentToolCall.tsx` (a `ToolResultCard` export rendering just the view for a call, with Suspense). Tests: `AgentTurn.test.tsx`.

- [ ] **Step 1: Failing tests.** Register a fake view for `x.view` in the test, then build a turn with a fold of 5 items containing 4 done `x.view` calls and 1 other call:
  - after the fold, 3 `data-testid="turn-result-card"` and a line `+1 more result in the work above`;
  - the fold still lists all 5 rows;
  - a running `x.view` call is not surfaced;
  - a single bare item with a view renders as a card, not a collapsed row;
  - subagent folds surface nothing.
- [ ] **Step 2:** Run the tests. They must FAIL.
- [ ] **Step 3: Implement.**
  - Selection: `surfaced = items.filter(done && hasToolResultView(toolNameOf(call)) && !isToolActivity(call))`. Activities already render outside the fold.
  - Cap at 3, newest last, kept in call order.
  - Each surfaced call renders `ToolResultCard`. It uses `toolPayloadOf`, loads the full row when the payload is `null` and the call is truncated (same loader as AgentToolCall), and renders `renderToolResult`. When the view returns `null`, it renders nothing.
  - Place the cards after the fold and before `step-activities`.
  - In `StepRow`, a lone item with a view uses `ToolResultCard`.
- [ ] **Step 4:** Run the tests. They must PASS.
- [ ] **Step 5:** Commit `agenthub: results with a view show after the work, not inside it`.

### Task 4: Database views (`modules/database`)

**Files:**
- Create `modules/database/toolViews/` containing:
  - `parse.ts`: pure shape checks and snake_case → camel mapping for query, plan, schema and foreign keys;
  - `QueryResultCard.tsx`, `PlanResultCard.tsx`, `SchemaResultCard.tsx`;
  - tests.
- Modify `modules/database/manifest.ts` (register the views and a `database.reveal` intent) and `workbench/intents.ts` docs if needed.

- [ ] **Step 1: Failing tests.**
  - Parsers accept the shapes above and reject malformed input with `null`.
  - **QueryResultCard:**
    - Header: icon, title `Query`, meta `N rows · truncated?`.
    - Body: the first line of the SQL from `rawInput.sql`, in `text-code-12`, truncated with the full SQL in `title`.
    - A `DataTable` of the first 20 rows. `Show all N` expands to every returned row (virtualized).
    - Actions: `Copy CSV` (RFC 4180 quoting; a pure `toCsv` with tests) and `Open in Database`.
    - A write (`columns.length === 0`) shows `N rows affected` (or `Done` when -1).
  - **PlanResultCard:** `PlanDiagram` from `planGraph(mapped)` in a fixed `h-72` container, plus `Open in Database`.
  - **SchemaResultCard:** a list of schemas → tables with column counts. Expanding a table shows columns (name, `data_type`, nullable). Foreign keys appear as rows `table.column → referenced_table.referenced_column`.
  - Intent `database.reveal`:
    - `{kind:"table", id:"schema.table"}` opens the table tab (`openTableTab(schema, table)`) on the selected profile;
    - `{kind:"console", id: sql}` opens a console tab with that SQL (as `openSavedQuery` does).
    - Unit-test the handler's routing through a pure function.
- [ ] **Step 2:** Run the tests. They must FAIL.
- [ ] **Step 3: Implement.**
  - Register `db.query`, `db.explain`, `db.schema.get` and `db.listForeignKeys` with lazy cards.
  - `Open in Database` calls the module's own helpers directly (same module), not the intent.
  - The intent is for agenthub references (Task 8).
- [ ] **Step 4:** Run the tests. They must PASS. The module-boundary ratchet must be unchanged.
- [ ] **Step 5:** Commit `database: query, plan and schema results read as tables in the transcript`.

### Task 5: Kafka and SSH views (`modules/kafka`, `modules/ssh`)

**Files:** For each module, create `toolViews/parse.ts`, cards and tests. Modify both manifests (`kafka.reveal` intent: `{kind:"topic", id}` → `selectTopic(id); setActiveTab("topics"); showModuleHome("kafka")`).

- [ ] **Step 1: Failing tests.**
  - **Peek card:**
    - Header: `Messages · <topic> · partition P`, meta `N messages`.
    - `DataTable` with columns Offset (mono, right), Key, Value preview (≤ 120 chars, one line), Time (`formatClock`).
    - Clicking a row expands its full value with `prettyValue` into a `CodeBlock`, showing a json/raw tag.
    - Actions: `Open in Kafka`.
  - **Topics / groups / describe cards:** compact `DataTable` or definition rows.
  - **Publish card:** `Published to <topic>` with `partition · offset · time`.
  - **SSH card (`ssh.runCommand`):**
    - Header: `Command`, meta `N lines`.
    - The command from `rawInput.command` in mono with a `$` prompt, then the output in `pre whitespace-pre-wrap break-all text-code-12`, max 20 lines with `Show all` and `Copy`.
    - Exit code is unknown (F4), so no status chip.
- [ ] **Step 2:** Run the tests. They must FAIL.
- [ ] **Step 3:** Implement and register the views and `kafka.reveal`.
- [ ] **Step 4:** Run the tests. They must PASS.
- [ ] **Step 5:** Commit `kafka, ssh: messages, topics and command output read as themselves`.

### Task 6: PDF and workspace-memory views

**Files:**
- Create `modules/agenthub/toolViews/pdf.tsx` (PDF tools belong to the agenthub session), `toolViews/memory.tsx`, and tests.
- Register them in `modules/agenthub/manifest.ts`. Before that, check `internal/app/app.go` and `moduleViewsParity.test.ts` for which module owns `workspace.memory.*`; if the workspace layer owns it, register from where workspace memory UI lives (grep `WorkspaceMemory` in `frontend/src`).

- [ ] **Step 1: Failing tests.**
  - **`pdf.viewPages`:**
    - parse the caption and the JSON array from the first text block;
    - render each page as an image when `content` has image blocks with data (live sessions);
    - otherwise render a grey placeholder of the right aspect ratio, labelled `Page N`, with the note `Page images aren't kept after the session reloads`;
    - clicking an image opens a Dialog at full size.
  - **`pdf.search`:** hits as rows `p.N · snippet` (match highlighted with `<mark>`), meta `N hits · pages searched`, and `complete=false` → `Stopped at page X`.
  - **`pdf.readText`:** the header line, then per page a collapsible section `Page N` with the text in `whitespace-pre-wrap`, first page open, plus the footer notes.
  - **`pdf.info` / `pdf.list`:** definition rows.
  - **Memory `get|create|update`:** a card with a kind chip (`decision` / `convention` / `knowledge` / `handoff`), the title, the summary, the body collapsed to 6 lines with `Show all`, and `revision N · updated <time>`. A `deleted` entry shows `Deleted`.
  - **Memory `list|search`:** rows `[kind] title — summary`, plus `More results available` when `hasMore`.
- [ ] **Step 2:** Run the tests. They must FAIL.
- [ ] **Step 3:** Implement and register the views.
- [ ] **Step 4:** Run the tests. They must PASS.
- [ ] **Step 5:** Commit `agenthub: PDF pages, searches and memory entries read as themselves`.

### Task 7 (Go): Keep pdf and memory error codes (F6)

**Files:** `internal/bridge/errors.go` (`publicErrorForCode`, ~L240) and its test.

- [ ] **Step 1: Failing test.** `publicError` of an AppError with code `pdf.notFound` (message "No such document.") keeps code and message. Same for `memory.revisionConflict`. Other unknown codes still become `internal_error`.
- [ ] **Step 2:** `go test ./internal/bridge/ -run PublicError -count=1` must FAIL.
- [ ] **Step 3: Implement.** Keep AppErrors whose code starts with `pdf.` or `memory.`, and keep their messages. Those messages are written for the person; check `internal/mcp/tools_pdf.go` `pdfProblem` and `memoryError`.
- [ ] **Step 4:** `go test ./internal/bridge/ ./internal/mcp/ ./internal/arch/ -count=1` must pass.
- [ ] **Step 5:** Commit `bridge: pdf and memory errors keep their own words`.

### Task 8: DB tables and Kafka topics as references

**Files:**
- Create `modules/agenthub/transcript/entityRefs.ts`: a pure `sessionEntities(items)` that collects known table and topic names from this session's tool calls.
- Modify `transcript/AgentMessageRich.tsx` (code-span cascade after commit/symbol), `transcript/CodeReference.tsx` (`EntityReference`) and `transcriptActions.tsx`. TranscriptStream computes `sessionEntities` from items with `useMemo` and passes it through the actions context.

- [ ] **Step 1: Failing tests.**
  - `sessionEntities` collects:
    - tables from `db.schema.get` namespaces (as `schema.table`, and as bare `table` when it is unique) and from `db.listForeignKeys`;
    - topics from `kafka.listTopics` results and from `topic` inputs of `kafka.peekMessages`, `kafka.describeTopic` and `kafka.publishMessage`.
  - It uses `toolPayloadOf`.
  - In settled prose, a code span equal to a known table renders `data-testid="agent-entity-ref"` with `data-kind="table"` and a dotted underline. A known topic renders the same with `data-kind="topic"`. Unknown names stay plain. Spans inside links stay plain.
  - The hover card shows `Table · schema` or `Topic`, plus an `Open in Database` / `Open in Kafka` button.
  - Click → `dispatchIntent("database.reveal", {module:"database", kind:"table", id:"schema.table", label})` or `kafka.reveal`. On `"unavailable"`, toast "Couldn't open …".
  - Menu: Open, Copy name.
- [ ] **Step 2:** Run the tests. They must FAIL.
- [ ] **Step 3: Implement.**
  - Cascade order: file → commit → symbol → entity → plain.
  - Entities need no lookup; the names come from the session's own results. They render only when the message is settled.
- [ ] **Step 4:** Run the tests. They must PASS. The module-boundary ratchet must be unchanged, because only intents are used.
- [ ] **Step 5:** Commit `agenthub: tables and topics the session has seen become references`.

### Task 9: Docs and verification

- [ ] **Step 1: Docs.**
  - `AGENTS.md`, Agent Hub section: one bullet "Tool results read as themselves". It covers:
    - `toolPayloadOf` (Claude content vs Codex rawOutput, fences, stubs);
    - module-owned views per tool;
    - the surfacing cap of 3;
    - `DataTable`;
    - `database.reveal` / `kafka.reveal`;
    - entity refs.
  - `docs/dev-kit/tool-result-view.mdx`: add the payload rule (views read `toolPayloadOf`'s payload, and `ToolResultInput.content` for images).
  - Wide-layout spec: a "Phase 2 decisions" section with F1–F6 and anything else that changed.
- [ ] **Step 2: Checks.**
  - `cd frontend && bun test 2>&1 | tail -3` → 0 fail.
  - `bun run build` passes the budget.
  - `bunx eslint src | grep problems` → 47.
  - `go test ./internal/bridge/ ./internal/mcp/ ./internal/arch/ -count=1` passes.
- [ ] **Step 3: Visual.** Use a throwaway dev page, as in Phase 1b (bridge faked through `setTransport`; delete it afterwards and never stage it). Render a settled turn containing:
  - a Claude-shaped `db.query` (25 rows);
  - a `db.explain`;
  - a `kafka.peekMessages`;
  - an `ssh.runCommand`;
  - a `pdf.search`;
  - a memory entry;
  - a markdown table;
  - prose naming a table and a topic.

  Then:
  - Screenshot light and dark at 1600px.
  - Drag a column resize handle with CDP mouse events and screenshot before and after.
  - Hover the table and topic references.
  - Look at each PNG and report.
- [ ] **Step 4:** Commit `docs: Agent Hub rich results (phase 2)` with the doc files only.
