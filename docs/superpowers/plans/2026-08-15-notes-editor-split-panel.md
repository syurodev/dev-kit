# Notes Editor + App-Wide Split Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a shared, app-wide `SplitPanel` primitive (content-squeezing side panel) and `moduleActions` header-button registry, then use both to redesign the Notes editor: a centered/width-capped writing column with a large title, and folder/color/tag editing moved into a toggleable panel whose toggle button lives in the app-wide title bar.

**Architecture:** Two new framework-level primitives land first and are wired into `Workbench.tsx` once: `frontend/src/stores/moduleActions.ts` (Zustand registry any module can push an icon-button into the app-wide title bar) and `frontend/src/components/ui/split-panel.tsx` (Provider + Slot + hook; the panel is a flex sibling of the active module's content, animated with `framer-motion`'s `LazyMotion`/`m` — the same pattern already used in `frontend/src/workbench/dock/{Dock,DockItem}.tsx` — so opening it shrinks the content area via flexbox, no manual width math). `NoteEditor.tsx` is the first (and, this pass, only) consumer of both.

**Tech Stack:** React 19.2 + TypeScript + Tailwind v4, Zustand (matching the existing `useAgentSessionsStore` convention), `framer-motion` (already a dependency, already used in this codebase).

## Global Constraints

- `useModuleAction`'s registration effect must depend on primitive fields (`id`/`icon`/`label`/`isActive`) via a ref for `onClick` — never on the whole action object — or it re-registers on every parent render (e.g. every keystroke in the editor). See the spec's "Researched" section for why `useEffectEvent` was considered and rejected for this specific job (its contract requires being called from an Effect in the same component that defines it; storing it in Zustand for a different component to invoke later is outside that contract).
- `SplitPanelSlot` needs its own `<LazyMotion features={domAnimation}>` wrap — it is not a descendant of `Dock`'s own `LazyMotion` (siblings under `Workbench`, not parent/child), so it cannot rely on that ancestor.
- The panel is a flex sibling *inside* `Workbench.tsx`'s existing bordered content card (`mx-2 mb-2 ... rounded-xl border border-border bg-card`) — not a new top-level card. Module actions render inside the existing `<header>`, before the existing Search button — not a new header row.
- No mobile drawer fallback, no app-sidebar auto-collapse coordination, no resizable/draggable panel width, no reordering/overflow logic for multiple simultaneous module actions — all explicitly out of scope (see spec Non-goals).
- Spec: `docs/superpowers/specs/2026-08-15-notes-editor-split-panel-design.md`.
- `docs/superpowers/*` is gitignored; `git add -f` plan/spec files if `git status` does not stage them.

## File structure

- `frontend/src/stores/moduleActions.ts` — new. `ModuleAction` type, `useModuleActionsStore` (Zustand), `useModuleAction()` hook.
- `frontend/src/components/ui/split-panel.tsx` — new. `SplitPanelProvider`, `useSplitPanel()`, `SplitPanelSlot`.
- `frontend/src/workbench/Workbench.tsx` — wrap in `SplitPanelProvider`; render registered module actions in the header; nest `<section>` + `<SplitPanelSlot />` as a flex-row pair inside the existing content card.
- `frontend/src/modules/notes/NoteEditor.tsx` — full-file rewrite: centered/width-capped writing column with a large title; new local `NoteMetaPanel` component (folder/color/tag controls, relocated from the old inline row); wires `useModuleAction` (title-bar toggle) and `useSplitPanel` (push/pull panel content).

---

### Task 1: `moduleActions` store

**Files:**
- Create: `frontend/src/stores/moduleActions.ts`

**Interfaces:**
- Consumes: `zustand` (`create`), `LucideIcon` type from `lucide-react`, React (`useEffect`, `useRef`)
- Produces: `ModuleAction` type; `useModuleActionsStore` (Zustand store: `actions: ModuleAction[]`, `registerAction`, `unregisterAction`); `useModuleAction(action: ModuleAction | null)` hook

- [ ] **Step 1: Create the file**

Create `frontend/src/stores/moduleActions.ts`:

