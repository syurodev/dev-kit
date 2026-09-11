# Workbench Tab Strip (Phase 1: Infrastructure) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the single "active module" switch in `Workbench.tsx` with an open/close tab model rendered as a horizontally-scrollable strip inside the native macOS title bar (between the traffic lights and the header icons), with a generic per-tab status field ready for live updates.

**Architecture:** A new Zustand store (`useWorkbenchTabsStore`) owns `openTabs` + `activeTabId`, mirroring the existing `useAgentSessionsStore`/`useDockStore` pattern already in this codebase. Opening a tab is `{moduleId, viewId, title, params?}` deduped by a stable key; the content area mounts every open tab's view component simultaneously and toggles visibility via CSS (`contents`/`hidden`) instead of unmounting, so a module's in-progress state survives switching away and back. `ModuleView.component` gains an optional `params` prop (backward compatible — existing zero-arg view components remain assignable). The dock and the header Settings icon both go through one `focusModule()` helper that opens-or-focuses a module's first view as a tab.

**Explicit scope boundary (read this before questioning why X isn't here):** This phase ships the tab strip UI, the drag-region fix, and one tab per *module* (click a dock icon → get/focus that module's tab). It does **not** yet give Notes a tab per note or Agent Hub a tab per session, and does not yet wire live agent-session status onto tab dots — those are two separate follow-up specs (per the writing-plans skill's guidance to split independent subsystems) because they each require refactoring how `NotesView` and `AgentHubView` manage state internally, not just workbench plumbing. The `status` field and `setTabStatus` action are built now because the tab strip needs to render them either way, and it is cheaper to build the generic primitive once than to bolt it on later.

**Tech Stack deviation from the skill default:** This repo's frontend has **zero unit test infrastructure** (no vitest, no `*.test.ts` anywhere, confirmed by search) and `AGENTS.md` states the frontend check is `cd frontend && bun run build` (tsc + vite build) — not unit tests. Per the instruction-priority rule (user/repo conventions in `AGENTS.md` outrank the skill's default "write a failing test" template), each task below verifies with a typecheck/build step instead, and Task 5 is a concrete manual QA script that specifically exercises the branchy bits (tab dedupe, close-tab fallback selection) since ponytail still wants one runnable check for non-trivial logic and an automated one isn't available here.

---

## File Structure

Create:
- `frontend/src/stores/workbenchTabs.ts` — `WorkbenchTab`/`TabStatus` types, `tabKey()`, the `useWorkbenchTabsStore` Zustand store (open/close/focus/status actions).
- `frontend/src/workbench/tabs/TabStrip.tsx` — the horizontally-scrollable tab row rendered inside `<header>`.

Modify:
- `frontend/src/modules/registry.ts:5-9` — `ModuleView.component` accepts optional `params`.
- `frontend/src/index.css:93-99` — add `--status-success` next to the existing status-color vocabulary.
- `frontend/src/workbench/Workbench.tsx` — swap `activeModuleId` state for the tabs store, restructure `<header>` to host `<TabStrip>` plus the Wails drag-region CSS, render all open tabs (visibility-toggled) instead of one `ActiveView`, route the Dock and the Settings icon through one `focusModule()` helper.

---

### Task 1: Let module views accept tab params

**Files:**
- Modify: `frontend/src/modules/registry.ts:5-9`

- [ ] **Step 1: Widen the `ModuleView.component` type**

In `frontend/src/modules/registry.ts`, change:

```ts
export type ModuleView = {
  id: string;
  title: string;
  component: ComponentType;
};
```

to:

```ts
export type ModuleView = {
  id: string;
  title: string;
  component: ComponentType<{ params?: Record<string, unknown> }>;
};
```

