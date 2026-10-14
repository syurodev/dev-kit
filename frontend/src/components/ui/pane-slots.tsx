import * as React from "react";
import { createPortal } from "react-dom";
import { LazyMotion, domAnimation, m, useReducedMotion } from "framer-motion";
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
  requestedWidths,
  resolve,
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
import { panelCloseMs, panelContentFade, panelTransition, useClosedInert, usePanelMotionReport } from "@/lib/panelMotion";
import {
  useResting,
  useRetainedRecord,
  useRetainedWhileClosing,
  useStillLeaving,
} from "@/workbench/slots/retain";
import { dismissOwnedLayers } from "@/workbench/slots/ownedLayers";
import { moveHost } from "@/workbench/slots/hostMove";
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
  /** Panes kept alive while hidden (the Agent companion): a closed slot goes on holding them. */
  resting: ReadonlySet<PaneId>;
  /** A slot card lets go of a pane's host: detached, or parked if the pane is kept alive. */
  release: (pane: PaneId, host: HTMLDivElement, body: HTMLElement) => void;
  moveTo: (pane: PaneId, slot: SlotId) => void;
  targets: (pane: PaneId) => ReturnType<typeof menuTargets>;
  beginDrag: (pane: PaneId, e: PointerEvent, card: HTMLElement, origin: HTMLElement) => void;
};
const Ctx = React.createContext<SlotsCtx | null>(null);
const useSlots = () => React.useContext(Ctx)!;

/**
 * Whether the panel a card sits in is opening or closing, for the card's
 * content fade: the card itself — the surface — travels with its panel, and
 * what is on it trails in after (pane-slots.tsx, above BottomRow).
 */
