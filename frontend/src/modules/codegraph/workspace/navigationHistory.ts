/**
 * Back / Forward for one editor group.
 *
 * A history is a list of places visited and a pointer at the one the group is
 * showing. The rules (docs/design/codegraph-workspace.md § 7):
 *
 *  - The place being left is *captured* before a deliberate jump (go to
 *    definition, open from search, a breadcrumb), so Back returns to the exact
 *    cursor and scroll, not to the top of the file.
 *  - The place arrived at is *recorded* once it has actually opened.
 *  - Replaying Back/Forward moves the pointer and records nothing.
 *  - A new jump after Back cuts the Forward branch.
 *  - A jump to the line already shown is the same place, not a new one; a jump
 *    to another line of the same file is a new one.
 *  - At most MAX_HISTORY places are kept; the oldest go first.
 *
 * Pure, so every rule is a test.
 */

export type NavPlace = {
  workspaceId: string;
  folderId: string;
  path: string;
  /** 1-based. */
  line: number;
  column: number;
  /** Selection as document offsets, when it is not just the caret. */
  anchor?: number;
  head?: number;
  scrollTop?: number;
  /**
   * Set when the place is a flow document rather than a file: `path` is then
   * empty and the flow is named by its root node. Back and Forward cross both
   * kinds, so a flow opened from a file is a step you can come back from.
   */
  flow?: { root: string; entryPoint?: string; title: string };
};

export type NavHistory = { places: NavPlace[]; index: number };

export const MAX_HISTORY = 200;

export const emptyHistory = (): NavHistory => ({ places: [], index: -1 });

const sameDoc = (a: NavPlace, b: NavPlace) =>
  a.folderId === b.folderId && a.workspaceId === b.workspaceId && a.path === b.path && a.flow?.root === b.flow?.root;

/** The same place for history purposes: the same document and the same line. */
export const samePlace = (a: NavPlace, b: NavPlace) => sameDoc(a, b) && a.line === b.line;

export function current(h: NavHistory): NavPlace | null {
  return h.places[h.index] ?? null;
}

function withPlaces(places: NavPlace[], index: number): NavHistory {
  if (places.length <= MAX_HISTORY) return { places, index };
  const drop = places.length - MAX_HISTORY;
  return { places: places.slice(drop), index: Math.max(0, index - drop) };
}

/**
 * Notes where the group is now, before it leaves. If the current entry is this
 * same place its cursor/scroll are refreshed; if the group has been somewhere
 * the history never heard of (a sidebar click), that place becomes the current
 * entry — after the old ones, forward branch cut — so Back can return to it.
 */
export function capture(h: NavHistory, at: NavPlace): NavHistory {
  const cur = current(h);
  if (cur && samePlace(cur, at)) {
    const places = h.places.slice();
    places[h.index] = at;
    return { places, index: h.index };
  }
  return push(h, at);
}

/** Records a place the group has arrived at, cutting the Forward branch. */
export function push(h: NavHistory, to: NavPlace): NavHistory {
  const kept = h.places.slice(0, h.index + 1);
  const cur = kept[kept.length - 1];
  if (cur && samePlace(cur, to)) {
    kept[kept.length - 1] = to;
    return { places: kept, index: kept.length - 1 };
  }
  kept.push(to);
  return withPlaces(kept, kept.length - 1);
}

export type Step = { history: NavHistory; target: NavPlace };

/**
 * Back: the nearest entry before the pointer that `usable` accepts (a deleted
 * file or a removed folder is skipped, not an error). Call `capture(h, here)`
 * first so Forward can return to the exact place that was left — and so a group
 * that is somewhere the history never heard of lands on the latest recorded
 * place, not the one before it. Null when there is nowhere to go.
 */
export function back(h: NavHistory, usable: (p: NavPlace) => boolean = () => true): Step | null {
  return move(h, -1, usable);
}

export function forward(h: NavHistory, usable: (p: NavPlace) => boolean = () => true): Step | null {
  return move(h, 1, usable);
}

/** How many unusable entries one Back/Forward will step over before giving up. */
const MAX_SKIPS = 25;

function move(h: NavHistory, dir: 1 | -1, usable: (p: NavPlace) => boolean): Step | null {
  let index = h.index;
  let skipped = 0;
  for (;;) {
    index += dir;
    if (index < 0 || index >= h.places.length) return null;
    const target = h.places[index];
    if (usable(target)) return { history: { places: h.places, index }, target };
    if (++skipped > MAX_SKIPS) return null;
  }
}

export const canBack = (h: NavHistory) => h.index > 0;
export const canForward = (h: NavHistory) => h.index >= 0 && h.index < h.places.length - 1;
