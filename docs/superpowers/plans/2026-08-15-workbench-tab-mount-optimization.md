# Workbench Tab Mount Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Only mount tab content for the currently active module (plus a small recently-used grace window and the always-exempt Settings/Sync), instead of keeping every tab ever opened mounted for the app's whole lifetime.

**Architecture:** A new `useMountedModuleIds` hook, local to `Workbench.tsx`, tracks an MRU list of non-exempt module ids: the active module always qualifies; up to 2 more recently-left modules stay mounted for a 2-minute grace period; a hard cap of 3 concurrently-mounted non-exempt modules evicts the least-recently-active one immediately if exceeded. `HEADER_ONLY_MODULE_IDS` (Settings, Sync) is unioned in separately and always mounted, untouched by the hook. `WorkbenchViews`'s existing `openTabs.map(...)` gains one early-return: tabs whose module isn't in the mounted set render `null` instead of their `<TabView>`. Nothing about `useWorkbenchTabsStore` or `TabStrip.tsx` changes — the tab list, order, and clickability are identical to today; only whether a tab's content is in the DOM changes.

**Tech Stack deviation from the skill default:** no frontend unit test runner in this repo — verification is `cd frontend && bun run build` (tsc + vite, with `noUnusedLocals: true` in `tsconfig.json`, which is why this plan is one task instead of split into "add hook" / "wire it in": an unused top-level function would fail the build) plus a manual QA pass (final task).

**Builds on:** `docs/superpowers/specs/2026-08-15-workbench-tab-mount-optimization-design.md` (the approved design this plan implements — read it for the full Decisions/Non-goals record and the codebase-safety verification for Agent Hub/SSH/Database/Kafka/Notes if anything here seems under-motivated).

---

## File Structure

Modify:
- `frontend/src/workbench/Workbench.tsx` — add grace/cap constants, add the `useMountedModuleIds` hook, wire it into `WorkbenchViews`'s render filter.

No files created. No other files touched.

---

### Task 1: Bounded module mount in `WorkbenchViews`

**Files:**
- Modify: `frontend/src/workbench/Workbench.tsx`

- [ ] **Step 1: Add `useRef` to the React import**

Find:

```ts
import { memo, Suspense, useEffect, useMemo, useState } from "react";
```

Replace:

```ts
import { memo, Suspense, useEffect, useMemo, useRef, useState } from "react";
```

- [ ] **Step 2: Add the grace-period and cap constants next to `HEADER_ONLY_MODULE_IDS`**

Find:

```ts
const HEADER_ONLY_MODULE_IDS = new Set(["settings", "sync"]);
```

Replace:

```ts
const HEADER_ONLY_MODULE_IDS = new Set(["settings", "sync"]);

// Bounds how many modules' tab content stays mounted in WorkbenchViews:
// the active module plus up to MAX_MOUNTED_MODULES - 1 more that were
// active within the last MODULE_GRACE_MS. See useMountedModuleIds below.
const MODULE_GRACE_MS = 2 * 60 * 1000;
const MAX_MOUNTED_MODULES = 3;
```

- [ ] **Step 3: Add the `useMountedModuleIds` hook, between the props type and the `WorkbenchViews` doc comment**

Find:

```ts
type WorkbenchViewsProps = {
  availableModules: ModuleManifest[];
  hasMetadataWarning: boolean;
  openTabs: WorkbenchTab[];
  activeTabId: string;
};

/**
 * Heavy module trees are deliberately isolated from tab-strip ordering.
```

Replace:

```ts
type WorkbenchViewsProps = {
  availableModules: ModuleManifest[];
  hasMetadataWarning: boolean;
  openTabs: WorkbenchTab[];
  activeTabId: string;
};

/**
 * MRU-tracked set of non-exempt module ids allowed to stay mounted: the
 * active module always qualifies; up to MAX_MOUNTED_MODULES - 1 more
 * recently-active modules stay mounted for MODULE_GRACE_MS after being
 * left. Exempt ids (HEADER_ONLY_MODULE_IDS) are never added here — the
 * caller unions them in separately and they're always mounted regardless
 * of this hook's output.
 */
function useMountedModuleIds(
  activeModuleId: string,
  exemptIds: ReadonlySet<string>,
): Set<string> {
  const [recent, setRecent] = useState<string[]>([]);
  const prevActive = useRef("");
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>());

  useEffect(() => {
    if (!activeModuleId) {
      return;
    }
    const leaving = prevActive.current;
    prevActive.current = activeModuleId;

    if (leaving && leaving !== activeModuleId && !exemptIds.has(leaving)) {
      const timer = setTimeout(() => {
        timers.current.delete(leaving);
        setRecent((prev) => prev.filter((id) => id !== leaving));
      }, MODULE_GRACE_MS);
      timers.current.set(leaving, timer);
    }

    if (exemptIds.has(activeModuleId)) {
      return;
    }

    const ownTimer = timers.current.get(activeModuleId);
    if (ownTimer) {
      clearTimeout(ownTimer);
      timers.current.delete(activeModuleId);
    }

    setRecent((prev) => {
      const next = [activeModuleId, ...prev.filter((id) => id !== activeModuleId)];
      for (const evictedId of next.slice(MAX_MOUNTED_MODULES)) {
        const evictedTimer = timers.current.get(evictedId);
        if (evictedTimer) {
          clearTimeout(evictedTimer);
          timers.current.delete(evictedId);
        }
      }
      return next.slice(0, MAX_MOUNTED_MODULES);
    });
  }, [activeModuleId, exemptIds]);

  useEffect(() => {
    const timerMap = timers.current;
    return () => {
      for (const timer of timerMap.values()) {
        clearTimeout(timer);
      }
    };
  }, []);

  return new Set(recent);
}

/**
 * Heavy module trees are deliberately isolated from tab-strip ordering.
```

