import type { AvailableCommand } from "./types";

/**
 * Typing a slash command in the composer.
 *
 * The commands themselves are the agent's, not DevKit's: they arrive as
 * available_commands_update and differ per agent and per project. Nothing here
 * knows what any of them does — a command is a name the prompt is allowed to
 * start with, and the turn that carries it is an ordinary prompt.
 */

/** The command's name as it is sent, however the agent spelled it. */
export function commandName(name: string): string {
  return name.startsWith("/") ? name : `/${name}`;
}

function bareName(name: string): string {
  return name.replace(/^\//, "").toLowerCase();
}

/**
 * What the prompt is in the middle of naming, or null when it is naming no
 * command at all. "" means the slash has been typed and nothing after it.
 *
 * Matched against the WHOLE prompt rather than the text around the caret: a
 * slash command is the entire turn — the agent reads "/compact" as a command
 * and "see /compact" as a sentence — so the list belongs open exactly while the
 * prompt is nothing but a command being typed, and closes the moment an
 * argument follows. That also keeps the caret out of it: the value alone
 * decides, rather than a second source of truth to keep in step with the
 * textarea's selection.
 */
export function commandQuery(value: string): string | null {
  const match = /^\s*\/(\S*)$/.exec(value);
  return match ? match[1] : null;
}

/**
 * The commands worth showing for what has been typed, best match first.
 *
 * Names that START with the query come before names that merely contain it:
 * "/re" should offer /review before /clear-history, which is what typing a
 * prefix means everywhere else. Within each group the agent's own order is
 * kept — it is the order the agent chose to advertise them in.
 */
export function filterCommands(commands: AvailableCommand[], query: string): AvailableCommand[] {
  const needle = bareName(query);
  if (!needle) return commands;
  const prefix: AvailableCommand[] = [];
  const contains: AvailableCommand[] = [];
  for (const command of commands) {
    const name = bareName(command.name);
    if (name.startsWith(needle)) prefix.push(command);
    else if (name.includes(needle)) contains.push(command);
  }
  return [...prefix, ...contains];
}

/**
 * The prompt with `name` in place of the command being typed, ready for the
 * argument that follows it.
 *
 * Replaces rather than appends when a command is already being typed: picking
 * /compact after typing "/comp" has to leave "/compact ", not "/comp /compact".
 * With no command in progress the name is appended instead, which is what the
 * composer's command pill does to a prompt that already has words in it.
 */
export function promptWithCommand(value: string, name: string): string {
  const slug = commandName(name);
  if (commandQuery(value) !== null) return `${slug} `;
  if (value === "" || /\s$/.test(value)) return `${value}${slug} `;
  return `${value} ${slug} `;
}

/**
 * The prompt with the command being typed removed — what the composer is left
 * holding once a command that takes no argument has been sent as its own turn.
 * Anything else the prompt holds is left alone: the pill can be clicked with a
 * half-written message in the box, and that message is not ours to throw away.
 */
export function promptWithoutCommand(value: string): string {
  return commandQuery(value) === null ? value : "";
}

/**
 * Whether the context meter may offer "Compact session".
 *
 * Only when the agent announced a `/compact` — a button that sends a prompt the
 * agent does not understand is a button that makes it reply "?" — and not while a
 * turn is running: Codex ends the running turn when a compaction starts on its
 * thread, and Claude would only queue the command behind it. Both vendors
 * announce the same way (Codex's list is DevKit's own, Claude's is the
 * vendor's), so nothing here asks which one it is.
 */
export function compactOffered(commands: AvailableCommand[], running: boolean): boolean {
  return !running && commands.some((command) => bareName(command.name) === "compact");
}
