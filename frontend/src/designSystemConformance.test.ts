import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

/**
 * Phase 5 turns "apply the design system everywhere" into a number that can only
 * go down.
 *
 * The scale lives in src/index.css: 18 text-style utilities and the Scale
 * variables (space, radius, control height, icon, stroke). A component that
 * reaches past them — Tailwind's own text-sm, an arbitrary [13px], a raw hex —
 * is a step the design file has no bind for, and the docs call that out as the
 * exact drift this system exists to stop.
 *
 * This is a ratchet, not a gate: the baseline records what each surface owes
 * today, and the test fails when a count goes UP. Convert a module, lower its
 * numbers here, and they can never climb back without a red test. When a
 * surface reaches zero the entry stays at 0 and becomes a real guard.
 */

// Surfaces already converted sit at 0 and must stay there. The rest carry their
// debt as a number; lower it as you convert, never raise it.
const BASELINE: Record<string, { twText: number; rawPx: number; rawHex: number }> = {
  // Converted 2026-09-09: arbitrary px moved onto the spacing scale (w-80,
  // max-w-66, h-56.25…) and the hex in the comments went with them.
  "modules/agenthub": { twText: 0, rawPx: 0, rawHex: 0 },
  // Reconciled with Figma 14 on 2026-09-09. The 9px canvas labels became
  // text-label-11-caps — consistency across surfaces beat the literal size.
  "modules/codegraph": { twText: 0, rawPx: 0, rawHex: 0 },
  // Converted 2026-09-08. 13 rawPx remain and none is a design step: 5 are the
  // ERD canvas node labels (same carve-out as CodeGraph) and 8 are dialog and
  // panel widths — content measures the Scale has no bucket for. Lowered from
  // 15 when the console's inline result table became ResultGrid: its two
  // max-w-[NNNpx] cell caps are now colgroup widths the caller owns.
  "modules/database": { twText: 0, rawPx: 8, rawHex: 0 },
  "modules/kafka": { twText: 0, rawPx: 0, rawHex: 0 },
  // Reconciled with Figma 13 on 2026-09-09; the 720 writing column is
  // max-w-180 on the spacing scale.
  "modules/notes": { twText: 0, rawPx: 0, rawHex: 0 },
  // Converted 2026-09-09 against Figma 11 Settings. The 9 remaining rawPx are
  // the modal's own frame (620×940, a 220 nav, 656 content columns) and the
  // two blur radii — measures of the modal, not steps on the scale.
  // 6 → 3: Settings is a Dialog, and its hand-drawn scrim and shadow went.
  "modules/settings": { twText: 0, rawPx: 3, rawHex: 0 },
  "modules/ssh": { twText: 0, rawPx: 0, rawHex: 0 },
  "modules/sync": { twText: 0, rawPx: 0, rawHex: 0 },
  "modules/tools": { twText: 0, rawPx: 0, rawHex: 0 },
  // Re-measured once comments stopped counting (see strip): rawHex 23 → 1, so
  // twenty-two of the "raw hexes" in the shared UI were notes ABOUT hexes. The
  // figure had been measuring its own documentation, which is the same mistake
  // in a different place.
  // components/ (the shared UI) joined the table 2026-09-09 at its measured
  // debt. It is where the hand-written glass materials live — including a
  // light-theme bug, since a hardcoded rgba(255,255,255,0.1) hairline stays
  // white on a white surface — so this row is the one to work down next.
  // rawHex 48 → 23: the Plate code block carried twenty-five literal hexes on
  // one className — two GitHub palettes behind a `dark:` prefix — and they are
  // tokens now, so a code block follows the theme family. What remains is
  // documentation: comments naming the Figma token a class maps to.
  // Spacing and radius literals now resolve through shared tokens.
  // rawPx 79 → 78: the command dialog's width is one min(640px, 100% - 2rem)
  // where it was a w-[640px] max-w-[640px] pair.
  components: { twText: 0, rawPx: 78, rawHex: 1 },
  // 33 → 24: nine `rounded-[8px]` went with the nine copies of the title
  // bar's action button, which is Button's `titlebar` variant now. --radius-md
  // is that 8px, so the scale had the number the whole time. 24 → 19: the
  // dock went, and modules switch from title-bar Buttons.
  workbench: { twText: 0, rawPx: 9, rawHex: 1 },
};

