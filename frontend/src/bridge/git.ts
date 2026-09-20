import { AppService } from "../../bindings/devkit/internal/bridge";
import { callBridge } from "./client";

export type GitAvailability = {
  ok: boolean;
  path: string;
  version: string;
  reason: string;
};

export type GitStatus = {
  branch: string;
  upstream: string;
  ahead: number;
  behind: number;
  detached: boolean;
  files: Record<string, string>;
  staged: string[] | null;
  unstaged: string[] | null;
  untracked: string[] | null;
  /** Unmerged paths. Not "modified in both": staging one commits the markers. */
  conflicted: string[] | null;
};

export type Change = {
  path: string;
  kind: "added" | "modified" | "deleted" | "typechange";
  added: number;
  removed: number;
  mode: string;
};

export type Commit = {
  sha: string;
  short: string;
  subject: string;
  body: string;
  author: string;
  date: string;
};

export type Branch = { name: string; current: boolean; upstream: string };

export type Remote = { name: string; url: string; accountId: string; env: string };

/** One unmerged file, in the shape the shared MergePanes layout renders. */
export type Conflict = {
  path: string;
  ours: string;
  theirs: string;
  base: string;
  /**
   * Whether that stage EXISTS, which an empty string cannot say: a side that
   * deleted the file and a side that emptied it are different conflicts.
   */
  hasOurs: boolean;
  hasTheirs: boolean;
  hasBase: boolean;
  /**
   * Git's own merge as it sits in the work tree — everything that merged
   * cleanly, plus `<<<<<<<` markers around what did not. This is what the
   * middle pane edits; seeding from `ours` would drop the other side's clean
   * changes without saying so.
   */
  merged: string;
  /** Nothing to merge by hand: only a side to pick. */
  binary: boolean;
};

export type ConflictSide = "ours" | "theirs";

export function gitAvailability(): Promise<GitAvailability> {
  return callBridge(async () => (await AppService.GitAvailability()) as GitAvailability);
}

/**
 * The folder the module is reading — "" when it is in none: no folder open, or
 * one that is not a repository. Pointing the module somewhere is the work
 * folder's job (bridge/workfolder.ts); this is only what came of it.
 */
export function gitProject(): Promise<string> {
  return callBridge(async () => (await AppService.GitProject()) as string);
}

export function gitStatus(): Promise<GitStatus> {
  return callBridge(async () => (await AppService.GitStatus()) as GitStatus);
}

export function gitDiff(rev = "", staged = false): Promise<Change[]> {
  return callBridge(async () => ((await AppService.GitDiff(rev, staged)) ?? []) as Change[]);
}

export function gitLog(limit = 100): Promise<Commit[]> {
  return callBridge(async () => ((await AppService.GitLog(limit)) ?? []) as Commit[]);
}

/** A task's commits, found by its trailer rather than by a stored SHA. */
export function commitsForTask(key: string, limit = 50): Promise<Commit[]> {
  return callBridge(
    async () => ((await AppService.GitCommitsForTask(key, limit)) ?? []) as Commit[],
  );
}

export function gitStage(paths: string[]): Promise<void> {
  return callBridge(async () => {
    await AppService.GitStage(paths);
  });
}

export function gitUnstage(paths: string[]): Promise<void> {
  return callBridge(async () => {
    await AppService.GitUnstage(paths);
  });
}

/**
 * One file's unified diff, work tree against the index — the pairing hunk
 * staging needs, because gitStageHunk applies what it is given to the index.
 * A binary or untracked file has no hunks and comes back empty.
 */
export function gitFilePatch(path: string): Promise<string> {
  return callBridge(async () => await AppService.GitFilePatch(path));
}

export function gitStageHunk(patch: string): Promise<void> {
  return callBridge(async () => {
    await AppService.GitStageHunk(patch);
  });
}

/** Stages exactly what one agent turn changed. */
export function gitStageTurn(paths: string[]): Promise<string[]> {
  return callBridge(async () => (await AppService.GitStageTurn(paths)) ?? []);
}

export function gitCommit(message: string, taskKey = "", amend = false): Promise<Commit> {
  return callBridge(
    async () => (await AppService.GitCommit(message, taskKey, amend)) as Commit,
  );
}

