import { describe, expect, test } from "bun:test";
import type { ColumnInfo } from "@/bridge/database";
import { pageKeyFor } from "./tableKey";

const index = (over: Partial<Parameters<typeof pageKeyFor>[0][number]>) => ({
  schema: "public",
  table: "orders",
  name: "PRIMARY",
  columns: ["id"],
  unique: true,
  primary: true,
  ...over,
});

const column = (name: string, nullable = false): ColumnInfo => ({
  name,
  dataType: "text",
  nullable,
  position: 1,
});

const notNull = (...names: string[]) => names.map((n) => column(n, false));

describe("pageKeyFor", () => {
  test("the primary key pages the table", () => {
    expect(pageKeyFor([index({})], notNull("id"), "public", "orders")).toEqual(["id"]);
  });

  test("a composite primary key pages in its own column order", () => {
    expect(
      pageKeyFor(
        [index({ columns: ["tenant_id", "id"] })],
        notNull("tenant_id", "id"),
        "public",
        "orders",
      ),
    ).toEqual(["tenant_id", "id"]);
  });

  // No primary key but a unique index is still a stable order. Preferring the
  // narrowest keeps the keyset comparison short.
  test("a unique index stands in for a missing primary key", () => {
    const indexes = [
      index({ name: "by_email", columns: ["email"], primary: false }),
      index({ name: "by_pair", columns: ["a", "b"], primary: false }),
    ];
    expect(pageKeyFor(indexes, notNull("email", "a", "b"), "public", "orders")).toEqual(["email"]);
  });

  // A non-unique index is not a total order — it has no reason to be — so a
  // keyset walk over it would skip and repeat rows at every page boundary.
  test("a non-unique index is not a key", () => {
    const indexes = [index({ name: "by_status", columns: ["status"], unique: false, primary: false })];
    expect(pageKeyFor(indexes, notNull("status"), "public", "orders")).toBeNull();
  });

  // Views and heap tables. The caller falls back to OFFSET.
  test("a table with nothing unique has no key", () => {
    expect(pageKeyFor([], notNull("id"), "public", "orders")).toBeNull();
  });

  test("another table's indexes do not page this one", () => {
    expect(pageKeyFor([index({ table: "customers" })], notNull("id"), "public", "orders")).toBeNull();
  });

  // A24. A primary key is NOT NULL by definition, so the catalogue's nullable
  // flag on one of its columns is a mistake somewhere below us, not a reason to
  // stop paging a table that has a real key.
  test("a primary key over a column reported nullable still pages", () => {
    expect(pageKeyFor([index({})], [column("id", true)], "public", "orders")).toEqual(["id"]);
  });

  // A24. UNIQUE permits NULLs and SQL compares them as unknown, so the index is
  // not a total order: the walk skips and repeats rows around the NULL row.
  test("a unique index over a nullable column is not a key", () => {
    const indexes = [index({ name: "by_email", columns: ["email"], primary: false })];
    expect(pageKeyFor(indexes, [column("email", true)], "public", "orders")).toBeNull();
  });

  test("a unique index over NOT NULL columns is a key", () => {
    const indexes = [index({ name: "by_email", columns: ["email"], primary: false })];
    expect(pageKeyFor(indexes, [column("email", false)], "public", "orders")).toEqual(["email"]);
  });

  // One nullable member is enough: that column's NULLs make the whole row value
  // incomparable, however narrow the rest of the key is.
  test("a composite unique index with one nullable member is not a key", () => {
    const indexes = [index({ name: "by_pair", columns: ["a", "b"], primary: false })];
    expect(
      pageKeyFor(indexes, [column("a", false), column("b", true)], "public", "orders"),
    ).toBeNull();
  });

  // The narrowest unique index is nullable, a wider one is not: pick the wider
  // one rather than giving up on the table.
  test("a nullable unique index does not shadow a usable wider one", () => {
    const indexes = [
      index({ name: "by_email", columns: ["email"], primary: false }),
      index({ name: "by_pair", columns: ["a", "b"], primary: false }),
    ];
    expect(
      pageKeyFor(indexes, [column("email", true), column("a"), column("b")], "public", "orders"),
    ).toEqual(["a", "b"]);
  });

  // Unknown nullability is not evidence of NOT NULL.
  test("a unique index over a column we have no row for is not a key", () => {
    const indexes = [index({ name: "by_email", columns: ["email"], primary: false })];
    expect(pageKeyFor(indexes, notNull("id"), "public", "orders")).toBeNull();
  });

  test("with no column information at all a unique index is not a key", () => {
    const indexes = [index({ name: "by_email", columns: ["email"], primary: false })];
    expect(pageKeyFor(indexes, undefined, "public", "orders")).toBeNull();
    expect(pageKeyFor(indexes, [], "public", "orders")).toBeNull();
  });

  test("with no column information at all the primary key still pages", () => {
    expect(pageKeyFor([index({})], undefined, "public", "orders")).toEqual(["id"]);
  });
});
