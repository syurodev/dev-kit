import { describe, expect, test } from "bun:test";
import { beforeIdAfterMove, overflowEdges, revealScrollLeft, scrollStep } from "./tabStripModel";

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
