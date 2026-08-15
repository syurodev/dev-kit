import { create } from "zustand";

export type TabStatus = "idle" | "running" | "success" | "error";

export type WorkbenchTab = {
  id: string;
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  status?: TabStatus;
};

export type OpenTabInput = {
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
};

/**
 * Stable identity for a tab: same moduleId+viewId+params always resolves to
 * the same tab, so opening an already-open note (or module) focuses the
 * existing tab instead of duplicating it. Params are sorted by key so
 * `{a:1,b:2}` and `{b:2,a:1}` collide on purpose.
 */
export function tabKey(
  moduleId: string,
  viewId: string,
  params?: Record<string, unknown>,
): string {
  const keys = params ? Object.keys(params).sort() : [];
  if (keys.length === 0) {
    return `${moduleId}:${viewId}`;
  }
  const suffix = keys.map((key) => `${key}=${String(params![key])}`).join("&");
  return `${moduleId}:${viewId}:${suffix}`;
}

type WorkbenchTabsState = {
  openTabs: WorkbenchTab[];
  activeTabId: string;
  openTab: (input: OpenTabInput) => string;
  closeTab: (id: string) => void;
  setActiveTab: (id: string) => void;
  setTabStatus: (id: string, status: TabStatus) => void;
};

export const useWorkbenchTabsStore = create<WorkbenchTabsState>((set, get) => ({
  openTabs: [],
  activeTabId: "",

  openTab: (input) => {
    const id = tabKey(input.moduleId, input.viewId, input.params);
    const existing = get().openTabs.find((tab) => tab.id === id);
    if (existing) {
      set({ activeTabId: id });
      return id;
    }
    const tab: WorkbenchTab = {
      id,
      moduleId: input.moduleId,
      viewId: input.viewId,
      title: input.title,
      params: input.params,
    };
    set((state) => ({ openTabs: [...state.openTabs, tab], activeTabId: id }));
    return id;
  },

  closeTab: (id) => {
    set((state) => {
      const index = state.openTabs.findIndex((tab) => tab.id === id);
      if (index === -1) {
        return state;
      }
      const openTabs = state.openTabs.filter((tab) => tab.id !== id);
      if (state.activeTabId !== id) {
        return { openTabs };
      }
      // Closing the active tab focuses its former left neighbor, matching
      // VS Code — falling back to the new first tab if we closed index 0.
      const fallback = openTabs[index - 1] ?? openTabs[0];
      return { openTabs, activeTabId: fallback?.id ?? "" };
    });
  },

  setActiveTab: (id) => set({ activeTabId: id }),

  setTabStatus: (id, status) =>
    set((state) => ({
      openTabs: state.openTabs.map((tab) => (tab.id === id ? { ...tab, status } : tab)),
    })),
}));
