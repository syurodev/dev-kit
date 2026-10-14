import { beforeEach, describe, expect, test } from "bun:test";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useDatabaseStore } from "./databaseStore";
import { consoleIdFor, openNewConsoleTab, openSqlConsole, openTableTab } from "./documents";

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
  test("holds the SQL, once per statement", () => {
    const long = "select id, name, email, created_at from public.users where ";
    openSqlConsole(`${long}id = 1`, "Query");
    openSqlConsole(`${long}id = 1`, "Query");
    openSqlConsole(`${long}id = 2`, "Query");
    const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
    expect(tabs.openTabs).toHaveLength(2);
    const consoleId = tabs.openTabs[0].params?.consoleId as string;
    expect(useDatabaseStore.getState().consoleState(consoleId).sql).toBe(`${long}id = 1`);
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
