# Code Editor Primitive + CodeGraph File Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the shared [Code editor](base-doc `content/projects/dev-kit/code-editor.mdx`) primitive (CodeMirror 6, currently "Design only" — zero runtime, not even a dependency installed) and wire it into CodeGraph's file panel — the frontend for `2026-08-19-codegraph-file-editing.md`'s already-implemented, already-tested Go backend (`ListFiles`/`ReadFile`/`WriteFile`, all 3 already generated as TS bindings — confirmed by reading the current bindings tree, not assumed).

**Why build the primitive and this consumer together:** same reasoning as `2026-08-19-terminal-primitive-and-codegraph.md` — a primitive with zero consumers can't prove its `Compartment`-based reconfiguration actually works without something real reconfiguring it (switching files mid-session, toggling readOnly). The design doc names **two** consumers (Database's query console, CodeGraph's file editing) — this plan only builds the second; Database's SQL-dialect/schema-aware extension is separate, unstarted work for a future plan.

**Architecture:** `CodeEditor` is a thin, headless React wrapper around a raw CodeMirror 6 `EditorView` — no language knowledge, no schema knowledge, exactly per the doc's API (`value`, `onChange`, `extensions?`, `readOnly?`). Three `Compartment`s (language extensions, read-only state, theme) let every one of those change without unmounting the `EditorView`, preserving undo history and cursor position — the doc's explicit requirement. CodeGraph's file panel is a flat, depth-computed, `@tanstack/react-virtual`-backed tree view (not a new generic `Sidebar` primitive — see "Scope decision" below) that opens a clicked file into the editor and saves on `Mod-S`.

**Scope decision — no generic `VirtualTree` primitive built here:** the design doc references `Sidebar § Hiệu năng khi nội dung sidebar lớn` for virtualizing the file panel, but no such component exists yet, and CodeGraph is its only real consumer today. Building a fully generic, reusable tree primitive for exactly one consumer would be the premature abstraction this codebase's own conventions (and this session's established practice) avoid — the virtualized tree here is built directly inside CodeGraph's module using `@tanstack/react-virtual` (already an installed dependency), not extracted into a shared component. Extract one later if and when a second consumer actually needs it.

**Tech Stack:** CodeMirror 6, verified against its real API before designing (not assumed): `basicSetup`/`EditorView` from the `codemirror` meta-package, `EditorState`/`Compartment`/`Extension` from `@codemirror/state`, `keymap` from `@codemirror/view`, `oneDark` from `@codemirror/theme-one-dark` (verified its actual named exports via the published bundle, not just documentation), plus `@codemirror/lang-java`/`-json`/`-yaml`/`-markdown` for CodeGraph's 4 supported languages (FR-10's scope). None of these are installed yet — this plan adds them. `@tanstack/react-virtual` is already a dependency (confirmed in `frontend/package.json`).

**No frontend test infrastructure exists** (confirmed against `AGENTS.md`: the documented frontend check is still `cd frontend && bun run build`, i.e. `tsc` + `vite build` — no test runner is part of the committed baseline). Verification here is typecheck/build plus a manual QA script, same pattern every prior frontend plan in this sequence has used.

---

## File Structure

Create:
- `frontend/src/components/code-editor/CodeEditor.tsx` — the primitive.
- `frontend/src/modules/codegraph/FileTree.tsx` — the virtualized file/directory list.

Modify:
- `frontend/package.json` — 7 new dependencies.
- `frontend/src/bridge/codegraph.ts` — `listCodeGraphFiles`/`readCodeGraphFile`/`writeCodeGraphFile` wrappers.
- `frontend/src/modules/codegraph/CodeGraphView.tsx` — a new "Files" tab wiring `FileTree` + `CodeEditor` together.

---

## Task 1: Install CodeMirror

**Files:**
- Modify: `frontend/package.json`

- [ ] **Step 1: Add the dependencies**

