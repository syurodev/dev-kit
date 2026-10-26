import type { UsageGroup, UsageMetric, UsageReport, UsageSession, UsageTotals } from "@/bridge/agentUsage";

/** Shown with every report: a token count is not the subscription's own measure. */
export const QUOTA_NOTE =
  "These are tokens, not a share of your subscription limit. How the limit counts cached reads is not known, so fewer tokens does not by itself mean a smaller share.";

const DASH = "—";

export function formatTokens(n: number): string {
  if (n < 1_000) return String(n);
  if (n < 1_000_000) return `${(n / 1_000).toFixed(1)}K`;
  if (n < 1_000_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  return `${(n / 1_000_000_000).toFixed(2)}B`;
}

function percent(part: number, whole: number): string {
  return whole > 0 ? `${((part / whole) * 100).toFixed(1)}%` : DASH;
}

export function formatMetric(totals: UsageTotals, metric: UsageMetric): string {
  const known = totals.known[metric];
  if (known === 0) return DASH;
  return `${formatTokens(totals[metric])}${known < totals.turns ? " (partial)" : ""}`;
}

/** A usable denominator only when every turn reported its total. */
export function completeTotal(totals: UsageTotals): number | null {
  return totals.turns > 0 && totals.known.total === totals.turns ? totals.total : null;
}

/** Only divide metrics covering the same complete set of turns. */
export function cacheReadShare(totals: UsageTotals): string {
  const total = completeTotal(totals);
  return total !== null && totals.known.cacheRead === totals.turns
    ? percent(totals.cacheRead, total) : DASH;
}

/** A workspace id as the window names it: `""` is no workspace, an id it does not know was removed. */
export function workspaceName(id: string, names: Record<string, string>): string {
  if (id === "") return "No workspace";
  return names[id] ?? "Removed workspace";
}

export type UsageRow = {
  key: string;
  name: string;
  turns: number;
  freshInput: string;
  total: string;
  cacheRead: string;
  cacheWrite: string;
  output: string;
  /** This row's share of the grand total, or a dash. */
  share: string;
};

/** Rows in the order given, each with its share of `grandTotal`. */
export function rowsOf(groups: UsageGroup[], name: (key: string) => string, grandTotal: number | null): UsageRow[] {
  return groups.map((group) => ({
    key: group.key,
    name: name(group.key),
    turns: group.totals.turns,
    freshInput: formatMetric(group.totals, "freshInput"),
    total: formatMetric(group.totals, "total"),
    cacheRead: formatMetric(group.totals, "cacheRead"),
    cacheWrite: formatMetric(group.totals, "cacheWrite"),
    output: formatMetric(group.totals, "output"),
    share: grandTotal !== null && completeTotal(group.totals) !== null
      ? percent(group.totals.total, grandTotal) : DASH,
  }));
}

const SHORT_ID = 8;

/** Heaviest-session rows: a short id and the workspace's name, never a title. */
export function sessionRowsOf(sessions: UsageSession[], names: Record<string, string>, grandTotal: number | null): UsageRow[] {
  return sessions.map((session) => ({
    ...rowsOf([{ key: session.sessionId, totals: session.totals }], () => "", grandTotal)[0],
    name: `${session.sessionId.slice(0, SHORT_ID)} · ${workspaceName(session.workspaceId, names)}`,
  }));
}

/** The newest `limit` days, newest first (the report lists them oldest first). */
export function recentDays(days: UsageGroup[], limit: number): UsageGroup[] {
  return days.slice(-limit).reverse();
}

/** What a reader must know to read the numbers right; empty when nothing is off. */
export function dataNotes(report: UsageReport): string[] {
  const notes: string[] = [];
  const { turns, turnsWithoutData } = report.totals;
  if (turnsWithoutData > 0) {
    notes.push(`${turnsWithoutData} of ${turns} turns have no token data and are not counted.`);
  }
  if (report.partialSplitTurns > 0) {
    notes.push(
      `On ${report.partialSplitTurns} turns the per-model split is incomplete or differs from the turn total. Model totals may not add up to the overall total.`,
    );
  }
  return notes;
}
