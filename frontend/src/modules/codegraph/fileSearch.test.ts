import { describe, expect, it } from "bun:test";
import { filterFileEntries, visibleEntries } from "./fileSearch";

const tree = [
  { path: "src", isDir: true },
  { path: "src/main", isDir: true },
  { path: "src/main/UserService.java", isDir: false },
  { path: "src/main/OrderService.java", isDir: false },
  { path: "pom.xml", isDir: false },
];

describe("filterFileEntries", () => {
  it("returns everything, and forces nothing open, without a query", () => {
    const r = filterFileEntries(tree, "  ");
    expect(r.entries).toBe(tree);
    expect(r.forceExpanded).toBeNull();
  });

  it("keeps a nested match's ancestor folders and forces them open", () => {
    const r = filterFileEntries(tree, "userservice");
    expect(r.entries.map((e) => e.path)).toEqual(["src", "src/main", "src/main/UserService.java"]);
    expect([...r.forceExpanded!].sort()).toEqual(["src", "src/main"]);
    // The point of the fix: the match is actually visible in the tree.
    const shown = visibleEntries(r.entries, r.forceExpanded!);
    expect(shown.map((e) => e.path)).toContain("src/main/UserService.java");
  });

  it("drops what neither matches nor contains a match", () => {
    const r = filterFileEntries(tree, "pom");
    expect(r.entries.map((e) => e.path)).toEqual(["pom.xml"]);
    expect(r.forceExpanded!.size).toBe(0);
  });
});

describe("file toggles", () => {
  it("a toggle keeps matching files and the folders that hold them", () => {
    const entries = [
      { path: "src", isDir: true },
      { path: "src/A.java", isDir: false },
      { path: "src/b.xml", isDir: false },
    ];
    const r = filterFileEntries(entries, "", (p) => p.endsWith(".java"));
    expect(r.entries.map((e) => e.path)).toEqual(["src", "src/A.java"]);
    expect(r.forceExpanded).toBeNull();
  });
});
