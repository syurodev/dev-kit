import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { restrictToHorizontalAxis } from "@dnd-kit/modifiers";
import {
  SortableContext,
  horizontalListSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Plus, Wrench, X } from "lucide-react";
import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { getModule } from "../../modules/registry";
import type { TabStatus, WorkbenchTab } from "../../stores/workbenchTabs";
import { beforeIdAfterMove, revealScrollLeft } from "./tabStripModel";

/**
 * The workbench tab strip, built the way VS Code's multi-editor tabs control
 * is, because the previous one was a history of patches over one design: JS
 * computed every tab's width and framer animated it, so switching tabs resized
 * the whole row and closing one slid another under the pointer.
 *
 * - Widths are CSS: every tab asks for the max and they share the room
 *   equally down to a min (VS Code's `fixed` sizing). Nothing is measured to
 *   lay out.
 * - Closing with the mouse locks every tab at the width it has, until the
 *   pointer leaves the strip — the next close button stays under the cursor.
 * - The active tab is scrolled into view by the smallest amount, except right
 *   after a close. A vertical wheel scrolls the strip sideways.
 * - A tab activates on mousedown; middle-click closes; arrow keys, Home and
 *   End move between tabs with a roving tabindex.
 * - Dragging is the result grid's column drag: dnd-kit sortable, the dragged
 *   tab follows the pointer and the others slide aside.
 */

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

/**
 * A module's home view opened as the shared preview tab (a Dock click with no
 * item picked yet — Workbench.tsx's focusModule) gets no strip entry: it is
 * not a distinct piece of content, just "you're looking at this module".
 */
function isHiddenHomePreview(tab: WorkbenchTab): boolean {
  return Boolean(
    tab.preview && getModule(tab.moduleId)?.views[0]?.id === tab.viewId,
  );
}

/** Gap between two tabs. */
const TAB_GAP_PX = 8;
/** The narrowest a tab gets before the strip scrolls: icon, a few letters, close. */
const TAB_MIN_WIDTH_PX = 80;
/** The widest: short titles do not stretch into mostly-empty pills. */
const TAB_MAX_WIDTH_PX = 320;
/** ⌘⇧E browsing widens every tab so the label being chosen can be read. */
const BROWSING_MIN_WIDTH_PX = 200;

type TabItemProps = {
  tab: WorkbenchTab;
  isActive: boolean;
  lockedWidth: number | undefined;
  minWidth: number;
  showFocusRing: boolean;
  draggable: boolean;
  registerNode: (id: string, node: HTMLDivElement | null) => void;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onCloseWithPointer: (id: string) => void;
  onKeyDown: (event: ReactKeyboardEvent, id: string) => void;
};

