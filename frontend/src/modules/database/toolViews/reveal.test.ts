import { describe, expect, test } from "bun:test";
import { revealTarget } from "./reveal";

const ref = (kind: string, id: string, label = "") => ({ module: "database", kind, id, label });

describe("database.reveal routing", () => {
  test("a table is schema.table, split at the first dot", () => {
    expect(revealTarget(ref("table", "public.orders"))).toEqual({ open: "table", schema: "public", table: "orders" });
    expect(revealTarget(ref("table", "sales.order.lines"))).toEqual({
      open: "table",
      schema: "sales",
      table: "order.lines",
    });
  });

  test("a quoted schema keeps its dots", () => {
    expect(revealTarget(ref("table", '"sales.eu".orders'))).toEqual({ open: "table", schema: "sales.eu", table: "orders" });
    expect(revealTarget(ref("table", '"a""b".t'))).toEqual({ open: "table", schema: 'a"b', table: "t" });
    expect(revealTarget(ref("table", '"sales.eu"'))).toBeNull();
    expect(revealTarget(ref("table", '"sales.eu.orders'))).toBeNull();
  });

  test("the ids Agent Hub writes (entityRefs.tableId) open the table they name", () => {
    expect(revealTarget(ref("table", "public.orders"))).toEqual({ open: "table", schema: "public", table: "orders" });
    expect(revealTarget(ref("table", '"sales.eu".orders'))).toEqual({ open: "table", schema: "sales.eu", table: "orders" });
    expect(revealTarget(ref("table", '"a""b.c".t'))).toEqual({ open: "table", schema: 'a"b.c', table: "t" });
    expect(revealTarget(ref("table", '"""x".t'))).toEqual({ open: "table", schema: '"x', table: "t" });
  });

  test("a table without its schema cannot be opened", () => {
    expect(revealTarget(ref("table", "orders"))).toBeNull();
    expect(revealTarget(ref("table", ".orders"))).toBeNull();
    expect(revealTarget(ref("table", "public."))).toBeNull();
  });

  test("a console carries its SQL, titled by the label when there is one", () => {
    expect(revealTarget(ref("console", "select 1", "Top users"))).toEqual({
      open: "console",
      sql: "select 1",
      title: "Top users",
    });
    expect(revealTarget(ref("console", "select 1"))).toEqual({ open: "console", sql: "select 1", title: "Console" });
    expect(revealTarget(ref("console", "   "))).toBeNull();
  });

  test("any other kind is not this module's to open", () => {
    expect(revealTarget(ref("topic", "orders"))).toBeNull();
  });
});
