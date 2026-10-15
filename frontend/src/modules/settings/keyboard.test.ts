import { describe, expect, test } from "bun:test";
import type { ModuleManifest } from "@/modules/registry";
import { buildSections, capLabels, filterSections } from "./keyboard";

const module = (id: string, title: string, commands: ModuleManifest["commands"] = []) =>
  ({ id, title, commands }) as ModuleManifest;

const modules = [
  module("notes", "Notes"),
  module("database", "Database", [
    { id: "database.query", title: "Run query", shortcut: "⌘↵" },
    { id: "database.console.new", title: "New console", shortcut: "⌘T" },
    { id: "database.connection.new", title: "New connection…", shortcut: "⇧⌘N" },
    { id: "database.explain", title: "Explain" },
  ]),
  module("git", "Git"),
];

describe("buildSections", () => {
  test("lists the module you are in first, then the shell's own, then the rest", () => {
    const sections = buildSections(modules, "notes");
    expect(sections.map((s) => [s.title, s.here])).toEqual([
      ["Notes", true],
      ["Everywhere", false],
      ["Database", false],
    ]);
    expect(buildSections(modules, "git").map((s) => s.title)).toEqual(["Everywhere", "Database", "Notes"]);
  });

  test("a module with no shortcuts has no section", () => {
    expect(buildSections(modules, "git").some((s) => s.id === "git")).toBe(false);
  });

  test("reads a module's command shortcuts from its manifest, without the ellipsis", () => {
    const database = buildSections(modules, "git").find((s) => s.id === "database")!;
    const connection = database.entries.find((e) => e.id === "database.connection.new");
    expect(connection?.action).toBe("New connection");
    expect(connection && capLabels(connection.keys[0], true)).toEqual(["⌘⇧N"]);
    // No shortcut, no row.
    expect(database.entries.some((e) => e.id === "database.explain")).toBe(false);
  });

  test("says a command the editor already covers once, not twice", () => {
    const database = buildSections(modules, "git").find((s) => s.id === "database")!;
    expect(database.entries.filter((e) => e.action.toLowerCase().includes("run query")).length).toBe(1);
  });

  test("the shell's list has the peek chord and the way to this page", () => {
    const everywhere = buildSections(modules, "git")[0];
    const peek = everywhere.entries.find((e) => e.id === "peek")!;
    expect(capLabels(peek.keys[0], true)).toEqual(["⌘⌥1", "⌘⌥9"]);
    expect(capLabels(peek.keys[0], false)).toEqual(["Ctrl+Alt+1", "Ctrl+Alt+9"]);
    expect(everywhere.entries.find((e) => e.id === "keyboard")).toBeDefined();
  });
});

describe("filterSections", () => {
  const sections = buildSections(modules, "notes");

  test("no query keeps everything", () => {
    expect(filterSections(sections, "  ")).toBe(sections);
  });

  test("matches an action by any of its words", () => {
    const found = filterSections(sections, "run");
    expect(found.map((s) => s.id)).toEqual(["database"]);
    expect(found[0].entries.map((e) => e.id)).toContain("database.run");
  });

  test("matches keys written either way, whatever platform this is", () => {
    expect(filterSections(sections, "⌘⇧").some((s) => s.entries.some((e) => e.id === "database.connection.new"))).toBe(true);
    expect(filterSections(sections, "ctrl+shift").some((s) => s.entries.some((e) => e.id === "database.connection.new"))).toBe(true);
  });

  test("matches the module name, and drops sections left with nothing", () => {
    expect(filterSections(sections, "database").map((s) => s.id)).toEqual(["database"]);
    expect(filterSections(sections, "zzzz")).toEqual([]);
  });
});

describe("capLabels", () => {
  test("draws the modifier click per platform", () => {
    expect(capLabels({ kind: "click", alt: true }, true)).toEqual(["⌥ Click"]);
    expect(capLabels({ kind: "click", alt: true }, false)).toEqual(["Alt+Click"]);
  });
});

test("the shell lists reopening a closed tab", () => {
  const everywhere = buildSections(modules, "git").find((s) => s.title === "Everywhere")!;
  const reopen = everywhere.entries.find((e) => e.id === "tab-reopen");
  expect(reopen?.action).toBe("Reopen closed tab");
  expect(reopen && capLabels(reopen.keys[0], true)).toEqual(["⌘⇧T"]);
});
