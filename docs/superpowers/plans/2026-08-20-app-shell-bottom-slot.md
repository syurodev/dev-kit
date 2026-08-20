# App Shell Bottom Slot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a resizable bottom slot to the app shell — a third shared shell primitive alongside `moduleActions` and `SplitPanel` — plus an always-visible title-bar toggle for it.

**Architecture:** The bottom slot mirrors the sidebar exactly: a global open/closed flag persisted through `ui_state`, per-module content registered into a Zustand store by a `useBottomContent(node)` hook, and a `BottomSlotSurface` that renders the *active* module's node inside an animated flex box. The only genuinely new mechanics are the Y axis — the existing `useResizableWidth` hook grows an `axis` parameter, and `panelLayout.ts` grows a height clamp that protects a floor for the content row above.

**Tech Stack:** React 19 + TypeScript, Zustand (+ `persist` through `bridge/uistate`), framer-motion (`LazyMotion`/`domAnimation`), Tailwind v4, `bun test`.

---

## Context an implementer needs before starting

Read these before Task 1. They are short and every one of them is load-bearing.

| File | Why |
|---|---|
| `frontend/src/components/ui/sidebar-shell.tsx` | The shape this plan copies. `useSidebarContent` + `SidebarSurface` are the direct model for `useBottomContent` + `BottomSlotSurface`. |
| `frontend/src/stores/sidebarContent.ts` | Explains, in its own doc comment, why the content registry is Zustand and not Context. That reasoning applies unchanged here — do not "simplify" it to Context. |
| `frontend/src/stores/sidebarShell.ts` | The persisted open/closed flag, 20 lines. `stores/bottomSlot.ts` is this plus a height. |
| `frontend/src/workbench/panelLayout.ts` | `GAP_WIDTH`, `useWindowWidth`, `maxPanelWidth`. Task 2 adds the height twins. |
| `frontend/src/workbench/Workbench.tsx:520-663` | The title bar's three zones and the content row this plan restructures. |
| `base-doc/content/projects/dev-kit/shell-primitives.mdx` | Documents the two existing shell primitives. This slot is the third; that page will need a follow-up edit (out of scope, noted at the end). |

### Two things that are already true and must not be re-litigated

**The dock overlaps this slot on purpose.** `Dock.tsx` renders `fixed inset-x-0 bottom-2` and floats above everything. The Figma App Shell places the dock pill *inside* the bottom surface's bounds, and the user asked for exactly that ("cho dock đè lên bottom slot luôn không cần chừa khoản trống"). Do not add bottom padding to the slot, and do not move the dock. Nothing in this plan touches `Dock.tsx`.

**The terminal is not part of this plan.** The Go side is already finished — `internal/terminal.Registry`, `AppService.TerminalWrite/TerminalResize/TerminalClose`, the `terminal:io` capability grant, and `CodeGraphStartTerminal` all exist with tests. What is missing is the `Events.Emit` wiring (`main.go:201` still holds the `TODO(frontend Terminal component)` comment), the xterm.js component, and a module tenant. That is a separate plan; see "What this plan does NOT do".

---

## File Structure

**Created**

| File | Responsibility |
|---|---|
| `frontend/src/stores/bottomSlot.ts` | The slot's own state: `open` (persisted) and `height` (not persisted). One store because the two are always read together and neither participates in the cross-panel width clamp. |
| `frontend/src/stores/bottomContent.ts` | Per-module registry of bottom-slot content. A near-copy of `sidebarContent.ts`, including its identity bail-out. |
| `frontend/src/stores/bottomContent.test.ts` | Guards the identity bail-out that keeps registration from looping. |
| `frontend/src/components/ui/bottom-slot.tsx` | `useBottomContent(node)` hook + `BottomSlotSurface` component. Sits next to `sidebar-shell.tsx` and `split-panel.tsx` for the same reason they do. |

**Modified**

| File | Change |
|---|---|
| `frontend/src/workbench/useResizableWidth.ts` → `useResizableSize.ts` | Renamed and given an `axis: "x" \| "y"` parameter. |
| `frontend/src/components/ui/sidebar-shell.tsx` | Updated to the renamed hook. |
| `frontend/src/components/ui/split-panel.tsx` | Updated to the renamed hook. |
| `frontend/src/workbench/panelLayout.ts` | Adds `TITLE_BAR_HEIGHT`, `CONTENT_MIN_HEIGHT`, `useWindowHeight`, `maxBottomHeight`. |
| `frontend/src/workbench/panelLayout.test.ts` | Adds `maxBottomHeight` cases. |
| `frontend/src/workbench/Workbench.tsx` | Wraps the content row so the slot can sit under it; adds the title-bar toggle. |

---

## Geometry, and where the numbers come from

