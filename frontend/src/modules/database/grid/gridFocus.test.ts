import { describe, expect, it } from "bun:test";
import { HEADER_ROW, nextFocus, type GridBounds } from "./gridFocus";

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
