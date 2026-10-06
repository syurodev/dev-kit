import * as React from "react";
import { createPortal } from "react-dom";
import { LazyMotion, domAnimation, m } from "framer-motion";
import { Bot, PanelRight, PanelsTopLeft, SquareTerminal, X } from "lucide-react";
import { SHELL_PANEL_OWNER, useSplitPanel } from "@/components/ui/split-panel";
import { useShallow } from "zustand/react/shallow";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { ShellResizeHandle } from "@/components/ui/resize-handle";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
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
  columnVisible,
  contextPane,
  moduleOfPane,
  move,
  dropTarget,
  menuTargets,
  resolve,
  slotOf,
  type ColumnId,
  type Geometry,
  type OpenPanes,
  type PaneId,
  type Rect,
  type SlotId,
} from "@/workbench/slots/slotModel";
import { startPaneDrag } from "@/workbench/slots/paneDrag";
import { useLayoutPressure } from "@/workbench/useLayoutPressure";
import type { LayoutPressure } from "@/workbench/panelLayout";
import { TerminalTabs, TerminalWorkspace } from "@/workbench/TerminalWorkspace";
import { AgentPaneTabs } from "@/workbench/AgentPaneTabs";
import { getModule } from "@/modules/registry";
import { usePanelWidthsStore } from "@/stores/panelWidths";
import { placementFor, usePaneSlotsStore } from "@/stores/paneSlots";
import { useBottomSlotStore } from "@/stores/bottomSlot";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
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
  hostFor: (pane: PaneId) => HTMLDivElement;
  titles: Record<PaneId, string>;
  /** The context pane of the focused module, when several are on screen. */
  focusedPane: PaneId | null;
  /** What gives way while the window is too narrow (panelLayout.layoutPressure). */
  pressure: LayoutPressure;
  /** Whether the active module put its own tab in the terminal pane. */
  terminalHasModule: boolean;
  resolved: ReturnType<typeof resolve>;
  close: (pane: PaneId) => void;
  moveTo: (pane: PaneId, slot: SlotId) => void;
  targets: (pane: PaneId) => ReturnType<typeof menuTargets>;
  beginDrag: (pane: PaneId, e: PointerEvent, card: HTMLElement, origin: HTMLElement) => void;
};
const Ctx = React.createContext<SlotsCtx | null>(null);
const useSlots = () => React.useContext(Ctx)!;

/** A companion's glyph, or the module's own for its context pane. */
function iconFor(pane: PaneId): typeof Bot {
  if (pane === "agent") return Bot;
  if (pane === "terminal") return SquareTerminal;
  const m = moduleOfPane(pane);
  return (m && getModule(m)?.icon) || PanelRight;
}

const rectOf = (el: Element | null): Rect | undefined => {
  if (!el) return undefined;
  const r = el.getBoundingClientRect();
  return { left: r.left, top: r.top, right: r.right, bottom: r.bottom };
};

