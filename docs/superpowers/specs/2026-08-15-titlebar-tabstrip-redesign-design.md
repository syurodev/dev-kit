# Titlebar 3-column layout + fisheye/color tab strip

**Date:** 2026-08-15
**Status:** Draft — pending user review
**Builds on:** supersedes `docs/superpowers/specs/2026-08-15-tab-strip-module-grouping-design.md`
(the collapse-to-chip design, implemented in `112e5ea`/`9e95c58`/`cf912bc`) —
this spec removes that mechanism entirely in favor of the approach below.
**Companion spec:** the keyboard quick-switcher popup (`⌘⇧E`/`Ctrl⇧E`,
escalating small → large popup) is a separate, independent spec — see
`docs/superpowers/specs/2026-08-15-tab-quick-switcher-design.md` — split out
because it doesn't depend on how the tab strip itself renders.

## Context

The collapse-to-chip design (previous spec) solved cross-module clutter by
hiding inactive modules' tabs behind a chip. Working through further UX
questions in conversation surfaced two things:

1. With the newer ideas in play — tabs narrow by **width** (not a chip) as
   they get farther from the active tab, and the whole strip can be
   revealed on hover or via a keyboard quick-switcher — the chip's "hide
   entirely" behavior became redundant. The user explicitly chose to drop
   it: tabs should always render individually, never collapse to an icon
   representing multiple tabs.
2. The user wants the titlebar itself restructured into three zones — a
   left status area (reserved, not built yet), the tab strip centered in
   the middle, and the existing action buttons on the right — evenly
   split so the tab strip visually anchors the titlebar.

## Goals

1. **Titlebar becomes a 3-column layout**: left (status, reserved/empty for
   now) / middle (tab strip) / right (existing action buttons), with the
   middle column exactly 3x the width of each side column.
2. **No collapse state at all.** Every open tab always renders as an
   individual, clickable, draggable element — the `CollapsedChip`/
   `pinnedModuleIds`/collapse-chevron mechanism from the superseded spec is
   deleted, not adapted.
3. **Tabs stay clustered by module** (unchanged from the superseded spec's
   `groupTabs()`) and each module gets a distinct **background color**,
   applied to every one of its tabs at two opacity levels: ~30% for
   inactive tabs, ~70% for the active tab.
4. **Fisheye width falloff**: the active tab renders at its full/natural
   width; every other tab's width is `100% - 5% × distance` from the
   active tab in the visual (left-to-right, post-clustering) tab order,
   floored at 30%. Below-floor tabs keep their icon at full size and let
   the title truncate away — the icon is never sacrificed for the title.
5. **Hovering anywhere over the tab-strip viewport** (not a specific tab)
   temporarily suspends the fisheye falloff — all tabs render at equal,
   fully-readable width for scanning/scrolling/clicking — reusing the same
   viewport-level hover pattern `Dock.tsx` already uses (`onHoverStart`/
   `onHoverEnd` on the container, not per-item).
