import { beforeEach, expect, test } from "bun:test";
import { Package } from "lucide-react";
import { registerModule, type ModuleManifest } from "@/modules/registry";
import { useWorkbenchTabsStore, type ClosedTab } from "@/stores/workbenchTabs";
import { offeredClosedTabs, reopenClosedAt, reopenLastClosed, setUnavailableModules } from "./reopenTab";

useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

const MODULE = "reopen-test";
const manifest: ModuleManifest = {
  id: MODULE,
  title: "Reopen test",
  version: "1.0.0",
  apiVersion: "1",
  icon: Package,
  capabilities: [],
  commands: [],
  views: [
    { id: `${MODULE}.home`, title: "Home", component: () => null },
    { id: `${MODULE}.doc`, title: "Doc", component: () => null },
  ],
};
// The registry is shared across test files and refuses a second registration.
registerModule(manifest);

beforeEach(() => {
  setUnavailableModules(new Set());
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {}, groups: [], focusedGroupId: "", recentlyClosed: [] });
});

const open = (title: string) =>
  useWorkbenchTabsStore.getState().openTab({ moduleId: MODULE, viewId: `${MODULE}.doc`, title, params: { doc: title } });
const close = (id: string) => useWorkbenchTabsStore.getState().closeTab(id);
const titles = () => useWorkbenchTabsStore.getState().modules[MODULE]?.openTabs.map((t) => t.title) ?? [];
const record = (extra: Partial<ClosedTab>): ClosedTab => ({
  moduleId: MODULE, viewId: `${MODULE}.doc`, title: "x", pinned: false, groupId: "g", index: 0, closedAt: 1, ...extra,
});

test("offered: records whose tab is open again are left out, each keeps its place in the stack", () => {
  const a = open("a");
  open("b");
  close(a);
  // A record for a tab that is open (a preview, a background add keeps it).
  useWorkbenchTabsStore.setState((s) => ({
    recentlyClosed: [record({ title: "b", params: { doc: "b" } }), ...s.recentlyClosed],
  }));
  expect(offeredClosedTabs(useWorkbenchTabsStore.getState()).map(({ entry, at }) => [entry.title, at])).toEqual([["a", 1]]);
});

test("the shortcut reopens the newest record that is not open", () => {
  const a = open("a");
  open("b");
  close(a);
  useWorkbenchTabsStore.setState((s) => ({
    recentlyClosed: [record({ title: "b", params: { doc: "b" } }), ...s.recentlyClosed],
  }));
  expect(reopenLastClosed()).toBe(a);
  expect(titles()).toEqual(["a", "b"]);
  expect(reopenLastClosed()).toBeNull();
});

test("a record whose module or view is gone is dropped, not opened", () => {
  useWorkbenchTabsStore.setState({
    recentlyClosed: [record({ moduleId: "gone-module", viewId: "gone-module.doc" }), record({ viewId: `${MODULE}.removed` })],
  });
  expect(reopenClosedAt(1)).toBeNull();
  expect(useWorkbenchTabsStore.getState().recentlyClosed.map((e) => e.moduleId)).toEqual(["gone-module"]);
  expect(reopenLastClosed()).toBeNull();
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toEqual([]);
  expect(titles()).toEqual([]);
});

test("a record of a module the app found unusable is dropped like a gone one", () => {
  const a = open("a");
  close(a);
  setUnavailableModules(new Set([MODULE]));
  expect(reopenLastClosed()).toBeNull();
  expect(useWorkbenchTabsStore.getState().recentlyClosed).toEqual([]);
  expect(titles()).toEqual([]);
});
