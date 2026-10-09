import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { RotateCcw, TriangleAlert } from "lucide-react";
import { Prec } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { CodeEditor, type RevealTarget } from "@/components/code-editor/CodeEditor";
import { codeIntel, markCodeIntelBaseline, savedLineAt, type CodeIntelPanelRequest, type CodeLocation } from "@/components/code-editor/codeIntel";
import { languageForPath } from "@/components/code/languages";
import { useLanguageExtension } from "@/components/code/useLanguageExtension";
import { detectPlatform } from "@/lib/platform";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { useSplitPanel } from "@/components/ui/split-panel";
import { tabKey, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { BlastRadiusPanel } from "./BlastRadiusPanel";
import { createCodeGraphIntelProvider } from "./codegraphIntelProvider";
import { useCodeGraphStore } from "./codegraphStore";
import { discardFile, editFile, ensureBuffer, goBack, goForward, keepMine, loadOutline, openFile, reloadFile, reloadFromDisk, saveFile } from "./fileActions";
import { describeParams, FILE_VIEW, MODULE } from "./documents";
import { registerEditor } from "./navigation";
import { setCursorLine } from "./cursor";
import { set } from "./storeAccess";
import { useDocumentOwnership, useIsFrontDocument } from "./useDocument";
import { docKey } from "./workspace/identity";
import { ExternalChangeBanner, MixedEolNote, OrphanBanner } from "./file/FileBanners";
import { CompareDialog } from "./file/CompareDialog";
import { FileToolbar, SavingMark } from "./file/FileToolbar";
import { NavigationPanel, closePanel } from "./file/NavigationPanel";

const platform = detectPlatform();

/**
 * One file as a tab, open for editing.
 *
 * The file is `(workspace, folder, path)` — read from, saved into and watched in
 * that folder whatever the primary is and whichever workspace the title bar now
 * shows. Each file is its own buffer, so opening another never asks to throw
 * edits away, and closing the tab keeps unsaved ones. The disk is watched: a
 * clean buffer follows it, a dirty one raises a conflict that only the person
 * resolves (file/FileBanners.tsx).
 */
export function FileDocument({ params }: { params?: Record<string, unknown> }) {
  const doc = describeParams(FILE_VIEW, params);
  const path = doc.path ?? "";
  const tabId = tabKey(MODULE, FILE_VIEW, params);
  const ownership = useDocumentOwnership(doc);
  const { owner, folder, relation } = ownership;
  const key = owner && path ? docKey(owner.workspaceId, owner.folderId, path) : "";
  const buffer = useCodeGraphStore((s) => (key ? s.buffers[key] : undefined));
  const front = useIsFrontDocument(tabId);
  const [compareOpen, setCompareOpen] = useState(false);

  // A tab restored after a relaunch has no buffer yet: read it once its owner is known.
  useEffect(() => {
    if (owner && path && !buffer && relation !== "removed") ensureBuffer({ ...owner, path });
  }, [owner, path, buffer, relation]);

  // The outline of the file in front, for the breadcrumb and the sidebar's Outline.
  useEffect(() => {
    if (front && key && buffer && !buffer.loading && !buffer.orphan) void loadOutline(key);
  }, [front, key, buffer?.loading, buffer?.orphan]); // eslint-disable-line react-hooks/exhaustive-deps

  const frontRef = useRef(front);
  frontRef.current = front;
  const keyRef = useRef(key);
  keyRef.current = key;

  const folderId = owner?.folderId ?? "";
  const workspaceId = owner?.workspaceId ?? "";
  const provider = useMemo(() => (folder ? createCodeGraphIntelProvider({ id: folder.id, path: folder.path }) : null), [folder?.id, folder?.path]); // eslint-disable-line react-hooks/exhaustive-deps

  const onNavigate = useCallback(
    (location: CodeLocation, { beside }: { beside: boolean }) =>
      void openFile({ workspaceId, folderId: location.owner ?? folderId, path: location.path }, location.line, { beside, preview: false }),
    [workspaceId, folderId],
  );
  const onPanel = useCallback(
    (request: CodeIntelPanelRequest) =>
      set({
        panel: {
          mode: request.mode,
          source: { workspaceId, folderId, path, line: request.line },
          identifier: request.identifier,
          locations: request.locations.map((l) => ({
            folderId: l.owner ?? folderId,
            path: l.path,
            line: l.line,
            label: l.label,
            kind: l.kind,
            preview: l.preview,
          })),
          selected: 0,
        },
      }),
    [workspaceId, folderId, path],
  );

  // Per file: the keymap reads the store when pressed, never a stale buffer.
  // Its own memo, apart from the language: the grammar arrives a frame later,
  // and rebuilding codeIntel then would throw away its save baseline.
  const fileExtensions = useMemo(() => {
    if (!key || !provider) return [];
    const back = platform === "mac" ? [] : [{ key: "Alt-ArrowLeft", run: () => (void goBack(), true) }, { key: "Alt-ArrowRight", run: () => (void goForward(), true) }];
    return [
      Prec.highest(
        keymap.of([
          { key: "Mod-s", run: () => (void saveFile(keyRef.current), true) },
          { key: "Mod-Alt-ArrowLeft", run: () => (void goBack(), true) },
          { key: "Mod-Alt-ArrowRight", run: () => (void goForward(), true) },
          ...back,
        ]),
      ),
      codeIntel({ path, owner: folderId, provider, onNavigate, onPanel }),
      EditorView.updateListener.of((update) => {
        if ((update.selectionSet || update.docChanged) && frontRef.current) {
          setCursorLine(keyRef.current, savedLineAt(update.state, update.state.selection.main.head));
        }
      }),
    ];
  }, [key, path, folderId, provider, onNavigate, onPanel]);
  const language = useLanguageExtension(languageForPath(path));
  const extensions = useMemo(() => [...language, ...fileExtensions], [language, fileExtensions]);

  // Once a save lands, the index will describe the text on screen, so that is
  // what go-to-definition should count lines from (see codeIntel.ts's
  // baseline). Watching dirty go false rather than hooking the save itself:
  // Mod-S and the Save button both end here.
  const viewRef = useRef<EditorView | null>(null);
  const dirty = buffer?.dirty ?? false;
  const wasDirty = useRef(dirty);
  useEffect(() => {
    if (wasDirty.current && !dirty && viewRef.current) markCodeIntelBaseline(viewRef.current);
    wasDirty.current = dirty;
  }, [dirty]);

  const unregister = useRef<(() => void) | null>(null);
  const onView = useCallback((view: EditorView | null) => {
    viewRef.current = view;
    unregister.current?.();
    unregister.current = null;
    if (!view || !keyRef.current) return;
    unregister.current = registerEditor(keyRef.current, {
      probe: () => {
        const head = view.state.selection.main;
        const line = view.state.doc.lineAt(head.head);
        return { line: line.number, column: head.head - line.from + 1, anchor: head.anchor, head: head.head, scrollTop: view.scrollDOM.scrollTop };
      },
      focus: () => view.focus(),
    });
  }, []);
  useEffect(() => () => unregister.current?.(), []);

  useNavigationPane(front);

  const restore = buffer?.restore ?? null;
  const revealTarget = useMemo<RevealTarget>(
    () => (restore ? { line: restore.line, token: restore.token, column: restore.column, anchor: restore.anchor, head: restore.head, scrollTop: restore.scrollTop } : null),
    [restore],
  );

  const closeTab = () => useWorkbenchTabsStore.getState().closeTab(tabId);

  if (!owner) {
    return (
      <EmptyState
        icon={TriangleAlert}
        title="Which folder is this file in?"
        body="This tab was saved before documents named their folder, and it cannot be told from the workspace now. Open the file again from Files."
      >
        <Button variant="outline" className="bg-surface-raised" onClick={closeTab}>Close this tab</Button>
      </EmptyState>
    );
  }
  if (!buffer && relation === "removed") {
    return (
      <EmptyState icon={TriangleAlert} title="This file's folder left the workspace" body={path}>
        <Button variant="outline" className="bg-surface-raised" onClick={closeTab}>Close this tab</Button>
      </EmptyState>
    );
  }
  if (!buffer || (buffer.loading && !buffer.content)) {
    return <EmptyState busy title={`Opening ${path.slice(path.lastIndexOf("/") + 1)}`} body={path} />;
  }
  if (buffer.error && !buffer.content && !buffer.dirty) {
    return (
      <EmptyState icon={TriangleAlert} tone="danger" title="Could not open this file" body={buffer.error}>
        <Button variant="outline" className="bg-surface-raised" onClick={() => reloadFile(key)}>
          Try again
          <RotateCcw className="size-4" />
        </Button>
      </EmptyState>
    );
  }

  // A file of another workspace, or of a folder that left this one, still says whose it is.
  const origin = buffer.origin;
  const label =
    ownership.label ||
    (relation === "other-workspace"
      ? origin
        ? `${origin.workspace} › ${origin.folder}`
        : "another workspace"
      : origin
        ? `${origin.folder} (removed)`
        : "removed folder");
  const orphaned = buffer.orphan !== null;
  const conflicted = buffer.external !== null && !orphaned;

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      <FileToolbar
        workspaceId={workspaceId}
        folderId={folderId}
        folderLabel={label}
        path={path}
        buffer={buffer}
        right={
          <>
            {buffer.saving ? <SavingMark /> : null}
            {buffer.reloadedAt > 0 ? <ReloadedMark at={buffer.reloadedAt} /> : null}
            {buffer.dirty ? (
              <span className="flex shrink-0 items-center gap-2">
                <span className="text-code-11 text-status-warning">unsaved</span>
                {!orphaned && !conflicted ? (
                  <Button size="xs" variant="outline" className="bg-surface-raised" disabled={buffer.saving} onClick={() => void saveFile(key)}>
                    Save
                  </Button>
                ) : null}
              </span>
            ) : null}
            {!orphaned && folder ? <BlastRadiusPanel key={key} folderId={folderId} filePath={path} onOpenFile={(p) => void openFile({ workspaceId, folderId, path: p }, null, { preview: false })} /> : null}
          </>
        }
      />
      {orphaned ? (
        <OrphanBanner buffer={buffer} folderLabel={label} onDiscard={() => discardFile(key)} />
      ) : conflicted ? (
        <>
          <ExternalChangeBanner
            buffer={buffer}
            onCompare={() => setCompareOpen(true)}
            onReload={() => void reloadFromDisk(key)}
            onKeepMine={() => keepMine(key)}
            onOverwrite={() => void saveFile(key, "overwrite")}
            onRecreate={() => void saveFile(key, "recreate")}
            onClose={() => (buffer.dirty ? discardFile(key) : closeTab())}
          />
          <CompareDialog
            open={compareOpen}
            onOpenChange={setCompareOpen}
            bufferKey={key}
            buffer={buffer}
            onReload={() => void reloadFromDisk(key)}
            onOverwrite={() => void saveFile(key, "overwrite")}
          />
        </>
      ) : null}
      {buffer.mixedEol ? <MixedEolNote eol={buffer.eol} /> : null}
      <div className="min-h-0 flex-1 overflow-hidden">
        <CodeEditor
          // Line endings are fixed when the editor is created.
          key={buffer.eol}
          value={buffer.content}
          revealLine={revealTarget}
          onChange={(content) => editFile(key, content, tabId)}
          extensions={extensions}
          onView={onView}
          lineSeparator={buffer.eol}
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

/** "Reloaded from disk" for a few seconds after a clean buffer followed a change. */
function ReloadedMark({ at }: { at: number }) {
  const [fresh, setFresh] = useState(() => Date.now() - at < 4000);
  useEffect(() => {
    const left = 4000 - (Date.now() - at);
    if (left <= 0) {
      setFresh(false);
      return;
    }
    setFresh(true);
    const t = window.setTimeout(() => setFresh(false), left);
    return () => window.clearTimeout(t);
  }, [at]);
  return fresh ? (
    <span role="status" className="text-code-11 text-text-tertiary">
      reloaded from disk
    </span>
  ) : null;
}

/**
 * The references / peek panel, in this module's context pane while a file is in
 * front. The panel's state is the store's, not this document's: choosing a
 * result opens another file, whose document shows the same panel.
 */
function useNavigationPane(front: boolean) {
  const panel = useCodeGraphStore((s) => s.panel);
  const { openPanel, closePanel: closePane } = useSplitPanel();
  useEffect(() => {
    if (!front || !panel) return;
    openPanel(<NavigationPanel panel={panel} />, { width: 480, onClose: closePanel });
    return () => closePane();
  }, [front, panel, openPanel, closePane]);
}
