# Workbench tabs: preview/pin lifecycle, per-tab icon override, drag-to-reorder

**Date:** 2026-08-15
**Status:** Draft — pending user review
**Builds on:** `docs/superpowers/plans/2026-08-15-workbench-tab-strip.md` (Phase 1
— generic tab strip infra + module-level tabs, implemented and committed:
`4aaec7f`, `93e080d`, `6be8c87`, `9e204f3`, `9605968`) and its published docs
(`/dev-kit/workbench-tabs`, ADR-012 in `architecture-decisions.mdx`). Phase 1
explicitly deferred "per-item tabs" as a follow-up; this spec is that
follow-up, scoped to Notes as the first (and only, in this pass) consumer.
Agent Hub per-session tabs + live status remain deferred — separate spec.

## Context

Phase 1 gave every module exactly one tab (its "home" view, opened via Dock
click or the Settings icon). The user wants Notes specifically to behave
like a real per-document tab bar: opening Note 1 makes a tab titled "Note
1"; opening Note 2 (while Note 1's tab is still open) makes a **second,
separate tab** "Note 2" — not a replace. Three requirements emerged while
discussing how that should actually behave, all resolved through Q&A before
this spec:

1. What decides "reuse this tab" vs "open a new tab" when clicking a note in
   the list — resolved as VS Code's **preview tab** model (see Decisions).
2. A tab should be able to show something other than its module's default
   icon — resolved as a **generic** per-tab icon override (any module, not
   Notes-specific color logic).
3. Tabs should be **drag-reorderable**.

A fourth question came up organically while working through the design: what
happens to already-open item tabs (e.g. a pinned "Note 1" tab) when the user
clicks the Notes **Dock** icon while on a different module — resolved as "no
change from Phase 1" (see Decisions).

## Goals

1. **Preview tab lifecycle** in `useWorkbenchTabsStore` — one global preview
   slot (not per-module), replaced in place by the next preview-open, and
   promotable to a permanent ("pinned") tab by an explicit action.
2. **Generic per-tab icon override** — `WorkbenchTab.icon?: LucideIcon`,
   settable by any module through `openTab()`, falling back to the module's
   registered `ModuleManifest.icon` when absent.
3. **Notes as the first per-item-tab consumer** — clicking a note in
   `NotesDashboard` opens a preview tab for that note; double-clicking (or
   editing its content) pins it; `NotesView`'s internal dashboard↔editor
   switch is replaced by two real `ModuleView`s (`notes.list`,
   `notes.editor`) driven through the tabs store.
4. **Drag-to-reorder** any open tab via pointer events, live-updating
   `openTabs` order as the drag crosses neighbor midpoints, reusing the
   `layout` animation already on each tab from Phase 1 — independent of
   preview/pin state.

## Non-goals

- **Agent Hub per-session tabs / live tab status** — still deferred, tracked
  as a separate follow-up in `/dev-kit/workbench-tabs`'s Scope section;
  nothing in this pass changes that.
- **Per-module "last active tab" memory for Dock/Settings** — explicitly
  rejected by the user. Dock click and the Settings icon both keep calling
  `focusModule(moduleId)` exactly as Phase 1 built it: always open-or-focus
  the module's `views[0]` ("home") tab, a fixed `tabKey`, completely
  independent of whatever item tabs (pinned or preview) that module already
  has open. No new state to track, no behavior change to that path at all.
- **framer-motion `Reorder.Group`/`Reorder.Item`** — considered and
  rejected; see Decisions. Drag-to-reorder is hand-rolled pointer tracking.
- **Drag changing preview/pin state** — explicitly orthogonal, confirmed by
  the user. Dragging a preview (italic) tab moves its position only; it
  stays a preview tab and is still replaceable by the next preview-open.
