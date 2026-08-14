# Notes — conform to DESIGN.md (Raycast token system)

**Date:** 2026-08-14
**Status:** Draft — pending user review
**Supersedes:** the card visual treatment from
`docs/superpowers/specs/2026-08-14-notes-card-redesign-design.md` (the
"mini-window" dark-frame-with-inset-colored-panel card). Everything else
that spec/plan shipped — the `note.color` field, effective-color live
inheritance, copy-down on folder delete, the 3:2 card ratio, dashboard
structure, sidebar folder management — is unchanged.

## Context

`frontend/DESIGN.md` (front-matter `name: Raycast-design-analysis`) is a
tokenized design system extracted from Raycast's marketing site, and its
color values match the app's actual shipped dark theme in
`frontend/src/index.css` almost exactly (`canvas #07080a` = `--background`,
`surface #0d0d0d` = `--card`, `hairline #242728` = `--border`,
`accent-blue #57c1ff` = `--ring`, `accent-red #ff6161` = `--destructive`,
`accent-yellow #ffc533` = `--status-permission`, `stone #434345` =
`--status-idle`). It is the actual design source the app's shell already
draws from — not an unrelated template. The Notes module's dashboard/card
work shipped in the prior two specs was built without reading this file, and
diverges from it in specific, checkable ways: oversized card radii, ad hoc
pixel font sizes not on the defined type scale, a color-panel alpha that
doesn't match the system's `-soft` (15%) convention, and (in one shared
primitive used by Notes) a drop-shadow the system never uses.

## Goals

1. Bring every Notes-owned UI surface (`NoteCard`, dashboard sidebar/search/
   dialogs, editor header, `ColorPicker`) onto DESIGN.md's actual tokens:
   radius scale, type scale, spacing scale, the `-soft` alpha convention,
   and the "no drop shadow, ever — depth from the surface ladder" rule.
2. Apply DESIGN.md's specified typeface — **Inter with the `ss03` feature
   set** — scoped to the Notes module only (see Decisions).
3. Replace the note card's colored inset panel (previous spec) with a
   neutral `feature-card-dark`-equivalent surface + a small square color
   accent, matching DESIGN.md's explicit rule that saturated accent color
   belongs in illustrations/tags, never washed across chrome.

## Non-goals

- Changing the app-wide font (Geist stays everywhere outside Notes).
- Touching the shared `Dialog`/`Popover`/`Button`/`Input` component files —
  any DESIGN.md-driven override is applied at Notes' call sites only
  (`className` overrides), not to the shared primitives other modules use.
- Re-opening the note-folder 9-color palette, the `note.color` field, live
  effective-color inheritance, or copy-down-on-delete — all from the prior
  spec, unchanged.
- Re-opening the dashboard's overall structure (left sidebar + right grid,
  not top pill-tabs) or the card's 3:2 aspect ratio — both already decided
  in the prior spec and out of scope here.
- A general "shadow audit" of the rest of the app — scoped to what Notes
  itself renders (the color `Popover` it opens).

## Decisions validated with user

