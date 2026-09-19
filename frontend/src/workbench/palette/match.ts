/**
 * How well `text` answers `query`, as a number to sort by; 0 is "not at all".
 *
 * Shared by the palette and by every module's search so that a result ranks
 * the same wherever it came from. The tiers are the ones people expect from a
 * launcher, in the order they expect them:
 *
 *   the whole text        "orders"  → orders
 *   its beginning         "ord"     → orders, order_items
 *   the start of a word   "item"    → order_items, OrderItemService
 *   anywhere inside       "der"     → orders
 *   in order, with gaps   "oitm"    → order_items
 *
 * Words start after a separator or at a lower→Upper camelCase step, so both
 * snake_case tables and Java classes match the way they are read. Within a
 * tier shorter text wins: "orders" is a better answer to "ord" than
 * "order_audit_log_archive".
 */
export function matchScore(query: string, text: string): number {
  const q = query.trim().toLowerCase();
  if (!q) return 1;
  const t = text.toLowerCase();
  if (!t) return 0;

  // Shorter is better, but only ever as a tie-break inside a tier.
  const brevity = 1 / (1 + t.length / 64);

  if (t === q) return 5 + brevity;
  if (t.startsWith(q)) return 4 + brevity;

  const at = t.indexOf(q);
  if (at > 0) return (isWordStart(text, at) ? 3 : 2) + brevity;

  return isSubsequence(q, t) ? 1 + brevity : 0;
}

function isWordStart(text: string, index: number): boolean {
  const before = text[index - 1];
  const here = text[index];
  if (/[^\p{L}\p{N}]/u.test(before)) return true;
  return /\p{Ll}/u.test(before) && /\p{Lu}/u.test(here);
}

function isSubsequence(query: string, text: string): boolean {
  let i = 0;
  for (const ch of text) {
    if (ch === query[i]) i++;
    if (i === query.length) return true;
  }
  return false;
}

/**
 * The best of several texts an item can be found by — its title, its
 * subtitle, its keywords. The title counts in full; anything else is a weaker
 * reason to show the item, so it is discounted: "orders" the table outranks a
 * table that merely lives in a schema called orders.
 */
export function bestScore(query: string, title: string, others: readonly string[] = []): number {
  let best = matchScore(query, title);
  for (const other of others) best = Math.max(best, matchScore(query, other) * 0.6);
  return best;
}

/** `items` that match, best first, at most `limit`. Stable for equal scores. */
export function rank<T>(query: string, items: readonly T[], texts: (item: T) => { title: string; others?: readonly string[] }, limit: number): T[] {
  const scored: Array<{ item: T; score: number; index: number }> = [];
  items.forEach((item, index) => {
    const { title, others } = texts(item);
    const score = bestScore(query, title, others);
    if (score > 0) scored.push({ item, score, index });
  });
  scored.sort((a, b) => b.score - a.score || a.index - b.index);
  return scored.slice(0, limit).map((entry) => entry.item);
}