const PanelFade = React.createContext<{ open: boolean; instant: boolean }>({ open: true, instant: true });

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
    // A hidden companion is still mounted (AgentCompanionSlot), but not open.
    const o: OpenPanes = { agent: companion !== null && !companion.hidden, terminal: bottomOpen };
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
  const terminalBody = React.useMemo(
    () => (
      <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
        <TerminalWorkspace moduleContent={moduleContent ?? undefined} />
      </div>
    ),
    [moduleContent],
  );
  const terminalAlive = bottomOpen || hasTerminalTabs;
  // What each pane renders, by identity: the content the module (or the
  // companion) pushed, so a re-render of this row is not a change.
  const liveBodies = React.useMemo(() => {
    const out: Record<PaneId, React.ReactNode> = {};
    // Mounted while hidden too: closing the Agent pane keeps the conversation,
    // its scroll and its composer, and reopening it is instant.
    if (companion) out.agent = <div className="flex min-h-0 flex-1 flex-col overflow-hidden">{companion.content}</div>;
    if (terminalAlive) out.terminal = terminalBody;
    for (const m of visibleModules) {
      const panel = panels[m];
      if (panel) out[contextPane(m)] = <div className="flex min-h-0 flex-1 flex-col overflow-auto">{panel.content}</div>;
    }
    return out;
  }, [companion, terminalAlive, terminalBody, visibleModules, panels]);
  // A pane that closes keeps its last content while its slot closes around it,
  // instead of sliding out as an empty card (workbench/slots/retain.ts).
  const reduceMotion = useReducedMotion() ?? false;
  const bodies = useRetainedRecord(liveBodies, panelCloseMs(reduceMotion));

  // A module asks for a width when it opens its panel (slotModel.requestedWidths).
  // In a layout effect: written before the first frame, so the column's open
  // animation starts toward the width it ends at instead of re-targeting.
  const setWidthA = usePanelWidthsStore((s) => s.setSplitPanelWidth);
  const setWidthB = usePaneSlotsStore((s) => s.setColumnBWidth);
  const wasOpen = React.useRef(new Set<string>());
  React.useLayoutEffect(() => {
    const widths = requestedWidths({ modules: visibleModules, panels, wasOpen: wasOpen.current, resolved });
    if (widths.a !== undefined) setWidthA(widths.a);
    if (widths.b !== undefined) setWidthB(widths.b);
    wasOpen.current = new Set(visibleModules.filter((m) => m in panels));
  }, [panels, visibleModules, resolved, setWidthA, setWidthB]);

  // A hidden companion stays in the card it was in (retain.restingStep):
  // moving it costs WebKit a rebuild of the whole conversation. Only when
  // another pane takes its slot, or it is shown somewhere else, does its host
  // go to the park: in the document, at the size it had, out of sight and out
  // of reach. Detached, its scroll boxes would be lost; display:none would do
  // the same.
  const companionHidden = companion?.hidden ?? false;
  const resting = React.useMemo<ReadonlySet<PaneId>>(() => new Set(companionHidden ? ["agent"] : []), [companionHidden]);
  const parkRef = React.useRef<HTMLDivElement>(null);
  const release = React.useCallback((pane: PaneId, host: HTMLDivElement, body: HTMLElement) => {
    // Only if it is still ours: when two panes swap, the other slot may
    // already have adopted it.
    if (host.parentNode !== body) return;
    const park = parkRef.current;
    if (pane === "agent" && park) {
      // Focus cannot stay in an inert, invisible subtree.
      if (host.contains(document.activeElement)) (document.activeElement as HTMLElement | null)?.blur();
      const r = body.getBoundingClientRect();
      if (r.width > 0 && r.height > 0) {
        park.style.width = `${r.width}px`;
        park.style.height = `${r.height}px`;
      }
      moveHost(park, host);
      return;
    }
    body.removeChild(host);
  }, []);

  // A menu, popover or select the conversation opened is portalled to the
  // body and would outlive the pane closing around it — a modal one with the
  // whole app's pointer events off. Closed the way the person would: Escape,
  // while the topmost open layer is one the companion owns. Focus cannot stay
  // in the inert box it now rests in either.
  React.useEffect(() => {
    if (!companionHidden) return;
    const host = hostFor("agent");
    if (host.contains(document.activeElement)) (document.activeElement as HTMLElement | null)?.blur();
    dismissOwnedLayers(host, document);
  }, [companionHidden, hostFor]);

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
            // A card in a closed slot (a resting companion, a pane on its way
            // out) is clipped away, but its box is still there under the open ones.
            if (el.closest("[inert]")) continue;
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
  // Before paint: the sidebar reads pressure from it, so a column opening on a
  // narrow window and the sidebar giving way for it start in the same frame,
  // each toward its final size, rather than the column re-targeting a frame
  // later when the sidebar's yield frees room.
  React.useLayoutEffect(() => setVisibleColumns(visibleColumns), [visibleColumns, setVisibleColumns]);
  const pressure = useLayoutPressure();

  const ctx = React.useMemo<SlotsCtx>(
    () => ({
      hostFor,
      pressure,
      titles,
      resting,
      focusedPane,
      terminalHasModule: moduleContent != null,
      resolved,
      close,
      release,
      moveTo,
      targets,
      beginDrag,
    }),
    [hostFor, pressure, titles, resting, focusedPane, moduleContent, resolved, close, release, moveTo, targets, beginDrag],
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

      {Object.entries(bodies).map(([pane, body]) => createPortal(body, hostFor(pane), pane))}

      <div
        ref={parkRef}
        data-pane-park
        aria-hidden
        inert
        className="pointer-events-none invisible fixed top-0 left-full flex flex-col contain-strict"
      />

      {createPortal(
        <div ref={hintRef} data-show="false" className="pane-drop-hint" aria-hidden>
          <span data-hint-label className="pane-drop-hint-label text-label-12" />
        </div>,
        document.body,
      )}
    </Ctx.Provider>
  );
}

/**
 * How a shell panel moves (smoothness §3.8, §4.4), the same for the bottom
 * row, the columns and the sidebar (sidebar-shell.tsx):
 *
 *   - the outer box animates its size and clips;
 *   - an inner box is pinned at the size the panel ends at, anchored to the
 *     edge that moves, so the content lays out once and slides in with that
 *     edge instead of reflowing on every frame;
 *   - the content fades in CONTENT_FADE_DELAY after the box starts, and out
 *     with it;
 *   - closing keeps the last content until the box is shut (panelCloseMs);
 *   - the move is reported to lib/panelMotion, so what re-measures on resize
 *     beside it waits for the end.
 *
 * The document beside it still re-lays out each frame — that is CSS flex and
 * cheap; what was not cheap is what listens to it.
 */

