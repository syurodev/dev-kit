import { describe, expect, it } from "bun:test";

import { parseInline, parseMarkdown, parseMarkdownIncremental } from "./agentMarkdown";

describe("agentMarkdown", () => {
  it("reads the blocks Agent / Message Rich draws", () => {
    const blocks = parseMarkdown(
      [
        "# Why the rounding fails",
        "",
        "The amount is rounded before",
        "the currency conversion.",
        "",
        "- rounds at line 44",
        "- conversion runs after",
        "",
        "1. Move the rounding",
        "2. Rewrite the fixture",
        "",
        "> Rounding is a presentation concern.",
        "",
        "| Currency | Before | After |",
        "| --- | --- | --- |",
        "| EUR | 10.04 | 10.05 |",
      ].join("\n"),
    );

    expect(blocks.map((b) => b.type)).toEqual([
      "heading",
      "paragraph",
      "list",
      "list",
      "quote",
      "table",
    ]);
    // A wrapped sentence is one paragraph, not two.
    expect(blocks[1]).toEqual({
      type: "paragraph",
      text: "The amount is rounded before the currency conversion.",
    });
    expect(blocks[2]).toMatchObject({ ordered: false, items: ["rounds at line 44", "conversion runs after"] });
    expect(blocks[3]).toMatchObject({ ordered: true });
    expect(blocks[5]).toMatchObject({
      head: ["Currency", "Before", "After"],
      rows: [["EUR", "10.04", "10.05"]],
    });
  });

  it("keeps a fenced block whole, including its blank lines", () => {
    const blocks = parseMarkdown("```java\nint a;\n\nint b;\n```\nafter");
    expect(blocks[0]).toEqual({ type: "code", language: "java", code: "int a;\n\nint b;" });
    expect(blocks[1]).toMatchObject({ type: "paragraph", text: "after" });
  });

  it("reads each column's alignment off the divider row", () => {
    const [table] = parseMarkdown(
      ["| Name | Status | Count |", "| :--- | :---: | ---: |", "| api | ok | 12 |"].join("\n"),
    );
    expect(table).toMatchObject({ type: "table", align: ["left", "center", "right"] });
    // No colons is the common case, and it means left.
    const [plain] = parseMarkdown(["| A | B |", "| --- | --- |", "| 1 | 2 |"].join("\n"));
    expect(plain).toMatchObject({ align: ["left", "left"] });
  });

  it("does not mistake a piped command for a table", () => {
    // Without a divider row this is a shell line, and turning it into a
    // two-column table would hide half of it.
    const blocks = parseMarkdown("run `grep foo | wc -l` to count");
    expect(blocks).toHaveLength(1);
    expect(blocks[0].type).toBe("paragraph");
  });

  it("matches one-shot parsing while code, tables, and lists stream", () => {
    const source = [
      "Introductory paragraph.",
      "",
      "```ts",
      "const answer = await run();",
      "",
      "return answer;",
      "```",
      "",
      "| Name | Status |",
      "| --- | :---: |",
      "| stream | passed |",
      "",
      "- keep old blocks",
      "- update the tail",
    ].join("\n");
    let state;
    for (let offset = 1; offset <= source.length; offset += 3) {
      const prefix = source.slice(0, offset);
      state = parseMarkdownIncremental(prefix, state);
      expect(state.blocks).toEqual(parseMarkdown(prefix));
    }
    state = parseMarkdownIncremental(source, state);
    expect(state.blocks).toEqual(parseMarkdown(source));
  });

  it("keeps the active block key while its text grows", () => {
    const first = parseMarkdownIncremental("A paragraph that is still streaming");
    const second = parseMarkdownIncremental(`${first.source} with another token`, first);
    expect(second.blockKeys).toEqual(first.blockKeys);
  });

  it("reads inline code, links and emphasis", () => {
    expect(parseInline("see `Currency.scale()` in [the spec](http://x) — **now**")).toEqual([
      { type: "text", value: "see " },
      { type: "code", value: "Currency.scale()" },
      { type: "text", value: " in " },
      { type: "link", value: "the spec", href: "http://x" },
      { type: "text", value: " — " },
      { type: "strong", value: "now" },
    ]);
  });
});
