import type { ColumnInfo, IndexInfo } from "@/bridge/database";

/**
 * The columns a keyset walk can page a table by, or null when it has none.
 *
 * Keyset paging needs a total order: a column set that is unique and present on
 * every row, so "the row after this one" has exactly one answer. A primary key
 * is that by definition; a unique index is that only when none of its columns
 * can be NULL. Anything else — a view, a heap table, a mapping table nobody
 * keyed — gets null, and the caller pages by OFFSET instead. Returning a
 * non-total order here would not fail loudly: it would silently skip rows and
 * repeat others at every page boundary, which is the worst way for a data
 * browser to be wrong.
 *
 * A24: uniqueness alone is NOT the whole of the check. UNIQUE permits NULLs
 * (several of them, in most engines), and SQL compares a NULL to anything as
 * unknown, so the row-value comparison a page runs neither includes nor
 * excludes the NULL rows — the walk skips some and repeats others right where
 * it crosses them. runQuery also stringifies NULL to "", so a null key value
 * and an empty-string one are the same bytes inside a cursor and cannot even be
 * told apart after the fact. So a unique index is a key only with positive
 * evidence that every one of its columns is NOT NULL, taken from `columns`
 * (ColumnInfo.nullable, which the bridge reports per column).
 *
 * `columns` may be missing or incomplete — the sidebar populates it when a
 * table is expanded, and a table opened another way may never have been. That
 * is the "unknown ⇒ not a key" rule: absence of a nullable flag is not evidence
 * of NOT NULL, and the only wrong guess that is silent is guessing in the
 * accepting direction. Rejecting costs a slower OFFSET page for a table we
 * could have walked; accepting costs rows a person never sees and rows they see
 * twice, with nothing on screen to say so. A primary key is returned regardless
 * of `columns`, because a primary key is NOT NULL by definition — a nullable
 * flag on one of its columns is a fault below us, not a reason to stop paging a
 * table that genuinely has a key.
 */
export function pageKeyFor(
  indexes: readonly IndexInfo[],
  columns: readonly ColumnInfo[] | null | undefined,
  schema: string,
  table: string,
): string[] | null {
  const mine = indexes.filter(
    (index) => index.schema === schema && index.table === table && index.columns.length > 0,
  );

  const primary = mine.find((index) => index.primary);
  if (primary) {
    return [...primary.columns];
  }

  const nullableByColumn = new Map((columns ?? []).map((column) => [column.name, column.nullable]));
  // Unknown (no row for the column) is read as nullable, per the rule above.
  const notNull = (column: string) => nullableByColumn.get(column) === false;

  // The narrowest unique index whose columns are all known NOT NULL: every
  // column in the key lengthens the row-value comparison each page runs.
  const unique = mine
    .filter((index) => index.unique && index.columns.every(notNull))
    .sort((a, b) => a.columns.length - b.columns.length)[0];
  return unique ? [...unique.columns] : null;
}
