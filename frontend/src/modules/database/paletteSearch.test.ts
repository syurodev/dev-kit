import { describe, expect, it } from "bun:test";
import { buildGroups } from "./paletteSearch";
import type { ConnectionProfile, SchemaSnapshot } from "@/bridge/database";

function profile(overrides: Partial<ConnectionProfile> = {}): ConnectionProfile {
  return {
    id: "p1",
    name: "Prod",
    host: "db.acme.internal",
    port: 5432,
    database: "acme",
    username: "app",
    sslMode: "require",
    env: "prod",
    readOnly: false,
    ...overrides,
  };
}

const SNAPSHOT: SchemaSnapshot = {
  namespace: {
    public: { orders: [], order_items: [] },
    audit: { orders: [] },
  },
  columns: {},
  indexedAt: "",
};

function actions() {
  const openedTables: Array<[string, string]> = [];
  const openedConnections: string[] = [];
  return {
    openedTables,
    openedConnections,
    actions: {
      openTable: (schema: string, table: string) => openedTables.push([schema, table]),
      openConnection: (id: string) => openedConnections.push(id),
    },
  };
}

describe("database buildGroups", () => {
  it("offers connections but no tables for an empty query", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("", { schemaSnapshot: SNAPSHOT, profiles: [profile()] }, acts);
    expect(groups.map((g) => g.heading)).toEqual(["CONNECTIONS"]);
    expect(groups[0].items).toHaveLength(1);
  });

  it("ranks an exact table-name match over a merely fuzzy one", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("orders", { schemaSnapshot: SNAPSHOT, profiles: [] }, acts);
    const tables = groups.find((g) => g.heading === "TABLES");
    // Both schemas' "orders" are exact matches and rank first; order_items
    // only matches as a weak subsequence ("order" + the trailing "s") and
    // trails behind them.
    expect(tables?.items.map((i) => i.id)).toEqual([
      "table:public.orders",
      "table:audit.orders",
      "table:public.order_items",
    ]);
  });

  it("matches a table via its schema-qualified name", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("public.order", { schemaSnapshot: SNAPSHOT, profiles: [] }, acts);
    const tables = groups.find((g) => g.heading === "TABLES");
    expect(tables?.items.map((i) => i.title)).toContain("orders");
    expect(tables?.items.map((i) => i.title)).toContain("order_items");
  });

  it("omits an empty group rather than showing a heading with nothing under it", () => {
    const { actions: acts } = actions();
    const groups = buildGroups("zzz-does-not-exist", { schemaSnapshot: SNAPSHOT, profiles: [profile()] }, acts);
    expect(groups).toEqual([]);
  });

  it("runs the caller's action with the picked table or connection", () => {
    const { actions: acts, openedTables, openedConnections } = actions();
    const groups = buildGroups("orders", { schemaSnapshot: SNAPSHOT, profiles: [profile()] }, acts);
    groups.find((g) => g.heading === "TABLES")!.items[0].run();
    expect(openedTables).toEqual([["public", "orders"]]);

    const empty = buildGroups("", { schemaSnapshot: SNAPSHOT, profiles: [profile()] }, acts);
    empty.find((g) => g.heading === "CONNECTIONS")!.items[0].run();
    expect(openedConnections).toEqual(["p1"]);
  });

  it("has nothing to show without a schema or any connections", () => {
    const { actions: acts } = actions();
    expect(buildGroups("orders", { schemaSnapshot: null, profiles: [] }, acts)).toEqual([]);
    expect(buildGroups("", { schemaSnapshot: null, profiles: [] }, acts)).toEqual([]);
  });
});
