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
 * The base palette, spelled out.
 *
 * It was two empty maps until the Settings picker needed it: a swatch renders
 * a family by setting that family's tokens inline on itself, and "no tokens"
 * cannot mean "the values in index.css" for an element the `:root` rule does
 * not match — an empty devkit swatch would have painted itself in whichever
 * family was active. So the base is data like every other family, which also
 * deletes the branch that used to treat it as the absence of one.
 *
 * These ARE a copy of index.css, and a copy is a second place to change a
 * colour. tokenContract.test.ts pins every value against the stylesheet, so
 * the copy cannot drift — that is the price of paying it.
 */
export const DEVKIT_FAMILY: ThemeFamilyDefinition = {
  id: "devkit",
  label: "DevKit",
  dark: {
    "--tint-veil": "#ffffff",
    "--tint-border": "#b2d5e5",
    "--background-app": "#020202",
    "--background-canvas": "#171717",
    "--surface-base": "#1d1d1d",
    "--surface-raised": "#23212c",
    "--surface-tinted": "#32292f",
    "--text-primary": "#f0f1eb",
    "--text-secondary": "#c6c8c2",
    "--text-tertiary": "#90938f",
    "--icon-secondary": "#c6c8c2",
    "--action-primary": "#c6ff34",
    "--action-primary-foreground": "#020202",
    "--action-primary-hover": "#d2ff64",
    "--action-primary-pressed": "#add92e",
    "--brand-text": "#c6ff34",
    "--status-info": "#b2d5e5",
    "--status-success": "#99e1d9",
    "--status-warning": "#f1fec8",
    "--status-recovery": "#e5bddf",
    "--status-danger": "#d87886",
    "--border-focus": "#b2d5e5",
  },
  light: {
    "--tint-veil": "#1d1d1f",
    "--tint-border": "#1d1d1f",
    "--background-app": "#e9eaee",
    "--background-canvas": "#f4f5f7",
    "--surface-base": "#ffffff",
    "--surface-raised": "#f4f5f7",
    "--surface-tinted": "#e8e9ec",
    "--text-primary": "#1d1d1f",
    "--text-secondary": "#55565b",
    "--text-tertiary": "#68696f",
    "--icon-secondary": "#55565b",
    // Light keeps the lime fill — onyx on lime passes in both modes — and
    // flips only the accent used as TEXT, which index.css says in a comment.
    "--action-primary": "#c6ff34",
    "--action-primary-foreground": "#020202",
    "--action-primary-hover": "#d2ff64",
    "--action-primary-pressed": "#add92e",
    "--brand-text": "#567300",
    "--status-info": "#0a719a",
    "--status-success": "#1d766b",
    "--status-warning": "#915f00",
    "--status-recovery": "#9b4e8e",
    "--status-danger": "#b3374a",
    "--border-focus": "#4b8cb8",
  },
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
    "--text-tertiary": "#969aae", // overlay1
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
    "--text-primary": "#43455c", // text
    "--text-secondary": "#53556b", // subtext1
    "--text-tertiary": "#5f6274", // overlay1, 4% deeper — see the contrast note below
    "--icon-secondary": "#606276", // subtext0
    "--action-primary": "#8839ef", // mauve
    "--action-primary-foreground": "#eff1f5",
    ...states("#8839ef", "light"),
    "--brand-text": "#802cee",
    "--status-info": "#0a55ea", // blue
    "--status-success": "#2d711e", // green
    "--status-warning": "#8b5812", // yellow
    "--status-recovery": "#a43988", // pink
    "--status-danger": "#c60e36", // red
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
    "--text-tertiary": "#95a0c1", // comment
    "--icon-secondary": "#adb5cb",
    "--action-primary": "#bd93f9", // purple
    "--action-primary-foreground": "#282a36",
    ...states("#bd93f9", "dark"),
    "--brand-text": "#bd93f9",
    "--status-info": "#8be9fd", // cyan
    "--status-success": "#50fa7b", // green
    "--status-warning": "#f1fa8c", // yellow
    "--status-recovery": "#ff79c6", // pink
    "--status-danger": "#ff7575", // red
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
    "--status-warning": "#796413", // Alucard yellow
    "--status-recovery": "#a3144d", // Alucard pink
    "--status-danger": "#bb3527", // Alucard red
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
    "--text-tertiary": "#828894", // ui.fg, lifted 5% to clear 3:1 on the panel
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
    "--text-primary": "#484c50", // common.fg
    "--text-secondary": "#575c61",
    "--text-tertiary": "#636971", // ui.fg
    "--icon-secondary": "#63696f",
    "--action-primary": "#ffaa33", // common.accent
    "--action-primary-foreground": "#1c1f23",
    ...states("#ffaa33", "light"),
    "--brand-text": "#9d5700", // the accent is a fill; as text it needs depth
    "--status-info": "#1e6da5", // entity
    "--status-success": "#527200", // string, deepened for white
    "--status-warning": "#a35200", // func, deepened for white
    "--status-recovery": "#7d4ad2", // constant
    "--status-danger": "#cd1d1d", // error
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
    "--text-tertiary": "#7f8792", // fg.subtle
    "--icon-secondary": "#9198a1",
    "--action-primary": "#238636", // the Primary button
    "--action-primary-foreground": "#ffffff",
    ...states("#238636", "dark"),
    "--brand-text": "#3283f7", // accent.fg — links
    "--status-info": "#3283f7",
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
    "--text-secondary": "#565f6a", // fg.muted
    "--text-tertiary": "#646d76", // fg.subtle
    "--icon-secondary": "#59636e",
    "--action-primary": "#1f883d", // the Primary button
    "--action-primary-foreground": "#ffffff",
    ...states("#1f883d", "light"),
    "--brand-text": "#0968d9", // accent.fg — links
    "--status-info": "#0968d9",
    "--status-success": "#197c36",
    "--status-warning": "#936200",
    "--status-recovery": "#7f4cde",
    "--status-danger": "#d0242f",
    "--border-focus": "#0969da",
  },
};

