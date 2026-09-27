import type { ThemeFamilyPreference } from "@/bridge/preferences";
import type { MixToken, SyntaxToken, ThemeToken, TintToken } from "./tokens";

/**
 * One palette, in one mode: the tokens it overrides and nothing else.
 *
 * Partial on purpose. A family says only what differs from index.css, so it
 * does not have to restate a value it agrees with, and a token added to the
 * base later reaches every family for free.
 */
export type ThemePalette = Partial<Record<TintToken | ThemeToken | MixToken | SyntaxToken, string>>;

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

/** Nord — the arctic blue-grey palette, adapted for both UI modes. */
const NORD: ThemeFamilyDefinition = {
  id: "nord",
  label: "Nord",
  dark: {
    "--tint-veil": "#ffffff",
    "--tint-border": "#8f9baa",
    "--background-app": "#242933",
    "--background-canvas": "#2e3440",
    "--surface-base": "#333a48", // was #353c4a: a shade darker, so the well over it keeps Claude's mark at 3:1
    "--surface-raised": "#3b4252",
    "--surface-tinted": "#434c5e",
    "--text-primary": "#eceff4",
    "--text-secondary": "#d8dee9",
    "--text-tertiary": "#b3bdcd",
    "--icon-secondary": "#d8dee9",
    "--action-primary": "#88c0d0",
    "--action-primary-foreground": "#242933",
    ...states("#88c0d0", "dark"),
    "--brand-text": "#8fcbdb",
    "--status-info": "#9db6cf", // was #81a1c1, lifted in its own hue to clear themeContrast.test.ts
    "--status-success": "#a3be8c",
    "--status-warning": "#ebcb8b",
    "--status-recovery": "#c7aac2", // was #b48ead, lifted in its own hue to clear themeContrast.test.ts
    "--status-danger": "#dba5aa", // was #bf616a, lifted in its own hue to clear themeContrast.test.ts
    "--border-focus": "#88c0d0",
  },
  light: {
    "--tint-veil": "#2e3440",
    "--tint-border": "#737f91",
    "--background-app": "#d8dee9",
    "--background-canvas": "#e5e9f0",
    "--surface-base": "#eceff4",
    "--surface-raised": "#e5e9f0",
    "--surface-tinted": "#d8dee9",
    "--text-primary": "#29313d",
    "--text-secondary": "#3d4655",
    "--text-tertiary": "#505c6e",
    "--icon-secondary": "#3d4655",
    "--action-primary": "#4b7898",
    "--action-primary-foreground": "#ffffff",
    ...states("#4b7898", "light"),
    "--brand-text": "#3d6683", // was #426f8e, deepened in its own hue to clear themeContrast.test.ts
    "--status-info": "#34668b", // was #35698f, deepened in its own hue to clear themeContrast.test.ts
    "--status-success": "#436c35", // was #477238, deepened in its own hue to clear themeContrast.test.ts
    "--status-warning": "#805600",
    "--status-recovery": "#795776", // was #815d7e, deepened in its own hue to clear themeContrast.test.ts
    "--status-danger": "#a33f4a",
    "--border-focus": "#35698f",
  },
};

/** Solarized — blue-green dark and parchment light, with a shared golden accent. */
const SOLARIZED: ThemeFamilyDefinition = {
  id: "solarized",
  label: "Solarized",
  dark: {
    "--tint-veil": "#fdf6e3",
    "--tint-border": "#839496",
    "--background-app": "#00252e",
    "--background-canvas": "#073642",
    "--surface-base": "#0b414d",
    "--surface-raised": "#174b56",
    "--surface-tinted": "#245964",
    "--text-primary": "#fdf6e3",
    "--text-secondary": "#eee8d5",
    "--text-tertiary": "#b8c5c6",
    "--icon-secondary": "#eee8d5",
    "--action-primary": "#b58900",
    "--action-primary-foreground": "#00252e",
    ...states("#b58900", "dark"),
    "--brand-text": "#f2c75c",
    "--status-info": "#72c5dc",
    "--status-success": "#91d59a",
    "--status-warning": "#f0ca63",
    "--status-recovery": "#d6a6d8",
    "--status-danger": "#f49c92", // was #f0786b, lifted in its own hue to clear themeContrast.test.ts
    "--border-focus": "#72c5dc",
  },
  light: {
    "--tint-veil": "#073642",
    "--tint-border": "#839496",
    "--background-app": "#eee8d5",
    "--background-canvas": "#f5efdc",
    "--surface-base": "#fdf6e3",
    "--surface-raised": "#f5efdc",
    "--surface-tinted": "#eee8d5",
    "--text-primary": "#173c46",
    "--text-secondary": "#31545b",
    "--text-tertiary": "#49666b",
    "--icon-secondary": "#31545b",
    "--action-primary": "#006b8f",
    "--action-primary-foreground": "#ffffff",
    ...states("#006b8f", "light"),
    "--brand-text": "#006b8f",
    "--status-info": "#006b8f",
    "--status-success": "#287544",
    "--status-warning": "#805700",
    "--status-recovery": "#8b477d",
    "--status-danger": "#b33b32",
    "--border-focus": "#006b8f",
  },
};

