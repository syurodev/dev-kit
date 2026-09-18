// frontend/src/modules/codegraph/FileTree.tsx
import { useMemo, useRef, useState } from "react";
import { ChevronRight, FileCode2, Folder } from "lucide-react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { cn } from "@/lib/utils";
import type { CodeGraphFileEntry } from "../../bridge/codegraph";

type FileTreeProps = {
  entries: CodeGraphFileEntry[];
  selectedPath: string | null;
  onSelectFile: (path: string) => void;
  /**
   * Folders to render open regardless of what the user toggled — the
   * ancestors of search matches (see fileSearch.ts). Null when no search is
   * active; the user's own expansion is untouched either way.
   */
  forceExpanded?: Set<string> | null;
  /** Git state per file (M, A, D, R), drawn at the end of its row. */
  marks?: Record<string, string>;
};

const MARK_TONE: Record<string, string> = {
  M: "text-status-warning",
  A: "text-status-success",
  D: "text-status-danger",
  R: "text-status-info",
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
// Exported as a plain function so fileSearch.test.ts can check a search match
// is actually visible, not merely present in the filtered list.
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

/** Figma `Files Tab Content` (862:33549): 28px rows, 16px per depth, chevron 10 · glyph 13 · name. */
// The tree tier (--control-height-xs). Virtualised, so the number has to be
// here as well as in the class below.
const ROW_HEIGHT = 24;

export function FileTree({ entries, selectedPath, onSelectFile, forceExpanded, marks }: FileTreeProps) {
  const [userExpanded, setExpanded] = useState<Set<string>>(new Set());
  const expanded = useMemo(
    () => (forceExpanded ? new Set([...userExpanded, ...forceExpanded]) : userExpanded),
    [userExpanded, forceExpanded],
  );
  const rows = useMemo(() => visibleEntries(entries, expanded), [entries, expanded]);

  const parentRef = useRef<HTMLDivElement | null>(null);
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => ROW_HEIGHT,
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
    // A flat, virtualised list, so the hierarchy is told through aria-level on
    // each treeitem rather than nested role="group" elements — ARIA allows
    // either, and nesting is impossible when only the visible rows exist.
    <div ref={parentRef} role="tree" aria-label="Project files" className="h-full overflow-y-auto">
      <div role="presentation" style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
        {virtualizer.getVirtualItems().map((item) => {
          const entry = rows[item.index];
          const depth = depthOf(entry.path);
          const name = entry.path.slice(entry.path.lastIndexOf("/") + 1);
          const selected = selectedPath === entry.path;
          return (
            <button
              key={entry.path}
              role="treeitem"
              aria-level={depth + 1}
              aria-expanded={entry.isDir ? expanded.has(entry.path) : undefined}
              aria-selected={entry.isDir ? undefined : selected}
              className={cn(
                "absolute top-0 left-0 flex w-full items-center gap-1.5 rounded-md border pr-2 text-left",
                selected
                  ? "border-border-subtle bg-surface-raised"
                  : "border-transparent hover:bg-state-soft",
              )}
              style={{
                height: item.size,
                transform: `translateY(${item.start}px)`,
                paddingLeft: `calc(${depth} * var(--space-tree-indent) + var(--space-inline))`,
              }}
              onClick={() => (entry.isDir ? toggle(entry.path) : onSelectFile(entry.path))}
            >
              <span className="flex size-2.5 shrink-0 items-center justify-center text-text-tertiary">
                {entry.isDir && (
                  <ChevronRight
                    size={10}
                    className={cn("transition-transform", expanded.has(entry.path) && "rotate-90")}
                  />
                )}
              </span>
              {entry.isDir ? (
                <Folder size={13} className="shrink-0 text-text-tertiary" />
              ) : (
                <FileCode2 size={13} className="shrink-0 text-text-tertiary" />
              )}
              <span
                className={cn(
                  "min-w-0 flex-1 truncate",
                  entry.isDir
                    ? "text-label-12 text-text-primary"
                    : selected
                      ? "text-body-12 text-text-primary"
                      : "text-body-12 text-text-secondary",
                )}
              >
                {name}
              </span>
              {!entry.isDir && marks?.[entry.path] ? (
                <span className={cn("shrink-0 text-code-11", MARK_TONE[marks[entry.path]] ?? "text-text-tertiary")}>
                  {marks[entry.path]}
                </span>
              ) : null}
            </button>
          );
        })}
      </div>
    </div>
  );
}
