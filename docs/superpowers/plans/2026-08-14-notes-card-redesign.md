# Notes Card Redesign and Per-Note Color Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the note dashboard card as a "mini-window" (dark frame + inset colored panel) and give each note an optional own color, reusing the folder palette, with live inheritance from its folder.

**Architecture:** `note.color` is a new optional field in the note's plaintext metadata, validated with the same `vaultitem.ValidNoteFolderColor` used by folders. `notes.Service` computes `EffectiveColor` (own color, else live folder color, else none) the same way it already computes `EffectiveTags`. `SetOrganization` grows a fourth `color` parameter — the single write path for folder/tags/color, still `UpdateMetadata`-only (no body reseal). `DeleteFolder`'s existing tag copy-down gains a matching color copy-down. Frontend gets a new `NoteCard` component (dark outer frame, colored inset panel, 3:2 ratio, single-line truncated title) and a shared `ColorPicker` (extracted from the folder dialogs, reused in the note editor with an added "inherit" option).

**Tech Stack:** Go vaultitem/notes/bridge/MCP, Wails bindings, React + shadcn (`Popover`, `Button`) + lucide-react (`Palette`, `Ban`), Tailwind v4 CSS variables (`color-mix()` on the existing `--note-folder-*` tokens).

## Global Constraints

- Reuse the existing 9-color palette and `vaultitem.ValidNoteFolderColor` — do not add a second color system or new CSS tokens.
- `color` is optional on notes. Empty means "no own color" (inherit from folder, live). Never persist an invalid color string — reject the write.
- No gradients, no colored box-shadow/glow. The card's only color surface is the inset panel's `color-mix()` background/border.
- Card is a fixed `aspect-[3/2]`. No masonry, no variable row heights.
- Note title is always one line — Tailwind `truncate` (nowrap + ellipsis), never wraps.
- No dashboard structural change: sidebar (folder list + management), search, and the AND tag filter are unchanged. Only the note card's internals and the new color field are in scope.
- No dashboard-card inline color quick-set. Color is set from the note editor only (Popover + `ColorPicker`, mirrors the folder color picker already in `NotesDashboard.tsx`).
- No new capability grants needed — color flows through the existing `notes:write` / `note:<id>` scope on `SetOrganization`, same as folder/tags today.
- Spec: `docs/superpowers/specs/2026-08-14-notes-card-redesign-design.md`.
- `docs/superpowers/*` is gitignored; `git add -f` plan/spec files if `git status` does not stage them.

## File structure

- `internal/vaultitem/schema.go` — note metadata gains optional `color`, validated against `ValidNoteFolderColor`.
- `internal/notes/notes.go` — `metadata`/`Summary`/`Note` gain `Color`/`EffectiveColor`; `effectiveColor` helper; `SetOrganization` gains a `color` parameter.
- `internal/notes/folders.go` — `DeleteFolder` copy-down also carries the folder's color onto notes that were inheriting it.
- `internal/bridge/notes.go` — `NotesSetOrganization` gains `color`.
- `internal/mcp/server.go`, `internal/mcp/tools_notes.go`, `internal/mcp/server_test.go` — `NotesAPI.SetOrganization` + `notes.set_organization` tool + `fakeNotes` grow the `color` parameter.
- `frontend/bindings/...` — regenerated (`wails3 generate bindings`).
- `frontend/src/bridge/notes.ts` — `NoteSummary`/`Note` gain `color`/`effectiveColor`; `setNoteOrganization` grows a `color` parameter.
- `frontend/src/modules/notes/ColorPicker.tsx` — new, extracted from `NotesDashboard.tsx`'s inline `ColorPicker`, adds an `allowInherit` option.
- `frontend/src/modules/notes/noteColor.ts` — new, `notePanelStyle(color)` → inline `color-mix()` style built from `--note-folder-*`.
- `frontend/src/modules/notes/NoteCard.tsx` — new, the mini-window card (frame + inset panel, 3:2, truncated title).
- `frontend/src/modules/notes/NotesDashboard.tsx` — drops the inline `ColorPicker` and the inline card `<button>` markup, uses the new `ColorPicker` and `NoteCard`.
- `frontend/src/modules/notes/NoteEditor.tsx` — adds a color `Popover` next to the folder `Select`; `persistOrganization` grows a `color` parameter.

---

### Task 1: Vault-item schema — note `color` field

**Files:**
- Modify: `internal/vaultitem/schema.go`
- Test: `internal/vaultitem/schema_test.go`

**Interfaces:**
- Consumes: existing `ValidNoteFolderColor`
- Produces: notes may include an optional `color`; invalid non-empty values are rejected

- [ ] **Step 1: Write the failing tests**

