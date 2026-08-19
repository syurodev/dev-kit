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