/** Gruvbox — warm paper and dark charcoal with muted retro accents. */
const GRUVBOX: ThemeFamilyDefinition = {
  id: "gruvbox",
  label: "Gruvbox",
  dark: {
    "--tint-veil": "#fbf1c7",
    "--tint-border": "#a89984",
    "--background-app": "#1d2021",
    "--background-canvas": "#282828",
    "--surface-base": "#32302f",
    "--surface-raised": "#3c3836",
    "--surface-tinted": "#504945",
    "--text-primary": "#fbf1c7",
    "--text-secondary": "#ebdbb2",
    "--text-tertiary": "#c5b89f",
    "--icon-secondary": "#ebdbb2",
    "--action-primary": "#b8bb26",
    "--action-primary-foreground": "#1d2021",
    ...states("#b8bb26", "dark"),
    "--brand-text": "#b8bb26",
    "--status-info": "#8eada1", // was #83a598, lifted in its own hue to clear themeContrast.test.ts
    "--status-success": "#b8bb26",
    "--status-warning": "#fabd2f",
    "--status-recovery": "#d893a6", // was #d3869b, lifted in its own hue to clear themeContrast.test.ts
    "--status-danger": "#fc8375", // was #fb4934, lifted in its own hue to clear themeContrast.test.ts
    "--border-focus": "#83a598",
  },
  light: {
    "--tint-veil": "#3c3836",
    "--tint-border": "#928374",
    "--background-app": "#ebdbb2",
    "--background-canvas": "#f2e5bc",
    "--surface-base": "#fbf1c7",
    "--surface-raised": "#f2e5bc",
    "--surface-tinted": "#ebdbb2",
    "--text-primary": "#282828",
    "--text-secondary": "#3c3836",
    "--text-tertiary": "#504945",
    "--icon-secondary": "#3c3836",
    "--action-primary": "#5b7500",
    "--action-primary-foreground": "#ffffff",
    ...states("#5b7500", "light"),
    "--brand-text": "#526900", // was #5b7500, deepened in its own hue to clear themeContrast.test.ts
    "--status-info": "#176983", // was #176b85, deepened in its own hue to clear themeContrast.test.ts
    "--status-success": "#506900", // was #557000, deepened in its own hue to clear themeContrast.test.ts
    "--status-warning": "#805600",
    "--status-recovery": "#8b435c",
    "--status-danger": "#b52b20",
    "--border-focus": "#176b85",
  },
};

