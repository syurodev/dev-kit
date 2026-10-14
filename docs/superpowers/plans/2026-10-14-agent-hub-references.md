# Agent Hub Phase 1b — References Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make file references, links, symbols and commit hashes in agent prose open reliably, preview on hover (shadcn HoverCard) and offer a context menu, per `docs/superpowers/specs/2026-10-14-agent-hub-references-design.md`. Also fold in three Phase-1 visual fixes.

**Architecture:**
- **Pure parsers.** `fileRef` v2 and `linkTarget` classify text and hrefs.
- **Resolver.** `resolveFileRef` turns a reference into `{workspaceId, folderId, rel}` through injected bridge deps: workspace folders, file stat, and the context-file search.
- **Opening.** `codegraph.reveal` learns to open by ids. One `openFileRef` helper handles found, ambiguous, missing and unsupported, for both the Hub and the companion rail.
- **Prose wiring.** `TranscriptActions` carries the session context and these helpers to the prose.
- **Preview and menu.** A `ReferencePreview` HoverCard and pure context-menu builders wrap every reference in `AgentMessageRich`.
- **Symbols and commits.** These become references only for finished messages, through a capped, cached lookup.

**Tech Stack:** React 19, TypeScript, TanStack Query, Radix (HoverCard, ContextMenu, Popover) via `components/ui`, Tailwind v4 container queries, bun test (renderToString, no DOM).

## Conventions (every task)

- Repo: `/home/ptvu3/Downloads/dev-kit`. Frontend commands run in `frontend/`. Branch: `feat/mono-chrome-color-palettes`.
- Work TDD: write the failing test, watch it fail, implement, watch it pass, commit.
- Each commit message ends with the `Co-Authored-By` line for the model doing the work.
- Stage only the files the task touches. Never `git stash`, never `--no-verify`, never push.
- Before each commit, run:
  - `bun test src/modules/agenthub src/modules/codegraph src/designSystemConformance.test.ts src/smoothnessRatchet.test.ts src/storeSubscriptionRatchet.test.ts src/focusRing.test.ts`
  - `bunx tsc --noEmit -p .`
  - eslint on the changed files. It must report no new errors; the repo-wide baseline is 47.
- The spec is authoritative for behaviour and look. Read the section a task names.

## Facts (from the code survey)

**Intents and opening files**
- `workbench/intents.ts`:
  - `EntityRef = {module, kind, id, label, project?, line?}`
  - `dispatchIntent(name, ref, {placement}) → "ok"|"unavailable"`
- `codegraph.reveal`:
  - Registered at `modules/codegraph/manifest.ts:99`. For `kind:"file"` it calls `actions.openFileInProject(project, id, line, placement)` (`codegraphActions.ts:99-120`), which resolves against the **title-bar** workspace's folders.
  - `openFile({workspaceId, folderId, path}, line|null, {preview:false, beside})` (`fileActions.ts:86`) accepts any workspace.
  - `openFileInProject` also sets `activeTab:"files"` on the Code Graph store.
- `git.reveal` (`modules/git/manifest.ts:78`): `{kind:"commit", id: sha}` opens `openCommitDiff` on Git's pinned folder. Pin first with `pinWorkFolder("git", repoPath)` (`bridge/workfolder.ts:28`).

**Workspace and file bridges**
- `listWorkspaceServices(workspaceId) → {id, path, name, …}[]` (`bridge/workspace.ts:15`).
- `statFolderFile(ws, folder, rel) → {exists, revision, size, modifiedAt, unreadable: "binary"|"too_large"|"not_regular"|""}` (`bridge/workspaceCode.ts:71`).
- `readFolderFile(ws, folder, rel) → {content, revision, size, modifiedAt}` (l.54). The 5 MB cap and binary files are errors.
- `agentHubListContextFiles(sessionId, query) → {path (absolute), name, description?}[]` (`bridge/agenthub.ts:294`). Case-insensitive substring match, at most 100 results, roots are the session's project plus its extra paths.
- `searchWorkspaceSymbols(ws, folderIds, query) → {folderId, state, matches: {nodeId, label, kind, layer, filePath (folder-relative), line}[]}[]` (`bridge/workspaceCode.ts:340`). Substring match, at most 200.

