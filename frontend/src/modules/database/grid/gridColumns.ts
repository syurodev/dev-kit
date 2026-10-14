/**
 * A result grid's column order and widths.
 *
 * Positions, not names: a result can repeat a column name — `SELECT a.id,
 * b.id` — and a map keyed by name would collapse the two. Kept pure so the
 * arithmetic can be tested without a DOM, the way tabs/tabStripModel is.
 *
 * The width rules themselves are shared with every other resizable table and
 * live in components/ui/data-table/columns; they are re-exported here so the
 * grid's imports do not move.
 */

export {
  DEFAULT_COLUMN_WIDTH_PX,
  MIN_COLUMN_WIDTH_PX,
  measureColumns,
  resizeColumn,
} from "@/components/ui/data-table/columns";

/**
 * The display order for `columns`, or the natural order when the saved one
 * does not describe this result.
 *
 * A console runs a different query every time, and an order saved against the
 * last one would show these columns in an arrangement nobody chose. Length is
 * the cheap check that catches it.
 */
export function orderedColumns(columns: readonly string[], saved: readonly number[]): number[] {
  const natural = columns.map((_, index) => index);
  if (saved.length !== columns.length) {
    return natural;
  }
  const seen = new Set(saved);
  if (seen.size !== saved.length || saved.some((index) => index < 0 || index >= columns.length)) {
    return natural;
  }
  return [...saved];
}

/** Moves the column at `from` to `to`, keeping every other column's order. */
export function reorderColumn(order: readonly number[], from: number, to: number): number[] {
  if (from === to || from < 0 || to < 0 || from >= order.length || to >= order.length) {
    return [...order];
  }
  const next = [...order];
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}

/**
 * The row-number gutter, in px. Fixed rather than measured: it is sticky at
 * the left edge, so the frozen columns beside it are positioned FROM it, and a
 * width that changed with the page would move them mid-scroll.
 *
 * 44 with 12px of padding leaves 32 for digits — five at the grid's monospace
 * 11px, which covers a page starting at row 99,999. Past that the number
 * overflows its own column rather than the table growing a wider gutter on
 * every table for the one that needed it.
 */
export const ROW_NUMBER_WIDTH_PX = 44;

/**
 * Where each frozen column sits while the grid scrolls sideways.
 *
 * `frozen` is a count, not a set: freezing means "keep the first n columns of
 * the display order on screen", the way a spreadsheet freezes panes. A set of
 * arbitrary columns would have to answer what happens when you drag one of
 * them out of the frozen run, and nobody wants two answers to that.
 *
 * Returns the `left` offset for each frozen column in DISPLAY order — the
 * widths of everything frozen before it — and nothing for the rest. Sticky
 * offsets have to be absolute pixels: `left: 0` on all of them stacks every
 * frozen column on the same edge.
 */
export function frozenOffsets(
  order: readonly number[],
  widthOf: (index: number) => number,
  frozen: number,
  /** Where the frozen run starts — the row-number gutter is sticky before it. */
  base = 0,
): (number | undefined)[] {
  const offsets: (number | undefined)[] = [];
  let left = base;
  for (const [position, index] of order.entries()) {
    if (position >= frozen) {
      offsets.push(undefined);
      continue;
    }
    offsets.push(left);
    left += widthOf(index);
  }
  return offsets;
}

/**
 * Pins a column, or unpins one already pinned.
 *
 * Pinning MOVES the column: it joins the pinned run at the left edge rather
 * than dragging every column between it and the edge into the run with it.
 * "Freeze the first n columns" is the spreadsheet model, and on a 74-column
 * table where the column you care about sits at position 40 it pins forty
 * columns you did not ask for.
 *
 * Unpinning drops it out of the run and leaves it as the first column outside
 * it. Its original position is not restored — the order is the user's to drag,
 * and guessing where a column "came from" is a guess it would have to keep.
 */
export function pinColumn(
  order: readonly number[],
  frozen: number,
  columnIndex: number,
): { order: number[]; frozen: number } {
  const position = order.indexOf(columnIndex);
  if (position === -1) {
    return { order: [...order], frozen };
  }
  if (position < frozen) {
    return { order: reorderColumn(order, position, frozen - 1), frozen: frozen - 1 };
  }
  // To the end of the pinned run, not to 0: pins accumulate left to right in
  // the order they were made, so pinning a second column does not shove the
  // first one out of the place you just put it.
  return { order: reorderColumn(order, position, frozen), frozen: frozen + 1 };
}
