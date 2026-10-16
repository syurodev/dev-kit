import { useMemo, useState } from "react";
import { ChevronRight, ListTree } from "lucide-react";
import { SidebarSection } from "@/components/ui/sidebar-shell";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { useCodeGraphStore } from "../codegraphStore";
import { openFile } from "../fileActions";
import { useCursor } from "../cursor";
import { shortLabel } from "../graph/labels";
import { countSymbols, outlineRows, symbolPathAt } from "../workspace/outline";
import { ago } from "../workspace/indexHealth";
import { isPdfPath } from "../viewers/viewerKind";

const FILTER_FROM = 15;

/**
 * The declarations of the file in front: classes and methods nested by range,
 * from the index, with the one the caret is in marked.
 *
 * The index describes the SAVED file as of its last run, so the section says
 * when the file has unsaved edits (or is saved but not yet reindexed) rather than
 * pointing at lines that may have moved. A file the analyzer does not read has
 * no outline and the section says that instead of looking empty; opening it,
 * reading it and editing it are unaffected.
 */
export function OutlineSection({
  docKey: key,
  workspaceId,
  folderId,
  path,
  open,
  onToggle,
}: {
  /** Empty when no file is in front: the section is still there, so opening one does not move the tree. */
  docKey: string;
  workspaceId: string;
  folderId: string;
  path: string;
  open: boolean;
  onToggle: () => void;
}) {
  const outline = useCodeGraphStore((s) => (key ? s.outlines[key] : undefined));
  const dirty = useCodeGraphStore((s) => s.buffers[key]?.dirty ?? false);
  const savedAt = useCodeGraphStore((s) => s.buffers[key]?.reloadedAt ?? 0);
  const line = useCursor(key);
  const [filter, setFilter] = useState("");
  const total = useMemo(() => (outline ? countSymbols(outline.symbols) : 0), [outline]);
  const current = useMemo(() => (outline && line > 0 ? new Set(symbolPathAt(outline.symbols, line).map((s) => s.id)) : new Set<string>()), [outline, line]);
  const rows = useMemo(() => (outline ? outlineRows(outline.symbols, filter, (s) => shortLabel(s.label, s.kind)) : []), [outline, filter]);

  let note = "";
  if (!key) note = "Open a file to see what it declares.";
  // A PDF has nothing the index could declare, and no outline is ever read for it.
  else if (isPdfPath(path)) note = "A PDF has no outline.";
  else if (outline) {
    if (outline.state === "unsupported") note = "No analyzer reads this folder, so there is no outline. The file can still be read and edited.";
    else if (outline.state === "not-indexed") note = "This folder has no index yet — reindex it to get an outline.";
    else if (outline.state === "indexing") note = "Waiting for the index…";
    else if (total === 0) note = "Nothing declared in this file that the index knows.";
  }
  void savedAt;

  return (
    <>
      <SidebarSection label="Outline" count={outline && outline.state === "indexed" ? total : undefined} open={open} onToggle={onToggle} />
      {open ? (
        // A fixed height: a taller outline must not squeeze the file tree above it, which would move
        // the row that was just clicked out from under a double click.
        <div className="flex h-44 shrink-0 flex-col gap-1 pb-2">
          {outline?.state === "indexed" && outline.indexedAt ? (
            <p className="px-3 text-caption-11 text-text-tertiary">
              {dirty ? "Unsaved edits are not reflected — " : ""}from the index of {ago(outline.indexedAt, Date.now())}.
            </p>
          ) : null}
          {total >= FILTER_FROM ? (
            <div className="px-1.5">
              <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter symbols" aria-label="Filter symbols" className="h-(--control-height-sm) border-border-subtle bg-surface-well text-code-11" />
            </div>
          ) : null}
          {note ? (
            <p className="flex items-start gap-1.5 px-3 text-caption-11 text-text-tertiary" role="status">
              <ListTree className="mt-0.5 size-3 shrink-0" aria-hidden />
              {outline?.loading && !outline.symbols.length ? "Reading the outline…" : note}
            </p>
          ) : null}
          {key && !outline && !isPdfPath(path) ? <p className="px-3 text-caption-11 text-text-tertiary">Reading the outline…</p> : null}
          {outline?.error ? <p role="alert" className="px-3 text-caption-11 text-status-danger">{outline.error}</p> : null}
          <div className="min-h-0 flex-1 overflow-y-auto" role="tree" aria-label="Outline">
            {rows.map(({ symbol, depth }) => (
              <button
                key={symbol.id}
                type="button"
                role="treeitem"
                aria-selected={current.has(symbol.id)}
                onClick={() => void openFile({ workspaceId, folderId, path }, symbol.line, { preview: false })}
                style={{ paddingLeft: 12 + depth * 12 }}
                className={cn(
                  "flex h-6 w-full items-center gap-1 pr-3 text-left text-body-12",
                  current.has(symbol.id) ? "bg-surface-raised text-text-primary" : "text-text-secondary hover:bg-state-soft",
                )}
                title={`${symbol.label} (line ${symbol.line})`}
              >
                <ChevronRight className="size-2.5 shrink-0 opacity-0" aria-hidden />
                <span className="min-w-0 flex-1 truncate">{shortLabel(symbol.label, symbol.kind)}</span>
                <span className="shrink-0 text-code-11 text-text-tertiary">{symbol.line}</span>
              </button>
            ))}
          </div>
        </div>
      ) : null}
    </>
  );
}
