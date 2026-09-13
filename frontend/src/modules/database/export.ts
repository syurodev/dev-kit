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
 * Serializes QueryResult into a JSON string where every value is kept as the
 * string it arrived as (e.g. {"id": "42"}), and SQL NULL is null (#47) rather
 * than an empty string nobody could tell from a real one.
 */
export function serializeJSON(result: QueryResult): string {
  const data = result.rows.map((row) => {
    const record: Record<string, string | null> = {};
    result.columns.forEach((col, idx) => {
      record[col] = row[idx] ?? null;
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

/**
 * The filename to suggest when exporting a table tab's page.
 *
 * The table's own name, which is what someone browsing it would type anyway,
 * minus anything a filesystem would refuse. An identifier is not a filename:
 * both Postgres and MySQL allow a quoted name containing `/`, and a suggested
 * name carrying one is a save dialog that either fails or writes somewhere
 * nobody asked for. A name that sanitises away to nothing falls back rather
 * than proposing the bare extension.
 */
export function tableExportFilename(table: string): string {
  const cleaned = table
    .trim()
    // Denied, not allowed: path separators, what Windows reserves, and control
    // codes. An allowlist of ASCII would turn `caf\u00e9` into `caf`, and a name
    // mangled past recognition is its own small lie about what was exported.
    .replace(/[/\\:*?"<>|\u0000-\u001f]+/g, "-")
    .replace(/\s+/g, "-")
    // A leading dot hides the file; a trailing one runs into the extension we
    // append. `..` survives neither, which is the point.
    .replace(/^[-.]+/, "")
    .replace(/[-.]+$/, "");
  return cleaned || "table-rows";
}
