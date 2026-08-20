import { expect, test } from "bun:test";
import { rankGraph } from "./rank";

const ids = (...v: string[]) => v;

test("ranks nodes by longest path from the root", () => {
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a", "b", "c"),
    edges: [
      { from: "a", to: "b" },
      { from: "b", to: "c" },
    ],
  });
  expect(r.get("a")!.rank).toBe(0);
  expect(r.get("b")!.rank).toBe(1);
  expect(r.get("c")!.rank).toBe(2);
});

test("a node reachable by two paths takes the longest rank", () => {
  // a->c directly and a->b->c. If c shared a rank with b, the edge b->c would
  // run sideways, which the connector geometry cannot express.
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a", "b", "c"),
    edges: [
      { from: "a", to: "b" },
      { from: "a", to: "c" },
      { from: "b", to: "c" },
    ],
  });
  expect(r.get("c")!.rank).toBe(2);
});

test("siblings get distinct, contiguous orders within a rank", () => {
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a", "b", "c"),
    edges: [
      { from: "a", to: "b" },
      { from: "a", to: "c" },
    ],
  });
  const orders = [r.get("b")!.order, r.get("c")!.order].sort();
  expect(orders).toEqual([0, 1]);
  expect(r.get("b")!.rank).toBe(r.get("c")!.rank);
});

test("a cycle terminates instead of hanging", () => {
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a", "b"),
    edges: [
      { from: "a", to: "b" },
      { from: "b", to: "a" },
    ],
  });
  expect(r.size).toBe(2);
});

test("nodes unreachable from the root (such as callers) are excluded", () => {
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a", "caller", "orphan"),
    edges: [{ from: "caller", to: "a" }],
  });
  expect(r.has("a")).toBe(true);
  expect(r.has("caller")).toBe(false);
  expect(r.has("orphan")).toBe(false);
});

test("edges referencing unknown nodes are ignored", () => {
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a"),
    edges: [{ from: "a", to: "ghost" }],
  });
  expect(r.size).toBe(1);
});