Taken from the App Shell component in Figma (page **07 App Shell**, component set `107:137`, variant `Bottom=Open`) at its 1440×900 frame size:

```
y=0    ┌──────────────────────────────┐
       │ title bar               h=30 │
y=30   ├──────────────────────────────┤
       │ content row            h=642 │   sidebar | views | split panel
y=672  ├──────────────────────────────┤
       │ gutter                   h=4 │   = GAP_WIDTH
y=676  ├──────────────────────────────┤
       │ bottom surface         h=220 │   ← this plan
y=896  ├──────────────────────────────┤
       │ window margin            h=4 │   = the existing mb-1
y=900  └──────────────────────────────┘
```

So: **default height 220**, gutter `GAP_WIDTH` (4, already the shared constant), and the surface ends at the same `mb-1` the content row already has.

`MIN_BOTTOM_HEIGHT = 120` and `MAX_BOTTOM_HEIGHT = 600` are **choices, not Figma values** — 120 is roughly six rows of monospace text plus chrome, 600 keeps a 900px window from being all panel. `CONTENT_MIN_HEIGHT = 240` is the same kind of choice as the existing `CONTENT_MIN_WIDTH = 480`.

> **Unverified against Figma:** the Figma MCP connector is not authorised in the session this plan was written in, so the surface's *fill and stroke* could not be read from `_Slot / Bottom Content` (`203:250`). This plan specifies `rounded-xl border border-border bg-card`, matching `SplitPanelSlot` — a raised content card, not the recessed `bg-sidebar` rail. If the Figma node says otherwise, that class list is the one line to change (Task 4, `BottomSlotSurface`).

---

## Task 1: Give the resize hook a Y axis

The bottom slot resizes vertically. `useResizableWidth` is X-only in three places: the delta it reads (`clientX`), the cursor it sets (`col-resize`), and its name.

**Files:**
- Rename: `frontend/src/workbench/useResizableWidth.ts` → `frontend/src/workbench/useResizableSize.ts`
- Modify: `frontend/src/components/ui/sidebar-shell.tsx:4`, `:69-75`
- Modify: `frontend/src/components/ui/split-panel.tsx:7`, `:95-107`

- [ ] **Step 1: Rename the file with git so history follows it**

```bash
cd dev-kit-app/frontend
git mv src/workbench/useResizableWidth.ts src/workbench/useResizableSize.ts
```

- [ ] **Step 2: Rewrite the hook with an axis**

Replace the whole contents of `frontend/src/workbench/useResizableSize.ts`:

```ts
import { useEffect, useRef, useState } from "react";

/**
 * Pointer-drag resize with min/max clamping, animated via the caller's own
 * framer-motion element. Extracted from components/ui/split-panel.tsx's
 * original inline implementation — SidebarSurface (workbench/) was the
 * second consumer that made sharing it worth doing, and BottomSlotSurface
 * the third, which is what added the axis.
 */
export function useResizableSize(opts: {
  size: number;
  onSizeChange: (size: number) => void;
  min: number;
  max: number;
  /** Which edge the handle drags along. */
  axis: "x" | "y";
  /**
   * Which way along that axis grows the panel. The rule is the same on both
   * axes: +1 when the panel lies *after* its handle in that direction
   * (Sidebar — handle on its right, dragging right grows it), -1 when it lies
   * before (SplitPanel — handle on its left; BottomSlot — handle on its top,
   * dragging up grows it).
   */
  sign: 1 | -1;
}) {
  const { size, onSizeChange, min, max, axis, sign } = opts;
  const [isResizing, setIsResizing] = useState(false);
  const resizeStart = useRef<{ position: number; size: number } | null>(null);

  useEffect(() => {
    if (!isResizing) return;
    function onMove(e: PointerEvent) {
      if (!resizeStart.current) return;
      const position = axis === "x" ? e.clientX : e.clientY;
      const delta = sign * (position - resizeStart.current.position);
      onSizeChange(Math.min(max, Math.max(min, resizeStart.current.size + delta)));
    }
    function onUp() {
      resizeStart.current = null;
      setIsResizing(false);
    }
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
    document.body.style.cursor = axis === "x" ? "col-resize" : "row-resize";
    document.body.style.userSelect = "none";
    return () => {
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
  }, [isResizing, min, max, axis, sign, onSizeChange]);

  function onPointerDown(e: React.PointerEvent) {
    e.preventDefault();
    resizeStart.current = {
      position: axis === "x" ? e.clientX : e.clientY,
      size,
    };
    setIsResizing(true);
  }

  return { isResizing, onPointerDown };
}
```

- [ ] **Step 3: Update `sidebar-shell.tsx`**

Change the import on line 4 from:

```ts
import { useResizableWidth } from "@/workbench/useResizableWidth";
```

