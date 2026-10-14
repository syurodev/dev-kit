import { describe, expect, test } from "bun:test";
import { fileRef, linkTarget } from "./fileRef";

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
