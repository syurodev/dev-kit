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
 * Longest-path ranking, then barycenter ordering within each rank.
 *
 * Traverses ONLY downstream nodes reachable from rootId via outgoing edges.
 * Callers or unreachable nodes are excluded so the entry point remains at the top.
 *
 * Linear in nodes + edges. One DFS from the root finds the reachable set, the
 * back edges (an edge into a node still on the DFS stack — what closes a
 * cycle) and a reverse postorder, which is a topological order of what is left
 * once the back edges are dropped. Longest path over a topological order is a
 * single relaxation pass. The previous version re-walked a subtree every time
 * it found a longer path into it, which is exponential on a dense DAG.
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

  const out = new Map<string, Ranked>();
  if (!known.has(input.rootId)) return out;

  // Iterative DFS: a deep call chain must not overflow the JS stack.
  const onPath = new Set<string>();
  const done = new Set<string>();
  const postorder: string[] = [];
  const forward = new Map<string, string[]>(); // outgoing minus back edges
  const stack: { id: string; next: number }[] = [{ id: input.rootId, next: 0 }];
  onPath.add(input.rootId);
  while (stack.length > 0) {
    const frame = stack[stack.length - 1];
    const targets = outgoing.get(frame.id) ?? [];
    if (frame.next < targets.length) {
      const to = targets[frame.next++];
      if (onPath.has(to)) continue; // back edge: ignoring it breaks the cycle
      const kept = forward.get(frame.id);
      if (kept) kept.push(to);
      else forward.set(frame.id, [to]);
      if (!done.has(to)) {
        onPath.add(to);
        stack.push({ id: to, next: 0 });
      }
      continue;
    }
    stack.pop();
    onPath.delete(frame.id);
    done.add(frame.id);
    postorder.push(frame.id);
  }

  const topo = postorder.reverse();
  const rank = new Map<string, number>([[input.rootId, 0]]);
  const parents = new Map<string, string[]>();
  for (const id of topo) {
    const r = rank.get(id)!;
    for (const to of forward.get(id) ?? []) {
      if ((rank.get(to) ?? -1) < r + 1) rank.set(to, r + 1);
      const list = parents.get(to);
      if (list) list.push(id);
      else parents.set(to, [id]);
    }
  }

  // Order each rank by the mean order of its parents (all of which sit in
  // lower ranks, so are already ordered), ties broken by input order. A child
  // then lands under the parent that points at it instead of wherever the
  // backend happened to list it, which is most of what keeps edges from
  // crossing on a call graph.
  const inputIndex = new Map(input.nodeIds.map((id, i) => [id, i]));
  const byRank = new Map<number, string[]>();
  for (const id of input.nodeIds) {
    const r = rank.get(id);
    if (r === undefined) continue;
    const list = byRank.get(r);
    if (list) list.push(id);
    else byRank.set(r, [id]);
  }
  for (const r of [...byRank.keys()].sort((a, b) => a - b)) {
    const members = byRank.get(r)!;
    const barycenter = new Map<string, number>();
    for (const id of members) {
      const ps = (parents.get(id) ?? []).map((p) => out.get(p)?.order ?? 0);
      barycenter.set(id, ps.length ? ps.reduce((a, b) => a + b, 0) / ps.length : 0);
    }
    members.sort(
      (a, b) => barycenter.get(a)! - barycenter.get(b)! || inputIndex.get(a)! - inputIndex.get(b)!,
    );
    members.forEach((id, order) => out.set(id, { rank: r, order }));
  }
  return out;
}
