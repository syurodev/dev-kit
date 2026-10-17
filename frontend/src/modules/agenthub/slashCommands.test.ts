import { describe, expect, it } from "bun:test";

import type { AvailableCommand } from "./types";
import {
  commandName,
  compactOffered,
  commandQuery,
  filterCommands,
  promptWithCommand,
  promptWithoutCommand,
} from "./slashCommands";

const commands: AvailableCommand[] = [
  { name: "compact", description: "summarise the turn" },
  { name: "clear-history" },
  { name: "review", input: { hint: "path" } },
  { name: "code-review" },
];

describe("deciding when the prompt is naming a command", () => {
  it("opens on the slash and narrows as the name is typed", () => {
    expect(commandQuery("/")).toBe("");
    expect(commandQuery("/comp")).toBe("comp");
    expect(commandQuery("  /comp")).toBe("comp"); // leading space is still a command
  });

  it("closes once an argument follows, and never opens mid-sentence", () => {
    // The space is the end of the name: what comes next is the command's own
    // argument, not another command to pick.
    expect(commandQuery("/review ")).toBeNull();
    expect(commandQuery("/review src/main.go")).toBeNull();
    // A slash inside a sentence is a sentence — the agent reads it that way too.
    expect(commandQuery("see /compact")).toBeNull();
    expect(commandQuery("")).toBeNull();
    expect(commandQuery("hello")).toBeNull();
  });
});

describe("choosing which commands to offer", () => {
  it("offers everything before a name is typed", () => {
    expect(filterCommands(commands, "")).toHaveLength(4);
  });

  it("puts names that start with the query before names that merely contain it", () => {
    // /review is typed in full; code-review only contains it, so it comes after.
    const matches = filterCommands(commands, "review").map((command) => command.name);
    expect(matches).toEqual(["review", "code-review"]);
  });

  it("keeps the agent's own order within a group", () => {
    const matches = filterCommands(commands, "c").map((command) => command.name);
    expect(matches).toEqual(["compact", "clear-history", "code-review"]);
  });

  it("ignores the slash and the case the agent happened to use", () => {
    expect(filterCommands([{ name: "/Compact" }], "comp")).toHaveLength(1);
    expect(filterCommands(commands, "/comp")).toHaveLength(1);
  });

  it("offers nothing when nothing matches", () => {
    expect(filterCommands(commands, "zzz")).toHaveLength(0);
  });
});

describe("putting the chosen command into the prompt", () => {
  it("replaces the half-typed name rather than appending to it", () => {
    expect(promptWithCommand("/comp", "compact")).toBe("/compact ");
    expect(promptWithCommand("/", "compact")).toBe("/compact ");
  });

  it("appends when the prompt already holds a message", () => {
    // The pill can be clicked with words already in the box.
    expect(promptWithCommand("look at this", "review")).toBe("look at this /review ");
    expect(promptWithCommand("look at this ", "review")).toBe("look at this /review ");
    expect(promptWithCommand("", "review")).toBe("/review ");
  });

  it("sends the name the agent advertised, slash or not", () => {
    expect(commandName("compact")).toBe("/compact");
    expect(commandName("/compact")).toBe("/compact");
  });
});

describe("clearing the command once it has been sent on its own", () => {
  it("takes the half-typed command out", () => {
    expect(promptWithoutCommand("/comp")).toBe("");
  });

  it("leaves a real message alone", () => {
    // A command picked from the pill mid-message must not eat the message.
    expect(promptWithoutCommand("half a thought")).toBe("half a thought");
  });
});

// What a Codex session announces: DevKit's own list (internal/agenthub/codex_commands.go),
// in the same shape Claude's arrive in — bare names, a hint only where text follows.
const codexCommands: AvailableCommand[] = [
  { name: "compact", description: "Summarize the conversation to free up context" },
  { name: "review", description: "Review code changes", input: { hint: "[base <branch> | commit <sha> | what to look for]" } },
];

describe("the commands a Codex session announces", () => {
  it("fill the slash menu like any agent's", () => {
    expect(commandQuery("/")).toBe("");
    expect(filterCommands(codexCommands, "").map((c) => c.name)).toEqual(["compact", "review"]);
    expect(filterCommands(codexCommands, "re").map((c) => c.name)).toEqual(["review"]);
    expect(filterCommands(codexCommands, "comp").map((c) => c.name)).toEqual(["compact"]);
  });

  it("leave /review ready for its target and /compact ready to send", () => {
    expect(promptWithCommand("/rev", "review")).toBe("/review ");
    expect(promptWithCommand("/co", "compact")).toBe("/compact ");
    expect(promptWithoutCommand("/compact")).toBe("");
  });
});

describe("offering Compact session", () => {
  it("appears once the agent announces /compact, in either spelling", () => {
    expect(compactOffered(codexCommands, false)).toBe(true);
    expect(compactOffered([{ name: "/compact" }], false)).toBe(true);
  });

  it("is not offered before the announcement, or for an agent without the command", () => {
    // A Codex session before its commands arrive, and one that never lists /compact.
    expect(compactOffered([], false)).toBe(false);
    expect(compactOffered([{ name: "review" }, { name: "compact-all" }], false)).toBe(false);
  });

  it("is not offered while a turn is running", () => {
    // Codex ends the running turn when a compaction starts on its thread.
    expect(compactOffered(codexCommands, true)).toBe(false);
  });
});
