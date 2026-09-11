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

// The statement asks for pageSize + 1 rows. The extra one answers "is there a
// next page" without a COUNT(*), and must never reach the grid.
test("the probe row sets hasNext and is not shown", () => {
  const { applyBrowsePage } = useDatabaseStore.getState();
  const rows = Array.from({ length: 201 }, (_, i) => [String(i)]);

  applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows, rowCount: 201, truncated: false },
    pageSize: 200,
    reversed: false,
    pageKey: ["id"],
  });

  const state = useDatabaseStore.getState().browseState("public.orders");
  expect(state.hasNext).toBe(true);
  expect(state.rows?.rows).toHaveLength(200);
});

test("a short page is the last page", () => {
  const { applyBrowsePage } = useDatabaseStore.getState();
  applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows: [["1"], ["2"]], rowCount: 2, truncated: false },
    pageSize: 200,
    reversed: false,
    pageKey: ["id"],
  });

  expect(useDatabaseStore.getState().browseState("public.orders").hasNext).toBe(false);
});

// A1. The server caps every result at DefaultMaxRows = 200 (internal/database/
// database.go) and there is no per-query override, so LIMIT 201 comes back as
// 200 rows with `truncated` set — the probe row never survives the trip. If
// hasNext read the row count alone it would be false on every full page and the
// table could not be paged past row 200 at all. `truncated` is the server
// saying exactly what the probe row would have said.
test("a page the server capped still has a next page", () => {
  const { applyBrowsePage } = useDatabaseStore.getState();
  const rows = Array.from({ length: 200 }, (_, i) => [String(i)]);

  applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows, rowCount: 200, truncated: true },
    pageSize: 200,
    reversed: false,
    pageKey: ["id"],
  });

  const state = useDatabaseStore.getState().browseState("public.orders");
  expect(state.hasNext).toBe(true);
  expect(state.rows?.rows).toHaveLength(200);
});

// Exactly one page of matching rows is not a capped result: the driver ran out
// before the cap, so `truncated` is false and there is genuinely no next page.
test("a full page that the server did not cap is the last page", () => {
  const { applyBrowsePage } = useDatabaseStore.getState();
  const rows = Array.from({ length: 200 }, (_, i) => [String(i)]);

  applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows, rowCount: 200, truncated: false },
    pageSize: 200,
    reversed: false,
    pageKey: ["id"],
  });

  expect(useDatabaseStore.getState().browseState("public.orders").hasNext).toBe(false);
});

// Paging back asks the server for rows before the cursor in DESC order, so
// they arrive bottom-first and have to be flipped before display.
test("a reversed page is flipped back", () => {
  const { applyBrowsePage } = useDatabaseStore.getState();
  applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows: [["3"], ["2"], ["1"]], rowCount: 3, truncated: false },
    pageSize: 200,
    reversed: true,
    pageKey: ["id"],
  });

  expect(useDatabaseStore.getState().browseState("public.orders").rows?.rows).toEqual([
    ["1"],
    ["2"],
    ["3"],
  ]);
});

// A21. A failed step must not renumber the page. loadBrowsePage clears `error`
// before it runs, so an error afterwards belongs to this load — and a toolbar
// reading "201-400" over page 1's rows is the same lie the runId guard exists
// to prevent, reached from the other side.
test("a step whose load fails leaves the page number where it was", async () => {
  useDatabaseStore.setState({
    // No profile selected, so loadBrowsePage returns before clearing anything:
    // a stand-in for any load that ends without delivering a page.
    selectedProfileId: "",
    browse: {
      "public.orders": { ...EMPTY_BROWSE, pageIndex: 1, hasNext: true, error: "boom" },
    },
  });

  await useDatabaseStore.getState().stepBrowsePage(
    "public.orders",
    { schema: "public", table: "orders" },
    1,
  );

  expect(useDatabaseStore.getState().browseState("public.orders").pageIndex).toBe(1);
});
