# Notes Module Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the Notes module's documented "no in-place edit API" gap and replace its plain `<textarea>` with a Plate (platejs.org) rich-text editor giving Notion-like block editing, markdown-shortcut typing, and Mermaid diagram rendering — while keeping note bodies stored as plain markdown text so Search and sync conflict-merge keep working unmodified.

**Architecture:** A new `vaultitem.Store.Update` method (sibling to the existing `UpdateMetadata`/`Reseal`) threads through `notes.Service` → the bridge → an MCP tool, following the exact patterns `Create`/`Delete` already use at each layer. The frontend splits `NotesView` into a master-detail shell (`NoteList` + `NoteEditor`), with `NoteEditor` wrapping a Plate editor that deserializes/serializes markdown on load/save and autosaves 1.5s after the user stops typing.

**Tech Stack:** Go (backend, existing `vaultitem`/`notes`/`bridge`/`mcp` packages), React 19 + TypeScript, Plate (`platejs`), `@platejs/markdown` + `remark-gfm`, `mermaid`, shadcn CLI (Plate's own registry-based component installer).

---

## Reference

Design spec: `docs/superpowers/specs/2026-08-13-notes-module-design.md`

Backend verification: `go test ./internal/vaultitem/... ./internal/notes/... ./internal/bridge/... ./internal/mcp/...`

Frontend verification: `cd frontend && bunx tsc --noEmit -p tsconfig.json` (no test runner exists in this project — confirmed during the earlier Phase 5 Workbench UI work, still true).

---

### Task 1: `vaultitem.Store.Update`

**Files:**
- Modify: `internal/vaultitem/store.go` (add a method after `UpdateMetadata`, before `Delete`)
- Test: `internal/vaultitem/store_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/vaultitem/store_test.go`, after `TestStoreOpenRejectsSwappedID`:

```go
func TestStoreUpdateReplacesPayloadAndMetadata(t *testing.T) {
	s := newUnlockedStore(t)

	rec, err := s.Create("note", "note-body", json.RawMessage(`{"title":"old"}`), []byte("old body"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := s.Update(rec.ID, "note-body", json.RawMessage(`{"title":"new"}`), []byte("new body"))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.SyncState != syncStateLocal {
		t.Fatalf("sync_state = %q, want %q", updated.SyncState, syncStateLocal)
	}
	if string(updated.Metadata) != `{"title":"new"}` {
		t.Fatalf("metadata = %s, want new title", updated.Metadata)
	}

	plain, err := s.OpenPayload(updated, "note-body")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(plain) != "new body" {
		t.Fatalf("plain = %q, want %q", plain, "new body")
	}
}

func TestStoreUpdateFailsWhenVaultLocked(t *testing.T) {
	s, v := newStoreWithVault(t)

	rec, err := s.Create("note", "note-body", json.RawMessage(`{"title":"x"}`), []byte("secret body"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	v.Lock()

	if _, err := s.Update(rec.ID, "note-body", json.RawMessage(`{"title":"y"}`), []byte("new secret")); err == nil {
		t.Fatal("expected Update to fail when vault is locked")
	}
}

func TestStoreUpdateFailsForUnknownID(t *testing.T) {
	s := newUnlockedStore(t)

	if _, err := s.Update("does-not-exist", "note-body", json.RawMessage(`{"title":"x"}`), []byte("body")); err == nil {
		t.Fatal("expected Update to fail for an unknown id")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/vaultitem/... -run TestStoreUpdate -v`
Expected: FAIL — `s.Update undefined (type *Store has no field or method Update)`

- [ ] **Step 3: Implement `Store.Update`**

In `internal/vaultitem/store.go`, add this method immediately after `UpdateMetadata` (which ends right before `// Delete removes an item by id.`):

```go
// Update rewrites both metadata and payload in one write, bumping
// UpdatedAt and marking the record dirty for sync. The general-purpose
// sibling of UpdateMetadata (metadata-only) and Reseal (payload-only,
// caller persists) — this is the method that lets a caller replace both
// plaintext metadata and the encrypted payload atomically in one Put.
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

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vaultitem/... -run TestStoreUpdate -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Run the full package test suite to confirm no regressions**

Run: `go test ./internal/vaultitem/...`
Expected: PASS (ok)

- [ ] **Step 6: Commit**

```bash
git add internal/vaultitem/store.go internal/vaultitem/store_test.go
git commit -m "feat(vaultitem): add Store.Update for atomic metadata+payload rewrite"
```

---

### Task 2: `notes.Service.Update`

**Files:**
- Modify: `internal/notes/notes.go` (widen the `Store` interface, add `Service.Update`)
- Test: `internal/notes/notes_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/notes/notes_test.go`, after `TestCreateRequiresTitle`:

```go
func TestUpdateReplacesTitleAndBody(t *testing.T) {
	s := newService(t)
	summary, err := s.Create("Old title", "old body")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := s.Update(summary.ID, "New title", "new body")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Title != "New title" || updated.Body != "new body" {
		t.Fatalf("updated = %+v", updated)
	}

	fetched, err := s.Get(summary.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Title != "New title" || fetched.Body != "new body" {
		t.Fatalf("fetched = %+v", fetched)
	}
}

func TestUpdateRequiresTitle(t *testing.T) {
	s := newService(t)
	summary, _ := s.Create("Title", "body")
	if _, err := s.Update(summary.ID, "", "body"); err == nil {
		t.Fatal("expected error for empty title")
	}
}

func TestUpdateFailsForUnknownID(t *testing.T) {
	s := newService(t)
	if _, err := s.Update("does-not-exist", "Title", "body"); err == nil {
		t.Fatal("expected error for unknown id")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/notes/... -run TestUpdate -v`
Expected: FAIL — `s.Update undefined (type *Service has no field or method Update)`

- [ ] **Step 3: Widen the `Store` interface**

In `internal/notes/notes.go`, the `Store` interface currently reads:

```go
type Store interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	Delete(id string) error
}
```

Add an `Update` method to it, matching `vaultitem.Store.Update`'s exact signature from Task 1:

```go
type Store interface {
	Create(itemType, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Get(id string) (vaultitem.Record, error)
	OpenPayload(record vaultitem.Record, keyID string) ([]byte, error)
	ListByType(itemType string) ([]vaultitem.Record, error)
	Update(id, keyID string, metadata json.RawMessage, secret []byte) (vaultitem.Record, error)
	Delete(id string) error
}
```

- [ ] **Step 4: Implement `Service.Update`**

Add this method after `Get` and before `Delete`:

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

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/notes/... -run TestUpdate -v`
Expected: PASS (3 tests)

- [ ] **Step 6: Run the full package test suite to confirm no regressions**

Run: `go test ./internal/notes/...`
Expected: PASS (ok) — this also confirms `*vaultitem.Store` (used via `internal/app/container.go`'s `notes.NewService(itemStore)`) still satisfies the widened `notes.Store` interface now that Task 1 added the matching method.

- [ ] **Step 7: Commit**

```bash
git add internal/notes/notes.go internal/notes/notes_test.go
git commit -m "feat(notes): add Service.Update, widen Store interface"
```

---

### Task 3: Bridge `NotesUpdate`

**Files:**
- Modify: `internal/bridge/notes.go` (add `NotesUpdate` after `NotesCreate`)
- Test: `internal/bridge/notes_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/bridge/notes_test.go`, after `TestNotesBridgeCreateListGet`:

```go
func TestNotesBridgeUpdate(t *testing.T) {
	container := app.NewContainer()
	service := NewAppService(container)
	if _, err := service.SetupVault("master-password"); err != nil {
		t.Fatalf("SetupVault: %v", err)
	}

	summary, err := service.NotesCreate("Old title", "old body")
	if err != nil {
		t.Fatalf("NotesCreate: %v", err)
	}

	updated, err := service.NotesUpdate(summary.ID, "New title", "new body")
	if err != nil {
		t.Fatalf("NotesUpdate: %v", err)
	}
	if updated.Title != "New title" || updated.Body != "new body" {
		t.Fatalf("updated = %+v", updated)
	}

	note, err := service.NotesGet(summary.ID)
	if err != nil {
		t.Fatalf("NotesGet: %v", err)
	}
	if note.Title != "New title" || note.Body != "new body" {
		t.Fatalf("note = %+v", note)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bridge/... -run TestNotesBridgeUpdate -v`
Expected: FAIL — `service.NotesUpdate undefined`

- [ ] **Step 3: Implement `AppService.NotesUpdate`**

In `internal/bridge/notes.go`, add this method after `NotesCreate`, before `NotesGet` — same shape as `NotesDelete` (capability `notes:write`, scope `note:<opaqueID>`):

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

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bridge/... -run TestNotesBridgeUpdate -v`
Expected: PASS

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/bridge/...`
Expected: PASS (ok)

- [ ] **Step 6: Commit**

```bash
git add internal/bridge/notes.go internal/bridge/notes_test.go
git commit -m "feat(bridge): add NotesUpdate"
```

---

### Task 4: MCP `notes.update` tool

**Files:**
- Modify: `internal/mcp/server.go` (widen `NotesAPI` interface)
- Modify: `internal/mcp/tools_notes.go` (add the tool + handler)

No dedicated test file exists for `tools_notes.go` today (confirmed — only `internal/bridge/notes_test.go` and `internal/notes/notes_test.go` exist for this feature; MCP tool wiring for notes has no existing test precedent to follow). This task is verified by compilation + the existing `go build ./...` — do not introduce a new test file/pattern that doesn't match this package's existing coverage style (YAGNI: don't invent test infrastructure this specific file has never had).

- [ ] **Step 1: Widen the `NotesAPI` interface**

In `internal/mcp/server.go`, the interface currently reads (around line 31):

```go
// NotesAPI is the notes domain surface MCP tools invoke (duck-typed).
type NotesAPI interface {
	List() ([]notes.Summary, error)
	Search(query string) ([]notes.Summary, error)
	Get(id string) (notes.Note, error)
	Create(title, body string) (notes.Summary, error)
	Delete(id string) error
}
```

Add `Update` after `Create`:

```go
// NotesAPI is the notes domain surface MCP tools invoke (duck-typed).
type NotesAPI interface {
	List() ([]notes.Summary, error)
	Search(query string) ([]notes.Summary, error)
	Get(id string) (notes.Note, error)
	Create(title, body string) (notes.Summary, error)
	Update(id, title, body string) (notes.Note, error)
	Delete(id string) error
}
```

- [ ] **Step 2: Register the tool**

In `internal/mcp/tools_notes.go`, inside `registerNotes()`, add this block after the `notes.create` registration and before the `notes.delete` registration:

```go
	sdk.AddTool(s.mcp, &sdk.Tool{
		Name:        "notes.update",
		Description: "Update an existing note's title and body",
		Annotations: &sdk.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
		},
	}, s.toolNotesUpdate)
```

- [ ] **Step 3: Implement the handler**

Add this after `toolNotesCreate`, before `toolNotesDelete`:

```go
type notesUpdateInput struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (s *Server) toolNotesUpdate(ctx context.Context, _ *sdk.CallToolRequest, in notesUpdateInput) (*sdk.CallToolResult, any, error) {
	opaqueID := recordID(in.ID)
	v, err := s.deps.Caller.Call(ctx, "builtin.notes", "notes:write", "note:"+opaqueID, meta("update", map[string]any{
		"note_id": opaqueID,
	}), func(context.Context) (any, error) {
		return s.deps.Notes.Update(in.ID, in.Title, in.Body)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, v, nil
}
```

- [ ] **Step 4: Build to confirm it compiles**

Run: `go build ./...`
Expected: no output, exit code 0. This confirms `*notes.Service` (passed as `Deps.Notes` when the MCP server is constructed) still satisfies the widened `NotesAPI` interface, since Task 2 already added the matching `Update` method to `notes.Service`.

- [ ] **Step 5: Run the mcp package test suite to confirm no regressions**

Run: `go test ./internal/mcp/...`
Expected: PASS (ok)

- [ ] **Step 6: Commit**

```bash
git add internal/mcp/server.go internal/mcp/tools_notes.go
git commit -m "feat(mcp): add notes.update tool"
```

---

### Task 5: Regenerate Wails bindings, add `updateNote` to the frontend bridge

**Files:**
- Generated (do not hand-edit): `frontend/bindings/devkit/internal/bridge/appservice.ts`
- Modify: `frontend/src/bridge/notes.ts`

- [ ] **Step 1: Regenerate the TypeScript bindings**

Wails auto-generates `frontend/bindings/**` from the Go `AppService` methods — `NotesUpdate` (added in Task 3) does not have a TypeScript binding yet. Run from the repo root (`dev-kit-app/`):

```bash
task common:generate:bindings
```

- [ ] **Step 2: Verify the binding was generated**

Run: `grep -n "NotesUpdate" frontend/bindings/devkit/internal/bridge/appservice.ts`
Expected: one line similar to
`export function NotesUpdate(id: string, title: string, body: string): $CancellablePromise<notes$0.Note> {`

If this doesn't appear, do not hand-write the binding — re-run Step 1 and confirm Task 3's Go changes actually compiled (`go build ./...` from repo root) before retrying.

- [ ] **Step 3: Add `updateNote` to the frontend bridge**

In `frontend/src/bridge/notes.ts`, add this function after `createNote`:

```ts
export function updateNote(id: string, title: string, body: string): Promise<Note> {
  return callBridge(async () => {
    const n = await AppService.NotesUpdate(id, title, body);
    return { id: n.id, title: n.title, body: n.body };
  });
}
```

- [ ] **Step 4: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/bindings frontend/src/bridge/notes.ts
git commit -m "feat(notes): regenerate bindings, add updateNote to the frontend bridge"
```

---

### Task 6: Install Plate

**Files:**
- Modify: `frontend/components.json` (add the `@plate` registry)
- Modify: `frontend/package.json`, `frontend/bun.lock` (new dependencies)
- Generated: `frontend/src/components/editor/plugins/*-kit.tsx`, `frontend/src/components/ui/*-node.tsx`, `frontend/src/components/ui/editor.tsx`, and related Plate UI files written by the shadcn CLI

This task installs Plate via its own documented shadcn-registry CLI path — the same install mechanism every existing `components/ui/*.tsx` file in this repo already came from. The exact set of files the registry writes isn't something to guess at; the verification step below is how the next task confirms what's actually available before importing it, the same "generate, then verify the output" approach Task 5 used for Wails bindings.

- [ ] **Step 1: Register the Plate registry**

Add a `registries` entry to `frontend/components.json` (currently `"registries": {}`):

```json
  "registries": {
    "@plate": "https://platejs.org/r/{name}.json"
  }
```

- [ ] **Step 2: Install the core editor + starter kit bundle**

Run from `frontend/`:

```bash
npx shadcn@latest add @plate/plate-ui
```

This is Plate's own documented "fastest working editor path" bundle — it installs the `platejs`/`@platejs/*` npm packages it needs and writes the base editor shell (`components/ui/editor.tsx` exporting `Editor`/`EditorContainer`) plus starter Kit files under `components/editor/plugins/`.

- [ ] **Step 3: Install the specific node/toolbar components this module needs**

Run from `frontend/`:

```bash
npx shadcn@latest add @plate/media-image-node @plate/code-node @plate/link-toolbar-button @plate/fixed-toolbar @plate/mark-toolbar-button
```

- [ ] **Step 4: Install the two npm-only dependencies the CLI won't add**

`mermaid` (for Task 8's diagram rendering — version pinned to match what `base-doc` already trusts) and `remark-gfm` (for GFM table/task-list markdown, per the design spec):

```bash
bun add mermaid@^11.16.0 remark-gfm@^4.0.0
```

- [ ] **Step 5: List what got scaffolded**

Run:
```bash
ls frontend/src/components/editor/plugins/ 2>/dev/null
ls frontend/src/components/ui/ | grep -E "node|editor|toolbar"
```

Write down (for use in Task 7) which `*-kit.tsx` files exist under `components/editor/plugins/` and which `*-node.tsx` files exist under `components/ui/`. Task 7's plugin list assumes the conventional names Plate's own docs use (`BasicBlocksKit`, `BasicMarksKit`, `ListKit`, `LinkKit`, `TableKit`, `CodeBlockKit`, each in a matching `*-kit.tsx` file, plus `CodeBlockElement` in `code-block-node.tsx`) — if this listing shows different names, adjust Task 7's imports to match what actually exists rather than what's assumed.

- [ ] **Step 6: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors (the CLI-generated files should already be internally consistent; this just confirms nothing is broken before building on top of them).

- [ ] **Step 7: Commit**

```bash
git add frontend/components.json frontend/package.json frontend/bun.lock frontend/src/components
git commit -m "chore(notes): install Plate editor via shadcn registry"
```

---

### Task 7: Notes editor plugin kit

**Files:**
- Create: `frontend/src/modules/notes/plate/plugins.ts`

- [ ] **Step 1: Confirm the Kit import paths from Task 6's listing**

Before writing this file, re-check Task 6 Step 5's output. The code below assumes these files exist:
- `@/components/editor/plugins/basic-blocks-kit` exporting `BasicBlocksKit`
- `@/components/editor/plugins/basic-marks-kit` exporting `BasicMarksKit`
- `@/components/editor/plugins/list-kit` exporting `ListKit`
- `@/components/editor/plugins/link-kit` exporting `LinkKit`
- `@/components/editor/plugins/table-kit` exporting `TableKit`
- `@/components/editor/plugins/code-block-kit` exporting `CodeBlockKit`

If any of these don't exist under those exact names, the fallback is to import the plugin directly from its npm package instead of a Kit wrapper — e.g. if there's no `list-kit.tsx`, use `import { ListPlugin } from '@platejs/list/react'` directly (unconfigured plugins still work, just without the Kit's pre-wired markdown input rules — acceptable degradation, not a blocker).

- [ ] **Step 2: Write the plugin kit**

```ts
// frontend/src/modules/notes/plate/plugins.ts
import remarkGfm from "remark-gfm";
import { MarkdownPlugin } from "@platejs/markdown";
import { SlashInputPlugin, SlashPlugin } from "@platejs/slash-command/react";
import { TogglePlugin } from "@platejs/toggle/react";

import { BasicBlocksKit } from "@/components/editor/plugins/basic-blocks-kit";
import { BasicMarksKit } from "@/components/editor/plugins/basic-marks-kit";
import { CodeBlockKit } from "@/components/editor/plugins/code-block-kit";
import { LinkKit } from "@/components/editor/plugins/link-kit";
import { ListKit } from "@/components/editor/plugins/list-kit";
import { TableKit } from "@/components/editor/plugins/table-kit";

import { MermaidCodeBlock } from "./MermaidCodeBlock";
import { ImageUploadPlugin } from "./ImageUpload";

/**
 * CodeBlockKit's own code_block plugin renders every language with plain
 * syntax highlighting. Swap in MermaidCodeBlock only for that one plugin —
 * every other language keeps CodeBlockKit's default rendering unchanged.
 */
const codeBlockKitWithMermaid = CodeBlockKit.map((plugin) =>
  plugin.key === "code_block" ? plugin.withComponent(MermaidCodeBlock) : plugin,
);

export const NotesEditorKit = [
  ...BasicBlocksKit,
  ...BasicMarksKit,
  ...ListKit,
  ...LinkKit,
  ...TableKit,
  ...codeBlockKitWithMermaid,
  TogglePlugin,
  ImageUploadPlugin,
  SlashPlugin,
  SlashInputPlugin,
  MarkdownPlugin.configure({
    options: {
      remarkPlugins: [remarkGfm],
    },
  }),
];
```

- [ ] **Step 3: Typecheck**

This will fail until Tasks 8 and 9 create `MermaidCodeBlock.tsx` and `ImageUpload.ts` — that's expected. Run it again after Task 9 completes:

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected (after Task 9): no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/notes/plate/plugins.ts
git commit -m "feat(notes): define the Plate plugin kit"
```

---

### Task 8: Image upload handler (base64 data URI, no network)

**Files:**
- Create: `frontend/src/modules/notes/plate/ImageUpload.ts`

- [ ] **Step 1: Write the handler**

```ts
// frontend/src/modules/notes/plate/ImageUpload.ts
import { ImagePlugin } from "@platejs/media/react";

// Matches Plate's own default image cap (4MB/image) — enforced explicitly
// here so an oversized image is rejected with a clear error instead of
// silently accepted and only failing (or bloating the note) later.
const MAX_IMAGE_BYTES = 4 * 1024 * 1024;

/**
 * Local-first: there is no server to upload to. Plate's uploadImage option
 * accepts a Data URL and may return the original input unchanged — that's
 * exactly what happens here. The data URI becomes the image node's `url`
 * and gets embedded directly in the note's markdown body, inside the same
 * encrypted payload as everything else. No separate asset store, no new
 * decrypt/serve path.
 */
export const ImageUploadPlugin = ImagePlugin.configure({
  options: {
    uploadImage: async (dataUrl: ArrayBuffer | string) => {
      if (typeof dataUrl === "string" && dataUrl.startsWith("data:")) {
        const approxBytes = Math.ceil((dataUrl.length * 3) / 4);
        if (approxBytes > MAX_IMAGE_BYTES) {
          throw new Error("Image exceeds the 4MB limit and was not inserted.");
        }
      }
      return dataUrl;
    },
  },
});
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: this file itself should typecheck cleanly; `plugins.ts` (Task 7) will still error until Task 9 lands `MermaidCodeBlock.tsx` — that's expected at this point.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/notes/plate/ImageUpload.ts
git commit -m "feat(notes): add local base64 image upload handler"
```

---

### Task 9: Mermaid code block renderer

**Files:**
- Create: `frontend/src/modules/notes/plate/MermaidCodeBlock.tsx`

- [ ] **Step 1: Confirm the base code block component name from Task 6**

This wraps whatever component Task 6 installed for `@plate/code-node` (expected to be `CodeBlockElement`, exported from `@/components/ui/code-block-node`, per Plate's own docs convention). Adjust the import in Step 2 if Task 6's listing showed a different export name.

- [ ] **Step 2: Write the component**

```tsx
// frontend/src/modules/notes/plate/MermaidCodeBlock.tsx
import { useEffect, useId, useState } from "react";
import { NodeApi } from "platejs";
import { PlateElement, type PlateElementProps } from "platejs/react";
import { CodeBlockElement } from "@/components/ui/code-block-node";
import { Button } from "@/components/ui/button";

type RenderState =
  | { status: "rendering" }
  | { status: "rendered"; svg: string }
  | { status: "error"; message: string };

const renderCache = new Map<string, Promise<{ svg: string }>>();

function renderMermaid(id: string, source: string) {
  const key = `${id}:${source}`;
  const cached = renderCache.get(key);
  if (cached) {
    return cached;
  }
  const promise = import("mermaid").then(({ default: mermaid }) => {
    mermaid.initialize({ startOnLoad: false, securityLevel: "loose", theme: "dark" });
    return mermaid.render(id.replaceAll(":", ""), source);
  });
  renderCache.set(key, promise);
  return promise;
}

function MermaidDiagram({ source }: { source: string }) {
  const id = useId();
  const [state, setState] = useState<RenderState>({ status: "rendering" });

  useEffect(() => {
    let cancelled = false;
    setState({ status: "rendering" });
    renderMermaid(id, source)
      .then(({ svg }) => {
        if (!cancelled) {
          setState({ status: "rendered", svg });
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState({
            status: "error",
            message: error instanceof Error ? error.message : "Invalid diagram source.",
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [id, source]);

  if (state.status === "error") {
    return <p className="text-sm text-destructive">Mermaid error: {state.message}</p>;
  }
  if (state.status === "rendering") {
    return <p className="text-sm text-muted-foreground">Rendering diagram…</p>;
  }
  // eslint-disable-next-line react/no-danger -- Mermaid's own render output, not user HTML
  return <div dangerouslySetInnerHTML={{ __html: state.svg }} />;
}

export function MermaidCodeBlock(props: PlateElementProps) {
  const lang = (props.element as { lang?: string }).lang;
  const [mode, setMode] = useState<"preview" | "edit">("preview");

  if (lang !== "mermaid") {
    return <CodeBlockElement {...props} />;
  }

  if (mode === "edit") {
    return (
      <PlateElement {...props} className="relative">
        <Button
          type="button"
          size="sm"
          variant="outline"
          contentEditable={false}
          className="absolute right-2 top-2 z-10"
          onClick={() => setMode("preview")}
        >
          Preview
        </Button>
        {props.children}
      </PlateElement>
    );
  }

  const source = NodeApi.string(props.element);

  return (
    <PlateElement
      {...props}
      contentEditable={false}
      className="relative rounded-lg border border-border bg-card p-4"
    >
      <Button
        type="button"
        size="sm"
        variant="outline"
        className="absolute right-2 top-2 z-10"
        onClick={() => setMode("edit")}
      >
        Edit
      </Button>
      <MermaidDiagram source={source} />
      <div className="hidden">{props.children}</div>
    </PlateElement>
  );
}
```

Notes on this implementation:
- In `edit` mode, `props.children` (Plate's own code-line rendering) stays editable — the user types Mermaid source exactly like any other code block. In `preview` mode, `props.children` is rendered `hidden` (not removed — Plate/Slate requires an element's children to stay in the DOM for its internal void/selection bookkeeping) while the actual visible content is the rendered SVG.
- `NodeApi.string(props.element)` extracts the block's plain-text content regardless of how many `code_line` children it has — this is Plate's stable, documented text-extraction API (re-exported from Slate), not something that depends on Task 6's generated file internals.

- [ ] **Step 3: Typecheck the full plugin kit now that all three files exist**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/notes/plate/MermaidCodeBlock.tsx
git commit -m "feat(notes): render mermaid code blocks as diagrams with an edit/preview toggle"
```

---

### Task 10: `NoteList`

**Files:**
- Create: `frontend/src/modules/notes/NoteList.tsx`

- [ ] **Step 1: Write the component**

```tsx
// frontend/src/modules/notes/NoteList.tsx
import { useEffect, useState } from "react";
import { Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { createNote, listNotes, searchNotes, type NoteSummary } from "../../bridge/notes";
import { errorMessage } from "../../bridge/errors";

type NoteListProps = {
  selectedId: string | null;
  onSelect: (id: string) => void;
  refreshToken: number;
};

export function NoteList({ selectedId, onSelect, refreshToken }: NoteListProps) {
  const [notes, setNotes] = useState<NoteSummary[]>([]);
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");
  const [creating, setCreating] = useState(false);

  async function refresh(q: string) {
    try {
      const list = q.trim() ? await searchNotes(q) : await listNotes();
      setNotes(list);
    } catch (e) {
      setError(errorMessage(e));
    }
  }

  useEffect(() => {
    void refresh(query);
    // Intentionally omits `query` — this effect re-runs on mount and on
    // every refreshToken bump (create/delete/save elsewhere), using
    // whatever query is current at that moment; typing itself triggers
    // its own refresh via onChange below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refreshToken]);

  async function onCreate() {
    setError("");
    setCreating(true);
    try {
      const created = await createNote("Untitled", "");
      await refresh(query);
      onSelect(created.id);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setCreating(false);
    }
  }

  return (
    <div className="flex h-full w-64 shrink-0 flex-col border-r border-border">
      <div className="flex items-center gap-2 border-b border-border p-2">
        <Input
          aria-label="note-search"
          placeholder="Search notes..."
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            void refresh(e.target.value);
          }}
          className="h-8"
        />
        <Button
          type="button"
          size="icon-sm"
          variant="outline"
          aria-label="New note"
          disabled={creating}
          onClick={() => void onCreate()}
        >
          <Plus className="size-4" />
        </Button>
      </div>
      {error ? (
        <p role="alert" className="px-3 py-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
      <ScrollArea className="flex-1">
        <div className="flex flex-col gap-0.5 p-2">
          {notes.map((note) => (
            <button
              key={note.id}
              type="button"
              onClick={() => onSelect(note.id)}
              className={cn(
                "flex flex-col items-start gap-0.5 rounded-lg px-3 py-2 text-left",
                note.id === selectedId ? "bg-accent" : "hover:bg-accent/60",
              )}
            >
              <span className="w-full truncate text-sm font-medium text-foreground">
                {note.title || "Untitled"}
              </span>
              <span className="text-xs text-muted-foreground">{note.updatedAt}</span>
            </button>
          ))}
          {notes.length === 0 ? (
            <p className="px-3 py-2 text-xs text-muted-foreground">No notes yet.</p>
          ) : null}
        </div>
      </ScrollArea>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/notes/NoteList.tsx
git commit -m "feat(notes): add NoteList (search + create)"
```

---

### Task 11: `NoteEditor`

**Files:**
- Create: `frontend/src/modules/notes/NoteEditor.tsx`

- [ ] **Step 1: Write the component**

```tsx
// frontend/src/modules/notes/NoteEditor.tsx
import { useEffect, useRef, useState } from "react";
import { Trash2 } from "lucide-react";
import { Plate, usePlateEditor } from "platejs/react";
import { Button } from "@/components/ui/button";
import { Editor, EditorContainer } from "@/components/ui/editor";
import { Input } from "@/components/ui/input";
import { deleteNote, getNote, updateNote, type Note } from "../../bridge/notes";
import { errorMessage } from "../../bridge/errors";
import { NotesEditorKit } from "./plate/plugins";

type SaveStatus = "idle" | "saving" | "saved" | "error";

type NoteEditorProps = {
  noteId: string;
  onDeleted: () => void;
  onSaved: () => void;
};

const AUTOSAVE_DELAY_MS = 1500;

export function NoteEditor({ noteId, onDeleted, onSaved }: NoteEditorProps) {
  const [note, setNote] = useState<Note | null>(null);
  const [title, setTitle] = useState("");
  const [status, setStatus] = useState<SaveStatus>("idle");
  const [loadError, setLoadError] = useState("");
  const [saveError, setSaveError] = useState("");
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const editor = usePlateEditor({
    plugins: NotesEditorKit,
    value: [],
  });

  useEffect(() => {
    let mounted = true;
    setLoadError("");
    setStatus("idle");
    getNote(noteId)
      .then((n) => {
        if (!mounted) {
          return;
        }
        setNote(n);
        setTitle(n.title);
        const value = editor.api.markdown.deserialize(n.body);
        editor.tf.setValue(value);
      })
      .catch((e) => {
        if (mounted) {
          setLoadError(errorMessage(e));
        }
      });
    return () => {
      mounted = false;
      if (saveTimer.current) {
        clearTimeout(saveTimer.current);
      }
    };
    // Runs once per mount — NotesView remounts this component (via `key`)
    // whenever the selected note changes, so `noteId` is stable for the
    // lifetime of any given instance.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [noteId]);

  function scheduleSave(nextTitle: string) {
    if (saveTimer.current) {
      clearTimeout(saveTimer.current);
    }
    saveTimer.current = setTimeout(() => void save(nextTitle), AUTOSAVE_DELAY_MS);
  }

  async function save(nextTitle: string) {
    if (!note) {
      return;
    }
    setStatus("saving");
    setSaveError("");
    try {
      const body = editor.api.markdown.serialize();
      const updated = await updateNote(note.id, nextTitle, body);
      setNote(updated);
      setStatus("saved");
      onSaved();
    } catch (e) {
      setStatus("error");
      setSaveError(errorMessage(e));
    }
  }

  async function onDelete() {
    if (!note) {
      return;
    }
    try {
      await deleteNote(note.id);
      onDeleted();
    } catch (e) {
      setSaveError(errorMessage(e));
    }
  }

  if (loadError) {
    return (
      <div className="flex flex-1 items-center justify-center p-4">
        <p role="alert" className="text-sm text-destructive">
          {loadError}
        </p>
      </div>
    );
  }

  if (!note) {
    return (
      <div className="flex flex-1 items-center justify-center p-4">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-1 flex-col">
      <div className="flex items-center gap-2 border-b border-border px-4 py-2">
        <Input
          aria-label="note-title"
          value={title}
          onChange={(e) => {
            setTitle(e.target.value);
            scheduleSave(e.target.value);
          }}
          className="h-8 flex-1 border-none px-0 text-base font-semibold shadow-none focus-visible:ring-0"
        />
        <span className="text-xs text-muted-foreground">
          {status === "saving" ? "Saving…" : status === "saved" ? "Saved" : null}
        </span>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label="Delete note"
          onClick={() => void onDelete()}
        >
          <Trash2 className="size-4" />
        </Button>
      </div>
      {saveError ? (
        <p role="alert" className="border-b border-border px-4 py-1 text-xs text-destructive">
          {saveError}
        </p>
      ) : null}
      <Plate editor={editor} onChange={() => scheduleSave(title)}>
        <EditorContainer className="flex-1 overflow-auto">
          <Editor placeholder="Write something..." className="px-8 py-6" />
        </EditorContainer>
      </Plate>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/notes/NoteEditor.tsx
git commit -m "feat(notes): add NoteEditor (Plate wrapper, autosave, delete)"
```

---

### Task 12: Rewrite `NotesView` as a master-detail shell

**Files:**
- Modify: `frontend/src/modules/notes/NotesView.tsx` (full rewrite)

- [ ] **Step 1: Replace the file contents**

```tsx
// frontend/src/modules/notes/NotesView.tsx
import { useState } from "react";
import { NoteEditor } from "./NoteEditor";
import { NoteList } from "./NoteList";

export function NotesView() {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [refreshToken, setRefreshToken] = useState(0);

  return (
    <div className="flex h-full">
      <NoteList selectedId={selectedId} onSelect={setSelectedId} refreshToken={refreshToken} />
      {selectedId ? (
        <NoteEditor
          key={selectedId}
          noteId={selectedId}
          onDeleted={() => {
            setSelectedId(null);
            setRefreshToken((t) => t + 1);
          }}
          onSaved={() => setRefreshToken((t) => t + 1)}
        />
      ) : (
        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          Select a note or create a new one.
        </div>
      )}
    </div>
  );
}
```

The old stacked create-form/list/detail-pane layout (`<section className="notes-view">` with `.notes-create`/`.notes-list`/`.notes-detail`) is fully replaced — this component no longer references any of those CSS classes.

- [ ] **Step 2: Remove the now-dead CSS**

In `frontend/src/app.css`, remove the `.notes-view`, `.notes-create`, `.notes-search`, `.notes-list`, and `.notes-detail` rule blocks (everything between the `.tools-view` rules above them and the `.database-view` rules below them) — nothing in the rewritten `NotesView`/`NoteList`/`NoteEditor` uses these class names; they're all Tailwind utility classes now.

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bunx tsc --noEmit -p tsconfig.json`
Expected: no errors.

- [ ] **Step 4: Confirm no other file references the old class names or component shape**

Run: `grep -rn "notes-view\|notes-create\|notes-search\|notes-list\|notes-detail" frontend/src`
Expected: no matches.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/notes/NotesView.tsx frontend/src/app.css
git commit -m "feat(notes): rewrite NotesView as a master-detail shell"
```

---

### Task 13: Manual verification

**Files:** none — this task runs the app, no code changes.

No frontend test runner exists in this project. This is the actual verification for the whole plan. Use the `run` skill or start the Wails dev server directly (`task dev` from the repo root, per `Taskfile.yml`).

- [ ] **Step 1: Start the dev server and open the app, unlock the vault**

- [ ] **Step 2: Create a note**

Click "+" in the Notes module's list. Confirm a new "Untitled" note opens in the editor immediately.

- [ ] **Step 3: Confirm markdown-shortcut typing works**

Type `# ` at the start of a line — confirm it becomes a heading. Type `**bold**` — confirm it becomes bold text. Type `- ` — confirm it becomes a bullet list item.

- [ ] **Step 4: Confirm the slash-command menu**

Type `/` on an empty line — confirm a menu appears; select "Table" (or another block type) and confirm it inserts correctly.

- [ ] **Step 5: Confirm the floating selection toolbar**

Select some text — confirm a formatting toolbar appears near the selection (not a fixed bar at the top), and that it disappears when the selection is cleared.

- [ ] **Step 6: Confirm a Mermaid diagram renders**

Insert a code block, set its language to `mermaid` (or type ` ```mermaid ` then Enter, per the code block's markdown shortcut), and enter:
```
graph TD
  A[Start] --> B[End]
```
Confirm it renders as an actual diagram, not syntax-highlighted text. Click "Edit" — confirm it switches to raw source. Click "Preview" — confirm it switches back to the rendered diagram.

- [ ] **Step 7: Confirm autosave**

Type some text, wait ~2 seconds without typing, confirm the status indicator shows "Saved". Switch to a different note in the list, then switch back — confirm the edited content persisted (this is the real proof `NotesUpdate` round-trips correctly, not just that the UI looks right).

- [ ] **Step 8: Confirm an oversized image is rejected**

Try inserting an image file larger than 4MB (via drag-drop or the image block's upload control) — confirm it's rejected with a visible message, not silently ignored.

- [ ] **Step 9: Confirm delete**

Click the delete (trash) icon in a note's header — confirm the note disappears from the list and the editor pane returns to the "Select a note..." empty state.

- [ ] **Step 10: Confirm search**

Type a query into the search box that matches a note's title or body text — confirm the list filters to matching notes only; clear the query — confirm the full list returns.

- [ ] **Step 11: Note results**

If anything fails, fix it and re-run the relevant earlier task's typecheck/test step before re-verifying here. No commit for this task unless a fix was needed, in which case commit that fix with a `fix(notes): ...` message describing what broke.

---

## Self-Review

**Spec coverage:**
- Update path (Store/Service/bridge/MCP) — Tasks 1-4. ✓
- Plate editor, markdown storage — Tasks 6, 7, 11 (deserialize on load, serialize on save). ✓
- Extended plugin set (headings/marks/lists/toggle/quote/code/table/image/divider/link) — Task 6 install + Task 7 kit composition. ✓
- Images as base64 data URI, size-capped — Task 8. ✓
- Mermaid via custom code_block renderer, Edit/Preview toggle — Task 9. ✓
- Master-detail layout, floating selection toolbar (no fixed bar) — Task 12 (layout), Task 6's `@plate/plate-ui` bundle (floating toolbar is Plate's default UX per the earlier brainstorming research; `@plate/fixed-toolbar` was installed in Task 6 Step 3 only because `plate-ui`'s bundle may reference it as a shared dependency of other components — Task 12's `NoteEditor` deliberately never renders a `<FixedToolbar>`, matching the chosen design). ✓
- Autosave, 1.5s debounce, "Saving…/Saved" indicator — Task 11. ✓
- Error handling: autosave failure keeps local edits + shows error (Task 11's `saveError` state, never clears editor content on failure); note-deleted-elsewhere surfaces as a `NotesUpdate` failure through the same `saveError` path; oversized image rejected client-side (Task 8); Mermaid syntax error shown inline (Task 9). ✓

**Placeholder scan:** no TBD/TODO; the two spots referencing "Task 6's actual output" (Tasks 7 and 9) are not placeholders — they're a stated verification procedure with concrete fallback code given, for the one part of this plan (a live third-party CLI scaffold) that can't be fully pinned down from documentation alone, exactly like Task 5's Wails-binding-regeneration step.

**Type consistency:** `Note` (`{id, title, body}`) and `NoteSummary` (`{id, title, updatedAt}`) from `frontend/src/bridge/notes.ts` are used identically across `NoteList`/`NoteEditor`/`NotesView`. `updateNote(id, title, body): Promise<Note>` (Task 5) matches the call site in `NoteEditor.save()` (Task 11) exactly. Go-side `Update(id, title, body string) (Note, error)` on `notes.Service` (Task 2) is called identically from the bridge (Task 3) and MCP tool (Task 4), and `vaultitem.Store.Update`'s signature (Task 1) matches the `notes.Store` interface addition (Task 2) exactly.
