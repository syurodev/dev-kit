import { expect, test } from "bun:test";
import { filterLibrary, libraryPreview } from "./queryLibrary";
test("preview stays bounded for huge SQL while copy source is untouched", () => {
  const sql = "select\n" + "x".repeat(1000000);
  expect(libraryPreview(sql).length).toBeLessThanOrEqual(301);
  expect(sql.length).toBe(1000007);
});
test("filter handles whitespace, case, names and full SQL beyond preview", () => {
  const entries = [
    { id: "1", name: "Customers", sql: "select 1" },
    { id: "2", sql: "x".repeat(400) + "SELECT hidden" },
  ];
  expect(filterLibrary(entries, " customers ").map((e) => e.id)).toEqual(["1"]);
  expect(filterLibrary(entries, "HIDDEN").map((e) => e.id)).toEqual(["2"]);
  expect(filterLibrary(entries, "  ")).toEqual(entries);
});
