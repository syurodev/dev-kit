import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { spacingPx, DOCX_CODE_METRICS } from "./design-metrics";

const root = join(import.meta.dir, "..");
const css = readFileSync(join(root, "index.css"), "utf8");

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    return entry.isDirectory() ? sources(path)
      : /\.(tsx?|css)$/.test(path) && !path.includes(".test.") ? [path] : [];
  });
}

describe("shared design metrics", () => {
  test("numeric API fallbacks agree with the CSS spacing tokens", () => {
    for (const name of ["inline", "control"] as const) {
      const value = Number(css.match(new RegExp(`--space-${name}: (\\d+)px;`))?.[1]);
      expect(spacingPx(name)).toBe(value);
    }
  });

  test("Word export still receives literal point values", () => {
    expect(DOCX_CODE_METRICS).toEqual({ margin: "8pt 0", padding: "12pt", fontSize: "10pt" });
  });

  test("style consumers do not embed spacing, font-size or radius dimensions", () => {
    const violations: string[] = [];
    for (const path of sources(root)) {
      if (path.endsWith("design-metrics.ts")) continue; // Numeric/Word adapters.
      const source = readFileSync(path, "utf8")
        .replace(/\/\*[\s\S]*?\*\//g, "")
        .replace(/@theme inline\s*\{[\s\S]*?\n\}/g, ""); // Token definitions.
      const arbitrary = /\b(?:rounded(?:-[trblse]{1,2})?|[pm][xytrblse]?|gap(?:-[xy])?|text|leading|tracking)-\[(?:-?[\d.]+(?:px|rem|em)|(?:calc|max|min)\([^\]]*\d+(?:px|rem|em))[^\]]*\]/g;
      const literal = /\b(?:font-size|line-height|border-radius|letter-spacing|padding(?:-[a-z]+)?|margin(?:-[a-z]+)?|gap|fontSize|lineHeight|borderRadius|padding(?:Left|Right|Top|Bottom)?|margin(?:Left|Right|Top|Bottom)?)\s*:\s*["']?-?[\d.]+(?:px|rem|em|ch|pt)\b/g;
      for (const match of [...source.matchAll(arbitrary), ...source.matchAll(literal)]) {
        violations.push(`${relative(root, path)}: ${match[0]}`);
      }
    }
    expect(violations).toEqual([]);
  });
});
