# Notes — dashboard content area (header + card), clean/spacious pass

**Date:** 2026-08-15
**Status:** Draft — pending user review
**Supersedes:** the toolbar/grid portion of
`docs/superpowers/specs/2026-08-14-notes-design-system-conformance-design.md`
and the `NoteCard` shape from
`docs/superpowers/specs/2026-08-14-notes-card-redesign-design.md`, on two
specific points only: card corner radius (`{rounded.lg}` 10px →
`{rounded.xl}` 14px) and tag chip radius/shape (`{rounded.xs}` 4px dot-style
chip → `{rounded.md}` 8px icon pill). Everything else already locked in —
sidebar (just rebuilt on shadcn `Sidebar`), 3:2 card ratio, `note.color`
data model, DESIGN.md's no-shadow/hairline-border/Inter+ss03/`-soft`-alpha
rules, dashboard scope-filtering logic — is unchanged.

## Context

The sidebar was just rebuilt on shadcn `Sidebar` (Notion-style). The
dashboard's main content area — the header/toolbar row and the note grid —
is what's left of "the outer (non-editor) screen." Shown a reference image
(a task-detail panel: bold title, `Status`/`Priority` shown as icon+text
pills, generous section spacing, hairline dividers) and asked to bring that
panel's *clean, spacious* quality into the content area — not its literal
components (a property list and an activity feed don't fit a grid of many
notes) — specifically: a bolder page header, more generous spacing/padding
throughout, and tags rendered as icon pills instead of the current small
dot-prefixed chip.

## Goals

1. Give the content area a real page header: bold "Notes" title + a
   scope/count subtitle, visually separated from the grid by a hairline
   rule — replacing the toolbar-row-is-the-first-thing-you-see layout.
2. Raise the whole area's spacing scale one notch (header margins, card
   padding, grid gutters, toolbar height) so it reads closer to the
   reference's confident, uncrowded feel — every new value still snapped to
   a real DESIGN.md token, no arbitrary one-off pixel values.
3. Turn each `NoteCard` tag from a small dot + 12px text into an icon pill
   (`Tag` glyph + bold label, `-soft` background) — the closest read on a
   *freeform* tag of the reference's `Status`/`Priority` pill pattern
   (those are fixed enums with dedicated icons; tags aren't, so one shared
   `Tag` icon stands in for all of them).

## Non-goals

- Adopting the reference's property-list layout (`Status`/`Household`/
  `Client`/...), activity feed, comment box, or attach-file row — none of
  that maps onto a grid of many notes; explicitly out of scope, confirmed
  with user (scope selection was "header/framing" + "the card itself" only).
- Any change to the sidebar (just redone), the editor, scope-filtering/tag-
  AND-filter logic, or the `note.color`/folder data model.
- A literal switch to a list/property-row view — considered and explicitly
  declined in favor of keeping the card grid.
- Changing the accent color dot next to the card title — stays exactly as
  shipped (`size-2`, `rounded-[4px]`); only the card frame around it and
  the tag row change in this pass.

## Decisions validated with user

- **Scope: header/framing of the content area + the note card itself.**
  Not a structural switch to a list view (offered as a third option,
  declined).
- **Borrow spacing/typography quality, not components.** The reference is a
  task-detail panel; its property-list/activity-feed/attach-file patterns
  don't apply to a card grid. What transfers: bold page title, generous
  section rhythm, hairline section dividers, and pill-shaped metadata chips
  with an icon.
- **Tag pill uses a generic `Tag` icon**, not per-tag icons — tags are
  freeform strings (see
  `docs/superpowers/specs/2026-08-14-notes-folders-ui-design.md`), unlike
  the reference's fixed `Status`/`Priority` enums which each get a
  dedicated glyph. One shared icon keeps this from turning into a tag-icon-
  picker feature nobody asked for.
- **Card radius moves to `{rounded.xl}` (14px)**, one step up from the
  `{rounded.lg}` (10px) set in the DESIGN.md conformance pass — reusing an
  existing token one notch larger, not introducing a new arbitrary radius,
  to match the airier feel without breaking the token discipline that pass
  established.
- **Card title snaps to the DESIGN.md `body-strong` token** (16px / 600 /
  0.2px tracking) instead of the previous `body-sm-strong` (14px / 500) —
  the nearest real token to the mockup's bolder title, chosen over
  hand-picking an arbitrary 15px to keep every size on the defined scale.

## Token mapping

