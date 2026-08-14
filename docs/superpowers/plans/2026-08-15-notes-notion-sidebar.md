# Notes Notion-Style Sidebar Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `NotesDashboard.tsx`'s hand-rolled sidebar with `components/ui/sidebar.tsx` (shadcn's Sidebar primitives, currently unused anywhere in the app), Notion-style: compact rows, hover-reveal per-row actions, a collapsible "Folders" group with a hover-reveal "+", colored folder icons instead of dots, and a real collapse/expand toggle via `Sidebar`'s built-in `collapsible="icon"` mode.

**Architecture:** Pure frontend, no backend/data-model change, no change to scope-filtering logic or folder CRUD handlers — only the sidebar's markup/component layer. `Sidebar`'s desktop-collapsible render path is `position: fixed`, built for a viewport-edge app shell sidebar; since Notes' sidebar lives inside a bounded workbench pane, `NotesDashboard`'s root gains `contain-layout` (CSS `contain: layout`) so that pane becomes the fixed sidebar's containing block instead of the browser viewport.

**Tech Stack:** React + TypeScript + Tailwind v4, shadcn `Sidebar`/`Collapsible` primitives (both already present in `frontend/src/components/ui/`, previously unused/used-once respectively), lucide-react icons.

## Global Constraints

- Do not edit `frontend/src/components/ui/{sidebar,collapsible,dropdown-menu}.tsx` — this plan only consumes them from `NotesDashboard.tsx`.
- Do not touch `Workbench.tsx` — the fixed-position containment fix (`contain-layout`) lives entirely on `NotesDashboard`'s own root.
- No change to `createFolder`/`updateFolder`/`deleteFolder`/scope-filtering/tag-filtering logic, or to any `Dialog`/`AlertDialog` content — only how the sidebar rows that trigger them are rendered.
- `SidebarMenuBadge` (count) and `SidebarMenuAction` ("..." button) share the same trailing visual slot on a folder row (both default to `absolute right-1 ...` in the shared component) — the count must fade out exactly when the action fades in on hover, or they render on top of each other. Give the badge `group-hover/menu-item:opacity-0`.
- Spec: `docs/superpowers/specs/2026-08-15-notes-notion-sidebar-design.md`.
- `docs/superpowers/*` is gitignored; `git add -f` plan/spec files if `git status` does not stage them.

## File structure

- `frontend/src/modules/notes/NotesDashboard.tsx` — only file touched. Imports gain the `Sidebar` family, `Collapsible` family, and `Folder`/`Inbox`/`LayoutGrid`/`ChevronRight` icons; the hand-rolled `<aside>` + `scopeButtonClass` helper are replaced by the `Sidebar` component tree; the toolbar row gains a `SidebarTrigger`; the root `<div>` gains `contain-layout`.

---

### Task 1: Sidebar → shadcn `Sidebar` primitives, with real collapse

**Files:**
- Modify: `frontend/src/modules/notes/NotesDashboard.tsx`

**Interfaces:**
- Consumes: `Sidebar`, `SidebarContent`, `SidebarGroup`, `SidebarGroupAction`, `SidebarGroupContent`, `SidebarGroupLabel`, `SidebarMenu`, `SidebarMenuAction`, `SidebarMenuBadge`, `SidebarMenuButton`, `SidebarMenuItem`, `SidebarProvider`, `SidebarTrigger` (`@/components/ui/sidebar`); `Collapsible`, `CollapsibleContent`, `CollapsibleTrigger` (`@/components/ui/collapsible`); `Folder`, `Inbox`, `LayoutGrid`, `ChevronRight` (`lucide-react`)
- Produces: same `NotesDashboard` public props/behavior — scope selection, folder CRUD dialogs, note grid, search, and tag filter are all unchanged; only the sidebar's DOM/interaction layer changes

- [x] **Step 1: Update imports**

In `frontend/src/modules/notes/NotesDashboard.tsx`, replace the import block (lines 1–47):

