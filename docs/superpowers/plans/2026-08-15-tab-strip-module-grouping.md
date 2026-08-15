# Tab Strip Module Grouping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Group the tab strip by module; a module's tabs collapse into one icon+count chip when its group isn't expanded, expand into full individual tabs when it is — with expand/collapse tracked as independent per-module state (not "only the active module is ever expanded"), so a future split-view feature can extend the same mechanism instead of requiring a rewrite.

**Architecture:** `useWorkbenchTabsStore.reorderTab` moves from an index-based signature to an id-based `(fromId, beforeId)` one, because drag-to-reorder is now scoped per expanded group (each group gets its own independent `Reorder.Group`), so there's no single global array index to compute correctly anymore — "move `fromId` to just before `beforeId`" stays correct regardless of what subset is currently rendered. `TabStrip.tsx` is restructured: tabs are grouped by `moduleId` (stable first-open order); `expandedModuleIds = pinnedModuleIds ∪ {activeModuleId}` decides which groups render as full draggable tabs (via a new `ExpandedGroup` sub-component, the same drag mechanics as today just scoped to one group's tabs) versus a `CollapsedChip` (icon + count badge, opens a dropdown menu listing that group's tab titles plus a "Show all tabs" peek action that pins the group open without switching the active tab).

**Tech Stack deviation from the skill default:** no frontend unit test runner in this repo — verification is `cd frontend && bun run build` (tsc + vite) plus a manual QA pass (final task). Reuses the existing `components/ui/dropdown-menu.tsx` (shadcn/Radix) — no new dependency.

**Builds on:** `docs/superpowers/specs/2026-08-15-tab-strip-module-grouping-design.md` (the approved design this plan implements — read it for the full Decisions/Non-goals record, including why this supersedes an earlier simpler version, and the Chrome/Firefox tab-group research behind the per-group-not-per-active-module model).

---

## File Structure

Modify:
- `frontend/src/stores/workbenchTabs.ts` — `reorderTab` signature and implementation change from `(fromId, toIndex: number)` to `(fromId, beforeId: string | null)`.
- `frontend/src/workbench/tabs/TabStrip.tsx` — full rewrite: adds `groupTabs()`, a new `ExpandedGroup` sub-component (today's drag-reorder logic, scoped to one group), a new `CollapsedChip` sub-component (icon + count badge + dropdown), and the top-level `TabStrip` now composes groups instead of rendering one flat `Reorder.Group`.

No files created. `frontend/src/workbench/Workbench.tsx` needs no change — it already just passes the store's `reorderTab` action through as `<TabStrip onReorder={reorderTab} />`; both sides read the type from the same store, so it stays correct once Task 1 updates both files together.

---

### Task 1: `reorderTab` id-based signature + grouped `TabStrip` rendering

**Files:**
- Modify: `frontend/src/stores/workbenchTabs.ts`
- Modify: `frontend/src/workbench/tabs/TabStrip.tsx`

This is one task, not two, because `TabStrip.tsx` calls `onReorder` (typed from the store's `reorderTab`) — changing the store's signature without updating `TabStrip.tsx` in the same commit leaves the build broken, and there's no intermediate state where both files independently typecheck.

- [x] **Step 1: Change `reorderTab`'s type and implementation in the store**

Find:

```ts
  pinTab: (id: string) => void;
  reorderTab: (fromId: string, toIndex: number) => void;
};
```

Replace:

```ts
  pinTab: (id: string) => void;
  reorderTab: (fromId: string, beforeId: string | null) => void;
};
```

Find:

```ts
  reorderTab: (fromId, toIndex) =>
    set((state) => {
      const fromIndex = state.openTabs.findIndex((tab) => tab.id === fromId);
      if (fromIndex === -1 || fromIndex === toIndex) {
        return state;
      }
      const openTabs = state.openTabs.slice();
      const [moved] = openTabs.splice(fromIndex, 1);
      openTabs.splice(toIndex, 0, moved);
      return { openTabs };
    }),
```

Replace:

```ts
  // beforeId (not a numeric index) because callers now only ever have a
  // subset of openTabs rendered (one expanded module group at a time) —
  // an id stays correct regardless of what subset produced the drag,
  // where a splice index would have to be translated back into the full
  // array's index space and would be easy to get wrong.
  reorderTab: (fromId, beforeId) =>
    set((state) => {
      if (fromId === beforeId) {
        return state;
      }
      const fromIndex = state.openTabs.findIndex((tab) => tab.id === fromId);
      if (fromIndex === -1) {
        return state;
      }
      const openTabs = state.openTabs.slice();
      const [moved] = openTabs.splice(fromIndex, 1);
      const insertAt = beforeId
        ? openTabs.findIndex((tab) => tab.id === beforeId)
        : -1;
      if (insertAt === -1) {
        openTabs.push(moved);
      } else {
        openTabs.splice(insertAt, 0, moved);
      }
      return { openTabs };
    }),
```

- [x] **Step 2: Rewrite `TabStrip.tsx` in full**

Replace the full contents of `frontend/src/workbench/tabs/TabStrip.tsx` with:

```tsx
import { useEffect, useRef, useState } from "react";
import { AnimatePresence, Reorder } from "framer-motion";
import { ChevronDown, Wrench, X } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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
  }

  function handleDragEnd() {
    const draggedId = activeDragId.current;
    setDraggingTabId(null);
    setHoveredTabId(null);
    if (draggedId !== null) {
      const index = visualTabsRef.current.findIndex((tab) => tab.id === draggedId);
      const beforeId = visualTabsRef.current[index + 1]?.id ?? null;
      // The store change commits once per completed gesture, not once per
      // crossed tab. Heavy module views therefore remain isolated from the
      // high-frequency drag path.
      onReorder(draggedId, beforeId);
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
 * Clicking opens a menu: "Show all tabs" pins the group open without
 * switching the active tab (peek), or pick a specific tab to switch to it
 * directly — no blind "always opens the first tab" default.
 */
function CollapsedChip({ group, onSelect, onExpand }: CollapsedChipProps) {
  const moduleTitle = getModule(group.moduleId)?.title ?? group.moduleId;
  const ModuleIcon = group.tabs[0].icon ?? getModule(group.moduleId)?.icon ?? Wrench;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          aria-label={`${moduleTitle} — ${group.tabs.length} open tab${group.tabs.length === 1 ? "" : "s"}`}
          className="flex h-6 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-muted-foreground transition-colors hover:bg-foreground/5 hover:text-foreground [--wails-draggable:no-drag]"
        >
          <ModuleIcon className="size-3.5 shrink-0 opacity-70" />
          {group.tabs.length > 1 ? (
            <span className="rounded-full bg-foreground/10 px-1.5 text-[10px] leading-4">
              {group.tabs.length}
            </span>
          ) : null}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuItem onClick={onExpand}>Show all tabs</DropdownMenuItem>
        <DropdownMenuSeparator />
        {group.tabs.map((tab) => (
          <DropdownMenuItem key={tab.id} onClick={() => onSelect(tab.id)}>
            {tab.title}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
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
      {groups.map((group) => {
        if (!expandedModuleIds.has(group.moduleId)) {
          return (
            <CollapsedChip
              key={group.moduleId}
              group={group}
              onSelect={onSelect}
              onExpand={() => pinModule(group.moduleId)}
            />
          );
        }

        const isActiveGroup = group.moduleId === activeModuleId;
        return (
          <div key={group.moduleId} className="flex shrink-0 items-center gap-1">
            <ExpandedGroup
              tabs={group.tabs}
              activeTabId={activeTabId}
              onSelect={onSelect}
              onClose={onClose}
              onReorder={onReorder}
            />
            {isActiveGroup ? null : (
              // Only a manually-pinned (non-active) expanded group can be
              // collapsed back — the active module's own group has no
              // collapse control, since that would hide the tab the user
              // is currently looking at.
              <button
                type="button"
                aria-label={`Collapse ${getModule(group.moduleId)?.title ?? group.moduleId}`}
                onClick={() => unpinModule(group.moduleId)}
                className="flex size-5 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-foreground/5 hover:text-foreground [--wails-draggable:no-drag]"
              >
                <ChevronDown className="size-3.5" />
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
}
```

- [x] **Step 3: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds with no TypeScript errors. `Workbench.tsx` isn't touched
by this task — its `<TabStrip onReorder={reorderTab} />` wiring must still
typecheck cleanly since both sides now agree on the `(fromId, beforeId: string | null) => void`
signature.

- [x] **Step 4: Commit**

```bash
git add frontend/src/stores/workbenchTabs.ts frontend/src/workbench/tabs/TabStrip.tsx
git commit -m "feat(workbench): group tab strip by module with collapsible chips"
```

---

### Task 2: Manual QA

No code changes. Start the app (`task dev:frontend`, or whatever this
session's `run` skill resolves to for dev-kit-app) and, in the running app:

1. **Grouping renders correctly.** Open 5 notes (5 separate Notes tabs via
   double-click in the Notes dashboard so each is pinned, not preview),
   then switch to Database. Notes collapses to one chip (icon + "5"
   badge); Database's own tab renders normally with no badge (exactly one
   tab in that group).
2. **Peek without switching.** While on Database, click the Notes chip →
   dropdown lists all 5 note titles plus a "Show all tabs" item at the
   top, active tab is still the Database tab. Click "Show all tabs" →
   Notes' 5 tabs expand in place in the strip; Database is still the
   active tab and its group is still expanded too — confirm **two groups
   are expanded simultaneously** (this is the split-view-readiness check
   from the spec).
3. **Manually collapse the peeked group.** With Notes expanded-but-not-active
   (from step 2), click the chevron next to Notes' group → it collapses
   back to a chip. Database (still active) is unaffected.
4. **Pick a specific tab from the menu.** Click the (now collapsed again)
   Notes chip, pick one specific note title directly from the list (not
   "Show all tabs") → that exact note becomes the active tab, Notes'
   group expands with that note focused/highlighted. Database's group
   correctly collapses to a chip (it's not pinned, and it's no longer
   active).
5. **Active group has no collapse control.** While any module is active
   and expanded, confirm there is no chevron/control next to its own
   group — only non-active expanded (pinned) groups show one.
6. **Drag stays within a group.** With Notes expanded (multiple tabs),
   drag one note tab to sit after another — order updates live, same feel
   as before this change. Confirm there's no way to drag a Notes tab out
   into Database's chip/space.
7. **Reorder persists correctly.** After reordering Notes tabs in step 6,
   switch away to Database and back to Notes (via its chip or the Dock) —
   confirm the new order is still there (proves `reorderTab`'s new
   `beforeId` logic actually persisted to the store, not just the local
   visual state).
8. **Badge omitted at 1.** Any module with exactly one open tab (Database,
   SSH, Kafka in typical use) shows its collapsed chip with icon only, no
   numeric badge.
9. **Close still works inside an expanded group.** With a group expanded,
   hover a tab and click its close (×) button — closes just that tab,
   rest of the group's tabs and their order are unaffected.
