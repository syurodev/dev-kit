import { readThemeColors, resolveColor } from "./themeColors";

/**
 * The terminal's palette, from the app's tokens.
 *
 * xterm renders to a canvas, so unlike CodeMirror it cannot be handed
 * `var(--token)` and left to re-resolve itself — the values have to be read
 * out of the document and pushed in on every theme change.
 *
 * The sixteen ANSI slots come off the status ramp rather than from sixteen new
 * tokens. No family in FAMILIES publishes a full ANSI set, and inventing one
 * per family is sixteen guesses each; mapping onto colours that are already
 * contrast-checked against the surfaces keeps a terminal legible in a theme
 * nobody tested it against. Bright variants lift toward the foreground, which
 * is what "bright" means on every real terminal palette.
 */
const TOKENS = [
  "--text-primary",
  "--text-secondary",
  "--text-tertiary",
  "--surface-tinted",
  "--brand-text",
  "--status-info",
  "--status-success",
  "--status-warning",
  "--status-recovery",
  "--status-danger",
  "--background-app",
] as const;

export type TerminalPalette = {
  foreground: string;
  cursor: string;
  cursorAccent: string;
  selectionBackground: string;
  black: string;
  red: string;
  green: string;
  yellow: string;
  blue: string;
  magenta: string;
  cyan: string;
  white: string;
  brightBlack: string;
  brightRed: string;
  brightGreen: string;
  brightYellow: string;
  brightBlue: string;
  brightMagenta: string;
  brightCyan: string;
  brightWhite: string;
};

/**
 * Mixes `color` toward `toward`, and RESOLVES the result.
 *
 * The resolve is not optional: xterm parses `#rgb`, `rgb()` and named colours,
 * and a `color-mix(…)` string handed to it would not parse — it would fall
 * back without saying so. Caught by reading the palette back in a browser
 * rather than by reasoning about it.
 */
function lift(color: string, toward: string, percent: number): string {
  return resolveColor(`color-mix(in srgb, ${color} ${100 - percent}%, ${toward} ${percent}%)`);
}

export function terminalTheme(): TerminalPalette | null {
  const c = readThemeColors(TOKENS);
  if (!c["--text-primary"]) {
    return null;
  }
  const fg = c["--text-primary"];
  return {
    foreground: fg,
    cursor: c["--brand-text"],
    cursorAccent: c["--background-app"],
    selectionBackground: c["--surface-tinted"],
    black: c["--background-app"],
    red: c["--status-danger"],
    green: c["--status-success"],
    yellow: c["--status-warning"],
    blue: c["--status-info"],
    magenta: c["--status-recovery"],
    cyan: c["--brand-text"],
    white: c["--text-secondary"],
    // "Bright" is the same hue carried toward the foreground — the relationship
    // every terminal palette encodes, rather than eight more invented colours.
    brightBlack: c["--text-tertiary"],
    brightRed: lift(c["--status-danger"], fg, 30),
    brightGreen: lift(c["--status-success"], fg, 30),
    brightYellow: lift(c["--status-warning"], fg, 30),
    brightBlue: lift(c["--status-info"], fg, 30),
    brightMagenta: lift(c["--status-recovery"], fg, 30),
    brightCyan: lift(c["--brand-text"], fg, 30),
    brightWhite: fg,
  };
}