to:

```ts
import { useResizableSize } from "@/workbench/useResizableSize";
```

and the call (currently `sidebar-shell.tsx:69-75`) from:

```ts
  const { isResizing, onPointerDown } = useResizableWidth({
    width: sidebarWidth,
    onWidthChange: setSidebarWidth,
    min: MIN_SIDEBAR_WIDTH,
    max,
    sign: 1,
  });
```

to:

```ts
  const { isResizing, onPointerDown } = useResizableSize({
    size: sidebarWidth,
    onSizeChange: setSidebarWidth,
    min: MIN_SIDEBAR_WIDTH,
    max,
    axis: "x",
    sign: 1,
  });
```

- [ ] **Step 4: Update `split-panel.tsx`**

Change the import on line 7 from:

```ts
import { useResizableWidth } from "@/workbench/useResizableWidth";
```

to:

```ts
import { useResizableSize } from "@/workbench/useResizableSize";
```

and the call (currently `split-panel.tsx:95-107`) from:

```ts
  const { isResizing, onPointerDown } = useResizableWidth({
    width,
    onWidthChange: setWidth,
    min: MIN_PANEL_WIDTH,
```

to:

```ts
  const { isResizing, onPointerDown } = useResizableSize({
    size: width,
    onSizeChange: setWidth,
    min: MIN_PANEL_WIDTH,
```

and add `axis: "x",` immediately above the existing `sign: -1,` line at the end of that call. The `max: maxPanelWidth({...})` argument in between is unchanged — leave it exactly as it is, including its comment.

- [ ] **Step 5: Verify nothing else referenced the old name**

Run: `cd dev-kit-app/frontend && grep -rn "useResizableWidth" src/`
Expected: no output at all. If anything prints, update it the same way.

- [ ] **Step 6: Typecheck**

Run: `cd dev-kit-app/frontend && npx tsc --noEmit`
Expected: no output (clean).

- [ ] **Step 7: Commit**

```bash
cd dev-kit-app
git add frontend/src/workbench/useResizableSize.ts frontend/src/components/ui/sidebar-shell.tsx frontend/src/components/ui/split-panel.tsx
git commit -m "refactor(workbench): give the resize hook an axis"
```

---

## Task 2: The height clamp

The two side panels clamp against each other because they compete for the same horizontal space. The bottom slot has no such neighbour — it only has to leave the content row a floor. So this is a simpler function than `maxPanelWidth`, not a reuse of it.

**Files:**
- Modify: `frontend/src/workbench/panelLayout.ts`
- Test: `frontend/src/workbench/panelLayout.test.ts`

- [ ] **Step 1: Write the failing tests**

Append to `frontend/src/workbench/panelLayout.test.ts`:

```ts
test("the bottom slot may grow to its hard maximum on a tall window", () => {
  expect(maxBottomHeight({ windowHeight: 1400, min: 120, hardMax: 600 })).toBe(600);
  // Including at the 900px Figma frame height: the raw budget there is 622,
  // so the hard max is what actually caps the slot on the design target.
  expect(maxBottomHeight({ windowHeight: 900, min: 120, hardMax: 600 })).toBe(600);
});

test("the bottom slot's ceiling leaves the content row its floor", () => {
  // 800, not the 900 design height: at 900 the hard max binds first and this
  // assertion would pass without the content floor doing any work at all.
  const windowHeight = 800;
  const max = maxBottomHeight({ windowHeight, min: 120, hardMax: 600 });
  expect(max).toBeLessThan(600);
  expect(max).toBe(
    windowHeight - TITLE_BAR_HEIGHT - CONTENT_MIN_HEIGHT - GAP_WIDTH * 2,
  );
  const contentRow = windowHeight - TITLE_BAR_HEIGHT - max - GAP_WIDTH * 2;
  expect(contentRow).toBe(CONTENT_MIN_HEIGHT);
});

test("never returns below the slot's own minimum", () => {
  // Same failure mode maxPanelWidth guards: on a short window the raw
  // arithmetic goes under the floor, and a max below min would drag the slot
  // down through its own minimum on the next pointer move.
  const max = maxBottomHeight({ windowHeight: 300, min: 120, hardMax: 600 });
  expect(max).toBe(120);
});
```

Then extend the import at the top of that file from:

```ts
import { maxPanelWidth, CONTENT_MIN_WIDTH, GAP_WIDTH } from "./panelLayout";
```

to:

```ts
import {
  maxPanelWidth,
  maxBottomHeight,
  CONTENT_MIN_WIDTH,
  CONTENT_MIN_HEIGHT,
  TITLE_BAR_HEIGHT,
  GAP_WIDTH,
} from "./panelLayout";
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd dev-kit-app/frontend && npx bun test src/workbench/panelLayout.test.ts`
Expected: FAIL — `error: Export named 'maxBottomHeight' not found in module`.

