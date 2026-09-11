import type { IndexInfo } from "@/bridge/database";

/**
 * The columns a keyset walk can page a table by, or null when it has none.
 *
 * Keyset paging needs a total order: a column set that is unique and present on
 * every row, so "the row after this one" has exactly one answer. A primary key
 * is that by definition; a unique index is that in practice. Anything else —
 * a view, a heap table, a mapping table nobody keyed — gets null, and the
 * caller pages by OFFSET instead. Returning a non-unique column here would not
 * fail loudly: it would silently skip rows and repeat others at every page
 * boundary, which is the worst way for a data browser to be wrong.
 *
 * A23: uniqueness is the whole of the check, because it is the whole of what
 * IndexInfo reports. A unique index on a NULLABLE column is therefore accepted
 * here and is the one case this is best-effort about: SQL compares NULLs as
 * unknown, and runQuery stringifies NULL to "", so a null key value is
 * indistinguishable from an empty one in a cursor. Excluding it needs
 * nullability from the backend's IndexInfo, which this plan does not change.
 */
export function pageKeyFor(
  indexes: readonly IndexInfo[],
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

  // The narrowest unique index: every column in the key lengthens the row-value
  // comparison each page runs.
  const unique = mine
    .filter((index) => index.unique)
    .sort((a, b) => a.columns.length - b.columns.length)[0];
  return unique ? [...unique.columns] : null;
}
