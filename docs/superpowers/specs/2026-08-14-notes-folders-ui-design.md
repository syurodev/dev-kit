# Notes — folders, tags, dashboard, slash menu

**Date:** 2026-08-14
**Status:** Draft — pending user review
**Supersedes:** the "folders/tags deferred" non-goal in
`docs/superpowers/specs/2026-08-13-notes-module-design.md`. Editing, markdown
storage, autosave, Plate, Mermaid, and the encryption envelope stay as that
spec shipped them.

## Context

Notes is now a working Plate editor over encrypted markdown (`internal/notes`,
`frontend/src/modules/notes/`). The UI is still a master-detail list + editor
with a plain slash combobox. There is no organization: every note is a flat
`type: "note"` vault item whose plaintext metadata is `{ "title": ... }`.

This pass adds one-level folders, freeform tags (including folder → note
inheritance), a dashboard → full-editor navigation, and a restyled slash menu.
The target is many users, so folders are first-class vault records from day one
rather than a denormalized name string on each note.

## Goals

1. Organize notes in **one-level folders** (no nesting) with empty folders
   allowed and rename as a single-record write.
2. **Freeform tags** on both notes and folders. A note's **effective tags** are
   the union of its own tags and its folder's tags. Dashboard can **filter by
   effective tag** (AND when several are selected).
3. Replace master-detail with **dashboard → full editor** (back via
   back/breadcrumb).
4. Restyle the `/` menu to a grouped block list with `Kbd` showing **real
   editor hotkeys**, using **theme tokens** (no hardcoded dark palette) so a
   later light mode is not blocked.

## Non-goals

- Nested folder trees, drag-and-drop reorder, favorites/pin.
- A tag-management screen, shared tag records, or user-picked tag colors.
- SQL / metadata indexes (list is still `ListByType`; join is in-memory).
- Shipping light mode in this pass (only: do not hardcode dark).
- AI / Embeds / extra slash items (H4, image, mermaid). Image stays
  paste/drop; mermaid stays a ` ```mermaid ` code block.
- Changing the encryption envelope, AAD binding, conflict-merge UI, or
  treating markdown as anything other than a `string` body.
- Changing Search from substring match — only **what** is matched expands
  (tags + folder name).

## Decisions validated with user

- **Folders are first-class vault items** (`type: "note-folder"`), not a
  `folder` string on each note. Notes hold `folder_id`. Chosen because the
  product is aimed at many users and empty folders / O(1) rename should be
  cheap from the start.
- **One level only.** Sidebar is a flat list, not a tree.
- **Navigation: dashboard → full editor**, not master-detail. Home is folder
  cards + note grid; opening a note replaces the pane with the existing Plate
  editor.
- **Tags are freeform strings.** Same name is the same tag. Color is hashed
  from the tag name. No tag CRUD screen.
- **Notes inherit folder tags** (live union). **Deleting a folder** ungroups
  its notes and **copies the folder's tags onto each note** so effective tags
  do not change. Moving a note to another folder / ungrouped does **not**
  copy; inheritance is live, so effective tags follow the new folder.
- **Tag filter** on the dashboard, combinable with the selected folder.
  Multiple selected tags use **AND**.
- **Slash menu** looks like a Notion block picker (icon + label + shortcut
  column) but shortcuts are **real hotkeys in `Kbd`**, not Notion markdown
  glyphs (`#`, `>`, `[]`). Items with no hotkey omit `Kbd`. Colors come from
  shadcn/theme tokens, not a hardcoded dark theme.
- **Body stays encrypted markdown.** Folder name/color/tags and note
  title/`folder_id`/own tags stay plaintext metadata so they list without
  decrypting.

## Data model

### Folder record — `vaultitem` type `note-folder`

Constant: `vaultitem.ItemTypeNoteFolder = "note-folder"`.

Must be added to `IsSupportedItemType` and `ValidateRecordSchema`. Sync
rejects unknown types; without this, folders cannot sync.

