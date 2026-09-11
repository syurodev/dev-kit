import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { DEVKIT_FAMILY, FAMILIES } from "./families";
import { CONTRACT, MIX_TOKENS, THEME_TOKENS, TINT_TOKENS } from "./tokens";

// Comments stripped first: index.css documents tokens in prose that starts
// with the token's own name ("--brand-text is the accent for text/icons…"),
// and a parser that reads those as declarations invents tokens that do not
// exist. Measured the hard way — this exact line showed up as an orphan.
const css = readFileSync(join(import.meta.dir, "../index.css"), "utf8").replace(
  /\/\*[\s\S]*?\*\//g,
  "",
);

/** The declarations inside one `{ … }` block, as name → value. */
function block(selector: RegExp): Map<string, string> {
  const match = css.match(new RegExp(selector.source + String.raw`\s*\{([\s\S]*?)\n\}`));
  if (!match) throw new Error(`no block for ${selector}`);
  const out = new Map<string, string>();
  for (const line of match[1].split("\n")) {
    const declaration = line.trim();
    if (!declaration.startsWith("--")) continue;
    const colon = declaration.indexOf(":");
    if (colon === -1) continue;
    out.set(declaration.slice(0, colon).trim(), declaration.slice(colon + 1).split(";")[0].trim());
  }
  return out;
}

const dark = block(/\n:root/);
const light = block(/:root\[data-theme="light"\]/);

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
  test("no colour token exists outside the contract without being derived", () => {
    const derived = (value: string) => value.startsWith("var(") || value.startsWith("color-mix(");
    const shape = (name: string) =>
      /blur|saturate|radius|motion|ease|font|spring/.test(name) || name.startsWith("--palette-");
    const orphans = [...dark]
      .filter(([name, value]) => !shape(name) && !derived(value) && !CONTRACT.includes(name as never))
      .map(([name]) => name);
    expect(orphans, `${orphans.join(", ")} is a literal colour no theme can reach`).toEqual([]);
  });
});

describe("light is a working second implementation", () => {
  // Light is the proof that the contract is implementable by something other
  // than the values it was extracted from. It may inherit a token whose value
  // genuinely does not change (the lime fill), so this is a floor, not an
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

  test("the three tiers do not overlap", () => {
    const tint = new Set<string>(TINT_TOKENS);
    const mix = new Set<string>(MIX_TOKENS);
    expect(THEME_TOKENS.filter((t) => tint.has(t) || mix.has(t))).toEqual([]);
  });
});

describe("the devkit family mirrors the stylesheet", () => {
  /** One level of `var(--x)`, which is all index.css uses for these. */
  function effective(token: string, mode: "dark" | "light"): string | undefined {
    const source = mode === "light" ? light : dark;
    let value = source.get(token) ?? dark.get(token);
    for (let hop = 0; hop < 4 && value?.startsWith("var("); hop += 1) {
      const name = value.slice(4, value.indexOf(")"));
      value = source.get(name) ?? dark.get(name);
    }
    return value;
  }

  // DEVKIT_FAMILY is a copy of index.css, which it has to be for the Settings
  // swatch to paint itself — an element the `:root` rule does not match cannot
  // fall back to values only `:root` declares. A copy is a second place to
  // change a colour, so this is the price: every value pinned, both ways.
  for (const mode of ["dark", "light"] as const) {
    test(`${mode} matches what index.css resolves to`, () => {
      const drift: string[] = [];
      for (const [token, value] of Object.entries(DEVKIT_FAMILY[mode])) {
        const fromCss = effective(token, mode);
        if (fromCss !== value) {
          drift.push(`${token}: family has ${value}, index.css resolves ${fromCss}`);
        }
      }
      expect(drift, drift.join("\n")).toEqual([]);
    });
  }

  // Every family has to cover the same ground, or switching to one leaves
  // tokens behind from whichever was applied before it.
  test("every family declares the same tokens as the base", () => {
    const expected = Object.keys(DEVKIT_FAMILY.dark).sort();
    for (const family of Object.values(FAMILIES)) {
      for (const mode of ["dark", "light"] as const) {
        const got = Object.keys(family[mode]).sort();
        const missing = expected.filter((token) => !got.includes(token));
        expect(missing, `${family.label} ${mode} is missing ${missing.join(", ")}`).toEqual([]);
      }
    }
  });
});
