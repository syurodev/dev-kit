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
