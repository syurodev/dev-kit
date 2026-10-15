# Tab Strip UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preview tabs close like any tab; an overflowing strip shows fades and `‹ ›` buttons; every tab shows its status and unsaved work; closed tabs reopen with `⌘⇧T` and a "Recently Closed" list.

**Architecture:** Pure arithmetic in `workbench/tabs/tabStripModel.ts`; tab state (`dirty`, `recentlyClosed`, `reopenClosedTab`) in the zustand store `stores/workbenchTabs.ts`; the strip (`workbench/tabs/TabStrip.tsx`) renders it; modules report dirtiness through `setTabDirty`; the shortcut lives in `workbench/Workbench.tsx`, the palette rows in `workbench/palette/paletteModel.ts`, the key list in `modules/settings/keyboard.ts`.

**Tech Stack:** React 19, TypeScript, zustand (+persist), Radix context menu (`components/ui/context-menu.tsx`), dnd-kit (existing), bun test.

**Spec:** `docs/superpowers/specs/2026-10-15-tab-strip-ux-design.md` · **Branch:** `feat/tab-strip-ux` (from `main`, already checked out).

**Commands:** `cd frontend && bun test src/stores src/workbench src/modules/settings` · `bun run build` (tsc + vite) · `bunx eslint <files>`. Commits end with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

**House rules (AGENTS.md):** theme tokens only — status colours are `--status-*` (`bg-(--status-running)` etc., see `statusDotClass` in TabStrip.tsx); motion uses `--motion-*` and is disabled under `prefers-reduced-motion` (`components/ui/motionCatalogue.test.tsx` enforces new CSS classes); follow the surrounding comment style (why, not what).

---

## File structure

- `frontend/src/workbench/tabs/tabStripModel.ts` (+ `.test.ts`) — add `overflowEdges`, `scrollStep`, `tabMarks`.
- `frontend/src/stores/workbenchTabs.ts` (+ tests in `workbenchTabs.test.ts`, new `workbenchTabsReopen.test.ts`) — `dirty`, `setTabDirty`, `recentlyClosed`, `reopenClosedTab`, recording in `closeTab`, dedupe in `openTab`.
- `frontend/src/workbench/tabs/TabStrip.tsx` — preview close, marks, overflow chrome, reopen menu items.
- `frontend/src/workbench/tabs/TabStrip.test.tsx` (new) — render tests.
- `frontend/src/modules/codegraph/FileDocument.tsx`, the API Client request document — `setTabDirty` producers.
- `frontend/src/workbench/Workbench.tsx` — `⌘⇧T`.
- `frontend/src/workbench/palette/paletteModel.ts` (+ test), `frontend/src/workbench/CommandPalette.tsx` — reopen rows.
- `frontend/src/modules/settings/keyboard.ts` — list the shortcut.

---

### Task 1: A preview tab closes

**Files:** Modify `frontend/src/workbench/tabs/TabStrip.tsx` (`onAuxClick` ~302, close slot ~358). Test: create `frontend/src/workbench/tabs/TabStrip.test.tsx`.

- [ ] **Step 1: Failing test.** Look at `frontend/src/workbench/step4aComponents.test.tsx` for how it renders `TabStrip` with `renderToStaticMarkup` (providers, required props). Create `TabStrip.test.tsx` with a small `renderStrip(tabs, activeTabId)` helper copied from that pattern and:

```tsx
test("a preview tab has a close button", () => {
  const html = renderStrip([
    { id: "m:v:a", moduleId: "m", viewId: "v.doc", title: "Kept" },
    { id: "m:v:b", moduleId: "m", viewId: "v.doc", title: "Peek", preview: true },
  ], "m:v:a");
  expect(html).toContain('aria-label="Close Peek"');
  expect(html).toContain('aria-label="Close Kept"');
});
```