/** The bottom row, under the document. Height is useBottomSlotStore's. */
function BottomRow({ pane }: { pane: PaneId | null }) {
  const { resolved, resting } = useSlots();
  const height = useBottomSlotStore((s) => s.height);
  const setHeight = useBottomSlotStore((s) => s.setHeight);
  const windowHeight = useWindowHeight();
  const reduceMotion = useReducedMotion() ?? false;
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

  const drawn = open ? Math.min(height, max) + GAP_WIDTH : 0;
  const [lastDrawn, setLastDrawn] = React.useState(drawn);
  if (open && drawn !== lastDrawn) setLastDrawn(drawn);
  const motion = usePanelMotionReport({
    id: "bottom",
    target: drawn,
    open,
    stored: height,
    instant: isResizing || reduceMotion,
    windowSize: windowHeight,
  });
  const fade = React.useMemo(() => ({ open, instant: motion.instant }), [open, motion.instant]);
  const { inert, shielded } = useClosedInert(open, motion.still);

  // While it closes, what it showed — unless that pane is on screen elsewhere
  // now (moved to a column), where it already is — and after, a hidden
  // companion it held, for as long as it is hidden (retain.restingStep).
  const kept = useRetainedWhileClosing(pane, panelCloseMs(reduceMotion));
  const leaving = useStillLeaving(pane === null && kept !== null ? { pane: kept } : null, Object.values(resolved));
  const shown = useResting({ pane: pane ?? leaving?.pane ?? null }, resting, Object.values(resolved)).drawn.pane ?? null;

  return (
    <PanelFade.Provider value={fade}>
      <m.div
        initial={false}
        // Clamped at render, not only while dragging: the ceiling moves with the
        // window, the stored height does not, and a stale one would crush the
        // document above it. One line: step4bComponents.test.tsx looks for it.
        animate={{ height: open ? Math.min(height, max) + GAP_WIDTH : 0 }}
        transition={panelTransition(open, motion.instant)}
        onAnimationComplete={motion.onAnimationComplete}
        className="relative min-w-0 shrink-0 overflow-hidden"
      >
        {/* Pinned to the open height and hung from the top edge, the one that
            moves: the row slides up into view rather than growing. Inert once
            closed, a shield while it closes (panelMotion.useClosedInert). */}
        <div inert={inert} className="absolute inset-x-0 top-0 flex flex-col" style={{ height: open ? drawn : lastDrawn }}>
          {shielded ? <div aria-hidden className="absolute inset-0 z-50" /> : null}
          {shown ? (
            <ShellResizeHandle
              orientation="horizontal"
              aria-label="Resize bottom row"
              onPointerDown={onPointerDown}
              isResizing={isResizing}
              style={{ height: GAP_WIDTH }}
            />
          ) : null}
          {shown ? <SlotCard slot="bottom" pane={shown} className="flex-1" /> : null}
        </div>
      </m.div>
    </PanelFade.Provider>
  );
}

/** What a column shows: its own two slots, and column B's while it is stacked in. */
type ColumnPanes = { top: PaneId | null; bottom: PaneId | null; bTop: PaneId | null; bBottom: PaneId | null };

/**
 * One column on the right. Drawn only while it holds an open pane; a single
 * pane takes the full height, two share it at a draggable split.
 */