In `internal/vaultitem/schema_test.go`, add to the valid-records table (`TestValidateRecordSchemaAcceptsSupportedModuleRecords`, after the `"note with org fields"` row at line 20):

```go
		{name: "note with color", itemType: ItemTypeNote, metadata: `{"title":"Deploy notes","color":"blue"}`, keyID: "note-body"},
```

Add to the invalid-records table (`TestValidateRecordSchemaRejectsInvalidModuleRecords`, after the `"missing note title"` row at line 46):

```go
		{name: "note unknown color", itemType: ItemTypeNote, metadata: `{"title":"x","color":"neon"}`, payload: validSchemaEnvelope("note-body")},
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vaultitem/ -run 'TestValidateRecordSchema' -count=1`

Expected: FAIL on the `TestValidateRecordSchemaAcceptsSupportedModuleRecords/note_with_color` subtest. The note struct has no `Color` field yet, so `decodeMetadataObject`'s `DisallowUnknownFields` rejects the `color` key as unknown, and the valid-case test (which expects no error) fails. The `note unknown color` row in the invalid-records table already passes today (rejected, just for the "unknown field" reason instead of "invalid color") — that's fine, it stays correct after Step 3 too.

- [ ] **Step 3: Implement schema**

In `internal/vaultitem/schema.go`, replace the `ItemTypeNote` case (lines 48–57):

```go
	case ItemTypeNote:
		var value struct {
			Title    string   `json:"title"`
			FolderID string   `json:"folder_id"`
			Tags     []string `json:"tags"`
			Color    string   `json:"color"`
		}
		if decodeMetadataObject(metadata, &value) != nil || value.Title == "" ||
			(value.Color != "" && !ValidNoteFolderColor(value.Color)) {
			return ErrInvalidRecordSchema
		}
		expectedKeyID = "note-body"
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vaultitem/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vaultitem/schema.go internal/vaultitem/schema_test.go
git commit -m "feat(vaultitem): accept an optional color on note records"
```

---

### Task 2: `notes.Service` — own/effective color and `SetOrganization`

**Files:**
- Modify: `internal/notes/notes.go`
- Test: `internal/notes/notes_test.go`

**Interfaces:**
- Consumes: `vaultitem.ValidNoteFolderColor`, `loadFolderMap`
- Produces:
  - `Summary`/`Note` gain `Color string` (own) and `EffectiveColor string` (computed)
  - `SetOrganization(id, folderID string, tags []string, color string) (Summary, error)` — grows a fourth parameter; `color == ""` clears the note's own color (falls back to live folder color); non-empty invalid color → error `"invalid note color"`

- [ ] **Step 1: Write the failing tests**

In `internal/notes/notes_test.go`, change the existing `SetOrganization` call inside `TestSetOrganizationAndLiveInheritance` (line 207) from:

```go
	if _, err := s.SetOrganization(summary.ID, "", []string{"keep"}); err != nil {
```

to:

```go
	if _, err := s.SetOrganization(summary.ID, "", []string{"keep"}, ""); err != nil {
```

Append these new tests to the same file:

```go
func TestEffectiveColorFromFolder(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	summary, err := s.Create("T", "b", folder.ID, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if summary.Color != "" {
		t.Fatalf("Color = %q, want empty", summary.Color)
	}
	if summary.EffectiveColor != "blue" {
		t.Fatalf("EffectiveColor = %q, want blue", summary.EffectiveColor)
	}
}

func TestSetOrganizationOwnColorOverridesFolder(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	summary, _ := s.Create("T", "b", folder.ID, nil)
	updated, err := s.SetOrganization(summary.ID, folder.ID, nil, "red")
	if err != nil {
		t.Fatalf("SetOrganization: %v", err)
	}
	if updated.Color != "red" || updated.EffectiveColor != "red" {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestSetOrganizationRejectsInvalidColor(t *testing.T) {
	s := newService(t)
	summary, _ := s.Create("T", "b", "", nil)
	if _, err := s.SetOrganization(summary.ID, "", nil, "neon"); err == nil {
		t.Fatal("expected error for invalid color")
	}
}

func TestSetOrganizationClearingColorRevertsToFolder(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	summary, _ := s.Create("T", "b", folder.ID, nil)
	if _, err := s.SetOrganization(summary.ID, folder.ID, nil, "red"); err != nil {
		t.Fatalf("SetOrganization: %v", err)
	}
	cleared, err := s.SetOrganization(summary.ID, folder.ID, nil, "")
	if err != nil {
		t.Fatalf("SetOrganization: %v", err)
	}
	if cleared.Color != "" || cleared.EffectiveColor != "blue" {
		t.Fatalf("cleared = %+v", cleared)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/notes/ -count=1`

