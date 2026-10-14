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
