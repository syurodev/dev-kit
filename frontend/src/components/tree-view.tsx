import {
  createContext,
  forwardRef,
  memo,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type ReactNode,
} from "react";
import { flushSync } from "react-dom";
import { ChevronRight } from "lucide-react";
import { observeElementRect, useVirtualizer } from "@tanstack/react-virtual";
import { cn } from "@/lib/utils";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { treeKeyboardTarget, treePath } from "./treeNavigation";
import { Chip } from "@/components/ui/chip";
import { TREE_CHEVRON, TREE_ROW_HEIGHT, TreeGuides, treeContentX } from "./tree-guides";

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
   * it, this many rows down: 0 at the top, 1 under a row pinned at 0.
   *
   * Rows rather than px so the caller never needs the tree's row height: the
   * database rail pins a connection at 0 and its schema at 1 beneath it, and a
   * folder above them is not pinned at all. Deriving it from the tree level
   * would pin the tables too — five levels of stuck header on a narrow rail.
   */
  sticky?: number;
  /** Double-click. The tables use it to write a SELECT into the console. */
  onActivate?: () => void;
  title?: string;
  /** Wraps the finished row — a `ContextMenuTrigger asChild`, typically. */
  wrap?: (row: ReactNode) => ReactNode;
};

/** Defined with the guides it is measured against (tree-guides.tsx). */
export { TREE_ROW_HEIGHT };

const RowHeight = createContext(TREE_ROW_HEIGHT);

const PAD_TOP = 4;
const PAD_BOTTOM = 8;
const FADE = 16;
/** The narrowest right-edge strip the fades leave alone, for an overlay scrollbar's thumb. */
const SCROLLBAR_STRIP = 10;
/** How long a pointer rests on a row before its tooltip opens. Longer than a
 *  button's: moving down a list should not light a tooltip on every row. */
const TOOLTIP_DELAY = 600;

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

export type Pin = { index: number; top: number };

/**
 * Which rows are pinned, and where, with the scroller `offset` px into the list.
 *
 * What CSS sticky would do, done by hand because a windowed list has already
 * unmounted the row that should be stuck: a row shows at its `sticky` offset
 * once it has scrolled above that line, and is pushed up by the end of its own
 * subtree, so the next connection or schema slides it out rather than landing
 * on top of it.
 *
 * `candidates` is the rows that pin at all — worked out once per tree, so a
 * scroll event looks at a handful of rows instead of every table.
 */
export function pinnedRows(
  rows: FlatRow[],
  offset: number,
  rowHeight: number,
  candidates?: number[],
): Pin[] {
  const pins: Pin[] = [];
  if (offset <= 0) return pins;
  for (const i of candidates ?? rows.map((_, index) => index)) {
    if (rows[i].node.sticky === undefined || rows[i].placeholder) continue;
    const pin = rows[i].node.sticky * rowHeight;
    const y = i * rowHeight - offset;
    if (y >= pin) continue;
    const top = Math.min(pin, rows[i].end * rowHeight - offset - rowHeight);
    if (top > -rowHeight) pins.push({ index: i, top });
  }
  return pins;
}

/** The scroller's edge fades, starting under the pinned rows. */
function fadeMask(pinBottom: number, fadeTop: boolean, fadeBottom: boolean): string {
  return `linear-gradient(to bottom, #000 0, #000 ${pinBottom}px, ${fadeTop ? "transparent" : "#000"} ${pinBottom}px, #000 ${pinBottom + FADE}px, #000 calc(100% - ${FADE}px), ${fadeBottom ? "transparent" : "#000"} 100%)`;
}

/** A row's guides (tree-guides.tsx), at this tree's row height. */
function Guides({ row }: { row: FlatRow }) {
  const height = useContext(RowHeight);
  return (
    <TreeGuides
      level={row.level}
      lasts={row.lasts}
      open={row.open}
      leaf={!!row.placeholder || !row.node.expandable}
      rowHeight={height}
    />
  );
}

