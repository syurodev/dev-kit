import { useEffect, useMemo, useRef, useState } from "react";
import { Columns2, ExternalLink, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { CodeEditor, type RevealTarget } from "@/components/code-editor/CodeEditor";
import { splitPreviewSegments } from "@/components/code-editor/codeIntel";
import { languageForPath } from "@/components/code/languages";
import { useLanguageExtension } from "@/components/code/useLanguageExtension";
import { cn } from "@/lib/utils";
import { errorMessage } from "@/bridge/errors";
import { readFolderFile } from "@/bridge/workspaceCode";
import { useCodeGraphStore, type NavigationPanelState, type PanelLocation } from "../codegraphStore";
import { openFile } from "../fileActions";
import { focusEditor } from "../navigation";
import { set } from "../storeAccess";
import { useFolderLabel } from "../useDocument";
import { baseName, docKey, treeFileId } from "../workspace/identity";

const noop = () => undefined;
/** A peek shows the whole file up to this size; beyond it, a window around the target. */
const WHOLE_FILE_BYTES = 300_000;
const WINDOW_LINES = 40;

export function closePanel() {
  const panel = useCodeGraphStore.getState().panel;
  set({ panel: null });
  // The caret goes back to where the lookup was made.
  if (panel) focusEditor(docKey(panel.source.workspaceId, panel.source.folderId, panel.source.path));
}

function select(index: number) {
  const panel = useCodeGraphStore.getState().panel;
  if (panel && index >= 0 && index < panel.locations.length && index !== panel.selected) set({ panel: { ...panel, selected: index } });
}

/**
 * The references list or a peeked definition, beside the code it was asked from.
 *
 * Both are read-only views of the index's answer. References stay in the panel
 * while results are read: choosing one opens its file (the tab you are in is
 * replaced as a preview, nothing is closed) and the list is still there; Back
 * returns to where the lookup was made, because choosing a result is a recorded
 * navigation. Peek shows the target in place, in a read-only editor — never a
 * second editable copy of the file — with Open / Open beside to go there.
 * Neither follows names across folders: the locations are what the source
 * folder's index resolved, each marked with its folder.
 */
export function NavigationPanel({ panel }: { panel: NavigationPanelState }) {
  const workspaceId = panel.source.workspaceId;
  const title =
    panel.mode === "peek"
      ? `Peek · ${panel.identifier}`
      : `${panel.locations.length} reference${panel.locations.length === 1 ? "" : "s"} to ${panel.identifier}`;
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    rootRef.current?.focus();
  }, [panel.mode, panel.source.path, panel.source.line, panel.identifier]);

  const go = (loc: PanelLocation, beside: boolean) =>
    void openFile({ workspaceId, folderId: loc.folderId, path: loc.path }, loc.line, { preview: !beside, beside });

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      closePanel();
    } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      select(panel.selected + (event.key === "ArrowDown" ? 1 : -1));
    } else if (event.key === "Enter") {
      const loc = panel.locations[panel.selected];
      if (loc) {
        event.preventDefault();
        go(loc, event.metaKey || event.ctrlKey);
      }
    }
  };

  return (
    <div
      ref={rootRef}
      tabIndex={-1}
      role="region"
      aria-label={title}
      onKeyDown={onKeyDown}
      className="flex h-full min-h-0 flex-col outline-none"
    >
      <div className="flex h-9 shrink-0 items-center justify-between gap-2 border-b border-border-subtle px-3">
        <span className="min-w-0 truncate text-title-13 text-text-primary">{title}</span>
        <Button size="icon-sm" variant="ghost" aria-label="Close panel" onClick={closePanel}>
          <X className="size-3.5" aria-hidden />
        </Button>
      </div>
      {panel.mode === "peek" ? <PeekBody panel={panel} onOpen={go} /> : <ReferenceList panel={panel} onOpen={go} />}
      <p className="shrink-0 border-t border-border-subtle px-3 py-1.5 text-caption-11 text-text-tertiary">
        Resolved by the index of {panel.locations.length ? "the folder this was asked in" : "this folder"}; other folders are not searched.
        Esc closes.
      </p>
    </div>
  );
}

