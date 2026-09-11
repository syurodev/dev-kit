import { quoteIdentifier } from "./identifier";

export type BrowseCursor = {
  /** The key values of the row this page starts after (or before). */
  values: readonly string[];
  direction: "next" | "prev";
};

export type BrowseQuery = {
  sql: string;
  /** How the caller must interpret the cursor it gets back. */
  mode: "keyset" | "offset";
  /** Rows to keep; the statement asks for one more to detect a next page. */
  pageSize: number;
  /** True when the rows come back in reverse and must be flipped. */
  reversed: boolean;
};

/**
 * The statement behind a table tab.
 *
 * Keyset, not OFFSET, wherever the table has a key: OFFSET makes the server
 * scan and discard every row before the page, so page 500 costs five hundred
 * pages of work. A keyset walk asks "the rows after this one" and costs the
 * same at any depth.
 *
 * One extra row is always requested. That is how the pager knows whether a next
 * page exists without a COUNT(*), which on a large table is a full scan every
 * time you open it.
 *
 * Values are inlined rather than bound because the bridge takes a statement and
 * nothing else (`DBQuery(id, sql)`). They are escaped here; identifiers go
 * through quoteIdentifier. The user's own WHERE is passed through untouched —
 * it is their SQL, the same as the console's, and the connection's read-only
 * flag is what limits it.
 */
export function buildBrowseQuery(opts: {
  schema: string;
  table: string;
  engine: string | undefined;
  where: string;
  orderBy: { column: string; direction: "asc" | "desc" } | null;
  pageKey: readonly string[] | null;
  cursor: BrowseCursor | null;
  pageSize: number;
  offset?: number;
}): BrowseQuery {
  const { schema, table, engine, where, orderBy, pageKey, cursor, pageSize } = opts;
  const q = (name: string) => quoteIdentifier(name, engine);
  const target = `${q(schema)}.${q(table)}`;

  const conditions: string[] = [];
  if (where.trim()) {
    conditions.push(`(${where.trim()})`);
  }

  const keyed = pageKey !== null && pageKey.length > 0;
  const reversed = keyed && cursor?.direction === "prev";

  if (keyed && cursor) {
    // A15: one value per key column, or there is nothing to compare. Both ways
    // of carrying on are worse than stopping — an empty row value is a syntax
    // error, and dropping the comparison re-serves the previous page. The
    // caller's catch turns this into an error banner, which is the honest
    // outcome for a bug in the caller.
    if (cursor.values.length !== pageKey.length) {
      throw new Error(
        `browse cursor has ${cursor.values.length} value(s) for a ${pageKey.length}-column key`,
      );
    }
    const comparison = cursor.direction === "next" ? ">" : "<";
    const columns = pageKey.map(q).join(", ");
    const values = cursor.values.map(quoteLiteral).join(", ");
    conditions.push(`(${columns}) ${comparison} (${values})`);
  }

  // The key always ends the ORDER BY. Sorting by a non-unique column alone is
  // not a total order, and a keyset walk over one skips and repeats rows at
  // every page boundary.
  const terms: string[] = [];
  if (orderBy) {
    terms.push(`${q(orderBy.column)} ${orderBy.direction.toUpperCase()}`);
  }
  if (keyed) {
    for (const column of pageKey) {
      terms.push(`${q(column)} ${reversed ? "DESC" : "ASC"}`);
    }
  }

  const parts = [`SELECT * FROM ${target}`];
  if (conditions.length > 0) {
    parts.push(`WHERE ${conditions.join(" AND ")}`);
  }
  if (terms.length > 0) {
    parts.push(`ORDER BY ${terms.join(", ")}`);
  }
  parts.push(`LIMIT ${pageSize + 1}`);
  if (!keyed && opts.offset) {
    parts.push(`OFFSET ${Math.trunc(opts.offset)}`);
  }

  return {
    sql: parts.join(" ") + ";",
    mode: keyed ? "keyset" : "offset",
    pageSize,
    reversed,
  };
}

/** A string literal for the statement: the quote doubles, as in both dialects. */
function quoteLiteral(value: string): string {
  return "'" + value.split("'").join("''") + "'";
}
