import type { Ranked } from "./rank";

export type Size = { width: number; height: number };
export type Position = { x: number; y: number };

/**
 * Vertical gap matches the Connector component's own height (167:79), so the
 * elbow has exactly the room it was drawn with. Horizontal gap is the sibling
 * spacing from the assembled screen (150:46). Both are gaps, not card sizes —
 * the cards report their own dimensions.
 */
const RANK_GAP = 86;
const SIBLING_GAP = 52;

/**
 * Returns null when any node has not been measured yet. Callers keep the graph
 * hidden until this returns a real map; the alternative — a fallback size —
 * would hardcode the card's dimensions in the one module that must not know
 * them, and would flash wrong positions on first paint.
 */
export function place(
  ranks: Map<string, Ranked>,
  sizes: Map<string, Size>,
): Map<string, Position> | null {
  const byRank = new Map<number, string[]>();
  for (const [id, r] of ranks) {
    if (!sizes.has(id)) return null;
    const list = byRank.get(r.rank);
    if (list) list.push(id);
    else byRank.set(r.rank, [id]);
  }

  const out = new Map<string, Position>();
  let y = 0;
  for (const rank of [...byRank.keys()].sort((a, b) => a - b)) {
    const members = byRank.get(rank)!.sort((a, b) => ranks.get(a)!.order - ranks.get(b)!.order);
    let x = 0;
    let tallest = 0;
    for (const id of members) {
      const size = sizes.get(id)!;
      out.set(id, { x, y });
      x += size.width + SIBLING_GAP;
      tallest = Math.max(tallest, size.height);
    }
    y += tallest + RANK_GAP;
  }
  return out;
}