- [ ] **Step 4: Compute the mounted-module set inside `WorkbenchViews`**

Find:

```tsx
  const activeTab = openTabs.find((tab) => tab.id === activeTabId);
  const activeModule =
    availableModules.find((module) => module.id === activeTab?.moduleId) ??
    availableModules[0];

  return (
```

Replace:

```tsx
  const activeTab = openTabs.find((tab) => tab.id === activeTabId);
  const activeModule =
    availableModules.find((module) => module.id === activeTab?.moduleId) ??
    availableModules[0];
  const recentModuleIds = useMountedModuleIds(
    activeTab?.moduleId ?? "",
    HEADER_ONLY_MODULE_IDS,
  );
  const mountedModuleIds = useMemo(
    () => new Set([...HEADER_ONLY_MODULE_IDS, ...recentModuleIds]),
    [recentModuleIds],
  );

  return (
```

- [ ] **Step 5: Skip rendering tabs whose module isn't mounted**

Find:

```tsx
          openTabs.map((tab) => {
            const module = getModule(tab.moduleId);
            const view = module?.views.find((candidate) => candidate.id === tab.viewId);
            if (!view) {
              return null;
            }
            const TabView = view.component;
            // Keep inactive tabs mounted so running work and drafts survive
            // switching, while `contents` preserves the active view's flex
            // participation without an extra layout wrapper.
            return (
```

Replace:

```tsx
          openTabs.map((tab) => {
            // Tabs outside the active module's grace/cap window aren't
            // mounted at all — see useMountedModuleIds above. Their tab
            // stays in openTabs (visible, clickable) and remounts fresh
            // when reactivated; background work belonging to that module
            // (e.g. Agent Hub sessions) lives outside this render tree and
            // is unaffected either way.
            if (!mountedModuleIds.has(tab.moduleId)) {
              return null;
            }
            const module = getModule(tab.moduleId);
            const view = module?.views.find((candidate) => candidate.id === tab.viewId);
            if (!view) {
              return null;
            }
            const TabView = view.component;
            // Keep same-module inactive tabs mounted so in-progress edits
            // and scroll position survive switching between tabs of one
            // module, while `contents` preserves the active view's flex
            // participation without an extra layout wrapper.
            return (
```

- [ ] **Step 6: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds with no TypeScript errors. This is the step that proves
`useMountedModuleIds` is actually wired in (not left unused — `tsconfig.json`
has `noUnusedLocals: true`, so a defined-but-uncalled top-level function
would fail this build) and that `mountedModuleIds`/`recentModuleIds` type
correctly against `Set<string>`/`ReadonlySet<string>`.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/workbench/Workbench.tsx
git commit -m "perf(workbench): bound mounted module tab content with MRU grace window"
```

---

### Task 2: Manual QA

No code changes. Start the app (`task dev:frontend`, or whatever this
session's `run` skill resolves to for dev-kit-app) and, in the running app:

1. **Cap and grace window.** Open Notes, then Database, then SSH (3
   distinct modules, one tab each). Open React DevTools' Components panel
   and confirm `NotesView`/`DatabaseView`/`SSHView` (or their lazy-loaded
   wrappers) are all present in the tree — Notes and Database inside their
   2-minute grace period, SSH as the active module.
2. **Cap eviction.** With those three still mounted, open a 4th distinct
   module (Kafka). Confirm in React DevTools that `NotesView` (the
   least-recently-active of the three) is no longer in the tree
   immediately — not after waiting — while its tab is still visible and
   clickable in the tab strip.
3. **Grace window prevents refetch on quick toggling.** Click back to
   Database (still within its 2-minute window since Step 1). Confirm it's
   still mounted (no flash of a loading state, no re-fetch of its profile
   list) and the switch is instant.
4. **Remount after eviction.** Click back to Notes (evicted in Step 2).
   Confirm it remounts cleanly (its dashboard fetch runs again), its tab in
   the strip is unchanged, and no separate action was needed to reopen it —
   clicking the existing tab was enough.
5. **Background work survives eviction.** Start an Agent Hub session (or
   use an already-running one), switch to another module and stay there
   for over 2 minutes so Agent Hub's view is evicted, then switch back.
   Confirm the session's status/output is unaffected — it kept running
   while unmounted (this exercises the `AgentSessionsSubscriber` decoupling
   documented in the spec).
6. **Autosave flush on eviction.** Open a note, type an edit, immediately
   switch to two other modules in a row (enough to push the note's module
   out of the mounted set) before the autosave debounce would normally
   fire, then reopen Notes and the same note. Confirm the edit was saved
   (existing `NoteEditor` unmount-flush behavior, now also exercised by
   this eviction path).
7. **Settings/Sync always mounted.** Open Settings mid-session (with other
   modules already occupying the cap). Confirm opening it doesn't evict
   anything from the recently-active set (it's unioned in separately, not
   counted against `MAX_MOUNTED_MODULES`), and after navigating away and
   waiting past the grace period, Settings itself is still mounted when you
   return to it.
8. **No regression to tab list behavior.** Throughout the above, confirm
   opening, closing, reordering (drag), and the preview/pin italic
   lifecycle of tabs all behave exactly as before this change — nothing in
   this plan touches `useWorkbenchTabsStore` or `TabStrip.tsx`.
