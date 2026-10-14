import { expect, test } from "bun:test";
import { graphBounds, place } from "./place";
import type { Ranked } from "./rank";

const ranked = (v: Record<string, Ranked>) => new Map(Object.entries(v));
const sized = (v: Record<string, { width: number; height: number }>) => new Map(Object.entries(v));

test("returns null while any node is still unmeasured", () => {
  const out = place(
    ranked({ a: { rank: 0, order: 0 }, b: { rank: 1, order: 0 } }),
    sized({ a: { width: 268, height: 94 } }),
  );
  expect(out).toBeNull();
});

test("stacks ranks downward using the tallest node in each rank", () => {
  const out = place(
    ranked({ a: { rank: 0, order: 0 }, tall: { rank: 0, order: 1 }, b: { rank: 1, order: 0 } })!,
    sized({
      a: { width: 100, height: 40 },
      tall: { width: 100, height: 120 },
      b: { width: 100, height: 40 },
    }),
  )!;
  expect(out.get("a")!.y).toBe(0);
  expect(out.get("tall")!.y).toBe(0);
  // rank 1 clears the tallest card in rank 0, not the first one
  expect(out.get("b")!.y).toBeGreaterThanOrEqual(120);
});

test("siblings advance by the previous sibling's own width", () => {
  const out = place(
    ranked({ wide: { rank: 0, order: 0 }, next: { rank: 0, order: 1 } })!,
    sized({ wide: { width: 400, height: 40 }, next: { width: 100, height: 40 } }),
  )!;
  expect(out.get("wide")!.x).toBe(0);
  expect(out.get("next")!.x).toBeGreaterThanOrEqual(400);
});

test("the first node of every rank sits on the shared trunk at x=0", () => {
  const out = place(
    ranked({ a: { rank: 0, order: 0 }, b: { rank: 1, order: 0 } })!,
    sized({ a: { width: 268, height: 94 }, b: { width: 268, height: 94 } }),
  )!;
  expect(out.get("a")!.x).toBe(0);
  expect(out.get("b")!.x).toBe(0);
});

test("graphBounds is the extent of the placed cards", () => {
  const positions = new Map([
    ["a", { x: 0, y: 0 }],
    ["b", { x: 0, y: 180 }],
    ["c", { x: 320, y: 180 }],
  ]);
  const sizes = sized({
    a: { width: 268, height: 94 },
    b: { width: 268, height: 130 },
    c: { width: 268, height: 60 },
  });
  expect(graphBounds(positions, sizes)).toEqual({ width: 588, height: 310 });
  expect(graphBounds(new Map(), sizes)).toBeNull();
});