/** Tokyo Night — crisp indigo surfaces and vivid editor-inspired accents. */
const TOKYO_NIGHT: ThemeFamilyDefinition = {
  id: "tokyonight",
  label: "Tokyo Night",
  dark: {
    "--tint-veil": "#c0caf5",
    "--tint-border": "#565f89",
    "--background-app": "#16161e",
    "--background-canvas": "#1a1b26",
    "--surface-base": "#20212e",
    "--surface-raised": "#292e42",
    "--surface-tinted": "#343b58",
    "--text-primary": "#c0caf5",
    "--text-secondary": "#b4c0e0",
    "--text-tertiary": "#9aa5ce",
    "--icon-secondary": "#b4c0e0",
    "--action-primary": "#7aa2f7",
    "--action-primary-foreground": "#16161e",
    ...states("#7aa2f7", "dark"),
    "--brand-text": "#89b4fa",
    "--status-info": "#7dcfff",
    "--status-success": "#9ece6a",
    "--status-warning": "#e0af68",
    "--status-recovery": "#bb9af7",
    "--status-danger": "#f7768e",
    "--border-focus": "#7aa2f7",
  },
  light: {
    "--tint-veil": "#343b58",
    "--tint-border": "#737aa2",
    "--background-app": "#c8cde0",
    "--background-canvas": "#d5d9e8",
    "--surface-base": "#e1e2e9",
    "--surface-raised": "#d5d9e8",
    "--surface-tinted": "#c8cde0",
    "--text-primary": "#292e42",
    "--text-secondary": "#3b4261",
    "--text-tertiary": "#4c5577",
    "--icon-secondary": "#3b4261",
    "--action-primary": "#34548a",
    "--action-primary-foreground": "#ffffff",
    ...states("#34548a", "light"),
    "--brand-text": "#34548a",
    "--status-info": "#145d87", // was #176b9b, deepened in its own hue to clear themeContrast.test.ts
    "--status-success": "#43601d", // was #4d6f21, deepened in its own hue to clear themeContrast.test.ts
    "--status-warning": "#785000", // was #805600, deepened in its own hue to clear themeContrast.test.ts
    "--status-recovery": "#6e429f", // was #7144a3, deepened in its own hue to clear themeContrast.test.ts
    "--status-danger": "#9f2c48", // was #ad304e, deepened in its own hue to clear themeContrast.test.ts
    "--border-focus": "#34548a",
  },
};

/** Everforest — soft forest greens with a warm, low-glare paper mode. */
const EVERFOREST: ThemeFamilyDefinition = {
  id: "everforest",
  label: "Everforest",
  dark: {
    "--tint-veil": "#d3c6aa",
    "--tint-border": "#859289",
    "--background-app": "#272e33",
    "--background-canvas": "#2d353b",
    "--surface-base": "#343f44",
    "--surface-raised": "#3d484d",
    "--surface-tinted": "#475258",
    "--text-primary": "#e5decd", // was #d3c6aa, lifted in its own hue to clear themeContrast.test.ts
    "--text-secondary": "#cdc6b0", // was #c5bda4, lifted in its own hue to clear themeContrast.test.ts
    "--text-tertiary": "#aeb6a7", // was #a7b0a0, lifted in its own hue to clear themeContrast.test.ts
    "--icon-secondary": "#c5bda4",
    "--action-primary": "#a7c080",
    "--action-primary-foreground": "#272e33",
    ...states("#a7c080", "dark"),
    "--brand-text": "#a7c080",
    "--status-info": "#85beb7", // was #7fbbb3, lifted in its own hue to clear themeContrast.test.ts
    "--status-success": "#a7c080",
    "--status-warning": "#dbbc7f",
    "--status-recovery": "#dba4be", // was #d699b6, lifted in its own hue to clear themeContrast.test.ts
    "--status-danger": "#eda0a1", // was #e67e80, lifted in its own hue to clear themeContrast.test.ts
    "--border-focus": "#7fbbb3",
  },
  light: {
    "--tint-veil": "#414b50",
    "--tint-border": "#899287",
    "--background-app": "#e6dfc8",
    "--background-canvas": "#f0ead6",
    "--surface-base": "#f8f3e8",
    "--surface-raised": "#f0ead6",
    "--surface-tinted": "#e6dfc8",
    "--text-primary": "#343f44",
    "--text-secondary": "#46534b",
    "--text-tertiary": "#59665b",
    "--icon-secondary": "#46534b",
    "--action-primary": "#55734b",
    "--action-primary-foreground": "#ffffff",
    ...states("#55734b", "light"),
    "--brand-text": "#4f6a45", // was #55734b, deepened in its own hue to clear themeContrast.test.ts
    "--status-info": "#2c6d69", // was #327c78, deepened in its own hue to clear themeContrast.test.ts
    "--status-success": "#4e6c1d", // was #54751f, deepened in its own hue to clear themeContrast.test.ts
    "--status-warning": "#805700",
    "--status-recovery": "#8d4f70",
    "--status-danger": "#ac3c3b", // was #b33f3d, deepened in its own hue to clear themeContrast.test.ts
    "--border-focus": "#327c78",
  },
};

