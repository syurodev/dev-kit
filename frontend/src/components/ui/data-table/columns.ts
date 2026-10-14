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

/**
 * How a table other than the grid measures: its face's advance, what its
 * header spends beside the name (0 for a DataTable), slack for anything the
 * character count misses, and the widest a measured column may start.
 */
export type MeasureOptions = {
  charPx?: number;
  headerChromePx?: number;
  slackPx?: number;
  maxPx?: number;
};

export function measureColumns(
  columns: readonly string[],
  rows: readonly (readonly (string | null)[])[],
  {
    charPx = CHAR_PX,
    headerChromePx = HEADER_CHROME_PX,
    slackPx = 0,
    maxPx = MAX_MEASURED_WIDTH_PX,
  }: MeasureOptions = {},
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
      Math.ceil(name.length * charPx) + CELL_PADDING_PX + headerChromePx,
      Math.ceil(longest * charPx) + CELL_PADDING_PX,
    ) + slackPx;
    return Math.min(maxPx, Math.max(MIN_COLUMN_WIDTH_PX, wanted));
  });
}

/**
 * The widest a DataTable column may be dragged. The grid has no ceiling and
 * keeps none; a table in a card or in prose needs one for its handle to state
 * a range.
 */
export const MAX_COLUMN_WIDTH_PX = 1200;

/** One resize of `delta` px on column `index`, held between the minimum and the maximum. */
export function nextWidths(widths: readonly number[], index: number, delta: number): number[] {
  const next = resizeColumn(widths, index, delta);
  next[index] = Math.min(MAX_COLUMN_WIDTH_PX, next[index]);
  return next;
}

/**
 * The widths while a handle is dragged: the width the column had when the drag
 * began plus how far the pointer has moved since, then clamped. Adding each
 * move to an already-clamped width would leave the edge behind the pointer
 * after a drag past the minimum and back.
 */
export function dragWidths(
  from: readonly number[],
  index: number,
  startX: number,
  x: number,
): number[] {
  return nextWidths(from, index, x - startX);
}

/**
 * A sans cell's advance. Body text is proportional, so this is a generous
 * average for text-body-14 rather than an exact one: a column a little wide is
 * a column, one a little narrow cuts its longest value.
 */
const SANS_CHAR_PX = 8;
/** What a sans cell spends besides its letters: inline-code chips' padding, mostly. */
const SANS_SLACK_PX = 16;
/** A column that never wraps may start this wide before it truncates. */
const NOWRAP_MAX_WIDTH_PX = 640;

/**
 * The width a DataTable column starts at, from its header and values. No
 * header chrome (a DataTable header is only its name and the handle); sans
 * columns measured as sans; a column that cannot wrap allowed past the wrap cap,
 * since past its width it can only be cut.
 */
export function tableColumnWidth(
  label: string,
  values: readonly (string | null)[],
  { mono = false, nowrap = false }: { mono?: boolean; nowrap?: boolean },
): number {
  const [width] = measureColumns(
    [label],
    values.map((value) => [value]),
    {
      charPx: mono ? CHAR_PX : SANS_CHAR_PX,
      headerChromePx: 0,
      slackPx: mono ? 0 : SANS_SLACK_PX,
      maxPx: nowrap ? NOWRAP_MAX_WIDTH_PX : MAX_MEASURED_WIDTH_PX,
    },
  );
  return width;
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

/**
 * Starting widths shrunk to fit `available` px, so a table whose content asks
 * for more than the room does not push its last column past the edge — a
 * measured 640px value would otherwise leave a timestamp after it cut in half.
 *
 * Width comes off the widest columns first, levelled so two wide columns share
 * the cut rather than one emptying, and off columns that wrap before columns
 * that can only be cut (`wraps`): a narrower wrapping cell still says all of
 * it. No column goes below its floor (`min`, by position — the header's own
 * width, at least MIN_COLUMN_WIDTH_PX) or grows; past every floor the table is
 * as narrow as it gets and scrolls.
 */
export function fitWidths(
  widths: readonly number[],
  available: number,
  { min, wraps }: { min: readonly number[]; wraps: readonly boolean[] },
): number[] {
  const next = [...widths];
  if (!(available > 0)) return next;
  const floors = widths.map((width, index) => Math.min(width, min[index] ?? MIN_COLUMN_WIDTH_PX));
  let excess = next.reduce((sum, width) => sum + width, 0) - available;
  const passes = [
    widths.map((_, index) => wraps[index] === true),
    widths.map(() => true),
  ];
  for (const inPass of passes) {
    if (excess <= 0) break;
    excess -= level(next, floors, inPass, excess);
  }
  return next;
}

/**
 * Lowers the widest of the chosen columns to one level, the highest that frees
 * at least `excess` px (or every floor, when none does), in place. Returns how
 * much it freed.
 */
function level(
  widths: number[],
  floors: readonly number[],
  chosen: readonly boolean[],
  excess: number,
): number {
  const freedAt = (at: number) =>
    widths.reduce(
      (sum, width, index) => (chosen[index] ? sum + Math.max(0, width - Math.max(at, floors[index])) : sum),
      0,
    );
  // The highest whole-pixel level that frees enough; freedAt falls as it rises.
  let low = 0;
  let high = Math.max(0, ...widths.map(Math.floor));
  while (low < high) {
    const mid = Math.ceil((low + high) / 2);
    if (freedAt(mid) >= excess) low = mid;
    else high = mid - 1;
  }
  const freed = freedAt(low);
  widths.forEach((width, index) => {
    if (chosen[index]) widths[index] = Math.min(width, Math.max(low, floors[index]));
  });
  return freed;
}
