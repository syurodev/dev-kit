import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { markdownNorm } from "./markdownNorm";
import { losesText, matchLines, spliceMarkdown, splitBlocks } from "./markdownSplice";

const norm = markdownNorm();
const splice = (src: string, edited: string) => spliceMarkdown(src, edited, norm).text;

describe("splitBlocks", () => {
  test("splits on blank lines and keeps a fence whole", () => {
    const lines = ["# T", "", "a", "b", "", "```", "x", "", "y", "```", "", "tail"];
    expect(splitBlocks(lines)).toEqual([
      { start: 0, end: 1 },
      { start: 2, end: 4 },
      { start: 5, end: 10 },
      { start: 11, end: 12 },
    ]);
  });

  test("a longer fence is closed only by an equal or longer one", () => {
    const lines = ["````", "```", "", "````", "after"];
    expect(splitBlocks(lines)).toEqual([{ start: 0, end: 5 }]);
  });
});

describe("matchLines", () => {
  test("matches the common start, end and the sequence between", () => {
    const m = matchLines(["a", "b", "c", "d", "e"], ["a", "x", "c", "e"]);
    expect(Array.from(m)).toEqual([0, -1, 2, -1, 3]);
  });
});

// Changed lines as `diff` counts them (matchLines gives up on a very large middle, which would flatter nothing).
const lineDiff = (a: string, b: string) => {
  const dir = mkdtempSync(join(tmpdir(), "splice-"));
  writeFileSync(join(dir, "a"), a);
  writeFileSync(join(dir, "b"), b);
  const out = spawnSync("diff", [join(dir, "a"), join(dir, "b")], { encoding: "utf8", maxBuffer: 1 << 28 }).stdout;
  rmSync(dir, { recursive: true });
  return out.split("\n").filter((l) => /^[<>]/.test(l)).length;
};

describe("spliceMarkdown", () => {
  const src = [
    "# Title",
    "",
    "A hard-wrapped paragraph that goes on",
    "over two lines.",
    "",
    "| a | b |",
    "|---|---|",
    "| 1 | 2 |",
    "",
    "* star item",
    "* second",
    "",
    "Last paragraph.",
    "",
  ].join("\n");

  test("an unchanged document comes back byte for byte", () => {
    expect(splice(src, norm(src))).toBe(src);
  });

  test("an edit rewrites its own block and leaves every other byte alone", () => {
    const edited = norm(src).replace("Last paragraph.", "Last paragraph, edited.");
    const out = splice(src, edited);
    expect(out).toBe(src.replace("Last paragraph.", "Last paragraph, edited."));
  });

  test("editing a hard-wrapped paragraph reformats only that paragraph", () => {
    const edited = norm(src).replace("goes on\\\nover", "goes on\\\nright over");
    const out = splice(src, edited);
    expect(out).toContain("|---|---|"); // the table keeps the way it was written
    expect(out).toContain("* star item"); // and so do the list markers
    expect(out.startsWith("# Title\n\nA hard-wrapped paragraph")).toBe(true);
    expect(norm(out).trim()).toBe(edited.trim());
  });

  test("a new paragraph between blocks is inserted with the blank lines around it", () => {
    const edited = norm(src).replace("Last paragraph.", "Inserted.\n\nLast paragraph.");
    const out = splice(src, edited);
    expect(out).toBe(src.replace("Last paragraph.", "Inserted.\n\nLast paragraph."));
  });

  test("a deleted block takes its blank line with it", () => {
    const edited = norm(src).replace("Last paragraph.", "").replace(/\n+$/, "\n");
    const out = splice(src, edited);
    expect(out.includes("Last paragraph")).toBe(false);
    expect(out.startsWith("# Title")).toBe(true);
    expect(out).toContain("* second");
  });

  test("adding an item to a list rewrites that list only", () => {
    const edited = norm(src).replace("* second", "* second\n* third");
    const out = splice(src, edited);
    expect(out).toContain("|---|---|");
    expect(out).toContain("third");
  });

  test("whatever happens, the result reads as what the editor holds", () => {
    const edited = norm(src).replace("# Title", "## Other title").replace("| 1 | 2 |", "| 1 | 3 |");
    const out = splice(src, edited);
    expect(norm(out).trim()).toBe(norm(edited).trim());
  });

  test("a file with blank lines before and after, and CRLF-free trailing newlines, keeps them", () => {
    const s = "\n\nOne.\n\n\nTwo.\n\n";
    const out = splice(s, norm(s).replace("Two.", "Two!"));
    expect(out).toBe("\n\nOne.\n\n\nTwo!\n\n");
  });
});

// The repo's own Markdown, hard-wrapped and padded the way people write it: one word added to one
// paragraph must come back as a change of about that paragraph, not of the file.
describe("on the repository's own Markdown", () => {
  const root = join(import.meta.dir, "../../../../..");
  const files: string[] = [];
  const walk = (dir: string) => {
    for (const n of readdirSync(dir)) {
      if (n === "node_modules" || n.startsWith(".git")) continue;
      const p = join(dir, n);
      if (statSync(p).isDirectory()) walk(p);
      else if (/\.md$/i.test(n)) files.push(p);
    }
  };
  walk(join(root, "docs"));
  files.push(join(root, "AGENTS.md"));

  test("Plate's text loss is detected, and a one-word edit elsewhere changes only its own block", () => {
    const changedLines: Array<[number, string]> = [];
    const reformatted: string[] = [];
    let lossy = 0;
    for (const f of files) {
      const text = readFileSync(f, "utf8");
      // A file Plate cannot write back without losing text is not edited visually at all.
      if (losesText(text, norm)) {
        lossy++;
        continue;
      }
      const base = norm(text);
      // One word added to the first plain paragraph of the editor's output.
      const edited = base.replace(/^([A-Za-z][^\n]*[a-z.])$/m, "$1 EDITED");
      if (edited === base) continue;
      const out = spliceMarkdown(text, edited, norm);
      expect(out.text.includes("EDITED")).toBe(true);
      if (out.reformatted) reformatted.push(f.replace(root, ""));
      else changedLines.push([lineDiff(text, out.text), f.replace(root, "")]);
    }
    changedLines.sort((a, b) => b[0] - a[0]);
    const median = changedLines[Math.floor(changedLines.length / 2)][0];
    console.log(
      `files ${files.length}: Plate loses text in ${lossy}; spliced ${changedLines.length} (median ${median} changed lines, worst ${JSON.stringify(changedLines.slice(0, 3))}); reformatted whole file ${reformatted.length}`,
    );
    expect(files.length - lossy).toBeGreaterThan(30);
    // Most files come back with the edit and little else; the rest fall back to Plate's own formatting, never to a lost edit.
    expect(median).toBeLessThan(10);
    expect(reformatted.length).toBeLessThan((files.length - lossy) * 0.2);
  }, 120000);
});
