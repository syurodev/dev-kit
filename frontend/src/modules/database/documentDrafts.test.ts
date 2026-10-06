import { beforeEach, expect, test } from "bun:test";
import { runDraftOperation, useDatabaseDrafts } from "./documentDrafts";
import { useDatabaseStore } from "./databaseStore";

beforeEach(() => {
  useDatabaseDrafts.setState({ save: null, export: null });
});
test("save captures connection and SQL before console/profile changes", () => {
  useDatabaseStore.setState({ selectedProfileId: "original", consoles: {} });
  useDatabaseStore.getState().setSql("scope", "select original");
  useDatabaseStore.getState().openSaveQueryModal("scope");
  useDatabaseStore.setState({ selectedProfileId: "new" });
  useDatabaseStore.getState().setSql("scope", "select changed");
  expect(useDatabaseDrafts.getState().save?.profileId).toBe("original");
  expect(useDatabaseDrafts.getState().save?.sql).toBe("select original");
});
test("a pending operation survives unmount semantics, blocks duplicate submit, close and replacement", async () => {
  const store = useDatabaseDrafts.getState();
  store.openSave("p", "select 1");
  let release!: () => void;
  const wait = new Promise<void>((resolve) => (release = resolve));
  let calls = 0;
  const run = runDraftOperation("save", async () => {
    calls++;
    await wait;
  });
  expect(
    await runDraftOperation("save", async () => {
      calls++;
    }),
  ).toBe(false);
  expect(store.closeSave()).toBe(false);
  expect(store.openSave("other", "wrong")).toBe(false);
  release();
  await run;
  expect(calls).toBe(1);
  expect(useDatabaseDrafts.getState().save?.pending).toBe(false);
});
test("late completion cannot patch a newly opened draft including A to B to A", () => {
  const store = useDatabaseDrafts.getState();
  store.openSave("A", "old");
  const old = useDatabaseDrafts.getState().save!.id;
  store.closeSave();
  store.openSave("B", "middle");
  store.closeSave();
  store.openSave("A", "new");
  store.patchSave(old, { error: "old failure", name: "wrong" });
  expect(useDatabaseDrafts.getState().save?.error).toBe("");
  expect(useDatabaseDrafts.getState().save?.sql).toBe("new");
});
test("export retains captured rows and filename on a new console result", () => {
  const result = {
    columns: ["x"],
    rows: [["old"]],
    rowCount: 1,
    truncated: false,
  };
  useDatabaseStore.getState().openExportModal(result, "old-page");
  expect(useDatabaseDrafts.getState().export?.result).toBe(result);
  const id = useDatabaseDrafts.getState().export!.id;
  useDatabaseDrafts.getState().patchExport(id, { filename: "draft" });
  expect(useDatabaseDrafts.getState().export?.filename).toBe("draft");
  useDatabaseStore.getState().setShowExportModal(false);
  expect(useDatabaseDrafts.getState().export).toBe(null);
});
test("operation failure always releases pending", async () => {
  useDatabaseDrafts.getState().openSave("p", "sql");
  await expect(
    runDraftOperation("save", async () => {
      throw Error("failed");
    }),
  ).rejects.toThrow("failed");
  expect(useDatabaseDrafts.getState().save?.pending).toBe(false);
});

test("repeated export clears the previous receipt before a new picker opens", async () => {
  const store = useDatabaseDrafts.getState();
  store.openExport(
    { columns: ["x"], rows: [["x"]], rowCount: 1, truncated: false },
    "file",
  );
  store.patchExport(useDatabaseDrafts.getState().export!.id, {
    status: "Saved earlier",
  });
  await runDraftOperation("export", async () => {
    expect(useDatabaseDrafts.getState().export?.status).toBe("");
  });
});
