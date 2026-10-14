import { beforeEach, describe, expect, test } from "bun:test";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useDatabaseStore } from "./databaseStore";
import type { StoredConsole } from "@/bridge/database";
import { resetConsoleHydration } from "./consoleHydration";
import { consoleIdFor, openNewConsoleTab, openSqlConsole, openTableTab, sqlConsoleId } from "./documents";

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {}, groups: [], focusedGroupId: "" });
});

describe("table documents", () => {
  test("opening a table opens one tab titled after it", () => {
    openTableTab("chiro", "ba_user");
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
    expect(tabs.openTabs).toHaveLength(1);
    expect(tabs.openTabs[0].title).toBe("ba_user");
  });

  // tabKey dedupes on moduleId+viewId+params, so this is the store's promise
  // rather than ours — the test is here because losing it would silently
  // duplicate a tab on every click.
  test("opening the same table twice focuses the first tab", () => {
    openTableTab("chiro", "ba_user");
    openTableTab("chiro", "ba_user");
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
    expect(tabs.openTabs).toHaveLength(1);
  });

  test("the same table name in two schemas is two tabs", () => {
    openTableTab("chiro", "users");
    openTableTab("billing", "users");
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
    expect(tabs.openTabs).toHaveLength(2);
  });
});

// An agent's statement opens in a console that holds it; the same statement
// twice is the console it already has, and two that start alike are two.
describe("a console for a statement from outside", () => {
  const noFiles = async (): Promise<StoredConsole[]> => [];
  const onDisk = (sql: string, saved: string): StoredConsole => ({
    id: sqlConsoleId(sql),
    profileId: "",
    database: "",
    sql: saved,
    updatedAt: "",
  });

  beforeEach(() => {
    resetConsoleHydration();
    useDatabaseStore.setState({ consoles: {} });
  });

  test("holds the SQL, once per statement", async () => {
    const long = "select id, name, email, created_at from public.users where ";
    await openSqlConsole(`${long}id = 1`, "Query", { load: noFiles });
    await openSqlConsole(`${long}id = 1`, "Query", { load: noFiles });
    await openSqlConsole(`${long}id = 2`, "Query", { load: noFiles });
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
    expect(tabs.openTabs).toHaveLength(2);
    const consoleId = tabs.openTabs[0].params?.consoleId as string;
    expect(useDatabaseStore.getState().consoleState(consoleId).sql).toBe(`${long}id = 1`);
  });

  test("opening it again keeps what the person typed there", async () => {
    await openSqlConsole("select 1", "Agent query", { load: noFiles });
    const consoleId = sqlConsoleId("select 1");
    useDatabaseStore.getState().setSql(consoleId, "select 1 -- mine now");
    await openSqlConsole("select 1", "Agent query", { load: noFiles });
    expect(useDatabaseStore.getState().consoleState(consoleId).sql).toBe("select 1 -- mine now");
    expect(moduleTabs(useWorkbenchTabsStore.getState().modules, "database").openTabs).toHaveLength(1);
  });

  // After a relaunch the rail may not have mounted yet, so nothing has read
  // the files: an empty slice is not an empty console.
  test("a saved edit survives an open that happens before the rail mounts", async () => {
    // Wrapped through setState, not spyOn: zustand copies the state object on
    // every set, so a spy on one copy outlives its restore in the next.
    const original = useDatabaseStore.getState().setSql;
    const writes: string[] = [];
    useDatabaseStore.setState({ setSql: (id, sql) => (writes.push(sql), original(id, sql)) });
    try {
      await openSqlConsole("select 1", "Agent query", {
        load: async () => [onDisk("select 1", "select 1 -- saved edit")],
      });
    } finally {
      useDatabaseStore.setState({ setSql: original });
    }
    expect(useDatabaseStore.getState().consoleState(sqlConsoleId("select 1")).sql).toBe("select 1 -- saved edit");
    expect(writes).toEqual([]);
  });

  // Closing a tab drops the slice and keeps the file.
  test("a console closed since the files were read comes back as it was saved", async () => {
    let disk: StoredConsole[] = [];
    const load = async () => disk;
    await openSqlConsole("select 1", "Agent query", { load });
    disk = [onDisk("select 1", "select 1 -- edited, then closed")];
    useDatabaseStore.setState({ consoles: {} });
    await openSqlConsole("select 1", "Agent query", { load });
    expect(useDatabaseStore.getState().consoleState(sqlConsoleId("select 1")).sql).toBe(
      "select 1 -- edited, then closed",
    );
  });
});

/**
 * Open in Database from a db.query card brings the agent's rows with the
 * statement, so the console shows what the agent saw without running it. The
 * rows go only where they describe the console's text: a console the person
 * has edited keeps its text and gets no rows, and a result of their own is
 * never replaced.
 */