/**
 * Everything right of the sidebar: the document, and the panes that move
 * between the slots around it (workbench/slots/slotModel.ts).
 *
 *   document column   the document groups (workbench/DocumentGroups.tsx), and
 *                     under them the bottom row — under the document only, so the columns
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
  const { panels, companion } = useSplitPanel();
  const bottomOpen = useBottomSlotStore((s) => s.open);
  const setBottomOpen = useBottomSlotStore((s) => s.setOpen);
  const moduleContent = useBottomContentStore((s) => (activeModuleId ? s.content[activeModuleId] : undefined));

  // Modules on screen: one per document group, the focused one first so its
  // context pane wins a contested slot.
  const groupModules = useWorkbenchTabsStore(useShallow((s) => s.groups.map((g) => g.moduleId)));
  const visibleModules = React.useMemo(() => {
    const list = activeModuleId ? [activeModuleId] : [];
    for (const m of groupModules) if (m && !list.includes(m)) list.push(m);
    // The dev harness and anything else outside a module.
    list.push(SHELL_PANEL_OWNER);
    return list;
  }, [activeModuleId, groupModules]);

  const agentSlot = usePaneSlotsStore((s) => s.agent);
  const terminalSlot = usePaneSlotsStore((s) => s.terminal);
  const contextByModule = usePaneSlotsStore((s) => s.contextByModule);
  const applyPlacement = usePaneSlotsStore((s) => s.applyPlacement);

  const placement = React.useMemo(
    () => placementFor({ agent: agentSlot, terminal: terminalSlot, contextByModule }, visibleModules),
    [agentSlot, terminalSlot, contextByModule, visibleModules],
  );
  const open = React.useMemo(() => {
    const o: OpenPanes = { agent: companion !== null, terminal: bottomOpen };
    for (const m of visibleModules) o[contextPane(m)] = m in panels;
    return o;
  }, [panels, companion, bottomOpen, visibleModules]);
  const resolved = React.useMemo(() => resolve(placement, open), [placement, open]);

  const titles = React.useMemo(() => {
    const t: Record<PaneId, string> = { agent: "Agent", terminal: "Terminal" };
    for (const m of visibleModules) t[contextPane(m)] = getModule(m)?.title ?? "Panel";
    return t;
  }, [visibleModules]);

  // One detached host per pane, made on first use and kept: a pane that comes
  // back finds the same element.
  const hostsRef = React.useRef(new Map<PaneId, HTMLDivElement>());
  const hostFor = React.useCallback((pane: PaneId) => {
    let el = hostsRef.current.get(pane);
    if (!el) {
      el = document.createElement("div");
      el.className = "flex min-h-0 flex-1 flex-col";
      el.dataset.paneHost = pane;
      hostsRef.current.set(pane, el);
    }
    return el;
  }, []);

  // Keep terminals alive in their detached host when the panel is hidden.
  const hasTerminalTabs = useTerminalTabsStore((s) => s.tabs.length > 0);
  const bodies: [PaneId, React.ReactNode][] = [
    ["agent", companion ? <div className="flex min-h-0 flex-1 flex-col overflow-hidden">{companion.content}</div> : null],
    [
      "terminal",
      bottomOpen || hasTerminalTabs ? (
        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
          <TerminalWorkspace moduleContent={moduleContent ?? undefined} />
        </div>
      ) : null,
    ],
    ...visibleModules.map((m): [PaneId, React.ReactNode] => [
      contextPane(m),
      panels[m] ? <div className="flex min-h-0 flex-1 flex-col overflow-auto">{panels[m].content}</div> : null,
    ]),
  ];

  // A module asks for a width when it opens its panel — honoured for the
  // column the panel lands in, as the split panel always did, and only on the
  // closed → open edge, so re-pushed content (NoteEditor does it on every
  // edit) never undoes a drag. Not in the bottom row: that has no width.
  const setWidthA = usePanelWidthsStore((s) => s.setSplitPanelWidth);
  const setWidthB = usePaneSlotsStore((s) => s.setColumnBWidth);
  const wasOpen = React.useRef(new Set<string>());
  React.useEffect(() => {
    for (const m of visibleModules) {
      const panel = panels[m];
      if (panel && !wasOpen.current.has(m)) {
        const slot = slotOf(resolved, contextPane(m));
        const col = slot && slot !== "bottom" ? (slot[0] as ColumnId) : null;
        if (col) (col === "a" ? setWidthA : setWidthB)(panel.width);
      }
    }
    wasOpen.current = new Set(visibleModules.filter((m) => m in panels));
  }, [panels, visibleModules, resolved, setWidthA, setWidthB]);

  const rowRef = React.useRef<HTMLDivElement>(null);
  const docRef = React.useRef<HTMLDivElement | null>(null);
  const hintRef = React.useRef<HTMLDivElement>(null);

  const close = React.useCallback(
    (pane: PaneId) => {
      const m = moduleOfPane(pane);
      if (m) panels[m]?.onClose();
      else if (pane === "agent") companion?.onClose();
      else setBottomOpen(false);
    },
    [panels, companion, setBottomOpen],
  );

  const moveTo = React.useCallback(
    (pane: PaneId, slot: SlotId) => applyPlacement(move(placement, pane, slot)),
    [applyPlacement, placement],
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

  // With more than one context pane on screen, the focused module's reads as
  // focused and the others as idle — the only cue for where keys will go.
  const contextCount = Object.keys(resolved).filter((s) => moduleOfPane(resolved[s as SlotId]!)).length;
  const focusedPane = contextCount > 1 && activeModuleId ? contextPane(activeModuleId) : null;

  // The columns' count before any merge, for the pressure calculation every
  // surface shares — published from here because only here is it known.
  const visibleColumns = (["a", "b"] as const).filter((c) => columnVisible(resolved, c)).length;
  const setVisibleColumns = usePanelWidthsStore((s) => s.setVisibleColumns);
  React.useEffect(() => setVisibleColumns(visibleColumns), [visibleColumns, setVisibleColumns]);
  const pressure = useLayoutPressure();

  const ctx = React.useMemo<SlotsCtx>(
    () => ({
      hostFor,
      pressure,
      titles,
      focusedPane,
      terminalHasModule: moduleContent != null,
      resolved,
      close,
      moveTo,
      targets,
      beginDrag,
    }),
    [hostFor, pressure, titles, focusedPane, moduleContent, resolved, close, moveTo, targets, beginDrag],
  );

  return (
    <Ctx.Provider value={ctx}>
      <LazyMotion features={domAnimation}>
        <div ref={rowRef} data-slot-row className="flex min-h-0 min-w-0 flex-1">
          {/* The bottom row stacks under the document ONLY — the columns keep
              their full height beside it, so opening it never shortens them.
              min-w-0 because this column has no overflow-hidden of its own to
              zero its automatic minimum width. */}
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            {/* The document area: one card per document group
                (workbench/DocumentGroups.tsx). Not a card itself — the groups
                are, so two of them side by side sit on the app background with
                the same gap as everything else. */}
            <div ref={docRef} data-slot-document className="flex min-h-0 min-w-0 flex-1">
              {children}
            </div>
            <BottomRow pane={resolved.bottom ?? null} />
          </div>
          <SlotColumn column="a" />
          <SlotColumn column="b" />
        </div>
      </LazyMotion>

      {bodies.map(([pane, body]) => (body ? createPortal(body, hostFor(pane), pane) : null))}

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
  const { resolved, pressure } = useSlots();
  const top = resolved[`${column}.top`] ?? null;
  const bottom = resolved[`${column}.bottom`] ?? null;
  // Under pressure the second column stacks into the first rather than take a
  // column of its own; its panes keep their slots, so they go back on widening.
  const merged = pressure.mergeColumns;
  const extras: [SlotId, PaneId][] =
    merged && column === "a"
      ? (["b.top", "b.bottom"] as const).flatMap((s) => (resolved[s] ? [[s, resolved[s]!] as [SlotId, PaneId]] : []))
      : [];
  const open = merged && column === "b" ? false : columnVisible(resolved, column) || extras.length > 0;
  const other: ColumnId = column === "a" ? "b" : "a";
  const otherOpen = merged ? false : columnVisible(resolved, other);

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
    contentMin: pressure.contentMin,
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
        {extras.length > 0 ? (
          // Stacked: every pane an equal share, no split to drag — this is a
          // temporary arrangement that the window's width will undo.
          <div className="flex min-h-0 flex-1 flex-col" style={{ gap: GAP_WIDTH }}>
            {([...(top ? [[`${column}.top`, top]] : []), ...(bottom ? [[`${column}.bottom`, bottom]] : []), ...extras] as [SlotId, PaneId][]).map(
              ([slot, pane]) => (
                <SlotCard key={slot} slot={slot} pane={pane} className="flex-1" />
              ),
            )}
          </div>
        ) : null}
        {extras.length === 0 && top ? (
          <SlotCard
            slot={`${column}.top`}
            pane={top}
            className={both ? "shrink-0" : "flex-1"}
            style={both ? { height: sizes.top } : undefined}
          />
        ) : null}
        {extras.length === 0 && both ? (
          <ShellResizeHandle
            orientation="horizontal"
            aria-label={`Resize column ${column.toUpperCase()} split`}
            onPointerDown={split.onPointerDown}
            isResizing={split.isResizing}
            style={{ height: GAP_WIDTH }}
          />
        ) : null}
        {extras.length === 0 && bottom ? (
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
  const { hostFor } = useSlots();
  const originRef = React.useRef<HTMLDivElement>(null);
  const cardRef = React.useRef<HTMLDivElement | null>(null);
  const bodyRef = React.useRef<HTMLDivElement>(null);

  React.useLayoutEffect(() => {
    const body = bodyRef.current;
    const host = hostFor(pane);
    if (!body) return;
    body.appendChild(host);
    return () => {
      // Only if it is still ours: when two panes swap, the other slot may
      // already have adopted it.
      if (host.parentNode === body) body.removeChild(host);
    };
  }, [hostFor, pane]);

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
  const { titles, focusedPane, terminalHasModule, close, moveTo, targets, beginDrag } = useSlots();
  const ref = React.useRef<HTMLDivElement>(null);
  const Icon = iconFor(pane);
  const idle = focusedPane !== null && moduleOfPane(pane) !== null && pane !== focusedPane;
  const title = titles[pane];

  // Native, not onPointerDown: the drag needs the raw event's button and a
  // preventDefault that stops a text selection starting before the threshold.
  React.useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const onDown = (e: PointerEvent) => {
      // Controls in the header — its buttons, and the terminal's tabs and
      // their close marks — are not handles.
      if ((e.target as HTMLElement).closest("button, [role=button], [role=tab]")) return;
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
      data-idle={idle || undefined}
      className={cn(
        "group/pane-header relative flex h-8 shrink-0 items-center gap-1.5 border-b border-border-subtle pr-1 pl-2.5",
        idle ? "text-text-tertiary" : "text-text-secondary",
      )}
    >
      {/* The handle: a short bar centred on the top edge, shown while the
          pointer is over the header. The whole header still drags; the bar is
          what says so, without a grip icon taking a place in the title row. */}
      <Tooltip>
        <TooltipTrigger asChild>
          <div
            data-pane-handle
            aria-label={`Move ${title}`}
            className="absolute top-0 left-1/2 z-10 flex h-3 w-12 -translate-x-1/2 cursor-grab items-center justify-center active:cursor-grabbing"
          >
            <span className="h-1 w-8 rounded-full bg-text-tertiary opacity-0 transition-opacity duration-150 group-hover/pane-header:opacity-60 hover:opacity-100!" />
          </div>
        </TooltipTrigger>
        <TooltipContent side="bottom">Move</TooltipContent>
      </Tooltip>
      <Icon className="size-3.5 shrink-0" aria-hidden />
      {/* The terminal's tabs are its title: one header, not a header over a
          strip. The empty stretch of the strip still drags the pane. */}
      {pane === "terminal" ? (
        <TerminalTabs showModule={terminalHasModule} />
      ) : pane === "agent" ? (
        <AgentPaneTabs />
      ) : (
        <span className="min-w-0 flex-1 truncate text-label-12">{title}</span>
      )}
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
