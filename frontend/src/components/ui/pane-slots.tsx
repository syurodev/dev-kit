import * as React from "react";
import { createPortal } from "react-dom";
import { LazyMotion, domAnimation, m } from "framer-motion";
import { Bot, GripVertical, PanelRight, PanelsTopLeft, SquareTerminal, X } from "lucide-react";
import { useSplitPanel } from "@/components/ui/split-panel";
import { ShellResizeHandle } from "@/components/ui/resize-handle";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useResizableSize } from "@/workbench/useResizableSize";
import {
  GAP_WIDTH,
  maxBottomHeight,
  maxPanelWidth,
  useWindowHeight,
  useWindowWidth,
} from "@/workbench/panelLayout";
import { SPLIT_MIN_HEIGHT, splitColumn } from "@/workbench/splitColumn";
import {
  PANES,
  columnVisible,
  dropTarget,
  menuTargets,
  resolve,
  slotOf,
  type ColumnId,
  type Geometry,
  type PaneId,
  type Rect,
  type SlotId,
} from "@/workbench/slots/slotModel";
import { startPaneDrag } from "@/workbench/slots/paneDrag";
import { TerminalWorkspace } from "@/workbench/TerminalWorkspace";
import { getModule } from "@/modules/registry";
import { usePanelWidthsStore } from "@/stores/panelWidths";
import { placementFor, usePaneSlotsStore } from "@/stores/paneSlots";
import { useBottomSlotStore } from "@/stores/bottomSlot";
import { useBottomContentStore } from "@/stores/bottomContent";
import { shellSquircleRef } from "@/lib/shellSquircle";
import { cn } from "@/lib/utils";
import { EASE_OUT_STRONG, MOTION_FAST, MOTION_SLOW } from "@/lib/motion";
import "@/workbench/slots/slots.css";

const MIN_COLUMN_WIDTH = 320;
const MAX_COLUMN_WIDTH = 720;
const MIN_BOTTOM_HEIGHT = 120;
const MAX_BOTTOM_HEIGHT = 600;

type SlotsCtx = {
  hosts: Record<PaneId, HTMLDivElement>;
  titles: Record<PaneId, string>;
  resolved: ReturnType<typeof resolve>;
  close: (pane: PaneId) => void;
  moveTo: (pane: PaneId, slot: SlotId) => void;
  targets: (pane: PaneId) => ReturnType<typeof menuTargets>;
  beginDrag: (pane: PaneId, e: PointerEvent, card: HTMLElement, origin: HTMLElement) => void;
};
const Ctx = React.createContext<SlotsCtx | null>(null);
const useSlots = () => React.useContext(Ctx)!;

const ICONS: Record<PaneId, typeof Bot> = { context: PanelRight, agent: Bot, terminal: SquareTerminal };

const rectOf = (el: Element | null): Rect | undefined => {
  if (!el) return undefined;
  const r = el.getBoundingClientRect();
  return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
};

/**
 * Everything right of the sidebar: the document, and the panes that move
 * between the slots around it (workbench/slots/slotModel.ts).
 *
 *   document column   the content card (tab strip + views), and under it the
 *                     bottom row — under the document only, so the columns
 *                     keep their full height beside it
 *   column A, B       up to two columns on the right, each split top/bottom
 *
 * Which pane is drawn where comes from usePaneSlotsStore; whether a pane is
 * open still comes from where it always did — openPanel for the module's
 * context pane, the companion for the agent, useBottomSlotStore for the
 * terminal. Nothing a module calls changed.
 *
 * Pane bodies are rendered once, each into its own detached element, and a
 * slot card adopts that element. Moving a pane moves the element, so the
 * terminal's xterm, a canvas or a half-written message never remount.
 */
