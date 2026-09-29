/**
 * The flow through one node: everything that leads to it and everything it
 * leads to. Hovering a card keeps this lit and dims the rest, so a reader
 * following one call chain in a wide graph is not reading the other twenty.
 *
 * "Connected" alone would be the wrong set. A service called by three
 * controllers is connected to all of them, but the path a reader is on runs
 * root → this controller → the service → its repository; siblings of a node
 * on that path are not on it. So the set is the node's ancestors (what can
 * reach it) plus its descendants (what it can reach), and an edge is on the
 * flow only when it runs along one of those two chains — an edge between two
 * lit nodes that bypasses the hovered one (a controller calling the
 * repository directly) stays dim.
 */
export type Focus = { nodes: ReadonlySet<string>; edges: ReadonlySet<string> };

type Link = { id: string; source: string; target: string };

export function flowThrough(nodeId: string, links: readonly Link[]): Focus {
  const out = new Map<string, Link[]>();
  const into = new Map<string, Link[]>();
  for (const link of links) {
    (out.get(link.source) ?? out.set(link.source, []).get(link.source)!).push(link);
    (into.get(link.target) ?? into.set(link.target, []).get(link.target)!).push(link);
  }

  const edges = new Set<string>();
  // Breadth-first either way; `seen` makes a cycle (a recursive call, two
  // services calling each other) terminate.
  const walk = (next: Map<string, Link[]>, far: (l: Link) => string) => {
    const seen = new Set([nodeId]);
    const queue = [nodeId];
    while (queue.length > 0) {
      const at = queue.shift()!;
      for (const link of next.get(at) ?? []) {
        edges.add(link.id);
        const other = far(link);
        if (!seen.has(other)) {
          seen.add(other);
          queue.push(other);
        }
      }
    }
    return seen;
  };
  const descendants = walk(out, (l) => l.target);
  const ancestors = walk(into, (l) => l.source);
  return { nodes: new Set([...ancestors, ...descendants]), edges };
}