This is backward compatible: every existing view component (`NotesView`, `DatabaseView`, `ToolsView`, `SSHView`, `KafkaView`, `AgentHubView`, `SyncView`, `SettingsView`) is declared as a zero-argument function component, and a function that ignores its arguments is structurally assignable to a type that expects one optional prop. No other file needs to change for this step.

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds with no new TypeScript errors (this step only widens a type, it can't narrow anything that was previously valid).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/registry.ts
git commit -m "feat(workbench): allow module views to accept tab params"
```

---

### Task 2: Workbench tabs store

**Files:**
- Create: `frontend/src/stores/workbenchTabs.ts`

- [ ] **Step 1: Write the store**

```ts
import { create } from "zustand";

export type TabStatus = "idle" | "running" | "success" | "error";

export type WorkbenchTab = {
  id: string;
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  status?: TabStatus;
};

export type OpenTabInput = {
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
};

/**
 * Stable identity for a tab: same moduleId+viewId+params always resolves to
 * the same tab, so opening an already-open note (or module) focuses the
 * existing tab instead of duplicating it. Params are sorted by key so
 * `{a:1,b:2}` and `{b:2,a:1}` collide on purpose.
 */
export function tabKey(
  moduleId: string,
  viewId: string,
  params?: Record<string, unknown>,
): string {
  const keys = params ? Object.keys(params).sort() : [];
  if (keys.length === 0) {
    return `${moduleId}:${viewId}`;
  }
  const suffix = keys.map((key) => `${key}=${String(params![key])}`).join("&");
  return `${moduleId}:${viewId}:${suffix}`;
}

type WorkbenchTabsState = {
  openTabs: WorkbenchTab[];
  activeTabId: string;
  openTab: (input: OpenTabInput) => string;
  closeTab: (id: string) => void;
  setActiveTab: (id: string) => void;
  setTabStatus: (id: string, status: TabStatus) => void;
};

export const useWorkbenchTabsStore = create<WorkbenchTabsState>((set, get) => ({
  openTabs: [],
  activeTabId: "",

  openTab: (input) => {
    const id = tabKey(input.moduleId, input.viewId, input.params);
    const existing = get().openTabs.find((tab) => tab.id === id);
    if (existing) {
      set({ activeTabId: id });
      return id;
    }
    const tab: WorkbenchTab = {
      id,
      moduleId: input.moduleId,
      viewId: input.viewId,
      title: input.title,
      params: input.params,
    };
    set((state) => ({ openTabs: [...state.openTabs, tab], activeTabId: id }));
    return id;
  },

  closeTab: (id) => {
    set((state) => {
      const index = state.openTabs.findIndex((tab) => tab.id === id);
      if (index === -1) {
        return state;
      }
      const openTabs = state.openTabs.filter((tab) => tab.id !== id);
      if (state.activeTabId !== id) {
        return { openTabs };
      }
      // Closing the active tab focuses its former left neighbor, matching
      // VS Code — falling back to the new first tab if we closed index 0.
      const fallback = openTabs[index - 1] ?? openTabs[0];
      return { openTabs, activeTabId: fallback?.id ?? "" };
    });
  },

  setActiveTab: (id) => set({ activeTabId: id }),

  setTabStatus: (id, status) =>
    set((state) => ({
      openTabs: state.openTabs.map((tab) => (tab.id === id ? { ...tab, status } : tab)),
    })),
}));
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds — this file has no external dependents yet, so a clean compile just confirms the store itself is well-typed.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/stores/workbenchTabs.ts
git commit -m "feat(workbench): add workbench tabs store"
```

---

### Task 3: Tab strip UI

**Files:**
- Modify: `frontend/src/index.css:93-99`
- Create: `frontend/src/workbench/tabs/TabStrip.tsx`

- [ ] **Step 1: Add the missing status color token**

`frontend/src/index.css` already defines a status-color vocabulary for Agent Hub (`--status-running`, `--status-permission`, `--status-idle`, `--status-error`) reused via `bg-(--status-running)` etc. in `AgentHubDockPanel.tsx` and `DockItem.tsx`. Tabs need one more state this vocabulary doesn't have yet: a finished-successfully state. In `frontend/src/index.css`, change:

```css
    --status-running: #57c1ff;
    --status-permission: #ffc533;
    --status-permission-soft: rgba(255, 197, 51, 0.18);
    --status-idle: #434345;
    --status-error: #ff6161;
```

to:

```css
    --status-running: #57c1ff;
    --status-permission: #ffc533;
    --status-permission-soft: rgba(255, 197, 51, 0.18);
    --status-idle: #434345;
    --status-success: #22c55e;
    --status-error: #ff6161;
```

- [ ] **Step 2: Write the tab strip**

```tsx
import { X } from "lucide-react";
import { cn } from "@/lib/utils";
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
};

