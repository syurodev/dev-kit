# Notes DESIGN.md Conformance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring the Notes module's dashboard/card/editor chrome onto `frontend/DESIGN.md`'s actual tokens (radius, type scale, `-soft` alpha convention, no drop-shadow), and scope Inter + `ss03` to Notes only.

**Architecture:** Pure frontend, no backend/data-model change. Two small color helpers replace the old panel-alpha helper; `NoteCard` drops its inset "mini-window" panel for a flat `feature-card-dark`-equivalent surface with a small solid-color accent dot; the sidebar's row class and the note-color `Popover` get token-accurate tweaks; a single unlayered `.notes-font-scope` CSS class carries Inter + `ss03` onto the two Notes screen roots.

**Tech Stack:** React + TypeScript + Tailwind v4 CSS variables (`color-mix()` on the existing `--note-folder-*` / `--note-tag-*` tokens), `@fontsource-variable/inter` (already imported, unused until now).

## Global Constraints

- No new CSS custom properties — every value maps to an existing `--note-folder-*`/`--note-tag-*` token or an existing Tailwind theme radius (`rounded-sm`/`rounded-md`/`rounded-lg`).
- Do not edit `frontend/src/components/ui/{button,input,dialog,popover}.tsx` — any override is a `className` at the Notes call site only.
- Do not touch `--font-sans` or any global font setting — the Inter/`ss03` scope is additive and Notes-only, via `.notes-font-scope`.
- Tags render from `--note-tag-0`..`--note-tag-7` (`tagColor.ts`, hashed from the tag name) — **not** `--note-folder-*`. The note/folder accent dot uses `--note-folder-*` (`noteColor.ts`). These are two different palettes; don't cross them.
- Spec: `docs/superpowers/specs/2026-08-14-notes-design-system-conformance-design.md`.
- `docs/superpowers/*` is gitignored; `git add -f` plan/spec files if `git status` does not stage them.

## File structure

- `frontend/src/index.css` — add `.notes-font-scope` (Inter + `ss03`, unlayered so it beats the inherited Geist `font-sans` from `@layer base`).
- `frontend/src/modules/notes/noteColor.ts` — replace `notePanelStyle` with `noteAccentColor(color)` (plain `var(--note-folder-<x>)` string or `undefined`).
- `frontend/src/modules/notes/tagColor.ts` — add `tagSoftBackground(name)` (15%-alpha background + full-color text, for tag badges).
- `frontend/src/modules/notes/NoteCard.tsx` — rewrite: flat card, accent dot, soft tag badges, DESIGN.md type scale.
- `frontend/src/modules/notes/NotesDashboard.tsx` — sidebar row radius/type-scale fix; add `.notes-font-scope` to the root.
- `frontend/src/modules/notes/NoteEditor.tsx` — `shadow-none` on the color `PopoverContent`; add `.notes-font-scope` to the root.

---

### Task 1: `.notes-font-scope` CSS

**Files:**
- Modify: `frontend/src/index.css`

**Interfaces:**
- Consumes: `@fontsource-variable/inter` (already imported at the top of this file)
- Produces: a `.notes-font-scope` class other Notes tasks apply to their screen roots

- [ ] **Step 1: Add the rule**

In `frontend/src/index.css`, insert immediately after the closing `}` of the existing `@layer base { ... }` block (the block ending at line 139, right after the `html { @apply font-sans; ... }` rule):

```css

/* Unlayered on purpose: Tailwind's own `.font-sans` utility (and the
   `@apply font-sans` on `html` above) live inside Tailwind's cascade
   layers, and unlayered author styles always win over layered ones
   regardless of source order. This lets Notes opt into Inter + ss03
   without touching the app-wide --font-sans (Geist) every other module
   still inherits. */
.notes-font-scope {
  font-family: 'Inter Variable', sans-serif;
  font-feature-settings: "calt", "kern", "liga", "ss03";
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS (CSS-only change, no TS surface touched — this just confirms nothing else broke)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/index.css
git commit -m "feat(notes): add Inter+ss03 font scope for the Notes module"
```

