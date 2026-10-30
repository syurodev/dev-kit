import { describe, expect, it } from "bun:test";
import type { LimitWindow, Usage } from "./types";
import { mergeSubagentUsage, mergeUsage } from "./usageLimits";

const now = 1_000_000_000;
const future = now / 1000 + 3600;
const past = now / 1000 - 60;
const five: LimitWindow = { label: "5-hour", usedPercent: 40, resetsAt: future, status: "ok" };
const week: LimitWindow = { label: "Weekly", usedPercent: 10, resetsAt: future + 86400, status: "ok" };
const ctx = (used: number, limits?: LimitWindow[]): Usage => ({ used, size: 100, amount: 0, ...(limits ? { limits } : {}) });

describe("mergeUsage", () => {
  it("is the incoming usage when there was none before", () => {
    expect(mergeUsage(null, ctx(2), now)).toEqual(ctx(2));
    expect(mergeUsage(undefined, ctx(2, [five]), now)).toEqual(ctx(2, [five]));
  });

  it("keeps the windows a later update does not carry, with the context from the newest", () => {
    expect(mergeUsage(ctx(1, [five]), ctx(2), now)).toEqual(ctx(2, [five]));
    expect(mergeUsage(ctx(1, [five]), { ...ctx(2), limits: [] }, now)).toEqual(ctx(2, [five]));
    expect(mergeUsage(ctx(1, [five]), { ...ctx(2), limits: null }, now)).toEqual(ctx(2, [five]));
  });

  it("replaces the window a new update names by label and keeps the others", () => {
    const newer = { ...five, usedPercent: 55 };
    expect(mergeUsage(ctx(1, [five, week]), ctx(2, [newer]), now).limits).toEqual([newer, week]);
  });

  it("adds a window only the new update names", () => {
    expect(mergeUsage(ctx(1, [five]), ctx(2, [week]), now).limits).toEqual([five, week]);
  });

  it("drops a window whose reset has passed, with or without new windows", () => {
    const expired = { ...five, usedPercent: 99, resetsAt: past };
    expect(mergeUsage(ctx(1, [expired, week]), ctx(2), now).limits).toEqual([week]);
    expect(mergeUsage(ctx(1, [expired]), ctx(2, [week]), now).limits).toEqual([week]);
    expect(mergeUsage(ctx(1, [expired]), ctx(2), now).limits).toBeUndefined();
  });

  it("keeps a window that names no reset time", () => {
    const spark = { label: "Spark", usedPercent: 30 };
    expect(mergeUsage(ctx(1, [spark]), ctx(2), now).limits).toEqual([spark]);
  });

  it("keeps the rate-limit verdict while its window lasts, not past it, never a rejected one", () => {
    const warn = { status: "allowed_warning", limitType: "five_hour", resetsAt: future, utilization: 82 };
    expect(mergeUsage({ ...ctx(1), rateLimit: warn }, ctx(2), now).rateLimit).toEqual(warn);
    expect(mergeUsage({ ...ctx(1), rateLimit: { ...warn, resetsAt: past } }, ctx(2), now).rateLimit).toBeUndefined();
    expect(mergeUsage({ ...ctx(1), rateLimit: { ...warn, status: "rejected" } }, ctx(2), now).rateLimit).toBeUndefined();
    const fresh = { status: "allowed" };
    expect(mergeUsage({ ...ctx(1), rateLimit: warn }, { ...ctx(2), rateLimit: fresh }, now).rateLimit).toEqual(fresh);
  });

  it("keeps the percent a window had when the update names it without a reading, taking its status and reset", () => {
    const named = { label: "5-hour", usedPercent: 0, resetsAt: future + 60, status: "warning", noReading: true };
    const prev = { ...five, usedPercent: 73 };
    expect(mergeUsage(ctx(1, [prev]), ctx(2, [named]), now).limits).toEqual([{ label: "5-hour", usedPercent: 73, resetsAt: future + 60, status: "warning" }]);
    // Nothing before it for the label: as the vendor gave it.
    expect(mergeUsage(ctx(1, [week]), ctx(2, [named]), now).limits).toEqual([week, named]);
    // Without a reading over one without: still none.
    expect(mergeUsage(ctx(1, [{ ...named, status: "ok" }]), ctx(2, [named]), now).limits).toEqual([named]);
  });

  it("carries a window with no reset time only while the update brings no windows", () => {
    const plan = { label: "Plan", usedPercent: 30 };
    expect(mergeUsage(ctx(1, [plan]), ctx(2), now).limits).toEqual([plan]);
    expect(mergeUsage(ctx(1, [plan, five]), ctx(2, [week]), now).limits).toEqual([five, week]);
    const rate = { status: "allowed" };
    expect(mergeUsage({ ...ctx(1), rateLimit: rate }, ctx(2), now).rateLimit).toEqual(rate);
    expect(mergeUsage({ ...ctx(1), rateLimit: rate }, ctx(2, [week]), now).rateLimit).toBeUndefined();
  });

  it("takes tokens and cost from the newest update only, and does not touch its input", () => {
    const prev: Usage = { ...ctx(1, [five]), amount: 1, tokens: { session: { total: 9, input: 5, cached: 2, output: 4, reasoning: 1 }, lastRequest: { total: 3, input: 2, cached: 0, output: 1, reasoning: 0 } } };
    const incoming = { ...ctx(2), amount: 2 };
    const merged = mergeUsage(prev, incoming, now);
    expect(merged.tokens).toBeUndefined();
    expect(merged.amount).toBe(2);
    expect(incoming.limits).toBeUndefined();
    merged.limits![0].usedPercent = 99;
    expect(prev.limits![0].usedPercent).toBe(40);
  });
});

describe("mergeSubagentUsage", () => {
  const own: Usage = { used: 500, size: 1_000_000, amount: 1.5, currency: "USD", limits: [five] };
  const warn = { status: "allowed_warning", limitType: "five_hour", resetsAt: future };

  it("takes its plan windows and none of its context numbers", () => {
    const sub = { used: 3, size: 200_000, amount: 9, limits: [{ ...five, usedPercent: 60 }, week], rateLimit: warn };
    expect(mergeSubagentUsage(own, sub, now)).toEqual({ ...own, limits: [{ ...five, usedPercent: 60 }, week], rateLimit: warn });
  });

  it("leaves the usage alone when it carries no windows", () => {
    expect(mergeSubagentUsage(own, { used: 3, size: 200_000, amount: 0 }, now)).toBe(own);
    expect(mergeSubagentUsage(null, { used: 3, size: 200_000, amount: 0 }, now)).toBeNull();
  });

  it("holds the windows alone when there was no usage before", () => {
    expect(mergeSubagentUsage(null, { used: 3, size: 200_000, amount: 0, limits: [five] }, now)).toEqual({ used: 0, size: 0, amount: 0, limits: [five] });
  });
});
