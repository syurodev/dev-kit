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

  it("reads a table that starts right after a sentence, with no blank line", () => {
    // What an agent wrote in the field: the intro line, then the table. GFM
    // lets a table interrupt a paragraph; read as one paragraph, the whole
    // table came out as a line of pipes and dashes.
    const blocks = parseMarkdown(
      [
        "4 folder, kong-plugin là primary:",
        "| folder | path |",
        "|---|---|",
        "| **kong-plugin** (primary) | `/w/fsap/kong-plugin` |",
        "| fsap-config-service | `/w/g-platform/fsap-config-service` |",
        "",
        "**Repo kong-plugin**",
      ].join("\n"),
    );
    expect(blocks.map((b) => b.type)).toEqual(["paragraph", "table", "paragraph"]);
    expect(blocks[0]).toEqual({ type: "paragraph", text: "4 folder, kong-plugin là primary:" });
    expect(blocks[1]).toMatchObject({ type: "table", head: ["folder", "path"] });
    if (blocks[1].type === "table") expect(blocks[1].rows).toHaveLength(2);
  });

  it("needs as many divider cells as header cells, as GFM does", () => {
    // A sentence with a pipe above a "---" rule is not a one-column table.
    const blocks = parseMarkdown(["Summary:", "use `a | b` here", "---"].join("\n"));
    expect(blocks.map((b) => b.type)).toEqual(["paragraph"]);
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

  // A stream is cut wherever the transport cuts it, including in the middle of
  // a fence or its closing line. Whatever the cut, the incremental result must
  // be exactly what parsing the same text in one go gives.
  const streamed = {
    "fence closed by a longer run": "intro\n\n```\ncode\n````\nafter",
    "fence closing line that grows a word": "```\ncode\n```j\nmore",
    "fence with language and blank lines": "a\n\n```ts\nconst x = 1;\n\nreturn x;\n```\n\nb",
    "tilde fence": "~~~\ncode\n~~~\n\ntext",
    "four-backtick fence": "````\ncode\n````\n\ntext",
    "fence inside a list": "- one\n```\ncode\n```\n- two\n\n1. three",
    "table whose divider comes after the header": "| A | B |\n| --- | ---: |\n| 1 | 2 |\n\nafter",
    "table right after a sentence": "Summary:\n| A | B |\n|---|---|\n| 1 | 2 |\n\nafter",
    "list that keeps going": "- a\n- b\n\n- c\n- d\ntrailing",
    quote: "> one\n> two\n\n> three\n\nend",
    CRLF: "para\r\n\r\n```js\r\nx\r\n```\r\n\r\n| A |\r\n| - |\r\n| 1 |",
    "unclosed fence at the end": "text\n\n```\nstill open\n\nmore",
  };

  for (const [name, source] of Object.entries(streamed)) {
    for (const step of [1, 2, 3, 7]) {
      it(`streams "${name}" in steps of ${step} exactly as a one-shot parse`, () => {
        let state;
        for (let offset = 1; offset < source.length + step; offset += step) {
          const prefix = source.slice(0, Math.min(offset, source.length));
          state = parseMarkdownIncremental(prefix, state);
          expect(state.blocks).toEqual(parseMarkdown(prefix));
          expect(state.blockKeys).toHaveLength(state.blocks.length);
        }
      });
    }
  }

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

  it("leaves a snake_case identifier alone rather than reading it as emphasis", () => {
    // CommonMark disallows intraword underscore emphasis for exactly this
    // reason. Without that rule, program_config_auth read as `program`,
    // <em>config</em>, `auth` — its own underscores eaten as markup.
    expect(parseInline("column platform_client_id on program_config_auth")).toEqual([
      { type: "text", value: "column platform_client_id on program_config_auth" },
    ]);
  });

  it("still reads underscore emphasis at a word boundary", () => {
    expect(parseInline("that is _not_ optional")).toEqual([
      { type: "text", value: "that is " },
      { type: "em", value: "not" },
      { type: "text", value: " optional" },
    ]);
  });

  describe("JWTs read as a secret to mask, not text to print", () => {
    // A real sample header/payload/signature, shortened at each segment —
    // long enough to clear the shape's own minimum segment lengths.
    const jwt =
      "eyJhbGciOiJSUzI1NiIsImtpZCI6ImdhbGF4eS1nLXVzZXItYXNzZXJ0aW9uIn0" +
      ".eyJpc3MiOiJnYWxheHktZyIsInN1YiI6Ik0wMDEifQ" +
      ".RZbWj7P-qiOsKBu7cKfiAYWw8ANxz6esebuuSWdmmrw";

    it("pulls a bare token out of plain prose", () => {
      expect(parseInline(`token mẫu là ${jwt}`)).toEqual([
        { type: "text", value: "token mẫu là " },
        { type: "secret", value: jwt },
      ]);
    });

    it("reads a backtick-quoted token as a secret too, not as code", () => {
      expect(parseInline(`Authorization: \`${jwt}\``)).toEqual([
        { type: "text", value: "Authorization: " },
        { type: "secret", value: jwt },
      ]);
    });

    it("leaves a short, unrelated eyJ-looking word as plain text", () => {
      // The anchor is "eyJ" plus the three-segment shape — "eyJust" alone,
      // with no dots, must never trip the mask.
      expect(parseInline("eyJust checking in")).toEqual([{ type: "text", value: "eyJust checking in" }]);
    });
  });
});
