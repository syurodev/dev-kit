import { describe, expect, test } from "bun:test";
import { elisionPattern, fileRef, hasElision, linkTarget } from "./fileRef";

describe("fileRef", () => {
  test("paths with a source extension", () => {
    expect(fileRef("auth/session.go:88")).toEqual({ path: "auth/session.go", line: 88 });
    expect(fileRef("src/App.tsx:12:4")).toEqual({ path: "src/App.tsx", line: 12 });
    expect(fileRef("internal/agenthub/service.go")).toEqual({ path: "internal/agenthub/service.go" });
    expect(fileRef("/abs/path/main.go")).toEqual({ path: "/abs/path/main.go" });
    expect(fileRef("README.md")).toEqual({ path: "README.md" });
  });
  test("not paths", () => {
    for (const s of ["https://x.test/a.go", "SessionTTL", "a b/c.go", "v1.2.3", "x.y", "npm run build", "1.5", "../"]) {
      expect(fileRef(s)).toBeNull();
    }
  });
  test("line forms", () => {
    expect(fileRef("handler.lua:27")).toEqual({ path: "handler.lua", line: 27 });
    expect(fileRef("a/b.go#L27")).toEqual({ path: "a/b.go", line: 27 });
    expect(fileRef("a/b.go#L27-L30")).toEqual({ path: "a/b.go", line: 27 });
    expect(fileRef("./a/b.go:3:9")).toEqual({ path: "a/b.go", line: 3 });
    expect(fileRef("/abs/a/b.go")).toEqual({ path: "/abs/a/b.go" });
    expect(fileRef("../shared/x.ts")).toEqual({ path: "../shared/x.ts" });
  });
  test("well-known names without an extension", () => {
    for (const n of ["Dockerfile", "Makefile", "go.mod", "package.json", "Taskfile.yml", "docker/Dockerfile"]) {
      expect(fileRef(n)?.path).toBe(n);
    }
  });
  test("still not paths", () => {
    for (const s of ["https://x.test/a.go", "SessionTTL", "a b/c.go", "v1.2.3", "x.y", "1.5", "../", "a/../../etc/passwd"]) {
      expect(fileRef(s)).toBeNull();
    }
  });
});

describe("fileRef review fixes", () => {
  test("line ranges", () => {
    expect(fileRef("a/b.go:27-30")).toEqual({ path: "a/b.go", line: 27 });
  });
  test("an extension needs a stem", () => {
    expect(fileRef(".go")).toBeNull();
    expect(fileRef(".ts")).toBeNull();
    expect(fileRef("src/.ts")).toBeNull();
    expect(fileRef(".env")).toEqual({ path: ".env" });
  });
  test("framework names are prose", () => {
    for (const s of ["Node.js", "Next.js", "Vue.js", "Nuxt.js", "D3.js"]) expect(fileRef(s)).toBeNull();
    expect(fileRef("index.js")).toEqual({ path: "index.js" });
    expect(fileRef("Button.js")).toEqual({ path: "Button.js" });
    expect(fileRef("src/Node.js")).toEqual({ path: "src/Node.js" });
  });
  test("file://localhost", () => {
    expect(linkTarget("file://localhost/a/b.go")).toEqual({ kind: "file", ref: { path: "/a/b.go" } });
  });
});

describe("a link to a document that is not source code", () => {
  test("an explicit link opens any file with an extension, the way the author meant it", () => {
    expect(linkTarget("/Users/me/project-docs/huong-dan-ca-12.pdf")).toEqual({ kind: "file", ref: { path: "/Users/me/project-docs/huong-dan-ca-12.pdf" } });
    expect(linkTarget("docs/report.docx")).toEqual({ kind: "file", ref: { path: "docs/report.docx" } });
    expect(linkTarget("file:///Users/me/a/spec.pdf")).toEqual({ kind: "file", ref: { path: "/Users/me/a/spec.pdf" } });
    expect(linkTarget("docs/report")).toEqual({ kind: "other" });
  });
  test("an inline `span` still has to look like a file, but a PDF or text file does", () => {
    expect(fileRef("docs/spec.pdf")).toEqual({ path: "docs/spec.pdf" });
    expect(fileRef("notes/todo.txt")).toEqual({ path: "notes/todo.txt" });
    expect(fileRef("report.docx")).toBeNull();
    expect(fileRef("v1.2")).toBeNull();
  });
});

describe("linkTarget", () => {
  test("classifies hrefs", () => {
    expect(linkTarget("https://x.test/a")).toEqual({ kind: "url", url: "https://x.test/a" });
    expect(linkTarget("mailto:a@b.c")).toEqual({ kind: "url", url: "mailto:a@b.c" });
    expect(linkTarget("internal/auth/session.go#L88")).toEqual({ kind: "file", ref: { path: "internal/auth/session.go", line: 88 } });
    expect(linkTarget("file:///home/u/p/a%20b.go")).toEqual({ kind: "other" }); // a space after decoding is still not a path
    expect(linkTarget("file:///home/u/p/x.go")).toEqual({ kind: "file", ref: { path: "/home/u/p/x.go" } });
    expect(linkTarget("ftp://x")).toEqual({ kind: "other" });
  });
});

describe("an elided path", () => {
  // An answer shortens a long path to fit a table cell: `core/.../DispatchFunction.java`.
  test("is a file reference, with either kind of ellipsis", () => {
    expect(fileRef("core/.../DispatchFunction.java")).toEqual({ path: "core/.../DispatchFunction.java" });
    expect(fileRef("core/\u2026/DispatchFunction.java:12")).toEqual({ path: "core/\u2026/DispatchFunction.java", line: 12 });
    expect(fileRef(".../DispatchFunction.java")).toEqual({ path: ".../DispatchFunction.java" });
    expect(hasElision("core/.../X.java")).toBe(true);
    expect(hasElision("core/\u2026/X.java")).toBe(true);
    expect(hasElision("core/src/X.java")).toBe(false);
    expect(hasElision("../core/X.java")).toBe(false);
    expect(hasElision("a/v1.2.3/X.java")).toBe(false);
  });

  test("is no way to hide a climb out: a .. after a name is still refused", () => {
    expect(fileRef("core/.../../x.go")).toBeNull();
    expect(fileRef("/w/.../../x.go")).toBeNull();
  });

  test("matches the real paths its ends allow, and no others", () => {
    const pattern = elisionPattern("core/.../DispatchFunction.java");
    expect(pattern.test("/w/api/core/src/main/java/io/x/DispatchFunction.java")).toBe(true);
    expect(pattern.test("/w/api/core/DispatchFunction.java")).toBe(true); // nothing between is fine
    expect(pattern.test("/w/api/other/src/DispatchFunction.java")).toBe(false); // wrong start
    expect(pattern.test("/w/api/core/src/OtherDispatchFunction.java")).toBe(false); // the name is whole
    expect(pattern.test("/w/api/core/src/DispatchFunction.java.bak")).toBe(false);
    expect(elisionPattern(".../DispatchFunction.java").test("/a/b/DispatchFunction.java")).toBe(true);
    // A folder named like a start is not the start unless it is a whole segment.
    expect(pattern.test("/w/api/hardcore/src/DispatchFunction.java")).toBe(false);
  });

  test("an absolute one is anchored at the root, and a dot in a name is a dot", () => {
    const pattern = elisionPattern("/w/.../a.b.java");
    expect(pattern.test("/w/x/y/a.b.java")).toBe(true);
    expect(pattern.test("/other/w/x/a.b.java")).toBe(false);
    expect(pattern.test("/w/x/aXb.java")).toBe(false);
  });
});