/**
 * Rosé Pine — the main variant for dark, Dawn for light.
 *
 * The palette only names three surface stops (base, surface, overlay) against
 * the five this contract wants, so the gap is filled from its own
 * `highlight{Low,Med,High}` tokens — published for exactly this, a table row
 * or a selected line sitting a shade off the base. `--surface-base` doubling
 * as `--background-canvas` in dark mirrors what GitHub's own `canvas.default`
 * already does twice in this file.
 *
 * Every one of the six accent hues (love, gold, rose, pine, foam, iris) is
 * used exactly once, so the family spends the whole palette rather than
 * leaving half of it on the table.
 */
const ROSE_PINE: ThemeFamilyDefinition = {
  id: "rosepine",
  label: "Rosé Pine",
  dark: {
    "--tint-veil": "#ffffff",
    "--tint-border": "#6e6a86", // muted
    "--background-app": "#191724", // base
    "--background-canvas": "#1f1d2e", // surface
    "--surface-base": "#1f1d2e", // surface
    "--surface-raised": "#26233a", // overlay
    "--surface-tinted": "#403d52", // highlightMed
    "--text-primary": "#e0def4", // text
    "--text-secondary": "#c3c1dd", // subtle, lifted to clear 5.5:1 on the lightest surface
    "--text-tertiary": "#9490b3", // muted, lifted to clear 4.5:1
    "--icon-secondary": "#c3c1dd",
    "--action-primary": "#c4a7e7", // iris
    "--action-primary-foreground": "#191724",
    ...states("#c4a7e7", "dark"),
    "--brand-text": "#c4a7e7",
    "--status-info": "#9ccfd8", // foam
    "--status-success": "#6bb9c9", // pine, lightened off its editor value to clear 4.5:1 on --surface-tinted
    "--status-warning": "#f6c177", // gold
    "--status-recovery": "#ebbcba", // rose
    "--status-danger": "#eb6f92", // love
    "--border-focus": "#c4a7e7",
  },
  light: {
    "--tint-veil": "#575279",
    "--tint-border": "#9893a5", // muted
    "--background-app": "#f2e9e1", // overlay
    "--background-canvas": "#faf4ed", // base
    "--surface-base": "#fffaf3", // surface
    "--surface-raised": "#f4ede8", // highlightLow
    "--surface-tinted": "#dfdad9", // highlightMed
    "--text-primary": "#4d496c", // text, deepened to clear 7:1
    "--text-secondary": "#5c587c", // subtle, deepened to clear 5.5:1
    "--text-tertiary": "#6c6681", // muted, deepened to clear 4.5:1
    "--icon-secondary": "#5c587c",
    "--action-primary": "#907aa9", // iris
    "--action-primary-foreground": "#fffaf3",
    ...states("#907aa9", "light"),
    "--brand-text": "#796090", // iris, deepened for text use — the fill can stay lighter, text cannot
    "--status-info": "#286a83", // foam, deepened for white — the published value is a fill, not text
    "--status-success": "#286983", // pine
    "--status-warning": "#9e5909", // gold, deepened for white
    "--status-recovery": "#a24f69", // rose, deepened for white
    "--status-danger": "#995468", // love, deepened for white
    "--border-focus": "#907aa9",
  },
};

