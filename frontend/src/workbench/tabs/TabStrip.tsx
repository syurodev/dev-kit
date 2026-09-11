import { cn } from "@/lib/utils";
import { AnimatePresence, Reorder, motion } from "framer-motion";
import { Plus, Wrench, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { getModule } from "../../modules/registry";
import type { TabStatus, WorkbenchTab } from "../../stores/workbenchTabs";
import { ACTIVE_MIN_WIDTH_PX, computeTabWidths } from "./tabWidths";

function statusDotClass(status: TabStatus): string {
  switch (status) {
    case "running":
      return "bg-(--status-running)";
    case "success":
      return "bg-(--status-success)";
    case "error":
      return "bg-(--status-error)";
    default:
      return "bg-(--status-idle)";
  }
}

type TabStripProps = {
  tabs: WorkbenchTab[];
  activeTabId: string;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onReorder: (fromId: string, beforeId: string | null) => void;
  onNewTab?: () => void;
  /** True while ⌘⇧E/Ctrl⇧E is held (Workbench.tsx): every tab goes full
   * width and ArrowLeft/Right + Enter/Escape navigate the strip. */
  keyboardBrowsing: boolean;
  /** Browsing ended — by Enter, Escape, or the modifier release. The caller
   * resets whatever set keyboardBrowsing. */
  onExitBrowsing: () => void;
};

type TabGroup = {
  moduleId: string;
  tabs: WorkbenchTab[];
};

/**
 * A module's home view opened as the shared preview tab (a Dock click with no
 * item picked yet — Workbench.tsx's focusModule) gets no strip entry: it is
 * not a distinct piece of content, just "you're looking at this module".
 * Rendering it meant every Dock click tore down a whole tab group and mounted
 * a new one, which is what read as jerky. Picking an item opens a different
 * viewId in the same preview slot and shows up normally.
 */
function isHiddenHomePreview(tab: WorkbenchTab): boolean {
  return Boolean(
    tab.preview && getModule(tab.moduleId)?.views[0]?.id === tab.viewId,
  );
}

/**
 * Groups tabs by moduleId. A group sits where its first tab first appeared —
 * not where the active module is — and keeps its tabs in relative order.
 */
function groupTabs(tabs: WorkbenchTab[]): TabGroup[] {
  const groups: TabGroup[] = [];
  const byModule = new Map<string, TabGroup>();
  for (const tab of tabs) {
    let group = byModule.get(tab.moduleId);
    if (!group) {
      group = { moduleId: tab.moduleId, tabs: [] };
      byModule.set(tab.moduleId, group);
      groups.push(group);
    }
    group.tabs.push(tab);
  }
  return groups;
}

// The width model lives in ./tabWidths, where it can be tested without a DOM.

type TabGroupRowProps = {
  tabs: WorkbenchTab[];
  activeTabId: string;
  widths: Map<string, number | undefined>;
  keyboardFocusedTabId: string | null;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onReorder: (fromId: string, beforeId: string | null) => void;
  onDraggingChange: (dragging: boolean) => void;
};

/**
 * One module's tabs as a self-contained Reorder.Group. Dragging reorders
 * within the group only, so a tab can never leave its module's colour cluster.
 *
 * Inside a draggable shell region the row opts out with no-drag: Wails detects
 * drag targets on a capture-phase mousedown, before Motion's pointer gesture,
 * and once it starts the native window move the OS owns the pointer and React
 * cannot recover the tab drag.
 *
 * There is no `layout` prop — position snaps rather than FLIPs, which is what
 * finally settled a drop-stutter. Width is still animated via `animate`, and
 * the close button sits in permanently reserved absolute space so only the
 * deliberate equal-share recompute ever changes a measured width.
 */
function TabGroupRow({
  tabs,
  activeTabId,
  widths,
  keyboardFocusedTabId,
  onSelect,
  onClose,
  onReorder,
  onDraggingChange,
}: TabGroupRowProps) {
  const activeDragId = useRef<string | null>(null);
  const dragOriginBeforeId = useRef<string | null>(null);
  const [visualTabs, setVisualTabs] = useState(tabs);
  const [draggingTabId, setDraggingTabId] = useState<string | null>(null);
  const visualTabsRef = useRef(tabs);
  // A Reorder.Item is also a button, so WebKit fires a click after the
  // pointerup ending a drag. Consuming it stops a reorder from also activating
  // an inactive tab and hydrating a heavy editor on drop.
  const suppressActivationForId = useRef<string | null>(null);
  const suppressionClearTimer = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );

  useEffect(() => {
    // Store order is committed only once on drop. External open/close/status
    // changes still flow into the group immediately while no gesture is active.
    if (activeDragId.current === null) {
      visualTabsRef.current = tabs;
      setVisualTabs(tabs);
    }
  }, [tabs]);

  function handleVisualReorder(nextTabs: WorkbenchTab[]) {
    if (activeDragId.current === null) {
      return;
    }
    visualTabsRef.current = nextTabs;
    setVisualTabs(nextTabs);
  }

  function handleDragStart(draggedId: string) {
    activeDragId.current = draggedId;
    suppressActivationForId.current = draggedId;
    if (suppressionClearTimer.current) {
      clearTimeout(suppressionClearTimer.current);
      suppressionClearTimer.current = null;
    }
    setDraggingTabId(draggedId);
    onDraggingChange(true);
    const index = visualTabsRef.current.findIndex(
      (tab) => tab.id === draggedId,
    );
    dragOriginBeforeId.current = visualTabsRef.current[index + 1]?.id ?? null;
  }

  function handleDragEnd() {
    const draggedId = activeDragId.current;
    setDraggingTabId(null);
    onDraggingChange(false);
    if (draggedId !== null) {
      const index = visualTabsRef.current.findIndex(
        (tab) => tab.id === draggedId,
      );
      const beforeId = visualTabsRef.current[index + 1]?.id ?? null;
      // No commit when the gesture never crossed a neighbour: a jiggle would
      // otherwise produce a new but identical openTabs array. WorkbenchViews
      // ignores pure order changes as a second guard on the mounted tree.
      if (beforeId !== dragOriginBeforeId.current) {
        onReorder(draggedId, beforeId);
      }
    }
    activeDragId.current = null;
    // The native click follows mouseup in the same turn; clearing on the next
    // task keeps a missing click (dropping outside) from suppressing a real one.
    suppressionClearTimer.current = setTimeout(() => {
      suppressActivationForId.current = null;
      suppressionClearTimer.current = null;
    }, 0);
  }

  useEffect(
    () => () => {
      if (suppressionClearTimer.current) {
        clearTimeout(suppressionClearTimer.current);
      }
    },
    [],
  );

  return (
    <Reorder.Group
      as="div"
      axis="x"
      values={visualTabs}
      onReorder={handleVisualReorder}
      className="flex shrink-0 items-center gap-1.5"
    >
      <AnimatePresence initial={false}>
        {visualTabs.map((tab) => {
          const isActive = tab.id === activeTabId;
          const widthPx = widths.get(tab.id);
          return (
            <Reorder.Item
              as="button"
              value={tab}
              key={tab.id}
              // Momentum would keep the item drifting on release while the
              // layout system pulls it back to its slot — two motions competing
              // at drop, which read as the stutter.
              dragMomentum={false}
              // Scaling a text-heavy tab makes WebKit re-rasterise label and
              // icon while Motion projects every sibling. Stacking order is
              // enough feedback and keeps the work to translation.
              whileDrag={{ zIndex: 10 }}
              // opacity/scale belong here, not just width: they are what drives
              // the AnimatePresence enter, and without them a new tab would sit
              // at its `initial` values forever. widthPx is a real number once
              // the container has been measured; "auto" covers the first paint.
              animate={{ opacity: 1, scale: 1, width: widthPx ?? "auto" }}
              // One duration and easing for all four properties. Mismatched
              // speeds made the FLIP footprint land long before the tab had
              // finished growing into it — one motion read as two.
              transition={{
                layout: { duration: 0.22, ease: "easeOut" },
                width: { duration: 0.22, ease: "easeOut" },
                opacity: { duration: 0.22, ease: "easeOut" },
                scale: { duration: 0.22, ease: "easeOut" },
              }}
              style={{
                // Siblings pass under the captured pointer as Motion projects
                // them. Excluding them from hit-testing stops WKWebView holding
                // hover chains for elements no longer under the cursor.
                pointerEvents:
                  draggingTabId !== null && draggingTabId !== tab.id
                    ? "none"
                    : "auto",
              }}
              onDragStart={() => handleDragStart(tab.id)}
              onDragEnd={handleDragEnd}
              // Width belongs here too: without it a new tab's footprint is
              // full-size on the first frame, so neighbours jump aside before
              // the tab exists to fill the gap. Same in reverse on close.
              initial={{ opacity: 0, scale: 0.95, width: 0 }}
              exit={{ opacity: 0, scale: 0.95, width: 0 }}
              type="button"
              data-tab-id={tab.id}
              onClick={(event) => {
                if (suppressActivationForId.current === tab.id) {
                  event.preventDefault();
                  suppressActivationForId.current = null;
                  if (suppressionClearTimer.current) {
                    clearTimeout(suppressionClearTimer.current);
                    suppressionClearTimer.current = null;
                  }
                  return;
                }
                onSelect(tab.id);
              }}
              className={cn(
                "group relative flex h-8 max-w-[200px] shrink-0 items-center rounded-[6px] border px-3 gap-2 transition-[filter,border-color,background-color] select-none [--wails-draggable:no-drag]",
                isActive
                  ? "bg-surface-raised border-border text-text-primary"
                  : tab.preview
                    ? "bg-state-soft border-border-subtle border-dashed text-body-12 text-text-secondary hover:bg-state-soft/80"
                    : "bg-state-soft border-border-subtle border-solid text-label-12 text-text-secondary hover:bg-state-soft/80",
              )}
            >
              {/* Focus ring: separate layer at radius 8, not the pill's radius 6 (Figma 448:343 exception) */}
              {tab.id === keyboardFocusedTabId && (
                <span
                  className="pointer-events-none absolute -inset-[2px] rounded-[8px] border-2 border-[var(--border-focus,#b2d5e5)]"
                  aria-hidden="true"
                />
              )}
              <span className="relative z-10 flex min-w-0 flex-1 items-center gap-2">
                {(() => {
                  const ModuleIcon =
                    tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench;
                  return <ModuleIcon className="size-3.5 shrink-0 opacity-85" />;
                })()}
                <span
                  className={cn(
                    "truncate flex-1 min-w-0",
                    tab.preview ? "text-body-12 font-normal text-text-secondary" : "text-label-12",
                  )}
                >
                  {tab.title}
                </span>
                {/* Active state status dot: 6px */}
                {isActive && tab.status && tab.status !== "idle" ? (
                  <span
                    className={cn(
                      "size-1.5 shrink-0 rounded-full",
                      statusDotClass(tab.status),
                    )}
                  />
                ) : null}
              </span>
              {/* Close button: 12px, shown on Active and Inactive, NOT on Preview */}
              {!tab.preview && (
                <span
                  role="button"
                  tabIndex={0}
                  aria-label={`Close ${tab.title}`}
                  onClick={(event) => {
                    event.stopPropagation();
                    onClose(tab.id);
                  }}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" || event.key === " ") {
                      event.preventDefault();
                      event.stopPropagation();
                      onClose(tab.id);
                    }
                  }}
                  className="z-10 flex size-3 shrink-0 items-center justify-center rounded text-text-secondary hover:text-text-primary transition-colors focus:outline-none [--wails-draggable:no-drag] cursor-pointer"
                >
                  <X className="size-3" />
                </span>
              )}
            </Reorder.Item>
          );
        })}
      </AnimatePresence>
    </Reorder.Group>
  );
}