// Two files carry raw hex on purpose and are not design-system surfaces: a
// colour picker whose palette IS its data, and a syntax-highlight theme.
const HEX_EXEMPT = [
  "components/ui/font-color-toolbar-button.tsx",
  "components/ui/code-block-node-static.tsx",
];
// engineMarks.tsx was the third entry, exempted as "vendor brand marks — a
// logo's colour is the vendor's, not the theme's". The file's own header said
// otherwise on the line above the fills: they are "the palette values the
// design chose, not the vendors' own brand colours", and they were
// candy-blue, vanilla, orchid and the secondary text colour. So the guard was
// switched off over a premise that was never true, and four dark-mode hexes
// sat behind it until a light theme made the connection icon disappear.
// Exempt a file from this only for colour that is genuinely DATA.

const TAILWIND_TEXT = /\btext-(xs|sm|base|lg|xl|2xl|3xl|4xl)\b/g;
const ARBITRARY_PX = /\[[0-9.]+px\]/g;
const RAW_HEX = /#[0-9a-fA-F]{6}\b/g;

const ROOT = join(import.meta.dir);

function tsxFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      out.push(...tsxFiles(path));
      continue;
    }
    // A test file's fixtures are not product chrome.
    if (entry.endsWith(".tsx") && !entry.includes(".test.")) out.push(path);
  }
  return out;
}

/**
 * Comments are not chrome.
 *
 * Every metric here is a substring search over source, and a comment
 * explaining why a raw value was removed contains the raw value. This has
 * caught its own explanation five separate times — a hex quoted in the note
 * saying the hexes are gone, `min-w-[200px]` in the note about a menu that no
 * longer hard-codes one. Stripping them first is the fix; naming the thing you
 * removed is how a comment earns its place.
 */
function strip(body: string): string {
  return body.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");
}

function count(pattern: RegExp, body: string): number {
  return body.match(new RegExp(pattern.source, "g"))?.length ?? 0;
}

function measure(surface: string) {
  const dir = join(ROOT, surface);
  let twText = 0;
  let rawPx = 0;
  let rawHex = 0;
  for (const file of tsxFiles(dir)) {
    const body = strip(readFileSync(file, "utf8"));
    twText += count(TAILWIND_TEXT, body);
    rawPx += count(ARBITRARY_PX, body);
    const relative = file.slice(ROOT.length + 1);
    if (!HEX_EXEMPT.includes(relative)) rawHex += count(RAW_HEX, body);
  }
  return { twText, rawPx, rawHex };
}

describe("design system conformance", () => {
  for (const [surface, owed] of Object.entries(BASELINE)) {
    test(`${surface} does not drift further from the scale`, () => {
      const actual = measure(surface);
      for (const metric of ["twText", "rawPx", "rawHex"] as const) {
        expect(
          actual[metric],
          `${surface}.${metric}: ${actual[metric]} > baseline ${owed[metric]}. ` +
            `Use the scale in src/index.css instead of a raw value, or — if this ` +
            `surface got converted — lower the baseline in this file.`,
        ).toBeLessThanOrEqual(owed[metric]);
      }
    });
  }

  // A baseline nobody lowers is a baseline nobody reads. Fail when a surface is
  // cleaner than recorded, so the number tracks reality instead of history.
  test("the baseline is not stale", () => {
    const stale: string[] = [];
    for (const [surface, owed] of Object.entries(BASELINE)) {
      const actual = measure(surface);
      for (const metric of ["twText", "rawPx", "rawHex"] as const) {
        if (actual[metric] < owed[metric]) {
          stale.push(`${surface}.${metric}: now ${actual[metric]}, baseline says ${owed[metric]}`);
        }
      }
    }
    expect(stale, `lower these baselines — the work is already done:\n${stale.join("\n")}`).toEqual(
      [],
    );
  });
});
