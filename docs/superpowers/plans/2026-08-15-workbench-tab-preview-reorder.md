# Workbench Tab Preview/Pin, Icon Override, Drag-Reorder Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Notes real per-note tabs (VS Code-style preview/pin lifecycle), let any module override a tab's icon, and let the user drag tabs to reorder them.

**Architecture:** `useWorkbenchTabsStore` gains a `preview` flag (one global replaceable "preview" tab, promoted to permanent by an explicit non-preview open or a new `pinTab` action) and an `icon` override field, plus a `reorderTab` splice action. `TabStrip` renders the preview state (italic) and icon override, and drives `reorderTab` from hand-rolled `pointerdown`/global `pointermove`/`pointerup` tracking — not framer-motion's `Reorder.Group`, which isn't usable through the `LazyMotion`/`m` lazy-loading pattern this codebase already commits to for bundle size. The existing `layout` spring (already on every tab) animates the reflow with zero new animation code. Notes becomes the first per-item-tab consumer: `NotesView` stops managing dashboard↔editor switching itself and instead opens tabs through the store; `NoteEditor` becomes a `ModuleView` in its own right (`notes.editor`, reading `params.noteId`) instead of a component `NotesView` renders directly.

**Tech Stack deviation from the skill default (same as Phase 1):** no frontend unit test runner in this repo — verification is `cd frontend && bun run build` (tsc + vite) plus a manual QA pass (final task).

**Builds on:** `docs/superpowers/plans/2026-08-15-workbench-tab-strip.md` (Phase 1, implemented) and `docs/superpowers/specs/2026-08-15-workbench-tab-preview-reorder-design.md` (the approved design this plan implements — read it for the full Decisions/Non-goals record if anything here seems under-motivated).

---

## File Structure

Create:
- `frontend/src/stores/notesRefresh.ts` — tiny Zustand store (`token`, `bump()`), same minimal shape as `dock-store.ts`. Needed because `NotesDashboard` and `NoteEditor` are becoming two independently-mounted tab components (no longer parent/child), so the old "bump a `refreshToken` prop" wiring needs a shared store instead.

Modify:
- `frontend/src/stores/workbenchTabs.ts` — add `icon`/`preview` fields, rewrite `openTab`, add `pinTab`/`reorderTab`.
- `frontend/src/workbench/tabs/TabStrip.tsx` — italic preview styling, icon override fallback, drag-to-reorder.
- `frontend/src/workbench/Workbench.tsx` — wire `reorderTab` into `<TabStrip>`.
- `frontend/src/modules/notes/NoteCard.tsx` — pass the whole note (not just its id) to open handlers; add double-click.
- `frontend/src/modules/notes/NotesDashboard.tsx` — `onOpenNote`/`onOpenNoteDouble` prop rename+retype; new-note creation opens pinned, not preview.
- `frontend/src/modules/notes/NoteEditor.tsx` — becomes a real `ModuleView` component (`{ params }` instead of `{ noteId, onBack, onDeleted, onSaved }`), wires its own back/delete/save behavior through the tabs + notes-refresh stores, pins its own tab on first edit.
- `frontend/src/modules/notes/manifest.ts` — register `notes.editor` as a second view.
- `frontend/src/modules/notes/NotesView.tsx` — drops the internal dashboard↔editor switch; becomes `NotesDashboard` wired to the tabs store + notes-refresh store.

---

### Task 1: Preview/pin lifecycle + icon override + reorder in the tabs store

**Files:**
- Modify: `frontend/src/stores/workbenchTabs.ts`

- [ ] **Step 1: Rewrite the file**

Replace the full contents of `frontend/src/stores/workbenchTabs.ts` with:

