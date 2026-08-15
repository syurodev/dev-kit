# Titlebar 3-Column Layout + Fisheye/Color Tab Strip Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the collapse-to-chip tab strip with one where every tab always renders individually — clustered and colored by module, narrowing by width (never scaling/hiding) the farther a tab sits from the active one, fully readable on hover — inside a titlebar restructured into an even 3-column (status / tabs / actions) layout.

**Architecture:** `TabStrip.tsx` drops `CollapsedChip`/`pinnedModuleIds`/the collapse chevron entirely. `groupTabs()` (kept) still clusters same-module tabs contiguously; a new `moduleColor.ts` derives a stable hue per `moduleId` via string hashing (no manifest changes). Each tab's background becomes `hsl(hue 55% 45% / alpha)` — 0.7 alpha active, 0.3 inactive. Width is computed once per render in `TabStrip` over the *visual* (post-clustering) tab order: the active tab is never width-constrained (always shows its full title); every other tab gets `100% - 5%×distance` of a 140px base, floored at 30%, animated via each `Reorder.Item`'s `animate={{ width }}` (a real tween, not a `layout` FLIP, since this is a genuine size change) — `layout="position"` stays on the same items purely to animate the position shifts those width changes (or a drag) cause among siblings. Hovering anywhere over the tab-strip viewport forces every tab's distance to 0 (same formula, same code path — no separate "expanded" state), which is exactly how "all tabs render at equal, fully-readable width" falls out for free. `Workbench.tsx`'s header wraps its content in a `grid-cols-[1fr_3fr_1fr]` (left status placeholder / tab strip / right action-button cluster).

**Tech Stack deviation from the skill default:** no frontend unit test runner in this repo — verification is `cd frontend && bun run build` (tsc + vite) plus a manual QA pass (final task).

**Builds on:** `docs/superpowers/specs/2026-08-15-titlebar-tabstrip-redesign-design.md` (the approved design this plan implements — read it for the full Decisions/Non-goals record, including why width (not `scale`) and why Grid (not flex-grow ratios), if anything here seems under-motivated).

---

## File Structure

Create:
- `frontend/src/workbench/tabs/moduleColor.ts` — `moduleHue(moduleId): number`, a pure string-hash-to-hue function.

