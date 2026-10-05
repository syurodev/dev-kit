import { expect, test } from "bun:test";
import { stagedResult } from "./stagedResult";
import { databasePerformanceFixture } from "./databasePerformance.fixture";
test("only edited rows get copied in the 10,000 × 30 fixture", () => {
  const result = databasePerformanceFixture();
  const changed = stagedResult(result, {
    "7:3": "edited",
    "7:4": null,
    "9000:2": "second",
  })!;
  expect(changed.rows[7][3]).toBe("edited");
  expect(changed.rows[7][4]).toBeNull();
  expect(result.rows[7][3]).not.toBe("edited");
  expect(changed.rows.filter((r, i) => r !== result.rows[i])).toHaveLength(2);
  expect(changed.columns).toBe(result.columns);
});
test("an unstaged result retains its whole identity", () => {
  const result = databasePerformanceFixture();
  expect(stagedResult(result, {})).toBe(result);
  expect(stagedResult(null, { "1:1": "a" })).toBeNull();
});
test("invalid or stale positions do not grow the page", () => {
  const result = {
    columns: ["id"],
    rows: [["1"]],
    rowCount: 1,
    truncated: false,
  };
  const changed = stagedResult(result, {
    "-1:0": "bad",
    "2:0": "bad",
    "0:3": "bad",
    "0:0:1": "bad",
  })!;
  expect(changed.rows).toEqual([["1"]]);
  expect(changed.rows[0]).toBe(result.rows[0]);
});
