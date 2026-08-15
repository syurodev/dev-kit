import { useEffect, useRef, useState } from "react";
import { AnimatePresence, Reorder, motion } from "framer-motion";
import { ChevronDown, Wrench, X } from "lucide-react";
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

type ExpandedGroupProps = {
  tabs: WorkbenchTab[];
  activeTabId: string;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onReorder: (fromId: string, beforeId: string | null) => void;
};

/**
 * Renders one module's tabs as a self-contained draggable Reorder.Group,
 * scoped to just this group's tabs. Every expanded group gets its own
 * instance of this component (and its own drag state) — dragging a tab
 * only ever reorders it among its own group's siblings, never across
 * groups, so there's no cross-group index math to get wrong.
 *
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
 */
function ExpandedGroup({
  tabs,
  activeTabId,
  onSelect,
  onClose,
  onReorder,
}: ExpandedGroupProps) {
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
    const index = visualTabsRef.current.findIndex((tab) => tab.id === draggedId);
    dragOriginBeforeId.current = visualTabsRef.current[index + 1]?.id ?? null;
  }

  function handleDragEnd() {
    const draggedId = activeDragId.current;
    setDraggingTabId(null);
    setHoveredTabId(null);
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

type CollapsedChipProps = {
  group: TabGroup;
  onSelect: (id: string) => void;
  onExpand: () => void;
};

/**
 * One module's tabs, collapsed to an icon + count badge (badge omitted at
 * count 1 — most modules normally have exactly one tab, and a permanent
 * "1" badge on every collapsed module would itself be visual noise).
 * One click, no intermediate menu: a single-tab group switches straight to
 * its one tab; a multi-tab group expands in place (same as "Show all
 * tabs") without switching the active tab — the user then clicks whichever
 * of the now-visible individual tabs they actually want.
 */
function CollapsedChip({ group, onSelect, onExpand }: CollapsedChipProps) {
  const moduleTitle = getModule(group.moduleId)?.title ?? group.moduleId;
  const ModuleIcon = group.tabs[0].icon ?? getModule(group.moduleId)?.icon ?? Wrench;

  function handleClick() {
    if (group.tabs.length === 1) {
      onSelect(group.tabs[0].id);
    } else {
      onExpand();
    }
  }

  return (
    <button
      type="button"
      onClick={handleClick}
      aria-label={
        group.tabs.length === 1
          ? `Switch to ${group.tabs[0].title}`
          : `Show ${moduleTitle} tabs (${group.tabs.length})`
      }
      className="flex h-6 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-foreground/5 hover:text-foreground [--wails-draggable:no-drag]"
    >
      <ModuleIcon className="size-3.5 shrink-0 opacity-70" />
      {group.tabs.length > 1 ? (
        <span className="rounded-full bg-foreground/10 px-1.5 text-[10px] leading-4">
          {group.tabs.length}
        </span>
      ) : null}
    </button>
  );
}

export function TabStrip({
  tabs,
  activeTabId,
  onSelect,
  onClose,
  onReorder,
}: TabStripProps) {
  const [pinnedModuleIds, setPinnedModuleIds] = useState<Set<string>>(new Set());

  const activeModuleId = tabs.find((tab) => tab.id === activeTabId)?.moduleId;
  // The one line that generalizes to split view later: today activeModuleId
  // is a single value, so this is pinned ∪ {one id}. When a second pane can
  // have its own active module, this becomes pinned ∪ {active ids across
  // every pane} — nothing else in this file needs to change.
  const expandedModuleIds = new Set(
    activeModuleId ? [...pinnedModuleIds, activeModuleId] : pinnedModuleIds,
  );

  function pinModule(moduleId: string) {
    setPinnedModuleIds((prev) => new Set(prev).add(moduleId));
  }

  function unpinModule(moduleId: string) {
    setPinnedModuleIds((prev) => {
      const next = new Set(prev);
      next.delete(moduleId);
      return next;
    });
  }

  const groups = groupTabs(tabs);

  return (
    <div className="scrollbar-hide flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [--wails-draggable:no-drag]">
      <AnimatePresence initial={false}>
        {groups.map((group) => {
          const isExpanded = expandedModuleIds.has(group.moduleId);
          const isActiveGroup = group.moduleId === activeModuleId;
          return (
            // A stable per-module key with `layout` (not `layout="position"`
            // — this box's own width is what needs to visibly grow/shrink,
            // not just its position) is what makes the chip <-> full-tabs
            // swap read as "bung ra/thu vào" instead of an instant cut, and
            // makes every other group's chip/tabs slide over to make room.
            // No overflow-hidden here: a drag inside ExpandedGroup can
            // legitimately track the pointer past this box's resting
            // width, and clipping that would look worse than the transient
            // overflow during a resize.
            <motion.div
              key={group.moduleId}
              layout
              initial={{ opacity: 0, scale: 0.9 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.9 }}
              transition={{ layout: { duration: 0.18, ease: "easeOut" } }}
              className="flex shrink-0 items-center gap-1"
            >
              {isExpanded ? (
                <>
                  <ExpandedGroup
                    tabs={group.tabs}
                    activeTabId={activeTabId}
                    onSelect={onSelect}
                    onClose={onClose}
                    onReorder={onReorder}
                  />
                  {isActiveGroup ? null : (
                    // Only a manually-pinned (non-active) expanded group can
                    // be collapsed back — the active module's own group has
                    // no collapse control, since that would hide the tab
                    // the user is currently looking at.
                    <button
                      type="button"
                      aria-label={`Collapse ${getModule(group.moduleId)?.title ?? group.moduleId}`}
                      onClick={() => unpinModule(group.moduleId)}
                      className="flex size-5 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-foreground/5 hover:text-foreground [--wails-draggable:no-drag]"
                    >
                      <ChevronDown className="size-3.5" />
                    </button>
                  )}
                </>
              ) : (
                <CollapsedChip
                  group={group}
                  onSelect={onSelect}
                  onExpand={() => pinModule(group.moduleId)}
                />
              )}
            </motion.div>
          );
        })}
      </AnimatePresence>
    </div>
  );
}
