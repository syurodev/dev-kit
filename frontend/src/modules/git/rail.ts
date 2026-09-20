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

/** What a status mark says about one path — the vocabulary of Figma `Git / Status Mark`. */
export type MarkKind = "modified" | "added" | "deleted" | "untracked" | "renamed" | "conflict";

/**
 * The mark for a path in the work tree. The group decides before the letter
 * does: git reports an untracked file as "added" (its state is "A"), but it is
 * not staged and a reader scanning the rail should not take it for one that is.
 */
export function markKind(group: ChangeGroup["key"], mark: string | undefined): MarkKind {
  if (group === "conflicted") return "conflict";
  if (group === "untracked") return "untracked";
  if (mark === "A") return "added";
  if (mark === "D") return "deleted";
  if (mark === "R") return "renamed";
  return "modified";
}

/** The mark for a file in a commit, from the kind its diff reports. */
export function changeMarkKind(kind: "added" | "modified" | "deleted" | "typechange"): MarkKind {
  return kind === "added" ? "added" : kind === "deleted" ? "deleted" : "modified";
}

/** How far a branch is from its upstream — "↑2 ahead · ↓1 behind" — and empty when it is level. */
export function syncText(status: Pick<GitStatus, "ahead" | "behind">): string {
  return [status.ahead > 0 ? `↑${status.ahead} ahead` : "", status.behind > 0 ? `↓${status.behind} behind` : ""]
    .filter(Boolean)
    .join(" · ");
}

/**
 * The line under a repository's name: where it is, then how it stands against
 * its upstream — "dev · ↑2 ahead". Nothing after the branch when it is level.
 */
export function branchMeta(status: Pick<GitStatus, "branch" | "detached" | "ahead" | "behind">): string {
  const where = status.detached ? "detached HEAD" : status.branch || "no commits";
  return [where, syncText(status)].filter(Boolean).join(" · ");
}
