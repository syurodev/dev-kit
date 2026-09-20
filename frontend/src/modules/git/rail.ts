import type { GitStatus } from "@/bridge/git";

export type ChangeGroup = {
  key: "conflicted" | "staged" | "unstaged" | "untracked";
  label: string;
  files: string[];
};

/**
 * The work tree's changes as the rail lists them: unmerged files first, because
 * they block a commit, then what is staged, then what is not. A group with
 * nothing in it is left out, so a clean tree is an empty list and not four
 * headings over nothing.
 */
export function changeGroups(status: Pick<GitStatus, "conflicted" | "staged" | "unstaged" | "untracked">): ChangeGroup[] {
  const groups: ChangeGroup[] = [
    { key: "conflicted", label: "Conflicts", files: status.conflicted ?? [] },
    { key: "staged", label: "Staged", files: status.staged ?? [] },
    { key: "unstaged", label: "Changes", files: status.unstaged ?? [] },
    { key: "untracked", label: "Untracked", files: status.untracked ?? [] },
  ];
  return groups.filter((group) => group.files.length > 0);
}

/** A path as a file name over the folder it is in. */
export function splitPath(path: string): { name: string; dir: string } {
  const slash = path.lastIndexOf("/");
  return slash < 0 ? { name: path, dir: "" } : { name: path.slice(slash + 1), dir: path.slice(0, slash) };
}

/** The first `limit` rows and how many were left out, for a list that must not run down the rail forever. */
export function capRows<T>(rows: T[], limit: number): { shown: T[]; hidden: number } {
  return rows.length <= limit ? { shown: rows, hidden: 0 } : { shown: rows.slice(0, limit), hidden: rows.length - limit };
}
