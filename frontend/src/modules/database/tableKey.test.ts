import { describe, expect, test } from "bun:test";
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

describe("pageKeyFor", () => {
  test("the primary key pages the table", () => {
    expect(pageKeyFor([index({})], "public", "orders")).toEqual(["id"]);
  });

  test("a composite primary key pages in its own column order", () => {
    expect(pageKeyFor([index({ columns: ["tenant_id", "id"] })], "public", "orders")).toEqual([
      "tenant_id",
      "id",
    ]);
  });

  // No primary key but a unique index is still a stable order. Preferring the
  // narrowest keeps the keyset comparison short.
  test("a unique index stands in for a missing primary key", () => {
    const indexes = [
      index({ name: "by_email", columns: ["email"], primary: false }),
      index({ name: "by_pair", columns: ["a", "b"], primary: false }),
    ];
    expect(pageKeyFor(indexes, "public", "orders")).toEqual(["email"]);
  });

  // A non-unique index is not a total order — it has no reason to be — so a
  // keyset walk over it would skip and repeat rows at every page boundary.
  test("a non-unique index is not a key", () => {
    const indexes = [index({ name: "by_status", columns: ["status"], unique: false, primary: false })];
    expect(pageKeyFor(indexes, "public", "orders")).toBeNull();
  });

  // Views and heap tables. The caller falls back to OFFSET.
  test("a table with nothing unique has no key", () => {
    expect(pageKeyFor([], "public", "orders")).toBeNull();
  });

  test("another table's indexes do not page this one", () => {
    expect(pageKeyFor([index({ table: "customers" })], "public", "orders")).toBeNull();
  });
});
