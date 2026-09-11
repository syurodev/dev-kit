import type { ThemeFamilyPreference } from "@/bridge/preferences";
import type { MixToken, ThemeToken, TintToken } from "./tokens";

/**
 * One palette, in one mode: the tokens it overrides and nothing else.
 *
 * Partial on purpose. A family says only what differs from index.css, so it
 * does not have to restate a value it agrees with, and a token added to the
 * base later reaches every family for free.
 */
export type ThemePalette = Partial<Record<TintToken | ThemeToken | MixToken, string>>;

/**
 * A family supplies BOTH modes.
 *
 * Not optional, and it is the one rule that keeps the rest of the system free
 * of branches: with both modes guaranteed, "family × mode" is a total function
 * and nothing has to answer "the system went light and this family has no
 * light". Dracula ships Alucard, its own light palette, for exactly this.
 */
export type ThemeFamilyDefinition = {
  id: ThemeFamilyPreference;
  label: string;
  dark: ThemePalette;
  light: ThemePalette;
};

/**
 * Hover and pressed, derived rather than picked.
 *
 * Eight accents would otherwise mean sixteen hand-invented shades, each a
 * chance to pick one that fails contrast against the foreground it keeps. The
 * direction is the part that is a decision: a dark UI lifts its accent on
 * hover and sinks it on press, a light UI sinks it both times and further on
 * press — which is what index.css already does by hand (lime #c6ff34 hovers to
 * #d2ff64 and presses to #add92e).
 */
function states(accent: string, mode: "dark" | "light") {
  return mode === "dark"
    ? {
        "--action-primary-hover": `color-mix(in srgb, ${accent} 85%, #fff)`,
        "--action-primary-pressed": `color-mix(in srgb, ${accent} 85%, #000)`,
      }
    : {
        "--action-primary-hover": `color-mix(in srgb, ${accent} 88%, #000)`,
        "--action-primary-pressed": `color-mix(in srgb, ${accent} 76%, #000)`,
      };
}

/**
 * The base palette is the absence of an override.
 *
 * index.css already declares every token for both modes, so "devkit" is two
 * empty maps rather than a copy of them — a copy would be a second place to
 * change a colour, and the second place is always the one that gets missed.
 */
export const DEVKIT_FAMILY: ThemeFamilyDefinition = {
  id: "devkit",
  label: "DevKit",
  dark: {},
  light: {},
};

/**
 * Catppuccin — Mocha and Latte.
 *
 * Surfaces follow the family's own ramp (crust · mantle · base · surface0),
 * which happens to match how index.css orders app · canvas · base · raised:
 * outermost is darkest in a dark theme and lightest in a light one.
 */
const CATPPUCCIN: ThemeFamilyDefinition = {
  id: "catppuccin",
  label: "Catppuccin",
  dark: {
    "--tint-veil": "#ffffff",
    "--tint-border": "#9399b2", // overlay2 — the hairline reads as the palette's grey, not white
    "--background-app": "#11111b", // crust
    "--background-canvas": "#181825", // mantle
    "--surface-base": "#1e1e2e", // base
    "--surface-raised": "#313244", // surface0
    "--surface-tinted": "#45475a", // surface1
    "--text-primary": "#cdd6f4", // text
    "--text-secondary": "#bac2de", // subtext1
    "--text-tertiary": "#7f849c", // overlay1
    "--icon-secondary": "#a6adc8", // subtext0
    "--action-primary": "#cba6f7", // mauve
    "--action-primary-foreground": "#11111b",
    ...states("#cba6f7", "dark"),
    "--brand-text": "#cba6f7",
    "--status-info": "#89b4fa", // blue
    "--status-success": "#a6e3a1", // green
    "--status-warning": "#f9e2af", // yellow
    "--status-recovery": "#f5c2e7", // pink
    "--status-danger": "#f38ba8", // red
    "--border-focus": "#b4befe", // lavender
  },
  light: {
    "--tint-veil": "#4c4f69", // text — light recesses with ink
    "--tint-border": "#7c7f93", // overlay2
    "--background-app": "#dce0e8", // crust
    "--background-canvas": "#e6e9ef", // mantle
    "--surface-base": "#eff1f5", // base
    "--surface-raised": "#e6e9ef", // mantle
    "--surface-tinted": "#ccd0da", // surface0
    "--text-primary": "#4c4f69", // text
    "--text-secondary": "#5c5f77", // subtext1
    "--text-tertiary": "#86899b", // overlay1, 4% deeper — see the contrast note below
    "--icon-secondary": "#6c6f85", // subtext0
    "--action-primary": "#8839ef", // mauve
    "--action-primary-foreground": "#eff1f5",
    ...states("#8839ef", "light"),
    "--brand-text": "#8839ef",
    "--status-info": "#1e66f5", // blue
    "--status-success": "#3f9d2a", // green
    "--status-warning": "#c07a19", // yellow
    "--status-recovery": "#c965af", // pink
    "--status-danger": "#d20f39", // red
    "--border-focus": "#7287fd", // lavender
  },
};

