import { Inbox, LayoutGrid, MoreHorizontal, Plus, Trash2 } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { SidebarRow } from "@/components/ui/sidebar-shell";
import { folderDotVar } from "./folderColors";
import type { NoteFolder } from "../../bridge/notes";

export type NotesScope = "all" | "ungrouped" | "trash" | `folder:${string}`;

/**
 * Figma `Notes / Sidebar Content` (424:303), on the shell's rail primitives.
 *
 * The component has no section headings: folders are rows carrying their own
 * colour as a dot, and Trash sits at the bottom behind a rule rather than
 * third in the top group. Its metrics are rounded onto the 4px grid the
 * Foundations page mandates — see SidebarRow for why.
 *
 * Plain markup rather than the shadcn Sidebar components: SidebarMenuButton
 * calls useSidebar() and throws outside a SidebarProvider, so those cannot
 * live in the shell's rail at all.
 */
export function NotesSidebar({
  scope,
  onScopeChange,
  folders,
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
  counts: { all: number; ungrouped: number; byFolder: Map<string, number> };
  trashCount: number;
  onCreateFolder: () => void;
  onRenameFolder: (folder: NoteFolder) => void;
  onEditFolderTags: (folder: NoteFolder) => void;
  onDeleteFolder: (folder: NoteFolder) => void;
}) {
  return (
    <div className="flex h-full flex-col select-none p-4">
      <div className="flex-1 space-y-1 overflow-y-auto">
        <SidebarRow
          icon={<LayoutGrid className="size-4 shrink-0" />}
          label="All"
          count={counts.all}
          active={scope === "all"}
          onClick={() => onScopeChange("all")}
        />
        <SidebarRow
          icon={<Inbox className="size-4 shrink-0" />}
          label="Ungrouped"
          count={counts.ungrouped}
          active={scope === "ungrouped"}
          onClick={() => onScopeChange("ungrouped")}
        />

        {folders.map((folder) => (
          <SidebarRow
            key={folder.id}
            icon={
              // A folder is its colour: an 8px dot, not a tinted folder glyph.
              <span
                className="size-2 shrink-0 rounded-full"
                style={{ backgroundColor: folderDotVar(folder.color) }}
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
                    className="hidden rounded-md p-1 text-text-tertiary hover:text-text-primary group-hover:block"
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
        ))}

        <SidebarRow
          icon={<Plus className="size-4 shrink-0" />}
          label="New folder"
          onClick={onCreateFolder}
        />
      </div>

      {/* Trash is pinned to the bottom behind a rule, as the component draws
          it — it is where things go, not another way to browse them. */}
      <div className="mt-4 border-t border-border pt-2">
        <SidebarRow
          icon={<Trash2 className="size-4 shrink-0" />}
          label="Trash"
          count={trashCount}
          active={scope === "trash"}
          onClick={() => onScopeChange("trash")}
        />
      </div>
    </div>
  );
}
