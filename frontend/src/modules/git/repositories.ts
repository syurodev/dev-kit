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
