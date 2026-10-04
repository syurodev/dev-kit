import { beforeEach, expect, test } from "bun:test";
import { moduleTabs, useWorkbenchTabsStore } from "@/stores/workbenchTabs";
import { useTerminalTabsStore } from "@/stores/terminalTabs";
import { useSSHWorkspaceState } from "./workspaceState";
import { openSSHConnection, activeSSHProfileId, closeSSHConnection, renameSSHConnection } from "./documents";

useWorkbenchTabsStore.persist.setOptions({ storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });
beforeEach(() => {
  useWorkbenchTabsStore.setState({ modules: {}, groups: [], focusedGroupId: "", activeModuleId: "ssh" });
  useSSHWorkspaceState.setState({ entries: {} });
  useTerminalTabsStore.setState({ tabs: [], activeKey: "" });
});
const a = { id: "a", name: "Development" };
const b = { id: "b", name: "Staging" };
const tabs = () => moduleTabs(useWorkbenchTabsStore.getState().modules, "ssh");

test("each connection is a workbench document; reopening focuses rather than duplicates", () => {
  openSSHConnection(a); openSSHConnection(b); openSSHConnection(a);
  expect(tabs().openTabs.map(t => t.title)).toEqual(["Development", "Staging"]);
  expect(tabs().openTabs[0].params).toEqual({ profileId: "a" });
  expect(activeSSHProfileId(useWorkbenchTabsStore.getState())).toBe("a");
  const state = useWorkbenchTabsStore.getState();
  state.setActiveTab(tabs().openTabs[1].id);
  expect(activeSSHProfileId(useWorkbenchTabsStore.getState())).toBe("b");
});

test("connection identity follows the focused split group", () => {
  openSSHConnection(a); openSSHConnection(b);
  const state = useWorkbenchTabsStore.getState();
  state.splitTab(tabs().openTabs[1].id);
  expect(activeSSHProfileId(useWorkbenchTabsStore.getState())).toBe("b");
  const original = useWorkbenchTabsStore.getState().groups.find(g => g.activeTabId === tabs().openTabs[0].id)!;
  state.focusGroup(original.id);
  expect(activeSSHProfileId(useWorkbenchTabsStore.getState())).toBe("a");
});

test("closing a document preserves command drafts and running terminal registrations", () => {
  openSSHConnection(a); openSSHConnection(b);
  useSSHWorkspaceState.getState().patch("a", { command: "uptime", output: "result", busy: "Running" });
  useTerminalTabsStore.setState({ tabs: [{ key: "terminal-a", title: "Remote shell", profileId: "a" }] });
  closeSSHConnection("a");
  expect(tabs().openTabs.map(t => t.params?.profileId)).toEqual(["b"]);
  expect(useSSHWorkspaceState.getState().entries.a.command).toBe("uptime");
  expect(useSSHWorkspaceState.getState().entries.a.busy).toBe("Running");
  expect(useTerminalTabsStore.getState().tabs[0].profileId).toBe("a");
  openSSHConnection(a);
  expect(useSSHWorkspaceState.getState().entries.a.output).toBe("result");
});

test("renaming a profile updates only its tab title without replacing or unpinning it", () => {
  openSSHConnection(a); openSSHConnection(b);
  const id = tabs().openTabs[0].id;
  useWorkbenchTabsStore.getState().togglePinTab(id);
  renameSSHConnection({ ...a, name: "API server" });
  const tab = tabs().openTabs.find(t => t.id === id)!;
  expect(tab.title).toBe("API server"); expect(tab.pinned).toBe(true);
  expect(tabs().openTabs.find(t => t.params?.profileId === "b")?.title).toBe("Staging");
});