export function gitBranches(): Promise<Branch[]> {
  return callBridge(async () => ((await AppService.GitBranches()) ?? []) as Branch[]);
}

/**
 * Returns the branch name so the caller can record it on a task — which is
 * what makes an agent's push permission outlive its session.
 */
export function gitCreateBranch(name: string, from = ""): Promise<string> {
  return callBridge(async () => await AppService.GitCreateBranch(name, from));
}

export function gitCheckout(name: string): Promise<void> {
  return callBridge(async () => {
    await AppService.GitCheckout(name);
  });
}

export function gitStash(message = ""): Promise<void> {
  return callBridge(async () => {
    await AppService.GitStash(message);
  });
}

export function gitStashPop(): Promise<void> {
  return callBridge(async () => {
    await AppService.GitStashPop();
  });
}

export function gitConflict(path: string): Promise<Conflict> {
  return callBridge(async () => (await AppService.GitConflict(path)) as Conflict);
}

export function gitResolve(path: string, merged: string): Promise<void> {
  return callBridge(async () => {
    await AppService.GitResolve(path, merged);
  });
}

/**
 * Takes one whole side. Separate from gitResolve because git copies the stage
 * itself: sending the text out here and back would re-encode it, and a binary
 * file could not be expressed at all.
 */
export function gitResolveSide(path: string, side: ConflictSide): Promise<void> {
  return callBridge(async () => {
    await AppService.GitResolveSide(path, side);
  });
}

export function gitAbortMerge(): Promise<void> {
  return callBridge(async () => {
    await AppService.GitAbortMerge();
  });
}

export function gitRemotes(): Promise<Remote[]> {
  return callBridge(async () => ((await AppService.GitRemotes()) ?? []) as Remote[]);
}

/** A protected branch pattern: the thing that turns commit gating on. */
export type Guard = { id: string; projectKey: string; pattern: string; env: string };

export function gitGuards(): Promise<Guard[]> {
  return callBridge(async () => ((await AppService.GitGuards()) ?? []) as Guard[]);
}

/**
 * Protects a branch pattern with an environment tag. Without a guard,
 * committing is never gated — that is the default, and it is deliberate; a
 * guard is for the repository whose commits are deploy manifests.
 */
export function createGuard(pattern: string, env: string): Promise<Guard> {
  return callBridge(async () => (await AppService.GitCreateGuard(pattern, env)) as Guard);
}

export function deleteGuard(id: string): Promise<void> {
  return callBridge(async () => {
    await AppService.GitDeleteGuard(id);
  });
}

/**
 * Points a remote at an account. An empty account clears the binding and git's
 * own configuration takes over again.
 */
export function bindRemote(name: string, accountId: string, env: string): Promise<Remote> {
  return callBridge(async () => (await AppService.GitBindRemote(name, accountId, env)) as Remote);
}

/**
 * Stages exactly what one agent turn changed and drafts its message. Staging
 * and drafting is the part a machine can do; committing stays a decision.
 */
export type TurnCommit = {
  staged: string[];
  skipped: string[];
  message: string;
  taskKey?: string;
};

export function commitTurn(sessionId: string, checkpointId: string): Promise<TurnCommit> {
  return callBridge(
    async () => (await AppService.GitCommitTurn(sessionId, checkpointId)) as TurnCommit,
  );
}

export function gitPush(remote: string, branch: string, setUpstream = false): Promise<void> {
  return callBridge(async () => {
    await AppService.GitPush(remote, branch, setUpstream);
  });
}

export function gitFetch(remote = "origin"): Promise<void> {
  return callBridge(async () => {
    await AppService.GitFetch(remote);
  });
}

export function gitPull(remote = "origin"): Promise<void> {
  return callBridge(async () => {
    await AppService.GitPull(remote);
  });
}

/**
 * Seeds a commit message from what is staged. The body is a statement of
 * fact — the paths — rather than prose a model invented; the subject line is
 * left for the person to write.
 */
export function seedMessage(staged: string[], taskKey = ""): string {
  const lines: string[] = ["", ""];
  for (const path of staged.slice(0, 20)) {
    lines.push(`- ${path}`);
  }
  if (staged.length > 20) {
    lines.push(`- …and ${staged.length - 20} more`);
  }
  if (taskKey) {
    lines.push("", `Task: ${taskKey}`);
  }
  return lines.join("\n");
}
