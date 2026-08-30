import { ChevronDown, ChevronRight, Folder, Inbox, LayoutGrid, MoreHorizontal, Plus, Trash2 } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { folderDotVar } from "./folderColors";
import type { NoteFolder } from "../../bridge/notes";

export type NotesScope = "all" | "ungrouped" | "trash" | `folder:${string}`;

/**
 * Figma `Notes / Sidebar Content`, registered through the shell rail.
 *
 * Written in plain markup rather than the shadcn Sidebar primitives on
 * purpose: SidebarMenuButton calls useSidebar() and throws outside a
 * SidebarProvider, so those components cannot live in the shell's rail at all.
 * Notes was the only module still painting its own rail — Database and
 * CodeGraph both register content instead.
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
      <div className="space-y-0.5 p-2">
        <ScopeRow
          icon={<LayoutGrid className="size-3.5" />}
          label="All"
          count={counts.all}
          active={scope === "all"}
          onClick={() => onScopeChange("all")}
        />
        <ScopeRow
          icon={<Inbox className="size-3.5" />}
          label="Ungrouped"
          count={counts.ungrouped}
          active={scope === "ungrouped"}
          onClick={() => onScopeChange("ungrouped")}
        />
        <ScopeRow
          icon={<Trash2 className="size-3.5" />}
          label="Trash"
          count={trashCount}
          active={scope === "trash"}
          onClick={() => onScopeChange("trash")}
        />
      </div>

      <div className="flex items-center justify-between border-t border-border px-3 py-2">
        <button
          type="button"
          onClick={() => onFoldersOpenChange(!foldersOpen)}
          className="flex items-center gap-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-text-tertiary hover:text-text-secondary"
        >
          {foldersOpen ? (
            <ChevronDown className="size-3" />
          ) : (
            <ChevronRight className="size-3" />
          )}
          Folders
          <span className="font-mono text-text-tertiary/70">{folders.length}</span>
        </button>
        <button
          type="button"
          onClick={onCreateFolder}
          aria-label="New folder"
          title="New folder"
          className="rounded p-0.5 text-text-tertiary hover:text-text-primary"
        >
          <Plus className="size-3.5" />
        </button>
      </div>

      {foldersOpen && (
        <div className="flex-1 space-y-0.5 overflow-y-auto p-2 pt-0">
          {folders.map((folder) => (
            <div
              key={folder.id}
              className={`group flex items-center gap-1.5 rounded px-2 py-1 text-xs ${
                scope === `folder:${folder.id}`
                  ? "bg-surface-raised text-text-primary"
                  : "text-text-secondary hover:bg-surface-raised"
              }`}
            >
              <button
                type="button"
                onClick={() => onScopeChange(`folder:${folder.id}`)}
                className="flex min-w-0 flex-1 items-center gap-1.5 text-left hover:text-text-primary"
              >
                <Folder
                  className="size-3.5 shrink-0"
                  fill={folderDotVar(folder.color)}
                  stroke="none"
                />
                <span className="truncate">{folder.name}</span>
              </button>
              <span className="font-mono text-[10px] text-text-tertiary group-hover:hidden">
                {counts.byFolder.get(folder.id) ?? 0}
              </span>
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
                  <DropdownMenuItem variant="destructive" onSelect={() => onDeleteFolder(folder)}>
                    Delete
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function ScopeRow({
  icon,
  label,
  count,
  active,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex w-full items-center gap-1.5 rounded px-2 py-1 text-xs ${
        active
          ? "bg-surface-raised text-text-primary"
          : "text-text-secondary hover:bg-surface-raised hover:text-text-primary"
      }`}
    >
      {icon}
      <span className="flex-1 text-left">{label}</span>
      <span className="font-mono text-[10px] text-text-tertiary">{count}</span>
    </button>
  );
}
