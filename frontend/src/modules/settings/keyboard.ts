import { chordLabel, parseGlyphShortcut, type Chord } from "@/lib/shortcut";
import type { ModuleManifest } from "@/modules/registry";

/**
 * What one key cap says. A chord is drawn per platform; a range is two chords
 * with a dash between ("⌘1 – ⌘9"); a click is the modifier held while clicking,
 * which is not a key at all.
 */
export type KeyCap =
  | { kind: "chord"; chord: Chord }
  | { kind: "range"; from: Chord; to: Chord }
  | { kind: "click"; alt: true };

export type ShortcutEntry = {
  id: string;
  action: string;
  /** Where it applies, when that is narrower than the section: "in the editor". */
  context?: string;
  keys: KeyCap[];
};

export type ShortcutSection = {
  id: string;
  title: string;
  /** The module the person is in: listed first and marked. */
  here: boolean;
  entries: ShortcutEntry[];
};

const chord = (key: string, mods: Omit<Chord, "key"> = { mod: true }): KeyCap => ({
  kind: "chord",
  chord: { ...mods, key },
});

/**
 * Shortcuts that belong to no module: the shell's own. Each is a handler that
 * exists — Workbench.tsx for the keys, the module switcher for the click — so
 * this list is what the app does, not what it might. A new one is added here
 * with the handler.
 */
const EVERYWHERE: ShortcutEntry[] = [
  { id: "palette", action: "Command palette", keys: [chord("K")] },
  { id: "keyboard", action: "Keyboard shortcuts", keys: [chord("/")] },
  { id: "settings", action: "Settings", keys: [chord(",")] },
  {
    id: "module",
    action: "Go to module 1–9",
    keys: [{ kind: "range", from: { mod: true, key: "1" }, to: { mod: true, key: "9" } }],
  },
  {
    id: "peek",
    action: "Peek a module",
    context: "opens its tabs or members",
    keys: [
      {
        kind: "range",
        from: { mod: true, alt: true, key: "1" },
        to: { mod: true, alt: true, key: "9" },
      },
    ],
  },
  {
    id: "tab-switch",
    action: "Switch tab",
    context: "hold to see all",
    keys: [chord("E", { mod: true, shift: true })],
  },
  { id: "tab-close", action: "Close tab", keys: [chord("W")] },
  { id: "tab-split", action: "Split tab right", keys: [chord("\\")] },
  {
    id: "module-beside",
    action: "Open module beside",
    context: "on the module switcher",
    keys: [{ kind: "click", alt: true }],
  },
];

/**
 * Shortcuts a module has that are not commands: the ones its editor binds
 * itself. Commands that carry a `shortcut` in the manifest are read from there,
 * so they cannot drift from what the menu bar and palette say.
 */
const MODULE_SHORTCUTS: Record<string, ShortcutEntry[]> = {
  notes: [
    {
      id: "notes.marks",
      action: "Bold, italic, underline",
      context: "in the editor",
      keys: [chord("B"), chord("I"), chord("U")],
    },
    { id: "notes.code", action: "Inline code", context: "in the editor", keys: [chord("E")] },
    {
      id: "notes.strike",
      action: "Strikethrough",
      context: "in the editor",
      keys: [chord("X", { mod: true, shift: true })],
    },
    {
      id: "notes.highlight",
      action: "Highlight",
      context: "in the editor",
      keys: [chord("H", { mod: true, shift: true })],
    },
    {
      id: "notes.heading",
      action: "Heading 1–6",
      context: "in the editor",
      keys: [{ kind: "range", from: { mod: true, alt: true, key: "1" }, to: { mod: true, alt: true, key: "6" } }],
    },
    { id: "notes.quote", action: "Quote", context: "in the editor", keys: [chord(".", { mod: true, shift: true })] },
    { id: "notes.codeblock", action: "Code block", context: "in the editor", keys: [chord("8", { mod: true, alt: true })] },
  ],
  database: [
    {
      id: "database.run",
      action: "Run query, or the selection",
      context: "in a console",
      keys: [chord("Enter")],
    },
    {
      id: "database.run-statement",
      action: "Run the statement at the caret",
      context: "in a console",
      keys: [chord("Enter", { mod: true, shift: true })],
    },
    {
      id: "database.cancel",
      action: "Cancel the running query",
      context: "while running",
      keys: [{ kind: "chord", chord: { key: "Escape" } }, chord(".")],
    },
  ],
};

/** Commands the entries above already say, with the context the manifest cannot. */
const COVERED_COMMANDS = new Set(["database.query", "database.query.cancel"]);

/**
 * Every shortcut, in the sections the Keyboard settings lists: the module the
 * person is in first, then the shell's own, then every other module that has
 * any. A module with none has no section.
 */
export function buildSections(modules: ModuleManifest[], activeModuleId: string): ShortcutSection[] {
  const perModule: ShortcutSection[] = [];
  for (const module of modules) {
    const entries: ShortcutEntry[] = [...(MODULE_SHORTCUTS[module.id] ?? [])];
    for (const command of module.commands) {
      if (!command.shortcut || COVERED_COMMANDS.has(command.id)) continue;
      const parsed = parseGlyphShortcut(command.shortcut);
      if (!parsed) continue;
      entries.push({
        id: command.id,
        action: command.title.replace(/…$/, ""),
        keys: [{ kind: "chord", chord: parsed }],
      });
    }
    if (entries.length > 0) {
      perModule.push({ id: module.id, title: module.title, here: module.id === activeModuleId, entries });
    }
  }
  perModule.sort((a, b) => a.title.localeCompare(b.title));
  const here = perModule.filter((section) => section.here);
  const others = perModule.filter((section) => !section.here);
  return [
    ...here,
    { id: "everywhere", title: "Everywhere", here: false, entries: EVERYWHERE },
    ...others,
  ];
}

/** The words a key cap is drawn with on a platform. */
export function capLabels(cap: KeyCap, mac: boolean): string[] {
  if (cap.kind === "chord") return [chordLabel(cap.chord, mac)];
  if (cap.kind === "range") return [chordLabel(cap.from, mac), chordLabel(cap.to, mac)];
  return [mac ? "⌥ Click" : "Alt+Click"];
}

/**
 * What the search matches on: the action, where it applies, the module, and the
 * keys — written for both platforms, so "⌘⇧" and "ctrl+shift" each find what
 * they mean whichever this is.
 */
function haystack(section: ShortcutSection, entry: ShortcutEntry): string {
  const keys = entry.keys.flatMap((cap) => [...capLabels(cap, true), ...capLabels(cap, false)]);
  return [entry.action, entry.context ?? "", section.title, ...keys].join(" ").toLowerCase();
}

/** The sections narrowed to the entries the query names; a section left with none is dropped. */
export function filterSections(sections: ShortcutSection[], query: string): ShortcutSection[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return sections;
  return sections
    .map((section) => ({
      ...section,
      entries: section.entries.filter((entry) => haystack(section, entry).includes(needle)),
    }))
    .filter((section) => section.entries.length > 0);
}
