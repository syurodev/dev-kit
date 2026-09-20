import type { ModuleGroup } from "@/modules/groups";

export type SwitcherModule = { id: string; title: string; unavailable?: boolean };

/**
 * One button in the title bar: a module, or a group of them.
 *
 * The slots are what ⌘1–⌘9 count, so a group is one slot however many members it
 * has — which is the point of grouping: nine buttons, nine digits.
 */
export type SwitcherItem =
  | { kind: "module"; module: SwitcherModule }
  | { kind: "group"; group: ModuleGroup; members: SwitcherModule[] };

/** The key a slot sorts by: a module's id, a group's own id. */
function sortKey(item: SwitcherItem): string {
  return item.kind === "module" ? item.module.id : item.group.id;
}

/**
 * The switcher's slots. Modules keep the registry's order — by id — and a group
 * stands where its own id falls, so Infra sits between Git and Notes without any
 * order being kept for it. A group of one member is that member: there is
 * nothing to choose between.
 */
export function buildSwitcherItems(
  modules: SwitcherModule[],
  groupOf: (moduleId: string) => ModuleGroup | undefined,
): SwitcherItem[] {
  const items: SwitcherItem[] = [];
  const groups = new Map<string, { group: ModuleGroup; members: SwitcherModule[] }>();
  for (const module of modules) {
    const group = groupOf(module.id);
    if (!group) {
      items.push({ kind: "module", module });
      continue;
    }
    const entry = groups.get(group.id) ?? { group, members: [] };
    entry.members.push(module);
    groups.set(group.id, entry);
  }
  for (const { group, members } of groups.values()) {
    items.push(members.length === 1 ? { kind: "module", module: members[0] } : { kind: "group", group, members });
  }
  return items.sort((left, right) => sortKey(left).localeCompare(sortKey(right)));
}

/** The members of a group that can be opened. */
function usable(members: SwitcherModule[]): SwitcherModule[] {
  return members.filter((member) => !member.unavailable);
}

/**
 * The member a group's button stands for: the one on screen, else the one used
 * last, else the group's primary, else the first that can be opened. Null when
 * none can.
 */
export function representative(
  members: SwitcherModule[],
  lastUsed: string | undefined,
  activeModuleId: string,
  primary?: string,
): SwitcherModule | null {
  const open = usable(members);
  return (
    open.find((member) => member.id === activeModuleId) ??
    open.find((member) => member.id === lastUsed) ??
    open.find((member) => member.id === primary) ??
    open[0] ??
    null
  );
}

/**
 * The module a slot's ⌘digit opens. A module opens itself; a group opens the
 * member used last — and pressed again while one of its members is on screen,
 * moves to the next, so the keyboard reaches every member with one chord.
 */
export function slotTarget(
  item: SwitcherItem | undefined,
  lastUsed: string | undefined,
  activeModuleId: string,
): SwitcherModule | null {
  if (!item) return null;
  if (item.kind === "module") return item.module.unavailable ? null : item.module;
  const open = usable(item.members);
  const here = open.findIndex((member) => member.id === activeModuleId);
  if (here >= 0) return open[(here + 1) % open.length];
  return representative(item.members, lastUsed, activeModuleId, item.group.primary);
}

/**
 * What the command palette shows as each module's digit: the slot's index for a
 * module of its own, and nothing for a member of a group — ⌘6 opens the member
 * used last, which is not necessarily this one, so the palette would be lying.
 */
export function slotModuleIds(items: SwitcherItem[]): string[] {
  return items.map((item) => (item.kind === "module" ? item.module.id : ""));
}
