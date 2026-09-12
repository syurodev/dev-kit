import { quoteIdentifier, quoteLiteral } from "./identifier";

/** A cell's new value. `null` is SQL NULL, not the four letters. */
export type CellValue = string | null;

export type RowKey = {
  /** The key column values of one row, in the key's own column order. */
  values: readonly CellValue[];
};

export type UpdateRequest = {
  schema: string;
  table: string;
  engine: string | undefined;
  /** The table's key columns — what identifies a row. */
  keyColumns: readonly string[];
  /** The rows to change, by their key values. */
  rows: readonly RowKey[];
  /** The columns to set, and what to set them to. */
  columns: readonly string[];
  value: CellValue;
};

/**
 * Why a grid cell cannot be edited, or "" when it can.
 *
 * Checked before a statement is built rather than after it fails, because the
 * failure a missing key produces is not an error — it is an UPDATE that hits
 * every row that happens to match. There is no safe fallback here: a table
 * with no key has no way to name one row, so the answer is no.
 */
export function updateRefusal(opts: {
  keyColumns: readonly string[] | null | undefined;
  readOnly: boolean;
  schema: string;
  table: string;
}): string {
  if (opts.readOnly) {
    return "This connection is in safe mode — turn off read-only in Connection options to edit.";
  }
  if (!opts.schema || !opts.table) {
    return "These rows did not come from a single table.";
  }
  if (!opts.keyColumns || opts.keyColumns.length === 0) {
    return "This table has no primary key, so a row cannot be identified to update.";
  }
  return "";
}

/**
 * One UPDATE for a rectangle of cells.
 *
 * One statement, not one per row: a bulk edit is a single intention and it
 * should be a single thing to undo, and a hundred round trips to set a hundred
 * cells is a hundred chances to end up half-applied.
 *
 * The row list is a disjunction of full key comparisons rather than
 * `key IN (…)`, because a composite key's tuple syntax is not portable — MySQL
 * takes row values, and writing them out plainly works on every engine this
 * app speaks.
 *
 * Values are inlined, like buildBrowseQuery's: the bridge takes a statement and
 * nothing else. quoteLiteral is what makes that safe for the value, and
 * quoteIdentifier for every name.
 */
export function buildUpdate(request: UpdateRequest): string {
  const { schema, table, engine, keyColumns, rows, columns, value } = request;
  if (rows.length === 0 || columns.length === 0) {
    throw new Error("an update needs at least one row and one column");
  }
  if (keyColumns.length === 0) {
    throw new Error("an update needs a key to identify its rows");
  }
  const q = (name: string) => quoteIdentifier(name, engine);
  const literal = (v: CellValue) => (v === null ? "NULL" : quoteLiteral(v, engine));

  const assignments = columns.map((column) => `${q(column)} = ${literal(value)}`).join(", ");
  const predicate = rows
    .map((row) =>
      keyColumns
        .map((column, index) => {
          const key = row.values[index] ?? null;
          // A NULL key never equals anything, so `= NULL` would match no row
          // and the update would silently do nothing.
          return key === null
            ? `${q(column)} IS NULL`
            : `${q(column)} = ${quoteLiteral(key, engine)}`;
        })
        .join(" AND "),
    )
    .map((clause) => (keyColumns.length > 1 ? `(${clause})` : clause))
    .join(" OR ");

  return `UPDATE ${q(schema)}.${q(table)} SET ${assignments} WHERE ${predicate}`;
}
