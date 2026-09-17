import type { CheckpointAvailability } from "../../../bindings/devkit/internal/agenthub/models";

/**
 * What the Settings row says about file checkpoints.
 *
 * Two facts, never collapsed: whether this machine can take checkpoints, and
 * whether the user wants it to. A switch showing "off" with no reason reads as
 * broken, and one showing "on" on a machine with no git is a lie — so the
 * sentence beside it always names which of the two is the case.
 */

export type CheckpointStatusLine = {
  /** The sentence beside the switch. */
  text: string;
  /** Warning tone for a machine that cannot do this; plain otherwise. */
  tone: "plain" | "warning";
  /** The switch is dead when the machine cannot do it at all. */
  switchEnabled: boolean;
};

export function checkpointStatusLine(
  status: CheckpointAvailability | null | undefined,
): CheckpointStatusLine {
  if (!status) {
    return { text: "Checking…", tone: "plain", switchEnabled: false };
  }
  if (!status.available) {
    // The reason comes from the backend and already names the fix — installing
    // git, running xcode-select, upgrading a version that is too old.
    return {
      text: status.reason || "File checkpoints are not available on this machine.",
      tone: "warning",
      switchEnabled: false,
    };
  }
  if (!status.enabled) {
    return {
      text: `Off. Git ${status.gitVersion} is available at ${status.gitPath}.`,
      tone: "plain",
      switchEnabled: true,
    };
  }
  return {
    text: `On, using git ${status.gitVersion} at ${status.gitPath}.`,
    tone: "plain",
    switchEnabled: true,
  };
}

/**
 * The projects that could not be checkpointed, as lines to show under the row.
 *
 * Without these, a project that is simply too large looks exactly like the
 * feature quietly not working — and the fix for the first (raise the ceiling,
 * add an exclude) is right there in the same panel.
 */
export function checkpointProblemLines(
  status: CheckpointAvailability | null | undefined,
): { project: string; reason: string }[] {
  if (!status?.available || !status.enabled) return [];
  return (status.problems ?? []).map((problem) => ({
    project: problem.project,
    reason: tidyReason(problem.reason),
  }));
}

/**
 * The backend's error strings are wrapped for a log; the panel wants the
 * sentence, not the chain of packages that produced it.
 */
function tidyReason(reason: string): string {
  const trimmed = reason.replace(/^checkpoint:\s*/, "").trim();
  if (!trimmed) return "This project could not be snapshotted.";
  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
}

/** A typed number back from an input, or null when it is not one. */
export function parsePositiveInteger(value: string): number | null {
  const trimmed = value.trim();
  if (!/^[0-9]+$/.test(trimmed)) return null;
  const parsed = Number(trimmed);
  return Number.isSafeInteger(parsed) ? parsed : null;
}

/** The excludes box is one pattern per line; empty lines are not patterns. */
export function parseExcludes(value: string): string[] {
  return value
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

export function formatExcludes(patterns: readonly string[] | null | undefined): string {
  return (patterns ?? []).join("\n");
}
