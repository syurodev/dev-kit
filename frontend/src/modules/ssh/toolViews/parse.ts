/**
 * Shape check for `ssh.runCommand`'s result (docs/dev-kit/tool-result-view.mdx),
 * run synchronously in the manifest before the card's chunk loads.
 *
 * Mirrors internal/ssh/ssh.go: CommandResult — `{output}` and nothing else: no
 * exit code, no profile. The command is the call's own input.
 */

import { parseToolOutput } from "@/workbench/toolResultViews";

export type RunCommandResult = { command: string; output: string };

/** How many lines of output a card shows before "Show all". */
export const SHOWN_LINES = 20;

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

export function parseRunCommand(rawInput: unknown, rawOutput: unknown): RunCommandResult | null {
  const parsed = parseToolOutput(rawOutput);
  if (!isRecord(parsed) || typeof parsed.output !== "string") return null;
  const command = isRecord(rawInput) && typeof rawInput.command === "string" ? rawInput.command : "";
  return { command, output: parsed.output };
}

function lines(text: string): string[] {
  if (text === "") return [];
  const all = text.split(/\r?\n/);
  // A newline ends the last line; it does not start another.
  if (all[all.length - 1] === "") all.pop();
  return all;
}

/** The output's line count, as a terminal would show it. */
export function lineCount(text: string): number {
  return lines(text).length;
}

/** The first `max` lines, and how many more there were. */
export function clipLines(text: string, max: number): { shown: string; hidden: number } {
  const all = lines(text);
  return { shown: all.slice(0, max).join("\n"), hidden: Math.max(0, all.length - max) };
}

/**
 * Terminal escapes, all of ECMA-48's shapes: CSI with private (`<=>?`) and
 * intermediate bytes; OSC ended by BEL or ST (titles, OSC 8 hyperlinks); DCS,
 * SOS, PM and APC strings; charset designators (`ESC ( B`); and the
 * two-character escapes (`ESC M`, `ESC 7`, `ESC =`).
 */
const ANSI = /\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[PX^_][^\x1b]*\x1b\\|[()][0-9A-Za-z]|[0-9@-Z\\-_=>])/g;

/**
 * The output as a terminal would have left it on screen, for display only
 * (Copy keeps the raw bytes): escapes dropped, and a line a progress bar
 * redrew with `\r` reduced to what it last said. A `\r` that ends a line
 * returns the cursor and draws nothing, so it erases nothing.
 */
export function displayOutput(output: string): string {
  return output
    .replace(ANSI, "")
    .split("\n")
    .map((line) => {
      const drawn = line.replace(/\r+$/, "");
      return drawn.slice(drawn.lastIndexOf("\r") + 1);
    })
    .join("\n");
}

export type CommandDisplay = { count: number; shown: string; hidden: number };

/** What the card draws of an output: its display lines, counted and clipped unless `all`. */
export function commandDisplay(output: string, all: boolean): CommandDisplay {
  const text = displayOutput(output);
  const clip = clipLines(text, all ? Number.POSITIVE_INFINITY : SHOWN_LINES);
  return { count: lineCount(text), shown: clip.shown, hidden: clip.hidden };
}
