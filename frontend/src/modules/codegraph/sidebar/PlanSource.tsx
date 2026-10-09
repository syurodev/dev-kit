import { ChevronDown, FolderGit2 } from "lucide-react";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { SidebarInset } from "@/components/ui/sidebar-shell";
import type { WorkspaceService } from "@/bridge/workspace";

/**
 * Whose plan the Plan tab shows. A plan lives in one folder's index, so there is
 * no plan of "the workspace": the tab shows one folder's and says which, with a
 * choice among the folders in view. Nothing is merged across folders.
 */
export function PlanSource({
  folders,
  labels,
  selectedId,
  onSelect,
}: {
  folders: WorkspaceService[];
  labels: Record<string, string>;
  selectedId: string;
  onSelect: (folderId: string) => void;
}) {
  const current = folders.find((f) => f.id === selectedId);
  return (
    <SidebarInset className="flex flex-col gap-1">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            aria-label={`Plan of ${current ? labels[current.id] : "no folder"}. Choose another folder`}
            disabled={folders.length === 0}
            className="flex min-w-0 items-center gap-1.5 rounded-md border border-border-subtle bg-surface-raised px-2 py-1 text-left text-code-11 text-text-secondary hover:bg-state-soft disabled:opacity-60"
          >
            <FolderGit2 className="size-3 shrink-0" aria-hidden />
            <span className="shrink-0 text-text-tertiary">Plan of</span>
            <span className="min-w-0 flex-1 truncate text-text-primary" title={current?.path}>{current ? labels[current.id] : "choose a folder"}</span>
            <ChevronDown className="size-3 shrink-0 text-text-tertiary" aria-hidden />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-64">
          <DropdownMenuLabel>A plan belongs to one folder</DropdownMenuLabel>
          {folders.map((f) => (
            <DropdownMenuItem key={f.id} icon={<FolderGit2 />} onSelect={() => onSelect(f.id)}>
              <span className="truncate">{labels[f.id]}</span>
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarInset>
  );
}
