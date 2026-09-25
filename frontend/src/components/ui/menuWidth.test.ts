import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

/**
 * A menu's items are at least 200px wide (MENU_ROW's `min-w-[200px]`), and
 * the content pads them by 4px a side (`p-1`). A menu content given a fixed
 * width below 208px cannot hold them: the rows overflow, and the content's
 * `overflow-x-hidden` cuts off their end — the check that marks the current
 * choice, a shortcut, the last letters of a label. The agent picker in the
 * composer was `w-48` (192px) and showed half a check as a stray "⌄" beside
 * Claude. Leave the width unset (the rows size the menu) or make it wider.
 */
const ROW_MIN_PX = 200 + 2 * 4;

/**
 * Menus known to be narrower, file → reason. A ratchet, not a pass: each one
 * is a menu to fix, and the entry goes when it is.
 */
const KNOWN_NARROW: Record<string, string> = {
  // The notes editor's Table menu: 180px around sub-triggers that are 200px
  // wide. Plate's own sizing, not checked on screen yet — fix separately.
  "components/ui/table-toolbar-button.tsx": "plate table menu, 180px",
};
const SRC = join(import.meta.dir, "..", "..");
const CONTENT_TAG = /<(DropdownMenuContent|ContextMenuContent|DropdownMenuSubContent|ContextMenuSubContent)\b([^>]*)>/g;

function sources(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      if (entry !== "node_modules") out.push(...sources(path));
    } else if (entry.endsWith(".tsx") && !entry.includes(".test.")) {
      out.push(path);
    }
  }
  return out;
}

/** The fixed widths a className sets, in px: `w-48` → 192, `w-[180px]` → 180. */
function fixedWidthsPx(className: string): number[] {
  const widths: number[] = [];
  for (const token of className.split(/\s+/)) {
    const scale = /^w-(\d+(?:\.\d+)?)$/.exec(token);
    if (scale) widths.push(Number(scale[1]) * 4);
    const px = /^w-\[(\d+)px\]$/.exec(token);
    if (px) widths.push(Number(px[1]));
  }
  return widths;
}

describe("menu content width", () => {
  test("reads the widths Tailwind would set", () => {
    expect(fixedWidthsPx("w-48")).toEqual([192]);
    expect(fixedWidthsPx("z-50 w-[180px] min-w-32")).toEqual([180]);
    expect(fixedWidthsPx("w-64 max-w-80")).toEqual([256]);
  });

  test("no menu is narrower than the rows it holds", () => {
    const narrow: string[] = [];
    for (const file of sources(SRC)) {
      if (KNOWN_NARROW[file.slice(SRC.length + 1)]) continue;
      const body = readFileSync(file, "utf8");
      for (const match of body.matchAll(CONTENT_TAG)) {
        const className = /className="([^"]*)"/.exec(match[2])?.[1] ?? "";
        for (const px of fixedWidthsPx(className)) {
          if (px < ROW_MIN_PX) narrow.push(`${file.slice(SRC.length + 1)}: <${match[1]}> ${px}px ("${className}")`);
        }
      }
    }
    expect(narrow, `menus narrower than ${ROW_MIN_PX}px clip their rows' end`).toEqual([]);
  });

  test("the known-narrow list is not stale", () => {
    const stale = Object.keys(KNOWN_NARROW).filter((file) => {
      const body = readFileSync(join(SRC, file), "utf8");
      return ![...body.matchAll(CONTENT_TAG)].some((match) =>
        fixedWidthsPx(/className="([^"]*)"/.exec(match[2])?.[1] ?? "").some((px) => px < ROW_MIN_PX),
      );
    });
    expect(stale, "these menus are wide enough now; drop them from KNOWN_NARROW").toEqual([]);
  });
});
