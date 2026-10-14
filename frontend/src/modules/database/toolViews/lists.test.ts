import { describe, expect, test } from "bun:test";
import { LIST_CAP, LIST_MAX, listWindow, visibleSchemas } from "./lists";

describe("listWindow", () => {
  test("folded: the first LIST_CAP, and a way to see all", () => {
    expect(listWindow(45, false)).toEqual({ shown: LIST_CAP, toggle: "Show all 45", notShown: 0 });
  });

  test("a short list needs no toggle", () => {
    expect(listWindow(LIST_CAP, false)).toEqual({ shown: LIST_CAP, toggle: null, notShown: 0 });
  });

  test("open: everything, and a way back", () => {
    expect(listWindow(45, true)).toEqual({ shown: 45, toggle: "Show fewer", notShown: 0 });
  });

  test("open never draws more than LIST_MAX, and says how many it left out", () => {
    expect(listWindow(LIST_MAX + 120, true)).toEqual({ shown: LIST_MAX, toggle: "Show fewer", notShown: 120 });
    expect(listWindow(LIST_MAX + 120, false).toggle).toBe(`Show first ${LIST_MAX}`);
  });
});

describe("visibleSchemas", () => {
  const schema = (name: string, n: number) => ({
    name,
    tables: Array.from({ length: n }, (_, i) => ({ name: `t${i}`, columns: [] })),
  });

  test("tables are spent across schemas in order", () => {
    const shown = visibleSchemas([schema("a", 30), schema("b", 30)], 40);
    expect(shown.map((s) => [s.name, s.tables.length])).toEqual([
      ["a", 30],
      ["b", 10],
    ]);
  });

  test("an uncapped list keeps a trailing empty schema", () => {
    const shown = visibleSchemas([schema("a", 3), schema("b", 0)], 3);
    expect(shown.map((s) => s.name)).toEqual(["a", "b"]);
  });

  test("an empty schema shows while there is budget, and not after", () => {
    const shown = visibleSchemas([schema("a", 0), schema("b", 40), schema("c", 0), schema("d", 5)], 40);
    expect(shown.map((s) => s.name)).toEqual(["a", "b"]);
  });
});