export function TabStrip({
  tabs,
  activeTabId,
  onSelect,
  onClose,
  onReorder,
  onNewTab,
  keyboardBrowsing,
  onExitBrowsing,
}: TabStripProps) {
  const [keyboardFocusedTabId, setKeyboardFocusedTabId] = useState<
    string | null
  >(keyboardBrowsing ? activeTabId : null);
  // Kept current so the seed effect below reads the latest activeTabId without
  // taking it as a dependency — that would re-seed mid-browse and fight the
  // user's arrow keys.
  const activeTabIdRef = useRef(activeTabId);
  activeTabIdRef.current = activeTabId;
  // Memoised because both effects below depend on it: a new array identity
  // every render would re-run and re-set state forever.
  const visibleTabs = useMemo(
    () => tabs.filter((tab) => !isHiddenHomePreview(tab)),
    [tabs],
  );

  useEffect(() => {
    if (keyboardBrowsing) {
      setKeyboardFocusedTabId(activeTabIdRef.current);
    }
  }, [keyboardBrowsing]);

  useEffect(() => {
    if (!keyboardBrowsing) {
      return;
    }

    function handleBrowseKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        onExitBrowsing();
        return;
      }
      if (event.key === "Enter") {
        event.preventDefault();
        if (keyboardFocusedTabId) {
          onSelect(keyboardFocusedTabId);
        }
        onExitBrowsing();
        return;
      }
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") {
        return;
      }
      event.preventDefault();
      const order = groupTabs(visibleTabs).flatMap((group) => group.tabs);
      const currentIndex = order.findIndex(
        (tab) => tab.id === keyboardFocusedTabId,
      );
      if (currentIndex === -1) {
        return;
      }
      const delta = event.key === "ArrowLeft" ? -1 : 1;
      const nextIndex = Math.min(
        Math.max(currentIndex + delta, 0),
        order.length - 1,
      );
      setKeyboardFocusedTabId(order[nextIndex]?.id ?? null);
    }

    window.addEventListener("keydown", handleBrowseKeyDown);
    return () => window.removeEventListener("keydown", handleBrowseKeyDown);
  }, [
    keyboardBrowsing,
    visibleTabs,
    keyboardFocusedTabId,
    onSelect,
    onExitBrowsing,
  ]);
  // Keyed by moduleId, not a boolean, so two groups reporting out of order
  // cannot clobber each other.
  const [draggingGroupIds, setDraggingGroupIds] = useState<Set<string>>(
    new Set(),
  );
  // Widths snapshotted when a drag starts and reused for the gesture — see the
  // isDragging block below.
  const dragFreezeWidthsRef = useRef<Map<string, number | undefined> | null>(
    null,
  );
  const scrollRef = useRef<HTMLDivElement>(null);
  // The strip's box, not its content — what the equal-share model divides.
  // scrollRef is `w-full`, so its clientWidth is the column's width whether or
  // not the content overflows: no feedback loop with the tab widths it feeds.
  // Only the grid column resizes it, so a plain ResizeObserver suffices.
  const [containerWidth, setContainerWidth] = useState<number | undefined>(
    undefined,
  );

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) {
      return;
    }

    function measure() {
      if (el) {
        setContainerWidth(el.clientWidth);
      }
    }

    measure();
    const resizeObserver = new ResizeObserver(measure);
    resizeObserver.observe(el);
    return () => resizeObserver.disconnect();
  }, []);

  function handleGroupDraggingChange(moduleId: string, dragging: boolean) {
    setDraggingGroupIds((prev) => {
      const next = new Set(prev);
      if (dragging) {
        next.add(moduleId);
      } else {
        next.delete(moduleId);
      }
      return next;
    });
  }

  const groups = groupTabs(visibleTabs);
  const visualOrder = groups.flatMap((group) => group.tabs);

  // The at-rest widths, independent of drag and keyboard browsing: used
  // directly when neither is active, and as the drag-freeze source when one is.
  const computedWidths = computeTabWidths({
    tabIds: visualOrder.map((tab) => tab.id),
    activeTabId,
    containerWidth,
  });

  const isDragging = draggingGroupIds.size > 0;
  if (isDragging && dragFreezeWidthsRef.current === null) {
    // computedWidths still holds the pre-drag values this frame — only the drag
    // flag changed. Holding that snapshot for the gesture is what stops a drag
    // that crosses a sibling and returns from jumping widths on the way.
    dragFreezeWidthsRef.current = new Map(computedWidths);
  } else if (!isDragging && dragFreezeWidthsRef.current !== null) {
    dragFreezeWidthsRef.current = null;
  }

  let widths = computedWidths;
  if (keyboardBrowsing) {
    // ⌘⇧E widens every tab equally for readability while arrow-navigating — an
    // intentional change, not something to freeze against.
    widths = new Map(visualOrder.map((tab) => [tab.id, ACTIVE_MIN_WIDTH_PX]));
  } else if (isDragging && dragFreezeWidthsRef.current) {
    widths = dragFreezeWidthsRef.current;
  }
  // The pill's width, computed from data we already have rather than sized by
  // its children's live flex layout: Reorder's drag changes how the dragged
  // item participates in that flow, which shifted the pill's measured width
  // (and so its centring) even though no tab's width had changed. Gaps are 4px
  // between every tab, group boundaries included — the outer and inner gaps
  // always sum to visualOrder.length - 1.
  const tabWidthValues = visualOrder.map((tab) => widths.get(tab.id));
  const pillWidthPx = tabWidthValues.every((value) => typeof value === "number")
    ? tabWidthValues.reduce(
        (sum: number, value) => sum + (value as number),
        0,
      ) +
      Math.max(0, visualOrder.length - 1) * 4
    : undefined;

  return (
    // Outer: always the strip's full measured width (ref={scrollRef} lives
    // here, not on the inner pill) — that's what the equal-share math above
    // needs to divide, and it has to stay decoupled from how wide the
    // rendered pill actually ends up (now that widths are capped at
    // MAX_TAB_WIDTH_PX, a couple of short tabs render *narrower* than the
    // container again, so measuring the pill itself would create a feedback
    // loop: its width shrinks, `containerWidth` reads too small, tabs get
    // sized as if there's less room than there really is). The pill starts at
    // the left edge: tabs are read left to right and a new one should appear
    // where the eye already is, rather than shifting every existing tab
    // sideways to keep the group centred. Inner: the actual visible, content-sized
    // (max-w-full, not w-full) scrollable pill — grows only as wide as its
    // tabs need, scrolling past that once they don't fit.
    <div
      ref={scrollRef}
      className="flex h-12 min-w-0 w-full items-center justify-start p-2 gap-2 [--wails-draggable:no-drag]"
    >
      <div
        // 449:325: gap-2 between tab items
        className="scrollbar-hide flex max-w-full items-center gap-2 overflow-x-auto"
        style={pillWidthPx !== undefined ? { width: pillWidthPx } : undefined}
      >
        <AnimatePresence initial={false}>
          {groups.map((group) => (
            <motion.div
              key={group.moduleId}
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              // Same 0.22s easeOut as the tabs themselves: a group and its tabs
              // appearing together must land at the same moment.
              transition={{
                layout: { duration: 0.22, ease: "easeOut" },
                opacity: { duration: 0.22, ease: "easeOut" },
                scale: { duration: 0.22, ease: "easeOut" },
              }}
              className="flex shrink-0 items-center"
            >
              <TabGroupRow
                tabs={group.tabs}
                activeTabId={activeTabId}
                widths={widths}
                keyboardFocusedTabId={
                  keyboardBrowsing ? keyboardFocusedTabId : null
                }
                onSelect={onSelect}
                onClose={onClose}
                onReorder={onReorder}
                onDraggingChange={(dragging) =>
                  handleGroupDraggingChange(group.moduleId, dragging)
                }
              />
            </motion.div>
          ))}
        </AnimatePresence>
      </div>
      {/* New-tab button: 28x28, radius 8, plus glyph 13px (Figma 449:325) */}
      <button
        type="button"
        aria-label="New tab"
        onClick={onNewTab}
        className="size-7 shrink-0 rounded-[8px] flex items-center justify-center text-text-secondary hover:text-text-primary hover:bg-state-soft transition-colors cursor-pointer"
      >
        <Plus className="size-[13px]" />
      </button>
      {/* Trailing spacer: a flex-grow 8px-high frame after new-tab (Figma 449:325) */}
      <div className="flex-1 h-2 self-center" aria-hidden="true" />
    </div>
  );
}
