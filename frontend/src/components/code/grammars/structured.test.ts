import { describe, expect, test } from "bun:test";
import { EditorState } from "@codemirror/state";
import { syntaxTree } from "@codemirror/language";
import { loadParser, tokenize } from "../highlight";
import { languageForPath, loadLanguage, normalizeLanguage } from "../languages";

describe("build and configuration filenames", () => {
  test("recognizes Make variants and extensionless configuration scripts", () => {
    for (const file of ["Makefile", "GNUmakefile", "BSDmakefile", "build/Makefile.local", "C:\\work\\rules.mk", "rules.mak"]) {
      expect(languageForPath(file), file).toBe("makefile");
    }
    expect(normalizeLanguage("make")).toBe("makefile");
    expect(languageForPath(".env.production")).toBe("properties");
    expect(languageForPath("deploy/Containerfile.dev")).toBe("dockerfile");
    expect(languageForPath("gradlew")).toBe("shell");
    expect(languageForPath("Makefile.md")).toBe("markdown");
  });

  test("colours make targets, assignments, expansions, recipes and comments", async () => {
    const code = 'CC := gcc\ninclude rules.mk\n.PHONY: build\nbuild: main.c\n\t@echo "building"\n\t$(CC) -o $@ $< # compile\nFILES = $(patsubst %.c,%.o,$(wildcard src/*.c))\n';
    const segments = tokenize(code, await loadParser("makefile"));
    const classOf = (text: string) => segments.find((segment) => segment.text === text)?.className;
    expect(classOf("CC")).toContain("syntax-definition");
    expect(classOf("include")).toContain("syntax-keyword");
    expect(classOf(".PHONY")).toContain("syntax-function");
    expect(classOf("$(CC)")).toContain("syntax-meta");
    expect(classOf("$@")).toContain("syntax-meta");
    expect(classOf('"building"')).toContain("syntax-string");
    expect(classOf("# compile")).toContain("syntax-comment");
    expect(classOf("$(patsubst %.c,%.o,$(wildcard src/*.c))")).toContain("syntax-meta");
    expect(segments.map((segment) => segment.text).join("")).toBe(code);
  });
});

describe("YAML source", () => {
  test("highlights SQL inside explicit scalar blocks, leaving arbitrary text alone", async () => {
    const code = `databaseChangeLog:
  - changeSet:
      id: migration
      sql: |-
        SELECT COUNT(*) FROM statements WHERE status = 'open';
        -- SQL comment
      description: |
        SELECT is just prose here
      'init.sql': |
        CREATE TABLE demo (id INT);
      query: |
        query GraphQL { user { id } }
`;
    const segments = tokenize(code, await loadParser("yaml"));
    expect(segments.find((segment) => segment.text === "SELECT")?.className).toContain("syntax-keyword");
    expect(segments.find((segment) => segment.text === "-- SQL comment")?.className).toContain("syntax-comment");
    expect(segments.find((segment) => segment.text === "'open'")?.className).toContain("syntax-string");
    expect(segments.find((segment) => segment.text === "CREATE")?.className).toContain("syntax-keyword");
    expect(segments.find((segment) => segment.text.includes("SELECT is just prose"))?.className).toBe("");
    expect(segments.find((segment) => segment.text.includes("query GraphQL"))?.className).toBe("");
    expect(segments.map((segment) => segment.text).join("")).toBe(code);
  });

  test("updates embedded language after editing the key without changing text", async () => {
    const support = await loadLanguage("yaml");
    const doc = 'sql: |\n  SELECT 1;\nother: false\n';
    let state = EditorState.create({ doc, extensions: [support] });
    expect(syntaxTree(state).resolveInner(doc.indexOf("SELECT"), 1).name).toBe("Keyword");
    state = state.update({ changes: { from: 0, to: 3, insert: "description" } }).state;
    const updated = state.doc.toString();
    expect(syntaxTree(state).resolveInner(updated.indexOf("SELECT"), 1).name).toBe("BlockLiteralContent");
    expect(updated).toBe(doc.replace("sql:", "description:"));
  });
});
