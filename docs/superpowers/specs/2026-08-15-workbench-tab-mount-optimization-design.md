# Workbench tabs: bounded mount for inactive-module tab content

**Date:** 2026-08-15
**Status:** Draft — pending user review
**Builds on:** `docs/superpowers/specs/2026-08-15-workbench-tab-preview-reorder-design.md`
(preview/pin/reorder, implemented) and the original Phase 1 tab strip. This
spec touches only how `WorkbenchViews` decides what to mount — no change to
`useWorkbenchTabsStore` or `TabStrip.tsx`.

## Context

Today every open tab, across every module, stays mounted in the DOM for the
lifetime of the app — `WorkbenchViews` renders all of `openTabs` and toggles
`contents`/`hidden` via CSS, never unmounting (see
`frontend/src/workbench/Workbench.tsx`). This was deliberate (comment: "Keep
inactive tabs mounted so running work and drafts survive switching") but
means every module the user has ever opened a tab for stays in the render
tree forever, which gets heavier as more modules and heavier views (Kafka
explorer, long note lists, agent sessions) accumulate.

The user wants only the currently active module's tab content mounted,
carving out an exception for header-only modules (Settings, Sync), without
losing already-open tabs of other modules — closing/reopening the tab list
itself must stay untouched, and background work belonging to other modules
must keep running regardless of whether its view is mounted.

## Goals

1. Bound how many modules' tab content is mounted at once, to keep the React
   tree from growing unbounded as the user opens more modules over a
   session.
2. Preserve smooth switching between a couple of modules in active use — no
   jank or refetch-on-every-click from a naive "always unmount on module
   switch" rule.
3. Never lose or close any tab in `openTabs` — this is a render-only
   optimization, not a tab-lifecycle change.
4. Never interrupt actual background work (running agent sessions, in-flight
   requests) belonging to a module whose view isn't currently mounted.

## Non-goals

- **No change to `useWorkbenchTabsStore`, `tabKey`, `TabStrip.tsx`, or any
  open/close/preview/pin/reorder behavior.** The tab strip still shows every
  open tab from every module exactly as today; clicking any tab still works
  identically.
- **No persistence of the mounted-set across app restart.** Every app launch
  starts with only the active module's tab(s) mounted, same as it starts
  with `openTabs` from a clean/persisted state today (unrelated to this
  change either way).
- **No per-module opt-out beyond the existing `HEADER_ONLY_MODULE_IDS`
  set.** Settings and Sync are exempt because they're already
  header-only, non-Dock modules; no new module-level config is introduced.
- **No new state-preservation mechanism** (e.g. saving scroll position or
  form drafts before unmount). Confirmed acceptable — see Decisions.
- **No automated test added.** The frontend has no test runner configured
  anywhere in the repo; see Testing section.

## Decisions validated with user

- **Settings and Sync are always mounted, never unmounted.** Both are
  already carved out of the Dock via `HEADER_ONLY_MODULE_IDS` in
  `Workbench.tsx`; this spec reuses that exact set as the exemption list
  rather than introducing a second concept.
- **Losing transient local UI state on unmount (scroll position, an
  unsubmitted form, a displayed query result) is an acceptable trade-off.**
  Confirmed explicitly by the user, with the caveat below.
- **Unmounting a module's view must never stop or interrupt that module's
  actual background work** (a running agent session, an in-flight
  connection) — only the React render tree is affected. Verified against
  the current codebase, not just assumed:
  - **Agent Hub**: session state and status already live in
    `useAgentSessionsStore`, kept in sync by `AgentSessionsSubscriber`
    (`frontend/src/workbench/AgentSessionsSubscriber.tsx`), which is
    mounted once in `Workbench.tsx` as a sibling of the content area —
    independent of whether `AgentHubView` itself is mounted. Unaffected by
    this change.
  - **SSH / Database / Kafka views**: each `useEffect` in
    `SSHView.tsx`/`DatabaseView.tsx`/`KafkaView.tsx` is a one-shot
    request/response fetch (`listProfiles`, `runQuery`, `describeKafkaTopic`,
    etc.) — no persistent WebSocket or push subscription lives inside the
    component. Unmounting loses only the last-fetched data in local
    `useState`, which is re-fetched on remount.
  - **Notes**: `NoteEditor`'s existing unmount/dependency-change cleanup
    (`frontend/src/modules/notes/NoteEditor.tsx`, the `useEffect` keyed on
    `noteId`) already flushes any pending debounced autosave via
    `updateNote(...)` before tearing down — this already handles the note
    being unmounted mid-edit, independent of this change.
  - Net: nothing in the current codebase requires a view to stay mounted
    for its backend-side work to continue. No code changes needed outside
    the mount-filter itself to satisfy this requirement.
- **Grace period before actually unmounting a module the user just left:
  2 minutes.** Chosen over immediate unmount (rejected — causes
  refetch/flicker when flipping between two modules in normal use) and a
  60s default (rejected — too short for the user's actual back-and-forth
  pattern).
- **Hard cap: at most 3 non-exempt modules mounted at once** (the active
  module plus up to 2 more still inside their grace period). Chosen to
  bound worst-case memory when the user opens several distinct heavy
  modules within one grace window — without this cap, a 2-minute grace
  period alone could let mounted modules grow unbounded during a burst of
  module-hopping. Exceeding the cap evicts the least-recently-active
  non-exempt module immediately, without waiting out its remaining grace
  time.

## Architecture

### New: `useMountedModuleIds` hook, local to `Workbench.tsx`

Not extracted to its own file — used from exactly one call site
(`WorkbenchViews`), and `HEADER_ONLY_MODULE_IDS` (its exempt-set argument)
is already a module-scope constant in the same file.

```ts
const MODULE_GRACE_MS = 2 * 60 * 1000;
const MAX_MOUNTED_MODULES = 3;

/**
 * MRU-tracked set of non-exempt module ids allowed to stay mounted: the
 * active module always qualifies; up to MAX_MOUNTED_MODULES - 1 more
 * recently-active modules stay mounted for MODULE_GRACE_MS after being
 * left. Exempt ids are never added here — callers union them in separately
 * and they're always mounted regardless of this hook's output.
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
```

### `WorkbenchViews` change

```ts
const activeTab = openTabs.find((tab) => tab.id === activeTabId);
const recentModuleIds = useMountedModuleIds(
  activeTab?.moduleId ?? "",
  HEADER_ONLY_MODULE_IDS,
);
const mountedModuleIds = useMemo(
  () => new Set([...HEADER_ONLY_MODULE_IDS, ...recentModuleIds]),
  [recentModuleIds],
);
```

Inside the existing `openTabs.map(...)`, skip tabs whose module isn't
mounted — added as the first line of the map callback, everything else in
the loop body unchanged:

```ts
openTabs.map((tab) => {
  if (!mountedModuleIds.has(tab.moduleId)) {
    return null;
  }
  // ...existing lookup, Suspense, contents/hidden wrapper, unchanged
})
```

Tabs belonging to a mounted module still render every time (with the
existing `contents`/`hidden` toggle) regardless of whether that specific tab
is the active one — e.g. two open Notes tabs both stay mounted-hidden while
Notes is the active module, matching today's within-module behavior
exactly. Only tabs whose *module* has fallen out of the mounted set are
skipped.

## Error handling

No new failure modes — the hook is a pure client-side timer/array
bookkeeping with no I/O. One correctness requirement: timers must not leak
across `Workbench` unmounts (app-level teardown, or in dev under React
StrictMode's mount/unmount/remount cycle) — handled by the second `useEffect`
above, which clears every outstanding timer on cleanup.

## Testing / verification

No frontend unit test runner exists anywhere in this repo (same convention
noted in the preview/reorder spec). Verification is `cd frontend && bun run
build` (type-checks the new hook) plus a manual QA pass in the running app:

1. Open Notes, then Database, then SSH (3 distinct modules) — confirm via
   React DevTools that all three views are mounted (Notes and Database
   inside their grace period, SSH active).
2. Open a 4th distinct module (Kafka) — confirm the oldest of the three
   (Notes) is evicted from the DOM immediately, while its tab remains
   visible and clickable in the tab strip.
3. Click back to Database (still within its 2-minute grace period) —
   confirm it's still mounted, no refetch/flicker, instant switch.
4. Click back to Notes (already evicted) — confirm it remounts cleanly
   (fresh fetch), its tab is still present and unchanged, and clicking it
   didn't require reopening from the dashboard.
5. Start an Agent Hub session, switch away to another module for over 2
   minutes, switch back — confirm the session kept running (status/output
   unaffected by `AgentHubView` having been unmounted).
6. Edit a note, switch modules before the autosave debounce fires — confirm
   the edit is still persisted (existing `NoteEditor` unmount-flush, exercised
   under the new unmount trigger).
7. Open Settings mid-session — confirm it mounts alongside whatever else is
   currently mounted (doesn't evict anything) and is never itself evicted
   after leaving it.

## Out of scope recap

Store/tab-strip changes, cross-restart persistence, per-module opt-out
config, state-preservation-before-unmount, automated tests.
