import { FileIcon } from "@/components/file-icon/FileIcon";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { cn } from "@/lib/utils";
import { shortcut } from "@/lib/shortcut";
import {
  DndContext,
  DragOverlay,
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
import { ChevronLeft, ChevronRight, Columns2, MoveRight, Pin, PinOff, Plus, Settings2, Wrench, X } from "lucide-react";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { useReducedMotion } from "framer-motion";
import { useShallow } from "zustand/react/shallow";
import { getModule, viewIcon, type TabAction } from "../../modules/registry";
import { useTabSettingsStore } from "../../stores/tabSettings";
import { useWorkbenchTabsStore, type TabStatus, type WorkbenchTab } from "../../stores/workbenchTabs";
import { offeredClosedTabs, reopenClosedAt, reopenLastClosed } from "../reopenTab";
import { useSeenSuccessStore } from "./seenSuccess";
import { beforeIdAfterMove, overflowEdges, revealScrollLeftWithin, scrollStep, tabMarks } from "./tabStripModel";

/**
 * The workbench tab strip, built the way VS Code's multi-editor tabs control
 * is, because the previous one was a history of patches over one design: JS
 * computed every tab's width and framer animated it, so switching tabs resized
 * the whole row and closing one slid another under the pointer.
 *
 * - Widths are CSS: every tab asks for the max and they share the room
 *   equally down to a min (VS Code's `fixed` sizing, both set in Settings).
 *   Nothing is measured to lay out.
 * - Closing with the mouse locks every tab at the width it has, until the
 *   pointer leaves the strip — the next close button stays under the cursor.
 * - The active tab is scrolled into view by the smallest amount, except right
 *   after a close. A vertical wheel scrolls the strip sideways. While tabs are
 *   out of sight, the side hiding them fades and ‹ › scroll the strip.
 * - A tab activates on mousedown; middle-click closes; arrow keys, Home and
 *   End move between tabs with a roving tabindex.
 * - Dragging is the result grid's column drag: dnd-kit sortable, the dragged
 *   tab follows the pointer and the others slide aside.
 * - Pinned tabs sit at the left and the group closes pass over them.
 */

function statusDotClass(status: Exclude<TabStatus, "idle">): string {
  switch (status) {
    case "running":
      return "bg-(--status-running)";
    case "success":
      return "bg-(--status-success)";
    case "error":
      return "bg-(--status-error)";
  }
}

type TabStripProps = {
  tabs: WorkbenchTab[];
  activeTabId: string;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onReorder: (fromId: string, beforeId: string | null) => void;
  onNewTab?: () => void;
  /** Keep a preview tab open (the store's pinTab). Offered in the tab's menu. */
  onPin?: (id: string) => void;
  /** Pin or unpin (the store's togglePinTab). */
  onTogglePin?: (id: string) => void;
  /** Settings ▸ Appearance, where tab widths are set. */
  onConfigureTabs?: () => void;
  /** Move the tab into a new document group beside this one (⌘\\). */
  onSplitRight?: (id: string) => void;
  /** Other groups showing this module, which a tab can move to. */
  moveTargets?: { id: string; label: string }[];
  onMoveToGroup?: (tabId: string, groupId: string) => void;
  /**
   * A tab dragged out of the strip, for the document groups to place
   * (DocumentGroups.tsx): `over` as it moves (null when the drag ends), `drop`
   * where it lands — true if it was placed there, else the strip reorders.
   * Given, the dragged tab follows the pointer anywhere, not only sideways.
   */
  onTabDragOver?: (tabId: string, point: { x: number; y: number } | null) => void;
  onTabDrop?: (tabId: string, point: { x: number; y: number }) => boolean;
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

/** The tab as it is carried out of its strip. */
function TabDragGhost({ tab }: { tab: WorkbenchTab | undefined }) {
  if (!tab) return null;
  const Icon = tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench;
  return (
    <div className="flex h-(--control-height-sm) max-w-60 cursor-grabbing items-center gap-2 rounded-md border border-border-subtle bg-surface-raised px-2.5 text-label-12 text-text-primary shadow-lg">
      {tab.viewId === "codegraph.file" && typeof tab.params?.path === "string" ? <FileIcon path={tab.params.path} className="size-3.5" /> : <Icon className="size-3.5 shrink-0" />}
      <span className="truncate">{tab.title}</span>
    </div>
  );
}

/**
 * ‹ or ›. Out of the tab order: a keyboard reaches a hidden tab with the
 * arrow keys, which reveal it. The span takes the double-click a disabled
 * button lets through (it has no pointer events), which would otherwise land
 * on the strip's empty space and open a tab.
 */
function ScrollTabsButton({
  direction,
  disabled,
  onScroll,
  nodeRef,
}: {
  direction: "left" | "right";
  disabled: boolean;
  onScroll: (direction: "left" | "right") => void;
  nodeRef: (node: HTMLSpanElement | null) => void;
}) {
  const Icon = direction === "left" ? ChevronLeft : ChevronRight;
  return (
    <span ref={nodeRef} className="flex shrink-0" onDoubleClick={(event) => event.stopPropagation()}>
      <Button
        type="button"
        variant="titlebar"
        size="icon-sm"
        tabIndex={-1}
        aria-label={`Scroll tabs ${direction}`}
        disabled={disabled}
        onClick={() => onScroll(direction)}
      >
        <Icon className="size-3.5" />
      </Button>
    </span>
  );
}

/**
 * Gap between two tabs — and the strip's inset on all four sides, and the
 * space between the last tab, ‹ › and + (p-1.5 gap-1.5 below): a 40px strip
 * holds 28px tabs 6px from every edge, so the 6px tab radius sits
 * concentric in the pane's 12px corner.
 */
const TAB_GAP_PX = 6;
/**
 * How far the keyboard focus ring reaches outside a tab (-inset-0.5). The
 * scroller clips both axes (overflow-x makes overflow-y clip too), so it pads
 * itself by this much and gives it back in negative margin: the ring is drawn
 * whole and nothing moves.
 */
const RING_PX = 2;
/** ⌘⇧E browsing widens every tab so the label being chosen can be read. */
const BROWSING_MIN_WIDTH_PX = 200;
/** The overflow fade: about a tab icon's width plus its padding — enough to
 * read as "more", not enough to hide a title. */
const FADE_PX = 24;

/**
 * A module's own entries, drawn as the menu opens.
 *
 * A component rather than a list built with the tab: Radix mounts menu
 * content only while it is open, so this reads the module's state at that
 * moment — a console's "Copy SQL" is about the SQL in it now.
 */
function ModuleTabActions({ tab }: { tab: WorkbenchTab }) {
  const actions = getModule(tab.moduleId)?.tabActions?.(tab) ?? [];
  if (actions.length === 0) return null;
  return (
    <>
      <ContextMenuSeparator />
      <TabActionItems actions={actions} />
    </>
  );
}

/** The newest this many closed tabs are listed; the stack holds more. */
const RECENTLY_CLOSED_LISTED = 10;

/**
 * Reopen Closed Tab and the Recently Closed list. Drawn as the menu opens,
 * like ModuleTabActions, so a strip does not follow the stack while no menu
 * is showing it. A record whose tab is open again is not offered; each entry
 * reopens its own place in the stack.
 */
function ReopenClosedItems() {
  const stack = useWorkbenchTabsStore((s) => s.recentlyClosed);
  const modules = useWorkbenchTabsStore((s) => s.modules);
  const offered = useMemo(() => offeredClosedTabs({ recentlyClosed: stack, modules }), [stack, modules]);
  return (
    <>
      <ContextMenuSeparator />
      <ContextMenuItem
        shortcut={shortcut("T", { shift: true })}
        disabled={offered.length === 0}
        onSelect={() => reopenLastClosed()}
      >
        Reopen Closed Tab
      </ContextMenuItem>
      <ContextMenuSub>
        <ContextMenuSubTrigger disabled={offered.length === 0}>Recently Closed</ContextMenuSubTrigger>
        <ContextMenuSubContent>
          {offered.slice(0, RECENTLY_CLOSED_LISTED).map(({ entry, at }) => {
            const Icon = entry.icon ?? viewIcon(entry.moduleId, entry.viewId);
            return (
              <ContextMenuItem
                key={`${entry.closedAt}:${at}`}
                icon={Icon ? <Icon /> : undefined}
                onSelect={() => reopenClosedAt(at)}
              >
                {entry.title}
              </ContextMenuItem>
            );
          })}
        </ContextMenuSubContent>
      </ContextMenuSub>
    </>
  );
}

function TabActionItems({ actions }: { actions: TabAction[] }) {
  return (
    <>
      {actions.map((action) => {
        if (action.type === "separator") return <ContextMenuSeparator key={action.id} />;
        const Icon = action.icon;
        if (action.type === "submenu") {
          return (
            <ContextMenuSub key={action.id}>
              <ContextMenuSubTrigger icon={Icon ? <Icon /> : undefined}>{action.label}</ContextMenuSubTrigger>
              <ContextMenuSubContent>
                <TabActionItems actions={action.items} />
              </ContextMenuSubContent>
            </ContextMenuSub>
          );
        }
        return (
          <ContextMenuItem
            key={action.id}
            icon={Icon ? <Icon /> : undefined}
            shortcut={action.shortcut}
            disabled={action.disabled}
            onSelect={action.run}
          >
            {action.label}
          </ContextMenuItem>
        );
      })}
    </>
  );
}

type TabItemProps = {
  tab: WorkbenchTab;
  isActive: boolean;
  /** A finished tab that has been looked at since, so its success dot is spent. */
  seenSuccess: boolean;
  lockedWidth: number | undefined;
  minWidth: number;
  maxWidth: number;
  showFocusRing: boolean;
  draggable: boolean;
  registerNode: (id: string, node: HTMLDivElement | null) => void;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onCloseWithPointer: (id: string) => void;
  onKeyDown: (event: ReactKeyboardEvent, id: string) => void;
  /** The tab menu's groups, worked out by the strip, which knows the order. */
  others: string[];
  toTheLeft: string[];
  toTheRight: string[];
  all: string[];
  onCloseMany: (ids: string[]) => void;
  onPin?: (id: string) => void;
  onTogglePin?: (id: string) => void;
  onConfigureTabs?: () => void;
  onSplitRight?: (id: string) => void;
  moveTargets?: { id: string; label: string }[];
  onMoveToGroup?: (tabId: string, groupId: string) => void;
};

function TabItem({
  tab,
  isActive,
  seenSuccess,
  lockedWidth,
  minWidth,
  maxWidth,
  showFocusRing,
  draggable,
  registerNode,
  onSelect,
  onClose,
  onCloseWithPointer,
  onKeyDown,
  others,
  toTheLeft,
  toTheRight,
  all,
  onCloseMany,
  onPin,
  onTogglePin,
  onConfigureTabs,
  onSplitRight,
  moveTargets = [],
  onMoveToGroup,
}: TabItemProps) {
  // `attributes` are not spread, for the grid's reason: they would replace
  // role="tab" and the roving tabindex with role="button" and tabIndex 0.
  const { listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: tab.id,
    disabled: !draggable,
  });
  const ModuleIcon = tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench;
  const pinned = Boolean(tab.pinned);
  const marks = tabMarks({ status: tab.status, active: isActive, seenSuccess, dirty: Boolean(tab.dirty) });

  const style: CSSProperties = {
    // Every tab asks for the max and they all give way equally down to the
    // min, past which the strip scrolls.
    flex: `0 1 ${maxWidth}px`,
    minWidth: lockedWidth ?? minWidth,
    maxWidth: lockedWidth ?? maxWidth,
    // The grid column's drag: follow the pointer, lift above the neighbours
    // sliding aside under it.
    transform: CSS.Translate.toString(transform),
    transition,
    zIndex: isDragging ? 3 : undefined,
    opacity: isDragging ? 0.8 : undefined,
  };

  return (
    // The app's tooltip, not the OS one a `title` draws. Held shut while the
    // tab is being dragged: the tab moves under the pointer, and a hover that
    // "arrives" mid-drag would open it over the neighbours.
    //
    // Roots first, triggers after: a Radix Root is a provider with no element,
    // so it cannot be an asChild target — the two triggers nest directly and
    // both land on the tab.
    <Tooltip {...(isDragging ? { open: false } : {})}>
    <ContextMenu>
    <TooltipTrigger asChild>
    <ContextMenuTrigger asChild>
    <div
      ref={(node) => {
        setNodeRef(node);
        registerNode(tab.id, node);
      }}
      role="tab"
      aria-selected={isActive}
      tabIndex={isActive ? 0 : -1}
      data-tab-id={tab.id}
      data-pinned={pinned || undefined}
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
        if (event.button === 1) onCloseWithPointer(tab.id);
      }}
      onKeyDown={(event) => onKeyDown(event, tab.id)}
      className={cn(
        "group relative flex h-(--control-height-sm) items-center rounded-sm border px-3 gap-2 outline-none transition-border-bg select-none wails-no-drag focus-visible:border-border-focus",
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
          className="pointer-events-none absolute -inset-0.5 rounded-md border-2 border-border-focus"
          aria-hidden="true"
        />
      )}
      <span className="pointer-events-none relative z-10 flex min-w-0 flex-1 items-center gap-2">
        {tab.viewId === "codegraph.file" && typeof tab.params?.path === "string" ? <FileIcon path={tab.params.path} className="size-3.5" /> : <ModuleIcon className="size-3.5 shrink-0 opacity-85" />}
        {marks.status === "running" ? (
          <span aria-hidden="true" className="tab-status-spin size-2.5 shrink-0 rounded-full" />
        ) : marks.status ? (
          <span aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-full", statusDotClass(marks.status))} />
        ) : null}
        <span
          className={cn(
            "truncate flex-1 min-w-0",
            isActive
              ? "text-label-12 text-text-primary"
              : tab.preview ? "text-body-12 font-normal text-text-secondary" : "text-label-12",
          )}
        >
          {tab.title}
        </span>
        {/* The marks are drawn, so a screen reader is told them in words. */}
        {marks.hint.length > 0 ? <span className="sr-only">, {marks.hint.join(", ")}</span> : null}
      </span>
      {/* A pinned tab shows its pin where the close button goes, and the pin
          unpins (VS Code). Every other tab — a preview too — has the 12px
          close. Unsaved work shows as a dot laid over that 12px slot (right-3
          is the tab's px-3), and gives way to the pin or close under the
          pointer or keyboard focus (VS Code). The button is only see-through
          meanwhile, so a screen reader still finds it. */}
      {marks.dirty ? (
        <span
          aria-hidden="true"
          data-tab-dirty
          className="pointer-events-none absolute top-1/2 right-3 z-10 flex size-3 -translate-y-1/2 items-center justify-center group-hover:opacity-0 group-focus-visible:opacity-0"
        >
          <span className="size-2 rounded-full bg-text-secondary" />
        </span>
      ) : null}
      {pinned && onTogglePin ? (
        <span
          role="button"
          tabIndex={-1}
          data-tab-close
          aria-label={`Unpin ${tab.title}`}
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => {
            event.stopPropagation();
            onTogglePin(tab.id);
          }}
          className={cn(
            "z-10 flex size-3 shrink-0 items-center justify-center rounded text-text-secondary hover:text-text-primary transition-colors focus:outline-none wails-no-drag cursor-pointer",
            marks.dirty &&
              "pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-focus-visible:pointer-events-auto group-focus-visible:opacity-100",
          )}
        >
          <Pin className="size-3" />
        </span>
      ) : (
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
          className={cn(
            "z-10 flex size-3 shrink-0 items-center justify-center rounded text-text-secondary hover:text-text-primary transition-colors focus:outline-none wails-no-drag cursor-pointer",
            marks.dirty &&
              "pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-focus-visible:pointer-events-auto group-focus-visible:opacity-100",
          )}
        >
          <X className="size-3" />
        </span>
      )}
    </div>
    </ContextMenuTrigger>
    </TooltipTrigger>
    <TooltipContent side="bottom" className={marks.hint.length > 0 ? "h-auto flex-col items-start gap-0.5 py-1.5" : undefined}>
      <span>{tab.title}</span>
      {marks.hint.map((line) => (
        <span key={line} className="text-text-secondary">{line}</span>
      ))}
    </TooltipContent>
    {/* VS Code's tab menu: the closes, pinning, then whatever the module
        adds, then the strip's own settings. The group closes pass over
        pinned tabs, which is what pinning is for. */}
    <ContextMenuContent>
      <ContextMenuItem shortcut={shortcut("W")} onSelect={() => onClose(tab.id)}>
        Close
      </ContextMenuItem>
      <ContextMenuItem disabled={others.length === 0} onSelect={() => onCloseMany(others)}>
        Close Other Tabs
      </ContextMenuItem>
      <ContextMenuItem disabled={toTheLeft.length === 0} onSelect={() => onCloseMany(toTheLeft)}>
        Close Tabs to the Left
      </ContextMenuItem>
      <ContextMenuItem disabled={toTheRight.length === 0} onSelect={() => onCloseMany(toTheRight)}>
        Close Tabs to the Right
      </ContextMenuItem>
      <ContextMenuItem disabled={all.length === 0} onSelect={() => onCloseMany(all)}>
        Close All Tabs
      </ContextMenuItem>
      <ReopenClosedItems />
      {onTogglePin || (tab.preview && onPin) ? <ContextMenuSeparator /> : null}
      {onTogglePin ? (
        <ContextMenuItem
          icon={pinned ? <PinOff /> : <Pin />}
          onSelect={() => onTogglePin(tab.id)}
        >
          {pinned ? "Unpin Tab" : "Pin Tab"}
        </ContextMenuItem>
      ) : null}
      {tab.preview && onPin ? (
        <ContextMenuItem onSelect={() => onPin(tab.id)}>Keep Open</ContextMenuItem>
      ) : null}
      {onSplitRight || (moveTargets.length > 0 && onMoveToGroup) ? <ContextMenuSeparator /> : null}
      {onSplitRight ? (
        <ContextMenuItem icon={<Columns2 />} shortcut={shortcut("\\")} onSelect={() => onSplitRight(tab.id)}>
          Split Right
        </ContextMenuItem>
      ) : null}
      {onMoveToGroup
        ? moveTargets.map((target) => (
            <ContextMenuItem key={target.id} icon={<MoveRight />} onSelect={() => onMoveToGroup(tab.id, target.id)}>
              {target.label}
            </ContextMenuItem>
          ))
        : null}
      <ModuleTabActions tab={tab} />
      {onConfigureTabs ? (
        <>
          <ContextMenuSeparator />
          <ContextMenuItem icon={<Settings2 />} onSelect={onConfigureTabs}>
            Configure Tabs…
          </ContextMenuItem>
        </>
      ) : null}
    </ContextMenuContent>
    </ContextMenu>
    </Tooltip>
  );
}