```ts
import { create } from "zustand";
import type { LucideIcon } from "lucide-react";

export type TabStatus = "idle" | "running" | "success" | "error";

export type WorkbenchTab = {
  id: string;
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  status?: TabStatus;
  icon?: LucideIcon;
  preview?: boolean;
};

export type OpenTabInput = {
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  icon?: LucideIcon;
  preview?: boolean;
};

/**
 * Stable identity for a tab: same moduleId+viewId+params always resolves to
 * the same tab, so opening an already-open note (or module) focuses the
 * existing tab instead of duplicating it. Params are sorted by key so
 * `{a:1,b:2}` and `{b:2,a:1}` collide on purpose.
 */
export function tabKey(
  moduleId: string,
  viewId: string,
  params?: Record<string, unknown>,
): string {
  const keys = params ? Object.keys(params).sort() : [];
  if (keys.length === 0) {
    return `${moduleId}:${viewId}`;
  }
  const suffix = keys.map((key) => `${key}=${String(params![key])}`).join("&");
  return `${moduleId}:${viewId}:${suffix}`;
}

type WorkbenchTabsState = {
  openTabs: WorkbenchTab[];
  activeTabId: string;
  openTab: (input: OpenTabInput) => string;
  closeTab: (id: string) => void;
  setActiveTab: (id: string) => void;
  setTabStatus: (id: string, status: TabStatus) => void;
  pinTab: (id: string) => void;
  reorderTab: (fromId: string, toIndex: number) => void;
};

export const useWorkbenchTabsStore = create<WorkbenchTabsState>((set, get) => ({
  openTabs: [],
  activeTabId: "",

  openTab: (input) => {
    const id = tabKey(input.moduleId, input.viewId, input.params);
    const state = get();
    const existingIndex = state.openTabs.findIndex((tab) => tab.id === id);

    if (existingIndex !== -1) {
      const existing = state.openTabs[existingIndex];
      if (input.preview === false && existing.preview) {
        // Explicit non-preview open of the tab that's currently the
        // preview tab (double-click, or first edit via pinTab elsewhere)
        // promotes it in place — same slot, no reorder.
        const openTabs = state.openTabs.slice();
        openTabs[existingIndex] = { ...existing, preview: false };
        set({ openTabs, activeTabId: id });
        return id;
      }
      set({ activeTabId: id });
      return id;
    }

    const tab: WorkbenchTab = {
      id,
      moduleId: input.moduleId,
      viewId: input.viewId,
      title: input.title,
      params: input.params,
      icon: input.icon,
      preview: input.preview ?? false,
    };

    if (input.preview) {
      const previewIndex = state.openTabs.findIndex((t) => t.preview);
      if (previewIndex !== -1) {
        // Replace the current preview tab in place — same array index —
        // so the strip doesn't jump when browsing via single-click.
        const openTabs = state.openTabs.slice();
        openTabs[previewIndex] = tab;
        set({ openTabs, activeTabId: id });
        return id;
      }
    }

    set((s) => ({ openTabs: [...s.openTabs, tab], activeTabId: id }));
    return id;
  },

  closeTab: (id) => {
    set((state) => {
      const index = state.openTabs.findIndex((tab) => tab.id === id);
      if (index === -1) {
        return state;
      }
      const openTabs = state.openTabs.filter((tab) => tab.id !== id);
      if (state.activeTabId !== id) {
        return { openTabs };
      }
      // Closing the active tab focuses its former left neighbor, matching
      // VS Code — falling back to the new first tab if we closed index 0.
      const fallback = openTabs[index - 1] ?? openTabs[0];
      return { openTabs, activeTabId: fallback?.id ?? "" };
    });
  },

  setActiveTab: (id) => set({ activeTabId: id }),

  setTabStatus: (id, status) =>
    set((state) => ({
      openTabs: state.openTabs.map((tab) => (tab.id === id ? { ...tab, status } : tab)),
    })),

  pinTab: (id) =>
    set((state) => ({
      openTabs: state.openTabs.map((tab) =>
        tab.id === id ? { ...tab, preview: false } : tab,
      ),
    })),

  reorderTab: (fromId, toIndex) =>
    set((state) => {
      const fromIndex = state.openTabs.findIndex((tab) => tab.id === fromId);
      if (fromIndex === -1 || fromIndex === toIndex) {
        return state;
      }
      const openTabs = state.openTabs.slice();
      const [moved] = openTabs.splice(fromIndex, 1);
      openTabs.splice(toIndex, 0, moved);
      return { openTabs };
    }),
}));
```

