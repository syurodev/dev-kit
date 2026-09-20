import { useCallback, useEffect, useState } from "react";
import { GitBranch, GitCommitHorizontal, RotateCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription } from "@/components/ui/empty";
import { SIDEBAR_ROW_HEIGHT, SidebarRow, SidebarSection } from "@/components/ui/sidebar-shell";
import { gitLog, gitStatus, type Commit, type GitStatus, type Repository } from "@/bridge/git";
import { cn } from "@/lib/utils";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useModuleFolder } from "@/stores/workFolder";
import { RepositoryRow, useGitProject, useRepositories } from "./GitRepositories";
import { useGitRevision } from "./gitRevision";
import { capRows, changeGroups, splitPath } from "./rail";
import { repositoryRoot } from "./repositories";

/** How many rows a list may put in the rail before it says how many it left out. */
const FILE_ROWS = 40;
const COMMIT_ROWS = 20;

/**
 * The Git rail — docs/dev-kit/git.mdx: repositories, changes, history.
 *
 * It is what is there whenever Git is on screen, including with no tab open,
 * so it reads its own state instead of borrowing a view's: the folder from the
 * work folder, and the repository, status and log from Go. It reads again when
 * the folder changes, when the Changes view has changed the repository, when
 * the window comes back into focus (git is also changed from a terminal), and
 * on the refresh button.
 */
