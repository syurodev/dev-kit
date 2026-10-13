import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

/**
 * A diff keeps its status colours: added is --status-success, removed is
 * --status-danger, as in code blocks and the terminal's git diff. In the Mono
 * chrome --brand-text is plain text colour, so an added line drawn with it
 * would not read as added. `brand-lime` is not a defined utility at all.
 */
const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");

function sources(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return entry.name === "node_modules" ? [] : sources(path);
    return /\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name) ? [path] : [];
  });
}

describe("diff colours", () => {
  test("the shared diff does not colour added lines with brand-text", () => {
    const source = strip(readFileSync(join(import.meta.dir, "components/ui/diff.tsx"), "utf8"));
    expect(source).not.toContain("brand-text");
    expect(source).toContain("--status-success");
    expect(source).toContain("--feedback-success-soft");
  });

  test("the file diff pane counts added lines as success", () => {
    const source = strip(
      readFileSync(join(import.meta.dir, "modules/agenthub/surfaces/FileDiffPane.tsx"), "utf8"),
    );
    expect(source).not.toContain("brand-text");
  });

  test("no source uses the undefined brand-lime utility", () => {
    const offenders = sources(import.meta.dir).filter((file) =>
      readFileSync(file, "utf8").includes("brand-lime"),
    );
    expect(offenders.map((file) => file.slice(import.meta.dir.length + 1))).toEqual([]);
  });
});