function SlotColumn({ column }: { column: ColumnId }) {
  const { resolved, pressure, resting } = useSlots();
  const top = resolved[`${column}.top`] ?? null;
  const bottom = resolved[`${column}.bottom`] ?? null;
  // Under pressure the second column stacks into the first rather than take a
  // column of its own; its panes keep their slots, so they go back on widening.
  const merged = pressure.mergeColumns;
  const bTop = merged && column === "a" ? (resolved["b.top"] ?? null) : null;
  const bBottom = merged && column === "a" ? (resolved["b.bottom"] ?? null) : null;
  const open = merged && column === "b" ? false : columnVisible(resolved, column) || bTop !== null || bBottom !== null;
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
  const reduceMotion = useReducedMotion() ?? false;

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

  // Publish the columns' occupancy so SidebarSurface can clamp against it —
  // before paint, so both clamp against the same frame.
  const setColumnWidth = usePanelWidthsStore((s) => s.setColumnWidth);
  React.useLayoutEffect(() => setColumnWidth(column, drawn), [column, drawn, setColumnWidth]);

  const { isResizing, onPointerDown } = useResizableSize({
    size: width,
    onSizeChange: setWidth,
    min: MIN_COLUMN_WIDTH,
    max,
    axis: "x",
    sign: -1,
  });
  const motion = usePanelMotionReport({
    id: `column-${column}`,
    target: drawn,
    open,
    stored: width,
    instant: isResizing || reduceMotion,
    windowSize: windowWidth,
  });
  const fade = React.useMemo(() => ({ open, instant: motion.instant }), [open, motion.instant]);
  const { inert, shielded } = useClosedInert(open, motion.still);

  // The width the content is laid out at: the open width, held through the close.
  const [lastDrawn, setLastDrawn] = React.useState(drawn);
  if (open && drawn !== lastDrawn) setLastDrawn(drawn);

  // While it closes, the panes it last showed — less any now drawn elsewhere
  // (moved to another slot, or stacked into column A), which are already
  // there — and a hidden companion it held, for as long as it is hidden
  // (retain.restingStep). Beside another pane here that one is stowed: out
  // of the flow and invisible, its card and host where they were
  // (retain.stowStep).
  const live = React.useMemo<ColumnPanes | null>(
    () => (open ? { top, bottom, bTop, bBottom } : null),
    [open, top, bottom, bTop, bBottom],
  );
  const kept = useRetainedWhileClosing(live, panelCloseMs(reduceMotion));
  const leaving = useStillLeaving(live === null ? kept : null, Object.values(resolved));
  const { drawn: shown, stowed: stowedKeys } = useResting<keyof ColumnPanes>(live ?? leaving, resting, Object.values(resolved));
  const stowed = (key: keyof ColumnPanes) => stowedKeys.has(key);
  const extras = (
    [["b.top", shown.bTop ?? null, stowed("bTop")], ["b.bottom", shown.bBottom ?? null, stowed("bBottom")]] as [SlotId, PaneId | null, boolean][]
  ).filter((e): e is [SlotId, PaneId, boolean] => e[1] !== null);
  const shownTop = shown.top ?? null;
  const shownBottom = shown.bottom ?? null;
  const topInFlow = shownTop !== null && !stowed("top");
  const bottomInFlow = shownBottom !== null && !stowed("bottom");

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

  const both = topInFlow && bottomInFlow;
  const sizes = splitColumn({ columnHeight, topOpen: topInFlow, bottomOpen: bottomInFlow, topHeight });
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
    <PanelFade.Provider value={fade}>
      <m.div
        initial={false}
        // Clamped at render for the same reason as the sidebar and the bottom
        // row. One line: step4bComponents.test.tsx looks for it.
        animate={{ width: open ? Math.min(width, max) + GAP_WIDTH : 0 }}
        transition={panelTransition(open, motion.instant)}
        onAnimationComplete={motion.onAnimationComplete}
        // On the clipping box: what a drag measures is what is on screen.
        data-slot-column={column}
        className="relative flex min-h-0 shrink-0 overflow-hidden"
      >
        {/* Pinned to the open width and hung from the left edge, the one that
            moves: the column slides in from the right and back out to it.
            Inert once closed, a shield while it closes (useClosedInert). */}
        <div inert={inert} className="absolute inset-y-0 left-0 flex" style={{ width: open ? drawn : lastDrawn }}>
          {shielded ? <div aria-hidden className="absolute inset-0 z-50" /> : null}
          {open ? (
            <ShellResizeHandle
              orientation="vertical"
              aria-label={`Resize column ${column.toUpperCase()}`}
              onPointerDown={onPointerDown}
              isResizing={isResizing}
              style={{ width: GAP_WIDTH }}
            />
          ) : (
            // The sash's place, so the closing content does not shift into it.
            <div aria-hidden className="shrink-0" style={{ width: GAP_WIDTH }} />
          )}
          {/* relative: a stowed card fills exactly this box. */}
          <div ref={columnRef} className="relative flex min-h-0 min-w-0 flex-1 flex-col">
            {extras.length > 0 ? (
              // Stacked: every pane an equal share, no split to drag — this is a
              // temporary arrangement that the window's width will undo.
              <div className="flex min-h-0 flex-1 flex-col" style={{ gap: GAP_WIDTH }}>
                {(
                  [
                    ...(shownTop ? [[`${column}.top`, shownTop, stowed("top")]] : []),
                    ...(shownBottom ? [[`${column}.bottom`, shownBottom, stowed("bottom")]] : []),
                    ...extras,
                  ] as [SlotId, PaneId, boolean][]
                ).map(([slot, pane, away]) => (
                  <SlotCard key={slot} slot={slot} pane={pane} stowed={away} className="flex-1" />
                ))}
              </div>
            ) : null}
            {extras.length === 0 && shownTop ? (
              <SlotCard
                slot={`${column}.top`}
                pane={shownTop}
                stowed={stowed("top")}
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
            {extras.length === 0 && shownBottom ? (
              <SlotCard
                slot={`${column}.bottom`}
                pane={shownBottom}
                stowed={stowed("bottom")}
                className={both ? "shrink-0" : "flex-1"}
                style={both ? { height: sizes.bottom } : undefined}
              />
            ) : null}
          </div>
        </div>
      </m.div>
    </PanelFade.Provider>
  );
}

/**
 * A slot's card: the pane header, and the body element the pane's content was
 * rendered into, adopted for as long as this slot holds that pane.
 */
function SlotCard({
  slot,
  pane,
  stowed = false,
  className,
  style,
}: {
  slot: SlotId;
  pane: PaneId;
  /**
   * A hidden companion resting beside a pane still open in its column: out
   * of the flow, invisible and inert, at the column's size. Not unmounted —
   * its host stays put (retain.restingStep).
   */
  stowed?: boolean;
  className?: string;
  style?: React.CSSProperties;
}) {
  const { hostFor, release } = useSlots();
  const fade = React.useContext(PanelFade);
  const originRef = React.useRef<HTMLDivElement>(null);
  const cardRef = React.useRef<HTMLDivElement | null>(null);
  const bodyRef = React.useRef<HTMLDivElement>(null);

  React.useLayoutEffect(() => {
    const body = bodyRef.current;
    const host = hostFor(pane);
    if (!body) return;
    // Moved, not just appended: a pane coming back from the park, or from
    // another slot, keeps where it was scrolled to (slots/hostMove.ts).
    moveHost(body, host);
    return () => release(pane, host, body);
  }, [hostFor, release, pane]);

  const setCard = React.useCallback((el: HTMLDivElement | null) => {
    cardRef.current = el;
    return shellSquircleRef(el);
  }, []);

  return (
    <div
      ref={originRef}
      data-slot-card={slot}
      inert={stowed}
      className={cn("pane-slot flex min-h-0 min-w-0 flex-col", stowed ? "invisible absolute inset-0" : cn("relative", className))}
      style={stowed ? undefined : style}
    >
      <div ref={setCard} className="isolate relative flex min-h-0 flex-1 flex-col overflow-hidden shell-surface">
        <PaneHeader pane={pane} cardRef={cardRef} originRef={originRef} />
        <div ref={bodyRef} className="flex min-h-0 flex-1 flex-col" />
        {/* The surface moves with the panel; what is on it fades in after it
            and out with it — drawn as a veil of the surface's own colour
            fading out over it, not as the content fading in. An opacity
            animation on the content makes the engine give the whole pane a
            layer of its own and paint all of it in one frame: on WebKitGTK
            that frame, a few into the open, took 50-70ms on a transcript
            (smoothness §3.8). The veil is a plain fill: nothing to paint. */}
        <m.div
          aria-hidden
          initial={fade.instant ? false : { "--veil": 1 } as never}
          animate={{ "--veil": fade.open ? 0 : 1 } as never}
          transition={panelContentFade(fade.open, fade.instant)}
          className="pointer-events-none absolute inset-0 z-50"
          style={{ background: "color-mix(in srgb, var(--surface-base) calc(var(--veil) * 100%), transparent)" }}
        />
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
