import { describe, expect, test } from "bun:test";
import { DEFAULT_COLUMN_WIDTH_PX, orderedColumns, resizeColumn, reorderColumn } from "./gridColumns";

describe("column order", () => {
  // Column state is keyed by name, but a result can repeat a name —
  // SELECT a.id, b.id — so everything here works on positions.
  test("two columns sharing a name stay distinct", () => {
    const ordered = orderedColumns(["id", "id"], []);
    expect(ordered).toEqual([0, 1]);
  });

  test("a saved order survives a query returning the same columns", () => {
    expect(orderedColumns(["a", "b", "c"], [2, 0, 1])).toEqual([2, 0, 1]);
  });

  // A different query returns different columns. A stale order would show
  // them in an order the user never chose, or drop some entirely.
  test("a saved order is dropped when the column count changes", () => {
    expect(orderedColumns(["a", "b"], [2, 0, 1])).toEqual([0, 1]);
  });

  test("moving a column left shifts the rest right", () => {
    expect(reorderColumn([0, 1, 2, 3], 3, 1)).toEqual([0, 3, 1, 2]);
  });

  test("moving a column onto itself changes nothing", () => {
    expect(reorderColumn([0, 1, 2], 1, 1)).toEqual([0, 1, 2]);
  });
});

describe("column width", () => {
  test("an unsized column gets the default", () => {
    expect(resizeColumn([], 2, 0)[2]).toBe(DEFAULT_COLUMN_WIDTH_PX);
  });

  // Dragging left past the minimum must stop at it, not invert the column.
  test("a column cannot be dragged narrower than its minimum", () => {
    const widths = resizeColumn([200, 200], 0, -1000);
    expect(widths[0]).toBeGreaterThan(0);
    expect(widths[0]).toBeLessThan(200);
  });

  test("resizing one column leaves the others alone", () => {
    const widths = resizeColumn([200, 200, 200], 1, 40);
    expect(widths[0]).toBe(200);
    expect(widths[1]).toBe(240);
    expect(widths[2]).toBe(200);
  });
});
