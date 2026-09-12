import { beforeEach, describe, expect, it, mock } from "bun:test";

const calls: string[] = [];
mock.module("@/bridge/database", () => ({
  beginTransaction: async (id: string) => void calls.push(`begin:${id}`),
  commitTransaction: async (id: string) => void calls.push(`commit:${id}`),
  rollbackTransaction: async (id: string) => void calls.push(`rollback:${id}`),
  transactionOpen: async () => false,
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
  // Not "the next statement opens one": a statement that silently landed
  // outside the transaction the user just asked for is the failure this
  // prevents.
  it("opens a transaction the moment Manual is chosen", async () => {
    await useDatabaseStore.getState().setTxManual(true);
    expect(calls).toEqual(["begin:p1"]);
    expect(useDatabaseStore.getState()).toMatchObject({ txManual: true, txOpen: true });
  });

  // Committing, not rolling back: leaving the mode must not throw away work
  // silently. Discarding it is what the button beside it is for.
  it("commits what is open when leaving Manual", async () => {
    useDatabaseStore.setState({ txManual: true, txOpen: true });
    await useDatabaseStore.getState().setTxManual(false);
    expect(calls).toEqual(["commit:p1"]);
    expect(useDatabaseStore.getState()).toMatchObject({ txManual: false, txOpen: false });
  });

  // Ending a transaction is not leaving the mode, so the next statement needs
  // one of its own — otherwise Manual would quietly become Auto after the
  // first commit.
  it.each(["commit", "rollback"] as const)("opens the next one after %s", async (action) => {
    useDatabaseStore.setState({ txManual: true, txOpen: true });
    await useDatabaseStore.getState().endTransaction(action);
    expect(calls).toEqual([`${action}:p1`, "begin:p1"]);
    expect(useDatabaseStore.getState().txOpen).toBe(true);
  });

  it("ends without reopening when the mode is Auto", async () => {
    useDatabaseStore.setState({ txManual: false, txOpen: true });
    await useDatabaseStore.getState().endTransaction("commit");
    expect(calls).toEqual(["commit:p1"]);
    expect(useDatabaseStore.getState().txOpen).toBe(false);
  });

  it("does nothing with no connection selected", async () => {
    useDatabaseStore.setState({ selectedProfileId: "" });
    await useDatabaseStore.getState().setTxManual(true);
    await useDatabaseStore.getState().endTransaction("commit");
    expect(calls).toEqual([]);
  });
});
