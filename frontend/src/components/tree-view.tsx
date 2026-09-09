import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import { cn } from "@/lib/utils";

/**
 * One row of the tree.
 *
 * `expandable` is separate from `children` on purpose: that is what makes a
 * lazy tree possible. A table whose columns have not been fetched yet has no
 * children, and a tree that infers "leaf" from an empty `children` would draw
 * it without a chevron — leaving no way to open it and trigger the fetch.
 *
 * Expansion is the caller's state, not the row's: every node carries its own
 * `expanded` + `onToggle`. The database sidebar keeps that state in its
 * Zustand store because opening a node is what starts a bridge call, and
 * because folder state is persisted across module switches.
 */
export type TreeNodeData = {
  id: string;
  label: ReactNode;
  /** A 14px glyph before the label. */
  icon?: ReactNode;
  /**
   * Muted text set immediately after the label — a count, a column type.
   * Inline rather than pinned right, which is what lets a long name spend the
   * rest of the row: a right-aligned column of numbers charges every name for
   * the width of the widest one.
   */
  meta?: ReactNode;
  /** Pinned to the right edge, outside the row's button — chips, hover actions. */
  trailing?: ReactNode;
  expandable?: boolean;
  expanded?: boolean;
  onToggle?: () => void;
  children?: TreeNodeData[];
  /** Drawn under an expanded node while its children are on their way. */
  loading?: boolean;
  /** Drawn under an expanded node that turned out to have nothing in it. */
  emptyLabel?: string;
  selected?: boolean;
  onSelect?: () => void;
  /** Double-click. The tables use it to write a SELECT into the console. */
  onActivate?: () => void;
  title?: string;
  /** Wraps the finished row — a `ContextMenuTrigger asChild`, typically. */
  wrap?: (row: ReactNode) => ReactNode;
};

/**
 * Rows are 24 tall and step 10 per level.
 *
 * Five levels of a database tree have to fit a name in a ~280px rail, and the
 * chrome before the label is what decides whether they do. A table sits at
 * level 3: 8 + 3×12 of indent plus a 14px chevron, a 16px glyph and two 6px
 * gaps put its name 86px in. At 4 + 3×10 with 12/14 and 4px gaps it starts at
 * 68 — measured against the running app, not computed — and the 28→24 row
 * buys back a seventh of the height on a 173-table schema.
 */
const ROW_INDENT = 10;
const ROW_INSET = 4;

function indent(level: number): number {
  return ROW_INSET + level * ROW_INDENT;
}

export function TreeView({
  nodes,
  className,
}: {
  nodes: TreeNodeData[];
  className?: string;
}) {
  return (
    <div role="tree" className={cn("flex flex-col", className)}>
      <TreeRows nodes={nodes} level={0} />
    </div>
  );
}

function TreeRows({ nodes, level }: { nodes: TreeNodeData[]; level: number }) {
  return (
    <>
      {nodes.map((node) => (
        <TreeRow key={node.id} node={node} level={level} />
      ))}
    </>
  );
}

function TreeRow({ node, level }: { node: TreeNodeData; level: number }) {
  const open = !!node.expandable && !!node.expanded;

  const row = (
    <div
      role="treeitem"
      aria-level={level + 1}
      aria-expanded={node.expandable ? open : undefined}
      aria-selected={node.selected || undefined}
      title={node.title}
      className={cn(
        "group flex h-6 items-center gap-1 rounded-md pr-1.5",
        node.selected
          ? "bg-surface-raised text-text-primary"
          : "text-text-secondary hover:bg-surface-raised/60 hover:text-text-primary",
      )}
      style={{ paddingLeft: indent(level) }}
    >
      {/* One button for the whole row, chevron included: a separate chevron
          target would be a button inside a button. Clicking a row both selects
          it and opens it, which is what a single-click tree does anyway. */}
      <button
        type="button"
        className="flex min-w-0 flex-1 items-center gap-1 text-left"
        onClick={() => {
          node.onSelect?.();
          node.onToggle?.();
        }}
        onDoubleClick={node.onActivate}
      >
        {/* The slot is kept on a leaf so names stay on one vertical line. */}
        <span className="flex size-3 shrink-0 items-center justify-center text-text-tertiary">
          {node.expandable && (
            <ChevronRight
              className={cn("size-3 transition-transform", open && "rotate-90")}
            />
          )}
        </span>
        {node.icon}
        {/* Not flex-1: the label takes what it needs and no more, so `meta`
            follows the name instead of the row's right edge. */}
        <span className="min-w-0 truncate text-body-13">{node.label}</span>
        {node.meta ? (
          <span className="shrink-0 text-code-11 text-text-tertiary">{node.meta}</span>
        ) : null}
      </button>
      {node.trailing}
    </div>
  );

  return (
    <>
      {node.wrap ? node.wrap(row) : row}
      {open &&
        (node.children?.length ? (
          <TreeRows nodes={node.children} level={level + 1} />
        ) : (
          <span
            className="flex h-6 items-center pr-1.5 text-code-11 text-text-tertiary"
            style={{ paddingLeft: indent(level + 1) + 16 }}
          >
            {node.loading ? "Loading…" : node.emptyLabel}
          </span>
        ))}
    </>
  );
}