Modify:
- `frontend/src/workbench/tabs/TabStrip.tsx` — full rewrite: drops the chip/pin mechanism, adds fisheye width + module color, renames `ExpandedGroup` to `TabGroupRow` (there's no longer a "collapsed" alternative for it to be "expanded" relative to).
- `frontend/src/workbench/Workbench.tsx` — header restructured into a 3-column grid; `<TabStrip>` and the action-button cluster move inside it, a new empty status placeholder `<div>` is added as the first column.

No files deleted (the design doc for the superseded chip spec stays in `docs/` as history, per how every other spec in this series has been kept rather than removed).

---

### Task 1: `moduleHue` — per-module color derivation

**Files:**
- Create: `frontend/src/workbench/tabs/moduleColor.ts`

- [ ] **Step 1: Write the file**

```ts
/**
 * Deterministic hue (0-359) for a module id, used to give every tab in
 * that module's cluster a consistent background color without touching
 * every module's manifest (unlike `icon`, this isn't module-authored —
 * two authors picking clashing colors independently isn't a risk worth
 * taking on for a purely decorative grouping cue).
 */
export function moduleHue(moduleId: string): number {
  let hash = 0;
  for (let i = 0; i < moduleId.length; i++) {
    hash = (hash * 31 + moduleId.charCodeAt(i)) | 0;
  }
  return Math.abs(hash) % 360;
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds (this file has no consumers yet, but it's a valid,
fully-typed, exported function — nothing to fail on).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/workbench/tabs/moduleColor.ts
git commit -m "feat(workbench): add per-module color hashing"
```

---

### Task 2: Rewrite `TabStrip.tsx` — drop chip, add fisheye width + color

**Files:**
- Modify: `frontend/src/workbench/tabs/TabStrip.tsx`

`TabStripProps` (the component's public interface) doesn't change shape,
so this task is independently typecheck-clean without touching
`Workbench.tsx` — Task 3 is a separate, unrelated change to that file.

- [ ] **Step 1: Replace the full contents of the file**

```tsx
import { useEffect, useRef, useState } from "react";
import { AnimatePresence, Reorder } from "framer-motion";
import { Wrench, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { getModule } from "../../modules/registry";
import { moduleHue } from "./moduleColor";
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

const FISHEYE_STEP_PERCENT = 5;
const FISHEYE_FLOOR_PERCENT = 30;
const TAB_BASE_WIDTH_PX = 140;

/** Width for a tab `distance` positions away from the active tab. */
function fisheyeWidthPx(distance: number): number {
  const percent = Math.max(
    FISHEYE_FLOOR_PERCENT,
    100 - distance * FISHEYE_STEP_PERCENT,
  );
  return Math.round((TAB_BASE_WIDTH_PX * percent) / 100);
}

type TabGroupRowProps = {
  tabs: WorkbenchTab[];
  activeTabId: string;
  widths: Map<string, number | undefined>;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onReorder: (fromId: string, beforeId: string | null) => void;
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
  onSelect,
  onClose,
  onReorder,
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
        {visualTabs.map((tab) => {
          const hue = moduleHue(tab.moduleId);
          const isActive = tab.id === activeTabId;
          const widthPx = widths.get(tab.id);
          return (
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
              // opacity/scale here (not just width) are what drive the
              // AnimatePresence enter/exit — dropping them from `animate`
              // would leave a newly-appeared tab stuck at its `initial`
              // opacity/scale forever, since nothing would ever animate it
              // to its resting state. "auto" for the active tab: Motion
              // measures the actual rendered width and tweens toward it,
              // rather than us trying to compute "however long this title
              // needs" ourselves.
              animate={{ opacity: 1, scale: 1, width: widthPx ?? "auto" }}
              transition={{
                layout: { duration: 0.045, ease: "easeOut" },
                width: { duration: 0.18, ease: "easeOut" },
                opacity: { duration: 0.18 },
                scale: { duration: 0.18 },
              }}
              style={{
                backgroundColor: `hsl(${hue} 55% 45% / ${isActive ? 0.7 : 0.3})`,
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
              exit={{ opacity: 0, scale: 0.9 }}
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
                "group relative flex h-6 shrink-0 items-center overflow-hidden rounded-md py-0 pl-2 pr-7 text-xs font-medium transition-[filter] [--wails-draggable:no-drag]",
                !isActive && hoveredTabId === tab.id && draggingTabId === null
                  ? "brightness-125"
                  : "",
                isActive ? "text-foreground" : "text-muted-foreground",
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
                className={cn("ml-1.5 truncate", tab.preview && "italic")}
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
}: TabStripProps) {
  const [isBarHovered, setIsBarHovered] = useState(false);

  const groups = groupTabs(tabs);
  // Distance is measured over this clustered order (not raw `tabs`
  // order) because "close to the active tab" means spatial left-right
  // adjacency in what's actually rendered, and clustering can reorder
  // that relative to openTabs' own (drag-reorderable) order.
  const visualOrder = groups.flatMap((group) => group.tabs);
  const activeIndex = visualOrder.findIndex((tab) => tab.id === activeTabId);

  const widths = new Map<string, number | undefined>();
  visualOrder.forEach((tab, index) => {
    if (isBarHovered) {
      // Hovering the strip suspends the fisheye for every tab, including
      // the active one — "toàn bộ tab bung ra cùng kích thước": same
      // fixed width for all, not each tab's own natural width.
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
    <div
      onPointerEnter={() => setIsBarHovered(true)}
      onPointerLeave={() => setIsBarHovered(false)}
      className="scrollbar-hide flex min-w-0 items-center gap-1 overflow-x-auto [--wails-draggable:no-drag]"
    >
      {groups.map((group) => (
        <TabGroupRow
          key={group.moduleId}
          tabs={group.tabs}
          activeTabId={activeTabId}
          widths={widths}
          onSelect={onSelect}
          onClose={onClose}
          onReorder={onReorder}
        />
      ))}
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds. This is the step that proves `TabStripProps` really is
unchanged (no fallout in `Workbench.tsx`, which this task doesn't touch)
and that `moduleHue` (Task 1) is correctly imported and used — an unused
import would fail here too, since `tsconfig.json` has `noUnusedLocals: true`.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/workbench/tabs/TabStrip.tsx
git commit -m "feat(workbench): fisheye width + per-module color, drop tab collapse"
```

---

### Task 3: Titlebar 3-column layout

**Files:**
- Modify: `frontend/src/workbench/Workbench.tsx`

**Step 1: Wrap the header's content in a 3-column grid**

Find:

```tsx
        <header
          className={cn(
            "relative flex h-[30px] shrink-0 items-center gap-2 pt-0.5 pr-4 transition-[padding-left] duration-300 ease-out motion-reduce:transition-none [--wails-draggable:no-drag]",
            isFullscreen ? "pl-1" : "pl-20",
          )}
        >
          <div
            aria-hidden="true"
            className={cn(
              "absolute inset-y-0 left-0 transition-[width] duration-300 ease-out motion-reduce:transition-none [--wails-draggable:drag]",
              isFullscreen ? "w-1" : "w-20",
            )}
          />
          <TabStrip
            tabs={openTabs}
            activeTabId={activeTabId}
            onSelect={setActiveTab}
            onClose={closeTab}
            onReorder={reorderTab}
          />
          {moduleActions.map((action) => (
            <Tooltip key={action.id}>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label={action.label}
                  aria-pressed={action.isActive}
                  className={cn(
                    action.isActive && "bg-accent text-foreground",
                    "shrink-0 [--wails-draggable:no-drag]",
                  )}
                  onClick={action.onClick}
                >
                  <action.icon className="size-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="bottom">{action.label}</TooltipContent>
            </Tooltip>
          ))}
          {metadataLoading ? (
            <span
              role="status"
              className="shrink-0 text-xs text-muted-foreground [--wails-draggable:no-drag]"
            >
              Loading module metadata...
            </span>
          ) : null}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label="Commands"
                className="shrink-0 [--wails-draggable:no-drag]"
                onClick={() => setPaletteOpen(true)}
              >
                <Search className="size-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              Commands <Kbd>⌘K</Kbd>
            </TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-xs"
                aria-label="Settings"
                className="shrink-0 [--wails-draggable:no-drag]"
                onClick={() => focusModule("settings")}
              >
                <SettingsIcon className="size-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">
              Settings <Kbd>⌘,</Kbd>
            </TooltipContent>
          </Tooltip>
        </header>
```

Replace:

```tsx
        <header
          className={cn(
            "relative flex h-[30px] shrink-0 items-center pt-0.5 pr-4 transition-[padding-left] duration-300 ease-out motion-reduce:transition-none [--wails-draggable:no-drag]",
            isFullscreen ? "pl-1" : "pl-20",
          )}
        >
          <div
            aria-hidden="true"
            className={cn(
              "absolute inset-y-0 left-0 transition-[width] duration-300 ease-out motion-reduce:transition-none [--wails-draggable:drag]",
              isFullscreen ? "w-1" : "w-20",
            )}
          />
          {/* 1fr/3fr/1fr, not flex-grow ratios: Grid's fr unit divides the
              full track width by ratio regardless of each column's own
              content size, which is what an even 1:3:1 split actually
              requires — flex-grow only distributes space left over after
              content sizing, so an empty left column wouldn't reliably end
              up the same width as a busy right column under flex. */}
          <div className="grid w-full grid-cols-[1fr_3fr_1fr] items-center gap-2">
            {/* Status column — reserved layout space only. Content (agent
                status, index status, ...) isn't built yet. */}
            <div />
            <TabStrip
              tabs={openTabs}
              activeTabId={activeTabId}
              onSelect={setActiveTab}
              onClose={closeTab}
              onReorder={reorderTab}
            />
            <div className="flex min-w-0 items-center justify-end gap-2">
              {moduleActions.map((action) => (
                <Tooltip key={action.id}>
                  <TooltipTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label={action.label}
                      aria-pressed={action.isActive}
                      className={cn(
                        action.isActive && "bg-accent text-foreground",
                        "shrink-0 [--wails-draggable:no-drag]",
                      )}
                      onClick={action.onClick}
                    >
                      <action.icon className="size-3.5" />
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom">{action.label}</TooltipContent>
                </Tooltip>
              ))}
              {metadataLoading ? (
                <span
                  role="status"
                  className="shrink-0 text-xs text-muted-foreground [--wails-draggable:no-drag]"
                >
                  Loading module metadata...
                </span>
              ) : null}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label="Commands"
                    className="shrink-0 [--wails-draggable:no-drag]"
                    onClick={() => setPaletteOpen(true)}
                  >
                    <Search className="size-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  Commands <Kbd>⌘K</Kbd>
                </TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label="Settings"
                    className="shrink-0 [--wails-draggable:no-drag]"
                    onClick={() => focusModule("settings")}
                  >
                    <SettingsIcon className="size-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  Settings <Kbd>⌘,</Kbd>
                </TooltipContent>
              </Tooltip>
            </div>
          </div>
        </header>
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/workbench/Workbench.tsx
git commit -m "feat(workbench): restructure titlebar into 1fr/3fr/1fr grid"
```

---

### Task 4: Manual QA

No code changes. Start the app (`task dev:frontend`, or whatever this
session's `run` skill resolves to for dev-kit-app — note the frontend
can't run meaningfully in a plain browser tab for this project, since
`@wailsio/runtime` calls 404 outside the native Wails window; verify in
the actual running app) and, in the running app:

1. **3-column layout.** Titlebar shows an empty left third, tab strip
   centered in a track 3x as wide, action buttons (Commands, Settings,
   any module actions, the metadata-loading label if it appears)
   right-aligned in the final third. Resize the window — the 1:3:1 ratio
   holds.
2. **No collapse anywhere.** Open 8+ tabs across 3+ modules — every tab is
   individually visible and clickable at all times; there is no chip, no
   chevron, no hidden-tabs state.
3. **Clustering + color.** Notes tabs are visually contiguous and share
   one background hue; Database tabs are contiguous with a visibly
   different hue.
4. **Active vs inactive opacity.** The active tab's background reads
   noticeably more solid than its own module's inactive tabs.
5. **Fisheye falloff at rest.** With 6+ tabs open and the active one
   roughly in the middle, tabs get visibly narrower moving away from it in
   both directions, bottoming out (not continuing to shrink) a few tabs
   out. Icons stay full-size throughout; only titles truncate.
6. **Active tab never truncates.** Give the active tab a long title (a
   long note title, for instance) — it renders in full, no ellipsis,
   regardless of how many other tabs are open around it.
7. **Hover reveals everything.** Move the pointer onto the tab strip
   (anywhere) — all tabs (including the active one) animate to the same
   width. Move the pointer off — fisheye falloff resumes around whichever
   tab is active.
8. **Drag still scoped per module.** Dragging a tab only reorders it among
   its own module's tabs; there's no way to drag it into a different
   module's color cluster.
9. **Switching active tab re-centers the fisheye.** Click a tab far from
   the previous active tab — the falloff recomputes around the new active
   tab, animating each affected tab's width rather than snapping instantly.
