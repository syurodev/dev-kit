import { parseToolOutput } from "@/workbench/toolResultViews";
import { firstLine, registerToolActivity, stringField, toolRecord, type ToolActivityTarget } from "@/workbench/toolActivity";
import { parseCommitResult, parseHistoryResult } from "./commitResult";

/** A commit's card target: its subject, its short hash, and its diff. */
function commitTarget(commit: { sha: string; short: string; subject: string }): ToolActivityTarget {
  return { label: commit.subject, detail: commit.short, ref: { module: "git", kind: "commit", id: commit.sha, label: commit.short } };
}

/** The input's commits, as the card names them: the first, and how many more. */
function commitsLabel(rawInput: unknown): string {
  const raw = toolRecord(rawInput)?.commits;
  const commits = Array.isArray(raw) ? raw.filter((c): c is string => typeof c === "string") : [];
  if (commits.length === 0) return "";
  return commits.length === 1 ? commits[0] : `${commits[0]} +${commits.length - 1}`;
}

/**
 * A history tool that can stop: a finished one names the branch's new head;
 * a stopped one says so, which is the thing the person needs to see.
 */
function historyTarget(rawOutput: unknown, state: string, pending: ToolActivityTarget | null): ToolActivityTarget | null {
  const result = state === "done" ? parseHistoryResult(rawOutput) : null;
  if (result?.stopped) {
    const n = result.conflicted.length;
    return { label: `Stopped on ${n || "a"} conflict${n === 1 ? "" : "s"}`, detail: result.conflicted[0] };
  }
  if (result?.head) return commitTarget(result.head);
  return pending;
}

/**
 * What the git.* tools that change something do (workbench/toolActivity.ts).
 * Push is here above all: it is the one that leaves the machine.
 */
export function registerGitActivities(): void {
  registerToolActivity("git.commit", {
    module: "git",
    running: "Committing",
    done: "Committed",
    failed: "Couldn't commit",
    preload: () => import("./GitCommitResultCard"),
    target: ({ rawInput, rawOutput, state }) => {
      const commit = state === "done" ? parseCommitResult(rawOutput) : null;
      if (commit) return commitTarget(commit);
      const message = firstLine(stringField(rawInput, "message"));
      return message ? { label: message } : null;
    },
  });
  registerToolActivity("git.push", {
    module: "git",
    running: "Pushing",
    done: "Pushed",
    failed: "Couldn't push",
    target: ({ rawInput, rawOutput, state }) => {
      const out = state === "done" ? toolRecord(parseToolOutput(rawOutput)) : null;
      const branch = stringField(rawInput, "branch") || stringField(out, "branch");
      const remote = stringField(rawInput, "remote") || stringField(out, "remote");
      if (!branch && !remote) return null;
      return { label: branch || "current branch", detail: remote ? `to ${remote}` : undefined };
    },
  });
  registerToolActivity("git.branch.create", {
    module: "git",
    running: "Creating branch",
    done: "Created branch",
    failed: "Couldn't create branch",
    target: ({ rawInput }) => {
      const name = stringField(rawInput, "name");
      const from = stringField(rawInput, "from");
      return name ? { label: name, detail: from ? `from ${from}` : undefined } : null;
    },
  });
  registerToolActivity("git.checkout", {
    module: "git",
    running: "Switching branch",
    done: "Switched branch",
    failed: "Couldn't switch branch",
    target: ({ rawInput }) => {
      const name = stringField(rawInput, "name");
      return name ? { label: name } : null;
    },
  });
  // Fetch and pull answer where the branch stands; that is the card.
  for (const [tool, verb, past] of [
    ["git.fetch", "Fetching", "Fetched"],
    ["git.pull", "Pulling", "Pulled"],
  ] as const) {
    registerToolActivity(tool, {
      module: "git",
      running: verb,
      done: past,
      failed: `Couldn't ${tool === "git.fetch" ? "fetch" : "pull"}`,
      target: ({ rawInput, rawOutput, state }) => {
        const remote = stringField(rawInput, "remote") || "origin";
        const out = state === "done" ? toolRecord(parseToolOutput(rawOutput)) : null;
        if (!out) return { label: remote };
        const ahead = typeof out.ahead === "number" ? out.ahead : 0;
        const behind = typeof out.behind === "number" ? out.behind : 0;
        const where = [ahead ? `${ahead} ahead` : "", behind ? `${behind} behind` : ""].filter(Boolean).join(", ") || "up to date";
        return { label: stringField(out, "branch") || remote, detail: `${stringField(out, "upstream") || remote} · ${where}` };
      },
    });
  }
  // History: amend and squash rewrite only what no remote has; rebase, too,
  // and can stop on a conflict, like cherry-pick and revert.
  registerToolActivity("git.amend", {
    module: "git",
    running: "Amending commit",
    done: "Amended commit",
    failed: "Couldn't amend",
    preload: () => import("./GitCommitResultCard"),
    target: ({ rawInput, rawOutput, state }) => {
      const commit = state === "done" ? parseCommitResult(rawOutput) : null;
      if (commit) return commitTarget(commit);
      const message = firstLine(stringField(rawInput, "message"));
      return message ? { label: message } : null;
    },
  });
  registerToolActivity("git.squash", {
    module: "git",
    running: "Squashing commits",
    done: "Squashed commits",
    failed: "Couldn't squash",
    preload: () => import("./GitCommitResultCard"),
    target: ({ rawInput, rawOutput, state }) => {
      const commit = state === "done" ? parseCommitResult(rawOutput) : null;
      const count = toolRecord(rawInput)?.count;
      const many = typeof count === "number" ? `${count} commits` : undefined;
      if (commit) return { ...commitTarget(commit), detail: many ? `${many} · ${commit.short}` : commit.short };
      return many ? { label: many } : null;
    },
  });
  registerToolActivity("git.rebase", {
    module: "git",
    running: "Rebasing",
    done: "Rebased",
    failed: "Couldn't rebase",
    preload: () => import("./GitHistoryResultCard"),
    target: ({ rawInput, rawOutput, state }) => {
      const action = stringField(rawInput, "action");
      const onto = stringField(rawInput, "onto");
      const pending = action === "abort" ? { label: "Abort" } : action === "continue" ? { label: "Continue" } : onto ? { label: `onto ${onto}` } : null;
      // An abort puts the branch back: its head is the one it had, not news.
      if (action === "abort") return pending;
      return historyTarget(rawOutput, state, pending);
    },
  });
  for (const [tool, verb, past] of [
    ["git.cherryPick", "Cherry-picking", "Cherry-picked"],
    ["git.revert", "Reverting", "Reverted"],
  ] as const) {
    registerToolActivity(tool, {
      module: "git",
      running: verb,
      done: past,
      failed: `Couldn't ${past === "Reverted" ? "revert" : "cherry-pick"}`,
      preload: () => import("./GitHistoryResultCard"),
      target: ({ rawInput, rawOutput, state }) => {
        const action = stringField(rawInput, "action");
        if (action === "abort") return { label: "Abort" };
        const commits = commitsLabel(rawInput);
        return historyTarget(rawOutput, state, action === "continue" ? { label: "Continue" } : commits ? { label: commits } : null);
      },
    });
  }
}
