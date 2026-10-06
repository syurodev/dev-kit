import { afterEach, expect, mock, test } from "bun:test";
const bridge = await import("@/bridge/database");
let calls = 0;
let finish!: () => void;
let fail!: (error: Error) => void;
mock.module("@/bridge/database", () => ({
  ...bridge,
  applyEdits: () => {
    calls++;
    return new Promise<void>((resolve, reject) => {
      finish = resolve;
      fail = reject;
    });
  },
  transactionState: async () => ({ manual: false, open: false }),
  closeSession: async () => {},
  getSchemaCache: async () => ({ namespace: {} }),
}));
const { useDatabaseStore: S, EMPTY_BROWSE } = await import("./databaseStore");
const originalLoad = S.getState().loadBrowsePage;
const key = "public.users",
  target = { schema: "public", table: "users" };
let loads = 0;
function seed() {
  calls = 0;
  loads = 0;
  S.setState({
    selectedProfileId: "one",
    txManual: false,
    browse: {
      [key]: {
        ...EMPTY_BROWSE,
        rows: {
          columns: ["id", "name"],
          rows: [["1", "old"]],
          rowCount: 1,
          truncated: false,
        },
        staged: { "0:1": "new" },
      },
    },
    loadBrowsePage: async () => {
      loads++;
    },
  });
}
afterEach(() =>
  S.setState({
    browse: {},
    browseFilterDrafts: {},
    loadBrowsePage: originalLoad,
  }),
);
test("serialises Apply, rejects reload/stage/revert while writing, then clears only accepted edits", async () => {
  seed();
  const write = S.getState().applyStagedEdits(key, target, ["UPDATE users"]);
  await expect(
    S.getState().applyStagedEdits(key, target, ["UPDATE again"]),
  ).rejects.toThrow("Wait for");
  S.getState().stageBrowseEdits(key, [
    { row: 0, column: 1, value: "changed while pending" },
  ]);
  S.getState().revertBrowseEdits(key);
  expect(S.getState().browseState(key).staged).toEqual({ "0:1": "new" });
  const page = S.getState().browseState(key);
  await originalLoad(key, target);
  S.getState().setBrowseWhere(key, "id > 99");
  S.getState().setBrowseOrder(key, { column: "name", direction: "asc" });
  await expect(S.getState().setTxManual(true)).rejects.toThrow(
    "Wait for table changes",
  );
  await expect(S.getState().endTransaction("commit")).rejects.toThrow(
    "Wait for table changes",
  );
  expect(S.getState().browseState(key)).toBe(page);
  expect(calls).toBe(1);
  finish();
  await write;
  expect(S.getState().browseState(key)).toMatchObject({
    editing: false,
    staged: {},
    editStatus: "Changes applied.",
  });
  expect(loads).toBe(1);
});
test("failure keeps staged edits and an error in their table", async () => {
  seed();
  const write = S.getState().applyStagedEdits(key, target, ["UPDATE users"]);
  fail(new Error("constraint rejected"));
  await expect(write).rejects.toThrow("constraint rejected");
  expect(S.getState().browseState(key)).toMatchObject({
    editing: false,
    staged: { "0:1": "new" },
    editError: "constraint rejected",
  });
  expect(loads).toBe(0);
});
test("late write completion after A → B → A neither reloads nor erases a new page", async () => {
  seed();
  const write = S.getState().applyStagedEdits(key, target, ["UPDATE users"]);
  S.getState().setSelectedProfileId("two");
  S.getState().setSelectedProfileId("one");
  S.setState({
    browse: {
      [key]: { ...EMPTY_BROWSE, runId: 9, staged: { "0:1": "new page" } },
    },
  });
  expect(S.getState().writingTables[key]).toBe(true);
  finish();
  await expect(write).rejects.toThrow("Table target changed");
  expect(S.getState().writingTables).toEqual({});
  expect(S.getState().browseState(key).staged).toEqual({ "0:1": "new page" });
  expect(loads).toBe(0);
});
test("typing a filter draft does not change row identity, applied filter or count", () => {
  seed();
  const before = S.getState().browseState(key);
  S.getState().setBrowseFilterDraft(key, "name = 'Ada'");
  expect(S.getState().browseState(key)).toBe(before);
  expect(S.getState().browseState(key).where).toBe("");
  expect(loads).toBe(0);
});
test("a reload failure after an accepted write never restores edits for another Apply", async () => {
  seed();
  S.setState({
    loadBrowsePage: async () => {
      throw new Error("reload failed");
    },
  });
  const write = S.getState().applyStagedEdits(key, target, ["UPDATE users"]);
  finish();
  await expect(write).rejects.toThrow("reload failed");
  expect(S.getState().browseState(key).staged).toEqual({});
  expect(S.getState().browseState(key).editing).toBe(false);
  expect(calls).toBe(1);
});
test("closing a table removes a filter draft even before any rows or metadata arrive", () => {
  S.setState({
    browse: {},
    pageKeys: {},
    browseFilterDrafts: { [key]: "unsent" },
  });
  S.getState().closeBrowse(key);
  expect(S.getState().browseFilterDrafts).toEqual({});
});