/**
 * The app's tooltip on a row's main button, not the browser's `title`: the
 * native one is a different font, a different surface and a second-long delay
 * nobody chose — and the OS draws it, so no stylesheet can change that. On the
 * button rather than the row, so a row's own actions — which carry their own
 * tooltips — never open two at once.
 *
 * Where the pointer is when it opens, just below it, the way a native title
 * lands — not at the button's edge wherever the pointer happened to be. Placed
 * once and then still. Pointer moves only write a ref; the one layout read is
 * the trigger's rect, at the moment it opens. Opened from the keyboard, with no
 * pointer to go by, it sits to the right of the row.
 */
const POINTER_GAP = 18;

function RowTooltip({ title, children }: { title?: string; children: ReactNode }) {
  const pointer = useRef<{ x: number; y: number; el: Element | null }>({ x: 0, y: 0, el: null });
  const [at, setAt] = useState<{ x: number; y: number } | null>(null);
  if (!title) return <>{children}</>;
  return (
    <Tooltip
      delayDuration={TOOLTIP_DELAY}
      onOpenChange={(open) => {
        if (!open) return;
        const { x, y, el } = pointer.current;
        const rect = el?.getBoundingClientRect();
        setAt(rect && x >= rect.left && x <= rect.right ? { x: x - rect.left, y: y - rect.bottom } : null);
      }}
    >
      <TooltipTrigger
        asChild
        onPointerMove={(event) => {
          pointer.current = { x: event.clientX, y: event.clientY, el: event.currentTarget };
        }}
        onPointerLeave={() => {
          pointer.current.el = null;
        }}
      >
        {children}
      </TooltipTrigger>
      {at ? (
        <TooltipContent side="bottom" align="start" alignOffset={at.x} sideOffset={at.y + POINTER_GAP}>
          {title}
        </TooltipContent>
      ) : (
        <TooltipContent side="right" align="start">
          {title}
        </TooltipContent>
      )}
    </Tooltip>
  );
}

/**
 * A control in a row's `actions`, the same size in every tree: 24×24 — the
 * macOS minimum target with room to spare in a 28px row — a 14px glyph, the
 * row's quiet colour until hovered. forwardRef so it can be a Radix trigger.
 */
export const TreeRowAction = forwardRef<HTMLButtonElement, ButtonHTMLAttributes<HTMLButtonElement>>(
  function TreeRowAction({ className, onClick, ...props }, ref) {
    return (
      <button
        ref={ref}
        type="button"
        className={cn(
          "flex size-6 shrink-0 items-center justify-center rounded-sm text-text-tertiary hover:bg-state-soft hover:text-text-primary [&_svg]:size-3.5",
          className,
        )}
        // A row action is not a click on the row.
        onClick={(event) => {
          event.stopPropagation();
          onClick?.(event);
        }}
        {...props}
      />
    );
  },
);

/**
 * A short tag at a row's end — a connection's environment — the same size in
 * every tree. `danger` for the one that must stand out (prod); the rest stay
 * quiet so a list of them does not shout.
 */
export function TreeRowTag({ danger = false, children }: { danger?: boolean; children: ReactNode }) {
  return (
    <Chip tone={danger ? "danger" : "default"} className={cn("h-5 px-1.5", !danger && "text-text-tertiary")}>
      {children}
    </Chip>
  );
}

/**
 * Memoised on the row, which flattenTree keeps stable for as long as the nodes
 * are: a scroll that brings a row into the window mounts it, and every row
 * already there renders nothing.
 */