```bash
cd frontend
bun add codemirror @codemirror/state @codemirror/view @codemirror/theme-one-dark @codemirror/lang-java @codemirror/lang-json @codemirror/lang-yaml @codemirror/lang-markdown
```

- [ ] **Step 2: Confirm the exact exports these plan's later tasks assume**

```bash
grep -n "^export" node_modules/@codemirror/lang-java/dist/index.d.ts
grep -n "^export" node_modules/@codemirror/lang-json/dist/index.d.ts
grep -n "^export" node_modules/@codemirror/lang-yaml/dist/index.d.ts
grep -n "^export" node_modules/@codemirror/lang-markdown/dist/index.d.ts
```

Expected: each exports a function named `java`/`json`/`yaml`/`markdown` respectively (matching every other `@codemirror/lang-*` package's established naming convention — this plan's design doc already confirmed `sql()` follows this pattern, and this step just confirms these 4 do too, rather than assuming further). If any package uses a different export name, adjust Task 5's `languageExtensionForPath` import accordingly — don't guess past what this command shows.

- [ ] **Step 3: Build to confirm nothing else broke**

```bash
bun run build
```

Expected: succeeds — no code imports these packages yet.

- [ ] **Step 4: Commit**

```bash
git add package.json bun.lock
git commit -m "chore(frontend): add CodeMirror 6 dependencies"
```

---

## Task 2: `CodeEditor` primitive

**Files:**
- Create: `frontend/src/components/code-editor/CodeEditor.tsx`

- [ ] **Step 1: Write the component**

```tsx
// frontend/src/components/code-editor/CodeEditor.tsx
import { useEffect, useRef } from "react";
import { EditorView, basicSetup } from "codemirror";
import { EditorState, Compartment, type Extension } from "@codemirror/state";
import { oneDark } from "@codemirror/theme-one-dark";
import { useTheme } from "@/theme/ThemeProvider";

export type CodeEditorProps = {
  value: string;
  onChange: (value: string) => void;
  extensions?: Extension[];
  readOnly?: boolean;
};

// Headless CodeMirror 6 wrapper — no language or schema knowledge of its
// own, per base-doc code-editor.mdx. Every prop that can change after mount
// is applied through a Compartment (or, for onChange, a ref) instead of
// remounting the EditorView, which would lose undo history and cursor
// position — the doc's explicit requirement, and CodeMirror's own
// documented pattern for dynamic reconfiguration, not a DevKit invention.
export function CodeEditor({ value, onChange, extensions, readOnly }: CodeEditorProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const languageCompartment = useRef(new Compartment()).current;
  const readOnlyCompartment = useRef(new Compartment()).current;
  const themeCompartment = useRef(new Compartment()).current;

  // onChange is very likely a new function identity on every parent render
  // in React — a ref sidesteps needing a 4th Compartment (or a stale
  // closure) just to keep the update listener current.
  const onChangeRef = useRef(onChange);
  onChangeRef.current = onChange;

  const { resolvedTheme } = useTheme();

  useEffect(() => {
    if (!containerRef.current) return;
    const view = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          languageCompartment.of(extensions ?? []),
          readOnlyCompartment.of(EditorState.readOnly.of(!!readOnly)),
          themeCompartment.of(resolvedTheme === "dark" ? [oneDark] : []),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              onChangeRef.current(update.state.doc.toString());
            }
          }),
        ],
      }),
      parent: containerRef.current,
    });
    viewRef.current = view;
    return () => {
      view.destroy();
      viewRef.current = null;
    };
    // Deliberately empty — see the function's doc comment. Every prop this
    // effect would otherwise need to depend on is instead applied by the
    // effects below, without remounting.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // External value changes (e.g. the consumer opened a different file) —
  // guarded by an equality check so this doesn't fight the update listener
  // above during normal typing (onChange already updated the parent's
  // value to match what's on screen; dispatching again here would be a
  // harmless no-op change, but skipping it avoids the extra work).
  useEffect(() => {
    const view = viewRef.current;
    if (!view || view.state.doc.toString() === value) return;
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } });
  }, [value]);

  useEffect(() => {
    viewRef.current?.dispatch({ effects: languageCompartment.reconfigure(extensions ?? []) });
  }, [extensions, languageCompartment]);

  useEffect(() => {
    viewRef.current?.dispatch({
      effects: readOnlyCompartment.reconfigure(EditorState.readOnly.of(!!readOnly)),
    });
  }, [readOnly, readOnlyCompartment]);

  useEffect(() => {
    viewRef.current?.dispatch({
      effects: themeCompartment.reconfigure(resolvedTheme === "dark" ? [oneDark] : []),
    });
  }, [resolvedTheme, themeCompartment]);

  return <div ref={containerRef} className="h-full w-full overflow-auto" />;
}
```

