import { describe, expect, it } from "bun:test";
import { pullSegments, pushSegments } from "./syncMeter";

const report = {
  pushAttempted: 128,
  pushSent: 120,
  pushRetried: 3,
  pushConflicts: 4,
  pushFailed: 1,
  pulled: 96,
  applied: 92,
};

describe("sync meters", () => {
  it("splits push by outcome, in Figma's order", () => {
    expect(pushSegments(report).map((s) => `${s.label}:${s.count}`)).toEqual([
      "sent:120",
      "retried:3",
      "conflicts:4",
      "dead-lettered:1",
    ]);
  });

  it("derives held-by-conflicts from pulled minus applied", () => {
    expect(pullSegments(report).map((s) => `${s.label}:${s.count}`)).toEqual([
      "applied:92",
      "held by conflicts:4",
    ]);
  });

  it("drops empty segments so the track never shows a zero sliver", () => {
    const clean = { ...report, pushRetried: 0, pushConflicts: 0, pushFailed: 0, applied: 96 };
    expect(pushSegments(clean).map((s) => s.key)).toEqual(["sent"]);
    expect(pullSegments(clean).map((s) => s.key)).toEqual(["applied"]);
  });
});
