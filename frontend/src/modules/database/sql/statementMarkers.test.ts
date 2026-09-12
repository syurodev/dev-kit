import { describe, expect, test } from "bun:test";
import { statementMarkers } from "./statementMarkers";
import { dialectOf } from "./statements";

const pg = dialectOf("postgres");

/**
 * The mark says which statement ran and how it went, beside the statement
 * itself. Everything here is about the marks staying true while the script
 * underneath them is edited.
 */
describe("which line is marked", () => {
  test("the line the statement starts on", () => {
    const sql = "SELECT 1;\nSELECT 2;\nSELECT 3";
    const marks = statementMarkers(sql, pg, { "SELECT 1": "ok", "SELECT 3": "error" });
    expect([...marks]).toEqual([
      [1, "ok"],
      [3, "error"],
    ]);
  });

  // A tick beside "-- takings by day" is a tick beside a note.
  test("past the note the statement opens with", () => {
    const sql = "-- takings\n-- by day\nSELECT sum(total) FROM orders";
    const marks = statementMarkers(sql, pg, {
      "-- takings\n-- by day\nSELECT sum(total) FROM orders": "ok",
    });
    expect([...marks]).toEqual([[3, "ok"]]);
  });

  /*
   * Keyed by text, not by position — which is the whole reason the marks
   * survive an edit. Insert a line above and every offset moves; the
   * statement is still the one that ran.
   */
  test("an insertion above moves the mark with the statement", () => {
    const outcomes = { "SELECT 2": "ok" } as const;
    expect([...statementMarkers("SELECT 1;\nSELECT 2", pg, outcomes)]).toEqual([[2, "ok"]]);
    expect([...statementMarkers("\n\nSELECT 1;\nSELECT 2", pg, outcomes)]).toEqual([[4, "ok"]]);
  });

  test("editing the statement drops its mark, because it is a different statement now", () => {
    const marks = statementMarkers("SELECT 2 + 1", pg, { "SELECT 2": "ok" });
    expect(marks.size).toBe(0);
  });

  test("nothing run, nothing marked", () => {
    expect(statementMarkers("SELECT 1", pg, {}).size).toBe(0);
  });

  // Two identical statements share one answer, which is the honest reading:
  // running either of them tells you the same thing.
  test("the same statement twice is marked twice", () => {
    const marks = statementMarkers("SELECT 1;\nSELECT 1", pg, { "SELECT 1": "error" });
    expect([...marks]).toEqual([
      [1, "error"],
      [2, "error"],
    ]);
  });
});
