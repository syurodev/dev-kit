import { describe, expect, test } from "bun:test";
import { buildBrowseQuery, buildCountQuery } from "./browseQuery";

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

  // A pager that can jump lands on pages this tab has never walked to, so
  // there is no cursor behind them — and a keyed table used to refuse that
  // outright. OFFSET is the fallback: the ORDER BY is a total order either
  // way, so the page is the right one, just paid for the slow way.
  test("a keyed table jumped to without a trail falls back to offset", () => {
    const { sql } = buildBrowseQuery({ ...base, cursor: null, offset: 6000 });
    expect(sql).toContain('ORDER BY "id" ASC');
    expect(sql).toContain("LIMIT 201 OFFSET 6000");
  });

  // …and once it has one, the walk goes back to being a walk. A cursor AND an
  // offset would skip a page's worth of rows past the cursor.
  test("a cursor means no offset, ever", () => {
    const { sql } = buildBrowseQuery({
      ...base,
      cursor: { values: ["42"], direction: "next" },
      offset: 6000,
    });
    expect(sql).not.toContain("OFFSET");
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

  // A25. The table document resolves its page key as null when it has no
  // identifier to look one up with, so the load runs instead of the pane
  // sitting blank forever. This is what the load then meets: a refusal, caught
  // by loadBrowsePage and shown in the error banner. A statement built around
  // `""."".` would have been the silent version of the same fault.
  test("a missing identifier is refused rather than quoted into nothing", () => {
    expect(() => buildBrowseQuery({ ...base, schema: "" })).toThrow();
    expect(() => buildBrowseQuery({ ...base, table: "" })).toThrow();
  });
});

describe("buildCountQuery", () => {
  // What the pager needs that a probe row cannot tell it: how many pages there
  // are, so Last has somewhere to go.
  test("counts the table", () => {
    expect(buildCountQuery({ schema: "public", table: "orders", engine: "postgres", where: "" }))
      .toBe('SELECT COUNT(*) FROM "public"."orders";');
  });

  // The count has to answer for the filter the grid is showing, or "of 8,420"
  // sits over a filtered page and the last page is a page that does not exist.
  test("counts what the filter matches, not the table", () => {
    const sql = buildCountQuery({
      schema: "public",
      table: "orders",
      engine: "postgres",
      where: "status = 'open'",
    });
    expect(sql).toContain("WHERE (status = 'open')");
  });

  test("a missing identifier is refused here too", () => {
    expect(() =>
      buildCountQuery({ schema: "", table: "orders", engine: "postgres", where: "" }),
    ).toThrow();
  });
});
