import { describe, expect, it } from "bun:test";
import { Database, Network, Wrench } from "lucide-react";
import { composeSections, palettePlaceholder, staticSections, type PaletteInput, type PaletteModule } from "./paletteModel";

const noop = () => {};

function module(id: string, title: string, overrides: Partial<PaletteModule> = {}): PaletteModule {
  return { id, title, icon: Wrench, switchable: true, unavailable: false, commands: [], ...overrides };
}

const modules: PaletteModule[] = [
  module("agenthub", "Agent Hub", { shortcut: "⌘1", commands: [{ id: "agenthub.cancel", title: "Cancel turn", run: noop }] }),
  module("codegraph", "Code Graph", {
    icon: Network,
    shortcut: "⌘2",
    commands: [{ id: "codegraph.reindex_project", title: "Re-index Code Graph project", run: noop }],
  }),
  module("database", "Database", {
    icon: Database,
    shortcut: "⌘3",
    commands: [
      { id: "database.console.new", title: "New console", shortcut: "⌘T", run: noop },
      { id: "database.schema.reindex", title: "Re-index schema", run: noop },
    ],
  }),
  module("tools", "Tools", { commands: [{ id: "tools.hash", title: "Hash text" }] }), // no run
  module("settings", "Settings", { switchable: false }),
];

const actions = { focusModule: noop, activateTab: noop };

function sections(overrides: Partial<PaletteInput>) {
  return staticSections({ query: "", activeModuleId: "database", modules, tabs: [], ...overrides }, actions).map((s) => ({
    heading: s.heading,
    rows: s.rows.map((r) => r.title),
  }));
}

describe("staticSections", () => {
  it("with nothing typed, offers the module in front's commands and the other modules", () => {
    expect(sections({})).toEqual([
      { heading: "Database", rows: ["New console", "Re-index schema"] },
      { heading: "Go to", rows: ["Agent Hub", "Code Graph", "Tools"] },
    ]);
  });

  it("keeps another module's commands out until the query asks for them, then names the module", () => {
    expect(sections({}).some((s) => s.rows.includes("Cancel turn"))).toBe(false);
    expect(sections({ query: "cancel" })).toEqual([{ heading: "Agent Hub", rows: ["Cancel turn"] }]);
  });

  it("puts the module in front first when two modules answer the same query", () => {
    expect(sections({ query: "re-index" })).toEqual([
      { heading: "Database", rows: ["Re-index schema"] },
      { heading: "Code Graph", rows: ["Re-index Code Graph project"] },
    ]);
  });

  it("never lists a command that cannot run", () => {
    expect(sections({ query: "hash" })).toEqual([]);
    expect(sections({ activeModuleId: "tools" })[0].heading).toBe("Go to");
  });

  it("finds a module by name, with its shortcut, and leaves out the one in front and the header-only ones", () => {
    const found = staticSections({ query: "code", activeModuleId: "database", modules, tabs: [] }, actions);
    expect(found[0].heading).toBe("Go to");
    expect(found[0].rows.map((r) => [r.title, r.shortcut])).toEqual([["Code Graph", "⌘2"]]);
    expect(sections({ query: "database" }).some((s) => s.heading === "Go to")).toBe(false);
    expect(sections({ query: "settings" })).toEqual([]);
  });

  it("searches open tabs across modules, labelled by module, only when typing", () => {
    const tabs = [
      { id: "t1", moduleId: "codegraph", title: "DeviceController.java" },
      { id: "t2", moduleId: "database", title: "public.devices" },
    ];
    expect(sections({ tabs }).some((s) => s.heading === "Open tabs")).toBe(false);
    const found = staticSections({ query: "device", activeModuleId: "database", modules, tabs }, actions);
    const open = found.find((s) => s.heading === "Open tabs")!;
    expect(open.rows.map((r) => [r.title, r.subtitle])).toEqual([
      ["DeviceController.java", "Code Graph"],
      ["public.devices", "Database"],
    ]);
  });

  it("skips an unavailable module entirely", () => {
    const withDown = modules.map((m) => (m.id === "codegraph" ? { ...m, unavailable: true } : m));
    const found = staticSections({ query: "", activeModuleId: "database", modules: withDown, tabs: [] }, actions);
    expect(found[1].rows.map((r) => r.title)).toEqual(["Agent Hub", "Tools"]);
  });

  it("runs what the row says", () => {
    const focused: string[] = [];
    const found = staticSections({ query: "", activeModuleId: "database", modules, tabs: [] }, { ...actions, focusModule: (id) => focused.push(id) });
    found[1].rows[1].run();
    expect(focused).toEqual(["codegraph"]);
  });
});

describe("composeSections", () => {
  const fixed = [
    { id: "commands:database", heading: "Database", rows: [] },
    { id: "modules", heading: "Go to", rows: [] },
  ];
  const found = [
    { heading: "Tables", items: [{ id: "public.orders", title: "orders", run: noop }] },
    { heading: "Connections", items: [] },
  ];

  it("leads with what was searched for", () => {
    expect(composeSections("ord", "database", fixed, found).map((s) => s.heading)).toEqual(["Tables", "Database", "Go to"]);
  });

  it("offers unprompted results under the module's own commands", () => {
    expect(composeSections("", "database", fixed, found).map((s) => s.heading)).toEqual(["Database", "Tables", "Go to"]);
  });

  it("gives every found row an id no other row can have", () => {
    const [tables] = composeSections("ord", "database", fixed, found);
    expect(tables.rows[0].id).toBe("search:database:Tables:public.orders");
  });
});

describe("palettePlaceholder", () => {
  it("names what the module in front can be searched for", () => {
    expect(palettePlaceholder("tables and connections")).toBe("Search tables and connections, commands, modules…");
    expect(palettePlaceholder(undefined)).toBe("Search commands, modules…");
  });
});
