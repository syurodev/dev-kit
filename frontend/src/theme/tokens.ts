/**
 * The colour contract a theme has to satisfy.
 *
 * Three tiers, and only the third is a theme's job:
 *
 * - `TINT_TOKENS` — what alpha layers are made OF. A state layer, a hairline
 *   and a specular edge are three different questions, so there are three.
 * - `THEME_TOKENS` — the semantic colours. `:root[data-theme="light"]` is the
 *   reference implementation of exactly this list.
 * - Everything else in index.css is derived: `--card: var(--surface-base)`,
 *   `--state-soft: color-mix(… var(--tint-veil) …)`. Derived tokens follow
 *   their parent and must never appear here, or a theme would be asked to
 *   restate a value the cascade already knows.
 *
 * The split matters because it is what keeps a theme to ~40 declarations
 * rather than the 91 colour custom properties index.css declares in total.
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
  // Note/folder palette
  "--note-folder-red",
  "--note-folder-orange",
  "--note-folder-yellow",
  "--note-folder-green",
  "--note-folder-teal",
  "--note-folder-blue",
  "--note-folder-violet",
  "--note-folder-pink",
  "--note-folder-gray",
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
 * Code colours. Optional for a family, unlike the tiers above: index.css
 * derives every one from the ramp (`--syntax-keyword: var(--brand-text)` …),
 * so a family that says nothing about code gets code in its own status
 * colours. A family whose editor has a palette of its own — Xcode's pink
 * keywords and red strings — sets them, and themeContrast.test.ts floors
 * them like any other text.
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

export type TintToken = (typeof TINT_TOKENS)[number];
export type ThemeToken = (typeof THEME_TOKENS)[number];
export type MixToken = (typeof MIX_TOKENS)[number];
export type SyntaxToken = (typeof SYNTAX_TOKENS)[number];

/** Every custom property a complete theme may set. */
export const CONTRACT = [...TINT_TOKENS, ...THEME_TOKENS, ...MIX_TOKENS, ...SYNTAX_TOKENS] as const;
