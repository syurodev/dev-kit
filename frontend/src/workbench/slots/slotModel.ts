/**
 * The shell's slot layout (docs/dev-kit/layout.mdx, option B): panes move
 * between a few named slots around the document, and nowhere else.
 *
 *   ┌──────────┬──────────┬──────────┐
 *   │          │ a.top    │ b.top    │
 *   │ document ├──────────┼──────────┤
 *   │          │ a.bottom │ b.bottom │
 *   ├──────────┼──────────┴──────────┘
 *   │ bottom   │
 *   └──────────┘
 *
 * The document is not a slot: it never moves and nothing docks into it. An
 * empty slot takes no space — an empty column is not drawn at all, and a
 * column holding one pane gives it the full height.
 *
 * Pure: the shell renders what `resolve` says and hit-tests with `dropTarget`,
 * so every rule here is testable without a DOM.
 */

export type ColumnId = "a" | "b";
export type SlotId = "bottom" | `${ColumnId}.${"top" | "bottom"}`;
export const SLOTS: readonly SlotId[] = ["a.top", "a.bottom", "b.top", "b.bottom", "bottom"];

/**
 * `agent` and `terminal` are companions: their place is global, and a module
 * switch never moves them. Every other pane is a module's context pane —
 * whatever that module passed to openPanel — named `context:<moduleId>`
 * (contextPane), one per module on screen, its place remembered per module.
 */
export type PaneId = string;
export const COMPANIONS: readonly PaneId[] = ["agent", "terminal"];
export const contextPane = (moduleId: string): PaneId => `context:${moduleId}`;
export const moduleOfPane = (pane: PaneId): string | null =>
  pane.startsWith("context:") ? pane.slice("context:".length) : null;

export type Placement = Record<PaneId, SlotId>;
export type OpenPanes = Record<PaneId, boolean | undefined>;

/** Where a pane goes before anyone has moved it: today's shell. */
export const DEFAULT_SLOT: Record<string, SlotId> = { agent: "a.bottom", terminal: "bottom" };
export const defaultSlotOf = (pane: PaneId): SlotId => DEFAULT_SLOT[pane] ?? "a.top";

/** Today's shell, so nothing moves for someone who never drags a pane. */
export const DEFAULT_PLACEMENT: Placement = { context: "a.top", agent: "a.bottom", terminal: "bottom" };

export const columnOf = (slot: SlotId): ColumnId | null => (slot === "bottom" ? null : (slot[0] as ColumnId));
const sibling = (slot: SlotId): SlotId | null =>
  slot === "bottom" ? null : (`${slot[0]}.${slot.endsWith("top") ? "bottom" : "top"}` as SlotId);

/** What is on screen: the open panes, each in the slot it is drawn in. */
export type Resolved = Partial<Record<SlotId, PaneId>>;

/**
 * Seats the open panes. Two can claim one slot — a context pane remembered for
 * a module can point where a companion has since been moved — and companions
 * win, because they are the ones that must not move on a module switch. Among
 * context panes, the one listed first in `open` wins (the caller lists the
 * focused module's first). The loser takes the nearest free slot: its
 * column's other half first.
 */
export function resolve(placement: Placement, open: OpenPanes): Resolved {
  const out: Resolved = {};
  const order: PaneId[] = [...COMPANIONS, ...Object.keys(open).filter((p) => !COMPANIONS.includes(p))];
  for (const pane of order) {
    if (!open[pane]) continue;
    const want = placement[pane] ?? defaultSlotOf(pane);
    const fallbacks = [want, sibling(want), ...SLOTS].filter((s): s is SlotId => s !== null);
    const slot = fallbacks.find((s) => out[s] === undefined);
    if (slot) out[slot] = pane;
  }
  return out;
}

export const slotOf = (resolved: Resolved, pane: PaneId): SlotId | null =>
  (Object.keys(resolved) as SlotId[]).find((s) => resolved[s] === pane) ?? null;

export function columnVisible(resolved: Resolved, column: ColumnId): boolean {
  return resolved[`${column}.top`] !== undefined || resolved[`${column}.bottom`] !== undefined;
}

/**
 * Moves `pane` to `to`. Whatever sat there — open or not — swaps into the
 * pane's old slot, so a closed pane never comes back on top of the one that
 * replaced it.
 */
export function move(placement: Placement, pane: PaneId, to: SlotId): Placement {
  const from = placement[pane] ?? defaultSlotOf(pane);
  if (from === to) return placement;
  const next = { ...placement, [pane]: to };
  for (const other of Object.keys(placement)) {
    if (other !== pane && placement[other] === to) next[other] = from;
  }
  return next;
}

/**
 * The arrangement as the eye sees it: columns in order, empty ones dropped.
 * Two placements with the same shape look identical, which is how a drop that
 * would change nothing is recognised.
 */
function shape(resolved: Resolved): string {
  const cols = (["a", "b"] as const)
    .map((c) => [resolved[`${c}.top`], resolved[`${c}.bottom`]].filter(Boolean).join("/"))
    .filter(Boolean);
  return `${cols.join("|")}#${resolved.bottom ?? ""}`;
}

export function changesLayout(placement: Placement, open: OpenPanes, pane: PaneId, to: SlotId): boolean {
  return shape(resolve(placement, open)) !== shape(resolve(move(placement, pane, to), open));
}

// ── Hit testing ─────────────────────────────────────────────────────────────

export type Rect = { left: number; top: number; right: number; bottom: number };

/** Rects of what is on screen, read once when a drag starts. */
export type Geometry = {
  row: Rect;
  document: Rect;
  columns: Partial<Record<ColumnId, Rect>>;
  /** One per occupied slot. */
  cards: Partial<Record<SlotId, Rect>>;
};

