import { describe, expect, it } from "bun:test";
import { readIndexCss, resolveVar, themeBlocks } from "./cssTokens";
import { FAMILIES, resolvePalette } from "./families";
import { PALETTE_TOKENS, SYNTAX_STYLE_TOKENS } from "./tokens";
import { METHOD_CHIP_CLASSES } from "@/lib/httpMethod";

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
 * Three tiers, not one floor: 4.5 everywhere would pass a ramp whose primary,
 * secondary and tertiary all land on 4.6 — a hierarchy with none left in it.
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

const blocks = themeBlocks();
const MODES = ["dark", "light"] as const;
type Mode = (typeof MODES)[number];

/** A chrome token or a `var(--x)` / hex value, as the hex it paints. */
function chrome(tokenOrValue: string, mode: Mode): string {
  const value = resolveVar(tokenOrValue, blocks[mode], blocks.dark);
  if (!value?.startsWith("#")) throw new Error(`${tokenOrValue} (${mode}) is ${value}, not a hex`);
  return value;
}

/** The shell backdrop pair, declared outside the token blocks. */
const shellCss = readIndexCss();
function shellColour(token: string, mode: Mode) {
  const values = [...shellCss.matchAll(new RegExp(`${token}:\\s*(#[a-f0-9]+)`, "g"))];
  if (values.length !== 2) {
    throw new Error(`expected one dark and one light ${token} in index.css, found ${values.length}`);
  }
  return values[mode === "dark" ? 0 : 1][1];
}

/** Every ground text sits on, including both ends of the shell backdrop. */
function grounds(mode: Mode): Record<string, string> {
  const base = chrome("--surface-base", mode);
  return {
    base,
    raised: chrome("--surface-raised", mode),
    canvas: chrome("--background-canvas", mode),
    app: chrome("--background-app", mode),
    // index.css: --surface-well is --tint-veil at 6% in dark, 4% in light.
    well: over(chrome("--tint-veil", mode), base, mode === "dark" ? 0.06 : 0.04),
    shell: shellColour("--shell-backdrop", mode),
    shellTop: shellColour("--shell-backdrop-top", mode),
  };
}

function expectOnGrounds(colour: string, mode: Mode, floor: number) {
  for (const [name, ground] of Object.entries(grounds(mode))) {
    const ratio = contrast(colour, ground);
    expect(ratio, `${colour} on ${name} ${ground} is ${ratio.toFixed(2)}:1, floor ${floor}:1`).toBeGreaterThanOrEqual(floor);
  }
}