export function PaneSlotsRow({ activeModuleId, children }: { activeModuleId: string | null; children: React.ReactNode }) {
  const { panel, companion } = useSplitPanel();
  const bottomOpen = useBottomSlotStore((s) => s.open);
  const setBottomOpen = useBottomSlotStore((s) => s.setOpen);
  const moduleContent = useBottomContentStore((s) => (activeModuleId ? s.content[activeModuleId] : undefined));

  const agentSlot = usePaneSlotsStore((s) => s.agent);
  const terminalSlot = usePaneSlotsStore((s) => s.terminal);
  const contextByModule = usePaneSlotsStore((s) => s.contextByModule);
  const movePane = usePaneSlotsStore((s) => s.movePane);

  const placement = React.useMemo(
    () => placementFor({ agent: agentSlot, terminal: terminalSlot, contextByModule }, activeModuleId),
    [agentSlot, terminalSlot, contextByModule, activeModuleId],
  );
  const open = React.useMemo(
    () => ({ context: panel !== null, agent: companion !== null, terminal: bottomOpen }),
    [panel, companion, bottomOpen],
  );
  const resolved = React.useMemo(() => resolve(placement, open), [placement, open]);

  const titles = React.useMemo<Record<PaneId, string>>(
    () => ({
      context: (activeModuleId && getModule(activeModuleId)?.title) || "Panel",
      agent: "Agent",
      terminal: "Terminal",
    }),
    [activeModuleId],
  );

  const [hosts] = React.useState<Record<PaneId, HTMLDivElement>>(() => {
    const make = (pane: PaneId) => {
      const el = document.createElement("div");
      el.className = "flex min-h-0 flex-1 flex-col";
      el.dataset.paneHost = pane;
      return el;
    };
    return { context: make("context"), agent: make("agent"), terminal: make("terminal") };
  });

  const bodies: Record<PaneId, React.ReactNode> = {
    context: panel ? <div className="flex min-h-0 flex-1 flex-col overflow-auto">{panel.content}</div> : null,
    agent: companion ? <div className="flex min-h-0 flex-1 flex-col overflow-hidden">{companion.content}</div> : null,
    terminal: bottomOpen ? (
      <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
        <TerminalWorkspace moduleContent={moduleContent ?? undefined} />
      </div>
    ) : null,
  };

  // The module asks for a width when it opens its panel — honoured for the
  // column the panel lands in, as the split panel always did, and only on the
  // closed → open edge, so re-pushed content (NoteEditor does it on every
  // edit) never undoes a drag. Not in the bottom row: that has no width.
  const setWidthA = usePanelWidthsStore((s) => s.setSplitPanelWidth);
  const setWidthB = usePaneSlotsStore((s) => s.setColumnBWidth);
  const panelWasOpen = React.useRef(false);
  React.useEffect(() => {
    if (panel && !panelWasOpen.current) {
      const slot = slotOf(resolved, "context");
      const col = slot && slot !== "bottom" ? (slot[0] as ColumnId) : null;
      if (col) (col === "a" ? setWidthA : setWidthB)(panel.width);
    }
    panelWasOpen.current = panel !== null;
  }, [panel, resolved, setWidthA, setWidthB]);

  const rowRef = React.useRef<HTMLDivElement>(null);
  const docRef = React.useRef<HTMLDivElement | null>(null);
  const hintRef = React.useRef<HTMLDivElement>(null);

  const close = React.useCallback(
    (pane: PaneId) => {
      if (pane === "context") panel?.onClose();
      else if (pane === "agent") companion?.onClose();
      else setBottomOpen(false);
    },
    [panel, companion, setBottomOpen],
  );

  const moveTo = React.useCallback(
    (pane: PaneId, slot: SlotId) => movePane(pane, slot, activeModuleId),
    [movePane, activeModuleId],
  );

  const targets = React.useCallback(
    (pane: PaneId) => menuTargets(placement, open, pane, titles),
    [placement, open, titles],
  );

  const beginDrag = React.useCallback(
    (pane: PaneId, e: PointerEvent, card: HTMLElement, origin: HTMLElement) => {
      const row = rowRef.current;
      const hint = hintRef.current;
      if (!row || !hint || !docRef.current) return;
      startPaneDrag({
        down: e,
        card,
        origin,
        hint,
        measure: () => {
          const cards: Geometry["cards"] = {};
          for (const el of row.querySelectorAll<HTMLElement>("[data-slot-card]")) {
            cards[el.dataset.slotCard as SlotId] = rectOf(el);
          }
          const columns: Geometry["columns"] = {};
          for (const el of row.querySelectorAll<HTMLElement>("[data-slot-column]")) {
            if (el.getBoundingClientRect().width > 0) columns[el.dataset.slotColumn as ColumnId] = rectOf(el);
          }
          const geometry: Geometry = { row: rectOf(row)!, document: rectOf(docRef.current)!, columns, cards };
          return (x, y) => dropTarget({ placement, open, pane, geometry, x, y, titles });
        },
        onDrop: (slot) => moveTo(pane, slot),
      });
    },
    [placement, open, titles, moveTo],
  );

  const ctx = React.useMemo<SlotsCtx>(
    () => ({ hosts, titles, resolved, close, moveTo, targets, beginDrag }),
    [hosts, titles, resolved, close, moveTo, targets, beginDrag],
  );

  const setDocRef = React.useCallback((el: HTMLDivElement | null) => {
    docRef.current = el;
    return shellSquircleRef(el);
  }, []);

  return (
    <Ctx.Provider value={ctx}>
      <LazyMotion features={domAnimation}>
        <div ref={rowRef} data-slot-row className="flex min-h-0 min-w-0 flex-1">
          {/* The bottom row stacks under the document ONLY — the columns keep
              their full height beside it, so opening it never shortens them.
              min-w-0 because this column has no overflow-hidden of its own to
              zero its automatic minimum width. */}
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            {/* One card holds the strip and the views, which is how Figma's
                content-surface is built. Opaque on purpose: module content
                must not sit on the user's wallpaper. */}
            <div ref={setDocRef} className="flex min-h-0 flex-1 flex-col overflow-hidden shell-surface">
              {children}
            </div>
            <BottomRow pane={resolved.bottom ?? null} />
          </div>
          <SlotColumn column="a" />
          <SlotColumn column="b" />
        </div>
      </LazyMotion>

      {PANES.map((pane) => (bodies[pane] ? createPortal(bodies[pane], hosts[pane], pane) : null))}

      {createPortal(
        <div ref={hintRef} data-show="false" className="pane-drop-hint" aria-hidden>
          <span data-hint-label className="pane-drop-hint-label text-label-12" />
        </div>,
        document.body,
      )}
    </Ctx.Provider>
  );
}

