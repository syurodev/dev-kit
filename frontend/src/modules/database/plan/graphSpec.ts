/**
 * A diagram, as data.
 *
 * The contract between whatever works out the shape and the one thing that
 * draws it. Deliberately free of React Flow, of the layout library, and of
 * React: two producers are planned and they do not live in the same place.
 * One is here in TypeScript and reads a parsed statement — what the SQL
 * MEANS. The other is Go and reads what the engine came back with — what it
 * DID. They answer different questions about the same query and there is no
 * reason for them to draw two canvases.
 *
 * The rule that keeps that true: nothing in this file may import from
 * `@xyflow/react`. The moment it does, the Go side has to speak React Flow's
 * shape, and a producer in another language cannot.
 */

/** What a node is, for the reader — not for the renderer. */
export type GraphNodeKind =
  /** A step the engine performed. */
  | "operation"
  /** A relation the query reads or writes. */
  | "relation";

export type GraphMetrics = {
  /** Time in this node alone, not including its children. */
  selfMillis: number;
  /** That time as a share of the whole plan's, 0..1. */
  shareOfTime: number;
  estimatedRows: number;
  actualRows: number;
  /**
   * How far the estimate was out, as a factor ≥ 1, or null when there is
   * nothing to compare — an un-analyzed plan has no actual rows, and a node
   * that returned none has no ratio.
   *
   * The direction is deliberately dropped: over and under both mean the
   * planner was working from the wrong number, and both are fixed by the
   * same thing.
   */
  rowsOffBy: number | null;
};

export type GraphNode = {
  id: string;
  /** The line the node is named by. */
  label: string;
  /** Everything else the engine said about it. */
  detail: string;
  kind: GraphNodeKind;
  /** Distance from the root, which is what a layout reads. */
  depth: number;
  /** The node above it, or null for a root. */
  parentId: string | null;
  metrics: GraphMetrics | null;
  /** The single node that spent the most time in itself. */
  hottest: boolean;
};

export type GraphEdge = { id: string; source: string; target: string };

export type GraphSpec = {
  nodes: GraphNode[];
  edges: GraphEdge[];
  /** The sum of every node's own time. */
  totalMillis: number;
  /** False when the statement was explained but never run. */
  analyzed: boolean;
};

export const EMPTY_GRAPH: GraphSpec = { nodes: [], edges: [], totalMillis: 0, analyzed: false };