/*
 * On the Latte values that are not Latte's exactly: overlay1, green, yellow
 * and pink are published for syntax on Latte's own base, where they measure
 * 2.3–3.0:1. As status TEXT they have to clear 3:1, so each is mixed toward
 * black by the smallest amount that does — 2% for green, 14% for yellow and
 * pink. Ayu Light's entity blue and Ayu Dark's ui.fg got the same treatment
 * for the same reason. The amounts were computed against the exact surface
 * each colour sits on, not guessed; themeContrast.test.ts is what holds them.
 */

/**
 * Dracula, and Alucard for light.
 *
 * Dracula publishes one background and one foreground where index.css wants
 * four surface levels and three text levels, so the intermediates are mixes of
 * the two the palette does give — noted at each one. Alucard is Dracula's own
 * light theme; picking it rather than writing a "no light variant" rule is
 * what lets every family guarantee both modes.
 */
const DRACULA: ThemeFamilyDefinition = {
  id: "dracula",
  label: "Dracula",
  dark: {
    "--tint-veil": "#ffffff",
    "--tint-border": "#6272a4", // comment — Dracula's own blue-grey
    "--background-app": "#191a21",
    "--background-canvas": "#21222c",
    "--surface-base": "#282a36", // background
    "--surface-raised": "#343746", // background ↔ current-line, halfway
    "--surface-tinted": "#44475a", // current line
    "--text-primary": "#f8f8f2", // foreground
    "--text-secondary": "#adb5cb", // foreground ↔ comment, halfway
    "--text-tertiary": "#6272a4", // comment
    "--icon-secondary": "#adb5cb",
    "--action-primary": "#bd93f9", // purple
    "--action-primary-foreground": "#282a36",
    ...states("#bd93f9", "dark"),
    "--brand-text": "#bd93f9",
    "--status-info": "#8be9fd", // cyan
    "--status-success": "#50fa7b", // green
    "--status-warning": "#f1fa8c", // yellow
    "--status-recovery": "#ff79c6", // pink
    "--status-danger": "#ff5555", // red
    "--border-focus": "#bd93f9",
  },
  light: {
    "--tint-veil": "#1f1f1f",
    "--tint-border": "#6c664b", // Alucard comment
    "--background-app": "#e8e4d5",
    "--background-canvas": "#f3efdf",
    "--surface-base": "#fffbeb", // Alucard background
    "--surface-raised": "#f3efdf",
    "--surface-tinted": "#cfcfde", // Alucard selection
    "--text-primary": "#1f1f1f", // Alucard foreground
    "--text-secondary": "#44412f",
    "--text-tertiary": "#6c664b", // Alucard comment
    "--icon-secondary": "#44412f",
    "--action-primary": "#644ac9", // Alucard purple
    "--action-primary-foreground": "#fffbeb",
    ...states("#644ac9", "light"),
    "--brand-text": "#644ac9",
    "--status-info": "#036a96", // Alucard cyan
    "--status-success": "#14710a", // Alucard green
    "--status-warning": "#846e15", // Alucard yellow
    "--status-recovery": "#a3144d", // Alucard pink
    "--status-danger": "#cb3a2a", // Alucard red
    "--border-focus": "#644ac9",
  },
};

/**
 * Ayu — Dark and Light.
 *
 * Ayu publishes two surface levels (bg and panel); app and raised here are the
 * bg darkened and lightened to keep index.css's four-level ramp.
 *
 * The accent needs two values, not one. Ayu's #e6b450 is a fill colour: it
 * carries dark text well and is unreadable as text on Ayu Light's near-white
 * background (about 1.6:1), so --brand-text, which IS text, is a deeper amber.
 */