- [ ] **Step 3: Implement**

Append to `frontend/src/workbench/panelLayout.ts`:

```ts
/**
 * The title bar's fixed height. Mirrors the `h-[30px]` on Workbench.tsx's
 * <header> — a Tailwind class cannot be read from here, so the two are kept
 * in step by hand. Change one, change the other.
 */
export const TITLE_BAR_HEIGHT = 30;

/** Hard floor for the centre content row, whatever the bottom slot does. */
export const CONTENT_MIN_HEIGHT = 240;

/** Tracks the live viewport height, for the bottom slot's clamp. */
export function useWindowHeight(): number {
  const [height, setHeight] = useState(() => window.innerHeight);
  useEffect(() => {
    const onResize = () => setHeight(window.innerHeight);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  return height;
}

/**
 * How tall the bottom slot may grow.
 *
 * Deliberately not maxPanelWidth with different constants: the two side
 * panels clamp against *each other* because they share one horizontal budget,
 * and the bottom slot has no such neighbour — it only owes the content row
 * above it a floor. Passing a fake `otherEffectiveWidth: 0` through the other
 * function would have hidden that difference behind a coincidence.
 *
 * The two GAP_WIDTHs are different things that happen to be equal: the gutter
 * between the content row and this slot, and the window margin below it (the
 * `mb-1` on Workbench.tsx's content wrapper).
 *
 * Never returns less than `min`, for the same reason maxPanelWidth does not.
 */
export function maxBottomHeight(opts: {
  windowHeight: number;
  min: number;
  hardMax: number;
}): number {
  const { windowHeight, min, hardMax } = opts;
  const available = windowHeight - TITLE_BAR_HEIGHT - CONTENT_MIN_HEIGHT - GAP_WIDTH * 2;
  return Math.max(min, Math.min(hardMax, available));
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd dev-kit-app/frontend && npx bun test src/workbench/panelLayout.test.ts`
Expected: PASS, 7 tests (4 existing + 3 new).

- [ ] **Step 5: Commit**

```bash
cd dev-kit-app
git add frontend/src/workbench/panelLayout.ts frontend/src/workbench/panelLayout.test.ts
git commit -m "feat(workbench): clamp for the bottom slot's height"
```

---

## Task 3: The two stores

**Files:**
- Create: `frontend/src/stores/bottomSlot.ts`
- Create: `frontend/src/stores/bottomContent.ts`
- Test: `frontend/src/stores/bottomContent.test.ts`

- [ ] **Step 1: Write the failing test for the content registry**

Create `frontend/src/stores/bottomContent.test.ts`:

```ts
import { expect, test, beforeEach } from "bun:test";
import { useBottomContentStore } from "./bottomContent";

beforeEach(() => {
  useBottomContentStore.setState({ content: {} });
});

test("registering content stores it under the module's id", () => {
  const node = { marker: "codegraph panel" };
  useBottomContentStore.getState().setContent("codegraph", node);
  expect(useBottomContentStore.getState().content.codegraph).toBe(node);
});

test("re-registering the same node does not produce a new state object", () => {
  // Load-bearing, not an optimisation: a module registers from an effect whose
  // dependency is the node itself. If an identical registration still produced
  // a new `content` object, SidebarSurface's equivalent subscriber would
  // re-render, the module would re-render, and the effect would fire again —
  // the "Maximum update depth exceeded" loop stores/sidebarContent.ts
  // documents.
  const node = { marker: "codegraph panel" };
  const setContent = useBottomContentStore.getState().setContent;
  setContent("codegraph", node);
  const first = useBottomContentStore.getState().content;
  setContent("codegraph", node);
  expect(useBottomContentStore.getState().content).toBe(first);
});

test("registering null removes the module's entry", () => {
  const setContent = useBottomContentStore.getState().setContent;
  setContent("codegraph", { marker: "x" });
  setContent("codegraph", null);
  expect("codegraph" in useBottomContentStore.getState().content).toBe(false);
});

test("removing an entry that was never registered changes nothing", () => {
  const before = useBottomContentStore.getState().content;
  useBottomContentStore.getState().setContent("notes", null);
  expect(useBottomContentStore.getState().content).toBe(before);
});

test("two modules can be registered at once", () => {
  // Modules stay mounted-but-hidden after being visited (WorkbenchViews' mount
  // policy), so a background module must not clobber the active one's panel.
  const setContent = useBottomContentStore.getState().setContent;
  const a = { marker: "a" };
  const b = { marker: "b" };
  setContent("codegraph", a);
  setContent("ssh", b);
  expect(useBottomContentStore.getState().content.codegraph).toBe(a);
  expect(useBottomContentStore.getState().content.ssh).toBe(b);
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd dev-kit-app/frontend && npx bun test src/stores/bottomContent.test.ts`
Expected: FAIL — `Cannot find module './bottomContent'`.

