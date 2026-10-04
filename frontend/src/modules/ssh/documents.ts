import { Terminal } from "lucide-react";
import { moduleTabs, useWorkbenchTabsStore, type WorkbenchTabsState } from "@/stores/workbenchTabs";

const VIEW = "ssh.connections";
export function openSSHConnection(profile: { id: string; name: string }): void {
  useWorkbenchTabsStore.getState().openTab({ moduleId: "ssh", viewId: VIEW, title: profile.name,
    icon: Terminal, params: { profileId: profile.id }, preview: false });
}

export function activeSSHProfileId(state: WorkbenchTabsState): string {
  const tabs = moduleTabs(state.modules, "ssh");
  const group = state.groups.find(g => g.id === state.focusedGroupId && g.moduleId === "ssh")
    ?? state.groups.find(g => g.moduleId === "ssh");
  const tab = tabs.openTabs.find(t => t.id === (group?.activeTabId ?? tabs.activeTabId));
  return typeof tab?.params?.profileId === "string" ? tab.params.profileId : "";
}

export function closeSSHConnection(profileId: string): void {
  const state = useWorkbenchTabsStore.getState();
  for (const tab of moduleTabs(state.modules, "ssh").openTabs) {
    if (tab.params?.profileId === profileId) state.closeTab(tab.id);
  }
}

export function renameSSHConnection(profile: { id: string; name: string }): void {
  useWorkbenchTabsStore.setState(state => {
    const tabs = moduleTabs(state.modules, "ssh");
    if (!tabs.openTabs.some(t => t.params?.profileId === profile.id && t.title !== profile.name)) return state;
    return { modules: { ...state.modules, ssh: { ...tabs, openTabs: tabs.openTabs.map(t =>
      t.params?.profileId === profile.id ? { ...t, title: profile.name } : t) } } };
  });
}
