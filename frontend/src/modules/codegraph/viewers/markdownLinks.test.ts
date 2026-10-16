import { describe, expect, test } from "bun:test";
import { headingSlug, resolveDocLink, uniqueSlugs } from "./markdownLinks";

describe("resolveDocLink", () => {
  test("the web goes to the browser, nothing else with a scheme does", () => {
    expect(resolveDocLink("a.md", "https://example.com/x")).toEqual({ kind: "url", url: "https://example.com/x" });
    expect(resolveDocLink("a.md", "mailto:me@example.com")).toEqual({ kind: "url", url: "mailto:me@example.com" });
    for (const href of ["javascript:alert(1)", "data:text/html,x", "file:///etc/passwd", "ftp://h/x", "//evil.example/x", " JavaScript:alert(1)"]) {
      expect(resolveDocLink("a.md", href)).toEqual({ kind: "other" });
    }
  });

  test("#heading stays in the file", () => {
    expect(resolveDocLink("docs/a.md", "#install-guide")).toEqual({ kind: "anchor", id: "install-guide" });
    expect(resolveDocLink("a.md", "#")).toEqual({ kind: "other" });
    expect(resolveDocLink("a.md", "#c%E1%BA%A5u-h%C3%ACnh")).toEqual({ kind: "anchor", id: "cấu-hình" });
  });

  test("a relative path is read from the file's own directory", () => {
    expect(resolveDocLink("docs/guide/a.md", "b.md")).toEqual({ kind: "file", path: "docs/guide/b.md" });
    expect(resolveDocLink("docs/guide/a.md", "./b.md#top")).toEqual({ kind: "file", path: "docs/guide/b.md", anchor: "top" });
    expect(resolveDocLink("docs/guide/a.md", "../img/spec.pdf?raw=1")).toEqual({ kind: "file", path: "docs/img/spec.pdf" });
    expect(resolveDocLink("a.md", "sub/c.go")).toEqual({ kind: "file", path: "sub/c.go" });
  });

  test("a leading slash is the folder's root", () => {
    expect(resolveDocLink("docs/a.md", "/README.md")).toEqual({ kind: "file", path: "README.md" });
  });

  test("a path that leaves the folder is not a link", () => {
    expect(resolveDocLink("a.md", "../outside.md")).toEqual({ kind: "other" });
    expect(resolveDocLink("docs/a.md", "../../x.md")).toEqual({ kind: "other" });
    expect(resolveDocLink("docs/a.md", "/../x.md")).toEqual({ kind: "other" });
    expect(resolveDocLink("a.md", "")).toEqual({ kind: "other" });
  });

  test("a path to the file itself with an anchor is an anchor", () => {
    expect(resolveDocLink("a.md", "?x=1#part")).toEqual({ kind: "anchor", id: "part" });
  });
});

describe("heading ids", () => {
  test("follow GitHub's rule", () => {
    expect(headingSlug("Getting Started")).toBe("getting-started");
    expect(headingSlug("What's new? (v2.0)")).toBe("whats-new-v20");
    expect(headingSlug("Cấu hình & cài đặt")).toBe("cấu-hình--cài-đặt");
    expect(headingSlug("snake_case and kebab-case")).toBe("snake_case-and-kebab-case");
  });

  test("a repeated heading gets a counter", () => {
    expect(uniqueSlugs(["Usage", "Notes", "Usage", "Usage"])).toEqual(["usage", "notes", "usage-1", "usage-2"]);
  });
});