- [ ] **Step 3: Implement the content registry**

Create `frontend/src/stores/bottomContent.ts`:

```ts
import { create } from "zustand";
import type { ReactNode } from "react";

type BottomContentState = {
  /** Registered bottom-slot content, keyed by the owning module's id. */
  content: Record<string, ReactNode>;
  setContent: (moduleId: string, node: ReactNode | null) => void;
};

/**
 * The bottom slot's content registry — the same shape, and the same three
 * reasons, as stores/sidebarContent.ts. Read that file's doc comment before
 * changing this one; all of it applies here.
 *
 * In short: Zustand rather than Context, because with a Context the module
 * doing the registering is itself a consumer and registration feeds back into
 * its own render; keyed by module id, because more than one module is mounted
 * at a time; and identity-checked on write, because the effect that registers
 * depends on the node it registers.
 */
export const useBottomContentStore = create<BottomContentState>((set) => ({
  content: {},
  setContent: (moduleId, node) =>
    set((state) => {
      if (node === null) {
        if (!(moduleId in state.content)) {
          return state;
        }
        const next = { ...state.content };
        delete next[moduleId];
        return { content: next };
      }
      if (state.content[moduleId] === node) {
        return state;
      }
      return { content: { ...state.content, [moduleId]: node } };
    }),
}));
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd dev-kit-app/frontend && npx bun test src/stores/bottomContent.test.ts`
Expected: PASS, 5 tests.

- [ ] **Step 5: Implement the open/height store**

Create `frontend/src/stores/bottomSlot.ts`:

```ts
import { create } from "zustand";
import { persist } from "zustand/middleware";
import { uiStateStorage } from "@/bridge/uistate";

type BottomSlotState = {
  open: boolean;
  /** Live drag height. 220 matches the Figma App Shell's Bottom=Open variant. */
  height: number;
  setOpen: (open: boolean) => void;
  setHeight: (height: number) => void;
};

/**
 * Global, not per-module — same as stores/sidebarShell.ts: closing the slot in
 * one module closes it everywhere until the user reopens it. Default false so
 * the shell still opens on the Figma component's default variant.
 *
 * Only `open` is persisted. The height is deliberately session-only, matching
 * stores/panelWidths.ts, which does not persist the sidebar or split-panel
 * widths either — persisting one panel's size and not the others' would be a
 * worse inconsistency than persisting none.
 */
export const useBottomSlotStore = create<BottomSlotState>()(
  persist(
    (set) => ({
      open: false,
      height: 220,
      setOpen: (open) => set({ open }),
      setHeight: (height) => set({ height }),
    }),
    {
      name: "bottomSlot",
      storage: uiStateStorage,
      partialize: (state) => ({ open: state.open }),
    },
  ),
);
```

- [ ] **Step 6: Typecheck**

Run: `cd dev-kit-app/frontend && npx tsc --noEmit`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
cd dev-kit-app
git add frontend/src/stores/bottomSlot.ts frontend/src/stores/bottomContent.ts frontend/src/stores/bottomContent.test.ts
git commit -m "feat(workbench): bottom slot state and content registry"
```

---

## Task 4: `useBottomContent` and `BottomSlotSurface`

**Files:**
- Create: `frontend/src/components/ui/bottom-slot.tsx`

- [ ] **Step 1: Write the component file**

Create `frontend/src/components/ui/bottom-slot.tsx`:

```tsx
import * as React from "react";
import { LazyMotion, domAnimation, m } from "framer-motion";
import { useResizableSize } from "@/workbench/useResizableSize";
import {
  GAP_WIDTH,
  maxBottomHeight,
  useWindowHeight,
} from "@/workbench/panelLayout";
import { useBottomContentStore } from "@/stores/bottomContent";
import { useBottomSlotStore } from "@/stores/bottomSlot";
import { SidebarModuleIdContext } from "@/components/ui/sidebar-shell";

/**
 * Registers `content` as the calling module's bottom-slot panel for as long as
 * the component stays mounted. Registration is independent of whether the slot
 * is currently shown — that is a global toggle (useBottomSlotStore), so a
 * module registers unconditionally and the shell decides visibility.
 *
 * Reuses SidebarModuleIdContext rather than introducing a second identical
 * context: it answers "which module is this subtree", which is the same
 * question both slots ask, and WorkbenchViews already provides it once per
 * module subtree.
 *
 * Deliberately reads only the store's setter: see stores/bottomContent.ts for
 * why subscribing to the content itself here would deadlock the render loop.
 */