/** The bottom row, under the document. Height is useBottomSlotStore's. */
function BottomRow({ pane }: { pane: PaneId | null }) {
  const height = useBottomSlotStore((s) => s.height);
  const setHeight = useBottomSlotStore((s) => s.setHeight);
  const windowHeight = useWindowHeight();
  const open = pane !== null;

  const max = maxBottomHeight({ windowHeight, min: MIN_BOTTOM_HEIGHT, hardMax: MAX_BOTTOM_HEIGHT });
  const { isResizing, onPointerDown } = useResizableSize({
    size: height,
    onSizeChange: setHeight,
    min: MIN_BOTTOM_HEIGHT,
    max,
    axis: "y",
    // The handle sits on this row's top edge, so dragging up grows it.
    sign: -1,
  });

  return (
    <m.div
      initial={false}
      // Clamped at render, not only while dragging: the ceiling moves with the
      // window, the stored height does not, and a stale one would crush the
      // document above it. One line: step4bComponents.test.tsx looks for it.
      animate={{ height: open ? Math.min(height, max) + GAP_WIDTH : 0 }}
      transition={isResizing ? { duration: 0 } : { duration: open ? MOTION_SLOW : MOTION_FAST, ease: EASE_OUT_STRONG }}
      className="flex min-w-0 shrink-0 flex-col overflow-hidden"
    >
      {open ? (
        <>
          <ShellResizeHandle
            orientation="horizontal"
            aria-label="Resize bottom row"
            onPointerDown={onPointerDown}
            isResizing={isResizing}
            style={{ height: GAP_WIDTH }}
          />
          <SlotCard slot="bottom" pane={pane} className="flex-1" />
        </>
      ) : null}
    </m.div>
  );
}

/**
 * One column on the right. Drawn only while it holds an open pane; a single
 * pane takes the full height, two share it at a draggable split.
 */
