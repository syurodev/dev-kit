import { AppService } from "../../bindings/devkit/internal/bridge";
import type {
  AgentUsageGroupInfo as RawGroup,
  AgentUsageReportInfo as RawReport,
  AgentUsageSessionInfo as RawSession,
  AgentUsageTotalsInfo as RawTotals,
} from "../../bindings/devkit/internal/bridge/models";
import { callBridge } from "./client";

/**
 * What a vendor's turns spent, in tokens (Settings → Agents → Accounts & limits).
 * The window's own call: there is no MCP tool for it and the backend refuses an
 * agent caller. It carries numbers and ids only. Tokens are not a share of a
 * subscription limit: how a limit counts cached reads is not known.
 */

export type UsageMetric = "freshInput" | "cacheRead" | "cacheWrite" | "output" | "total";

/** Sums plus counts of known values; a zero sum without coverage is not measured zero. */
export type UsageTotals = {
  known: Record<UsageMetric, number>;
  turns: number;
  turnsWithoutData: number;
  freshInput: number;
  cacheRead: number;
  cacheWrite: number;
  output: number;
  total: number;
};

/** One workspace id (`""` = none), UTC day (`YYYY-MM-DD`) or model. A model group fills `turns` and `total` only. */
export type UsageGroup = { key: string; totals: UsageTotals };

export type UsageSession = { sessionId: string; workspaceId: string; totals: UsageTotals };

export type UsageReport = {
  vendor: string;
  totals: UsageTotals;
  byWorkspace: UsageGroup[];
  byAccount: { accountId: string; generation: string; totals: UsageTotals }[];
  /** Oldest first. */
  byDay: UsageGroup[];
  byModel: UsageGroup[];
  /** Heaviest first. */
  topSessions: UsageSession[];
  /** Turns whose model split is incomplete, differs, or cannot be compared with a known turn total. */
  partialSplitTurns: number;
};

const totalsOf = (raw: RawTotals): UsageTotals => ({
  known: {
    freshInput: raw.known?.["freshInput"] ?? 0,
    cacheRead: raw.known?.["cacheRead"] ?? 0,
    cacheWrite: raw.known?.["cacheWrite"] ?? 0,
    output: raw.known?.["output"] ?? 0,
    total: raw.known?.["total"] ?? 0,
  },
  turns: raw.turns,
  turnsWithoutData: raw.turnsWithoutData,
  freshInput: raw.freshInput,
  cacheRead: raw.cacheRead,
  cacheWrite: raw.cacheWrite,
  output: raw.output,
  total: raw.total,
});

const groupOf = (raw: RawGroup): UsageGroup => ({ key: raw.key, totals: totalsOf(raw.totals) });

const sessionOf = (raw: RawSession): UsageSession => ({
  sessionId: raw.sessionId,
  workspaceId: raw.workspaceId,
  totals: totalsOf(raw.totals),
});

/** The bindings type every list `T[] | null`; the window wants `[]`. */
export function reportOf(raw: RawReport): UsageReport {
  return {
    vendor: raw.vendor,
    totals: totalsOf(raw.totals),
    byAccount: (raw.byAccount ?? []).map((row) => ({accountId: row.accountId, generation: row.generation, totals: totalsOf(row.totals)})),
    byWorkspace: (raw.byWorkspace ?? []).map(groupOf),
    byDay: (raw.byDay ?? []).map(groupOf),
    byModel: (raw.byModel ?? []).map(groupOf),
    topSessions: (raw.topSessions ?? []).map(sessionOf),
    partialSplitTurns: raw.partialSplitTurns,
  };
}

export function agentUsageReport(vendor: string): Promise<UsageReport> {
  return callBridge(async () => reportOf(await AppService.AgentUsageReport(vendor)));
}
