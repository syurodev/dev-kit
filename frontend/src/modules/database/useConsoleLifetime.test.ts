import { beforeEach, expect, test } from "bun:test";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { browseKeyOfTab, consoleIdOfTab } from "./useConsoleLifetime";
import { DEFAULT_CONSOLE_ID } from "./databaseStore";

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {} });
});

// A table tab stopped owning a console when it stopped being one. Claiming an
// id here would have closeConsole free a console a real console tab might be
// using, and would leave the browse state — a page of rows — behind instead.
test("a table tab owns browse state, not a console", () => {
  expect(consoleIdOfTab("database.table", { schema: "chiro", table: "ba_user" })).toBeNull();
  expect(browseKeyOfTab("database.table", { schema: "chiro", table: "ba_user" })).toBe(
    "chiro.ba_user",
  );
});

test("a console tab owns no browse state", () => {
  expect(browseKeyOfTab("database.console", { consoleId: "c1" })).toBeNull();
});

test("a console tab carries its id directly, and falls back to the default", () => {
  expect(consoleIdOfTab("database.console", { consoleId: "c7" })).toBe("c7");
  expect(consoleIdOfTab("database.console", undefined)).toBe(DEFAULT_CONSOLE_ID);
});

// The ERD and the read-only views own no console, so they must not claim one —
// otherwise closing them would free a console still open in another tab.
test("a view with no console owns none", () => {
  expect(consoleIdOfTab("database.erd")).toBeNull();
  expect(consoleIdOfTab("database.history")).toBeNull();
});
