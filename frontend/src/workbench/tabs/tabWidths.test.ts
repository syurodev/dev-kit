import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  ACTIVE_MIN_WIDTH_PX,
  CHROME_PX,
  INACTIVE_MIN_WIDTH_PX,
  MAX_TAB_WIDTH_PX,
  TAB_GAP_PX,
  computeTabWidths,
  pillWidth,
} from "./tabWidths";

const ids = (n: number) => Array.from({ length: n }, (_, i) => `t${i}`);

describe("tab widths", () => {
  // The report this came from: a 889px strip with three tabs capped them at
  // 200px each and left 221px of the row empty next to a truncated label.
  test("three tabs in 889px use the room instead of leaving it empty", () => {
    const widths = computeTabWidths({
      tabIds: ids(3),
      activeTabId: "t0",
      containerWidth: 889,
    });
    const share = widths.get("t0")!;
    expect(share).toBeGreaterThan(260);

    // What the row actually spends: the tabs, their gaps, and the chrome —
    // p-2 either side, the + button, and the gap on both sides of it. It must
    // come to exactly the measured width, or the strip either overflows or
    // leaves a hole like the one this test came from.
    const spent = share * 3 + 2 * TAB_GAP_PX + CHROME_PX;
    expect(Math.round(spent)).toBe(889);
  });

  test("a lone tab is capped rather than stretched across the row", () => {
    const widths = computeTabWidths({
      tabIds: ["only"],
      activeTabId: "only",
      containerWidth: 1600,
    });
    expect(widths.get("only")).toBe(MAX_TAB_WIDTH_PX);
  });

  // The floor must never exceed the cap, or a roomy strip would render the
  // active tab narrower than a cramped one.
  test("the active floor never exceeds the cap", () => {
    expect(ACTIVE_MIN_WIDTH_PX).toBeLessThanOrEqual(MAX_TAB_WIDTH_PX);
  });

  test("once cramped, the active tab holds its floor and the rest give way", () => {
    const widths = computeTabWidths({
      tabIds: ids(8),
      activeTabId: "t3",
      containerWidth: 889,
    });
    expect(widths.get("t3")).toBe(ACTIVE_MIN_WIDTH_PX);
    expect(widths.get("t0")!).toBeLessThan(ACTIVE_MIN_WIDTH_PX);
    expect(widths.get("t0")!).toBeGreaterThanOrEqual(INACTIVE_MIN_WIDTH_PX);
  });

  test("past the inactive floor the strip scrolls rather than shrinking further", () => {
    const widths = computeTabWidths({
      tabIds: ids(40),
      activeTabId: "t0",
      containerWidth: 889,
    });
    expect(widths.get("t1")).toBe(INACTIVE_MIN_WIDTH_PX);
  });

  // Crossing from roomy to cramped must not jump: at the boundary both
  // branches have to agree, or one tab added would resize every tab twice.
  test("the roomy and cramped branches meet without a jump", () => {
    // Chosen so equalShare lands exactly on the active floor.
    const n = 4;
    const container = ACTIVE_MIN_WIDTH_PX * n + (n - 1) * TAB_GAP_PX + CHROME_PX;
    const widths = computeTabWidths({
      tabIds: ids(n),
      activeTabId: "t0",
      containerWidth: container,
    });
    for (const id of ids(n)) {
      expect(widths.get(id)).toBe(ACTIVE_MIN_WIDTH_PX);
    }
  });

  // The bug this pair came from: the model counted 4px between tabs while the
  // rows drew 6 inside a group and 8 between groups. Both errors landed in the
  // same place — the pill was sized from the model's sum and then had to hold
  // tabs laid out with the DOM's, so it clipped the last tab's right edge by
  // the difference. The fix is one number; these say the number is used.
  test("the pill holds exactly the tabs and the gaps between them", () => {
    expect(pillWidth([100, 100, 100])).toBe(300 + 2 * TAB_GAP_PX);
    expect(pillWidth([100])).toBe(100);
    expect(pillWidth([])).toBe(0);
  });

  test("the pill and the chrome fill the strip they were measured from", () => {
    const containerWidth = 889;
    const widths = computeTabWidths({
      tabIds: ids(3),
      activeTabId: "t0",
      containerWidth,
    });
    const pill = pillWidth(ids(3).map((id) => widths.get(id)))!;
    expect(Math.round(pill + CHROME_PX)).toBe(containerWidth);
  });

  test("a pill with an unmeasured tab has no width of its own", () => {
    expect(pillWidth([100, undefined])).toBeUndefined();
  });

  test("before the first measurement every tab is auto", () => {
    const widths = computeTabWidths({
      tabIds: ids(2),
      activeTabId: "t0",
      containerWidth: undefined,
    });
    expect(widths.get("t0")).toBeUndefined();
  });
});

// The cap rose from 200 to 320 and a `max-w-[200px]` on the tab element stayed
// behind, so the strip sized a 320px slot around a tab CSS would not let past
// 200 — a lone tab truncated its own title while 120px of empty pill pushed
// the new-tab button off on its own. One cap, in the arithmetic, is the fix.
test("the tab element carries no width cap of its own", () => {
  const source = readFileSync(join(import.meta.dir, "TabStrip.tsx"), "utf8");
  const clamps = source.match(/max-w-\[\d+px\]/g) ?? [];
  expect(clamps, `TabStrip caps width in CSS as well as in computeTabWidths: ${clamps}`)
    .toHaveLength(0);
});
