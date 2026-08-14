# Notes Folders, Tags, and Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add first-class note folders, freeform tags with live folder inheritance, a dashboard → full-editor Notes UI, and a token-themed slash menu that shows real editor hotkeys.

**Architecture:** Folders are `vaultitem` records of type `note-folder` (plaintext name/color/tags, sealed empty `{}` payload). Notes gain `folder_id` and own `tags` in plaintext metadata; `notes.Service` joins in memory to compute `effective_tags` and a decrypted `preview`. `Update(title, body)` never touches organization; `SetOrganization` and folder writes use `UpdateMetadata`. The frontend drops master-detail for a dashboard grid plus the existing Plate editor.

**Tech Stack:** Go vaultitem/notes/bridge/MCP, Wails bindings, React + Plate + shadcn (`Dialog`, `DropdownMenu`, `Badge`, `Kbd`, `AlertDialog`), Tailwind v4 CSS variables.

## Global Constraints

- Note body stays an encrypted markdown `string`. Do not change SealV2 / AAD / conflict-merge.
- Folder type constant is `note-folder`; payload key id is `note-folder`; sealed payload is `{}`.
- Folder colors (exact strings): `red`, `orange`, `yellow`, `green`, `teal`, `blue`, `violet`, `pink`, `gray`.
- Tag write rules: trim, drop empty, dedupe case-insensitive (first spelling wins), max 32 tags, max 40 chars each — reject, do not clip.
- Effective tags = unique(note.own ∪ folder.tags); own tags first. Inheritance is live. Copy-down **only** on `DeleteFolder`.
- `Update(id, title, body)` read-modify-writes metadata so title/body autosave cannot wipe `folder_id`/`tags`.
- UI classes use theme tokens / CSS variables only — no hardcoded dark hex (`#0d0d0d`, `bg-zinc-950`, etc.).
- One-level folders. No DnD, nested trees, tag admin, SQL indexes, light-mode theme, or extra slash items (H4/image/mermaid/AI).
- Spec: `docs/superpowers/specs/2026-08-14-notes-folders-ui-design.md`.
- `docs/superpowers/*` is gitignored; `git add -f` plan/spec files if git status does not stage them.

## File structure

- `internal/vaultitem/schema.go` — `ItemTypeNoteFolder`, note metadata fields, folder schema.
- `internal/notes/tags.go` — `NormalizeTags`, `UnionTags` (folder colors: `vaultitem.ValidNoteFolderColor`).
- `internal/notes/folders.go` — `Folder` type + folder CRUD + `DeleteFolder` copy-down.
- `internal/notes/notes.go` — widened `Store`, `Summary`/`Note`, `Create`/`Update`/`SetOrganization`/`List`/`Get`/`Search`.
- `internal/app/container.go` — `note-folder:*` grants.
- `internal/bridge/notes.go` — folder methods + `NotesSetOrganization` + `NotesCreate` extra args.
- `internal/mcp/server.go`, `internal/mcp/tools_notes.go`, `internal/mcp/server_test.go` — MCP surface + `fakeNotes`.
- `frontend/src/bridge/notes.ts` — TS types and wrappers.
- `frontend/src/index.css` — `--note-folder-*`, `--note-tag-*`.
- `frontend/src/modules/notes/tagColor.ts` — hash tag name → CSS variable.
- `frontend/src/modules/notes/NotesDashboard.tsx` — sidebar + grid + filters.
- `frontend/src/modules/notes/NotesView.tsx` — dashboard vs editor.
- `frontend/src/modules/notes/NoteEditor.tsx` — breadcrumb, folder picker, own/inherited tags.
- `frontend/src/modules/notes/plate/SlashInput.tsx` — `Kbd` hotkeys.
- `frontend/src/components/ui/inline-combobox.tsx` — popover border/tokens; item shortcut slot.
- Delete `frontend/src/modules/notes/NoteList.tsx` once the dashboard replaces it.

---

### Task 1: Vault-item schema for folders and note org fields

**Files:**
- Modify: `internal/vaultitem/schema.go`
- Test: `internal/vaultitem/schema_test.go`

**Interfaces:**
- Consumes: existing `ValidateRecordSchema` / `IsSupportedItemType`
- Produces: `ItemTypeNoteFolder = "note-folder"`; notes may include `folder_id` + `tags`; folders require `name`, palette `color`, optional `tags`, payload key `note-folder`

- [ ] **Step 1: Write the failing tests**

Add cases to the existing table tests in `internal/vaultitem/schema_test.go`:

```go
{name: "note with org fields", itemType: ItemTypeNote, metadata: `{"title":"Deploy notes","folder_id":"aabbccddeeff001122334455","tags":["ops"]}`, keyID: "note-body"},
{name: "note-folder", itemType: ItemTypeNoteFolder, metadata: `{"name":"Work","color":"blue","tags":["work"]}`, keyID: "note-folder"},
```

In `TestValidateRecordSchemaRejectsInvalidModuleRecords`:

```go
{name: "note-folder missing name", itemType: ItemTypeNoteFolder, metadata: `{"color":"blue"}`, payload: validSchemaEnvelope("note-folder")},
{name: "note-folder unknown color", itemType: ItemTypeNoteFolder, metadata: `{"name":"Work","color":"neon"}`, payload: validSchemaEnvelope("note-folder")},
{name: "note-folder unknown field", itemType: ItemTypeNoteFolder, metadata: `{"name":"Work","color":"blue","icon":"x"}`, payload: validSchemaEnvelope("note-folder")},
{name: "note-folder wrong payload key", itemType: ItemTypeNoteFolder, metadata: `{"name":"Work","color":"blue"}`, payload: validSchemaEnvelope("note-body")},
{name: "note-folder missing payload", itemType: ItemTypeNoteFolder, metadata: `{"name":"Work","color":"blue"}`},
```

