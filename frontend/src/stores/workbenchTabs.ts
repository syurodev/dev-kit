import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { LucideIcon } from "lucide-react";
import { uiStateStorage } from "@/bridge/uistate";
import { viewIcon } from "@/modules/registry";
import {
  MAX_GROUPS,
  dropIfEmpty,
  focusedGroup,
  groupOfTab,
  neighbourIn,
  newGroupId,
  reconcile,
  tabsOfGroup,
  targetGroup,
  withGroup,
  type DocGroup,
} from "./documentGroups";

export type TabStatus = "idle" | "running" | "success" | "error";

export type WorkbenchTab = {
  id: string;
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  status?: TabStatus;
  /** Unsaved work in the document: drawn as a dot in the close slot. Never persisted, like status. */
  dirty?: boolean;
  icon?: LucideIcon;
  preview?: boolean;
  /**
   * Pinned: kept at the left of the strip, and passed over by the group
   * closes (Close Others, to the Left/Right, All) — VS Code's sticky tab.
   */
  pinned?: boolean;
  /**
   * The document group it sits in (stores/documentGroups.ts). Unset, or a
   * group that is gone: the first group showing its module.
   */
  group?: string;
};

export type CloseTabOptions = {
  /** Offer the tab to reopenClosedTab afterwards. Default true. */
  remember?: boolean;
};

export type OpenTabInput = {
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  icon?: LucideIcon;
  preview?: boolean;
  /**
   * Open beside the focused group rather than in it: in another group showing
   * the module, or a new one to its right (docs/dev-kit/layout.mdx § Mở một
   * group). ⌘-click in a sidebar sets it through armBeside, and it is what a
   * cross-module hand-off will pass as placement "beside".
   */
  beside?: boolean;
};

/**
 * ⌘-click in a sidebar means "open beside" for whatever that click opens.
 * The sidebar arms it in the capture phase of the click, and the openTab the
 * same click makes consumes it — so no module's sidebar had to learn about it.
 * Disarmed after the event either way.
 */
let besideArmed = false;
export function armBeside() {
  besideArmed = true;
  setTimeout(() => {
    besideArmed = false;
  }, 0);
}

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

/** A closed tab, as `reopenClosedTab` needs it back. */
export type ClosedTab = {
  moduleId: string;
  viewId: string;
  title: string;
  params?: Record<string, unknown>;
  icon?: LucideIcon;
  pinned: boolean;
  /** The group it sat in, and its position in that group's strip. */
  groupId: string;
  index: number;
  closedAt: number;
};

/** Enough to undo a burst of closes, small enough to read as a menu. */
export const MAX_RECENTLY_CLOSED = 20;

/** The id the record's tab had — and will have again: tabKey is the document's identity. */
export const closedTabId = (entry: ClosedTab) => tabKey(entry.moduleId, entry.viewId, entry.params);

/**
 * The stack without `id`'s records; the same array when it had none, so nothing
 * is published for it. Exported for code that adds a tab without openTab
 * (a background add writes the partition itself) and must forget it too.
 */
export function forgetClosed(stack: ClosedTab[], id: string): ClosedTab[] {
  return stack.some((entry) => closedTabId(entry) === id) ? stack.filter((entry) => closedTabId(entry) !== id) : stack;
}

/**
 * The stack with each record `title` names a new title for renamed — so a
 * document renamed after its tab closed is offered back under its new name.
 * The same array when nothing changed. For the helpers that retitle open tabs.
 */
