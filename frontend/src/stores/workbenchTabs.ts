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

/**
 * The module a tab id belongs to. tabKey always leads with the moduleId and
 * module ids cannot contain ":" (registry's identifierPattern), so the first
 * segment is authoritative — every id-taking action below routes to its
 * partition through this and never has to be told the module separately.
 */
export function moduleOfTab(id: string): string {
  return id.slice(0, id.indexOf(":") === -1 ? id.length : id.indexOf(":"));
}

/** Moves `id` to the front of an MRU list, deduping any earlier entry. */
function pushMru(mru: string[], id: string): string[] {
  return [id, ...mru.filter((existing) => existing !== id)];
}

/**
 * One module's tabs. Nothing in here knows another module exists: the strip,
 * the ⌘⇧E switcher, preview replacement and close-fallback all operate on one
 * of these at a time, which is what "Notes' tabs are Notes' business" means
 * in practice (module-tabs.mdx, Design: per-module ownership).
 */
export type ModuleTabs = {
  openTabs: WorkbenchTab[];
  activeTabId: string;
  /** Most-recently-active tab ids, index 0 = most recent. Drives the
   * keyboard quick-switcher's ordering — independent of openTabs' own
   * (drag-reorderable) display order. */
  tabMru: string[];
};

export const EMPTY_MODULE_TABS: ModuleTabs = Object.freeze({
  openTabs: [],
  activeTabId: "",
  tabMru: [],
}) as ModuleTabs;

type WorkbenchTabsState = {
  /** The module the content area is showing. Set by the Dock, by opening or
   * activating any tab, and by nothing else. */
  activeModuleId: string;
  /** Tab state per module, keyed by moduleId. A module that has never opened
   * a tab has no entry; read through moduleTabs() for the empty default. */
  modules: Record<string, ModuleTabs>;
  /** Switch the content area to a module. Its own last-active tab stays
   * active (sticky). Does NOT open anything — a module with no tabs is the
   * caller's cue to open its home view. */
  focusModule: (moduleId: string) => void;
  openTab: (input: OpenTabInput) => string;
  closeTab: (id: string) => void;
  setActiveTab: (id: string) => void;
  setTabStatus: (id: string, status: TabStatus) => void;
  pinTab: (id: string) => void;
  reorderTab: (fromId: string, beforeId: string | null) => void;
};

export function moduleTabs(
  modules: Record<string, ModuleTabs>,
  moduleId: string,
): ModuleTabs {
  return modules[moduleId] ?? EMPTY_MODULE_TABS;
}

/** Selector for the partition of the module currently on screen. */
export function selectActiveModuleTabs(state: {
  activeModuleId: string;
  modules: Record<string, ModuleTabs>;
}): ModuleTabs {
  return moduleTabs(state.modules, state.activeModuleId);
}

/**
 * Rewrites one module's partition. Returning the same partition object from
 * `update` publishes no new state at all, which is what keeps a no-op reorder
 * from re-rendering every mounted module view (workbenchTabReorder.test.ts).
 */
function updateModule(
  state: WorkbenchTabsState,
  moduleId: string,
  update: (current: ModuleTabs) => ModuleTabs,
  extra?: Partial<Pick<WorkbenchTabsState, "activeModuleId">>,
): Partial<WorkbenchTabsState> | WorkbenchTabsState {
  const current = moduleTabs(state.modules, moduleId);
  const next = update(current);
  if (next === current && (!extra || extra.activeModuleId === state.activeModuleId)) {
    return state;
  }
  return {
    ...extra,
    modules: next === current ? state.modules : { ...state.modules, [moduleId]: next },
  };
}

