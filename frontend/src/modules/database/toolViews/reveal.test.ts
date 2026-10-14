import { describe, expect, test } from "bun:test";
import { reveal, revealTarget, type RevealDeps } from "./reveal";

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

describe("a table read on a connection opens on that connection", () => {
  test("revealTarget carries the ref's profile", () => {
    expect(revealTarget({ ...ref("table", "public.orders"), profileId: "p2" })).toEqual({
      open: "table",
      schema: "public",
      table: "orders",
      profileId: "p2",
    });
    expect(revealTarget({ ...ref("table", "public.orders"), profileId: "" })).toEqual({ open: "table", schema: "public", table: "orders" });
  });

  /** A store with these profiles, `selected` chosen; each open records the profile it browsed on. */
  const fake = (profiles: string[], selected: string, later: string[] = profiles) => {
    let known = profiles;
    let current = selected;
    const log: string[] = [];
    const deps: RevealDeps = {
      profileIds: () => known,
      selectedProfileId: () => current,
      refreshProfiles: async () => {
        log.push("refresh");
        known = later;
      },
      selectProfile: (id) => {
        log.push(`select ${id}`);
        current = id;
      },
      openTable: (schema, table, beside) => {
        log.push(`open ${schema}.${table} on ${current}${beside ? " beside" : ""}`);
      },
      openConsole: async (sql) => {
        log.push(`console ${sql} on ${current}`);
      },
    };
    return { deps, log };
  };

  test("with no profile it opens on the selected connection, as before", async () => {
    const { deps, log } = fake(["p1", "p2"], "p1");
    expect(await reveal(ref("table", "public.orders"), "beside", deps)).toBe("ok");
    expect(log).toEqual(["open public.orders on p1 beside"]);
  });

  test("another profile is selected first, the way the rail selects it", async () => {
    const { deps, log } = fake(["p1", "p2"], "p1");
    expect(await reveal({ ...ref("table", "public.orders"), profileId: "p2" }, "replace", deps)).toBe("ok");
    expect(log).toEqual(["select p2", "open public.orders on p2"]);
  });

  test("the selected profile is not selected again", async () => {
    const { deps, log } = fake(["p1", "p2"], "p2");
    await reveal({ ...ref("table", "public.orders"), profileId: "p2" }, "replace", deps);
    expect(log).toEqual(["open public.orders on p2"]);
  });

  test("a profile not loaded yet is asked for once before giving up", async () => {
    const { deps, log } = fake([], "", ["p1", "p3"]);
    expect(await reveal({ ...ref("table", "public.orders"), profileId: "p3" }, "replace", deps)).toBe("ok");
    expect(log).toEqual(["refresh", "select p3", "open public.orders on p3"]);
  });

  test("a profile this machine does not have is unavailable, and nothing opens", async () => {
    const { deps, log } = fake(["p1"], "p1");
    expect(await reveal({ ...ref("table", "public.orders"), profileId: "elsewhere" }, "replace", deps)).toBe("unavailable");
    expect(log).toEqual(["refresh"]);
  });

  test("a selection that did not take never browses on the wrong profile", async () => {
    const { deps, log } = fake(["p1", "p2"], "p1");
    deps.selectProfile = (id) => log.push(`select ${id} (refused)`);
    expect(await reveal({ ...ref("table", "public.orders"), profileId: "p2" }, "replace", deps)).toBe("unavailable");
    expect(log).toEqual(["select p2 (refused)"]);
  });

  test("an unopenable ref is unavailable; a console still opens", async () => {
    const { deps, log } = fake(["p1"], "p1");
    expect(await reveal(ref("table", "orders"), "replace", deps)).toBe("unavailable");
    expect(await reveal(ref("console", "select 1"), "replace", deps)).toBe("ok");
    expect(log).toEqual(["console select 1 on p1"]);
  });
});
