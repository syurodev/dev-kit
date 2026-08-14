# Notes — card redesign and per-note color

**Date:** 2026-08-14
**Status:** Draft — pending user review
**Supersedes:** the dashboard card visuals from
`docs/superpowers/specs/2026-08-14-notes-folders-ui-design.md` (the "Right:
card grid" bullet under **Dashboard layout**). Folder-first data model,
one-level folders, live tag inheritance, dashboard→editor navigation, and the
slash menu from that spec are unchanged and stay as shipped.

## Context

The folders/tags/dashboard work from the prior spec is implemented and
tested (`internal/notes`, `internal/bridge`, `internal/mcp`,
`frontend/src/modules/notes/`). Functionally complete, but the dashboard
reads as plain: flat bordered sidebar rows, note cards that are just a
bordered `bg-card` rectangle with no visual identity per note. This pass
redesigns the note card only, and adds one new capability the visual
direction needs: **a note can carry its own color**, separate from (but
defaulting to) its folder's color.

Direction was worked out interactively with mockups (three initial style
directions, then iterated through a Google-Keep-style reference, a job-card
reference, and the app's own window-chrome screenshot) — see the "Decisions
validated with user" section below for the resolved shape.

## Goals

1. Redesign the note card as a **"mini-window"**: a neutral dark outer frame
   (same visual language as the app's own window chrome — dark bezel, inset
   content pane) containing the title in the frame, and an **inset colored
   panel** below it holding the preview, tags, and relative time.
2. Give each note an **optional own color**, reusing the folder palette (the
   same 9 strings and the same `vaultitem.ValidNoteFolderColor` validator —
   no second color system). A note's **effective color** is its own color if
   set, else its folder's color (live), else neutral. Same inheritance shape
   as effective tags, including copy-down on folder delete.
3. Cards are a uniform **3:2** grid (no masonry, no variable heights).
4. Note title is **always one line** — CSS truncation (`ellipsis`), never
   wraps, regardless of length.
5. No structural change to the dashboard: left sidebar (All / Ungrouped /
   folders + management), search, and the AND tag filter stay exactly as
   shipped. This pass only touches the note card's internals and adds the
   color field end to end.

## Non-goals

- A freeform/custom hex color picker. Notes reuse the folder's 9-color
  palette only.
- Setting a note's color from the dashboard card directly (hover swatch,
  right-click, etc.). MVP is editor-only, same pattern as the folder color
  picker.
- Gradients or colored drop-shadow/glow effects on cards (tried during
  mockup iteration, explicitly rejected in favor of the inset-panel look).
- Masonry / variable-height cards.
- Any change to the folder color system, folder CRUD, tag rules, dashboard
  navigation, sidebar structure, or the slash menu — all out of scope, all
  stay as the prior spec shipped them.
- Light theme, nested folders, drag-and-drop — still out of scope per the
  prior spec.

## Decisions validated with user

- **Sidebar stays** (rejected a top pill-tab nav explored during mockups):
  folder management (rename/color/tags/delete) needs an anchored place, and
  the sidebar already does this well.
- **Color source is per-note, not per-folder-only.** A note can override its
  inherited folder color. Chosen over "purely folder-derived" because the
  user wants individual notes to carry visual identity, matching a
  Keep/sticky-note-style reference. Chosen over "separate freeform pastel
  palette" because reusing the folder's 9 colors is less backend surface and
  stays visually consistent with the folder dots/tag chips already in the
  UI.
- **Card visual = inset panel, not full-bleed tint, not thin border alone,
  not colored glow.** Iterated through: (a) full-card flat color wash →
  rejected, felt like it fought the neutral `--card` surface every other
  module uses; (b) colored border + colored box-shadow glow (from a job-card
  reference) → rejected, "too strong a shadow"; (c) thin 1px colored border
  only → rejected as under-expressive once color was already approved; (d)
  **mini-window**: dark neutral outer frame (same idiom as the app's own
  title-bar-and-inset-content-pane window chrome) with a smaller-radius
  colored panel inset inside it → **accepted**, refined twice (tighter
  frame padding, richer panel alpha + a matching 1px inner border for edge
  definition).
- **Aspect ratio 3:2** (landscape), not the initially-tried 2:3 (portrait) —
  changed once the title-truncation requirement made a wider card more
  natural.
- **Title never wraps.** Single line, CSS `truncate` (nowrap + ellipsis).
- **Tags inside the panel are dot + label**, matching the `TagChip` pattern
  already used elsewhere in the dashboard/editor, not a separate outlined
  pill style (tried and rejected as one more UI language on the same card).

## Data model

### Note metadata — add `color`

```json
{
  "title": "Standup notes",
  "folder_id": "aabbccddeeff001122334455",
  "tags": ["standup"],
  "color": "blue"
}
```

- `color` — optional. Empty or omitted means "no own color" (inherit from
  folder, see below). When present, must be one of the same nine palette
  strings as folders (`red`, `orange`, `yellow`, `green`, `teal`, `blue`,
  `violet`, `pink`, `gray`); validated with the existing
  `vaultitem.ValidNoteFolderColor`. Invalid value → reject the write
  (`invalid note color`), same pattern as folder color validation.
- Existing note records without `color` remain valid (no own color, same as
  a record with no `tags`).

### Effective color

```
effectiveColor(note) = note.color != "" ? note.color
                      : (folder exists ? folder.color : "")
```

Live, same inheritance shape as `EffectiveTags`: changing a folder's color
changes every member note's effective color without rewriting those notes.
An empty `effectiveColor` renders a neutral (non-tinted) panel.

### Delete-folder copy-down — extended

The existing copy-down (unions the folder's tags onto each member note,
then ungroups it — see the prior spec) gains one more step:

1. For each note with `folder_id == folder.ID`:
   - `tags = unique(note.tags ∪ folder.tags)` — unchanged.
   - **If `note.color == ""`** (was inheriting the folder's color):
     `color = folder.Color`.
   - **If `note.color != ""`** (note already had an explicit own color):
     leave it untouched.
   - `folder_id = ""`.
2. Delete the folder record.

This preserves each note's effective color after the folder is gone, the
same way tag copy-down preserves effective tags.

**Move** (including to Ungrouped) still does **not** copy anything: a note
with no own color immediately reflects the new folder's color (or neutral,
if ungrouped); a note with an explicit own color is unaffected by moving.

## Backend

### Schema (`internal/vaultitem/schema.go`)

Note metadata struct gains `Color string `json:"color,omitempty"``.
Validation: `value.Color == "" || ValidNoteFolderColor(value.Color)`, else
`ErrInvalidRecordSchema`. `ValidNoteFolderColor` is reused as-is (already
exported for folders).

### `notes.Service`

- `SetOrganization(id, folderID string, tags []string, color string) (Summary, error)`
  — grows a fourth parameter. `UpdateMetadata`-only (no body reseal),
  same as today. Validates `color` the same way `Create`/schema does.
- `Summary` and `Note` gain `Color string` (json `color`) and
  `EffectiveColor string` (json `effective_color`), computed via the same
  `loadFolderMap` lookup used for `EffectiveTags`. Always a plain string,
  `""` when there is no effective color.
- `Create` does **not** take a color argument — a brand-new note has no own
  color at creation time (matches "no own tags at creation" from the prior
  spec); the user sets one afterward via `SetOrganization`, same flow as
  tags.
- `DeleteFolder` implements the extended copy-down above.

This is a signature change to an already-shipped internal method
(`SetOrganization`). Every call site from the prior plan's inventory needs
the same treatment: `internal/notes/notes_test.go`,
`internal/bridge/notes.go` + `internal/bridge/notes_test.go`,
`internal/mcp/server.go` + `internal/mcp/tools_notes.go` +
`internal/mcp/server_test.go`, and the frontend wrapper below. Small,
enumerable set — same shape as the `Create` arity change the prior plan
already did once.

### Bridge (`internal/bridge/notes.go`)

`NotesSetOrganization(id, folderID string, tags []string, color string) (notes.Summary, error)`
— grows the same fourth parameter, same capability/scope pattern as today
(`notes:write`, `note:<id>`).

### MCP (`internal/mcp/tools_notes.go`, `server.go`)

`notes.set_organization` input gains optional `color string `json:"color"``.
`notesSetOrganizationInput` and `fakeNotes.SetOrganization` in
`server_test.go` grow the parameter. `notes.list` / `get` / `search` outputs
include `color` and `effective_color` (flow through automatically once
`Summary`/`Note` carry the fields).

### Frontend bindings (`frontend/bindings`, `frontend/src/bridge/notes.ts`)

Regenerate Wails bindings (`wails3 generate bindings`) after the Go
signature change. `NoteSummary` and `Note` types gain `color: string` and
`effectiveColor: string` (mapped from `color`/`effective_color` the same
way `folderId`/`effectiveTags` are mapped today). `setNoteOrganization`
wrapper grows to `setNoteOrganization(id, folderId, tags, color)`.

## Frontend

### `NoteCard` component (new)

Extract the current inline card markup in `NotesDashboard.tsx` into
`frontend/src/modules/notes/NoteCard.tsx` — the redesign is enough new
structure that inlining it in the dashboard file would make that file
harder to read, and the card is naturally its own unit (props: a
`NoteSummary`, an `onOpen`, an `onTagClick`).

Structure (all theme tokens, no hardcoded hex — a new
`frontend/src/modules/notes/noteColor.ts` helper mirrors `tagColor.ts` and
resolves a palette name to the `--note-folder-*` CSS variables already
defined):

- Outer frame: `bg-card border border-border rounded-2xl p-[5px]
  aspect-[3/2] flex flex-col` — the "window bezel."
- Title row (lives in the frame, not the panel): `px-1.5 pt-1 pb-1.5
  text-[13px] font-semibold truncate` — Tailwind's `truncate` utility is
  exactly `white-space: nowrap; overflow: hidden; text-overflow: ellipsis`,
  satisfying the one-line requirement with no extra CSS.
- Inset panel: `flex-1 rounded-xl p-2.5 flex flex-col overflow-hidden
  min-h-0`, background and border color from `noteColor.ts`'s
  `notePanelStyle(effectiveColor)`: ~30% alpha fill of the resolved
  `--note-folder-*` variable plus a matching ~45% alpha 1px border for edge
  definition; falls back to `bg-muted` with no colored border when
  `effectiveColor` is `""`.
- Preview text inside the panel: `text-[10px] line-clamp-3 flex-1`
  (existing `line-clamp` pattern, already used for the current preview).
- Footer row inside the panel: tag chips (dot + label, transparent
  background since they already sit on a tinted panel — a lighter variant
  of the existing `TagChip`) on the left, relative time on the right, both
  in a muted-on-color tone.
- No note has a color and no folder → the panel is the plain `bg-muted`
  neutral case (see the "Untitled / No preview" card in the mockups).

### Grid

`NotesDashboard.tsx`'s card grid changes from the current
`grid-cols-[repeat(auto-fill,minmax(14rem,1fr))]` free-height cards to a
grid of fixed `aspect-[3/2]` `NoteCard`s at a comparable column width (exact
`minmax` breakpoint is an implementation call, not a design constraint —
just needs to keep the 3:2 ratio per card). No masonry / CSS `columns`.

### Editor color picker (`NoteEditor.tsx`)

Reuse the `ColorPicker` component already built for folders in
`NotesDashboard.tsx` (move it to a shared location, e.g.
`frontend/src/modules/notes/ColorPicker.tsx`, since both the folder dialogs
and the note editor now use it). In the editor header, next to the existing
folder `Select`: a `Popover` (shadcn) triggered by a `Palette` (lucide-react)
icon button showing the note's current effective color as a small swatch.
Selecting a palette color calls `setNoteOrganization(id, folderId, tags,
color)` immediately (same "don't wait for autosave" rule already used for
folder/tag changes). One extra swatch — visually distinct, e.g. a
slash-circle — clears the own color back to `""` ("Inherit from folder").

## Error handling

Unchanged patterns from the prior spec, extended to `color`:

- Invalid `color` value on `SetOrganization` → reject, no write
  (`invalid note color`); UI shows a toast, keeps local editor state (same
  policy as the existing folder/tag error handling — no retry loop).
- `color` on a note pointing at a deleted/orphan folder: same "treat as
  ungrouped" rule as tags — `effectiveColor` falls back to the note's own
  color if set, else neutral. Never crashes the list.

## Testing / verification

Backend `go test` additions to the existing `internal/notes` suite:

- Schema: reject an unknown `color` string; accept a note record with
  `color` omitted (back-compat with pre-this-pass records).
- `SetOrganization` sets and clears `color`; `Get`/`List` return the write
  back unchanged (no reseal — same assertion style already used for
  tags/folder in the prior spec's tests).
- `EffectiveColor` resolves: note's own color wins; else live folder color;
  else `""` when neither is set.
- `DeleteFolder` copy-down: a note with no own color ends up with
  `color == folder.Color` after delete; a note that already had an explicit
  own color is untouched by the same delete.

Bridge/MCP: extend the existing `TestNotesBridgeFoldersAndOrganization` /
`TestNotesSetOrganizationTool` style tests to cover the fourth parameter.

Frontend: no test runner, per existing project convention. Manual script:

1. Create a note in a colored folder — card panel tinted with the folder's
   color, no own color set yet.
2. Set an explicit own color different from the folder's — card follows the
   own color, not the folder's.
3. Clear the own color ("Inherit from folder") — card reverts to live
   tracking of the folder's color.
4. Delete that folder — the note (still on its own explicit color from step
   2) is unaffected; a sibling note that was still inheriting keeps the same
   visual tint post-delete via copy-down.
5. A very long title never wraps to a second line; it truncates with an
   ellipsis.
6. A note with no folder and no own color renders the neutral (`bg-muted`)
   panel, matching the "Untitled" card in the mockups.

## Out of scope recap

Freeform/custom colors, dashboard-card inline color quick-set, gradients,
colored shadow/glow, masonry, top pill-tab navigation, sidebar
restructuring, folder system changes, light theme, nested folders,
drag-and-drop.
