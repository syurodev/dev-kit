import { cn } from "@/lib/utils";
import { AnimatePresence, Reorder, motion } from "framer-motion";
import { Wrench, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { getModule } from "../../modules/registry";
import type { TabStatus, WorkbenchTab } from "../../stores/workbenchTabs";

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
  /** True while the ⌘⇧E/Ctrl⇧E quick-switch shortcut is held (see
   * Workbench.tsx) — forces every tab to full width (same as hovering)
   * and turns on ArrowLeft/ArrowRight + Enter/Escape navigation. */
  keyboardBrowsing: boolean;
  /** Called once browsing ends, however it ended (Enter, Escape, or the
   * modifier keys being released in Workbench.tsx) — the caller resets
   * whatever triggered keyboardBrowsing in the first place. */
  onExitBrowsing: () => void;
};

type TabGroup = {
  moduleId: string;
  tabs: WorkbenchTab[];
};

/**
 * Groups tabs by moduleId, preserving each group's left-to-right position
 * as the position its first tab first appeared at (not disturbed by which
 * module is currently active) and each group's own tabs in their existing
 * relative order.
 */
/**
 * A module's home view opened as the shared preview tab (Dock click, no
 * specific item picked yet — see Workbench.tsx's focusModule) never gets a
 * strip entry: it isn't a distinct piece of content to track, just "you're
 * looking at this module." Rendering it anyway meant every Dock click tore
 * down one module's whole tab group and mounted a new one, which is what
 * actually read as jerky — not just the individual tab's own transition.
 * Picking a specific item (e.g. a note) opens a different viewId in the
 * same preview slot and becomes visible normally.
 */
function isHiddenHomePreview(tab: WorkbenchTab): boolean {
  return Boolean(
    tab.preview && getModule(tab.moduleId)?.views[0]?.id === tab.viewId,
  );
}

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

