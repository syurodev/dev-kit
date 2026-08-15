import { useEffect, useMemo, useRef, useState } from "react";
import { AnimatePresence, Reorder, motion } from "framer-motion";
import { Wrench, X } from "lucide-react";
import { cn } from "@/lib/utils";
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
  return Boolean(tab.preview && getModule(tab.moduleId)?.views[0]?.id === tab.viewId);
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

const FISHEYE_STEP_PERCENT = 35;
const FISHEYE_FLOOR_PERCENT = 10;
const TAB_BASE_WIDTH_PX = 200;

/** Width for a tab `distance` positions away from the active tab. */
function fisheyeWidthPx(distance: number): number {
  const percent = Math.max(
    FISHEYE_FLOOR_PERCENT,
    100 - distance * FISHEYE_STEP_PERCENT,
  );
  return Math.round((TAB_BASE_WIDTH_PX * percent) / 100);
}

const EDGE_FADE_PX = 20;

/**
 * Only fades the edge(s) that actually have more content scrolled past
 * them — `undefined` (no mask at all) when the row isn't scrolled or
 * doesn't overflow, so a fully-visible tab list never shows a fade with
 * nothing behind it to justify one.
 */
function edgeFadeMask(canScrollLeft: boolean, canScrollRight: boolean): string | undefined {
  if (canScrollLeft && canScrollRight) {
    return `linear-gradient(to right, transparent, black ${EDGE_FADE_PX}px, black calc(100% - ${EDGE_FADE_PX}px), transparent)`;
  }
  if (canScrollLeft) {
    return `linear-gradient(to right, transparent, black ${EDGE_FADE_PX}px)`;
  }
  if (canScrollRight) {
    return `linear-gradient(to right, black calc(100% - ${EDGE_FADE_PX}px), transparent)`;
  }
  return undefined;
}

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
 * Lives inside the 30px titlebar row. Native InvisibleTitleBarHeight is zero;
 * both this row and its header default to no-drag, and Workbench owns
 * a separate native window-drag handle over the empty traffic-light clearance.
 * This is important because Wails detects draggable targets from a capture-
 * phase mousedown before Motion's pointer gesture. Once it invokes the native
 * window move, the OS owns the pointer and React cannot recover the tab drag.
 *
 * `layout="position"` animates a tab's position when a sibling's width
 * changes (fisheye recompute, or a drag) without scale-projecting content.
 * The tab's own `width` is a separate, real size change — animated through
 * `animate={{ width }}` (a direct tween) rather than through `layout`,
 * which only interpolates position/scale via FLIP, not genuine content
 * reflow. The close button is absolutely positioned in permanently
 * reserved space, so hover and drag alone never change a tab's measured
 * width — only the deliberate fisheye/hover-driven width does.
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
  const [hoveredTabId, setHoveredTabId] = useState<string | null>(null);
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
    setHoveredTabId(null);
    onDraggingChange(true);
    const index = visualTabsRef.current.findIndex((tab) => tab.id === draggedId);
    dragOriginBeforeId.current = visualTabsRef.current[index + 1]?.id ?? null;
  }

  function handleDragEnd() {
    const draggedId = activeDragId.current;
    setDraggingTabId(null);
    setHoveredTabId(null);
    onDraggingChange(false);
    if (draggedId !== null) {
      const index = visualTabsRef.current.findIndex((tab) => tab.id === draggedId);
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
      className="flex shrink-0 items-center gap-1"
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
              // A preview tab replacing a different preview tab (e.g.
              // switching modules via the Dock, or clicking through notes)
              // is a completely different `key` — React unmounts the old
              // one and mounts a new one with no visual relationship
              // between them, which read as a jarring pop/jerk especially
              // when replacements happen in quick succession (rapid
              // clicks interrupting each other's still-playing exit/enter).
              // Sharing this layoutId across every preview tab makes
              // Motion treat the old and new one as the same continuous
              // element crossing into its new spot/size, instead of two
              // unrelated mount/unmount events landing at once.
              layoutId={tab.preview ? "preview-tab" : undefined}
              layout="position"
              // Scaling a text-heavy tab forces WebKit to re-rasterise its
              // label and icon while Motion is also projecting every sibling.
              // A raised stacking order is enough feedback here and keeps the
              // compositor work limited to translation.
              whileDrag={{ zIndex: 10 }}
              // opacity/scale here (not just width) are what drive the
              // AnimatePresence enter/exit — dropping them from `animate`
              // would leave a newly-appeared tab stuck at its `initial`
              // opacity/scale forever, since nothing would ever animate it
              // to its resting state. "auto" for the active tab: Motion
              // measures the actual rendered width and tweens toward it,
              // rather than us trying to compute "however long this title
              // needs" ourselves.
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
                // "auto" width alone lets the active tab size to fit its own
                // title — which reads as *smaller* than a neighbor whose
                // fisheye width is a percentage of the fixed base when the
                // active tab's own title happens to be short (e.g. "Kafka"
                // next to a longer neighbor still near 100%). minWidth
                // guarantees the active tab is never narrower than the 100%
                // baseline, while still growing past it for longer titles.
                minWidth: isActive ? TAB_BASE_WIDTH_PX : undefined,
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
              onPointerEnter={() => {
                if (activeDragId.current === null) {
                  setHoveredTabId(tab.id);
                }
              }}
              onPointerMove={() => {
                if (activeDragId.current === null && hoveredTabId !== tab.id) {
                  setHoveredTabId(tab.id);
                }
              }}
              onPointerLeave={() => {
                setHoveredTabId((current) =>
                  current === tab.id ? null : current,
                );
              }}
              onClick={() => onSelect(tab.id)}
              className={cn(
                // overflow-hidden: at the fisheye floor (30% of 140px =
                // 42px) there isn't room for icon + reserved close-button
                // space + any title at once. Content clips rather than
                // spilling out of the tab's bounds. ponytail: the icon can
                // get visually tight at the exact floor width if
                // TAB_BASE_WIDTH_PX stays this small — bump the base width
                // up if that reads as broken rather than "very narrow" in
                // practice.
                // Dropping the per-module color wash left inactive tabs
                // with nothing at all marking their bounds — invisible
                // against the transparent titlebar whenever no tab in the
                // strip happens to be the active one (e.g. Dock-browsing a
                // module whose home view has no strip entry, see
                // isHiddenHomePreview). A theme-aware border keeps every
                // tab reading as a distinct chip on its own, independent of
                // the active pill.
                "group relative flex h-6 shrink-0 items-center overflow-hidden rounded-md border border-border/50 py-0 pl-2 pr-7 text-xs font-medium transition-[filter,box-shadow] [--wails-draggable:no-drag] backdrop-blur-sm",
                !isActive && hoveredTabId === tab.id && draggingTabId === null
                  ? "brightness-125"
                  : "",
                isActive ? "text-foreground" : "text-muted-foreground",
                // The ⌘⇧E keyboard-browsing cursor — separate from isActive,
                // since Enter hasn't committed it as the active tab yet.
                tab.id === keyboardFocusedTabId ? "ring-2 ring-foreground/80" : "",
              )}
            >
              {isActive ? (
                // The Safari-compact-tab "pill": a single frosted highlight
                // shared (via layoutId) across whichever tab is currently
                // active, anywhere in the strip — including a different
                // module's group entirely. Motion doesn't diff old vs new
                // parent, only the old and new screen rect, so switching
                // tabs reads as one glass capsule sliding and resizing to
                // its new home rather than one tab's highlight fading out
                // while an unrelated one fades in.
                <motion.div
                  layoutId="tab-active-glass"
                  transition={{ type: "spring", stiffness: 460, damping: 32 }}
                  className="absolute inset-0 rounded-md border border-white/15 bg-white/12 shadow-[0_1px_3px_rgba(0,0,0,0.18),inset_0_1px_0_rgba(255,255,255,0.3)] backdrop-blur-md"
                />
              ) : null}
              <span className="relative z-10 flex min-w-0 flex-1 items-center">
                {(() => {
                  // A tab-level `icon` (set by whoever called openTab) wins
                  // over the module's self-registered default
                  // (modules/registry.ts ModuleManifest.icon) — no separate
                  // tab-strip-only icon map to drift either way.
                  const ModuleIcon =
                    tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench;
                  return <ModuleIcon className="size-3.5 shrink-0 opacity-70" />;
                })()}
                {tab.status && tab.status !== "idle" ? (
                  <span
                    className={cn(
                      "ml-1.5 size-1.5 shrink-0 rounded-full",
                      statusDotClass(tab.status),
                    )}
                  />
                ) : null}
                <span
                  className={cn("ml-1.5 truncate", tab.preview && "italic")}
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
                className={cn(
                  "absolute right-1 top-1/2 z-10 flex size-4 -translate-y-1/2 scale-75 items-center justify-center rounded opacity-0 transition-[opacity,transform] duration-150 ease-out hover:bg-foreground/10 focus:scale-100 focus:opacity-100 [--wails-draggable:no-drag]",
                  hoveredTabId === tab.id &&
                    draggingTabId === null &&
                    "scale-100 opacity-100",
                )}
              >
                <X className="size-3" />
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
  const [isBarHovered, setIsBarHovered] = useState(false);
  const [keyboardFocusedTabId, setKeyboardFocusedTabId] = useState<string | null>(null);
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
      const currentIndex = order.findIndex((tab) => tab.id === keyboardFocusedTabId);
      if (currentIndex === -1) {
        return;
      }
      const delta = event.key === "ArrowLeft" ? -1 : 1;
      const nextIndex = Math.min(Math.max(currentIndex + delta, 0), order.length - 1);
      setKeyboardFocusedTabId(order[nextIndex]?.id ?? null);
    }

    window.addEventListener("keydown", handleBrowseKeyDown);
    return () => window.removeEventListener("keydown", handleBrowseKeyDown);
  }, [keyboardBrowsing, visibleTabs, keyboardFocusedTabId, onSelect, onExitBrowsing]);
  // Keyed by moduleId rather than a single boolean so two groups reporting
  // out of order (shouldn't happen with one pointer, but costs nothing to
  // be exact about) can't clobber each other's state.
  const [draggingGroupIds, setDraggingGroupIds] = useState<Set<string>>(new Set());
  const scrollRef = useRef<HTMLDivElement>(null);
  const [edgeFade, setEdgeFade] = useState({ left: false, right: false });

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) {
      return;
    }

    function updateEdgeFade() {
      if (!el) {
        return;
      }
      const maxScrollLeft = el.scrollWidth - el.clientWidth;
      setEdgeFade({
        left: el.scrollLeft > 1,
        right: el.scrollLeft < maxScrollLeft - 1,
      });
    }

    updateEdgeFade();
    // A fisheye width tween (activeTabId change) can still be resizing the
    // row's scrollWidth after this effect runs — ResizeObserver only fires
    // for the *observed* element's own box, which stays capped at
    // max-w-full while overflowing, not for its unclipped content growing
    // inside it. Re-check once after the width transition (0.32s below)
    // has had time to settle.
    const settleTimer = setTimeout(updateEdgeFade, 350);

    el.addEventListener("scroll", updateEdgeFade, { passive: true });
    const resizeObserver = new ResizeObserver(updateEdgeFade);
    resizeObserver.observe(el);

    return () => {
      clearTimeout(settleTimer);
      el.removeEventListener("scroll", updateEdgeFade);
      resizeObserver.disconnect();
    };
  }, [visibleTabs, activeTabId]);

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

  const edgeMask = edgeFadeMask(edgeFade.left, edgeFade.right);
  const edgeMaskStyle = edgeMask
    ? { WebkitMaskImage: edgeMask, maskImage: edgeMask }
    : undefined;

  const groups = groupTabs(visibleTabs);
  // Distance is measured over this clustered order (not raw `tabs`
  // order) because "close to the active tab" means spatial left-right
  // adjacency in what's actually rendered, and clustering can reorder
  // that relative to openTabs' own (drag-reorderable) order.
  const visualOrder = groups.flatMap((group) => group.tabs);
  const activeIndex = visualOrder.findIndex((tab) => tab.id === activeTabId);
  // A drag in progress also suspends the fisheye, same as hovering —
  // tracked through the actual drag lifecycle (not pointer position) so a
  // drag that carries the pointer outside the strip's ~30px-tall bounds
  // (very easy given how thin it is) can't cause every tab's width to
  // snap back to fisheye percentages mid-gesture, underneath the cursor.
  // Keyboard browsing (⌘⇧E) suspends it too — the whole point is seeing
  // every tab at a readable width while arrow-navigating.
  const suspendFisheye = isBarHovered || draggingGroupIds.size > 0 || keyboardBrowsing;

  const widths = new Map<string, number | undefined>();
  visualOrder.forEach((tab, index) => {
    if (suspendFisheye) {
      // Hovering the strip (or dragging within it) suspends the fisheye
      // for every tab, including the active one — "toàn bộ tab bung ra
      // cùng kích thước": same fixed width for all, not each tab's own
      // natural width.
      widths.set(tab.id, TAB_BASE_WIDTH_PX);
      return;
    }
    if (tab.id === activeTabId) {
      // The active tab is never width-constrained at rest — it always
      // shows its full title, however long. Confirmed explicitly: this
      // isn't a Dock-style uniform scale-down for everything else relative
      // to one fixed size.
      widths.set(tab.id, undefined);
      return;
    }
    widths.set(tab.id, fisheyeWidthPx(Math.abs(index - activeIndex)));
  });

  return (
    // Outer: centers the tab row when it's narrower than the available
    // track (few tabs open) via justify-center. Inner: the actual
    // scrollable row, capped at max-w-full so once its content is wider
    // than the track it fills the whole width instead of trying to grow
    // past it — at that point justify-center has no room left to do
    // anything, and the inner element's own overflow-x-auto takes over
    // with normal left-anchored scrolling. (A flex row with
    // justify-center and no such cap would make the start of an
    // overflowing tab list unreachable by scroll — this two-level
    // structure is what avoids that.)
    <div
      onPointerEnter={() => setIsBarHovered(true)}
      onPointerLeave={() => setIsBarHovered(false)}
      className="flex min-w-0 justify-center [--wails-draggable:no-drag]"
    >
      <div
        ref={scrollRef}
        className="scrollbar-hide flex max-w-full items-center gap-1 overflow-x-auto"
        // Soft-fades only the edge(s) that actually have a tab scrolled
        // half out of view (tracked via scroll position + ResizeObserver
        // above) instead of a hard clip — and no mask at all when the row
        // isn't scrolled, so a fully-visible tab list never gets a fade
        // with nothing behind it to justify one. The -webkit- prefix
        // matters here specifically, not just as a fallback: this app
        // renders inside WKWebView, where unprefixed `mask-image` support
        // depends on OS version.
        style={edgeMaskStyle}
      >
        <AnimatePresence initial={false}>
          {groups.map((group) => (
            <motion.div
              key={group.moduleId}
              layout="position"
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
                keyboardFocusedTabId={keyboardBrowsing ? keyboardFocusedTabId : null}
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
