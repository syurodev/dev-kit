import { expect, test } from "bun:test";
import {
  maxPanelWidth,
  maxBottomHeight,
  CONTENT_MIN_WIDTH,
  CONTENT_MIN_HEIGHT,
  TITLE_BAR_HEIGHT,
  GAP_WIDTH,
} from "./panelLayout";

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

test("the bottom slot may grow to its hard maximum on a tall window", () => {
  expect(maxBottomHeight({ windowHeight: 1400, min: 120, hardMax: 600 })).toBe(600);
  // Including at the 900px Figma frame height: the raw budget there is 622,
  // so the hard max is what actually caps the slot on the design target.
  expect(maxBottomHeight({ windowHeight: 900, min: 120, hardMax: 600 })).toBe(600);
});

test("the bottom slot's ceiling leaves the content row its floor", () => {
  // 800, not the 900 design height: at 900 the hard max binds first and this
  // assertion would pass without the content floor doing any work at all.
  const windowHeight = 800;
  const max = maxBottomHeight({ windowHeight, min: 120, hardMax: 600 });
  expect(max).toBeLessThan(600);
  expect(max).toBe(
    windowHeight - TITLE_BAR_HEIGHT - CONTENT_MIN_HEIGHT - GAP_WIDTH * 2,
  );
  const contentRow = windowHeight - TITLE_BAR_HEIGHT - max - GAP_WIDTH * 2;
  expect(contentRow).toBe(CONTENT_MIN_HEIGHT);
});

test("never returns below the slot's own minimum", () => {
  // Same failure mode maxPanelWidth guards: on a short window the raw
  // arithmetic goes under the floor, and a max below min would drag the slot
  // down through its own minimum on the next pointer move.
  const max = maxBottomHeight({ windowHeight: 300, min: 120, hardMax: 600 });
  expect(max).toBe(120);
});