const Row = memo(function Row({ row, copy = false, keyboard = false, focused = false, id }: { row: FlatRow; copy?: boolean; keyboard?: boolean; focused?: boolean; id?: string }) {
  const { node, level, open } = row;
  const hasCount = node.count !== undefined;
  const hasActions = node.actions != null;
  const height = useContext(RowHeight);

  const line = (
    <div
      id={id}
      data-tree-key={row.key}
      role={copy ? undefined : "treeitem"}
      aria-level={copy ? undefined : level + 1}
      aria-expanded={!copy && node.expandable ? open : undefined}
      aria-selected={(!copy && node.selected) || undefined}
      data-level={level}
      className="group/row relative"
      style={{ height }}
    >
      <Guides row={row} />
      {/* The highlight starts at the row's own chevron, not the rail's edge:
          a full-width one would paint over the guides it hangs from. */}
      <div
        className={cn(
          // The border is on every row, transparent until selected: a border
          // that appears would move the label 1px.
          "absolute inset-y-0 right-1 flex items-center gap-1 rounded-md border pr-1.5",
          focused && "group-focus/tree:outline group-focus/tree:outline-1 group-focus/tree:outline-border-focus",
          node.selected
            // Tinted, and the border stays: --status-info is the mark that
            // clears 3:1 in every palette (themeContrast.test.ts), the tint
            // alone does not. Measured 4.51–13.38 in the gallery.
            ? "border-status-info bg-status-info/10 text-text-primary"
            // --state-soft is defined per mode — white alpha where dark lifts,
            // ink alpha where light recesses.
            : "border-transparent text-text-secondary hover:bg-state-soft hover:text-text-primary",
        )}
        style={{ left: treeContentX(level) - 2, paddingLeft: 'var(--space-tree-guide)' }}
      >
        {/* One button for the whole row, chevron included: a separate chevron
            target would be a button inside a button. Clicking a row both
            selects it and opens it, which is what a single-click tree does. */}
        <RowTooltip title={node.title}>
        <button
          type="button"
          tabIndex={copy || keyboard ? -1 : undefined}
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
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
          {/* A fixed 14px slot, glyphs sized to it whatever the caller drew:
              icons are the tree's metric, not the caller's. A dot or a chip
              keeps its own size inside it. */}
          {node.icon ? (
            <span
              data-slot="tree-icon"
              className="flex size-3.5 shrink-0 items-center justify-center text-icon-secondary [&_svg]:size-3.5 [&_svg]:shrink-0"
            >
              {node.icon}
            </span>
          ) : null}
          <span
            data-slot="tree-label"
            className={cn(
              "min-w-0 truncate text-text-primary",
              node.selected ? "text-body-13-medium" : "text-body-13",
            )}
          >
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
        </RowTooltip>
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
        {node.trailing ? (
          // Marks, not controls: small, quiet, the same size in every tree.
          <span className="flex shrink-0 items-center gap-1 text-code-11 text-text-tertiary [&_svg]:size-3 [&_svg]:shrink-0">
            {node.trailing}
          </span>
        ) : null}
      </div>
    </div>
  );

  return node.wrap ? <>{node.wrap(line)}</> : line;
});

const Placeholder = memo(function Placeholder({ row }: { row: FlatRow }) {
  const height = useContext(RowHeight);
  return (
    <div className="relative" style={{ height }}>
      <Guides row={row} />
      <span
        className="absolute inset-y-0 flex items-center text-code-11 text-text-tertiary"
        style={{ left: treeContentX(row.level) + TREE_CHEVRON + 4 }}
      >
        {row.node.loading ? "Loading…" : row.node.emptyLabel}
      </span>
    </div>
  );
});

/**
 * The tree, windowed, in a scroller of its own — give it a height with
 * `className` (flex-1 in a rail).
 *
 * One rendering for every size. Only the rows in view are in the DOM, which is
 * what keeps a 900-table connection cheap to bring back on screen: the rail
 * used to lay out every row each time, 96–134ms against 5–6ms windowed.
 *
 * Scrolling renders as little as it can. The fades are written to the
 * scroller's style directly, and the pins are state only when they change —
 * which is the push-up at a schema boundary and nothing else. Scrolling used
 * to set state on every event and re-render every visible row with it:
 * 8.3–8.7ms of main thread a step, p95 15ms, in the harness.
 */