export const useWorkbenchTabsStore = create<WorkbenchTabsState>((set) => ({
  activeModuleId: "",
  modules: {},

  focusModule: (moduleId) =>
    set((state) => (state.activeModuleId === moduleId ? state : { activeModuleId: moduleId })),

  openTab: (input) => {
    const id = tabKey(input.moduleId, input.viewId, input.params);
    set((state) =>
      updateModule(
        state,
        input.moduleId,
        (m) => {
          const existingIndex = m.openTabs.findIndex((tab) => tab.id === id);

          if (existingIndex !== -1) {
            const existing = m.openTabs[existingIndex];
            if (input.preview === false && existing.preview) {
              // Explicit non-preview open of the tab that's currently the
              // preview tab (double-click, or first edit via pinTab elsewhere)
              // promotes it in place — same slot, no reorder.
              const openTabs = m.openTabs.slice();
              openTabs[existingIndex] = { ...existing, preview: false };
              return { openTabs, activeTabId: id, tabMru: pushMru(m.tabMru, id) };
            }
            return { ...m, activeTabId: id, tabMru: pushMru(m.tabMru, id) };
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
            // One preview slot per module, not one for the whole app: leaving
            // Notes for SSH via the Dock must not evict the note being
            // previewed, because coming back has to find it exactly as left
            // (module-tabs.mdx: state survives leaving a module).
            const previewIndex = m.openTabs.findIndex((t) => t.preview);
            if (previewIndex !== -1) {
              // Replace the current preview tab in place — same array index —
              // so the strip doesn't jump when browsing via single-click.
              const openTabs = m.openTabs.slice();
              openTabs[previewIndex] = tab;
              return { openTabs, activeTabId: id, tabMru: pushMru(m.tabMru, id) };
            }
          }

          return {
            openTabs: [...m.openTabs, tab],
            activeTabId: id,
            tabMru: pushMru(m.tabMru, id),
          };
        },
        // Opening a tab means looking at it, so its module becomes the one on
        // screen — same as before ownership was split, when a single global
        // activeTabId implied the module.
        { activeModuleId: input.moduleId },
      ),
    );
    return id;
  },

  closeTab: (id) =>
    set((state) =>
      updateModule(state, moduleOfTab(id), (m) => {
        const index = m.openTabs.findIndex((tab) => tab.id === id);
        if (index === -1) {
          return m;
        }
        const openTabs = m.openTabs.filter((tab) => tab.id !== id);
        const tabMru = m.tabMru.filter((existing) => existing !== id);
        if (m.activeTabId !== id) {
          return { ...m, openTabs, tabMru };
        }
        // Closing the active tab focuses its former left neighbor, matching
        // VS Code — falling back to the new first tab if we closed index 0.
        // Closing the last one leaves the partition empty; Workbench reads
        // that as "show this module's home view", not "jump modules".
        const fallback = openTabs[index - 1] ?? openTabs[0];
        return { openTabs, tabMru, activeTabId: fallback?.id ?? "" };
      }),
    ),

  setActiveTab: (id) => {
    const moduleId = moduleOfTab(id);
    set((state) =>
      updateModule(
        state,
        moduleId,
        (m) => {
          if (m.activeTabId === id && m.tabMru[0] === id) {
            return m;
          }
          return { ...m, activeTabId: id, tabMru: pushMru(m.tabMru, id) };
        },
        { activeModuleId: moduleId },
      ),
    );
  },

  setTabStatus: (id, status) =>
    set((state) =>
      updateModule(state, moduleOfTab(id), (m) => {
        if (!m.openTabs.some((tab) => tab.id === id && tab.status !== status)) {
          return m;
        }
        return {
          ...m,
          openTabs: m.openTabs.map((tab) => (tab.id === id ? { ...tab, status } : tab)),
        };
      }),
    ),

  pinTab: (id) =>
    set((state) =>
      updateModule(state, moduleOfTab(id), (m) => {
        if (!m.openTabs.some((tab) => tab.id === id && tab.preview)) {
          return m;
        }
        return {
          ...m,
          openTabs: m.openTabs.map((tab) =>
            tab.id === id ? { ...tab, preview: false } : tab,
          ),
        };
      }),
    ),

  // beforeId (not a numeric index) because callers only ever have their own
  // module's tabs rendered — an id stays correct regardless of what subset
  // produced the drag, where a splice index would have to be translated
  // back into the array's index space and would be easy to get wrong.
  reorderTab: (fromId, beforeId) =>
    set((state) => {
      if (fromId === beforeId) {
        return state;
      }
      return updateModule(state, moduleOfTab(fromId), (m) => {
        const fromIndex = m.openTabs.findIndex((tab) => tab.id === fromId);
        if (fromIndex === -1) {
          return m;
        }
        const openTabs = m.openTabs.slice();
        const [moved] = openTabs.splice(fromIndex, 1);
        const insertAt = beforeId ? openTabs.findIndex((tab) => tab.id === beforeId) : -1;
        if (insertAt === -1) {
          openTabs.push(moved);
        } else {
          openTabs.splice(insertAt, 0, moved);
        }
        if (openTabs.every((tab, index) => tab === m.openTabs[index])) {
          return m;
        }
        return { ...m, openTabs };
      });
    }),
}));

/** Every open tab across every module, in module-insertion order. For the
 * views layer, which keeps recently-left modules mounted; the strip and the
 * switcher must NOT use this — they show one module. */
export function allOpenTabs(modules: Record<string, ModuleTabs>): WorkbenchTab[] {
  return Object.values(modules).flatMap((m) => m.openTabs);
}
