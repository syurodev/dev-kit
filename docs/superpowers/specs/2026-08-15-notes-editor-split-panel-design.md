# Notes editor redesign + app-wide split panel primitive

**Date:** 2026-08-15
**Status:** Draft — pending user review
**Supersedes:** the editor non-goal from
`docs/superpowers/specs/2026-08-15-notes-content-area-design.md` ("Any
change to... the editor... is unchanged") — the editor is now explicitly in
scope. Everything else already shipped (dashboard header/card, sidebar,
DESIGN.md token conformance, `note.color`/tags data model) is unchanged.

## Context

The dashboard content-area pass is done. Shown the editor screen as-is
(breadcrumb → single crowded row of title + folder `Select` + color
`Popover` + tag `Input`, all competing for space → a full-bleed writing
area with no margin, starting flush at the left edge on any window width)
and asked to redesign it, referencing the same "clean/spacious" quality as
the dashboard pass.

While iterating, the request grew twice:

1. Move folder/color/tag editing out of the crowded inline row into a
   **right-side panel that squeezes the center content when open** (not an
   overlay) — and build that panel as a **shared, app-wide primitive**, not
   a Notes-only component, modeled on a reference implementation
   (`SplitView`/`SplitPanelSlot` pattern from an unrelated project:
   `/Users/syuro/Workspace/OUTSOURCE/CHIRO/dashboard`), with
   `framer-motion` driving the open/close animation.
2. Move the panel's *toggle button* out of the editor's own local header
   entirely, up into the app-wide title-bar row `Workbench.tsx` already
   renders (where Search/Settings live, top-right) — via a small store any
   module can register/unregister a header action into. The user proposed
   the working name "right group"; this spec names it **module actions**
   instead (matches this codebase's existing vocabulary —
   `ModuleManifest`, `activeModule`, `modules/registry.ts` — and names the
   concept by *what it is* — actions belonging to whichever module is
   currently active — rather than *where it currently happens to render*).

## Goals

1. **New shared primitive** — `frontend/src/components/ui/split-panel.tsx`:
   a `SplitPanelProvider` + `SplitPanelSlot` + `useSplitPanel()` hook that
   any module can use to slide a right-side panel in/out. The panel is a
   flex sibling of the active module's content, not an overlay — opening it
   shrinks the content area via flexbox, no manual width math anywhere.
2. **Wire it into the app shell once** (`Workbench.tsx`), inside the
   existing bordered content card, so every module can reach it via the
   hook without each module re-plumbing context/layout.
3. **New `moduleActions` store** — `frontend/src/stores/moduleActions.ts`
   (Zustand, matching the existing `useAgentSessionsStore` convention) +
   a `useModuleAction()` hook: any module registers an icon-button
   descriptor (icon, label, onClick, active state) that renders in
   `Workbench.tsx`'s title-bar row, next to Search/Settings, for as long as
   the registering component stays mounted (i.e. for as long as its module
   is active — unmounting is what "un-shows" the action, no separate
   activation logic needed).
4. **Redesign `NoteEditor`**: writing column becomes a centered, width-capped
   document column (title + body), matching the "document, not a bare
   textarea" feel. Folder/color/tag editing moves into a `SplitPanel`
   panel, opened by a **module action registered into the app-wide
   title-bar row** (not a button inside the editor's own header) — closed
   by default, confirmed with user: the column gets full width until the
   user asks for the metadata panel.

## Non-goals

- Porting CHIRO's mobile-drawer fallback or its "auto-collapse the app
  sidebar when the panel doesn't fit" logic — DevKit has no persistent
  app-wide sidebar (it moved to a floating dock nav, see
  `docs/superpowers/specs/2026-08-12-workbench-shell-design.md`) and no
  mobile breakpoint to design for (confirmed elsewhere in this project:
  "this app has no mobile breakpoint to reach"). The shared primitive is
  desktop-only, panel-vs-content-width only.
- A `SplitView` wrapper component matching CHIRO's — that component's job
  in CHIRO was almost entirely the mobile/sidebar-coordination logic we're
  explicitly not porting. What's left (sync a local `open` boolean into the
  global panel context) is 3-4 lines any consumer can write directly against
  `useSplitPanel()` — not worth its own abstraction yet. Add one if a third
  consumer shows the same boilerplate.
- Making the panel resizable (drag handle) — CHIRO's own `resizable.tsx`
  primitive exists in that repo but isn't actually used by its
  `SplitView`/`SplitPanelSlot` (fixed width, animated 0→width). Match that:
  fixed width per open call, no drag-resize in this pass.
- Reusing the split panel or the module-actions row anywhere else in
  DevKit right now — both are built app-wide (any module *can* use them),
  but Notes' editor is the only actual consumer this pass ships.
- Reordering, grouping, or overflow handling for the module-actions row —
  this pass ships exactly one registered action (the panel toggle). A
  second consumer registering actions at the same time is possible
  (multiple modules could theoretically be mounted, though today only one
  module renders at a time) but overflow/priority behavior isn't designed
  here — add it when a real second consumer shows up.
- Changing the sidebar (just rebuilt), the dashboard (just redesigned), or
  any backend/data-model.

## Decisions validated with user

- **Toggle button, closed by default** (not always-open) — confirmed via
  question: a toggle opens/closes the panel; the writing column is
  full-width/centered until the user asks for metadata.
- **Toggle lives in the app-wide title bar, not the editor's own header**
  — explicit follow-up request, surfaced through a new shared
  `moduleActions` store rather than a component the editor renders itself.
  Named "module actions" (the user's own working name was "right group";
  renamed to describe what the concept is, not where it currently sits).
- **Panel squeezes content (flex-sibling width animation), not an overlay**
  — explicit user request, matches the CHIRO reference exactly in
  mechanism (a slot whose `width` animates 0 → target, main content is a
  flex sibling that naturally shrinks).
- **`framer-motion` for the animation** — already a project dependency,
  already used elsewhere in this codebase
  (`frontend/src/workbench/dock/{Dock,DockItem}.tsx`, via the
  `LazyMotion`/`domAnimation`/`m` lazy-loaded pattern, not the full
  `motion` import). This pass follows that exact established convention,
  not a different framer-motion API.
- **Shared/app-wide, not Notes-scoped** — explicit user request. Lives in
  `components/ui/` (this app's existing home for cross-module primitives —
  `Sidebar`, `Dialog`, `Popover`, etc. all live there), wired once in
  `Workbench.tsx`, not in `frontend/src/modules/notes/`.
- **Panel sits inside the existing per-module bordered card**, not as its
  own separate top-level card sibling to it. `Workbench.tsx`'s content card
  (`<div className="mx-2 mb-2 ... rounded-xl border border-border
  bg-card">`) is DevKit's established "one card = current view" chrome
  language (every module today renders inside exactly one such card, e.g.
  Notes' own sidebar lives *inside* the Notes section, not as a separate
  card next to it). CHIRO's version gives the panel its own rounded surface
  outside `SidebarInset` — not reused here, to stay consistent with how
  every other DevKit surface already nests.

## Architecture

### `frontend/src/components/ui/split-panel.tsx`

```tsx
type PanelState = {
  content: React.ReactNode;
  width: number; // px
  onClose: () => void;
};

SplitPanelContext: React.Context<{
  panel: PanelState | null;
  openPanel: (content: React.ReactNode, opts: { width?: number; onClose: () => void }) => void;
  closePanel: () => void;
} | null>

export function SplitPanelProvider({ children }): JSX.Element
export function useSplitPanel(): { panel, openPanel, closePanel }
export function SplitPanelSlot(): JSX.Element
```

- `SplitPanelProvider` — plain `useState<PanelState | null>`, same shape as
  CHIRO's, minus the mobile/sidebar bookkeeping this pass doesn't port.
- `SplitPanelSlot` — the visual piece. `LazyMotion features={domAnimation}`
  + `m.div` animating `width` (`animate={{ width: panel ? panel.width : 0
  }}`, `transition={{ duration: 0.22, ease: "easeInOut" }}`) so the
  sibling `<section>` (the active module's content) shrinks via flexbox as
  the slot grows — no JS layout math, matching the CHIRO mechanism exactly.
  `AnimatePresence` wraps the panel's *content* (fade+slight-x slide on
  mount/unmount, mirroring the exact `initial`/`animate`/`exit` shape
  `DockItem.tsx` already uses in this codebase) so content doesn't pop in
  in a wide-but-empty container mid-animation. A small `X` icon button
  (top-right, `size-7 rounded-full`, matches `SidebarTrigger`/existing
  icon-button sizing already used in this codebase) calls `panel.onClose`.
  Border: `border-l border-border` (hairline, matches every other
  divider already in this app — Notes' own sidebar uses `border-r
  border-border` for the same left/right split role).
- `useSplitPanel()` throws outside a `SplitPanelProvider`, matching the
  existing `useSidebar()` pattern in `components/ui/sidebar.tsx` (fail
  loud, not silent).

Default width: **420px**, matching the CHIRO reference exactly (no reason
found to diverge — Notes' metadata content is comparable in density to a
CHIRO detail panel's summary section).

### `frontend/src/stores/moduleActions.ts` — module actions

```tsx
export type ModuleAction = {
  id: string;
  icon: LucideIcon;
  label: string;      // aria-label + tooltip text
  isActive?: boolean;  // pressed/toggled visual state
  onClick: () => void;
};

type ModuleActionsState = {
  actions: ModuleAction[];
  registerAction: (action: ModuleAction) => void;
  unregisterAction: (id: string) => void;
};

export const useModuleActionsStore = create<ModuleActionsState>((set) => ({
  actions: [],
  registerAction: (action) => set((state) => { /* replace-by-id or append */ }),
  unregisterAction: (id) => set((state) => ({ actions: state.actions.filter((a) => a.id !== id) })),
}));
```

Same `registerAction`/replace-by-`id` shape as `useAgentSessionsStore`'s
existing `upsertSession`.

**Researched: React 19.2.8 (the version actually installed —
`node -e "require('react').useEffectEvent"` confirms it exists) ships
`useEffectEvent` as a stable export.** It was considered for the "keep
`onClick` fresh without re-registering on every render" problem this hook
has, and rejected: `useEffectEvent` is contracted to be called *from
within an Effect in the same component that defines it* — its whole
purpose is letting that Effect read latest render values without listing
them as deps. Storing the Effect Event itself in Zustand and having a
*different* component (`Workbench.tsx`) invoke it later via a click
handler is outside that contract, even though nothing stops it from
technically running. The right tool for "a value from a different
component needs to always call the latest version of a callback" is the
plain **ref pattern** (predates React 19, still the correct fit — this
isn't an Effect reading its own component's fresh state, it's a value
handed across a component boundary):

```tsx
export function useModuleAction(action: ModuleAction | null) {
  const registerAction = useModuleActionsStore((s) => s.registerAction);
  const unregisterAction = useModuleActionsStore((s) => s.unregisterAction);

  const onClickRef = useRef(action?.onClick);
  onClickRef.current = action?.onClick; // updated every render, cheap — ref writes during render are an accepted React pattern for exactly this "latest callback" case, not a purity violation

  const id = action?.id;
  const icon = action?.icon;
  const label = action?.label;
  const isActive = action?.isActive;

  useEffect(() => {
    if (!id || !icon || !label) return;
    registerAction({ id, icon, label, isActive, onClick: () => onClickRef.current?.() });
    return () => unregisterAction(id);
    // Deps are the primitive fields, not `action` itself — re-registers
    // only when id/icon/label/isActive actually change (e.g. the user
    // toggles the panel), never on unrelated re-renders (e.g. every
    // keystroke in the editor, which re-renders NoteEditor via
    // scheduleSave but doesn't change any of these four values).
  }, [id, icon, label, isActive, registerAction, unregisterAction]);
}
```

For `NoteEditor`'s specific `onClick` (`() => setMetaOpen((v) => !v)`),
this is doubly safe even without the ref: `useState` setters are stable
across renders by React's own guarantee, and the functional-updater form
`(v) => !v` never reads a closed-over value at all — so even a "stale"
closure toggles correctly. The ref layer in `useModuleAction` exists for
the *general* case (a future consumer whose `onClick` does need to read
current props/state), not because this specific call needs it — kept in
the shared hook so no consumer has to rediscover this pattern themselves.

`Workbench.tsx` renders `useModuleActionsStore((s) => s.actions)` as a row
of icon `Button`s (`variant="ghost" size="icon-sm"`, matching the existing
Search/Settings buttons exactly) inserted **before** Search/Settings in the
header's right-aligned cluster — module-specific actions sit closest to
the module title, global actions (search, settings) stay outermost/last,
unchanged in position.

**Researched: this header row sits inside `main.go`'s
`Mac.InvisibleTitleBarHeight: 50` strip** (a native-OS-handled draggable
region, not a CSS `-webkit-app-region` hack — confirmed by reading
`main.go`). The existing Search/Settings buttons already render and work
correctly inside that exact strip today, so adding more `Button`s to the
same cluster carries no new drag-region risk — it's the same proven
pattern, not a new one.

### `Workbench.tsx` wiring

Wrap the return value in `SplitPanelProvider`; inside the existing bordered
content card, change the direct children from `[warning?, <section>]` to
`[warning?, <div className="flex min-h-0 flex-1"><section>...</section>
<SplitPanelSlot /></div>]` — i.e. `<section>` and `<SplitPanelSlot />`
become flex-row siblings inside a new wrapping row, nested inside the
card's existing `flex-col`. Additionally, insert the module-actions button
row (see above) into the existing `<header>`, between the module title and
the existing Search button. No other change to `Workbench.tsx` — routing,
dock, command palette are untouched.

### `NoteEditor.tsx` — consuming the panel

- Remove the crowded inline row (folder `Select` + color `Popover` + tag
  `Input`, currently sharing a row with the title `Input`).
- Title becomes its own row: large (`text-3xl font-bold tracking-tight` —
  30px, a stock Tailwind step above the dashboard header's `text-2xl`
  24px, no arbitrary bracket value — since this is the single focal
  element on the page, not sharing a row with a subtitle), no
  border/background (plain `Input` with transparent chrome, as today),
  inside a centered, width-capped column
  (`max-w-[720px] mx-auto`, matching common document-editor conventions —
  Notion/Craft-class width for comfortable reading, and this project has
  no prior max-width token to reuse here since no other screen constrains
  reading width this way yet).
- The Plate editor body (`EditorContainer`/`Editor`) moves inside the same
  centered column, so title and body share one reading width.
- No toggle button in the editor's own local header — instead, `NoteEditor`
  calls `useModuleAction({ id: "notes.toggle-meta-panel", icon: PanelRight,
  label: "Note details", isActive: metaOpen, onClick: () =>
  setMetaOpen((v) => !v) })`, which surfaces the same `PanelRight` (lucide)
  toggle button in `Workbench.tsx`'s title-bar row instead, next to
  Search/Settings. The editor's own local header (breadcrumb, Saving/Saved
  status, Delete) is otherwise unchanged.
- Local `const [metaOpen, setMetaOpen] = useState(false)`. A `useEffect`
  syncs it to the global panel:
  ```tsx
  useEffect(() => {
    if (!metaOpen) {
      closePanel();
      return;
    }
    openPanel(
      <NoteMetaPanel
        folders={folders}
        folderId={folderId}
        ownTags={ownTags}
        color={color}
        effectiveColor={note.effectiveColor}
        onFolderChange={onFolderChange}
        onColorChange={onColorChange}
        onAddTag={tryAddTag}
        onRemoveTag={removeOwnTag}
      />,
      { width: 420, onClose: () => setMetaOpen(false) },
    );
    return () => closePanel();
  }, [metaOpen, folders, folderId, ownTags, color, note.effectiveColor]);
  ```
  Unlike `useModuleAction`'s registration effect, this one does **not**
  need ref-based stabilization: it only runs while `metaOpen` is true (the
  panel is actually open), and its other dependencies
  (`folders`/`folderId`/`ownTags`/`color`/`note.effectiveColor`) only
  change on deliberate organization edits or the ~1.5s autosave tick — not
  per keystroke in the title/body (those don't touch any of these values),
  so re-pushing fresh panel JSX at that rate is cheap and correct, not a
  perf concern worth optimizing away.
- New `NoteMetaPanel` (small component, colocated in `NoteEditor.tsx` or a
  sibling file if it grows) renders the moved-out controls as a vertical
  stack: a small caps label above each control (`Folder`, `Color`, `Tags`),
  reusing the exact same `Select`/`ColorPicker`/tag-`Input`+chip pieces
  that exist today — this is a relocation of existing controls into a new
  layout, not new controls.
- Breadcrumb row (`Notes / {folder} / {title}`) is unchanged and remains
  the always-visible folder indicator even while the panel is closed — the
  user isn't fully blind to organization state without opening the panel.

## Error handling

No new failure modes — `openPanel`/`closePanel` are pure UI state, no I/O.
Existing toast-on-failure behavior for folder/color/tag writes
(`persistOrganization`) is unchanged, just now triggered from controls
rendered inside the panel instead of the old inline row.

## Testing / verification

No backend involved, no test runner (unchanged convention).
`cd frontend && bunx tsc --noEmit` plus a manual pass:

1. `useSplitPanel()` outside `SplitPanelProvider` throws (sanity-check via
   code inspection, not a runtime test we can automate here).
2. Opening the editor: writing column is centered, capped width, no
   metadata row cluttering it; a `PanelRight` toggle button appears in the
   app-wide title bar (top-right, next to Search/Settings) — not inside the
   editor's own local header.
3. Switching away from Notes (e.g. to Database via the dock) removes that
   toggle button from the title bar; switching back to an open note
   restores it. No leaked/duplicate module-action entries after repeated
   switches (`useModuleActionsStore.getState().actions` should never grow
   unbounded).
4. Clicking the toggle: panel slides in from the right, writing column
   visibly narrows (flexbox squeeze, not an overlay covering it); folder/
   color/tag controls all present and functional inside the panel,
   producing identical persistence behavior to before (same
   `persistOrganization` calls, same toasts on failure).
5. Clicking the toggle again (or the panel's own close `X`): panel slides
   out, column returns to full centered width, and the title-bar button's
   `isActive`/pressed state reflects closed.
6. Changing folder/color/tags while the panel is open updates its content
   live (no stale display) and updates the breadcrumb folder name too.
7. Switching notes (`onBack` → open a different note) doesn't leave a
   stale panel open with the previous note's data.
8. No visual/behavioral change to the dashboard, sidebar, or any other
   module — `SplitPanelSlot` renders as zero-width/invisible and the
   title-bar module-actions row renders empty whenever no module has
   registered anything.

## Out of scope recap

Mobile drawer fallback, app-sidebar auto-collapse coordination, a
`SplitView` wrapper abstraction, resizable/draggable panel width, reusing
the panel in Database/SSH/Kafka/etc. in this pass, any dashboard/sidebar/
backend change.
