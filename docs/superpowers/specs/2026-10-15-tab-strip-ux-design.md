# Tab strip — overflow, status on every tab, reopen closed tabs

Status: approved in conversation 2026-10-15. Branch `feat/tab-strip-ux` (from `main`).
Code: `frontend/src/workbench/tabs/TabStrip.tsx`, `tabStripModel.ts`,
`frontend/src/stores/workbenchTabs.ts`.

## Problem

- **A preview tab has no close button** and ignores a middle click
  (`!tab.preview ? <close/> : null`, `onAuxClick … && !tab.preview`). The store
  closes a preview tab like any other, so nothing but the strip stands in the way.
- **Overflowing tabs are invisible.** The strip scrolls (`scrollbar-hide`) but
  shows no sign that more tabs exist on either side.
- **Only the active tab shows its status dot**, and nothing shows unsaved work:
  CodeGraph file buffers and API Client requests know they are dirty, the tab
  does not.
- **A closed tab cannot be reopened.**

## Decisions

| Question | Decision |
| --- | --- |
| Overflow | Edge fades + `‹ ›` scroll buttons, only while the strip overflows (option B). |
| Status / unsaved | VS Code style (option A): unsaved = `●` in the close slot, `✕` on hover; status (running / error / success) before the title on every tab. |
| Reopen | `⌘⇧T` / `Ctrl⇧T` reopens the last closed tab, repeatedly; a "Recently closed" list in the tab menu and the Command Palette. Memory only. |

## 0. Preview tabs close

A preview tab renders the same `✕` as any tab and closes on a middle click. Its
dashed border stays: that is what marks it as a preview.

## 1. Overflow

- Fades and buttons appear only while `scrollWidth > clientWidth`. A fade shows
  on a side only while there is something beyond it; a button is disabled when
  its side is fully scrolled.
- A button scrolls by 80% of the viewport, smoothly (`behavior: "smooth"`, or
  instant under `prefers-reduced-motion`). The wheel and the reveal-on-activate
  behaviour stay.
- Pure helpers in `tabStripModel.ts`: `overflowEdges({ scrollLeft, scrollWidth,
  clientWidth })` → `{ left: boolean; right: boolean }` (1px tolerance) and
  `scrollStep(view, direction)` → the next `scrollLeft`, clamped.
- The state follows scroll, resize (ResizeObserver) and tab-count changes.
- Fades are a mask on the scroller (no overlay that eats clicks), sized with
  existing tokens; buttons are `Button variant="titlebar" size="icon-sm"`, like
  the New tab button, with `aria-label` "Scroll tabs left/right".

## 2. Status and unsaved work on every tab

- **Store:** `WorkbenchTab.dirty?: boolean` and `setTabDirty(id, dirty)`,
  shaped like `setTabStatus` (no-op when unchanged, never persisted, like
  `status`). Any module may report it; this round wires:
  - CodeGraph `FileDocument`: dirty while its buffer differs from disk (the
    buffer model already knows).
  - API Client request documents: dirty while the editor holds unsaved changes
    (what its toolbar already shows).
- **Close slot:** `dirty` → a `●` dot (`--text-secondary`), which becomes the
  `✕` on hover or keyboard focus of the tab. A pinned dirty tab shows the dot,
  and the pin (unpin) on hover. Not dirty: as today (pin or `✕`).