```tsx
import { useEffect, useMemo, useState } from "react";
import {
  ChevronRight,
  Folder,
  Inbox,
  LayoutGrid,
  MoreHorizontal,
  Plus,
  X,
} from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuAction,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
} from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import { errorMessage } from "../../bridge/errors";
import {
  createFolder,
  createNote,
  deleteFolder,
  listFolders,
  listNotes,
  searchNotes,
  updateFolder,
  type NoteFolder,
  type NoteSummary,
} from "../../bridge/notes";
import { FOLDER_COLORS, folderDotVar, type FolderColor } from "./folderColors";
import { ColorPicker } from "./ColorPicker";
import { NoteCard } from "./NoteCard";
import { tagChipVar } from "./tagColor";
```

- [x] **Step 2: Remove the now-unused `scopeButtonClass` helper**

Delete the `scopeButtonClass` function (originally lines 343–348, right before the component's `return`):

```tsx
  function scopeButtonClass(active: boolean) {
    return cn(
      "flex w-full items-center gap-2 rounded-sm px-2.5 py-1.5 text-left text-base tracking-[0.2px]",
      active ? "bg-accent text-accent-foreground" : "hover:bg-accent/60",
    );
  }
```

- [x] **Step 3: Replace the sidebar + wrap it (with the note grid `<section>`) in `SidebarProvider`**

Replace from the component's `return (` through the end of the original
`</section>` block (originally lines 350–502 — this covers both the old
`<aside>` and the `<section>` next to it, since `<section>` also moves
inside `SidebarProvider` and gains the `SidebarTrigger`) with:

```tsx
  return (
    <div className="notes-font-scope contain-layout flex h-full min-h-0 flex-1">
      <SidebarProvider className="flex h-full min-h-0 w-full">
        <Sidebar collapsible="icon" className="border-r border-border">
          <SidebarContent>
            <SidebarGroup>
              <SidebarGroupContent>
                <SidebarMenu>
                  <SidebarMenuItem>
                    <SidebarMenuButton
                      isActive={scope === "all"}
                      tooltip="All"
                      onClick={() => setScope("all")}
                    >
                      <LayoutGrid />
                      <span>All</span>
                    </SidebarMenuButton>
                    <SidebarMenuBadge>{counts.all}</SidebarMenuBadge>
                  </SidebarMenuItem>
                  <SidebarMenuItem>
                    <SidebarMenuButton
                      isActive={scope === "ungrouped"}
                      tooltip="Ungrouped"
                      onClick={() => setScope("ungrouped")}
                    >
                      <Inbox />
                      <span>Ungrouped</span>
                    </SidebarMenuButton>
                    <SidebarMenuBadge>{counts.ungrouped}</SidebarMenuBadge>
                  </SidebarMenuItem>
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>

            <Collapsible defaultOpen className="group/folders">
              <SidebarGroup>
                <CollapsibleTrigger asChild>
                  <SidebarGroupLabel className="cursor-pointer">
                    <ChevronRight className="size-3 shrink-0 transition-transform group-data-[state=open]/folders:rotate-90" />
                    <span className="flex-1">Folders</span>
                  </SidebarGroupLabel>
                </CollapsibleTrigger>
                <SidebarGroupAction
                  onClick={() => setCreateOpen(true)}
                  aria-label="New folder"
                  className="opacity-0 group-hover/folders:opacity-100"
                >
                  <Plus />
                </SidebarGroupAction>
                <CollapsibleContent>
                  <SidebarGroupContent>
                    <SidebarMenu>
                      {folders.map((folder) => (
                        <SidebarMenuItem key={folder.id}>
                          <SidebarMenuButton
                            isActive={scope === `folder:${folder.id}`}
                            tooltip={folder.name}
                            onClick={() => setScope(`folder:${folder.id}`)}
                          >
                            <Folder
                              fill={folderDotVar(folder.color)}
                              stroke="none"
                            />
                            <span className="truncate">{folder.name}</span>
                          </SidebarMenuButton>
                          <SidebarMenuBadge className="group-hover/menu-item:opacity-0">
                            {counts.byFolder.get(folder.id) ?? 0}
                          </SidebarMenuBadge>
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <SidebarMenuAction
                                showOnHover
                                aria-label={`${folder.name} options`}
                              >
                                <MoreHorizontal />
                              </SidebarMenuAction>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end" side="right">
                              <DropdownMenuItem
                                onClick={() => {
                                  setRenameFolder(folder);
                                  setRenameName(folder.name);
                                  setRenameColor(
                                    (FOLDER_COLORS.includes(
                                      folder.color as FolderColor,
                                    )
                                      ? folder.color
                                      : "gray") as FolderColor,
                                  );
                                }}
                              >
                                Rename
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                onClick={() => {
                                  setEditTagsFolder(folder);
                                  setEditTags([...folder.tags]);
                                }}
                              >
                                Edit tags
                              </DropdownMenuItem>
                              <DropdownMenuItem
                                variant="destructive"
                                onClick={() => setDeleteTarget(folder)}
                              >
                                Delete
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        </SidebarMenuItem>
                      ))}
                    </SidebarMenu>
                  </SidebarGroupContent>
                </CollapsibleContent>
              </SidebarGroup>
            </Collapsible>
          </SidebarContent>
        </Sidebar>

        <section className="flex min-h-0 flex-1 flex-col">
          <div className="flex flex-wrap items-center gap-2 border-b border-border p-3">
            <SidebarTrigger />
            <Input
              aria-label="note-search"
              placeholder="Search notes..."
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                void refresh(e.target.value);
              }}
              className="h-8 max-w-sm"
            />
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={creating}
              onClick={() => void onCreateNote()}
            >
              <Plus className="size-4" />
              New note
            </Button>
            {selectedTags.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5">
                {selectedTags.map((tag) => (
                  <TagChip
                    key={tag.toLowerCase()}
                    tag={tag}
                    onClick={() => toggleTagFilter(tag)}
                    onRemove={() => toggleTagFilter(tag)}
                  />
                ))}
              </div>
            ) : null}
          </div>

          <ScrollArea className="min-h-0 flex-1">
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
          </ScrollArea>
        </section>
      </SidebarProvider>
```

Leave everything from the four `<Dialog>`/`<AlertDialog>` blocks onward (originally lines 504–613, including the closing `</div>` and `);`/`}`) exactly as-is — only the sidebar/toolbar/`SidebarProvider` wrapping above it changes. The dialogs stay siblings of `SidebarProvider` inside the outer `<div>`, same as they were siblings of the old `<aside>`/`<section>` pair.

- [x] **Step 4: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [x] **Step 5: Manual check**

Run the app, open Notes:

1. Sidebar renders confined inside Notes' rounded card — no bleed over the title bar, no overlap with the dock.
2. Click `SidebarTrigger` (the panel icon at the start of the toolbar row): sidebar collapses to an icon-only rail (All/Ungrouped/folder icons still visible and clickable, with a label tooltip on hover) and back. `⌘/Ctrl+B` does the same. Reload the app — collapsed/expanded state persisted.
3. All / Ungrouped / each folder row still selects scope on click, same as before.
4. Hovering a folder row swaps its count badge for the "..." action (never both visible at once); the dropdown still opens Rename / Edit tags / Delete with identical behavior, including the delete confirmation dialog copy.
5. Hovering anywhere in the "Folders" group (header or an expanded folder row) reveals the "+" on the group header; clicking it opens the same "New folder" dialog as before.
6. Clicking the "Folders" chevron collapses/expands just the folder list; All/Ungrouped stay visible regardless — independent of the whole-sidebar collapse from step 2.
7. Folder icons show the correct color per folder (filled, no outline).
8. No visual/behavioral change to the note grid, search, tag filter, or any other module.

- [x] **Step 6: Commit**

```bash
git add frontend/src/modules/notes/NotesDashboard.tsx
git commit -m "feat(notes): rebuild the sidebar on shadcn Sidebar, Notion-style"
```

---

### Task 2: End-to-end verification

**Files:** none new

- [x] **Step 1: Typecheck**

Run: `cd frontend && bunx tsc --noEmit`

Expected: PASS

- [x] **Step 2: Full manual script**

Repeat Task 1 Step 5's script once more end-to-end in a single pass (sidebar collapse/expand + persistence, folder row hover-swap, group hover "+", folder-group chevron, folder CRUD dialogs, scope selection, note grid/search/tag-filter untouched) to confirm nothing regressed after the full file is in its final state.

If anything fails, fix in a focused commit, then re-run the failing check.