---

### Task 2: Color helpers — `noteAccentColor` and `tagSoftBackground`

**Files:**
- Modify: `frontend/src/modules/notes/noteColor.ts`
- Modify: `frontend/src/modules/notes/tagColor.ts`

**Interfaces:**
- Consumes: `FOLDER_COLORS`/`FolderColor` from `./folderColors`
- Produces:
  - `noteAccentColor(color: string): string | undefined` — replaces `notePanelStyle`; returns `var(--note-folder-<x>)` for a known palette color, `undefined` otherwise
  - `tagSoftBackground(name: string): CSSProperties` — `{ backgroundColor: color-mix(... 15% ...), color: <full-saturation var> }`, matching DESIGN.md's `accent-*-soft` convention (15% alpha fill + full-color text)

- [ ] **Step 1: Replace `noteColor.ts`**

Replace the full contents of `frontend/src/modules/notes/noteColor.ts`:

```ts
import { FOLDER_COLORS, type FolderColor } from "./folderColors";

export function noteAccentColor(color: string): string | undefined {
  if (!FOLDER_COLORS.includes(color as FolderColor)) {
    return undefined;
  }
  return `var(--note-folder-${color})`;
}
```

- [ ] **Step 2: Add `tagSoftBackground` to `tagColor.ts`**

