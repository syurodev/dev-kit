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

/** Figma `Files Tab Content` (862:33549): 28px rows, 16px per depth, chevron 10 · glyph 13 · name. */
const ROW_HEIGHT = 28;

export function FileTree({ entries, selectedPath, onSelectFile }: FileTreeProps) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
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
    <div ref={parentRef} className="h-full overflow-y-auto">
      <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
        {virtualizer.getVirtualItems().map((item) => {
          const entry = rows[item.index];
          const depth = depthOf(entry.path);
          const name = entry.path.slice(entry.path.lastIndexOf("/") + 1);
          const selected = selectedPath === entry.path;
          return (
            <button
              key={entry.path}
              className={cn(
                "absolute top-0 left-0 flex w-full items-center gap-1.5 rounded-sm border pr-2 text-left",
                selected
                  ? "border-border-subtle bg-surface-raised"
                  : "border-transparent hover:bg-surface-raised/60",
              )}
              style={{
                height: item.size,
                transform: `translateY(${item.start}px)`,
                paddingLeft: `${depth * 16 + 8}px`,
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
            </button>
          );
        })}
      </div>
    </div>
  );
}
