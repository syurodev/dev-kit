/**
 * The colour contract a theme has to satisfy.
 *
 * Two owners:
 *
 * - CHROME (`TINT_TOKENS`, `THEME_TOKENS`, `MIX_TOKENS`) — the Mono shell.
 *   index.css is its only author; light mode is `:root[data-theme="light"]`.
 * - PALETTE (`HUE_TOKENS`, `SYNTAX_TOKENS`, `SYNTAX_STYLE_TOKENS`,
 *   `TERMINAL_TOKENS`) — what the person picks as "Color palette". A family
 *   declares hues (and optionally syntax); families.ts resolves the rest.
 *
 * Derived tokens (`--card: var(--surface-base)`, `--note-folder-red:
 * var(--hue-red)`) follow their parent and appear in neither list.
 */

/** What alpha layers are drawn from. Dark lifts, light recesses. */
export const TINT_TOKENS = [
  "--tint-veil",
  "--tint-border",
  "--tint-specular",
] as const;

/**
 * The semantic colours, grouped the way the design file groups them.
 *
 * `--action-primary` and its hover/pressed pair are here even though light
 * does not override them: light deliberately keeps the lime fill (onyx text
 * on lime passes in both modes), but a theme with its own accent has to say
 * all three, and a hover shade is not derivable from a hue without a decision.
 */
export const THEME_TOKENS = [
  // Backgrounds and surfaces
  "--background-app",
  "--background-canvas",
  "--surface-base",
  "--surface-raised",
  "--surface-tinted",
  // Text and icons
  "--text-primary",
  "--text-secondary",
  "--text-tertiary",
  "--icon-secondary",
  // The accent
  "--action-primary",
  "--action-primary-foreground",
  "--action-primary-hover",
  "--action-primary-pressed",
  "--brand-text",
  // Status
  "--status-info",
  "--status-success",
  "--status-warning",
  "--status-recovery",
  "--status-danger",
  // Focus
  "--border-focus",
  // Glass
  "--glass-rail-bg",
] as const;

/**
 * Percentages, not colours: the same token can want a different strength in a
 * light theme than a dark one (a hairline recessing into white needs more
 * than one lifting off black). A theme may override these; most will not.
 */
export const MIX_TOKENS = [
  "--feedback-success-soft",
  "--surface-glass-modal",
  "--glass-panel-bg",
  "--glass-float-bg",
  "--glass-border",
  "--glass-highlight",
  "--state-soft",
  "--surface-well",
  "--scrollbar-thumb",
  "--scrollbar-thumb-hover",
  "--border-default",
  "--border-subtle",
] as const;

/**
 * Code colours. A family may set them; when it does not, `resolvePalette`
 * (families.ts) derives them from its hues. The base (devkit) palette sets
 * them to the text ramp — Mono code — and index.css carries those values.
 */
export const SYNTAX_TOKENS = [
  "--syntax-keyword",
  "--syntax-string",
  "--syntax-number",
  "--syntax-type",
  "--syntax-function",
  "--syntax-definition",
  "--syntax-comment",
  "--syntax-meta",
] as const;

/**
 * Not a colour, but part of how a palette draws code: Mono code has one hue,
 * so its keywords are told apart by weight. Every other palette resolves it
 * to 400.
 */
export const SYNTAX_STYLE_TOKENS = ["--syntax-keyword-weight"] as const;

/**
 * The palette proper: eight hues per mode, the one thing every family must
 * declare. Decorative and categorical colour reads these (note folders,
 * category chips, CodeGraph layers, engine marks); status never does.
 */
export const HUE_TOKENS = [
  "--hue-red",
  "--hue-orange",
  "--hue-yellow",
  "--hue-green",
  "--hue-teal",
  "--hue-blue",
  "--hue-violet",
  "--hue-pink",
] as const;

/**
 * The terminal's six chromatic ANSI slots. Black, white, foreground, cursor
 * and selection come from the chrome (terminalTheme.ts); bright variants are
 * derived there.
 */
export const TERMINAL_TOKENS = [
  "--ansi-red",
  "--ansi-green",
  "--ansi-yellow",
  "--ansi-blue",
  "--ansi-magenta",
  "--ansi-cyan",
] as const;

export type TintToken = (typeof TINT_TOKENS)[number];
export type ThemeToken = (typeof THEME_TOKENS)[number];
export type MixToken = (typeof MIX_TOKENS)[number];
export type SyntaxToken = (typeof SYNTAX_TOKENS)[number];
export type SyntaxStyleToken = (typeof SYNTAX_STYLE_TOKENS)[number];
export type HueToken = (typeof HUE_TOKENS)[number];
export type TerminalToken = (typeof TERMINAL_TOKENS)[number];

/** Owned by index.css alone. A family may not set any of these. */
export const CHROME_TOKENS = [...TINT_TOKENS, ...THEME_TOKENS, ...MIX_TOKENS] as const;

/** Everything a family's resolved palette carries, and nothing else. */
export const PALETTE_TOKENS = [
  ...HUE_TOKENS,
  ...SYNTAX_TOKENS,
  ...SYNTAX_STYLE_TOKENS,
  ...TERMINAL_TOKENS,
] as const;
export type PaletteToken = (typeof PALETTE_TOKENS)[number];

/** Every custom property index.css must declare at :root. */
export const CONTRACT = [...CHROME_TOKENS, ...PALETTE_TOKENS] as const;
