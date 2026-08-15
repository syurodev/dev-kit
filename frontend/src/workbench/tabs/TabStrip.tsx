import { useEffect, useRef } from "react";
import { AnimatePresence, LazyMotion, domMax, m, type PanInfo } from "framer-motion";
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
  onReorder: (fromId: string, toIndex: number) => void;
};

/**
 * Lives inside the 50px native-titlebar strip (Mac.InvisibleTitleBarHeight
 * in main.go). The container is `--wails-draggable: no-drag` — not `drag`
 * like the rest of the header — because Wails' own drag detection
 * (`@wailsio/runtime/dist/drag.js`) resolves drag-eligibility from
 * `getComputedStyle` on the exact `mousedown` target: with `drag` on this
 * container, the `gap-1` slivers between tabs (and any trailing space
 * after the last one) belonged to it, not to a tab button, so a grab that
 * landed even slightly off a tab handed the whole gesture to a native
 * OS window-move instead of our own pointer-based reorder — which then
 * never sees another event, since the OS owns the drag from that point.
 * The window is still draggable from the `pl-24` header padding before
 * this strip and the header's own default `drag`, just not from inside
 * the tab row itself.
 *
 * `layout` on each tab (needs the heavier `domMax` bundle, not the
 * `domAnimation` one Dock.tsx uses — layout animation isn't in that
 * smaller bundle) means a tab's own width change (the close button
 * revealing on hover/focus) and a tab being added/removed both animate
 * smoothly, including the resulting reflow of every other open tab —
 * without it, siblings snap to their new position in a single frame.
 *
 * Drag-to-reorder uses each tab's own `drag="x"` gesture (also from
 * `domMax`) instead of hand-rolled pointer tracking — a first attempt at
 * hand-rolling this (raw `pointerdown`/`pointermove`/`pointerup`) had no
 * visual feedback until the cursor crossed a neighbor's midpoint, which
 * read as "nothing happening" for a normal-speed drag. `drag` makes the
 * grabbed tab visibly follow the cursor immediately, and Motion's own
 * drag implementation is built on the same layout-projection system as
 * `layout` specifically so the two compose correctly — no fighting over
 * who owns the element's transform, which a manual approach would have
 * had to solve itself. `onDrag` still runs the same midpoint-crossing
 * scan as before to call `onReorder` live as the drag crosses a neighbor.
 *
 * Neighbor midpoints are measured once in `onDragStart`, not on every
 * `onDrag` frame — `getBoundingClientRect()` forces a synchronous layout
 * read, and calling it for every other tab on every frame, right while
 * `layout`/`drag` are mid-transform-update, was visibly janky whenever a
 * drag actually crossed a neighbor (each crossing already costs a real
 * `layout` re-animation of every sibling; stacking N forced reflows on
 * top of that every frame in between made it worse). Tab widths don't
 * change mid-drag, only their order does, so a snapshot taken once stays
 * accurate enough for crossing detection through the whole gesture.
 *
 * `onReorder` itself is also rAF-throttled: a fast drag can fire several
 * `onDrag` events (each detecting a further crossing) before the browser
 * paints a frame, and each one calling `onReorder` immediately means
 * several full `layout` re-animations of every open tab stack up within
 * one frame instead of one. Collapsing them to "at most one commit per
 * frame, using the farthest target seen" keeps the visual result the
 * same (dropping the tab at the same slot) without the redundant work.
 */