/**
 * Lotus, paired with Wave — Kanagawa's own dark counterpart.
 *
 * Kanagawa ships three variants and Lotus is only ever the light one, so it
 * takes a sibling the same way Alucard needs Dracula: Wave is the classic,
 * best-known Kanagawa dark, kept here under the Lotus label so the family
 * still guarantees both modes.
 *
 * The signature colour is oniViolet / lotusViolet4 — Kanagawa's own accent
 * for keywords — carried to the UI accent the way Dracula's purple was.
 */
const LOTUS: ThemeFamilyDefinition = {
  id: "lotus",
  label: "Lotus",
  dark: {
    "--tint-veil": "#ffffff",
    "--tint-border": "#54546d", // sumiInk6
    "--background-app": "#16161d", // sumiInk0
    "--background-canvas": "#181820", // sumiInk1
    "--surface-base": "#1f1f28", // sumiInk3
    "--surface-raised": "#2a2a37", // sumiInk4
    "--surface-tinted": "#363646", // sumiInk5
    "--text-primary": "#dcd7ba", // fujiWhite
    "--text-secondary": "#c8c093", // oldWhite
    "--text-tertiary": "#9c9b93", // fujiGray, lifted to clear 4.5:1
    "--icon-secondary": "#c8c093",
    "--action-primary": "#957fb8", // oniViolet
    "--action-primary-foreground": "#16161d",
    ...states("#957fb8", "dark"),
    "--brand-text": "#9f8bbe", // oniViolet, lightened for text use — the fill can stay darker, text cannot
    "--status-info": "#7e9cd8", // crystalBlue
    "--status-success": "#98bb6c", // springGreen
    "--status-warning": "#ff9e3b", // roninYellow
    "--status-recovery": "#d27e99", // sakuraPink
    "--status-danger": "#ef6868", // samuraiRed, lightened off its editor value to clear 4.5:1
    "--border-focus": "#7e9cd8",
  },
  light: {
    "--tint-veil": "#545464",
    "--tint-border": "#716e61", // lotusGray2
    "--background-app": "#d5cea3", // lotusWhite0
    "--background-canvas": "#dcd5ac", // lotusWhite1
    "--surface-base": "#f2ecbc", // lotusWhite3
    "--surface-raised": "#e7dba0", // lotusWhite4
    "--surface-tinted": "#e4d794", // lotusWhite5
    "--text-primary": "#3a3a45", // lotusInk1, deepened to clear 7:1
    "--text-secondary": "#4f4a3e", // lotusGray2, deepened to clear 5.5:1
    "--text-tertiary": "#5b5743", // lotusGray3, deepened to clear 4.5:1
    "--icon-secondary": "#4f4a3e",
    "--action-primary": "#624c83", // lotusViolet4
    "--action-primary-foreground": "#f2ecbc",
    ...states("#624c83", "light"),
    "--brand-text": "#624c83",
    "--status-info": "#3c5b68", // lotusTeal3, deepened for white
    "--status-success": "#4a5d34", // lotusGreen, deepened for white
    "--status-warning": "#7e4c00", // lotusOrange2, deepened for white
    "--status-recovery": "#85415a", // lotusPink, deepened for white
    "--status-danger": "#a82227", // lotusRed3, deepened for white
    "--border-focus": "#3c5b68",
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
  rosepine: ROSE_PINE,
  lotus: LOTUS,
};

/** The family to paint with, falling back to the base for an unknown id. */
export function familyOf(id: ThemeFamilyPreference): ThemeFamilyDefinition {
  return FAMILIES[id] ?? DEVKIT_FAMILY;
}
