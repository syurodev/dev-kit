import { useEffect, useRef, useState } from "react";
import { AnimatePresence, Reorder } from "framer-motion";
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
 * Lives inside the 30px titlebar row. Native InvisibleTitleBarHeight is zero;
 * both this row and its header default to no-drag, and Workbench owns
 * a separate native window-drag handle over the empty traffic-light clearance.
 * This is important because Wails detects draggable targets from a capture-
 * phase mousedown before Motion's pointer gesture. Once it invokes the native
 * window move, the OS owns the pointer and React cannot recover the tab drag.
 *
 * `layout="position"` on each tab (needs the heavier `domMax` bundle, not the
 * `domAnimation` one Dock.tsx uses — layout animation isn't in that
 * smaller bundle) animates sibling movement without scale-projecting their
 * content. The close button is absolutely positioned in permanently reserved
 * space, so hover and drag never change a tab's measured width.
 *
 * Motion's Reorder primitives own both pointer offset and layout projection.
 * Keeping those two calculations in the same system avoids transform resets
 * when a dragged tab changes DOM slot. Reorder.Group updates only this
 * component's visual array; Zustand is still committed exactly once on drop,
 * so mounted module views stay outside the high-frequency path.
 */
export function TabStrip({
  tabs,
  activeTabId,
  onSelect,
  onClose,
  onReorder,
}: TabStripProps) {
  const activeDragId = useRef<string | null>(null);
  const dragOriginIndex = useRef<number | null>(null);
  const [visualTabs, setVisualTabs] = useState(tabs);
  const [draggingTabId, setDraggingTabId] = useState<string | null>(null);
  const [hoveredTabId, setHoveredTabId] = useState<string | null>(null);
  const visualTabsRef = useRef(tabs);

  useEffect(() => {
    // Store order is committed only once on drop. External open/close/status
    // changes still flow into the strip immediately while no gesture is active.
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
    dragOriginIndex.current = visualTabsRef.current.findIndex(
      (tab) => tab.id === draggedId,
    );
  }

  function handleDragEnd() {
    const draggedId = activeDragId.current;
    const originIndex = dragOriginIndex.current;
    const targetIndex =
      draggedId === null
        ? null
        : visualTabsRef.current.findIndex((tab) => tab.id === draggedId);
    // Do not let WebKit preserve :hover hit-tests from transformed siblings
    // after pointer capture ends. Hover is rebuilt from the next real pointer
    // movement over a settled tab.
    setDraggingTabId(null);
    setHoveredTabId(null);
    if (
      draggedId !== null &&
      originIndex !== null &&
      targetIndex !== null &&
      originIndex !== targetIndex
    ) {
      // The global store changes once per completed gesture, not once per
      // crossed tab. Heavy module views therefore remain isolated from the
      // high-frequency drag path.
      onReorder(draggedId, targetIndex);
    }
    activeDragId.current = null;
    dragOriginIndex.current = null;
  }

  return (
    <Reorder.Group
      as="div"
      axis="x"
      values={visualTabs}
      onReorder={handleVisualReorder}
      className="scrollbar-hide flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [--wails-draggable:no-drag]"
    >
      <AnimatePresence initial={false}>
        {visualTabs.map((tab) => (
          <Reorder.Item
            as="button"
            value={tab}
            key={tab.id}
            layout="position"
            // Scaling a text-heavy tab forces WebKit to re-rasterise its
            // label and icon while Motion is also projecting every sibling.
            // A raised stacking order is enough feedback here and keeps the
            // compositor work limited to translation.
            whileDrag={{ zIndex: 10 }}
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
            initial={{ opacity: 0, scale: 0.9 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: 0.9 }}
            // Long test gestures cross roughly 20 slots/second (~50ms per
            // crossing), so this must settle before the next crossing to
            // avoid overlapping projections in WKWebView.
            transition={{ layout: { duration: 0.045, ease: "easeOut" } }}
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
              // No `gap` here on purpose: gap reserves space between EVERY
              // flex child, including the close button even while it's
              // collapsed to `w-0` (unlike the `hidden` it replaced, a
              // zero-width child still participates in `gap`) — that
              // phantom space was pushing the title off-center at rest.
              // Spacing between children is explicit margin below instead.
              "group relative flex h-6 shrink-0 items-center rounded-md py-0 pl-2 pr-7 text-xs font-medium transition-colors [--wails-draggable:no-drag]",
              tab.id === activeTabId
                ? "bg-card text-foreground"
                : hoveredTabId === tab.id && draggingTabId === null
                  ? "bg-foreground/5 text-foreground"
                  : "text-muted-foreground",
            )}
          >
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
              className={cn(
                "ml-1.5 max-w-40 truncate",
                tab.preview && "italic",
              )}
            >
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
              className={cn(
                "absolute right-1 top-1/2 flex size-4 -translate-y-1/2 scale-75 items-center justify-center rounded opacity-0 transition-[opacity,transform] duration-150 ease-out hover:bg-foreground/10 focus:scale-100 focus:opacity-100 [--wails-draggable:no-drag]",
                hoveredTabId === tab.id &&
                  draggingTabId === null &&
                  "scale-100 opacity-100",
              )}
            >
              <X className="size-3" />
            </span>
          </Reorder.Item>
        ))}
      </AnimatePresence>
    </Reorder.Group>
  );
}
