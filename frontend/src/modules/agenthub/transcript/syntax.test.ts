import { describe, expect, it } from "bun:test";
import {
  SYNTAX_LANGUAGES,
  languageForPath,
  loadParser,
  normalizeLanguage,
  parseListing,
  tokenize,
} from "./syntax";

describe("transcript syntax", () => {
  it("names a grammar from a fence's info string or a file", () => {
    expect(normalizeLanguage("TS")).toBe("typescript");
    expect(normalizeLanguage(" golang ")).toBe("go");
    expect(normalizeLanguage("bash")).toBe("shell");
    // Console output is not a script; colouring it as shell would be wrong.
    expect(normalizeLanguage("console")).toBeUndefined();
    expect(normalizeLanguage(undefined)).toBeUndefined();

    expect(languageForPath("/Users/me/src/ArbitrationPolicy.java")).toBe("java");
    expect(languageForPath("build.gradle.kts")).toBe("kotlin");
    expect(languageForPath("deploy/Dockerfile")).toBe("dockerfile");
    expect(languageForPath("C:\\work\\tool\\main.py")).toBe("python");
    expect(languageForPath("README")).toBeUndefined();
  });

  it("reads Claude's numbered Read listing as the code and its first line", () => {
    // The shape the adapter sends, from a real session's transcript.
    const read = "```\n1\tpackage com.synx.devkit.identity;\n2\t\n3\timport java.util.Optional;\n```";
    expect(parseListing(read)).toEqual({
      code: "package com.synx.devkit.identity;\n\nimport java.util.Optional;",
      startLine: 1,
      info: undefined,
    });
    expect(parseListing("```\n40\tint a;\n41\tint b;\n```")?.startLine).toBe(40);
    expect(parseListing("```\n1→first\n2→second\n```")?.code).toBe("first\nsecond");

    // Fenced but not numbered: the fence goes, the text stays.
    expect(parseListing("```console\n=== gateway ===\npackage x;\n```")).toEqual({
      code: "=== gateway ===\npackage x;",
      info: "console",
    });
    // Numbers out of sequence are content, not a listing.
    expect(parseListing("12\tone\n14\ttwo")).toBeNull();
    expect(parseListing("./deploy\n./docs")).toBeNull();
  });

  it("loads every grammar it names, and a parse keeps the text exact", async () => {
    const sample = 'name = "x"\n# note\n';
    for (const language of SYNTAX_LANGUAGES) {
      const parser = await loadParser(language);
      expect(tokenize(sample, parser).map((segment) => segment.text).join(""), language).toBe(sample);
    }
    await expect(loadParser("brainfuck")).rejects.toThrow();
  });

  it("colours Java with the app's syntax classes", async () => {
    const code = 'public final class Policy {\n  String name = "x"; // why\n}';
    const segments = tokenize(code, await loadParser("java"));
    const classOf = (text: string) => segments.find((segment) => segment.text === text)?.className ?? "";
    expect(classOf("public")).toContain("text-(--brand-text)");
    expect(classOf('"x"')).toContain("text-(--status-success)");
    expect(classOf("// why")).toContain("italic");
    expect(segments.map((segment) => segment.text).join("")).toBe(code);
  });

  it("colours the grammars that come through the legacy stream adapter too", async () => {
    // Shell, Kotlin, TOML and the rest are StreamLanguage modes, not Lezer
    // grammars; their tokens have to reach the same classes.
    const segments = tokenize('echo "hello" # greet\n', await loadParser("shell"));
    const classOf = (text: string) => segments.find((segment) => segment.text.includes(text))?.className ?? "";
    expect(classOf('"hello"')).toContain("text-(--status-success)");
    expect(classOf("# greet")).toContain("text-(--text-tertiary)");
  });
});

describe("tokenizeLines", () => {
  it("splits the runs per line, keeping the text exactly", async () => {
    const { loadParser, tokenizeLines } = await import("./syntax");
    const parser = await loadParser("typescript");
    const code = "const a = 1;\n// two\nfunction f() {}";
    const lines = tokenizeLines(code, parser);
    expect(lines).toHaveLength(3);
    expect(lines.map((segments) => segments.map((s) => s.text).join(""))).toEqual(code.split("\n"));
    // The keyword is coloured, not the whole line.
    expect(lines[0].some((s) => s.className && s.text === "const")).toBe(true);
  });
});
