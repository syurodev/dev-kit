import { describe, expect, test } from "bun:test";
import { planGraph } from "./planGraph";
import { layoutTree } from "./treeLayout";
import type { PlanNode, QueryPlan } from "../../../bridge/database";

const node = (over: Partial<PlanNode> = {}): PlanNode => ({
  operation: "Seq Scan",
  depth: 0,
  estimatedRows: 0,
  actualRows: 0,
  selfMillis: 0,
  detail: "",
  children: [],
  ...over,
});

const plan = (nodes: PlanNode[], analyzed = true): QueryPlan => ({ nodes, raw: "", analyzed });

/**
 * The engine already answered in a tree; this is what the canvas draws from.
 * Everything here is about the three numbers worth a colour.
 */
describe("a plan as a diagram", () => {
  test("every node, and an edge for every parent", () => {
    const graph = planGraph(
      plan([
        node({
          operation: "Hash Join",
          children: [node({ operation: "Seq Scan" }), node({ operation: "Index Scan" })],
        }),
      ]),
    );

    expect(graph.nodes.map((n) => n.label)).toEqual(["Hash Join", "Seq Scan", "Index Scan"]);
    expect(graph.edges).toHaveLength(2);
    expect(graph.nodes[1].parentId).toBe(graph.nodes[0].id);
    expect(graph.nodes[1].depth).toBe(1);
  });

  /*
   * Self time, not inclusive. With inclusive time the root is always 100% and
   * the picture says nothing — the decision is already in the Go type and
   * this keeps faith with it.
   */
  test("the share is of the whole plan's own time", () => {
    const graph = planGraph(
      plan([
        node({ operation: "Aggregate", selfMillis: 1, children: [node({ selfMillis: 3 })] }),
      ]),
    );

    expect(graph.totalMillis).toBe(4);
    expect(graph.nodes[0].metrics?.shareOfTime).toBe(0.25);
    expect(graph.nodes[1].metrics?.shareOfTime).toBe(0.75);
  });

  test("the hottest node is the one that spent the most in itself", () => {
    const graph = planGraph(
      plan([
        node({ operation: "Aggregate", selfMillis: 1, children: [node({ selfMillis: 9 })] }),
      ]),
    );

    expect(graph.nodes.filter((n) => n.hottest).map((n) => n.label)).toEqual(["Seq Scan"]);
  });

  // A plan that was never run has no time at all, and marking one of its
  // nodes hottest would be an answer to a question nobody asked.
  test("nothing is hottest when nothing took any time", () => {
    const graph = planGraph(plan([node({ children: [node()] })], false));
    expect(graph.nodes.some((n) => n.hottest)).toBe(false);
    expect(graph.analyzed).toBe(false);
  });

  /*
   * How far the planner was out is the other number worth a colour: a node
   * that expected 12 rows and found 120,000 is where the query went wrong,
   * whatever it cost in milliseconds.
   */
  test.each([
    [100, 10, 10],
    [10, 100, 10],
    [100, 100, 1],
  ])("estimated %d, actual %d → off by %d", (estimated, actual, expected) => {
    const graph = planGraph(plan([node({ estimatedRows: estimated, actualRows: actual })]));
    expect(graph.nodes[0].metrics?.rowsOffBy).toBe(expected);
  });

  // Null, not 1: an un-analyzed plan has no actual rows and a node that
  // returned none has no ratio. Neither is "the estimate was right".
  test.each([
    [100, 0],
    [0, 100],
    [0, 0],
  ])("estimated %d, actual %d → nothing to compare", (estimated, actual) => {
    const graph = planGraph(plan([node({ estimatedRows: estimated, actualRows: actual })]));
    expect(graph.nodes[0].metrics?.rowsOffBy).toBeNull();
  });

  test("no plan is an empty diagram, not a crash", () => {
    expect(planGraph(null).nodes).toEqual([]);
    expect(planGraph(plan([])).nodes).toEqual([]);
  });
});

/**
 * Depth decides the row, and a parent sits over its children. Written here
 * rather than pulled in: dagre is 932K for a tree whose depths are already
 * computed.
 */
describe("laying the tree out", () => {
  const OPTIONS = { nodeWidth: 100, nodeHeight: 40, columnGap: 20, rowGap: 30 };
  const at = (places: Map<string, { x: number; y: number }>, id: string) => places.get(id);

  test("children share a row, and the parent is centred over them", () => {
    const graph = planGraph(plan([node({ children: [node(), node()] })]));
    const places = layoutTree(graph.nodes, OPTIONS);

    const [root, left, right] = graph.nodes.map((n) => at(places, n.id)!);
    expect(left.y).toBe(right.y);
    expect(root.y).toBeLessThan(left.y);
    expect(root.x).toBe((left.x + right.x) / 2);
  });

  test("a chain goes straight down", () => {
    const graph = planGraph(plan([node({ children: [node({ children: [node()] })] })]));
    const places = layoutTree(graph.nodes, OPTIONS);
    const xs = graph.nodes.map((n) => at(places, n.id)!.x);
    expect(new Set(xs).size).toBe(1);
  });

  // Two statements explained together stand side by side rather than on top
  // of one another.
  test("two roots do not overlap", () => {
    const graph = planGraph(plan([node(), node()]));
    const places = layoutTree(graph.nodes, OPTIONS);
    const [first, second] = graph.nodes.map((n) => at(places, n.id)!);
    expect(second.x).toBeGreaterThan(first.x);
    expect(second.y).toBe(first.y);
  });

  test("nothing in, nothing out", () => {
    expect(layoutTree([], OPTIONS).size).toBe(0);
  });
});
