import { describe, expect, test } from "bun:test";
import { IDLE_MAX_MINUTES, IDLE_MIN_MINUTES, minutesText, parseMinutes } from "./idleRelease";

describe("parseMinutes", () => {
  test("takes whole numbers inside the range", () => {
    expect(parseMinutes("10")).toEqual({ ok: true, value: 10 });
    expect(parseMinutes(" 30 ")).toEqual({ ok: true, value: 30 });
    expect(parseMinutes(String(IDLE_MIN_MINUTES))).toEqual({ ok: true, value: IDLE_MIN_MINUTES });
    expect(parseMinutes(String(IDLE_MAX_MINUTES))).toEqual({ ok: true, value: IDLE_MAX_MINUTES });
  });

  test("refuses blanks, decimals, negatives and anything outside the range", () => {
    for (const bad of ["", "  ", "1.5", "-5", "abc", "1", "0", String(IDLE_MAX_MINUTES + 1)]) {
      expect(parseMinutes(bad).ok).toBe(false);
    }
  });
});

test("minutesText reads a stored value the way a person says it", () => {
  expect(minutesText(10)).toBe("10 minutes");
  expect(minutesText(1)).toBe("1 minute");
  expect(minutesText(60)).toBe("1 hour");
  expect(minutesText(90)).toBe("1 hour 30 minutes");
  expect(minutesText(1440)).toBe("24 hours");
});
