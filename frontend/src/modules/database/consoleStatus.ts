import type { QueryResult } from "@/bridge/database";

/**
 * What the console says about the statement it just ran.
 *
 * One line, in a pill beside Run. It is data rather than JSX because the whole
 * of the feature is the wording and the ranking — which of four things
 * happened, in how long, short enough for a toolbar — and a component deciding
 * that inline is a component nobody can test the wording of.
 */
export type ConsoleStatus =
  | { kind: "idle" }
  | { kind: "running"; time: string }
  | { kind: "success"; time: string; message: string }
  | { kind: "warning"; time: string; message: string }
  | { kind: "error"; time: string; message: string; detail: string };

export type ConsoleStatusInput = {
  running: boolean;
  /** The live clock, while running. */
  elapsedSec: number;
  /** What the finished run took. Absent until one has finished. */
  durationMs?: number;
  error?: string;
  rowCount?: number;
  sql: string;
};

/** The running stopwatch: seconds, one decimal, ticking ten times a second. */
function seconds(value: number): string {
  return `${value.toFixed(1)}s`;
}

/**
 * What the run took, once it is over.
 *
 * Milliseconds under a second, seconds above. The pill used to say "0.0s"
 * for everything faster than 50ms while the result header beside it said
 * "32 ms" — the same event, two units, and the one with the useful number
 * was the one that has since been removed. This is now the only place the
 * duration appears, so it has to carry the precision the header did: the
 * difference between 2ms and 900ms is the difference between an index and a
 * table scan, and "0.0s" says neither.
 *
 * The stopwatch above stays in seconds. It only ever renders from 0.1s
 * upward, so the two never disagree on screen about the same moment.
 *
 * Exported because the editor prints the same number beside each statement
 * it ran. One formatter, or the pill and the line disagree about what 1000
 * milliseconds is called.
 */
export function formatDuration(ms: number): string {
  return ms < 1000 ? `${Math.round(ms)}ms` : `${(ms / 1000).toFixed(1)}s`;
}

export function consoleStatus(input: ConsoleStatusInput): ConsoleStatus {
  if (input.running) {
    return { kind: "running", time: seconds(input.elapsedSec) };
  }
  // Idle means nothing has run. A duration alone used to decide that, and the
  // store only records one when a statement SUCCEEDS — so a failure left the
  // engine's sentence sitting in the store while the pill showed an empty
  // toolbar. An error is by itself proof that something ran.
  if (input.durationMs === undefined && !input.error) {
    return { kind: "idle" };
  }
  const time = formatDuration(input.durationMs ?? 0);

  if (input.error) {
    // The first line, because a driver puts the sentence first and the
    // position, the hint and the pointer caret under it. Everything after it
    // is what expanding is for.
    const [first = ""] = input.error.split("\n");
    return { kind: "error", time, message: first.trim(), detail: input.error };
  }

  const warning = statementWarning(input.sql);
  if (warning) {
    return { kind: "warning", time, message: warning };
  }

  const rows = input.rowCount ?? 0;
  return { kind: "success", time, message: `${rows} ${rows === 1 ? "row" : "rows"}` };
}

/**
 * The one thing worth saying about a statement that otherwise succeeded.
 *
 * An UPDATE or DELETE with no WHERE did exactly what it was told to every row
 * it could reach. Nothing is wrong, which is the problem: reported as a plain
 * success it looks like any other, and you find out tomorrow.
 *
 * Comments and string literals are stripped first. Without that, a note
 * reading 'where did it go' supplies a WHERE that is not there, and a
 * commented-out DELETE raises a warning about a statement that never ran.
 */
export function statementWarning(sql: string): string | null {
  const bare = stripNoise(sql);
  const verb = /^\s*(update|delete)\b/i.exec(bare)?.[1];
  if (!verb) {
    return null;
  }
  if (/\bwhere\b/i.test(bare)) {
    return null;
  }
  return `${verb.toUpperCase()} with no WHERE`;
}

/** Line comments, block comments and single-quoted literals, gone. */
function stripNoise(sql: string): string {
  return sql
    .replace(/--[^\n]*/g, " ")
    .replace(/\/\*[\s\S]*?\*\//g, " ")
    .replace(/'(?:[^']|'')*'/g, "''");
}

/**
 * The line over an agent's rows in a console: whose they are, how many, and
 * that they are not live. A cut result says where it was cut, since rows
 * stopped at the cap look exactly like all of them; rows from a call that
 * named no connection say the connection is unknown.
 */
export function agentResultNote(
  result: Pick<QueryResult, "rowCount" | "truncated">,
  options: { connectionUnknown?: boolean } = {},
): string {
  const count = `${result.rowCount} ${result.rowCount === 1 ? "row" : "rows"}`;
  const where = options.connectionUnknown ? " · connection unknown" : "";
  return `Agent's result${where} · ${result.truncated ? `truncated at ${count}` : count} — run to refresh`;
}