/**
 * Lives inside the 50px native-titlebar strip (Mac.InvisibleTitleBarHeight
 * in main.go). The container defaults to `--wails-draggable: drag` so the
 * empty space after the last tab still moves the window like the rest of
 * the header; every clickable element inside overrides it back to no-drag,
 * same pattern Wails' own frameless-window docs use.
 */
export function TabStrip({ tabs, activeTabId, onSelect, onClose }: TabStripProps) {
  return (
    <div className="scrollbar-hide flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [--wails-draggable:drag]">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          onClick={() => onSelect(tab.id)}
          className={cn(
            "group flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium transition-colors [--wails-draggable:no-drag]",
            tab.id === activeTabId
              ? "bg-card text-foreground"
              : "text-muted-foreground hover:bg-white/5 hover:text-foreground",
          )}
        >
          {tab.status && tab.status !== "idle" ? (
            <span
              className={cn("size-1.5 shrink-0 rounded-full", statusDotClass(tab.status))}
            />
          ) : null}
          <span className="max-w-40 truncate">{tab.title}</span>
          <span
            role="button"
            aria-label={`Close ${tab.title}`}
            onClick={(event) => {
              event.stopPropagation();
              onClose(tab.id);
            }}
            className="ml-0.5 hidden size-4 shrink-0 items-center justify-center rounded hover:bg-white/10 group-hover:flex [--wails-draggable:no-drag]"
          >
            <X className="size-3" />
          </span>
        </button>
      ))}
    </div>
  );
}
```

`scrollbar-hide` is an existing utility class (`@plugin "tailwind-scrollbar-hide"` in `index.css`, already used in `fixed-toolbar.tsx`/`floating-toolbar.tsx`/`table-node.tsx`) — no new CSS needed for hiding the scrollbar. Trackpad two-finger horizontal swipe scrolls `overflow-x-auto` natively in WKWebView; no extra JS wheel handler in this phase.

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds — `TabStrip` isn't imported anywhere yet, so this just confirms the component itself compiles.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/index.css frontend/src/workbench/tabs/TabStrip.tsx
git commit -m "feat(workbench): add tab strip component"
```

---

### Task 4: Wire the tab strip into Workbench

**Files:**
- Modify: `frontend/src/workbench/Workbench.tsx`

- [ ] **Step 1: Update imports**

Add to the top of `frontend/src/workbench/Workbench.tsx`:

```ts
import { useWorkbenchTabsStore } from "../stores/workbenchTabs";
import { TabStrip } from "./tabs/TabStrip";
```

And add `getModule` to the existing registry import:

```ts
import {
  compareModuleParity,
  CURRENT_MODULE_API_VERSION,
  getModule,
  listRegisteredModules,
  registerModule,
} from "../modules/registry";
```

- [ ] **Step 2: Replace `activeModuleId` state with the tabs store**

Remove:

```ts
const [activeModuleId, setActiveModuleId] = useState(modules[0]?.id ?? "");
```

Add in its place (same spot, alongside the other `useState`/store hooks near the top of the component body):

```ts
const openTabs = useWorkbenchTabsStore((state) => state.openTabs);
const activeTabId = useWorkbenchTabsStore((state) => state.activeTabId);
const openTab = useWorkbenchTabsStore((state) => state.openTab);
const closeTab = useWorkbenchTabsStore((state) => state.closeTab);
const setActiveTab = useWorkbenchTabsStore((state) => state.setActiveTab);
```