export type DropTarget = { slot: SlotId; rect: Rect; label: string };

/** How far in from an edge a new column or row can be opened. */
export const EDGE_BAND = 64;
/** The share of a hovered column or row that the hint claims for a split. */
const NEW_COLUMN_WIDTH = 0.28;
const NEW_ROW_HEIGHT = 0.3;

export const PANE_TITLES: Record<PaneId, string> = { context: "Panel", agent: "Agent", terminal: "Terminal" };
const titleOf = (titles: Partial<Record<PaneId, string>>, pane: PaneId) => titles[pane] ?? pane;

const inside = (r: Rect, x: number, y: number) => x >= r.left && x <= r.right && y >= r.top && y <= r.bottom;
const upper = (r: Rect): Rect => ({ ...r, bottom: r.top + (r.bottom - r.top) / 2 });
const lower = (r: Rect): Rect => ({ ...r, top: r.top + (r.bottom - r.top) / 2 });

/**
 * The slot under the pointer while `pane` is dragged, or null where a drop
 * would do nothing: over the document's body, over the pane's own place, or
 * anywhere the result would look the same as now.
 *
 * Checked in this order, most specific first:
 *   1. the band at the edge where a missing column would open;
 *   2. a column — upper half is its top slot, lower half its bottom;
 *   3. the bottom row, or the band along the document's foot that opens it.
 */
export function dropTarget(opts: {
  placement: Placement;
  open: OpenPanes;
  pane: PaneId;
  geometry: Geometry;
  x: number;
  y: number;
  titles?: Partial<Record<PaneId, string>>;
}): DropTarget | null {
  const { placement, open, pane, geometry: g, x, y } = opts;
  const titles = { ...PANE_TITLES, ...opts.titles };
  const resolved = resolve(placement, open);
  const accept = (slot: SlotId, rect: Rect, label: string): DropTarget | null =>
    changesLayout(placement, open, pane, slot) ? { slot, rect, label } : null;
  const occupantLabel = (slot: SlotId, fallback: string) => {
    const at = resolved[slot];
    return at && at !== pane ? `Swap with ${titleOf(titles, at)}` : fallback;
  };

  // 1. A new column. The missing one opens where it will be drawn: B after A
  // at the row's right edge, A before B at the document's right edge.
  const visible = (["a", "b"] as const).filter((c) => columnVisible(resolved, c));
  if (visible.length < 2) {
    const missing: ColumnId = visible.includes("a") ? "b" : "a";
    const opensAtRowEdge = missing === "b" || visible.length === 0;
    const edge = opensAtRowEdge ? g.row.right : g.document.right;
    if (x >= edge - EDGE_BAND && x <= edge + EDGE_BAND / 4 && y >= g.row.top && y <= g.row.bottom) {
      const width = (g.row.right - g.row.left) * NEW_COLUMN_WIDTH;
      const rect = { left: edge - width, top: g.row.top, right: edge, bottom: g.row.bottom };
      const hit = accept(`${missing}.top`, rect, "New column");
      if (hit) return hit;
    }
  }

  // 2. A column.
  for (const c of visible) {
    const col = g.columns[c];
    if (!col || !inside(col, x, y)) continue;
    const slot: SlotId = y < col.top + (col.bottom - col.top) / 2 ? `${c}.top` : `${c}.bottom`;
    const occupied = resolved[slot] !== undefined;
    const card = g.cards[slot];
    const rect = occupied && card ? card : slot.endsWith("top") ? upper(col) : lower(col);
    return accept(slot, rect, occupantLabel(slot, slot.endsWith("top") ? "Split above" : "Split below"));
  }

  // 3. The bottom row.
  const bottomCard = g.cards.bottom;
  if (bottomCard && inside(bottomCard, x, y)) {
    return accept("bottom", bottomCard, occupantLabel("bottom", "Below the document"));
  }
  if (!bottomCard && inside(g.document, x, y)) {
    const h = g.document.bottom - g.document.top;
    const band = Math.max(EDGE_BAND, h * NEW_ROW_HEIGHT);
    if (y >= g.document.bottom - band) {
      const rect = { ...g.document, top: g.document.bottom - h * NEW_ROW_HEIGHT };
      return accept("bottom", rect, "Below the document");
    }
  }
  return null;
}

/** The same destinations as a drag, for the pane's Move to… menu. */
export function menuTargets(
  placement: Placement,
  open: OpenPanes,
  pane: PaneId,
  titles: Partial<Record<PaneId, string>> = {},
): { slot: SlotId; label: string; current: boolean }[] {
  const t = { ...PANE_TITLES, ...titles };
  const resolved = resolve(placement, open);
  const here = slotOf(resolved, pane);
  const visible = (["a", "b"] as const).filter((c) => columnVisible(resolved, c));
  const out: { slot: SlotId; label: string; current: boolean }[] = [];
  const add = (slot: SlotId, label: string) => {
    const current = slot === here;
    if (!current && !changesLayout(placement, open, pane, slot)) return;
    const at = resolved[slot];
    out.push({ slot, current, label: at && at !== pane ? `${label} · swap with ${titleOf(t, at)}` : label });
  };
  add("bottom", "Below the document");
  visible.forEach((c, i) => {
    const name = visible.length > 1 ? `Column ${i + 1}` : "Right column";
    add(`${c}.top`, `${name} · top`);
    add(`${c}.bottom`, `${name} · bottom`);
  });
  if (visible.length < 2) add(visible.includes("a") ? "b.top" : "a.top", "New column");
  return out;
}
