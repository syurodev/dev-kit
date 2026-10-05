import { useCallback, useEffect, useState } from "react";
import { ChevronRight, FolderGit2, GitBranch, RotateCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { gitProject, type Repository } from "@/bridge/git";
import { cn } from "@/lib/utils";
import { StatePill } from "./GitParts";
import { gitRepo, refreshRepositories } from "./gitRepo";
import { describeRepository } from "./repositories";

/**
 * The repositories at or under `root`, and a way to read them again.
 *
 * Read on demand rather than watched: a change count is stale the moment it is
 * drawn, and the places it is shown — a list, a menu just opened — are exactly
 * where it is cheap to ask again. The scan is shared (gitRepo): the rail and the
 * folder list ask for the same root and it is walked once. `repos` is null until
 * this root has been read, and after that the last read stays while the next is
 * on its way, so a list for the previous folder is never shown under the new one
 * and a list is never blanked to be redrawn.
 */
export function useRepositories(root: string) {
  const entry = gitRepo.useStore((state) => state.reposByRoot[root]);
  const loading = gitRepo.useStore((state) => state.scanning[root] === true);

  useEffect(() => {
    void refreshRepositories(root);
  }, [root]);

  const reload = useCallback(() => {
    void refreshRepositories(root, true);
  }, [root]);

  return { repos: entry?.repos ?? null, error: entry?.error ?? "", loading, reload };
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
      <span className="min-w-0 flex-1 truncate font-mono text-code-12">{repo.name}</span>
      {line.branch ? (
        <span className="max-w-56 min-w-0 shrink truncate text-code-12 text-text-tertiary" title={line.branch}>
          {line.branch}
        </span>
      ) : null}
      {line.sync ? <span className="shrink-0 text-code-12 text-text-tertiary">{line.sync}</span> : null}
      <span
        className={cn(
          "shrink-0 text-code-12",
          line.attention ? "text-status-danger" : repo.changed > 0 ? "text-text-primary" : "text-text-tertiary",
        )}
      >
        {line.state}
      </span>
    </span>
  );
}

/**
 * Figma `Git / Repositories Content`: shown in place of a view when the folder
 * Git is in holds repositories and is not one itself — a folder of services.
 * Choosing one points Git at it, and nothing else moves.
 */
export function RepositoryList({
  root,
  repos,
  loading,
  onReload,
  onChoose,
}: {
  root: string;
  repos: Repository[];
  loading: boolean;
  onReload: () => void;
  onChoose: (repo: Repository) => void;
}) {
  const folderName = root.split("/").filter(Boolean).pop() ?? root;
  return (
    <div className="scrollbar-gutter-stable flex h-full min-h-0 flex-col gap-3 overflow-auto px-4 pt-4 pb-4">
      <div className="flex items-center gap-2.5">
        <FolderGit2 className="size-4 shrink-0 text-text-secondary" aria-hidden />
        <h2 className="min-w-0 flex-1 truncate text-title-14 text-text-primary">
          {repos.length} repositories in {folderName}
        </h2>
        <Button variant="ghost" onClick={onReload} disabled={loading} aria-label="Read the repositories again">
          <RotateCw className={cn("size-(--icon-14)", loading && "animate-spin")} aria-hidden />
          Refresh
        </Button>
      </div>
      <p className="text-body-13 text-text-secondary">
        This folder is not a repository itself. Pick one to work in — Code Graph stays where it is, so nothing is
        re-indexed.
      </p>
      <ul className="flex flex-col rounded-lg border border-border-subtle bg-surface-well p-1">
        {repos.map((repo) => {
          const line = describeRepository(repo);
          return (
            <li key={repo.path}>
              <button
                type="button"
                onClick={() => onChoose(repo)}
                className="flex h-9 w-full items-center gap-2.5 rounded-md px-3 text-left transition-colors duration-(--motion-fast) ease-(--ease-out-strong) hover:bg-state-soft"
              >
                <FolderGit2 className="size-(--icon-14) shrink-0 text-text-secondary" aria-hidden />
                <span className="min-w-0 truncate text-body-13 text-text-primary">{repo.name}</span>
                {line.branch ? (
                  <StatePill
                    className="max-w-96 min-w-0 font-mono"
                    title={line.branch}
                    icon={<GitBranch className="size-(--icon-12) shrink-0" aria-hidden />}
                  >
                    <span className="truncate">{line.branch}</span>
                  </StatePill>
                ) : null}
                <span className="min-w-0 flex-1" />
                {line.sync ? <span className="shrink-0 text-code-11 text-text-tertiary">{line.sync}</span> : null}
                <span
                  className={cn(
                    "shrink-0 text-code-11",
                    line.attention ? "text-status-danger" : repo.changed > 0 ? "text-text-secondary" : "text-text-tertiary",
                  )}
                >
                  {line.state}
                </span>
                <ChevronRight className="size-(--icon-12) shrink-0 text-text-tertiary" aria-hidden />
              </button>
            </li>
          );
        })}
      </ul>
    </div>
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