Keep the existing `"note"` row with only `{"title":"Deploy notes"}` — old records must still validate.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vaultitem/ -run 'TestValidateRecordSchema' -count=1`

Expected: FAIL — `ItemTypeNoteFolder` undefined and/or unknown JSON fields `folder_id`/`tags` rejected on notes.

- [ ] **Step 3: Implement schema**

In `internal/vaultitem/schema.go`:

```go
const (
	ItemTypeNote       = "note"
	ItemTypeNoteFolder = "note-folder"
	ItemTypeDatabase   = "db-profile"
	ItemTypeSSH        = "ssh-profile"
	ItemTypeKafka      = "kafka-profile"
)

func IsSupportedItemType(itemType string) bool {
	switch itemType {
	case ItemTypeNote, ItemTypeNoteFolder, ItemTypeDatabase, ItemTypeSSH, ItemTypeKafka:
		return true
	default:
		return false
	}
}
```

Replace the `ItemTypeNote` branch of `ValidateRecordSchema` with:

```go
case ItemTypeNote:
	var value struct {
		Title    string   `json:"title"`
		FolderID string   `json:"folder_id"`
		Tags     []string `json:"tags"`
	}
	if decodeMetadataObject(metadata, &value) != nil || value.Title == "" {
		return ErrInvalidRecordSchema
	}
	expectedKeyID = "note-body"

case ItemTypeNoteFolder:
	var value struct {
		Name  string   `json:"name"`
		Color string   `json:"color"`
		Tags  []string `json:"tags"`
	}
	if decodeMetadataObject(metadata, &value) != nil || value.Name == "" || !ValidNoteFolderColor(value.Color) {
		return ErrInvalidRecordSchema
	}
	expectedKeyID = "note-folder"
