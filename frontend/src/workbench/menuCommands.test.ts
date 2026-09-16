import { describe, expect, it } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { databaseModule } from "@/modules/database/manifest";
import { agenthubModule } from "@/modules/agenthub/manifest";
import { useAgentSessionsStore } from "@/stores/agentSessions";
import { isCloseTabShortcut } from "./menuCommands";

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
      [...databaseModule.commands, ...agenthubModule.commands].map((command) => [command.id, command]),
    );
    for (const id of menuIDs()) {
      const command = runnable.get(id);
      expect(command, `the menu has an item for "${id}" and no module declares it`).toBeDefined();
      expect(typeof command!.run, `"${id}" is declared but has nothing to run`).toBe("function");
    }
  });

  it("every declared command can run", () => {
    for (const command of [...databaseModule.commands, ...agenthubModule.commands]) {
      expect(typeof command.run, `"${command.id}" has no handler`).toBe("function");
    }
  });

  it("routes Agent Hub palette commands through the shared command bus", () => {
    const command = agenthubModule.commands.find((item) => item.id === "agenthub.create");
    expect(typeof command?.run).toBe("function");
    useAgentSessionsStore.getState().consumeAgentHubCommand("create");
    command?.run?.();
    expect(useAgentSessionsStore.getState().pendingAgentHubCommand).toBe("create");
    useAgentSessionsStore.getState().consumeAgentHubCommand("create");
  });

  /**
   * Both directions, because the Go list is also what internal/app reports as
   * the module's commands — and the shell compares THAT against this manifest
   * on every launch. A command here and not there is the "Module metadata is
   * unavailable or out of sync" banner, which is how this was found.
   *
   * It is also the rule the menu bar is for: designing-for-macos asks for all
   * of an app's commands to be reachable from it, so a command that is in the
   * palette and not the menu is a gap rather than a preference.
   */
  it("names exactly the same set as the manifest, both ways", () => {
    expect(menuIDs().slice().sort()).toEqual(
      [...databaseModule.commands, ...agenthubModule.commands].map((command) => command.id).sort(),
    );
  });
});

describe("Close Tab's shortcut where there is no menu bar to carry it", () => {
  const press = (over: Partial<KeyboardEvent> = {}) =>
    ({
      key: "w",
      ctrlKey: true,
      metaKey: false,
      altKey: false,
      shiftKey: false,
      repeat: false,
      isComposing: false,
      defaultPrevented: false,
      ...over,
    }) as KeyboardEvent;

  it("answers Ctrl+W on Linux, which has no menu bar", () => {
    expect(isCloseTabShortcut(press(), "linux")).toBe(true);
  });

  it("stands aside everywhere the menu already carries the accelerator", () => {
    // Bound on both, one keypress would close two tabs.
    expect(isCloseTabShortcut(press(), "mac")).toBe(false);
    expect(isCloseTabShortcut(press(), "windows")).toBe(false);
    expect(isCloseTabShortcut(press({ metaKey: true, ctrlKey: false }), "mac")).toBe(false);
  });

  it("leaves Ctrl+Shift+W to the window manager", () => {
    expect(isCloseTabShortcut(press({ shiftKey: true }), "linux")).toBe(false);
  });

  it("stands aside for a key something else already handled, a repeat or an IME", () => {
    expect(isCloseTabShortcut(press({ defaultPrevented: true }), "linux")).toBe(false);
    expect(isCloseTabShortcut(press({ repeat: true }), "linux")).toBe(false);
    expect(isCloseTabShortcut(press({ isComposing: true }), "linux")).toBe(false);
  });

  it("is only W", () => {
    expect(isCloseTabShortcut(press({ key: "q" }), "linux")).toBe(false);
    expect(isCloseTabShortcut(press({ ctrlKey: false }), "linux")).toBe(false);
  });
});
