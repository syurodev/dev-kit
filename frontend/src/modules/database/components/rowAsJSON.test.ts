import { describe, expect, it } from "bun:test";
import { rowAsJSON } from "./QueryConsole";

describe("rowAsJSON", () => {
  it("pairs a row back up with its column names", () => {
    expect(JSON.parse(rowAsJSON(["id", "email"], ["7", "a@b.c"]))).toEqual({
      id: "7",
      email: "a@b.c",
    });
  });

  // A query can return duplicate or empty column names — SELECT 1, 1 — and the
  // bridge passes them through. Falling back keeps every value in the object
  // instead of collapsing them onto one key.
  it("names a column that arrived without one", () => {
    expect(JSON.parse(rowAsJSON(["", ""], ["a", "b"]))).toEqual({
      col_0: "a",
      col_1: "b",
    });
  });

  it("keeps every value when there are more cells than column names", () => {
    const parsed = JSON.parse(rowAsJSON(["id"], ["7", "extra"]));
    expect(Object.values(parsed)).toEqual(["7", "extra"]);
  });
});
