import { describe, expect, it } from "bun:test";
import { loadParser, tokenize, tokenizeLines } from "./highlight";
import {
  LANGUAGES,
  SYNTAX_LANGUAGES,
  detectLanguage,
  languageForContentType,
  languageForPath,
  loadLanguage,
  loadedLanguage,
  normalizeLanguage,
} from "./languages";

describe("language registry", () => {
  it("names a language from an id, an alias, a fence's info string or an extension", () => {
    expect(normalizeLanguage("TS")).toBe("typescript");
    expect(normalizeLanguage(" golang ")).toBe("go");
    expect(normalizeLanguage("bash")).toBe("shell");
    expect(normalizeLanguage("postgresql")).toBe("sql");
    expect(normalizeLanguage("yml")).toBe("yaml");
    expect(normalizeLanguage("jsonc")).toBe("json");
    expect(normalizeLanguage("c++")).toBe("cpp");
    // Console output is not a script; colouring it as shell would be wrong.
    expect(normalizeLanguage("console")).toBeUndefined();
    expect(normalizeLanguage("text")).toBeUndefined();
    expect(normalizeLanguage(undefined)).toBeUndefined();
  });

  it("names a file's language by extension, or by name when the name is the tell", () => {
    expect(languageForPath("/Users/me/src/ArbitrationPolicy.java")).toBe("java");
    expect(languageForPath("build.gradle.kts")).toBe("kotlin");
    expect(languageForPath("deploy/Dockerfile")).toBe("dockerfile");
    expect(languageForPath("deploy/Dockerfile.prod")).toBe("dockerfile");
    expect(languageForPath("C:\\work\\tool\\main.py")).toBe("python");
    expect(languageForPath("fsap/jsonschema.lua")).toBe("lua");
    expect(languageForPath("db/migrations/001_init.sql")).toBe("sql");
    expect(languageForPath("docker-compose.yml")).toBe("yaml");
    expect(languageForPath("pom.xml")).toBe("xml");
    expect(languageForPath("src/main/resources/application.properties")).toBe("properties");
    expect(languageForPath(".env")).toBe("properties");
    expect(languageForPath("Gemfile")).toBe("ruby");
    expect(languageForPath("README")).toBeUndefined();
  });

  it("names a body's language by its Content-Type", () => {
    expect(languageForContentType("application/json; charset=utf-8")).toBe("json");
    expect(languageForContentType("application/problem+json")).toBe("json");
    expect(languageForContentType("text/xml")).toBe("xml");
    expect(languageForContentType("application/atom+xml")).toBe("xml");
    expect(languageForContentType("text/html; charset=UTF-8")).toBe("html");
    expect(languageForContentType("application/x-yaml")).toBe("yaml");
    expect(languageForContentType("text/plain")).toBeUndefined();
    expect(languageForContentType(undefined)).toBeUndefined();
  });

  it("guesses only the unambiguous shapes from the text alone", () => {
    expect(detectLanguage('{"id": 1, "tags": ["a"]}')).toBe("json");
    expect(detectLanguage("  [1, 2, 3]\n")).toBe("json");
    // Looks like JSON, is not: a truncated preview stays plain.
    expect(detectLanguage('{"id": 1, "ta')).toBeUndefined();
    expect(detectLanguage('<?xml version="1.0"?><a/>')).toBe("xml");
    expect(detectLanguage("<order id=\"1\"><item/></order>")).toBe("xml");
    expect(detectLanguage("<!DOCTYPE html><html></html>")).toBe("html");
    expect(detectLanguage("user 42 signed in")).toBeUndefined();
    expect(detectLanguage("")).toBeUndefined();
  });

  it("gives every alias, extension and file name to one language only", () => {
    const owner = new Map<string, string>();
    for (const language of LANGUAGES) {
      for (const name of [language.id, ...(language.aliases ?? [])]) {
        expect(owner.get(`name:${name}`), name).toBeUndefined();
        owner.set(`name:${name}`, language.id);
      }
      for (const extension of language.extensions ?? []) {
        expect(owner.get(`ext:${extension}`), extension).toBeUndefined();
        owner.set(`ext:${extension}`, language.id);
      }
    }
  });
});

describe("grammars", () => {
  it("loads every language it names, and a parse keeps the text exact", async () => {
    const sample = 'name = "x"\n# note\n';
    for (const language of SYNTAX_LANGUAGES) {
      const parser = await loadParser(language);
      expect(tokenize(sample, parser).map((segment) => segment.text).join(""), language).toBe(sample);
      expect(loadedLanguage(language), language).toBeDefined();
    }
    await expect(loadLanguage("brainfuck")).rejects.toThrow();
  });

  it("colours Java with the app's syntax classes", async () => {
    const code = 'public final class Policy {\n  String name = "x"; // why\n}';
    const segments = tokenize(code, await loadParser("java"));
    const classOf = (text: string) => segments.find((segment) => segment.text === text)?.className ?? "";
    expect(classOf("public")).toContain("text-(--syntax-keyword)");
    expect(classOf('"x"')).toContain("text-(--syntax-string)");
    expect(classOf("// why")).toContain("italic");
    expect(segments.map((segment) => segment.text).join("")).toBe(code);
  });

  it("colours JSON, YAML and SQL", async () => {
    const classOf = (segments: { text: string; className: string }[], text: string) =>
      segments.find((segment) => segment.text.includes(text))?.className ?? "";

    const json = tokenize('{"name": "x", "n": 1}', await loadParser("json"));
    expect(classOf(json, '"x"')).toContain("text-(--syntax-string)");
    expect(classOf(json, "1")).not.toBe("");

    const yaml = tokenize("name: x # note\n", await loadParser("yaml"));
    expect(classOf(yaml, "# note")).toContain("text-(--syntax-comment)");

    const sql = tokenize("SELECT id FROM users WHERE name = 'x'", await loadParser("sql"));
    expect(classOf(sql, "SELECT")).toContain("text-(--syntax-keyword)");
    expect(classOf(sql, "'x'")).toContain("text-(--syntax-string)");
  });

  it("colours the grammars that come through the legacy stream adapter too", async () => {
    // Shell, Kotlin, TOML and the rest are StreamLanguage modes, not Lezer
    // grammars; their tokens have to reach the same classes.
    const segments = tokenize('echo "hello" # greet\n', await loadParser("shell"));
    const classOf = (text: string) => segments.find((segment) => segment.text.includes(text))?.className ?? "";
    expect(classOf('"hello"')).toContain("text-(--syntax-string)");
    expect(classOf("# greet")).toContain("text-(--syntax-comment)");
  });
});

describe("tokenizeLines", () => {
  it("splits the runs per line, keeping the text exactly", async () => {
    const parser = await loadParser("typescript");
    const code = "const a = 1;\n// two\nfunction f() {}";
    const lines = tokenizeLines(code, parser);
    expect(lines).toHaveLength(3);
    expect(lines.map((segments) => segments.map((s) => s.text).join(""))).toEqual(code.split("\n"));
    // The keyword is coloured, not the whole line.
    expect(lines[0].some((s) => s.className && s.text === "const")).toBe(true);
  });
});
