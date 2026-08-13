# Notes module — complete the editing experience

**Date:** 2026-08-13
**Status:** Draft — pending user review

## Context

`dev-kit-app`'s Notes module (`internal/notes`, `frontend/src/modules/notes/`)
is a thin adapter over the shared `vaultitem.Store` substrate — the same
encrypted-record mechanism SSH and Database profiles use. Its documented
"known limitation" (`base-doc/content/projects/dev-kit/notes.mdx`, confirmed
by reading `internal/notes/notes.go` and `frontend/src/bridge/notes.ts`) is
that **there is no way to edit a note's content after creation** — only
Create/Get/List/Search/Delete exist, at every layer (`vaultitem.Store`,
`notes.Service`, the bridge, and the MCP tool surface). The UI's body input
is a plain `<textarea>`.

This spec closes that gap and replaces the textarea with a Plate
(platejs.org) rich-text editor giving Notion-like block editing with
markdown-shortcut input, while keeping the note body stored as plain
markdown text — a zero-schema-change decision explained in §3.

## Goals

1. Add a real Update path — `vaultitem.Store` → `notes.Service` → bridge →
   MCP — so editing a note's title/body is possible for the first time.
2. Replace the `<textarea>` with a Plate editor giving Notion-like block
   editing (headings, lists, toggle, quote, code block, table, image,
   divider, link) with markdown-shortcut typing (`# ` → heading, `**bold**`
   → bold, etc.) and a slash-command menu for block insertion.
3. Support Mermaid diagrams inside notes — a fenced ` ```mermaid ` code
   block that renders as an actual diagram, toggling to raw source for
   editing.
4. Move the module to a master-detail layout (note list + full-height
   editor) instead of today's stacked create-form/list/detail-pane.
5. Autosave — no explicit Save button.

## Non-goals

- Note organization features (tags, folders, favorites/pin) — explicitly
  deferred; this pass is "make editing work," not "make notes more
  featureful to manage."
- Changing the vault-item storage schema, encryption envelope, or AAD
  binding — Update reuses the exact `SealV2`/`OpenWithAD` mechanism
  `Create` already uses.
- Changing `Search` behavior (still substring match over plaintext title +
  decrypted body) — markdown text is exactly as searchable as the current
  plain-text body.
- Changing the sync conflict-merge UI (`ResolvePanel`, git-style markers) —
  it already operates on raw text, which markdown continues to be.
- A generic "any module can embed diagrams" mechanism — Mermaid support is
  scoped to Notes' code block renderer only.

## Decisions validated with user (brainstorming)

- **Storage format: markdown text, not Plate's JSON AST.** `body` stays a
  `string`, identical to today. Plate's `@platejs/markdown` plugin
  round-trips via `serializeMd`/`deserializeMd`, so the editor gives a rich
  block UI while the wire/at-rest format never changes. This was the
  deciding factor for every other "keep as-is" non-goal above — Search,
  merge-conflict resolution, and the encryption envelope all operate on
  text and stay untouched.
- **Save behavior: autosave, debounced ~1.5s after the user stops typing.**
  No Save button. A small status indicator ("Saving…" / "Saved") lives in
  the editor header.
- **Feature set: extended**, not just Notion-basics — includes tables,
  toggle (collapsible) blocks, and inline image embeds, in addition to
  headings/marks/lists/quote/code/divider/link.
- **Images: embedded as base64 data URIs inside the markdown**, not saved
  to a separate file store. This matches the app's local-first,
  zero-separate-asset-surface architecture — the image lives inside the
  same encrypted payload as everything else, no new decrypt/serve path
  needed. Plate's default caps (4MB/image, 3 images/insert) apply as-is;
  enforced client-side before insertion, not silently truncated.
- **Layout: master-detail, floating selection toolbar, no fixed toolbar
  bar.** Confirmed via a visual mockup comparison (Option A). Formatting
  controls only appear when text is selected (Notion-style), plus a
  slash-command menu (`/`) for inserting blocks. This was chosen
  specifically because it matches the "no unnecessary chrome" visual
  language already established across the rest of the shell (floating
  dock, minimal header) — a fixed toolbar bar was the losing alternative.
- **Mermaid: reuses the existing `code_block` node, not a new node type.**
  A fenced ` ```mermaid ` block is standard markdown (portable, readable
  outside the app, identical to how GitHub/Obsidian/Notion represent
  Mermaid). Only the *rendering component* is custom — Plate's own docs
  show `CodeBlockPlugin.withComponent(...)` as exactly this extension
  point. No new markdown syntax, no new serialization logic.
