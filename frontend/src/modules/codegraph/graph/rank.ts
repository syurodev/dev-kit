export type RankInput = {
  rootId: string;
  nodeIds: string[];
  edges: { from: string; to: string }[];
};

export type Ranked = {
  /** Distance from the root along the longest path. Row index. */
  rank: number;
  /** Position among the nodes sharing this rank. Column index. */
  order: number;
};

/**
 * Longest-path ranking, then contiguous ordering within each rank.
 *
 * Traverses ONLY downstream nodes reachable from rootId via outgoing edges.
 * Callers or unreachable nodes are excluded so the entry point remains at the top.
 */
export function rankGraph(input: RankInput): Map<string, Ranked> {
  const known = new Set(input.nodeIds);
  const outgoing = new Map<string, string[]>();
  for (const e of input.edges) {
    if (!known.has(e.from) || !known.has(e.to)) continue;
    const list = outgoing.get(e.from);
    if (list) list.push(e.to);
    else outgoing.set(e.from, [e.to]);
  }

  const rank = new Map<string, number>();
  const onPath = new Set<string>();

  const visit = (id: string, depth: number) => {
    if (onPath.has(id)) return;
    if ((rank.get(id) ?? -1) >= depth) return;
    rank.set(id, depth);
    onPath.add(id);
    for (const next of outgoing.get(id) ?? []) {
      visit(next, depth + 1);
    }
    onPath.delete(id);
  };

  if (known.has(input.rootId)) {
    visit(input.rootId, 0);
  }

  const counters = new Map<number, number>();
  const out = new Map<string, Ranked>();
  for (const id of input.nodeIds) {
    if (!rank.has(id)) continue;
    const r = rank.get(id)!;
    const order = counters.get(r) ?? 0;
    counters.set(r, order + 1);
    out.set(id, { rank: r, order });
  }
  return out;
}