Plaintext metadata:

```json
{
  "name": "Work",
  "color": "blue",
  "tags": ["work", "q3"]
}
```

- `name` — required, trimmed, non-empty.
- `color` — required, one of the palette below.
- `tags` — optional; omitted or `[]` means none. Normalized on write (see
  Tag rules).

**Payload:** seal an empty JSON object `{}` under key id `note-folder`.
`ValidateRecordSchema` currently requires a non-nil envelope for every
supported type; a nil payload would fail sync validation. Folders have no
secret content — the envelope exists so the record is a valid vault item.

Palette (stored as these strings, rendered via CSS variables, not raw hex in
components): `red`, `orange`, `yellow`, `green`, `teal`, `blue`, `violet`,
`pink`, `gray`. Map each to a token such as `--note-folder-blue` defined on
`:root` with a dark-theme override under `.dark` when light mode lands.
This pass only needs values that read well on the current dark shell, but
the **component classes must use the variables**, not `#rrggbb`.

### Note metadata — additive

```json
{
  "title": "Standup notes",
  "folder_id": "aabbccddeeff001122334455",
  "tags": ["standup"]
}
```

- `title` — required (unchanged).
- `folder_id` — optional. Empty / omitted = **Ungrouped**. When set, must be
  the id of an existing `note-folder` at write time.
- `tags` — the note's **own** tags only. Optional. Normalized on write.

Existing notes (`{"title":"..."}`) remain valid: ungrouped, no own tags.
`decodeMetadataObject` uses `DisallowUnknownFields`; the schema struct must
gain `FolderID` and `Tags` with `json` names `folder_id` / `tags` so new
writes validate, and missing fields on old records still decode.

### Effective tags

```
effective(note) = unique(note.tags ∪ folder.tags)
```

If `folder_id` is empty or the folder record is missing (orphan — see
Error handling), `folder.tags` is empty.

Union is case-insensitive for uniqueness; the **first spelling wins** (note
own tags first, then folder tags). Display and filter use this list.

Inheritance is **live**: changing a folder's tags changes every member
note's effective tags without rewriting those notes.

### Tag rules

On every write of `tags` (note or folder):

1. Trim whitespace; drop empties.
2. Dedupe case-insensitively, keep the first spelling in input order.
3. Cap at 32 tags per record; each tag at most 40 characters. Reject the
   write if a tag exceeds 40 characters after trim (do not silently clip).
   If more than 32, reject (do not silently drop).

Color: hash the **lowercased** tag string to one of eight chip tokens
(`--note-tag-0` … `--note-tag-7`), same CSS-variable rule as folder colors.
No per-tag stored color.

### Delete-folder copy-down

Atomic from the caller's point of view (service method does all note
metadata writes then deletes the folder):

1. Load the folder. Not found → error (same as deleting a missing note).
2. For each note with `folder_id == folder.ID`:
   - `tags = unique(note.tags ∪ folder.tags)` (same union rule).
   - `folder_id = ""`.
   - `UpdateMetadata` only (do not reseal the body).
3. Delete the folder record.

After this, effective tags of those notes equal what they were while in the
folder. Notes are **Ungrouped**. Folder tags are not kept as a ghost record.

**Move** (including to Ungrouped) does **not** copy-down. Effective tags
become `note.tags ∪ newFolder.tags` immediately.

## Backend

Keep a single `notes.Service`. Folders are not a separate module; they are
another item type the notes service owns. Widen the `Store` interface with
`UpdateMetadata` (already on `vaultitem.Store`) and `ListByType` is already
there.

### Folder API

```
ListFolders() []Folder
CreateFolder(name, color string, tags []string) (Folder, error)
UpdateFolder(id, name, color string, tags []string) (Folder, error)  // UpdateMetadata
DeleteFolder(id string) error  // copy-down then delete
```

`Folder` is `{ID, Name, Color, Tags, CreatedAt, UpdatedAt}`. No payload
returned.

