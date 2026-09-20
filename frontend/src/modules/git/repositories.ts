import type { Repository } from "@/bridge/git";

/** What a repository's row says, in the order a person scans it: where it is, then how it stands. */
export type RepositoryLine = {
  branch: string;
  /** "clean", "3 changes" — or why the repository could not be read. */
  state: string;
  /** "↑2 ↓1", empty when level with its upstream. */
  sync: string;
  /** True for a state that needs attention: unmerged files, or unreadable. */
  attention: boolean;
};

export function describeRepository(repo: Repository): RepositoryLine {
  if (repo.error) {
    return { branch: "", state: "cannot read", sync: "", attention: true };
  }
  const branch = repo.detached ? "detached HEAD" : repo.branch || "no commits";
  const sync = [repo.ahead > 0 ? `↑${repo.ahead}` : "", repo.behind > 0 ? `↓${repo.behind}` : ""]
    .filter(Boolean)
    .join(" ");
  if (repo.conflicted > 0) {
    return { branch, state: `${repo.conflicted} conflicted`, sync, attention: true };
  }
  const state = repo.changed === 0 ? "clean" : `${repo.changed} change${repo.changed === 1 ? "" : "s"}`;
  return { branch, state, sync, attention: false };
}

/**
 * The folder the repositories are searched under: the folder the app has open,
 * which stays put while Git is pinned to one of its repositories — that is what
 * lets the switcher list the neighbours of the repository Git is in. With no
 * folder open it is the folder Git itself is in.
 */
export function repositoryRoot(current: string, folder: string): string {
  return current || folder;
}

/** The last `n` segments of a path, for a name that says where a folder is without the whole way there. */
export function pathTail(path: string, n: number): string {
  return path.split("/").filter(Boolean).slice(-n).join("/");
}

export type FolderStatus = { tone: "success" | "info" | "warning"; text: string };

/**
 * How Git stands to the app's work folder, as the line under the rail's header
 * and the pill on the folder bar say it. Three answers, and each is a fact the
 * person could otherwise only work out from the path: Git is following Code
 * Graph, or it is pinned and Code Graph is somewhere else, or the folder it is
 * in is not a repository at all.
 */
export function folderStatus(input: {
  folder: string;
  current: string;
  pinned: boolean;
  /** What Go says Git is reading in `folder`; "" when it is in no repository. */
  project: string | null;
  repos: Repository[] | null;
}): FolderStatus {
  const { folder, current, pinned, project, repos } = input;
  if (pinned) {
    if (!current) return { tone: "info", text: "Pinned · Code Graph has no project open" };
    const name = pathTail(current, 1);
    return repos?.some((repo) => repo.path === current)
      ? { tone: "info", text: `Pinned · Code Graph is in ${name}` }
      : { tone: "info", text: `Pinned · ${name} is not a repository` };
  }
  if (project === "") return { tone: "warning", text: "Not a repository · following Code Graph" };
  return { tone: "success", text: `Following Code Graph · ${pathTail(folder, 2)}` };
}
