import { useEffect, useRef, useState, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { ChevronRight } from "lucide-react";
import { observeElementRect, useVirtualizer } from "@tanstack/react-virtual";
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
   * Muted text set immediately after the label — a column's type. It gives way
   * before the name does when the rail is narrow.
   */
  meta?: ReactNode;
  /** A short number pinned to the right edge — tables in a schema. */
  count?: number;
  /**
   * Controls shown only while the row is hovered or focused, in the count's
   * place where there is one. They take no room until then.
   */
  actions?: ReactNode;
  /** Always shown, right of everything else — status marks, an env chip. */
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
 * --control-height-xs, the row height. Rows are fixed-height, which is what
 * lets the tree be windowed by arithmetic instead of by measuring.
 */
export const TREE_ROW_HEIGHT = 24;

/**
 * 14 a level: a guide needs room for its elbow between the parent's chevron
 * and the child's. The guides carry depth, which is what keeps five levels
 * readable in a 280px rail — measured in the gallery at 158px of name left at
 * a table, 118 at the 240 floor.
 */
const INDENT = 14;
const INSET = 4;
const CHEVRON = 12;
const PAD_TOP = 4;
const PAD_BOTTOM = 8;
const FADE = 16;
/**
 * --border-default measured 1.02–1.30 against the rail across the palettes, a
 * guide nobody sees. Tertiary text at 35% measured 1.55–1.93: a line in every
 * palette, well under the labels.
 */
//
// Mixed into the rail's ground rather than into transparent: an elbow overlaps
// the line it leaves, and two translucent borders on one pixel draw it twice
// as dark. Opaque, the overlap does not show.
const GUIDE = "1px solid color-mix(in srgb, var(--text-tertiary) 35%, var(--background-app))";
/** Elbow corner radius. */
const ELBOW_RADIUS = 5;

/** Where a row at `level` starts its chevron. */
const contentX = (level: number) => INSET + level * INDENT;
/** The guide under a row at `depth`: the centre of its chevron. */
const lineX = (depth: number) => contentX(depth) + CHEVRON / 2;

/** A row of the flattened tree: the rows that are showing, in order. */
export type FlatRow = {
  /** The id path from the root: ids need only be unique among siblings. */
  key: string;
  node: TreeNodeData;
  level: number;
  /** One past the last row of this node's subtree — itself included. */
  end: number;
  /** For each level of the path, from the root: whether that node is its parent's last child. */
  lasts: boolean[];
  /** The row is open, so a guide runs down from its chevron. */
  open: boolean;
  /** The loading or empty line under an expanded node with no children. */
  placeholder?: boolean;
};

export function flattenTree(nodes: TreeNodeData[]): FlatRow[] {
  const rows: FlatRow[] = [];
  const walk = (list: TreeNodeData[], level: number, parentKey: string, lasts: boolean[]) => {
    list.forEach((node, index) => {
      const key = `${parentKey}/${node.id}`;
      const mine = [...lasts, index === list.length - 1];
      const open = !!node.expandable && !!node.expanded;
      const at = rows.length;
      rows.push({ key, node, level, end: at + 1, lasts: mine, open });
      if (open) {
        if (node.children?.length) {
          walk(node.children, level + 1, key, mine);
        } else {
          rows.push({
            key: `${key}/…`,
            node,
            level: level + 1,
            end: rows.length + 1,
            lasts: [...mine, true],
            open: false,
            placeholder: true,
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

/**
 * The guides a row draws: a full-height line for every ancestor whose branch
 * continues past it, an elbow from its parent's line into its own chevron, and
 * — when open — a stub down from that chevron to its first child.
 *
 * CSS borders on absolute spans, which snap to the pixel at any display scale.
 * Everything about the lines lives here, so drawing them another way (SVG, for
 * curves or a highlighted path) is a change to this function alone.
 */
function Guides({ row }: { row: FlatRow }) {
  const { level, lasts, open, node, placeholder } = row;
  const half = TREE_ROW_HEIGHT / 2;
  // A row with a chevron takes the elbow to the chevron. A leaf has an empty
  // chevron slot, and stopping there left its elbow 14px short of the icon —
  // shallow next to the rows above it. It runs through the slot instead.
  const reachesIcon = placeholder || !node.expandable;
  const lines: ReactNode[] = [];
  for (let depth = 0; depth < level; depth++) {
    const continues = !lasts[depth + 1];
    if (depth === level - 1) {
      // A continuing branch is one straight line the full height of the row,
      // with the elbow laid over it. It used to be the elbow's own border down
      // to the middle and a second line from the middle on — but the elbow's
      // rounded corner spends its last ELBOW_RADIUS px curving away, so every
      // row left that much of a gap in the line below its elbow.
      if (continues) {
        lines.push(
          <span key={`line-${depth}`} style={{ position: "absolute", left: lineX(depth), top: 0, bottom: 0, borderLeft: GUIDE }} />,
        );
      }
      lines.push(
        <span
          key={`elbow-${depth}`}
          style={{
            position: "absolute",
            left: lineX(depth),
            top: 0,
            width: contentX(level) - lineX(depth) - 2 + (reachesIcon ? CHEVRON + 2 : 0),
            height: half,
            borderLeft: GUIDE,
            borderBottom: GUIDE,
            borderBottomLeftRadius: ELBOW_RADIUS,
          }}
        />,
      );
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

function Row({ row, copy = false }: { row: FlatRow; copy?: boolean }) {
  const { node, level, open } = row;
  const hasCount = node.count !== undefined;
  const hasActions = node.actions != null;

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
          // The border is on every row, transparent until selected: a border
          // that appears would move the label 1px.
          "absolute inset-y-0 right-1 flex items-center gap-1 rounded-md border pr-1.5",
          node.selected
            // Tinted, and the border stays: --status-info is the mark that
            // clears 3:1 in every palette (themeContrast.test.ts), the tint
            // alone does not. Measured 4.51–13.38 in the gallery.
            ? "border-status-info bg-status-info/10 text-text-primary"
            // --state-soft is defined per mode — white alpha where dark lifts,
            // ink alpha where light recesses.
            : "border-transparent text-text-secondary hover:bg-state-soft hover:text-text-primary",
        )}
        style={{ left: contentX(level) - 2, paddingLeft: 1 }}
      >
        {/* One button for the whole row, chevron included: a separate chevron
            target would be a button inside a button. Clicking a row both
            selects it and opens it, which is what a single-click tree does. */}
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
              <ChevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
            )}
          </span>
          {node.icon}
          <span data-slot="tree-label" className="min-w-0 truncate text-body-13">
            {node.label}
          </span>
          {/* Gives way first: at 240 a column's type kept its width and the
              name was cut. The name is what the row is for. */}
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
                  hasActions &&
                    "group-hover/row:opacity-0 group-focus-within/row:opacity-0 group-has-data-[state=open]/row:opacity-0",
                )}
              >
                {node.count}
              </span>
            ) : null}
            {hasActions ? (
              // Over the count where there is one; where there is not, no room
              // until shown — kept at opacity 0 they cost every name 40px.
              // Held open while a menu they opened is: hiding its trigger would
              // leave the menu anchored to nothing.
              <span
                className={cn(
                  "items-center gap-0.5",
                  hasCount
                    ? "absolute right-0 flex opacity-0 group-hover/row:opacity-100 group-focus-within/row:opacity-100 has-data-[state=open]:opacity-100"
                    : "hidden group-hover/row:flex group-focus-within/row:flex has-data-[state=open]:flex",
                )}
              >
                {node.actions}
              </span>
            ) : null}
          </span>
        ) : null}
        {node.trailing}
      </div>
    </div>
  );

  return node.wrap ? <>{node.wrap(line)}</> : line;
}

function Placeholder({ row }: { row: FlatRow }) {
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
 * The tree, windowed, in a scroller of its own — give it a height with
 * `className` (flex-1 in a rail).
 *
 * One rendering for every size. Only the rows in view are in the DOM, which is
 * what keeps a 900-table connection cheap to bring back on screen: the rail
 * used to lay out every row each time, 96–134ms against 5–6ms windowed.
 *
 * No motion but the chevron's turn. Owning the scroller is what the edge fades
 * need — a mask on it — and the pins and the window both read its offset.
 */
export function TreeView({ nodes, className }: { nodes: TreeNodeData[]; className?: string }) {
  const rows = flattenTree(nodes);
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
    // Rows before the scroller is measured: a window's worth, not an empty rail.
    initialRect: { width: 0, height: typeof window === "undefined" ? 800 : window.innerHeight },
    // A module's rail stays mounted while another module is on screen
    // (SidebarSurface). Hidden, the scroller measures 0 tall; kept, that would
    // render no rows and the rail would come back empty for a frame.
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
    // Its own listener, not the virtualizer's offset: the virtualizer
    // re-renders only when the window changes, which left the pins a row late.
    // flushSync, so pins and fades move in the frame the rows do.
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
  // The fades start under the pins: what is pinned stays solid, and what
  // scrolls beneath it dissolves into it instead of being cut at its edge.
  const mask = `linear-gradient(to bottom, #000 0, #000 ${pinBottom}px, ${view.top ? "transparent" : "#000"} ${pinBottom}px, #000 ${pinBottom + FADE}px, #000 calc(100% - ${FADE}px), ${view.bottom ? "transparent" : "#000"} 100%)`;

  return (
    <div
      ref={scrollRef}
      className={cn("min-h-0 overflow-y-auto", className)}
      style={{ maskImage: mask, WebkitMaskImage: mask }}
    >
      <div role="tree" style={{ position: "relative", height: virtualizer.getTotalSize() }}>
        {/* Pinned rows on a zero-height host stuck to the scroller's top.
            Copies for the eye and the pointer; the windowed rows are what
            assistive tech and the tab order see. Deepest first, so a schema
            being pushed out slides under its connection. */}
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
