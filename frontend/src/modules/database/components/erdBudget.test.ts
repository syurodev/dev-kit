import { expect, test } from "bun:test";
import {
  erdTableWindow,
  erdColumnWindow,
  hasSingleIdPrimaryKey,
  ERD_NODE_LIMIT,
} from "./erdBudget";
test("whole schema is bounded with stable ordering", () => {
  const tables = Object.fromEntries(
    Array.from({ length: 1000 }, (_, i) => [`table${i}`, ["id"]]),
  );
  const a = erdTableWindow(tables, {}, "", 1);
  expect(a.names.length).toBe(ERD_NODE_LIMIT);
  expect(a.limited).toBe(true);
  expect(a.names).toEqual([...a.names].sort());
});
test("large hub stays bounded and retains the chosen root", () => {
  const tables = Object.fromEntries(
    Array.from({ length: 500 }, (_, i) => [`t${i}`, []]),
  );
  const graph = { t499: new Set(Object.keys(tables)) };
  const a = erdTableWindow(tables, graph, "t499", 3);
  expect(a.names.length).toBe(100);
  expect(a.names[0]).toBe("t499");
  expect(a.hops.t499).toBe(0);
  expect(a.limited).toBe(true);
});
test("BFS preserves hop distance, ignores stale targets and does not invent neighbours", () => {
  const a = erdTableWindow(
    { a: [], b: [], c: [], isolated: [] },
    { a: new Set(["b", "gone"]), b: new Set(["a", "c"]) },
    "a",
    2,
  );
  expect(a.names).toEqual(["a", "b", "c"]);
  expect(a.hops.c).toBe(2);
  expect(a.limited).toBe(false);
});

test("wide tables retain late key handles without unbounded column DOM", () => {
  const columns = Array.from({ length: 1000 }, (_, i) => `c${i}`);
  const selected = erdColumnWindow(columns, ["c999"], true);
  expect(selected.length).toBe(80);
  expect(selected[0]).toBe("c999");
  expect(erdColumnWindow(columns, ["c999"], false)).toEqual(["c999"]);
});

test("inferred targets need a proven single-column id primary key", () => {
  expect(hasSingleIdPrimaryKey([], "users")).toBe(false);
  expect(
    hasSingleIdPrimaryKey(
      [{ table: "users", primary: false, columns: ["id"] }],
      "users",
    ),
  ).toBe(false);
  expect(
    hasSingleIdPrimaryKey(
      [{ table: "users", primary: true, columns: ["tenant", "id"] }],
      "users",
    ),
  ).toBe(false);
  expect(
    hasSingleIdPrimaryKey(
      [{ table: "users", primary: true, columns: ["id"] }],
      "users",
    ),
  ).toBe(true);
});