| Element | Before | After |
|---|---|---|
| Page header | *(none — toolbar was the first element)* | New: "Notes" in `text-2xl font-bold tracking-tight` (24px/700 — see Component changes for why this is the one non-table-literal size in this pass); count/scope subtitle in `caption-md` (13px) `text-muted-foreground` |
| Header → grid divider | *(none)* | 1px `border-border` hairline, full width |
| Search input height | `h-8` (32px) | `h-9` (36px) — DESIGN.md's own `text-input` token height |
| "New note" button weight | `font-medium` (via `Button` default) | unchanged component, no override needed — already matches `button-primary` |
| Card outer radius | `rounded-lg` (`--radius-lg`, 10px) | `rounded-xl` (`--radius-xl`, 14px) |
| Card padding | `p-3` (12px) | `p-4` (16px) — DESIGN.md's own card-gutter/`store-extension-card`-padding token |
| Card→card grid gap | `gap-3.5` (14px, off-scale) | `gap-4` (16px) — matches DESIGN.md's stated `{spacing.lg}` card-grid gutter exactly |
| Card title | `text-sm font-medium tracking-[0.2px]` (14px/500) | `text-base font-semibold tracking-[0.2px]` (16px/600) — DESIGN.md `body-strong` |
| Tag chip shape | `rounded-[4px] px-1.5 py-0.5 text-xs font-medium` (dot-adjacent micro chip) | `rounded-md px-2.5 py-1 text-xs font-semibold` (`{rounded.md}` 8px) + leading `Tag` icon (11px) |
| Tag chip color | `tagSoftBackground` (15% alpha bg, full-color text) — unchanged formula | same helper, same 15% alpha; only the chip's shape/icon changes |
| Accent color dot | `size-2 rounded-[4px]` | **unchanged** |
| Preview text | `text-[13px] leading-[1.4] tracking-[0.1px]` | **unchanged** |
| Relative-time text | `text-xs tracking-[0.4px]` | **unchanged** |

No new CSS colors and no new alpha formula — every color reference is an
existing `--note-folder-*`/`--note-tag-*` token or an existing semantic
token (`border-border`, `text-muted-foreground`, `text-foreground`).

## Component changes

### `NotesDashboard.tsx`

Toolbar `<div>` (currently the first child of the content `<section>`) gets
a new header block immediately above it:

```
<section>
  <div> — new page header
    "Notes" (see typography below) + subtitle: `{count} note{s} · {scope label}`
  <div> — 1px border-border hairline, full width
  <div> — existing toolbar row (SidebarTrigger, search, New note, tag chips), height bump only
  <ScrollArea> — grid, gap-4 instead of gap-3.5
```

The subtitle's scope label reuses the same three states already computed
for the sidebar (`All`, `Ungrouped`, or the active folder's name) — no new
state, just a small derived string next to the existing `scope`/`folders`
values already in the component.

Page title typography: large enough to read as a page header without
inventing a new token — `text-2xl font-bold tracking-tight` (24px/700,
Tailwind's built-in scale) is the closest *bold display* step above
`body-strong` for a one-line page title; `body-strong` itself (16px) is
reserved for the card title per the mapping table above, so the page title
needs its own, taller step. This is the one place in this pass that isn't a
literal DESIGN.md-table entry (the table's largest non-hero size is
`heading-xl` 24px/500) — closest match is `heading-xl`'s size at bold
weight, since DESIGN.md's own weight ladder for page-level headers this
size doesn't otherwise clash.

### `NoteCard.tsx`

- Outer button: `rounded-xl` (was `rounded-lg`), `p-4` (was `p-3`).
- Title span: `text-base font-semibold tracking-[0.2px]` (was `text-sm
  font-medium tracking-[0.2px]`).
- Tag chip: add a leading `Tag` icon (`lucide-react`, `className="size-2.5"`,
  inherits the chip's `color` from `tagSoftBackground` like the text does),
  `rounded-md px-2.5 py-1 text-xs font-semibold` (was `rounded-[4px]
  px-1.5 py-0.5 text-xs font-medium`). `tagSoftBackground`'s return value
  (background + text color) is unchanged and still applied via `style`.
- Everything else (accent dot, preview text, relative time, `min-w-0`
  truncation fix) is untouched.

## Error handling

No behavioral change — this is a visual-only pass over already-shipped
read paths (`List`/`Search` results rendered into cards). No new failure
modes.

## Testing / verification

No backend involved, no test runner (unchanged project convention).
`cd frontend && bunx tsc --noEmit` plus a manual pass:

1. Dashboard shows a bold "Notes" header with a live count/scope subtitle
   above a hairline divider, above the toolbar.
2. Switching sidebar scope (All / Ungrouped / a folder) updates the
   subtitle's scope label live.
3. Search input and toolbar row read slightly taller (36px) than before;
   "New note" still creates a note in the current scope exactly as before.
4. Note cards are visibly larger/airier (14px corner radius, 16px padding,
   16px grid gutters) without breaking the fixed 3:2 aspect ratio.
5. Card titles are visibly bolder/larger (16px/600) and still truncate on
   one line, never wrap, per the existing `min-w-0` fix.
6. Tags render as pills with a small leading tag icon, `-soft` background,
   same color as before — clicking one still toggles the AND tag filter
   exactly as before.
7. No visual/behavioral change to the sidebar or editor.

## Out of scope recap

Property-list/activity-feed/attach-file patterns from the reference, a
list-view alternative to the card grid, per-tag custom icons, sidebar/
editor changes, `note.color` data model, folder CRUD.