(Use a `viewId` that is not the module's first view, so `isHiddenHomePreview` does not hide the preview; register or stub the module the way step4aComponents.test.tsx does.)

- [ ] **Step 2:** `cd frontend && bun test src/workbench/tabs/TabStrip.test.tsx` → FAIL (no "Close Peek").
- [ ] **Step 3: Implement.** In `onAuxClick` drop `&& !tab.preview`. In the close slot replace `) : !tab.preview ? (` with `) : (` and the trailing `) : null}` with `)}`. Update the comment above the slot: "A pinned tab shows its pin where the close button goes, and the pin unpins (VS Code). Every other tab — a preview too — has the 12px close."
- [ ] **Step 4:** test passes; `bun test src/workbench` green.
- [ ] **Step 5: Commit** `tabs: a preview tab closes like any other`.

---

### Task 2: Overflow arithmetic

**Files:** Modify `frontend/src/workbench/tabs/tabStripModel.ts`; test `tabStripModel.test.ts`.

- [ ] **Step 1: Failing tests** (append):

```ts
import { overflowEdges, scrollStep } from "./tabStripModel";

test("overflowEdges says which sides hide tabs", () => {
  expect(overflowEdges({ scrollLeft: 0, scrollWidth: 300, clientWidth: 300 })).toEqual({ left: false, right: false });
  expect(overflowEdges({ scrollLeft: 0, scrollWidth: 900, clientWidth: 300 })).toEqual({ left: false, right: true });
  expect(overflowEdges({ scrollLeft: 600, scrollWidth: 900, clientWidth: 300 })).toEqual({ left: true, right: false });
  expect(overflowEdges({ scrollLeft: 200, scrollWidth: 900, clientWidth: 300 })).toEqual({ left: true, right: true });
  // Sub-pixel scroll positions at either end still read as the end.
  expect(overflowEdges({ scrollLeft: 0.4, scrollWidth: 900, clientWidth: 300 })).toEqual({ left: false, right: true });
  expect(overflowEdges({ scrollLeft: 599.6, scrollWidth: 900, clientWidth: 300 })).toEqual({ left: true, right: false });
});

test("scrollStep moves by most of a viewport and stops at the ends", () => {
  const view = { scrollLeft: 100, scrollWidth: 900, clientWidth: 300 };
  expect(scrollStep(view, "right")).toBe(340);
  expect(scrollStep(view, "left")).toBe(0);
  expect(scrollStep({ ...view, scrollLeft: 500 }, "right")).toBe(600);
});
```

- [ ] **Step 2:** run → FAIL (not exported).
- [ ] **Step 3: Implement** (append to `tabStripModel.ts`):

```ts
export type ScrollView = { scrollLeft: number; scrollWidth: number; clientWidth: number };

/** A pixel of slack: a fractional scrollLeft at either end is still the end. */
const EDGE_TOLERANCE = 1;

/** Which sides of the strip have tabs scrolled out of sight. */
export function overflowEdges(view: ScrollView): { left: boolean; right: boolean } {
  const max = view.scrollWidth - view.clientWidth;
  if (max <= EDGE_TOLERANCE) return { left: false, right: false };
  return { left: view.scrollLeft > EDGE_TOLERANCE, right: view.scrollLeft < max - EDGE_TOLERANCE };
}

/** How far a ‹ › press scrolls: most of a viewport, so a tab cut at the edge stays in sight. */
const STEP_SHARE = 0.8;

export function scrollStep(view: ScrollView, direction: "left" | "right"): number {
  const max = Math.max(0, view.scrollWidth - view.clientWidth);
  const delta = view.clientWidth * STEP_SHARE * (direction === "left" ? -1 : 1);
  return Math.min(max, Math.max(0, view.scrollLeft + delta));
}
```

- [ ] **Step 4:** tests pass. **Step 5: Commit** `tabs: the strip knows which sides overflow`.

---

### Task 3: Overflow fades and `‹ ›` buttons

**Files:** Modify `frontend/src/workbench/tabs/TabStrip.tsx` (the scroller `row` ~636 and the wheel/reveal effects ~540–570); CSS in `frontend/src/index.css` only if a mask utility is missing.

- [ ] **Step 1: Read** the strip's render (~595–698): the outer `div` (`h-10 … px-2 py-1 gap-2`), the scroller `div` (`scrollbar-hide relative flex min-w-0 shrink items-center overflow-x-auto`, ref `row`), the New tab button.
- [ ] **Step 2: State.** Add:

```tsx
const [edges, setEdges] = useState({ left: false, right: false });
const syncEdges = useCallback(() => {
  const el = rowRef.current; // use the existing ref name for the scroller
  if (!el) return;
  const next = overflowEdges({ scrollLeft: el.scrollLeft, scrollWidth: el.scrollWidth, clientWidth: el.clientWidth });
  setEdges((prev) => (prev.left === next.left && prev.right === next.right ? prev : next));
}, []);
useLayoutEffect(syncEdges, [syncEdges, visibleTabs.length, tabMaxWidth, minWidth]);
useEffect(() => {
  const el = rowRef.current;
  if (!el || typeof ResizeObserver === "undefined") return;
  const observer = new ResizeObserver(syncEdges);
  observer.observe(el);
  el.addEventListener("scroll", syncEdges, { passive: true });
  return () => {
    observer.disconnect();
    el.removeEventListener("scroll", syncEdges);
  };
}, [syncEdges]);
const scrollBy = (direction: "left" | "right") => {
  const el = rowRef.current;
  if (!el) return;
  const reduce = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  el.scrollTo({ left: scrollStep(el, direction), behavior: reduce ? "auto" : "smooth" });
};
```

- [ ] **Step 3: Render.** Only when `edges.left || edges.right`, wrap the scroller with a button on each side:

```tsx
{edges.left || edges.right ? (
  <Button type="button" variant="titlebar" size="icon-sm" aria-label="Scroll tabs left" disabled={!edges.left} onClick={() => scrollBy("left")}>
    <ChevronLeft className="size-3.5" />
  </Button>
) : null}
{/* scroller, unchanged, plus the fade mask below */}
{edges.left || edges.right ? (
  <Button type="button" variant="titlebar" size="icon-sm" aria-label="Scroll tabs right" disabled={!edges.right} onClick={() => scrollBy("right")}>
    <ChevronRight className="size-3.5" />
  </Button>
) : null}
```

and give the scroller a mask that fades only the overflowing sides (a mask, so nothing overlays the tabs and eats clicks):

```tsx
style={{
  ...existingStyle,
  maskImage: edges.left || edges.right
    ? `linear-gradient(to right, ${edges.left ? "transparent" : "#000"} 0, #000 ${FADE_PX}px, #000 calc(100% - ${FADE_PX}px), ${edges.right ? "transparent" : "#000"} 100%)`
    : undefined,
}}
```

with `const FADE_PX = 24;` near the strip's other constants (comment: "about a tab icon's width plus its padding: enough to read as 'more', not enough to hide a title"). The mask colour `#000` is an alpha mask, not a colour — say so in the comment, since AGENTS.md forbids hard-coded colours. `ChevronLeft/ChevronRight` from `lucide-react`.

