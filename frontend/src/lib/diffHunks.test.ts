import { describe, expect, it } from "bun:test";
import { foldContext } from "./diffHunks";
import { lineDiff } from "@/modules/notes/noteDiff";

describe("foldContext", () => {
  it("keeps three lines around a change and folds the rest", () => {
    const before = Array.from({ length: 20 }, (_, i) => `line ${i}`).join("\n");
    const after = before.replace("line 10", "LINE 10");
    const rows = foldContext(lineDiff(before, after));
    expect(rows[0]).toEqual({ kind: "gap", count: 7 });
    expect(rows.filter((r) => r.kind !== "gap")).toHaveLength(8); // 3 + removed + added + 3
    expect(rows[rows.length - 1]).toEqual({ kind: "gap", count: 6 });
  });

  it("folds nothing when nothing is far enough from a change", () => {
    const rows = foldContext(lineDiff("a\nb", "a\nc"));
    expect(rows.some((r) => r.kind === "gap")).toBe(false);
  });
});

describe("wholeFileDiff", () => {
  it("matches the shared head and tail before diffing the middle", async () => {
    const { wholeFileDiff } = await import("./diffHunks");
    let seen = "";
    const spy = (a: string, b: string) => {
      seen = `${a}|${b}`;
      return lineDiff(a, b);
    };
    const before = ["a", "b", "c", "d", "e"].join("\n");
    const after = ["a", "b", "X", "d", "e"].join("\n");
    const rows = wholeFileDiff(before, after, spy);
    expect(seen).toBe("c|X");
    expect(rows.map((r) => r.kind)).toEqual(["same", "same", "removed", "added", "same", "same"]);
  });

  it("handles a pure insertion without the LCS", async () => {
    const { wholeFileDiff } = await import("./diffHunks");
    const rows = wholeFileDiff("a\nc", "a\nb\nc", () => {
      throw new Error("should not diff");
    });
    expect(rows.map((r) => `${r.kind}:${r.text}`)).toEqual(["same:a", "added:b", "same:c"]);
  });
});

describe("sourceLines", () => {
  it("numbers each line by the side it came from", async () => {
    const { sourceLines } = await import("./diffHunks");
    const rows = sourceLines(lineDiff("a\nb\nc", "a\nB\nc"));
    expect(rows.map((r) => `${r.kind}:${r.before}:${r.after}`)).toEqual([
      "same:0:0",
      "removed:1:-1",
      "added:-1:1",
      "same:2:2",
    ]);
  });
});

describe("splitPairs", () => {
  it("faces a replaced line and its replacement across the gutter", async () => {
    const { splitPairs, sourceLines } = await import("./diffHunks");
    const pairs = splitPairs(sourceLines(lineDiff("a\nb\nc", "a\nB\nc")));
    expect(pairs.map((p) => [p.left?.text ?? null, p.right?.text ?? null])).toEqual([
      ["a", "a"],
      ["b", "B"],
      ["c", "c"],
    ]);
  });

  it("leaves a blank opposite the tail of the longer run", async () => {
    const { splitPairs, sourceLines } = await import("./diffHunks");
    const pairs = splitPairs(sourceLines(lineDiff("a", "a\nb\nc")));
    expect(pairs.map((p) => [p.left?.text ?? null, p.right?.text ?? null])).toEqual([
      ["a", "a"],
      [null, "b"],
      [null, "c"],
    ]);
  });
});

describe("changedRuns", () => {
  it("groups touching changed rows and names what happened to them", async () => {
    const { changedRuns, splitPairs, sourceLines } = await import("./diffHunks");
    const before = "keep\nold1\nold2\nkeep2\nkeep3";
    const after = "keep\nnew1\nnew2\nkeep2\nextra\nkeep3";
    const runs = changedRuns(splitPairs(sourceLines(lineDiff(before, after))));
    expect(runs).toEqual([
      { start: 1, end: 3, kind: "modified" },
      { start: 4, end: 5, kind: "added" },
    ]);
  });
});
