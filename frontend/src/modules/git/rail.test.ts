import { describe, expect, test } from "bun:test";
import { capRows, changeGroups, splitPath } from "./rail";

describe("changeGroups", () => {
  test("a clean tree has no groups", () => {
    expect(changeGroups({ conflicted: null, staged: null, unstaged: null, untracked: null })).toEqual([]);
  });

  test("conflicts come first, then staged, changed and untracked, and empty groups are left out", () => {
    const groups = changeGroups({
      conflicted: ["c.ts"],
      staged: null,
      unstaged: ["b.ts"],
      untracked: ["a.ts", "z.ts"],
    });
    expect(groups.map((g) => [g.key, g.files.length])).toEqual([
      ["conflicted", 1],
      ["unstaged", 1],
      ["untracked", 2],
    ]);
  });
});

describe("splitPath", () => {
  test("separates the file name from its folder", () => {
    expect(splitPath("src/app/main.ts")).toEqual({ name: "main.ts", dir: "src/app" });
    expect(splitPath("README.md")).toEqual({ name: "README.md", dir: "" });
  });
});

describe("capRows", () => {
  test("keeps a short list whole and says how many a long one dropped", () => {
    expect(capRows([1, 2, 3], 5)).toEqual({ shown: [1, 2, 3], hidden: 0 });
    expect(capRows([1, 2, 3, 4, 5, 6], 4)).toEqual({ shown: [1, 2, 3, 4], hidden: 2 });
  });
});
