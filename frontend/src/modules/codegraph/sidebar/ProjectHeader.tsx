import { useEffect, useState } from "react";
import { Check, ChevronDown, FolderGit2, RotateCw } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import type { CodeGraphIndexInfo } from "@/bridge/codegraph";
import { useCurrentWorkspaceServices } from "@/stores/workspaceSwitcher";
import { indexStatusLine, projectLabel, type IndexTone } from "./sidebarModel";

/** Re-renders every `ms`, for "2m ago" to keep counting. */
export function useNow(ms = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), ms);
    return () => window.clearInterval(t);
  }, [ms]);
  return now;
}

const DOT: Record<IndexTone, string> = {
  ok: "bg-status-success",
  busy: "bg-status-info animate-pulse",
  failed: "bg-status-danger",
};

const CARD = "rounded-lg border border-border-subtle bg-surface-raised";

/**
 * Figma `project-header` (1192:69479): the project switcher, the re-index
 * button beside it, and the line that says how fresh the index is.
 */
export function ProjectHeader({
  projectPath,
  indexInfo,
  counts,
  busy,
  onReindex,
  onOpenProject,
}: {
  projectPath: string;
  indexInfo: CodeGraphIndexInfo | null;
  counts: { endpoints: number; services: number };
  busy: boolean;
  onReindex: () => void;
  onOpenProject: (path: string) => void;
}) {
  const now = useNow();
  const { services } = useCurrentWorkspaceServices();
  const { name, where } = projectLabel(projectPath);
  const branch = indexInfo?.branch;
  const status = indexStatusLine(busy && indexInfo ? { ...indexInfo, indexing: true } : indexInfo, counts, now);

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-stretch gap-1.5">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label={`Project: ${name}. Switch project`}
              className={cn(CARD, "flex min-w-0 flex-1 items-center gap-2 py-1.5 pr-2 pl-2.5 text-left hover:bg-state-soft")}
            >
              <FolderGit2 className="size-4 shrink-0 text-text-secondary" aria-hidden />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate text-title-13 text-text-primary">{name}</span>
                <span className="truncate text-code-11 text-text-tertiary">{branch ? `${branch} · ${where}` : where}</span>
              </span>
              <ChevronDown className="size-3.5 shrink-0 text-text-tertiary" aria-hidden />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-72">
            {services.length === 0 ? (
              <DropdownMenuItem disabled>No workspace selected</DropdownMenuItem>
            ) : (
              <DropdownMenuLabel>Workspace services</DropdownMenuLabel>
            )}
            {services.map((svc) => {
              const current = svc.path === projectPath;
              return (
                <DropdownMenuItem
                  key={svc.id}
                  icon={current ? <Check /> : <FolderGit2 />}
                  disabled={busy}
                  // Two lines, a name over its folder: taller than the menu's
                  // one-line row, which would draw them over each other.
                  className="h-auto min-h-11 py-1.5"
                  onSelect={() => !current && onOpenProject(svc.path)}
                >
                  <span className="flex min-w-0 flex-col">
                    <span className="truncate">{svc.name}</span>
                    <span className="truncate text-code-11 text-text-tertiary">{svc.path}</span>
                  </span>
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuContent>
        </DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              type="button"
              aria-label="Re-index project"
              disabled={busy}
              onClick={onReindex}
              className={cn(CARD, "flex w-11 shrink-0 items-center justify-center text-text-secondary hover:bg-state-soft disabled:opacity-60")}
            >
              <RotateCw className={cn("size-4", busy && "animate-spin")} />
            </button>
          </TooltipTrigger>
          <TooltipContent>Re-index project</TooltipContent>
        </Tooltip>
      </div>
      <div className="flex items-center gap-1.5 pl-1" role="status">
        <span className={cn("size-1.5 shrink-0 rounded-full", DOT[status.tone])} aria-hidden />
        <span className="min-w-0 truncate text-caption-11 text-text-tertiary" title={status.text}>
          {status.text}
        </span>
      </div>
    </div>
  );
}
