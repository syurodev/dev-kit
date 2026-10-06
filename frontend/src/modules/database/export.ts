import { saveExportFile, type QueryResult } from "../../bridge/database";

import { serializeCSV, serializeJSON } from "./exportSerialization";
export {
  escapeCSVCell,
  serializeCSV,
  serializeJSON,
} from "./exportSerialization";
/**
 * Exports a QueryResult by serializing and passing through DBSaveExportFile bridge method,
 * which opens Wails native Save As dialog in Go and writes the file securely.
 * Returns the written file path, or empty string if user cancelled.
 */
export async function exportQueryResult(
  result: QueryResult,
  format: "csv" | "json",
  customFilename?: string,
): Promise<string> {
  const isCSV = format === "csv";
  const defaultFilename =
    customFilename || (isCSV ? "query-result.csv" : "query-result.json");
  if (
    format === "json" &&
    new Set(result.columns).size !== result.columns.length
  )
    throw new Error(
      "JSON needs unique column names. Alias duplicate columns in SQL or export CSV.",
    );
  const content = await serializeExport(result, format);

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

/** A suggestion is a basename; destination selection belongs to the native picker. */
export function exportFilenameError(value: string): string {
  const name = value.trim();
  if (!name || !name.replace(/\.(csv|json)$/i, "")) return "Enter a filename.";
  if (/[/\\:*?"<>|\u0000-\u001f]/.test(name) || /^\.+$/.test(name))
    return "Use a filename without path separators or reserved characters.";
  return "";
}

/** Large loaded results serialize away from the UI thread; a worker lives for one export. */
export function serializeExport(
  result: QueryResult,
  format: "csv" | "json",
): Promise<string> {
  let characters = 0;
  const large =
    result.rows.length >= 1000 ||
    result.rows.some((row) =>
      row.some((value) => (characters += value?.length ?? 0) > 128 * 1024),
    );
  if (!large || typeof Worker === "undefined")
    return Promise.resolve(
      format === "csv" ? serializeCSV(result) : serializeJSON(result),
    );
  return new Promise((resolve, reject) => {
    const worker = new Worker(new URL("./export.worker.ts", import.meta.url), {
      type: "module",
    });
    worker.onmessage = (e) => {
      worker.terminate();
      if (e.data.error) reject(new Error(e.data.error));
      else resolve(e.data.content);
    };
    worker.onerror = (e) => {
      worker.terminate();
      reject(new Error(e.message || "Could not prepare export."));
    };
    try {
      worker.postMessage({ result, format });
    } catch (e) {
      worker.terminate();
      reject(e);
    }
  });
}
