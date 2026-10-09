import { useEffect, useMemo, useState } from "react";
import { Boxes, Check, ChevronDown, GitBranch, RotateCw, Star } from "lucide-react";
import { useShallow } from "zustand/react/shallow";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { useCodeGraphStore } from "../codegraphStore";
import { cancelIndexing, reindexFolders, reindexScope, reindexWorkspace, retryFailed, setScope } from "../workspaceSync";
import { folderHealth, summarizeHealth, batchProgress, type FolderHealth, type Tone } from "../workspace/indexHealth";
import { WHOLE_WORKSPACE, scopeFolderIds, scopeSummary, toggleScopeFolder } from "../workspace/scope";
import { folderLabels } from "../workspace/identity";

/** Re-renders every `ms`, for "2m ago" to keep counting. */
export function useNow(ms = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), ms);
    return () => window.clearInterval(t);
  }, [ms]);
  return now;
}

export const DOT: Record<Tone, string> = {
  ok: "bg-status-success",
  busy: "bg-status-info animate-pulse motion-reduce:animate-none",
  warn: "bg-status-warning",
  failed: "bg-status-danger",
  neutral: "bg-text-tertiary",
};

const CARD = "rounded-lg border border-border-subtle bg-surface-raised";

/** Each folder's health, in workspace order, from what the store knows. */
export function useFolderHealths(): Array<{ id: string; health: FolderHealth }> {
  const now = useNow();
  const folders = useCodeGraphStore((s) => s.folders);
  const data = useCodeGraphStore((s) => s.data);
  return useMemo(
    () => folders.map((f) => ({ id: f.id, health: folderHealth(data[f.id]?.state ?? undefined, data[f.id]?.entries ?? undefined, data[f.id]?.indexStatus ?? undefined, now) })),
    [folders, data, now],
  );
}

/**
 * The workspace, and what the sidebar is looking at in it.
 *
 * It names the workspace, says the scope in words ("Whole workspace · 4
 * folders", "Viewing: api"), and sums up how well the folders can be read.
 * Choosing a scope narrows the lists below it and nothing else: it does not
 * open a folder as "the project", close a tab, or touch the primary. Reindex
 * defaults to the scope; "whole workspace" is one click further, and the
 * per-folder state is one click away in the details.
 */
export function WorkspaceHeader() {
  const workspaceName = useCodeGraphStore((s) => s.workspace?.name ?? "");
  const folders = useCodeGraphStore((s) => s.folders);
  const scope = useCodeGraphStore((s) => s.scope);
  const labels = useMemo(() => folderLabels(folders.map((f) => ({ id: f.id, name: f.name, path: f.path }))), [folders]);
  const summary = scopeSummary(scope, folders, labels);
  const healths = useFolderHealths();
  const scoped = useMemo(() => new Set(scopeFolderIds(scope, folders)), [scope, folders]);
  const sum = useMemo(() => summarizeHealth(healths.filter((h) => scoped.has(h.id)).map((h) => h.health)), [healths, scoped]);
  const progress = useCodeGraphStore(
    useShallow((s) => batchProgress(s.folders.filter((f) => scoped.has(f.id)).map((f) => s.data[f.id]?.indexStatus ?? undefined))),
  );
  const busy = progress !== null;

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-stretch gap-1.5">
        <ScopePicker workspaceName={workspaceName} summary={summary} />
        <ReindexMenu busy={busy} scopeCount={scoped.size} />
      </div>
      <IndexSummary tone={sum.tone} text={sum.text} progress={progress} scopeLabel={scope.kind === "all" ? "" : `in ${summary.replace("Viewing: ", "")}`} />
    </div>
  );
}

