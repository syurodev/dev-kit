import { describe, expect, test } from "bun:test";
import { WHERE_KEYWORDS, whereSchema } from "./WhereInput";

describe("whereSchema", () => {
  const indexed = {
    public: { orders: ["id", "customer_id"], invoices: ["id", "total"] },
  };

  // The offline index is the source: a filter on one table still gets to name
  // another one's columns, which is what makes a subquery completable.
  test("every indexed schema and table is offered", () => {
    const schema = whereSchema(indexed, "public", "orders", ["id", "customer_id"]) as Record<
      string,
      Record<string, string[]>
    >;
    expect(Object.keys(schema.public).sort()).toEqual(["invoices", "orders"]);
  });

  // The grid's own result is authoritative and free. Nobody has to index a
  // connection before the table in front of them completes.
  test("an unindexed connection still completes the table on screen", () => {
    const schema = whereSchema(undefined, "public", "attendances", ["id", "branch_id"]) as Record<
      string,
      Record<string, string[]>
    >;
    expect(schema.public.attendances).toEqual(["id", "branch_id"]);
  });

  // A stale index describing a table that has since gained a column would
  // otherwise hide it — the result is newer than the snapshot, always.
  test("the result wins over the index for the table being browsed", () => {
    const schema = whereSchema(indexed, "public", "orders", ["id", "customer_id", "voided_at"]) as Record<
      string,
      Record<string, string[]>
    >;
    expect(schema.public.orders).toEqual(["id", "customer_id", "voided_at"]);
    expect(schema.public.invoices).toEqual(["id", "total"]);
  });

  // Before the first page lands there is nothing to add, and inventing an
  // empty table would shadow the indexed one.
  test("no columns yet leaves the index untouched", () => {
    expect(whereSchema(indexed, "public", "orders", [])).toEqual(indexed);
  });
});

describe("WHERE_KEYWORDS", () => {
  // The dialect's own list is every reserved and non-reserved word the engine
  // has. Typing "nam" in a filter offered NAME, NAMES, NAMESPACE, RENAME and
  // DYNAMIC above the `username` column — none of which can appear in a WHERE
  // clause. A negative boost was not enough: an exact prefix match on a
  // keyword outranks a mid-word match on a column, and should.
  test("offers what a filter is written in", () => {
    for (const word of ["AND", "OR", "NOT", "IN", "IS", "NULL", "LIKE", "BETWEEN", "EXISTS"]) {
      expect(WHERE_KEYWORDS).toContain(word);
    }
  });

  // A filter may hold a subquery: id IN (SELECT employee_id FROM his_shifts).
  test("keeps the words a subquery needs", () => {
    expect(WHERE_KEYWORDS).toContain("SELECT");
    expect(WHERE_KEYWORDS).toContain("FROM");
  });

  test("offers nothing that cannot appear in one", () => {
    for (const word of ["NAMES", "NAMESPACE", "NATURAL", "NATIONAL", "RENAME", "DYNAMIC"]) {
      expect(WHERE_KEYWORDS, `${word} cannot appear in a WHERE clause`).not.toContain(word);
    }
  });
});