export function TabStrip({ tabs, activeTabId, onSelect, onClose, onReorder }: TabStripProps) {
  const tabRefs = useRef(new Map<string, HTMLButtonElement>());
  const dragStartMidpoints = useRef(new Map<string, number>());
  const pendingReorder = useRef<{ draggedId: string; targetIndex: number } | null>(null);
  const reorderFrame = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (reorderFrame.current !== null) {
        cancelAnimationFrame(reorderFrame.current);
      }
    };
  }, []);

  function flushReorder() {
    reorderFrame.current = null;
    const pending = pendingReorder.current;
    if (pending) {
      pendingReorder.current = null;
      onReorder(pending.draggedId, pending.targetIndex);
    }
  }

  function handleDragStart() {
    const midpoints = new Map<string, number>();
    for (const tab of tabs) {
      const el = tabRefs.current.get(tab.id);
      if (el) {
        const rect = el.getBoundingClientRect();
        midpoints.set(tab.id, rect.left + rect.width / 2);
      }
    }
    dragStartMidpoints.current = midpoints;
  }

  function handleDrag(draggedId: string, info: PanInfo) {
    const draggedIndex = tabs.findIndex((tab) => tab.id === draggedId);
    if (draggedIndex === -1) {
      return;
    }
    let targetIndex = -1;
    for (let i = 0; i < tabs.length; i += 1) {
      if (i === draggedIndex) {
        continue;
      }
      const mid = dragStartMidpoints.current.get(tabs[i].id);
      if (mid === undefined) {
        continue;
      }
      if (i > draggedIndex && info.point.x > mid) {
        // Rightward: keep scanning for the farthest match, don't stop at
        // the nearest neighbor — a fast drag can jump past several tabs
        // between two drag events, and we want the tab to land where the
        // cursor actually is, not advance one slot at a time.
        targetIndex = i;
      } else if (i < draggedIndex && info.point.x < mid) {
        // Leftward: midpoints increase with index, so the first (smallest
        // i) match found ascending is already the correct farthest-left
        // target — safe to stop here.
        targetIndex = i;
        break;
      }
    }
    if (targetIndex !== -1) {
      pendingReorder.current = { draggedId, targetIndex };
      if (reorderFrame.current === null) {
        reorderFrame.current = requestAnimationFrame(flushReorder);
      }
    }
  }

  return (
    <LazyMotion features={domMax}>
      <div className="scrollbar-hide flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [--wails-draggable:no-drag]">
        <AnimatePresence initial={false}>
          {tabs.map((tab) => (
            <m.button
              key={tab.id}
              ref={(el) => {
                if (el) {
                  tabRefs.current.set(tab.id, el);
                } else {
                  tabRefs.current.delete(tab.id);
                }
              }}
              layout
              drag="x"
              dragMomentum={false}
              // Matches framer-motion's own Reorder.Item exactly (it
              // hardcodes this true) — "origin" here isn't where the drag
              // started, it's x/y snapping back to 0 on release, which
              // hands control back to `layout`'s own transform. Since
              // onDrag already reordered the array live, `layout` recomputes
              // from the *new* position, so this reads as "snaps into its
              // new slot," not "springs back to where it was." `false` (the
              // prior value) left the raw drag offset applied forever —
              // the tab rendered wherever the pointer was released and
              // never returned to a real flex slot.
              dragSnapToOrigin
              whileDrag={{ scale: 1.05, zIndex: 10 }}
              onDragStart={handleDragStart}
              onDrag={(_event, info) => handleDrag(tab.id, info)}
              initial={{ opacity: 0, scale: 0.9 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.9 }}
              transition={{ layout: { type: "spring", stiffness: 500, damping: 35 } }}
              type="button"
              onClick={() => onSelect(tab.id)}
              className={cn(
                // No `gap` here on purpose: gap reserves space between EVERY
                // flex child, including the close button even while it's
                // collapsed to `w-0` (unlike the `hidden` it replaced, a
                // zero-width child still participates in `gap`) — that
                // phantom space was pushing the title off-center at rest.
                // Spacing between children is explicit margin below instead.
                "group flex h-7 shrink-0 items-center rounded-md px-2.5 text-xs font-medium transition-colors [--wails-draggable:no-drag]",
                tab.id === activeTabId
                  ? "bg-card text-foreground"
                  : "text-muted-foreground hover:bg-white/5 hover:text-foreground",
              )}
            >
              {(() => {
                // A tab-level `icon` (set by whoever called openTab) wins
                // over the module's self-registered default
                // (modules/registry.ts ModuleManifest.icon) — no separate
                // tab-strip-only icon map to drift either way.
                const ModuleIcon = tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench;
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
              <span className={cn("ml-1.5 max-w-40 truncate", tab.preview && "italic")}>
                {tab.title}
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
                className="ml-1 flex w-0 shrink-0 scale-75 items-center justify-center overflow-hidden rounded opacity-0 transition-[width,opacity,transform] duration-150 ease-out hover:bg-white/10 group-hover:w-4 group-hover:scale-100 group-hover:opacity-100 group-focus-within:w-4 group-focus-within:scale-100 group-focus-within:opacity-100 [--wails-draggable:no-drag]"
              >
                <X className="size-3" />
              </span>
            </m.button>
          ))}
        </AnimatePresence>
      </div>
    </LazyMotion>
  );
}
