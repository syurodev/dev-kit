/**
 * The pure half of the composer's context bar and its panel area: which panel
 * is showing, and what the repository chip says. No React, no stores — each
 * rule is a test.
 */

export type TrayOpened = "subagents" | "tasks" | "repos" | "commands" | null;

export type TrayPanelKind = "permission" | "question" | TrayOpened;

/**
 * Which panel the area shows — one at a time, first of:
 *
 * 1. a permission the agent is waiting on (it blocks the turn),
 * 2. a question it is waiting on,
 * 3. whatever the person opened from the bar.
 *
 * A permission or question the person folded away is not showing; the bar keeps
 * a chip for it. An opened list whose subject has gone — the last subagent
 * finished, the boards were unbound — closes with it rather than showing an
 * empty list. (An "@" or "/" list being typed goes above all of these; the
 * composer decides that, since it is the one that knows.)
 */
export function chooseTrayPanel(input: {
  permission: boolean;
  permissionFolded: boolean;
  question: boolean;
  questionFolded: boolean;
  opened: TrayOpened;
  runningSubagents: number;
  hasBoards: boolean;
  repoCount: number;
  slowCommands: boolean;
}): TrayPanelKind | null {
  if (input.permission && !input.permissionFolded) return "permission";
  if (input.question && !input.questionFolded) return "question";
  switch (input.opened) {
    case "subagents":
      return input.runningSubagents > 0 ? "subagents" : null;
    case "tasks":
      return input.hasBoards ? "tasks" : null;
    case "repos":
      return input.repoCount > 0 ? "repos" : null;
    case "commands":
      return input.slowCommands ? "commands" : null;
    default:
      return null;
  }
}

/** The fields of a repository the summary reads (a Repository from bridge/git has more). */
export type SummarisedRepo = {
  name: string;
  changed: number;
  conflicted: number;
  ahead?: number;
  behind?: number;
  error?: string;
};

export type RepoSummary = {
  /** One repository: its branch. Several: how many there are. */
  label: string;
  /** The chip's tooltip and name: every number, in words. */
  title: string;
  /** A second number, apart from the label's: how many need a look. Absent when none do. */
  badge: { value: number; tone: "warning" | "danger"; label: string } | null;
};

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;

/**
 * The repository chip. Several repositories used to read "8 repos 6", a total
 * and a subset in one pill with nothing to say the six were a part of the
 * eight. The label now says only the total, and the number that needs a look
 * is a badge of its own — a tone, and a sentence naming exactly what it
 * counts.
 *
 * "Needs a look" is a repository with changes, conflicts, or one that could
 * not be read; conflicts and unreadable ones make the badge danger.
 *
 * A single repository's badge counts its changed files instead: there is
 * nothing to count among one.
 */
export function repoSummary(repos: readonly SummarisedRepo[], branch: string | null): RepoSummary {
  if (repos.length === 1) {
    const repo = repos[0];
    const name = branch ?? repo.name;
    const sync = [repo.ahead ? `↑${repo.ahead}` : "", repo.behind ? `↓${repo.behind}` : ""].filter(Boolean).join(" ");
    const parts = [`Branch ${name}`, sync].filter(Boolean);
    if (repo.error) {
      return { label: name, title: [...parts, "unreadable"].join(" · "), badge: { value: 1, tone: "danger", label: "Repository unreadable" } };
    }
    if (repo.conflicted > 0) {
      const label = `${plural(repo.conflicted, "conflicted file")}${repo.changed ? `, ${repo.changed} changed` : ""}`;
      return { label: name, title: [...parts, label].join(" · "), badge: { value: repo.conflicted, tone: "danger", label } };
    }
    if (repo.changed > 0) {
      const label = plural(repo.changed, "changed file");
      return { label: name, title: [...parts, label].join(" · "), badge: { value: repo.changed, tone: "warning", label } };
    }
    return { label: name, title: [...parts, "clean"].join(" · "), badge: null };
  }

  const total = repos.length;
  const conflicted = repos.filter((repo) => repo.conflicted > 0).length;
  const unreadable = repos.filter((repo) => repo.error).length;
  const changed = repos.filter((repo) => !repo.error && repo.conflicted === 0 && repo.changed > 0).length;
  const attention = conflicted + unreadable + changed;
  const label = plural(total, "repo");
  if (attention === 0) return { label, title: `${plural(total, "repository", "repositories")} · all clean`, badge: null };

  const reasons = [
    conflicted ? `${conflicted} with conflicts` : "",
    changed ? `${changed} with changes` : "",
    unreadable ? `${unreadable} unreadable` : "",
  ].filter(Boolean);
  const sentence = `${attention} of ${total} ${total === 1 ? "repository" : "repositories"} need attention (${reasons.join(", ")})`;
  return {
    label,
    title: `${plural(total, "repository", "repositories")} · ${sentence}`,
    badge: { value: attention, tone: conflicted || unreadable ? "danger" : "warning", label: sentence },
  };
}
