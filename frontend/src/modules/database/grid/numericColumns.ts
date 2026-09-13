/**
 * Which columns hold numbers, decided from the values.
 *
 * A number belongs on the right. Scanning a column of ids for the one that
 * is four digits instead of three is what right alignment is FOR — the
 * digits line up by place value, so the odd one out has a different length
 * and the eye finds it without reading. Left-aligned, every row starts at
 * the same x and the difference is at the ragged end nobody looks at.
 *
 * From the values because a query result has no types. The bridge sends
 * columns and rows of strings — no `int4`, no `numeric` — and inventing a
 * type system for the console to guess with would be a larger lie than
 * reading what is actually there.
 *
 * A column counts as numeric only when EVERY value present parses as one.
 * One "n/a" in a column of counts and it goes back to the left, because a
 * column that is right-aligned except for three rows is worse than one that
 * never claimed to be.
 */

/** Enough rows to be sure without walking a 10,000-row result. */
const SAMPLE = 200;

export function numericColumns(
  rows: readonly (readonly (string | null)[])[],
  columnCount: number,
): boolean[] {
  const numeric = new Array<boolean>(columnCount).fill(false);
  const seen = new Array<boolean>(columnCount).fill(false);

  const limit = Math.min(rows.length, SAMPLE);
  for (let index = 0; index < columnCount; index += 1) {
    let allNumbers = true;
    for (let row = 0; row < limit && allNumbers; row += 1) {
      const value = rows[row]?.[index];
      // A column of numbers with NULLs in it is still a column of numbers.
      if (value == null) continue;
      seen[index] = true;
      if (!isNumeric(value)) allNumbers = false;
    }
    numeric[index] = allNumbers && seen[index];
  }
  return numeric;
}

/**
 * A whole value that is a number, in the shapes a database actually sends:
 * integers, decimals, a leading sign, scientific notation from a float
 * column. Deliberately not Number() alone, which says "" and " " and "0x1f"
 * and "Infinity" are numbers.
 */
function isNumeric(value: string): boolean {
  return /^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$/.test(value.trim());
}
