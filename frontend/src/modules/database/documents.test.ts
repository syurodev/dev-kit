import { beforeEach, describe, expect, test } from "bun:test";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { openNewConsoleTab, openTableTab } from "./documents";

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {} });
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

// The + button must give you a console you did not have. tabKey resolves the
// same module+view+params to the same tab, so a fixed id would focus the
// existing console instead of opening a new one.
test("the + button opens a console each time", () => {
  openNewConsoleTab();
  openNewConsoleTab();
  const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "database");
  expect(tabs.openTabs).toHaveLength(2);
});
