import { describe, expect, test } from "bun:test";
import {
  DEFAULT_COLUMN_WIDTH_PX,
  MAX_COLUMN_WIDTH_PX,
  MIN_COLUMN_WIDTH_PX,
  dragWidths,
  fitWidths,
  keyboardResize,
  tableColumnWidth,
  measureColumns,
  nextWidths,
  resizeColumn,
} from "./columns";
import * as grid from "@/modules/database/grid/gridColumns";

describe("shared column widths", () => {
  // The database grid keeps importing from gridColumns; it must be handed the
  // same functions, not copies that can drift.
  test("the result grid reads the same rules", () => {
    expect(grid.resizeColumn).toBe(resizeColumn);
    expect(grid.measureColumns).toBe(measureColumns);
    expect(grid.DEFAULT_COLUMN_WIDTH_PX).toBe(DEFAULT_COLUMN_WIDTH_PX);
    expect(grid.MIN_COLUMN_WIDTH_PX).toBe(MIN_COLUMN_WIDTH_PX);
  });

  test("an unsized column gets the default", () => {
    expect(resizeColumn([], 2, 0)[2]).toBe(DEFAULT_COLUMN_WIDTH_PX);
  });

  test("a column cannot be dragged narrower than its minimum", () => {
    expect(resizeColumn([200, 200], 0, -1000)[0]).toBe(MIN_COLUMN_WIDTH_PX);
  });

  test("measured widths stay between the minimum and the cap", () => {
    const long = "x".repeat(4000);
    expect(measureColumns([long], [[long]])[0]).toBeLessThanOrEqual(320);
    expect(measureColumns(["a"], [["b"]])[0]).toBeGreaterThanOrEqual(MIN_COLUMN_WIDTH_PX);
    const [id, at] = measureColumns(["id", "at"], [["5", "2026-03-03 09:21:31"]]);
    expect(id).toBeLessThan(at);
  });
});

describe("nextWidths", () => {
  test("moves one column by the drag and leaves the rest", () => {
    expect(nextWidths([100, 200, 300], 1, 24)).toEqual([100, 224, 300]);
  });

  test("never goes below the minimum", () => {
    expect(nextWidths([100], 0, -500)).toEqual([MIN_COLUMN_WIDTH_PX]);
  });
});

describe("keyboardResize", () => {
  test("arrows move a column by 16px", () => {
    expect(keyboardResize([200, 200], 0, "ArrowRight", false)).toEqual([216, 200]);
    expect(keyboardResize([200, 200], 1, "ArrowLeft", false)).toEqual([200, 184]);
  });

  test("Shift moves four times as far", () => {
    expect(keyboardResize([200], 0, "ArrowRight", true)).toEqual([264]);
    expect(keyboardResize([200], 0, "ArrowLeft", true)).toEqual([136]);
  });

  test("other keys are not a resize", () => {
    expect(keyboardResize([200], 0, "ArrowUp", false)).toBeNull();
    expect(keyboardResize([200], 0, "Enter", false)).toBeNull();
  });
});

describe("measuring for the shared table", () => {
  // The grid's header carries a type glyph and a sort slot; a DataTable
  // header carries neither, so it is not charged for them.
  test("header chrome is the grid's alone", () => {
    const [grid] = measureColumns(["created_at_long_name"], [["1"]]);
    const [plain] = measureColumns(["created_at_long_name"], [["1"]], { headerChromePx: 0 });
    expect(plain).toBe(grid - 40);
    expect(tableColumnWidth("created_at_long_name", ["1"], { mono: true })).toBe(plain);
  });

  // Prose cells are body text with inline-code chips, wider per character
  // than the code face the grid measures against.
  test("a sans column is measured wider than the same text in mono", () => {
    const value = "root (src/) and more";
    expect(tableColumnWidth("n", [value], {})).toBeGreaterThan(
      tableColumnWidth("n", [value], { mono: true }),
    );
  });

  // A column that never wraps must not be cut at the grid's 320 cap.
  test("a nowrap column may grow past the wrap cap, within a bound", () => {
    const name = "x".repeat(60);
    expect(tableColumnWidth("n", [name], {})).toBe(320);
    const nowrap = tableColumnWidth("n", [name], { nowrap: true });
    expect(nowrap).toBeGreaterThan(320);
    expect(tableColumnWidth("n", ["x".repeat(4000)], { nowrap: true })).toBeLessThanOrEqual(640);
  });
});

describe("dragWidths", () => {
  // From the drag's start, not by increments on a clamped width: a drag past
  // the minimum and back puts the edge back under the pointer.
  test("is measured from where the drag began", () => {
    expect(dragWidths([100, 50], 0, 10, -490)).toEqual([MIN_COLUMN_WIDTH_PX, 50]);
    expect(dragWidths([100, 50], 0, 10, 30)).toEqual([120, 50]);
  });

  test("stops at the maximum", () => {
    expect(dragWidths([100], 0, 0, 100_000)).toEqual([MAX_COLUMN_WIDTH_PX]);
    expect(nextWidths([100], 0, 100_000)).toEqual([MAX_COLUMN_WIDTH_PX]);
  });
});

describe("fitWidths", () => {
  const floors = (n: number) => Array.from({ length: n }, () => MIN_COLUMN_WIDTH_PX);

  test("leaves widths that already fit alone, and never grows them", () => {
    expect(fitWidths([100, 200], 400, { min: floors(2), wraps: [true, true] })).toEqual([100, 200]);
    expect(fitWidths([100, 200], 300, { min: floors(2), wraps: [true, true] })).toEqual([100, 200]);
  });

  // The Kafka peek: offset, key, a 640px value and a time, in an 872px card.
  test("shrinks the widest column so the last one is not cut at the edge", () => {
    const fitted = fitWidths([90, 120, 640, 75], 872, {
      min: [70, 50, 60, 50],
      wraps: [false, false, false, false],
    });
    expect(fitted.reduce((a, b) => a + b, 0)).toBeLessThanOrEqual(872);
    expect(fitted[0]).toBe(90);
    expect(fitted[1]).toBe(120);
    expect(fitted[3]).toBe(75);
    expect(fitted[2]).toBeLessThan(640);
  });

  test("takes width from wrapping columns before one that can only be cut", () => {
    const fitted = fitWidths([300, 600], 800, { min: floors(2), wraps: [true, false] });
    expect(fitted).toEqual([200, 600]);
  });

  test("evens out the widest columns rather than emptying one", () => {
    const fitted = fitWidths([400, 300, 100], 600, { min: floors(3), wraps: [true, true, true] });
    expect(fitted).toEqual([250, 250, 100]);
  });

  test("a cut column gives up width once the wrapping ones are at their floor", () => {
    const fitted = fitWidths([150, 600], 500, { min: [100, 64], wraps: [true, false] });
    expect(fitted).toEqual([100, 400]);
  });

  test("never goes below a column's floor; past that the table scrolls", () => {
    const fitted = fitWidths([200, 300], 100, { min: [80, 90], wraps: [true, false] });
    expect(fitted).toEqual([80, 90]);
  });

  test("a floor above the starting width does not widen the column", () => {
    expect(fitWidths([40, 500], 300, { min: [64, 64], wraps: [true, true] })).toEqual([40, 260]);
  });

  test("an unknown or empty room changes nothing", () => {
    expect(fitWidths([100, 200], 0, { min: floors(2), wraps: [true, true] })).toEqual([100, 200]);
    expect(fitWidths([100, 200], Number.NaN, { min: floors(2), wraps: [true, true] })).toEqual([100, 200]);
  });
});
