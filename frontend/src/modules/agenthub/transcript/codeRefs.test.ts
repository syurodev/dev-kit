import { describe, expect, test } from "bun:test";
import { codeSpans, commitCandidate, exactSymbolMatch, lastSegment, projectFolderPath, symbolCandidate, symbolNames } from "./codeRefs";
import { parseMarkdownIncremental } from "@/lib/agentMarkdown";

describe("symbolCandidate", () => {
  test("identifiers, dotted names and calls", () => {
    for (const text of ["SessionTTL", "Service.Invoke", "Service.Invoke()", "useHighlight", "$store", "_private"]) {
      expect(symbolCandidate(text)).toBe(true);
    }
  });

  test("not reserved words, short names, broken shapes, commands or paths", () => {
    for (const text of ["null", "true", "false", "nil", "undefined", "this", "self", "return", "a", "ab", "x.y.", "1abc", "rm -rf", "a/b.go", "foo()()", ""]) {
      expect(symbolCandidate(text)).toBe(false);
    }
  });

  test("at most 80 characters", () => {
    expect(symbolCandidate("a".repeat(80))).toBe(true);
    expect(symbolCandidate("a".repeat(81))).toBe(false);
  });
});

describe("commitCandidate", () => {
  test("seven to forty lowercase hex with a digit and a letter", () => {
    expect(commitCandidate("a1b2c3d")).toBe(true);
    expect(commitCandidate("0123456789abcdef0123456789abcdef01234567")).toBe(true);
  });

  test("not words, numbers, short or uppercase hex", () => {
    expect(commitCandidate("deadbeef")).toBe(false);
    expect(commitCandidate("1234567")).toBe(false);
    expect(commitCandidate("abc12")).toBe(false);
    expect(commitCandidate("A1B2C3D")).toBe(false);
    expect(commitCandidate("0123456789abcdef0123456789abcdef012345678")).toBe(false);
  });
});

describe("lastSegment", () => {
  test("the name after the last dot, without the call", () => {
    expect(lastSegment("Service.Invoke()")).toBe("Invoke");
    expect(lastSegment("useHighlight")).toBe("useHighlight");
  });
});

describe("exactSymbolMatch", () => {
  const m = (label: string, folderId = "api") => ({ label, folderId, filePath: "x.go", line: 1, kind: "method" });

  test("a bare name matches a label's last segment, never a longer name", () => {
    expect(exactSymbolMatch("Invoke", [m("Service.Invoke"), m("Other.Invokes")])?.label).toBe("Service.Invoke");
  });

  test("a call reads as the name", () => {
    expect(exactSymbolMatch("Service.Invoke()", [m("Service.Invoke"), m("Other.Invoke")])?.label).toBe("Service.Invoke");
  });

  test("the qualifier is a suffix of the label's", () => {
    expect(exactSymbolMatch("Service.Invoke", [m("agenthub.Service.Invoke"), m("agenthub.MyService.Invoke")])?.label).toBe(
      "agenthub.Service.Invoke",
    );
  });

  test("two exact matches are ambiguous", () => {
    expect(exactSymbolMatch("Invoke", [m("Service.Invoke"), m("Other.Invoke")])).toBeNull();
    expect(exactSymbolMatch("Service.Invoke", [m("Service.Invoke", "api"), m("Service.Invoke", "web")])).toBeNull();
  });

  test("no match", () => {
    expect(exactSymbolMatch("Invoke", [m("Invoker")])).toBeNull();
    expect(exactSymbolMatch("Invoke", [])).toBeNull();
  });
});

describe("codeSpans and symbolNames", () => {
  test("every inline code span, in blocks, lists, quotes and tables", () => {
    const md = [
      "# `Heading`",
      "Use `Service.Invoke` and **`useHighlight`**.",
      "- item `ListItem`",
      "> quoted `Quoted`",
      "",
      "| a | b |",
      "| - | - |",
      "| `Cell` | x |",
    ].join("\n");
    expect(codeSpans(parseMarkdownIncremental(md, undefined).blocks)).toEqual([
      "Heading",
      "Service.Invoke",
      "useHighlight",
      "ListItem",
      "Quoted",
      "Cell",
    ]);
  });

  test("distinct symbol names only: no commits, files or words, capped at 12", () => {
    expect(symbolNames(["Service.Invoke", "Service.Invoke", "a1b2c3d", "auth/session.go", "main.go", "null", "useHighlight"])).toEqual([
      "Service.Invoke",
      "useHighlight",
    ]);
    const many = Array.from({ length: 30 }, (_, i) => `Name${i}`);
    expect(symbolNames(many)).toHaveLength(12);
  });

  test("a short bare word is not looked up; a qualified, called or cased one is", () => {
    expect(symbolNames(["get", "run", "Add", "getUser", "Run()", "Foo.add", "load", "SessionTTL"])).toEqual([
      "getUser",
      "Run()",
      "Foo.add",
      "load",
      "SessionTTL",
    ]);
  });

  test("code inside a link is the link's, not a span of its own", () => {
    const md = "See [`abc1234`](https://github.com/o/r/commit/abc1234), [**`Service.Invoke`**](src/service.go:12) and `Real`.";
    expect(codeSpans(parseMarkdownIncremental(md, undefined).blocks)).toEqual(["Real"]);
  });
});

describe("projectFolderPath", () => {
  const folders = [
    { id: "a", path: "/work/repo" },
    { id: "b", path: "/work/repo/services/api" },
  ];
  test("the deepest workspace folder holding the project", () => {
    expect(projectFolderPath("/work/repo/services/api/internal", folders)).toBe("/work/repo/services/api");
    expect(projectFolderPath("/work/repo", folders)).toBe("/work/repo");
  });
  test("the project itself when no folder holds it", () => {
    expect(projectFolderPath("/elsewhere", folders)).toBe("/elsewhere");
    expect(projectFolderPath("/work/repository", folders)).toBe("/work/repository");
  });
});