export function useBottomContent(content: React.ReactNode | null) {
  const moduleId = React.useContext(SidebarModuleIdContext);
  const setContent = useBottomContentStore((s) => s.setContent);

  React.useEffect(() => {
    if (!moduleId) return;
    setContent(moduleId, content);
    return () => setContent(moduleId, null);
  }, [moduleId, content, setContent]);
}

const MIN_BOTTOM_HEIGHT = 120;
const MAX_BOTTOM_HEIGHT = 600;

/**
 * The rendered bottom surface. Flex sibling *below* the row that holds the
 * sidebar, the module views and the split panel (see Workbench.tsx) — so it
 * spans the full window width, under all three, the way the Figma App Shell's
 * Bottom=Open variant draws it.
 *
 * The dock floats over this surface on purpose (Dock.tsx is `fixed bottom-2`,
 * and the Figma places the pill inside the bottom surface's bounds) — there is
 * deliberately no bottom padding here to make room for it.
 */
export function BottomSlotSurface({ activeModuleId }: { activeModuleId: string | null }) {
  const open = useBottomSlotStore((s) => s.open);
  const height = useBottomSlotStore((s) => s.height);
  const setHeight = useBottomSlotStore((s) => s.setHeight);
  const windowHeight = useWindowHeight();

  const content = useBottomContentStore((s) =>
    activeModuleId ? s.content[activeModuleId] : undefined,
  );

  const max = maxBottomHeight({
    windowHeight,
    min: MIN_BOTTOM_HEIGHT,
    hardMax: MAX_BOTTOM_HEIGHT,
  });

  const { isResizing, onPointerDown } = useResizableSize({
    size: height,
    onSizeChange: setHeight,
    min: MIN_BOTTOM_HEIGHT,
    max,
    axis: "y",
    // The handle sits on this panel's top edge, so dragging up grows it.
    sign: -1,
  });

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ height: 0 }}
        // Clamped at render, not only while dragging: the clamp depends on the
        // window, and the window can shrink long after the drag ended. Without
        // this, dragging to 600px on a tall window and then making the window
        // short would leave a shrink-0 surface taller than the space available
        // and crush the content row above it to nothing. The *stored* height
        // stays unclamped on purpose, so widening the window back restores the
        // size the user actually chose.
        //
        // SidebarSurface has the same hole on the horizontal axis today. Not
        // fixed here — it is a separate change to a file this plan otherwise
        // only renames an import in.
        animate={{ height: open ? Math.min(height, max) + GAP_WIDTH : 0 }}
        transition={isResizing ? { duration: 0 } : { duration: 0.22, ease: "easeInOut" }}
        className="flex min-w-0 shrink-0 flex-col overflow-hidden"
      >
        {open ? (
          <div
            role="separator"
            aria-orientation="horizontal"
            aria-label="Resize bottom panel"
            onPointerDown={onPointerDown}
            style={{ height: GAP_WIDTH }}
            className="group flex shrink-0 cursor-row-resize items-center justify-center"
          >
            <div
              className={
                isResizing
                  ? "h-px w-full bg-ring transition-colors"
                  : "h-px w-full bg-transparent transition-colors group-hover:bg-border"
              }
            />
          </div>
        ) : null}
        {/* Same treatment as SplitPanelSlot's own box — a raised content card,
            not the recessed bg-sidebar rail. A terminal or a task log reads as
            content, not as navigation chrome. */}
        <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border border-border bg-card">
          {content ?? (
            <p className="flex h-full items-center justify-center px-4 text-sm text-muted-foreground">
              This module has nothing to show down here yet.
            </p>
          )}
        </div>
      </m.div>
    </LazyMotion>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd dev-kit-app/frontend && npx tsc --noEmit`
Expected: no output.

- [ ] **Step 3: Commit**

```bash
cd dev-kit-app
git add frontend/src/components/ui/bottom-slot.tsx
git commit -m "feat(workbench): bottom slot surface and registration hook"
```

---

## Task 5: Wire it into the shell, and add the toggle

Two changes to `Workbench.tsx`: the content row gains a vertical wrapper so the slot can sit under it, and zone 3 of the title bar gains a third button.

**Files:**
- Modify: `frontend/src/workbench/Workbench.tsx`

- [ ] **Step 1: Add the imports**

In `frontend/src/workbench/Workbench.tsx`, change the lucide import on line 3 from:

```ts
import { PanelLeft, Search, Settings as SettingsIcon } from "lucide-react";
```

to:

```ts
import { PanelBottom, PanelLeft, Search, Settings as SettingsIcon } from "lucide-react";
```

Then, immediately after the existing line 39:

```ts
import { SidebarSurface, SidebarModuleIdContext } from "@/components/ui/sidebar-shell";
```

add:

```ts
import { BottomSlotSurface } from "@/components/ui/bottom-slot";
```

and immediately after the existing line 42:

```ts
import { useSidebarOpenStore } from "../stores/sidebarShell";
```

add:

```ts
import { useBottomSlotStore } from "../stores/bottomSlot";
```

- [ ] **Step 2: Subscribe to the store**

Immediately after these two existing lines (currently `Workbench.tsx:277-278`):

```ts
  const sidebarOpen = useSidebarOpenStore((s) => s.open);
  const setSidebarOpen = useSidebarOpenStore((s) => s.setOpen);
