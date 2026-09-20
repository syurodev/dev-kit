import { describe, expect, test } from "bun:test";
import { branchMeta, capRows, changeGroups, changeMarkKind, markKind, splitPath, syncText } from "./rail";

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

describe("markKind", () => {
  test("an untracked file is not marked as added, though git calls its state A", () => {
    expect(markKind("untracked", "A")).toBe("untracked");
    expect(markKind("staged", "A")).toBe("added");
  });

  test("a conflict outranks whatever letter it carries", () => {
    expect(markKind("conflicted", "M")).toBe("conflict");
  });

  test("reads the letter for a tracked file, and calls the rest modified", () => {
    expect(markKind("unstaged", "D")).toBe("deleted");
    expect(markKind("unstaged", "R")).toBe("renamed");
    expect(markKind("unstaged", "M")).toBe("modified");
    expect(markKind("unstaged", undefined)).toBe("modified");
  });
});

describe("changeMarkKind", () => {
  test("maps a commit's change kinds, folding a type change into modified", () => {
    expect(changeMarkKind("added")).toBe("added");
    expect(changeMarkKind("deleted")).toBe("deleted");
    expect(changeMarkKind("typechange")).toBe("modified");
  });
});

describe("branchMeta", () => {
  test("names the branch and how far it is from its upstream", () => {
    expect(branchMeta({ branch: "dev", detached: false, ahead: 2, behind: 0 })).toBe("dev · ↑2 ahead");
    expect(branchMeta({ branch: "dev", detached: false, ahead: 1, behind: 3 })).toBe("dev · ↑1 ahead · ↓3 behind");
    expect(branchMeta({ branch: "main", detached: false, ahead: 0, behind: 0 })).toBe("main");
  });

  test("says so when there is no branch to name", () => {
    expect(branchMeta({ branch: "", detached: true, ahead: 0, behind: 0 })).toBe("detached HEAD");
    expect(branchMeta({ branch: "", detached: false, ahead: 0, behind: 0 })).toBe("no commits");
  });
});

describe("syncText", () => {
  test("is empty for a branch level with its upstream", () => {
    expect(syncText({ ahead: 0, behind: 0 })).toBe("");
    expect(syncText({ ahead: 0, behind: 2 })).toBe("↓2 behind");
  });
});
