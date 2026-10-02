import { ChevronRight, ExternalLink, FilePlus, FileText, MoreHorizontal, Plus, Tag, Trash2, Type } from "lucide-react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ActionTooltip } from "@/components/ui/action-tooltip";
import { Button } from "@/components/ui/button";
import { SidebarInset, SidebarRow, SidebarSection } from "@/components/ui/sidebar-shell";
import { cn } from "@/lib/utils";
import type { NoteFolder, NoteSummary } from "../../bridge/notes";
import { folderDotVar } from "./folderColors";
import { relativeTime } from "./relativeTime";

export type NotesScope = "all" | "ungrouped" | "trash" | `folder:${string}`;

/**
 * Figma `Notes / Sidebar Content`, Selected=All (812:3731), on the shell's
 * rail primitives.
 *
 * Top to bottom: a "New note" action, All, Ungrouped, a FOLDERS label with
 * each folder as a row carrying its colour as a dot, "New folder", then a
 * MAINTENANCE label with Trash. Ungrouped and every folder are expandable
 * and list their notes underneath as 28px rows indented past the dot column.
 *
 * Expanding is a second click on the selected row, not a chevron target:
 * SidebarRow's icon slot is inside its button, and a button inside a button is
 * not HTML. The chevron reflects state; the row is the control.
 *
 * Plain markup rather than the shadcn Sidebar components: SidebarMenuButton
 * calls useSidebar() and throws outside a SidebarProvider, so those cannot
 * live in the shell's rail at all.
 */
export function NotesSidebar({
  scope,
  onScopeChange,
  folders,
  notes,
  counts,
  trashCount,
  expanded,
  onToggleExpanded,
  onNewNote,
  onOpenNote,
  onOpenNoteInTab,
  onTrashNote,
  onCreateFolder,
  onRenameFolder,
  onEditFolderTags,
  onDeleteFolder,
}: {
  scope: NotesScope;
  onScopeChange: (scope: NotesScope) => void;
  folders: NoteFolder[];
  notes: NoteSummary[];
  counts: { all: number; ungrouped: number; byFolder: Map<string, number> };
  trashCount: number;
  expanded: Set<string>;
  onToggleExpanded: (id: string) => void;
  onNewNote: () => void;
  onOpenNote: (note: NoteSummary) => void;
  /** Open pinned — "Open in new tab" in the note's context menu. */
  onOpenNoteInTab: (note: NoteSummary) => void;
  onTrashNote: (note: NoteSummary) => void;
  onCreateFolder: () => void;
  onRenameFolder: (folder: NoteFolder) => void;
  onEditFolderTags: (folder: NoteFolder) => void;
  onDeleteFolder: (folder: NoteFolder) => void;
}) {
  const folderIds = new Set(folders.map((f) => f.id));

  function notesIn(id: string): NoteSummary[] {
    if (id === "ungrouped") {
      return notes.filter((n) => !n.folderId || !folderIds.has(n.folderId));
    }
    return notes.filter((n) => n.folderId === id);
  }

  // Selecting a row selects its scope and opens it; selecting it again folds
  // it. The chevron only reports which of those the next click will do.
  function selectExpandable(id: string, nextScope: NotesScope) {
    if (scope === nextScope) {
      onToggleExpanded(id);
      return;
    }
    onScopeChange(nextScope);
    if (!expanded.has(id)) {
      onToggleExpanded(id);
    }
  }

  return (
    // The scroller reaches the rail's edge; the rows sit in the shared gutter
    // (SidebarInset), so an active row's ground stops where every other
    // rail's does instead of touching the rail's edge.
    <div className="h-full overflow-y-auto select-none">
      <SidebarInset className="flex flex-col gap-0.5 pb-2">
        {/* The rail's header is the shared one: a section label with its
            actions, the same shape as Agent Hub's swarm and Database's tree.
            It used to be a raised full-width "New note" control (815:4437),
            which stood beside the dashboard's own New note button — two
            buttons for one action on the same screen. */}
        <SidebarSection
          first
          label="Notes"
          actions={
            <ActionTooltip label="New note">
              <Button variant="ghost" size="icon-xs" aria-label="New note" onClick={onNewNote}>
                <Plus />
              </Button>
            </ActionTooltip>
          }
        />

        <SidebarRow
          icon={<Chevron hidden />}
          label="All"
          count={counts.all}
          active={scope === "all"}
          onClick={() => onScopeChange("all")}
        />
        {/* With nothing ungrouped there is nothing to unfold: the row keeps
            its scope (an empty list is still an answer) but drops the chevron
            and the "No notes" line it used to open onto. */}
        <SidebarRow
          icon={<Chevron open={expanded.has("ungrouped")} hidden={counts.ungrouped === 0} />}
          label="Ungrouped"
          count={counts.ungrouped}
          active={scope === "ungrouped"}
          onClick={() =>
            counts.ungrouped === 0 ? onScopeChange("ungrouped") : selectExpandable("ungrouped", "ungrouped")
          }
        />
        {expanded.has("ungrouped") && counts.ungrouped > 0 ? (
          <NoteRows
            notes={notesIn("ungrouped")}
            onOpenNote={onOpenNote}
            onOpenNoteInTab={onOpenNoteInTab}
            onTrashNote={onTrashNote}
          />
        ) : null}

        <SidebarSection label="Folders" />
        {folders.map((folder) => {
          const id = folder.id;
          const isOpen = expanded.has(id);
          return (
            <div key={id} className="contents">
              <ContextMenu>
                <ContextMenuTrigger asChild>
              <SidebarRow
                icon={
                  <>
                    <Chevron open={isOpen} />
                    {/* A folder is its colour: an 8px dot, not a tinted glyph. */}
                    <span
                      className="size-2 shrink-0 rounded-full"
                      style={{ backgroundColor: folderDotVar(folder.color) }}
                    />
                  </>
                }
                label={folder.name}
                count={counts.byFolder.get(id) ?? 0}
                active={scope === `folder:${id}`}
                onClick={() => selectExpandable(id, `folder:${id}`)}
                trailing={
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <button
                        type="button"
                        aria-label={`${folder.name} options`}
                        // Opacity, not display: `hidden` gave the button no box
                        // until hover, so revealing it grew the row and shoved the
                        // count sideways. Reserving the space keeps the row still.
                        // Focus-visible too, or it is unreachable by keyboard.
                        className="rounded-md p-1 text-text-tertiary opacity-0 hover:text-text-primary focus-visible:opacity-100 group-hover:opacity-100"
                      >
                        <MoreHorizontal className="size-4" />
                      </button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" side="right">
                      <DropdownMenuItem onSelect={() => onRenameFolder(folder)}>
                        Rename
                      </DropdownMenuItem>
                      <DropdownMenuItem onSelect={() => onEditFolderTags(folder)}>
                        Edit tags
                      </DropdownMenuItem>
                      <DropdownMenuItem
                        variant="destructive"
                        onSelect={() => onDeleteFolder(folder)}
                      >
                        Delete
                      </DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                }
              />
                </ContextMenuTrigger>
                <ContextMenuContent>
                  <ContextMenuItem
                    icon={<FilePlus />}
                    onSelect={() => {
                      if (scope !== `folder:${id}`) onScopeChange(`folder:${id}`);
                      onNewNote();
                    }}
                  >
                    New note here
                  </ContextMenuItem>
                  <ContextMenuItem icon={<Type />} onSelect={() => onRenameFolder(folder)}>
                    Rename
                  </ContextMenuItem>
                  <ContextMenuItem icon={<Tag />} onSelect={() => onEditFolderTags(folder)}>
                    Edit tags
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                  <ContextMenuItem icon={<Trash2 />} variant="destructive" onSelect={() => onDeleteFolder(folder)}>
                    Delete folder
                  </ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>
              {isOpen ? (
                <NoteRows
                  notes={notesIn(id)}
                  onOpenNote={onOpenNote}
                  onOpenNoteInTab={onOpenNoteInTab}
                  onTrashNote={onTrashNote}
                />
              ) : null}
            </div>
          );
        })}
        <SidebarRow
          icon={<Plus className="size-3.5 shrink-0 text-text-tertiary" />}
          label="New folder"
          onClick={onCreateFolder}
        />

        <SidebarSection label="Maintenance" />
        <SidebarRow
          icon={<Trash2 className="size-3.5 shrink-0" />}
          label="Trash"
          count={trashCount}
          active={scope === "trash"}
          onClick={() => onScopeChange("trash")}
        />
      </SidebarInset>
    </div>
  );
}