```

add:

```ts
  const bottomOpen = useBottomSlotStore((s) => s.open);
  const setBottomOpen = useBottomSlotStore((s) => s.setOpen);
```

- [ ] **Step 3: Restructure the content row**

Replace this block (currently `Workbench.tsx:628-641`):

```tsx
        {/* Transparent row: the main content card and the split panel are two
            independent bg-card boxes, separated by a gap that shows the same
            transparent/vibrancy background as the mx-1/mb-1 margins around
            this whole row — not one shared card with an internal divider. */}
        <div className="mx-1 mb-1 flex min-h-0 flex-1">
          <SidebarSurface activeModuleId={activeModule?.id ?? null} />
          <WorkbenchViews
            availableModules={availableModules}
            hasMetadataWarning={hasMetadataWarning}
            openTabs={openTabs}
            activeTabId={activeTabId}
          />
          <SplitPanelSlot />
        </div>
```

with:

```tsx
        {/* Transparent row: the main content card and the split panel are two
            independent bg-card boxes, separated by a gap that shows the same
            transparent/vibrancy background as the mx-1/mb-1 margins around
            this whole row — not one shared card with an internal divider.
            The bottom slot is a full-width sibling *under* all three, so the
            mx-1/mb-1 margins move out here to the column that holds both. */}
        <div className="mx-1 mb-1 flex min-h-0 flex-1 flex-col">
          <div className="flex min-h-0 flex-1">
            <SidebarSurface activeModuleId={activeModule?.id ?? null} />
            <WorkbenchViews
              availableModules={availableModules}
              hasMetadataWarning={hasMetadataWarning}
              openTabs={openTabs}
              activeTabId={activeTabId}
            />
            <SplitPanelSlot />
          </div>
          <BottomSlotSurface activeModuleId={activeModule?.id ?? null} />
        </div>
```

- [ ] **Step 4: Add the toggle button**

In zone 3 of the title bar, immediately after the closing `</Tooltip>` of the Settings button and before the `</div>` that closes the zone (currently `Workbench.tsx:625-626`), add:

```tsx
              {/* Zone 3, after Settings — the rightmost item in the title bar.
                  The sidebar toggle is pinned at the far left, outside the
                  three zones, because Figma's App Shell puts it there against
                  the traffic lights; this one has no such constraint, so the
                  two layout toggles bracket the bar at its outer edges.

                  Always rendered and always enabled, even when the active
                  module contributes no panel: it toggles the surface, and the
                  surface explains its own emptiness. Disabling it would make
                  the control flicker in and out as the user changes module. */}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label="Toggle bottom panel"
                    aria-pressed={bottomOpen}
                    className={cn(
                      bottomOpen && "bg-accent text-foreground",
                      "shrink-0 [--wails-draggable:no-drag]",
                    )}
                    onClick={() => setBottomOpen(!bottomOpen)}
                  >
                    <PanelBottom className="size-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Toggle bottom panel</TooltipContent>
              </Tooltip>