**Highlighting and UI components**
- Highlighting:
  - `components/code/CodeBlock.tsx`: `useHighlight(code, language)`, `renderHighlighted(code, highlight)`.
  - `components/code/languages.ts`: `languageForPath(path)`.
  - `transcript/AgentCodeBlock.tsx` shows the gutter pattern.
- HoverCard:
  - `components/ui/hover-card.tsx` exports `HoverCard`, `HoverCardTrigger`, `HoverCardContent`. Radix defaults are 700/300 ms, so pass `openDelay={400} closeDelay={120}`.
  - Pattern to copy: `workbench/ModuleSwitcher.tsx:145-191` (`HoverCardTrigger asChild` > `ContextMenuTrigger asChild`; ContextMenu `onOpenChange` closes the hover card).
- `components/ui/context-menu.tsx`: `ContextMenuItem` accepts `icon`, `shortcut` and `detail` props.

**Agent prose today**
- `transcript/AgentMessageRich.tsx` `InlineNodes`:
  - `case "code"`: a `fileRef` match renders `<code role="button" data-testid="agent-file-ref">` that calls `onOpenFile`.
  - `case "link"`: `<a data-testid="agent-link" data-href onClick=preventDefault+openExternal>` inside a Tooltip.
- `transcript/transcriptActions.tsx`: `TranscriptActions = { onOpenFile? }`.
- `surfaces/TranscriptStream.tsx` provides it: `stableOnOpenFile` and the provider, around l.235–254 and l.578.
- Callers of `onOpenFile`:
  - `AgentHubView.tsx:766` `handleOpenFile` dispatches `codegraph.reveal` with `project`.
  - `workbench/AgentCompanion.tsx:178` `openFile` does the same, via `AgentRail`.

**Session data**
- The session has `id`, `workspaceId` (`""` for scratch) and `projectPath`.
- `AgentHubContent` holds `session` (null for drafts). `AgentRail` holds `activeSession`.

---

### Task 1: Parsers — `fileRef` v2 and `linkTarget`

**Files:** Modify `frontend/src/modules/agenthub/transcript/fileRef.ts`, `fileRef.test.ts`.

- [ ] **Step 1: Failing tests.** Extend `fileRef.test.ts`:
  ```ts
  import { fileRef, linkTarget } from "./fileRef";

  test("line forms", () => {
    expect(fileRef("handler.lua:27")).toEqual({ path: "handler.lua", line: 27 });
    expect(fileRef("a/b.go#L27")).toEqual({ path: "a/b.go", line: 27 });
    expect(fileRef("a/b.go#L27-L30")).toEqual({ path: "a/b.go", line: 27 });
    expect(fileRef("./a/b.go:3:9")).toEqual({ path: "a/b.go", line: 3 });
    expect(fileRef("/abs/a/b.go")).toEqual({ path: "/abs/a/b.go" });
    expect(fileRef("../shared/x.ts")).toEqual({ path: "../shared/x.ts" });
  });
  test("well-known names without an extension", () => {
    for (const n of ["Dockerfile", "Makefile", "go.mod", "package.json", "Taskfile.yml", "docker/Dockerfile"]) {
      expect(fileRef(n)?.path).toBe(n);
    }
  });
  test("still not paths", () => {
    for (const s of ["https://x.test/a.go", "SessionTTL", "a b/c.go", "v1.2.3", "x.y", "1.5", "../", "a/../../etc/passwd"]) {
      expect(fileRef(s)).toBeNull();
    }
  });
  test("linkTarget", () => {
    expect(linkTarget("https://x.test/a")).toEqual({ kind: "url", url: "https://x.test/a" });
    expect(linkTarget("mailto:a@b.c")).toEqual({ kind: "url", url: "mailto:a@b.c" });
    expect(linkTarget("internal/auth/session.go#L88")).toEqual({ kind: "file", ref: { path: "internal/auth/session.go", line: 88 } });
    expect(linkTarget("file:///home/u/p/a%20b.go")).toEqual({ kind: "other" }); // a space after decoding is still not a path
    expect(linkTarget("file:///home/u/p/x.go")).toEqual({ kind: "file", ref: { path: "/home/u/p/x.go" } });
    expect(linkTarget("ftp://x")).toEqual({ kind: "other" });
  });
  ```
  Keep every existing case passing.