Expected: FAIL — compile error (`SetOrganization` called with 4 args, defined with 3) and `summary.Color`/`summary.EffectiveColor` undefined.

- [ ] **Step 3: Implement**

In `internal/notes/notes.go`, widen the `metadata` struct (lines 31–35):

```go
type metadata struct {
	Title    string   `json:"title"`
	FolderID string   `json:"folder_id,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Color    string   `json:"color,omitempty"`
}
```

Widen `Summary` (lines 38–47):

```go
type Summary struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Preview        string    `json:"preview"`
	FolderID       string    `json:"folder_id"`
	Tags           []string  `json:"tags"`
	EffectiveTags  []string  `json:"effective_tags"`
	Color          string    `json:"color"`
	EffectiveColor string    `json:"effective_color"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
```

Widen `Note` (lines 50–59):

```go
type Note struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	FolderID       string    `json:"folder_id"`
	Tags           []string  `json:"tags"`
	EffectiveTags  []string  `json:"effective_tags"`
	Color          string    `json:"color"`
	EffectiveColor string    `json:"effective_color"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
```

Replace `SetOrganization` (lines 169–203):

```go
func (s *Service) SetOrganization(id, folderID string, tags []string, color string) (Summary, error) {
	normalized, err := NormalizeTags(tags)
	if err != nil {
		return Summary{}, err
	}
	if color != "" && !vaultitem.ValidNoteFolderColor(color) {
		return Summary{}, errors.New("invalid note color")
	}
	if folderID != "" {
		record, err := s.store.Get(folderID)
		if err != nil || record.Type != vaultitem.ItemTypeNoteFolder {
			return Summary{}, errors.New("folder not found")
		}
	}
	record, err := s.store.Get(id)
	if err != nil {
		return Summary{}, err
	}
	meta, err := decodeMetadata(record)
	if err != nil {
		return Summary{}, err
	}
	meta.FolderID = folderID
	meta.Tags = normalized
	meta.Color = color
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return Summary{}, err
	}
	record, err = s.store.UpdateMetadata(id, metaBytes)
	if err != nil {
		return Summary{}, err
	}
	folderMap, err := s.loadFolderMap()
	if err != nil {
		return Summary{}, err
	}
	return s.toSummary(record, folderMap)
}
```

Add an `effectiveColor` helper next to `effectiveTags` (after line 296):

```go
func effectiveColor(meta metadata, folderMap map[string]Folder) string {
	if meta.Color != "" {
		return meta.Color
	}
	if meta.FolderID == "" {
		return ""
	}
	folder, ok := folderMap[meta.FolderID]
	if !ok {
		return ""
	}
	return folder.Color
}
```

Update `summaryFrom` (lines 315–329) to set the new fields:

```go
func (s *Service) summaryFrom(record vaultitem.Record, meta metadata, folderMap map[string]Folder) Summary {
	summary := Summary{
		ID:             record.ID,
		Title:          meta.Title,
		FolderID:       meta.FolderID,
		Tags:           nonNilTags(meta.Tags),
		EffectiveTags:  effectiveTags(meta, folderMap),
		Color:          meta.Color,
		EffectiveColor: effectiveColor(meta, folderMap),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
	if plain, err := s.store.OpenPayload(record, bodyKeyID); err == nil {
		summary.Preview = previewFromBody(string(plain))
	}
	return summary
}
```

Update `noteFrom` (lines 331–342):

```go
func noteFrom(record vaultitem.Record, meta metadata, body string, folderMap map[string]Folder) Note {
	return Note{
		ID:             record.ID,
		Title:          meta.Title,
		Body:           body,
		FolderID:       meta.FolderID,
		Tags:           nonNilTags(meta.Tags),
		EffectiveTags:  effectiveTags(meta, folderMap),
		Color:          meta.Color,
		EffectiveColor: effectiveColor(meta, folderMap),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/notes/ -count=1`

Expected: PASS. This will also break `internal/notes/folders.go` (calls the old 3-arg `SetOrganization`) and every downstream caller — that's Task 3 onward; `go build ./...` is not expected to pass again until Task 5.

- [ ] **Step 5: Commit**

```bash
git add internal/notes/notes.go internal/notes/notes_test.go
git commit -m "feat(notes): add own/effective color to notes"
```

---

### Task 3: `DeleteFolder` copy-down carries color

**Files:**
- Modify: `internal/notes/folders.go`
- Test: `internal/notes/notes_test.go`

**Interfaces:**
- Consumes: `SetOrganization` (Task 2's 4-arg form)
- Produces: a note that was inheriting its folder's color keeps that color (now as its own) after the folder is deleted; a note with an already-explicit own color is untouched

- [ ] **Step 1: Write the failing tests**

Append to `internal/notes/notes_test.go` (near `TestDeleteFolderCopyDown`):

```go
func TestDeleteFolderCopiesColorWhenInheriting(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	summary, _ := s.Create("T", "b", folder.ID, nil)
	if err := s.DeleteFolder(folder.ID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	got, _ := s.Get(summary.ID)
	if got.Color != "blue" {
		t.Fatalf("Color = %q, want blue", got.Color)
	}
	if got.EffectiveColor != "blue" {
		t.Fatalf("EffectiveColor = %q, want blue", got.EffectiveColor)
	}
}

func TestDeleteFolderKeepsExplicitNoteColor(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	summary, _ := s.Create("T", "b", folder.ID, nil)
	if _, err := s.SetOrganization(summary.ID, folder.ID, nil, "red"); err != nil {
		t.Fatalf("SetOrganization: %v", err)
	}
	if err := s.DeleteFolder(folder.ID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	got, _ := s.Get(summary.ID)
	if got.Color != "red" {
		t.Fatalf("Color = %q, want red (untouched)", got.Color)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/notes/ -run DeleteFolder -count=1`

Expected: FAIL — compile error, `DeleteFolder` still calls the old 3-arg `SetOrganization`.

- [ ] **Step 3: Implement**

In `internal/notes/folders.go`, replace the copy-down loop inside `DeleteFolder` (lines 142–149):

```go
	for _, note := range notes {
		if note.FolderID != id {
			continue
		}
		color := note.Color
		if color == "" {
			color = folder.Color
		}
		if _, err := s.SetOrganization(note.ID, "", UnionTags(note.Tags, folder.Tags), color); err != nil {
			return err
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/notes/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/notes/folders.go internal/notes/notes_test.go
git commit -m "feat(notes): copy folder color onto notes on folder delete"
```

---

### Task 4: Bridge — `NotesSetOrganization` grows `color`

**Files:**
- Modify: `internal/bridge/notes.go`
- Test: `internal/bridge/notes_test.go`

**Interfaces:**
- Consumes: `notes.Service.SetOrganization` (Task 2's 4-arg form)
- Produces: `NotesSetOrganization(id, folderID string, tags []string, color string) (notes.Summary, error)`

- [ ] **Step 1: Write the failing test**

In `internal/bridge/notes_test.go`, replace the `NotesSetOrganization` call inside `TestNotesBridgeFoldersAndOrganization` (lines 103–105):

```go
	if _, err := service.NotesSetOrganization(summary.ID, "", []string{"solo"}); err != nil {
		t.Fatalf("NotesSetOrganization: %v", err)
	}
```

with:

```go
	organized, err := service.NotesSetOrganization(summary.ID, "", []string{"solo"}, "red")
	if err != nil {
		t.Fatalf("NotesSetOrganization: %v", err)
	}
	if organized.Color != "red" {
		t.Fatalf("Color = %q, want red", organized.Color)
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bridge/ -run Notes -count=1`

Expected: FAIL — compile error, `NotesSetOrganization` still takes 3 args.

- [ ] **Step 3: Implement**

In `internal/bridge/notes.go`, replace `NotesSetOrganization` (lines 45–57):

```go
func (s *AppService) NotesSetOrganization(id, folderID string, tags []string, color string) (notes.Summary, error) {
	opaqueID := recordID(id)
	result, err := s.call(context.Background(), "builtin.notes", "notes:write", "note:"+opaqueID, map[string]any{
		"action":  "set_organization",
		"note_id": opaqueID,
	}, func(context.Context) (any, error) {
		return s.container.Notes.SetOrganization(id, folderID, tags, color)
	})
	if err != nil {
		return notes.Summary{}, err
	}
	return result.(notes.Summary), nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/bridge/ -count=1`

Expected: FAIL only on `internal/mcp` compile (Task 5) if run together; `internal/bridge` and `internal/notes` PASS. Run `go build ./internal/bridge/...` to confirm the bridge package itself compiles.

- [ ] **Step 5: Commit**

```bash
git add internal/bridge/notes.go internal/bridge/notes_test.go
git commit -m "feat(bridge): pass note color through SetOrganization"
```

---

### Task 5: MCP — `notes.set_organization` grows `color`

**Files:**
- Modify: `internal/mcp/server.go`
- Modify: `internal/mcp/tools_notes.go`
- Modify: `internal/mcp/server_test.go`

**Interfaces:**
- Consumes: `notes.Service.SetOrganization` (Task 2's 4-arg form)
- Produces: `notes.set_organization` input gains optional `color`; `NotesAPI.SetOrganization` signature matches

- [ ] **Step 1: Write the failing test**

In `internal/mcp/server_test.go`, replace the `fakeNotes.SetOrganization` method (lines 60–62):

```go
func (f *fakeNotes) SetOrganization(string, string, []string, string) (notes.Summary, error) {
	return notes.Summary{ID: "org"}, nil
}
```

In `TestNotesSetOrganizationTool` (lines 253–255), add `Color`:

```go
	_, _, err := srv.toolNotesSetOrganization(context.Background(), &sdk.CallToolRequest{}, notesSetOrganizationInput{
		ID: id, FolderID: "", Tags: []string{"solo"}, Color: "red",
	})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run Notes -count=1`

Expected: FAIL — compile error, `notesSetOrganizationInput` has no `Color` field and `SetOrganization` signatures mismatch.

- [ ] **Step 3: Implement**

In `internal/mcp/server.go`, update the `NotesAPI` interface (line 38):

```go
	SetOrganization(id, folderID string, tags []string, color string) (notes.Summary, error)
```

In `internal/mcp/tools_notes.go`, replace `notesSetOrganizationInput` and `toolNotesSetOrganization` (lines 188–205):

```go
type notesSetOrganizationInput struct {
	ID       string   `json:"id"`
	FolderID string   `json:"folder_id"`
	Tags     []string `json:"tags"`
	Color    string   `json:"color"`
}

func (s *Server) toolNotesSetOrganization(ctx context.Context, _ *sdk.CallToolRequest, in notesSetOrganizationInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ID)
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:write", "note:"+opaqueID, meta("set_organization", map[string]any{
		"note_id": opaqueID,
	}), func(context.Context) (any, error) {
		return s.deps.Notes.SetOrganization(in.ID, in.FolderID, in.Tags, in.Color)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/vaultitem/ ./internal/notes/ ./internal/bridge/ -count=1 && go test ./internal/mcp/ -run Notes -count=1`

Expected: PASS. Then confirm the whole module still compiles:

Run: `go build ./...`

Expected: succeeds (ignore the pre-existing unrelated `build/ios` main-package warning if present; `devkit` itself must link).

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools_notes.go internal/mcp/server_test.go
git commit -m "feat(mcp): pass note color through notes.set_organization"
```

---

### Task 6: Frontend bindings and bridge wrapper

**Files:**
- Modify: `frontend/bindings/devkit/internal/bridge/appservice.ts` (generated)
- Modify: `frontend/bindings/devkit/internal/notes/models.ts` (generated)
- Modify: `frontend/src/bridge/notes.ts`
- Modify: `frontend/src/modules/notes/NoteEditor.tsx` (compile fix only — the color picker UI itself is Task 10)

**Interfaces:**
- Consumes: the new `AppService.NotesSetOrganization` 4-arg signature and `color`/`effective_color` on `Summary`/`Note`
- Produces: `NoteSummary`/`Note` gain `color: string`, `effectiveColor: string`; `setNoteOrganization(id, folderId, tags, color)`

- [ ] **Step 1: Regenerate Wails bindings**

Run from `dev-kit-app`:

```bash
wails3 generate bindings
```

Expected: `frontend/bindings/devkit/internal/notes/models.ts`'s `Summary` and `Note` interfaces gain `"color": string;` and `"effective_color": string;`; `frontend/bindings/devkit/internal/bridge/appservice.ts`'s `NotesSetOrganization` gains a fourth `color: string` parameter.

- [ ] **Step 2: Update `frontend/src/bridge/notes.ts`**

Replace the `NoteSummary` and `Note` types:

```ts
export type NoteSummary = {
  id: string;
  title: string;
  folderId: string;
  tags: string[];
  effectiveTags: string[];
  color: string;
  effectiveColor: string;
  preview: string;
  updatedAt: string;
};

export type Note = {
  id: string;
  title: string;
  body: string;
  folderId: string;
  tags: string[];
  effectiveTags: string[];
  color: string;
  effectiveColor: string;
};
```

Replace `mapSummary`:

```ts
function mapSummary(n: {
  id: string;
  title: string;
  folder_id?: string;
  tags?: string[] | null;
  effective_tags?: string[] | null;
  color?: string;
  effective_color?: string;
  preview?: string;
  updated_at: string;
}): NoteSummary {
  return {
    id: n.id,
    title: n.title,
    folderId: n.folder_id ?? "",
    tags: asStrings(n.tags),
    effectiveTags: asStrings(n.effective_tags),
    color: n.color ?? "",
    effectiveColor: n.effective_color ?? "",
    preview: n.preview ?? "",
    updatedAt: n.updated_at,
  };
}
```

Replace `mapNote`:

```ts
function mapNote(n: {
  id: string;
  title: string;
  body: string;
  folder_id?: string;
  tags?: string[] | null;
  effective_tags?: string[] | null;
  color?: string;
  effective_color?: string;
}): Note {
  return {
    id: n.id,
    title: n.title,
    body: n.body,
    folderId: n.folder_id ?? "",
    tags: asStrings(n.tags),
    effectiveTags: asStrings(n.effective_tags),
    color: n.color ?? "",
    effectiveColor: n.effective_color ?? "",
  };
}
```

Replace `setNoteOrganization`:

```ts
export function setNoteOrganization(
  id: string,
  folderId: string,
  tags: string[],
  color: string,
): Promise<NoteSummary> {
  return callBridge(async () => {
    const n = await AppService.NotesSetOrganization(id, folderId, tags, color);
    return mapSummary(n);
  });
}
```

- [ ] **Step 3: Fix compile of the existing editor call site**

In `frontend/src/modules/notes/NoteEditor.tsx`, `persistOrganization` (inside its body) calls `setNoteOrganization(note.id, nextFolderId, nextTags);` — change to pass the note's current own color so behavior is unchanged until Task 10 adds real color-picker state:

```ts
      const summary = await setNoteOrganization(note.id, nextFolderId, nextTags, note.color);
```

- [ ] **Step 4: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/bindings frontend/src/bridge/notes.ts frontend/src/modules/notes/NoteEditor.tsx
git commit -m "feat(notes): regenerate bindings and wrap note color"
```

---

### Task 7: Shared `ColorPicker` with an "inherit" option

**Files:**
- Create: `frontend/src/modules/notes/ColorPicker.tsx`
- Modify: `frontend/src/modules/notes/NotesDashboard.tsx`

**Interfaces:**
- Consumes: `FOLDER_COLORS`, `folderDotVar` from `./folderColors`
- Produces: `ColorPicker({ value, onChange, allowInherit? })` — `onChange` receives `FolderColor | ""`; when `allowInherit` is set, an extra swatch (a `Ban` icon) calls `onChange("")`

- [ ] **Step 1: Create the shared component**

Create `frontend/src/modules/notes/ColorPicker.tsx`:

```tsx
import { Ban } from "lucide-react";
import { cn } from "@/lib/utils";
import { FOLDER_COLORS, folderDotVar, type FolderColor } from "./folderColors";

export function ColorPicker({
  value,
  onChange,
  allowInherit,
}: {
  value: string;
  onChange: (color: FolderColor | "") => void;
  allowInherit?: boolean;
}) {
  return (
    <div className="flex flex-wrap gap-2">
      {allowInherit ? (
        <button
          type="button"
          aria-label="Inherit from folder"
          aria-pressed={value === ""}
          className={cn(
            "flex size-6 items-center justify-center rounded-full border border-border bg-muted text-muted-foreground ring-offset-background transition-shadow",
            value === "" && "ring-2 ring-ring ring-offset-2",
          )}
          onClick={() => onChange("")}
        >
          <Ban className="size-3.5" />
        </button>
      ) : null}
      {FOLDER_COLORS.map((color) => (
        <button
          key={color}
          type="button"
          aria-label={color}
          aria-pressed={value === color}
          className={cn(
            "size-6 rounded-full ring-offset-background transition-shadow",
            value === color && "ring-2 ring-ring ring-offset-2",
          )}
          style={{ background: folderDotVar(color) }}
          onClick={() => onChange(color)}
        />
      ))}
    </div>
  );
}
```

- [ ] **Step 2: Use it from the dashboard**

In `frontend/src/modules/notes/NotesDashboard.tsx`, delete the inline `ColorPicker` function (lines 125–150) and add the import alongside the other local module imports:

```ts
import { ColorPicker } from "./ColorPicker";
```

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/notes/ColorPicker.tsx frontend/src/modules/notes/NotesDashboard.tsx
git commit -m "refactor(notes): extract shared ColorPicker with an inherit option"
```

---

### Task 8: `NoteCard` — the mini-window card

**Files:**
- Create: `frontend/src/modules/notes/noteColor.ts`
- Create: `frontend/src/modules/notes/NoteCard.tsx`

**Interfaces:**
- Consumes: `NoteSummary` (from `../../bridge/notes`), `FOLDER_COLORS` (from `./folderColors`)
- Produces: `notePanelStyle(color): CSSProperties`; `NoteCard({ note, onOpen, onTagClick })` — dark outer frame, inset `color-mix()` panel, `aspect-[3/2]`, single-line truncated title

- [ ] **Step 1: Color helper**

Create `frontend/src/modules/notes/noteColor.ts`:

```ts
import type { CSSProperties } from "react";
import { FOLDER_COLORS, type FolderColor } from "./folderColors";

export function notePanelStyle(color: string): CSSProperties {
  if (!FOLDER_COLORS.includes(color as FolderColor)) {
    return {};
  }
  const base = `var(--note-folder-${color})`;
  return {
    background: `color-mix(in oklch, ${base} 30%, transparent)`,
    borderColor: `color-mix(in oklch, ${base} 45%, transparent)`,
  };
}
```

- [ ] **Step 2: Card component**

Create `frontend/src/modules/notes/NoteCard.tsx`:

```tsx
import { cn } from "@/lib/utils";
import type { NoteSummary } from "../../bridge/notes";
import { notePanelStyle } from "./noteColor";

function relativeTime(iso: string): string {
  const diffMs = Date.now() - new Date(iso).getTime();
  const minute = 60_000;
  const hour = 60 * minute;
  const day = 24 * hour;
  if (diffMs < hour) return `${Math.max(1, Math.round(diffMs / minute))}m`;
  if (diffMs < day) return `${Math.round(diffMs / hour)}h`;
  return `${Math.round(diffMs / day)}d`;
}

export function NoteCard({
  note,
  onOpen,
  onTagClick,
}: {
  note: NoteSummary;
  onOpen: (id: string) => void;
  onTagClick: (tag: string) => void;
}) {
  const hasColor = note.effectiveColor !== "";
  return (
    <button
      type="button"
      onClick={() => onOpen(note.id)}
      className="flex aspect-[3/2] flex-col rounded-2xl border border-border bg-card p-[5px] text-left"
    >
      <span className="truncate px-1.5 pb-1.5 pt-1 text-[13px] font-semibold text-foreground">
        {note.title || "Untitled"}
      </span>
      <span
        className={cn(
          "flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border p-2.5",
          hasColor ? "" : "border-transparent bg-muted",
        )}
        style={hasColor ? notePanelStyle(note.effectiveColor) : undefined}
      >
        <span className="line-clamp-3 flex-1 text-[10px] leading-relaxed text-foreground/90">
          {note.preview || "No preview"}
        </span>
        <span className="mt-1.5 flex items-center justify-between gap-2">
          <span className="flex flex-wrap gap-2">
            {note.effectiveTags.map((tag) => (
              <span
                key={tag.toLowerCase()}
                role="button"
                tabIndex={0}
                className="flex items-center gap-1 text-[9px] font-medium text-foreground/80"
                onClick={(e) => {
                  e.stopPropagation();
                  onTagClick(tag);
                }}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    e.stopPropagation();
                    onTagClick(tag);
                  }
                }}
              >
                <span className="size-[5px] shrink-0 rounded-full bg-current" aria-hidden />
                {tag}
              </span>
            ))}
          </span>
          <span className="shrink-0 text-[8.5px] text-foreground/60">
            {relativeTime(note.updatedAt)}
          </span>
        </span>
      </span>
    </button>
  );
}
```

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS (the file isn't used yet — Task 9 wires it in)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/notes/noteColor.ts frontend/src/modules/notes/NoteCard.tsx
git commit -m "feat(notes): add the mini-window NoteCard component"
```

---

### Task 9: Wire `NoteCard` into the dashboard grid

**Files:**
- Modify: `frontend/src/modules/notes/NotesDashboard.tsx`

**Interfaces:**
- Consumes: `NoteCard` (Task 8)
- Produces: dashboard grid renders fixed 3:2 `NoteCard`s instead of the free-height inline card markup

- [ ] **Step 1: Replace the card grid**

In `frontend/src/modules/notes/NotesDashboard.tsx`, add the import:

```ts
import { NoteCard } from "./NoteCard";
```

Replace the grid `<div>` inside the `<ScrollArea>` (the block rendering `filtered.map((note) => (<button ...>...))`, originally lines 511–548):

```tsx
          <div className="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-3.5 p-3">
            {filtered.map((note) => (
              <NoteCard
                key={note.id}
                note={note}
                onOpen={onOpenNote}
                onTagClick={toggleTagFilter}
              />
            ))}
            {filtered.length === 0 ? (
              <p className="col-span-full px-1 py-6 text-sm text-muted-foreground">
                No notes match this view.
              </p>
            ) : null}
          </div>
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 3: Manual check**

Run the app, open Notes: cards render at a fixed 3:2 ratio with a dark frame and a colored inset panel for notes with an effective color; notes with no folder and no own color show a plain neutral (`bg-muted`) panel; a long title truncates with an ellipsis instead of wrapping; clicking a tag chip on a card still adds it to the AND filter.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/notes/NotesDashboard.tsx
git commit -m "feat(notes): render the dashboard grid with NoteCard"
```

---

### Task 10: Editor color picker

**Files:**
- Modify: `frontend/src/modules/notes/NoteEditor.tsx`

**Interfaces:**
- Consumes: `ColorPicker` (Task 7), `Popover`/`PopoverContent`/`PopoverTrigger` (`@/components/ui/popover`), `Palette` (lucide-react)
- Produces: a color swatch button next to the folder `Select` that opens a `Popover` with the shared `ColorPicker` (`allowInherit`); selecting a color calls `SetOrganization` immediately, same as folder/tag changes

- [ ] **Step 1: Imports and state**

Add to the import block at the top of `frontend/src/modules/notes/NoteEditor.tsx`:

```ts
import { Palette } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ColorPicker } from "./ColorPicker";
```

Add a `color` state next to `ownTags` (in the state declarations near the top of the component):

```ts
  const [color, setColor] = useState("");
```

In the note-loading effect, set it alongside `setOwnTags(n.tags);`:

```ts
        setOwnTags(n.tags);
        setColor(n.color);
```

- [ ] **Step 2: Thread color through `persistOrganization`**

Replace `persistOrganization`:

```ts
  async function persistOrganization(nextFolderId: string, nextTags: string[], nextColor: string) {
    if (!note || orgInFlight.current) {
      return;
    }
    orgInFlight.current = true;
    try {
      const summary = await setNoteOrganization(note.id, nextFolderId, nextTags, nextColor);
      const refreshed = {
        ...noteRef.current!,
        folderId: summary.folderId,
        tags: summary.tags,
        effectiveTags: summary.effectiveTags,
        color: summary.color,
        effectiveColor: summary.effectiveColor,
      };
      noteRef.current = refreshed;
      setNote(refreshed);
      setFolderId(summary.folderId);
      setOwnTags(summary.tags);
      setColor(summary.color);
      onSaved();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      orgInFlight.current = false;
    }
  }
```

Update its three existing call sites to pass `color`:

- In `tryAddTag`, `void persistOrganization(folderId, next);` → `void persistOrganization(folderId, next, color);`
- In `removeOwnTag`, `void persistOrganization(folderId, next);` → `void persistOrganization(folderId, next, color);`
- In `onFolderChange`, `void persistOrganization(nextFolderId, ownTags);` → `void persistOrganization(nextFolderId, ownTags, color);`

Add a new handler right after `onFolderChange`:

```ts
  function onColorChange(nextColor: string) {
    setColor(nextColor);
    void persistOrganization(folderId, ownTags, nextColor);
  }
```

- [ ] **Step 3: Add the color popover to the header**

In the JSX, insert this immediately after the folder `<Select>...</Select>` block and before the tag `<Input aria-label="Add tag" ...>`:

```tsx
        <Popover>
          <PopoverTrigger asChild>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="h-8 gap-1.5 px-2"
              aria-label="Note color"
            >
              <Palette className="size-3.5" />
              <span
                className="size-3 shrink-0 rounded-full border border-border"
                style={
                  note.effectiveColor
                    ? { background: folderDotVar(note.effectiveColor) }
                    : undefined
                }
                aria-hidden
              />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-auto">
            <ColorPicker value={color} onChange={onColorChange} allowInherit />
          </PopoverContent>
        </Popover>
```

- [ ] **Step 4: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 5: Manual check**

Open a note in a colored folder: swatch shows the folder's color, `ColorPicker` highlights "Inherit". Pick an explicit color: swatch and dashboard card follow it immediately, no need to wait for body autosave. Reopen the note: color persisted. Click "Inherit": swatch reverts to tracking the folder's color live.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/modules/notes/NoteEditor.tsx
git commit -m "feat(notes): add editor color picker with folder inheritance"
```

---

### Task 11: End-to-end verification

**Files:** none new

- [ ] **Step 1: Backend tests**

Run: `go test ./internal/vaultitem/ ./internal/notes/ ./internal/bridge/ -count=1`

Expected: PASS

- [ ] **Step 2: MCP notes tests**

Run: `go test ./internal/mcp/ -run Notes -count=1`

Expected: PASS

- [ ] **Step 3: Full build**

Run: `go build ./...`

Expected: succeeds

- [ ] **Step 4: Frontend typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 5: Manual script**

1. Create a note in a colored folder — card panel tinted with the folder's color, no own color set.
2. Set an explicit own color different from the folder's, from the editor — card follows the own color.
3. Clear the own color ("Inherit from folder") — card reverts to live-tracking the folder's color.
4. Delete that folder — a note with an explicit own color (step 2) is unaffected; a sibling note that was still inheriting keeps the same visual tint post-delete.
5. A very long title never wraps to a second line; it truncates with an ellipsis.
6. A note with no folder and no own color renders the neutral (`bg-muted`) panel.
7. Card grid is a uniform 3:2 layout, no masonry/uneven heights.
8. Sidebar folder management (rename/color/tags/delete) still works exactly as before — unchanged by this plan.

If anything fails, fix in a focused commit on that layer, then re-run the failing command.