```

- [ ] **Step 5: Typecheck**

Run: `cd dev-kit-app/frontend && npx tsc --noEmit`
Expected: no output.

- [ ] **Step 6: Build and check the performance budget**

Run: `cd dev-kit-app/frontend && npm run build`
Expected: the last line reads `Performance budget: initial JS <n> KiB (<n> KiB gzip), CSS <n> KiB gzip.` and the command exits 0. The baseline before this plan is `174.5 KiB gzip` JS against a 225 limit; this task adds no new dependency, so a jump of more than ~2 KiB means something was imported that should not have been.

- [ ] **Step 7: Commit**

```bash
cd dev-kit-app
git add frontend/src/workbench/Workbench.tsx
git commit -m "feat(workbench): mount the bottom slot and its title-bar toggle"
```

---

## Task 6: Full regression

**Files:** none — verification only.

- [ ] **Step 1: Frontend suite**

Run: `cd dev-kit-app/frontend && npx tsc --noEmit && npx bun test`
Expected: `tsc` silent; `bun test` reports **44 pass, 0 fail** (36 before this plan, +3 in `panelLayout.test.ts`, +5 in `bottomContent.test.ts`).

- [ ] **Step 2: Backend suite**

Run:

```bash
cd dev-kit-app && go build $(go list ./... | grep -v '/build/ios') && go test $(go list ./... | grep -v '/build/ios')
```

Expected: all `ok`. Nothing in this plan touches Go — this only proves it. (`build/ios` is excluded because that package is an iOS-only Wails template that has never built on the host toolchain; it fails identically on `main`.)

- [ ] **Step 3: Run the app and check by hand**

Run: `cd dev-kit-app && task run:desktop` (or the project's usual desktop run task).

Check each of these:

1. The title bar's right edge shows three icons: Search, Settings, and a new panel icon. The panel icon is present in every module, including ones with no bottom content.
2. Clicking it opens a full-width surface at the bottom, over which the dock pill floats. It reads "This module has nothing to show down here yet."
3. Clicking it again closes the surface; the content row grows back to fill the space, animating over ~0.22s.
4. Dragging the 4px gutter along the surface's top edge resizes it. The cursor becomes `row-resize` while dragging. It stops growing at 600px, and at the point where the content row above would drop under 240px, whichever comes first.
5. With the slot open, drag it as tall as it will go, then drag the window's own bottom edge up until the window is short. The slot shrinks with the window and the content row above it never disappears. Make the window tall again: the slot returns to the height you dragged it to.
6. Open the slot, quit the app, reopen it: the slot is still open. Its *height* resets to 220 — that is intended (see `stores/bottomSlot.ts`).
7. The sidebar and the split panel still resize horizontally, with a `col-resize` cursor. This is what Task 1's rename could have broken.
8. The dock still expands on hover near the bottom of the window, and `⌘1`–`⌘9` still switch modules.

- [ ] **Step 4: Commit anything the manual pass turned up**

If steps 1–8 all pass, there is nothing to commit and this task is done. If one fails, fix it, re-run steps 1 and 2, and commit the fix on its own.

---

## What this plan does NOT do

**No terminal.** This is the largest deliberate omission, and it means the slot ships with no tenant: until a module calls `useBottomContent`, opening it shows only the empty state. That is the honest consequence of building the slot the user asked for rather than widening into the terminal. The follow-up plan is well-defined and mostly small, because the backend is already finished:

1. **`main.go:201`** — replace the `TODO(frontend Terminal component)` callback body with a real emit. The design doc (`base-doc/content/projects/dev-kit/terminal.mdx` § Transport) specifies `app.Event.Emit("terminal:output:<sessionId>", chunk)`. **Open question to settle first, by reading `@wailsio/runtime`'s current source rather than guessing:** how a Go `[]byte` survives the JSON hop — base64 string, number array, or something else — and therefore what the frontend must decode before `term.write()`.
2. **`frontend/src/bridge/terminal.ts`** — `terminalWrite`/`terminalResize`/`terminalClose` over `callBridge`, plus `onTerminalOutput(sessionId, cb)` over `Events.On`, following `bridge/agenthub.ts`'s existing subscriber shape.
3. **`@xterm/xterm` + `@xterm/addon-fit`** — new dependencies. They must land in a lazy chunk, not the initial bundle; the budget has ~50 KiB gzip of headroom and xterm alone would eat most of it if statically imported.
4. **A tenant.** `CodeGraphStartTerminal` requires an open project and puts the shell at the project root, so CodeGraph is the natural first one. **Design problem the terminal plan must solve, not this one:** switching modules unmounts CodeGraph's subtree entirely (this is why `modules/codegraph/codegraphStore.ts` exists), which would unregister the panel and, if the cleanup closes the session, kill a running `mvn test`. The session id has to outlive the view — the same fix `codegraphStore.ts` already applies to the rest of CodeGraph's state.

**No keyboard shortcut for the toggle.** VS Code's equivalent is `⌘J` and adding it is about five lines in `Workbench.tsx`'s existing `handleKeyDown`, next to the `⌘,` branch. Left out because it was not asked for.

**No tabs inside the slot.** One module, one panel. If the terminal later needs several sessions, that is a tab strip inside the tenant's own content, not a shell feature.

**No `shell-primitives.mdx` update.** `base-doc/content/projects/dev-kit/shell-primitives.mdx` documents "hai primitive dùng chung" and will be wrong the moment this ships — there will be three. Updating the design docs is a separate pass over `shell-primitives.mdx` and `title-bar.mdx` (whose zone-3 table lists only Search and Settings).

**No fix for the dock/slot pointer conflict.** The dock expands when the pointer comes within 32px of it, which is the bottom ~55px of the window — now the bottom ~55px of *this slot's content*. Reading the last lines of a terminal will pop the dock open over them. Real, introduced by this feature, and deliberately not solved here: the fix depends on what the slot actually contains, so it belongs to the terminal plan.
