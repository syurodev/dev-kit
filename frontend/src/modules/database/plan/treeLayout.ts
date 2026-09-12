import type { GraphNode } from "./graphSpec";

export type Placement = { x: number; y: number };

export type LayoutOptions = {
  nodeWidth: number;
  nodeHeight: number;
  /** Between siblings. */
  columnGap: number;
  /** Between a parent's row and its children's. */
  rowGap: number;
};

/**
 * A tidy tree: depth decides the row, and a parent sits over its children.
 *
 * Written here rather than pulled in, and the arithmetic is the argument:
 * dagre is 932K for a graph that is already a tree with its depths computed.
 * A tree needs one pass — give every leaf the next column and centre each
 * parent on the span of its own children — and that is the whole file.
 *
 * The root at the top and the scans at the bottom. Postgres reads its own
 * plans the other way up, leaves first, but a reader coming to a diagram
 * looks at the top of it, and the top is the question the query asked; the
 * leaves are how it was answered.
 */
export function layoutTree(
  nodes: readonly GraphNode[],
  { nodeWidth, nodeHeight, columnGap, rowGap }: LayoutOptions,
): Map<string, Placement> {
  const places = new Map<string, Placement>();
  if (nodes.length === 0) return places;

  const children = new Map<string, GraphNode[]>();
  const roots: GraphNode[] = [];
  for (const node of nodes) {
    if (node.parentId === null) {
      roots.push(node);
      continue;
    }
    const siblings = children.get(node.parentId);
    if (siblings) siblings.push(node);
    else children.set(node.parentId, [node]);
  }

  // Columns are handed out left to right as leaves are reached, so the
  // diagram reads in the order the tree is written. Shared across roots: two
  // statements in one plan stand side by side rather than on top of one
  // another.
  let nextColumn = 0;

  const column = (node: GraphNode): number => {
    const kids = children.get(node.id) ?? [];
    if (kids.length === 0) {
      const own = nextColumn;
      nextColumn += 1;
      places.set(node.id, { x: own * (nodeWidth + columnGap), y: node.depth * (nodeHeight + rowGap) });
      return own;
    }
    const spans = kids.map(column);
    const centre = (spans[0] + spans[spans.length - 1]) / 2;
    places.set(node.id, { x: centre * (nodeWidth + columnGap), y: node.depth * (nodeHeight + rowGap) });
    return centre;
  };

  for (const root of roots) column(root);
  return places;
}
