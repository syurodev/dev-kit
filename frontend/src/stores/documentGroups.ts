import type { ModuleTabs, WorkbenchTab } from "./workbenchTabs";

/**
 * Document groups (docs/dev-kit/layout.mdx § Document group): the document
 * area split into up to three side-by-side groups, each bound to one module
 * and showing one of that module's tabs.
 *
 * Tabs stay where they always were — in their module's partition, which still
 * owns their order, the MRU and the preview slot. A group only says which
 * module it shows and which of that module's tabs is on screen in it; a tab
 * says which group it belongs to through `WorkbenchTab.group`. A tab whose
 * group is gone, or now shows another module, falls back to the first group
 * showing its module — or to none, and waits in its partition until one does.
 *
 * The partition's `activeTabId` keeps meaning what modules already read it as:
 * the tab you are working in. It is the FOCUSED group's tab, kept in step by
 * `reconcile`, so a module never has to know that groups exist.
 */
export type DocGroup = {
  id: string;
  moduleId: string;
  /** The tab on screen in this group. May be stale; read through shownTab. */
  activeTabId: string;
  /** flex-grow share of the document area. */
  weight: number;
};

export const MAX_GROUPS = 3;
/** Narrower than this and no document in the group is readable. */
export { GROUP_MIN_WIDTH } from "@/workbench/panelLayout";

export type GroupsSnapshot = {
  activeModuleId: string;
  modules: Record<string, ModuleTabs>;
  groups: DocGroup[];
  focusedGroupId: string;
};

let seq = 0;
export const newGroupId = () => `g${Date.now().toString(36)}${(seq++).toString(36)}`;

export function groupOfTab(groups: DocGroup[], tab: Pick<WorkbenchTab, "moduleId" | "group">): DocGroup | null {
  const own = tab.group ? groups.find((g) => g.id === tab.group && g.moduleId === tab.moduleId) : undefined;
  return own ?? groups.find((g) => g.moduleId === tab.moduleId) ?? null;
}

export function tabsOfGroup(s: Pick<GroupsSnapshot, "modules" | "groups">, group: DocGroup): WorkbenchTab[] {
  const partition = s.modules[group.moduleId];
  if (!partition) return [];
  return partition.openTabs.filter((tab) => groupOfTab(s.groups, tab)?.id === group.id);
}

/** The tab a group shows: its own choice if still valid, else its most recent tab. */
export function shownTab(s: Pick<GroupsSnapshot, "modules" | "groups">, group: DocGroup): string {
  const tabs = tabsOfGroup(s, group);
  if (tabs.some((t) => t.id === group.activeTabId)) return group.activeTabId;
  const mru = s.modules[group.moduleId]?.tabMru ?? [];
  const ids = new Set(tabs.map((t) => t.id));
  return mru.find((id) => ids.has(id)) ?? tabs[0]?.id ?? "";
}

export const focusedGroup = (s: Pick<GroupsSnapshot, "groups" | "focusedGroupId">): DocGroup | undefined =>
  s.groups.find((g) => g.id === s.focusedGroupId) ?? s.groups[0];

/**
 * Brings the snapshot back to its invariants after any change:
 *   - there is at least one group, and the focused one exists;
 *   - every group's activeTabId is one of its tabs;
 *   - activeModuleId is the focused group's module, and that module's
 *     partition.activeTabId is the focused group's tab.
 *
 * It does not close empty groups: a group emptied by closing or moving its
 * last tab closes (dropIfEmpty), but one opened empty on purpose — a module
 * with nothing open yet, put beside another — stays until you use or close it.
 *
 * Returns `s` itself when nothing needed fixing, so a no-op action still
 * publishes nothing (workbenchTabReorder.test.ts).
 */
export function reconcile<S extends GroupsSnapshot>(s: S): S {
  let groups = s.groups;
  if (groups.length === 0) {
    groups = [{ id: newGroupId(), moduleId: s.activeModuleId, activeTabId: "", weight: 1 }];
  }

  const probe = { ...s, groups };
  groups = groups.map((g) => {
    const shown = shownTab(probe, g);
    return shown === g.activeTabId ? g : { ...g, activeTabId: shown };
  });
  if (groups.every((g, i) => g === s.groups[i]) && groups.length === s.groups.length) groups = s.groups;

  let focusedGroupId = s.focusedGroupId;
  if (!groups.some((g) => g.id === focusedGroupId)) {
    const was = s.groups.findIndex((g) => g.id === focusedGroupId);
    focusedGroupId = (groups[Math.max(0, Math.min(was - 1, groups.length - 1))] ?? groups[0]).id;
  }
  const focus = groups.find((g) => g.id === focusedGroupId)!;

  let modules = s.modules;
  const partition = modules[focus.moduleId];
  if (partition && focus.activeTabId && partition.activeTabId !== focus.activeTabId) {
    modules = { ...modules, [focus.moduleId]: { ...partition, activeTabId: focus.activeTabId } };
  }

  const activeModuleId = focus.moduleId || s.activeModuleId;
  if (
    groups === s.groups &&
    focusedGroupId === s.focusedGroupId &&
    modules === s.modules &&
    activeModuleId === s.activeModuleId
  ) {
    return s;
  }
  return { ...s, groups, focusedGroupId, modules, activeModuleId };
}

/**
 * Where a tab of `moduleId` opens: the focused group if it shows that module,
 * else the first group that does, else the focused group — which then switches
 * to the module. The returned groups carry that switch.
 */
export function targetGroup(s: GroupsSnapshot, moduleId: string): { groups: DocGroup[]; group: DocGroup } {
  const focus = focusedGroup(s);
  if (focus && focus.moduleId === moduleId) return { groups: s.groups, group: focus };
  const showing = s.groups.find((g) => g.moduleId === moduleId);
  if (showing) return { groups: s.groups, group: showing };
  if (!focus) {
    const group = { id: newGroupId(), moduleId, activeTabId: "", weight: 1 };
    return { groups: [group], group };
  }
  const switched = { ...focus, moduleId, activeTabId: s.modules[moduleId]?.activeTabId ?? "" };
  return { groups: s.groups.map((g) => (g === focus ? switched : g)), group: switched };
}

/** Closes `id` if it has no tabs left — never the last group. */
export function dropIfEmpty<S extends GroupsSnapshot>(s: S, id: string): S {
  const group = s.groups.find((g) => g.id === id);
  if (!group || s.groups.length <= 1 || tabsOfGroup(s, group).length > 0) return s;
  return { ...s, groups: s.groups.filter((g) => g.id !== id) };
}

/** Replaces one group, by id. */
export const withGroup = (groups: DocGroup[], id: string, patch: Partial<DocGroup>): DocGroup[] =>
  groups.map((g) => (g.id === id ? { ...g, ...patch } : g));

/** The tab left of `id` among a group's tabs, else the new first — VS Code's close order. */
export function neighbourIn(tabs: WorkbenchTab[], id: string): string {
  const i = tabs.findIndex((t) => t.id === id);
  const rest = tabs.filter((t) => t.id !== id);
  return (rest[i - 1] ?? rest[0])?.id ?? "";
}
