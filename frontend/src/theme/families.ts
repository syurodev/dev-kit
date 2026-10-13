import type { ThemeFamilyPreference } from "@/bridge/preferences";
import { HUE_TOKENS } from "./tokens";
import type { HueToken, PaletteToken, SyntaxStyleToken, SyntaxToken } from "./tokens";

/**
 * One Color palette, in one mode.
 *
 * Eight hues are required; syntax colours are optional (a family with an
 * editor palette of its own — Xcode — names them; the rest get them from
 * their hues). A palette never names a chrome token: the chrome is Mono and
 * index.css is its only author.
 *
 * Every hue is the family's own colour moved in LIGHTNESS only until it
 * clears 4.5:1 on every Mono ground (themeContrast.test.ts). Where a family
 * publishes fewer than eight accents, its comment says what stands in.
 */
export type ThemePalette = Record<HueToken, string> &
  Partial<Record<SyntaxToken | SyntaxStyleToken, string>>;

/** Every palette token, resolved: what applyThemeFamily writes. */
export type ResolvedPalette = Record<PaletteToken, string>;

/** A family supplies BOTH modes, so "family × mode" is a total function. */
export type ThemeFamilyDefinition = {
  id: ThemeFamilyPreference;
  label: string;
  dark: ThemePalette;
  light: ThemePalette;
};

/** Mono code: structure by weight and the text ramp. Mirrors index.css. */
const MONO_SYNTAX = {
  "--syntax-keyword": "var(--text-primary)",
  "--syntax-keyword-weight": "600",
  "--syntax-string": "var(--text-secondary)",
  "--syntax-number": "var(--text-secondary)",
  "--syntax-type": "var(--text-primary)",
  "--syntax-function": "var(--text-primary)",
  "--syntax-definition": "var(--text-primary)",
  "--syntax-comment": "var(--text-tertiary)",
  "--syntax-meta": "var(--text-tertiary)",
} as const;

/** Which hue a syntax role takes when the family names no syntax colours. */
const SYNTAX_FROM_HUE: Record<SyntaxToken, HueToken | `var(--${string})`> = {
  "--syntax-keyword": "--hue-violet",
  "--syntax-string": "--hue-green",
  "--syntax-number": "--hue-orange",
  "--syntax-type": "--hue-yellow",
  "--syntax-function": "--hue-blue",
  "--syntax-definition": "var(--text-primary)",
  "--syntax-comment": "var(--text-tertiary)",
  "--syntax-meta": "var(--text-tertiary)",
};

/**
 * Hues to the full palette.
 *
 * Written out rather than left to `var(--hue-x)` in the stylesheet: the
 * Settings swatch sets a palette inline on one element, and a `var()`
 * declared on :root has already resolved by the time it is inherited there.
 * Literal values work on any element.
 */
export function resolvePalette(palette: ThemePalette): ResolvedPalette {
  const hue = (source: string) =>
    (HUE_TOKENS as readonly string[]).includes(source) ? palette[source as HueToken] : source;
  const syntax = Object.fromEntries(
    Object.entries(SYNTAX_FROM_HUE).map(([token, source]) => [
      token,
      palette[token as SyntaxToken] ?? hue(source),
    ]),
  ) as Record<SyntaxToken, string>;
  return {
    "--hue-red": palette["--hue-red"],
    "--hue-orange": palette["--hue-orange"],
    "--hue-yellow": palette["--hue-yellow"],
    "--hue-green": palette["--hue-green"],
    "--hue-teal": palette["--hue-teal"],
    "--hue-blue": palette["--hue-blue"],
    "--hue-violet": palette["--hue-violet"],
    "--hue-pink": palette["--hue-pink"],
    ...syntax,
    "--syntax-keyword-weight": palette["--syntax-keyword-weight"] ?? "400",
    "--ansi-red": palette["--hue-red"],
    "--ansi-green": palette["--hue-green"],
    "--ansi-yellow": palette["--hue-yellow"],
    "--ansi-blue": palette["--hue-blue"],
    "--ansi-magenta": palette["--hue-violet"],
    "--ansi-cyan": palette["--hue-teal"],
  };
}