- **Font scope:** DESIGN.md mandates Inter + `ss03`; the rest of the app
  uses Geist Variable everywhere. Rather than compromise on DESIGN.md's
  explicit typographic identity ("without `ss03`, the chrome loses its
  signature voice") or force a silent app-wide font change beyond this
  pass's scope, **Inter + `ss03` is scoped to the Notes module only** via a
  wrapper class on the Notes view root. Accepted as an intentional,
  contained inconsistency — Notes will look and read slightly differently
  from sibling modules until/unless a future pass takes the app-wide font
  decision separately.
- **Card treatment — Option A (neutral card + accent dot), not Option B
  (keep colored panel, just fix alpha).** Validated via mockup: card
  background goes back to plain `surface` (`#0d0d0d`) with a hairline
  border — the DESIGN.md `feature-card-dark` shape — and the note/folder
  color is now a small `{rounded.xs}` square dot next to the title, plus
  tag chips styled as `-soft` badges (15% alpha fill, matching
  `accent-*-soft` exactly). This is the more literal reading of DESIGN.md's
  "saturated accent colors... never on chrome buttons or text" rule.
- **Sidebar rows use `command-palette-row` / `command-palette-row-active`**,
  not `pill-tab` — `pill-tab` is DESIGN.md's horizontal filter-chip
  component (used in Raycast's store filter strip), while the Notes
  sidebar is a vertical list of selectable rows, which is exactly what
  `command-palette-row` describes (transparent default, `surface-card`
  background when active, `{rounded.sm}` 6px).
- **Shared components (`Button`, `Input`, `Dialog`) are left alone.**
  `Button` already resolves color-for-color to DESIGN.md's
  `button-primary` (`bg-primary` #ffffff / `text-primary-foreground`
  #000000) with no shadow class, so no override is needed. `Popover`
  (`frontend/src/components/ui/popover.tsx:31`) does carry `shadow-md` by
  default — DESIGN.md has zero drop-shadows in the entire system — so the
  one `PopoverContent` Notes renders (the color picker) gets a scoped
  `shadow-none` override at its call site, not a change to the shared
  `popover.tsx`.

## Token mapping

All values below are DESIGN.md tokens; the "App class" column is how they're
already expressed in this codebase's Tailwind theme
(`frontend/src/index.css`), so no new CSS variables are introduced —
this pass is about which existing token each element uses, not about adding
new ones.

| DESIGN.md token | Value | App Tailwind equivalent | Where it lands in Notes |
|---|---|---|---|
| `{rounded.lg}` | 10px | `rounded-lg` (`--radius-lg`) | `NoteCard` outer frame (was `rounded-2xl`/18px) |
| `{rounded.md}` | 8px | `rounded-md` (`--radius-md`) | Editor `Input`/`Select` trigger, color-picker swatches container |
| `{rounded.sm}` | 6px | `rounded-sm` (`--radius-sm`) | Sidebar row active state |
| `{rounded.xs}` | 4px | `rounded-[4px]` | Note/folder color accent dot, tag `-soft` badge |
| `{typography.body-sm-strong}` | 14px / 500 / 0.2px | `text-sm font-medium tracking-[0.2px]` | `NoteCard` title |
| `{typography.caption-md}` | 13px / 400 / 0.1px | `text-[13px] tracking-[0.1px]` | `NoteCard` preview text |
| `{typography.caption-sm}` | 12px / 400 / 0.4px | `text-xs tracking-[0.4px]` | `NoteCard` tag + relative-time text, sidebar row count |
| `{typography.body-md}` | 16px / 400 | `text-base` | Sidebar row label |
| `{spacing.md}` | 12px | `p-3` | `NoteCard` outer padding |
| `{spacing.sm}` | 8px | `p-2` | Sidebar row / `command-palette-row` padding-y equivalent (6px×10px, closest bracket values) |
| `accent-*-soft` alpha | `rgba(color, 0.15)` | `color-mix(in oklch, var(--note-folder-<x>) 15%, transparent)` | Tag badge background in `NoteCard` |

Note colors keep the existing 9-value `--note-folder-*` palette (unchanged
from the prior spec) — the `-soft` convention is applied *to that palette*,
not replaced by DESIGN.md's 4-color accent set (blue/red/green/yellow),
since folders/notes need more than 4 distinguishable colors.

## Component changes

### `NoteCard.tsx`

- Outer element: `rounded-lg border border-border bg-card p-3` (was
  `rounded-2xl ... p-[5px]`). No more inset "mini-window" panel — the whole
  card is one flat surface, matching `feature-card-dark`.
- Title row: small `{rounded.xs}` (4px) square dot before the title —
  `background: notePanelStyle` becomes a *solid* fill (not 15%/30% alpha —
  the dot itself is the accent, at full color) sized ~8×8px; hidden
  entirely when `effectiveColor` is `""`. Title text
  `text-sm font-medium tracking-[0.2px] truncate`.
- Preview text: `text-[13px] tracking-[0.1px] leading-[1.4] text-muted-foreground line-clamp-3`.
- Tag row: each tag becomes a `-soft` badge —
  `background: color-mix(in oklch, var(--note-folder-<color>) 15%, transparent)`,
  `text-[color]` at full saturation (matching `accent-blue-soft`'s pairing
  of 15%-alpha background with full-saturation text), `rounded-[4px]`,
  `px-1.5 py-0.5`, `text-xs tracking-[0.4px]`. A tag whose color can't be
  resolved (shouldn't happen — tag color is always derived, never absent)
  falls back to the existing `bg-muted` neutral chip.
- Relative time: unchanged position (bottom-right of the row), restyled to
  `text-xs tracking-[0.4px] text-muted-foreground`.
- `notePanelStyle` in `noteColor.ts` is replaced by two smaller helpers:
  `noteAccentColor(color)` → the plain `var(--note-folder-<x>)` string (for
  the dot and tag text), and `noteSoftBackground(color)` → the
  `color-mix(... 15% ...)` string (for the tag badge background).

### `NotesDashboard.tsx`

- Sidebar row (`scopeButtonClass` helper): default state
  `rounded-sm px-2.5 py-1.5 text-base` (was `rounded-lg ... text-sm`),
  transparent background; active state `bg-muted` (`surface-card`, already
  the correct token — no change there) `text-foreground`. Count badge text
  drops to `text-xs tracking-[0.4px]` (`caption-sm`).
- No shadow anywhere in the sidebar/grid chrome (there wasn't one before —
  confirmed no regression risk here).
- Root wrapper of the whole dashboard gains the Notes font-scope class (see
  Typography below).

### `NoteEditor.tsx`

- Same font-scope class on its root wrapper.
- Color `Popover`'s `PopoverContent` gets `className="shadow-none"` at the
  call site (the only shadow-bearing element Notes renders).
- No other structural change — folder `Select`, tag `Input`, title `Input`
  already route through shared components that already match DESIGN.md's
  `text-input` token (surface-elevated background, hairline border,
  `rounded-md`), so nothing to fix there.

### `ColorPicker.tsx`

- Swatch buttons keep their current circular shape (DESIGN.md has no
  "color swatch" component to map to — this one stays a reasonable,
  self-contained addition per the prior spec, matching the app's own
  `--radius-full` convention already in use for the same purpose elsewhere
  in this file's caller).
- No other change.

## Typography (Inter + ss03, Notes-scoped)

`frontend/src/index.css` already imports `@fontsource-variable/inter`
(confirmed present, used by no component yet). Add one scope class instead
of touching the global `--font-sans` token:

```css
.notes-font-scope {
  font-family: 'Inter Variable', sans-serif;
  font-feature-settings: "calt", "kern", "liga", "ss03";
}
```

Applied once, on the outermost element `NotesView.tsx` renders (wraps both
the dashboard and the editor screens), so every text node under Notes
inherits it without per-component repetition. Every other module keeps
inheriting the app root's Geist `--font-sans` untouched.

## Error handling

No behavioral/error-path changes — this pass is purely visual token
alignment. Existing toast-on-failure, autosave, and validation behavior
(from the prior two specs) is unchanged.

## Testing / verification

No backend changes, so no Go tests. Frontend: no test runner, per existing
project convention (unchanged). Verification is `cd frontend && bunx tsc
--noEmit` plus a manual pass:

1. Note cards render as flat `surface` rectangles (no inset panel), 10px
   corner radius, with a small solid-color square dot next to the title
   when the note has an effective color, and none when it doesn't.
2. Tags on a card render as soft-background badges (15% alpha) with
   full-saturation text, not the previous dot-and-label chip.
3. Title truncates on one line at the new size; long titles still never
   wrap (unchanged rule from the prior spec).
4. Sidebar rows: active row shows the `surface-card` background at 6px
   radius; inactive rows are transparent until hovered.
5. Opening the note color `Popover` shows no drop-shadow under its panel
   (only the existing `ring-1`/hairline border).
6. Notes text (dashboard, card, editor) visibly renders in Inter (distinct
   from the rest of the app's Geist) — check the alternate single-story `g`
   is present if the system has Inter installed/loaded.
7. No visual change to Database/SSH/Kafka/Settings/etc. — confirms the
   font scope class and any Notes-local overrides didn't leak outside
   `frontend/src/modules/notes/`.

## Out of scope recap

App-wide font change, shared `Button`/`Input`/`Dialog`/`Popover` component
edits, dashboard structural changes, note-folder color palette changes,
`note.color` data model, 3:2 card ratio, a full-app shadow/token audit.
