import { describe, expect, test } from "bun:test";
import { commitMenuItems, copiedPath, fileMenuItems, symbolMenuItems, urlMenuItems } from "./referenceMenu";

describe("fileMenuItems", () => {
  test("a reference with a line offers every action", () => {
    expect(fileMenuItems({ path: "auth/session.go", line: 88 })).toEqual([
      { id: "open-beside", label: "Open beside" },
      { id: "open-here", label: "Open here" },
      { id: "copy-path", label: "Copy path" },
      { id: "copy-path-line", label: "Copy path:line" },
      { id: "reveal", label: "Reveal in Files" },
    ]);
  });

  test("with no line there is no path:line to copy", () => {
    expect(fileMenuItems({ path: "auth/session.go" }).map((item) => item.id)).toEqual([
      "open-beside",
      "open-here",
      "copy-path",
      "reveal",
    ]);
  });
});

describe("urlMenuItems", () => {
  test("open in the browser, or copy", () => {
    expect(urlMenuItems("https://a.dev/x")).toEqual([
      { id: "open", label: "Open in browser" },
      { id: "copy", label: "Copy link" },
    ]);
  });
});

describe("copiedPath", () => {
  test("the resolved folder-relative path wins over what was written", () => {
    expect(copiedPath({ path: "../api/a.go", line: 3 }, "a.go", false)).toBe("a.go");
    expect(copiedPath({ path: "../api/a.go", line: 3 }, "a.go", true)).toBe("a.go:3");
  });

  test("unresolved, the path as written", () => {
    expect(copiedPath({ path: "src/a.ts", line: 9 }, undefined, true)).toBe("src/a.ts:9");
    expect(copiedPath({ path: "src/a.ts" }, undefined, true)).toBe("src/a.ts");
  });
});

describe("symbolMenuItems and commitMenuItems", () => {
  test("a symbol: go to its definition, or copy its name", () => {
    expect(symbolMenuItems()).toEqual([
      { id: "definition", label: "Go to definition" },
      { id: "copy", label: "Copy name" },
    ]);
  });

  test("a commit: open it in Git, or copy the SHA", () => {
    expect(commitMenuItems()).toEqual([
      { id: "open", label: "Open in Git" },
      { id: "copy", label: "Copy SHA" },
    ]);
  });
});
