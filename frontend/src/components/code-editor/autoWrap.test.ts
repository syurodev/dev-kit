import { describe, expect, it } from "bun:test";
import { shouldWrap, UNWRAP_HYSTERESIS_COLUMNS, WRAP_BELOW_COLUMNS } from "./autoWrap";

describe("shouldWrap", () => {
  it("wraps a narrow editor and leaves a wide one scrolling", () => {
    expect(shouldWrap(40, false)).toBe(true);
    expect(shouldWrap(140, false)).toBe(false);
    expect(shouldWrap(WRAP_BELOW_COLUMNS, false)).toBe(false);
  });

  it("keeps wrapping until the editor is clearly past the threshold", () => {
    expect(shouldWrap(WRAP_BELOW_COLUMNS + 1, true)).toBe(true);
    expect(shouldWrap(WRAP_BELOW_COLUMNS + UNWRAP_HYSTERESIS_COLUMNS, true)).toBe(false);
  });

  it("changes nothing for an editor that has no width yet", () => {
    expect(shouldWrap(0, false)).toBe(false);
    expect(shouldWrap(0, true)).toBe(true);
    expect(shouldWrap(Number.NaN, true)).toBe(true);
  });
});
