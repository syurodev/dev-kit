import { create } from "zustand";
import type { LucideIcon } from "lucide-react";

export type TabStatus = "idle" | "running" | "success" | "error";

export type WorkbenchTab = {
  id: string;
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  status?: TabStatus;
  icon?: LucideIcon;
  preview?: boolean;
};

export type OpenTabInput = {
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  icon?: LucideIcon;
  preview?: boolean;
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

/** Moves `id` to the front of an MRU list, deduping any earlier entry. */
function pushMru(mru: string[], id: string): string[] {
  return [id, ...mru.filter((existing) => existing !== id)];
}

type WorkbenchTabsState = {
  openTabs: WorkbenchTab[];
  activeTabId: string;
  /** Most-recently-active tab ids, index 0 = most recent. Drives the
   * keyboard quick-switcher's ordering — independent of openTabs' own
   * (drag-reorderable, module-clustered) display order. */
  tabMru: string[];
  openTab: (input: OpenTabInput) => string;
  closeTab: (id: string) => void;
  setActiveTab: (id: string) => void;
  setTabStatus: (id: string, status: TabStatus) => void;
  pinTab: (id: string) => void;
  reorderTab: (fromId: string, beforeId: string | null) => void;
};

export const useWorkbenchTabsStore = create<WorkbenchTabsState>((set, get) => ({
  openTabs: [],
  activeTabId: "",
  tabMru: [],

  openTab: (input) => {
    const id = tabKey(input.moduleId, input.viewId, input.params);
    const state = get();
    const existingIndex = state.openTabs.findIndex((tab) => tab.id === id);

    if (existingIndex !== -1) {
      const existing = state.openTabs[existingIndex];
      if (input.preview === false && existing.preview) {
        // Explicit non-preview open of the tab that's currently the
        // preview tab (double-click, or first edit via pinTab elsewhere)
        // promotes it in place — same slot, no reorder.
        const openTabs = state.openTabs.slice();
        openTabs[existingIndex] = { ...existing, preview: false };
        set({ openTabs, activeTabId: id, tabMru: pushMru(state.tabMru, id) });
        return id;
      }
      set({ activeTabId: id, tabMru: pushMru(state.tabMru, id) });
      return id;
    }

    const tab: WorkbenchTab = {
      id,
      moduleId: input.moduleId,
      viewId: input.viewId,
      title: input.title,
      params: input.params,
      icon: input.icon,
      preview: input.preview ?? false,
    };

    if (input.preview) {
      const previewIndex = state.openTabs.findIndex((t) => t.preview);
      if (previewIndex !== -1) {
        // Replace the current preview tab in place — same array index —
        // so the strip doesn't jump when browsing via single-click.
        const openTabs = state.openTabs.slice();
        openTabs[previewIndex] = tab;
        set({ openTabs, activeTabId: id, tabMru: pushMru(state.tabMru, id) });
        return id;
      }
    }

    set((s) => ({
      openTabs: [...s.openTabs, tab],
      activeTabId: id,
      tabMru: pushMru(s.tabMru, id),
    }));
    return id;
  },

  closeTab: (id) => {
    set((state) => {
      const index = state.openTabs.findIndex((tab) => tab.id === id);
      if (index === -1) {
        return state;
      }
      const openTabs = state.openTabs.filter((tab) => tab.id !== id);
      const tabMru = state.tabMru.filter((existing) => existing !== id);
      if (state.activeTabId !== id) {
        return { openTabs, tabMru };
      }
      // Closing the active tab focuses its former left neighbor, matching
      // VS Code — falling back to the new first tab if we closed index 0.
      const fallback = openTabs[index - 1] ?? openTabs[0];
      return { openTabs, tabMru, activeTabId: fallback?.id ?? "" };
    });
  },

  setActiveTab: (id) =>
    set((state) => ({ activeTabId: id, tabMru: pushMru(state.tabMru, id) })),

  setTabStatus: (id, status) =>
    set((state) => ({
      openTabs: state.openTabs.map((tab) => (tab.id === id ? { ...tab, status } : tab)),
    })),

  pinTab: (id) =>
    set((state) => ({
      openTabs: state.openTabs.map((tab) =>
        tab.id === id ? { ...tab, preview: false } : tab,
      ),
    })),

  // beforeId (not a numeric index) because callers now only ever have a
  // subset of openTabs rendered (one expanded module group at a time) —
  // an id stays correct regardless of what subset produced the drag,
  // where a splice index would have to be translated back into the full
  // array's index space and would be easy to get wrong.
  reorderTab: (fromId, beforeId) =>
    set((state) => {
      if (fromId === beforeId) {
        return state;
      }
      const fromIndex = state.openTabs.findIndex((tab) => tab.id === fromId);
      if (fromIndex === -1) {
        return state;
      }
      const openTabs = state.openTabs.slice();
      const [moved] = openTabs.splice(fromIndex, 1);
      const insertAt = beforeId
        ? openTabs.findIndex((tab) => tab.id === beforeId)
        : -1;
      if (insertAt === -1) {
        openTabs.push(moved);
      } else {
        openTabs.splice(insertAt, 0, moved);
      }
      return { openTabs };
    }),
}));