6. **Drag-to-reorder stays scoped to one module's own tabs**, unchanged
   from the superseded spec's per-group `Reorder.Group` approach and the
   `reorderTab(fromId, beforeId)` store API — dragging a tab out of its
   module's color cluster isn't supported (would break the "same module,
   same place, same color" guarantee).

## Non-goals

- **Status area content.** The left column is reserved layout space only —
  empty in this pass. What it eventually shows (agent status, index
  status, etc.) is explicitly out of scope; confirmed by the user ("chưa
  làm nên đừng quan tâm" — not built yet, don't worry about it).
- **The keyboard quick-switcher popup.** Separate spec (see Companion
  spec above).
- **Exact hue/saturation/lightness tuning, exact base tab width in px, and
  exact hover-highlight treatment on top of the module tint.** These are
  visual-polish parameters explicitly left to implementation-time
  adjustment, same convention as prior specs in this series — the spec
  fixes the *mechanism* (hash-derived hue, 30%/70% alpha, 5%-per-step/30%-
  floor width falloff) and the exact numbers given, not pixel-perfect
  final values.
- **Split view.** Not built here; nothing in this spec re-introduces a
  single "active module" assumption that would block it later — see
  Architecture's note on where "active tab" is read from.
- **Persisting module colors, or letting the user override them.** Colors
  are derived deterministically from `moduleId`; no settings UI for this.

## Decisions validated with user

- **Chip mechanism removed outright, not layered on top of the new
  ideas.** Confirmed explicitly: once width-based narrowing + hover-reveal
  + a keyboard switcher exist, hiding tabs behind a chip is redundant
  complexity solving a problem the other three mechanisms already cover.
- **Fisheye is a real width change, not a `transform: scale()`.** The user
  explicitly corrected an earlier framing that compared this to `Dock.tsx`'s
  icon magnification (a `scale` transform): "các tab ở đây không giống dock
  chỉ scale chiều x" — text must stay crisp (normal font size) and
  *truncate* as its container narrows, not shrink into unreadably small
  scaled-down text. This is why the mechanism is a `width`/`max-width`
  change, not `scale`.
- **Icon always wins over title when space is tight.** Explicit: "luôn luôn
  ưu tiên hiện icon, scale sẻ cắt bỏ title" — the icon's own size never
  changes; only the title's available space shrinks, truncating via the
  existing `truncate` CSS (already on the title `<span>`) rather than any
  new icon-hiding logic.
- **30% floor**, not floorless shrinking — otherwise a tab far from active
  in a long row could shrink toward zero width. Confirmed explicitly.
- **Hover-to-reveal is viewport-level, not per-tab** — confirmed in an
  earlier turn specifically to avoid the friction of having to precisely
  aim at an already-narrow tab before you can read/click it; moving the
  mouse toward the strip at all (which you're already doing to interact
  with it) is what reveals everything.
- **Grid (`1fr 3fr 1fr`), not flex-grow ratios, for the 3-column
  titlebar.** Flex-grow ratios only distribute *remaining* space after
  content sizing, which doesn't guarantee an exact 1:3:1 split if a
  column's content is wider or narrower than its share — CSS Grid's `fr`
  unit divides the full track width by ratio regardless of content,
  which is what "2 bên đều nhau" (both sides even) actually requires. Not
  explicitly asked by the user in these words, but chosen as the correct
  implementation of the ratio they described; flagged here per the
  "ship the lazy-correct version, note the call" pattern already used
  earlier in this session.

## Architecture

### Module color — new pure function, no manifest changes

Rather than adding a `color` field to every `ModuleManifest` (8 modules to
touch, and a real risk of two authors picking colors that clash), the
color is derived deterministically from `moduleId`:

```ts
// frontend/src/workbench/tabs/moduleColor.ts (new file)
export function moduleHue(moduleId: string): number {
  let hash = 0;
  for (let i = 0; i < moduleId.length; i++) {
    hash = (hash * 31 + moduleId.charCodeAt(i)) | 0;
  }
  return Math.abs(hash) % 360;
}
```

Consumed at render time as `hsl(${moduleHue(tab.moduleId)} 55% 45% / ${alpha})`
where `alpha` is `0.7` for the active tab, `0.3` for every other tab of
that module (exact `55% 45%` saturation/lightness is the implementation-
time-tunable part called out in Non-goals — needs to read acceptably on
both the light and dark theme tokens already in `app.css`).

### `frontend/src/workbench/tabs/TabStrip.tsx` — remove collapse, add fisheye + color

**Deleted entirely:** `CollapsedChip`, `pinnedModuleIds` state, `pinModule`/
`unpinModule`, the collapse chevron button, the `expandedModuleIds` union,
and the `motion.div` chip↔group swap animation — none of it is needed once
every tab always renders.

**Kept, adapted:** `groupTabs()` (unchanged — still clusters same-module
tabs contiguously in first-open order) and the per-group `Reorder.Group`
drag mechanics from the old `ExpandedGroup` (renamed `TabGroupRow` since
"expanded" no longer means anything when there's no collapsed alternative)
— including the drag no-op guard fixed earlier this session.

**New: fisheye distance + width, computed once per render in `TabStrip`,
passed down to each tab:**

```ts
const FISHEYE_STEP_PERCENT = 5;
const FISHEYE_FLOOR_PERCENT = 30;
const TAB_BASE_WIDTH_PX = 140; // implementation-time tunable

function fisheyeWidthPx(distance: number): number {
  const percent = Math.max(
    FISHEYE_FLOOR_PERCENT,
    100 - distance * FISHEYE_STEP_PERCENT,
  );
  return Math.round((TAB_BASE_WIDTH_PX * percent) / 100);
}
```

Distance is computed over the **visual** (post-clustering) order, not the
raw `openTabs` array order, because "gần tab active" (close to the active
tab) means spatial left-right adjacency in what's actually rendered:

```ts
const visualOrder = groups.flatMap((group) => group.tabs);
const activeIndex = visualOrder.findIndex((tab) => tab.id === activeTabId);
// distance for a tab at visualOrder[i]: Math.abs(i - activeIndex)
```

`TabGroupRow` receives each tab's precomputed `widthPx` (or `null` to mean
"natural/unconstrained," used for the active tab and whenever the
viewport-hover suspension below is active) and applies it as an inline
`style={{ width: widthPx ?? undefined }}` — a `motion` `animate`d width
(not `layout="position"`, which only animates position) so the resize
itself is smooth, using the same `duration: 0.18, ease: "easeOut"` already
used for the (now-removed) chip/group swap elsewhere in this file.

**Viewport hover suspends fisheye:** `TabStrip`'s root container gets
`onPointerEnter`/`onPointerLeave` (viewport-level, matching `Dock.tsx`'s
`onHoverStart`/`onHoverEnd` placement on the container rather than per-item)
toggling a local `isBarHovered` boolean. While true, every tab's `widthPx`
resolves to `null` (natural width) regardless of computed fisheye distance.
No special handling is needed for "don't fisheye-shrink mid-drag": you
can't start a drag without your pointer already being over the strip, so
`isBarHovered` is already `true` (fisheye already suspended) by the time
any `Reorder.Item` drag could begin — the two behaviors compose for free.

**Active-tab styling replaced.** The existing `tab.id === activeTabId ?
"bg-card text-foreground" : ... "text-muted-foreground"` className branch
is replaced by the module-hue background (inline `style`, since the hue is
a runtime-computed value, not a static Tailwind class) at 0.7 alpha for the
active tab and 0.3 for others; icon/status-dot/title/close-button markup is
otherwise unchanged from today.

### `frontend/src/workbench/Workbench.tsx` — 3-column header

Find the `<header>`'s content (currently: absolute drag-handle div, then
`TabStrip`, `moduleActions`, the metadata-loading label, Commands, Settings
all as flat siblings). Wrap everything **after** the absolute drag-handle
div in a 3-column grid:

```tsx
<div className="grid w-full grid-cols-[1fr_3fr_1fr] items-center gap-2">
  <div /> {/* left: status, reserved — empty in this pass */}
  <TabStrip
    tabs={openTabs}
    activeTabId={activeTabId}
    onSelect={setActiveTab}
    onClose={closeTab}
    onReorder={reorderTab}
  />
  <div className="flex min-w-0 items-center justify-end gap-2">
    {/* moduleActions.map(...), metadataLoading label, Commands button,
        Settings button — unchanged markup, just moved inside this
        right-column wrapper */}
  </div>
</div>
```

`TabStrip`'s own root needs `min-w-0` (it already has this from `flex-1
min-w-0` today; keep `min-w-0`, drop `flex-1` since the grid column — not
flex-grow — now determines its width) so its internal `overflow-x-auto`
still works instead of the grid track stretching to fit unclamped content.
The absolute-positioned drag-handle div and the header's `pl-20`/`pl-1`
fullscreen-aware padding are untouched — they operate independently of
whatever's inside the padded content area.

## Error handling

No new failure modes. `moduleHue` is a pure function over a string,
`fisheyeWidthPx` is pure arithmetic with an explicit floor — neither can
throw or return an out-of-range value for any real `moduleId`/`distance`.
The `visualOrder.findIndex` for `activeIndex` can return `-1` (no tabs open,
or `activeTabId` stale for one render) — every tab's `distance` becomes
`Math.abs(i - (-1))`, i.e. shifted by one but still a bounded, sane number
that only matters when `openTabs` is empty anyway (nothing renders).

## Testing / verification

No frontend unit test runner in this repo (unchanged convention).
Verification is `cd frontend && bun run build` plus a manual QA pass:

1. **3-column layout.** Titlebar shows an empty left third, tab strip
   centered in a track 3x as wide, action buttons (Commands, Settings,
   any module actions) right-aligned in the final third. Resizing the
   window keeps the 1:3:1 ratio.
2. **No collapse anywhere.** Open 8+ tabs across 3+ modules — every tab is
   individually visible and clickable at all times; there is no chip, no
   chevron, no hidden-tabs state to discover.
3. **Clustering + color.** Notes tabs are visually contiguous and share
   one background hue; Database tabs are contiguous with a different hue;
   colors are visually distinguishable from each other.
4. **Active vs inactive opacity.** The active tab's background reads
   noticeably more saturated/solid than its own module's inactive tabs
   (0.7 vs 0.3 alpha of the same hue).
5. **Fisheye falloff at rest.** With 6+ tabs open and one active roughly in
   the middle, tabs get visibly narrower moving away from the active tab
   in both directions, bottoming out (not continuing to shrink) at the
   3rd–4th tab away. Icons stay full-size throughout; only titles
   truncate.
6. **Hover reveals everything.** Move the pointer onto the tab strip
   (anywhere, not a specific narrow tab) — all tabs animate to equal,
   fully-readable width. Move the pointer off — fisheye falloff resumes
   around whatever tab is active.
7. **Drag still scoped per module.** Dragging a tab only reorders it among
   its own module's tabs; there's no way to drag it into a different
   module's color cluster.
8. **Switching active tab re-centers the fisheye.** Click a tab far from
   the previous active tab — the falloff recomputes around the new active
   tab, animating each tab's width smoothly rather than snapping.

## Out of scope recap

Status-area content, the keyboard quick-switcher (companion spec), split
view, exact color/width visual tuning, user-configurable module colors.
