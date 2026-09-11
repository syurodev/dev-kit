/**
 * A result grid's column order and widths.
 *
 * Positions, not names: a result can repeat a column name — `SELECT a.id,
 * b.id` — and a map keyed by name would collapse the two. Kept pure so the
 * arithmetic can be tested without a DOM, the way tabs/tabWidths is.
 */

/** What a column is given before anyone drags it. */
export const DEFAULT_COLUMN_WIDTH_PX = 180;

/**
 * The narrowest a column may be dragged: enough for the sort caret and a
 * character or two, so a column dragged to nothing can still be dragged back.
 */
export const MIN_COLUMN_WIDTH_PX = 48;

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

/** Applies a drag of `delta` px to one column, leaving the rest untouched. */
export function resizeColumn(
  widths: readonly number[],
  index: number,
  delta: number,
): number[] {
  const next = widths.length > 0 ? [...widths] : [];
  while (next.length <= index) {
    next.push(DEFAULT_COLUMN_WIDTH_PX);
  }
  next[index] = Math.max(MIN_COLUMN_WIDTH_PX, (next[index] ?? DEFAULT_COLUMN_WIDTH_PX) + delta);
  return next;
}
