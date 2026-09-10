import { beforeEach, expect, test } from "bun:test";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { isActiveDocument } from "./useIsActiveDocument";

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {} });
});

function openTab(viewId: string, params?: Record<string, unknown>) {
  useWorkbenchTabsStore.getState().openTab({
    moduleId: "database",
    viewId,
    title: viewId,
    params,
    preview: false,
  });
}

test("only the active tab's document reports active", () => {
  openTab("database.erd");
  openTab("database.history"); // opening second makes it active

  const state = useWorkbenchTabsStore.getState();
  expect(isActiveDocument(state, "database.history")).toBe(true);
  expect(isActiveDocument(state, "database.erd")).toBe(false);
});

// Two consoles are the same viewId, so the view alone cannot tell them apart —
// the params have to be part of the answer, or every console tab would render
// its own copy of the overlays.
test("two documents of the same view are told apart by params", () => {
  openTab("database.console", { consoleId: "a" });
  openTab("database.console", { consoleId: "b" });

  const state = useWorkbenchTabsStore.getState();
  expect(isActiveDocument(state, "database.console", { consoleId: "b" })).toBe(true);
  expect(isActiveDocument(state, "database.console", { consoleId: "a" })).toBe(false);
});

// Nothing open means nothing active, or a view mounted for one frame during
// teardown would claim the overlays on its way out.
test("nothing open means nothing active", () => {
  expect(isActiveDocument(useWorkbenchTabsStore.getState(), "database.console")).toBe(false);
});
