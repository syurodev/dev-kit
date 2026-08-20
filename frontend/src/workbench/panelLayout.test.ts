import { expect, test } from "bun:test";
import { maxPanelWidth, CONTENT_MIN_WIDTH, GAP_WIDTH } from "./panelLayout";

const SIDEBAR_MIN = 240;
const SIDEBAR_MAX = 480;

test("a closed neighbour frees its whole width", () => {
  // Regression: the first implementation subtracted the split panel's
  // remembered width (420) even while it was closed, capping the sidebar at
  // 292px on a 1200px window instead of its real 480px ceiling.
  const max = maxPanelWidth({
    windowWidth: 1200,
    otherEffectiveWidth: 0,
    min: SIDEBAR_MIN,
    hardMax: SIDEBAR_MAX,
  });
  expect(max).toBe(SIDEBAR_MAX);
});

test("an open neighbour shrinks this panel's ceiling", () => {
  const max = maxPanelWidth({
    windowWidth: 1200,
    otherEffectiveWidth: 420,
    min: SIDEBAR_MIN,
    hardMax: SIDEBAR_MAX,
  });
  expect(max).toBe(1200 - 420 - CONTENT_MIN_WIDTH - GAP_WIDTH * 2);
  expect(max).toBeLessThan(SIDEBAR_MAX);
});

test("never returns below the panel's own minimum", () => {
  // Regression: at the 1080px window floor with the split panel open wide,
  // the raw arithmetic goes under 240, and a max below min would drag the
  // panel down through its own minimum on the next pointer move.
  const max = maxPanelWidth({
    windowWidth: 1080,
    otherEffectiveWidth: 720,
    min: SIDEBAR_MIN,
    hardMax: SIDEBAR_MAX,
  });
  expect(max).toBe(SIDEBAR_MIN);
});

test("the content floor is preserved when both panels sit at their max", () => {
  // Symmetric check: whatever one panel is allowed, the centre keeps its floor.
  const windowWidth = 1600;
  const sidebar = maxPanelWidth({
    windowWidth,
    otherEffectiveWidth: 0,
    min: SIDEBAR_MIN,
    hardMax: SIDEBAR_MAX,
  });
  const splitPanel = maxPanelWidth({
    windowWidth,
    otherEffectiveWidth: sidebar,
    min: 320,
    hardMax: 720,
  });
  expect(windowWidth - sidebar - splitPanel - GAP_WIDTH * 2).toBeGreaterThanOrEqual(
    CONTENT_MIN_WIDTH,
  );
});
