import { useEffect, useState, type ComponentType } from "react";
import { FolderOpen, GitBranch, Pin } from "lucide-react";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { agentHubChooseDirectory } from "@/bridge/agenthub";
import { gitProject, type Repository } from "@/bridge/git";
import { useModuleFolder } from "@/stores/workFolder";
import { NoRepositories, RepositoryList, RepositorySwitcher, useRepositories } from "./GitRepositories";
import { repositoryRoot } from "./repositories";

/**
 * Wraps a Git view with the folder it reads. Git works in the app's work
 * folder — whatever Code Graph has open — until it is pointed at another, and
 * every view shows the same bar so it is always clear which repository it is
 * looking at and how to change that.
 *
 * The view is keyed by folder: a folder change remounts it, so each view loads
 * the new repository from scratch instead of each one having to notice.
 */
export function withGitFolder(View: ComponentType): ComponentType {
  return function GitFolderView() {
    return <GitFolderGate View={View} />;
  };
}

function GitFolderGate({ View }: { View: ComponentType }) {
  const { loaded, folder, current, pinned, pin, follow } = useModuleFolder("git");
  const [error, setError] = useState("");
  // The repositories in the folder the app has open, so Git can offer its
  // neighbours while it is pinned to one of them.
  const { repos, loading, reload } = useRepositories(repositoryRoot(current, folder));
  // What Go says Git is reading, and for which folder it said so, so a stale
  // answer to the previous folder is never taken for this one's.
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

  async function openFolder() {
    setError("");
    try {
      const picked = await agentHubChooseDirectory(folder);
      // "" is a cancelled dialog: Git keeps the folder it had.
      if (picked) await pin(picked);
    } catch (e) {
      setError((e as Error).message);
    }
  }

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

  const openButton = (
    <Button variant="outline" size="sm" onClick={openFolder}>
      <FolderOpen aria-hidden /> Open folder…
    </Button>
  );

  let body;
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
  } else if (asked?.folder !== folder) {
    body = null;
  } else if (!asked.project) {
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
          action={openButton}
        />
      ) : (
        <NoRepositories folder={folder}>{openButton}</NoRepositories>
      );
  } else {
    body = <View key={folder} />;
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-2 border-b border-border-subtle px-3 py-1.5">
        {pinned ? <Pin className="size-3.5 shrink-0 text-fg-muted" aria-label="Pinned to this folder" /> : null}
        {asked?.folder === folder && asked.project && repos && repos.length > 1 ? (
          <div className="min-w-0 flex-1">
            <RepositorySwitcher folder={folder} repos={repos} onOpen={reload} onChoose={chooseRepository} />
          </div>
        ) : (
          <span className="min-w-0 flex-1 truncate font-mono text-xs text-fg-muted" title={folder || undefined}>
            {folder || "No folder"}
          </span>
        )}
        {error ? <span className="truncate text-xs text-status-danger">{error}</span> : null}
        {pinned ? (
          <Button variant="ghost" size="sm" onClick={() => follow().catch((e: Error) => setError(e.message))}>
            {current ? "Follow open project" : "Follow"}
          </Button>
        ) : null}
        {folder ? (
          <Button variant="ghost" size="sm" onClick={openFolder}>
            Open folder…
          </Button>
        ) : null}
      </div>
      <div className="min-h-0 flex-1">{body}</div>
    </div>
  );
}
