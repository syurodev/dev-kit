import { describe, expect, test } from "bun:test";
import { themeBlocks } from "./cssTokens";
import { DEVKIT_FAMILY } from "./families";
import {
  CONTRACT,
  HUE_TOKENS,
  MIX_TOKENS,
  PALETTE_TOKENS,
  SYNTAX_STYLE_TOKENS,
  SYNTAX_TOKENS,
  TERMINAL_TOKENS,
  THEME_TOKENS,
  TINT_TOKENS,
} from "./tokens";

const { dark, light } = themeBlocks();

describe("the base theme implements the contract", () => {
  // :root is the dark theme. It has to answer for every token, because
  // everything else in the app — light, and later a named theme — is an
  // override on top of it, and an override cannot introduce a token.
  test("dark declares every token in the contract", () => {
    const missing = CONTRACT.filter((token) => !dark.has(token));
    expect(missing, `:root is missing ${missing.join(", ")}`).toEqual([]);
  });

  // The reverse: a token that exists in CSS but not in the contract is one a
  // theme will silently fail to set. Derived tokens are exempt — they follow
  // their parent by construction.
  //
  // So are `--vendor-*`. They are another company's colour, and a theme moving
  // Claude's coral to fit its own palette would be repainting someone's logo.
  // They still answer to themeContrast.test.ts, which measures them against
  // every family's grounds — theme-invariant is not the same as unchecked.
  test("no colour token exists outside the contract without being derived", () => {
    const derived = (value: string) => value.startsWith("var(") || value.startsWith("color-mix(");
    const shape = (name: string) => /blur|saturate|radius|motion|ease|font|spring/.test(name);
    const brand = (name: string) => name.startsWith("--vendor-");
    const orphans = [...dark]
      .filter(
        ([name, value]) =>
          !shape(name) && !brand(name) && !derived(value) && !CONTRACT.includes(name as never),
      )
      .map(([name]) => name);
    expect(orphans, `${orphans.join(", ")} is a literal colour no theme can reach`).toEqual([]);
  });
});

describe("light is a working second implementation", () => {
  // Light is the proof that the contract is implementable by something other
  // than the values it was extracted from. It may inherit a token whose value
  // genuinely does not change (a derived token such as --icon-secondary), so this is a floor, not an
  // equality — but it must carry the tints, or every alpha layer in the app
  // would still be built from dark's white.
  test("light redefines the tint sources", () => {
    for (const tint of ["--tint-veil", "--tint-border"] as const) {
      expect(light.has(tint), `light inherits ${tint} from dark`).toBe(true);
    }
  });

  test("light redefines the surfaces and the text ramp", () => {
    for (const token of [
      "--background-app",
      "--surface-base",
      "--surface-raised",
      "--text-primary",
      "--text-secondary",
      "--text-tertiary",
    ] as const) {
      expect(light.has(token), `light inherits ${token} from dark`).toBe(true);
    }
  });

  // A token light overrides with the SAME formula as dark is a line that will
  // rot: change dark's percentage and light keeps the old one silently.
  test("light overrides nothing it could have inherited", () => {
    const redundant = [...light].filter(([name, value]) => dark.get(name) === value);
    expect(
      redundant.map(([name]) => name),
      "these light declarations are identical to dark's and can go",
    ).toEqual([]);
  });
});

describe("alpha layers are derived, not typed out", () => {
  // The bug this whole pass exists for: --border-default read
  // rgba(178, 213, 229, .16), which is candy-blue written by hand, so a theme
  // that changed the accent kept candy-blue hairlines.
  test("every mix token is a color-mix over a named source", () => {
    for (const token of MIX_TOKENS) {
      const value = light.get(token) ?? dark.get(token) ?? "";
      expect(value, `${token} is a literal, so a theme cannot move it`).toContain("color-mix(");
      expect(value, `${token} mixes a literal instead of a token`).toMatch(/var\(--[\w-]+\)/);
    }
  });

  test("no semantic token still carries a hand-written rgba", () => {
    const typed = [...dark, ...light].filter(
      ([name, value]) => CONTRACT.includes(name as never) && /^rgba?\(/.test(value),
    );
    expect(typed.map(([name]) => name), "still rgba() by hand").toEqual([]);
  });
});

describe("the contract's own shape", () => {
  test("no token is listed twice", () => {
    expect(new Set(CONTRACT).size).toBe(CONTRACT.length);
  });

  test("the tiers do not overlap", () => {
    const lists = [TINT_TOKENS, THEME_TOKENS, MIX_TOKENS, HUE_TOKENS, SYNTAX_TOKENS, SYNTAX_STYLE_TOKENS, TERMINAL_TOKENS];
    const all = lists.flatMap((list) => [...list]);
    expect(new Set(all).size).toBe(all.length);
  });
});


describe("the base palette in index.css is devkit", () => {
  // DEVKIT_FAMILY is a copy of index.css's palette values: the stylesheet
  // needs them for the first paint, families.ts for the Settings swatch and
  // for applyThemeFamily. A copy is a second place to change a colour, so
  // every value is pinned, both ways.
  for (const mode of ["dark", "light"] as const) {
    test(`${mode} hues and syntax match`, () => {
      const block = mode === "dark" ? dark : light;
      const drift: string[] = [];
      for (const [token, value] of Object.entries(DEVKIT_FAMILY[mode])) {
        const fromCss = block.get(token) ?? dark.get(token);
        if (fromCss !== value) drift.push(`${token}: family has ${value}, index.css has ${fromCss}`);
      }
      expect(drift, drift.join("\n")).toEqual([]);
    });
  }

  test("light restates only the hues", () => {
    const restated = [...PALETTE_TOKENS].filter((t) => light.has(t));
    expect(restated.sort()).toEqual([...HUE_TOKENS].sort());
  });

  test("ANSI derives from hues", () => {
    expect(dark.get("--ansi-red")).toBe("var(--hue-red)");
    expect(dark.get("--ansi-green")).toBe("var(--hue-green)");
    expect(dark.get("--ansi-yellow")).toBe("var(--hue-yellow)");
    expect(dark.get("--ansi-blue")).toBe("var(--hue-blue)");
    expect(dark.get("--ansi-magenta")).toBe("var(--hue-violet)");
    expect(dark.get("--ansi-cyan")).toBe("var(--hue-teal)");
  });

  test("note-folder colours are the hues", () => {
    for (const hue of ["red", "orange", "yellow", "green", "teal", "blue", "violet", "pink"]) {
      expect(dark.get(`--note-folder-${hue}`)).toBe(`var(--hue-${hue})`);
      expect(light.has(`--note-folder-${hue}`)).toBe(false);
    }
  });
});