// Width model: split the strip's actual measured width evenly across every
// tab, capped at MAX_TAB_WIDTH_PX — one or two short-titled tabs shouldn't
// stretch into one giant mostly-empty pill just because the strip has the
// room. Opening more tabs (past what fits at the cap) shrinks everyone's
// equal share together. Once that equal share would push the active tab
// below its own floor, the active tab locks at ACTIVE_MIN_WIDTH_PX instead
// and only the *other* tabs keep splitting whatever's left — down to their
// own, smaller floor (INACTIVE_MIN_WIDTH_PX, just enough for the icon and
// the reserved close-button space), past which the strip scrolls
// horizontally instead of shrinking further.
const MAX_TAB_WIDTH_PX = 180;
// Kept equal to MAX_TAB_WIDTH_PX: if the active tab's floor were ever above
// the general cap, a roomy strip would render it *narrower* than a cramped
// one (capped by MAX_TAB_WIDTH_PX in the roomy branch, locked at its own
// higher floor only once cramped) — backwards from what "floor" should mean.
const ACTIVE_MIN_WIDTH_PX = MAX_TAB_WIDTH_PX;
// Floor for an inactive tab: pl-3 (12px) + icon (16px) + pr-8 (32px reserved
// for the close button) sums to 60px at rest — 52px still leaves the icon
// fully visible even at the exact floor, just tighter than at rest.
const INACTIVE_MIN_WIDTH_PX = 52;

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
 * Renders one module's tabs as a self-contained draggable Reorder.Group,
 * scoped to just this group's tabs — dragging a tab only ever reorders it
 * among its own group's siblings, never across groups, so a tab can never
 * leave its module's color cluster.
 *
 * Lives inside the titlebar row. Native InvisibleTitleBarHeight is zero;
 * both this row and its header default to no-drag, and Workbench owns
 * a separate native window-drag handle over the empty traffic-light clearance.
 * This is important because Wails detects draggable targets from a capture-
 * phase mousedown before Motion's pointer gesture. Once it invokes the native
 * window move, the OS owns the pointer and React cannot recover the tab drag.
 *
 * `layout="position"` used to live on both the tab and its group wrapper,
 * animating position via FLIP whenever a sibling's width changed (an
 * equal-share recompute, or a drag) instead of snapping instantly. Removed
 * as a diagnostic step for a drop-stutter that survived every other fix —
 * see the inline comments at each removal site. The tab's own `width` is
 * still a separate, real size change, animated through `animate={{ width }}`
 * (a direct tween). The close button is absolutely positioned in permanently
 * reserved space, so hover and drag alone never change a tab's measured
 * width — only the deliberate equal-share width does.
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
      // Skip the store commit when the gesture never actually crossed a
      // neighbor — otherwise every accidental sub-threshold jiggle produces
      // a new (but content-identical) openTabs array, which defeats
      // WorkbenchViews' React.memo shallow prop check and re-renders the
      // whole mounted content tree for zero visible change.
      if (beforeId !== dragOriginBeforeId.current) {
        onReorder(draggedId, beforeId);
      }
    }
    activeDragId.current = null;
  }

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
              // layout="position" removed (was here): dropping it as a test
              // for a reported drop-stutter that survived every other
              // targeted fix (frozen widths, pinned pill width, disabled
              // drag momentum, removing the active-highlight's own
              // layoutId) — this was the last remaining Motion layer
              // actively tracking every tab's position on every render,
              // including throughout a drag/reorder. Without it, a sibling
              // whose width or list position changes will snap instead of
              // sliding smoothly — an explicit tradeoff to isolate whether
              // this specific system is the source, not a settled decision
              // to keep it removed either way.
              // Framer's drag defaults to momentum: on release, the item
              // keeps drifting along its velocity for a moment before the
              // layout system pulls it back to its actual slot — two
              // motions competing right at drop, which read as a stutter
              // exactly when the tab was supposed to be smoothly settling
              // into place. Disabling it means release stops the item dead
              // where the pointer left it, so the layout transition is the
              // only motion happening from that point on.
              dragMomentum={false}
              // Scaling a text-heavy tab forces WebKit to re-rasterise its
              // label and icon while Motion is also projecting every sibling.
              // A raised stacking order is enough feedback here and keeps the
              // compositor work limited to translation.
              whileDrag={{ zIndex: 10 }}
              // opacity/scale here (not just width) are what drive the
              // AnimatePresence enter/exit — dropping them from `animate`
              // would leave a newly-appeared tab stuck at its `initial`
              // opacity/scale forever, since nothing would ever animate it
              // to its resting state. widthPx is always a real number once
              // TabStrip has measured its own container at least once;
              // "auto" is only a fallback for the very first paint before
              // that measurement lands.
              animate={{ opacity: 1, scale: 1, width: widthPx ?? "auto" }}
              // One shared duration/easing across layout, width, opacity and
              // scale — these used to run at mismatched speeds (layout
              // 45ms vs. width 260ms vs. opacity/scale 180ms), so a new or
              // closing tab's footprint (layout FLIP) snapped into its
              // final position almost instantly while the tab itself was
              // still visibly growing/shrinking and fading into or out of
              // that already-settled gap — two motions landing at different
              // times read as a jerky double-step rather than one motion.
              transition={{
                layout: { duration: 0.22, ease: "easeOut" },
                width: { duration: 0.22, ease: "easeOut" },
                opacity: { duration: 0.22, ease: "easeOut" },
                scale: { duration: 0.22, ease: "easeOut" },
              }}
              style={{
                // Siblings can visually pass under the captured pointer while
                // Motion projects them into a new slot. Excluding them from
                // hit-testing prevents WKWebView from retaining multiple hover
                // chains for elements that are no longer under the cursor.
                pointerEvents:
                  draggingTabId !== null && draggingTabId !== tab.id
                    ? "none"
                    : "auto",
              }}
              onDragStart={() => handleDragStart(tab.id)}
              onDragEnd={handleDragEnd}
              // Width included here too (not just opacity/scale): without
              // it, a new tab's DOM footprint (and the FLIP-computed gap
              // its neighbors slide to fill) was already full-size from the
              // very first frame — only the fade/scale was gradual — so
              // neighbors visibly jumped aside before the new tab existed
              // to fill that space. Same idea in reverse on close.
              initial={{ opacity: 0, scale: 0.95, width: 0 }}
              exit={{ opacity: 0, scale: 0.95, width: 0 }}
              type="button"
              data-tab-id={tab.id}
              onClick={() => onSelect(tab.id)}
              className={cn(
                // overflow-hidden: at INACTIVE_MIN_WIDTH_PX there isn't
                // room for icon + reserved close-button space + any title
                // at once — content clips down to just the icon rather than
                // spilling out of the tab's bounds. bg-foreground/5 on
                // inactive tabs: the strip's own pill container border
                // alone read as too faint to tell individual tabs apart at
                // a glance.
                "group relative flex h-8 shrink-0 items-center overflow-hidden rounded-full py-0 pl-3 pr-8 text-sm font-medium transition-[filter] [--wails-draggable:no-drag]",
                !isActive && "bg-foreground/5",
                // Plain CSS :hover (not JS-tracked hoveredTabId state) —
                // hovering one tab used to re-render every sibling in this
                // group (they all read the same hoveredTabId), which
                // spuriously re-ran each one's `layout` FLIP measurement
                // and read as a slight left-right jitter across the whole
                // row. pointerEvents below already disables hit-testing on
                // siblings during a drag, so :hover can't fire on them
                // then either — no JS gating needed for that case.
                !isActive && "hover:brightness-125",
                isActive ? "text-foreground" : "text-muted-foreground",
                // The ⌘⇧E keyboard-browsing cursor — separate from isActive,
                // since Enter hasn't committed it as the active tab yet.
                tab.id === keyboardFocusedTabId
                  ? "ring-2 ring-foreground/80"
                  : "",
              )}
            >
              {/* Plain CSS transition, not a shared layoutId Motion node —
                  the layoutId version ran its own FLIP+spring computation
                  on every render where this tab's position could differ,
                  including mid-drag/reorder (its own layout="position"
                  FLIP was already running then too), two separate Motion
                  systems tracking the same tab concurrently. Reported
                  stutter was specifically on drop, exactly where both
                  would be settling at once — removing this one entirely
                  in favor of a plain background-color transition trades
                  the old "slides between tabs when switching active"
                  effect for a same-spot fade, one fewer animated layer
                  for the reorder/drop path to fight with. */}
              <div
                className={cn(
                  "absolute inset-0 rounded-full bg-primary/20 transition-opacity duration-150 ease-out",
                  isActive ? "opacity-100" : "opacity-0",
                )}
              />
              <span className="relative z-10 flex min-w-0 flex-1 items-center">
                {(() => {
                  // A tab-level `icon` (set by whoever called openTab) wins
                  // over the module's self-registered default
                  // (modules/registry.ts ModuleManifest.icon) — no separate
                  // tab-strip-only icon map to drift either way.
                  const ModuleIcon =
                    tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench;
                  return <ModuleIcon className="size-4 shrink-0 opacity-85" />;
                })()}
                {tab.status && tab.status !== "idle" ? (
                  <span
                    className={cn(
                      "ml-2 size-1.5 shrink-0 rounded-full",
                      statusDotClass(tab.status),
                    )}
                  />
                ) : null}
                <span
                  className={cn(
                    "ml-2 truncate",
                    // pr-0.5: italic glyphs lean past their own box, and
                    // truncate's overflow-hidden clips right at this
                    // element's edge — without the extra room, the last
                    // character's slanted stroke reads as slightly cut off.
                    tab.preview && "pr-0.5 italic",
                  )}
                >
                  {tab.title}
                </span>
              </span>
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
                className="absolute right-1.5 top-1/2 z-10 flex size-5 -translate-y-1/2 scale-75 items-center justify-center rounded opacity-0 transition-[opacity,transform] duration-150 ease-out hover:bg-foreground/10 focus:scale-100 focus:opacity-100 group-hover:scale-100 group-hover:opacity-100 [--wails-draggable:no-drag]"
              >
                <X className="size-3.5" />
              </span>
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
  keyboardBrowsing,
  onExitBrowsing,
}: TabStripProps) {
  const [keyboardFocusedTabId, setKeyboardFocusedTabId] = useState<
    string | null
  >(null);
  // Kept current every render so the "seed on browsing start" effect below
  // can read the latest activeTabId without needing it in its own
  // dependency array (which would re-seed on every activeTabId change
  // while already browsing, fighting the user's own arrow-key moves).
  const activeTabIdRef = useRef(activeTabId);
  activeTabIdRef.current = activeTabId;
  // Memoized (not a plain .filter() every render): both effects below use
  // this as a dependency, and a freshly-identitied array on every render
  // would make them re-run — and re-set state — on every render forever,
  // regardless of whether `tabs` actually changed.
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
  // Keyed by moduleId rather than a single boolean so two groups reporting
  // out of order (shouldn't happen with one pointer, but costs nothing to
  // be exact about) can't clobber each other's state.
  const [draggingGroupIds, setDraggingGroupIds] = useState<Set<string>>(
    new Set(),
  );
  // Snapshot of every tab's width taken the instant a drag starts, reused
  // for the whole gesture instead of recomputing — see the isDragging block
  // below for why.
  const dragFreezeWidthsRef = useRef<Map<string, number | undefined> | null>(
    null,
  );
  const scrollRef = useRef<HTMLDivElement>(null);
  // The strip's own available width (its box, not its content) — this is
  // what the equal-share width model below divides among tabs. Reliable
  // because scrollRef is styled `w-full`: its clientWidth is always the
  // column's full width regardless of whether its content over- or
  // under-fills it, so measuring it can't create a feedback loop with the
  // very tab widths it's used to compute. Only the container's own box can
  // resize it (the grid column, e.g. on window resize) — tab width tweens
  // don't, so a plain ResizeObserver is enough with no settle timer needed.
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
  const hasVisibleActive = visualOrder.some((tab) => tab.id === activeTabId);

  // The at-rest equal-share/cramped computation, independent of drag or
  // keyboard-browsing state — used directly when neither is active, and as
  // the drag-freeze snapshot's source (see below) while a drag is.
  const computedWidths = new Map<string, number | undefined>();
  const equalShare =
    containerWidth !== undefined && visualOrder.length > 0
      ? containerWidth / visualOrder.length
      : undefined;
  if (equalShare === undefined) {
    // First paint, before the container has been measured even once —
    // "auto" (via widthPx ?? "auto" below) until the real share is known.
    visualOrder.forEach((tab) => computedWidths.set(tab.id, undefined));
  } else if (!hasVisibleActive || equalShare >= ACTIVE_MIN_WIDTH_PX) {
    // Room for everyone at (at least) the active tab's floor — including
    // when nothing in the strip is actually active (Dock-browsing a hidden
    // home preview, see isHiddenHomePreview), where there's no one tab to
    // prioritize a width for in the first place.
    const share = Math.min(
      MAX_TAB_WIDTH_PX,
      Math.max(equalShare, INACTIVE_MIN_WIDTH_PX),
    );
    visualOrder.forEach((tab) => computedWidths.set(tab.id, share));
  } else if (containerWidth !== undefined) {
    // Cramped: the active tab locks at its floor, and every other tab
    // splits whatever's left — shrinking further as more open, down to
    // their own smaller floor, past which the strip scrolls instead
    // (overflow-x-auto below) rather than shrinking anyone further.
    const remaining = containerWidth - ACTIVE_MIN_WIDTH_PX;
    const inactiveCount = visualOrder.length - 1;
    const inactiveShare =
      inactiveCount > 0
        ? Math.max(INACTIVE_MIN_WIDTH_PX, remaining / inactiveCount)
        : undefined;
    visualOrder.forEach((tab) => {
      computedWidths.set(
        tab.id,
        tab.id === activeTabId ? ACTIVE_MIN_WIDTH_PX : inactiveShare,
      );
    });
  }

  const isDragging = draggingGroupIds.size > 0;
  if (isDragging && dragFreezeWidthsRef.current === null) {
    // Snapshot the instant a drag starts (this render's computedWidths is
    // still the pre-drag values — only the drag flag changed this frame,
    // nothing feeding computedWidths itself has). Reusing this snapshot for
    // the whole gesture, rather than forcing every tab to one shared width,
    // is what keeps a drag that crosses a sibling and comes back without
    // dropping from visibly jumping widths on the way in and back out —
    // cramped tabs (an uneven active/inactive split) used to visibly snap
    // to a uniform width the instant any drag started, then snap back on
    // release, even when the gesture never actually committed a reorder.
    dragFreezeWidthsRef.current = new Map(computedWidths);
  } else if (!isDragging && dragFreezeWidthsRef.current !== null) {
    dragFreezeWidthsRef.current = null;
  }

  let widths = computedWidths;
  if (keyboardBrowsing) {
    // Keyboard-browsing (⌘⇧E) deliberately widens every tab to the same
    // comfortable width for readability while arrow-navigating — a real
    // intentional change, not something to freeze against.
    widths = new Map(visualOrder.map((tab) => [tab.id, ACTIVE_MIN_WIDTH_PX]));
  } else if (isDragging && dragFreezeWidthsRef.current) {
    widths = dragFreezeWidthsRef.current;
  }
  // The pill's own rendered width, computed directly from data we already
  // have instead of letting it size itself from the live flex layout of its
  // children. Framer Motion's Reorder drag appears to change how the
  // dragged item participates in that flex flow while a drag is active
  // (its sibling(s)' widths never changed per the logging above, but the
  // *pill's own* measured width still visibly shifted, recentering it via
  // justify-center on the outer wrapper — the border looked like it was
  // moving even though no tab's width was). Pinning the pill to a width we
  // computed ourselves sidesteps whatever Framer is doing internally,
  // rather than chasing it. gap-1.5 (6px) between every tab regardless of
  // group boundaries (verified: outer gaps between groups + each group's
  // own internal gaps always sum to visualOrder.length - 1 total gaps), +
  // p-1 (4px each side) + the pill's own 1px border each side.
  const tabWidthValues = visualOrder.map((tab) => widths.get(tab.id));
  const pillWidthPx = tabWidthValues.every((value) => typeof value === "number")
    ? tabWidthValues.reduce((sum: number, value) => sum + (value as number), 0) +
      Math.max(0, visualOrder.length - 1) * 6 +
      8 +
      2
    : undefined;

  return (
    // Outer: always the strip's full measured width (ref={scrollRef} lives
    // here, not on the inner pill) — that's what the equal-share math above
    // needs to divide, and it has to stay decoupled from how wide the
    // rendered pill actually ends up (now that widths are capped at
    // MAX_TAB_WIDTH_PX, a couple of short tabs render *narrower* than the
    // container again, so measuring the pill itself would create a feedback
    // loop: its width shrinks, `containerWidth` reads too small, tabs get
    // sized as if there's less room than there really is). justify-center
    // balances that narrower pill inside the available width instead of
    // pinning it to the left edge. Inner: the actual visible, content-sized
    // (max-w-full, not w-full) scrollable pill — grows only as wide as its
    // tabs need, scrolling past that once they don't fit.
    <div
      ref={scrollRef}
      className="flex min-w-0 w-full justify-center [--wails-draggable:no-drag]"
    >
      <div
        // Only drawn when there's at least one visible tab to hold —
        // otherwise (e.g. Dock-browsing a module whose home view has no
        // strip entry, see isHiddenHomePreview) it rendered as an empty
        // capsule with nothing inside it.
        className={cn(
          "scrollbar-hide flex max-w-full items-center gap-1.5 overflow-x-auto",
          // p-1 (not px-1.5/py-1): the horizontal and vertical gap between
          // this outer border and the inner tab has to match, or the ring
          // reads as uneven — most visible around the rounded end caps,
          // where an unequal x/y offset breaks the two pills' concentricity.
          visualOrder.length > 0 && "rounded-full border border-primary/20 p-1",
        )}
        style={pillWidthPx !== undefined ? { width: pillWidthPx } : undefined}
      >
        <AnimatePresence initial={false}>
          {groups.map((group) => (
            <motion.div
              key={group.moduleId}
              // layout="position" removed here too, same test as the tab
              // level above — see that comment.
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              // Matches the individual tab's own layout/width/opacity/scale
              // timing below exactly (0.22s easeOut across the board) — a
              // group appearing/disappearing and its tab(s) doing the same
              // needs to land at the same moment, not slightly staggered.
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
    </div>
  );
}