```ts
import { useEffect, useRef } from "react";
import { create } from "zustand";
import type { LucideIcon } from "lucide-react";

export type ModuleAction = {
  id: string;
  icon: LucideIcon;
  label: string;
  isActive?: boolean;
  onClick: () => void;
};

type ModuleActionsState = {
  actions: ModuleAction[];
  registerAction: (action: ModuleAction) => void;
  unregisterAction: (id: string) => void;
};

export const useModuleActionsStore = create<ModuleActionsState>((set) => ({
  actions: [],
  registerAction: (action) =>
    set((state) => {
      const index = state.actions.findIndex((a) => a.id === action.id);
      if (index === -1) {
        return { actions: [...state.actions, action] };
      }
      const next = state.actions.slice();
      next[index] = action;
      return { actions: next };
    }),
  unregisterAction: (id) =>
    set((state) => ({ actions: state.actions.filter((a) => a.id !== id) })),
}));

/**
 * Registers `action` into the app-wide title-bar row for as long as the
 * calling component stays mounted (i.e. for as long as its module is
 * active). `onClick` is read through a ref so the registration effect only
 * re-runs when `id`/`icon`/`label`/`isActive` actually change — not on
 * every render of the calling component.
 */
export function useModuleAction(action: ModuleAction | null) {
  const registerAction = useModuleActionsStore((s) => s.registerAction);
  const unregisterAction = useModuleActionsStore((s) => s.unregisterAction);

  const onClickRef = useRef(action?.onClick);
  onClickRef.current = action?.onClick;

  const id = action?.id;
  const icon = action?.icon;
  const label = action?.label;
  const isActive = action?.isActive;

  useEffect(() => {
    if (!id || !icon || !label) {
      return;
    }
    registerAction({
      id,
      icon,
      label,
      isActive,
      onClick: () => onClickRef.current?.(),
    });
    return () => unregisterAction(id);
  }, [id, icon, label, isActive, registerAction, unregisterAction]);
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add frontend/src/stores/moduleActions.ts
git commit -m "feat(workbench): add moduleActions store for title-bar action buttons"
```

---

### Task 2: `SplitPanel` primitive

**Files:**
- Create: `frontend/src/components/ui/split-panel.tsx`

**Interfaces:**
- Consumes: `framer-motion` (`AnimatePresence`, `LazyMotion`, `domAnimation`, `m`), `@/components/ui/button`, `@/lib/utils` (`cn`), `lucide-react` (`X`)
- Produces: `SplitPanelProvider`, `useSplitPanel()` (`{ panel, openPanel, closePanel }`), `SplitPanelSlot`

- [ ] **Step 1: Create the file**

Create `frontend/src/components/ui/split-panel.tsx`:

```tsx
import * as React from "react";
import { AnimatePresence, LazyMotion, domAnimation, m } from "framer-motion";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

type PanelState = {
  content: React.ReactNode;
  width: number;
  onClose: () => void;
};

type SplitPanelContextValue = {
  panel: PanelState | null;
  openPanel: (
    content: React.ReactNode,
    opts: { width?: number; onClose: () => void },
  ) => void;
  closePanel: () => void;
};

const SplitPanelContext = React.createContext<SplitPanelContextValue | null>(null);

export function SplitPanelProvider({ children }: { children: React.ReactNode }) {
  const [panel, setPanel] = React.useState<PanelState | null>(null);

  const openPanel = React.useCallback(
    (content: React.ReactNode, opts: { width?: number; onClose: () => void }) => {
      setPanel({ content, width: opts.width ?? 420, onClose: opts.onClose });
    },
    [],
  );
  const closePanel = React.useCallback(() => setPanel(null), []);

  const value = React.useMemo(
    () => ({ panel, openPanel, closePanel }),
    [panel, openPanel, closePanel],
  );

  return (
    <SplitPanelContext.Provider value={value}>{children}</SplitPanelContext.Provider>
  );
}

export function useSplitPanel(): SplitPanelContextValue {
  const ctx = React.useContext(SplitPanelContext);
  if (!ctx) {
    throw new Error("useSplitPanel must be used within a SplitPanelProvider.");
  }
  return ctx;
}

/**
 * Renders as a flex sibling of the active module's content (see
 * Workbench.tsx). Its own LazyMotion wrap is required — it is not a
 * descendant of Dock's LazyMotion (siblings under Workbench, not
 * parent/child).
 */
export function SplitPanelSlot() {
  const { panel } = useSplitPanel();

  return (
    <LazyMotion features={domAnimation}>
      <m.div
        initial={{ width: 0 }}
        animate={{ width: panel ? panel.width : 0 }}
        transition={{ duration: 0.22, ease: "easeInOut" }}
        className={cn(
          "relative flex min-h-0 shrink-0 flex-col overflow-hidden",
          panel ? "border-l border-border" : "",
        )}
      >
        {panel ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label="Close panel"
            className="absolute top-2 right-2 z-10"
            onClick={panel.onClose}
          >
            <X className="size-4" />
          </Button>
        ) : null}
        <AnimatePresence>
          {panel ? (
            <m.div
              key="panel-content"
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.15 }}
              className="flex min-h-0 flex-1 flex-col overflow-auto"
            >
              {panel.content}
            </m.div>
          ) : null}
        </AnimatePresence>
      </m.div>
    </LazyMotion>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/ui/split-panel.tsx
git commit -m "feat(ui): add SplitPanel primitive (content-squeezing side panel)"
```

