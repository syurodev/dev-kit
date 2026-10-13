import { readThemeColors, resolveColor } from "./themeColors";

/**
 * The terminal's palette, from the app's tokens.
 *
 * xterm renders to a canvas, so unlike CodeMirror it cannot be handed
 * `var(--token)` and left to re-resolve itself — the values have to be read
 * out of the document and pushed in on every theme change.
 *
 * The six chromatic ANSI slots are the Color palette's `--ansi-*` (derived
 * from its hues, contrast-checked against the Mono grounds). Black, white and
 * the foreground come from the chrome. Bright variants lift toward the
 * foreground, which is what "bright" means on every real terminal palette.
 */
const TOKENS = [
  "--text-primary",
  "--text-secondary",
  "--text-tertiary",
  "--surface-tinted",
  "--brand-text",
  "--background-app",
  "--ansi-red",
  "--ansi-green",
  "--ansi-yellow",
  "--ansi-blue",
  "--ansi-magenta",
  "--ansi-cyan",
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
    red: c["--ansi-red"],
    green: c["--ansi-green"],
    yellow: c["--ansi-yellow"],
    blue: c["--ansi-blue"],
    magenta: c["--ansi-magenta"],
    cyan: c["--ansi-cyan"],
    white: c["--text-secondary"],
    // "Bright" is the same hue carried toward the foreground — the relationship
    // every terminal palette encodes, rather than eight more invented colours.
    brightBlack: c["--text-tertiary"],
    brightRed: lift(c["--ansi-red"], fg, 30),
    brightGreen: lift(c["--ansi-green"], fg, 30),
    brightYellow: lift(c["--ansi-yellow"], fg, 30),
    brightBlue: lift(c["--ansi-blue"], fg, 30),
    brightMagenta: lift(c["--ansi-magenta"], fg, 30),
    brightCyan: lift(c["--ansi-cyan"], fg, 30),
    brightWhite: fg,
  };
}