```

Add:

```go
func ValidNoteFolderColor(color string) bool {
	switch color {
	case "red", "orange", "yellow", "green", "teal", "blue", "violet", "pink", "gray":
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vaultitem/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vaultitem/schema.go internal/vaultitem/schema_test.go
git commit -m "feat(vaultitem): accept note-folder records and note org metadata"
```

---

### Task 2: Tag normalization helpers

**Files:**
- Create: `internal/notes/tags.go`
- Test: `internal/notes/tags_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `NormalizeTags(tags []string) ([]string, error)` — trim, drop empty, case-insensitive dedupe (first spelling), error `"tag too long"` if any tag > 40 runes, error `"too many tags"` if > 32 after dedupe
  - `UnionTags(own, inherited []string) []string` — own first, then inherited, same uniqueness rule; never errors (inputs already normalized)
  - `ErrTagTooLong`, `ErrTooManyTags`

- [ ] **Step 1: Write the failing test**

Create `internal/notes/tags_test.go`:

```go
package notes

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	got, err := NormalizeTags([]string{" Work ", "work", "Q3", ""})
	if err != nil {
		t.Fatalf("NormalizeTags: %v", err)
	}
	if len(got) != 2 || got[0] != "Work" || got[1] != "Q3" {
		t.Fatalf("got %#v", got)
	}
}

func TestNormalizeTagsRejectsTooLong(t *testing.T) {
	_, err := NormalizeTags([]string{strings.Repeat("a", 41)})
	if !errors.Is(err, ErrTagTooLong) {
		t.Fatalf("err = %v, want ErrTagTooLong", err)
	}
}

func TestNormalizeTagsRejectsTooMany(t *testing.T) {
	in := make([]string, 33)
	for i := range in {
		in[i] = string(rune('a' + i))
	}
	_, err := NormalizeTags(in)
	if !errors.Is(err, ErrTooManyTags) {
		t.Fatalf("err = %v, want ErrTooManyTags", err)
	}
}

func TestUnionTagsOwnFirst(t *testing.T) {
	got := UnionTags([]string{"standup"}, []string{"Work", "standup"})
	if len(got) != 2 || got[0] != "standup" || got[1] != "Work" {
		t.Fatalf("got %#v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notes/ -run 'TestNormalizeTags|TestUnionTags' -count=1`

Expected: FAIL — `NormalizeTags` undefined

- [ ] **Step 3: Implement helpers**

Create `internal/notes/tags.go`:

```go
package notes

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	maxTags   = 32
	maxTagLen = 40
)

var (
	ErrTagTooLong  = errors.New("tag too long")
	ErrTooManyTags = errors.New("too many tags")
)

func NormalizeTags(tags []string) ([]string, error) {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > maxTagLen {
			return nil, ErrTagTooLong
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	if len(out) > maxTags {
		return nil, ErrTooManyTags
	}
	return out, nil
}

func UnionTags(own, inherited []string) []string {
	out := make([]string, 0, len(own)+len(inherited))
	seen := make(map[string]struct{}, len(own)+len(inherited))
	for _, group := range [][]string{own, inherited} {
		for _, tag := range group {
			key := strings.ToLower(tag)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, tag)
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/notes/ -count=1`

Expected: PASS (existing notes tests still pass)

- [ ] **Step 5: Commit**

```bash
git add internal/notes/tags.go internal/notes/tags_test.go
git commit -m "feat(notes): add tag normalize and union helpers"
```

---

### Task 3: Folder CRUD

**Files:**
- Create: `internal/notes/folders.go`
- Modify: `internal/notes/notes.go` (widen `Store` with `UpdateMetadata`)
- Test: `internal/notes/folders_test.go`

**Interfaces:**
- Consumes: `vaultitem.ItemTypeNoteFolder`, `vaultitem.ValidNoteFolderColor`, `NormalizeTags`
- Produces:
  - `type Folder struct { ID, Name, Color string; Tags []string; CreatedAt, UpdatedAt time.Time }`
  - `ListFolders() ([]Folder, error)`
  - `CreateFolder(name, color string, tags []string) (Folder, error)`
  - `UpdateFolder(id, name, color string, tags []string) (Folder, error)`
  - errors: `"name is required"`, `"invalid folder color"`
  - Folder create uses `store.Create(vaultitem.ItemTypeNoteFolder, "note-folder", meta, []byte("{}"))`
  - Folder update uses `store.UpdateMetadata`

Do **not** implement `DeleteFolder` yet (needs note copy-down in Task 4).

- [ ] **Step 1: Write the failing test**

Create `internal/notes/folders_test.go`:

```go
package notes

import "testing"

func TestCreateAndListFolder(t *testing.T) {
	s := newService(t)
	folder, err := s.CreateFolder("Work", "blue", []string{" work "})
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if folder.Name != "Work" || folder.Color != "blue" || len(folder.Tags) != 1 || folder.Tags[0] != "work" {
		t.Fatalf("folder = %+v", folder)
	}
	list, err := s.ListFolders()
	if err != nil {
		t.Fatalf("ListFolders: %v", err)
	}
	if len(list) != 1 || list[0].ID != folder.ID {
		t.Fatalf("list = %+v", list)
	}
}

func TestCreateFolderValidation(t *testing.T) {
	s := newService(t)
	if _, err := s.CreateFolder("", "blue", nil); err == nil {
		t.Fatal("expected error for empty name")
	}
	if _, err := s.CreateFolder("Work", "neon", nil); err == nil {
		t.Fatal("expected error for invalid color")
	}
}

func TestUpdateFolderRenames(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", nil)
	updated, err := s.UpdateFolder(folder.ID, "Ops", "red", []string{"ops"})
	if err != nil {
		t.Fatalf("UpdateFolder: %v", err)
	}
	if updated.Name != "Ops" || updated.Color != "red" || updated.Tags[0] != "ops" {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestEmptyFolderAllowed(t *testing.T) {
	s := newService(t)
	if _, err := s.CreateFolder("Empty", "gray", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notes/ -run Folder -count=1`

Expected: FAIL — methods undefined

- [ ] **Step 3: Widen Store and implement folder CRUD**

Add to `Store` in `internal/notes/notes.go`:

```go
UpdateMetadata(id string, metadata json.RawMessage) (vaultitem.Record, error)
```

Create `internal/notes/folders.go` with `folderMetadata`, `Folder`, `CreateFolder`, `ListFolders`, `UpdateFolder`, `decodeFolderMetadata`, and `folderFrom`. Name is `strings.TrimSpace`; empty → `errors.New("name is required")`. Color → `vaultitem.ValidNoteFolderColor`, else `errors.New("invalid folder color")`. Tags through `NormalizeTags`. Marshal:

```go
type folderMetadata struct {
	Name  string   `json:"name"`
	Color string   `json:"color"`
	Tags  []string `json:"tags,omitempty"`
}
```

Sort `ListFolders` by `UpdatedAt` descending, same as notes.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/notes/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/notes/notes.go internal/notes/folders.go internal/notes/folders_test.go
git commit -m "feat(notes): add note-folder create, list, and update"
```

---

### Task 4: Note organization, effective tags, and delete-folder copy-down

**Files:**
- Modify: `internal/notes/notes.go`
- Modify: `internal/notes/folders.go` (`DeleteFolder`)
- Modify all Go `Create(title, body)` call sites so the project compiles:
  - `internal/notes/notes_test.go`
  - `internal/bridge/notes.go`
  - `internal/bridge/notes_test.go`
  - `internal/bridge/search_test.go`
  - `internal/mcp/server.go`
  - `internal/mcp/tools_notes.go`
  - `internal/mcp/server_test.go`
- Test: `internal/notes/notes_test.go`, `internal/notes/folders_test.go`

**Interfaces:**
- Consumes: Folder CRUD, `NormalizeTags`, `UnionTags`
- Produces:
  - `Create(title, body, folderID string, tags []string) (Summary, error)`
  - `SetOrganization(id, folderID string, tags []string) (Summary, error)`
  - `Update` preserves `folder_id`/`tags`
  - `DeleteFolder(id string) error` — copy-down then delete
  - `Summary`/`Note` fields: `FolderID string`, `Tags []string`, `EffectiveTags []string` (Preview is Task 5)
  - error `"folder not found"` when `folderID` is non-empty and not a folder

Bridge/MCP in this task only change the `Create` arity (`"", nil`) so `go test ./...` still compiles. Do not add folder bridge methods yet.

- [ ] **Step 1: Write the failing tests**

Append to `internal/notes/notes_test.go` (update every existing `s.Create("x", "y")` to `s.Create("x", "y", "", nil)` in the same edit so the file compiles after Step 3):

```go
func TestCreateInFolderWithTags(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", []string{"work"})
	summary, err := s.Create("Standup", "body", folder.ID, []string{"standup"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if summary.FolderID != folder.ID {
		t.Fatalf("FolderID = %q", summary.FolderID)
	}
	if len(summary.EffectiveTags) != 2 {
		t.Fatalf("EffectiveTags = %#v", summary.EffectiveTags)
	}
}

func TestCreateRejectsUnknownFolder(t *testing.T) {
	s := newService(t)
	if _, err := s.Create("T", "b", "deadbeefdeadbeefdeadbeef", nil); err == nil {
		t.Fatal("expected folder not found")
	}
}

func TestUpdatePreservesOrganization(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", []string{"work"})
	summary, _ := s.Create("T", "old", folder.ID, []string{"standup"})
	updated, err := s.Update(summary.ID, "T2", "new")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Body != "new" || updated.FolderID != folder.ID || updated.Tags[0] != "standup" {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestSetOrganizationAndLiveInheritance(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", []string{"work"})
	summary, _ := s.Create("T", "b", folder.ID, nil)
	if _, err := s.UpdateFolder(folder.ID, "Work", "blue", []string{"work", "q3"}); err != nil {
		t.Fatalf("UpdateFolder: %v", err)
	}
	got, err := s.Get(summary.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Body != "b" {
		t.Fatalf("body changed: %q", got.Body)
	}
	joined := strings.Join(got.EffectiveTags, ",")
	if !strings.Contains(joined, "work") || !strings.Contains(joined, "q3") {
		t.Fatalf("EffectiveTags = %#v", got.EffectiveTags)
	}
	if _, err := s.SetOrganization(summary.ID, "", []string{"keep"}); err != nil {
		t.Fatalf("SetOrganization: %v", err)
	}
	moved, _ := s.Get(summary.ID)
	if moved.FolderID != "" || moved.EffectiveTags[0] != "keep" {
		t.Fatalf("moved = %+v", moved)
	}
	if moved.Body != "b" {
		t.Fatal("SetOrganization resealed or lost body")
	}
}

func TestDeleteFolderCopyDown(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Work", "blue", []string{"work"})
	summary, _ := s.Create("T", "keep-body", folder.ID, []string{"standup"})
	if err := s.DeleteFolder(folder.ID); err != nil {
		t.Fatalf("DeleteFolder: %v", err)
	}
	got, _ := s.Get(summary.ID)
	if got.FolderID != "" {
		t.Fatalf("still in folder: %q", got.FolderID)
	}
	if got.Body != "keep-body" {
		t.Fatalf("body = %q", got.Body)
	}
	joined := strings.Join(got.Tags, ",")
	if !strings.Contains(joined, "standup") || !strings.Contains(joined, "work") {
		t.Fatalf("own tags after copy-down = %#v", got.Tags)
	}
	list, _ := s.ListFolders()
	if len(list) != 0 {
		t.Fatalf("folder still listed: %+v", list)
	}
}
```

Add `import "strings"` to `notes_test.go` if missing.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notes/ -count=1`

Expected: FAIL on new tests (arity / missing methods)

- [ ] **Step 3: Implement**

Widen `metadata`:

```go
type metadata struct {
	Title    string   `json:"title"`
	FolderID string   `json:"folder_id,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}
```

Widen `Summary` and `Note` with `FolderID`, `Tags`, `EffectiveTags` (json names `folder_id`, `tags`, `effective_tags`). Always return non-nil slices (`[]string{}`).

Add `loadFolderMap() (map[string]Folder, error)` listing folders once per call.

`Create`: validate title; `NormalizeTags`; if `folderID != ""`, `Get` that record and confirm `Type == vaultitem.ItemTypeNoteFolder` else `errors.New("folder not found")`; marshal metadata; `store.Create` as today.

`Update`: `Get` existing record, decode metadata, replace **only** `Title`, keep `FolderID`/`Tags`, then `store.Update`.

`SetOrganization`: `NormalizeTags`; validate folder like Create; `Get` note; rewrite metadata title+folder+tags; `UpdateMetadata` only.

`Get`/`List`: attach `EffectiveTags` via `UnionTags(meta.Tags, folder.Tags)` when the folder exists in the map; if `folder_id` is set but missing, treat inherited tags as empty (orphan).

`DeleteFolder`: `Get` folder (not found → return that error); `List()` notes; for each with matching `FolderID`, `SetOrganization(id, "", UnionTags(note.Tags, folder.Tags))`; then `store.Delete(folderID)`.

Update every `Create(title, body)` call site to `Create(title, body, "", nil)`:

- Bridge `NotesCreate` still takes two strings internally calling `Create(title, body, "", nil)`
- MCP `NotesAPI.Create(title, body, folderID string, tags []string)`
- `fakeNotes.Create` matches that signature
- `toolNotesCreate` calls `Create(in.Title, in.Body, "", nil)` until Task 7 adds JSON fields

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/notes/ ./internal/bridge/ ./internal/mcp/ ./internal/vaultitem/ -count=1`

Expected: notes/bridge/vaultitem PASS. MCP package may have pre-existing env failures unrelated to notes; `TestNotesListTool` and compile must succeed.

- [ ] **Step 5: Commit**

```bash
git add internal/notes internal/bridge internal/mcp
git commit -m "feat(notes): store folder_id and tags with live inheritance"
```

---

### Task 5: Preview and expanded Search

**Files:**
- Modify: `internal/notes/notes.go`
- Test: `internal/notes/notes_test.go`

**Interfaces:**
- Consumes: `loadFolderMap`, `OpenPayload`
- Produces: `Summary.Preview string `json:"preview"``; `List`/`Search` fill it while decrypt succeeds (collapse whitespace, max 120 runes); `Search` also matches effective tags and folder name (case-insensitive substring)

- [ ] **Step 1: Write the failing tests**

```go
func TestListPreview(t *testing.T) {
	s := newService(t)
	s.Create("T", "hello   world from the body", "", nil)
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list[0].Preview != "hello world from the body" {
		t.Fatalf("Preview = %q", list[0].Preview)
	}
}

func TestSearchByTagAndFolderName(t *testing.T) {
	s := newService(t)
	folder, _ := s.CreateFolder("Engineering", "blue", []string{"platform"})
	s.Create("N1", "alpha", folder.ID, []string{"standup"})
	s.Create("N2", "beta", "", nil)
	byTag, _ := s.Search("platform")
	if len(byTag) != 1 || byTag[0].Title != "N1" {
		t.Fatalf("byTag = %+v", byTag)
	}
	byFolder, _ := s.Search("engineer")
	if len(byFolder) != 1 || byFolder[0].Title != "N1" {
		t.Fatalf("byFolder = %+v", byFolder)
	}
	byOwn, _ := s.Search("standup")
	if len(byOwn) != 1 {
		t.Fatalf("byOwn = %+v", byOwn)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/notes/ -run 'TestListPreview|TestSearchByTag' -count=1`

Expected: FAIL

- [ ] **Step 3: Implement**

Add `Preview` to `Summary`. Helper:

```go
func previewFromBody(body string) string {
	collapsed := strings.Join(strings.Fields(body), " ")
	runes := []rune(collapsed)
	if len(runes) > 120 {
		return string(runes[:120])
	}
	return collapsed
}
```

When building a summary, if `OpenPayload` succeeds, set `Preview`. Search: after title miss, check each effective tag, then folder name (from map), then body — same unlock degradation as today (skip body if decrypt fails; tags/folder name are plaintext).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/notes/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/notes/notes.go internal/notes/notes_test.go
git commit -m "feat(notes): add list preview and search tags/folder names"
```

---

### Task 6: Bridge and permission grants

**Files:**
- Modify: `internal/app/container.go`
- Modify: `internal/bridge/notes.go`
- Test: `internal/bridge/notes_test.go`

**Interfaces:**
- Consumes: `notes.Service` folder + `SetOrganization` + `Create(..., folderID, tags)`
- Produces:
  - Grants: `notes:write` `note-folder:new`, `notes:write` `note-folder:*`, `notes:read` `note-folder:list`, `notes:read` `note-folder:*`
  - `NotesCreate(title, body, folderID string, tags []string)`
  - `NotesSetOrganization(id, folderID string, tags []string) (notes.Summary, error)`
  - `NotesFolderList() ([]notes.Folder, error)`
  - `NotesFolderCreate(name, color string, tags []string) (notes.Folder, error)`
  - `NotesFolderUpdate(id, name, color string, tags []string) (notes.Folder, error)`
  - `NotesFolderDelete(id string) error`
  - Scopes follow notes: `note-folder:new`, `note-folder:`+`recordID(id)`, `note-folder:list`

- [ ] **Step 1: Write the failing test**

Append to `internal/bridge/notes_test.go`:

```go
func TestNotesBridgeFoldersAndOrganization(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}
	folder, err := service.NotesFolderCreate("Work", "blue", []string{"work"})
	if err != nil {
		t.Fatalf("NotesFolderCreate: %v", err)
	}
	summary, err := service.NotesCreate("Standup", "body", folder.ID, []string{"standup"})
	if err != nil {
		t.Fatalf("NotesCreate: %v", err)
	}
	if summary.FolderID != folder.ID {
		t.Fatalf("FolderID = %q", summary.FolderID)
	}
	if _, err := service.NotesSetOrganization(summary.ID, "", []string{"solo"}); err != nil {
		t.Fatalf("NotesSetOrganization: %v", err)
	}
	if err := service.NotesFolderDelete(folder.ID); err != nil {
		t.Fatalf("NotesFolderDelete: %v", err)
	}
	list, err := service.NotesFolderList()
	if err != nil || len(list) != 0 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
}
```

Update existing `NotesCreate("…", "…")` calls in this file and `search_test.go` to `NotesCreate("…", "…", "", nil)`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bridge/ -run Notes -count=1`

Expected: FAIL — missing methods / arity

- [ ] **Step 3: Implement bridge + grants**

Add the four grants next to the existing `builtin.notes` lines in `internal/app/container.go`.

Implement the new `AppService` methods in `internal/bridge/notes.go` mirroring `NotesUpdate` (capability `notes:write` / `notes:read`, metadata `action` + ids). `NotesCreate` signature becomes four parameters.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/bridge/ ./internal/app/ ./internal/notes/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/app/container.go internal/bridge/notes.go internal/bridge/notes_test.go internal/bridge/search_test.go
git commit -m "feat(bridge): expose note folders and organization"
```

---

### Task 7: MCP tools

**Files:**
- Modify: `internal/mcp/server.go` (`NotesAPI`)
- Modify: `internal/mcp/tools_notes.go`
- Modify: `internal/mcp/server_test.go` (`fakeNotes` + new tests)

**Interfaces:**
- Consumes: same service methods as the bridge
- Produces:
  - `notes.create` optional `folder_id`, `tags`
  - `notes.set_organization` input `{id, folder_id, tags}`
  - `notes.folder.list|create|update|delete`
  - `notes.update` remains title+body only

- [ ] **Step 1: Write the failing tests**

In `internal/mcp/server_test.go` keep `fakeNotes.Create` at the 4-arg signature from Task 4. Add:

```go
func (f *fakeNotes) SetOrganization(string, string, []string) (notes.Summary, error) {
	return notes.Summary{ID: "org"}, nil
}
func (f *fakeNotes) ListFolders() ([]notes.Folder, error) { return nil, nil }
func (f *fakeNotes) CreateFolder(string, string, []string) (notes.Folder, error) {
	return notes.Folder{ID: "f", Name: "Work", Color: "blue"}, nil
}
func (f *fakeNotes) UpdateFolder(string, string, string, []string) (notes.Folder, error) {
	return notes.Folder{ID: "f", Name: "Ops", Color: "red"}, nil
}
func (f *fakeNotes) DeleteFolder(string) error { return nil }
```

Add:

```go
func TestNotesFolderCreateTool(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, &fakeNotes{}))
	_, v, err := srv.toolNotesFolderCreate(context.Background(), &sdk.CallToolRequest{}, notesFolderCreateInput{
		Name: "Work", Color: "blue", Tags: []string{"work"},
	})
	if err != nil {
		t.Fatalf("toolNotesFolderCreate: %v", err)
	}
	folder, ok := v.(notes.Folder)
	if !ok || folder.Name != "Work" {
		t.Fatalf("output = %#v", v)
	}
	if caller.last.capability != "notes:write" || caller.last.scope != "note-folder:new" {
		t.Fatalf("Call = %+v", caller.last)
	}
}

func TestNotesSetOrganizationTool(t *testing.T) {
	caller := &fakeCaller{}
	srv := NewServer(testDeps(caller, &fakeNotes{}))
	id := "0123456789abcdef01234567"
	_, _, err := srv.toolNotesSetOrganization(context.Background(), &sdk.CallToolRequest{}, notesSetOrganizationInput{
		ID: id, FolderID: "", Tags: []string{"solo"},
	})
	if err != nil {
		t.Fatalf("toolNotesSetOrganization: %v", err)
	}
	if caller.last.scope != "note:"+id || caller.last.metadata["action"] != "set_organization" {
		t.Fatalf("Call = %+v", caller.last)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mcp/ -run 'TestNotesFolderCreateTool|TestNotesSetOrganizationTool' -count=1`

Expected: FAIL — tools undefined

- [ ] **Step 3: Implement MCP**

Widen `NotesAPI` to the service methods. Register tools in `registerNotes`. Wire `Caller.Call` like existing notes tools (`note-folder:new`, `note-folder:`+opaque, `note-folder:list`, `note-folder:*` for get-by-id updates). `notesCreateInput` gains `FolderID string `json:"folder_id"`` and `Tags []string `json:"tags"``.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mcp/ -run Notes -count=1`

Expected: those tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/mcp
git commit -m "feat(mcp): add notes folder tools and set_organization"
```

---

### Task 8: Frontend bindings and bridge wrappers

**Files:**
- Modify: `frontend/bindings/devkit/internal/bridge/appservice.ts` (generated)
- Modify: `frontend/src/bridge/notes.ts`
- Modify: `frontend/src/modules/notes/NoteList.tsx` (keep compiling: `createNote("Untitled", "", "", [])`)

**Interfaces:**
- Consumes: new `AppService` methods
- Produces: TS types `NoteSummary`, `Note`, `NoteFolder` matching Go json; wrappers `listFolders`, `createFolder`, `updateFolder`, `deleteFolder`, `setNoteOrganization`, `createNote(title, body, folderId, tags)`

- [ ] **Step 1: Regenerate Wails bindings**

Run from `dev-kit-app`:

```bash
wails3 generate bindings
```

Expected: `frontend/bindings/devkit/internal/bridge/appservice.ts` contains `NotesFolderCreate`, `NotesSetOrganization`, and `NotesCreate` with four parameters.

If `wails3` is not on PATH, use the same invocation the last notes pass used (repo README: `wails3 generate bindings`).

- [ ] **Step 2: Update `frontend/src/bridge/notes.ts`**

Replace types and functions:

```ts
export type NoteSummary = {
  id: string;
  title: string;
  folderId: string;
  tags: string[];
  effectiveTags: string[];
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
};

export type NoteFolder = {
  id: string;
  name: string;
  color: string;
  tags: string[];
  updatedAt: string;
};

function asStrings(value: string[] | undefined): string[] {
  return value ?? [];
}

function mapSummary(n: {
  id: string;
  title: string;
  folder_id?: string;
  tags?: string[];
  effective_tags?: string[];
  preview?: string;
  updated_at: string;
}): NoteSummary {
  return {
    id: n.id,
    title: n.title,
    folderId: n.folder_id ?? "",
    tags: asStrings(n.tags),
    effectiveTags: asStrings(n.effective_tags),
    preview: n.preview ?? "",
    updatedAt: n.updated_at,
  };
}
```

Map `createNote` → `AppService.NotesCreate(title, body, folderId, tags)`. Add folder wrappers calling the generated methods. `getNote` must return org fields. `setNoteOrganization(id, folderId, tags)` → `NotesSetOrganization`.

- [ ] **Step 3: Fix compile of existing UI**

In `NoteList.tsx` change `createNote("Untitled", "")` to `createNote("Untitled", "", "", [])`.

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/bindings frontend/src/bridge/notes.ts frontend/src/modules/notes/NoteList.tsx
git commit -m "feat(notes): regenerate bindings and wrap folder APIs"
```

---

### Task 9: Folder/tag color tokens

**Files:**
- Modify: `frontend/src/index.css`
- Create: `frontend/src/modules/notes/tagColor.ts`
- Create: `frontend/src/modules/notes/folderColors.ts`

**Interfaces:**
- Consumes: palette names from the spec
- Produces: CSS variables `--note-folder-red` … `--note-folder-gray`, `--note-tag-0` … `--note-tag-7`; `folderDotVar(color: string)` and `tagChipVar(name: string)` returning `var(--…)` only for known names / hashed 0–7

- [ ] **Step 1: Add CSS variables**

In `:root` of `frontend/src/index.css` (and duplicate under `.dark` with the same values so a future light `:root` can diverge):

```css
--note-folder-red: oklch(0.72 0.16 25);
--note-folder-orange: oklch(0.76 0.14 55);
--note-folder-yellow: oklch(0.82 0.13 95);
--note-folder-green: oklch(0.74 0.14 145);
--note-folder-teal: oklch(0.74 0.12 185);
--note-folder-blue: oklch(0.72 0.12 250);
--note-folder-violet: oklch(0.72 0.14 300);
--note-folder-pink: oklch(0.74 0.14 350);
--note-folder-gray: oklch(0.7 0.02 0);
--note-tag-0: oklch(0.72 0.12 250);
--note-tag-1: oklch(0.74 0.14 145);
--note-tag-2: oklch(0.76 0.14 55);
--note-tag-3: oklch(0.72 0.14 300);
--note-tag-4: oklch(0.74 0.14 350);
--note-tag-5: oklch(0.74 0.12 185);
--note-tag-6: oklch(0.82 0.13 95);
--note-tag-7: oklch(0.7 0.02 0);
```

Do not put these hex/oklch values in component `className`s.

- [ ] **Step 2: Helpers**

`folderColors.ts`:

```ts
export const FOLDER_COLORS = [
  "red", "orange", "yellow", "green", "teal", "blue", "violet", "pink", "gray",
] as const;
export type FolderColor = (typeof FOLDER_COLORS)[number];

export function folderDotVar(color: string): string {
  return FOLDER_COLORS.includes(color as FolderColor)
    ? `var(--note-folder-${color})`
    : "var(--note-folder-gray)";
}
```

`tagColor.ts`:

```ts
export function tagChipVar(name: string): string {
  const key = name.toLowerCase();
  let hash = 0;
  for (let i = 0; i < key.length; i++) {
    hash = (hash * 31 + key.charCodeAt(i)) >>> 0;
  }
  return `var(--note-tag-${hash % 8})`;
}
```

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/src/index.css frontend/src/modules/notes/tagColor.ts frontend/src/modules/notes/folderColors.ts
git commit -m "feat(notes): add folder and tag color tokens"
```

---

### Task 10: Dashboard UI (replaces master-detail list)

**Files:**
- Create: `frontend/src/modules/notes/NotesDashboard.tsx`
- Modify: `frontend/src/modules/notes/NotesView.tsx`
- Delete: `frontend/src/modules/notes/NoteList.tsx`

**Interfaces:**
- Consumes: `listNotes`/`searchNotes`/`createNote`/`listFolders`/`createFolder`/`updateFolder`/`deleteFolder`
- Produces: dashboard screen; `onOpenNote(id)`; folder scope `all | ungrouped | folder:<id>`; tag filter AND on `effectiveTags`; search box; new note in current folder (or ungrouped when All/Ungrouped)

- [ ] **Step 1: Implement `NotesDashboard.tsx`**

Layout: `flex min-h-0 flex-1`. Left column `w-56 shrink-0 border-r` with buttons:

- All
- Ungrouped
- each folder: 8px dot `style={{ background: folderDotVar(folder.color) }}`, name, count
- overflow `DropdownMenu` per folder: Rename (Dialog: name + color palette buttons using `FOLDER_COLORS` + `folderDotVar`), Edit tags (Input + chips), Delete (`AlertDialog` copy: notes are kept and tags are copied)

Create folder: `Dialog` with name + color picker + optional tags, calls `createFolder`.

Right: top search `Input` + selected tag chips (click chip on a card toggles it into the AND filter; click again / × to remove). Grid of `button` cards: title, `preview` (`line-clamp-2 text-muted-foreground`), effective tag chips (`Badge` with `style={{ background: tagChipVar(tag) }}` and `text-background` or `text-foreground` — pick `text-background` only if contrast fails; prefer `bg-muted` + a 6px color dot using `tagChipVar` so we never hardcode chip text color).

Filter logic (client):

```ts
const scoped = notes.filter((n) => {
  if (scope === "ungrouped") {
    return !n.folderId || !folderIds.has(n.folderId);
  }
  if (scope.startsWith("folder:")) {
    return n.folderId === scope.slice(7);
  }
  return true;
});
const tagged = selectedTags.every((tag) =>
  n.effectiveTags.some((t) => t.toLowerCase() === tag.toLowerCase()),
);
```

Search: on change call `searchNotes(q)` when `q.trim()`, else `listNotes()` (same immediate refresh as the old `NoteList`, no extra debounce). Then apply scope + tag AND on the result.

New note:

```ts
const folderId = scope.startsWith("folder:") ? scope.slice(7) : "";
const created = await createNote("Untitled", "", folderId, []);
onOpenNote(created.id);
```

Toast `toast.error` on CRUD failure (sonner, same as image upload).

- [ ] **Step 2: Rewrite `NotesView.tsx`**

```tsx
export function NotesView() {
  const [openId, setOpenId] = useState<string | null>(null);
  const [refreshToken, setRefreshToken] = useState(0);
  if (openId) {
    return (
      <NoteEditor
        key={openId}
        noteId={openId}
        onBack={() => setOpenId(null)}
        onDeleted={() => {
          setOpenId(null);
          setRefreshToken((t) => t + 1);
        }}
        onSaved={() => setRefreshToken((t) => t + 1)}
      />
    );
  }
  return (
    <NotesDashboard
      refreshToken={refreshToken}
      onOpenNote={setOpenId}
      onChanged={() => setRefreshToken((t) => t + 1)}
    />
  );
}
```

Temporarily add `onBack?: () => void` to `NoteEditor` props if Task 11 has not landed — render a Back `Button` that calls it so the dashboard is reachable. Task 11 replaces this with breadcrumb.

Delete `NoteList.tsx`.

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 4: Manual check**

Run the app, open Notes: grid fills the pane; create folder; create note in it; tag filter AND; delete folder leaves the note with copied tags under Ungrouped.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/notes
git commit -m "feat(notes): replace list with folder dashboard"
```

---

### Task 11: Editor chrome — breadcrumb, folder, tags

**Files:**
- Modify: `frontend/src/modules/notes/NoteEditor.tsx`

**Interfaces:**
- Consumes: `getNote`, `updateNote`, `setNoteOrganization`, `listFolders`, `Note.folderId/tags/effectiveTags`
- Produces: back + breadcrumb `Notes / {folder|Ungrouped} / {title}`; folder `<select>` or shadcn `Select` if present else native select; own-tag input; inherited tags read-only

- [ ] **Step 1: Extend load and organization writes**

On load, `listFolders()` + `getNote`. Keep autosave calling `updateNote(id, title, body)` only.

Folder change / own-tag change:

```ts
await setNoteOrganization(noteId, folderId, ownTags);
```

Do not wait for the 1.5s body debounce. Toast on failure; keep local state.

Own tags: `Input` on Enter / comma; `Normalize` client-side (trim, skip empty, skip dupes case-insensitive, reject >40 chars with toast, reject adding past 32). Inherited = `effectiveTags` whose lowercased name is not in own tags — render read-only chips, not removable.

Breadcrumb back calls `onBack`. Header stays `Saving…` / `Saved`.

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 3: Manual check**

Open a note from a tagged folder: inherited chips visible; changing folder does not require typing in the body; reload still has the new folder; autosave does not clear tags.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/notes/NoteEditor.tsx
git commit -m "feat(notes): add editor folder picker and tag inheritance chrome"
```

---

### Task 12: Slash menu hotkeys and token popover

**Files:**
- Modify: `frontend/src/modules/notes/plate/SlashInput.tsx`
- Modify: `frontend/src/components/ui/inline-combobox.tsx`

**Interfaces:**
- Consumes: existing slash items; `@/components/ui/kbd`
- Produces: group label `Basic blocks`; `Kbd` for H1–H3 `Mod+Alt+1..3`, Quote `Mod+Shift+.`, Code `Mod+Alt+8`; omit Kbd otherwise; `Mod` is `⌘` on Mac else `Ctrl`; popover uses `bg-popover text-popover-foreground border border-border`

Floating toolbar already uses `bg-popover` / `border` — do not restyle it.

- [ ] **Step 1: Combobox chrome**

In `InlineComboboxContent`, ensure class includes `border border-border bg-popover text-popover-foreground` (keep `z-500 max-h-[288px] w-[300px] overflow-y-auto rounded-md shadow-md`). No `bg-zinc-*`.

Give `InlineComboboxItem` a trailing slot by putting children in `flex w-full items-center gap-2` with `ml-auto` allowed on the last child. If the item already wraps children in a flex row, add `justify-between w-full`.

- [ ] **Step 2: Slash items**

Add optional `shortcut?: string[]` on `SlashItem`. Helper:

```ts
function modKey(): string {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/i.test(navigator.platform)
    ? "⌘"
    : "Ctrl";
}
```

Set:

- Heading 1: `[modKey(), "Alt", "1"]` (compute once at render, not module scope, so SSR/platform is correct)
- Heading 2 / 3 analogously
- Quote: `[modKey(), "Shift", "."]`
- Code block: `[modKey(), "Alt", "8"]`

In the item children:

```tsx
{item.icon}
<span className="flex-1">{item.label}</span>
{item.shortcut ? (
  <KbdGroup>
    {item.shortcut.map((part) => (
      <Kbd key={part}>{part}</Kbd>
    ))}
  </KbdGroup>
) : null}
```

Change group label to `Basic blocks`. Do not add image/mermaid/H4.

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 4: Manual check**

Type `/` in a note: popover uses popover tokens; Heading 1 shows ⌘/Ctrl+Alt+1; Toggle has no Kbd; markdown `# ` still becomes H1.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/notes/plate/SlashInput.tsx frontend/src/components/ui/inline-combobox.tsx
git commit -m "feat(notes): show real hotkeys on the slash menu"
```

---

### Task 13: End-to-end verification

**Files:** none new

- [ ] **Step 1: Backend tests**

Run: `go test ./internal/vaultitem/ ./internal/notes/ ./internal/bridge/ -count=1`

Expected: PASS

- [ ] **Step 2: MCP notes tests**

Run: `go test ./internal/mcp/ -run Notes -count=1`

Expected: PASS

- [ ] **Step 3: Frontend typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 4: Manual script**

1. Dashboard shows All / Ungrouped / folders; empty folder exists.
2. Note in folder inherits folder tags on cards and in the editor (read-only).
3. AND tag filter hides notes missing any selected tag.
4. Search matches folder name and tag.
5. Delete folder: notes survive under Ungrouped with copied tags; body unchanged.
6. Edit body autosave does not drop folder/tags.
7. Slash menu Kbd + no hardcoded dark classes on the popover.
8. Move note between folders does **not** copy old folder tags onto the note.

If anything fails, fix in a focused commit on that layer, then re-run the failing command.