export function GitSidebar() {
  const { folder, current, pinned, pin, follow } = useModuleFolder("git");
  const project = useGitProject(folder);
  const root = repositoryRoot(current, folder);
  const { repos, reload: reloadRepos, loading: reposLoading } = useRepositories(root);
  const revision = useGitRevision((state) => state.revision);
  const [focusTick, setFocusTick] = useState(0);
  const [read, setRead] = useState<{ project: string; status: GitStatus; commits: Commit[] } | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    const onFocus = () => setFocusTick((tick) => tick + 1);
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, []);

  const load = useCallback(() => {
    if (!project) return;
    let live = true;
    Promise.all([gitStatus(), gitLog(COMMIT_ROWS)])
      .then(([status, commits]) => {
        if (!live) return;
        setRead({ project, status, commits });
        setError("");
      })
      .catch((e: Error) => live && setError(e.message));
    return () => {
      live = false;
    };
    // revision and focusTick are not read: they are the reasons to read again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project, revision, focusTick]);
  useEffect(load, [load]);

  function refresh() {
    reloadRepos();
    setFocusTick((tick) => tick + 1);
  }

  async function chooseRepository(repo: Repository) {
    try {
      if (repo.path === current) await follow();
      else await pin(repo.path);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  const refreshButton = (
    <Button variant="ghost" size="icon-xs" onClick={refresh} disabled={reposLoading} aria-label="Refresh">
      <RotateCw className={cn(reposLoading && "animate-spin")} aria-hidden />
    </Button>
  );

  if (!folder) {
    return (
      <div className="flex h-full flex-col select-none">
        <Empty className="px-2 pt-6">
          <EmptyDescription>No folder open. Open a project in Code Graph, or pick a folder for Git.</EmptyDescription>
        </Empty>
      </div>
    );
  }

  // Shown when there is something to choose between: a folder of repositories,
  // whether or not Git is in one of them yet.
  const showRepos = repos !== null && (repos.length > 1 || (!project && repos.length > 0));
  const status = read?.project === project ? read.status : null;
  const commits = read?.project === project ? read.commits : null;
  const groups = status ? changeGroups(status) : [];

  return (
    <div className="flex h-full flex-col select-none">
      <div className="min-h-0 flex-1 overflow-y-auto scrollbar-gutter-stable">
        {showRepos ? (
          <>
            <SidebarSection first label="Repositories" count={repos.length} actions={refreshButton} />
            {repos.map((repo) => (
              <RepositoryItem
                key={repo.path}
                repo={repo}
                active={repo.path === folder}
                onChoose={() => void chooseRepository(repo)}
              />
            ))}
          </>
        ) : null}

        {project && status ? (
          <>
            <SidebarSection
              first={!showRepos}
              label={status.detached ? "Detached HEAD" : status.branch || "No commits"}
              actions={showRepos ? undefined : refreshButton}
            />
            {status.ahead > 0 || status.behind > 0 ? (
              <div className="px-3 pb-1 text-code-11 text-text-tertiary">
                {[status.ahead > 0 ? `↑${status.ahead} ahead` : "", status.behind > 0 ? `↓${status.behind} behind` : ""]
                  .filter(Boolean)
                  .join(" · ")}
              </div>
            ) : null}

            {groups.length === 0 ? (
              <div className="px-3 py-1 text-body-13 text-text-tertiary">The work tree is clean.</div>
            ) : (
              groups.map((group) => {
                const { shown, hidden } = capRows(group.files, FILE_ROWS);
                return (
                  <div key={group.key}>
                    <SidebarSection label={group.label} count={group.files.length} />
                    {shown.map((path) => (
                      <FileItem key={path} path={path} mark={status.files[path]} danger={group.key === "conflicted"} />
                    ))}
                    {hidden > 0 ? (
                      <div className="px-3 py-1 text-code-11 text-text-tertiary">…and {hidden} more</div>
                    ) : null}
                  </div>
                );
              })
            )}

            {commits && commits.length > 0 ? (
              <>
                <SidebarSection label="History" count={commits.length} />
                {commits.map((commit) => (
                  <SidebarRow
                    key={commit.sha}
                    tier="card"
                    icon={<GitCommitHorizontal className="size-(--icon-14) shrink-0 text-text-tertiary" aria-hidden />}
                    label={
                      <span className="flex min-w-0 flex-col">
                        <span className="truncate">{commit.subject}</span>
                        <span className="truncate text-code-11 text-text-tertiary">
                          {commit.short} · {commit.author} · {commit.date.slice(0, 10)}
                        </span>
                      </span>
                    }
                    onClick={() => openCommit(commit)}
                  />
                ))}
              </>
            ) : null}
          </>
        ) : null}

        {project === "" && repos !== null && repos.length === 0 ? (
          <Empty className="px-2 pt-6">
            <EmptyDescription>This folder is not a git repository, and holds none.</EmptyDescription>
          </Empty>
        ) : null}

        {error ? <div className="px-3 py-2 text-code-11 text-status-danger">{error}</div> : null}
      </div>

      <div className="flex h-10 shrink-0 items-center gap-2 px-3 text-code-11 text-text-tertiary">
        <GitBranch className="size-(--icon-12) shrink-0" aria-hidden />
        <span className="min-w-0 flex-1 truncate" title={folder}>
          {folder}
        </span>
        {pinned ? (
          <button type="button" className="shrink-0 hover:text-text-primary" onClick={() => void follow()}>
            Follow open project
          </button>
        ) : null}
      </div>
    </div>
  );
}

function RepositoryItem({ repo, active, onChoose }: { repo: Repository; active: boolean; onChoose: () => void }) {
  return (
    <button
      type="button"
      onClick={onChoose}
      data-accent={active ? "on" : undefined}
      className={cn(
        "row-accent flex w-full items-center rounded-md px-3 text-left",
        SIDEBAR_ROW_HEIGHT.list,
        active
          ? "bg-surface-raised text-text-primary"
          : "text-text-secondary hover:bg-state-soft hover:text-text-primary",
      )}
    >
      <RepositoryRow repo={repo} />
    </button>
  );
}

function FileItem({ path, mark, danger }: { path: string; mark: string | undefined; danger: boolean }) {
  const { name, dir } = splitPath(path);
  return (
    <SidebarRow
      tier="tree"
      onClick={openChanges}
      label={
        <span className="flex min-w-0 items-baseline gap-2">
          <span className="truncate">{name}</span>
          {dir ? <span className="truncate text-code-11 text-text-tertiary">{dir}</span> : null}
        </span>
      }
      trailing={
        mark ? (
          <span className={cn("shrink-0 text-code-11", danger ? "text-status-danger" : "text-text-tertiary")}>{mark}</span>
        ) : null
      }
    />
  );
}

// The rail opens what it lists in the module's own views: the Changes view is
// where a file is staged, and a commit's diff has its own view that takes the
// revision.
function openChanges() {
  useWorkbenchTabsStore.getState().openTab({ moduleId: "git", viewId: "git.changes", title: "Changes" });
}

function openCommit(commit: Commit) {
  useWorkbenchTabsStore.getState().openTab({
    moduleId: "git",
    viewId: "git.diff",
    title: commit.short,
    params: { rev: commit.sha },
  });
}
