import { afterEach, describe, expect, it, mock } from "bun:test";
import { createProgressBuffer } from "@/lib/progressBuffer";

/**
 * Smoothness 5. Two rules that are easy to regress and cheap to pin down:
 * a run in flight must not empty the pane, and an answer from a superseded run
 * must not overwrite a newer one.
 *
 * The store is exercised through its real actions with the bridge module
 * mocked, so the ordering under test is the store's own.
 */

type Deferred = {
  promise: Promise<unknown> & { cancel: () => void };
  resolve: (value: unknown) => void;
  reject: (reason: unknown) => void;
  cancelled: boolean;
};

function deferred(): Deferred {
  let resolve!: (value: unknown) => void;
  let reject!: (reason: unknown) => void;
  const base = new Promise<unknown>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  const d: Deferred = {
    promise: Object.assign(base, {
      cancel: () => {
        d.cancelled = true;
        const err = new Error("cancelled");
        err.name = "CancelError";
        reject(err);
      },
    }),
    resolve,
    reject,
    cancelled: false,
  };
  return d;
}

const pending: Deferred[] = [];
const schemas: Deferred[] = [];
let cacheRequest: Deferred | undefined;

// The store persists part of its state through the bridge. Outside the app
// that call has no implementation, and its rejection surfaces as an unhandled
// error between unrelated test files; an in-memory storage keeps this test to
// the ordering it is actually about.
mock.module("../../bridge/uistate", () => {
  const mem = new Map<string, unknown>();
  return {
    getUIState: async (key: string) => String(mem.get(key) ?? ""),
    setUIState: async (key: string, value: string) => void mem.set(key, value),
    uiStateStorage: {
      getItem: (key: string) => mem.get(key) ?? null,
      setItem: (key: string, value: unknown) => void mem.set(key, value),
      removeItem: (key: string) => void mem.delete(key),
    },
  };
});

// The store imports these five by name; only runQuery is exercised here, but a
// module mock has to satisfy the whole import list.
const databaseBridge = await import("../../bridge/database");
mock.module("../../bridge/database", () => ({
  ...databaseBridge,
  runQuery: () => {
    const d = deferred();
    pending.push(d);
    return d.promise;
  },
  listProfiles: async () => [],
  closeSession: async () => {},
  listColumns: async () => [],
  getSchemaCache: async () => cacheRequest ? await cacheRequest.promise : ({ namespace: {} }),
  indexSchema: () => { const d = deferred(); schemas.push(d); return d.promise; },
}));

const { useDatabaseStore, visibleResult } = await import("./databaseStore");

const RESULT = (rowCount: number) => ({
  columns: ["n"],
  rows: [[String(rowCount)]],
  rowCount,
  truncated: false,
});

// One console for the whole suite: these tests are about ordering within a
// single console, not about isolation between two. Isolation gets its own test
// at the bottom.
const C = "test-console";

afterEach(() => {
  pending.length = 0;
  schemas.length = 0;
  cacheRequest = undefined;
  useDatabaseStore.setState({ indexingSchema: false, schemaError: "", selectedProfileId: "" });
  useDatabaseStore.setState({ consoles: {} });
});

