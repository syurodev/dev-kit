import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { FAMILIES, type ThemePalette } from "./families";

/**
 * Contrast, for every family including the base.
 *
 * index.css's palette was tuned by hand and carries notes about it — "lime
 * text on white is illegible, so --brand-text flips". A family authored later
 * has none of that care behind it, so the guarantee has to become a number.
 *
 * The base is checked too, and that is the point: thresholds that only the new
 * themes must clear are thresholds nobody has validated. Anything the base
 * fails here would be a claim about DevKit's own palette, which it is not this
 * test's place to make — so the levels are set where the shipped design
 * already sits.
 */

const css = readFileSync(join(import.meta.dir, "../index.css"), "utf8").replace(
  /\/\*[\s\S]*?\*\//g,
  "",
);

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

const BASE = { dark: block(/\n:root/), light: block(/:root\[data-theme="light"\]/) };

/** One level of `var(--x)` indirection, which is all index.css uses for these. */
function resolve(token: string, mode: "dark" | "light", palette: ThemePalette): string | null {
  const fromPalette = palette[token as keyof ThemePalette];
  let value = fromPalette ?? BASE[mode].get(token) ?? BASE.dark.get(token);
  for (let hop = 0; hop < 4 && value?.startsWith("var("); hop += 1) {
    const name = value.slice(4, value.indexOf(")"));
    value = BASE[mode].get(name) ?? BASE.dark.get(name);
  }
  return value && /^#[0-9a-f]{6}$/i.test(value) ? value : null;
}

function channel(value: number): number {
  const c = value / 255;
  return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}

function luminance(hex: string): number {
  const n = parseInt(hex.slice(1), 16);
  return (
    0.2126 * channel((n >> 16) & 255) +
    0.7152 * channel((n >> 8) & 255) +
    0.0722 * channel(n & 255)
  );
}

/** WCAG 2.1 contrast ratio, 1..21. */
export function contrast(foreground: string, background: string): number {
  const [a, b] = [luminance(foreground), luminance(background)].sort((x, y) => y - x);
  return (a + 0.05) / (b + 0.05);
}

/**
 * What has to be readable against what, and how readable.
 *
 * 4.5 is AA for body text. 3 is AA for large text and for non-text UI, which
 * is what a tertiary hint, a status colour and an accent used on chips and
 * icons actually are — and where index.css's own light --brand-text (#6b8f00
 * on white, 3.6:1) already sits, so demanding 4.5 would fail the design this
 * test exists to protect.
 */
const PAIRS: ReadonlyArray<[foreground: string, background: string, min: number]> = [
  ["--text-primary", "--surface-base", 4.5],
  ["--text-secondary", "--surface-base", 4.5],
  ["--text-primary", "--surface-raised", 4.5],
  ["--text-secondary", "--surface-raised", 4.5],
  ["--text-tertiary", "--surface-base", 3],
  ["--icon-secondary", "--surface-base", 3],
  ["--action-primary-foreground", "--action-primary", 4.5],
  ["--brand-text", "--surface-base", 3],
  ["--status-info", "--surface-base", 3],
  ["--status-success", "--surface-base", 3],
  ["--status-warning", "--surface-base", 3],
  ["--status-recovery", "--surface-base", 3],
  ["--status-danger", "--surface-base", 3],
];

describe.each(Object.values(FAMILIES))("$label", (family) => {
  for (const mode of ["dark", "light"] as const) {
    describe(mode, () => {
      const palette = family[mode];

      test.each(PAIRS)("%s on %s clears %p:1", (fgToken, bgToken, min) => {
        const fg = resolve(fgToken, mode, palette);
        const bg = resolve(bgToken, mode, palette);
        expect(fg, `${fgToken} did not resolve to a hex`).not.toBeNull();
        expect(bg, `${bgToken} did not resolve to a hex`).not.toBeNull();
        const ratio = contrast(fg!, bg!);
        expect(
          Number(ratio.toFixed(2)),
          `${fgToken} ${fg} on ${bgToken} ${bg} is ${ratio.toFixed(2)}:1, want ${min}:1`,
        ).toBeGreaterThanOrEqual(min);
      });

      // The app frame recesses behind the content in BOTH modes — index.css
      // is dark #020202 behind #1d1d1d and light #e9eaee behind #ffffff. The
      // first version of this test assumed light inverted the ramp and failed
      // DevKit itself, which is the useful thing about checking the base: an
      // invariant the shipped design breaks is a wrong invariant.
      test("the app frame sits behind the content surface", () => {
        const app = resolve("--background-app", mode, palette);
        const base = resolve("--surface-base", mode, palette);
        expect(app, "--background-app did not resolve").not.toBeNull();
        expect(base, "--surface-base did not resolve").not.toBeNull();
        expect(
          luminance(app!),
          `the frame ${app} is lighter than the surface ${base} it sits behind`,
        ).toBeLessThanOrEqual(luminance(base!));
      });
    });
  }
});