/** Mono: muted hues at the status ramp's saturation, and code in the text ramp. */
export const DEVKIT_FAMILY: ThemeFamilyDefinition = {
  id: "devkit",
  label: "DevKit",
  dark: {
    "--hue-red": "#e07b7f",
    "--hue-orange": "#e0945e",
    "--hue-yellow": "#d9b25c",
    "--hue-green": "#7cc59a",
    "--hue-teal": "#6cc1bb",
    "--hue-blue": "#8aaee6",
    "--hue-violet": "#b29ce6",
    "--hue-pink": "#de8fb8",
    ...MONO_SYNTAX,
  },
  light: {
    "--hue-red": "#b3363f",
    "--hue-orange": "#a14f1a",
    "--hue-yellow": "#8a6100",
    "--hue-green": "#2b7350",
    "--hue-teal": "#1f7470",
    "--hue-blue": "#3a62a8",
    "--hue-violet": "#6e55a8",
    "--hue-pink": "#a4467a",
    ...MONO_SYNTAX,
  },
};

/** Mocha and Latte accents (peach is orange, mauve violet). */
const CATPPUCCIN: ThemeFamilyDefinition = {
  id: "catppuccin",
  label: "Catppuccin",
  dark: {
    "--hue-red": "#f38ba8",
    "--hue-orange": "#fab387",
    "--hue-yellow": "#f9e2af",
    "--hue-green": "#a6e3a1",
    "--hue-teal": "#94e2d5",
    "--hue-blue": "#89b4fa",
    "--hue-violet": "#cba6f7",
    "--hue-pink": "#f5c2e7",
  },
  light: {
    "--hue-red": "#d20f39",
    "--hue-orange": "#b94401",
    "--hue-yellow": "#955f13",
    "--hue-green": "#307820",
    "--hue-teal": "#12747a",
    "--hue-blue": "#125ef4",
    "--hue-violet": "#8839ef",
    "--hue-pink": "#c01d95",
  },
};

/** Dracula and Alucard. Dracula has no blue; its comment colour, lifted, stands in. */
const DRACULA: ThemeFamilyDefinition = {
  id: "dracula",
  label: "Dracula",
  dark: {
    "--hue-red": "#ff5555",
    "--hue-orange": "#ffb86c",
    "--hue-yellow": "#f1fa8c",
    "--hue-green": "#50fa7b",
    "--hue-teal": "#8be9fd",
    "--hue-blue": "#7c89b3",
    "--hue-violet": "#bd93f9",
    "--hue-pink": "#ff79c6",
  },
  light: {
    "--hue-red": "#c33828",
    "--hue-orange": "#a34d14",
    "--hue-yellow": "#7d6914",
    "--hue-green": "#14710a",
    "--hue-teal": "#036a96",
    "--hue-blue": "#3a4a8a",
    "--hue-violet": "#644ac9",
    "--hue-pink": "#a3144d",
  },
};

/** Ayu Dark and Ayu Light syntax colours. Ayu has no pink; a magenta stands in. */
const AYU: ThemeFamilyDefinition = {
  id: "ayu",
  label: "Ayu",
  dark: {
    "--hue-red": "#f07178",
    "--hue-orange": "#ff8f40",
    "--hue-yellow": "#e6b450",
    "--hue-green": "#aad94c",
    "--hue-teal": "#95e6cb",
    "--hue-blue": "#59c2ff",
    "--hue-violet": "#d2a6ff",
    "--hue-pink": "#f07dbb",
  },
  light: {
    "--hue-red": "#d31616",
    "--hue-orange": "#b14d05",
    "--hue-yellow": "#955d0b",
    "--hue-green": "#567300",
    "--hue-teal": "#2a765d",
    "--hue-blue": "#166ead",
    "--hue-violet": "#8751bc",
    "--hue-pink": "#b43c81",
  },
};

