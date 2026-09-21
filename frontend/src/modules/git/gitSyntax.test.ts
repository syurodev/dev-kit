import { describe, expect, it } from "bun:test";
import { highlightPatch } from "./gitSyntax";
import { parsePatch } from "./hunks";

const JAVA_PATCH = [
  "diff --git a/A.java b/A.java\n",
  "index 000..111 100644\n",
  "--- a/A.java\n",
  "+++ b/A.java\n",
  "@@ -10,3 +10,4 @@ class A {\n",
  " int x = 1;\n",
  "-int y = 2;\n",
  "+int y = 3;\n",
  "+int z = 4;\n",
  " int w = 5;\n",
].join("");

describe("highlightPatch", () => {
  it("keys segments by the file's own absolute line number, per side", async () => {
    const highlight = await highlightPatch(parsePatch(JAVA_PATCH), "A.java");
    expect(highlight).not.toBeNull();
    // Hunk starts at before=10 (1-based) -> 9 zero-based; "int x = 1;" is the
    // hunk's first (context) line, on both sides.
    expect(highlight!.before.has(9)).toBe(true);
    expect(highlight!.after.has(9)).toBe(true);
    // "int y = 2;" (removed) and "int y = 3;" (added) land at the same
    // absolute line number, 10 (zero-based) — each side keys its own map, so
    // that number holds a different line's segments on each.
    const removedText = highlight!.before.get(10)!.map((s) => s.text).join("");
    const addedText = highlight!.after.get(10)!.map((s) => s.text).join("");
    expect(removedText).toBe("int y = 2;");
    expect(addedText).toBe("int y = 3;");
  });

  it("colours a real keyword, not just splits text", async () => {
    const highlight = await highlightPatch(parsePatch(JAVA_PATCH), "A.java");
    const line = highlight!.after.get(9)!; // "int x = 1;"
    expect(line.length).toBeGreaterThan(1);
    expect(line.some((segment) => segment.className && segment.text === "int")).toBe(true);
  });

  it("gives up without a grammar for the path, rather than throwing", async () => {
    const highlight = await highlightPatch(parsePatch(JAVA_PATCH), "A.unknownext");
    expect(highlight).toBeNull();
  });

  it("gives up on a binary patch", async () => {
    const binary = parsePatch("diff --git a/A.png b/A.png\nBinary files a/A.png and b/A.png differ\n");
    expect(await highlightPatch(binary, "A.png")).toBeNull();
  });
});
