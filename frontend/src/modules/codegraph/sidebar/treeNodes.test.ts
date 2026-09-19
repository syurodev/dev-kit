import { describe, expect, it } from "bun:test";
import { compactFiles, nestFiles } from "./treeNodes";

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

describe("compactFiles", () => {
  const dirs = (...paths: string[]) => paths.map((path) => ({ path, isDir: true }));
  const tree = () =>
    compactFiles(
      nestFiles([
        ...dirs("gateway", "gateway/src", "gateway/src/main", "gateway/src/main/java", "gateway/src/main/java/com"),
        ...dirs("gateway/src/main/java/com/synx", "gateway/src/main/java/com/synx/devkit"),
        ...dirs("gateway/src/main/java/com/synx/devkit/gateway", "gateway/src/main/java/com/synx/devkit/gateway/web"),
        { path: "gateway/src/main/java/com/synx/devkit/gateway/App.java", isDir: false },
        { path: "gateway/src/main/java/com/synx/devkit/gateway/web/Api.java", isDir: false },
      ]),
    );

  it("folds single-folder chains, stopping at the source root", () => {
    const [root] = tree();
    expect(root.label).toBe("gateway/src/main/java");
    expect(root.path).toBe("gateway/src/main/java");
    expect(root.isPackage).toBe(false);
  });

  it("joins the packages below it with dots", () => {
    const [pkg] = tree()[0].children;
    expect(pkg.label).toBe("com.synx.devkit.gateway");
    expect(pkg.path).toBe("gateway/src/main/java/com/synx/devkit/gateway");
    expect(pkg.isPackage).toBe(true);
    // A folder with a class beside its subfolder is where the chain ends.
    expect(pkg.children.map((n) => n.label ?? n.name)).toEqual(["web", "App.java"]);
  });

  it("leaves a folder with more than one child as it is", () => {
    const out = compactFiles(nestFiles([...dirs("a", "a/b", "a/c")]));
    expect(out[0].label).toBe("a");
    expect(out[0].children.map((n) => n.label)).toEqual(["b", "c"]);
  });
});
