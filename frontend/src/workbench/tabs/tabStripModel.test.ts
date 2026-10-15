import { describe, expect, test } from "bun:test";
import { beforeIdAfterMove, overflowEdges, revealScrollLeft, revealScrollLeftWithin, scrollStep } from "./tabStripModel";

const ids = ["a", "b", "c", "d"];

describe("where a dragged tab lands", () => {
  test("dragged right, it goes in front of the tab after the slot", () => {
    // a over c → b, c, a, d
    expect(beforeIdAfterMove(ids, "a", "c")).toBe("d");
  });

  test("dragged left, it goes in front of the tab it was dropped on", () => {
    // d over a → d, a, b, c
    expect(beforeIdAfterMove(ids, "d", "a")).toBe("a");
  });

  test("dragged onto the last slot, it goes to the end", () => {
    // b over d → a, c, d, b
    expect(beforeIdAfterMove(ids, "b", "d")).toBeNull();
  });

  // Dropped where it started, or somewhere that is no tab: no store write.
  test("no move is no reorder", () => {
    expect(beforeIdAfterMove(ids, "b", "b")).toBeUndefined();
    expect(beforeIdAfterMove(ids, "b", "zz")).toBeUndefined();
  });
});

describe("revealing the active tab", () => {
  const view = { scrollLeft: 100, viewportWidth: 300 };

  test("a tab already in view does not move the strip", () => {
    expect(revealScrollLeft({ ...view, tabLeft: 150, tabWidth: 100 })).toBe(100);
  });

  test("a tab past the right edge scrolls just far enough to show its end", () => {
    expect(revealScrollLeft({ ...view, tabLeft: 350, tabWidth: 100 })).toBe(150);
  });

  test("a tab past the left edge lines up its start", () => {
    expect(revealScrollLeft({ ...view, tabLeft: 40, tabWidth: 100 })).toBe(40);
  });

  test("a tab wider than the strip lines up its start", () => {
    expect(revealScrollLeft({ ...view, tabLeft: 450, tabWidth: 400 })).toBe(450);
  });
});

test("overflowEdges says which sides hide tabs", () => {
  expect(overflowEdges({ scrollLeft: 0, scrollWidth: 300, clientWidth: 300 })).toEqual({ left: false, right: false });
  expect(overflowEdges({ scrollLeft: 0, scrollWidth: 900, clientWidth: 300 })).toEqual({ left: false, right: true });
  expect(overflowEdges({ scrollLeft: 600, scrollWidth: 900, clientWidth: 300 })).toEqual({ left: true, right: false });
  expect(overflowEdges({ scrollLeft: 200, scrollWidth: 900, clientWidth: 300 })).toEqual({ left: true, right: true });
  // Sub-pixel scroll positions at either end still read as the end.
  expect(overflowEdges({ scrollLeft: 0.4, scrollWidth: 900, clientWidth: 300 })).toEqual({ left: false, right: true });
  expect(overflowEdges({ scrollLeft: 599.6, scrollWidth: 900, clientWidth: 300 })).toEqual({ left: true, right: false });
});

test("scrollStep moves by most of a viewport and stops at the ends", () => {
  const view = { scrollLeft: 100, scrollWidth: 900, clientWidth: 300 };
  expect(scrollStep(view, "right")).toBe(340);
  expect(scrollStep(view, "left")).toBe(0);
  expect(scrollStep({ ...view, scrollLeft: 500 }, "right")).toBe(600);
});

describe("revealing a tab past the overflow fades", () => {
  // In sight: 124..376 — the 24px under each fade does not count.
  const view = { scrollLeft: 100, viewportWidth: 300 };
  const faded = { left: 24, right: 24 };

  test("a tab clear of both fades does not move the strip", () => {
    expect(revealScrollLeftWithin({ ...view, tabLeft: 150, tabWidth: 100 }, faded)).toBe(100);
  });

  test("a tab under the right fade comes out from under it", () => {
    expect(revealScrollLeftWithin({ ...view, tabLeft: 300, tabWidth: 100 }, faded)).toBe(124);
  });

  test("a tab under the left fade comes out from under it", () => {
    expect(revealScrollLeftWithin({ ...view, tabLeft: 110, tabWidth: 100 }, faded)).toBe(86);
  });

  test("a side with no fade is not inset", () => {
    expect(revealScrollLeftWithin({ ...view, tabLeft: 300, tabWidth: 100 }, { left: 24, right: 0 })).toBe(100);
  });

  test("with no fades it is revealScrollLeft", () => {
    for (const tabLeft of [40, 150, 350, 450]) {
      const tab = { ...view, tabLeft, tabWidth: 100 };
      expect(revealScrollLeftWithin(tab, { left: 0, right: 0 })).toBe(revealScrollLeft(tab));
    }
  });

  test("never scrolls before the start", () => {
    expect(revealScrollLeftWithin({ scrollLeft: 20, viewportWidth: 300, tabLeft: 10, tabWidth: 80 }, faded)).toBe(0);
  });
});
