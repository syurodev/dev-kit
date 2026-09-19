import type { LucideIcon } from "lucide-react";
import type { PaletteSearchGroup } from "@/modules/registry";
import { rank } from "./match";

/** One selectable row, whatever it came from. */
export type PaletteRow = {
  /** Unique across the whole palette: cmdk keys its selection on it. */
  id: string;
  title: string;
  subtitle?: string;
  icon?: LucideIcon;
  shortcut?: string;
  run: () => void;
};

export type PaletteSection = { id: string; heading: string; rows: PaletteRow[] };

export type PaletteModule = {
  id: string;
  title: string;
  icon: LucideIcon;
  /** "⌘3" — only modules the switcher can reach by digit have one. */
  shortcut?: string;
  /** Listed in the rail (not Settings or Sync, which are reached from the header). */
  switchable: boolean;
  unavailable: boolean;
  commands: ReadonlyArray<{ id: string; title: string; shortcut?: string; run?: () => void }>;
};

export type PaletteTab = { id: string; moduleId: string; title: string; icon?: LucideIcon };

export type PaletteInput = {
  query: string;
  activeModuleId: string | null;
  modules: readonly PaletteModule[];
  /** Every open tab but the one in front and the modules' hidden home previews. */
  tabs: readonly PaletteTab[];
};

export type PaletteActions = {
  focusModule: (moduleId: string) => void;
  activateTab: (tabId: string) => void;
};

const MAX_TABS = 6;
const MAX_FOREIGN_COMMANDS = 5;

/**
 * Everything the palette can offer without asking a module to search: the
 * commands, the modules, the open tabs.
 *
 * With nothing typed it is a short menu of what is relevant HERE — the module
 * in front's own commands, then the other modules to go to. Every module's
 * commands used to be listed flat, so "Cancel turn" sat in the list while
 * looking at a database; another module's commands now appear only when the
 * query asks for them, under that module's name.
 *
 * A command without a `run` is not listed at all. The registry allows one —
 * Go's view of a module is a list of ids, and parity compares those — but in
 * the palette it is a row that does nothing when picked, and several modules
 * had only those.
 */
export function staticSections(input: PaletteInput, actions: PaletteActions): PaletteSection[] {
  const { query, activeModuleId, modules, tabs } = input;
  const searching = query.trim() !== "";
  const usable = modules.filter((module) => !module.unavailable);
  const active = usable.find((module) => module.id === activeModuleId);
  const sections: PaletteSection[] = [];

  const commandRows = (module: PaletteModule, limit: number): PaletteRow[] =>
    rank(
      query,
      module.commands.filter((command) => command.run),
      (command) => ({ title: command.title, others: [module.title] }),
      limit,
    ).map((command) => ({
      id: `command:${command.id}`,
      title: command.title,
      shortcut: command.shortcut,
      run: command.run!,
    }));

  if (active) {
    const rows = commandRows(active, Number.POSITIVE_INFINITY);
    if (rows.length > 0) sections.push({ id: `commands:${active.id}`, heading: active.title, rows });
  }

  const destinations = rank(
    query,
    usable.filter((module) => module.switchable && module.id !== activeModuleId),
    (module) => ({ title: module.title }),
    Number.POSITIVE_INFINITY,
  ).map<PaletteRow>((module) => ({
    id: `module:${module.id}`,
    title: module.title,
    icon: module.icon,
    shortcut: module.shortcut,
    run: () => actions.focusModule(module.id),
  }));
  if (destinations.length > 0) sections.push({ id: "modules", heading: "Go to", rows: destinations });

  if (!searching) return sections;

  // Tabs only when asked for: ⌘⇧E already lists the ones in front by recency,
  // and a list of every open tab would push the modules off the first screen.
  const titleOf = (moduleId: string) => modules.find((module) => module.id === moduleId)?.title ?? moduleId;
  const tabRows = rank(query, tabs, (tab) => ({ title: tab.title, others: [titleOf(tab.moduleId)] }), MAX_TABS).map<PaletteRow>(
    (tab) => ({
      id: `tab:${tab.id}`,
      title: tab.title,
      subtitle: titleOf(tab.moduleId),
      icon: tab.icon,
      run: () => actions.activateTab(tab.id),
    }),
  );
  if (tabRows.length > 0) sections.push({ id: "tabs", heading: "Open tabs", rows: tabRows });

  for (const module of usable) {
    if (module.id === activeModuleId) continue;
    const rows = commandRows(module, MAX_FOREIGN_COMMANDS);
    if (rows.length > 0) sections.push({ id: `commands:${module.id}`, heading: module.title, rows });
  }
  return sections;
}

/**
 * Where a module's own search results go among the rest.
 *
 * Typed for, they are the answer, so they lead. Offered unprompted (recent
 * notes, the tools) they are a suggestion, and sit under the module's
 * commands rather than pushing them down.
 */
export function composeSections(
  query: string,
  moduleId: string | null,
  fixed: readonly PaletteSection[],
  found: readonly PaletteSearchGroup[],
): PaletteSection[] {
  const results: PaletteSection[] = found
    .filter((group) => group.items.length > 0)
    .map((group) => ({
      id: `search:${group.heading}`,
      heading: group.heading,
      rows: group.items.map((item) => ({ ...item, id: `search:${moduleId}:${group.heading}:${item.id}` })),
    }));
  if (query.trim() !== "") return [...results, ...fixed];
  const own = fixed.filter((section) => section.id === `commands:${moduleId}`);
  const rest = fixed.filter((section) => section.id !== `commands:${moduleId}`);
  return [...own, ...results, ...rest];
}

/** The input's placeholder: what can be found from here. */
export function palettePlaceholder(noun: string | undefined): string {
  return noun ? `Search ${noun}, commands, modules…` : "Search commands, modules…";
}
