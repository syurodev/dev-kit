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
  /**
   * On the title bar, alone or inside a group (not Settings or Sync, which are
   * reached from the header). A group's members have no digit of their own,
   * and they were left out of "Go to" for it — Database, Notes and the rest
   * could not be reached from the palette at all.
   */
  switchable: boolean;
  /** The title-bar group it sits in ("Infra"), shown beside it and matched. */
  group?: string;
  unavailable: boolean;
  commands: ReadonlyArray<{ id: string; title: string; shortcut?: string; run?: () => void }>;
};

export type PaletteTab = { id: string; moduleId: string; title: string; icon?: LucideIcon };

/** A closed tab that can be reopened; `at` is its place in the store's stack. */
export type PaletteClosedTab = { at: number; moduleId: string; title: string; icon?: LucideIcon };

/**
 * What a query is asking about. The first character narrows it, VS Code's way:
 *
 *   >text   commands only — every module's, with no cap, since that is now
 *           the whole list
 *   @text   only what the module in front can find inside itself
 *
 * Anything else searches everything. A prefix is a scope, not part of the
 * text: ">re-index" looks for "re-index".
 */
export type PaletteScope = "all" | "commands" | "module";

export function parseQuery(raw: string): { scope: PaletteScope; text: string } {
  const trimmed = raw.trimStart();
  if (trimmed.startsWith(">")) return { scope: "commands", text: trimmed.slice(1).trim() };
  if (trimmed.startsWith("@")) return { scope: "module", text: trimmed.slice(1).trim() };
  return { scope: "all", text: raw.trim() };
}

export type PaletteInput = {
  /** The text to match, already without its scope prefix. */
  query: string;
  /** Defaults to "all". */
  scope?: PaletteScope;
  activeModuleId: string | null;
  modules: readonly PaletteModule[];
  /** Every open tab but the one in front and the modules' hidden home previews. */
  tabs: readonly PaletteTab[];
  /** Closed tabs, newest first, without any whose tab is open again. */
  recentlyClosed?: readonly PaletteClosedTab[];
  /** ⌘⇧T's label for this platform (lib/shortcut), beside Reopen Closed Tab. */
  reopenShortcut?: string;
};

export type PaletteActions = {
  focusModule: (moduleId: string) => void;
  activateTab: (tabId: string) => void;
  /** Reopen the closed tab at this place in the store's stack. */
  reopenClosed?: (at: number) => void;
};

const MAX_TABS = 6;
const MAX_CLOSED = 6;
const REOPEN_TITLE = "Reopen Closed Tab";
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
  const { query, activeModuleId, modules, tabs, scope = "all" } = input;
  // "@" is the module's own search and nothing else; there is nothing fixed to add.
  if (scope === "module") return [];
  const commandsOnly = scope === "commands";
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

  const foreignCommands = (limit: number) => {
    for (const module of usable) {
      if (module.id === activeModuleId) continue;
      const rows = commandRows(module, limit);
      if (rows.length > 0) sections.push({ id: `commands:${module.id}`, heading: module.title, rows });
    }
  };

  const titleOf = (moduleId: string) => modules.find((module) => module.id === moduleId)?.title ?? moduleId;

  // ⌘⇧T's command, then the tabs it would bring back, each by its own place
  // in the stack. A command of the shell's, so > lists it, without the tabs.
  const closed = actions.reopenClosed ? (input.recentlyClosed ?? []) : [];
  const recentlyClosed = (withTabs: boolean) => {
    const reopen = actions.reopenClosed;
    if (!reopen || closed.length === 0) return;
    const rows: PaletteRow[] = [];
    if (!searching || rank(query, [REOPEN_TITLE], (title) => ({ title }), 1).length > 0) {
      rows.push({ id: "command:reopen-closed-tab", title: REOPEN_TITLE, shortcut: input.reopenShortcut, run: () => reopen(closed[0].at) });
    }
    if (withTabs) {
      const entries = rank(query, closed, (entry) => ({ title: entry.title, others: [titleOf(entry.moduleId)] }), MAX_CLOSED);
      for (const entry of entries) {
        rows.push({
          id: `reopen:${entry.at}`,
          title: `Reopen: ${entry.title}`,
          subtitle: titleOf(entry.moduleId),
          icon: entry.icon,
          run: () => reopen(entry.at),
        });
      }
    }
    if (rows.length > 0) sections.push({ id: "recently-closed", heading: "Recently closed", rows });
  };

  if (commandsOnly) {
    // Asked for by name, so all of them, typed or not — this is the one place
    // the whole app's commands can be browsed.
    foreignCommands(Number.POSITIVE_INFINITY);
    recentlyClosed(false);
    return sections;
  }

  const destinations = rank(
    query,
    usable.filter((module) => module.switchable && module.id !== activeModuleId),
    (module) => ({ title: module.title, others: module.group ? [module.group] : [] }),
    Number.POSITIVE_INFINITY,
  ).map<PaletteRow>((module) => ({
    id: `module:${module.id}`,
    title: module.title,
    subtitle: module.group,
    icon: module.icon,
    shortcut: module.shortcut,
    run: () => actions.focusModule(module.id),
  }));
  if (destinations.length > 0) sections.push({ id: "modules", heading: "Go to", rows: destinations });

  if (!searching) {
    recentlyClosed(true);
    return sections;
  }

  // Tabs only when asked for: ⌘⇧E already lists the ones in front by recency,
  // and a list of every open tab would push the modules off the first screen.
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
  recentlyClosed(true);

  foreignCommands(MAX_FOREIGN_COMMANDS);
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

/**
 * What to say when nothing is listed — which depends on what was asked for.
 * "@" in a module with no search of its own is not "no results": there was
 * never anything to search, and the way out is worth naming.
 */
export function paletteEmptyMessage(
  scope: PaletteScope,
  moduleTitle: string | undefined,
  noun: string | undefined,
): { title: string; body: string } {
  if (scope === "commands") return { title: "No command matches", body: "Every module's commands are searched here." };
  if (scope === "module") {
    return noun
      ? { title: `No ${noun} found`, body: `Searching only ${moduleTitle ?? "this module"}. Remove the @ to search everything.` }
      : {
          title: `${moduleTitle ?? "This module"} has nothing to search`,
          body: "Remove the @ to search commands, modules and open tabs.",
        };
  }
  return {
    title: "Nothing found",
    body: noun ? `No ${noun}, command, module or open tab matches.` : "No command, module or open tab matches.",
  };
}