Validation: `name` required; `color` must be in the palette; `tags`
normalized / capped as above.

### Note API changes

```
Create(title, body, folderID string, tags []string) (Summary, error)
Update(id, title, body string) (Note, error)
SetOrganization(id, folderID string, tags []string) (Summary, error)
```

- `Create` / `SetOrganization`: empty `folderID` = ungrouped; non-empty must
  exist. `Update` **does not take** folder/tags — it read-modify-writes
  metadata so title/body autosave cannot wipe organization.
- `SetOrganization` is `UpdateMetadata` only (no body reseal). Used when the
  editor or dashboard changes folder or own tags.
- `Summary` and `Note` grow `FolderID`, `Tags` (own), and `EffectiveTags`
  (computed at read time from the in-memory folder map). `Summary` also
  grows `Preview`: while the vault is unlocked, decrypt the body, strip to
  plain text, take ~120 characters; if locked or decrypt fails, `Preview`
  is empty. Dashboard cards use `Preview`; they do not N+1 `Get`.

`List` and `Search` both populate `EffectiveTags` and `Preview` the same
way. `Search` substring-matches, case-insensitive, against **title, body
(when decrypted), each effective tag, and the folder name**. Empty query
still returns all notes, newest first.

Reject `SetOrganization` / `Create` with `folder_id` pointing at a missing
or non-folder id: `folder not found`. Do not persist orphans on purpose.
If a folder is deleted on another device and a note still points at it
until copy-down runs locally, treat as ungrouped for effective tags and
show it under **Ungrouped** (do not crash the list).

### Bridge and permissions

Follow existing `NotesUpdate` / `notes:write` patterns.

New grants on `builtin.notes`:

- `notes:write` / `note-folder:new`
- `notes:write` / `note-folder:*`
- `notes:read` / `note-folder:list`
- `notes:read` / `note-folder:*`

Bridge methods (names illustrative; match current `Notes*` style):

- `NotesFolderList`, `NotesFolderCreate`, `NotesFolderUpdate`,
  `NotesFolderDelete`
- `NotesCreate` gains `folderID` + `tags`
- `NotesSetOrganization(id, folderID, tags)`
- `NotesList` / `NotesSearch` / `NotesGet` return the widened structs

### MCP

- `notes.create` — optional `folder_id`, `tags`
- `notes.update` — title + body only (preserves organization)
- `notes.set_organization` — `id`, `folder_id`, `tags`
- `notes.folder.list` / `create` / `update` / `delete`
- `notes.search` — matches the expanded fields above
- `notes.list` / `get` — return `folder_id`, `tags`, `effective_tags`

`notes.folder.delete` runs the copy-down path. Scopes: `note-folder:new` /
`note-folder:<opaqueID>` / `note-folder:list`, same 24-hex opaque-id
guard as notes.

## Frontend

### Navigation

`NotesView` has two screens in the same workbench pane:

1. **Dashboard** (default).
2. **Editor** — current `NoteEditor` (Plate), full pane, no list beside it.

Selecting a note sets the editor screen. Back control + breadcrumb
`Notes / {folder name or Ungrouped} / {title}` returns to the dashboard
(folder sidebar selection preserved). Switching notes is not a thing on
the dashboard; open one at a time.

Flush unsaved editor state on leave (already required when switching notes).

### Dashboard layout

Adapted to the existing dark floating-dock shell — not a light SaaS clone
(no avatar column, no tasks column).

- **Left:** virtual rows **All** and **Ungrouped**, then folder rows
  (color dot + name + note count), then a control to create a folder.
  Clicking a folder **filters the grid**, it does not open the editor.
  Rename / change color / edit folder tags / delete live on the folder row
  (overflow menu). Delete asks for confirmation: notes are kept, tags are
  copied, folder is removed.
