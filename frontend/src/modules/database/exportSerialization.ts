import type { QueryResult } from "@/bridge/database";

/**
 * Escapes a cell value per RFC 4180.
 * If the cell contains comma, quote, or newline, wrap in quotes and escape quotes with double quotes.
 */
export function escapeCSVCell(value: string | null | undefined): string {
  if (value == null) {
    return "";
  }
  const str = String(value);
  if (
    str.includes(",") ||
    str.includes('"') ||
    str.includes("\n") ||
    str.includes("\r")
  ) {
    return `"${str.replace(/"/g, '""')}"`;
  }
  return str;
}

/**
 * Serializes QueryResult into an RFC 4180 compliant CSV string.
 */
export function serializeCSV(result: QueryResult): string {
  const header = result.columns.map(escapeCSVCell).join(",");
  const rows = result.rows.map((row) => row.map(escapeCSVCell).join(","));
  return [header, ...rows].join("\r\n");
}

/**
 * Serializes QueryResult into a JSON string where every value is kept as the
 * string it arrived as (e.g. {"id": "42"}), and SQL NULL is null (#47) rather
 * than an empty string nobody could tell from a real one.
 */
export function serializeJSON(result: QueryResult): string {
  const data = result.rows.map((row) => {
    const record: Record<string, string | null> = Object.create(null);
    result.columns.forEach((col, idx) => {
      record[col] = row[idx] ?? null;
    });
    return record;
  });
  return JSON.stringify(data, null, 2);
}
