import { describe, expect, it } from "bun:test";
import { nestFiles } from "./treeNodes";

describe("nestFiles", () => {
  it("nests by path, folders before files at every level", () => {
    const tree = nestFiles([
      { path: "pom.xml", isDir: false },
      { path: "src", isDir: true },
      { path: "src/App.java", isDir: false },
      { path: "src/web", isDir: true },
      { path: "src/web/Api.java", isDir: false },
    ]);
    expect(tree.map((n) => n.name)).toEqual(["src", "pom.xml"]);
    expect(tree[0].children.map((n) => n.name)).toEqual(["web", "App.java"]);
    expect(tree[0].children[0].children[0].path).toBe("src/web/Api.java");
  });
});