export function retitleClosedTabs(stack: ClosedTab[], title: (entry: ClosedTab) => string | undefined): ClosedTab[] {
  let changed = false;
  const next = stack.map((entry) => {
    const renamed = title(entry);
    if (renamed === undefined || renamed === entry.title) return entry;
    changed = true;
    return { ...entry, title: renamed };
  });
  return changed ? next : stack;
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

/**
 * Where "beside" opens: another group already showing the module, else a new
 * group right of the focused one — or, at the group limit, where a plain open
 * would have gone.
 */
function besideGroup(s: WorkbenchTabsState, moduleId: string): { groups: DocGroup[]; group: DocGroup } {
  const focus = focusedGroup(s);
  const other = s.groups.find((g) => g.moduleId === moduleId && g.id !== focus?.id);
  if (other) return { groups: s.groups, group: other };
  if (!focus || s.groups.length >= MAX_GROUPS) return targetGroup(s, moduleId);
  const created: DocGroup = { id: newGroupId(), moduleId, activeTabId: "", weight: focus.weight / 2 };
  const groups = withGroup(s.groups, focus.id, { weight: focus.weight / 2 });
  groups.splice(groups.findIndex((g) => g.id === focus.id) + 1, 0, created);
  return { groups, group: created };
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

export type WorkbenchTabsState = {
  /** The module the content area is showing. Set by the Dock, by opening or
   * activating any tab, and by nothing else. */
  activeModuleId: string;
  /** Tab state per module, keyed by moduleId. A module that has never opened
   * a tab has no entry; read through moduleTabs() for the empty default. */
  modules: Record<string, ModuleTabs>;
  /** The document area's groups, left to right (stores/documentGroups.ts). */
  groups: DocGroup[];
  /** The group you are working in. activeModuleId is its module. */
  focusedGroupId: string;
  /**
   * Tabs closed this session, newest first, at most MAX_RECENTLY_CLOSED. Memory
   * only, like status: it is about what you just did, not what you had open.
   */
  recentlyClosed: ClosedTab[];
  /** Switch the content area to a module. Its own last-active tab stays
   * active (sticky). Does NOT open anything — a module with no tabs is the
   * caller's cue to open its home view. */
  focusModule: (moduleId: string) => void;
  openTab: (input: OpenTabInput) => string;
  /**
   * `remember: false` when the tab closes because its document is gone
   * (deleted, removed from the workspace, replaced by a saved one): reopening
   * it would bring back a tab with nothing to show.
   */
  closeTab: (id: string, options?: CloseTabOptions) => void;
  /**
   * Opens the `at`-th recently closed tab again (default: the last closed) in
   * its old group if that is still there, at its old place, pinned if it was.
   * Returns its id, or null when there is nothing to reopen.
   */
  reopenClosedTab: (at?: number) => string | null;
  setActiveTab: (id: string) => void;
  setTabStatus: (id: string, status: TabStatus) => void;
  setTabDirty: (id: string, dirty: boolean) => void;
  pinTab: (id: string) => void;
  reorderTab: (fromId: string, beforeId: string | null) => void;
  /** Pins or unpins, moving the tab to the edge of the pinned run. */
  togglePinTab: (id: string) => void;
  /** Make a group the one you are working in. */
  focusGroup: (groupId: string) => void;
  /** Move a tab into a new group to the right of its own (⌘\\). */
  splitTab: (id: string) => void;
  /** Move a tab into another group showing the same module. */
  moveTabToGroup: (id: string, groupId: string) => void;
  /** Show a module in a new group beside the focused one. */
  openModuleBeside: (moduleId: string) => void;
  /** Close a group; its tabs fall back to another group of their module. */
  closeGroup: (groupId: string) => void;
  setGroupWeights: (weights: Record<string, number>) => void;
};

/** Pinned tabs first, each group in its own order. */
function pinnedFirst(tabs: WorkbenchTab[]): WorkbenchTab[] {
  return [...tabs.filter((tab) => tab.pinned), ...tabs.filter((tab) => !tab.pinned)];
}

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

/**
 * An icon is a function, and a function does not survive storage.
 *
 * So it is not stored: a restored tab carries its view id, and viewIcon turns
 * that back into the same glyph the call site would have passed — which is
 * why documents.ts stopped passing one of its own.
 */
export function restoreIcons(
  modules: Record<string, ModuleTabs>,
  // Injected so a test can resolve glyphs without registering a module: the
  // registry is global and process-wide, and registering the same module
  // twice is an error the app relies on.
  resolve: (moduleId: string, viewId: string) => LucideIcon | undefined = viewIcon,
): Record<string, ModuleTabs> {
  const out: Record<string, ModuleTabs> = {};
  for (const [moduleId, tabs] of Object.entries(modules ?? {})) {
    out[moduleId] = {
      ...EMPTY_MODULE_TABS,
      ...tabs,
      openTabs: (tabs.openTabs ?? []).map((tab) => ({
        ...tab,
        icon: resolve(tab.moduleId, tab.viewId),
        // Status is about a run that ended with the process it belonged to.
        status: undefined,
      })),
    };
  }
  return out;
}

/** What goes to storage: no functions, and no state that died with the app. */
export function persistedTabs(
  state: Pick<WorkbenchTabsState, "activeModuleId" | "modules"> &
    Partial<Pick<WorkbenchTabsState, "groups" | "focusedGroupId">>,
) {
  return {
    activeModuleId: state.activeModuleId,
    groups: state.groups ?? [],
    focusedGroupId: state.focusedGroupId ?? "",
    modules: Object.fromEntries(
      Object.entries(state.modules).map(([moduleId, tabs]) => [
        moduleId,
        {
          activeTabId: tabs.activeTabId,
          tabMru: tabs.tabMru,
          openTabs: tabs.openTabs.map(({ icon: _icon, status: _status, dirty: _dirty, ...tab }) => tab),
        },
      ]),
    ),
  };
}

export const useWorkbenchTabsStore = create<WorkbenchTabsState>()(
  persist(
    (set, get) => ({
  activeModuleId: "",
  modules: {},

  groups: [],
  focusedGroupId: "",

  recentlyClosed: [],

  // A module already on screen in some group is focused there; otherwise the
  // focused group switches to it.
  focusModule: (moduleId) =>
    set((state) => {
      if (state.activeModuleId === moduleId && focusedGroup(state)?.moduleId === moduleId) return state;
      const showing = state.groups.find((g) => g.moduleId === moduleId);
      if (showing) return reconcile({ ...state, focusedGroupId: showing.id });
      const placed = targetGroup(state, moduleId);
      return reconcile({ ...state, groups: placed.groups, focusedGroupId: placed.group.id, activeModuleId: moduleId });
    }),

  openTab: (input) => {
    const id = tabKey(input.moduleId, input.viewId, input.params);
    const beside = input.beside || besideArmed;
    besideArmed = false;
    set((state) => {
      // An open tab is shown where it already is; a new one goes to the
      // focused group if it shows this module, else to one that does — or,
      // beside, to a group other than the focused one.
      const existing = moduleTabs(state.modules, input.moduleId).openTabs.find((tab) => tab.id === id);
      const home = existing ? groupOfTab(state.groups, existing) : null;
      const placed = home
        ? { groups: state.groups, group: home }
        : beside
          ? besideGroup(state, input.moduleId)
          : targetGroup(state, input.moduleId);
      const partial = updateModule(
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
            group: placed.group.id,
          };

          if (input.preview) {
            // One preview slot per module, not one for the whole app: leaving
            // Notes for SSH via the Dock must not evict the note being
            // previewed, because coming back has to find it exactly as left
            // (module-tabs.mdx: state survives leaving a module). And one per
            // group within it, as in VS Code: browsing in one group must not
            // take away what another group is showing.
            const previewIndex = m.openTabs.findIndex(
              (t) => t.preview && groupOfTab(placed.groups, t)?.id === placed.group.id,
            );
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
      );
      return reconcile({
        ...state,
        ...partial,
        groups: withGroup(placed.groups, placed.group.id, { activeTabId: id }),
        focusedGroupId: placed.group.id,
        // Opened for real, a closed tab is no longer one to reopen. A preview
        // is only a look, and may be gone with the next click: keep the record
        // (the Recently Closed list leaves out ids that are open).
        recentlyClosed: input.preview ? state.recentlyClosed : forgetClosed(state.recentlyClosed, id),
      });
    });
    return id;
  },

  closeTab: (id, options) =>
    set((state) => {
      const tab = moduleTabs(state.modules, moduleOfTab(id)).openTabs.find((t) => t.id === id);
      if (!tab) return state;
      // The group it leaves shows its own left neighbour, not whatever the
      // partition would pick from another group.
      const group = groupOfTab(state.groups, tab);
      // A preview tab was only being looked through: nothing to bring back.
      const recentlyClosed = tab.preview || options?.remember === false
        ? state.recentlyClosed
        : [
            {
              moduleId: tab.moduleId,
              viewId: tab.viewId,
              title: tab.title,
              params: tab.params,
              icon: tab.icon,
              pinned: !!tab.pinned,
              groupId: group?.id ?? "",
              index: Math.max(0, group ? tabsOfGroup(state, group).findIndex((t) => t.id === id) : 0),
              closedAt: Date.now(),
            },
            ...forgetClosed(state.recentlyClosed, id),
          ].slice(0, MAX_RECENTLY_CLOSED);
      const groups =
        group && group.activeTabId === id
          ? withGroup(state.groups, group.id, { activeTabId: neighbourIn(tabsOfGroup(state, group), id) })
          : state.groups;
      const partial = updateModule(state, moduleOfTab(id), (m) => {
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
      });
      const next = { ...state, ...partial, groups, recentlyClosed };
      return reconcile(group ? dropIfEmpty(next, group.id) : next);
    }),

  // Composed from the actions a person would use to put the tab back — focus
  // its group, open it, pin it, drag it into place — so it obeys their rules
  // (where an open lands, the pinned run's edge) rather than restating them.
  reopenClosedTab: (at = 0) => {
    const entry = get().recentlyClosed[at];
    if (!entry) return null;
    const recordedId = closedTabId(entry);
    // Open again as a preview, the one route that keeps the record: show it
    // where it is rather than moving it back.
    if (moduleTabs(get().modules, entry.moduleId).openTabs.some((t) => t.id === recordedId)) {
      set((state) => ({ recentlyClosed: forgetClosed(state.recentlyClosed, recordedId) }));
      get().setActiveTab(recordedId);
      return recordedId;
    }
    // Its old group, if it still exists and still shows the module; else
    // openTab's own placement, which starts at the focused group.
    const home = get().groups.find((g) => g.id === entry.groupId && g.moduleId === entry.moduleId);
    if (home) get().focusGroup(home.id);
    // openTab forgets the record (forgetClosed) as it opens the tab.
    const id = get().openTab({
      moduleId: entry.moduleId,
      viewId: entry.viewId,
      title: entry.title,
      params: entry.params,
      icon: entry.icon,
      preview: false,
    });
    const opened = moduleTabs(get().modules, entry.moduleId).openTabs.find((t) => t.id === id);
    if (entry.pinned && opened && !opened.pinned) get().togglePinTab(id);
    // Back to its old place in the strip — only in its own group: an index
    // in a strip that is gone means nothing in another one, where it stays at
    // the end like any open. reorderTab keeps it on its side of the pinned run,
    // and past the end means the end.
    const after = get();
    const placed = moduleTabs(after.modules, entry.moduleId).openTabs.find((t) => t.id === id);
    const group = placed ? groupOfTab(after.groups, placed) : null;
    if (home && group?.id === home.id) {
      const strip = tabsOfGroup(after, group).filter((t) => t.id !== id);
      after.reorderTab(id, strip[entry.index]?.id ?? null);
    }
    return id;
  },

  setActiveTab: (id) => {
    const moduleId = moduleOfTab(id);
    set((state) => {
      const tab = moduleTabs(state.modules, moduleId).openTabs.find((t) => t.id === id);
      const home = tab ? groupOfTab(state.groups, tab) : null;
      const placed = home ? { groups: state.groups, group: home } : targetGroup(state, moduleId);
      const partial = updateModule(
        state,
        moduleId,
        (m) => {
          if (m.activeTabId === id && m.tabMru[0] === id) {
            return m;
          }
          return { ...m, activeTabId: id, tabMru: pushMru(m.tabMru, id) };
        },
        { activeModuleId: moduleId },
      );
      const settled =
        state.groups.length === 0 ||
        (placed.group.activeTabId === id && state.focusedGroupId === placed.group.id);
      if (partial === state && settled) {
        return state;
      }
      return reconcile({
        ...state,
        ...partial,
        groups: withGroup(placed.groups, placed.group.id, { activeTabId: id }),
        focusedGroupId: placed.group.id,
      });
    });
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

  setTabDirty: (id, dirty) =>
    set((state) =>
      updateModule(state, moduleOfTab(id), (m) => {
        if (!m.openTabs.some((tab) => tab.id === id && !!tab.dirty !== dirty)) {
          return m;
        }
        return {
          ...m,
          openTabs: m.openTabs.map((tab) => (tab.id === id ? { ...tab, dirty } : tab)),
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
        const moving = m.openTabs.slice();
        const [moved] = moving.splice(fromIndex, 1);
        const insertAt = beforeId ? moving.findIndex((tab) => tab.id === beforeId) : -1;
        if (insertAt === -1) {
          moving.push(moved);
        } else {
          moving.splice(insertAt, 0, moved);
        }
        // A drag cannot carry a tab across the pinned run's edge: pinning is
        // what moves a tab there, not where it was dropped.
        const openTabs = pinnedFirst(moving);
        if (openTabs.every((tab, index) => tab === m.openTabs[index])) {
          return m;
        }
        return { ...m, openTabs };
      });
    }),

  togglePinTab: (id) =>
    set((state) =>
      updateModule(state, moduleOfTab(id), (m) => {
        const tab = m.openTabs.find((candidate) => candidate.id === id);
        if (!tab) {
          return m;
        }
        // A pinned tab is kept, so it stops being the preview slot too.
        const toggled: WorkbenchTab = { ...tab, pinned: !tab.pinned, preview: false };
        const rest = m.openTabs.filter((candidate) => candidate.id !== id);
        // The edge of the pinned run either way: the last pinned when pinning,
        // the first unpinned when unpinning — the nearest place it belongs.
        const edge = rest.filter((candidate) => candidate.pinned).length;
        rest.splice(edge, 0, toggled);
        return { ...m, openTabs: rest };
      }),
    ),

  focusGroup: (groupId) =>
    set((state) =>
      state.focusedGroupId === groupId || !state.groups.some((g) => g.id === groupId)
        ? state
        : reconcile({ ...state, focusedGroupId: groupId }),
    ),

  splitTab: (id) =>
    set((state) => {
      const moduleId = moduleOfTab(id);
      const tab = moduleTabs(state.modules, moduleId).openTabs.find((t) => t.id === id);
      const group = tab ? groupOfTab(state.groups, tab) : null;
      // One document lives in one group, so a group's only tab has nowhere
      // to leave from.
      if (!tab || !group || state.groups.length >= MAX_GROUPS) return state;
      const own = tabsOfGroup(state, group);
      if (own.length < 2) return state;
      const created: DocGroup = { id: newGroupId(), moduleId, activeTabId: id, weight: group.weight / 2 };
      const groups = withGroup(state.groups, group.id, {
        activeTabId: group.activeTabId === id ? neighbourIn(own, id) : group.activeTabId,
        weight: group.weight / 2,
      });
      groups.splice(groups.findIndex((g) => g.id === group.id) + 1, 0, created);
      return reconcile({
        ...state,
        ...updateModule(state, moduleId, (m) => ({
          ...m,
          openTabs: m.openTabs.map((t) => (t.id === id ? { ...t, group: created.id } : t)),
        })),
        groups,
        focusedGroupId: created.id,
      });
    }),

  moveTabToGroup: (id, groupId) =>
    set((state) => {
      const moduleId = moduleOfTab(id);
      const tab = moduleTabs(state.modules, moduleId).openTabs.find((t) => t.id === id);
      const from = tab ? groupOfTab(state.groups, tab) : null;
      const to = state.groups.find((g) => g.id === groupId);
      if (!tab || !from || !to || from.id === to.id || to.moduleId !== moduleId) return state;
      let groups = withGroup(state.groups, to.id, { activeTabId: id });
      if (from.activeTabId === id) {
        groups = withGroup(groups, from.id, { activeTabId: neighbourIn(tabsOfGroup(state, from), id) });
      }
      const next = {
        ...state,
        ...updateModule(state, moduleId, (m) => ({
          ...m,
          openTabs: m.openTabs.map((t) => (t.id === id ? { ...t, group: to.id } : t)),
        })),
        groups,
        focusedGroupId: to.id,
      };
      return reconcile(dropIfEmpty(next, from.id));
    }),

  openModuleBeside: (moduleId) =>
    set((state) => {
      const showing = state.groups.find((g) => g.moduleId === moduleId);
      if (showing) return reconcile({ ...state, focusedGroupId: showing.id });
      const focus = focusedGroup(state);
      if (!focus || state.groups.length >= MAX_GROUPS) {
        const placed = targetGroup(state, moduleId);
        return reconcile({ ...state, groups: placed.groups, focusedGroupId: placed.group.id });
      }
      const created: DocGroup = {
        id: newGroupId(),
        moduleId,
        activeTabId: moduleTabs(state.modules, moduleId).activeTabId,
        weight: focus.weight / 2,
      };
      const groups = withGroup(state.groups, focus.id, { weight: focus.weight / 2 });
      groups.splice(groups.findIndex((g) => g.id === focus.id) + 1, 0, created);
      return reconcile({ ...state, groups, focusedGroupId: created.id });
    }),

  closeGroup: (groupId) =>
    set((state) =>
      state.groups.length <= 1 || !state.groups.some((g) => g.id === groupId)
        ? state
        : reconcile({ ...state, groups: state.groups.filter((g) => g.id !== groupId) }),
    ),

  setGroupWeights: (weights) =>
    set((state) => ({
      groups: state.groups.map((g) => (weights[g.id] === undefined ? g : { ...g, weight: weights[g.id] })),
    })),
    }),
    {
      name: "workbench.tabs",
      storage: uiStateStorage,
      version: 1,
      /**
       * What was open, and where you were. A workbench that forgets its tabs
       * asks you to find your way back to eight documents every launch, and
       * the module that "opens with a blank console" was only ever hiding
       * that: there was nothing to restore, so something had to be invented.
       *
       * Icons are dropped rather than serialised — see restoreIcons.
       */
      // A lambda, not the bare reference: passing `persistedTabs` directly makes
      // zustand infer the STORE's state from its narrower parameter type, and
      // every action in the file then fails to typecheck against it.
      partialize: (state) => persistedTabs(state),
      merge: (persisted, current) => {
        const saved = (persisted ?? {}) as Partial<WorkbenchTabsState>;
        const activeModuleId = saved.activeModuleId ?? current.activeModuleId;
        return reconcile({
          ...current,
          activeModuleId,
          modules: restoreIcons(saved.modules ?? {}),
          // Stores from before document groups have none: one group showing
          // the module that was on screen.
          groups: saved.groups ?? [],
          focusedGroupId: saved.focusedGroupId ?? "",
        });
      },
    },
  ),
);

/** Every open tab across every module, in module-insertion order. For the
 * views layer, which keeps recently-left modules mounted; the strip and the
 * switcher must NOT use this — they show one module. */
export function allOpenTabs(modules: Record<string, ModuleTabs>): WorkbenchTab[] {
  return Object.values(modules).flatMap((m) => m.openTabs);
}
