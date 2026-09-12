/**
 * Where an arrow key takes the grid's focus.
 *
 * Pure, and separate from the grid for the same reason gridColumns is: this is
 * arithmetic with edges — the last row, the first column, a page near the
 * bottom, the header above row 0 — and every one of those is a place an
 * off-by-one hides. The component keeps the part that cannot be pure: putting
 * the cell on screen.
 */

/** `row` for the header, which is where ArrowUp from row 0 lands. */
export const HEADER_ROW = -1;

export type GridFocus = { row: number; position: number };

export type GridBounds = {
  /** Body rows. Zero is a legal result grid — columns, no rows. */
  rows: number;
  /** Columns on screen. */
  columns: number;
  /** Rows a Page key moves, already reduced by the one kept for context. */
  page: number;
  /** The platform modifier is held: Home and End mean the table, not the row. */
  jump: boolean;
};

/**
 * The key's destination, or null when this grid does not handle the key.
 *
 * Null rather than "no movement" so the caller knows whether to
 * preventDefault: Tab has to keep leaving the grid, and a bare letter has to
 * keep reaching whatever the shell does with it.
 */
export function nextFocus(from: GridFocus, key: string, bounds: GridBounds): GridFocus | null {
  const { rows, columns, page, jump } = bounds;
  switch (key) {
    case "ArrowDown": return clamp({ ...from, row: from.row + 1 }, bounds);
    case "ArrowUp": return clamp({ ...from, row: from.row - 1 }, bounds);
    case "ArrowRight": return clamp({ ...from, position: from.position + 1 }, bounds);
    case "ArrowLeft": return clamp({ ...from, position: from.position - 1 }, bounds);
    case "PageDown": return clamp({ ...from, row: from.row + page }, bounds);
    case "PageUp": return clamp({ ...from, row: from.row - page }, bounds);
    // Home and End are the row's ends; with the platform modifier, the table's.
    case "Home": return clamp({ row: jump ? 0 : from.row, position: 0 }, bounds);
    case "End": return clamp({ row: jump ? rows - 1 : from.row, position: columns - 1 }, bounds);
    default: return null;
  }
}

function clamp(focus: GridFocus, bounds: GridBounds): GridFocus {
  return {
    // HEADER_ROW is the floor, not 0: the header is a row of this grid and the
    // only way to reach a sort from the keyboard.
    row: Math.max(HEADER_ROW, Math.min(bounds.rows - 1, focus.row)),
    position: Math.max(0, Math.min(bounds.columns - 1, focus.position)),
  };
}

/**
 * A rectangle of cells: where the selection started, and where it is now.
 *
 * Two points rather than a set. A set would have to be rebuilt on every arrow
 * press over a 5,000-row page, and it cannot answer "extend from where" —
 * which is the whole of what Shift does.
 */
export type GridSelection = { anchor: GridFocus; focus: GridFocus };

export type GridRange = { top: number; bottom: number; left: number; right: number };

/** The rectangle the two corners describe, inclusive, in either drag order. */
export function selectedRange(selection: GridSelection): GridRange {
  return {
    top: Math.min(selection.anchor.row, selection.focus.row),
    bottom: Math.max(selection.anchor.row, selection.focus.row),
    left: Math.min(selection.anchor.position, selection.focus.position),
    right: Math.max(selection.anchor.position, selection.focus.position),
  };
}

export function inRange(range: GridRange, row: number, position: number): boolean {
  return row >= range.top && row <= range.bottom && position >= range.left && position <= range.right;
}

/** How many cells the selection covers — what a bulk edit is about to change. */
export function rangeSize(range: GridRange): number {
  return (range.bottom - range.top + 1) * (range.right - range.left + 1);
}

/**
 * Where a key takes the selection.
 *
 * `extend` is Shift: the anchor stays and the focus moves, which is what grows
 * a rectangle. Without it both move together, which is what collapses one.
 * Null means the grid does not handle the key — see nextFocus.
 */
export function nextSelection(
  selection: GridSelection,
  key: string,
  bounds: GridBounds,
  extend: boolean,
): GridSelection | null {
  const focus = nextFocus(selection.focus, key, bounds);
  if (!focus) {
    return null;
  }
  // The header is a row of headers, not a corner of a rectangle of data: a
  // selection that reached it would offer to edit the column names.
  if (focus.row === HEADER_ROW) {
    return { anchor: focus, focus };
  }
  return { anchor: extend && selection.anchor.row !== HEADER_ROW ? selection.anchor : focus, focus };
}
