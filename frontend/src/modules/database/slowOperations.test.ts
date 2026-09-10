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

const { useDatabaseStore } = await import("./databaseStore");

const RESULT = (rowCount: number) => ({
  columns: ["n"],
  rows: [[String(rowCount)]],
  rowCount,
  truncated: false,
});

afterEach(() => {
  pending.length = 0;
  useDatabaseStore.setState({
    queryResult: null,
    runningQuery: false,
    queryError: "",
    queryDurationMs: null,
    queryRunId: 0,
    queryStartTime: null,
  });
});

describe("executeQuery, while it is running", () => {
  it("keeps the previous result on screen and flags the run", async () => {
    useDatabaseStore.setState({ selectedProfileId: "p1", sql: "select 1", queryResult: RESULT(7) });

    const run = useDatabaseStore.getState().executeQuery();

    // The old table is still there — the pane does not empty for the length of
    // the statement — and the console knows to dim it.
    expect(useDatabaseStore.getState().queryResult?.rowCount).toBe(7);
    expect(useDatabaseStore.getState().runningQuery).toBe(true);

    pending[0].resolve(RESULT(9));
    await run;
    expect(useDatabaseStore.getState().queryResult?.rowCount).toBe(9);
    expect(useDatabaseStore.getState().runningQuery).toBe(false);
  });

  it("drops the answer of a run that has been superseded", async () => {
    useDatabaseStore.setState({ selectedProfileId: "p1", sql: "select 1" });

    const first = useDatabaseStore.getState().executeQuery();
    const second = useDatabaseStore.getState().executeQuery();
    expect(pending).toHaveLength(2);

    // The newer statement answers first, then the older one lands late.
    pending[1].resolve(RESULT(2));
    await second;
    pending[0].resolve(RESULT(1));
    await first;

    expect(useDatabaseStore.getState().queryResult?.rowCount).toBe(2);
  });

  it("a late failure from a superseded run does not raise an error card", async () => {
    useDatabaseStore.setState({ selectedProfileId: "p1", sql: "select 1" });

    const first = useDatabaseStore.getState().executeQuery();
    const second = useDatabaseStore.getState().executeQuery();

    pending[1].resolve(RESULT(3));
    await second;
    pending[0].reject(new Error("connection reset"));
    await first;

    expect(useDatabaseStore.getState().queryError).toBe("");
    expect(useDatabaseStore.getState().queryResult?.rowCount).toBe(3);
  });

  it("cancel stops the call and leaves the previous result alone", async () => {
    useDatabaseStore.setState({ selectedProfileId: "p1", sql: "select 1", queryResult: RESULT(4) });

    const run = useDatabaseStore.getState().executeQuery();
    useDatabaseStore.getState().cancelQuery();
    await run;

    expect(pending[0].cancelled).toBe(true);
    expect(useDatabaseStore.getState().runningQuery).toBe(false);
    expect(useDatabaseStore.getState().queryError).toBe("");
    expect(useDatabaseStore.getState().queryResult?.rowCount).toBe(4);
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
