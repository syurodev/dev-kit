import { useEffect, useState, type CSSProperties, type ReactNode } from "react";
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
 * Expansion is the caller's state: every node carries its own `expanded` +
 * `onToggle`. The database sidebar keeps it in Zustand because opening a node
 * starts a bridge call and folder state outlives a module switch.
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
  /**
   * Drawn under an expanded node that turned out to have nothing in it. A
   * node, not a string: "nothing here" is often one click away from being
   * something, and the fix belongs in the slot that says it is empty.
   */
  emptyLabel?: ReactNode;
  selected?: boolean;
  onSelect?: () => void;
  /**
   * Pins this row to the top of the scroller while its children scroll under
   * it, at this offset in px.
   *
   * A number rather than a boolean because the offset is the caller's: the
   * database rail pins a connection at 0 and its schema at 24 beneath it, and
   * a folder above them is not pinned at all. Deriving it from the tree level
   * would pin the tables too, which on five levels is 120px of stuck header on
   * a 280px rail.
   */
  sticky?: number;
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

// ponytail: mirrors --motion-fast in index.css; a token read would need a
// getComputedStyle per branch for a number that changes once a year.
const CLOSE_MS = 120;
/** --motion-base, the open direction. */
const OPEN_MS = 180;

/**
 * True while a branch is mid-transition, either direction.
 *
 * It exists to take `overflow: hidden` OFF a branch that has finished moving.
 * The collapse is a `grid-template-rows: 1fr → 0fr` and the grid item has to
 * be clipped for that to hide anything — but an ancestor with `overflow:
 * hidden` also kills `position: sticky` for everything inside it, and the tree
 * pins a connection and its schema so they stay on screen while 173 tables
 * scroll under them. A settled branch is exactly as tall as its content, so
 * clipping it buys nothing and costs that.
 */
function useBranchAnimating(open: boolean): boolean {
  const [animating, setAnimating] = useState(true);
  useEffect(() => {
    setAnimating(true);
    const t = setTimeout(() => setAnimating(false), OPEN_MS);
    return () => clearTimeout(t);
  }, [open]);
  return animating;
}

/**
 * True while `open`, and for CLOSE_MS after it turns false — the window the
 * collapse transition needs the children still in the DOM. Opening is the
 * cheap direction: mount, and CSS @starting-style takes it from there.
 */
function useLingering(open: boolean): boolean {
  const [lingering, setLingering] = useState(open);
  useEffect(() => {
    if (open) {
      setLingering(true);
      return;
    }
    const t = setTimeout(() => setLingering(false), CLOSE_MS);
    return () => clearTimeout(t);
  }, [open]);
  return open || lingering;
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
      {nodes.map((node, i) => (
        <TreeRow key={node.id} node={node} level={level} index={i} />
      ))}
    </>
  );
}

function TreeRow({ node, level, index }: { node: TreeNodeData; level: number; index: number }) {
  const open = !!node.expandable && !!node.expanded;
  const branchMounted = useLingering(open);
  const branchAnimating = useBranchAnimating(open);

  const row = (
    <div
      role="treeitem"
      aria-level={level + 1}
      aria-expanded={node.expandable ? open : undefined}
      aria-selected={node.selected || undefined}
      title={node.title}
      // The rail has no material of its own, so a pinned row would be
      // transparent and the tables would scroll through it. tree-row-sticky
      // paints an opaque ground BEHIND the row's own background, which leaves
      // the selected and hover tints to paint over it as usual.
      style={
        node.sticky === undefined
          ? ({ paddingLeft: indent(level), "--i": index } as CSSProperties)
          : ({
              paddingLeft: indent(level),
              "--i": index,
              position: "sticky",
              top: node.sticky,
              zIndex: 2,
            } as CSSProperties)
      }
      className={cn(
        // The tree tier (--control-height-xs): depth costs vertical room here,
        // so a row is denser than the flat lists in other rails.
        // The border is on every row, transparent until selected: a border that
        // appears would move the label 1px and shove the whole subtree down.
        "group tree-row-enter flex h-(--control-height-xs) items-center gap-1 rounded-md border pr-1.5",
        node.selected
          // --status-info, not the brand colour: a brand is free to be pale,
          // and --action-primary at 45% measured 1.05:1 against this very fill
          // on devkit light — a border that says "selected" to nobody looking
          // at it. See themeContrast.test.ts, which floors this mark at 3:1.
          ? "border-status-info bg-surface-raised text-text-primary"
          // The state layer, not a translucent surface. --surface-raised at 60%
          // lifts clearly off a near-black rail and is all but invisible on a
          // light one (1.06:1 against --background-app, against dark's 1.14);
          // --state-soft is defined per mode for exactly this — white alpha
          // where dark lifts, ink alpha where light recesses — and lands at
          // 1.08–1.18 in every palette either way.
          : "border-transparent text-text-secondary hover:bg-state-soft hover:text-text-primary",
        node.sticky !== undefined && "tree-row-sticky",
      )}
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
      {branchMounted && (
        // Stays mounted through the collapse (see useLingering); inert so a
        // closing branch cannot keep focus or be tabbed into meanwhile.
        <div
          className="tree-branch"
          data-closing={open ? undefined : ""}
          data-animating={branchAnimating ? "" : undefined}
          inert={!open}
        >
          <div role="group">
            {node.children?.length ? (
              <TreeRows nodes={node.children} level={level + 1} />
            ) : (
              <span
                className="flex h-(--control-height-xs) items-center pr-1.5 text-code-11 text-text-tertiary"
                style={{ paddingLeft: indent(level + 1) + 16 }}
              >
                {node.loading ? "Loading…" : node.emptyLabel}
              </span>
            )}
          </div>
        </div>
      )}
    </>
  );
}
