import { describe, expect, it } from "bun:test";
import { buildPatch, parsePatch } from "./hunks";

const TWO_HUNKS = `diff --git a/many.txt b/many.txt
index 9e040f2..1390cbb 100644
--- a/many.txt
+++ b/many.txt
@@ -1,5 +1,5 @@
 1
-2
+TOP
 3
 4
@@ -11,5 +11,5 @@
 11
 12
 13
-14
+BOTTOM
 15
`;

describe("parsePatch", () => {
  it("splits the file header from the hunks", () => {
    const patch = parsePatch(TWO_HUNKS);
    expect(patch.hunks).toHaveLength(2);
    expect(patch.head.join("")).toContain("diff --git a/many.txt");
    // The header stops at the first @@ — "+++" belongs to it, not to a hunk.
    expect(patch.head.join("")).toContain("+++ b/many.txt");
    expect(patch.hunks[0].header).toBe("@@ -1,5 +1,5 @@\n");
  });

  it("counts changed lines without counting the +++/--- header", () => {
    const patch = parsePatch(TWO_HUNKS);
    expect(patch.hunks[0].added).toBe(1);
    expect(patch.hunks[0].removed).toBe(1);
  });

  it("reports a binary file as having nothing to pick", () => {
    const patch = parsePatch(
      "diff --git a/blob.bin b/blob.bin\nindex 1..2 100644\nBinary files a/blob.bin and b/blob.bin differ\n",
    );
    expect(patch.binary).toBe(true);
    expect(patch.hunks).toHaveLength(0);
  });

  it("treats an empty patch as nothing to pick, not an error", () => {
    // An untracked file: git does not diff it at all.
    expect(parsePatch("").hunks).toHaveLength(0);
    expect(parsePatch("").binary).toBe(false);
  });

  it("keeps a no-newline marker with its hunk", () => {
    const patch = parsePatch(
      `diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-one\n+two\n\\ No newline at end of file\n`,
    );
    expect(patch.hunks[0].lines.join("")).toContain("\\ No newline at end of file");
    // The marker is neither an addition nor a removal.
    expect(patch.hunks[0].added).toBe(1);
    expect(patch.hunks[0].removed).toBe(1);
  });
});

describe("buildPatch", () => {
  it("rebuilds one hunk under the original file header, byte for byte", () => {
    const patch = parsePatch(TWO_HUNKS);
    const built = buildPatch(patch, [0]);
    expect(built).toBe(
      `diff --git a/many.txt b/many.txt
index 9e040f2..1390cbb 100644
--- a/many.txt
+++ b/many.txt
@@ -1,5 +1,5 @@
 1
-2
+TOP
 3
 4
`,
    );
    // The hunk that was not chosen must not appear anywhere.
    expect(built).not.toContain("BOTTOM");
  });

  it("keeps the hunks in file order however they were ticked", () => {
    const patch = parsePatch(TWO_HUNKS);
    const built = buildPatch(patch, [1, 0]);
    expect(built.indexOf("TOP")).toBeLessThan(built.indexOf("BOTTOM"));
  });

  it("ignores duplicates and indexes that do not exist", () => {
    const patch = parsePatch(TWO_HUNKS);
    expect(buildPatch(patch, [0, 0, 9, -1])).toBe(buildPatch(patch, [0]));
  });

  it("returns nothing for an empty selection rather than a headless patch", () => {
    // git apply rejects a file header with no hunks, and "nothing ticked" is
    // not an error to put on screen.
    expect(buildPatch(parsePatch(TWO_HUNKS), [])).toBe("");
  });

  it("returns the whole patch unchanged when every hunk is ticked", () => {
    // The invariant the file is built on: nothing is rewritten. Ticking
    // everything must produce exactly what git handed over.
    const patch = parsePatch(TWO_HUNKS);
    expect(buildPatch(patch, [0, 1])).toBe(TWO_HUNKS);
  });

  it("loses no bytes when the last line has no newline of its own", () => {
    const text = TWO_HUNKS.trimEnd();
    const patch = parsePatch(text);
    // Only the newline git apply insists on is added.
    expect(buildPatch(patch, [0, 1])).toBe(text + "\n");
  });

  it("always ends with a newline", () => {
    const patch = parsePatch(
      `diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n-one\n+two`,
    );
    expect(buildPatch(patch, [0].slice()).endsWith("\n")).toBe(true);
  });
});