- **Mermaid UX: toggle Edit/Preview, not live side-by-side preview.**
  Defaults to rendered diagram; click switches to raw source editing. Cheaper
  (no re-render on every keystroke) and matches how Obsidian/Notion already
  handle this exact case — validated via visual mockup comparison.

## Architecture

### Backend — Update path

`vaultitem.Store` (`internal/vaultitem/store.go`) gets one new method,
placed alongside the existing `UpdateMetadata` (metadata-only) and `Reseal`
(payload-only, caller persists):

```go
// Update rewrites both metadata and payload in one write, bumping
// UpdatedAt and marking the record dirty for sync. The general-purpose
// sibling of UpdateMetadata and Reseal — this is the first store method
// that lets a caller replace both plaintext metadata and the encrypted
// payload atomically in one Put.
func (s *Store) Update(id, keyID string, metadata json.RawMessage, secret []byte) (Record, error) {
	record, err := s.repo.Get(id)
	if err != nil {
		return Record{}, err
	}
	envelope, err := s.vault.SealV2(associatedData(record.Type, id, keyID), secret)
	if err != nil {
		return Record{}, err
	}
	record.Metadata = metadata
	record.Payload = &envelope
	record.UpdatedAt = time.Now().UTC()
	record.SyncState = syncStateLocal
	if err := s.repo.Put(record); err != nil {
		return Record{}, err
	}
	return record, nil
}
```

`s.repo.Put` (the `SQLMutationRepository` in production) already captures
the sync mutation intent on any write — same mechanism `UpdateMetadata`
relies on today, so this needs no new sync plumbing.

`notes.Service` (`internal/notes/notes.go`) gets:

```go
func (s *Service) Update(id, title, body string) (Note, error) {
	if title == "" {
		return Note{}, errors.New("title is required")
	}
	meta, err := json.Marshal(metadata{Title: title})
	if err != nil {
		return Note{}, err
	}
	record, err := s.store.Update(id, bodyKeyID, meta, []byte(body))
	if err != nil {
		return Note{}, err
	}
	return Note{
		ID:        record.ID,
		Title:     title,
		Body:      body,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}, nil
}
```

This requires widening the `Store` interface in `internal/notes/notes.go`
(currently `Create`/`Get`/`OpenPayload`/`ListByType`/`Delete`) with one new
method matching `vaultitem.Store.Update`'s signature:

```go
Update(id, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
```

Bridge (`internal/bridge/notes.go`), following the exact shape of
`NotesDelete` (same capability, same scope pattern):

```go
func (s *AppService) NotesUpdate(id string, title string, body string) (notes.Note, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.notes", "notes:write", "note:"+opaqueID, map[string]any{
		"action":  "update",
		"note_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Notes.Update(id, title, body)
	})
	if err != nil {
		return notes.Note{}, err
	}
	return result.(notes.Note), nil
}
```

No `container.go` policy change needed — `notes:write` already grants scope
`note:*` (confirmed by reading `internal/app/container.go:106-107`), which
covers `note:<id>` scopes for both `NotesDelete` and this new `NotesUpdate`.

MCP tool surface (`internal/mcp/tools_notes.go`) gets `notes.update`
(`execute` effect, same shape as `notes.create`), so an agent editing a
note's content no longer has to delete-and-recreate.

### Frontend — file structure

```
frontend/src/modules/notes/
  NotesView.tsx        — master-detail shell (list left, editor right).
                          Full rewrite of the current stacked layout.
  NoteList.tsx          — the note list + search box, extracted from the
                          current NotesView (logic unchanged: listNotes/
                          searchNotes, click to select).
  NoteEditor.tsx         — owns the Plate instance for the selected note:
                          markdown deserialize on load, serialize + debounced
                          autosave (~1.5s) via the new NotesUpdate bridge
                          call, "Saving…/Saved" status indicator.
  plate/
    plugins.ts            — the full Plate plugin kit list (see below).
    MermaidCodeBlock.tsx    — custom render component for code_block nodes
                             where lang === "mermaid": toggles between a
                             rendered <svg> (via the `mermaid` package,
                             version pinned to match base-doc's existing
                             ^11.16.0 usage) and the raw source textarea.
    ImageUpload.ts          — Plate's ImagePlugin upload handler: reads a
                             dropped/pasted file, converts to a base64 data
                             URI, returns it directly (no network I/O) —
                             the exact "local upload" pattern Plate's own
                             ImagePlugin docs describe (`uploadImage` may
                             return the original Data URL input unchanged).
                             Enforces the 4MB/image, 3-images/insert caps
                             client-side before calling Plate's insert, with
                             a visible rejection message rather than a
                             silent drop.
```

