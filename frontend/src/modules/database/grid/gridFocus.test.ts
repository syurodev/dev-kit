import { describe, expect, it } from "bun:test";
import {
  HEADER_ROW,
  inRange,
  nextFocus,
  nextSelection,
  rangeSize,
  selectedRange,
  type GridBounds,
} from "./gridFocus";

const bounds: GridBounds = { rows: 200, columns: 10, page: 20, jump: false };
const at = (row: number, position: number) => ({ row, position });

describe("nextFocus", () => {
  it("walks the two axes", () => {
    expect(nextFocus(at(5, 3), "ArrowDown", bounds)).toEqual(at(6, 3));
    expect(nextFocus(at(5, 3), "ArrowUp", bounds)).toEqual(at(4, 3));
    expect(nextFocus(at(5, 3), "ArrowRight", bounds)).toEqual(at(5, 4));
    expect(nextFocus(at(5, 3), "ArrowLeft", bounds)).toEqual(at(5, 2));
  });

  // The header is a row of the grid. Without it the sort is mouse-only, which
  // is what giving every header button its own tab stop was paying for.
  it("puts the header above row 0 and stops there", () => {
    expect(nextFocus(at(0, 2), "ArrowUp", bounds)).toEqual(at(HEADER_ROW, 2));
    expect(nextFocus(at(HEADER_ROW, 2), "ArrowUp", bounds)).toEqual(at(HEADER_ROW, 2));
    expect(nextFocus(at(HEADER_ROW, 2), "ArrowDown", bounds)).toEqual(at(0, 2));
  });

  it("stops at the last row and the last column", () => {
    expect(nextFocus(at(199, 9), "ArrowDown", bounds)).toEqual(at(199, 9));
    expect(nextFocus(at(199, 9), "ArrowRight", bounds)).toEqual(at(199, 9));
    expect(nextFocus(at(0, 0), "ArrowLeft", bounds)).toEqual(at(0, 0));
  });

  it("pages without running off either end", () => {
    expect(nextFocus(at(100, 0), "PageDown", bounds)).toEqual(at(120, 0));
    expect(nextFocus(at(190, 0), "PageDown", bounds)).toEqual(at(199, 0));
    expect(nextFocus(at(5, 0), "PageUp", bounds)).toEqual(at(HEADER_ROW, 0));
  });

  it("Home and End are the row's ends, and the table's with the modifier", () => {
    expect(nextFocus(at(42, 5), "Home", bounds)).toEqual(at(42, 0));
    expect(nextFocus(at(42, 5), "End", bounds)).toEqual(at(42, 9));
    expect(nextFocus(at(42, 5), "Home", { ...bounds, jump: true })).toEqual(at(0, 0));
    expect(nextFocus(at(42, 5), "End", { ...bounds, jump: true })).toEqual(at(199, 9));
  });

  // Null is not "stayed put": it tells the grid to let the key through, which
  // is what keeps Tab able to leave and the shell's own shortcuts alive.
  it("returns null for a key it does not handle", () => {
    expect(nextFocus(at(1, 1), "Tab", bounds)).toBeNull();
    expect(nextFocus(at(1, 1), "a", bounds)).toBeNull();
  });

  // Columns and no rows is a real result: the query ran and matched nothing.
  it("survives a result with no rows", () => {
    const empty = { ...bounds, rows: 0 };
    expect(nextFocus(at(HEADER_ROW, 0), "ArrowDown", empty)).toEqual(at(HEADER_ROW, 0));
  });
});

describe("the selected rectangle", () => {
  it("is the same whichever corner it was dragged from", () => {
    const forwards = selectedRange({ anchor: at(2, 1), focus: at(5, 3) });
    const backwards = selectedRange({ anchor: at(5, 3), focus: at(2, 1) });
    expect(forwards).toEqual({ top: 2, bottom: 5, left: 1, right: 3 });
    expect(backwards).toEqual(forwards);
  });

  it("counts the cells a bulk edit would change", () => {
    expect(rangeSize(selectedRange({ anchor: at(0, 0), focus: at(0, 0) }))).toBe(1);
    expect(rangeSize(selectedRange({ anchor: at(2, 1), focus: at(5, 3) }))).toBe(12);
  });

  it("knows what is inside it", () => {
    const range = selectedRange({ anchor: at(2, 1), focus: at(5, 3) });
    expect(inRange(range, 3, 2)).toBe(true);
    expect(inRange(range, 2, 1)).toBe(true);
    expect(inRange(range, 1, 2)).toBe(false);
    expect(inRange(range, 3, 4)).toBe(false);
  });
});

describe("nextSelection", () => {
  const sel = (r: number, p: number) => ({ anchor: at(r, p), focus: at(r, p) });

  it("moves both corners without Shift", () => {
    expect(nextSelection(sel(3, 2), "ArrowDown", bounds, false)).toEqual({
      anchor: at(4, 2),
      focus: at(4, 2),
    });
  });

  it("grows from the anchor with Shift", () => {
    const grown = nextSelection(sel(3, 2), "ArrowDown", bounds, true)!;
    expect(grown.anchor).toEqual(at(3, 2));
    expect(grown.focus).toEqual(at(4, 2));
    expect(rangeSize(selectedRange(grown))).toBe(2);
  });

  // A rectangle whose corner is the header row would offer to edit the column
  // names, which is not what a bulk edit means.
  it("collapses when it reaches the header", () => {
    const atHeader = nextSelection(sel(0, 2), "ArrowUp", bounds, true)!;
    expect(atHeader.anchor).toEqual(at(HEADER_ROW, 2));
    expect(atHeader.focus).toEqual(at(HEADER_ROW, 2));
  });

  it("passes a key it does not handle straight through", () => {
    expect(nextSelection(sel(1, 1), "Tab", bounds, true)).toBeNull();
  });
});