`readOnly` uses `EditorState.readOnly.of(...)` (a facet on `@codemirror/state`), **not** `EditorView.editable` — two different facets (`readOnly` disables edit commands, `editable` only controls DOM `contentEditable`) — per the design doc's explicit warning not to confuse them. Theme: only a dark extension is added conditionally; CodeMirror's un-themed default already reads as a reasonable light appearance, so no separate light theme package is pulled in for v1 — a deliberate simplification, add one later only if the bare default looks wrong against the rest of the app's light theme.

- [ ] **Step 2: Typecheck**

```bash
cd frontend && bun run build
```

Expected: succeeds. Nothing imports `CodeEditor` yet, so this only proves the file itself compiles.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/code-editor/CodeEditor.tsx
git commit -m "feat(code-editor): CodeMirror 6 primitive — value/onChange/extensions/readOnly"
```

---

## Task 3: Bridge wrappers for file editing

**Files:**
- Modify: `frontend/src/bridge/codegraph.ts`

`CodeGraphListFiles`/`CodeGraphReadFile`/`CodeGraphWriteFile` are already generated in `frontend/bindings/devkit/internal/bridge/appservice.ts` (confirmed: `CodeGraphListFiles(): $CancellablePromise<codegraph$0.FileEntry[] | null>`, `CodeGraphReadFile(relativePath: string): $CancellablePromise<string>`, `CodeGraphWriteFile(relativePath: string, content: string): $CancellablePromise<void>`) — this task only adds the thin wrapper functions, matching this file's existing convention exactly (`FileEntry`'s fields already match camelCase 1:1, same as every other CodeGraph type here — no snake_case remapping needed).

- [ ] **Step 1: Add the types and functions**

```ts
// Add to frontend/src/bridge/codegraph.ts, below CodeGraphSymbolMatch

export type CodeGraphFileEntry = {
  path: string;
  isDir: boolean;
};

export async function listCodeGraphFiles(): Promise<CodeGraphFileEntry[]> {
  return callBridge(async () => {
    const list = await AppService.CodeGraphListFiles();
    return (list ?? []).map((f) => ({ path: f.path, isDir: f.isDir }));
  });
}

export async function readCodeGraphFile(relativePath: string): Promise<string> {
  return callBridge(() => AppService.CodeGraphReadFile(relativePath));
}

export async function writeCodeGraphFile(relativePath: string, content: string): Promise<void> {
  return callBridge(() => AppService.CodeGraphWriteFile(relativePath, content));
}
```

- [ ] **Step 2: Typecheck**

```bash
cd frontend && bun run build
```

Expected: succeeds.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/bridge/codegraph.ts
git commit -m "feat(codegraph): frontend bridge wrappers for ListFiles/ReadFile/WriteFile"
```

---

## Task 4: `FileTree` — virtualized, expandable file/directory list

**Files:**
- Create: `frontend/src/modules/codegraph/FileTree.tsx`

