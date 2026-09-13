import { beforeEach, expect, test } from "bun:test";
import { BROWSE_PAGE_SIZE, BROWSE_PAGE_SIZES, EMPTY_BROWSE, useDatabaseStore } from "./databaseStore";

beforeEach(() => {
  useDatabaseStore.setState({ browse: {}, browsePageSize: BROWSE_PAGE_SIZE });
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

// The count is the pager's licence to jump. Without one, the only page it can
// prove exists ahead is the one the probe row found.
test("uncounted, the pager may step forward exactly one page", async () => {
  useDatabaseStore.setState({
    selectedProfileId: "",
    browse: { "public.orders": { ...EMPTY_BROWSE, pageIndex: 2, hasNext: true } },
  });
  const target = { schema: "public", table: "orders" };

  await useDatabaseStore.getState().goToBrowsePage("public.orders", target, 9);
  expect(useDatabaseStore.getState().browseState("public.orders").pageIndex).toBe(2);

  await useDatabaseStore.getState().goToBrowsePage("public.orders", target, 3);
  expect(useDatabaseStore.getState().browseState("public.orders").pageIndex).toBe(3);
});

test("counted, it may jump to any page up to the last", async () => {
  useDatabaseStore.setState({
    selectedProfileId: "",
    // 8,420 rows at 200 a page is pages 0..42.
    browse: { "public.orders": { ...EMPTY_BROWSE, pageIndex: 0, totalRows: 8420 } },
  });
  const target = { schema: "public", table: "orders" };

  expect(useDatabaseStore.getState().lastBrowsePage("public.orders")).toBe(42);

  await useDatabaseStore.getState().goToBrowsePage("public.orders", target, 42);
  expect(useDatabaseStore.getState().browseState("public.orders").pageIndex).toBe(42);

  // One past the end is not a page, counted or not.
  await useDatabaseStore.getState().goToBrowsePage("public.orders", target, 43);
  expect(useDatabaseStore.getState().browseState("public.orders").pageIndex).toBe(42);
});

test("a filter change drops the count with the cursors", () => {
  useDatabaseStore.setState({
    browse: { "public.orders": { ...EMPTY_BROWSE, totalRows: 8420, cursors: [["10"]] } },
  });

  useDatabaseStore.getState().setBrowseWhere("public.orders", "status = 'open'");

  const state = useDatabaseStore.getState().browseState("public.orders");
  expect(state.totalRows).toBeNull();
  expect(useDatabaseStore.getState().lastBrowsePage("public.orders")).toBeNull();
});

// A page size is where the page boundaries are, so changing it invalidates
// every walk in flight for the same reason a new filter does — cursors are
// boundaries, and the boundaries moved.
test("a new page size resets every open tab's walk, and keeps their counts", () => {
  useDatabaseStore.setState({
    browse: {
      "public.orders": { ...EMPTY_BROWSE, pageIndex: 7, cursors: [["10"]], hasNext: true, totalRows: 8420 },
      "public.customers": { ...EMPTY_BROWSE, pageIndex: 2, cursors: [["3"]], hasNext: true },
    },
  });

  useDatabaseStore.getState().setBrowsePageSize(1000);

  expect(useDatabaseStore.getState().browsePageSize).toBe(1000);
  for (const key of ["public.orders", "public.customers"]) {
    const state = useDatabaseStore.getState().browseState(key);
    expect(state.pageIndex).toBe(0);
    expect(state.cursors).toEqual([]);
    expect(state.hasNext).toBe(false);
  }
  // How many rows match is not a function of how many fit on a page.
  expect(useDatabaseStore.getState().browseState("public.orders").totalRows).toBe(8420);
  // 8,420 rows at 1,000 a page is pages 0..8, not 0..42.
  expect(useDatabaseStore.getState().lastBrowsePage("public.orders")).toBe(8);
});

// #38. Rows from the connection you left, under the one you picked, are rows an
// edit writes against the wrong database. The filter and sort are yours and
// stay; everything the server said goes, and runId moves past any load that
// is still on its way back.
test("switching connection drops every tab's rows, key and walk", () => {
  useDatabaseStore.setState({
    selectedProfileId: "prod",
    pageKeys: { "public.orders": ["id"] },
    browse: {
      "public.orders": {
        ...EMPTY_BROWSE,
        where: "status = 'open'",
        orderBy: { column: "id", direction: "desc" },
        rows: { columns: ["id"], rows: [["1"]], rowCount: 1, truncated: false },
        cursors: [["1"]],
        pageIndex: 1,
        totalRows: 5,
        widths: [120],
        runId: 3,
      },
    },
  });

  useDatabaseStore.getState().setSelectedProfileId("dev");

  const state = useDatabaseStore.getState().browseState("public.orders");
  expect(state.rows).toBeNull();
  expect(state.cursors).toEqual([]);
  expect(state.pageIndex).toBe(0);
  expect(state.totalRows).toBeNull();
  expect(state.widths).toEqual([]);
  expect(state.where).toBe("status = 'open'");
  expect(state.orderBy).toEqual({ column: "id", direction: "desc" });
  expect(state.runId).toBe(4);
  expect(useDatabaseStore.getState().pageKeys).toEqual({});
});

test("picking the connection already selected keeps the tabs", () => {
  const rows = { columns: ["id"], rows: [["1"]], rowCount: 1, truncated: false };
  useDatabaseStore.setState({
    selectedProfileId: "prod",
    pageKeys: { "public.orders": ["id"] },
    browse: { "public.orders": { ...EMPTY_BROWSE, rows } },
  });

  useDatabaseStore.getState().setSelectedProfileId("prod");

  expect(useDatabaseStore.getState().browseState("public.orders").rows).toEqual(rows);
  expect(useDatabaseStore.getState().pageKeys).toEqual({ "public.orders": ["id"] });
});

// The service clamps its own row cap, and a size it does not offer would be
// silently clamped into something the pager's arithmetic disagreed with.
test("a page size that is not on offer is refused, not clamped", () => {
  useDatabaseStore.getState().setBrowsePageSize(50_000);
  expect(useDatabaseStore.getState().browsePageSize).toBe(BROWSE_PAGE_SIZE);

  for (const size of BROWSE_PAGE_SIZES) {
    useDatabaseStore.getState().setBrowsePageSize(size);
    expect(useDatabaseStore.getState().browsePageSize).toBe(size);
  }
});

// #52. A staged change is a position on this page, and a change back to what
// the server has is no change at all.
test("staging keeps changes by position and drops one changed back", () => {
  useDatabaseStore.setState({
    browse: {
      "public.orders": {
        ...EMPTY_BROWSE,
        rows: { columns: ["id", "status"], rows: [["1", "open"]], rowCount: 1, truncated: false },
      },
    },
  });
  const { stageBrowseEdits } = useDatabaseStore.getState();

  stageBrowseEdits("public.orders", [{ row: 0, column: 1, value: "closed" }]);
  expect(useDatabaseStore.getState().browseState("public.orders").staged).toEqual({ "0:1": "closed" });

  stageBrowseEdits("public.orders", [{ row: 0, column: 1, value: "open" }]);
  expect(useDatabaseStore.getState().browseState("public.orders").staged).toEqual({});
});

test("a new page, or Revert, clears what was staged", () => {
  useDatabaseStore.setState({
    browse: { "public.orders": { ...EMPTY_BROWSE, staged: { "0:1": "closed" } } },
  });
  useDatabaseStore.getState().revertBrowseEdits("public.orders");
  expect(useDatabaseStore.getState().browseState("public.orders").staged).toEqual({});

  useDatabaseStore.setState({
    browse: { "public.orders": { ...EMPTY_BROWSE, staged: { "0:1": "closed" } } },
  });
  useDatabaseStore.getState().applyBrowsePage("public.orders", {
    result: { columns: ["id"], rows: [["2"]], rowCount: 1, truncated: false },
    pageSize: 200,
    reversed: false,
    pageKey: ["id"],
  });
  expect(useDatabaseStore.getState().browseState("public.orders").staged).toEqual({});
});