export function TabStrip({
  tabs,
  activeTabId,
  onSelect,
  onClose,
  onReorder,
  onNewTab,
  onPin,
  onTogglePin,
  onConfigureTabs,
  onSplitRight,
  moveTargets,
  onMoveToGroup,
  onTabDragOver,
  onTabDrop,
  keyboardBrowsing,
  onExitBrowsing,
}: TabStripProps) {
  const [draggingId, setDraggingId] = useState<string | null>(null);
  const visibleTabs = useMemo(
    () => tabs.filter((tab) => !isHiddenHomePreview(tab)),
    [tabs],
  );
  const ids = useMemo(() => visibleTabs.map((tab) => tab.id), [visibleTabs]);

  // Finished tabs looked at since: their success dot is spent (seenSuccess.ts).
  const seenSuccess = useSeenSuccessStore((s) => s.seen);
  const { minWidth: tabMinWidth, maxWidth: tabMaxWidth } = useTabSettingsStore(
    useShallow((s) => ({ minWidth: s.minWidth, maxWidth: s.maxWidth })),
  );

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
   * Which sides hide tabs. Read from the scroller after layout, and again on
   * scroll and resize: the count, the width settings, browsing and a close's
   * locked widths all change what the row holds without resizing it.
   *
   * Asked of the room the row would have without ‹ ›: measured with them, a
   * row that overflows only by their width would keep them for good.
   */
  const [edges, setEdges] = useState({ left: false, right: false });
  const scrollButtons = useRef<{ left: HTMLSpanElement | null; right: HTMLSpanElement | null }>({ left: null, right: null });
  const syncEdges = useCallback(() => {
    const row = rowRef.current;
    if (!row) return;
    // Each button and the gap beside it is what the row would get back.
    const buttonsRoom = [scrollButtons.current.left, scrollButtons.current.right].reduce(
      (room, button) => room + (button ? button.getBoundingClientRect().width + TAB_GAP_PX : 0),
      0,
    );
    const next = row.scrollWidth <= row.clientWidth + buttonsRoom
      ? { left: false, right: false }
      : overflowEdges({ scrollLeft: row.scrollLeft, scrollWidth: row.scrollWidth, clientWidth: row.clientWidth });
    setEdges((prev) => (prev.left === next.left && prev.right === next.right ? prev : next));
  }, []);
  const overflowing = edges.left || edges.right;

  /*
   * Widths frozen by a mouse close (VS Code's beforeCloseEditor +
   * --tab-sizing-current-width). Measured before the store drops the tab, so
   * the ones that remain keep their size and slide left by one slot: the next
   * close button lands under the pointer instead of wherever the equal share
   * would put it. Released when the pointer leaves the strip.
   *
   * The ‹ › pair is held the same way: a close that ends the overflow would
   * otherwise unmount ‹ and shift every tab left by its width.
   */
  const [lockedWidths, setLockedWidths] = useState<Map<string, number> | null>(null);
  const [buttonsHeld, setButtonsHeld] = useState(false);
  const showButtons = overflowing || (lockedWidths !== null && buttonsHeld);
  const skipNextReveal = useRef(false);

  function closeWithPointer(id: string) {
    skipNextReveal.current = true;
    if (ids.length > 1 && !keyboardBrowsing) {
      setLockedWidths(
        new Map(ids.map((tabId) => [tabId, tabNodes.current.get(tabId)?.getBoundingClientRect().width ?? 0])),
      );
      setButtonsHeld(showButtons);
    }
    onClose(id);
  }

  /*
   * Reveal the active tab (the one ⌘⇧E browsing is on, while browsing) after
   * the DOM has it, before paint — clear of the fades, which read as cut off.
   * Again when the ‹ › pair comes or goes: the tab that starts the overflow
   * was fitted into the room before the buttons took theirs.
   */
  const revealId = keyboardBrowsing ? (keyboardFocusedTabId ?? activeTabId) : activeTabId;
  useLayoutEffect(() => {
    if (skipNextReveal.current) {
      skipNextReveal.current = false;
      return;
    }
    const row = rowRef.current;
    const tab = tabNodes.current.get(revealId);
    if (!row || !tab) return;
    const faded = overflowEdges({ scrollLeft: row.scrollLeft, scrollWidth: row.scrollWidth, clientWidth: row.clientWidth });
    const next = revealScrollLeftWithin(
      {
        scrollLeft: row.scrollLeft,
        viewportWidth: row.clientWidth,
        tabLeft: tab.offsetLeft,
        tabWidth: tab.offsetWidth,
      },
      { left: faded.left ? FADE_PX : 0, right: faded.right ? FADE_PX : 0 },
    );
    if (next !== row.scrollLeft) row.scrollLeft = next;
  }, [revealId, ids.length, overflowing, showButtons]);

  // A vertical wheel scrolls the strip sideways (VS Code's scrollYToX) —
  // anywhere on it, ‹ › included. Non-passive, because it has to stop the
  // page scrolling instead.
  const stripRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const strip = stripRef.current;
    if (!strip) return;
    function onWheel(event: WheelEvent) {
      const row = rowRef.current;
      if (!row || event.deltaX !== 0 || event.deltaY === 0) return;
      if (row.scrollWidth <= row.clientWidth) return;
      event.preventDefault();
      row.scrollLeft += event.deltaY;
    }
    strip.addEventListener("wheel", onWheel, { passive: false });
    return () => strip.removeEventListener("wheel", onWheel);
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

  // Browsing widens the floor, but never past the max — a floor above the
  // ceiling is resolved by CSS in the floor's favour, and the setting says
  // otherwise.
  const minWidth = keyboardBrowsing
    ? Math.max(tabMinWidth, Math.min(BROWSING_MIN_WIDTH_PX, tabMaxWidth))
    : tabMinWidth;
  // What the group closes may take: everything but the pinned.
  const closable = visibleTabs.filter((tab) => !tab.pinned).map((tab) => tab.id);

  useLayoutEffect(() => syncEdges(), [syncEdges, visibleTabs.length, tabMaxWidth, minWidth, lockedWidths]);
  useEffect(() => {
    const row = rowRef.current;
    if (!row || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(syncEdges);
    observer.observe(row);
    row.addEventListener("scroll", syncEdges, { passive: true });
    return () => {
      observer.disconnect();
      row.removeEventListener("scroll", syncEdges);
    };
  }, [syncEdges]);
  const reduceMotion = useReducedMotion();
  function scrollTabs(direction: "left" | "right") {
    const row = rowRef.current;
    if (!row) return;
    row.scrollTo({ left: scrollStep(row, direction), behavior: reduceMotion ? "auto" : "smooth" });
  }
  // A mask, not an overlay, so nothing sits over the tabs eating clicks. Its
  // #000 is opacity (an alpha mask keeps what is opaque), not a colour.
  const fadeMask = overflowing
    ? `linear-gradient(to right, ${edges.left ? "transparent" : "#000"} 0, #000 ${FADE_PX}px, #000 calc(100% - ${FADE_PX}px), ${edges.right ? "transparent" : "#000"} 100%)`
    : undefined;

  return (
    <div
      ref={stripRef}
      className="flex h-10 min-w-0 w-full items-center justify-start p-1.5 gap-1.5 wails-no-drag"
      // Double-click on the strip's own empty space opens a tab, as in VS Code.
      onDoubleClick={(event) => {
        if (event.target === event.currentTarget) onNewTab?.();
      }}
      onMouseLeave={() => {
        setLockedWidths(null);
        setButtonsHeld(false);
      }}
    >
      {/* Only while tabs are out of sight; a side already at its end is
          disabled rather than removed, so the strip does not jump. */}
      {showButtons ? (
        <ScrollTabsButton
          direction="left"
          disabled={!edges.left}
          onScroll={scrollTabs}
          nodeRef={(node) => {
            scrollButtons.current.left = node;
          }}
        />
      ) : null}
      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        // Sideways only, unless the groups can take the tab somewhere else.
        modifiers={onTabDrop ? [] : [restrictToHorizontalAxis]}
        onDragStart={({ active }) => setDraggingId(String(active.id))}
        onDragMove={({ active, delta, activatorEvent }) => {
          const start = activatorEvent as PointerEvent;
          onTabDragOver?.(String(active.id), { x: start.clientX + delta.x, y: start.clientY + delta.y });
        }}
        onDragCancel={({ active }) => {
          setDraggingId(null);
          onTabDragOver?.(String(active.id), null);
        }}
        onDragEnd={({ active, over, delta, activatorEvent }) => {
          setDraggingId(null);
          onTabDragOver?.(String(active.id), null);
          const start = activatorEvent as PointerEvent;
          const point = { x: start.clientX + delta.x, y: start.clientY + delta.y };
          if (onTabDrop?.(String(active.id), point)) return;
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
            // offsetLeft is measured from here; -m-0.5 p-0.5 is RING_PX.
            className="scrollbar-hide relative -m-0.5 flex min-w-0 shrink items-center overflow-x-auto p-0.5"
            // Asks for every tab at its max; the strip hands back what fits
            // and the tabs share it equally. Arithmetic from the count, not a
            // measurement: a flex row's natural width is its labels', so
            // without this the row shrank to their average and truncated
            // beside empty space.
            style={{
              gap: TAB_GAP_PX,
              width: visibleTabs.length * tabMaxWidth + Math.max(0, visibleTabs.length - 1) * TAB_GAP_PX + 2 * RING_PX,
              maskImage: fadeMask,
              WebkitMaskImage: fadeMask,
            }}
          >
            {visibleTabs.map((tab, index) => (
              <TabItem
                key={tab.id}
                tab={tab}
                isActive={tab.id === activeTabId}
                seenSuccess={seenSuccess.has(tab.id)}
                lockedWidth={lockedWidths?.get(tab.id)}
                minWidth={minWidth}
                maxWidth={tabMaxWidth}
                showFocusRing={keyboardBrowsing && tab.id === keyboardFocusedTabId}
                draggable={!keyboardBrowsing}
                registerNode={registerNode}
                onSelect={onSelect}
                onClose={onClose}
                onCloseWithPointer={closeWithPointer}
                onKeyDown={handleTabKeyDown}
                others={closable.filter((id) => id !== tab.id)}
                toTheLeft={visibleTabs.slice(0, index).filter((t) => !t.pinned).map((t) => t.id)}
                toTheRight={visibleTabs.slice(index + 1).filter((t) => !t.pinned).map((t) => t.id)}
                all={closable}
                // One store close per tab: the same path the close button
                // takes, so a group close can never disagree with a single one.
                onCloseMany={(list) => list.forEach((id) => onClose(id))}
                onPin={onPin}
                onTogglePin={onTogglePin}
                onConfigureTabs={onConfigureTabs}
                onSplitRight={onSplitRight}
                moveTargets={moveTargets}
                onMoveToGroup={onMoveToGroup}
              />
            ))}
          </div>
        </SortableContext>
        {/* Out of the strip the tab would be clipped by its scroller, so a
            drag that may leave it is drawn in an overlay above everything. */}
        {onTabDrop ? (
          <DragOverlay dropAnimation={null}>
            {draggingId ? <TabDragGhost tab={visibleTabs.find((t) => t.id === draggingId)} /> : null}
          </DragOverlay>
        ) : null}
      </DndContext>
      {showButtons ? (
        <ScrollTabsButton
          direction="right"
          disabled={!edges.right}
          onScroll={scrollTabs}
          nodeRef={(node) => {
            scrollButtons.current.right = node;
          }}
        />
      ) : null}
      {/* New-tab button: 28x28, radius 8, plus glyph 13px (Figma 449:325) */}
      <Button type="button" variant="titlebar" size="icon-sm" aria-label="New tab" onClick={onNewTab}>
        <Plus className="size-3.25" />
      </Button>
      {/* Trailing spacer: a flex-grow 8px-high frame after new-tab (Figma 449:325) */}
      <div className="flex-1 h-2 self-center" aria-hidden="true" />
    </div>
  );
}
