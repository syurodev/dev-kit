import { useCallback, useEffect, useState } from "react";
import { ChevronDown, GitBranch, RotateCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { gitProject, gitRepositories, type Repository } from "@/bridge/git";
import { cn } from "@/lib/utils";
import { describeRepository } from "./repositories";

/**
 * The repositories at or under `root`, and a way to read them again.
 *
 * Read on demand rather than watched: a change count is stale the moment it is
 * drawn, and the places it is shown — a list, a menu just opened — are exactly
 * where it is cheap to ask again. `repos` is null until this root has been
 * read, so a list for the previous folder is never shown under the new one.
 */
export function useRepositories(root: string) {
  const [read, setRead] = useState<{ root: string; repos: Repository[]; error: string } | null>(null);
  const [loading, setLoading] = useState(false);

  const reload = useCallback(() => {
    if (!root) return;
    setLoading(true);
    gitRepositories(root)
      .then((repos) => setRead({ root, repos, error: "" }))
      .catch((e: Error) => setRead({ root, repos: [], error: e.message }))
      .finally(() => setLoading(false));
  }, [root]);

  useEffect(reload, [reload]);

  const current = read?.root === root ? read : null;
  return { repos: current?.repos ?? null, error: current?.error ?? "", loading, reload };
}

/**
 * The repository Git is reading in `folder`, "" when it is in none, and null
 * until Go has been asked about this folder — so an answer about the previous
 * folder is never taken for this one's.
 */
export function useGitProject(folder: string): string | null {
  const [asked, setAsked] = useState<{ folder: string; project: string } | null>(null);
  useEffect(() => {
    if (!folder) return;
    let live = true;
    gitProject()
      .then((project) => live && setAsked({ folder, project }))
      .catch(() => live && setAsked({ folder, project: "" }));
    return () => {
      live = false;
    };
  }, [folder]);
  return asked?.folder === folder ? asked.project : null;
}

export function RepositoryRow({ repo }: { repo: Repository }) {
  const line = describeRepository(repo);
  return (
    <span className="flex min-w-0 flex-1 items-center gap-3">
      <span className="min-w-0 flex-1 truncate font-mono text-xs">{repo.name}</span>
      {line.branch ? (
        <span className="max-w-[14rem] min-w-0 shrink truncate text-xs text-fg-muted" title={line.branch}>
          {line.branch}
        </span>
      ) : null}
      {line.sync ? <span className="shrink-0 text-xs text-fg-muted">{line.sync}</span> : null}
      <span
        className={cn(
          "shrink-0 text-xs",
          line.attention ? "text-status-danger" : repo.changed > 0 ? "text-text-primary" : "text-fg-muted",
        )}
      >
        {line.state}
      </span>
    </span>
  );
}

/**
 * Shown in place of a view when the folder Git is in holds repositories and is
 * not one itself — a folder of services. Choosing one points Git at it, and
 * nothing else moves.
 */
export function RepositoryList({
  root,
  repos,
  loading,
  onReload,
  onChoose,
  action,
}: {
  root: string;
  repos: Repository[];
  loading: boolean;
  onReload: () => void;
  onChoose: (repo: Repository) => void;
  /** Where else to go, beside choosing: the folder picker. */
  action: React.ReactNode;
}) {
  const folderName = root.split("/").filter(Boolean).pop() ?? root;
  return (
    <div className="h-full overflow-auto p-4">
      <div className="mx-auto flex max-w-2xl flex-col gap-3">
        <div className="flex items-center gap-2">
          <GitBranch className="size-4 shrink-0 text-fg-muted" aria-hidden />
          <h2 className="min-w-0 flex-1 truncate text-sm font-medium">
            {repos.length} repositories in {folderName}
          </h2>
          <Button variant="ghost" size="sm" onClick={onReload} disabled={loading} aria-label="Read the repositories again">
            <RotateCw className={cn(loading && "animate-spin")} aria-hidden />
          </Button>
          {action}
        </div>
        <p className="text-xs text-fg-muted">
          This folder is not a repository itself. Pick one to work in — Code Graph stays where it is.
        </p>
        <ul className="flex flex-col">
          {repos.map((repo) => (
            <li key={repo.path}>
              <button
                type="button"
                onClick={() => onChoose(repo)}
                className="flex w-full items-center rounded-md px-3 py-2 text-left hover:bg-state-soft"
              >
                <RepositoryRow repo={repo} />
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

/** The bar's repository: which one Git is in, and the others in the same folder. */
export function RepositorySwitcher({
  folder,
  repos,
  onOpen,
  onChoose,
}: {
  folder: string;
  repos: Repository[];
  /** The menu is about to show change counts; read them afresh. */
  onOpen: () => void;
  onChoose: (repo: Repository) => void;
}) {
  const here = repos.find((repo) => repo.path === folder);
  return (
    <DropdownMenu onOpenChange={(open) => open && onOpen()}>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="min-w-0 max-w-full font-mono text-xs" title={folder}>
          <span className="truncate">{here?.name ?? folder}</span>
          <ChevronDown aria-hidden />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-[28rem] max-w-[90vw]">
        <DropdownMenuLabel>{repos.length} repositories</DropdownMenuLabel>
        {repos.map((repo) => (
          <DropdownMenuItem key={repo.path} onSelect={() => onChoose(repo)}>
            <RepositoryRow repo={repo} />
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** A folder with nothing to list: the gate's own empty states handle it. */
export function NoRepositories({ folder, children }: { folder: string; children: React.ReactNode }) {
  return (
    <EmptyState icon={GitBranch} title="Not a git repository" body={`${folder} is not inside a git work tree, and holds none.`}>
      {children}
    </EmptyState>
  );
}
