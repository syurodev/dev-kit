import { describe, expect, it } from "bun:test";
import { lineDiff } from "./noteDiff";

const kinds = (before: string, after: string) => lineDiff(before, after).map((l) => l.kind);

describe("lineDiff", () => {
  it("marks only what changed", () => {
    const out = lineDiff("a\nb\nc", "a\nB\nc");
    expect(out).toEqual([
      { kind: "same", text: "a" },
      { kind: "removed", text: "b" },
      { kind: "added", text: "B" },
      { kind: "same", text: "c" },
    ]);
  });

  // The reason for the LCS walk. Comparing line by line would call every line
  // after an insertion both removed and added, and a history that highlights
  // the whole note tells you nothing about what changed.
  it("does not repaint the whole note when a line is inserted at the top", () => {
    expect(kinds("a\nb\nc", "new\na\nb\nc")).toEqual(["added", "same", "same", "same"]);
  });

  it("handles a line appended at the end", () => {
    expect(kinds("a\nb", "a\nb\nc")).toEqual(["same", "same", "added"]);
  });

  it("reports two identical bodies as unchanged", () => {
    expect(kinds("a\nb", "a\nb")).toEqual(["same", "same"]);
  });

  it("survives an empty side", () => {
    expect(kinds("", "a")).toEqual(["removed", "added"]);
  });
});