describe("an agent's result comes with its statement", () => {
  const noFiles = async (): Promise<StoredConsole[]> => [];
  const rows = { columns: ["id"], rows: [["7"]], rowCount: 1, truncated: false, rowsAffected: -1 };
  const id = () => sqlConsoleId("select id from t");
  const results = () => useDatabaseStore.getState().consoleState(id()).results;

  beforeEach(() => {
    resetConsoleHydration();
    useDatabaseStore.setState({ consoles: {} });
  });

  test("a new console holds the statement and shows the agent's rows", async () => {
    await openSqlConsole("select id from t", "Agent query", { load: noFiles, result: rows });
    expect(useDatabaseStore.getState().consoleState(id()).sql).toBe("select id from t");
    expect(results()).toEqual([{ statement: "select id from t", result: rows, origin: "agent" }]);
  });

  test("the same statement again, with nothing shown, shows the rows", async () => {
    await openSqlConsole("select id from t", "Agent query", { load: noFiles });
    expect(results()).toEqual([]);
    await openSqlConsole("select id from t", "Agent query", { load: noFiles, result: rows });
    expect(results()[0]?.origin).toBe("agent");
    expect(moduleTabs(useWorkbenchTabsStore.getState().modules, "database").openTabs).toHaveLength(1);
  });

  test("a result the person ran is not replaced", async () => {
    await openSqlConsole("select id from t", "Agent query", { load: noFiles });
    const own = { columns: ["id"], rows: [["1"], ["2"]], rowCount: 2, truncated: false };
    useDatabaseStore.setState({
      consoles: {
        ...useDatabaseStore.getState().consoles,
        [id()]: { ...useDatabaseStore.getState().consoleState(id()), results: [{ statement: "select id from t", result: own }] },
      },
    });
    await openSqlConsole("select id from t", "Agent query", { load: noFiles, result: rows });
    expect(results()).toEqual([{ statement: "select id from t", result: own }]);
  });

  test("a console the person edited keeps its text and gets no rows", async () => {
    await openSqlConsole("select id from t", "Agent query", { load: noFiles });
    useDatabaseStore.getState().setSql(id(), "select id from t where id > 5");
    await openSqlConsole("select id from t", "Agent query", { load: noFiles, result: rows });
    expect(useDatabaseStore.getState().consoleState(id()).sql).toBe("select id from t where id > 5");
    expect(results()).toEqual([]);
  });

  test("an edit saved on disk keeps its text and gets no rows", async () => {
    const saved: StoredConsole = { id: id(), profileId: "", database: "", sql: "select 2", updatedAt: "" };
    await openSqlConsole("select id from t", "Agent query", { load: async () => [saved], result: rows });
    expect(useDatabaseStore.getState().consoleState(id()).sql).toBe("select 2");
    expect(results()).toEqual([]);
  });

  // A run of the person's that failed is still their run: its error is on
  // screen and the agent's rows must not replace it.
  test("a console whose own run failed keeps its error, not the agent's rows", async () => {
    await openSqlConsole("select id from t", "Agent query", { load: noFiles });
    useDatabaseStore.getState().setQueryError(id(), "relation \"t\" does not exist");
    await openSqlConsole("select id from t", "Agent query", { load: noFiles, result: rows });
    expect(results()).toEqual([]);
    expect(useDatabaseStore.getState().consoleState(id()).queryError).toContain("does not exist");
  });

  // Earlier rows of the agent's are not the person's: a second Open brings
  // the newer answer.
  test("opening again refreshes rows that were the agent's", async () => {
    await openSqlConsole("select id from t", "Agent query", { load: noFiles, result: rows });
    const newer = { ...rows, rows: [["8"], ["9"]], rowCount: 2 };
    await openSqlConsole("select id from t", "Agent query", { load: noFiles, result: newer });
    expect(results()).toEqual([{ statement: "select id from t", result: newer, origin: "agent" }]);
  });

  test("rows from a connection nobody named are marked so", async () => {
    await openSqlConsole("select id from t", "Agent query", { load: noFiles, result: rows, connectionUnknown: true });
    expect(results()).toEqual([{ statement: "select id from t", result: rows, origin: "agent", connectionUnknown: true }]);
  });

  test("a console still running is left to its run", async () => {
    await openSqlConsole("select id from t", "Agent query", { load: noFiles });
    useDatabaseStore.setState({
      consoles: { [id()]: { ...useDatabaseStore.getState().consoleState(id()), runningQuery: true } },
    });
    await openSqlConsole("select id from t", "Agent query", { load: noFiles, result: rows });
    expect(results()).toEqual([]);
  });
});

// The + button must give you a console you did not have. tabKey resolves the
// same module+view+params to the same tab, so a fixed id would focus the
// existing console instead of opening a new one.
test("the + button opens a console each time", () => {
  openNewConsoleTab();
  openNewConsoleTab();
  const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
  expect(tabs.openTabs).toHaveLength(2);
});

/**
 * A console id is also the name of the file the console is written to.
 *
 * The store rejects an id that is not a plain file name rather than cleaning
 * it, so an id built from a table called `orders/2024` would silently stop
 * persisting. Deterministic too: reopening the same table has to find the
 * same console, not start a second one beside it.
 */
describe("console ids that have to survive being file names", () => {
  test.each([
    ["public.users", "console-public-users"],
    ["saved-7f3a", "console-saved-7f3a"],
    ["../../devkit", "console-devkit"],
    ["orders/2024", "console-orders-2024"],
    ["a b\tc", "console-a-b-c"],
  ])("%s", (seed, expected) => {
    expect(consoleIdFor(seed)).toBe(expected);
  });

  test("the same table always gets the same console", () => {
    expect(consoleIdFor("public.users")).toBe(consoleIdFor("public.users"));
  });

  test("what comes out is a plain file name, whatever went in", () => {
    for (const seed of ["../..", "/", "  ", "..\\..\\windows", "🙂"]) {
      expect(consoleIdFor(seed)).toMatch(/^[A-Za-z0-9_-]{1,64}$/);
    }
  });
});