- [ ] **Step 2:** `bun test src/modules/agenthub/transcript/fileRef.test.ts` → FAIL.
- [ ] **Step 3: Implement** in `fileRef.ts`:
  - Strip a leading `./`. Accept a `#L(\d+)(-L\d+)?` suffix as the line.
  - Reject any `..` segment except leading `../` segments that are followed by real names. Reject `../` alone and any `..` after a name segment.
  - Add `KNOWN_NAMES = /(^|\/)(Dockerfile|Makefile|Taskfile\.ya?ml|go\.mod|go\.sum|package\.json|tsconfig\.json|Cargo\.toml|pom\.xml|build\.gradle(\.kts)?|\.env(\.\w+)?)$/`. A path matching it counts as a file even without a `SOURCE_EXT` match.
  - Add `export type LinkTarget = {kind:"url"; url:string} | {kind:"file"; ref: FileRef} | {kind:"other"}`.
  - `linkTarget(href)`:
    - `^(https?:|mailto:)` → url.
    - `^file://` → `decodeURIComponent` the path, then `fileRef(path)`; a match is file, else other.
    - `^[a-z][a-z0-9+.-]*:` (any other scheme) → other.
    - Anything else → `fileRef(href)` → file or other.
  - Comment each rule in one line.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5:** Commit `agenthub: parse more file reference forms and markdown file links`.

### Task 2: Resolver

**Files:** Create `frontend/src/modules/agenthub/transcript/resolveFileRef.ts`, `resolveFileRef.test.ts`.

```ts
export type RefContext = { sessionId: string; workspaceId: string; projectPath: string };
export type RefTarget = { workspaceId: string; folderId: string; folderName: string; rel: string; line?: number; unreadable?: "binary" | "too_large" | "not_regular" };
export type RefResolution =
  | { kind: "found"; target: RefTarget }
  | { kind: "ambiguous"; targets: RefTarget[] }
  | { kind: "missing" }
  | { kind: "unsupported" };
export type ResolverDeps = {
  folders: (workspaceId: string) => Promise<{ id: string; name: string; path: string }[]>;
  stat: (workspaceId: string, folderId: string, rel: string) => Promise<{ exists: boolean; unreadable: string }>;
  byName: (sessionId: string, name: string) => Promise<{ path: string }[]>;
};
export async function resolveFileRef(ref: FileRef, ctx: RefContext, deps: ResolverDeps): Promise<RefResolution>;
```

