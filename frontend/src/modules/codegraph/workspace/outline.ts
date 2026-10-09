import type { CodeGraphOutlineSymbol } from "@/bridge/workspaceCode";

/**
 * The symbol containing a line, and the chain of symbols around it, from a
 * file's outline (declarations nested by line range, as the index recorded
 * them).
 *
 * Siblings are in source order with increasing start lines, so the one that
 * contains a line is found by binary search among them and then, recursively,
 * among its children: O(depth · log n), with nothing scanned and nothing
 * rebuilt when the cursor moves. A sibling set that overlaps (a node that only
 * partly overlaps its neighbour — the outline keeps those as siblings) is
 * handled by stepping back a few siblings from the search hit rather than
 * trusting that the nearest start is the container.
 */
export function symbolPathAt(symbols: readonly CodeGraphOutlineSymbol[], line: number): CodeGraphOutlineSymbol[] {
  const path: CodeGraphOutlineSymbol[] = [];
  let level = symbols;
  for (;;) {
    const hit = containing(level, line);
    if (!hit) return path;
    path.push(hit);
    level = hit.children ?? [];
  }
}

const OVERLAP_LOOKBACK = 4;

function containing(level: readonly CodeGraphOutlineSymbol[], line: number): CodeGraphOutlineSymbol | null {
  // Last sibling whose start is at or before the line.
  let lo = 0;
  let hi = level.length - 1;
  let found = -1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (level[mid].line <= line) {
      found = mid;
      lo = mid + 1;
    } else {
      hi = mid - 1;
    }
  }
  for (let i = found; i >= 0 && i >= found - OVERLAP_LOOKBACK; i--) {
    if (level[i].endLine >= line) return level[i];
  }
  return null;
}

/** Total symbols in an outline, for deciding whether a list needs a filter. */
export function countSymbols(symbols: readonly CodeGraphOutlineSymbol[]): number {
  let n = 0;
  for (const s of symbols) n += 1 + countSymbols(s.children ?? []);
  return n;
}

export type OutlineRow = { symbol: CodeGraphOutlineSymbol; depth: number };

/** The outline flattened to the rows a picker shows, filtered by a name fragment. */
export function outlineRows(symbols: readonly CodeGraphOutlineSymbol[], filter = "", name: (s: CodeGraphOutlineSymbol) => string = (s) => s.label): OutlineRow[] {
  const needle = filter.trim().toLowerCase();
  const rows: OutlineRow[] = [];
  const walk = (list: readonly CodeGraphOutlineSymbol[], depth: number): boolean => {
    let any = false;
    for (const symbol of list) {
      const start = rows.length;
      rows.push({ symbol, depth });
      const childMatch = walk(symbol.children ?? [], depth + 1);
      const selfMatch = !needle || name(symbol).toLowerCase().includes(needle);
      if (selfMatch || childMatch) any = true;
      else rows.length = start;
    }
    return any;
  };
  walk(symbols, 0);
  return rows;
}