function ScopePicker({ workspaceName, summary }: { workspaceName: string; summary: string }) {
  const folders = useCodeGraphStore((s) => s.folders);
  const primaryId = useCodeGraphStore((s) => s.primaryId);
  const scope = useCodeGraphStore((s) => s.scope);
  const data = useCodeGraphStore((s) => s.data);
  const labels = useMemo(() => folderLabels(folders.map((f) => ({ id: f.id, name: f.name, path: f.path }))), [folders]);
  const covered = new Set(scopeFolderIds(scope, folders));

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={`Workspace ${workspaceName}. ${summary}. Change what is shown`}
          className={cn(CARD, "flex min-w-0 flex-1 items-center gap-2 py-1.5 pr-2 pl-2.5 text-left hover:bg-state-soft")}
        >
          <Boxes className="size-4 shrink-0 text-text-secondary" aria-hidden />
          <span className="flex min-w-0 flex-1 flex-col">
            <span className="truncate text-title-13 text-text-primary">{workspaceName || "Workspace"}</span>
            <span className="truncate text-caption-11 text-text-tertiary" title={summary}>{summary}</span>
          </span>
          <ChevronDown className="size-3.5 shrink-0 text-text-tertiary" aria-hidden />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 gap-1.5 p-1.5">
        <p className="px-2 pt-1 text-label-11-caps text-text-tertiary">Show</p>
        <ScopeRow
          selected={scope.kind === "all"}
          title="Whole workspace"
          detail={`${folders.length} folder${folders.length === 1 ? "" : "s"}`}
          onSelect={() => setScope(WHOLE_WORKSPACE)}
        />
        <div className="max-h-72 overflow-y-auto" role="group" aria-label="Folders">
          {folders.map((f) => {
            const st = data[f.id]?.state;
            const checked = covered.has(f.id);
            return (
              <label
                key={f.id}
                className="group flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-state-soft"
              >
                <Checkbox checked={checked} onCheckedChange={() => setScope(toggleScopeFolder(scope, f.id, folders))} aria-label={labels[f.id]} />
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="flex items-center gap-1.5">
                    <span className="truncate text-body-13 text-text-primary">{labels[f.id]}</span>
                    {f.id === primaryId ? <PrimaryBadge /> : null}
                  </span>
                  <span className="truncate text-code-11 text-text-tertiary" title={f.path}>
                    {st?.isRepo && st.branch ? `${st.branch} · ` : ""}
                    {f.path}
                  </span>
                </span>
                <button
                  type="button"
                  className="invisible shrink-0 rounded-sm px-1.5 py-0.5 text-code-11 text-text-tertiary group-focus-within:visible group-hover:visible hover:bg-surface-raised hover:text-text-primary"
                  onClick={(e) => {
                    e.preventDefault();
                    setScope({ kind: "folders", ids: [f.id] });
                  }}
                >
                  Only
                </button>
              </label>
            );
          })}
        </div>
        <p className="px-2 pb-1 text-caption-11 text-text-tertiary">
          This narrows the lists. It closes no tab, keeps every unsaved edit, and does not change the primary folder.
        </p>
      </PopoverContent>
    </Popover>
  );
}

function ScopeRow({ selected, title, detail, onSelect }: { selected: boolean; title: string; detail: string; onSelect: () => void }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className="flex items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-state-soft"
    >
      <span className="flex size-4.5 shrink-0 items-center justify-center">{selected ? <Check className="size-4 text-text-primary" aria-hidden /> : null}</span>
      <span className="min-w-0 flex-1 truncate text-body-13 text-text-primary">{title}</span>
      <span className="shrink-0 text-code-11 text-text-tertiary">{detail}</span>
    </button>
  );
}

/** A light mark on the primary folder: where agents start by default, nothing more. */
export function PrimaryBadge() {
  return (
    <span
      title="Primary: where an agent session starts by default. It does not decide what is shown or saved."
      className="inline-flex shrink-0 items-center gap-0.5 rounded-sm bg-state-soft px-1 text-code-11 text-text-tertiary"
    >
      <Star className="size-2.5" aria-hidden />
      primary
    </span>
  );
}