- **Right:** card grid of notes in the current folder scope. Each card:
  title, `Preview`, effective-tag chips (no “inherited” badge — same chip
  style; color from name hash). Click → editor.
- **Top of the grid:** search input (calls `searchNotes`) and a tag-filter
  control. Tag filter is **exact membership** in `effective_tags`, AND
  across selected tags. Folder sidebar scope AND search AND tag filter
  compose. Clear tag chips to drop the filter. Clicking a chip on a card
  adds that tag to the filter.
- **New note** creates in the currently selected folder, or ungrouped when
  **All** / **Ungrouped** is selected, then opens the editor.

### Editor chrome

Keep Plate, autosave (~1.5s), Saving/Saved, delete. Add:

- Back / breadcrumb as above.
- Folder picker (including Ungrouped). Change → `SetOrganization`
  immediately (not waiting for body autosave).
- Own-tag input: type to add, same normalize rules, `SetOrganization` on
  change.
- Inherited tags shown **read-only** under the own-tag input (folder tags
  not in own tags). They are not removable here; edit the folder instead.

Title + body still go through `Update`, which must preserve organization
on the server.

### Slash menu

Keep the current block set and insert behavior. Restyle
`SlashInput` / `InlineCombobox`:

- One group label: **Basic blocks**.
- Row: lucide icon, label, optional `Kbd` on the right.
- Tokens only: `bg-popover`, `text-popover-foreground`, `border-border`,
  `text-muted-foreground`. No `bg-zinc-950`, no Notion hex, no
  `dark:`-only colors that have no light counterpart.
- Empty: “No results”.
- Not in the menu: image, mermaid, H4, AI.

`Kbd` shows **plugin hotkeys**, with `Mod` = ⌘ on macOS and Ctrl elsewhere:

| Block | Kbd |
|---|---|
| Heading 1 | `Mod+Alt+1` |
| Heading 2 | `Mod+Alt+2` |
| Heading 3 | `Mod+Alt+3` |
| Quote | `Mod+Shift+.` |
| Code block | `Mod+Alt+8` |
| Text, bulleted list, numbered list, to-do, toggle, table, divider | omit |

Markdown input rules (`# `, `- `, ` ``` `, etc.) stay registered on the
plugins; they are just not duplicated on the menu.

Floating selection toolbar: keep; restyle with the same token rule if any
hardcoded dark classes exist.

## Error handling

- Empty note title / folder name → validation error, no write.
- Invalid folder color or oversize tag list → validation error.
- `folder_id` not found on create/organize → reject; toast in UI.
- Autosave / folder CRUD failure → keep local editor/dashboard state, toast
  or inline error, retry on the next user action (no retry loop). Matches
  the 2026-08-13 autosave policy.
- Delete folder: confirmation required in UI; MCP/bridge skip the prompt
  and run copy-down immediately.
- Orphan `folder_id` (folder gone): list as Ungrouped, effective tags =
  own tags only.

## Testing / verification

Backend `go test` for:

- Schema accepts `note-folder` (non-empty `name`, `color` one of the nine
  palette strings, optional `tags`, payload key id `note-folder`) and note
  metadata with optional `folder_id`/`tags`; rejects unknown fields, empty
  folder name, empty note title, unknown color, missing folder payload.
- Create/list/rename/delete folder; empty folder allowed.
- Delete folder copy-down: notes ungrouped, tags unioned, folder gone.
- `Update` body does not clear `folder_id`/`tags`.
- `SetOrganization` does not reseal (assert via service tests that body
  from `Get` is unchanged).
- Search hits tag and folder name.
- Effective tags = union, live after folder tag update without touching
  the note record.

Frontend: no test runner (unchanged). Manual: dashboard filter (folder +
AND tags), inherit chips, delete-folder copy-down, slash `Kbd` + token
styling, create-in-folder, editor round-trip.

## Out of scope recap

Nested folders, DnD, tag admin, SQL indexes, light-mode theme itself,
slash AI/embeds, encryption/merge changes.