/** GitHub's Primer prettylights, dark and light. */
const GITHUB: ThemeFamilyDefinition = {
  id: "github",
  label: "GitHub",
  dark: {
    "--hue-red": "#ff7b72",
    "--hue-orange": "#ffa657",
    "--hue-yellow": "#d29922",
    "--hue-green": "#7ee787",
    "--hue-teal": "#39c5cf",
    "--hue-blue": "#79c0ff",
    "--hue-violet": "#d2a8ff",
    "--hue-pink": "#f778ba",
  },
  light: {
    "--hue-red": "#cf222e",
    "--hue-orange": "#953800",
    "--hue-yellow": "#906000",
    "--hue-green": "#116329",
    "--hue-teal": "#1a767d",
    "--hue-blue": "#0550ae",
    "--hue-violet": "#7d4ade",
    "--hue-pink": "#b73783",
  },
};

/** Main and Dawn. Rosé Pine has six accents; green borrows foam and blue sits between pine and iris. */
const ROSE_PINE: ThemeFamilyDefinition = {
  id: "rosepine",
  label: "Rosé Pine",
  dark: {
    "--hue-red": "#eb6f92",
    "--hue-orange": "#ebbcba",
    "--hue-yellow": "#f6c177",
    "--hue-green": "#9ccfd8",
    "--hue-teal": "#4094b6",
    "--hue-blue": "#7aa2c8",
    "--hue-violet": "#c4a7e7",
    "--hue-pink": "#e29fc0",
  },
  light: {
    "--hue-red": "#a44f67",
    "--hue-orange": "#bc403a",
    "--hue-yellow": "#975d10",
    "--hue-green": "#42727b",
    "--hue-teal": "#286983",
    "--hue-blue": "#3d5f8f",
    "--hue-violet": "#796095",
    "--hue-pink": "#a54c7c",
  },
};

/** Kanagawa Wave (dark) and Lotus (light). */
const LOTUS: ThemeFamilyDefinition = {
  id: "lotus",
  label: "Lotus",
  dark: {
    "--hue-red": "#e46876",
    "--hue-orange": "#ffa066",
    "--hue-yellow": "#e6c384",
    "--hue-green": "#98bb6c",
    "--hue-teal": "#7aa89f",
    "--hue-blue": "#7e9cd8",
    "--hue-violet": "#9681b9",
    "--hue-pink": "#d27e99",
  },
  light: {
    "--hue-red": "#bf374a",
    "--hue-orange": "#a35700",
    "--hue-yellow": "#726c3c",
    "--hue-green": "#5b7140",
    "--hue-teal": "#52716b",
    "--hue-blue": "#4d699b",
    "--hue-violet": "#624c83",
    "--hue-pink": "#a74d6c",
  },
};

/** Aurora and Frost. Nord publishes one set for both modes; light takes it darker. Nord has no pink; a deeper aurora rose stands in for light. */
const NORD: ThemeFamilyDefinition = {
  id: "nord",
  label: "Nord",
  dark: {
    "--hue-red": "#c6737b",
    "--hue-orange": "#d08770",
    "--hue-yellow": "#ebcb8b",
    "--hue-green": "#a3be8c",
    "--hue-teal": "#8fbcbb",
    "--hue-blue": "#81a1c1",
    "--hue-violet": "#b48ead",
    "--hue-pink": "#e07fa6",
  },
  light: {
    "--hue-red": "#b04751",
    "--hue-orange": "#a85237",
    "--hue-yellow": "#8a6418",
    "--hue-green": "#577140",
    "--hue-teal": "#447170",
    "--hue-blue": "#4c6c93",
    "--hue-violet": "#8a5c82",
    "--hue-pink": "#a3476f",
  },
};

/** Solarized's eight accents, shared by both modes and moved in lightness only. */
const SOLARIZED: ThemeFamilyDefinition = {
  id: "solarized",
  label: "Solarized",
  dark: {
    "--hue-red": "#e45f5d",
    "--hue-orange": "#e86027",
    "--hue-yellow": "#b58900",
    "--hue-green": "#859900",
    "--hue-teal": "#2aa198",
    "--hue-blue": "#2a90d8",
    "--hue-violet": "#8084cc",
    "--hue-pink": "#dc5e9b",
  },
  light: {
    "--hue-red": "#cd2522",
    "--hue-orange": "#b94414",
    "--hue-yellow": "#876600",
    "--hue-green": "#627000",
    "--hue-teal": "#1f7770",
    "--hue-blue": "#1e6ea7",
    "--hue-violet": "#5c61bd",
    "--hue-pink": "#c22b74",
  },
};

