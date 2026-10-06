import { parseToolOutput } from "@/workbench/toolResultViews";

/** Mirrors internal/git/git.go: Commit — git.commit's rawOutput. */
export type CommitResult = {
  sha: string;
  short: string;
  subject: string;
};

function isCommitResult(value: unknown): value is CommitResult {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return typeof v.sha === "string" && v.sha !== "" && typeof v.subject === "string";
}

/** Guards git.commit's result before the Card component is imported. */
export function parseCommitResult(rawOutput: unknown): CommitResult | null {
  const parsed = parseToolOutput(rawOutput);
  return isCommitResult(parsed) ? parsed : null;
}

/**
 * Mirrors internal/git/history.go: HistoryResult — what git.rebase,
 * git.cherryPick and git.revert return. `stopped` names the operation waiting
 * on `conflicted`; empty when it finished.
 */
export type HistoryResult = {
  head: CommitResult | null;
  stopped: string;
  conflicted: string[];
};

/** Guards a history tool's result; null unless it carries a head or a stop. */
export function parseHistoryResult(rawOutput: unknown): HistoryResult | null {
  const parsed = parseToolOutput(rawOutput);
  if (!parsed || typeof parsed !== "object") return null;
  const v = parsed as Record<string, unknown>;
  const head = isCommitResult(v.head) ? v.head : null;
  const stopped = typeof v.stopped === "string" ? v.stopped : "";
  const conflicted = Array.isArray(v.conflicted) ? v.conflicted.filter((p): p is string => typeof p === "string") : [];
  if (!head && !stopped) return null;
  return { head, stopped, conflicted };
}
