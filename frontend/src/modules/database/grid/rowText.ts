/**
 * The text behind "copy a row" and "copy a column".
 *
 * Lives beside the grid because the grid's context menu is the only thing that
 * asks for it now; it was in QueryConsole when the menu was.
 */

/** One column's values, newline separated — the shape that pastes into an IN
 *  list or a spreadsheet. A row shorter than the header becomes an empty line,
 *  which `join` already does for a hole; no guard needed. */
export function columnValues(rows: string[][], index: number): string {
  return rows.map((row) => row[index]).join("\n");
}

/**
 * A row is a string array beside a separate column list — JSON has to pair them
 * back up. Values arrive already stringified by the bridge, so this is a shape
 * change, not a re-encode.
 */
export function rowAsJSON(columns: string[], row: string[]): string {
  return JSON.stringify(
    Object.fromEntries(row.map((value, i) => [columns[i] || `col_${i}`, value])),
    null,
    2,
  );
}