- **Persisting `openTabs`/order across app restart** — not raised, stays
  out of scope (same as Phase 1's plan explicitly deferred this).
- **Command palette (`notes.create`) or any other entry point wiring into
  preview/pin logic** — only the dashboard note-card click/double-click is
  in scope. Other ways of reaching a note (if any exist later) are not
  addressed here.
- **Tab overflow menu, multi-select, or vertical/multi-row tab strip** — not
  raised; the single-row horizontal-scroll strip from Phase 1 is unchanged.

## Decisions validated with user

- **Preview tab model (VS Code), not "always open a new tab."** Single
  click on a note in `NotesDashboard` opens (or replaces) the one global
  preview tab — browsing many notes via single click never accumulates
  tabs. Double-click, or the user typing into the note, promotes it to a
  permanent tab. Confirmed over the simpler "every click = new/focused tab,
  no preview concept" alternative, specifically to avoid tab-spam from
  casual browsing while still letting deliberate multi-note work (compare
  two notes side by side in the tab strip) create real, independent tabs.
- **Exactly one preview slot, global across the whole strip — not one per
  module.** The tab strip is a single row with no per-module grouping, so
  previewing a note while a Database connection tab happens to also be in
  preview state would replace that one, matching "one editor group" from
  VS Code (this app has no split tab groups to preserve separate preview
  slots for).
- **Icon override is generic (any module can pass one), not Notes-specific
  per-note color.** The user explicitly chose the general
  `openTab({..., icon})` shape over a Notes-only "show the note's own color
  as a dot instead of an icon" alternative — even though Notes already has
  per-note color data (`noteColor.ts`) that could have driven this, this
  pass doesn't use it; whether/how Notes actually sets `icon` per tab is
  left to whoever wires it up (default: omitted, tab shows the module icon
  like every other tab today).
- **Dock/Settings unchanged.** Confirmed via a concrete example (pinned
  "Note 1" tab open, user on SSH module, clicks Notes Dock icon): the
  correct result is opening/focusing the Notes **dashboard** tab as its own
  separate tab, not jumping to "Note 1". Rejected the alternative
  (remembering per-module last-active tab) as unnecessary complexity for a
  behavior the user didn't actually want.
- **Drag-to-reorder implemented by hand, not framer-motion's `Reorder`.**
  `Reorder.Group`/`Reorder.Item` aren't usable through the `LazyMotion`/`m`
  lazy-loading pattern this codebase deliberately uses for `TabStrip`/`Dock`
  to keep bundle size down (`Reorder` requires importing the full
  top-level API). Chose manual `pointerdown`/global `pointermove`/
  `pointerup` tracking instead — the same technique `SplitPanel`'s gutter
  resize already uses in this codebase (`shell-primitives.mdx`) — combined
  with the `layout` animation already on every tab from Phase 1, which
  needs no new code to animate the reflow once the array order changes.
- **Drag never changes preview/pin state.** Matches actual VS Code
  behavior: dragging the preview tab to a new position leaves it in
  preview (still italic, still replaceable), confirmed over the
  alternative of treating a drag as an implicit "pin" action.

## Architecture

### `frontend/src/stores/workbenchTabs.ts` — type changes

```ts
export type WorkbenchTab = {
  id: string;
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  status?: TabStatus;
  icon?: LucideIcon;   // NEW — per-tab override; TabStrip falls back to the
                        // module's registered icon when this is absent
  preview?: boolean;   // NEW — true = replaceable "preview" tab (italic)
};

export type OpenTabInput = {
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  icon?: LucideIcon;
  preview?: boolean;   // default false — every Phase 1 caller (Dock,
                        // Settings) keeps calling openTab() with no preview
                        // field at all, so its behavior is 100% unchanged
};
```

### `openTab` — preview-aware open-or-focus

Replaces the Phase 1 body. Pseudocode (exact branching the implementation
must match):

```
function openTab(input):
  id = tabKey(input.moduleId, input.viewId, input.params)
  existing = openTabs.find(t => t.id === id)

  if existing:
    if input.preview === false and existing.preview === true:
      # explicit non-preview open of the CURRENT preview tab -> promote
      # in place, same array position, no move
      existing.preview = false
    setActiveTab(id)
    return id

  if input.preview === true:
    currentPreview = openTabs.find(t => t.preview === true)
    if currentPreview:
      # replace the existing preview tab's slot in place (same index)
      replace currentPreview with { id, moduleId, viewId, title, params, icon, preview: true }
    else:
      # no preview open — append as a new preview tab
      push { id, moduleId, viewId, title, params, icon, preview: true }
    setActiveTab(id)
    return id

  # preview: false/omitted and tab doesn't exist yet — normal permanent
  # tab, appended, exactly like every Phase 1 call site already behaves
  push { id, moduleId, viewId, title, params, icon, preview: false }
  setActiveTab(id)
  return id
```

Two new actions:

```ts
pinTab: (id: string) => void;
// set({ openTabs: state.openTabs.map(t => t.id === id ? { ...t, preview: false } : t) })
// Used by NoteEditor on first content edit — promotes without needing to
// re-supply moduleId/viewId/title/params just to call openTab() again.

reorderTab: (fromId: string, toIndex: number) => void;
// splice-move: find fromId's current index, remove it, re-insert at
// toIndex. No-op if fromId isn't found or toIndex === current index.
```

### `TabStrip.tsx` changes

- **Preview styling:** `tab.preview ? "italic" : ""` on the title `<span>`.
- **Icon resolution:** `tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench`
  — a tab-level override wins over the module default when present.
- **Drag-to-reorder:**
  - `onPointerDown` on the tab's own root element (NOT the close-button
    span — its own click handler must keep working unmolested) records
    the pointer's start x and the tab's id. A ~5px movement threshold
    before the interaction counts as a drag, so a normal click-to-select
    doesn't accidentally trigger reorder mode.
  - Once past the threshold: `document`-level `pointermove` reads the
    current pointer x, measures every tab's `getBoundingClientRect()`
    (cheap — module count is small, same reasoning Phase 1's code review
    already applied to `getModule`/`views.find` per render), and compares
    the dragged tab's center against each neighbor's midpoint. Crossing a
    neighbor's midpoint calls `reorderTab(draggedId, thatNeighborIndex)`
    immediately — not deferred to drop — so the rest of the strip
    reflows live as the user drags, exactly like VS Code/Chrome tabs.
  - The dragged tab itself is translated via an inline `transform:
    translateX(...)` tracking the pointer 1:1, with its own `layout`
    transition suppressed while dragging is active (mirrors
    `SplitPanel`'s resize-drag: no spring animation fighting a
    frame-by-frame pointer-driven position). Every other tab keeps its
    normal `layout` spring transition, so they visibly slide out of the
    way as the drag passes over them.
  - `document`-level `pointerup` clears drag state; the dragged tab's own
    `layout` transition re-enables and it settles into its final slot
    like any other `layout`-animated element.
  - No interaction with `--wails-draggable`: that CSS property only
    controls the native OS window-move gesture on `mousedown`; this
    pointer-tracking logic runs independently in JS on the same
    already-`no-drag` element and isn't affected by it either way.

