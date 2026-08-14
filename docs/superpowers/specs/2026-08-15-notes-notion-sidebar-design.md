# Notes — Notion-style sidebar on shadcn Sidebar

**Date:** 2026-08-15
**Status:** Draft — pending user review
**Supersedes:** the sidebar portion of
`docs/superpowers/specs/2026-08-14-notes-folders-ui-design.md` ("Left:
virtual rows All and Ungrouped, then folder rows...") and the sidebar tweaks
from `docs/superpowers/specs/2026-08-14-notes-design-system-conformance-design.md`.
Everything else in those specs (dashboard scope filtering, tag AND-filter,
folder CRUD dialogs, card treatment, DESIGN.md token conformance) is
unchanged.

## Context

The Notes sidebar has been a hand-rolled `<aside>` of plain `<button>` rows
since the first folders/tags pass. Two rounds of visual iteration (DESIGN.md
token conformance, then a composition pass fixing dashboard empty-space and
adding a page header) still left the sidebar reading as "too plain" against
real content (screenshotted at `docs/superpowers/specs/` review time — a
2-note vault with one empty folder). Validated via mockup toward a
Notion-style treatment: compact rows, hover-reveal row actions, a colored
folder icon instead of a dot, and a collapsible "Folders" group with a
hover-reveal "+" — and the user asked to build it on the project's existing
`components/ui/sidebar.tsx` (shadcn's sidebar primitives, currently unused
anywhere in the app) rather than hand-rolled markup.

## Goals

1. Replace the hand-rolled sidebar `<aside>`/`<button>` markup in
   `NotesDashboard.tsx` with `Sidebar`/`SidebarGroup`/`SidebarMenu`/etc. from
   `frontend/src/components/ui/sidebar.tsx`.
2. Match the approved Notion-style mock: compact rows (`size="sm"`, 28px),
   hover-only row background (no left accent bar, no border), a small
   folder icon tinted with the folder's color instead of the current dot,
   and per-folder "..." actions that only appear on row hover.
3. Group folders under a collapsible **"Folders"** section (chevron
   collapse/expand, matching the established `Collapsible` +
   `SidebarGroupLabel` pattern already used once before in this codebase's
   git history for the app's old global sidebar). The group header carries
   a hover-reveal **"+"** (`SidebarGroupAction`) that opens the existing
   "New folder" dialog — replacing the always-visible bordered button that
   used to sit at the bottom of the sidebar.
4. Let the whole sidebar collapse/expand using `Sidebar`'s own built-in
   `collapsible="icon"` mode + `SidebarTrigger` — not a hand-rolled toggle.
   Collapsed state shrinks to an icon-only rail (every row already carries
   a meaningful icon — `LayoutGrid`/`Inbox`/colored `Folder` — so the rail
   stays navigable, not just a blank strip) and persists via the
   component's own cookie, with zero custom state code on our side.

## Non-goals

- Any change to dashboard scope-filtering logic, tag AND-filter, folder
  CRUD dialogs' contents, `NoteCard`, the editor, or DESIGN.md token values
  already locked in — this pass only swaps the sidebar's markup/component
  layer and its immediate visual treatment.
- A mobile/`Sheet` sidebar — this app has no mobile breakpoint to reach
  (`useIsMobile` never trips at desktop window sizes), so that code path
  in `Sidebar` is inert here, not something this plan builds or tests.
- Reusing `Sidebar` anywhere else in the app (Database/SSH/Kafka/etc.) —
  out of scope, Notes-only.
- Nested/multi-level folders, drag-and-drop — unchanged non-goals from the
  original folders spec.

## Decisions validated with user

- **Notion reference, not the boxier SaaS-admin reference** (both were
  shown; user picked Notion: minimal rows, hover-reveal actions, no left
  accent bar, no icon-heavy nav).
- **Built on `components/ui/sidebar.tsx`**, not hand-rolled `<div>`/`<button>`
  markup — explicit user request, and the component's `--sidebar-*` color
  tokens in `index.css` already match the app's dark palette (defined,
  currently unused anywhere).
- **`collapsible="icon"`, with a CSS containment fix.** `Sidebar`'s
  desktop-collapsible render path is `position: fixed; inset-y-0; height:
  100svh` — built for a sidebar pinned to the real viewport edge. Notes'
  sidebar instead lives inside `Workbench.tsx`'s bordered content card
  (`<div className="mx-2 mb-2 ... rounded-xl border border-border bg-card">`,
  itself below a 50px title-bar `<header>`), which has neither a specified
  height that matches `100svh` nor any property that gives `position:fixed`
  a new containing block — so used as-is, the sidebar would render fixed to
  the *window*, bleeding over the title bar and past the card's rounded
  corners. Fix: give `NotesDashboard`'s own root element `contain-layout`
  (CSS `contain: layout`), which — per spec — makes it a containing block
  for `position: fixed` descendants, confining the sidebar to Notes' own
  pane with no visual side effects (no blur/transform/compositing change).
  This keeps the fix entirely inside `NotesDashboard.tsx`; `Workbench.tsx`
  is not touched.
- **Folder color moves from a dot to a filled folder icon** (`Folder` from
  lucide-react, `fill` set to the folder's `--note-folder-*` color) — reads
  as "this is a folder, and this is its color" in one glyph, closer to
  Notion's colored page icons, instead of a decoration next to the name.

## Component structure

```
NotesDashboard root <div> — gains `contain-layout` (see Decisions)
  SidebarProvider (uncontrolled — no `open`/`onOpenChange`; the component's
                   own cookie persists collapsed state across reloads;
                   className="flex h-full min-h-0 w-full" — overrides the
                   component's default `min-h-svh`, which would otherwise
                   force the wrapper taller than Notes' own bounded pane)
    Sidebar (collapsible="icon", `variant="sidebar"` default, className
             replaces the current `<aside>`'s `border-r border-border`;
             width still comes from `--sidebar-width`/`--sidebar-width-icon`)
      SidebarContent
        SidebarGroup                          — All / Ungrouped, no label
          SidebarGroupContent
            SidebarMenu
              SidebarMenuItem > SidebarMenuButton (size="sm", isActive)  — All
                icon: LayoutGrid (lucide)      SidebarMenuBadge — count
              SidebarMenuItem > SidebarMenuButton (size="sm", isActive)  — Ungrouped
                icon: Inbox (lucide)           SidebarMenuBadge — count
        Collapsible (defaultOpen, wraps the group below — established pattern)
          SidebarGroup (className="group/folders", relative — for the
                        hover-reveal "+" to key off)
            CollapsibleTrigger asChild
              SidebarGroupLabel                — "Folders" + chevron (rotate
                                                  on open, matches the once-
                                                  shipped app sidebar pattern)
            SidebarGroupAction (opens the "New folder" Dialog; className
                                 adds `opacity-0 group-hover/folders:opacity-100`
                                 — GroupAction has no built-in showOnHover,
                                 unlike MenuAction, so this is set explicitly)
            CollapsibleContent
              SidebarGroupContent
                SidebarMenu
                  SidebarMenuItem (per folder) > SidebarMenuButton (size="sm",
                      isActive)
                    icon: Folder (lucide), fill=folderDotVar(folder.color)
                    label, SidebarMenuBadge — count
                    SidebarMenuAction (showOnHover) — DropdownMenuTrigger for
                      the existing Rename / Edit tags / Delete menu (same
                      DropdownMenuContent as today, unchanged)
    <section> (the note grid — sibling of `Sidebar`, both inside
               `SidebarProvider` since `SidebarTrigger` needs the same
               context; unchanged position/content otherwise)
      toolbar row: SidebarTrigger (new, `PanelLeftIcon` button — toggles
                   collapse; placed in the content pane rather than inside
                   `Sidebar` so it stays reachable even when collapsed to
                   icon width) + Search Input + "New note" Button (both
                   unchanged)
```

Every `SidebarMenuButton` above (All, Ungrouped, and each folder) gets a
`tooltip={label}` prop — `SidebarMenuButton` only renders that tooltip when
`state === "collapsed"`, so it's a no-op at full width and becomes the row's
only label when collapsed to the icon rail.

`SidebarInset`/`SidebarRail`/`SidebarHeader`/`SidebarFooter` are not used —
the note grid stays exactly where it is today (a sibling `<section>` next
to `Sidebar`, not wrapped in `SidebarInset`, since `SidebarInset` assumes
the fixed-position desktop variant this pass deliberately avoids).

## Visual mapping (Notion-style, DESIGN.md tokens unchanged)

| Element | Before | After |
|---|---|---|
| Row height/padding | `px-2 py-1.5`, `text-base` (16px, from the DESIGN.md conformance pass) | `SidebarMenuButton size="sm"` → `h-7` (28px), `text-xs` (12px) — Notion's denser row |
| Active row | `bg-accent text-accent-foreground` | `isActive` prop → same tokens (`data-active:bg-sidebar-accent`, which resolves to the identical `#121212`) — no visible color change, just component-driven |
| Folder identity | 8px color dot before the name | `Folder` icon (14px), `fill: var(--note-folder-<color>)`, `stroke: none` |
| Row action (rename/tags/delete) | Always-mounted `DropdownMenu`, `opacity-0 group-hover:opacity-100` hand-rolled on the trigger `Button` | `SidebarMenuAction showOnHover` wrapping the same `DropdownMenuTrigger` — identical visual behavior, now componentized |
| "New folder" entry point | Bordered `Button` fixed at the bottom of the `<aside>` | `SidebarGroupAction` "+" on the "Folders" group header, hover-reveal, opens the same `Dialog` |
| Count | `text-xs tracking-[0.4px] text-muted-foreground` span | `SidebarMenuBadge` (already `text-xs`, right-aligned, matches token) |

No new colors, no new radii — `Sidebar`'s own `--sidebar-*` tokens
(`index.css`, currently unused) already equal the app's existing
`--accent`/`--muted-foreground`/etc. dark values.

## Error handling

No behavioral change — folder CRUD (`createFolder`/`updateFolder`/
`deleteFolder`), scope selection, and the dialogs they open are wired
identically to today, just from inside the new component tree. Toast-on-
failure and the delete confirmation `AlertDialog` are untouched.

## Testing / verification

No backend involved. Frontend: no test runner (unchanged project
convention). `cd frontend && bunx tsc --noEmit` plus a manual pass:

1. Sidebar renders at the same width as today, confined inside Notes' own
   rounded card — no bleed over the title bar, no overlap with the dock,
   no visible jump/flicker on load (confirms the `contain-layout` fix
   actually contains the `fixed` sidebar).
2. Clicking `SidebarTrigger` (or `⌘/Ctrl+B`, the component's built-in
   shortcut) collapses the sidebar to an icon-only rail and back; every row
   (All/Ungrouped/each folder) still shows its icon and is still clickable
   at icon width, with a tooltip on hover showing the label. Reloading the
   app preserves the collapsed/expanded state (cookie persistence).
3. All / Ungrouped rows and each folder row select scope on click,
   identical to before.
4. Hovering a folder row reveals its "..." action; clicking it still opens
   Rename / Edit tags / Delete with identical behavior (including the
   delete confirmation `AlertDialog` copy).
5. Hovering the "Folders" group header reveals "+"; clicking opens the same
   "New folder" dialog as before.
6. Clicking the "Folders" chevron collapses/expands the folder list; All/
   Ungrouped stay visible regardless (they're outside the `Collapsible`).
   This is independent of the sidebar-wide collapse in step 2.
7. Folder icons show the correct color per folder; a folder with an
   unrecognized/legacy color value falls back the same way `folderDotVar`
   already does today (gray).
8. No visual/behavioral change to the note grid, search, tag filter, or
   any other module — this pass only touches the sidebar DOM inside
   `NotesDashboard.tsx`.

## Out of scope recap

A mobile/`Sheet` sidebar variant, reusing `Sidebar` in other modules,
nested folders, drag-and-drop, any change to dashboard filtering logic or
the note grid/card/editor.