describe("executeQuery, while it is running", () => {
  it("keeps the previous result on screen and flags the run", async () => {
    useDatabaseStore.setState((s) => ({
      selectedProfileId: "p1",
      consoles: { ...s.consoles, [C]: { ...s.consoleState(C), sql: "select 1", results: [{ statement: "select 1", result: RESULT(7) }] } },
    }));

    const run = useDatabaseStore.getState().executeQuery(C);

    // The old table is still there — the pane does not empty for the length of
    // the statement — and the console knows to dim it.
    expect(visibleResult(useDatabaseStore.getState().consoleState(C))?.rowCount).toBe(7);
    expect(useDatabaseStore.getState().consoleState(C).runningQuery).toBe(true);

    pending[0].resolve(RESULT(9));
    await run;
    expect(visibleResult(useDatabaseStore.getState().consoleState(C))?.rowCount).toBe(9);
    expect(useDatabaseStore.getState().consoleState(C).runningQuery).toBe(false);
  });

  // #41. A second Run while one is in flight is refused, not raced: the
  // console has one slot for the call Stop reaches, and two runs shared it.
  it("refuses a second run while one is in flight, and Stop still reaches the first", async () => {
    useDatabaseStore.setState((s) => ({
      selectedProfileId: "p1",
      consoles: { ...s.consoles, [C]: { ...s.consoleState(C), sql: "select 1" } },
    }));

    const first = useDatabaseStore.getState().executeQuery(C);
    expect(await useDatabaseStore.getState().executeQuery(C)).toBe("skipped");
    expect(pending).toHaveLength(1);

    useDatabaseStore.getState().cancelQuery(C);
    expect(await first).toBe("cancelled");
    expect(pending[0].cancelled).toBe(true);
    expect(useDatabaseStore.getState().consoleState(C).runningQuery).toBe(false);
  });

  // #41. What supersedes a run now is the connection changing under it: the
  // call is cancelled, and nothing it says lands under the new connection.
  it("a connection switch cancels the run and drops what it would have said", async () => {
    useDatabaseStore.setState((s) => ({
      selectedProfileId: "p1",
      consoles: { ...s.consoles, [C]: { ...s.consoleState(C), sql: "select 1" } },
    }));

    const run = useDatabaseStore.getState().executeQuery(C);
    useDatabaseStore.getState().setSelectedProfileId("p2");
    pending[0].resolve(RESULT(1));
    await run;

    const state = useDatabaseStore.getState().consoleState(C);
    expect(pending[0].cancelled).toBe(true);
    expect(state.runningQuery).toBe(false);
    expect(state.queryError).toBe("");
    expect(state.results).toEqual([]);

    // And the console is free to run against the new connection at once.
    const next = useDatabaseStore.getState().executeQuery(C);
    expect(pending).toHaveLength(2);
    pending[1].resolve(RESULT(5));
    await next;
    expect(visibleResult(useDatabaseStore.getState().consoleState(C))?.rowCount).toBe(5);
  });

  it("cancel stops the call and leaves the previous result alone", async () => {
    useDatabaseStore.setState((s) => ({
      selectedProfileId: "p1",
      consoles: { ...s.consoles, [C]: { ...s.consoleState(C), sql: "select 1", results: [{ statement: "select 1", result: RESULT(4) }] } },
    }));

    const run = useDatabaseStore.getState().executeQuery(C);
    useDatabaseStore.getState().cancelQuery(C);
    await run;

    expect(pending[0].cancelled).toBe(true);
    expect(useDatabaseStore.getState().consoleState(C).runningQuery).toBe(false);
    expect(useDatabaseStore.getState().consoleState(C).queryError).toBe("");
    expect(visibleResult(useDatabaseStore.getState().consoleState(C))?.rowCount).toBe(4);
  });

  // The reason run tokens moved per console: two consoles running at once must
  // not invalidate each other. With one shared token the second console's run
  // would supersede the first's, and the first would silently drop its answer.
  it("a run in one console does not supersede a run in another", async () => {
    useDatabaseStore.setState((s) => ({
      selectedProfileId: "p1",
      consoles: {
        ...s.consoles,
        a: { ...s.consoleState("a"), sql: "select 1" },
        b: { ...s.consoleState("b"), sql: "select 2" },
      },
    }));

    const runA = useDatabaseStore.getState().executeQuery("a");
    const runB = useDatabaseStore.getState().executeQuery("b");
    expect(pending).toHaveLength(2);

    pending[1].resolve(RESULT(2));
    await runB;
    pending[0].resolve(RESULT(1));
    await runA;

    // Both answers land. Neither run was superseded, because neither shares a
    // token with the other.
    expect(visibleResult(useDatabaseStore.getState().consoleState("a"))?.rowCount).toBe(1);
    expect(visibleResult(useDatabaseStore.getState().consoleState("b"))?.rowCount).toBe(2);
  });
});

