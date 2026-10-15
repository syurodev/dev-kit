import { beforeEach, expect, test } from "bun:test";
import { useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useSeenSuccessStore } from "./seenSuccess";

useWorkbenchTabsStore.persist.setOptions({
  storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
});

beforeEach(() => {
  useWorkbenchTabsStore.setState({ activeModuleId: "", modules: {}, groups: [], focusedGroupId: "", recentlyClosed: [] });
  useSeenSuccessStore.setState({ seen: new Set() });
});

const open = (moduleId: string, title: string) =>
  useWorkbenchTabsStore.getState().openTab({ moduleId, viewId: `${moduleId}.doc`, title, params: { doc: title } });
const seen = () => [...useSeenSuccessStore.getState().seen];

test("a tab that finishes on screen is seen, through a switch to another module, until it closes", () => {
  const store = useWorkbenchTabsStore.getState();
  const job = open("notes", "job");
  store.setTabStatus(job, "success");
  expect(seen()).toEqual([job]);
  // Another module in the same group: no strip shows the job now.
  open("database", "query");
  expect(seen()).toEqual([job]);
  store.closeTab(job);
  expect(seen()).toEqual([]);
});

test("a tab that finishes out of sight is not seen until it is shown", () => {
  const store = useWorkbenchTabsStore.getState();
  const job = open("notes", "job");
  open("notes", "other");
  store.setTabStatus(job, "success");
  expect(seen()).toEqual([]);
  store.setActiveTab(job);
  expect(seen()).toEqual([job]);
  store.setTabStatus(job, "running");
  expect(seen()).toEqual([]);
});
