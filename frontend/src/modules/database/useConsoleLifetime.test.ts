import { beforeEach, expect, test } from "bun:test";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { consoleIdOfTab } from "./useConsoleLifetime";
import { DEFAULT_CONSOLE_ID } from "./databaseStore";

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {} });
});

// A table tab's params are {schema, table}, not {consoleId}. A cleanup that
// looked for params.consoleId would never match a table tab and would free its
// console the moment the tab left the mount window — losing the SQL the user
// edited and 200 rows of result.
test("a table tab's console id comes from what it shows", () => {
  expect(consoleIdOfTab("database.table", { schema: "chiro", table: "ba_user" })).toBe(
    "table:chiro.ba_user",
  );
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
