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
mock.module("../../bridge/database", () => ({
  runQuery: () => {
    const d = deferred();
    pending.push(d);
    return d.promise;
  },
  listProfiles: async () => [],
  listColumns: async () => [],
  getSchemaCache: async () => null,
  indexSchema: async () => null,
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

  it("drops the answer of a run that has been superseded", async () => {
    useDatabaseStore.setState((s) => ({
      selectedProfileId: "p1",
      consoles: { ...s.consoles, [C]: { ...s.consoleState(C), sql: "select 1" } },
    }));

    const first = useDatabaseStore.getState().executeQuery(C);
    const second = useDatabaseStore.getState().executeQuery(C);
    expect(pending).toHaveLength(2);

    // The newer statement answers first, then the older one lands late.
    pending[1].resolve(RESULT(2));
    await second;
    pending[0].resolve(RESULT(1));
    await first;

    expect(visibleResult(useDatabaseStore.getState().consoleState(C))?.rowCount).toBe(2);
  });

  it("a late failure from a superseded run does not raise an error card", async () => {
    useDatabaseStore.setState((s) => ({
      selectedProfileId: "p1",
      consoles: { ...s.consoles, [C]: { ...s.consoleState(C), sql: "select 1" } },
    }));

    const first = useDatabaseStore.getState().executeQuery(C);
    const second = useDatabaseStore.getState().executeQuery(C);

    pending[1].resolve(RESULT(3));
    await second;
    pending[0].reject(new Error("connection reset"));
    await first;

    expect(useDatabaseStore.getState().consoleState(C).queryError).toBe("");
    expect(visibleResult(useDatabaseStore.getState().consoleState(C))?.rowCount).toBe(3);
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
