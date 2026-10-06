import { quoteIdentifier, quoteLiteral } from "./identifier";
import { booleanValue, cellKind } from "./cellValue";

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
  columnTypes?: Readonly<Record<string, string>>;
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
 * Shared-value helper retained for direct edits. Reviewed bulk edits use
 * buildStagedUpdates: one statement per row, all changed columns together,
 * so the backend can refuse each missing row in its transaction.
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
  const { columns, value, ...rest } = request;
  return buildRowUpdate({
    ...rest,
    assignments: columns.map((column) => ({ column, value })),
  });
}

export function buildRowUpdate(
  request: Omit<UpdateRequest, "columns" | "value"> & {
    assignments: readonly { column: string; value: CellValue }[];
  },
): string {
  const { schema, table, engine, keyColumns, rows, assignments, columnTypes } =
    request;
  if (rows.length === 0 || assignments.length === 0) {
    throw new Error("an update needs at least one row and one column");
  }
  if (keyColumns.length === 0) {
    throw new Error("an update needs a key to identify its rows");
  }
  const q = (name: string) => quoteIdentifier(name, engine);
  const literal = (column: string, v: CellValue) => {
    if (v === null) return "NULL";
    if (cellKind(columnTypes?.[column]) === "boolean") {
      const b = booleanValue(v);
      if (b !== "true" && b !== "false")
        throw new Error("Invalid boolean value.");
      return b.toUpperCase();
    }
    return quoteLiteral(v, engine);
  };
  const setList = assignments
    .map(({ column, value }) => `${q(column)} = ${literal(column, value)}`)
    .join(", ");
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

  return `UPDATE ${q(schema)}.${q(table)} SET ${setList} WHERE ${predicate}`;
}

/** One UPDATE per row, including every edited key column in the same SET list.
 * Original keys remain the predicate. Each row keeps a no-match check in ApplyEdits;
 * grouping rows into an OR statement would lose that check for a deleted row.
 */
export function buildStagedUpdates(request: {
  schema: string;
  table: string;
  engine: string | undefined;
  keyColumns: readonly string[];
  columns: readonly string[];
  rows: readonly (readonly CellValue[])[];
  staged: Readonly<Record<string, CellValue>>;
  columnTypes?: Readonly<Record<string, string>>;
}): string[] {
  const {
    schema,
    table,
    engine,
    keyColumns,
    columns,
    rows,
    staged,
    columnTypes,
  } = request;
  const changes = new Map<
    number,
    { column: string; value: CellValue; index: number }[]
  >();
  for (const [position, value] of Object.entries(staged)) {
    const parts = position.split(":");
    const [row, column] = parts.map(Number);
    if (
      parts.length !== 2 ||
      !Number.isInteger(row) ||
      !Number.isInteger(column) ||
      row < 0 ||
      column < 0 ||
      !rows[row] ||
      columns[column] === undefined
    )
      throw new Error("A staged cell is outside the current page.");
    const list = changes.get(row) ?? [];
    list.push({ column: columns[column], value, index: column });
    changes.set(row, list);
  }
  return [...changes]
    .sort(([a], [b]) => a - b)
    .map(([row, assignments]) =>
      buildRowUpdate({
        schema,
        table,
        engine,
        keyColumns,
        columnTypes,
        rows: [
          {
            values: keyColumns.map((key) => {
              const at = columns.indexOf(key);
              if (at < 0)
                throw new Error("The row key is missing from the result.");
              return rows[row][at] ?? null;
            }),
          },
        ],
        assignments: assignments.sort((a, b) => a.index - b.index),
      }),
    );
}