- [ ] **Step 4: Test.** In `TabStrip.test.tsx`, the static render has no layout, so cover the logic through Task 2's helpers; add one render assertion that with no overflow (initial state) there are no "Scroll tabs" buttons. Run `bun test src/workbench` and `bun run build`.
- [ ] **Step 5: Commit** `tabs: an overflowing strip fades its edges and scrolls with ‹ ›`.

---

### Task 4: `dirty` in the store

**Files:** Modify `frontend/src/stores/workbenchTabs.ts`; test `frontend/src/stores/workbenchTabs.test.ts`.

- [ ] **Step 1: Failing test:**

```ts
describe("unsaved work", () => {
  test("setTabDirty marks a tab and is a no-op when unchanged", () => {
    const { openTab, setTabDirty } = useWorkbenchTabsStore.getState();
    const id = openTab({ moduleId: "notes", viewId: "notes.editor", title: "A", params: { noteId: "a" } });
    setTabDirty(id, true);
    const tab = () => useWorkbenchTabsStore.getState().modules.notes.openTabs[0];
    expect(tab().dirty).toBe(true);
    const before = useWorkbenchTabsStore.getState().modules;
    setTabDirty(id, true);
    expect(useWorkbenchTabsStore.getState().modules).toBe(before);
    setTabDirty(id, false);
    expect(tab().dirty).toBe(false);
  });
  test("dirty is not persisted", () => {
    const { openTab, setTabDirty } = useWorkbenchTabsStore.getState();
    const id = openTab({ moduleId: "notes", viewId: "notes.editor", title: "A", params: { noteId: "a" } });
    setTabDirty(id, true);
    const saved = persistedTabs(useWorkbenchTabsStore.getState() as never);
    expect(JSON.stringify(saved)).not.toContain("dirty");
  });
});
```