Append to `frontend/src/modules/notes/tagColor.ts` (keep the existing `tagChipVar` function as-is — it's still used by `TagChip` in `NotesDashboard.tsx` and `NoteEditor.tsx`):

```ts
import type { CSSProperties } from "react";

export function tagSoftBackground(name: string): CSSProperties {
  const accent = tagChipVar(name);
  return {
    backgroundColor: `color-mix(in oklch, ${accent} 15%, transparent)`,
    color: accent,
  };
}
```

Add the `import type { CSSProperties } from "react";` line at the top of the file, above the existing code.

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: FAIL — `NoteCard.tsx` still imports the now-deleted `notePanelStyle` from `noteColor.ts`. This is expected; Task 3 fixes the only call site.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/notes/noteColor.ts frontend/src/modules/notes/tagColor.ts
git commit -m "feat(notes): add DESIGN.md-conformant accent/soft color helpers"
```

---

### Task 3: `NoteCard.tsx` — flat card, accent dot, soft tag badges

**Files:**
- Modify: `frontend/src/modules/notes/NoteCard.tsx`

**Interfaces:**
- Consumes: `noteAccentColor` (Task 2), `tagSoftBackground` (Task 2), `NoteSummary` (unchanged)
- Produces: same `NoteCard({ note, onOpen, onTagClick })` public signature; visually replaces the inset colored panel with a flat `bg-card` surface + accent dot + soft tag badges

- [ ] **Step 1: Replace the file**

Replace the full contents of `frontend/src/modules/notes/NoteCard.tsx`:

```tsx
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
      className="flex aspect-[3/2] flex-col gap-1.5 rounded-lg border border-border bg-card p-3 text-left"
    >
      <span className="flex items-center gap-1.5">
        {accent ? (
          <span
            className="size-2 shrink-0 rounded-[4px]"
            style={{ background: accent }}
            aria-hidden
          />
        ) : null}
        <span className="truncate text-sm font-medium tracking-[0.2px] text-foreground">
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
              className="rounded-[4px] px-1.5 py-0.5 text-xs font-medium tracking-[0.4px]"
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
git commit -m "feat(notes): flatten NoteCard to DESIGN.md's feature-card-dark shape"
```

---

### Task 4: Sidebar row tokens + font scope on the dashboard

**Files:**
- Modify: `frontend/src/modules/notes/NotesDashboard.tsx`

**Interfaces:**
- Consumes: nothing new
- Produces: sidebar rows use `{rounded.sm}` (6px) and `{typography.body-md}` (16px); row counts use `{typography.caption-sm}` (12px/0.4px tracking); dashboard root carries `.notes-font-scope`

- [ ] **Step 1: Update `scopeButtonClass`**

In `frontend/src/modules/notes/NotesDashboard.tsx`, replace the `scopeButtonClass` function (originally lines 343–348):

```tsx
  function scopeButtonClass(active: boolean) {
    return cn(
      "flex w-full items-center gap-2 rounded-sm px-2.5 py-1.5 text-left text-base tracking-[0.2px]",
      active ? "bg-accent text-accent-foreground" : "hover:bg-accent/60",
    );
  }
```

(Only `rounded-lg` → `rounded-sm` and `text-sm` → `text-base tracking-[0.2px]` changed — the active/inactive background logic is untouched; `bg-accent` already resolves to the same `#121212` DESIGN.md calls `surface-card`.)

- [ ] **Step 2: Update the three count spans**

Each of the three `<span className="text-xs text-muted-foreground">` counters (All, Ungrouped, per-folder — originally lines 361, 369, 385–387) gains the `caption-sm` letter-spacing:

```tsx
              <span className="text-xs tracking-[0.4px] text-muted-foreground">{counts.all}</span>
```
```tsx
              <span className="text-xs tracking-[0.4px] text-muted-foreground">{counts.ungrouped}</span>
```
```tsx
                  <span className="text-xs tracking-[0.4px] text-muted-foreground">
                    {counts.byFolder.get(folder.id) ?? 0}
                  </span>
```

- [ ] **Step 3: Add the font scope to the root**

Change the component's root element (originally line 351):

```tsx
    <div className="notes-font-scope flex h-full min-h-0 flex-1">
```

- [ ] **Step 4: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add frontend/src/modules/notes/NotesDashboard.tsx
git commit -m "feat(notes): apply command-palette-row tokens to the sidebar"
```

---

### Task 5: Editor — no-shadow color popover + font scope

**Files:**
- Modify: `frontend/src/modules/notes/NoteEditor.tsx`

**Interfaces:**
- Consumes: nothing new
- Produces: the note color `Popover` renders without the shared `Popover` component's default `shadow-md`; editor root carries `.notes-font-scope`

- [ ] **Step 1: Strip the popover shadow**

In `frontend/src/modules/notes/NoteEditor.tsx`, change the `PopoverContent`:

```tsx
          <PopoverContent className="w-auto shadow-none">
```

- [ ] **Step 2: Add the font scope to the root**

Change the component's root element (the `return (` block's outermost `<div>`):

```tsx
    <div className="notes-font-scope flex h-full flex-1 flex-col">
```

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/notes/NoteEditor.tsx
git commit -m "feat(notes): drop the color popover shadow, apply font scope"
```

---

### Task 6: End-to-end verification

**Files:** none new

- [ ] **Step 1: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 2: Manual script**

Run the app, open Notes:

1. Note cards are flat rectangles (`bg-card`, hairline border, 10px corner radius) — no inset colored panel anywhere.
2. A note with an effective color shows a small 8×8px solid-color square dot to the left of its title; a note with no effective color shows no dot at all.
3. Tags on a card render as soft-background badges (subtle tinted fill, full-saturation text) — not the previous dot-and-label chip.
4. Long titles still truncate on one line (unchanged rule from the prior spec) at the new `text-sm` size.
5. Sidebar: active row background is the same `#121212` as before (unchanged color, just tighter 6px corner radius and 16px row text).
6. Opening the note's color `Popover` (the palette-icon button in the editor header) shows a hairline-bordered panel with **no** drop-shadow underneath it.
7. Text inside Notes (dashboard, cards, editor) visibly renders in Inter — distinct in feel from every other module (Database/SSH/Kafka/Settings), which stay on Geist. No visual change outside `frontend/src/modules/notes/`.
8. Tag AND-filter, folder create/rename/delete, note create/delete, autosave, and the folder/tag/color editor controls all still work exactly as before — this plan only changes visual chrome, not behavior.

If anything fails, fix in a focused commit on that layer, then re-run the failing command.