describe("the Mono chrome is readable on its own surfaces", () => {
  for (const mode of MODES) {
    for (const [token, floor] of Object.entries(FLOOR)) {
      it(`${mode} ${token}`, () => expectOnGrounds(chrome(token, mode), mode, floor));
    }

    it(`${mode} keeps its text ramp in order`, () => {
      const on = (token: string) => contrast(chrome(token, mode), chrome("--background-app", mode));
      expect(on("--text-primary")).toBeGreaterThan(on("--text-secondary"));
      expect(on("--text-secondary")).toBeGreaterThan(on("--text-tertiary"));
    });

    // A selected sidebar row is --surface-raised on the app-coloured rail, and a
    // raised card sits on a base page: the fill has to be a visible step off
    // both, or the selection is invisible (light once had them identical).
    it(`${mode} raised surface stands off the app background and the base`, () => {
      const raised = chrome("--surface-raised", mode);
      expect(contrast(raised, chrome("--background-app", mode))).toBeGreaterThanOrEqual(1.06);
      expect(contrast(raised, chrome("--surface-base", mode))).toBeGreaterThanOrEqual(1.06);
    });

    // The selected mark (active tab, selected tree row) is --status-info at
    // full opacity; non-text, 3:1 against the fill it outlines.
    it(`${mode} selected mark is visible`, () => {
      const ratio = contrast(chrome("--status-info", mode), chrome("--surface-raised", mode));
      expect(ratio).toBeGreaterThanOrEqual(3);
    });

    // The keyboard focus ring is --border-focus at 50% (`ring-border-focus/50`,
    // the global `outline-ring/50`): a WCAG focus indicator, non-text, 3:1
    // against whatever it is drawn on, once flattened.
    it(`${mode} focus ring at 50% is visible on every ground`, () => {
      for (const [name, ground] of Object.entries(grounds(mode))) {
        const ring = over(chrome("--border-focus", mode), ground, 0.5);
        const ratio = contrast(ring, ground);
        expect(ratio, `focus ring on ${name} ${ground} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(3);
      }
    });

    // A solid status fill (status pill, Stop button, git/verb chips) carries
    // a glyph in --surface-base (4.5:1) and stands off the card (3:1).
    for (const token of ["--status-success", "--status-warning", "--status-danger"]) {
      it(`${mode} ${token} as a filled pill`, () => {
        const fill = chrome(token, mode);
        expect(contrast(chrome("--surface-base", mode), fill)).toBeGreaterThanOrEqual(4.5);
        expectOnGrounds(fill, mode, 3);
      });
    }

    // A running agent's dot must not read as an idle one: --status-running is
    // plain text colour, --status-idle is --text-tertiary.
    it(`${mode} running agents are distinguishable from idle ones`, () => {
      const ratio = contrast(chrome("--status-running", mode), chrome("--text-tertiary", mode));
      expect(ratio).toBeGreaterThanOrEqual(1.5);
    });

    // The primary action: its label on its fill.
    it(`${mode} primary action label is legible`, () => {
      const ratio = contrast(chrome("--action-primary-foreground", mode), chrome("--action-primary", mode));
      expect(ratio).toBeGreaterThanOrEqual(7);
    });
  }

  // Claude's coral: a brand mark, non-text, 3:1. Read from index.css so a
  // copy here cannot pass forever after someone edits the stylesheet.
  it("a working agent's mark is visible", () => {
    for (const mode of MODES) expectOnGrounds(chrome("--vendor-claude", mode), mode, 3);
  });
});

/**
 * Every palette's colours are text on the Mono grounds: hues (chips, folder
 * names), syntax and ANSI. A family colour that misses is moved in its own
 * hue, lightness only, in families.ts.
 */
describe("every color palette is readable on the Mono grounds", () => {
  const style = new Set<string>(SYNTAX_STYLE_TOKENS);
  for (const family of Object.values(FAMILIES)) {
    for (const mode of MODES) {
      const resolved = resolvePalette(family[mode]);
      for (const token of PALETTE_TOKENS) {
        if (style.has(token)) continue;
        it(`${family.id}/${mode} ${token}`, () => {
          expectOnGrounds(chrome(resolved[token], mode), mode, 4.5);
        });
      }

      // A filled hue mark (git's untracked/renamed letters) carries the
      // primary action's foreground as its glyph.
      it(`${family.id}/${mode} hues carry a glyph`, () => {
        const glyph = chrome("--action-primary-foreground", mode);
        for (const hue of ["--hue-red", "--hue-green", "--hue-yellow", "--hue-blue", "--hue-violet"] as const) {
          const ratio = contrast(glyph, resolved[hue]);
          expect(ratio, `${glyph} on ${resolved[hue]} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(4.5);
        }
      });

      // An HTTP method chip is its hue as text on that hue tinted into a
      // chrome surface by --method-tint (per mode, index.css). The mix is read
      // back from the class the chip carries and flattened the way
      // `color-mix(in srgb, A p%, B)` paints two opaque colours: p of A over B.
      for (const [hue, cls] of Object.entries(METHOD_CHIP_CLASSES)) {
        it(`${family.id}/${mode} ${hue} method chip`, () => {
          const mix = cls.match(/color-mix\(in_srgb,var\((--hue-[a-z]+)\)_var\((--[a-z-]+)\),var\((--[a-z-]+)\)\)/);
          if (!mix) throw new Error(`${cls} is not a tinted chip`);
          const [, mixed, amountToken, surface] = mix;
          expect(mixed).toBe(hue);
          expect(cls).toContain(`text-(${hue})`);
          const percent = resolveVar(amountToken, blocks[mode], blocks.dark);
          if (!percent?.endsWith("%")) throw new Error(`${amountToken} (${mode}) is ${percent}, not a percentage`);
          const text = resolved[hue as keyof typeof resolved];
          const ground = over(text, chrome(surface, mode), Number.parseFloat(percent) / 100);
          const ratio = contrast(text, ground);
          expect(ratio, `${text} on its ${percent} tint ${ground} is ${ratio.toFixed(2)}:1`).toBeGreaterThanOrEqual(4.5);
        });
      }
    }
  }
});
