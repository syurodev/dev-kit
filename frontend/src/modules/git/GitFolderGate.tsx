import { useGitBusy } from "./gitUI";
import { useState, type ComponentType, type ReactNode, useRef } from "react";
import { ArrowLeftRight, FolderGit2, GitBranch, Link2 } from "lucide-react";
import { EmptyState } from "@/components/ui/empty-state";
import type { Repository } from "@/bridge/git";
import { useModuleFolder } from "@/stores/workFolder";
import { IconAction, StatePill } from "./GitParts";
import {
  NoRepositories,
  RepositoryList,
  RepositoryMenu,
  useGitProject,
  useWorkspaceRepositories,
} from "./GitRepositories";
import { repositoryRoot } from "./repositories";

/**
 * Wraps a Git view with the folder it reads. Git works in the app's work
 * folder — whatever Code Graph has open — until it is pointed at another.
 *
 * The folder bar says which repository the view is looking at and how to change
 * that. Views that draw their own — Changes puts it above the branch bar — or
 * none, like a commit, say so with `bar: false`; a folder that is not itself a
 * repository always gets it, because there the bar is the way in.
 *
 * The view is keyed by folder: a folder change remounts it, so each view loads
 * the new repository from scratch instead of each one having to notice.
 */
export type GitViewProps = { params?: Record<string, unknown> };

export function withGitFolder(
  View: ComponentType<GitViewProps>,
  options: { bar?: boolean } = {},
): ComponentType<GitViewProps> {
  const bar = options.bar ?? true;
  return function GitFolderView({ params }: GitViewProps) {
    return <GitFolderGate View={View} bar={bar} params={params} />;
  };
}

/**
 * Pointing Git at another repository: the ones under the open project and under
 * each folder of the current workspace, in one list. The folder picker this
 * used to open (agentHubChooseDirectory) is gone — a folder Git could work in
 * is a service somewhere in a workspace now, and adding one not yet registered
 * is the title bar workspace switcher's job.
 */
function RepositorySwitch({
  folder,
  current,
  onPick,
  disabled,
}: {
  folder: string;
  current: string;
  onPick: (repo: Repository) => void;
  disabled?: boolean;
}) {
  const { groups, loading, reload } = useWorkspaceRepositories(repositoryRoot(current, folder));
  return (
    <RepositoryMenu groups={groups} folder={folder} loading={loading} onOpen={reload} onChoose={onPick} align="end">
      <IconAction label="Switch repository" icon={<ArrowLeftRight />} variant="outline" disabled={disabled} />
    </RepositoryMenu>
  );
}

/**
 * Figma `Git / Folder Bar`: the folder Git is in, whether it is following Code
 * Graph or pinned, and the way out of either.
 */
export function GitFolderBar() {
  const busy = useGitBusy();
  const { folder, current, pinned, follow, pin } = useModuleFolder("git");
  const [pinError, setPinError] = useState("");
  const [followError, setFollowError] = useState("");
  const error = pinError || followError;
  async function choose(repo: Repository) {
    setPinError("");
    try {
      if (repo.path === current) await follow();
      else await pin(repo.path);
    } catch (e) {
      setPinError((e as Error).message);
    }
  }
  return (
    <div className="flex flex-wrap items-center gap-2.5">
      <FolderGit2 className="size-(--icon-14) shrink-0 text-text-secondary" aria-hidden />
      <span className="min-w-0 truncate text-code-12 text-text-secondary" title={folder || undefined}>
        {folder || "No folder"}
      </span>
      <StatePill dot={pinned ? "info" : "success"}>
        {pinned ? "Pinned to this folder" : "Following Code Graph"}
      </StatePill>
      {error ? (
        <span className="min-w-0 truncate text-caption-11 text-status-danger" role="alert">
          {error}
        </span>
      ) : null}
      <span className="min-w-0 flex-1" />
      {pinned ? (
        <IconAction
          label={current ? "Follow open project" : "Follow Code Graph"}
          icon={<Link2 />}
          disabled={busy}
          onClick={() => {
            setFollowError("");
            follow().catch((e: Error) => setFollowError(e.message));
          }}
        />
      ) : null}
      <RepositorySwitch folder={folder} current={current} onPick={(repo) => void choose(repo)} disabled={busy} />
    </div>
  );
}

function GitFolderGate({ View, bar, params }: { View: ComponentType<GitViewProps>; bar: boolean } & GitViewProps) {
  const busy = useGitBusy();
  const { loaded, folder, current, pin, follow } = useModuleFolder("git");
  const [error, setError] = useState("");
  // The repositories of the folder the app has open and of the current
  // workspace, so Git can offer its neighbours while it is pinned to one.
  const { repos, loading, reload } = useWorkspaceRepositories(repositoryRoot(current, folder));
  // What Go says Git is reading in this folder; null until it has said.
  const project = useGitProject(folder);

  // Choosing one of the folder's repositories points Git at it. The folder the
  // app has open being the repository itself is following, not a pin.
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

  const showBar = loaded && !!folder && (bar || project === "");
  // With the bar up, its own switcher is the way to another repository.
  const openButton = showBar ? null : (
    <RepositorySwitch folder={folder} current={current} onPick={(repo) => void chooseRepository(repo)} disabled={busy} />
  );

  const lastBody = useRef<ReactNode>(null);
  let body: ReactNode;
  if (!loaded) {
    body = null;
  } else if (!folder) {
    body = (
      <EmptyState
        icon={GitBranch}
        title="No folder open"
        body="Git works in the folder Code Graph has open. Open a project there, or pick a workspace service for Git alone."
      >
        {openButton}
      </EmptyState>
    );
  } else if (project === null) {
    // Go has not said yet what is in this folder. What was on screen stays: the
    // answer is a round trip away, and a blank pane for that long is the flicker
    // of choosing a repository.
    body = lastBody.current;
  } else if (!project) {
    // A folder of repositories is not one itself, but is not nothing either:
    // list what is in it. Until it has been read, show nothing rather than
    // flash the empty state.
    body =
      repos === null ? null : repos.length > 0 ? (
        <RepositoryList
          title={`${repos.length} ${repos.length === 1 ? "repository" : "repositories"}`}
          repos={repos}
          loading={loading}
          onReload={reload}
          onChoose={chooseRepository}
        />
      ) : (
        <NoRepositories folder={folder}>{openButton}</NoRepositories>
      );
  } else {
    body = <View key={folder} params={params} />;
  }
  if (project !== null) lastBody.current = body;

  return (
    <div className="flex h-full min-h-0 flex-col">
      {showBar ? (
        <div className="shrink-0 px-4 pt-4">
          <GitFolderBar />
        </div>
      ) : null}
      {error ? (
        <p className="shrink-0 px-4 pt-3 text-caption-11 text-status-danger" role="alert">
          {error}
        </p>
      ) : null}
      <div className="min-h-0 flex-1">{body}</div>
    </div>
  );
}
