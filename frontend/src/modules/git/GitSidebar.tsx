import { FeedbackBanner, PendingStatus } from "@/components/ui/feedback";
import { SidebarHeader, SidebarInset, SidebarSearch } from "@/components/ui/sidebar-shell";
import { gitUI, useGitBusy, useGitOperation } from "./gitUI";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useWorkFolderStore, effectiveFolder } from "@/stores/workFolder";
import { pinWorkFolder } from "@/bridge/workfolder";
import { errorMessage } from "@/bridge/errors";
import { notify } from "@/lib/notify";
import { useEffect, useState } from "react";
import { ChevronDown, FolderGit2, GitBranch, GitCommitHorizontal, RotateCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import { SIDEBAR_ROW_HEIGHT, SidebarRow, SidebarSection } from "@/components/ui/sidebar-shell";
import { gitStage, gitUnstage, type Commit, type Repository } from "@/bridge/git";
import { cn } from "@/lib/utils";
import { useModuleFolder } from "@/stores/workFolder";
import { RepositoryRow, useGitProject, useRepositories } from "./GitRepositories";
import { HeadingAction, StatusMark } from "./GitParts";
import { refreshGitRepo, useGitRepo, useLatestGitRepo } from "./gitRepo";
import { bumpGitRevision, useGitRevision } from "./gitRevision";
import { focusGitPath, useGitFocus } from "./gitFocus";
import { openChanges, openCommitDiff, openHistory, openFileDiff } from "./documents";
import { branchMeta, capRows, changeGroups, markKind, splitPath, type ChangeGroup } from "./rail";
import { describeRepository, folderStatus, pathTail, repositoryRoot } from "./repositories";

/** How many rows a list may put in the rail before it says how many it left out. */
const FILE_ROWS = 40;

/** What a group's heading offers to do to every file under it. Conflicts offer nothing: staging one commits the markers. */
const GROUP_ACTION: Record<ChangeGroup["key"], { label: string; run: (paths: string[]) => Promise<void> } | null> = {
  conflicted: null,
  staged: { label: "Unstage all", run: gitUnstage },
  unstaged: { label: "Stage all", run: gitStage },
  untracked: { label: "Stage all", run: gitStage },
};

/**
 * The Git rail — docs/dev-kit/git.mdx: repositories, changes, history.
 *
 * It is what is there whenever Git is on screen, including with no tab open,
 * so it reads its own state instead of borrowing a view's: the folder from the
 * work folder, and the repository, status and log from Go. It reads again when
 * the folder changes, when anything has changed the repository (the revision),
 * when the window comes back into focus (git is also changed from a terminal),
 * and on the refresh button.
 */
export function GitSidebar() {
  const { folder, current, pinned, pin, follow } = useModuleFolder("git");
  const project = useGitProject(folder);
  const root = repositoryRoot(current, folder);
  const { repos, reload: reloadRepos, loading: reposLoading } = useRepositories(root);
  const revision = useGitRevision((state) => state.revision);
  const focused = useGitFocus((state) => state.path);
  // What Git last read in this folder — shared with the Changes view, so a
  // change read once serves both. It stays while the next read is on its way.
  const own = useGitRepo(folder);
  const latest = useLatestGitRepo();
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const busy = useGitBusy();
  const operation = useGitOperation(folder);
  const activeView = useWorkbenchTabsStore((state) => {
    const tabs = state.modules.git;
    return tabs?.openTabs.find((tab) => tab.id === tabs.activeTabId)?.viewId;
  });
  const reportOperation = activeView !== "git.changes" && activeView !== "git.remotes";
  const needle = search.trim().toLowerCase();
  useEffect(
    () =>
      gitUI.useStore.subscribe((next, previous) => {
        for (const [repo, op] of Object.entries(next.operations)) {
          if (!previous.operations[repo]?.label || op.label) continue;
          if (
            useWorkbenchTabsStore.getState().activeModuleId === "git" &&
            effectiveFolder(useWorkFolderStore.getState(), "git") === repo
          )
            continue;
          notify({
            id: `git:${repo}:operation`,
            title: op.error ? "Git action failed" : op.notice || "Git action completed",
            tone: op.error ? "error" : "success",
            action: {
              label: "Open Git",
              onClick: () => {
                if (Object.values(gitUI.useStore.getState().operations).some((value) => value.label)) return;
                if (effectiveFolder(useWorkFolderStore.getState(), "git") === repo) {
                  openChanges();
                  return;
                }
                void pinWorkFolder("git", repo)
                  .then(() => openChanges())
                  .catch((error) =>
                    notify({
                      id: `git:${repo}:open`,
                      tone: "error",
                      title: `Could not open repository: ${errorMessage(error)}`,
                    }),
                  );
              },
            },
          });
        }
      }),
    [],
  );

  // Read again when the folder or the repository changes, and when anything has
  // changed it (the revision). The view beside this asks with the same stamp, and
  // the two share one read.
  useEffect(() => {
    if (project) void refreshGitRepo(folder, String(revision));
  }, [folder, project, revision]);

  // git is also changed from a terminal, and the window coming back into focus
  // is when that shows. Nothing is rendered unless the read finds a difference.
  useEffect(() => {
    if (!project) return;
    const onFocus = () => void refreshGitRepo(folder, `focus:${Date.now()}`);
    window.addEventListener("focus", onFocus);
    return () => window.removeEventListener("focus", onFocus);
  }, [folder, project]);

  function refresh() {
    reloadRepos();
    if (project) void refreshGitRepo(folder, `refresh:${Date.now()}`);
  }

  async function chooseRepository(repo: Repository) {
    if (busy) return;
    setError("");
    try {
      if (repo.path === current) await follow();
      else await pin(repo.path);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function actOnGroup(group: ChangeGroup) {
    const action = GROUP_ACTION[group.key];
    if (!action) return;
    await gitUI.run(folder, action.label, async () => {
      await action.run(group.files);
      bumpGitRevision();
    });
  }

  if (!folder) {
    return (
      <div className="flex h-full flex-col select-none">
        <EmptyState size="rail" title="No folder open" body="Open a project in Code Graph, or pick a folder for Git." />
      </div>
    );
  }

  // Shown when there is something to choose between: a folder of repositories,
  // whether or not Git is in one of them yet. "Not in one" is Go's answer, ""; a
  // null is Go not having answered, and a folder that turns out to be one
  // repository must not flash a list of it in the meantime.
  const showRepos = repos !== null && (repos.length > 1 || (project === "" && repos.length > 0));
  // What was read is kept while the next read is on its way, so choosing a
  // repository is a list that changes and not one that vanishes and comes back.
  // Until this repository's own first read lands the last one read is shown,
  // dimmed and inert; a folder that is no repository has nothing to show.
  const snapshot = project === "" ? null : (own?.snapshot ?? latest?.snapshot ?? null);
  const carried = project !== "" && !own?.snapshot && snapshot !== null;
  const status = snapshot?.status ?? null;
  const commits = snapshot?.commits ?? null;
  const groups = status
    ? changeGroups(status)
        .map((group) => ({
          ...group,
          files: group.files.filter((path) => !needle || path.toLowerCase().includes(needle)),
        }))
        .filter((group) => group.files.length)
    : [];
  // The header does not wait for the read: the repository list already knows
  // each repository's branch and how far it is from its upstream.
  const listed = repos?.find((repo) => repo.path === folder);
  const meta =
    project === ""
      ? repos
        ? `${repos.length} repositories`
        : ""
      : own?.snapshot
        ? branchMeta(own.snapshot.status)
        : listed && !listed.error
          ? branchMeta(listed)
          : "";
  const standing = folderStatus({ folder, current, pinned, project, repos });

  return (
    <div className="flex h-full flex-col gap-3 select-none">
      <SidebarHeader>
        <SidebarSearch
          value={search}
          onChange={setSearch}
          onClear={() => setSearch("")}
          placeholder="Search changes and commits…"
        />
      </SidebarHeader>
      <SidebarInset className="flex flex-col gap-1.5">
        <RepositoryHeader
          name={pathTail(folder, 1)}
          meta={meta}
          folder={folder}
          repos={showRepos ? repos : null}
          refreshing={reposLoading || busy}
          onOpenMenu={reloadRepos}
          onChoose={(repo) => void chooseRepository(repo)}
          onRefresh={refresh}
        />
        <div className="flex items-center gap-1.5 pl-1" role="status">
          <span
            aria-hidden
            className={cn(
              "size-1.5 shrink-0 rounded-full",
              standing.tone === "success" && "bg-status-success",
              standing.tone === "info" && "bg-status-info",
              standing.tone === "warning" && "bg-status-warning",
            )}
          />
          <span className="min-w-0 truncate text-caption-11 text-text-tertiary" title={standing.text}>
            {standing.text}
          </span>
        </div>
      </SidebarInset>

      <div className="min-h-0 flex-1 overflow-y-auto scrollbar-gutter-stable">
        {showRepos ? (
          <>
            <SidebarSection
              first
              label="Repositories"
              count={repos.length}
              actions={<HeadingAction onClick={refresh}>Refresh</HeadingAction>}
            />
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

        {project !== "" && status ? (
          <div
            aria-busy={carried}
            className={cn("transition-opacity duration-(--motion-fast)", carried && "pointer-events-none opacity-50")}
          >
            {groups.length === 0 ? (
              <div className="px-3 py-1 text-body-13 text-text-tertiary">
                {needle ? "No matching changes." : "The work tree is clean."}
              </div>
            ) : (
              groups.map((group, index) => {
                const { shown, hidden } = capRows(group.files, FILE_ROWS);
                const action = GROUP_ACTION[group.key];
                return (
                  <div key={group.key}>
                    <SidebarSection
                      first={index === 0 && !showRepos}
                      label={group.label}
                      count={group.files.length}
                      actions={
                        action ? (
                          <HeadingAction disabled={busy} onClick={() => void actOnGroup(group)}>
                            {action.label}
                          </HeadingAction>
                        ) : null
                      }
                    />
                    {shown.map((path) => (
                      <FileItem
                        key={path}
                        path={path}
                        kind={markKind(group.key, status.files[path])}
                        selected={group.key === "unstaged" && focused === path}
                        onOpen={() => openChange(path, group.key)}
                      />
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
                <SidebarSection
                  label="History"
                  count={commits.length}
                  actions={<HeadingAction onClick={openHistory}>View all</HeadingAction>}
                />
                {commits
                  .filter(
                    (commit) =>
                      !needle || `${commit.subject} ${commit.author} ${commit.sha}`.toLowerCase().includes(needle),
                  )
                  .map((commit) => (
                    <SidebarRow
                      key={commit.sha}
                      tier="card"
                      icon={
                        <GitCommitHorizontal className="size-(--icon-14) shrink-0 text-text-tertiary" aria-hidden />
                      }
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
          </div>
        ) : null}

        {project === "" && repos !== null && repos.length === 0 ? (
          <EmptyState size="rail" body="This folder is not a git repository, and holds none." />
        ) : null}

        {busy && reportOperation && (
          <SidebarInset>
            <PendingStatus>{operation.label || "Updating repository…"}</PendingStatus>
          </SidebarInset>
        )}
        {(error || (reportOperation && (operation.error || own?.error))) && (
          <SidebarInset>
            <FeedbackBanner tone="error" title="Git action failed">
              {error || operation.error || own?.error}
            </FeedbackBanner>
          </SidebarInset>
        )}
      </div>

      <div className="flex h-10 shrink-0 items-center gap-2 px-3 text-code-11 text-text-tertiary">
        <FolderGit2 className="size-(--icon-12) shrink-0" aria-hidden />
        <span className="min-w-0 flex-1 truncate" title={folder}>
          {folder}
        </span>
        {pinned ? (
          <button
            type="button"
            className="shrink-0 text-caption-11 hover:text-text-primary"
            disabled={busy}
            onClick={() => void follow().catch((e: Error) => setError(e.message))}
          >
            Follow open project
          </button>
        ) : null}
      </div>
    </div>
  );
}

/**
 * Figma `Git / Sidebar / Header`: the repository Git is in, over its branch,
 * with the other repositories of the folder one click away and a button to read
 * everything again. Without neighbours there is nothing to switch to, so the
 * card is a label and not a menu.
 */
function RepositoryHeader({
  name,
  meta,
  folder,
  repos,
  refreshing,
  onOpenMenu,
  onChoose,
  onRefresh,
}: {
  name: string;
  meta: string;
  folder: string;
  repos: Repository[] | null;
  refreshing: boolean;
  /** The menu is about to show change counts; read them afresh. */
  onOpenMenu: () => void;
  onChoose: (repo: Repository) => void;
  onRefresh: () => void;
}) {
  const label = (
    <>
      <FolderGit2 className="size-4 shrink-0 text-text-secondary" aria-hidden />
      <span className="flex min-w-0 flex-1 flex-col text-left">
        <span className="truncate text-title-13 text-text-primary">{name}</span>
        {/* Always there, so the card is the same height before the branch is known. */}
        <span className="min-h-4 truncate text-code-11 text-text-tertiary">{meta}</span>
      </span>
    </>
  );
  return (
    <div className="flex items-center gap-2 rounded-lg border border-border-subtle bg-surface-raised py-2 pr-2 pl-2.5">
      {repos ? (
        <DropdownMenu onOpenChange={(open) => open && onOpenMenu()}>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label={`Repository: ${name}. Switch repository`}
              className="flex min-w-0 flex-1 items-center gap-2 rounded-md hover:text-text-primary"
            >
              {label}
              <ChevronDown className="size-(--icon-14) shrink-0 text-text-tertiary" aria-hidden />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-112 max-w-screen-90">
            <DropdownMenuLabel>{repos.length} repositories</DropdownMenuLabel>
            {repos.map((repo) => (
              <DropdownMenuItem key={repo.path} disabled={repo.path === folder} onSelect={() => onChoose(repo)}>
                <RepositoryRow repo={repo} />
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      ) : (
        <div className="flex min-w-0 flex-1 items-center gap-2">{label}</div>
      )}
      <Button variant="ghost" size="icon-xs" onClick={onRefresh} disabled={refreshing} aria-label="Refresh">
        <RotateCw className={cn(refreshing && "animate-spin")} aria-hidden />
      </Button>
    </div>
  );
}

/** Figma `Git / Sidebar / Repo Row`: a repository's name over its branch, and how it stands beside them. */
function RepositoryItem({ repo, active, onChoose }: { repo: Repository; active: boolean; onChoose: () => void }) {
  const line = describeRepository(repo);
  return (
    <button
      type="button"
      onClick={onChoose}
      aria-current={active ? "true" : undefined}
      className={cn(
        "flex w-full items-center gap-2 rounded-md px-3 text-left transition-colors duration-(--motion-fast) ease-(--ease-out-strong)",
        SIDEBAR_ROW_HEIGHT.card,
        active
          ? "bg-surface-raised text-text-primary"
          : "text-text-secondary hover:bg-state-soft hover:text-text-primary",
      )}
    >
      <span className="flex min-w-0 flex-1 flex-col gap-px">
        <span className="truncate text-body-13 text-text-primary">{repo.name}</span>
        {line.branch ? (
          <span className="flex min-w-0 items-center gap-1 text-code-11 text-text-tertiary" title={line.branch}>
            <GitBranch className="size-(--icon-12) shrink-0" aria-hidden />
            <span className="truncate">{line.branch}</span>
          </span>
        ) : null}
      </span>
      <span className="flex shrink-0 flex-col items-end gap-px text-code-11 text-text-tertiary">
        <span className={cn(line.attention && "text-status-danger")}>{line.state}</span>
        {line.sync ? <span>{line.sync}</span> : null}
      </span>
    </button>
  );
}

/** Figma `Git / Sidebar / File Row`: what happened to a file, its name, and the folder it is in. */
function FileItem({
  path,
  kind,
  selected,
  onOpen,
}: {
  path: string;
  kind: ReturnType<typeof markKind>;
  selected: boolean;
  onOpen: () => void;
}) {
  const { name, dir } = splitPath(path);
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-current={selected ? "true" : undefined}
      title={path}
      className={cn(
        "flex h-7 w-full items-center gap-2 rounded-md px-3 text-left transition-colors duration-(--motion-fast) ease-(--ease-out-strong)",
        selected ? "bg-surface-raised" : "hover:bg-state-soft",
      )}
    >
      <StatusMark kind={kind} />
      <span className="min-w-0 truncate text-body-13 text-text-primary">{name}</span>
      {dir ? <span className="min-w-0 flex-1 truncate text-code-11 text-text-tertiary">{dir}</span> : null}
    </button>
  );
}

// The rail opens what it lists in the module's own views: the Changes view is
// where a file is staged — and a changed file opens there already unfolded — and
// a commit's diff has its own view that takes the revision.
function openChange(path: string, group: ChangeGroup["key"]) {
  // Only a changed file has hunks to unfold; a staged or untracked one has
  // none to pick, so it must not leave the previous choice standing.
  focusGitPath(group === "unstaged" ? path : "");
  if (group === "staged") openFileDiff(path, true);
  else openChanges();
}

function openCommit(commit: Commit) {
  openCommitDiff(commit.sha, commit.short);
}
