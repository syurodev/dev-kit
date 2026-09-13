import { expect, test } from "bun:test";
import { TAB_WIDTH_LIMITS, clampTabWidths } from "./tabSettings";

test("tab widths stay inside the limits", () => {
  expect(clampTabWidths(10, 9999)).toEqual({ minWidth: TAB_WIDTH_LIMITS.min, maxWidth: TAB_WIDTH_LIMITS.max });
});

test("the max is never below the min", () => {
  expect(clampTabWidths(200, 120)).toEqual({ minWidth: 200, maxWidth: 200 });
});

test("widths are whole pixels", () => {
  expect(clampTabWidths(80.4, 320.6)).toEqual({ minWidth: 80, maxWidth: 321 });
});
