import { beforeEach, describe, expect, it, mock } from "bun:test";

const calls: string[] = [];
mock.module("@/bridge/database", () => ({
  setTransactionMode: async (id: string, manual: boolean) =>
    void calls.push(`mode:${id}:${manual}`),
  commitTransaction: async (id: string) => void calls.push(`commit:${id}`),
  rollbackTransaction: async (id: string) => void calls.push(`rollback:${id}`),
  transactionState: async () => ({ manual: false, open: false }),
  listProfiles: async () => [],
  listColumns: async () => [],
  getSchemaCache: async () => ({ namespace: {} }),
  indexSchema: async () => ({ namespace: {} }),
  runQuery: () => Promise.resolve({ columns: [], rows: [], rowCount: 0, truncated: false }),
}));

const { useDatabaseStore } = await import("./databaseStore");

beforeEach(() => {
  calls.length = 0;
  useDatabaseStore.setState({
    selectedProfileId: "p1",
    txManual: false,
    txOpen: false,
    txBusy: false,
  });
});

/**
 * The mode is the UI's policy; the backend knows only whether a transaction is
 * open. These are the transitions that policy is made of, and each one could
 * reasonably have gone the other way — which is why they are pinned here
 * rather than left to be re-derived from the buttons.
 */
describe("transaction mode", () => {
  // The question that found this: does opening a table to read it hold locks?
  // Only if a transaction is open, and choosing the mode no longer opens one —
  // the first statement does, in the service. Nothing here begins anything.
  it("chooses the mode and opens nothing", async () => {
    await useDatabaseStore.getState().setTxManual(true);
    expect(calls).toEqual(["mode:p1:true"]);
    expect(useDatabaseStore.getState()).toMatchObject({ txManual: true, txOpen: false });
  });

  // Leaving the mode is what commits: the service does it, so the store asks
  // for the mode change and nothing else.
  it("leaves the mode with one call, and the service commits what was open", async () => {
    useDatabaseStore.setState({ txManual: true, txOpen: true });
    await useDatabaseStore.getState().setTxManual(false);
    expect(calls).toEqual(["mode:p1:false"]);
    expect(useDatabaseStore.getState()).toMatchObject({ txManual: false, txOpen: false });
  });

  // Ending one does not open the next: the next STATEMENT does. Staying in
  // manual mode with nothing open is the state that holds no locks.
  it.each(["commit", "rollback"] as const)("%s ends it without opening another", async (action) => {
    useDatabaseStore.setState({ txManual: true, txOpen: true });
    await useDatabaseStore.getState().endTransaction(action);
    expect(calls).toEqual([`${action}:p1`]);
    expect(useDatabaseStore.getState()).toMatchObject({ txManual: true, txOpen: false });
  });

  it("does nothing with no connection selected", async () => {
    useDatabaseStore.setState({ selectedProfileId: "" });
    await useDatabaseStore.getState().setTxManual(true);
    await useDatabaseStore.getState().endTransaction("commit");
    expect(calls).toEqual([]);
  });
});
