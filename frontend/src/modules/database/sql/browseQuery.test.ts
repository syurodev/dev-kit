import { describe, expect, test } from "bun:test";
import { buildBrowseQuery } from "./browseQuery";

const base = {
  schema: "public",
  table: "orders",
  engine: "postgres",
  where: "",
  orderBy: null,
  pageKey: ["id"],
  cursor: null,
  pageSize: 200,
};

describe("buildBrowseQuery", () => {
  test("the first page orders by the key and takes one extra row", () => {
    const { sql, mode } = buildBrowseQuery(base);
    expect(mode).toBe("keyset");
    // The extra row is how "is there a next page" is answered without COUNT(*).
    expect(sql).toContain("LIMIT 201");
    expect(sql).toContain('ORDER BY "id" ASC');
    expect(sql).not.toContain("WHERE");
  });

  test("a cursor becomes a row-value comparison", () => {
    const { sql } = buildBrowseQuery({ ...base, cursor: { values: ["42"], direction: "next" } });
    expect(sql).toContain('WHERE ("id") > (\'42\')');
  });

  // Sorting by a non-unique column cannot page on that column alone: ties
  // would skip and repeat rows. The key is appended as the tiebreaker.
  test("a user sort keeps the key as a tiebreaker", () => {
    const { sql } = buildBrowseQuery({
      ...base,
      orderBy: { column: "name", direction: "desc" },
    });
    expect(sql).toContain('ORDER BY "name" DESC, "id" ASC');
  });

  test("paging back reverses the comparison", () => {
    const { sql } = buildBrowseQuery({ ...base, cursor: { values: ["42"], direction: "prev" } });
    expect(sql).toContain('WHERE ("id") < (\'42\')');
    expect(sql).toContain('ORDER BY "id" DESC');
  });

  // Without a key there is no cursor to build, so the walk is by offset.
  test("an unkeyed table pages by offset", () => {
    const { sql, mode } = buildBrowseQuery({ ...base, pageKey: null, offset: 400 });
    expect(mode).toBe("offset");
    expect(sql).toContain("LIMIT 201 OFFSET 400");
  });

  test("the user's WHERE is combined with the cursor, not replaced by it", () => {
    const { sql } = buildBrowseQuery({
      ...base,
      where: "status = 'open'",
      cursor: { values: ["42"], direction: "next" },
    });
    expect(sql).toContain("status = 'open'");
    expect(sql).toContain('("id") > (\'42\')');
    expect(sql).toContain(" AND ");
  });

  test("identifiers are quoted for the engine", () => {
    const { sql } = buildBrowseQuery({ ...base, engine: "mysql" });
    expect(sql).toContain("`orders`");
    expect(sql).toContain("`id`");
  });

  // A value with a quote in it must not end the literal early.
  test("a cursor value carrying a quote is escaped", () => {
    const { sql } = buildBrowseQuery({
      ...base,
      cursor: { values: ["o'brien"], direction: "next" },
    });
    expect(sql).toContain("'o''brien'");
  });

  // A11. The reason the comparison is written `(a, b) > (…)` rather than
  // `a > …` is a composite key, and with a single-column key the two forms are
  // indistinguishable — so nothing above would catch a bug here. Every key
  // column must appear in the comparison, in key order, on both sides, and
  // every one must carry a direction in the ORDER BY: a composite key with a
  // direction on only its first column is not a total order either.
  test("a composite key compares every column, in key order", () => {
    const { sql } = buildBrowseQuery({
      ...base,
      pageKey: ["tenant_id", "id"],
      cursor: { values: ["7", "42"], direction: "next" },
    });
    expect(sql).toContain('("tenant_id", "id") > (\'7\', \'42\')');
    expect(sql).toContain('ORDER BY "tenant_id" ASC, "id" ASC');
  });

  // A15. A cursor with no values cannot be compared against. Emitting
  // `("id") > ()` is a syntax error, and quietly dropping the comparison is
  // worse — the statement becomes page 0's own query, so the next page serves
  // the first page's rows again. Neither is acceptable from a data browser, so
  // the builder refuses instead: loudly, at the one place that can tell.
  test("a cursor whose values do not match the key is refused, not guessed", () => {
    expect(() =>
      buildBrowseQuery({ ...base, cursor: { values: [], direction: "next" } }),
    ).toThrow();
    expect(() =>
      buildBrowseQuery({
        ...base,
        pageKey: ["tenant_id", "id"],
        cursor: { values: ["7"], direction: "next" },
      }),
    ).toThrow();
  });
});
