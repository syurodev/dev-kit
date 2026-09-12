import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { databaseModule } from "@/modules/database/manifest";

/**
 * The menu bar is built in Go, at startup, from a list of ids — it cannot ask
 * the frontend whether anything answers to them. An item pointing at an id no
 * module declares is a menu entry that does nothing, silently, which is the
 * failure this whole pass started from.
 */
const SOURCE = readFileSync(
  join(import.meta.dir, "../../../internal/appmenu/appmenu.go"),
  "utf8",
);

function menuIDs(): string[] {
  return [...SOURCE.matchAll(/ID:\s*"([^"]+)"/g)].map((match) => match[1]);
}

describe("the native menu and the module commands describe the same actions", () => {
  it("names some commands at all", () => {
    expect(menuIDs().length).toBeGreaterThan(0);
  });

  it("every menu item resolves to a command that can run", () => {
    const runnable = new Map(
      databaseModule.commands.map((command) => [command.id, command]),
    );
    for (const id of menuIDs()) {
      const command = runnable.get(id);
      expect(command, `the menu has an item for "${id}" and no module declares it`).toBeDefined();
      expect(typeof command!.run, `"${id}" is declared but has nothing to run`).toBe("function");
    }
  });

  // The other direction is deliberately NOT required: a command may live in
  // the palette without earning a place in the menu bar. This only pins that
  // the palette is not the one left empty.
  it("every declared command can run", () => {
    for (const command of databaseModule.commands) {
      expect(typeof command.run, `"${command.id}" has no handler`).toBe("function");
    }
  });
});
