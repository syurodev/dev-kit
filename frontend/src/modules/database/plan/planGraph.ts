import type { PlanNode, QueryPlan } from "../../../bridge/database";
import { EMPTY_GRAPH, type GraphMetrics, type GraphNode, type GraphSpec } from "./graphSpec";

/**
 * An execution plan as a diagram.
 *
 * The engine already answered in a tree — Go normalises Postgres's
 * `EXPLAIN (FORMAT JSON)` and MySQL's `EXPLAIN FORMAT=JSON`, two entirely
 * different schemas, into one PlanNode. This turns that tree into the shape
 * the canvas draws, and works out the three numbers that are worth a colour:
 * how long a node spent in ITSELF, what share of the query that was, and how
 * far the planner's row estimate was from what it found.
 *
 * Self time, not inclusive: with inclusive time the root is always 100% and
 * the picture says nothing. That decision is already in the Go type; this
 * keeps faith with it.
 */
export function planGraph(plan: QueryPlan | null | undefined): GraphSpec {
  if (!plan?.nodes?.length) return EMPTY_GRAPH;

  const nodes: GraphNode[] = [];
  const edges: GraphSpec["edges"] = [];

  const walk = (node: PlanNode, parentId: string | null, depth: number, path: string) => {
    const id = path;
    nodes.push({
      id,
      label: node.operation || "Step",
      detail: node.detail ?? "",
      kind: "operation",
      depth,
      parentId,
      // Filled in below: a share needs the total, and the total needs
      // every node.
      metrics: rawMetrics(node),
      hottest: false,
    });
    if (parentId !== null) {
      edges.push({ id: `${parentId}->${id}`, source: parentId, target: id });
    }
    (node.children ?? []).forEach((child, index) => walk(child, id, depth + 1, `${id}.${index}`));
  };

  plan.nodes.forEach((root, index) => walk(root, null, 0, `n${index}`));

  const totalMillis = nodes.reduce((sum, node) => sum + (node.metrics?.selfMillis ?? 0), 0);
  // A tie goes to the first, which is the one nearer the root: of two nodes
  // that cost the same, the higher one is the one worth looking at first.
  let hottest = -1;
  nodes.forEach((node, index) => {
    const millis = node.metrics?.selfMillis ?? 0;
    if (millis > 0 && (hottest === -1 || millis > (nodes[hottest].metrics?.selfMillis ?? 0))) {
      hottest = index;
    }
  });

  return {
    nodes: nodes.map((node, index) => ({
      ...node,
      hottest: index === hottest,
      metrics: node.metrics
        ? {
            ...node.metrics,
            shareOfTime: totalMillis > 0 ? node.metrics.selfMillis / totalMillis : 0,
          }
        : null,
    })),
    edges,
    totalMillis,
    analyzed: plan.analyzed,
  };
}

export type PlanStepKind = "scan" | "join" | "sort" | "aggregate" | "other";

/**
 * What kind of step an operation name is, across Postgres ("Hash Join",
 * "Index Only Scan"), MySQL ("nested_loop", "ordering_operation") and MariaDB.
 * Order matters: "HashAggregate" is an aggregate before it is anything else.
 */
export function planStepKind(operation: string): PlanStepKind {
  const op = operation.toLowerCase();
  if (/join|loop/.test(op)) return "join";
  if (/aggregate|group|distinct|unique/.test(op)) return "aggregate";
  if (/sort|order/.test(op)) return "sort";
  if (/scan|table|index|seek|lookup|\bref\b|\ball\b/.test(op)) return "scan";
  return "other";
}

function rawMetrics(node: PlanNode): GraphMetrics {
  return {
    selfMillis: node.selfMillis ?? 0,
    shareOfTime: 0,
    estimatedRows: node.estimatedRows ?? 0,
    actualRows: node.actualRows ?? 0,
    rowsOffBy: rowsOffBy(node.estimatedRows ?? 0, node.actualRows ?? 0),
  };
}

/**
 * How far out the estimate was, as a factor of at least 1.
 *
 * Null when there is nothing to compare: an EXPLAIN that never ran has no
 * actual rows, and a node that produced none has no ratio to give. Both are
 * different from "the estimate was right", so neither may answer 1.
 */
function rowsOffBy(estimated: number, actual: number): number | null {
  if (estimated <= 0 || actual <= 0) return null;
  return Math.max(estimated / actual, actual / estimated);
}
