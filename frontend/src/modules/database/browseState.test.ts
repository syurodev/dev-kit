import { beforeEach, expect, test } from "bun:test";
import { EMPTY_BROWSE, useDatabaseStore } from "./databaseStore";

beforeEach(() => {
  useDatabaseStore.setState({ browse: {} });
});

test("a table never browsed reads as empty, not undefined", () => {
  const state = useDatabaseStore.getState().browseState("public.orders");
  expect(state).toBe(EMPTY_BROWSE);
  expect(state.where).toBe("");
  expect(state.rows).toBeNull();
  expect(state.cursors).toEqual([]);
});

test("two tables hold their own filter", () => {
  const { setBrowseWhere } = useDatabaseStore.getState();
  setBrowseWhere("public.orders", "status = 'open'");
  setBrowseWhere("public.customers", "");

  expect(useDatabaseStore.getState().browseState("public.orders").where).toBe("status = 'open'");
  expect(useDatabaseStore.getState().browseState("public.customers").where).toBe("");
});

// Changing the filter invalidates the walk: the cursors describe positions in
// a result the new WHERE no longer produces.
test("changing the filter resets the page", () => {
  const { setBrowseWhere } = useDatabaseStore.getState();
  useDatabaseStore.setState((s) => ({
    browse: {
      ...s.browse,
      "public.orders": { ...s.browseState("public.orders"), cursors: [["10"], ["20"]], pageIndex: 2 },
    },
  }));

  setBrowseWhere("public.orders", "status = 'open'");

  const state = useDatabaseStore.getState().browseState("public.orders");
  expect(state.cursors).toEqual([]);
  expect(state.pageIndex).toBe(0);
});

test("changing the sort resets the page for the same reason", () => {
  const { setBrowseOrder } = useDatabaseStore.getState();
  useDatabaseStore.setState((s) => ({
    browse: {
      ...s.browse,
      "public.orders": { ...s.browseState("public.orders"), cursors: [["10"]], pageIndex: 1 },
    },
  }));

  setBrowseOrder("public.orders", { column: "name", direction: "asc" });

  expect(useDatabaseStore.getState().browseState("public.orders").pageIndex).toBe(0);
});
