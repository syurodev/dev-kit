import { ChevronRight, ExternalLink, FilePlus, FileText, MoreHorizontal, Plus, Tag, Trash2, Type } from "lucide-react";
import { useAutoAnimate } from "@formkit/auto-animate/react";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarRow } from "@/components/ui/sidebar-shell";
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
 * Expanding is a second click on the selected row, not a separate chevron
 * target: SidebarRow's icon slot sits inside its button, and a button inside
 * a button is not HTML. The chevron reflects the state; the row is the
 * control.
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
    <div className="flex h-full flex-col gap-0.5 overflow-y-auto py-2 select-none">
      {/* action/new-note (815:4437): a raised, bordered control at the top of
          the rail rather than a row — it makes something, it does not go
          somewhere. */}
      <button
        type="button"
        onClick={onNewNote}
        className="flex h-8 w-full items-center gap-2 rounded-md border border-border bg-surface-raised px-3 text-text-primary hover:border-status-info"
      >
        <Plus className="size-3.5 shrink-0" />
        <span className="flex-1 text-left text-body-13-medium">New note</span>
        <span className="text-code-11 text-text-tertiary">⌘N</span>
      </button>
      <div className="h-1.5 shrink-0" />

      <SidebarRow
        icon={<Chevron hidden />}
        label="All"
        count={counts.all}
        active={scope === "all"}
        onClick={() => onScopeChange("all")}
      />
      <SidebarRow
        icon={<Chevron open={expanded.has("ungrouped")} />}
        label="Ungrouped"
        count={counts.ungrouped}
        active={scope === "ungrouped"}
        onClick={() => selectExpandable("ungrouped", "ungrouped")}
      />
      {expanded.has("ungrouped") ? (
        <NoteRows
          notes={notesIn("ungrouped")}
          onOpenNote={onOpenNote}
          onOpenNoteInTab={onOpenNoteInTab}
          onTrashNote={onTrashNote}
        />
      ) : null}

      <SectionLabel>Folders</SectionLabel>
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
                  onSelect={() => {
                    if (scope !== `folder:${id}`) onScopeChange(`folder:${id}`);
                    onNewNote();
                  }}
                >
                  <FilePlus />
                  New note here
                  <ContextMenuShortcut>⌘N</ContextMenuShortcut>
                </ContextMenuItem>
                <ContextMenuItem onSelect={() => onRenameFolder(folder)}>
                  <Type />
                  Rename
                </ContextMenuItem>
                <ContextMenuItem onSelect={() => onEditFolderTags(folder)}>
                  <Tag />
                  Edit tags
                </ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem variant="destructive" onSelect={() => onDeleteFolder(folder)}>
                  <Trash2 />
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

      <SectionLabel>Maintenance</SectionLabel>
      <SidebarRow
        icon={<Trash2 className="size-3.5 shrink-0" />}
        label="Trash"
        count={trashCount}
        active={scope === "trash"}
        onClick={() => onScopeChange("trash")}
      />
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
 * frame sets these in Geist Mono, but page 13 is largely unbound and
 * design-system.mdx names Label/11 Caps as the uppercase-label step — the
 * documented rule wins over one unbound text layer. */
function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="px-3 pt-3.5 pb-1.5 text-label-11-caps text-text-tertiary">{children}</div>
  );
}

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
  const [listRef] = useAutoAnimate<HTMLDivElement>({ duration: 180, easing: "ease-out" });

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
            <ContextMenuItem onSelect={() => onOpenNoteInTab(note)}>
              <ExternalLink />
              Open in new tab
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem variant="destructive" onSelect={() => onTrashNote(note)}>
              <Trash2 />
              Move to trash
              <ContextMenuShortcut>⌫</ContextMenuShortcut>
            </ContextMenuItem>
          </ContextMenuContent>
        </ContextMenu>
      ))}
    </div>
  );
}
