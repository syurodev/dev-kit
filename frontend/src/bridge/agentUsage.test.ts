import { describe, expect, test } from "bun:test";
import type { AgentUsageReportInfo, AgentUsageTotalsInfo } from "../../bindings/devkit/internal/bridge/models";
import { reportOf } from "./agentUsage";

const totals = (over: Partial<AgentUsageTotalsInfo> = {}): AgentUsageTotalsInfo => ({
  turns: 2,
  turnsWithoutData: 1,
  freshInput: 3,
  cacheRead: 4,
  cacheWrite: 5,
  output: 6,
  total: 18,
  known: { freshInput: 1, cacheRead: 1, cacheWrite: 1, output: 1, total: 1 },
  ...over,
});

describe("reportOf", () => {
  test("keeps what Go sent", () => {
    const raw: AgentUsageReportInfo = {
      vendor: "claude",
      totals: totals(),
      byAccount: [], byWorkspace: [{ key: "w1", totals: totals({ total: 10 }) }],
      byDay: [{ key: "2026-10-26", totals: totals() }],
      byModel: [{ key: "opus", totals: totals({ turns: 1 }) }],
      topSessions: [{ sessionId: "s1", workspaceId: "w1", totals: totals() }],
      partialSplitTurns: 7,
    };
    const report = reportOf(raw);
    expect(report.vendor).toBe("claude");
    expect(report.totals).toEqual({ turns: 2, turnsWithoutData: 1, freshInput: 3, cacheRead: 4, cacheWrite: 5, output: 6, total: 18,
      known: { freshInput: 1, cacheRead: 1, cacheWrite: 1, output: 1, total: 1 } });
    expect(report.byWorkspace[0]).toEqual({ key: "w1", totals: expect.objectContaining({ total: 10 }) });
    expect(report.topSessions[0]).toMatchObject({ sessionId: "s1", workspaceId: "w1" });
    expect(report.partialSplitTurns).toBe(7);
  });

  test("turns the bindings' null lists into empty ones", () => {
    const raw: AgentUsageReportInfo = {
      vendor: "codex",
      totals: totals({ turns: 0, turnsWithoutData: 0 }),
      byAccount: [], byWorkspace: null,
      byDay: null,
      byModel: null,
      topSessions: null,
      partialSplitTurns: 0,
    };
    const report = reportOf(raw);
    expect(report.byWorkspace).toEqual([]);
    expect(report.byDay).toEqual([]);
    expect(report.byModel).toEqual([]);
    expect(report.topSessions).toEqual([]);
  });
});