### Notes wiring

- `frontend/src/modules/notes/manifest.ts` — add a second view:
  ```ts
  views: [
    { id: "notes.list", title: "Notes", component: NotesView },
    { id: "notes.editor", title: "Note", component: NoteEditorTab },
  ]
  ```
- `frontend/src/modules/notes/NotesView.tsx` — drop the internal
  `openId`/`useState` switch entirely; this component becomes just
  `NotesDashboard` wired to `onChanged`, nothing else (no more rendering
  `NoteEditor` itself under any condition).
- New thin component (e.g. `NoteEditorTab.tsx`, or inlined if small enough)
  registered as `notes.editor`'s `component` — reads
  `params?.noteId as string`, renders the existing `NoteEditor` with it.
  `NoteEditor`'s own internals (autosave, Plate editor, delete flow) are
  unchanged; only how it gets mounted changes.
- `NotesDashboard`/`NoteCard` — the click handler currently wired to
  `onOpenNote(noteId)` (a local `setOpenId` call in the old `NotesView`)
  changes to call the tabs store directly:
  ```ts
  openTab({
    moduleId: "notes",
    viewId: "notes.editor",
    title: note.title || "Untitled",
    params: { noteId: note.id },
    preview: true,
  });
  ```
  `NoteCard` needs a new `onDoubleClick` (doesn't exist today) calling the
  same with `preview: false`.
- `NoteEditor` — on the first real content edit (the existing
  autosave-trigger path, not on initial mount/load), call:
  ```ts
  pinTab(tabKey("notes", "notes.editor", { noteId }));
  ```
  guarded so it only fires once per note-open, not on every keystroke
  (autosave already debounces separately; this just needs to fire on the
  transition from "no edits yet" to "user changed something").

## Error handling

No new failure modes. `openTab`/`pinTab`/`reorderTab` are synchronous,
pure client-side array/object updates — no I/O, nothing to fail. The
drag-to-reorder pointer handlers have one edge case to guard explicitly:
if `pointerup` never fires (e.g. focus is lost mid-drag, browser quirk),
the global listeners must still be torn down — attach them via a cleanup
that also runs on the dragged element's unmount, not only on `pointerup`,
so a stuck listener can't survive a tab being closed mid-drag.

## Testing / verification

No frontend unit test runner in this repo (unchanged convention — see
Phase 1 plan's "Tech Stack deviation" note). Verification is
`cd frontend && bun run build` plus a manual QA pass:

1. Click note A in the dashboard → tab "A" appears, italic. Click note B →
   tab "A" is replaced in place by italic tab "B" (same position, not
   appended). Click A again → back to italic "A" in the same slot.
2. Double-click note C → tab "C" appears pinned (not italic), independent
   of any current preview tab.
3. With preview tab "D" open, start typing in D's editor → "D" loses
   italic (pinned) without moving position or losing focus.
4. Open pinned note "E", switch to SSH module, click the Notes Dock icon →
   a separate "Notes" dashboard tab opens/focuses; "E"'s tab is untouched
   and still pinned, not disturbed.
5. Drag a pinned tab past two neighbors → `openTabs` order updates live as
   the drag crosses each midpoint, other tabs visibly slide via the
   existing `layout` spring; dropping settles the dragged tab with no
   layout jump.
6. Drag the current preview tab → it moves position but stays italic and
   is still replaced by the next single-click preview-open elsewhere.
7. A tab whose `icon` is explicitly set (once some caller sets one) shows
   that icon instead of the Notes module's default `FileText` — confirms
   the override actually takes precedence, not just falls through.
8. Closing a tab mid-drag (if reachable) doesn't leave a stuck global
   `pointermove` listener — no console errors, no phantom drag state
   affecting the next unrelated drag.

## Out of scope recap

Agent Hub per-session tabs/live status, per-module last-active-tab memory
for Dock, framer-motion `Reorder.Group`/`Item`, drag changing preview/pin
state, persisting tab list/order across restart, command-palette wiring
into preview/pin, tab overflow menu / multi-select / multi-row strip.
