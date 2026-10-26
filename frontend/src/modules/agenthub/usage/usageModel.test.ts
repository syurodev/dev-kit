import { describe, expect, test } from "bun:test";
import type { UsageGroup, UsageReport, UsageTotals } from "@/bridge/agentUsage";
import {
  QUOTA_NOTE,
  cacheReadShare,
  completeTotal,
  dataNotes,
  formatMetric,
  formatTokens,
  recentDays,
  rowsOf,
  sessionRowsOf,
  workspaceName,
} from "./usageModel";

const totals = (over: Partial<UsageTotals> = {}): UsageTotals => ({
  turns: 0,
  turnsWithoutData: 0,
  freshInput: 0,
  cacheRead: 0,
  cacheWrite: 0,
  output: 0,
  total: 0,
  known: {
    freshInput: over.turns ?? 0, cacheRead: over.turns ?? 0,
    cacheWrite: over.turns ?? 0, output: over.turns ?? 0, total: over.turns ?? 0,
  },
  ...over,
});
const group = (key: string, over: Partial<UsageTotals> = {}): UsageGroup => ({ key, totals: totals(over) });
const report = (over: Partial<UsageReport> = {}): UsageReport => ({
  vendor: "claude",
  totals: totals(),
  byAccount: [], byWorkspace: [],
  byDay: [],
  byModel: [],
  topSessions: [],
  partialSplitTurns: 0,
  ...over,
});

describe("formatTokens", () => {
  test("scales to K, M and B with one or two decimals", () => {
    expect(formatTokens(0)).toBe("0");
    expect(formatTokens(999)).toBe("999");
    expect(formatTokens(1_500)).toBe("1.5K");
    expect(formatTokens(960_683_040)).toBe("960.7M");
    expect(formatTokens(2_340_000_000)).toBe("2.34B");
  });
});

describe("cacheReadShare", () => {
  test("is the share of cache reads in the total, one decimal", () => {
    expect(cacheReadShare(totals({ turns: 1, total: 960_683_040, cacheRead: 932_811_339 }))).toBe("97.1%");
  });
  test("is a dash when there is nothing to divide", () => {
    expect(cacheReadShare(totals())).toBe("—");
  });
});

describe("metric coverage", () => {
  const known = { freshInput: 2, cacheRead: 1, cacheWrite: 0, output: 2, total: 1 };
  test("distinguishes missing metrics, genuine zero and partial sums", () => {
    const t = totals({ turns: 2, freshInput: 5, total: 0, known });
    expect(formatMetric(t, "cacheWrite")).toBe("—");
    expect(formatMetric(t, "total")).toBe("0 (partial)");
    expect(formatMetric(t, "freshInput")).toBe("5");
    expect(formatMetric(totals({ turns: 1 }), "total")).toBe("0");
    expect(completeTotal(t)).toBeNull();
  });
  test("suppresses ratios with incomplete numerator or denominator coverage", () => {
    const t = totals({ turns: 2, total: 100, cacheRead: 50, known });
    expect(cacheReadShare(t)).toBe("—");
    expect(cacheReadShare({ ...t, known: { ...known, total: 2 } })).toBe("—");
    expect(rowsOf([{ key: "w", totals: t }], (key) => key, 100)[0].share).toBe("—");
    expect(rowsOf([group("w", { turns: 1, total: 100 })], (key) => key, null)[0].share).toBe("—");
  });
  test("model rows report fresh input, caches and output as unknown", () => {
    const row = rowsOf([group("opus", { turns: 1, total: 100,
      known: { freshInput: 0, cacheRead: 0, cacheWrite: 0, output: 0, total: 1 } })], (key) => key, 100)[0];
    expect(row).toMatchObject({ total: "100", freshInput: "—", cacheRead: "—", cacheWrite: "—", output: "—" });
  });
});

describe("workspaceName", () => {
  test("names a known workspace, no workspace, and a removed one", () => {
    const names = { w1: "fsap-security" };
    expect(workspaceName("w1", names)).toBe("fsap-security");
    expect(workspaceName("", names)).toBe("No workspace");
    expect(workspaceName("gone", names)).toBe("Removed workspace");
  });
});

describe("rowsOf", () => {
  test("keeps the order given and adds each row's share of the grand total", () => {
    const rows = rowsOf(
      [group("a", { turns: 3, total: 800, cacheRead: 700, cacheWrite: 50, output: 10 }), group("b", { turns: 1, total: 200 })],
      (key) => key.toUpperCase(),
      1000,
    );
    expect(rows.map((r) => r.name)).toEqual(["A", "B"]);
    expect(rows[0]).toMatchObject({ key: "a", turns: 3, total: "800", cacheRead: "700", cacheWrite: "50", output: "10", share: "80.0%" });
    expect(rows[1].share).toBe("20.0%");
  });
  test("has a dash for the share when the grand total is 0", () => {
    expect(rowsOf([group("a")], (k) => k, 0)[0].share).toBe("—");
  });
});

describe("sessionRowsOf", () => {
  test("names a session by a short id and its workspace", () => {
    const rows = sessionRowsOf(
      [{ sessionId: "11111111-2222-3333-4444-555555555555", workspaceId: "w1", totals: totals({ turns: 9, total: 250 }) }],
      { w1: "fsap-security" },
      1000,
    );
    expect(rows[0].name).toBe("11111111 · fsap-security");
    expect(rows[0]).toMatchObject({ turns: 9, total: "250", share: "25.0%" });
  });
});

describe("recentDays", () => {
  test("lists the newest days first and no more than asked", () => {
    const days = [group("2026-10-24"), group("2026-10-25"), group("2026-10-26")];
    expect(recentDays(days, 2).map((g) => g.key)).toEqual(["2026-10-26", "2026-10-25"]);
  });
});

describe("dataNotes", () => {
  test("says nothing when every turn has data and every split matches", () => {
    expect(dataNotes(report({ totals: totals({ turns: 5 }) }))).toEqual([]);
  });
  test("says how many turns have no data and how many splits differ", () => {
    const notes = dataNotes(report({ totals: totals({ turns: 512, turnsWithoutData: 4 }), partialSplitTurns: 95 }));
    expect(notes).toHaveLength(2);
    expect(notes[0]).toContain("4 of 512 turns have no token data");
    expect(notes[1]).toContain("95 turns");
    expect(notes[1]).toContain("incomplete or differs");
    expect(notes[1]).not.toContain("larger than");
    expect(notes[1]).not.toContain("helper models");
  });
});

describe("QUOTA_NOTE", () => {
  test("says tokens are not a share of the subscription limit", () => {
    expect(QUOTA_NOTE).toContain("not a share of your subscription limit");
    expect(QUOTA_NOTE).toContain("not known");
  });
});
