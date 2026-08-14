# Notes Dashboard Content-Area Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the Notes dashboard's content area (header + note grid — not the sidebar, not the editor) a bolder page header and a one-notch-more-spacious card, and turn `NoteCard` tags into icon pills — borrowing the spacing/typography quality of a reference task-detail panel without copying its components.

**Architecture:** Pure frontend, no backend/data-model change, no change to scope-filtering/tag-filtering logic. A new header block (title + live count/scope subtitle + hairline divider) is inserted above the existing toolbar row in `NotesDashboard.tsx`; `NoteCard.tsx`'s outer radius/padding, title size, and tag chip shape move one notch up, still snapped to existing DESIGN.md tokens (no new arbitrary values, no new colors).

**Tech Stack:** React + TypeScript + Tailwind v4, existing `tagSoftBackground`/`noteAccentColor` helpers (unchanged), lucide-react `Tag` icon (new import).

## Global Constraints

- No new CSS colors or alpha formulas — `tagSoftBackground` (15% alpha) and `noteAccentColor` are reused exactly as they are today.
- Every new size value must be an existing Tailwind/DESIGN.md token (`rounded-xl`, `rounded-md`, `p-4`, `gap-4`, `h-9`, `text-base`/`text-2xl`) — no arbitrary bracket pixel values introduced in this pass.
- Do not touch the sidebar (`Sidebar`/`SidebarGroup`/etc. tree) or `NoteEditor.tsx` — this plan only touches the dashboard's header/toolbar/grid and `NoteCard`.
- Do not change `createFolder`/`updateFolder`/`deleteFolder`/scope-filtering/tag-filtering logic — only rendering.
- Spec: `docs/superpowers/specs/2026-08-15-notes-content-area-design.md`.
- `docs/superpowers/*` is gitignored; `git add -f` plan/spec files if `git status` does not stage them.

## File structure

- `frontend/src/modules/notes/NotesDashboard.tsx` — new `scopeLabel` derived value; new header block + hairline divider inserted above the toolbar row; search input height and grid gap bumped.
- `frontend/src/modules/notes/NoteCard.tsx` — outer radius/padding, title typography, and tag chip shape/icon change.

---

### Task 1: Dashboard header + spacing bump

**Files:**
- Modify: `frontend/src/modules/notes/NotesDashboard.tsx`

**Interfaces:**
- Consumes: existing `scope`, `folders`, `filtered` state/derived values (no new state)
- Produces: a `scopeLabel` derived string; a page header (`Notes` + `{count} note(s) · {scopeLabel}`) rendered above a hairline divider and the existing toolbar

- [ ] **Step 1: Add the `scopeLabel` derived value**

In `frontend/src/modules/notes/NotesDashboard.tsx`, add this alongside the other `useMemo`s (e.g. right after the `counts` memo):

```tsx
  const scopeLabel = useMemo(() => {
    if (scope === "all") return "All";
    if (scope === "ungrouped") return "Ungrouped";
    const folderId = scope.slice(7);
    return folders.find((f) => f.id === folderId)?.name ?? "All";
  }, [scope, folders]);
```

- [ ] **Step 2: Insert the header block and divider above the toolbar**

Immediately before the toolbar `<div className="flex flex-wrap items-center gap-2 border-b border-border p-3">` (inside the `<section className="flex min-h-0 flex-1 flex-col">`), insert:

```tsx
          <div className="flex items-end justify-between gap-2 px-4 pt-4 pb-3">
            <div>
              <h1 className="text-2xl font-bold tracking-tight text-foreground">
                Notes
              </h1>
              <p className="mt-1.5 text-[13px] text-muted-foreground">
                {filtered.length} {filtered.length === 1 ? "note" : "notes"} ·{" "}
                {scopeLabel}
              </p>
            </div>
          </div>
          <div className="border-b border-border" />
```

- [ ] **Step 3: Bump the search input height**

Change the search `Input`'s className:

```tsx
              className="h-9 max-w-sm"
```

