import { describe, expect, test } from "bun:test";
import { fullPath, revealLabel } from "./fileMenu";

describe("revealLabel", () => {
  test("names the file manager of each desktop, and none for a browser", () => {
    expect(revealLabel("mac")).toBe("Reveal in Finder");
    expect(revealLabel("windows")).toBe("Show in File Explorer");
    expect(revealLabel("linux")).toBe("Show in File Manager");
    expect(revealLabel("web")).toBeNull();
  });
});

describe("fullPath", () => {
  test("joins with the separator of the folder's own system", () => {
    expect(fullPath("/Users/me/project-docs", "docs/spec.pdf")).toBe("/Users/me/project-docs/docs/spec.pdf");
    expect(fullPath("/Users/me/project-docs/", "/README.md")).toBe("/Users/me/project-docs/README.md");
    expect(fullPath("C:\\Users\\me\\docs", "a/b/spec.pdf")).toBe("C:\\Users\\me\\docs\\a\\b\\spec.pdf");
    expect(fullPath("C:/Users/me/docs/", "spec.pdf")).toBe("C:\\Users\\me\\docs\\spec.pdf");
    expect(fullPath("\\\\server\\share", "x.md")).toBe("\\\\server\\share\\x.md");
  });
});
