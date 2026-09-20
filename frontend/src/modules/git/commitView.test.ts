import { describe, expect, test } from "bun:test";
import { changeSummary, commitBody, patchLines } from "./commitView";
import { parsePatch } from "./hunks";

const PATCH = [
  "diff --git a/a.ts b/a.ts",
  "index 1..2 100644",
  "--- a/a.ts",
  "+++ b/a.ts",
  "@@ -10,3 +10,3 @@ fn",
  " keep",
  "-old",
  "+new",
  " tail",
  "@@ -40,2 +40,3 @@",
  " x",
  "+added",
  " y",
  "\\ No newline at end of file",
  "",
].join("\n");

describe("patchLines", () => {
  const lines = patchLines(parsePatch(PATCH));

  test("numbers each line in the file it came from, on either side", () => {
    expect(lines[0]).toEqual({ kind: "same", text: "keep", before: 9, after: 9 });
    expect(lines[1]).toEqual({ kind: "removed", text: "old", before: 10, after: -1 });
    expect(lines[2]).toEqual({ kind: "added", text: "new", before: -1, after: 10 });
    expect(lines[3]).toEqual({ kind: "same", text: "tail", before: 11, after: 11 });
  });

  test("starts each hunk where its header says, so a gap between them shows in the numbers", () => {
    expect(lines[4]).toEqual({ kind: "same", text: "x", before: 39, after: 39 });
    expect(lines[5]).toEqual({ kind: "added", text: "added", before: -1, after: 40 });
  });

  test("leaves out git's notes about the file", () => {
    expect(lines.some((line) => line.text.includes("No newline"))).toBe(false);
    expect(lines).toHaveLength(7);
  });

  test("a binary file has no lines", () => {
    expect(patchLines({ head: [], hunks: [], binary: true })).toEqual([]);
  });
});

describe("commitBody", () => {
  test("takes the task trailer out of the text and names it", () => {
    expect(commitBody("Buffer the writes.\n\nTask: REF-3a9f21")).toEqual({ text: "Buffer the writes.", task: "REF-3a9f21" });
  });

  test("a message with no trailer is left as it is", () => {
    expect(commitBody("Just prose.")).toEqual({ text: "Just prose.", task: "" });
    expect(commitBody("")).toEqual({ text: "", task: "" });
  });
});

describe("changeSummary", () => {
  test("totals the lines and pluralises the files", () => {
    expect(
      changeSummary([
        { added: 21, removed: 4 },
        { added: 5, removed: 1 },
      ]),
    ).toBe("2 files changed · +26 −5");
    expect(changeSummary([{ added: 3, removed: 0 }])).toBe("1 file changed · +3 −0");
  });

  test("does not count a binary file's lines, which git reports as -1", () => {
    expect(changeSummary([{ added: -1, removed: -1 }, { added: 2, removed: 1 }])).toBe("2 files changed · +2 −1");
  });
});
