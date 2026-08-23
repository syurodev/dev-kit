import { describe, expect, it } from "bun:test";
import { splitColumn, SPLIT_MIN_HEIGHT } from "./splitColumn";

const COL = 866; // content row at a 900px window

describe("splitColumn", () => {
  it("gives the whole column to whichever slot is alone", () => {
    expect(splitColumn({ columnHeight: COL, topOpen: true, bottomOpen: false, topHeight: 430 }))
      .toEqual({ top: COL, divider: 0, bottom: 0 });
    expect(splitColumn({ columnHeight: COL, topOpen: false, bottomOpen: true, topHeight: 430 }))
      .toEqual({ top: 0, divider: 0, bottom: COL });
  });

  it("splits at the stored height when both are open", () => {
    const s = splitColumn({ columnHeight: COL, topOpen: true, bottomOpen: true, topHeight: 430 });
    expect(s.top).toBe(430);
    expect(s.top + s.divider + s.bottom).toBe(COL);
  });

  it("never starves either slot below its floor", () => {
    const tall = splitColumn({ columnHeight: COL, topOpen: true, bottomOpen: true, topHeight: 5000 });
    expect(tall.bottom).toBeGreaterThanOrEqual(SPLIT_MIN_HEIGHT);
    const short = splitColumn({ columnHeight: COL, topOpen: true, bottomOpen: true, topHeight: 0 });
    expect(short.top).toBeGreaterThanOrEqual(SPLIT_MIN_HEIGHT);
  });

  it("clamps a stored height that no longer fits the window", () => {
    // The height persists across sessions; shrinking the window must not
    // leave the bottom slot with negative space.
    const s = splitColumn({ columnHeight: 500, topOpen: true, bottomOpen: true, topHeight: 430 });
    expect(s.top + s.divider + s.bottom).toBe(500);
    expect(s.bottom).toBeGreaterThanOrEqual(SPLIT_MIN_HEIGHT);
  });

  it("splits evenly rather than starving one slot when both floors cannot fit", () => {
    // 200px cannot hold two 180px floors. The claim is an EVEN split, so
    // assert that — "both are positive" passes even when one gets 16px.
    const s = splitColumn({ columnHeight: 200, topOpen: true, bottomOpen: true, topHeight: 430 });
    expect(Math.abs(s.top - s.bottom)).toBeLessThanOrEqual(1);
    expect(s.top + s.divider + s.bottom).toBe(200);
  });

  it("is empty when neither slot is open", () => {
    expect(splitColumn({ columnHeight: COL, topOpen: false, bottomOpen: false, topHeight: 430 }))
      .toEqual({ top: 0, divider: 0, bottom: 0 });
  });
});
