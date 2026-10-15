import { buildOutline, type OutlineInput, type OutlineRow } from "./buildOutline";

/**
 * How the Outline's rows behave as a tree: which node a row stands for, where
 * the cursor is, who a row's parent is. Pure, so the view only wires keys and
 * clicks to these.
 */

/** The node a row acts on: the method it names, or the method it calls (else the one it is in). */
export function nodeOf(row: OutlineRow): string | null {
  if (row.kind === "method") return row.nodeId;
  if (row.kind === "call") return row.targetId ?? row.ownerId;
  return null;
}

/**
 * Whether a row IS the node, for the selection: a method row naming it, or a
 * call into it. A library call inside the selected method is not.
 */
export function isRowOf(row: OutlineRow, nodeId: string): boolean {
  if (row.kind === "method") return row.nodeId === nodeId;
  if (row.kind === "call") return row.targetId === nodeId;
  return false;
}

/** A row the person can open or close. */
export function isToggleable(row: OutlineRow): boolean {
  return row.kind === "fold" || ((row.kind === "call" || row.kind === "method") && row.expandable);
}

export function isOpenRow(row: OutlineRow): boolean {
  if (row.kind === "fold") return row.open;
  if (row.kind === "call" || row.kind === "method") return row.expandable && row.open;
  return false;
}

/**
 * The cursor's index: the row it was on, by key, so a row opening above it
 * does not move it to another row; else the index it had, kept in range.
 */
export function cursorIndex(rows: readonly OutlineRow[], key: string | null, fallback: number): number {
  if (rows.length === 0) return 0;
  if (key !== null) {
    const at = rows.findIndex((r) => r.key === key);
    if (at >= 0) return at;
  }
  return Math.max(0, Math.min(rows.length - 1, fallback));
}

export type TreeShape = {
  /** Index of each row's parent, -1 for a top row. */
  parent: number[];
  /** aria-level, 1 for a top row. */
  level: number[];
  /** aria-posinset / aria-setsize among the rows of one parent. */
  posinset: number[];
  setsize: number[];
  /**
   * For each level of the row's path, from the top: whether that row is its
   * parent's last child — what TreeGuides needs to know which lines run on.
   */
  lasts: boolean[][];
};

/** Rows are a pre-order walk with depths; a row's parent is the nearest row above it that is less deep. */
export function treeShape(rows: readonly OutlineRow[]): TreeShape {
  const parent: number[] = [];
  const level: number[] = [];
  const posinset: number[] = [];
  const childCount = new Map<number, number>();
  const stack: number[] = [];
  rows.forEach((row, i) => {
    while (stack.length > 0 && rows[stack[stack.length - 1]].depth >= row.depth) stack.pop();
    const p = stack.length > 0 ? stack[stack.length - 1] : -1;
    parent.push(p);
    level.push(p >= 0 ? level[p] + 1 : 1);
    const n = (childCount.get(p) ?? 0) + 1;
    childCount.set(p, n);
    posinset.push(n);
    stack.push(i);
  });
  const setsize = parent.map((p) => childCount.get(p) ?? 1);
  const lasts: boolean[][] = [];
  parent.forEach((p, i) => lasts.push([...(p >= 0 ? lasts[p] : []), posinset[i] === setsize[i]]));
  return { parent, level, posinset, setsize, lasts };
}

/**
 * The rows to open so that the first row of `nodeId` is drawn: its ancestors
 * in the outline with every row open, root first. Empty when the node has no
 * row at all, or it has one with nothing above it to open.
 */
export function pathTo(input: OutlineInput, nodeId: string): string[] {
  const full = buildOutline({ ...input, openAll: true });
  const at = full.findIndex((r) => isRowOf(r, nodeId));
  if (at < 0) return [];
  const { parent } = treeShape(full);
  const keys: string[] = [];
  for (let p = parent[at]; p >= 0; p = parent[p]) if (isToggleable(full[p])) keys.push(full[p].key);
  return keys.reverse();
}

/**
 * The rows that were not there before: what opening a row (or a fold) just
 * revealed, which the view lets arrive with a little motion. Nothing is new on
 * the first rows of an outline — the whole list arriving is not an opening.
 */
export function newKeys(
  previous: readonly OutlineRow[],
  next: readonly OutlineRow[],
  /** The rows drawn right now, first and last index: a revealed row below them is mounted later by a scroll, and does not arrive. */
  window?: readonly [number, number],
): ReadonlySet<string> {
  if (previous.length === 0) return EMPTY_KEYS;
  const before = new Set(previous.map((r) => r.key));
  const [first, last] = window ?? [0, next.length - 1];
  const fresh = new Set<string>();
  for (let i = Math.max(0, first); i <= Math.min(last, next.length - 1); i++) if (!before.has(next[i].key)) fresh.add(next[i].key);
  return fresh.size > 0 ? fresh : EMPTY_KEYS;
}

/** Keys still arriving, plus those a new opening revealed. */
export function mergeKeys(a: ReadonlySet<string>, b: ReadonlySet<string>): ReadonlySet<string> {
  if (b.size === 0) return a;
  if (a.size === 0) return b;
  return new Set([...a, ...b]);
}

export const EMPTY_KEYS: ReadonlySet<string> = new Set();