(was `className="h-8 max-w-sm"`)

- [ ] **Step 4: Bump the grid gap**

Change the grid `<div>`'s className:

```tsx
            <div className="grid grid-cols-[repeat(auto-fill,minmax(13rem,1fr))] gap-4 p-3">
```

(was `gap-3.5`)

- [ ] **Step 5: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 6: Manual check**

Run the app, open Notes: bold "Notes" header with a live count/scope subtitle appears above a hairline divider, above the toolbar; switching sidebar scope updates the subtitle; search bar and toolbar read slightly taller; grid gutters are visibly wider.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/modules/notes/NotesDashboard.tsx
git commit -m "feat(notes): add dashboard page header and widen grid spacing"
```

---

### Task 2: `NoteCard` — larger radius/padding, bolder title, icon-pill tags

**Files:**
- Modify: `frontend/src/modules/notes/NoteCard.tsx`

**Interfaces:**
- Consumes: `Tag` icon (new, `lucide-react`); `tagSoftBackground`/`noteAccentColor` (unchanged)
- Produces: same `NoteCard({ note, onOpen, onTagClick })` public signature; visually larger card with icon-pill tags

- [ ] **Step 1: Replace the file**

Replace the full contents of `frontend/src/modules/notes/NoteCard.tsx`:

```tsx
import { Tag } from "lucide-react";
import type { NoteSummary } from "../../bridge/notes";
import { noteAccentColor } from "./noteColor";
import { tagSoftBackground } from "./tagColor";

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
  const accent = noteAccentColor(note.effectiveColor);
  return (
    <button
      type="button"
      onClick={() => onOpen(note.id)}
      className="flex aspect-3/2 flex-col gap-1.5 rounded-xl border border-border bg-card p-4 text-left"
    >
      <span className="flex min-w-0 items-center gap-1.5">
        {accent ? (
          <span
            className="size-2 shrink-0 rounded-[4px]"
            style={{ background: accent }}
            aria-hidden
          />
        ) : null}
        <span className="min-w-0 truncate text-base font-semibold tracking-[0.2px] text-foreground">
          {note.title || "Untitled"}
        </span>
      </span>
      <span className="line-clamp-3 flex-1 text-[13px] leading-[1.4] tracking-[0.1px] text-muted-foreground">
        {note.preview || "No preview"}
      </span>
      <span className="flex items-center justify-between gap-2">
        <span className="flex flex-wrap gap-1">
          {note.effectiveTags.map((tag) => (
            <span
              key={tag.toLowerCase()}
              role="button"
              tabIndex={0}
              className="inline-flex items-center gap-1 rounded-md px-2.5 py-1 text-xs font-semibold tracking-[0.4px]"
              style={tagSoftBackground(tag)}
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
              <Tag className="size-2.5" />
              {tag}
            </span>
          ))}
        </span>
        <span className="shrink-0 text-xs tracking-[0.4px] text-muted-foreground">
          {relativeTime(note.updatedAt)}
        </span>
      </span>
    </button>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add frontend/src/modules/notes/NoteCard.tsx
git commit -m "feat(notes): widen NoteCard and turn tags into icon pills"
```

---

### Task 3: End-to-end verification

**Files:** none new

- [ ] **Step 1: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 2: Full manual script**

1. Dashboard header shows "Notes" + live `{count} note(s) · {scope}` subtitle above a hairline divider.
2. Switching sidebar scope (All / Ungrouped / a folder) updates the header subtitle live; typing a search query or toggling a tag filter updates the count too (since it reads `filtered.length`).
3. Cards are visibly larger/airier (14px corner radius, 16px padding, 16px grid gutters), still fixed at a 3:2 aspect ratio.
4. Card titles are bolder/larger (16px/600) and still truncate on one line, never wrap.
5. Tags render as pills with a small leading tag icon and the same `-soft` background/color as before; clicking one still toggles the AND tag filter.
6. No visual/behavioral change to the sidebar or the editor.

If anything fails, fix in a focused commit, then re-run the failing check.