- [ ] **Step 1: Failing tests** with fake deps:
  - The folders are `api` → `/w/api` and `web` → `/w/web`; `projectPath` is `/w/api`.
  - **Absolute:** `/w/web/src/a.ts` → found `{folderId:"web", rel:"src/a.ts"}`. `/elsewhere/x.ts` → missing.
  - **Project-relative:** `handler.lua` exists at `/w/api/handler.lua` → found in api. A line passes through.
  - **Folder-relative fallback:** `src/a.ts` is missing under api but exists under web → found in web.
  - **By name:** `handler.lua` is not at any root. `byName` returns `/w/api/kong/plugins/x/handler.lua` and `/w/web/static/y.js` → found with `rel:"kong/plugins/x/handler.lua"`.
  - **Ambiguous:** `byName` returns two `.../handler.lua` in different folders → ambiguous, two targets, sorted `folderName` then `rel`.
  - **Suffix filter:** for `x/handler.lua`, `byName` also returns `/w/api/other/handler.lua`, which is filtered out because it does not end with `/x/handler.lua`.
  - **Missing:** nothing anywhere → missing.
  - **Unsupported:** `workspaceId: ""` → unsupported, and no dep is called.
  - **Unreadable:** `stat` returns `{exists:true, unreadable:"binary"}` → found with `unreadable:"binary"`.
  - **Leading `../`:** `../web/src/a.ts` relative to `/w/api` → `/w/web/src/a.ts` → found in web.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement** the order from spec §2. Rules:
  - Path joining uses a small `joinPath`/`normalize` that resolves `.` and `..` with `/` separators. Do not import node `path`; this is browser code.
  - The deepest containing folder wins.
  - Dedupe by `folderId + rel`. Cap ambiguous results at 10.
  - `stat` rejections count as "not there".
  - Add a hook:
    ```ts
    export function useFileRefResolution(ref: FileRef | null, ctx: RefContext | null, enabled: boolean)
    ```
    It uses TanStack `useQuery` with key `["agenthub","fileRef",ctx.sessionId,ref.path]`, `staleTime` 30_000, and `enabled: enabled && !!ref && !!ctx`. It uses the live deps built from `listWorkspaceServices`, `statFolderFile` and `agentHubListContextFiles`, exported as `liveResolverDeps`.
- [ ] **Step 4:** run → PASS. tsc clean.
- [ ] **Step 5:** Commit `agenthub: resolve agent file references inside the session's workspace`.

### Task 3: Open by ids — `codegraph.reveal`, `openFileRef`, picker, wiring

**Files:**
- Modify `frontend/src/workbench/intents.ts` (`EntityRef` gains optional `workspaceId?`, `folderId?`).
- Modify `frontend/src/modules/codegraph/manifest.ts` (`codegraph.reveal`).
- Create `frontend/src/modules/agenthub/transcript/openReference.ts` and `transcript/RefPicker.tsx`.
- Modify `transcript/transcriptActions.tsx`, `surfaces/TranscriptStream.tsx`, `surfaces/AgentHubContent.tsx`, `AgentHubView.tsx` (~766), `workbench/AgentCompanion.tsx` (~178), `surfaces/AgentRail.tsx`.
- Tests: codegraph manifest test (or a new `codegraphReveal.test.ts`), `openReference.test.ts`.

- [ ] **Step 1: Failing tests.**
  - `codegraph.reveal` with `{kind:"file", id:"src/a.ts", workspaceId:"w", folderId:"f", line:3}` calls `openFile({workspaceId:"w", folderId:"f", path:"src/a.ts"}, 3, {preview:false, beside:true})` for placement beside. Make `openFile` injectable for the test, following how other codegraph tests mock `fileActions`; if mocking is heavy, extract a tiny pure `revealFileArgs(ref, placement)` and test that.
  - `openFileRef` with injected deps (`resolve`, `dispatch`, `toastError`, `pick`):
    - found → dispatch called with the ids and placement beside;
    - missing → `toastError("Couldn't find handler.lua in this workspace")`;
    - ambiguous → `pick(targets)` called, and when it resolves a target, dispatch is called with it;
    - unsupported → dispatch with the old `{project}` ref.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement.**
  - `EntityRef`: add the two optional fields with a doc comment ("set when the caller already resolved the file; bypasses project resolution").
  - `codegraph.reveal` `kind:"file"`: if `ref.workspaceId && ref.folderId`, call `openFile(...)` and set the Code Graph sidebar tab to files (as `openFileInProject` does). Otherwise keep the existing path.
  - `openReference.ts`:
    ```ts
    export async function openFileRef(ref: FileRef, ctx: RefContext | null, opts: { placement?: "beside" | "replace"; anchor?: HTMLElement | null }, deps = liveOpenDeps): Promise<void>
    ```
    `liveOpenDeps` uses:
    - `queryClient.fetchQuery` with the Task 2 key, for caching;
    - `dispatchIntent`;
    - `toast.error` from sonner;
    - `pickTarget(anchor, targets)`, which mounts the picker.
  - `RefPicker.tsx`: a small Popover anchored to the element. Each candidate is a `button` row showing `folderName · rel` (file name bold). Enter picks, Escape cancels. Implement `pickTarget` with a module-level host component rendered once by `TranscriptStream` (a `RefPickerHost` that subscribes to a tiny zustand store `{anchorRect, targets, resolve}`). Keep it ≤120 lines.
  - `TranscriptActions` becomes:
    ```ts
    {
      onOpenFile?;
      refContext?: RefContext | null;
      openRef?: (ref: FileRef, anchor: HTMLElement | null, placement?: "beside" | "replace") => void;
    }
    ```
    `TranscriptStream` gets a new optional prop `refContext` and builds a stable `openRef` that calls `openFileRef(ref, refContext, {placement, anchor})`.
  - `AgentHubContent` passes `refContext={session ? {sessionId: session.id, workspaceId: session.workspaceId ?? "", projectPath: session.projectPath} : null}` (memoised). `AgentRail` does the same with `activeSession`.
  - `AgentMessageRich`:
    - `code` refs call `openRef(ref, event.currentTarget)` when available, else `onOpenFile` (fallback).
    - `link` with `linkTarget(href).kind === "file"` calls `openRef` (preventDefault).
    - `url` calls `openExternal`.
    - `other` does nothing, with a tooltip stating the href.
  - `AgentHubView.handleOpenFile` and `AgentCompanion.openFile` keep handling tool-call chips (absolute paths). Route them through `openFileRef` with the session context too, so absolute paths open in the right folder.