- [ ] **Step 3: Replace `activeModule`/`ActiveView` derivation**

Find:

```ts
const activeModule =
  availableModules.find((module) => module.id === activeModuleId) ??
  availableModules[0];
const activeView = activeModule?.views[0];
const ActiveView = activeView?.component;
```

Replace with:

```ts
const activeTab = openTabs.find((tab) => tab.id === activeTabId);
const activeModule =
  availableModules.find((module) => module.id === activeTab?.moduleId) ??
  availableModules[0];

function focusModule(moduleId: string) {
  const module = getModule(moduleId);
  const view = module?.views[0];
  if (module && view) {
    openTab({ moduleId: module.id, viewId: view.id, title: module.title });
  }
}

useEffect(() => {
  if (openTabs.length === 0 && availableModules.length > 0) {
    focusModule(availableModules[0].id);
  }
}, [availableModules, openTabs.length]);
```

This preserves today's behavior of landing directly on the first registered module's view on launch (no blank "no tabs open" state on first run), just routed through `openTab` instead of a raw `useState` default.

- [ ] **Step 4: Rewrite the header**

Find the whole `<header>` block:

```tsx
      <header className="flex h-[50px] shrink-0 items-center gap-2 pl-24 pr-4">
        <div className="flex size-6 items-center justify-center rounded-md bg-primary text-primary-foreground">
          <Bot className="size-3.5" />
        </div>
        <strong className="text-sm font-semibold">
          {activeModule?.title ?? "DevKit"}
        </strong>
        {metadataLoading ? (
          <span role="status" className="text-xs text-muted-foreground">
            Loading module metadata...
          </span>
        ) : null}
        <div className="flex-1" />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Commands"
              onClick={() => setPaletteOpen(true)}
            >
              <Search className="size-4" />
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
              size="icon-sm"
              aria-label="Settings"
              onClick={() => setActiveModuleId("settings")}
            >
              <SettingsIcon className="size-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            Settings <Kbd>⌘,</Kbd>
          </TooltipContent>
        </Tooltip>
      </header>
```

Replace with:

```tsx
      {/* Header root defaults to draggable so empty space still moves the
          window (Wails needs this set explicitly — there is no built-in
          drag region under web content in hiddenInset mode). TabStrip and
          every button below override back to no-drag on themselves. */}
      <header className="flex h-[50px] shrink-0 items-center gap-2 pl-24 pr-4 [--wails-draggable:drag]">
        <TabStrip
          tabs={openTabs}
          activeTabId={activeTabId}
          onSelect={setActiveTab}
          onClose={closeTab}
        />
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
              size="icon-sm"
              aria-label="Commands"
              className="shrink-0 [--wails-draggable:no-drag]"
              onClick={() => setPaletteOpen(true)}
            >
              <Search className="size-4" />
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
              size="icon-sm"
              aria-label="Settings"
              className="shrink-0 [--wails-draggable:no-drag]"
              onClick={() => focusModule("settings")}
            >
              <SettingsIcon className="size-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            Settings <Kbd>⌘,</Kbd>
          </TooltipContent>
        </Tooltip>
      </header>
```

The brand icon box and the `<strong>` module-title text are dropped: tabs now carry each open item's identity, so a separate fixed title next to them would just be a second, redundant label competing with the active tab for the same information (this mirrors VS Code's title bar, which has no such label either). `Bot` becomes unused — remove it from the `lucide-react` import at the top of the file.

- [ ] **Step 5: Replace the content-render section**

Find:

```tsx
        <section
          className={cn(
            "flex min-h-0 flex-1 flex-col overflow-auto",
            activeModule?.id === "notes" ? "p-0" : "p-4",
          )}
        >
          {ActiveView ? (
            <ActiveView />
          ) : (
            <p role="status" className="text-sm text-muted-foreground">
              {availableModules.length === 0
                ? "No modules are currently available."
                : "No view is currently available."}
            </p>
          )}
        </section>
```