`bridge/notes.ts` gets one addition: `updateNote(id, title, body):
Promise<Note>`, calling `AppService.NotesUpdate`, following the exact
pattern every other function in that file already uses.

### Plate plugin set

Installed via `npx shadcn@latest add @plate/editor-basic` (matches this
repo's existing shadcn-registry component convention — same install
mechanism as every `components/ui/*.tsx` file already in the tree), then
layered with:

- **Blocks:** paragraph, heading 1-3, blockquote, divider (horizontal
  rule), toggle (collapsible), code block (+ Mermaid override), table,
  image.
- **Marks:** bold, italic, underline, strikethrough, inline code.
- **Lists:** bulleted, numbered, task (checkbox).
- **Inline:** link.
- **Interaction:** slash-command (`/`) for block insertion, floating
  selection toolbar for mark formatting on text select — no fixed toolbar.
- **Markdown:** `@platejs/markdown` with `remark-gfm` (needed for GFM
  tables and task-list markdown syntax specifically, confirmed via Plate's
  docs as the standard way both are enabled).

Each plugin registers its own `inputRules` (Plate's markdown-shortcut
mechanism — e.g. `HeadingRules.markdown()`), which is what gives the
"type markdown, get Notion-style blocks" behavior without any custom
autoformat code.

### Data flow

**Load:** `NoteEditor` receives a note id from `NoteList`'s selection state
→ `getNote(id)` (existing bridge call, unchanged) → the returned `body`
string is deserialized via `editor.api.markdown.deserialize(body)` into the
Plate value used as the editor's initial content.

**Edit → autosave:** every Plate `onChange` resets a 1.5s debounce timer.
On fire: `editor.api.markdown.serialize()` → `updateNote(id, title, body)`
(new bridge call) → status indicator flips "Saving…" → "Saved" on success.
Title is a plain text input above the editor (not part of the Plate
document), matching how `title` has always been a separate field from
`body` at the data-model level (`metadata.Title` vs the encrypted payload)
— no reason to change that split now.

**Mermaid render:** `MermaidCodeBlock` reads the code block's text content,
calls `mermaid.render(...)` when in Preview mode; render errors (invalid
diagram syntax) show inline as an error message in place of the diagram,
not a crash — the user can still switch to Edit mode to fix the source.

### Error handling

- **Autosave failure** (e.g. a `NotesUpdate` call errors): keep the user's
  edits in the editor's local state (nothing is lost — Plate's value lives
  in memory regardless of save success), show an error banner in place of
  the "Saved" status, and retry only on the *next* debounce-triggered save
  (typing again or manually triggering another change), not an automatic
  retry loop.
- **Note deleted elsewhere while open** (e.g. by an agent via
  `notes.delete` through MCP): the next autosave's `NotesUpdate` call fails
  with a not-found error from the backend; show "This note was deleted" and
  disable further edits to that editor instance rather than silently
  recreating the note.
- **Image over size/count cap:** rejected before insertion with a visible
  message, not a silent no-op.
- **Mermaid syntax error:** rendered inline as an error state within the
  block, editor stays otherwise fully functional.

## Testing / verification

No frontend test runner exists in this project (confirmed during the
Phase 5 Workbench UI work — still true). Backend Go changes get the
project's existing `go test ./...` coverage for the new `Store.Update` and
`notes.Service.Update` methods (unit tests following the same shape as
existing `store_test.go`/`notes_test.go` coverage for `Create`/
`UpdateMetadata`). Frontend verification is manual: run the app, create a
note, edit it (confirm autosave persists across a re-fetch), insert each
block type via slash-command, confirm a mermaid code block renders and the
Edit/Preview toggle works, confirm an oversized image is rejected with a
message.
