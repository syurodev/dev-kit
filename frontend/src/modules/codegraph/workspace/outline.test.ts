import { describe, expect, it } from "bun:test";
import type { CodeGraphOutlineSymbol } from "@/bridge/workspaceCode";

import { countSymbols, outlineRows, symbolPathAt } from "./outline";

const last = <T>(list: T[]): T | undefined => list[list.length - 1];

const sym = (id: string, line: number, endLine: number, children?: CodeGraphOutlineSymbol[]): CodeGraphOutlineSymbol => ({
  id,
  label: id,
  kind: children ? "class" : "method",
  line,
  endLine,
  ...(children ? { children } : {}),
});

const tree = [
  sym("A", 1, 30, [sym("a1", 3, 8), sym("a2", 10, 20, [sym("a2x", 12, 14)]), sym("a3", 22, 28)]),
  sym("B", 31, 60, [sym("b1", 33, 40), sym("b2", 42, 58)]),
];

describe("symbolPathAt", () => {
  it("walks down to the innermost symbol containing the line", () => {
    expect(symbolPathAt(tree, 13).map((s) => s.id)).toEqual(["A", "a2", "a2x"]);
    expect(symbolPathAt(tree, 21).map((s) => s.id)).toEqual(["A"]); // between members: the class only
    expect(symbolPathAt(tree, 45).map((s) => s.id)).toEqual(["B", "b2"]);
  });

  it("finds nothing outside every range", () => {
    expect(symbolPathAt(tree, 0)).toEqual([]);
    expect(symbolPathAt(tree, 61)).toEqual([]);
    expect(symbolPathAt([], 5)).toEqual([]);
  });

  it("includes both boundary lines", () => {
    expect(last(symbolPathAt(tree, 3))?.id).toBe("a1");
    expect(last(symbolPathAt(tree, 8))?.id).toBe("a1");
    expect(last(symbolPathAt(tree, 9))?.id).toBe("A");
  });

  it("copes with a sibling that only partly overlaps its neighbour", () => {
    const odd = [sym("x", 1, 10), sym("y", 8, 20)];
    expect(last(symbolPathAt(odd, 9))?.id).toBe("y"); // nearest start wins when it contains the line
    expect(last(symbolPathAt(odd, 5))?.id).toBe("x");
  });

  it("is logarithmic: a wide outline does not scan its siblings", () => {
    const wide = Array.from({ length: 20000 }, (_, i) => sym(`m${i}`, i * 3 + 1, i * 3 + 2));
    const t0 = performance.now();
    for (let i = 0; i < 2000; i++) symbolPathAt(wide, ((i * 7919) % 20000) * 3 + 1);
    expect(performance.now() - t0).toBeLessThan(200);
    expect(last(symbolPathAt(wide, 3 * 12345 + 2))?.id).toBe("m12345");
  });
});

describe("outlineRows", () => {
  it("lists every symbol with its depth when unfiltered", () => {
    expect(outlineRows(tree).map((r) => `${r.depth}:${r.symbol.id}`)).toEqual([
      "0:A", "1:a1", "1:a2", "2:a2x", "1:a3", "0:B", "1:b1", "1:b2",
    ]);
    expect(countSymbols(tree)).toBe(8);
  });

  it("keeps the ancestors of a match so it still reads in context", () => {
    expect(outlineRows(tree, "a2x").map((r) => r.symbol.id)).toEqual(["A", "a2", "a2x"]);
    expect(outlineRows(tree, "zzz")).toEqual([]);
  });
});
