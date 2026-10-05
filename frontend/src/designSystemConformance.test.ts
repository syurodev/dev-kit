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
  // accounts, apiclient and git joined the table 2026-10-02 — none had been
  // measured, which is how Accounts kept Tailwind's text-sm/text-xs and a
  // `text-fg-muted` that no stylesheet defines. Accounts was converted on the
  // way in; Git cleared its typography debt and fg-muted in UI rebuild phase 5.
  "modules/accounts": { twText: 0, rawPx: 0, rawHex: 0 },
  "modules/agenthub": { twText: 0, rawPx: 0, rawHex: 0 },
  "modules/apiclient": { twText: 0, rawPx: 0, rawHex: 0 },
  // Reconciled with Figma 14 on 2026-09-09. The 9px canvas labels became
  // text-label-11-caps — consistency across surfaces beat the literal size.
  "modules/codegraph": { twText: 0, rawPx: 0, rawHex: 0 },
  // Converted 2026-09-08. 13 rawPx remain and none is a design step: 5 are the
  // ERD canvas node labels (same carve-out as CodeGraph) and 8 are dialog and
  // panel widths — content measures the Scale has no bucket for. Lowered from
  // 15 when the console's inline result table became ResultGrid: its two
  // max-w-[NNNpx] cell caps are now colgroup widths the caller owns.
  // 8 → 0 on 2026-09-27 with shadcn/no-arbitrary-values: every px width that
  // landed on the 4px grid is its spacing step (w-80, max-w-150…).
  "modules/database": { twText: 0, rawPx: 0, rawHex: 0 },
  "modules/git": { twText: 0, rawPx: 0, rawHex: 0 },
  "modules/kafka": { twText: 0, rawPx: 0, rawHex: 0 },
  // Reconciled with Figma 13 on 2026-09-09; the 720 writing column is
  // max-w-180 on the spacing scale.
  "modules/notes": { twText: 0, rawPx: 0, rawHex: 0 },
  // Converted 2026-09-09 against Figma 11 Settings. The 9 remaining rawPx are
  // the modal's own frame (620×940, a 220 nav, 656 content columns) and the
  // two blur radii — measures of the modal, not steps on the scale.
  // 6 → 3: Settings is a Dialog, and its hand-drawn scrim and shadow went.
  // 3 → 0: the frame is w-55 / max-w-164 / h-settings, a named measure.
  "modules/settings": { twText: 0, rawPx: 0, rawHex: 0 },
  "modules/ssh": { twText: 0, rawPx: 0, rawHex: 0 },
  "modules/sync": { twText: 0, rawPx: 0, rawHex: 0 },
  // Converted 2026-09-30 with the board/detail rewrite: the last Tailwind
  // text sizes and the native select in the verify dialog went with it.
  "modules/tasks": { twText: 0, rawPx: 0, rawHex: 0 },
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
  // 78 → 0, rawHex 1 → 0 on 2026-09-27 with shadcn/no-arbitrary-values:
  // on-grid px became spacing steps, the rest named tokens in index.css
  // (shadow-glass-edge, backdrop-blur-float, max-w-command…), and the static
  // code block took the editor's token classes.
  components: { twText: 0, rawPx: 0, rawHex: 0 },
  // 33 → 24: nine `rounded-[8px]` went with the nine copies of the title
  // bar's action button, which is Button's `titlebar` variant now. --radius-md
  // is that 8px, so the scale had the number the whole time. 24 → 19: the
  // dock went, and modules switch from title-bar Buttons. 9 → 0 (and rawHex
  // 1 → 0) on 2026-09-27 with shadcn/no-arbitrary-values.
  workbench: { twText: 0, rawPx: 0, rawHex: 0 },
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

/**
 * The scale stops a component drawing an off-grid value. It cannot stop a
 * module drawing an on-grid copy of a shared component — and that is how the
 * empty states drifted: every rail assembled its own from the `Empty` slots,
 * at four paddings with three title treatments, and every module picked its
 * own button for the call to action (the craft pill in one, a plain primary in
 * the next, nothing at all in a third).
 *
 * `EmptyState` is the one empty state now, in a `pane` and a `rail` size, and
 * it decides which button an action gets. These are the two ways around it,
 * and both are closed: the `Empty` slots and the craft button are its parts,
 * not things a module puts together.
 */
describe("shared components", () => {
  const PARTS: Record<string, { pattern: RegExp; owners: string[] }> = {
    "the Empty slots (use EmptyState)": {
      pattern: /from "@\/components\/ui\/empty"/,
      owners: ["components/ui/empty-state.tsx"],
    },
    "the craft button (use EmptyState's action)": {
      pattern: /from "@\/components\/ui\/craft-button"/,
      // The gallery draws every component on its own, which is its job.
      owners: ["components/ui/empty-state.tsx", "dev/ComponentGallery.tsx"],
    },
  };

  for (const [part, { pattern, owners }] of Object.entries(PARTS)) {
    test(`only its owner imports ${part}`, () => {
      const offenders = tsxFiles(ROOT)
        .map((file) => file.slice(ROOT.length + 1))
        .filter((relative) => !owners.includes(relative))
        .filter((relative) => pattern.test(readFileSync(join(ROOT, relative), "utf8")));
      expect(offenders).toEqual([]);
    });
  }
});

/**
 * A ⌘ written into the source reaches Linux and Windows as a key nobody's
 * keyboard has. Notes, Tasks, Git, Sync and the API client all did it, while
 * the palette beside them said "Ctrl+5". Labels go through lib/shortcut —
 * `shortcut()`, `chordLabel()`, or a key chip (Kbd, a menu's `shortcut=`),
 * which spells a glyph label for the platform itself.
 *
 * What is left is a line that already branches on the platform (`isMac()`,
 * `modKey() === "⌘"`) or hands the label to a key chip.
 */
describe("shortcut labels", () => {
  const ALLOWED = [/<Kbd\b/, /\bshortcut[=:]/, /=== "⌘"/, /\bisMac\(\)/];

  test("no surface writes ⌘ where the platform should decide", () => {
    const offenders: string[] = [];
    for (const surface of ["modules", "workbench", "components"]) {
      for (const file of tsxFiles(join(ROOT, surface))) {
        strip(readFileSync(file, "utf8"))
          .split("\n")
          .forEach((line, index) => {
            if (line.includes("⌘") && !ALLOWED.some((pattern) => pattern.test(line))) {
              offenders.push(`${file.slice(ROOT.length + 1)}:${index + 1}: ${line.trim()}`);
            }
          });
      }
    }
    expect(offenders).toEqual([]);
  });
});