/**
 * Xcode — macOS chrome around Xcode's own "Default" editor palette.
 *
 * Two sources, because Xcode itself is two things. The chrome is macOS: the
 * system grays for grounds and text, and the system colours for accent and
 * status (blue, green, orange, red, purple). The code is Xcode's "Default
 * (Dark)" / "Default (Light)" theme, Xcode 12 onwards — pink keywords, red
 * strings, purple system types — read out of the .xccolortheme files.
 *
 * Where a published value misses the floor in themeContrast.test.ts, Apple's
 * own Increased Contrast variant is used (dark: blue #409cff, red #ff6961,
 * purple #da8fff; light: red #d70015, purple #8944ab), and only when that
 * misses too is the colour taken darker or lighter in its own hue, exactly as
 * far as the floor needs:
 * - light: blue #007aff → #0067d7, green #248a3d → #207b36, orange #c93400 → #c83400
 * - dark code: function #a167e6 → #b282eb (Xcode 11's own "other function"),
 *   declaration #41a1c0 → #47a4c2, attribute #bf8555 → #c38d60, and comment
 *   #6c7986 → #909aa4 — the one real move: Xcode's comment is 2.9:1 on these
 *   grounds, fine for a gutter hint, not for text people read.
 * Every light code colour is Xcode's exactly.
 *
 * The filled button is #0071e3, Apple's own button blue: system blue under
 * white text is 3.7:1 (dark) and 4.0:1 (light), and white is what an Apple
 * button says on.
 */
const XCODE: ThemeFamilyDefinition = {
  id: "xcode",
  label: "Xcode",
  dark: {
    "--tint-veil": "#f5f5f7",
    "--tint-border": "#77777f",
    "--background-app": "#171719",
    "--background-canvas": "#1c1c1e",
    "--surface-base": "#242426",
    "--surface-raised": "#2d2d30",
    "--surface-tinted": "#39393d",
    "--text-primary": "#f5f5f7",
    "--text-secondary": "#d1d1d6",
    "--text-tertiary": "#a1a1a8",
    "--icon-secondary": "#d1d1d6",
    "--action-primary": "#0071e3",
    "--action-primary-foreground": "#ffffff",
    ...states("#0071e3", "dark"),
    "--brand-text": "#409cff",
    "--status-info": "#409cff",
    "--status-success": "#30d158",
    "--status-warning": "#ff9f0a",
    "--status-recovery": "#da8fff",
    "--status-danger": "#ff6961",
    "--border-focus": "#409cff",
    "--syntax-keyword": "#fc5fa3",
    "--syntax-string": "#fc6a5d",
    "--syntax-number": "#d0bf69",
    "--syntax-type": "#d0a8ff",
    "--syntax-function": "#b282eb",
    "--syntax-definition": "#47a4c2",
    "--syntax-comment": "#909aa4",
    "--syntax-meta": "#c38d60",
  },
  light: {
    "--tint-veil": "#1c1c1e",
    "--tint-border": "#8e8e93",
    // macOS's window background; systemGray5 (#e5e5ea) sat darker than the
    // window Xcode actually draws, and cost every status colour contrast.
    "--background-app": "#ececec",
    "--background-canvas": "#f2f2f7",
    "--surface-base": "#ffffff",
    "--surface-raised": "#f2f2f7",
    "--surface-tinted": "#e5e5ea",
    "--text-primary": "#1c1c1e",
    "--text-secondary": "#3a3a3c",
    "--text-tertiary": "#545458",
    "--icon-secondary": "#3a3a3c",
    "--action-primary": "#0071e3",
    "--action-primary-foreground": "#ffffff",
    ...states("#0071e3", "light"),
    "--brand-text": "#0067d7",
    "--status-info": "#0067d7",
    "--status-success": "#207b36",
    "--status-warning": "#c83400",
    "--status-recovery": "#8944ab",
    "--status-danger": "#d70015",
    "--border-focus": "#0067d7",
    "--syntax-keyword": "#9b2393",
    "--syntax-string": "#c41a16",
    "--syntax-number": "#1c00cf",
    "--syntax-type": "#3900a0",
    "--syntax-function": "#6c36a9",
    "--syntax-definition": "#0f68a0",
    "--syntax-comment": "#5d6c79",
    "--syntax-meta": "#815f03",
  },
};

/**
 * Every family the app can paint with.
 *
 * Note-folder colours are deliberately absent from theme families. They are nine
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
  nord: NORD,
  solarized: SOLARIZED,
  gruvbox: GRUVBOX,
  tokyonight: TOKYO_NIGHT,
  everforest: EVERFOREST,
  xcode: XCODE,
};

/** The family to paint with, falling back to the base for an unknown id. */
export function familyOf(id: ThemeFamilyPreference): ThemeFamilyDefinition {
  return FAMILIES[id] ?? DEVKIT_FAMILY;
}
