import { describe, expect, test } from "bun:test";
import { numericColumns } from "./numericColumns";

const columns = (rows: (string | null)[][], count = rows[0]?.length ?? 0) => numericColumns(rows, count);

/**
 * Right alignment is for finding the odd value: digits line up by place, so
 * the four-digit id in a column of three-digit ones is a different length and
 * the eye finds it without reading. That only works if the column really is
 * numbers, all of them.
 */
describe("which columns hold numbers", () => {
  test("a column of integers, and one of words", () => {
    expect(columns([["1", "credential"], ["2", "oauth"], ["30", "credential"]])).toEqual([
      true,
      false,
    ]);
  });

  test("the shapes a database actually sends", () => {
    expect(columns([["-1"], ["0.5"], [".25"], ["+3"], ["1e6"], ["1.2E-3"]])).toEqual([true]);
  });

  // A column right-aligned except for three rows is worse than one that never
  // claimed to be.
  test("one value that is not a number puts the column back on the left", () => {
    expect(columns([["1"], ["2"], ["n/a"], ["4"]])).toEqual([false]);
  });

  // Empty is NULL in this grid. A column of numbers with holes is still a
  // column of numbers.
  test("nulls do not disqualify a column", () => {
    expect(columns([["1"], [null], ["3"]])).toEqual([true]);
  });

  test("a column of nothing but nulls is not a column of numbers", () => {
    expect(columns([[null], [null], [null]])).toEqual([false]);
  });

  /*
   * Number() would take all of these. An id column that turns out to hold
   * "Infinity" is not a thing to align by place value, and "0x1f" is a
   * string that happens to be parseable.
   */
  test.each([["0x1f"], ["Infinity"], ["NaN"], [" "], ["1,000"], ["1 2"], ["١٢٣"]])(
    "%s is not a number",
    (value) => {
      expect(columns([[value]])).toEqual([false]);
    },
  );

  test("an empty result asks nothing of anyone", () => {
    expect(numericColumns([], 3)).toEqual([false, false, false]);
  });

  // Ten thousand rows is not ten thousand parses.
  test("it decides from a sample, not from the whole result", () => {
    const rows = Array.from({ length: 5000 }, () => ["1"]);
    rows[4000] = ["nope"];
    expect(columns(rows)).toEqual([true]);
  });
});
