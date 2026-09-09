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
  /** A 16px glyph before the label. */
  icon?: ReactNode;
  /** Right of the label — a count, a type, an env chip, hover actions. */
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

/** Rows are 28 tall and step 12 per level: five levels of a database tree
 *  still leave room for a name in a 264px rail. */
const ROW_INDENT = 12;
const ROW_INSET = 8;

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
        "group flex h-7 items-center gap-1.5 rounded-md pr-2",
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
        className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
        onClick={() => {
          node.onSelect?.();
          node.onToggle?.();
        }}
        onDoubleClick={node.onActivate}
      >
        <span className="flex size-3.5 shrink-0 items-center justify-center text-text-tertiary">
          {node.expandable && (
            <ChevronRight
              className={cn("size-3.5 transition-transform", open && "rotate-90")}
            />
          )}
        </span>
        {node.icon}
        <span className="min-w-0 flex-1 truncate text-body-13">{node.label}</span>
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
            className="flex h-7 items-center pr-2 text-code-11 text-text-tertiary"
            style={{ paddingLeft: indent(level + 1) + 20 }}
          >
            {node.loading ? "Loading…" : node.emptyLabel}
          </span>
        ))}
    </>
  );
}