---

### Task 3: Wire both primitives into `Workbench.tsx`

**Files:**
- Modify: `frontend/src/workbench/Workbench.tsx`

**Interfaces:**
- Consumes: `SplitPanelProvider`/`SplitPanelSlot` (Task 2), `useModuleActionsStore` (Task 1)
- Produces: no change to `Workbench`'s own props/behavior; any module can now reach `useSplitPanel()`/`useModuleAction()` from inside `<ActiveView />`

- [ ] **Step 1: Add imports**

In `frontend/src/workbench/Workbench.tsx`, add to the import block (after the existing `Tooltip` import, before `cn`):

```tsx
import { SplitPanelProvider, SplitPanelSlot } from "@/components/ui/split-panel";
import { useModuleActionsStore } from "../stores/moduleActions";
```

- [ ] **Step 2: Read the registered actions**

Inside the `Workbench()` function, add alongside the other `useState`/derived declarations (e.g. right after `const [paletteOpen, setPaletteOpen] = useState(false);`):

```tsx
  const moduleActions = useModuleActionsStore((s) => s.actions);
```

- [ ] **Step 3: Render module actions in the header**

Replace (the header's spacer + first Tooltip/Button, originally right after `<div className="flex-1" />`):

```tsx
        <div className="flex-1" />
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Commands"
              onClick={() => setPaletteOpen(true)}
            >
              <Search className="size-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            Commands <Kbd>⌘K</Kbd>
          </TooltipContent>
        </Tooltip>
```

with:

```tsx
        <div className="flex-1" />
        {moduleActions.map((action) => (
          <Tooltip key={action.id}>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={action.label}
                aria-pressed={action.isActive}
                className={action.isActive ? "bg-accent text-foreground" : undefined}
                onClick={action.onClick}
              >
                <action.icon className="size-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom">{action.label}</TooltipContent>
          </Tooltip>
        ))}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label="Commands"
              onClick={() => setPaletteOpen(true)}
            >
              <Search className="size-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            Commands <Kbd>⌘K</Kbd>
          </TooltipContent>
        </Tooltip>
```

- [ ] **Step 4: Add `SplitPanelSlot` as a flex-row sibling of `<section>`**

Replace the bordered content card's body:

```tsx
      <div className="mx-2 mb-2 flex flex-1 flex-col overflow-hidden rounded-xl border border-border bg-card">
        {hasMetadataWarning ? (
          <Alert variant="destructive" className="m-4 mb-0 w-auto">
            <AlertDescription>
              Module metadata is unavailable or out of sync.
            </AlertDescription>
          </Alert>
        ) : null}
        <section
          className={cn(
            "flex min-h-0 flex-1 flex-col overflow-auto",
            activeModule?.id === "notes" ? "p-0" : "p-4",
          )}
        >
          {ActiveView ? (
            <ActiveView />
          ) : (
            <p role="status" className="text-sm text-muted-foreground">
              {availableModules.length === 0
                ? "No modules are currently available."
                : "No view is currently available."}
            </p>
          )}
        </section>
      </div>
```

with:

```tsx
      <div className="mx-2 mb-2 flex flex-1 flex-col overflow-hidden rounded-xl border border-border bg-card">
        {hasMetadataWarning ? (
          <Alert variant="destructive" className="m-4 mb-0 w-auto">
            <AlertDescription>
              Module metadata is unavailable or out of sync.
            </AlertDescription>
          </Alert>
        ) : null}
        <div className="flex min-h-0 flex-1">
          <section
            className={cn(
              "flex min-h-0 flex-1 flex-col overflow-auto",
              activeModule?.id === "notes" ? "p-0" : "p-4",
            )}
          >
            {ActiveView ? (
              <ActiveView />
            ) : (
              <p role="status" className="text-sm text-muted-foreground">
                {availableModules.length === 0
                  ? "No modules are currently available."
                  : "No view is currently available."}
              </p>
            )}
          </section>
          <SplitPanelSlot />
        </div>
      </div>
```

- [ ] **Step 5: Wrap the return value in `SplitPanelProvider`**

Change the component's `return (` line and its matching closing tag. The outer element currently is:

```tsx
  return (
    // Transparent, not bg-background: this outer frame is what the native
    // macOS vibrancy blur (Mac.Backdrop in main.go) needs to show through.
    // Only the content card below (bg-card) stays opaque.
    <div className="relative flex min-h-svh w-full flex-col bg-transparent">
```

Change to:

```tsx
  return (
    <SplitPanelProvider>
      {/* Transparent, not bg-background: this outer frame is what the native
          macOS vibrancy blur (Mac.Backdrop in main.go) needs to show through.
          Only the content card below (bg-card) stays opaque. */}
      <div className="relative flex min-h-svh w-full flex-col bg-transparent">
```

And change the component's closing tags (currently `</div>\n  );\n}` at the very end, right after `<InteractionHost />`) to:

```tsx
      </div>
    </SplitPanelProvider>
  );
}
```

(This adds one level of indentation to everything between the outer `<div>` and its close — a pure reformat, no content change. Re-indent when applying the edit so the file stays consistently formatted.)

- [ ] **Step 6: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 7: Manual check**

Run the app: header, dock, and every module still render exactly as before (module actions row is empty — no module registers anything yet, so it renders nothing extra). No layout shift, no console errors.

- [ ] **Step 8: Commit**

```bash
git add frontend/src/workbench/Workbench.tsx
git commit -m "feat(workbench): wire SplitPanel and moduleActions into the app shell"
```

---

### Task 4: Redesign `NoteEditor` — centered column + panel-based metadata

**Files:**
- Modify: `frontend/src/modules/notes/NoteEditor.tsx` (full-file rewrite)

**Interfaces:**
- Consumes: `useModuleAction` (Task 1), `useSplitPanel` (Task 2); existing `ColorPicker`, `folderDotVar`, `tagChipVar`, bridge functions — all unchanged
- Produces: same `NoteEditor({ noteId, onBack, onDeleted, onSaved })` public signature; visually: centered/width-capped writing column with a large title; folder/color/tag editing relocated into a `NoteMetaPanel` opened via a title-bar toggle instead of an inline row

- [ ] **Step 1: Replace the file**

Replace the full contents of `frontend/src/modules/notes/NoteEditor.tsx`:

```tsx
import { useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  Bold,
  Code,
  Italic,
  PanelRight,
  Strikethrough,
  Trash2,
  Underline,
  X,
} from "lucide-react";
import { KEYS } from "platejs";
import { Plate, usePlateEditor } from "platejs/react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Editor, EditorContainer } from "@/components/ui/editor";
import { FloatingToolbar } from "@/components/ui/floating-toolbar";
import { Input } from "@/components/ui/input";
import { LinkToolbarButton } from "@/components/ui/link-toolbar-button";
import { MarkToolbarButton } from "@/components/ui/mark-toolbar-button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
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
import { folderDotVar } from "./folderColors";
import { tagChipVar } from "./tagColor";
import { NotesEditorKit } from "./plate/plugins";

type SaveStatus = "idle" | "saving" | "saved" | "error";

type NoteEditorProps = {
  noteId: string;
  onBack?: () => void;
  onDeleted: () => void;
  onSaved: () => void;
};

const AUTOSAVE_DELAY_MS = 1500;
const MAX_TAGS = 32;
const MAX_TAG_LEN = 40;

function inheritedTags(note: Note): string[] {
  const own = new Set(note.tags.map((t) => t.toLowerCase()));
  return note.effectiveTags.filter((t) => !own.has(t.toLowerCase()));
}

function TagChip({
  tag,
  onRemove,
  readOnly,
}: {
  tag: string;
  onRemove?: () => void;
  readOnly?: boolean;
}) {
  return (
    <Badge variant="secondary" className="gap-1.5 bg-muted text-foreground">
      <span
        className="size-1.5 shrink-0 rounded-full"
        style={{ background: tagChipVar(tag) }}
        aria-hidden
      />
      {tag}
      {onRemove && !readOnly ? (
        <button
          type="button"
          aria-label={`Remove ${tag}`}
          className="inline-flex"
          onClick={onRemove}
        >
          <X className="size-3" />
        </button>
      ) : null}
    </Badge>
  );
}

function NoteMetaPanel({
  folders,
  folderId,
  ownTags,
  color,
  tagDraft,
  inherited,
  onTagDraftChange,
  onFolderChange,
  onColorChange,
  onAddTag,
  onRemoveTag,
}: {
  folders: NoteFolder[];
  folderId: string;
  ownTags: string[];
  color: string;
  tagDraft: string;
  inherited: string[];
  onTagDraftChange: (value: string) => void;
  onFolderChange: (value: string) => void;
  onColorChange: (color: string) => void;
  onAddTag: (raw: string) => void;
  onRemoveTag: (tag: string) => void;
}) {
  return (
    <div className="flex flex-col gap-5 p-4">
      <div>
        <p className="mb-2 text-xs font-medium tracking-[0.4px] text-muted-foreground uppercase">
          Folder
        </p>
        <Select value={folderId || "ungrouped"} onValueChange={onFolderChange}>
          <SelectTrigger size="sm" className="w-full" aria-label="note-folder">
            <SelectValue placeholder="Folder" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="ungrouped">Ungrouped</SelectItem>
            {folders.map((folder) => (
              <SelectItem key={folder.id} value={folder.id}>
                <span className="flex items-center gap-2">
                  <span
                    className="size-2 shrink-0 rounded-full"
                    style={{ background: folderDotVar(folder.color) }}
                    aria-hidden
                  />
                  {folder.name}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div>
        <p className="mb-2 text-xs font-medium tracking-[0.4px] text-muted-foreground uppercase">
          Color
        </p>
        <ColorPicker value={color} onChange={onColorChange} allowInherit />
      </div>

      <div>
        <p className="mb-2 text-xs font-medium tracking-[0.4px] text-muted-foreground uppercase">
          Tags
        </p>
        <Input
          aria-label="Add tag"
          placeholder="Add tag…"
          value={tagDraft}
          onChange={(e) => onTagDraftChange(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === ",") {
              e.preventDefault();
              onAddTag(tagDraft);
            }
          }}
          onBlur={() => {
            if (tagDraft.trim()) {
              onAddTag(tagDraft);
            }
          }}
          className="h-8"
        />
        {ownTags.length > 0 || inherited.length > 0 ? (
          <div className="mt-2 space-y-1.5">
            {ownTags.length > 0 ? (
              <div className="flex flex-wrap gap-1">
                {ownTags.map((tag) => (
                  <TagChip key={tag} tag={tag} onRemove={() => onRemoveTag(tag)} />
                ))}
              </div>
            ) : null}
            {inherited.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1">
                <span className="text-xs text-muted-foreground">From folder:</span>
                {inherited.map((tag) => (
                  <TagChip key={tag} tag={tag} readOnly />
                ))}
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function NoteEditor({ noteId, onBack, onDeleted, onSaved }: NoteEditorProps) {
  const [note, setNote] = useState<Note | null>(null);
  const [folders, setFolders] = useState<NoteFolder[]>([]);
  const [title, setTitle] = useState("");
  const [folderId, setFolderId] = useState("");
  const [ownTags, setOwnTags] = useState<string[]>([]);
  const [color, setColor] = useState("");
  const [tagDraft, setTagDraft] = useState("");
  const [metaOpen, setMetaOpen] = useState(false);
  const [status, setStatus] = useState<SaveStatus>("idle");
  const [loadError, setLoadError] = useState("");
  const [saveError, setSaveError] = useState("");
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const saveInFlight = useRef(false);
  const pendingSave = useRef(false);
  const pendingTitle = useRef("");
  const titleRef = useRef("");
  const noteRef = useRef<Note | null>(null);
  const orgInFlight = useRef(false);

  const { openPanel, closePanel } = useSplitPanel();

  const editor = usePlateEditor({
    plugins: NotesEditorKit,
    value: [],
  });

  const folderName = useMemo(() => {
    if (!folderId) {
      return "Ungrouped";
    }
    return folders.find((f) => f.id === folderId)?.name ?? "Ungrouped";
  }, [folderId, folders]);

  const inherited = useMemo(
    () => (note ? inheritedTags({ ...note, tags: ownTags }) : []),
    [note, ownTags],
  );

  useEffect(() => {
    let mounted = true;
    setLoadError("");
    setStatus("idle");
    Promise.all([getNote(noteId), listFolders()])
      .then(([n, folderList]) => {
        if (!mounted) {
          return;
        }
        noteRef.current = n;
        setNote(n);
        setFolders(folderList);
        titleRef.current = n.title;
        setTitle(n.title);
        setFolderId(n.folderId);
        setOwnTags(n.tags);
        setColor(n.color);
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
      const loadedNote = noteRef.current;
      if (loadedNote) {
        void updateNote(
          loadedNote.id,
          titleRef.current,
          editor.api.markdown.serialize(),
        );
      }
    };
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
    if (saveInFlight.current) {
      pendingSave.current = true;
      pendingTitle.current = nextTitle;
      return;
    }
    saveInFlight.current = true;
    setStatus("saving");
    setSaveError("");
    try {
      const body = editor.api.markdown.serialize();
      const updated = await updateNote(note.id, nextTitle, body);
      const current = noteRef.current;
      noteRef.current = {
        ...updated,
        folderId: current?.folderId ?? folderId,
        tags: current?.tags ?? ownTags,
        effectiveTags: current?.effectiveTags ?? updated.effectiveTags,
        color: current?.color ?? color,
        effectiveColor: current?.effectiveColor ?? "",
      };
      setNote(noteRef.current);
      setStatus("saved");
      onSaved();
    } catch (e) {
      setStatus("error");
      setSaveError(errorMessage(e));
    } finally {
      saveInFlight.current = false;
      if (pendingSave.current) {
        pendingSave.current = false;
        void save(pendingTitle.current);
      }
    }
  }

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

  function tryAddTag(raw: string) {
    const tag = raw.trim();
    if (!tag) {
      return;
    }
    if (tag.length > MAX_TAG_LEN) {
      toast.error("Tag must be at most 40 characters.");
      return;
    }
    const key = tag.toLowerCase();
    if (ownTags.some((t) => t.toLowerCase() === key)) {
      setTagDraft("");
      return;
    }
    if (ownTags.length >= MAX_TAGS) {
      toast.error("A note can have at most 32 tags.");
      return;
    }
    const next = [...ownTags, tag];
    setOwnTags(next);
    setTagDraft("");
    void persistOrganization(folderId, next, color);
  }

  function removeOwnTag(tag: string) {
    const next = ownTags.filter((t) => t !== tag);
    setOwnTags(next);
    void persistOrganization(folderId, next, color);
  }

  function onFolderChange(value: string) {
    const nextFolderId = value === "ungrouped" ? "" : value;
    setFolderId(nextFolderId);
    void persistOrganization(nextFolderId, ownTags, color);
  }

  function onColorChange(nextColor: string) {
    setColor(nextColor);
    void persistOrganization(folderId, ownTags, nextColor);
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

  useModuleAction(
    note
      ? {
          id: "notes.toggle-meta-panel",
          icon: PanelRight,
          label: "Note details",
          isActive: metaOpen,
          onClick: () => setMetaOpen((v) => !v),
        }
      : null,
  );

  useEffect(() => {
    if (!metaOpen || !note) {
      closePanel();
      return;
    }
    openPanel(
      <NoteMetaPanel
        folders={folders}
        folderId={folderId}
        ownTags={ownTags}
        color={color}
        tagDraft={tagDraft}
        inherited={inherited}
        onTagDraftChange={setTagDraft}
        onFolderChange={onFolderChange}
        onColorChange={onColorChange}
        onAddTag={tryAddTag}
        onRemoveTag={removeOwnTag}
      />,
      { width: 420, onClose: () => setMetaOpen(false) },
    );
    return () => closePanel();
    // onFolderChange/onColorChange/tryAddTag/removeOwnTag close over the
    // same state already listed below; omitted to avoid re-pushing panel
    // content on every render (they're new function references each
    // render, but their behavior only changes when these listed values do).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [metaOpen, note, folders, folderId, ownTags, color, tagDraft, inherited]);

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
    <div className="notes-font-scope flex h-full flex-1 flex-col">
      <div className="flex items-center gap-2 border-b border-border px-4 py-2">
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
        <nav
          aria-label="Note breadcrumb"
          className="flex min-w-0 items-center gap-1 text-xs text-muted-foreground"
        >
          <button type="button" className="shrink-0 hover:text-foreground" onClick={onBack}>
            Notes
          </button>
          <span aria-hidden>/</span>
          <span className="truncate">{folderName}</span>
          <span aria-hidden>/</span>
          <span className="truncate text-foreground">{title || "Untitled"}</span>
        </nav>
        <span className="ml-auto shrink-0 text-xs text-muted-foreground">
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
          <div className="mx-auto w-full max-w-[720px] px-6 pt-10 pb-24">
            <Input
              aria-label="note-title"
              value={title}
              onChange={(e) => {
                titleRef.current = e.target.value;
                setTitle(e.target.value);
                scheduleSave(e.target.value);
              }}
              className="h-auto w-full border-none bg-transparent px-0 py-1 text-3xl font-bold tracking-tight shadow-none focus-visible:ring-0"
            />
            <Editor placeholder="Write something..." className="mt-6 px-0" />
          </div>
          <FloatingToolbar>
            <MarkToolbarButton nodeType={KEYS.bold} tooltip="Bold">
              <Bold />
            </MarkToolbarButton>
            <MarkToolbarButton nodeType={KEYS.italic} tooltip="Italic">
              <Italic />
            </MarkToolbarButton>
            <MarkToolbarButton nodeType={KEYS.underline} tooltip="Underline">
              <Underline />
            </MarkToolbarButton>
            <MarkToolbarButton nodeType={KEYS.strikethrough} tooltip="Strikethrough">
              <Strikethrough />
            </MarkToolbarButton>
            <MarkToolbarButton nodeType={KEYS.code} tooltip="Code">
              <Code />
            </MarkToolbarButton>
            <LinkToolbarButton />
          </FloatingToolbar>
        </EditorContainer>
      </Plate>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 3: Manual check**

Run the app, open Notes, open a note:

1. Writing column is centered, capped at ~720px, with a large bold title above the body — no metadata row cluttering it.
2. A `PanelRight` toggle button appears in the app-wide title bar, next to Search/Settings — not inside the editor's own local header.
3. Clicking the toggle slides the panel in from the right (content column visibly narrows — flexbox squeeze, not an overlay); folder `Select`, `ColorPicker`, and tag input/chips all work inside the panel exactly as they did in the old inline row (same `persistOrganization` calls, same toasts on failure).
4. Clicking the toggle again (or the panel's own close `X`) slides it back out; the title-bar button's pressed state reflects closed.
5. Changing folder/color/tags while the panel is open updates the panel's own display live and updates the breadcrumb folder name too.
6. Switching to a different note (back to dashboard, open another note) doesn't leave a stale panel open with the previous note's data.
7. Switching away from Notes to another module (dock) removes the toggle button from the title bar; switching back to an open note restores it.
8. Typing continuously in the title or body does not visibly lag or flicker the title-bar button or (if open) the panel — confirms the register/re-render-thrash fix actually works in practice, not just in theory.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/modules/notes/NoteEditor.tsx
git commit -m "feat(notes): redesign editor with centered column and metadata panel"
```

---

### Task 5: End-to-end verification

**Files:** none new

- [ ] **Step 1: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [ ] **Step 2: Full manual script**

Repeat Task 4 Step 3's script once more end-to-end, plus:

1. Dashboard and sidebar are unaffected — no visual/behavioral change from this plan.
2. No other module (Database/SSH/Kafka/Settings/Agent Hub) shows any title-bar button or panel — `moduleActions`/`SplitPanel` render as empty/zero-width whenever nothing has registered.
3. `useModuleActionsStore.getState().actions` is empty after navigating away from every note (no leaked registrations across repeated open/close/switch cycles).

If anything fails, fix in a focused commit on that layer, then re-run the failing command.
