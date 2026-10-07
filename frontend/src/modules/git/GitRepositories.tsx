import { Fragment, useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { Check, ChevronRight, FolderGit2, GitBranch, RotateCw } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { gitProject, type Repository } from "@/bridge/git";
import { cn } from "@/lib/utils";
import { useCurrentWorkspaceServices } from "@/stores/workspaceSwitcher";
import { IconAction, StatePill } from "./GitParts";
import { gitRepo, refreshRepositories } from "./gitRepo";
import {
  collapseTrivialGroups,
  describeRepository,
  groupRepositories,
  pathTail,
  type RepositoryGroup,
} from "./repositories";

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
 * Every repository Git could be pointed at: the ones under the project Code
 * Graph has open, and the ones under each folder of the current workspace —
 * the services of a workspace are usually separate checkouts, and listing only
 * what is under the open project left the rest unreachable from Git.
 *
 * `repos` is null until every place has been read once, so a list that is
 * about to grow is not shown as if it were complete. Scans are shared with
 * `useRepositories` (gitRepo), so a root both ask about is walked once.
 */
export function useWorkspaceRepositories(root: string) {
  const { services } = useCurrentWorkspaceServices();
  const byRoot = gitRepo.useStore((state) => state.reposByRoot);
  const scanningRoots = gitRepo.useStore((state) => state.scanning);

  // The service folders in the workspace's own order, then the open project
  // when it is not one of them. The project goes first when it is: it is what
  // the person is working in.
  const sources = useMemo(() => {
    const folders = services.map((service) => ({ key: service.path, label: service.name, root: service.path }));
    if (!root || folders.some((folder) => folder.root === root)) return folders;
    return [{ key: root, label: pathTail(root, 1), root }, ...folders];
  }, [root, services]);
  const roots = useMemo(() => sources.map((source) => source.root), [sources]);

  useEffect(() => {
    for (const each of roots) void refreshRepositories(each);
  }, [roots]);

  const reload = useCallback(() => {
    for (const each of roots) void refreshRepositories(each, true);
  }, [roots]);

  const loaded = roots.length > 0 && roots.every((each) => byRoot[each] !== undefined);
  const groups: RepositoryGroup[] = useMemo(
    () =>
      collapseTrivialGroups(
        groupRepositories(sources.map((source) => ({ ...source, repos: byRoot[source.root]?.repos }))),
      ),
    [sources, byRoot],
  );
  const repos = useMemo(() => (loaded ? groups.flatMap((group) => group.repos) : null), [loaded, groups]);
  const loading = roots.some((each) => scanningRoots[each] === true);
  return { groups, repos, loading, reload };
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
  title,
  repos,
  loading,
  onReload,
  onChoose,
}: {
  title: string;
  repos: Repository[];
  loading: boolean;
  onReload: () => void;
  onChoose: (repo: Repository) => void;
}) {
  return (
    <div className="scrollbar-gutter-stable flex h-full min-h-0 flex-col gap-3 overflow-auto px-4 pt-4 pb-4">
      <div className="flex items-center gap-2.5">
        <FolderGit2 className="size-4 shrink-0 text-text-secondary" aria-hidden />
        <h2 className="min-w-0 flex-1 truncate text-title-14 text-text-primary">
          {title}
        </h2>
        <IconAction
          label="Read the repositories again"
          onClick={onReload}
          disabled={loading}
          icon={<RotateCw className={cn(loading && "animate-spin")} aria-hidden />}
        />
      </div>
      <p className="text-body-13 text-text-secondary">
        This folder is not a repository itself. Pick one to work in — Code Graph stays where it is, so nothing is
        re-indexed. Repositories of the current workspace are listed too.
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

/**
 * The way to another repository: every one Git can reach, under the workspace
 * folder it was found in. `children` is the trigger — a card in the rail, an
 * icon on the folder bar — so both open the same list. The repository Git is
 * in is ticked rather than disabled: it is the one true thing in the list.
 */
export function RepositoryMenu({
  groups,
  folder,
  loading,
  onOpen,
  onChoose,
  align = "start",
  children,
}: {
  groups: RepositoryGroup[];
  folder: string;
  loading?: boolean;
  /** The menu is about to show change counts; read them afresh. */
  onOpen?: () => void;
  onChoose: (repo: Repository) => void;
  align?: "start" | "end";
  children: ReactNode;
}) {
  const total = groups.reduce((sum, group) => sum + group.repos.length, 0);
  return (
    <DropdownMenu onOpenChange={(open) => open && onOpen?.()}>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent align={align} className="max-h-96 w-112 max-w-screen-90">
        {total === 0 ? (
          <DropdownMenuItem disabled>
            {loading ? "Looking for repositories…" : "No repositories in this workspace"}
          </DropdownMenuItem>
        ) : (
          groups.map((group, index) => (
            <Fragment key={group.key}>
              {index > 0 ? <DropdownMenuSeparator /> : null}
              <DropdownMenuLabel>
                {groups.length > 1 ? group.label : `${total} repositories`}
              </DropdownMenuLabel>
              {group.repos.map((repo) => (
                <DropdownMenuItem
                  key={repo.path}
                  icon={repo.path === folder ? <Check /> : undefined}
                  title={repo.path}
                  onSelect={() => {
                    if (repo.path !== folder) onChoose(repo);
                  }}
                >
                  <RepositoryRow repo={repo} />
                </DropdownMenuItem>
              ))}
            </Fragment>
          ))
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
