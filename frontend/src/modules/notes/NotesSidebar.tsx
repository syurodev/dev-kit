import { Folder, Inbox, LayoutGrid, MoreHorizontal, Plus, Trash2 } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarRow, SidebarSection } from "@/components/ui/sidebar-shell";
import { folderDotVar } from "./folderColors";
import type { NoteFolder } from "../../bridge/notes";

export type NotesScope = "all" | "ungrouped" | "trash" | `folder:${string}`;

/**
 * Figma `Notes / Sidebar Content`, built on the shell's own rail primitives.
 *
 * Written in plain markup rather than the shadcn Sidebar components on
 * purpose: SidebarMenuButton calls useSidebar() and throws outside a
 * SidebarProvider, so those cannot live in the shell's rail at all.
 */
export function NotesSidebar({
  scope,
  onScopeChange,
  folders,
  foldersOpen,
  onFoldersOpenChange,
  counts,
  trashCount,
  onCreateFolder,
  onRenameFolder,
  onEditFolderTags,
  onDeleteFolder,
}: {
  scope: NotesScope;
  onScopeChange: (scope: NotesScope) => void;
  folders: NoteFolder[];
  foldersOpen: boolean;
  onFoldersOpenChange: (open: boolean) => void;
  counts: { all: number; ungrouped: number; byFolder: Map<string, number> };
  trashCount: number;
  onCreateFolder: () => void;
  onRenameFolder: (folder: NoteFolder) => void;
  onEditFolderTags: (folder: NoteFolder) => void;
  onDeleteFolder: (folder: NoteFolder) => void;
}) {
  return (
    <div className="flex h-full flex-col select-none">
      <SidebarSection label="Notes" count={counts.all} />
      <div className="space-y-0.5 px-2 pb-2">
        <SidebarRow
          icon={<LayoutGrid className="size-3.5 shrink-0" />}
          label="All"
          count={counts.all}
          active={scope === "all"}
          onClick={() => onScopeChange("all")}
        />
        <SidebarRow
          icon={<Inbox className="size-3.5 shrink-0" />}
          label="Ungrouped"
          count={counts.ungrouped}
          active={scope === "ungrouped"}
          onClick={() => onScopeChange("ungrouped")}
        />
        <SidebarRow
          icon={<Trash2 className="size-3.5 shrink-0" />}
          label="Trash"
          count={trashCount}
          active={scope === "trash"}
          onClick={() => onScopeChange("trash")}
        />
      </div>

      <div className="border-t border-border">
        <SidebarSection
          label="Folders"
          count={folders.length}
          open={foldersOpen}
          onToggle={() => onFoldersOpenChange(!foldersOpen)}
          actions={
            <button
              type="button"
              onClick={onCreateFolder}
              aria-label="New folder"
              title="New folder"
              className="rounded p-0.5 text-text-tertiary hover:text-text-primary"
            >
              <Plus className="size-3.5" />
            </button>
          }
        />
      </div>

      {foldersOpen && (
        <div className="flex-1 space-y-0.5 overflow-y-auto px-2 pb-2">
          {folders.map((folder) => (
            <SidebarRow
              key={folder.id}
              icon={
                <Folder
                  className="size-3.5 shrink-0"
                  fill={folderDotVar(folder.color)}
                  stroke="none"
                />
              }
              label={folder.name}
              count={counts.byFolder.get(folder.id) ?? 0}
              active={scope === `folder:${folder.id}`}
              onClick={() => onScopeChange(`folder:${folder.id}`)}
              trailing={
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <button
                      type="button"
                      aria-label={`${folder.name} options`}
                      className="hidden rounded p-0.5 text-text-tertiary hover:text-text-primary group-hover:block"
                    >
                      <MoreHorizontal className="size-3.5" />
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
          ))}
        </div>
      )}
    </div>
  );
}
