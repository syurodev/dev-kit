import { useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { ChevronRight } from "lucide-react";
import { observeElementRect, useVirtualizer } from "@tanstack/react-virtual";
import { cn } from "@/lib/utils";
import { shouldVirtualize } from "@/components/ui/virtual-rows";

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

/**
 * --control-height-xs, the row height below. Rows are fixed-height, which is
 * what lets a long tree be windowed by arithmetic instead of by measuring.
 */
export const TREE_ROW_HEIGHT = 24;

function indent(level: number): number {
  return ROW_INSET + level * ROW_INDENT;
}

// ponytail: mirrors --motion-fast in index.css; a token read would need a
// getComputedStyle per branch for a number that changes once a year.
const CLOSE_MS = 120;
/** --motion-base, the open direction. */
const OPEN_MS = 180;

const useIsomorphicLayoutEffect = typeof window === "undefined" ? useEffect : useLayoutEffect;

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

/**
 * A tree up to the virtualization threshold renders whole, nested, with its
 * branch animation and CSS sticky. Past it, as a flat window of rows.
 *
 * The threshold counts the rows that are showing, so a database rail with a
 * few schemas open is windowed and the same rail with them closed is not.
 * Windowing is what keeps a 900-table connection cheap to show: the rail used
 * to lay out every row each time it came back on screen.
 */
export function TreeView({
  nodes,
  className,
}: {
  nodes: TreeNodeData[];
  className?: string;
}) {
  const rows = flattenTree(nodes);
  if (shouldVirtualize(rows.length)) {
    return <VirtualTree rows={rows} className={className} />;
  }
  return (
    <div role="tree" className={cn("flex flex-col", className)}>
      <TreeRows nodes={nodes} level={0} />
    </div>
  );
}

/** A row of the flattened tree: the rows that are showing, in order. */
export type FlatRow = {
  /** The id path from the root: ids need only be unique among siblings. */
  key: string;
  node: TreeNodeData;
  level: number;
  /** Position among its siblings. */
  index: number;
  /** One past the last row of this node's subtree — itself included. */
  end: number;
  /** The loading or empty line under an expanded node with no children. */
  placeholder?: boolean;
};

export function flattenTree(nodes: TreeNodeData[]): FlatRow[] {
  const rows: FlatRow[] = [];
  const walk = (list: TreeNodeData[], level: number, parentKey: string) => {
    list.forEach((node, index) => {
      const key = `${parentKey}/${node.id}`;
      const at = rows.length;
      rows.push({ key, node, level, index, end: at + 1 });
      if (node.expandable && node.expanded) {
        if (node.children?.length) {
          walk(node.children, level + 1, key);
        } else {
          rows.push({ key: `${key}/…`, node, level: level + 1, index: 0, end: rows.length + 1, placeholder: true });
        }
      }
      rows[at].end = rows.length;
    });
  };
  walk(nodes, 0, "");
  return rows;
}

/**
 * Which rows are pinned, and where, with the scroller `offset` px into the list.
 *
 * What CSS sticky would do, done by hand because a windowed list has already
 * unmounted the row that should be stuck: a row shows at its `sticky` offset
 * once it has scrolled above that line, and is pushed up by the end of its own
 * subtree, so the next connection or schema slides it out rather than landing
 * on top of it.
 */
export function pinnedRows(
  rows: FlatRow[],
  offset: number,
  rowHeight: number,
): Array<{ index: number; top: number }> {
  const pins: Array<{ index: number; top: number }> = [];
  if (offset <= 0) return pins;
  for (let i = 0; i < rows.length; i++) {
    const pin = rows[i].node.sticky;
    if (pin === undefined || rows[i].placeholder) continue;
    const y = i * rowHeight - offset;
    if (y >= pin) continue;
    const top = Math.min(pin, rows[i].end * rowHeight - offset - rowHeight);
    if (top > -rowHeight) pins.push({ index: i, top });
  }
  return pins;
}

function scrollParent(element: HTMLElement): HTMLElement | null {
  for (let parent = element.parentElement; parent; parent = parent.parentElement) {
    const { overflowY } = getComputedStyle(parent);
    if (overflowY === "auto" || overflowY === "scroll") return parent;
  }
  return null;
}

function VirtualTree({ rows, className }: { rows: FlatRow[]; className?: string }) {
  const listRef = useRef<HTMLDivElement>(null);
  const [scroller, setScroller] = useState<HTMLElement | null>(null);
  // Where the list starts inside the scroller: the rail has headers and the
  // tree has its own top padding above the first row.
  const [margin, setMargin] = useState(0);

  useIsomorphicLayoutEffect(() => {
    const list = listRef.current;
    // Hidden with its module: nothing to measure, and zeros would be wrong.
    if (!list || list.getClientRects().length === 0) return;
    const element = scroller?.contains(list) ? scroller : scrollParent(list);
    if (element !== scroller) setScroller(element);
    if (!element) return;
    const next = Math.round(
      list.getBoundingClientRect().top - element.getBoundingClientRect().top + element.scrollTop,
    );
    if (next !== margin) setMargin(next);
  });

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scroller,
    getItemKey: (i) => rows[i].key,
    estimateSize: () => TREE_ROW_HEIGHT,
    overscan: 12,
    scrollMargin: margin,
    // Rows before the scroller is known: a window's worth, not an empty rail.
    initialRect: { width: 0, height: typeof window === "undefined" ? 800 : window.innerHeight },
    // A module's rail stays mounted while hidden (SidebarSurface). Hidden, the
    // scroller measures 0 tall; kept, that would render no rows and the rail
    // would come back empty for a frame. The last real size stands instead.
    observeElementRect: (instance, cb) =>
      observeElementRect(instance, (rect) => {
        if (rect.height > 0) cb(rect);
      }),
  });

  return (
    <div role="tree" className={cn("flex flex-col", className)}>
      <div ref={listRef} style={{ position: "relative", height: virtualizer.getTotalSize() }}>
        <PinnedRows rows={rows} scroller={scroller} margin={margin} />
        {virtualizer.getVirtualItems().map((item) => {
          const row = rows[item.index];
          return (
            <div
              key={item.key}
              style={{
                position: "absolute",
                top: 0,
                left: 0,
                right: 0,
                transform: `translateY(${item.start - margin}px)`,
              }}
            >
              {row.placeholder ? (
                <BranchPlaceholder node={row.node} level={row.level} />
              ) : (
                <TreeRowLine node={row.node} level={row.level} index={row.index} />
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/**
 * The pinned rows, on a zero-height host stuck to the scroller's top.
 *
 * Its own scroll listener rather than the virtualizer's offset: the virtualizer
 * re-renders only when the window of rows changes, so a scroll of a few pixels
 * left the pins where the last window put them — measured a row late at the
 * point one schema pushes out the next. flushSync, so the pins move in the same
 * frame as the rows; it renders two or three rows, not the window.
 *
 * Copies of real rows for the eye and the pointer; the windowed rows are what
 * assistive tech and the tab order see.
 */
function PinnedRows({
  rows,
  scroller,
  margin,
}: {
  rows: FlatRow[];
  scroller: HTMLElement | null;
  margin: number;
}) {
  const [offset, setOffset] = useState(0);

  useEffect(() => {
    if (!scroller) return;
    setOffset(scroller.scrollTop - margin);
    const onScroll = () => flushSync(() => setOffset(scroller.scrollTop - margin));
    scroller.addEventListener("scroll", onScroll, { passive: true });
    return () => scroller.removeEventListener("scroll", onScroll);
  }, [scroller, margin]);

  const pins = pinnedRows(rows, offset, TREE_ROW_HEIGHT);
  return (
    <div style={{ position: "sticky", top: 0, height: 0, zIndex: 2 }}>
      {/* Deepest first, so a schema being pushed out slides under its
          connection instead of over it. */}
      {[...pins].reverse().map(({ index, top }) => (
        <div
          key={rows[index].key}
          aria-hidden
          className="bg-(--background-app)"
          style={{ position: "absolute", top, left: 0, right: 0 }}
        >
          <TreeRowLine node={rows[index].node} level={rows[index].level} index={0} copy />
        </div>
      ))}
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

/**
 * The row itself, shared by both renderings.
 *
 * `animated` gives it the entry animation, which only the nested tree wants: a
 * windowed row mounts every time it scrolls into view. `copy` is a pinned row's
 * duplicate, kept out of the tab order.
 */
function TreeRowLine({
  node,
  level,
  index,
  animated = false,
  copy = false,
}: {
  node: TreeNodeData;
  level: number;
  index: number;
  animated?: boolean;
  copy?: boolean;
}) {
  const open = !!node.expandable && !!node.expanded;
  const row = (
    <div
      role={copy ? undefined : "treeitem"}
      aria-level={copy ? undefined : level + 1}
      aria-expanded={!copy && node.expandable ? open : undefined}
      aria-selected={(!copy && node.selected) || undefined}
      title={node.title}
      style={{ paddingLeft: indent(level), "--i": index } as CSSProperties}
      className={cn(
        // The tree tier (--control-height-xs): depth costs vertical room here,
        // so a row is denser than the flat lists in other rails.
        // The border is on every row, transparent until selected: a border that
        // appears would move the label 1px and shove the whole subtree down.
        "group flex h-(--control-height-xs) items-center gap-1 rounded-md border pr-1.5",
        animated && "tree-row-enter",
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
      )}
    >
      {/* One button for the whole row, chevron included: a separate chevron
          target would be a button inside a button. Clicking a row both selects
          it and opens it, which is what a single-click tree does anyway. */}
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

  return node.wrap ? node.wrap(row) : row;
}

/** The loading or empty line under an expanded node with no children. */
function BranchPlaceholder({ node, level }: { node: TreeNodeData; level: number }) {
  return (
    <span
      className="flex h-(--control-height-xs) items-center pr-1.5 text-code-11 text-text-tertiary"
      style={{ paddingLeft: indent(level) + 16 }}
    >
      {node.loading ? "Loading…" : node.emptyLabel}
    </span>
  );
}

function TreeRow({ node, level, index }: { node: TreeNodeData; level: number; index: number }) {
  const open = !!node.expandable && !!node.expanded;
  const branchMounted = useLingering(open);
  const branchAnimating = useBranchAnimating(open);

  const wrapped = <TreeRowLine node={node} level={level} index={index} animated />;

  return (
    <>
      {node.sticky === undefined ? (
        wrapped
      ) : (
        /*
         * A pinned row sits on a square, opaque ground of its own.
         *
         * The rail has no material, so a bare sticky row lets the tables
         * scroll through it. The ground used to be the row's ::after, and it
         * leaked three ways: it inherited the row's radius (text in the
         * corners), stopped at the padding box (a 1px ring through the
         * transparent border), and at z-index -1 inside the row's stacking
         * context it painted OVER the selected tint. A wrapper is square,
         * covers the border box, and paints under everything the row draws.
         */
        <div className="bg-(--background-app)" style={{ position: "sticky", top: node.sticky, zIndex: 2 }}>
          {wrapped}
        </div>
      )}
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
              <BranchPlaceholder node={node} level={level + 1} />
            )}
          </div>
        </div>
      )}
    </>
  );
}
