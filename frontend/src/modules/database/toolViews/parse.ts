import type { PlanNode, QueryPlan } from "@/bridge/database";
import { parseToolOutput } from "@/workbench/toolResultViews";
import { serializeCSV } from "../export";

/**
 * What the database tools return, read for the transcript's cards
 * (docs/dev-kit/tool-result-view.mdx). Pure and synchronous: the manifest runs
 * these before a card's chunk loads, and anything that is not the shape —
 * a cut preview, another tool's answer — is null, which falls back to the
 * generic block rather than to a card guessing at it.
 *
 * Go's JSON is snake_case; the module's own types are camelCase, so a plan
 * read here draws with the same planGraph the console uses.
 */

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** A Go slice: an array, or null for a nil one. Anything else is not one. */
function sliceOf(value: unknown): unknown[] | null {
  if (value === null || value === undefined) return [];
  return Array.isArray(value) ? value : null;
}

/** A number Go may have left out (omitempty) — 0 then — or null when it is something else. */
function numberOr0(value: unknown): number | null {
  if (value === undefined || value === null) return 0;
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

export type ParsedQueryResult = {
  columns: string[];
  /** Each cell as text, null for SQL NULL. */
  rows: (string | null)[][];
  rowCount: number;
  truncated: boolean;
  /** What a write changed; -1 when the driver cannot say. */
  rowsAffected: number;
};

/** `db.query` → `{columns, rows, row_count, truncated, rows_affected}`. */
export function parseQueryResult(rawOutput: unknown): ParsedQueryResult | null {
  const value = parseToolOutput(rawOutput);
  if (!isRecord(value) || !("columns" in value) || !("rows" in value)) return null;
  const columns = sliceOf(value.columns);
  const rows = sliceOf(value.rows);
  if (!columns || !rows) return null;
  if (!columns.every((column) => typeof column === "string")) return null;
  for (const row of rows) {
    if (!Array.isArray(row)) return null;
    if (!row.every((cell) => cell === null || typeof cell === "string")) return null;
  }
  const rowCount = numberOr0(value.row_count);
  const rowsAffected = value.rows_affected === undefined ? -1 : numberOr0(value.rows_affected);
  if (rowCount === null || rowsAffected === null) return null;
  return {
    columns: columns as string[],
    rows: rows as (string | null)[][],
    rowCount,
    truncated: value.truncated === true,
    rowsAffected,
  };
}

function planNodeOf(value: unknown): PlanNode | null {
  if (!isRecord(value) || typeof value.operation !== "string") return null;
  const depth = numberOr0(value.depth);
  const estimatedRows = numberOr0(value.estimated_rows);
  const actualRows = numberOr0(value.actual_rows);
  const selfMillis = numberOr0(value.self_millis);
  if (depth === null || estimatedRows === null || actualRows === null || selfMillis === null) return null;
  if (value.detail !== undefined && value.detail !== null && typeof value.detail !== "string") return null;
  const rawChildren = sliceOf(value.children);
  if (!rawChildren) return null;
  const children: PlanNode[] = [];
  for (const child of rawChildren) {
    const node = planNodeOf(child);
    if (!node) return null;
    children.push(node);
  }
  return {
    operation: value.operation,
    depth,
    estimatedRows,
    actualRows,
    selfMillis,
    detail: typeof value.detail === "string" ? value.detail : "",
    children,
  };
}

/** `db.explain` → `{nodes, raw, analyzed}`; a plan with no node has nothing to draw. */
export function parsePlanResult(rawOutput: unknown): QueryPlan | null {
  const value = parseToolOutput(rawOutput);
  if (!isRecord(value) || !Array.isArray(value.nodes) || value.nodes.length === 0) return null;
  const nodes: PlanNode[] = [];
  for (const raw of value.nodes) {
    const node = planNodeOf(raw);
    if (!node) return null;
    nodes.push(node);
  }
  return { nodes, raw: typeof value.raw === "string" ? value.raw : "", analyzed: value.analyzed === true };
}

export type SchemaColumn = {
  name: string;
  /** "" when the index holds the name only. */
  dataType: string;
  /** null when the index does not say. */
  nullable: boolean | null;
};

export type SchemaTable = { name: string; columns: SchemaColumn[] };

export type ParsedSchema = { schemas: { name: string; tables: SchemaTable[] }[] };

const byName = (a: { name: string }, b: { name: string }) => a.name.localeCompare(b.name);

/** One table's column detail from `columns`, in table order; null when malformed. */
function detailOf(value: unknown): SchemaColumn[] | null {
  const list = sliceOf(value);
  if (!list) return null;
  const columns: { column: SchemaColumn; position: number }[] = [];
  for (const raw of list) {
    if (!isRecord(raw) || typeof raw.name !== "string") return null;
    const position = numberOr0(raw.position);
    if (position === null) return null;
    columns.push({
      column: {
        name: raw.name,
        dataType: typeof raw.data_type === "string" ? raw.data_type : "",
        nullable: typeof raw.nullable === "boolean" ? raw.nullable : null,
      },
      position,
    });
  }
  return columns.sort((a, b) => a.position - b.position).map((entry) => entry.column);
}

/**
 * `db.schema.get` → `{namespace: {schema: {table: [column]}}, columns:
 * {schema: {table: [{name, data_type, nullable, position}]}}, indexed_at}`.
 * The namespace says what exists; `columns`, when it has the table, says
 * what each column is.
 */
export function parseSchemaResult(rawOutput: unknown): ParsedSchema | null {
  const value = parseToolOutput(rawOutput);
  if (!isRecord(value) || !isRecord(value.namespace)) return null;
  const detail = value.columns === undefined || value.columns === null ? {} : value.columns;
  if (!isRecord(detail)) return null;
  const schemas: ParsedSchema["schemas"] = [];
  for (const [schemaName, rawTables] of Object.entries(value.namespace)) {
    if (!isRecord(rawTables)) return null;
    const schemaDetail = detail[schemaName];
    if (schemaDetail !== undefined && schemaDetail !== null && !isRecord(schemaDetail)) return null;
    const tables: SchemaTable[] = [];
    for (const [tableName, rawNames] of Object.entries(rawTables)) {
      const names = sliceOf(rawNames);
      if (!names || !names.every((name) => typeof name === "string")) return null;
      const known = schemaDetail?.[tableName];
      const columns =
        known === undefined || known === null
          ? (names as string[]).map((name) => ({ name, dataType: "", nullable: null }))
          : detailOf(known);
      if (!columns) return null;
      tables.push({ name: tableName, columns });
    }
    schemas.push({ name: schemaName, tables: tables.sort(byName) });
  }
  return { schemas: schemas.sort(byName) };
}

export type ForeignKey = {
  schema: string;
  table: string;
  column: string;
  referencedSchema: string;
  referencedTable: string;
  referencedColumn: string;
};

const FK_FIELDS = ["schema", "table", "column", "referenced_schema", "referenced_table", "referenced_column"] as const;

/** `db.listForeignKeys` → `{result: [ForeignKey]}`, or the array once the envelope is off. */
export function parseForeignKeys(rawOutput: unknown): ForeignKey[] | null {
  // A nil slice in the envelope, once the payload layer has taken it off.
  if (rawOutput === null) return [];
  let value = parseToolOutput(rawOutput);
  // parseToolOutput takes the envelope off; a nil slice inside it is null.
  if (isRecord(rawOutput) && Object.keys(rawOutput).length === 1 && "result" in rawOutput && value === null) {
    value = [];
  }
  if (!Array.isArray(value)) return null;
  const keys: ForeignKey[] = [];
  for (const raw of value) {
    if (!isRecord(raw) || !FK_FIELDS.every((field) => typeof raw[field] === "string")) return null;
    keys.push({
      schema: raw.schema as string,
      table: raw.table as string,
      column: raw.column as string,
      referencedSchema: raw.referenced_schema as string,
      referencedTable: raw.referenced_table as string,
      referencedColumn: raw.referenced_column as string,
    });
  }
  return keys;
}

/**
 * The rows as CSV, exactly as the console's own Export writes them
 * (export.ts serializeCSV, RFC 4180: CRLF between records, quotes only
 * where needed). NULL and the empty string both serialize as an empty field
 * — CSV has no way to tell them apart. No spreadsheet-formula guard, so the
 * bytes match an export of the same result.
 */
export function toCsv(columns: string[], rows: (string | null)[][]): string {
  return serializeCSV({ columns, rows, rowCount: rows.length, truncated: false });
}

/** A JSON object from rawInput, which may still be JSON text. */
function inputRecord(rawInput: unknown): Record<string, unknown> | null {
  let value = rawInput;
  if (typeof value === "string") {
    try {
      value = JSON.parse(value);
    } catch {
      return null;
    }
  }
  return isRecord(value) ? value : null;
}

/** The statement a db.query / db.explain call sent (`{sql}`), trimmed, or "". */
export function sqlOf(rawInput: unknown): string {
  const value = inputRecord(rawInput);
  return typeof value?.sql === "string" ? value.sql.trim() : "";
}

/** The connection a db.explain call named (`{profileId}`); db.query names none (F4). */
export function profileIdOf(rawInput: unknown): string | undefined {
  const value = inputRecord(rawInput);
  return typeof value?.profileId === "string" && value.profileId ? value.profileId : undefined;
}

/** Statements whose count is rows they wrote — a data-modifying CTE starts with WITH. */
const ROW_WRITES = new Set(["INSERT", "UPDATE", "DELETE", "MERGE", "WITH"]);

/** The statement's first keyword, past leading whitespace and comments. */
function firstKeyword(sql: string): string {
  const body = sql.replace(/^(?:\s+|--[^\n]*(?:\n|$)|\/\*[\s\S]*?\*\/)*/, "");
  return (/^[A-Za-z]+/.exec(body)?.[0] ?? "").toUpperCase();
}

/**
 * What a statement with no result columns did. A count, unless the driver
 * cannot say (-1) or the 0 is DDL's: Postgres reports 0 for CREATE/ALTER,
 * which read as "0 rows affected" looks like a failure. A 0 from a statement
 * that writes rows is real — it matched none — and stays.
 */
export function writeSummary(rowsAffected: number, sql: string): string {
  if (rowsAffected < 0) return "Done";
  if (rowsAffected === 0 && !ROW_WRITES.has(firstKeyword(sql))) return "Done";
  return `${rowsAffected} ${rowsAffected === 1 ? "row" : "rows"} affected`;
}
