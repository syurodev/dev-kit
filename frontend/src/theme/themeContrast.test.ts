import { describe, expect, it } from "bun:test";
import { FAMILIES } from "./families";

/**
 * Every palette has to be readable on its own surfaces.
 *
 * This exists because five families shipped failing it, and not by accident:
 * Catppuccin's `overlay1`, Dracula's `comment` and Ayu's `ui.fg` are all
 * EDITOR colours, tuned for code sitting on an editor background. Used as UI
 * text — an 11px row number, a table count in a rail — they were as low as
 * 2.5:1. The palettes are followed everywhere they hold up and moved, in their
 * own hue, exactly as far as it takes to clear the floor.
 *
 * accessibility.md › Vision: "Up to 17 pts | All | 4.5:1". Nothing in this app
 * renders UI text at 17pt, so nothing here is eligible for the 3:1 row.
 */

const lin = (c: number) => {
  const v = c / 255;
  return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
};
const rgb = (h: string): [number, number, number] => {
  const n = Number.parseInt(h.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
};
const hex = (r: number, g: number, b: number) =>
  "#" + [r, g, b].map((v) => Math.round(v).toString(16).padStart(2, "0")).join("");

export function luminance(colour: string): number {
  const [r, g, b] = rgb(colour);
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

export function contrast(a: string, b: string): number {
  const x = luminance(a);
  const y = luminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

/** An alpha layer flattened onto what is behind it, the way the compositor does. */
function over(top: string, bottom: string, alpha: number): string {
  const T = rgb(top);
  const B = rgb(bottom);
  return hex(...(([0, 1, 2] as const).map((i) => T[i] * alpha + B[i] * (1 - alpha)) as [number, number, number]));
}

/**
 * Three tiers, not one floor.
 *
 * 4.5 everywhere would pass a palette whose primary, secondary and tertiary
 * all land on 4.6 — legal, and a hierarchy with no hierarchy left in it. Two
 * of the light palettes did exactly that once their tertiary was lifted.
 */
const FLOOR: Record<string, number> = {
  "--text-primary": 7,
  "--text-secondary": 5.5,
  "--text-tertiary": 4.5,
  "--icon-secondary": 4.5,
  "--brand-text": 4.5,
  "--status-info": 4.5,
  "--status-success": 4.5,
  "--status-warning": 4.5,
  "--status-recovery": 4.5,
  "--status-danger": 4.5,
};

/**
 * Every ground content is drawn on.
 *
 * `--background-app` is in the list because the sidebar rail has no material
 * of its own (`--glass-rail-bg: transparent`) — the tree's labels and counts
 * sit straight on the window's own fill, and in a light theme that is the
 * darkest of the four.
 */
function groundsOf(palette: Record<string, string>, mode: "dark" | "light") {
  const base = palette["--surface-base"];
  return {
    base,
    raised: palette["--surface-raised"],
    // index.css: --surface-well is --tint-veil at 6% in dark, 4% in light.
    well: over(palette["--tint-veil"], base, mode === "dark" ? 0.06 : 0.04),
    app: palette["--background-app"],
  };
}

describe("every family is readable on its own surfaces", () => {
  for (const family of Object.values(FAMILIES)) {
    for (const mode of ["dark", "light"] as const) {
      const palette = family[mode] as Record<string, string>;
      const grounds = groundsOf(palette, mode);

      for (const [token, floor] of Object.entries(FLOOR)) {
        const colour = palette[token];
        if (!colour?.startsWith("#")) continue;
        it(`${family.id}/${mode} ${token}`, () => {
          for (const [name, ground] of Object.entries(grounds)) {
            const ratio = contrast(colour, ground);
            expect(
              ratio,
              `${colour} on ${name} ${ground} is ${ratio.toFixed(2)}:1, floor ${floor}:1`,
            ).toBeGreaterThanOrEqual(floor);
          }
        });
      }

      // The tiers have to stay in order as well as above the floor: a palette
      // whose tertiary out-contrasts its secondary has three names for two
      // colours.
      it(`${family.id}/${mode} keeps its text ramp in order`, () => {
        const on = (token: string) => contrast(palette[token], grounds.app);
        expect(on("--text-primary")).toBeGreaterThan(on("--text-secondary"));
        expect(on("--text-secondary")).toBeGreaterThan(on("--text-tertiary"));
      });
    }
  }
});
