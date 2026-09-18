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

test("a dense layered DAG ranks in linear time", () => {
  // 30 layers of 10, each layer fully connected to the next: 10^30 root-to-leaf
  // paths. Re-walking a subtree whenever a longer path reaches it never
  // finishes on this; one relaxation pass over a topological order does.
  const layers = 30;
  const width = 10;
  const id = (l: number, i: number) => `n${l}_${i}`;
  const nodeIds = ["root"];
  const edges: { from: string; to: string }[] = [];
  for (let i = 0; i < width; i++) edges.push({ from: "root", to: id(0, i) });
  for (let l = 0; l < layers; l++) {
    for (let i = 0; i < width; i++) {
      nodeIds.push(id(l, i));
      if (l + 1 < layers) for (let j = 0; j < width; j++) edges.push({ from: id(l, i), to: id(l + 1, j) });
    }
  }
  const started = performance.now();
  const r = rankGraph({ rootId: "root", nodeIds, edges });
  expect(performance.now() - started).toBeLessThan(200);
  expect(r.size).toBe(nodeIds.length);
  expect(r.get(id(layers - 1, 0))!.rank).toBe(layers);
});

test("a cycle below the root still ranks by the longest acyclic path", () => {
  // a -> b -> c -> b: the back edge c->b is dropped, not followed.
  const r = rankGraph({
    rootId: "a",
    nodeIds: ids("a", "b", "c"),
    edges: [
      { from: "a", to: "b" },
      { from: "b", to: "c" },
      { from: "c", to: "b" },
    ],
  });
  expect(r.get("b")!.rank).toBe(1);
  expect(r.get("c")!.rank).toBe(2);
});

test("children are ordered under their parents, not in input order", () => {
  // Input lists y's child before x's; x sits left of y, so x's child should too.
  const r = rankGraph({
    rootId: "root",
    nodeIds: ids("root", "x", "y", "yChild", "xChild"),
    edges: [
      { from: "root", to: "x" },
      { from: "root", to: "y" },
      { from: "x", to: "xChild" },
      { from: "y", to: "yChild" },
    ],
  });
  expect(r.get("x")!.order).toBe(0);
  expect(r.get("y")!.order).toBe(1);
  expect(r.get("xChild")!.order).toBe(0);
  expect(r.get("yChild")!.order).toBe(1);
});