Import `persistedTabs` from `./workbenchTabs`; read its signature first (`stores/workbenchTabs.ts` ~253) and call it the way the existing persistence tests do if they exist (`grep -rn persistedTabs frontend/src`).

- [ ] **Step 2:** FAIL. **Step 3: Implement:** add to `WorkbenchTab`

```ts
  /** Unsaved work in the document: drawn as a dot in the close slot. Never persisted, like status. */
  dirty?: boolean;
```

to the state type `setTabDirty: (id: string, dirty: boolean) => void;`, and next to `setTabStatus`:

```ts
  setTabDirty: (id, dirty) =>
    set((state) =>
      updateModule(state, moduleOfTab(id), (m) => {
        const index = m.openTabs.findIndex((tab) => tab.id === id);
        if (index === -1 || !!m.openTabs[index].dirty === dirty) return m;
        const openTabs = m.openTabs.slice();
        openTabs[index] = { ...openTabs[index], dirty };
        return { ...m, openTabs };
      }),
    ),
```

(match `setTabStatus`'s exact shape; if it returns `state` rather than `m` for a no-op, do the same). In `persistedTabs`, strip `dirty` exactly where it strips `status`.

- [ ] **Step 4:** pass. **Step 5: Commit** `tabs: a tab can say it holds unsaved work`.

---

### Task 5: Recently closed and reopen

**Files:** Modify `frontend/src/stores/workbenchTabs.ts`; test: create `frontend/src/stores/workbenchTabsReopen.test.ts` (copy the persist-stubbing preamble and `beforeEach` reset from `workbenchTabs.test.ts`, adding `recentlyClosed: []` to the reset).

- [ ] **Step 1: Failing tests:**

```ts
const open = (title: string, extra: Partial<OpenTabInput> = {}) =>
  useWorkbenchTabsStore.getState().openTab({ moduleId: "notes", viewId: "notes.editor", title, params: { noteId: title }, ...extra });
const titles = () => useWorkbenchTabsStore.getState().modules.notes?.openTabs.map((t) => t.title) ?? [];

test("a closed tab comes back where it was", () => {
  const a = open("a"); open("b"); open("c");
  useWorkbenchTabsStore.getState().closeTab(a);
  expect(titles()).toEqual(["b", "c"]);
  const back = useWorkbenchTabsStore.getState().reopenClosedTab();
  expect(back).not.toBeNull();
  expect(titles()).toEqual(["a", "b", "c"]);
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(0);
});

test("reopen goes last-closed first and keeps the pin", () => {
  const a = open("a"); const b = open("b"); open("c");
  useWorkbenchTabsStore.getState().togglePinTab(a);
  useWorkbenchTabsStore.getState().closeTab(a);
  useWorkbenchTabsStore.getState().closeTab(b);
  useWorkbenchTabsStore.getState().reopenClosedTab();
  expect(titles()).toContain("b");
  expect(titles()).not.toContain("a");
  useWorkbenchTabsStore.getState().reopenClosedTab();
  const reopened = useWorkbenchTabsStore.getState().modules.notes.openTabs.find((t) => t.title === "a");
  expect(reopened?.pinned).toBe(true);
});

test("preview tabs are not recorded; the stack is capped at 20", () => {
  const p = open("peek", { preview: true });
  useWorkbenchTabsStore.getState().closeTab(p);
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(0);
  for (let i = 0; i < 25; i++) useWorkbenchTabsStore.getState().closeTab(open(`t${i}`));
  const stack = useWorkbenchTabsStore.getState().recentlyClosed;
  expect(stack).toHaveLength(20);
  expect(stack[0].title).toBe("t24");
});

test("opening a tab again drops its record", () => {
  useWorkbenchTabsStore.getState().closeTab(open("a"));
  open("a");
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toHaveLength(0);
});

test("reopen picks a given entry, and returns null on an empty stack", () => {
  useWorkbenchTabsStore.getState().closeTab(open("a"));
  useWorkbenchTabsStore.getState().closeTab(open("b"));
  useWorkbenchTabsStore.getState().reopenClosedTab(1);
  expect(titles()).toEqual(["a"]);
  useWorkbenchTabsStore.getState().reopenClosedTab();
  expect(useWorkbenchTabsStore.getState().reopenClosedTab()).toBeNull();
});

test("a tab whose group is gone reopens in the focused group", () => {
  // Use splitTab to make a second group, close the tab there, close that group, then reopen.
  const a = open("a"); const b = open("b");
  useWorkbenchTabsStore.getState().splitTab(b);
  const groupB = useWorkbenchTabsStore.getState().groups.find((g) => g.activeTabId === b)!.id;
  useWorkbenchTabsStore.getState().closeTab(b); // its group empties and is dropped
  expect(useWorkbenchTabsStore.getState().groups.some((g) => g.id === groupB)).toBe(false);
  useWorkbenchTabsStore.getState().reopenClosedTab();
  const state = useWorkbenchTabsStore.getState();
  const tab = state.modules.notes.openTabs.find((t) => t.title === "b")!;
  expect(state.groups.some((g) => g.id === (tab.group ?? ""))).toBe(true);
  void a;
});
```

(Import `OpenTabInput` type. If `splitTab` behaves differently from this assumption — read it at ~528 first — adapt the last test to reach "group no longer exists" another way, keeping the assertion.)

- [ ] **Step 2:** FAIL. **Step 3: Implement.**

```ts
/** A closed tab, as `reopenClosedTab` needs it back. */
export type ClosedTab = {
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  icon?: LucideIcon;
  pinned: boolean;
  /** The group it sat in, and its position in that group's strip. */
  groupId: string;
  index: number;
  closedAt: number;
};

/** Enough to undo a burst of closes, small enough to read as a menu. */
export const MAX_RECENTLY_CLOSED = 20;

/** Same document: module, view and params (key order ignored). */
function sameDocument(a: { moduleId: string; viewId: string; params?: Record<string, unknown> }, b: typeof a): boolean {
  return a.moduleId === b.moduleId && a.viewId === b.viewId && tabKey(a.moduleId, a.viewId, a.params) === tabKey(b.moduleId, b.viewId, b.params);
}
```

(`tabKey` already exists in the store — check its signature; if it is not usable for this, compare `JSON.stringify` of params with sorted keys.)

State: `recentlyClosed: ClosedTab[]` (initial `[]`; NOT in `persistedTabs`), `reopenClosedTab: (at?: number) => string | null`.

In `closeTab`, before removing, when `!tab.preview`: compute `group` (already computed) and `index = tabsOfGroup(state, group).findIndex(t => t.id === id)`, then prepend
`{ moduleId: tab.moduleId, viewId: tab.viewId, title: tab.title, params: tab.params, icon: tab.icon, pinned: !!tab.pinned, groupId: group?.id ?? "", index: Math.max(0, index), closedAt: Date.now() }`
to `recentlyClosed` with entries `sameDocument` as it removed, sliced to `MAX_RECENTLY_CLOSED`; include it in the returned state.

In `openTab`, drop entries `sameDocument` as the input from `recentlyClosed` (only when any match, to keep identity otherwise).

`reopenClosedTab`:

```ts
  reopenClosedTab: (at = 0) => {
    const entry = get().recentlyClosed[at];
    if (!entry) return null;
    set((state) => ({ recentlyClosed: state.recentlyClosed.filter((_, i) => i !== at) }));
    const state = get();
    const groupAlive = state.groups.some((g) => g.id === entry.groupId);
    if (groupAlive && state.focusedGroupId !== entry.groupId) state.focusGroup(entry.groupId);
    const id = state.openTab({ moduleId: entry.moduleId, viewId: entry.viewId, title: entry.title, params: entry.params, icon: entry.icon, preview: false });
    if (entry.pinned) {
      const opened = moduleTabs(get().modules, entry.moduleId).openTabs.find((t) => t.id === id);
      if (opened && !opened.pinned) get().togglePinTab(id);
    }
    // Back to its old place in the strip (clamped by reorderTab's own rules).
    const after = get();
    const placed = moduleTabs(after.modules, entry.moduleId).openTabs.find((t) => t.id === id);
    const group = placed ? groupOfTab(after.groups, placed) : null;
    if (group) {
      const strip = tabsOfGroup(after, group).filter((t) => t.id !== id);
      const before = strip[entry.index]?.id ?? null;
      after.reorderTab(id, before);
    }
    return id;
  },
```

Use the store's `get` (check how the store is created — `create<...>()(persist((set, get) => ...))` — add `get` if the factory lacks it). `focusGroup` must make `openTab` place the tab in that group (it targets the focused group); confirm by reading `targetGroup`. `reorderTab(fromId, beforeId)` semantics: tab placed before `beforeId`, `null` = end — confirm at ~475.

Group closes: `TabStrip`'s `onCloseMany={(list) => list.forEach((id) => onClose(id))}` already goes through `closeTab` per tab, so each is recorded; the stack then holds the LAST closed first. Add a test that closing `[a, b, c]` in that order and reopening three times gives back `c`, `b`, `a`.

- [ ] **Step 4:** pass; whole `bun test src/stores src/workbench` green. **Step 5: Commit** `tabs: closed tabs are remembered and reopen where they were`.

---

### Task 6: Marks on every tab

**Files:** Modify `frontend/src/workbench/tabs/tabStripModel.ts` (+ test), `frontend/src/workbench/tabs/TabStrip.tsx`, `frontend/src/index.css` (spinner class), `TabStrip.test.tsx`.

- [ ] **Step 1: Failing pure test** (`tabStripModel.test.ts`):

```ts
import { tabMarks } from "./tabStripModel";

test("tabMarks: status on every tab, success until seen, dirty in the close slot", () => {
  expect(tabMarks({ status: "running", active: false, seenSuccess: false, dirty: false })).toEqual({ status: "running", dirty: false, hint: ["Running"] });
  expect(tabMarks({ status: "error", active: true, seenSuccess: false, dirty: false })).toEqual({ status: "error", dirty: false, hint: ["Failed"] });
  expect(tabMarks({ status: "success", active: false, seenSuccess: false, dirty: false }).status).toBe("success");
  expect(tabMarks({ status: "success", active: true, seenSuccess: false, dirty: false }).status).toBeNull();
  expect(tabMarks({ status: "success", active: false, seenSuccess: true, dirty: false }).status).toBeNull();
  expect(tabMarks({ status: "idle", active: false, seenSuccess: false, dirty: true })).toEqual({ status: null, dirty: true, hint: ["Unsaved changes"] });
});
```

- [ ] **Step 2:** FAIL. **Step 3: Implement** in `tabStripModel.ts`:

```ts
export type TabMarkInput = { status?: "idle" | "running" | "success" | "error"; active: boolean; seenSuccess: boolean; dirty: boolean };
export type TabMarks = { status: "running" | "success" | "error" | null; dirty: boolean; hint: string[] };

/**
 * What a tab shows besides its title. Status sits before the title on every
 * tab; "finished" only until the tab is looked at (the active tab never
 * shows it — you are looking at it); unsaved work is a dot in the close slot.
 */
export function tabMarks(input: TabMarkInput): TabMarks {
  const hint: string[] = [];
  let status: TabMarks["status"] = null;
  if (input.status === "running") status = "running";
  else if (input.status === "error") status = "error";
  else if (input.status === "success" && !input.active && !input.seenSuccess) status = "success";
  if (status === "running") hint.push("Running");
  if (status === "error") hint.push("Failed");
  if (status === "success") hint.push("Finished");
  if (input.dirty) hint.push("Unsaved changes");
  return { status, dirty: input.dirty, hint };
}
```

- [ ] **Step 4: Strip.**
  - "Seen": in the strip component keep `const seenSuccess = useRef(new Set<string>())`; whenever `activeTabId`'s tab has `status === "success"`, add its id; when a tab's status changes away from success, delete it (an effect over `visibleTabs` keyed on `id+status`). Pass `seenSuccess={seenSuccess.current.has(tab.id)}` to `TabItem` (or compute `marks` in the parent and pass `marks`).
  - In `TabItem`, compute `const marks = tabMarks({ status: tab.status, active: isActive, seenSuccess, dirty: !!tab.dirty })`.
  - Remove the old after-title dot (`isActive && tab.status && tab.status !== "idle" ? … : null`). Before the title (inside the label span, replacing the module icon only when `marks.status` is set? NO — spec: before the title, the icon stays): render after `ModuleIcon`:

```tsx
{marks.status === "running" ? (
  <span aria-hidden className="tab-status-spin size-2.5 shrink-0 rounded-full border-[1.5px] border-(--status-running) border-t-transparent" />
) : marks.status ? (
  <span aria-hidden className={cn("size-1.5 shrink-0 rounded-full", statusDotClass(marks.status))} />
) : null}
```

  If `border-[1.5px]` trips the shadcn no-arbitrary-values lint, use `border-2` with `size-2.5`.
  - Close slot: when `marks.dirty` and the tab is not hovered/focused, show the dot instead of the X; implement with group-hover so it stays CSS-only:

```tsx
{marks.dirty ? (
  <span aria-hidden className="size-2 shrink-0 rounded-full bg-text-secondary group-hover:hidden group-focus-visible:hidden" />
) : null}
```

  placed just before the existing close/pin element, and add `marks.dirty && "hidden group-hover:flex group-focus-visible:flex"` to the close/pin element's className (keep its other classes). A dirty pinned tab therefore shows the dot, and the pin on hover.
  - Tooltip: replace `<TooltipContent side="bottom">{tab.title}</TooltipContent>` with the title plus `marks.hint` lines (`{[tab.title, ...marks.hint].join(" · ")}` or stacked lines — stacked: map to `<div>`).
  - `aria-label` of the tab (or an sr-only span) includes the hints, so a screen reader hears "Unsaved changes".
- [ ] **Step 5: CSS** in `index.css` next to the other motion classes:

```css
.tab-status-spin { animation: tab-status-spin 900ms linear infinite; }
@keyframes tab-status-spin { to { transform: rotate(360deg); } }
```

  and inside the existing `@media (prefers-reduced-motion: reduce)` block: `.tab-status-spin { animation: none; }`. Add `tab-status-spin` to whatever list `motionCatalogue.test.tsx` checks (read the test).
- [ ] **Step 6: Render tests** in `TabStrip.test.tsx`: an inactive running tab renders `tab-status-spin`; an inactive error tab renders the error dot class; an active success tab renders no success dot; a dirty tab renders the dot and still has `aria-label="Close …"` in the markup.
- [ ] **Step 7:** `bun test src/workbench src/components` + `bun run build` + eslint. **Commit** `tabs: every tab shows its status and unsaved work`.

---

### Task 7: Modules report unsaved work

**Files:** `frontend/src/modules/codegraph/FileDocument.tsx`; the API Client's request document (find it: `grep -rn "dirty" frontend/src/modules/apiclient` — `RequestToolbar.tsx` shows the state; the document component that owns it is where the effect goes).

- [ ] **Step 1: CodeGraph.** In `FileDocument`, find how it knows the buffer is dirty (`bufferModel` / the store's `buffers[docKey]`, e.g. a `dirty` or `base !== text` flag used for the Save button). Add:

```tsx
const tabId = tabKey(MODULE, FILE_VIEW, params); // use the same tab id helper FlowDocument uses
const setTabDirty = useWorkbenchTabsStore((s) => s.setTabDirty);
useEffect(() => {
  setTabDirty(tabId, dirty);
}, [setTabDirty, tabId, dirty]);
```

(no cleanup to `false` on unmount: the tab may stay open while the view unmounts; the tab's own close removes it.)
- [ ] **Step 2: API Client.** Same three lines in the request document, with its own tab id and unsaved flag.
- [ ] **Step 3: Tests.** If `fileActions.test.ts` / an API client test already renders or drives the dirty flag, add an assertion that the store tab gets `dirty: true` after an edit and `false` after save; otherwise add a small test of a pure selector if one is extracted; at minimum `bun run build` + existing tests green.
- [ ] **Step 4: Commit** `tabs: CodeGraph files and API requests mark unsaved work`.

---

### Task 8: Reopen from the keyboard, the tab menu and the palette

**Files:** `frontend/src/workbench/Workbench.tsx` (keydown ~400), `frontend/src/workbench/tabs/TabStrip.tsx` (context menu ~385), `frontend/src/workbench/palette/paletteModel.ts` (+ `paletteModel.test.ts`), `frontend/src/workbench/CommandPalette.tsx`, `frontend/src/modules/settings/keyboard.ts`.

- [ ] **Step 1: Shortcut.** In the keydown handler, beside `⌘\`:

```tsx
// ⌘⇧T / Ctrl⇧T: the tab closed last, back where it was (browsers, VS Code).
if ((event.metaKey || event.ctrlKey) && event.shiftKey && !event.altKey && event.key.toLowerCase() === "t") {
  event.preventDefault();
  reopenWithNotice();
  return;
}
```

with, in the same file,

```tsx
function reopenWithNotice() {
  const state = useWorkbenchTabsStore.getState();
  const entry = state.recentlyClosed[0];
  if (!entry) return;
  if (!getModule(entry.moduleId)?.views.some((v) => v.id === entry.viewId)) {
    // A module turned off since: nothing to open it in.
    useWorkbenchTabsStore.setState({ recentlyClosed: state.recentlyClosed.slice(1) });
    toast.error(`${entry.title} can't be reopened — its module is no longer available.`);
    return;
  }
  state.reopenClosedTab();
}
```

(`getModule` from `@/modules/registry` — the strip already imports it; `toast` from `sonner`.) Export a version that takes an index (`reopenClosedAt(at)`) from a small new module `frontend/src/workbench/reopenTab.ts` so the strip and palette share it; Workbench imports it.
- [ ] **Step 2: Tab menu.** In `TabStrip`'s `ContextMenuContent`, after "Close All Tabs":

```tsx
<ContextMenuSeparator />
<ContextMenuItem shortcut={shortcut("T", { shift: true })} disabled={recent.length === 0} onSelect={() => reopenClosedAt(0)}>
  Reopen Closed Tab
</ContextMenuItem>
<ContextMenuSub>
  <ContextMenuSubTrigger disabled={recent.length === 0}>Recently Closed</ContextMenuSubTrigger>
  <ContextMenuSubContent>
    {recent.slice(0, 10).map((entry, i) => {
      const Icon = entry.icon ?? viewIcon(entry.moduleId, entry.viewId);
      return (
        <ContextMenuItem key={`${entry.closedAt}-${i}`} icon={Icon ? <Icon /> : undefined} onSelect={() => reopenClosedAt(i)}>
          {entry.title}
        </ContextMenuItem>
      );
    })}
  </ContextMenuSubContent>
</ContextMenuSub>
```

`recent` = `useWorkbenchTabsStore((s) => s.recentlyClosed)` read in the strip and passed down (one subscription, not one per tab). Check `shortcut()`'s signature in TabStrip.tsx (it builds "⌘W"); extend it for shift if needed. `ContextMenuSub*` are exported by `components/ui/context-menu.tsx`. Use whatever icon resolver the store uses (`viewIcon` / `restoreIcons`) for entries without an icon.
- [ ] **Step 3: Palette.** In `paletteModel.ts` extend `PaletteInput` with `recentlyClosed?: readonly { title: string; moduleId: string; icon?: LucideIcon }[]` and `PaletteActions` with `reopenClosed?: (at: number) => void`. In `staticSections`, when `actions.reopenClosed` and the list is non-empty, add a section `{ id: "recently-closed", heading: "Recently closed", rows }` with a first row `{ id: "command:reopen-closed-tab", title: "Reopen Closed Tab", shortcut: "⌘⇧T", run: () => actions.reopenClosed!(0) }` and `ranked` rows `{ id: \`reopen:${i}\`, title: \`Reopen: ${entry.title}\`, icon: entry.icon, run: () => actions.reopenClosed!(i) }`, matched by `rank(query, …)` like tabs, capped at 6 when not searching. Place it after the open-tabs section. Test in `paletteModel.test.ts`: the section appears with entries and runs the right index; absent when the list is empty.
- [ ] **Step 4: `CommandPalette.tsx`**: pass `recentlyClosed` from the store and `reopenClosed: reopenClosedAt` into the model.
- [ ] **Step 5: Keyboard list.** In `modules/settings/keyboard.ts`'s shell section add `{ id: "reopen-closed-tab", action: "Reopen closed tab", keys: [chord("t", { mod: true, shift: true })] }` (match the existing entries' shape — read the list, and its test if any).
- [ ] **Step 6:** `bun test src/workbench src/modules/settings src/stores` + `bun run build` + eslint. **Commit** `tabs: reopen closed tabs with ⌘⇧T, from the tab menu and the palette`.

---

### Task 9: Verify

- [ ] `cd frontend && bun test && bun run build`.
- [ ] Server mode check (controller does this): many tabs → fades + `‹ ›`; a preview tab's `✕`; a dirty CodeGraph file's dot; `⌘⇧T` after closing tabs, including in a split.
