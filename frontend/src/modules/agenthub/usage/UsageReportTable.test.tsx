import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { UsageGroup, UsageReport, UsageTotals } from "@/bridge/agentUsage";
import { QUOTA_NOTE } from "./usageModel";
import { UsageReportTable } from "./UsageReportTable";

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
  totals: totals({ turns: 512, total: 960_683_040, cacheRead: 932_811_339, cacheWrite: 24_759_563, output: 3_105_232 }),
  byAccount: [], byWorkspace: [
    group("w1", { turns: 427, total: 824_519_570, cacheRead: 800_000_000 }),
    group("", { turns: 3, total: 3_583_422 }),
    group("gone", { turns: 1, total: 791_005 }),
  ],
  byDay: [group("2026-10-25", { turns: 1, total: 1_000 }), group("2026-10-26", { turns: 16, total: 5_000_000 })],
  byModel: [group("claude-opus-5-5", { turns: 326, total: 637_349_761,
    known: { freshInput: 0, cacheRead: 0, cacheWrite: 0, output: 0, total: 326 } })],
  topSessions: [{ sessionId: "11111111-2222-3333-4444-555555555555", workspaceId: "w1", totals: totals({ turns: 9, total: 90_000_000 }) }],
  partialSplitTurns: 0,
  ...over,
});

const render = (over: { report?: UsageReport | undefined; loadError?: string } = {}) =>
  renderToStaticMarkup(
    <UsageReportTable
      vendorName="Claude"
      report={"report" in over ? over.report : report()}
      loadError={over.loadError ?? ""}
      workspaceNames={{ w1: "fsap-security" }}
    />,
  );

describe("UsageReportTable", () => {
  test("always says tokens are not a share of the subscription limit", () => {
    expect(render()).toContain(QUOTA_NOTE);
    expect(render({ report: undefined })).toContain(QUOTA_NOTE);
  });

  test("gives the headline: tokens, turns and the cache read share", () => {
    const html = render();
    expect(html).toContain("960.7M tokens over 512 turns");
    expect(html).toContain("97.1% cache reads");
  });

  test("renders fresh input and does not turn unknown totals into a zero headline", () => {
    const t = totals({ turns: 1, freshInput: 5, output: 7,
      known: { freshInput: 1, output: 1, total: 0, cacheRead: 0, cacheWrite: 0 } });
    const html = render({ report: report({ totals: t, byAccount: [], byWorkspace: [group("w1", t)], byModel: [] }) });
    expect(html).toContain("Fresh input");
    expect(html).toContain(">5<");
    expect(html).toContain("— tokens over 1 turns");
    expect(html).not.toContain("0 tokens over");
    expect(html).toContain("— cache reads");
    const partial = render({ report: report({ totals: { ...t, turns: 2, total: 100,
      known: { ...t.known, total: 1 } } }) });
    expect(partial).toContain("100 (partial) tokens over 2 turns");
  });

  test("names each workspace by the window's own list", () => {
    const html = render();
    expect(html).toContain("fsap-security");
    expect(html).toContain("No workspace");
    expect(html).toContain("Removed workspace");
    expect(html).not.toContain(">w1<");
  });

  test("lists days newest first and models by name", () => {
    const html = render();
    expect(html.indexOf("2026-10-26")).toBeLessThan(html.indexOf("2026-10-25"));
    expect(html).toContain("claude-opus-5-5");
  });

  test("shows a session by a short id and its workspace, never a title", () => {
    const html = render();
    expect(html).toContain("11111111");
    expect(html).not.toContain("11111111-2222-3333-4444-555555555555");
  });

  test("says when turns have no data and when a split differs", () => {
    const html = render({ report: report({ totals: totals({ turns: 512, turnsWithoutData: 4, total: 10, cacheRead: 5 }), partialSplitTurns: 95 }) });
    expect(html).toContain("4 of 512 turns have no token data");
    expect(html).toContain("95 turns");
  });

  test("says there is no data yet when no turn has any", () => {
    const html = render({ report: report({ totals: totals({ turns: 3, turnsWithoutData: 3 }), byAccount: [], byWorkspace: [], byDay: [], byModel: [], topSessions: [] }) });
    expect(html).toContain("No token data yet for Claude.");
    expect(html).not.toContain("tokens over");
  });

  test("shows a skeleton while loading and the error when the load failed", () => {
    expect(render({ report: undefined })).toContain("animate-pulse");
    const failed = render({ report: undefined, loadError: "Could not read usage." });
    expect(failed).toContain("Could not read usage.");
    expect(failed).toContain('role="alert"');
  });
});