describe("progress buffer", () => {
  it("shows the first item at once and batches the rest onto one beat", async () => {
    const seen: string[][] = [];
    const buffer = createProgressBuffer<string>((batch) => seen.push(batch), 20);

    buffer.push("a");
    expect(seen).toEqual([["a"]]);

    // A burst inside one window costs a single flush, not one per item.
    for (const line of ["b", "c", "d", "e"]) buffer.push(line);
    expect(seen).toHaveLength(1);

    await new Promise((r) => setTimeout(r, 40));
    expect(seen).toEqual([["a"], ["b", "c", "d", "e"]]);
    buffer.dispose();
  });

  it("dispose flushes what is still pending", () => {
    const seen: string[][] = [];
    const buffer = createProgressBuffer<string>((batch) => seen.push(batch), 1000);
    buffer.push("a");
    buffer.push("b");
    buffer.dispose();
    expect(seen).toEqual([["a"], ["b"]]);
  });
});


describe("script lifecycle", () => {
  it("keeps the previous rows until the first statement succeeds, and ignores repeated Run", async () => {
    useDatabaseStore.setState((s) => ({ selectedProfileId: "p1", consoles: {
      [C]: { ...s.consoleState(C), sql: "select 1", results: [{ statement: "old", result: RESULT(7) }] },
    } }));
    const first = useDatabaseStore.getState().runScript(C, ["select 1", "select 2"]);
    await useDatabaseStore.getState().runScript(C, ["select 3"]);
    expect(pending).toHaveLength(1);
    expect(visibleResult(useDatabaseStore.getState().consoleState(C))?.rowCount).toBe(7);
    pending[0].resolve(RESULT(1));
    await Promise.resolve(); await Promise.resolve();
    expect(pending).toHaveLength(2);
    pending[1].resolve(RESULT(2));
    await first;
    expect(useDatabaseStore.getState().consoleState(C).results.map(r => r.result.rowCount)).toEqual([1, 2]);
  });

  it("Cancel on the first statement preserves the previous result and stops the script", async () => {
    useDatabaseStore.setState((s) => ({ selectedProfileId: "p1", consoles: {
      [C]: { ...s.consoleState(C), results: [{ statement: "old", result: RESULT(7) }] },
    } }));
    const script = useDatabaseStore.getState().runScript(C, ["select 1", "select 2"]);
    useDatabaseStore.getState().cancelQuery(C);
    await script;
    expect(pending).toHaveLength(1);
    expect(visibleResult(useDatabaseStore.getState().consoleState(C))?.rowCount).toBe(7);
  });
});

describe("schema request ownership", () => {
  it("refuses duplicate indexing and retains an error until retry", async () => {
    useDatabaseStore.setState({ selectedProfileId: "p1" });
    const first = useDatabaseStore.getState().refreshSchema("p1");
    await useDatabaseStore.getState().refreshSchema("p1");
    expect(schemas).toHaveLength(1);
    schemas[0].reject(new Error("offline")); await first;
    expect(useDatabaseStore.getState().schemaError).toBe("offline");
    const retry = useDatabaseStore.getState().refreshSchema("p1");
    expect(useDatabaseStore.getState().schemaError).toBe("");
    schemas[1].resolve({ namespace: { fresh: {} } }); await retry;
    expect(useDatabaseStore.getState().indexingSchema).toBe(false);
  });

  it("an old cache cannot replace a freshly indexed schema", async () => {
    useDatabaseStore.setState({ selectedProfileId: "p1", indexingSchema: false });
    cacheRequest = deferred();
    const cached = useDatabaseStore.getState().loadSchemaSnapshot("p1");
    const fresh = useDatabaseStore.getState().refreshSchema("p1");
    schemas[0].resolve({ namespace: { fresh: {} } }); await fresh;
    cacheRequest.resolve({ namespace: { stale: {} } }); await cached;
    expect(useDatabaseStore.getState().schemaSnapshot?.namespace).toEqual({ fresh: {} });
  });

  it("drops a late schema error after A → B → A", async () => {
    useDatabaseStore.setState({ selectedProfileId: "p1" });
    const old = useDatabaseStore.getState().refreshSchema("p1");
    useDatabaseStore.getState().setSelectedProfileId("p2");
    useDatabaseStore.getState().setSelectedProfileId("p1");
    schemas[0].reject(new Error("old connection failed")); await old;
    expect(useDatabaseStore.getState().schemaError).toBe("");
    expect(useDatabaseStore.getState().indexingSchema).toBe(false);
  });
});
