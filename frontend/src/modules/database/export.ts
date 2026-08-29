import { saveExportFile, type QueryResult } from "../../bridge/database";

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
 * Serializes QueryResult into a JSON string where every column value is strictly
 * preserved as a string (e.g. {"id": "42"}), reflecting QueryResult's string[][] representation.
 */
export function serializeJSON(result: QueryResult): string {
  const data = result.rows.map((row) => {
    const record: Record<string, string> = {};
    result.columns.forEach((col, idx) => {
      record[col] = row[idx] ?? "";
    });
    return record;
  });
  return JSON.stringify(data, null, 2);
}

/**
 * Exports a QueryResult by serializing and passing through DBSaveExportFile bridge method,
 * which opens Wails native Save As dialog in Go and writes the file securely.
 * Returns the written file path, or empty string if user cancelled.
 */
export async function exportQueryResult(
  result: QueryResult,
  format: "csv" | "json",
  customFilename?: string
): Promise<string> {
  const isCSV = format === "csv";
  const defaultFilename = customFilename || (isCSV ? "query-result.csv" : "query-result.json");
  const content = isCSV ? serializeCSV(result) : serializeJSON(result);

  return saveExportFile(defaultFilename, content);
}
