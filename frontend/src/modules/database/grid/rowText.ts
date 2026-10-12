import type { QueryResult } from "@/bridge/database";
import { serializeCSV, serializeJSON } from "../export";

/**
 * The text behind "copy a row" and "copy a column".
 *
 * Lives beside the grid because the grid's context menu is the only thing that
 * asks for it now; it was in QueryConsole when the menu was.
 */

/** One column's values, newline separated — the shape that pastes into an IN
 *  list or a spreadsheet. A row shorter than the header becomes an empty line,
 *  which `join` already does for a hole; no guard needed. */
export function columnValues(rows: (string | null)[][], index: number): string {
  return rows.map((row) => row[index] ?? "").join("\n");
}

/**
 * A row is a string array beside a separate column list — JSON has to pair them
 * back up. Values arrive already stringified by the bridge, so this is a shape
 * change, not a re-encode.
 */
export function rowAsJSON(columns: string[], row: (string | null)[]): string {
  return JSON.stringify(
    Object.fromEntries(row.map((value, i) => [columns[i] || `col_${i}`, value])),
    null,
    2,
  );
}

/** TSV in display order, suitable for pasting selected cells or complete rows. */
export function selectionAsTSV(rows: readonly (readonly (string | null)[])[], order: readonly number[], range: { top: number; bottom: number; left: number; right: number }): string {
  const field = (value: string | null | undefined) => {
    const raw = value ?? "";
    // TSV is commonly pasted into spreadsheets. Keep signed numbers numeric,
    // but force formula-like database text to be interpreted as literal text.
    const numeric = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(raw.trim());
    const text = /^\s*[=+@-]/.test(raw) && !numeric ? `'${raw}` : raw;
    return /[\t\r\n"]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
  };
  return rows.slice(Math.max(0, range.top), range.bottom + 1).map((row) =>
    order.slice(range.left, range.right + 1).map((column) => field(row[column])).join("\t"),
  ).join("\n");
}

export type CopyFormat = "tsv" | "csv" | "json";

/** Extract only the selected cells, keeping the grid's displayed column order. */
export function serializeSelection(
  result: QueryResult,
  order: readonly number[],
  range: { top: number; bottom: number; left: number; right: number },
  format: CopyFormat,
): string {
  if (format === "tsv") return selectionAsTSV(result.rows, order, range);
  const selected = selectedResult(result, order, range);
  return format === "json" ? serializeJSON(selected) : serializeCSV(selected);
}

export function selectedResult(result: QueryResult, order: readonly number[], range: {top: number; bottom: number; left: number; right: number}): QueryResult {
  const columns = order.slice(range.left, range.right + 1);
  return {
    columns: columns.map((column) => result.columns[column]),
    rows: result.rows.slice(Math.max(0, range.top), range.bottom + 1)
      .map((row) => columns.map((column) => row[column] ?? null)),
    rowCount: Math.max(0, Math.min(result.rows.length - 1, range.bottom) - Math.max(0, range.top) + 1),
    truncated: false,
  };
}
