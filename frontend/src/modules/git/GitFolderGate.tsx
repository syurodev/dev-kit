import { useRef, useState, type ComponentType, type ReactNode } from "react";
import { FolderGit2, GitBranch } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { agentHubChooseDirectory } from "@/bridge/agenthub";
import type { Repository } from "@/bridge/git";
import { useModuleFolder } from "@/stores/workFolder";
import { StatePill } from "./GitParts";
import { NoRepositories, RepositoryList, useGitProject, useRepositories } from "./GitRepositories";
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
export function withGitFolder(View: ComponentType, options: { bar?: boolean } = {}): ComponentType {
  const bar = options.bar ?? true;
  return function GitFolderView() {
    return <GitFolderGate View={View} bar={bar} />;
  };
}

/** Choosing a folder for Git alone. A cancelled dialog is "", and Git keeps the folder it had. */
function useFolderPicker() {
  const { folder, pin } = useModuleFolder("git");
  const [error, setError] = useState("");
  async function openFolder() {
    setError("");
    try {
      const picked = await agentHubChooseDirectory(folder);
      if (picked) await pin(picked);
    } catch (e) {
      setError((e as Error).message);
    }
  }
  return { openFolder, error };
}

/**
 * Figma `Git / Folder Bar`: the folder Git is in, whether it is following Code
 * Graph or pinned, and the two ways out of either.
 */
export function GitFolderBar() {
  const { folder, current, pinned, follow } = useModuleFolder("git");
  const { openFolder, error: pickError } = useFolderPicker();
  const [followError, setFollowError] = useState("");
  const error = pickError || followError;
  return (
    <div className="flex items-center gap-2.5">
      <FolderGit2 className="size-(--icon-14) shrink-0 text-text-secondary" aria-hidden />
      <span className="min-w-0 truncate text-code-12 text-text-secondary" title={folder || undefined}>
        {folder || "No folder"}
      </span>
      <StatePill dot={pinned ? "info" : "success"}>{pinned ? "Pinned to this folder" : "Following Code Graph"}</StatePill>
      {error ? (
        <span className="min-w-0 truncate text-caption-11 text-status-danger" role="alert">
          {error}
        </span>
      ) : null}
      <span className="min-w-0 flex-1" />
      {pinned ? (
        <Button
          variant="ghost"
          onClick={() => {
            setFollowError("");
            follow().catch((e: Error) => setFollowError(e.message));
          }}
        >
          {current ? "Follow open project" : "Follow"}
        </Button>
      ) : null}
      <Button variant="outline" onClick={openFolder}>
        Open folder…
      </Button>
    </div>
  );
}

function GitFolderGate({ View, bar }: { View: ComponentType; bar: boolean }) {
  const { loaded, folder, current, pin, follow } = useModuleFolder("git");
  const [error, setError] = useState("");
  const { openFolder, error: pickError } = useFolderPicker();
  // The repositories in the folder the app has open, so Git can offer its
  // neighbours while it is pinned to one of them.
  const { repos, loading, reload } = useRepositories(repositoryRoot(current, folder));
  // What Go says Git is reading in this folder; null until it has said.
  const project = useGitProject(folder);

  // Choosing one of the folder's repositories points Git at it. The folder the
  // app has open being the repository itself is following, not a pin.
  async function chooseRepository(repo: Repository) {
    setError("");
    try {
      if (repo.path === current) await follow();
      else await pin(repo.path);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  const showBar = loaded && !!folder && (bar || project === "");
  // With the bar up, its own button is the way to another folder.
  const openButton = showBar ? null : (
    <Button variant="outline" onClick={openFolder}>
      Open folder…
    </Button>
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
        body="Git works in the folder Code Graph has open. Open a project there, or pick a folder for Git alone."
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
          root={repositoryRoot(current, folder)}
          repos={repos}
          loading={loading}
          onReload={reload}
          onChoose={chooseRepository}
        />
      ) : (
        <NoRepositories folder={folder}>{openButton}</NoRepositories>
      );
  } else {
    body = <View key={folder} />;
  }
  if (project !== null) lastBody.current = body;

  const problem = error || (showBar ? "" : pickError);
  return (
    <div className="flex h-full min-h-0 flex-col">
      {showBar ? (
        <div className="shrink-0 px-4 pt-4">
          <GitFolderBar />
        </div>
      ) : null}
      {problem ? (
        <p className="shrink-0 px-4 pt-3 text-caption-11 text-status-danger" role="alert">
          {problem}
        </p>
      ) : null}
      <div className="min-h-0 flex-1">{body}</div>
    </div>
  );
}