- [ ] **Step 4:** run checks → PASS.
- [ ] **Step 5:** Commit `agenthub: references open the right file, or say why not`.

### Task 4: Preview and context menu for files and URLs

**Files:** Create `frontend/src/modules/agenthub/transcript/ReferencePreview.tsx`, `transcript/referenceMenu.ts` (pure item builders), and tests. Modify `transcript/AgentMessageRich.tsx`.

- [ ] **Step 1: Failing tests.**
  - `referenceMenu.ts`:
    ```ts
    fileMenuItems(ref) → [{id:"open-beside",label:"Open beside"},{id:"open-here",label:"Open here"},{id:"copy-path",label:"Copy path"},{id:"copy-path-line",label:"Copy path:line"},{id:"reveal",label:"Reveal in Files"}]
    ```
    Leave out `copy-path-line` when there is no line.
    ```ts
    urlMenuItems(url) → [{id:"open",label:"Open in browser"},{id:"copy",label:"Copy link"}]
    ```
  - Render tests:
    - a file ref and a URL link each sit inside a `[data-slot="hover-card-trigger"]` (check the component's real data-slot) and a context-menu trigger;
    - `ReferencePreview` renders its body only when open: `renderToString` of the closed state shows no body;
    - `previewSlice(content, line)` returns lines `line-4..line+4` clamped to the file, with `startLine`. When `line` is undefined it returns the first 9 lines. Test it as a pure function.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement.**
  - **Wrapping.** Follow `workbench/ModuleSwitcher.tsx:145-191` exactly: `HoverCard openDelay={400} closeDelay={120}` > `HoverCardTrigger asChild` > `ContextMenu` (`onOpenChange` closes the hover card) > `ContextMenuTrigger asChild` > the existing `<code role="button">` or `<a>`.
  - **File preview content**, on open:
    - `useFileRefResolution(ref, refContext, open)`; when found, `useQuery(["agenthub","fileRefRead",ws,folder,rel], readFolderFile, {enabled: open && found, staleTime: 30_000})`.
    - Header: the file name bold, the folder path dim, and `folderName` right-aligned.
    - Body: `previewSlice` with a gutter of line numbers (`text-text-tertiary tabular-nums`). The target line has `bg-state-soft`. Highlight the slice with `useHighlight(slice, languageForPath(rel) ?? "text")` and `renderHighlighted`.
    - Footer buttons: `Open` (beside) and `Open here`.
    - States:
      - loading: three skeleton lines of the final height;
      - binary or too_large: "Binary file" or "Too large to preview";
      - missing: "Not found in this workspace";
      - ambiguous: the candidate list, where clicking a row opens it;
      - unsupported: "Open in Code Graph" only.
  - **URL preview content:** the full URL with `break-all`, the host, and buttons `Open in browser` and `Copy link` (`copyText`).
  - **Content box:** `HoverCardContent className="w-96 p-0"` with inner sections padded `p-2.5`, using `text-code-12` for code.
  - **Menu actions:**
    - open-beside / open-here: `openRef(ref, anchor, placement)`;
    - copy-path: `copyText(rel ?? ref.path)`, where `rel` is the resolved one when known;
    - copy-path-line: the same with `:line` appended;
    - reveal: open here and focus Files;
    - URL open: `openExternal`; URL copy: `copyText`.
- [ ] **Step 4:** run checks → PASS.
- [ ] **Step 5:** Commit `agenthub: hover previews and context menus for file refs and links`.

### Task 5: Symbols and commits in finished messages

**Files:** Create `frontend/src/modules/agenthub/transcript/codeRefs.ts` (pure matchers), `transcript/useCodeRefs.ts` (lookup hook), tests. Modify `AgentMessageRich.tsx` (`AgentProse` gains `settled?: boolean`), `surfaces/AgentTurn.tsx` (pass `settled={!live}` to the answer prose), `transcriptActions.tsx`.

- [ ] **Step 1: Failing tests.**
  - `symbolCandidate(text)`: true for `SessionTTL`, `Service.Invoke`, `Service.Invoke()`, `useHighlight`. False for `null`, `true`, `self`, `a`, `ab`, `x.y.`, `1abc`, `rm -rf`, `a/b.go`.
  - `commitCandidate(text)`: true for `a1b2c3d` and a 40-hex string. False for `deadbeef` (no digit), `1234567` (no letter), `abc12` (too short), `A1B2C3D` (uppercase not accepted).
  - `exactSymbolMatch(span, matches)`:
    - `Invoke` vs labels `["Service.Invoke","Other.Invokes"]` → `Service.Invoke`;
    - `Service.Invoke()` → `Service.Invoke`;
    - two exact matches → null (ambiguous).
  - Render:
    - a settled prose with a code span that resolves (inject a fake lookup via `TranscriptActions.lookupSymbol`) renders `data-testid="agent-symbol-ref"` with `decoration-dotted`;
    - an unsettled one renders plain `<code>`;
    - a commit span in settled prose renders `data-testid="agent-commit-ref"`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement.**
  - `useCodeRefs(spans, refContext, settled)`:
    - Collect the distinct symbol candidates, up to 20, and query each with `useQueries`:
      - key `["agenthub","symbol",workspaceId,name]`;
      - query fn: `searchWorkspaceSymbols(workspaceId, [], lastSegment(name))`, mapped to `{folderId, filePath, line, kind, label}[]`, then `exactSymbolMatch`;
      - `staleTime` 300_000;
      - enabled only when settled, with a workspace, and the name is a candidate.
    - Concurrency: TanStack runs queries in parallel. Cap at 20 names per message.
    - Returns a `Map<span, SymbolHit>`.
  - **Symbol rendering** (in `InlineNodes` for code spans): when a `SymbolHit` exists, render `<code role="button" data-testid="agent-symbol-ref" … underline decoration-dotted>`.
    - Click opens `{workspaceId, folderId, rel: filePath, line}` through `codegraph.reveal` with ids, placement beside.
    - Wrap it with the same `HoverCard` + `ContextMenu`. The preview reuses the file preview body with that target. The menu offers "Go to definition" and "Copy name".
  - **Commits:** in settled prose, a `commitCandidate` code span renders `data-testid="agent-commit-ref"` with a dotted underline.
    - Click: `pinWorkFolder("git", projectFolderPath)`, where `projectFolderPath` is the path of the folder containing `refContext.projectPath`, or `projectPath` itself. Then `dispatchIntent("git.reveal", {module:"git", kind:"commit", id: sha, label: sha.slice(0,7)}, {placement:"beside"})`. On `"unavailable"`, show a toast error.
    - Hover card: the short sha, "Commit in <folder>", and an `Open in Git` button.
    - Menu: "Open in Git" and "Copy SHA".
  - `AgentTurn` passes `settled={!live}` only to the final answer prose (the AgentProse calls at ~201/229/470; pick the ones that render the agent's message text, not tool output). Code spans inside a live message keep the file-ref behaviour only, which needs no lookup.
- [ ] **Step 4:** run checks → PASS.
- [ ] **Step 5:** Commit `agenthub: symbols and commit hashes become references once a message settles`.

### Task 6: Phase-1 visual fixes

**Files:** `frontend/src/modules/agenthub/sidebar/model.ts` (+ test), `surfaces/AgentTurn.tsx` (+ test), `surfaces/AgentHubContent.tsx` and `surfaces/TranscriptStream.tsx` (+ `composerLayout.test.tsx`).

- [ ] **Step 1: Failing tests.**
  - `secondLine` or `askBySession`: the ask `` Run `rm -rf build`? `` becomes the text `Run rm -rf build?`. Add an exported `plainAsk(text)` that strips backticks, `**` and `__`, and folds whitespace.
  - The latest plan card in a turn renders two variants: the full card with `@6xl/agent-conversation:hidden`, and a one-line `data-testid="turn-plan-docked"` reading `Plan · 2 of 4 — in the summary` with `hidden @6xl/agent-conversation:flex`. Older turns' plan cards have no docked variant. Pass a prop `latestPlan` from TranscriptStream or AgentTurn, where the turn holding the session's latest plan item is the newest turn that has a plan.
  - The Hub transcript column and the composer column carry `@7xl/agent-conversation:max-w-220`. The rail does not.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement** the three items per spec §7. Check that `max-w-220` compiles (`bun run build` then grep the CSS for `max-width:55rem`) and that `@7xl/agent-conversation` emits a container rule at 80rem.
- [ ] **Step 4:** run checks → PASS.
- [ ] **Step 5:** Commit `agenthub: plain asks, one plan when docked, a wider column on wide screens`.

### Task 7: Docs and verification

- [ ] **Step 1: Docs.**
  - `AGENTS.md`, Agent Hub section: one bullet, "References in agent prose resolve, preview and open". Cover:
    - `fileRef`/`linkTarget` → `resolveFileRef` (session workspace: absolute, project-relative, folder-relative, by name);
    - `codegraph.reveal` with `workspaceId`/`folderId`;
    - HoverCard previews (400/120 ms);
    - context menus;
    - symbols and commits only in settled messages, capped at 20 lookups and cached.
  - Spec: a "Decisions while building" section listing anything that changed.
- [ ] **Step 2: Checks.**
  - `cd frontend && bun test 2>&1 | tail -3` → 0 fail. If timing tests flake under load, rerun them alone and report.
  - `bun run build` → the budget passes.
  - `bunx eslint src 2>&1 | grep problems` → still 47.
  - `go test ./internal/arch/ ./internal/bridge/ -count=1` → ok.
- [ ] **Step 3: Visual.**
  - Build a scratch dev page like Phase 1's (`frontend/hubshot.html` + `src/dev/__HubShot.tsx`, deleted afterwards and never staged). It renders AgentHubContent with prose containing `handler.lua:27`, a markdown file link, a URL, a symbol (fake lookup through TranscriptActions) and a commit hash.
  - Capture with headless Chrome over CDP: the hover card open on a file ref, on a URL and on a symbol, in light and dark; the context menu open on a file ref; the docked plan line; the 2200px layout.
  - Look at each PNG and report.
- [ ] **Step 4:** Commit `docs: Agent Hub references (phase 1b)` with the doc files only.