- **Status, before the title, on every tab** (today: the active tab only, after
  the title):
  - `running`: a small spinning ring in `--status-running`; static under
    reduced motion.
  - `error`: a `--status-error` dot, until the status changes.
  - `success`: a `--status-success` dot on an inactive tab until that tab is
    activated (display-side "seen" set; the store's status is not rewritten).
    The active tab never shows it.
  - `idle`: nothing.
- **Tooltip** of the tab: title, then "Unsaved changes" / "Running" / "Failed"
  / "Finished" lines as they apply.
- **Closing a dirty tab** does not ask. CodeGraph drafts survive in memory
  (`docs/design/codegraph-workspace.md` § 10) and the reopened tab finds them;
  whatever a module keeps across a close is what reopening restores.

## 3. Reopen closed tabs

- **Store:** `recentlyClosed: ClosedTab[]` (newest first, ≤ 20, memory only,
  not persisted), `ClosedTab = { moduleId, viewId, title, params, icon?,
  pinned, groupId, index, closedAt }` where `index` is the position in its
  group's strip. `closeTab` records the tab before removing it; preview tabs
  are not recorded. A record whose `(moduleId, viewId, params)` matches a tab
  being opened (`openTab` / `reopen`) is dropped from the stack.
- **`reopenClosedTab(at = 0)`:** removes the record and opens it non-preview:
  in its old group when that group still exists, else the focused group; at its
  old index in that group's strip (clamped); pinned again if it was. Returns the
  tab id, or `null` with an empty stack.
- Group closes (Close Others / Left / Right / All) record every tab they close,
  in strip order, so `⌘⇧T` brings them back one by one, last closed first.
- **Shortcut:** `⌘⇧T` / `Ctrl⇧T` in `Workbench.tsx`'s keydown handler (free
  today), listed on Settings → Keyboard.
- **UI:**
  - The tab context menu gains "Reopen Closed Tab" (with the shortcut) and a
    "Recently Closed" submenu: up to 10 entries, module icon + title,
    newest first; disabled when empty.
  - Command Palette: a "Reopen Closed Tab" command, and the recent entries as
    "Reopen: <title>" items.

## Error handling

- Reopening a tab whose view no longer exists (a module disabled since): the
  record is dropped and a toast says so; nothing opens.
- Scroll state is recomputed on every change; a strip that stops overflowing
  hides its buttons and fades at once.

## Testing

- `tabStripModel.test.ts`: `overflowEdges` (none / left / right / both,
  tolerance), `scrollStep` (both directions, clamping).
- `workbenchTabs` tests: `setTabDirty`; `closeTab` records (preview not
  recorded, cap 20, dedupe on open); `reopenClosedTab` restores group, index,
  pin, and falls back to the focused group; group closes record in order.
- Render tests (`TabStrip`): a preview tab has a close button; a dirty tab shows
  the dot; status marks on inactive tabs; success hidden on the active tab;
  overflow buttons only when overflowing (driven through the pure helper).
- Manual: server mode — many tabs (fades, buttons), an Agent session running,
  a dirty CodeGraph file, `⌘⇧T` after closing tabs in a split.

## Addendum — later requests (2026-10-15)

Asked for after the design above was approved; implemented on the same branch.

- **An even strip.** The strip stays 40px; tabs are 28px
  (`--control-height-sm`, the drag ghost's and the ‹ › / + buttons' height),
  6px from all four sides (`p-1.5`), 6px apart (`TAB_GAP_PX`) and 6px from
  ‹ ›, + and the split's close-group button. The 6px tab radius is then
  concentric in the pane's 12px corner. The tab row pads itself 2px and gives
  it back in negative margin (`RING_PX`) so the ⌘⇧E focus ring is no longer
  clipped by the scroller.
- **HTTP method chips** (`frontend/src/lib/httpMethod.ts`): one Postman-like
  colouring (GET green, POST yellow, PUT/PATCH blue, DELETE red, anything else
  neutral) wherever a method is written — the API Client's rail and picker,
  CodeGraph's entry points and flow header. Colour says which method, so it is
  a palette hue (`--hue-*`), never `--status-*`. A chip is the hue as text on
  the hue tinted into `--surface-base` by `--method-tint` (10% light, 13%
  dark), the most that keeps every palette's hue at 4.5:1 on its own tint;
  `themeContrast.test.ts` checks it. A tinted chip is never dimmed with
  opacity: CodeGraph's filtered-out verb chips use the neutral chip instead.
- **Flow header inset.** CodeGraph's flow header follows the strip: `px-1.5`
  (6px), so its first control lines up with the first tab.
- **`success` is produced.** A completed Agent Hub turn marks the session's
  tabs `success` (cancelled/denied stay `idle`); the strip's seen set clears
  the dot once any group shows the tab. The rail's unread badge is separate
  and unchanged.
- **Tool failures no longer mark `error`.** A failed tool call is routine — the
  agent reads it and goes on — so a session's tabs stay `running`; only a
  failed turn or session marks `error`. Only the session's own tabs are marked;
  the "New session" tab never is.