function ReferenceList({ panel, onOpen }: { panel: NavigationPanelState; onOpen: (loc: PanelLocation, beside: boolean) => void }) {
  const groups = useMemo(() => {
    const byFolder = new Map<string, Map<string, Array<{ loc: PanelLocation; index: number }>>>();
    panel.locations.forEach((loc, index) => {
      const files = byFolder.get(loc.folderId) ?? new Map();
      files.set(loc.path, [...(files.get(loc.path) ?? []), { loc, index }]);
      byFolder.set(loc.folderId, files);
    });
    return [...byFolder.entries()];
  }, [panel.locations]);
  const showFolders = groups.length > 1;

  return (
    <div role="listbox" aria-label="References" className="min-h-0 flex-1 overflow-y-auto py-1 scrollbar-gutter-stable">
      {groups.map(([folderId, files]) => (
        <div key={folderId}>
          {showFolders ? <FolderHeading folderId={folderId} /> : null}
          {[...files.entries()].map(([path, rows]) => (
            <div key={treeFileId(folderId, path)}>
              <div className="flex items-center gap-2 px-3 pt-1.5 pb-0.5 text-code-11 text-text-tertiary" title={path}>
                <span className="min-w-0 truncate">{path}</span>
                <span className="shrink-0">{rows.length}</span>
              </div>
              {rows.map(({ loc, index }) => (
                <button
                  key={`${index}`}
                  type="button"
                  role="option"
                  aria-selected={index === panel.selected}
                  onClick={(e) => {
                    select(index);
                    onOpen(loc, e.metaKey || e.ctrlKey);
                  }}
                  className={cn(
                    "flex w-full items-baseline gap-2 px-3 py-0.5 text-left",
                    index === panel.selected ? "bg-surface-raised" : "hover:bg-state-soft",
                  )}
                >
                  <span className="w-9 shrink-0 text-right text-code-11 tabular-nums text-text-tertiary">{loc.line}</span>
                  <span className="min-w-0 flex-1 truncate font-mono text-code-11 text-text-secondary">
                    {loc.preview ? <Highlighted text={loc.preview.trim()} needle={panel.identifier} /> : (loc.label ?? "")}
                  </span>
                  {loc.kind ? <span className="shrink-0 rounded-sm bg-state-soft px-1 text-code-11 text-text-tertiary">{loc.kind}</span> : null}
                </button>
              ))}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

function Highlighted({ text, needle }: { text: string; needle: string }) {
  return (
    <>
      {splitPreviewSegments(text, needle).map((seg, i) =>
        seg.match ? (
          <mark key={i} className="rounded-sm bg-state-soft px-0.5 text-text-primary">
            {seg.text}
          </mark>
        ) : (
          <span key={i}>{seg.text}</span>
        ),
      )}
    </>
  );
}

function FolderHeading({ folderId }: { folderId: string }) {
  const label = useFolderLabel(folderId);
  return <div className="px-3 pt-2 pb-0.5 text-label-11-caps text-text-tertiary">{label || "Folder"}</div>;
}

/** Read-only preview of the selected definition; several targets are chosen from a short list. */
function PeekBody({ panel, onOpen }: { panel: NavigationPanelState; onOpen: (loc: PanelLocation, beside: boolean) => void }) {
  const loc = panel.locations[panel.selected];
  const label = useFolderLabel(loc?.folderId);
  const workspaceId = panel.source.workspaceId;
  const key = loc ? docKey(workspaceId, loc.folderId, loc.path) : "";
  // The open buffer's text wins over the disk's: a peek shows what the editor shows.
  const buffered = useCodeGraphStore((s) => (key ? s.buffers[key]?.content : undefined));
  const [disk, setDisk] = useState<{ key: string; text: string } | { key: string; error: string } | null>(null);
  const language = useLanguageExtension(languageForPath(loc?.path ?? ""));

  useEffect(() => {
    if (!loc || buffered !== undefined) return;
    let live = true;
    readFolderFile(workspaceId, loc.folderId, loc.path).then(
      (r) => live && setDisk({ key, text: r.content }),
      (e) => live && setDisk({ key, error: errorMessage(e) }),
    );
    return () => {
      live = false;
    };
  }, [loc, buffered, key, workspaceId]);

  const text = buffered ?? (disk && disk.key === key && "text" in disk ? disk.text : null);
  const error = disk && disk.key === key && "error" in disk ? disk.error : "";
  const view = useMemo(() => (text === null ? null : windowOf(text, loc?.line ?? 1)), [text, loc?.line]);
  const reveal = useMemo<RevealTarget>(() => (view && loc ? { line: loc.line - view.offset, token: loc.line * 7919 + panel.selected } : null), [view, loc, panel.selected]);

  if (!loc) return null;
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {panel.locations.length > 1 ? (
        <div role="listbox" aria-label="Definitions" className="max-h-28 shrink-0 overflow-y-auto border-b border-border-subtle py-1">
          {panel.locations.map((l, i) => (
            <button
              key={i}
              type="button"
              role="option"
              aria-selected={i === panel.selected}
              onClick={() => select(i)}
              className={cn("flex w-full items-baseline gap-2 px-3 py-0.5 text-left", i === panel.selected ? "bg-surface-raised" : "hover:bg-state-soft")}
            >
              <span className="min-w-0 flex-1 truncate text-code-11 text-text-secondary">{l.label ?? baseName(l.path)}</span>
              <span className="shrink-0 truncate text-code-11 text-text-tertiary">{baseName(l.path)}:{l.line}</span>
            </button>
          ))}
        </div>
      ) : null}
      <div className="flex shrink-0 items-center justify-between gap-2 px-3 py-1.5">
        <span className="min-w-0 truncate text-code-11 text-text-secondary" title={`${label} › ${loc.path}`}>
          {label ? `${label} › ` : ""}
          {loc.path}:{loc.line}
        </span>
        <span className="flex shrink-0 gap-1">
          <Button size="xs" variant="outline" className="bg-surface-raised" onClick={() => onOpen(loc, false)}>
            <ExternalLink aria-hidden /> Open
          </Button>
          <Button size="xs" variant="outline" className="bg-surface-raised" onClick={() => onOpen(loc, true)}>
            <Columns2 aria-hidden /> Beside
          </Button>
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-hidden border-t border-border-subtle">
        {error ? (
          <p role="alert" className="p-3 text-body-12 text-status-danger">{error}</p>
        ) : view ? (
          <>
            {view.offset > 0 ? <p className="px-3 py-1 text-caption-11 text-text-tertiary">Showing lines {view.offset + 1}–{view.offset + view.lines}</p> : null}
            <CodeEditor key={`${key}:${view.offset}`} value={view.text} onChange={noop} readOnly wrap extensions={language} revealLine={reveal} />
          </>
        ) : (
          <p role="status" className="p-3 text-body-12 text-text-tertiary">Reading the file…</p>
        )}
      </div>
    </div>
  );
}

/** The whole text if it is small, else the lines around `line`. */
export function windowOf(text: string, line: number): { text: string; offset: number; lines: number } {
  const total = text.split("\n");
  if (text.length <= WHOLE_FILE_BYTES) return { text, offset: 0, lines: total.length };
  const from = Math.max(0, line - 1 - WINDOW_LINES);
  const to = Math.min(total.length, line + WINDOW_LINES);
  return { text: total.slice(from, to).join("\n"), offset: from, lines: to - from };
}