`ListFiles` returns a flat, already-lexically-ordered list (Go's `filepath.WalkDir` visits each directory's entries in lexical order, so no client-side re-sort is needed — confirmed by reading `Service.ListFiles`'s implementation). This task turns that flat list plus a set of expanded directory paths into the subset of rows that should actually render, then virtualizes that subset.

- [ ] **Step 1: Write the component**

```tsx
// frontend/src/modules/codegraph/FileTree.tsx
import { useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { CodeGraphFileEntry } from "../../bridge/codegraph";

type FileTreeProps = {
  entries: CodeGraphFileEntry[];
  selectedPath: string | null;
  onSelectFile: (path: string) => void;
};

function depthOf(path: string): number {
  return path.split("/").length - 1;
}

function parentOf(path: string): string | null {
  const idx = path.lastIndexOf("/");
  return idx === -1 ? null : path.slice(0, idx);
}

// visibleEntries returns, in order, every entry whose full ancestor chain is
// in expanded — a depth-0 entry (no "/" in its path) is always visible.
// Exported as a plain function (not inlined in the component) so its logic
// can be read and reasoned about on its own — there is no test runner in
// this frontend to exercise it automatically, so this is deliberately kept
// simple enough to verify by inspection.
export function visibleEntries(entries: CodeGraphFileEntry[], expanded: Set<string>): CodeGraphFileEntry[] {
  return entries.filter((e) => {
    let parent = parentOf(e.path);
    while (parent !== null) {
      if (!expanded.has(parent)) return false;
      parent = parentOf(parent);
    }
    return true;
  });
}

export function FileTree({ entries, selectedPath, onSelectFile }: FileTreeProps) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const rows = useMemo(() => visibleEntries(entries, expanded), [entries, expanded]);

  const parentRef = useRef<HTMLDivElement | null>(null);
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 28,
    overscan: 12,
  });

  function toggle(path: string) {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }

  return (
    <div ref={parentRef} className="h-full overflow-y-auto">
      <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
        {virtualizer.getVirtualItems().map((item) => {
          const entry = rows[item.index];
          const depth = depthOf(entry.path);
          const name = entry.path.slice(entry.path.lastIndexOf("/") + 1);
          return (
            <button
              key={entry.path}
              className={`absolute left-0 top-0 flex w-full items-center gap-1 truncate px-2 text-left text-sm hover:bg-surface-raised ${
                selectedPath === entry.path ? "bg-surface-raised" : ""
              }`}
              style={{
                height: item.size,
                transform: `translateY(${item.start}px)`,
                paddingLeft: `${depth * 16 + 8}px`,
              }}
              onClick={() => (entry.isDir ? toggle(entry.path) : onSelectFile(entry.path))}
            >
              <span className="text-muted-foreground">{entry.isDir ? (expanded.has(entry.path) ? "▾" : "▸") : ""}</span>
              <span className={entry.isDir ? "font-medium" : ""}>{name}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

```bash
cd frontend && bun run build
```

Expected: succeeds. Not imported anywhere yet.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/codegraph/FileTree.tsx
git commit -m "feat(codegraph): FileTree — virtualized, expandable file/directory list"
```

---

## Task 5: Wire the "Files" tab into `CodeGraphView`

**Files:**
- Modify: `frontend/src/modules/codegraph/CodeGraphView.tsx`

- [ ] **Step 1: Add a language-by-extension helper and the file panel state**

Add near the top of `CodeGraphView.tsx` (imports first):

```tsx
import { keymap } from "@codemirror/view";
import type { Extension } from "@codemirror/state";
import { java } from "@codemirror/lang-java";
import { json } from "@codemirror/lang-json";
import { yaml } from "@codemirror/lang-yaml";
import { markdown } from "@codemirror/lang-markdown";
import { CodeEditor } from "@/components/code-editor/CodeEditor";
import { FileTree } from "./FileTree";
import {
  listCodeGraphFiles,
  readCodeGraphFile,
  writeCodeGraphFile,
  type CodeGraphFileEntry,
} from "../../bridge/codegraph";

// Mirrors internal/codegraph/service.go's languageForPath exactly (same 4
// extensions, same default-to-none for anything else) — kept in sync by
// inspection since Go and TS can't share this switch directly.
function languageExtensionForPath(path: string): Extension[] {
  const ext = path.split(".").pop()?.toLowerCase();
  switch (ext) {
    case "java":
      return [java()];
    case "json":
      return [json()];
    case "yaml":
    case "yml":
      return [yaml()];
    case "md":
      return [markdown()];
    default:
      return [];
  }
}
```

- [ ] **Step 2: Add file-panel state and handlers inside `CodeGraphView`**

```tsx
  const [files, setFiles] = useState<CodeGraphFileEntry[]>([]);
  const [selectedFile, setSelectedFile] = useState<string | null>(null);
  const [fileContent, setFileContent] = useState("");
  const [fileDirty, setFileDirty] = useState(false);
  const [fileError, setFileError] = useState("");
  const fileContentRef = useRef(fileContent);
  fileContentRef.current = fileContent;
  const selectedFileRef = useRef(selectedFile);
  selectedFileRef.current = selectedFile;

  async function loadFiles() {
    try {
      setFiles(await listCodeGraphFiles());
    } catch (e) {
      setFileError(errorMessage(e));
    }
  }

  async function openFile(path: string) {
    setFileError("");
    try {
      const content = await readCodeGraphFile(path);
      setSelectedFile(path);
      setFileContent(content);
      setFileDirty(false);
    } catch (e) {
      setFileError(errorMessage(e));
    }
  }

  async function saveCurrentFile() {
    const path = selectedFileRef.current;
    if (!path) return;
    try {
      await writeCodeGraphFile(path, fileContentRef.current);
      setFileDirty(false);
    } catch (e) {
      setFileError(errorMessage(e));
    }
  }

  const saveKeymap = useMemo(
    () =>
      keymap.of([
        {
          key: "Mod-s",
          run: () => {
            void saveCurrentFile();
            return true;
          },
        },
      ]),
    [],
  );

  const editorExtensions = useMemo(
    () => (selectedFile ? [...languageExtensionForPath(selectedFile), saveKeymap] : []),
    [selectedFile, saveKeymap],
  );
```

Add `useMemo` and `useRef` to the existing `import { useState } from "react";` line, making it `import { useMemo, useRef, useState } from "react";`.

`loadFiles` needs to be called after a successful re-index — call it from the existing `reindex` function, right after `listCodeGraphEndpoints()` succeeds:

```tsx
      const list = await listCodeGraphEndpoints();
      setEndpoints(list);
      await loadFiles();
      setStatus(`Indexed. ${list.length} endpoint(s) found.`);
```

- [ ] **Step 3: Add the Files tab to the render output**

CodeGraphView currently renders one panel (endpoints + flow). Add a simple tab switch above it — this plan keeps the tab mechanism minimal (local `useState`, not a shared Tabs primitive) since introducing one is out of scope here:

```tsx
  const [activeTab, setActiveTab] = useState<"endpoints" | "files">("endpoints");
```

Wrap the existing `<div className="flex flex-1 gap-4 overflow-hidden">...</div>` block in a tab switch, and add the Files panel next to it:

```tsx
      <div className="flex items-center gap-2 border-b pb-2">
        <Button variant={activeTab === "endpoints" ? "secondary" : "ghost"} size="sm" onClick={() => setActiveTab("endpoints")}>
          Endpoints
        </Button>
        <Button variant={activeTab === "files" ? "secondary" : "ghost"} size="sm" onClick={() => setActiveTab("files")}>
          Files
        </Button>
      </div>

      {activeTab === "endpoints" && (
        <div className="flex flex-1 gap-4 overflow-hidden">
          {/* existing endpoint list + flow panel, unchanged */}
        </div>
      )}

      {activeTab === "files" && (
        <div className="flex flex-1 gap-4 overflow-hidden">
          <div className="w-1/4 overflow-hidden border-r">
            <FileTree entries={files} selectedPath={selectedFile} onSelectFile={openFile} />
          </div>
          <div className="flex flex-1 flex-col overflow-hidden">
            {selectedFile ? (
              <>
                <div className="flex items-center justify-between border-b px-2 py-1 text-sm text-muted-foreground">
                  <span className="truncate font-mono">{selectedFile}</span>
                  {fileDirty && <span className="text-status-warning">unsaved</span>}
                </div>
                <div className="flex-1 overflow-hidden">
                  <CodeEditor
                    value={fileContent}
                    onChange={(v) => {
                      setFileContent(v);
                      setFileDirty(true);
                    }}
                    extensions={editorExtensions}
                  />
                </div>
              </>
            ) : (
              <p className="p-4 text-sm text-muted-foreground">Select a file to edit it. Ctrl/Cmd+S saves.</p>
            )}
          </div>
        </div>
      )}
      {fileError && <p className="text-sm text-destructive">{fileError}</p>}
```

- [ ] **Step 4: Typecheck**

```bash
cd frontend && bun run build
```

Expected: succeeds. If `keymap`'s binding shape (`{key, run}`) doesn't match what `@codemirror/view`'s installed version expects, the compiler will name the real shape — fix to match what TypeScript reports rather than this snippet, same as every other CodeMirror-facing step in this plan.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/codegraph/CodeGraphView.tsx
git commit -m "feat(codegraph): Files tab — browse, open, and edit project files"
```

---

## Task 6: Manual QA script

**Files:** none (verification only)

- [ ] **Step 1: Run the app**

```bash
task run:server
```

- [ ] **Step 2: Open a real Java project** (or any folder — file editing works regardless of whether the Java analyzer has been implemented/wired yet)

Click "Open Java project…", pick a folder with a nested directory structure (e.g. `src/main/java/...`). Confirm:
- The Files tab shows top-level entries only, directories collapsed.
- Clicking a directory expands it in place (▸ becomes ▾), revealing its children indented one level deeper — clicking again collapses it and hides its children.
- Clicking a file loads it into the editor pane, with syntax highlighting if it's `.java`/`.json`/`.yaml`/`.md`, plain text otherwise.

- [ ] **Step 3: Verify editing and saving**

Type a change into the editor. Confirm the "unsaved" indicator appears. Press Ctrl+S (or Cmd+S on macOS). Confirm the indicator clears. Re-open the same file from disk in another editor (or click a different file then click back) to confirm the change actually persisted to disk, not just in memory.

- [ ] **Step 4: Verify switching files preserves per-file editor state correctly**

Open file A, make an unsaved edit, switch to file B without saving A, then switch back to A. Expected (and current, deliberate scope): **A's unsaved edit is lost** — this plan's state is keyed by a single `fileContent`/`selectedFile` pair, not a per-file buffer cache. Confirm this is what actually happens (not a crash, not silently-wrong content) and treat it as expected v1 behavior, not a bug to fix in this pass — note it below.

- [ ] **Step 5: Verify theme switching doesn't break the editor**

While a file is open in the editor, toggle the app's light/dark theme (Settings). Confirm the editor's own theme updates (or at minimum, doesn't crash/go blank) without losing the currently-typed content or cursor position — this is `CodeEditor`'s theme `Compartment` reconfiguring live, the exact behavior Task 2 built it to support.

---

## What this plan does NOT do

**No per-file unsaved-buffer cache across file switches** — switching files with unsaved changes silently discards them (Task 6, Step 4 turns this into a confirmed, known behavior rather than an unverified assumption). Add a `Map<path, {content, dirty}>` cache in a follow-up if this proves too surprising in practice — deliberately not built preemptively. **No Database query console integration** — the primitive's first consumer per the design doc, entirely separate unstarted work (SQL dialect + schema-aware completion). **No "Click node → xem code" preview/edit toggle** — that flow belongs to the still-deferred Graph UI canvas plan (it needs a clickable node to originate from); this plan only covers FR-9 through FR-12 (the file *panel*, opened independently of the graph). **No generic `Sidebar`/`VirtualTree` primitive extracted** — see this plan's "Scope decision" above. **No format-on-save, no lint** — explicit non-goals in the design doc.
