import { describe, expect, test } from "bun:test";
import {
  DEFAULT_COLUMN_WIDTH_PX,
  MIN_COLUMN_WIDTH_PX,
  keyboardResize,
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
