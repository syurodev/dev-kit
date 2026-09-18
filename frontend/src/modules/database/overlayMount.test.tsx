import { beforeEach, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { DEFAULT_CONSOLE_ID } from "./databaseStore";
import { isActiveDocument } from "./useIsActiveDocument";

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {}, groups: [], focusedGroupId: "" });
});

/**
 * The overlays — every dialog the module has — mount only on the active
 * document. The module's home tab is opened by the Workbench with no params at
 * all, while the console inside defaults a missing consoleId to "console-1", so
 * asking to be active under the defaulted id names a tab that does not exist:
 * nothing is active on a fresh visit and no dialog ever renders. That is what
 * made Connection options do nothing, from the row button and the context menu
 * alike.
 */
test("the home tab's identity is its own params, not the defaulted console id", () => {
  useWorkbenchTabsStore.getState().openTab({
    moduleId: "database",
    viewId: "database.console",
    title: "Database",
    preview: true,
  });
  const state = useWorkbenchTabsStore.getState();

  expect(isActiveDocument(state, "database.console", undefined)).toBe(true);
  expect(isActiveDocument(state, "database.console", { consoleId: DEFAULT_CONSOLE_ID })).toBe(
    false,
  );
});

// The rule above only holds if the view asks with the params it was handed.
// react-dom/server cannot check it by rendering: zustand serves the *initial*
// state as its server snapshot, so a store set up by a test is invisible to an
// SSR render and both the fixed and the broken version draw nothing.
test("DatabaseView asks with the tab's own params", () => {
  const source = readFileSync(join(import.meta.dir, "DatabaseView.tsx"), "utf8");
  expect(source).toContain('useIsActiveDocument("database.console", params)');
});
