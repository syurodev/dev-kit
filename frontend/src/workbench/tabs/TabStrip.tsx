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

// Width model: split the strip's actual measured width evenly across every
// tab. One tab fills the whole strip; opening more shrinks everyone's equal
// share together. Once that equal share would push the active tab below its
// own floor, the active tab locks at ACTIVE_MIN_WIDTH_PX instead and only
// the *other* tabs keep splitting whatever's left — down to their own,
// smaller floor (INACTIVE_MIN_WIDTH_PX, just enough for the icon and the
// reserved close-button space), past which the strip scrolls horizontally
// instead of shrinking further.
const ACTIVE_MIN_WIDTH_PX = 200;
const INACTIVE_MIN_WIDTH_PX = 44;

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
 * changes (an equal-share recompute, or a drag) without scale-projecting
 * content. The tab's own `width` is a separate, real size change — animated through
 * `animate={{ width }}` (a direct tween) rather than through `layout`,
 * which only interpolates position/scale via FLIP, not genuine content
 * reflow. The close button is absolutely positioned in permanently
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
              // A shared layoutId across every preview tab used to live
              // here, so a preview replacing a different preview (e.g.
              // clicking through notes) read as one continuous element
              // instead of an unrelated exit+enter. Removed: it was
              // fighting the active-tab pill's own layoutId (below) — both
              // trying to morph a box's size/position on the same swap,
              // at the same time, produced a briefly oversized/overlapping
              // tab instead of a clean transition. The matched enter/exit
              // timing plus the width-collapse-to-0 fix already makes a
              // plain exit+enter read as one motion without this.
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
                // overflow-hidden: at INACTIVE_MIN_WIDTH_PX there isn't
                // room for icon + reserved close-button space + any title
                // at once — content clips down to just the icon rather than
                // spilling out of the tab's bounds.
                // Dropping the per-module color wash left inactive tabs
                // with nothing at all marking their bounds — invisible
                // against the transparent titlebar whenever no tab in the
                // strip happens to be the active one (e.g. Dock-browsing a
                // module whose home view has no strip entry, see
                // isHiddenHomePreview). A border alone still read as too
                // faint against the native vibrancy blur behind it
                // (tinted, not a flat surface) — bg-foreground/5 gives
                // every tab an actual neutral surface under its text/icon,
                // not just an outline, in either theme.
                "group relative flex h-6 shrink-0 items-center overflow-hidden rounded-md border border-border/60 bg-foreground/5 py-0 pl-2 pr-7 text-xs font-medium transition-[filter,box-shadow] [--wails-draggable:no-drag] backdrop-blur-sm",
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
                  return <ModuleIcon className="size-3.5 shrink-0 opacity-85" />;
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
  // The strip's own available width (its box, not its content) — this is
  // what the equal-share width model below divides among tabs. Reliable
  // because scrollRef is styled `w-full`: its clientWidth is always the
  // column's full width regardless of whether its content over- or
  // under-fills it, so measuring it can't create a feedback loop with the
  // very tab widths it's used to compute.
  const [containerWidth, setContainerWidth] = useState<number | undefined>(undefined);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) {
      return;
    }

    function measure() {
      if (!el) {
        return;
      }
      setContainerWidth(el.clientWidth);
      const maxScrollLeft = el.scrollWidth - el.clientWidth;
      setEdgeFade({
        left: el.scrollLeft > 1,
        right: el.scrollLeft < maxScrollLeft - 1,
      });
    }

    measure();
    // A width tween (activeTabId change, or new tabs changing the equal
    // share) can still be resizing the row's scrollWidth after this effect
    // runs — ResizeObserver only fires for the *observed* element's own
    // box, not for its unclipped content growing inside it. Re-check once
    // after the width transition (0.22s) has had time to settle.
    const settleTimer = setTimeout(measure, 350);

    el.addEventListener("scroll", measure, { passive: true });
    const resizeObserver = new ResizeObserver(measure);
    resizeObserver.observe(el);

    return () => {
      clearTimeout(settleTimer);
      el.removeEventListener("scroll", measure);
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
  const visualOrder = groups.flatMap((group) => group.tabs);
  // A drag in progress, hovering, or keyboard-browsing all force every tab
  // to the same comfortable width regardless of the equal-share math below
  // — tracked through the actual drag lifecycle (not pointer position) so a
  // drag that carries the pointer outside the strip's ~30px-tall bounds
  // can't cause widths to snap back mid-gesture, underneath the cursor. The
  // whole point of hovering/keyboard-browsing is seeing every tab at a
  // readable width regardless of how cramped the strip currently is.
  const forceUniformWidth = isBarHovered || draggingGroupIds.size > 0 || keyboardBrowsing;
  const hasVisibleActive = visualOrder.some((tab) => tab.id === activeTabId);

  const widths = new Map<string, number | undefined>();
  const equalShare =
    containerWidth !== undefined && visualOrder.length > 0
      ? containerWidth / visualOrder.length
      : undefined;
  if (forceUniformWidth) {
    visualOrder.forEach((tab) => widths.set(tab.id, ACTIVE_MIN_WIDTH_PX));
  } else if (equalShare === undefined) {
    // First paint, before the container has been measured even once —
    // "auto" (via widthPx ?? "auto" below) until the real share is known.
    visualOrder.forEach((tab) => widths.set(tab.id, undefined));
  } else if (!hasVisibleActive || equalShare >= ACTIVE_MIN_WIDTH_PX) {
    // Room for everyone at (at least) the active tab's floor — including
    // when nothing in the strip is actually active (Dock-browsing a hidden
    // home preview, see isHiddenHomePreview), where there's no one tab to
    // prioritize a width for in the first place.
    const share = Math.max(equalShare, INACTIVE_MIN_WIDTH_PX);
    visualOrder.forEach((tab) => widths.set(tab.id, share));
  } else if (containerWidth !== undefined) {
    // Cramped: the active tab locks at its floor, and every other tab
    // splits whatever's left — shrinking further as more open, down to
    // their own smaller floor, past which the strip scrolls instead
    // (overflow-x-auto below) rather than shrinking anyone further.
    const remaining = containerWidth - ACTIVE_MIN_WIDTH_PX;
    const inactiveCount = visualOrder.length - 1;
    const inactiveShare =
      inactiveCount > 0 ? Math.max(INACTIVE_MIN_WIDTH_PX, remaining / inactiveCount) : undefined;
    visualOrder.forEach((tab) => {
      widths.set(tab.id, tab.id === activeTabId ? ACTIVE_MIN_WIDTH_PX : inactiveShare);
    });
  }

  return (
    // No centering trick needed here: tab widths are computed above to
    // always exactly fill the container (equal-share regime) or exceed it
    // (floor-clamped regime, via overflow-x-auto below) — never fall short
    // of it — so a plain full-width row is enough either way.
    <div
      onPointerEnter={() => setIsBarHovered(true)}
      onPointerLeave={() => setIsBarHovered(false)}
      className="flex min-w-0 [--wails-draggable:no-drag]"
    >
      <div
        ref={scrollRef}
        className="scrollbar-hide flex w-full items-center gap-1 overflow-x-auto"
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