/** 14px chevron slot. `hidden` keeps the column so "All" lines up with the
 * expandable rows below it, as the Figma row does with an invisible glyph. */
function Chevron({ open, hidden }: { open?: boolean; hidden?: boolean }) {
  return (
    <ChevronRight
      className={cn(
        "size-3.5 shrink-0 text-text-tertiary transition-transform",
        open && "rotate-90",
        hidden && "invisible",
      )}
    />
  );
}

/** label/FOLDERS, label/MAINTENANCE: Label/11 Caps, pt-14 pb-6. The Figma
/** Note Row (811:3500): h-28, indented past the chevron + dot columns, file
 * glyph, name in Body/13, age in Code/11. */
function NoteRows({
  notes,
  onOpenNote,
  onOpenNoteInTab,
  onTrashNote,
}: {
  notes: NoteSummary[];
  onOpenNote: (note: NoteSummary) => void;
  onOpenNoteInTab: (note: NoteSummary) => void;
  onTrashNote: (note: NoteSummary) => void;
}) {
  const [listRef] = useAutoAnimate<HTMLDivElement>({ duration: 180, easing: "cubic-bezier(0.23, 1, 0.32, 1)" });

  if (notes.length === 0) {
    return (
      <div className="pl-12 pr-3 text-caption-11 text-text-tertiary">No notes</div>
    );
  }
  return (
    <div ref={listRef} className="flex flex-col gap-0.5">
      {notes.map((note) => (
        <ContextMenu key={note.id}>
          <ContextMenuTrigger asChild>
            <button
              type="button"
              onClick={() => onOpenNote(note)}
              className="flex h-7 w-full items-center gap-2 rounded-md pl-12 pr-3 text-left text-text-secondary hover:bg-surface-raised hover:text-text-primary"
            >
              <FileText className="size-3.5 shrink-0 text-text-tertiary" />
              <span className="min-w-0 flex-1 truncate text-body-13">
                {note.title || "Untitled"}
              </span>
              <span className="shrink-0 text-code-11 text-text-tertiary">
                {relativeTime(note.updatedAt)}
              </span>
            </button>
          </ContextMenuTrigger>
          {/* Figma also lists "Move to folder…" and "Version history"; the
              first has no picker outside the editor's meta panel and the
              second lives in the editor, so both open the note instead. */}
          <ContextMenuContent>
            <ContextMenuItem icon={<ExternalLink />} onSelect={() => onOpenNoteInTab(note)}>
              Open in new tab
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem icon={<Trash2 />} shortcut="⌫" variant="destructive" onSelect={() => onTrashNote(note)}>
              Move to trash
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ))}
    </div>
  );
}
