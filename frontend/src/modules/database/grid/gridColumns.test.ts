import { describe, expect, test } from "bun:test";
import {
  DEFAULT_COLUMN_WIDTH_PX,
  MIN_COLUMN_WIDTH_PX,
  frozenOffsets,
  pinColumn,
  measureColumns,
  orderedColumns,
  resizeColumn,
  reorderColumn,
} from "./gridColumns";

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

describe("measureColumns", () => {
  const columns = ["id", "created_at", "note"];
  const rows = [
    ["5", "2026-03-03 09:21:31", "ok"],
    ["1274", "2026-03-12 16:40:26", "ok"],
  ];

  // The bug this exists for: every column started at 180px, so `id` holding
  // "5" got exactly as much room as a timestamp and a wide table scrolled its
  // interesting columns off the right.
  test("a narrow column is narrower than a wide one", () => {
    const [id, createdAt] = measureColumns(columns, rows);
    expect(id).toBeLessThan(createdAt);
  });

  // A header is a value too: "created_at" is longer than nothing, and a column
  // of empty cells must still be able to show its own name.
  test("the header name is measured with the values", () => {
    const [, , note] = measureColumns(columns, [["5", "x", ""], ["6", "y", ""]]);
    expect(note).toBeGreaterThanOrEqual(MIN_COLUMN_WIDTH_PX);
    expect(note).toBeGreaterThan(measureColumns(["n"], [["x"]])[0]);
  });

  test("nothing is narrower than the drag minimum or wider than the cap", () => {
    const long = "x".repeat(4000);
    const [wide] = measureColumns([long], [[long]]);
    const [tiny] = measureColumns(["a"], [["b"]]);
    expect(wide).toBeLessThanOrEqual(320);
    expect(tiny).toBeGreaterThanOrEqual(MIN_COLUMN_WIDTH_PX);
  });

  // A page is up to 5,000 rows and the columns are sized once. Row 4,000 being
  // long is not a reason to make every screen of the table narrower.
  test("only the first rows are sampled", () => {
    const rows = Array.from({ length: 200 }, (_, i) => [i === 199 ? "x".repeat(300) : "5"]);
    expect(measureColumns(["id"], rows)).toEqual(measureColumns(["id"], [["5"]]));
  });
});

describe("frozenOffsets", () => {
  const widthOf = (index: number) => [80, 200, 200, 120][index];

  test("nothing frozen is nothing sticky", () => {
    expect(frozenOffsets([0, 1, 2, 3], widthOf, 0)).toEqual([
      undefined,
      undefined,
      undefined,
      undefined,
    ]);
  });

  // `left: 0` on every frozen column stacks them all on the same edge — each
  // one starts where the frozen columns before it end.
  test("each frozen column starts after the ones before it", () => {
    expect(frozenOffsets([0, 1, 2, 3], widthOf, 2)).toEqual([0, 80, undefined, undefined]);
  });

  // The offsets follow the DISPLAY order, not the columns' own indexes: drag
  // the wide column to the front and the one behind it moves with it.
  test("a reordered grid freezes what is on the left, not what was", () => {
    expect(frozenOffsets([1, 0, 2, 3], widthOf, 2)).toEqual([0, 200, undefined, undefined]);
  });

  test("freezing every column leaves none scrolling", () => {
    expect(frozenOffsets([0, 1], widthOf, 5)).toEqual([0, 80]);
  });
});

describe("pinColumn", () => {
  const order = [0, 1, 2, 3, 4];

  // The whole point of the change: "freeze the first n columns" would have
  // dragged columns 0..2 into the run alongside column 3. On a 74-column table
  // that pins forty columns nobody asked for.
  test("pinning moves that column to the left edge, alone", () => {
    expect(pinColumn(order, 0, 3)).toEqual({ order: [3, 0, 1, 2, 4], frozen: 1 });
  });

  // Pins accumulate left to right in the order they were made, so a second pin
  // does not shove the first out of the place you just put it.
  test("a second pin lands after the first, not on top of it", () => {
    const first = pinColumn(order, 0, 3);
    expect(pinColumn(first.order, first.frozen, 4)).toEqual({
      order: [3, 4, 0, 1, 2],
      frozen: 2,
    });
  });

  test("unpinning drops it out of the run and leaves it just outside", () => {
    expect(pinColumn([3, 4, 0, 1, 2], 2, 3)).toEqual({ order: [4, 3, 0, 1, 2], frozen: 1 });
  });

  // Pin then unpin returns the run to nothing, which is the state that matters.
  test("a pin is undone by an unpin", () => {
    const pinned = pinColumn(order, 0, 2);
    expect(pinColumn(pinned.order, pinned.frozen, 2).frozen).toBe(0);
  });

  test("a column that is not in the order changes nothing", () => {
    expect(pinColumn(order, 1, 99)).toEqual({ order, frozen: 1 });
  });
});