const AYU: ThemeFamilyDefinition = {
  id: "ayu",
  label: "Ayu",
  dark: {
    "--tint-veil": "#ffffff",
    "--tint-border": "#565b66", // ui.fg
    "--background-app": "#070a10",
    "--background-canvas": "#0b0e14", // common.bg
    "--surface-base": "#0f131a", // ui.panel.bg
    "--surface-raised": "#1b1f2b",
    "--surface-tinted": "#253340",
    "--text-primary": "#bfbdb6", // common.fg
    "--text-secondary": "#9fa09e",
    "--text-tertiary": "#5e636e", // ui.fg, lifted 5% to clear 3:1 on the panel
    "--icon-secondary": "#9fa09e",
    "--action-primary": "#e6b450", // common.accent
    "--action-primary-foreground": "#0b0e14",
    ...states("#e6b450", "dark"),
    "--brand-text": "#e6b450",
    "--status-info": "#59c2ff", // entity
    "--status-success": "#aad94c", // string
    "--status-warning": "#ffb454", // func
    "--status-recovery": "#d2a6ff", // constant
    "--status-danger": "#f07178", // markup
    "--border-focus": "#59c2ff",
  },
  light: {
    "--tint-veil": "#5c6166",
    "--tint-border": "#8a9199", // ui.fg
    "--background-app": "#e7e8ea",
    "--background-canvas": "#f3f4f5", // ui.panel.bg
    "--surface-base": "#f8f9fa", // common.bg
    "--surface-raised": "#f3f4f5",
    "--surface-tinted": "#e7e8ea",
    "--text-primary": "#5c6166", // common.fg
    "--text-secondary": "#697076",
    "--text-tertiary": "#8a9199", // ui.fg
    "--icon-secondary": "#6b7278",
    "--action-primary": "#ffaa33", // common.accent
    "--action-primary-foreground": "#1c1f23",
    ...states("#ffaa33", "light"),
    "--brand-text": "#9e5700", // the accent is a fill; as text it needs depth
    "--status-info": "#3696da", // entity
    "--status-success": "#5c8000", // string, deepened for white
    "--status-warning": "#a35200", // func, deepened for white
    "--status-recovery": "#8a5cd6", // constant
    "--status-danger": "#e65050", // error
    "--border-focus": "#399ee6",
  },
};

/**
 * GitHub — Dark and Light.
 *
 * The one family where the fill accent and the text accent are genuinely
 * different colours rather than one adapted: GitHub's primary button is green
 * and its links are blue, so --action-primary is green and --brand-text blue.
 * index.css's split between the two exists for this.
 */
const GITHUB: ThemeFamilyDefinition = {
  id: "github",
  label: "GitHub",
  dark: {
    "--tint-veil": "#ffffff",
    "--tint-border": "#c9d1d9", // at 16% over the canvas this lands on #30363d, GitHub's border
    "--background-app": "#010409", // canvas.inset
    "--background-canvas": "#0d1117", // canvas.default
    "--surface-base": "#0d1117",
    "--surface-raised": "#161b22", // canvas.subtle
    "--surface-tinted": "#21262d",
    "--text-primary": "#e6edf3", // fg.default
    "--text-secondary": "#9198a1", // fg.muted
    "--text-tertiary": "#6e7681", // fg.subtle
    "--icon-secondary": "#9198a1",
    "--action-primary": "#238636", // the Primary button
    "--action-primary-foreground": "#ffffff",
    ...states("#238636", "dark"),
    "--brand-text": "#2f81f7", // accent.fg — links
    "--status-info": "#2f81f7",
    "--status-success": "#3fb950",
    "--status-warning": "#d29922",
    "--status-recovery": "#a371f7",
    "--status-danger": "#f85149",
    "--border-focus": "#2f81f7",
  },
  light: {
    "--tint-veil": "#1f2328",
    "--tint-border": "#1f2328",
    "--background-app": "#eaeef2",
    "--background-canvas": "#f6f8fa", // canvas.subtle
    "--surface-base": "#ffffff", // canvas.default
    "--surface-raised": "#f6f8fa",
    "--surface-tinted": "#eaeef2",
    "--text-primary": "#1f2328", // fg.default
    "--text-secondary": "#59636e", // fg.muted
    "--text-tertiary": "#6e7781", // fg.subtle
    "--icon-secondary": "#59636e",
    "--action-primary": "#1f883d", // the Primary button
    "--action-primary-foreground": "#ffffff",
    ...states("#1f883d", "light"),
    "--brand-text": "#0969da", // accent.fg — links
    "--status-info": "#0969da",
    "--status-success": "#1a7f37",
    "--status-warning": "#9a6700",
    "--status-recovery": "#8250df",
    "--status-danger": "#d1242f",
    "--border-focus": "#0969da",
  },
};

/**
 * Every family the app can paint with.
 *
 * Note-folder colours are deliberately absent from all four. They are nine
 * user-chosen folder tags rather than chrome, index.css already tunes them per
 * mode, and no family here publishes nine distinct hues — Dracula has seven —
 * so mapping them would mean inventing the rest. If they read as foreign
 * inside a family later, adding them is a small change of its own.
 */
export const FAMILIES: Record<ThemeFamilyPreference, ThemeFamilyDefinition> = {
  devkit: DEVKIT_FAMILY,
  catppuccin: CATPPUCCIN,
  dracula: DRACULA,
  ayu: AYU,
  github: GITHUB,
};

/** The family to paint with, falling back to the base for an unknown id. */
export function familyOf(id: ThemeFamilyPreference): ThemeFamilyDefinition {
  return FAMILIES[id] ?? DEVKIT_FAMILY;
}
