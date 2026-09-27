import { useEffect, useMemo, useRef } from "react";
import { RotateCcw, TriangleAlert } from "lucide-react";
import { keymap, type EditorView } from "@codemirror/view";
import { CodeEditor, type RevealTarget } from "@/components/code-editor/CodeEditor";
import { codeIntel, markCodeIntelBaseline, type CodeLocation } from "@/components/code-editor/codeIntel";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { tabKey } from "@/stores/workbenchTabs";
import { BlastRadiusPanel } from "./BlastRadiusPanel";
import { createCodeGraphIntelProvider } from "./codegraphIntelProvider";
import { languageExtensionForPath } from "./editorLanguage";
import { useCodeGraphStore } from "./codegraphStore";
import { discardFile, editFile, openFile, reloadFile, saveFile } from "./codegraphActions";
import { FILE_VIEW, MODULE } from "./documents";
import { useProjectReady } from "./useDocument";

const onOpenFile = (path: string) => openFile(path);

// Module-level so every file's codeIntel() closes over the same identities:
// the extension list below is memoized per path, and the provider holds no
// per-editor state, so one answers for every file.
const intelProvider = createCodeGraphIntelProvider();
const onNavigate = (location: CodeLocation, { beside }: { beside: boolean }) =>
  openFile(location.path, location.line, { beside });

/**
 * One file as a tab, open for editing, with file impact available in its toolbar.
 *
 * Each file is its own buffer, so opening another never asks to throw edits
 * away — the old single-editor pane had to, with a Discard dialog. Closing
 * the tab keeps unsaved edits too; opening the file again finds them.
 */
export function FileDocument({ params }: { params?: Record<string, unknown> }) {
  const path = typeof params?.path === "string" ? params.path : "";
  const tabId = tabKey(MODULE, FILE_VIEW, params);
  const ready = useProjectReady();
  const projectPath = useCodeGraphStore((s) => s.projectPath);
  const buffer = useCodeGraphStore((s) => s.buffers[path]);

  // Restored after a relaunch: read once the project can answer.
  useEffect(() => {
    if (ready && path && !buffer) openFile(path);
  }, [ready, path, buffer]);

  // Per path: the keymap reads the store when pressed, never a stale buffer.
  const extensions = useMemo(
    () => [
      ...languageExtensionForPath(path),
      keymap.of([{ key: "Mod-s", run: () => (void saveFile(path), true) }]),
      codeIntel({ path, provider: intelProvider, onNavigate }),
    ],
    [path],
  );

  // Once a save lands, the index will describe the text on screen, so that is
  // what go-to-definition should count lines from (see codeIntel.ts's
  // baseline). Watching dirty go false rather than hooking the save itself:
  // Mod-S and the Save button both end here, and saveFile only clears dirty
  // when what it wrote is still what the buffer holds.
  const viewRef = useRef<EditorView | null>(null);
  const dirty = buffer?.dirty ?? false;
  const wasDirty = useRef(dirty);
  useEffect(() => {
    if (wasDirty.current && !dirty && viewRef.current) markCodeIntelBaseline(viewRef.current);
    wasDirty.current = dirty;
  }, [dirty]);

  const line = buffer?.line ?? null;
  const revealToken = buffer?.revealToken ?? 0;
  const revealTarget = useMemo<RevealTarget>(
    () => (line === null ? null : { line, token: revealToken }),
    [line, revealToken],
  );

  if (!buffer || (buffer.loading && !buffer.content)) {
    return <EmptyState busy title={`Opening ${path.slice(path.lastIndexOf("/") + 1)}`} body={path} />;
  }
  if (buffer.error && !buffer.content && !buffer.dirty) {
    return (
      <EmptyState icon={TriangleAlert} tone="danger" title="Could not open this file" body={buffer.error}>
        <Button variant="outline" className="bg-surface-raised" onClick={() => reloadFile(path)}>
          Try again
          <RotateCcw className="size-4" />
        </Button>
      </EmptyState>
    );
  }

  // A buffer left over from a project switch (kept only because it had
  // unsaved edits — see switchProject). Saving it is refused here and by Go.
  const orphaned = Boolean(buffer.root) && buffer.root !== projectPath;

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      <div className="flex h-8 shrink-0 items-center justify-between gap-3 border-b border-border-subtle px-4">
        <span className="min-w-0 truncate text-code-11 text-text-secondary" title={path}>
          {path}
        </span>
        <div className="flex shrink-0 items-center gap-2">
          {buffer.dirty ? (
            <span className="flex shrink-0 items-center gap-2">
              <span className="text-code-11 text-status-warning">unsaved</span>
              {!orphaned ? (
                <Button size="xs" variant="outline" className="bg-surface-raised" onClick={() => void saveFile(path)}>
                  Save
                </Button>
              ) : null}
            </span>
          ) : null}
          {!orphaned ? <BlastRadiusPanel key={path} filePath={path} onOpenFile={onOpenFile} /> : null}
        </div>
      </div>
      {orphaned ? (
        <div role="alert" className="flex shrink-0 items-center gap-3 border-b border-border-subtle px-4 py-2">
          <p className="min-w-0 flex-1 text-body-12 text-status-warning">
            This file is from {buffer.root}, not the project now open, and cannot be saved here. Copy your edits before closing it.
          </p>
          <Button size="xs" variant="outline" className="bg-surface-raised" onClick={() => void navigator.clipboard?.writeText(buffer.content)}>
            Copy
          </Button>
          <Button size="xs" variant="ghost" onClick={() => discardFile(path)}>
            Discard
          </Button>
        </div>
      ) : null}
      <div className="min-h-0 flex-1 overflow-hidden">
        <CodeEditor
          value={buffer.content}
          revealLine={revealTarget}
          onChange={(content) => editFile(path, content, tabId)}
          extensions={extensions}
          onView={(view) => {
            viewRef.current = view;
          }}
          wrap="auto"
        />
      </div>
      {buffer.error ? (
        <p role="alert" className="shrink-0 px-4 py-1 text-body-12 text-status-danger">
          {buffer.error}
        </p>
      ) : null}
    </div>
  );
}