function SlotColumn({ column }: { column: ColumnId }) {
  const { resolved } = useSlots();
  const top = resolved[`${column}.top`] ?? null;
  const bottom = resolved[`${column}.bottom`] ?? null;
  const open = columnVisible(resolved, column);
  const other: ColumnId = column === "a" ? "b" : "a";
  const otherOpen = columnVisible(resolved, other);

  const widthA = usePanelWidthsStore((s) => s.splitPanelWidth);
  const setWidthA = usePanelWidthsStore((s) => s.setSplitPanelWidth);
  const topA = usePanelWidthsStore((s) => s.splitTopHeight);
  const setTopA = usePanelWidthsStore((s) => s.setSplitTopHeight);
  const widthB = usePaneSlotsStore((s) => s.columnBWidth);
  const setWidthB = usePaneSlotsStore((s) => s.setColumnBWidth);
  const topB = usePaneSlotsStore((s) => s.columnBTopHeight);
  const setTopB = usePaneSlotsStore((s) => s.setColumnBTopHeight);
  const [width, setWidth, otherWidth] = column === "a" ? [widthA, setWidthA, widthB] : [widthB, setWidthB, widthA];
  const [topHeight, setTopHeight] = column === "a" ? [topA, setTopA] : [topB, setTopB];

  const sidebarWidth = usePanelWidthsStore((s) => s.sidebarWidth);
  const sidebarOpen = usePanelWidthsStore((s) => s.sidebarOpen);
  const windowWidth = useWindowWidth();

  // Clamped against the sidebar and the other column: with everything open on
  // a narrow window a flat ceiling would crush the document through its floor.
  const max = maxPanelWidth({
    windowWidth,
    otherEffectiveWidth: (sidebarOpen ? sidebarWidth : 0) + (otherOpen ? Math.min(otherWidth, MAX_COLUMN_WIDTH) + GAP_WIDTH : 0),
    min: MIN_COLUMN_WIDTH,
    hardMax: MAX_COLUMN_WIDTH,
  });
  const drawn = open ? Math.min(width, max) + GAP_WIDTH : 0;

  // Publish the columns' occupancy so SidebarSurface can clamp against it.
  const setColumnWidth = usePanelWidthsStore((s) => s.setColumnWidth);
  React.useEffect(() => setColumnWidth(column, drawn), [column, drawn, setColumnWidth]);

  const { isResizing, onPointerDown } = useResizableSize({
    size: width,
    onSizeChange: setWidth,
    min: MIN_COLUMN_WIDTH,
    max,
    axis: "x",
    sign: -1,
  });

  // Measured, not derived: the column's height is the row's, and re-deriving
  // that here would drift the moment the title bar or gutter changes.
  const columnRef = React.useRef<HTMLDivElement | null>(null);
  const [columnHeight, setColumnHeight] = React.useState(0);
  React.useLayoutEffect(() => {
    const el = columnRef.current;
    if (!el) return;
    setColumnHeight(el.getBoundingClientRect().height);
    const observer = new ResizeObserver((entries) => setColumnHeight(entries[0].contentRect.height));
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const both = top !== null && bottom !== null;
  const sizes = splitColumn({ columnHeight, topOpen: top !== null, bottomOpen: bottom !== null, topHeight });
  // sign +1: the handle sits under the top slot, so dragging down grows it.
  const split = useResizableSize({
    size: topHeight,
    onSizeChange: setTopHeight,
    min: SPLIT_MIN_HEIGHT,
    max: Math.max(SPLIT_MIN_HEIGHT, columnHeight - GAP_WIDTH - SPLIT_MIN_HEIGHT),
    axis: "y",
    sign: 1,
  });

  return (
    <m.div
      initial={false}
      // Clamped at render for the same reason as the sidebar and the bottom
      // row. One line: step4bComponents.test.tsx looks for it.
      animate={{ width: open ? Math.min(width, max) + GAP_WIDTH : 0 }}
      transition={isResizing ? { duration: 0 } : { duration: open ? MOTION_SLOW : MOTION_FAST, ease: EASE_OUT_STRONG }}
      className="flex min-h-0 shrink-0 overflow-hidden"
    >
      {open ? (
        <ShellResizeHandle
          orientation="vertical"
          aria-label={`Resize column ${column.toUpperCase()}`}
          onPointerDown={onPointerDown}
          isResizing={isResizing}
          style={{ width: GAP_WIDTH }}
        />
      ) : null}
      <div ref={columnRef} data-slot-column={column} className="flex min-h-0 min-w-0 flex-1 flex-col">
        {top ? (
          <SlotCard
            slot={`${column}.top`}
            pane={top}
            className={both ? "shrink-0" : "flex-1"}
            style={both ? { height: sizes.top } : undefined}
          />
        ) : null}
        {both ? (
          <ShellResizeHandle
            orientation="horizontal"
            aria-label={`Resize column ${column.toUpperCase()} split`}
            onPointerDown={split.onPointerDown}
            isResizing={split.isResizing}
            style={{ height: GAP_WIDTH }}
          />
        ) : null}
        {bottom ? (
          <SlotCard
            slot={`${column}.bottom`}
            pane={bottom}
            className={both ? "shrink-0" : "flex-1"}
            style={both ? { height: sizes.bottom } : undefined}
          />
        ) : null}
      </div>
    </m.div>
  );
}

/**
 * A slot's card: the pane header, and the body element the pane's content was
 * rendered into, adopted for as long as this slot holds that pane.
 */
function SlotCard({
  slot,
  pane,
  className,
  style,
}: {
  slot: SlotId;
  pane: PaneId;
  className?: string;
  style?: React.CSSProperties;
}) {
  const { hosts } = useSlots();
  const originRef = React.useRef<HTMLDivElement>(null);
  const cardRef = React.useRef<HTMLDivElement | null>(null);
  const bodyRef = React.useRef<HTMLDivElement>(null);

  React.useLayoutEffect(() => {
    const body = bodyRef.current;
    const host = hosts[pane];
    if (!body) return;
    body.appendChild(host);
    return () => {
      // Only if it is still ours: when two panes swap, the other slot may
      // already have adopted it.
      if (host.parentNode === body) body.removeChild(host);
    };
  }, [hosts, pane]);

  const setCard = React.useCallback((el: HTMLDivElement | null) => {
    cardRef.current = el;
    return shellSquircleRef(el);
  }, []);

  return (
    <div ref={originRef} data-slot-card={slot} className={cn("pane-slot relative flex min-h-0 min-w-0 flex-col", className)} style={style}>
      <div ref={setCard} className="flex min-h-0 flex-1 flex-col overflow-hidden shell-surface">
        <PaneHeader pane={pane} cardRef={cardRef} originRef={originRef} />
        <div ref={bodyRef} className="flex min-h-0 flex-1 flex-col" />
      </div>
    </div>
  );
}

const headerButton =
  "flex size-6 shrink-0 items-center justify-center rounded-md text-text-tertiary hover:bg-state-soft hover:text-text-primary";

/**
 * Grip, title, Move to… and close. The whole header is the drag handle; the
 * buttons are not.
 */
function PaneHeader({
  pane,
  cardRef,
  originRef,
}: {
  pane: PaneId;
  cardRef: React.RefObject<HTMLDivElement | null>;
  originRef: React.RefObject<HTMLDivElement | null>;
}) {
  const { titles, close, moveTo, targets, beginDrag } = useSlots();
  const ref = React.useRef<HTMLDivElement>(null);
  const Icon = ICONS[pane];
  const title = titles[pane];

  // Native, not onPointerDown: the drag needs the raw event's button and a
  // preventDefault that stops a text selection starting before the threshold.
  React.useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onDown = (e: PointerEvent) => {
      if ((e.target as HTMLElement).closest("button")) return;
      if (!cardRef.current || !originRef.current) return;
      e.preventDefault();
      beginDrag(pane, e, cardRef.current, originRef.current);
    };
    el.addEventListener("pointerdown", onDown);
    return () => el.removeEventListener("pointerdown", onDown);
  }, [pane, beginDrag, cardRef, originRef]);

  const list = targets(pane);

  return (
    <div
      ref={ref}
      className="flex h-8 shrink-0 cursor-grab items-center gap-1.5 border-b border-border-subtle pr-1 pl-1.5 text-text-secondary active:cursor-grabbing"
    >
      <GripVertical className="size-3.5 shrink-0 text-text-tertiary" aria-hidden />
      <Icon className="size-3.5 shrink-0" aria-hidden />
      <span className="min-w-0 flex-1 truncate text-label-12">{title}</span>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button type="button" aria-label={`Move ${title} to…`} title="Move to…" className={headerButton}>
            <PanelsTopLeft className="size-3.5" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-72">
          <DropdownMenuLabel>Move {title} to</DropdownMenuLabel>
          {list.map((t) => (
            <DropdownMenuItem key={t.slot} disabled={t.current} onSelect={() => moveTo(pane, t.slot)} detail={t.current ? "current" : undefined}>
              {t.label}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      <button type="button" aria-label={`Close ${title}`} title="Close" className={headerButton} onClick={() => close(pane)}>
        <X className="size-3.5" />
      </button>
    </div>
  );
}