Replace with:

```tsx
        <section
          className={cn(
            "flex min-h-0 flex-1 flex-col overflow-auto",
            activeModule?.id === "notes" ? "p-0" : "p-4",
          )}
        >
          {openTabs.length === 0 ? (
            <p role="status" className="text-sm text-muted-foreground">
              {availableModules.length === 0
                ? "No modules are currently available."
                : "No view is currently available."}
            </p>
          ) : (
            openTabs.map((tab) => {
              const module = getModule(tab.moduleId);
              const view = module?.views.find((candidate) => candidate.id === tab.viewId);
              if (!view) {
                return null;
              }
              const TabView = view.component;
              // `contents` (not a fragment) keeps the active view's own
              // flex/overflow classes participating directly in this
              // section's layout, exactly as the single <ActiveView/> did
              // before. Inactive tabs stay mounted so in-progress state
              // (a running query, an unsaved draft) survives switching away.
              return (
                <div key={tab.id} className={tab.id === activeTabId ? "contents" : "hidden"}>
                  <TabView params={tab.params} />
                </div>
              );
            })
          )}
        </section>
```

- [ ] **Step 6: Update the Dock's `onSelectModule`**

Find:

```tsx
      <Dock
        activeModuleId={activeModule?.id ?? ""}
        modules={dockModules}
        onSelectModule={setActiveModuleId}
      />
```

Replace with:

```tsx
      <Dock
        activeModuleId={activeModule?.id ?? ""}
        modules={dockModules}
        onSelectModule={focusModule}
      />
```

- [ ] **Step 7: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds with no TypeScript errors and no unused-import warnings (confirm `Bot` was actually removed from the `lucide-react` import in step 4 — `tsc` will flag it as unused if not).

- [ ] **Step 8: Commit**

```bash
git add frontend/src/workbench/Workbench.tsx
git commit -m "feat(workbench): drive header from the tabs store"
```

---

### Task 5: Manual QA

No code changes. This repo has no frontend unit tests (see Tech Stack deviation above), so this is the actual verification pass — run it before considering the plan done.

- [ ] **Step 1: Launch the app**

Run: `task dev:frontend` (or the `run` skill) and open the app window.

- [ ] **Step 2: Verify drag region**

Click and drag on the empty header space to the right of the last tab (or, if tabs fill the width, the `pl-24` gap left of the first tab). Expected: the window moves. This is the regression check for the drag-region bug found during design — before this plan, dragging that strip did nothing because no `--wails-draggable` CSS existed anywhere in the frontend.

- [ ] **Step 3: Verify tab open/focus/dedupe**

Click each Dock icon once (Notes, Database, SSH, Kafka, Agent Hub, Tools, Sync-via-Settings). Expected: one tab appears per module, clicking the same Dock icon again focuses the existing tab instead of creating a second one with the same title.

- [ ] **Step 4: Verify horizontal scroll**

With 5+ tabs open, shrink the window width (or open enough tabs to overflow). Expected: the tab strip scrolls horizontally with a two-finger trackpad swipe, no vertical page scroll, no visible scrollbar track.

- [ ] **Step 5: Verify close + fallback focus**

Open tabs A, B, C in that order (A active first, then B, then C — C ends up active). Click back to focus B, then close B. Expected: A becomes active (the "former left neighbor" fallback in `closeTab`). Then close A (now the leftmost of the two remaining). Expected: whatever tab is now first becomes active, never a blank content area while any tab remains open.

- [ ] **Step 6: Verify state survives tab switching**

Open the Database tab, type something into the SQL box, switch to another tab, switch back. Expected: the SQL box still has what you typed — content is hidden via CSS, not unmounted.

- [ ] **Step 7: Verify Settings icon still works**

Click the header Settings (gear) icon. Expected: opens/focuses a "Settings" tab exactly like a Dock click would.