export function TreeView({ nodes, className, keyboardNavigation = false, revealNodeId }: {
  nodes: TreeNodeData[];
  className?: string;
  /** IDE-style arrows, Home/End, Space preview and Enter keep. */
  keyboardNavigation?: boolean;
  /** Reveal once when the target changes, without taking keyboard focus. */
  revealNodeId?: string | null;
}) {
  const treeId = useId();
  const treeRef = useRef<HTMLDivElement>(null);
  const [focusedKey, setFocusedKey] = useState<string | null>(null);
  const revealed = useRef<string | null>(null);
  const expanding = useRef<{ id: string | null; nodes: Set<string> }>({ id: null, nodes: new Set() });
  const rowHeight = TREE_ROW_HEIGHT;
  const rows = useMemo(() => flattenTree(nodes), [nodes]);
  const pinnable = useMemo(
    () => rows.flatMap((row, i) => (row.node.sticky !== undefined && !row.placeholder ? [i] : [])),
    [rows],
  );
  const scrollRef = useRef<HTMLDivElement>(null);
  const [pins, setPins] = useState<Pin[]>([]);

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    getItemKey: (i) => rows[i].key,
    estimateSize: () => rowHeight,
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
    if (expanding.current.id !== revealNodeId) expanding.current = { id: revealNodeId ?? null, nodes: new Set() };
    if (!revealNodeId) { revealed.current = null; return; }
    if (revealed.current === revealNodeId) return;
    const path = treePath(nodes, revealNodeId);
    if (!path) return;
    const closed = path.slice(0, -1).filter((node) => node.expandable && !node.expanded);
    if (closed.length) {
      closed.forEach((node) => {
        // Effects may replay before the parent publishes its expanded state.
        if (expanding.current.nodes.has(node.id)) return;
        expanding.current.nodes.add(node.id);
        node.onToggle?.();
      });
      return;
    }
    const index = rows.findIndex((row) => !row.placeholder && row.node.id === revealNodeId);
    if (index < 0) return;
    virtualizer.scrollToIndex(index, { align: "auto" });
    revealed.current = revealNodeId;
  }, [revealNodeId, nodes, rows, virtualizer]);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    let lastPins = "";
    let lastMask = "";
    // The mask is on the scroller, and the scroller's box holds its scrollbar:
    // one gradient faded the thumb along with the rows. A second, solid layer
    // covers a strip at the right edge — the gutter where there is one, and at
    // least SCROLLBAR_STRIP for an overlay scrollbar that takes no gutter.
    // Measured on resize only; a rect read per scroll event forces layout.
    let strip = SCROLLBAR_STRIP;
    const measureStrip = () => {
      const tree = el.firstElementChild as HTMLElement | null;
      const gutter = tree ? el.getBoundingClientRect().right - tree.getBoundingClientRect().right : 0;
      strip = Math.max(Math.round(gutter), SCROLLBAR_STRIP);
      el.style.setProperty("mask-size", `calc(100% - ${strip}px) 100%, ${strip}px 100%`);
      el.style.setProperty("-webkit-mask-size", `calc(100% - ${strip}px) 100%, ${strip}px 100%`);
    };
    el.style.setProperty("mask-position", "left top, right top");
    el.style.setProperty("-webkit-mask-position", "left top, right top");
    el.style.setProperty("mask-repeat", "no-repeat");
    el.style.setProperty("-webkit-mask-repeat", "no-repeat");
    measureStrip();
    const update = (sync: boolean) => {
      const next = pinnedRows(rows, el.scrollTop - PAD_TOP, rowHeight, pinnable);
      const pinBottom = next.reduce((bottom, pin) => Math.max(bottom, pin.top + rowHeight), 0);
      const mask = `${fadeMask(
        pinBottom,
        el.scrollTop > 0,
        el.scrollTop + el.clientHeight < el.scrollHeight - 1,
      )}, linear-gradient(#000, #000)`;
      if (mask !== lastMask) {
        lastMask = mask;
        el.style.setProperty("mask-image", mask);
        el.style.setProperty("-webkit-mask-image", mask);
      }
      const key = next.map((pin) => `${pin.index}:${pin.top}`).join(",");
      if (key !== lastPins) {
        lastPins = key;
        // flushSync so a pin moves in the frame its rows do.
        if (sync) flushSync(() => setPins(next));
        else setPins(next);
      }
    };
    update(false);
    const onScroll = () => update(true);
    el.addEventListener("scroll", onScroll, { passive: true });
    const observer = new ResizeObserver(() => {
      measureStrip();
      update(false);
    });
    observer.observe(el);
    return () => {
      el.removeEventListener("scroll", onScroll);
      observer.disconnect();
    };
  }, [rows, pinnable, rowHeight]);

  return (
    <RowHeight.Provider value={rowHeight}>
    <div ref={scrollRef} className={cn("min-h-0 overflow-y-auto", className)}>
      <div
        ref={treeRef}
        className="group/tree"
        role="tree"
        tabIndex={keyboardNavigation ? 0 : undefined}
        aria-label={keyboardNavigation ? "Files" : undefined}
        aria-activedescendant={keyboardNavigation && focusedKey && virtualizer.getVirtualItems().some((item) => rows[item.index].key === focusedKey) ? `${treeId}-${rows.findIndex((row) => row.key === focusedKey)}` : undefined}
        onFocus={keyboardNavigation ? (event) => {
          const row = (event.target as HTMLElement).closest<HTMLElement>("[data-tree-key]");
          if (row) setFocusedKey(row.dataset.treeKey ?? null);
          else if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
            const selected = rows.find((item) => item.node.selected && !item.placeholder);
            setFocusedKey(selected?.key ?? rows.find((item) => !item.placeholder)?.key ?? null);
          }
        } : undefined}
        onKeyDown={keyboardNavigation ? (event) => {
          if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
          if (!["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight", "Home", "End", "Enter", " "].includes(event.key)) return;
          // A row's action button keeps its own keyboard behavior.
          if (event.target !== treeRef.current && !(event.target as HTMLElement).closest("[data-tree-key] button.flex-1")) return;
          event.preventDefault();
          let current = rows.findIndex((row) => row.key === focusedKey);
          if (current < 0) current = rows.findIndex((row) => row.node.selected && !row.placeholder);
          if (current < 0) current = rows.findIndex((row) => !row.placeholder);
          const row = rows[current];
          if (!row) return;
          treeRef.current?.focus({ preventScroll: true });
          setFocusedKey(row.key);
          if (event.key === "Enter") {
            if (row.node.expandable) row.node.onToggle?.();
            else (row.node.onActivate ?? row.node.onSelect)?.();
            return;
          }
          if (event.key === " ") { row.node.onSelect?.(); return; }
          if ((event.key === "ArrowRight" && row.node.expandable && !row.open) || (event.key === "ArrowLeft" && row.open)) {
            row.node.onToggle?.();
            return;
          }
          const next = treeKeyboardTarget(rows, current, event.key);
          if (next === null || next < 0) return;
          setFocusedKey(rows[next].key);
          virtualizer.scrollToIndex(next, { align: "auto" });
          rows[next].node.onSelect?.();
        } : undefined}
        style={{ position: "relative", height: virtualizer.getTotalSize() }}
      >
        {/* Pinned rows on a zero-height host stuck to the scroller's top.
            Copies for the eye and the pointer; the windowed rows are what
            assistive tech and the tab order see. Deepest first, so a schema
            being pushed out slides under its connection. */}
        <div style={{ position: "sticky", top: 0, height: 0, zIndex: 2 }}>
          {[...pins].reverse().map(({ index, top }) =>
            rows[index] ? (
              <div
                key={rows[index].key}
                aria-hidden
                className="bg-(--background-app)"
                style={{ position: "absolute", top, left: 0, right: 0 }}
              >
                <Row row={rows[index]} copy />
              </div>
            ) : null,
          )}
        </div>
        {virtualizer.getVirtualItems().map((item) => {
          const row = rows[item.index];
          return (
            <div
              key={item.key}
              style={{ position: "absolute", top: 0, left: 0, right: 0, transform: `translateY(${item.start}px)` }}
            >
              {row.placeholder ? <Placeholder row={row} /> : <Row row={row} keyboard={keyboardNavigation} focused={keyboardNavigation && row.key === focusedKey} id={`${treeId}-${item.index}`} />}
            </div>
          );
        })}
      </div>
    </div>
    </RowHeight.Provider>
  );
}