function TabItem({
  tab,
  isActive,
  lockedWidth,
  minWidth,
  showFocusRing,
  draggable,
  registerNode,
  onSelect,
  onClose,
  onCloseWithPointer,
  onKeyDown,
}: TabItemProps) {
  // `attributes` are not spread, for the grid's reason: they would replace
  // role="tab" and the roving tabindex with role="button" and tabIndex 0.
  const { listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: tab.id,
    disabled: !draggable,
  });
  const ModuleIcon = tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench;

  const style: CSSProperties = {
    // Every tab asks for the max and they all give way equally down to the
    // min, past which the strip scrolls.
    flex: `0 1 ${TAB_MAX_WIDTH_PX}px`,
    minWidth: lockedWidth ?? minWidth,
    maxWidth: lockedWidth ?? TAB_MAX_WIDTH_PX,
    // The grid column's drag: follow the pointer, lift above the neighbours
    // sliding aside under it.
    transform: CSS.Translate.toString(transform),
    transition,
    zIndex: isDragging ? 3 : undefined,
    opacity: isDragging ? 0.8 : undefined,
  };

  return (
    <div
      ref={(node) => {
        setNodeRef(node);
        registerNode(tab.id, node);
      }}
      role="tab"
      aria-selected={isActive}
      tabIndex={isActive ? 0 : -1}
      title={tab.title}
      data-tab-id={tab.id}
      style={style}
      {...listeners}
      onMouseDown={(event) => {
        // Middle button: no autoscroll cursor; auxclick closes.
        if (event.button === 1) {
          event.preventDefault();
          return;
        }
        // Ctrl-click on macOS is a context menu, not a selection.
        if (event.button !== 0 || event.ctrlKey) return;
        if ((event.target as HTMLElement).closest("[data-tab-close]")) return;
        onSelect(tab.id);
      }}
      onAuxClick={(event) => {
        if (event.button === 1 && !tab.preview) onCloseWithPointer(tab.id);
      }}
      onKeyDown={(event) => onKeyDown(event, tab.id)}
      className={cn(
        "group relative flex h-8 items-center rounded-[6px] border px-3 gap-2 outline-none transition-[border-color,background-color] select-none [--wails-draggable:no-drag] focus-visible:border-status-info",
        // The same two marks the selected row in a tree carries — a
        // --status-info hairline and a fill. The hairline carries it (floored
        // at 3:1 in themeContrast.test); an inactive tab keeps a quiet
        // --state-soft ground and rises under the pointer.
        isActive
          ? "bg-surface-raised border-status-info text-text-primary"
          : tab.preview
            ? "bg-state-soft border-border-subtle border-dashed text-body-12 text-text-secondary hover:bg-surface-raised hover:text-text-primary"
            : "bg-state-soft border-transparent text-label-12 text-text-secondary hover:bg-surface-raised hover:text-text-primary",
      )}
    >
      {/* Focus ring: separate layer at radius 8, not the pill's radius 6 (Figma 448:343 exception) */}
      {showFocusRing && (
        <span
          className="pointer-events-none absolute -inset-[2px] rounded-[8px] border-2 border-[var(--border-focus,#b2d5e5)]"
          aria-hidden="true"
        />
      )}
      <span className="pointer-events-none relative z-10 flex min-w-0 flex-1 items-center gap-2">
        <ModuleIcon className="size-3.5 shrink-0 opacity-85" />
        <span
          className={cn(
            "truncate flex-1 min-w-0",
            tab.preview ? "text-body-12 font-normal text-text-secondary" : "text-label-12",
          )}
        >
          {tab.title}
        </span>
        {isActive && tab.status && tab.status !== "idle" ? (
          <span className={cn("size-1.5 shrink-0 rounded-full", statusDotClass(tab.status))} />
        ) : null}
      </span>
      {/* Close button: 12px, shown on Active and Inactive, NOT on Preview */}
      {!tab.preview && (
        <span
          role="button"
          tabIndex={-1}
          data-tab-close
          aria-label={`Close ${tab.title}`}
          // A press on the close button is a close, never the start of a drag.
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => {
            event.stopPropagation();
            onCloseWithPointer(tab.id);
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
    </div>
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
  const visibleTabs = useMemo(
    () => tabs.filter((tab) => !isHiddenHomePreview(tab)),
    [tabs],
  );
  const ids = useMemo(() => visibleTabs.map((tab) => tab.id), [visibleTabs]);

  const rowRef = useRef<HTMLDivElement>(null);
  const tabNodes = useRef(new Map<string, HTMLDivElement>());
  const registerNode = (id: string, node: HTMLDivElement | null) => {
    if (node) tabNodes.current.set(id, node);
    else tabNodes.current.delete(id);
  };

  // ⌘⇧E browsing: a focus ring walks the strip, Enter picks.
  const [keyboardFocusedTabId, setKeyboardFocusedTabId] = useState<string | null>(
    keyboardBrowsing ? activeTabId : null,
  );
  const activeTabIdRef = useRef(activeTabId);
  activeTabIdRef.current = activeTabId;

  useEffect(() => {
    if (keyboardBrowsing) setKeyboardFocusedTabId(activeTabIdRef.current);
  }, [keyboardBrowsing]);

  useEffect(() => {
    if (!keyboardBrowsing) return;
    function handleBrowseKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        onExitBrowsing();
        return;
      }
      if (event.key === "Enter") {
        event.preventDefault();
        if (keyboardFocusedTabId) onSelect(keyboardFocusedTabId);
        onExitBrowsing();
        return;
      }
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      const current = ids.indexOf(keyboardFocusedTabId ?? "");
      if (current === -1) return;
      const next = Math.min(Math.max(current + (event.key === "ArrowLeft" ? -1 : 1), 0), ids.length - 1);
      setKeyboardFocusedTabId(ids[next] ?? null);
    }
    window.addEventListener("keydown", handleBrowseKeyDown);
    return () => window.removeEventListener("keydown", handleBrowseKeyDown);
  }, [keyboardBrowsing, ids, keyboardFocusedTabId, onSelect, onExitBrowsing]);

  /*
   * Widths frozen by a mouse close (VS Code's beforeCloseEditor +
   * --tab-sizing-current-width). Measured before the store drops the tab, so
   * the ones that remain keep their size and slide left by one slot: the next
   * close button lands under the pointer instead of wherever the equal share
   * would put it. Released when the pointer leaves the strip.
   */
  const [lockedWidths, setLockedWidths] = useState<Map<string, number> | null>(null);
  const skipNextReveal = useRef(false);

  function closeWithPointer(id: string) {
    skipNextReveal.current = true;
    if (ids.length > 1 && !keyboardBrowsing) {
      setLockedWidths(
        new Map(ids.map((tabId) => [tabId, tabNodes.current.get(tabId)?.getBoundingClientRect().width ?? 0])),
      );
    }
    onClose(id);
  }

  // Reveal the active tab after the DOM has it, before paint.
  useLayoutEffect(() => {
    if (skipNextReveal.current) {
      skipNextReveal.current = false;
      return;
    }
    const row = rowRef.current;
    const tab = tabNodes.current.get(activeTabId);
    if (!row || !tab) return;
    const next = revealScrollLeft({
      scrollLeft: row.scrollLeft,
      viewportWidth: row.clientWidth,
      tabLeft: tab.offsetLeft,
      tabWidth: tab.offsetWidth,
    });
    if (next !== row.scrollLeft) row.scrollLeft = next;
  }, [activeTabId, ids.length]);

  // A vertical wheel scrolls the strip sideways (VS Code's scrollYToX).
  // Non-passive, because it has to stop the page scrolling instead.
  useEffect(() => {
    const row = rowRef.current;
    if (!row) return;
    function onWheel(event: WheelEvent) {
      if (!row || event.deltaX !== 0 || event.deltaY === 0) return;
      if (row.scrollWidth <= row.clientWidth) return;
      event.preventDefault();
      row.scrollLeft += event.deltaY;
    }
    row.addEventListener("wheel", onWheel, { passive: false });
    return () => row.removeEventListener("wheel", onWheel);
  }, []);

  // The grid's sensor: a press that moves 4px is a drag, anything less is
  // the mousedown that already activated the tab.
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  function handleTabKeyDown(event: ReactKeyboardEvent, id: string) {
    if (keyboardBrowsing) return;
    const index = ids.indexOf(id);
    let target: string | undefined;
    if (event.key === "ArrowLeft") target = ids[index - 1];
    else if (event.key === "ArrowRight") target = ids[index + 1];
    else if (event.key === "Home") target = ids[0];
    else if (event.key === "End") target = ids[ids.length - 1];
    else if (event.key === "Enter" || event.key === " ") target = id;
    if (target === undefined) return;
    event.preventDefault();
    onSelect(target);
    tabNodes.current.get(target)?.focus();
  }

  const minWidth = keyboardBrowsing ? BROWSING_MIN_WIDTH_PX : TAB_MIN_WIDTH_PX;

  return (
    <div
      className="flex h-12 min-w-0 w-full items-center justify-start p-2 gap-2 [--wails-draggable:no-drag]"
      // Double-click on the strip's own empty space opens a tab, as in VS Code.
      onDoubleClick={(event) => {
        if (event.target === event.currentTarget) onNewTab?.();
      }}
    >
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        modifiers={[restrictToHorizontalAxis]}
        onDragEnd={({ active, over }) => {
          if (!over) return;
          const beforeId = beforeIdAfterMove(ids, String(active.id), String(over.id));
          if (beforeId !== undefined) onReorder(String(active.id), beforeId);
        }}
      >
        <SortableContext items={ids} strategy={horizontalListSortingStrategy}>
          <div
            ref={rowRef}
            role="tablist"
            aria-orientation="horizontal"
            // Content-sized so + follows the last tab, min-w-0 so it gives way
            // to the window and scrolls past that, relative so a tab's
            // offsetLeft is measured from here.
            className="scrollbar-hide relative flex min-w-0 shrink items-center overflow-x-auto"
            // Asks for every tab at its max; the strip hands back what fits
            // and the tabs share it equally. Arithmetic from the count, not a
            // measurement: a flex row's natural width is its labels', so
            // without this the row shrank to their average and truncated
            // beside empty space.
            style={{
              gap: TAB_GAP_PX,
              width: visibleTabs.length * TAB_MAX_WIDTH_PX + Math.max(0, visibleTabs.length - 1) * TAB_GAP_PX,
            }}
            onMouseLeave={() => setLockedWidths(null)}
          >
            {visibleTabs.map((tab) => (
              <TabItem
                key={tab.id}
                tab={tab}
                isActive={tab.id === activeTabId}
                lockedWidth={lockedWidths?.get(tab.id)}
                minWidth={minWidth}
                showFocusRing={keyboardBrowsing && tab.id === keyboardFocusedTabId}
                draggable={!keyboardBrowsing}
                registerNode={registerNode}
                onSelect={onSelect}
                onClose={onClose}
                onCloseWithPointer={closeWithPointer}
                onKeyDown={handleTabKeyDown}
              />
            ))}
          </div>
        </SortableContext>
      </DndContext>
      {/* New-tab button: 28x28, radius 8, plus glyph 13px (Figma 449:325) */}
      <Button type="button" variant="titlebar" size="icon-sm" aria-label="New tab" onClick={onNewTab}>
        <Plus className="size-[13px]" />
      </Button>
      {/* Trailing spacer: a flex-grow 8px-high frame after new-tab (Figma 449:325) */}
      <div className="flex-1 h-2 self-center" aria-hidden="true" />
    </div>
  );
}
