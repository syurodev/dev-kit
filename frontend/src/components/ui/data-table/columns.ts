/**
 * Column widths for every resizable table: the database result grid and the
 * shared DataTable. Positions, not names, and pure, so the arithmetic is
 * tested without a DOM.
 */

/** What a column is given before anyone drags it. */
export const DEFAULT_COLUMN_WIDTH_PX = 180;

/**
 * The narrowest a column may be dragged: its header chrome — the datatype
 * glyph, the sort slot and the padding between them — plus a character or two,
 * so a column dragged to nothing can still be found and dragged back.
 */
export const MIN_COLUMN_WIDTH_PX = 64;

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

/**
 * The width a column gets before anyone drags it: what its content needs.
 *
 * Every column starting at DEFAULT_COLUMN_WIDTH_PX gives an `id` holding "5"
 * exactly as much room as a timestamp, so a wide table wastes its width on the
 * narrow columns and scrolls the interesting ones off the right.
 *
 * Measured in characters against the grid's monospace cell font rather than
 * with canvas metrics: the cells are `text-code-12`, so a character is a fixed
 * advance and the arithmetic is `chars * CHAR_PX + padding`. Only the first
 * rows are sampled — a page is up to 5,000 rows, the columns are sized once,
 * and row 4,000 being long is not a reason to make every screen of the table
 * narrower.
 */
const CHAR_PX = 7.3;
const CELL_PADDING_PX = 24;
const SAMPLE_ROWS = 50;
const MAX_MEASURED_WIDTH_PX = 320;

/**
 * What the header spends on things that are not the name: a datatype glyph
 * before it and a sort slot after it, both 12px, with a 6px gap either side —
 * 36, plus 4 of slack.
 *
 * The slack is not padding-by-feel. The name sits in a `flex-1` span, so it
 * gets what is left after the chrome to a fraction of a pixel, and CHAR_PX is
 * an estimate of the font's advance: measured against Geist Mono, "id" wants
 * 14.4px and an exact allowance handed it 14. Every two-letter column in the
 * app rendered as "i…".
 *
 * Charged to the header alone. Sizing every column as if its VALUES needed
 * this room would widen a 74-column table by 32px a column for chrome that is
 * only ever drawn once, at the top.
 */
const HEADER_CHROME_PX = 40;

export function measureColumns(
  columns: readonly string[],
  rows: readonly (readonly (string | null)[])[],
): number[] {
  const sample = rows.slice(0, SAMPLE_ROWS);
  return columns.map((name, index) => {
    // The header is the one cell that must never truncate to nothing, so it is
    // measured alongside the values — but with its own chrome added, not the
    // values'.
    let longest = 0;
    for (const row of sample) {
      const value = row[index];
      if (value && value.length > longest) {
        longest = value.length;
      }
    }
    const wanted = Math.max(
      Math.ceil(name.length * CHAR_PX) + CELL_PADDING_PX + HEADER_CHROME_PX,
      Math.ceil(longest * CHAR_PX) + CELL_PADDING_PX,
    );
    return Math.min(MAX_MEASURED_WIDTH_PX, Math.max(MIN_COLUMN_WIDTH_PX, wanted));
  });
}

/** One drag of `delta` px applied to column `index`: the DataTable's name for resizeColumn. */
export function nextWidths(widths: readonly number[], index: number, delta: number): number[] {
  return resizeColumn(widths, index, delta);
}

/** How far one arrow press moves a focused resize handle; Shift moves four times as far. */
export const KEYBOARD_STEP_PX = 16;

/**
 * The widths after a key press on column `index`'s resize handle, or null when
 * the key is not a resize — the caller then lets it through.
 */
export function keyboardResize(
  widths: readonly number[],
  index: number,
  key: string,
  shift: boolean,
): number[] | null {
  const direction = key === "ArrowRight" ? 1 : key === "ArrowLeft" ? -1 : 0;
  if (direction === 0) return null;
  return nextWidths(widths, index, direction * KEYBOARD_STEP_PX * (shift ? 4 : 1));
}
