import { beforeEach, expect, test } from "bun:test";
import { useWorkbenchTabsStore, moduleTabs } from "@/stores/workbenchTabs";
import { activeApiClientTab, openRequest, renameApiDocument, REQUEST_VIEW } from "./documents";

useWorkbenchTabsStore.persist.setOptions({ storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });
beforeEach(() => useWorkbenchTabsStore.setState({ modules: {}, groups: [], focusedGroupId: "", activeModuleId: "apiclient" }));

test("API selection follows the focused split and reopening keeps document identity", () => {
  const a = openRequest("a", "Users");
  const b = openRequest("b", "Projects");
  useWorkbenchTabsStore.getState().splitTab(b);
  const group = useWorkbenchTabsStore.getState().groups.find((g) => g.activeTabId === a)!;
  useWorkbenchTabsStore.getState().focusGroup(group.id);
  expect(activeApiClientTab(useWorkbenchTabsStore.getState())?.params?.requestId).toBe("a");
  expect(openRequest("a", "Users")).toBe(a);
  expect(moduleTabs(useWorkbenchTabsStore.getState().modules, "apiclient").openTabs.length).toBe(2);
});

test("renaming updates a request title without moving, unpinning or replacing the document", () => {
  const a = openRequest("a", "Users");
  const b = openRequest("b", "Projects");
  useWorkbenchTabsStore.getState().togglePinTab(a);
  const groups = useWorkbenchTabsStore.getState().groups;
  renameApiDocument(REQUEST_VIEW, { requestId: "a" }, "List users");
  const tabs = moduleTabs(useWorkbenchTabsStore.getState().modules, "apiclient").openTabs;
  expect(tabs.find((t) => t.id === a)?.title).toBe("List users");
  expect(tabs.find((t) => t.id === a)?.pinned).toBe(true);
  expect(tabs.find((t) => t.id === b)?.title).toBe("Projects");
  expect(useWorkbenchTabsStore.getState().groups).toEqual(groups);
});

test("a variables reveal can open beside the current request without duplicating its identity", async () => {
  const { openVariables } = await import("./documents");
  const request = openRequest("a", "Users");
  const variables = openVariables("environment", "staging", "Staging", { beside: true });
  const state = useWorkbenchTabsStore.getState();
  expect(state.groups.some((g) => g.activeTabId === request)).toBe(true);
  expect(state.groups.some((g) => g.activeTabId === variables)).toBe(true);
  expect(state.groups.length).toBe(2);
  expect(openVariables("environment", "staging", "Staging")).toBe(variables);
});
