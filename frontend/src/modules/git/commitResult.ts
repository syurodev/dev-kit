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