This is backward compatible with every Phase 1 caller: `Dock`'s `focusModule` and the Settings icon both call `openTab({moduleId, viewId, title})` with no `preview`/`icon` fields, which is exactly the "preview: false/omitted, tab doesn't exist yet" branch — unchanged behavior, byte-for-byte the same resulting `WorkbenchTab` shape those call sites produced before (just with two new `undefined`-valued optional fields).

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds. `TabStrip.tsx` already reads `tab.status`/renders tabs from this store — confirm no type errors are introduced by the two new optional fields (there shouldn't be; optional fields widen, never narrow, existing consumers).

- [ ] **Step 3: Commit**

```bash
git add frontend/src/stores/workbenchTabs.ts
git commit -m "feat(workbench): add tab preview/pin lifecycle, icon override, reorder"
```

---

### Task 2: TabStrip — preview styling + icon override

**Files:**
- Modify: `frontend/src/workbench/tabs/TabStrip.tsx`

- [ ] **Step 1: Italic title for preview tabs**

Find:

```tsx
              <span className="ml-1.5 max-w-40 truncate">{tab.title}</span>
```

Replace with:

```tsx
              <span className={cn("ml-1.5 max-w-40 truncate", tab.preview && "italic")}>
                {tab.title}
              </span>
```

- [ ] **Step 2: Icon override**

Find:

```tsx
              {(() => {
                // Same self-registered icon the module's Dock entry uses
                // (modules/registry.ts ModuleManifest.icon) — one source of
                // truth, no separate tab-strip-only icon map to drift.
                const ModuleIcon = getModule(tab.moduleId)?.icon ?? Wrench;
                return <ModuleIcon className="size-3.5 shrink-0 opacity-70" />;
              })()}
```

Replace with:

```tsx
              {(() => {
                // A tab-level `icon` (set by whoever called openTab) wins
                // over the module's self-registered default
                // (modules/registry.ts ModuleManifest.icon) — no separate
                // tab-strip-only icon map to drift either way.
                const ModuleIcon = tab.icon ?? getModule(tab.moduleId)?.icon ?? Wrench;
                return <ModuleIcon className="size-3.5 shrink-0 opacity-70" />;
              })()}
```

- [ ] **Step 3: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds. No consumer sets `tab.icon`/`tab.preview` yet (Task 5 is the first), so this is a purely additive, currently-inert code path — nothing changes visually until Task 5 lands.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/workbench/tabs/TabStrip.tsx
git commit -m "feat(workbench): render tab preview state and icon override"
```

---

### Task 3: Drag-to-reorder tabs

**Files:**
- Modify: `frontend/src/workbench/tabs/TabStrip.tsx`
- Modify: `frontend/src/workbench/Workbench.tsx`

- [ ] **Step 1: Add the `onReorder` prop and drag tracking**

In `frontend/src/workbench/tabs/TabStrip.tsx`, find the imports and props type:

```tsx
import { AnimatePresence, LazyMotion, domMax, m } from "framer-motion";
import { Wrench, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { getModule } from "../../modules/registry";
import type { TabStatus, WorkbenchTab } from "../../stores/workbenchTabs";
```

Add `useEffect`/`useRef` from React:

```tsx
import { useEffect, useRef } from "react";
import { AnimatePresence, LazyMotion, domMax, m } from "framer-motion";
import { Wrench, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { getModule } from "../../modules/registry";
import type { TabStatus, WorkbenchTab } from "../../stores/workbenchTabs";
```

Find:

```tsx
type TabStripProps = {
  tabs: WorkbenchTab[];
  activeTabId: string;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
};
```

Replace with:

```tsx
type TabStripProps = {
  tabs: WorkbenchTab[];
  activeTabId: string;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onReorder: (fromId: string, toIndex: number) => void;
};
```

- [ ] **Step 2: Drag tracking inside the component**

Find:

```tsx
export function TabStrip({ tabs, activeTabId, onSelect, onClose }: TabStripProps) {
  return (
```

Replace with:

```tsx
export function TabStrip({ tabs, activeTabId, onSelect, onClose, onReorder }: TabStripProps) {
  const tabRefs = useRef(new Map<string, HTMLButtonElement>());
  const dragRef = useRef<{ id: string; startX: number; dragging: boolean } | null>(null);

  // Global (not per-element) listeners: once a drag starts, the pointer can
  // move anywhere in the window, not just over the tab strip.
  useEffect(() => {
    function handlePointerMove(event: PointerEvent) {
      const drag = dragRef.current;
      if (!drag) {
        return;
      }
      if (!drag.dragging) {
        if (Math.abs(event.clientX - drag.startX) < 5) {
          return;
        }
        drag.dragging = true;
      }
      const draggedIndex = tabs.findIndex((tab) => tab.id === drag.id);
      if (draggedIndex === -1) {
        return;
      }
      for (let i = 0; i < tabs.length; i += 1) {
        if (i === draggedIndex) {
          continue;
        }
        const el = tabRefs.current.get(tabs[i].id);
        if (!el) {
          continue;
        }
        const rect = el.getBoundingClientRect();
        const mid = rect.left + rect.width / 2;
        const crossedGoingRight = i > draggedIndex && event.clientX > mid;
        const crossedGoingLeft = i < draggedIndex && event.clientX < mid;
        if (crossedGoingRight || crossedGoingLeft) {
          onReorder(drag.id, i);
          break;
        }
      }
    }

    function handlePointerUp() {
      dragRef.current = null;
    }

    document.addEventListener("pointermove", handlePointerMove);
    document.addEventListener("pointerup", handlePointerUp);
    return () => {
      document.removeEventListener("pointermove", handlePointerMove);
      document.removeEventListener("pointerup", handlePointerUp);
    };
  }, [tabs, onReorder]);

  return (
```

- [ ] **Step 3: Wire pointerdown + ref on each tab**

Find:

```tsx
            <m.button
              key={tab.id}
              layout
              initial={{ opacity: 0, scale: 0.9 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.9 }}
              transition={{ layout: { type: "spring", stiffness: 500, damping: 35 } }}
              type="button"
              onClick={() => onSelect(tab.id)}
```

Replace with:

```tsx
            <m.button
              key={tab.id}
              ref={(el) => {
                if (el) {
                  tabRefs.current.set(tab.id, el);
                } else {
                  tabRefs.current.delete(tab.id);
                }
              }}
              layout
              initial={{ opacity: 0, scale: 0.9 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.9 }}
              transition={{ layout: { type: "spring", stiffness: 500, damping: 35 } }}
              type="button"
              onClick={() => onSelect(tab.id)}
              onPointerDown={(event) => {
                if (event.button !== 0) {
                  return;
                }
                dragRef.current = { id: tab.id, startX: event.clientX, dragging: false };
              }}
```

`ponytail:` this swaps the array index live as the pointer crosses a
neighbor's midpoint and lets the existing `layout` spring animate
everything, including the dragged tab itself — not a pixel-glued
drag-follows-cursor feel (that would need suppressing `layout` on just the
dragged element and hand-animating its transform, which risks fighting
framer's own FLIP measurement; not worth the risk without being able to
live-test it). Upgrade to cursor-follow if this reads as too jumpy in
practice.

- [ ] **Step 4: Wire `reorderTab` into `Workbench.tsx`**

In `frontend/src/workbench/Workbench.tsx`, find:

```ts
  const openTabs = useWorkbenchTabsStore((state) => state.openTabs);
  const activeTabId = useWorkbenchTabsStore((state) => state.activeTabId);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);
  const closeTab = useWorkbenchTabsStore((state) => state.closeTab);
  const setActiveTab = useWorkbenchTabsStore((state) => state.setActiveTab);
```

Replace with:

```ts
  const openTabs = useWorkbenchTabsStore((state) => state.openTabs);
  const activeTabId = useWorkbenchTabsStore((state) => state.activeTabId);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);
  const closeTab = useWorkbenchTabsStore((state) => state.closeTab);
  const setActiveTab = useWorkbenchTabsStore((state) => state.setActiveTab);
  const reorderTab = useWorkbenchTabsStore((state) => state.reorderTab);
```

Find:

```tsx
          <TabStrip
            tabs={openTabs}
            activeTabId={activeTabId}
            onSelect={setActiveTab}
            onClose={closeTab}
          />
```

Replace with:

```tsx
          <TabStrip
            tabs={openTabs}
            activeTabId={activeTabId}
            onSelect={setActiveTab}
            onClose={closeTab}
            onReorder={reorderTab}
          />
```

- [ ] **Step 5: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/workbench/tabs/TabStrip.tsx frontend/src/workbench/Workbench.tsx
git commit -m "feat(workbench): drag tabs to reorder"
```

---

### Task 4: Notes refresh store

**Files:**
- Create: `frontend/src/stores/notesRefresh.ts`

- [ ] **Step 1: Write the store**

```ts
import { create } from "zustand";

/**
 * NotesDashboard and NoteEditor are now two independently-mounted tabs
 * (not parent/child), so a change in one (save, delete, folder edit) can't
 * just bump a local `refreshToken` prop passed down — both sides share
 * this instead. Same minimal shape as dock-store.ts.
 */
type NotesRefreshState = {
  token: number;
  bump: () => void;
};

export const useNotesRefreshStore = create<NotesRefreshState>((set) => ({
  token: 0,
  bump: () => set((state) => ({ token: state.token + 1 })),
}));
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds — no consumers yet.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/stores/notesRefresh.ts
git commit -m "feat(notes): add notes refresh store"
```

---

### Task 5: Split Notes into list + editor tabs

**Files:**
- Modify: `frontend/src/modules/notes/NoteCard.tsx`
- Modify: `frontend/src/modules/notes/NotesDashboard.tsx`
- Modify: `frontend/src/modules/notes/NoteEditor.tsx`
- Modify: `frontend/src/modules/notes/manifest.ts`
- Modify: `frontend/src/modules/notes/NotesView.tsx`

This is one task, not five, because the five files are mutually
type-coupled through this refactor (`NoteCard`'s open-handler signature
change requires `NotesDashboard` to change in the same step;
`NotesDashboard`'s new prop names require `NotesView` to change in the
same step; `NoteEditor`'s new `{params}` signature requires `manifest.ts`
in the same step to keep registering a component whose type actually
matches `ModuleView.component`) — splitting them into separate
typecheck-clean commits isn't possible without an intentionally broken
intermediate state.

- [ ] **Step 1: `NoteCard.tsx` — pass the note, not just its id; add double-click**

Find:

```tsx
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
```

Replace with:

```tsx
export function NoteCard({
  note,
  onOpen,
  onOpenDouble,
  onTagClick,
}: {
  note: NoteSummary;
  onOpen: (note: NoteSummary) => void;
  onOpenDouble: (note: NoteSummary) => void;
  onTagClick: (tag: string) => void;
}) {
  const accent = noteAccentColor(note.effectiveColor);
  return (
    <button
      type="button"
      onClick={() => onOpen(note)}
      onDoubleClick={() => onOpenDouble(note)}
      className="flex aspect-3/2 flex-col gap-1.5 rounded-xl border border-border bg-card p-4 text-left"
    >
```

- [ ] **Step 2: `NotesDashboard.tsx` — rename/retype the open props, wire double-click, pin on create**

Find:

```ts
type NotesDashboardProps = {
  refreshToken: number;
  onOpenNote: (id: string) => void;
  onChanged: () => void;
};
```

Replace with:

```ts
type NotesDashboardProps = {
  refreshToken: number;
  onOpenNote: (note: { id: string; title: string }) => void;
  onOpenNoteDouble: (note: { id: string; title: string }) => void;
  onChanged: () => void;
};
```

Find:

```ts
export function NotesDashboard({
  refreshToken,
  onOpenNote,
  onChanged,
}: NotesDashboardProps) {
```

Replace with:

```ts
export function NotesDashboard({
  refreshToken,
  onOpenNote,
  onOpenNoteDouble,
  onChanged,
}: NotesDashboardProps) {
```

Find:

```ts
  async function onCreateNote() {
    setCreating(true);
    try {
      const folderId = scope.startsWith("folder:") ? scope.slice(7) : "";
      const created = await createNote("Untitled", "", folderId, []);
      onChanged();
      onOpenNote(created.id);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setCreating(false);
    }
  }
```

Replace with:

```ts
  async function onCreateNote() {
    setCreating(true);
    try {
      const folderId = scope.startsWith("folder:") ? scope.slice(7) : "";
      const created = await createNote("Untitled", "", folderId, []);
      onChanged();
      // Explicit creation is a deliberate action (same as a double-click),
      // not casual browsing — opens pinned, not as a replaceable preview.
      onOpenNoteDouble(created);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setCreating(false);
    }
  }
```

Find:

```tsx
              {filtered.map((note) => (
                <NoteCard
                  key={note.id}
                  note={note}
                  onOpen={onOpenNote}
                  onTagClick={toggleTagFilter}
                />
              ))}
```

Replace with:

```tsx
              {filtered.map((note) => (
                <NoteCard
                  key={note.id}
                  note={note}
                  onOpen={onOpenNote}
                  onOpenDouble={onOpenNoteDouble}
                  onTagClick={toggleTagFilter}
                />
              ))}
```

- [ ] **Step 3: `NoteEditor.tsx` — become a `ModuleView`, wire tabs + notes-refresh, pin on first edit**

Find the import block's end (before the local component definitions):

```tsx
import { useSplitPanel } from "@/components/ui/split-panel";
import { errorMessage } from "../../bridge/errors";
import {
  deleteNote,
  getNote,
  listFolders,
  setNoteOrganization,
  updateNote,
  type Note,
  type NoteFolder,
} from "../../bridge/notes";
import { useModuleAction } from "../../stores/moduleActions";
import { ColorPicker } from "./ColorPicker";
```

Replace with:

```tsx
import { useSplitPanel } from "@/components/ui/split-panel";
import { errorMessage } from "../../bridge/errors";
import {
  deleteNote,
  getNote,
  listFolders,
  setNoteOrganization,
  updateNote,
  type Note,
  type NoteFolder,
} from "../../bridge/notes";
import { useModuleAction } from "../../stores/moduleActions";
import { useNotesRefreshStore } from "../../stores/notesRefresh";
import { tabKey, useWorkbenchTabsStore } from "../../stores/workbenchTabs";
import { ColorPicker } from "./ColorPicker";
```

Find:

```ts
type NoteEditorProps = {
  noteId: string;
  onBack?: () => void;
  onDeleted: () => void;
  onSaved: () => void;
};
```

Replace with:

```ts
type NoteEditorProps = {
  params?: Record<string, unknown>;
};
```

Find:

```tsx
export function NoteEditor({ noteId, onBack, onDeleted, onSaved }: NoteEditorProps) {
  const [note, setNote] = useState<Note | null>(null);
```

Replace with:

```tsx
export function NoteEditor({ params }: NoteEditorProps) {
  const noteId = typeof params?.noteId === "string" ? params.noteId : "";
  const tabId = useMemo(() => tabKey("notes", "notes.editor", { noteId }), [noteId]);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);
  const closeTab = useWorkbenchTabsStore((state) => state.closeTab);
  const pinTab = useWorkbenchTabsStore((state) => state.pinTab);
  const bumpNotesRefresh = useNotesRefreshStore((state) => state.bump);
  const hasEditedRef = useRef(false);

  function onBack() {
    openTab({ moduleId: "notes", viewId: "notes.list", title: "Notes" });
  }

  function onDeleted() {
    closeTab(tabId);
    bumpNotesRefresh();
  }

  function onSaved() {
    bumpNotesRefresh();
  }

  const [note, setNote] = useState<Note | null>(null);
```

Find:

```ts
  function scheduleSave(nextTitle: string) {
    if (saveTimer.current) {
      clearTimeout(saveTimer.current);
    }
    saveTimer.current = setTimeout(() => void save(nextTitle), AUTOSAVE_DELAY_MS);
  }
```

Replace with:

```ts
  function scheduleSave(nextTitle: string) {
    if (!hasEditedRef.current) {
      hasEditedRef.current = true;
      pinTab(tabId);
    }
    if (saveTimer.current) {
      clearTimeout(saveTimer.current);
    }
    saveTimer.current = setTimeout(() => void save(nextTitle), AUTOSAVE_DELAY_MS);
  }
```

Find:

```tsx
        {onBack ? (
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            aria-label="Back to notes"
            onClick={onBack}
          >
            <ArrowLeft className="size-4" />
          </Button>
        ) : null}
```

Replace with:

```tsx
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label="Back to notes"
          onClick={onBack}
        >
          <ArrowLeft className="size-4" />
        </Button>
```

Everything else in `NoteEditor.tsx` (data loading, autosave, organization
persistence, the Plate editor, the split-panel metadata wiring) is
unchanged — this only touches how the component receives its identity and
how it reports back "gone back" / "deleted" / "saved", which used to be
props from a parent and are now local functions calling shared stores.

- [ ] **Step 4: `manifest.ts` — register `notes.editor`**

Find:

```ts
import type { ModuleManifest } from "../registry";
import { NotesView } from "./NotesView";

export const notesModule: ModuleManifest = {
  id: "notes",
  title: "Notes",
  version: "1.0.0",
  apiVersion: "1",
  icon: FileText,
  capabilities: ["notes:read", "notes:write"],
  commands: [
    {
      id: "notes.create",
      title: "Create note",
    },
  ],
  views: [
    {
      id: "notes.list",
      title: "Notes",
      component: NotesView,
    },
  ],
};
```

Replace with:

```ts
import type { ModuleManifest } from "../registry";
import { NotesView } from "./NotesView";
import { NoteEditor } from "./NoteEditor";

export const notesModule: ModuleManifest = {
  id: "notes",
  title: "Notes",
  version: "1.0.0",
  apiVersion: "1",
  icon: FileText,
  capabilities: ["notes:read", "notes:write"],
  commands: [
    {
      id: "notes.create",
      title: "Create note",
    },
  ],
  views: [
    {
      id: "notes.list",
      title: "Notes",
      component: NotesView,
    },
    {
      id: "notes.editor",
      title: "Note",
      component: NoteEditor,
    },
  ],
};
```

(The `import { FileText } from "lucide-react";` line at the top of this
file, added in Phase 1's icon self-registration work, is untouched.)

- [ ] **Step 5: `NotesView.tsx` — drop the internal switch, open tabs instead**

Replace the full contents of `frontend/src/modules/notes/NotesView.tsx`
with:

```tsx
import { useNotesRefreshStore } from "../../stores/notesRefresh";
import { useWorkbenchTabsStore } from "../../stores/workbenchTabs";
import { NotesDashboard } from "./NotesDashboard";

export function NotesView() {
  const refreshToken = useNotesRefreshStore((state) => state.token);
  const bumpNotesRefresh = useNotesRefreshStore((state) => state.bump);
  const openTab = useWorkbenchTabsStore((state) => state.openTab);

  function openNoteTab(note: { id: string; title: string }, preview: boolean) {
    openTab({
      moduleId: "notes",
      viewId: "notes.editor",
      title: note.title || "Untitled",
      params: { noteId: note.id },
      preview,
    });
  }

  return (
    <NotesDashboard
      refreshToken={refreshToken}
      onOpenNote={(note) => openNoteTab(note, true)}
      onOpenNoteDouble={(note) => openNoteTab(note, false)}
      onChanged={bumpNotesRefresh}
    />
  );
}
```

- [ ] **Step 6: Typecheck**

Run: `cd frontend && bun run build`
Expected: succeeds with no TypeScript errors — this is the step that
proves the five files' mutual type coupling is actually resolved (a
mismatch anywhere in the chain — `NoteCard` → `NotesDashboard` →
`NotesView` → `openTab`, or `NoteEditor` → `manifest.ts`'s
`ModuleView.component` — would surface here).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/modules/notes/NoteCard.tsx frontend/src/modules/notes/NotesDashboard.tsx frontend/src/modules/notes/NoteEditor.tsx frontend/src/modules/notes/manifest.ts frontend/src/modules/notes/NotesView.tsx
git commit -m "feat(notes): per-note tabs with preview/pin lifecycle"
```

---

### Task 6: Manual QA

No code changes. Run: `task dev:frontend` (or the `run` skill) and, in the
running app:

1. **Preview replace-in-place.** Click note A → tab "A" appears, italic.
   Click note B → tab "A" is replaced in place by italic tab "B" (same
   position, not appended). Click A again → back to italic "A" in the same
   slot.
2. **Promote via double-click.** Double-click note C → tab "C" appears
   pinned (not italic), independent of any current preview tab.
3. **Promote via first edit.** With preview tab "D" open, type into D's
   title or body → "D" loses italic (pinned) without moving position or
   losing editor focus. Then: open a note you have **not** touched and
   confirm it does **not** immediately lose its italic on its own (i.e.
   loading the note's saved content into the Plate editor must not count
   as an "edit" — if it does, that's a real bug to fix, not expected
   behavior).
4. **New note opens pinned.** Click "New note" — the created note's tab is
   pinned (not italic) from the start, matching double-click semantics.
5. **Dock unaffected by item tabs.** Open pinned note "E", switch to SSH,
   click the Notes Dock icon → a separate "Notes" dashboard tab
   opens/focuses; "E"'s tab is untouched and still pinned.
6. **Drag reorder.** Drag a pinned tab past two neighbors — order updates
   live as the drag crosses each midpoint, other tabs slide via the
   existing spring; dropping settles the dragged tab with no layout jump.
7. **Drag doesn't touch preview state.** Drag the current preview tab — it
   moves position but stays italic and is still replaced by the next
   single-click preview-open elsewhere.
8. **State survives switching.** Open two different notes as separate
   tabs, type into both, switch back and forth — both keep their unsaved
   text (tabs stay mounted, not unmounted, per Phase 1's `contents`/
   `hidden` toggling — confirms per-note tabs didn't regress that).
9. **Delete closes the tab.** Delete a note from inside its editor tab —
   that tab closes (fallback focus per `closeTab`'s existing left-neighbor
   rule) and, if a Notes dashboard tab is open elsewhere, it no longer
   shows the deleted note after its own next refresh.