function ReindexMenu({ busy, scopeCount }: { busy: boolean; scopeCount: number }) {
  const failed = useCodeGraphStore((s) => s.folders.filter((f) => s.data[f.id]?.indexStatus?.state === "failed").length);
  const folderCount = useCodeGraphStore((s) => s.folders.length);
  const scope = useCodeGraphStore((s) => s.scope);
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label="Reindex"
              disabled={folderCount === 0}
              className={cn(CARD, "flex w-11 shrink-0 items-center justify-center text-text-secondary hover:bg-state-soft disabled:opacity-60")}
            >
              <RotateCw className={cn("size-4", busy && "animate-spin motion-reduce:animate-none")} />
            </button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>Reindex</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuItem icon={<RotateCw />} onSelect={() => void reindexScope()}>
          {scope.kind === "all" ? `Reindex all ${scopeCount} folders` : `Reindex ${scopeCount} folder${scopeCount === 1 ? "" : "s"} in view`}
        </DropdownMenuItem>
        {scope.kind !== "all" ? (
          <DropdownMenuItem icon={<RotateCw />} onSelect={() => void reindexWorkspace()}>
            Reindex whole workspace ({folderCount})
          </DropdownMenuItem>
        ) : null}
        {failed > 0 ? (
          <DropdownMenuItem icon={<RotateCw />} onSelect={() => void retryFailed()}>
            Retry {failed} failed
          </DropdownMenuItem>
        ) : null}
        {busy ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => void cancelIndexing()}>Cancel queued indexing</DropdownMenuItem>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function IndexSummary({ tone, text, progress, scopeLabel }: { tone: Tone; text: string; progress: { done: number; total: number } | null; scopeLabel: string }) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button type="button" className="flex min-w-0 items-center gap-1.5 rounded-sm pl-1 text-left hover:text-text-primary" aria-label={`Index: ${text}. Show details by folder`}>
          <span className={cn("size-1.5 shrink-0 rounded-full", DOT[tone])} aria-hidden />
          <span className="min-w-0 truncate text-caption-11 text-text-tertiary" title={text} role="status">
            {text}
            {scopeLabel ? ` ${scopeLabel}` : ""}
            {progress ? ` · ${progress.done}/${progress.total}` : ""}
          </span>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-96 gap-1 p-1.5">
        <IndexDetails />
      </PopoverContent>
    </Popover>
  );
}

/** Every folder of the workspace with its own state — never one number standing for all. */
function IndexDetails() {
  const folders = useCodeGraphStore((s) => s.folders);
  const primaryId = useCodeGraphStore((s) => s.primaryId);
  const data = useCodeGraphStore((s) => s.data);
  const labels = useMemo(() => folderLabels(folders.map((f) => ({ id: f.id, name: f.name, path: f.path }))), [folders]);
  const healths = useFolderHealths();
  const byId = new Map(healths.map((h) => [h.id, h.health]));

  return (
    <>
      <p className="px-2 pt-1 text-label-11-caps text-text-tertiary">Index by folder</p>
      <ul className="max-h-80 overflow-y-auto">
        {folders.map((f) => {
          const h = byId.get(f.id);
          const entries = data[f.id]?.entries;
          const st = data[f.id]?.state;
          return (
            <li key={f.id} className="flex items-start gap-2 rounded-md px-2 py-1.5">
              <span className={cn("mt-1.5 size-1.5 shrink-0 rounded-full", DOT[h?.tone ?? "neutral"])} aria-hidden />
              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                <div className="flex items-center gap-1.5">
                  <span className="truncate text-body-13 text-text-primary">{labels[f.id]}</span>
                  {f.id === primaryId ? <PrimaryBadge /> : null}
                  {st?.isRepo && st.branch ? (
                    <span className="inline-flex shrink-0 items-center gap-0.5 text-code-11 text-text-tertiary">
                      <GitBranch className="size-2.5" aria-hidden />
                      {st.branch}
                    </span>
                  ) : null}
                </div>
                <span className="text-code-11 text-text-secondary">
                  {h?.label ?? "Checking"}
                  {entries?.state === "indexed" ? ` · ${entries.entries.length} entry point${entries.entries.length === 1 ? "" : "s"}` : ""}
                </span>
                {h?.detail ? <span className="text-code-11 break-words text-text-tertiary">{h.detail}</span> : null}
              </div>
              <Button
                size="xs"
                variant="ghost"
                aria-label={`Reindex ${labels[f.id]}`}
                disabled={h?.health === "indexing" || h?.health === "pending" || h?.health === "unavailable"}
                onClick={() => void reindexFolders([f.id])}
              >
                <RotateCw aria-hidden />
              </Button>
            </li>
          );
        })}
      </ul>
      <p className="px-2 pb-1 text-caption-11 text-text-tertiary">
        One folder failing or indexing never stops another's files from being read, searched or edited.
      </p>
    </>
  );
}

