import { beforeEach, expect, test } from "bun:test";
import {
  confirmSavedQueryDeletion,
  useSavedQueryDeletion,
} from "./savedQueryDeletion";
beforeEach(() => useSavedQueryDeletion.setState({ intent: null }));
test("delete blocks duplicates and replacement and invalidates captured connection", async () => {
  const store = useSavedQueryDeletion.getState();
  store.open("A", { id: "query-A", sql: "select A" });
  let release!: () => void;
  const wait = new Promise<void>((r) => (release = r));
  let calls = 0;
  const scopes: string[] = [];
  const run = confirmSavedQueryDeletion(
    (p) => scopes.push(p),
    async (id) => {
      expect(id).toBe("query-A");
      calls++;
      await wait;
    },
  );
  store.close();
  store.open("B", { id: "query-B", sql: "B" });
  await confirmSavedQueryDeletion(
    () => {},
    async () => {
      calls++;
    },
  );
  expect(useSavedQueryDeletion.getState().intent?.profileId).toBe("A");
  release();
  await run;
  expect(calls).toBe(1);
  expect(scopes).toEqual(["A"]);
  expect(useSavedQueryDeletion.getState().intent).toBe(null);
});
test("failure keeps confirmation target for retry", async () => {
  useSavedQueryDeletion.getState().open("A", { id: "query", sql: "sql" });
  await confirmSavedQueryDeletion(
    () => {},
    async () => {
      throw Error("refused");
    },
  );
  expect(useSavedQueryDeletion.getState().intent?.pending).toBe(false);
  expect(useSavedQueryDeletion.getState().intent?.entry.id).toBe("query");
  expect(useSavedQueryDeletion.getState().intent?.error).toContain("refused");
});
