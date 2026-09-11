# Dock Resting Pill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the dock's scaled-down resting state with a flat pill of per-module lines, activated by pointer proximity rather than a hit area, plus `⌘1`–`⌘9` module switching surfaced in each item's tooltip.

**Architecture:** `Dock.tsx` keeps its `fixed` overlay `nav` and gains two rendered forms — a collapsed pill (one 12×3 line per module, the active one lit) and the existing icon row — swapped on a single `expanded` boolean. That boolean is `pointerNear || focusWithin`. Proximity is computed from `pointermove` coordinates against the pill's own rect, deliberately **not** an invisible padded hit area: an invisible hitbox claims real pointer events and would break text selection near the bottom of the window. Shortcuts are registered once in `Workbench.tsx` beside `focusModule`, and passed down for display only.

**Tech Stack:** React 19, TypeScript, Tailwind v4, `framer-motion` (already the dock's animation lib), `bun test`.

**Design source:** Figma `Dock` component set `208:438` on page **07 App Shell** (`State = Collapsed | Expanded`), and the `App Shell` set `107:137` where the collapsed pill is already placed in all 8 variants.

---

## Design decisions already settled

Recorded so they are not re-derived or quietly reversed mid-implementation.

- **Resting form is a line-per-module pill, not a scaled-down dock.** The current `animate={{ scale: 0.55, y: 14 }}` shrinks the icon row; the new resting state renders different content entirely. Figma: collapsed **108×13**, expanded **228×48**.
- **Activation is by pointer distance, never a hit area.** A padded invisible `pointer-events-auto` zone would swallow clicks and drags in the bottom band and break text selection in whatever sits there — which, once the bottom slot ships, is terminal output. Distance decouples *how close you must be* from *what the dock intercepts*: the dock claims events only on the pixels it actually draws.
- **Rest means no pointer nearby AND no focus within.** The focus clause is what makes the dock keyboard-usable: tab into it and it expands, so an arrow-key user is not navigating an invisible 13px sliver.
- **Shortcuts appear in the per-item tooltip**, not inline in the pill. Accepted trade-off: a tooltip only teaches people who already hover, and hovering is meant to become rare. The command palette would be the better discovery surface, but `CommandPalette.tsx` is currently a stub — its `<CommandItem onSelect={onClose}>` only closes the palette and its list never included module switching. Wiring that is out of scope here.
- **Dashes track modules only, not `contextItems`.** The pill answers "which module am I in", so a module-contributed transient action must not add a line. The expanded row still shows both.
- **The dock overlays the bottom slot; no reserved band.** At 13px and centred it can only clip the tail of an unusually long last line. Nothing to do in code today — the bottom slot is not implemented yet — but do not add padding "to be safe" when it is.

## Verified before writing this plan

- **`⌘1`–`⌘9` is free at every layer that could intercept it**, checked in order of who sees the key first:
  - **OS.** Neither macOS nor Windows nor Linux reserves `Cmd`/`Ctrl`+digit — the system-level equivalents are `Win`/`Super`+digit (taskbar/workspace). `Cmd`/`Ctrl`+digit has always been app-scoped (Finder view modes, browser tabs, Slack workspaces, VS Code editor groups).
  - **Native menu.** `main.go` sets no menu and no `application.Options.KeyBindings`, but Wails installs `DefaultApplicationMenu()` when an app sets none (`application_darwin.go:306`), and a native menu accelerator fires *before* the webview ever sees the key. Enumerated every accelerator in that default menu: it binds **`CmdOrCtrl+0`** (Actual Size) but **no `CmdOrCtrl+1`–`9`**.
  - **App.** `@codemirror/*` binds no `Mod-<digit>`; the Plate packages and our editor config use only `mod+alt`, `mod+comma`, `mod+e`, `mod+period`, `mod+shift`; and none of the four existing `window.addEventListener("keydown")` handlers (`Workbench.tsx`, `TabStrip.tsx`, `SettingsView.tsx`, `components/ui/sidebar.tsx`) claim a digit.
- The dock is already a zero-height overlay: `nav` is `pointer-events-none fixed inset-x-0 bottom-6 z-50`, only the pill is `pointer-events-auto`. Nothing about "make it absolute" is needed.
- 7 modules reach the dock today: 9 registered minus `HEADER_ONLY_MODULE_IDS` (`settings`, `sync`).
- `Kbd` exists at `components/ui/kbd.tsx` and already styles itself for use inside a tooltip (`in-data-[slot=tooltip-content]:…`), so shortcut hints need no new component.

## File structure

```
frontend/src/workbench/dock/
  dockActivation.ts        pure proximity + shortcut mapping (tested)
  dockActivation.test.ts
  CollapsedDock.tsx        the resting line pill
  Dock.tsx                 (modify: expanded state, proximity, focus)
  DockItem.tsx             (modify: shortcut in tooltip)
  types.ts                 (modify: shortcut field)
frontend/src/workbench/Workbench.tsx   (modify: ⌘1–⌘9 handler)
```

---

## Task 1: Pure activation and shortcut logic (TDD)

Both bits of logic here are easy to get subtly wrong and impossible to eyeball: an off-by-one in the digit mapping picks the wrong module, and a wrong proximity test either never expands or expands from across the screen.

**Files:**
- Create: `frontend/src/workbench/dock/dockActivation.ts`
- Test: `frontend/src/workbench/dock/dockActivation.test.ts`

- [ ] **Step 1: Write the failing tests**

```ts
import { expect, test } from "bun:test";
import { isPointerNear, shortcutLabel, moduleIndexFromEvent } from "./dockActivation";

const rect = { left: 660, top: 880, right: 770, bottom: 893 };

test("a pointer inside the pill is near", () => {
  expect(isPointerNear({ x: 700, y: 885 }, rect, 64)).toBe(true);
});

test("a pointer within the activation margin is near", () => {
  expect(isPointerNear({ x: 700, y: 830 }, rect, 64)).toBe(true);
  expect(isPointerNear({ x: 610, y: 885 }, rect, 64)).toBe(true);
});

test("a pointer beyond the margin is not near", () => {
  expect(isPointerNear({ x: 700, y: 700 }, rect, 64)).toBe(false);
  expect(isPointerNear({ x: 200, y: 885 }, rect, 64)).toBe(false);
});

test("the margin applies on every side, including below the pill", () => {
  // The pill sits 8px off the window bottom; the few pixels under it must
  // still count, or approaching from the very edge never expands it.
  expect(isPointerNear({ x: 700, y: 899 }, rect, 64)).toBe(true);
});

test("shortcut labels are one-based", () => {
  expect(shortcutLabel(0)).toBe("⌘1");
  expect(shortcutLabel(6)).toBe("⌘7");
});

test("only the first nine modules get a shortcut", () => {
  expect(shortcutLabel(8)).toBe("⌘9");
  expect(shortcutLabel(9)).toBeNull();
});

test("a meta+digit event maps to a zero-based module index", () => {
  expect(moduleIndexFromEvent({ key: "1", metaKey: true, ctrlKey: false, altKey: false, shiftKey: false })).toBe(0);
  expect(moduleIndexFromEvent({ key: "7", metaKey: false, ctrlKey: true, altKey: false, shiftKey: false })).toBe(6);
});

test("digit zero is not a module shortcut", () => {
  // Not merely "there is no module 0": ⌘0 is Actual Size in the default macOS
  // menu Wails installs, and a native menu accelerator fires before the
  // webview sees the key. Claiming it here would be dead code.
  expect(moduleIndexFromEvent({ key: "0", metaKey: true, ctrlKey: false, altKey: false, shiftKey: false })).toBeNull();
});

test("modified variants are left alone for other handlers", () => {
  // ⌘⇧1 and ⌘⌥1 must stay available — claiming every ⌘-digit combination
  // would silently shadow shortcuts other surfaces may add later.
  expect(moduleIndexFromEvent({ key: "1", metaKey: true, ctrlKey: false, altKey: false, shiftKey: true })).toBeNull();
  expect(moduleIndexFromEvent({ key: "1", metaKey: true, ctrlKey: false, altKey: true, shiftKey: false })).toBeNull();
});

test("a bare digit is not a shortcut", () => {
  expect(moduleIndexFromEvent({ key: "1", metaKey: false, ctrlKey: false, altKey: false, shiftKey: false })).toBeNull();
});
```

- [ ] **Step 2: Run to confirm they fail**

Run: `cd frontend && bun test src/workbench/dock/dockActivation.test.ts`
Expected: FAIL — `Cannot find module './dockActivation'`.

- [ ] **Step 3: Implement**

```ts
export type Point = { x: number; y: number };
export type Bounds = { left: number; top: number; right: number; bottom: number };

/**
 * Whether the pointer is close enough to expand the dock.
 *
 * Deliberately a distance test against the pill's real rect rather than an
 * invisible padded hit area: a padded `pointer-events-auto` element would
 * intercept clicks and drags across the bottom band and break text selection
 * in whatever is rendered there. This keeps the activation distance generous
 * while the dock still claims events only where it actually draws.
 */
export function isPointerNear(pointer: Point, rect: Bounds, margin: number): boolean {
  return (
    pointer.x >= rect.left - margin &&
    pointer.x <= rect.right + margin &&
    pointer.y >= rect.top - margin &&
    pointer.y <= rect.bottom + margin
  );
}

/** Modules past the ninth get no shortcut — there is no ⌘10. */
export function shortcutLabel(index: number): string | null {
  return index >= 0 && index < 9 ? `⌘${index + 1}` : null;
}

type KeyLike = {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
};

/**
 * Maps ⌘1–⌘9 (or Ctrl on non-mac) to a zero-based module index. Accepts
 * either modifier rather than branching on navigator.platform, which is
 * unreliable inside Wails' WKWebView — the same reasoning Workbench.tsx's
 * existing ⌘⇧E handler documents.
 */
export function moduleIndexFromEvent(event: KeyLike): number | null {
  if (!(event.metaKey || event.ctrlKey)) return null;
  if (event.altKey || event.shiftKey) return null;
  if (!/^[1-9]$/.test(event.key)) return null;
  return Number(event.key) - 1;
}
```

- [ ] **Step 4: Run the tests**

Run: `cd frontend && bun test src/workbench/dock/dockActivation.test.ts`
Expected: 9 pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/workbench/dock/dockActivation.ts frontend/src/workbench/dock/dockActivation.test.ts
git commit -m "feat(dock): pure proximity and shortcut mapping"
```

---

## Task 2: The collapsed pill

**Files:**
- Create: `frontend/src/workbench/dock/CollapsedDock.tsx`

Geometry read off Figma `State=Collapsed`: padding `8px` horizontal / `5px` vertical, gap `4px`, dash `12×3` fully rounded, pill fully rounded.

- [ ] **Step 1: Write the component**

```tsx
import { cn } from "@/lib/utils";

/**
 * The dock at rest: one line per module, the active one lit. Small enough
 * that it can float over content — including the bottom slot's terminal —
 * without a reserved band.
 *
 * Lines only, no labels: position is the cue, and dock order is the fixed
 * registerModule order in Workbench.tsx, so it is learnable.
 */
export function CollapsedDock({
  count,
  activeIndex,
}: {
  count: number;
  activeIndex: number;
}) {
  return (
    <div className="flex items-center gap-1 px-2 py-[5px]">
      {Array.from({ length: count }, (_, i) => (
        <span
          key={i}
          className={cn(
            "h-[3px] w-3 rounded-full transition-colors",
            // action/primary is the same token --sidebar-primary already uses
            // for an active nav item, so "you are here" reads the same
            // everywhere in the app.
            i === activeIndex ? "bg-primary" : "bg-text-tertiary/45",
          )}
        />
      ))}
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors. If `bg-text-tertiary` does not resolve, check the `@theme` block in `src/index.css` for the emitted colour name and use what is actually declared — do not invent one.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/workbench/dock/CollapsedDock.tsx
git commit -m "feat(dock): collapsed line pill"
```

---

## Task 3: Shortcut hint on each dock item

**Files:**
- Modify: `frontend/src/workbench/dock/types.ts`
- Modify: `frontend/src/workbench/dock/DockItem.tsx`

- [ ] **Step 1: Add the field**

In `types.ts`, inside `DockEntry`, after `urgent?: boolean;`:

```ts
  /** Display only — e.g. "⌘3". The handler lives in Workbench.tsx. */
  shortcut?: string | null;
```

- [ ] **Step 2: Render it in the tooltip**

In `DockItem.tsx`, replace the final return with:

```tsx
  return (
    <Tooltip>
      <TooltipTrigger asChild>{button}</TooltipTrigger>
      <TooltipContent side="top">
        {item.title}
        {item.shortcut ? <Kbd className="ml-1.5">{item.shortcut}</Kbd> : null}
      </TooltipContent>
    </Tooltip>
  );
```

and add `import { Kbd } from "@/components/ui/kbd";` at the top. `Kbd` already carries tooltip-specific styling (`in-data-[slot=tooltip-content]:…`), so no extra classes are needed — this matches how `Workbench.tsx` renders `Commands ⌘K`.

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/workbench/dock/types.ts frontend/src/workbench/dock/DockItem.tsx
git commit -m "feat(dock): show the module shortcut in each item's tooltip"
```

---

## Task 4: Proximity + focus activation in Dock

**Files:**
- Modify: `frontend/src/workbench/dock/Dock.tsx`

Replaces the hover-with-leave-timeout and the `scale: 0.55` resting animation.

- [ ] **Step 1: Swap the state model**

Replace the `isHovered` state, `leaveTimeoutRef`, `handleHoverStart` and `handleHoverEnd` block (lines 23–44) with:

```tsx
  const pillRef = useRef<HTMLDivElement | null>(null);
  const [pointerNear, setPointerNear] = useState(false);
  const [focusWithin, setFocusWithin] = useState(false);
  const expanded = pointerNear || focusWithin;

  useEffect(() => {
    let frame = 0;
    function onPointerMove(event: PointerEvent) {
      // Cheap reject before touching layout: the dock lives at the bottom, so
      // most of the window's pointer traffic never needs a rect read.
      if (event.clientY < window.innerHeight - BOTTOM_ZONE) {
        if (pointerNear) setPointerNear(false);
        return;
      }
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        const rect = pillRef.current?.getBoundingClientRect();
        if (!rect) return;
        setPointerNear(isPointerNear({ x: event.clientX, y: event.clientY }, rect, ACTIVATION_MARGIN));
      });
    }
    window.addEventListener("pointermove", onPointerMove);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      if (frame) cancelAnimationFrame(frame);
    };
  }, [pointerNear]);
```

with these constants above the component:

```tsx
/** Pointer distance that expands the dock. Generous on purpose — it costs
 *  nothing, because the dock never claims events outside its own pixels. */
const ACTIVATION_MARGIN = 64;
/** Rows from the window bottom worth measuring at all. */
const BOTTOM_ZONE = 160;
```

and imports `isPointerNear` from `./dockActivation`, plus `useRef` (already imported).

- [ ] **Step 2: Swap the rendered form**

Replace the `<m.div>` block (lines 90–100) with:

```tsx
        <m.div
          ref={pillRef}
          layout
          onFocusCapture={() => setFocusWithin(true)}
          onBlurCapture={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
              setFocusWithin(false);
            }
          }}
          transition={{ type: "spring", stiffness: 300, damping: 30, mass: 0.8 }}
          className={cn(
            "pointer-events-auto flex items-center border border-foreground/10 bg-card/90 backdrop-blur-xl",
            expanded ? "gap-1 rounded-2xl p-1.5" : "rounded-full",
          )}
        >
          {expanded ? (
            items.map((item) => <DockItem key={item.id} item={item} />)
          ) : (
            <CollapsedDock
              count={baseItems.length}
              activeIndex={baseItems.findIndex((i) => i.isActive)}
            />
          )}
        </m.div>
```

`baseItems`, not `items`: a module-contributed `contextItem` must not add a line to a pill whose whole job is saying which module you are in.

- [ ] **Step 3: Attach the shortcut for display**

In the `baseItems` map, add to the returned object, after `urgent,`:

```tsx
      shortcut: shortcutLabel(index),
```

and change the map signature to `modules.map((module, index) => {`. Import `shortcutLabel` from `./dockActivation`.

- [ ] **Step 4: Move the pill closer to the edge**

Change the `nav` class `bottom-6` to `bottom-2`, matching the 8px bottom margin the Figma App Shell variants use for the resting pill.

- [ ] **Step 5: Typecheck and build**

Run: `cd frontend && bunx tsc --noEmit && bun run build:dev`
Expected: both succeed. Add `import { cn } from "@/lib/utils";` and `import { CollapsedDock } from "./CollapsedDock";` if the typechecker flags them missing.

- [ ] **Step 6: Verify by hand**

Run the app. Check:
- At rest the dock is a short pill of lines with one lit; it does **not** look like a shrunken icon row.
- Moving the pointer toward the bottom centre expands it before the pointer reaches it.
- Moving away collapses it, with no flicker while crossing the boundary.
- Selecting text elsewhere near the bottom of the window still works — drag a selection across the area beside the pill and confirm nothing is swallowed.
- Tab into the dock: it expands and stays expanded while focus is inside.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/workbench/dock/Dock.tsx
git commit -m "feat(dock): resting line pill with proximity and focus activation"
```

---

## Task 5: `⌘1`–`⌘9` module switching

**Files:**
- Modify: `frontend/src/workbench/Workbench.tsx`

The handler belongs here, next to `focusModule` and the existing `dockModules` list, so the shortcut and the dock order can never disagree.

- [ ] **Step 1: Handle the key**

Inside the existing `handleKeyDown` in `Workbench.tsx` (the effect around line 300 that already handles `⌘,` and `⌘⇧E`), add before the `⌘,` branch:

```tsx
      const moduleIndex = moduleIndexFromEvent(event);
      if (moduleIndex !== null) {
        const target = dockModulesRef.current[moduleIndex];
        if (target && !target.unavailable) {
          event.preventDefault();
          focusModuleRef.current(target.id);
        }
        return;
      }
```

- [ ] **Step 2: Give the handler current values without re-subscribing**

That effect has an empty dependency array on purpose, so it must not close over a stale `dockModules`/`focusModule`. Add beside the other refs in `Workbench()`:

```tsx
  const dockModulesRef = useRef(dockModules);
  dockModulesRef.current = dockModules;
  const focusModuleRef = useRef(focusModule);
  focusModuleRef.current = focusModule;
```

This mirrors what `moduleActions.ts` already does for `onClick` — read the live value through a ref so a long-lived listener never re-registers.

Import `moduleIndexFromEvent` from `./dock/dockActivation`, and `useRef` if not already imported.

- [ ] **Step 3: Typecheck and build**

Run: `cd frontend && bunx tsc --noEmit && bun run build:dev`
Expected: both succeed.

- [ ] **Step 4: Verify by hand**

Run the app:
- `⌘1` … `⌘7` switch modules, in the same left-to-right order the expanded dock shows.
- The lit line in the resting pill moves to match.
- Hover a dock icon: the tooltip shows its shortcut.
- `⌘8`/`⌘9` with only 7 modules do nothing (no crash, no blank view).
- Inside the Notes editor and the code editor, `⌘1` still switches modules and does not type or trigger an editor command.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/workbench/Workbench.tsx
git commit -m "feat(dock): switch modules with cmd+1..9"
```

---

## Task 6: Full verification

- [ ] **Step 1: Frontend**

Run: `cd frontend && bunx tsc --noEmit && bun test && bun run build`
Expected: typecheck clean, all tests pass, performance budget passes.

- [ ] **Step 2: Confirm nothing else regressed in the shell**

Run the app and check the neighbours this touched: `⌘K` still opens the palette, `⌘,` still opens settings, `⌘⇧E` still starts tab browsing, and the sidebar toggle still works.

- [ ] **Step 3: Commit any fix**

```bash
git add -A frontend/src
git commit -m "fix(dock): address verification findings"
```

---

## Deliberate limitations

- **No shortcut for modules past the ninth.** `shortcutLabel` returns null and the tooltip simply omits the hint. There is no ⌘10, and inventing a second modifier for the tail would be worse than nothing at 7 modules.
- **A future native menu can silently kill these shortcuts.** The app sets no menu today, so Wails' default one applies and leaves `⌘1`–`⌘9` alone. If a custom menu is ever added, a native accelerator on any of those digits will intercept the key before the webview and the module shortcut will simply stop working, with nothing in the frontend to debug. Whoever adds a menu must keep `⌘1`–`⌘9` unassigned — and note `⌘0` is already Actual Size.
- **Discovery still depends on hover.** Tooltips only teach people who hover, and the design intends hovering to become rare. The real fix is the command palette, which is a stub today (`onSelect={onClose}` does nothing and module switching was never in its list) — a separate piece of work.
- **The pill shows lines, not labels.** Orientation is positional. That is only learnable because dock order is the fixed `registerModule` order; if module order ever becomes user-sortable, revisit.
- **`contextItems` get no line and no shortcut.** They appear only in the expanded row.
- **Proximity uses a rect, not a true radial distance.** A rectangle expanded by the margin is what the tests describe and is indistinguishable in use; a circular falloff would only matter for a magnification effect this dock does not have.

## Self-review notes

- Every settled design decision maps to a task: line pill → Task 2, proximity → Tasks 1 and 4, focus clause → Task 4, tooltip shortcut → Task 3, key handling → Tasks 1 and 5, 8px bottom offset → Task 4 Step 4.
- `isPointerNear`, `shortcutLabel` and `moduleIndexFromEvent` are defined in Task 1 and used with identical signatures in Tasks 3, 4 and 5.
- The two things most likely to break silently — the digit-to-index mapping and the proximity boundary — are the two things covered by unit tests rather than by eyeballing.
- Nothing here touches the bottom slot: it does not exist in code yet, and the dock deliberately overlays it when it does.
