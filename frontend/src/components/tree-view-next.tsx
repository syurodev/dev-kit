import { useEffect, useRef, useState, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { ChevronRight } from "lucide-react";
import { observeElementRect, useVirtualizer } from "@tanstack/react-virtual";
import { cn } from "@/lib/utils";
import {
  pinnedRows,
  TREE_ROW_HEIGHT,
  type FlatRow,
  type TreeNodeData,
} from "@/components/tree-view";

/**
 * PROTOTYPE — the tree every module with one is meant to share. Shown in the
 * component gallery (dev/TreeNextGallery.tsx); nothing in a module uses it yet.
 *
 * Against TreeView it adds indent guides with an elbow into each row, a count
 * pinned right that hover actions take over, a tinted selection that keeps its
 * border, and edge fades under the pinned rows. It has one rendering — flat
 * and windowed — so guides, pins and rows are drawn in one place.
 */
export type TreeNextNode = TreeNodeData & {
  /** A short number pinned to the right edge. Hover actions (`trailing`) take its place. */
  count?: number;
  children?: TreeNextNode[];
};

/**
 * 14 per level, up from TreeView's 10: a guide needs room for its elbow
 * between the parent's chevron and the child's. The guides are what carry
 * depth now, which is what keeps five levels readable at this indent.
 */
const INDENT = 14;
const INSET = 4;
const CHEVRON = 12;
const PAD_TOP = 4;
const PAD_BOTTOM = 8;
const FADE = 16;
// Measured on --border-default first: 1.02–1.30 against the rail across the
// palettes, which is a guide nobody sees. Tertiary text at a third reads as a
// line in every palette and still sits well under the labels.
const GUIDE = "1px solid color-mix(in srgb, var(--text-tertiary) 35%, transparent)";

/** Where a row at `level` starts its chevron. */
const contentX = (level: number) => INSET + level * INDENT;
/** The guide under a row at `depth`: the centre of its chevron. */
const lineX = (depth: number) => contentX(depth) + CHEVRON / 2;

export type NextRow = FlatRow & {
  node: TreeNextNode;
  /** For each level of the path, from the root: whether that node is its parent's last child. */
  lasts: boolean[];
  /** The row is open, so a guide runs down from its chevron. */
  open: boolean;
};

export function flattenWithGuides(nodes: TreeNextNode[]): NextRow[] {
  const rows: NextRow[] = [];
  const walk = (list: TreeNextNode[], level: number, parentKey: string, lasts: boolean[]) => {
    list.forEach((node, index) => {
      const key = `${parentKey}/${node.id}`;
      const mine = [...lasts, index === list.length - 1];
      const open = !!node.expandable && !!node.expanded;
      const at = rows.length;
      rows.push({ key, node, level, index, end: at + 1, lasts: mine, open });
      if (open) {
        if (node.children?.length) {
          walk(node.children, level + 1, key, mine);
        } else {
          rows.push({
            key: `${key}/…`,
            node,
            level: level + 1,
            index: 0,
            end: rows.length + 1,
            placeholder: true,
            lasts: [...mine, true],
            open: false,
          });
        }
      }
      rows[at].end = rows.length;
    });
  };
  walk(nodes, 0, "", []);
  return rows;
}

/**
 * The guides a row draws: a full-height line for every ancestor whose branch
 * continues past it, an elbow from its parent's line into its own chevron, and
 * — when open — a stub down from that chevron to its first child.
 */
function Guides({ row }: { row: NextRow }) {
  const { level, lasts, open } = row;
  const half = TREE_ROW_HEIGHT / 2;
  const lines: ReactNode[] = [];
  for (let depth = 0; depth < level; depth++) {
    const continues = !lasts[depth + 1];
    if (depth === level - 1) {
      lines.push(
        <span
          key={`elbow-${depth}`}
          style={{
            position: "absolute",
            left: lineX(depth),
            top: 0,
            width: contentX(level) - lineX(depth) - 2,
            height: half,
            borderLeft: GUIDE,
            borderBottom: GUIDE,
            borderBottomLeftRadius: 5,
          }}
        />,
      );
      if (continues) {
        lines.push(
          <span key={`tail-${depth}`} style={{ position: "absolute", left: lineX(depth), top: half, bottom: 0, borderLeft: GUIDE }} />,
        );
      }
    } else if (continues) {
      lines.push(
        <span key={`line-${depth}`} style={{ position: "absolute", left: lineX(depth), top: 0, bottom: 0, borderLeft: GUIDE }} />,
      );
    }
  }
  if (open) {
    lines.push(
      <span key="stub" style={{ position: "absolute", left: lineX(level), top: half + CHEVRON / 2, bottom: 0, borderLeft: GUIDE }} />,
    );
  }
  return <span aria-hidden>{lines}</span>;
}

function Row({ row, copy = false }: { row: NextRow; copy?: boolean }) {
  const { node, level } = row;
  const open = !!node.expandable && !!node.expanded;
  const hasActions = node.trailing != null;
  const hasCount = node.count !== undefined;

  const line = (
    <div
      role={copy ? undefined : "treeitem"}
      aria-level={copy ? undefined : level + 1}
      aria-expanded={!copy && node.expandable ? open : undefined}
      aria-selected={(!copy && node.selected) || undefined}
      title={node.title}
      data-level={level}
      className="group/row relative"
      style={{ height: TREE_ROW_HEIGHT }}
    >
      <Guides row={row} />
      {/* The highlight starts at the row's own chevron, not the rail's edge:
          a full-width one would paint over the guides it hangs from. */}
      <div
        className={cn(
          "absolute inset-y-0 right-1 flex items-center gap-1 rounded-md border pr-1.5",
          node.selected
            // Tinted like the reference, but the border stays: the tint alone
            // is not a 3:1 mark in every palette (themeContrast.test.ts).
            ? "border-status-info bg-status-info/10 text-text-primary"
            : "border-transparent text-text-secondary hover:bg-state-soft hover:text-text-primary",
        )}
        style={{ left: contentX(level) - 2, paddingLeft: 1 }}
      >
        <button
          type="button"
          tabIndex={copy ? -1 : undefined}
          className="flex min-w-0 flex-1 items-center gap-1 text-left"
          onClick={() => {
            node.onSelect?.();
            node.onToggle?.();
          }}
          onDoubleClick={node.onActivate}
        >
          <span className="flex size-3 shrink-0 items-center justify-center text-text-tertiary">
            {node.expandable && (
              <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
            )}
          </span>
          {node.icon}
          <span data-slot="tree-label" className="min-w-0 truncate text-body-13">
            {node.label}
          </span>
          {/* Gives way first: at 240 a column's type kept its width and the
              name was cut to "e…". The name is what the row is for. */}
          {node.meta ? (
            <span className="min-w-0 truncate text-code-11 text-text-tertiary" style={{ flexShrink: 100 }}>
              {node.meta}
            </span>
          ) : null}
        </button>
        {hasCount || hasActions ? (
          <span className="relative flex shrink-0 items-center justify-end">
            {hasCount ? (
              <span
                className={cn(
                  "text-code-11 tabular-nums text-text-tertiary",
                  hasActions && "group-hover/row:opacity-0 group-focus-within/row:opacity-0",
                )}
              >
                {node.count}
              </span>
            ) : null}
            {hasActions ? (
              // Over the count where there is one. Where there is not, they
              // take no room until shown: kept at opacity 0 they reserved two
              // buttons' width on every row and cut 40px from each name.
              <span
                className={cn(
                  "items-center gap-0.5",
                  hasCount
                    ? "absolute right-0 flex opacity-0 group-hover/row:opacity-100 group-focus-within/row:opacity-100"
                    : "hidden group-hover/row:flex group-focus-within/row:flex",
                )}
              >
                {node.trailing}
              </span>
            ) : null}
          </span>
        ) : null}
      </div>
    </div>
  );

  return node.wrap ? <>{node.wrap(line)}</> : line;
}

function Placeholder({ row }: { row: NextRow }) {
  return (
    <div className="relative" style={{ height: TREE_ROW_HEIGHT }}>
      <Guides row={row} />
      <span
        className="absolute inset-y-0 flex items-center text-code-11 text-text-tertiary"
        style={{ left: contentX(row.level) + CHEVRON + 4 }}
      >
        {row.node.loading ? "Loading…" : row.node.emptyLabel}
      </span>
    </div>
  );
}

/**
 * Owns its scroller, unlike TreeView: the edge fades are a mask on it, and the
 * pins and the window both read its offset.
 */
export function TreeViewNext({ nodes, className }: { nodes: TreeNextNode[]; className?: string }) {
  const rows = flattenWithGuides(nodes);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [view, setView] = useState({ offset: 0, top: false, bottom: false });

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    getItemKey: (i) => rows[i].key,
    estimateSize: () => TREE_ROW_HEIGHT,
    overscan: 12,
    paddingStart: PAD_TOP,
    paddingEnd: PAD_BOTTOM,
    initialRect: { width: 0, height: typeof window === "undefined" ? 800 : window.innerHeight },
    // Hidden with its module, the scroller measures 0 tall; keep the last size.
    observeElementRect: (instance, cb) =>
      observeElementRect(instance, (rect) => {
        if (rect.height > 0) cb(rect);
      }),
  });

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const read = () => ({
      offset: el.scrollTop - PAD_TOP,
      top: el.scrollTop > 0,
      bottom: el.scrollTop + el.clientHeight < el.scrollHeight - 1,
    });
    setView(read());
    // flushSync so the pins and fades move in the frame the rows do.
    const onScroll = () => flushSync(() => setView(read()));
    el.addEventListener("scroll", onScroll, { passive: true });
    const observer = new ResizeObserver(() => setView(read()));
    observer.observe(el);
    return () => {
      el.removeEventListener("scroll", onScroll);
      observer.disconnect();
    };
  }, [rows.length]);

  const pins = pinnedRows(rows, view.offset, TREE_ROW_HEIGHT);
  const pinBottom = pins.reduce((bottom, pin) => Math.max(bottom, pin.top + TREE_ROW_HEIGHT), 0);
  // Fades start under the pins: what is pinned stays solid, and what scrolls
  // beneath it dissolves into it rather than being cut off at its edge.
  const mask = `linear-gradient(to bottom, #000 0, #000 ${pinBottom}px, ${view.top ? "transparent" : "#000"} ${pinBottom}px, #000 ${pinBottom + FADE}px, #000 calc(100% - ${FADE}px), ${view.bottom ? "transparent" : "#000"} 100%)`;

  return (
    <div
      ref={scrollRef}
      className={cn("min-h-0 overflow-y-auto", className)}
      style={{ maskImage: mask, WebkitMaskImage: mask }}
    >
      <div role="tree" style={{ position: "relative", height: virtualizer.getTotalSize() }}>
        <div style={{ position: "sticky", top: 0, height: 0, zIndex: 2 }}>
          {[...pins].reverse().map(({ index, top }) => (
            <div
              key={rows[index].key}
              aria-hidden
              className="bg-(--background-app)"
              style={{ position: "absolute", top, left: 0, right: 0 }}
            >
              <Row row={rows[index]} copy />
            </div>
          ))}
        </div>
        {virtualizer.getVirtualItems().map((item) => {
          const row = rows[item.index];
          return (
            <div
              key={item.key}
              style={{ position: "absolute", top: 0, left: 0, right: 0, transform: `translateY(${item.start}px)` }}
            >
              {row.placeholder ? <Placeholder row={row} /> : <Row row={row} />}
            </div>
          );
        })}
      </div>
    </div>
  );
}