/** Gruvbox dark and light, bright and faded accents. */
const GRUVBOX: ThemeFamilyDefinition = {
  id: "gruvbox",
  label: "Gruvbox",
  dark: {
    "--hue-red": "#fb4934",
    "--hue-orange": "#fe8019",
    "--hue-yellow": "#fabd2f",
    "--hue-green": "#b8bb26",
    "--hue-teal": "#8ec07c",
    "--hue-blue": "#83a598",
    "--hue-violet": "#d3869b",
    "--hue-pink": "#fb6e8f",
  },
  light: {
    "--hue-red": "#9d0006",
    "--hue-orange": "#af3a03",
    "--hue-yellow": "#936010",
    "--hue-green": "#726d0d",
    "--hue-teal": "#3f7654",
    "--hue-blue": "#076678",
    "--hue-violet": "#8f3f71",
    "--hue-pink": "#b0305f",
  },
};

/** Tokyo Night and Day. */
const TOKYO_NIGHT: ThemeFamilyDefinition = {
  id: "tokyonight",
  label: "Tokyo Night",
  dark: {
    "--hue-red": "#f7768e",
    "--hue-orange": "#ff9e64",
    "--hue-yellow": "#e0af68",
    "--hue-green": "#9ece6a",
    "--hue-teal": "#73daca",
    "--hue-blue": "#7aa2f7",
    "--hue-violet": "#bb9af7",
    "--hue-pink": "#ff3095",
  },
  light: {
    "--hue-red": "#8c4351",
    "--hue-orange": "#965027",
    "--hue-yellow": "#8f5e15",
    "--hue-green": "#485e30",
    "--hue-teal": "#33635c",
    "--hue-blue": "#34548a",
    "--hue-violet": "#5a4a78",
    "--hue-pink": "#9a3f6e",
  },
};

/** Everforest dark and light. */
const EVERFOREST: ThemeFamilyDefinition = {
  id: "everforest",
  label: "Everforest",
  dark: {
    "--hue-red": "#e67e80",
    "--hue-orange": "#e69875",
    "--hue-yellow": "#dbbc7f",
    "--hue-green": "#a7c080",
    "--hue-teal": "#83c092",
    "--hue-blue": "#7fbbb3",
    "--hue-violet": "#d699b6",
    "--hue-pink": "#e67fa8",
  },
  light: {
    "--hue-red": "#d60c09",
    "--hue-orange": "#af4e08",
    "--hue-yellow": "#8b6400",
    "--hue-green": "#637101",
    "--hue-teal": "#267758",
    "--hue-blue": "#2c7096",
    "--hue-violet": "#be298f",
    "--hue-pink": "#ae408a",
  },
};

/** Xcode's default themes, with Xcode's own syntax mapping. */
const XCODE: ThemeFamilyDefinition = {
  id: "xcode",
  label: "Xcode",
  dark: {
    "--hue-red": "#ff6961",
    "--hue-orange": "#ff9f0a",
    "--hue-yellow": "#ffd60a",
    "--hue-green": "#30d158",
    "--hue-teal": "#40c8e0",
    "--hue-blue": "#409cff",
    "--hue-violet": "#da8fff",
    "--hue-pink": "#fc5fa3",
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
    "--hue-red": "#d70015",
    "--hue-orange": "#c53300",
    "--hue-yellow": "#9d5900",
    "--hue-green": "#1f7935",
    "--hue-teal": "#0071a4",
    "--hue-blue": "#0067d7",
    "--hue-violet": "#8944ab",
    "--hue-pink": "#ab3ca2",
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

/** Every palette the person can pick, in the order Settings shows them. */
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

/** Both modes of a palette, resolved — what applyThemeFamily takes. */
export function resolvedPalettes(id: ThemeFamilyPreference): {
  dark: ResolvedPalette;
  light: ResolvedPalette;
} {
  const family = familyOf(id);
  return { dark: resolvePalette(family.dark), light: resolvePalette(family.light) };
}
