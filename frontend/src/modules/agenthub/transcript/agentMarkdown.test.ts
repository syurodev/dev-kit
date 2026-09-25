import { describe, expect, it } from "bun:test";

import {
  newLexer,
  parseInline,
  parseMarkdown,
  parseMarkdownIncremental,
  type InlineToken,
  type MarkdownBlock,
} from "./agentMarkdown";

const text = (value: string): InlineToken => ({ type: "text", value });
const paragraph = (value: string): MarkdownBlock => ({ type: "paragraph", inline: [text(value)] });

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
    // A wrapped sentence is one paragraph, not two; the line break is a
    // soft one, which the page draws as a space.
    expect(blocks[1]).toEqual({
      type: "paragraph",
      inline: [text("The amount is rounded before\nthe currency conversion.")],
    });
    expect(blocks[2]).toMatchObject({
      ordered: false,
      items: [{ blocks: [paragraph("rounds at line 44")] }, { blocks: [paragraph("conversion runs after")] }],
    });
    expect(blocks[3]).toMatchObject({ ordered: true, start: 1 });
    expect(blocks[4]).toEqual({ type: "quote", blocks: [paragraph("Rounding is a presentation concern.")] });
    expect(blocks[5]).toMatchObject({
      head: [[text("Currency")], [text("Before")], [text("After")]],
      rows: [[[text("EUR")], [text("10.04")], [text("10.05")]]],
    });
  });

  it("keeps a fenced block whole, including its blank lines", () => {
    const blocks = parseMarkdown("```java\nint a;\n\nint b;\n```\nafter");
    expect(blocks[0]).toEqual({ type: "code", language: "java", code: "int a;\n\nint b;" });
    expect(blocks[1]).toEqual(paragraph("after"));
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
    expect(blocks[0]).toEqual(paragraph("4 folder, kong-plugin là primary:"));
    expect(blocks[1]).toMatchObject({ type: "table", head: [[text("folder")], [text("path")]] });
    if (blocks[1].type === "table") expect(blocks[1].rows).toHaveLength(2);
  });

  it("needs as many divider cells as header cells, as GFM does", () => {
    // A sentence with a pipe above a "---" rule is not a one-column table.
    // (In GFM the rule under it makes the two lines a setext heading.)
    const blocks = parseMarkdown(["Summary:", "use `a | b` here", "---"].join("\n"));
    expect(blocks.map((b) => b.type)).toEqual(["heading"]);
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
    "nested list": "- a\n  - b\n  - c\n- d\n\nend",
    "setext heading": "Title\n=====\n\nbody",
    "heading then paragraph with no blank line": "# One\ntext\n## Two\nmore",
    "indented code across a blank line": "para\n\n    one\n\n    two\n\nend",
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

  it("reads what the line-based reader could not", () => {
    // Nested lists, task items (a new bullet character starts a new list),
    // a numbered list that starts at 3, `~~~` fences and rules — all things
    // agents write.
    const blocks = parseMarkdown(
      [
        "- parent",
        "  - child",
        "",
        "* [x] done",
        "* [ ] todo",
        "",
        "3. third",
        "4. fourth",
        "",
        "~~~",
        "code",
        "~~~",
        "",
        "---",
      ].join("\n"),
    );
    expect(blocks.map((b) => b.type)).toEqual(["list", "list", "list", "code", "rule"]);
    expect(blocks[0]).toEqual({
      type: "list",
      ordered: false,
      start: 1,
      items: [
        {
          blocks: [
            paragraph("parent"),
            { type: "list", ordered: false, start: 1, items: [{ blocks: [paragraph("child")] }] },
          ],
        },
      ],
    });
    expect(blocks[1]).toMatchObject({ items: [{ checked: true }, { checked: false }] });
    expect(blocks[2]).toMatchObject({ ordered: true, start: 3 });
    expect(blocks[3]).toEqual({ type: "code", language: undefined, code: "code" });
  });

  it("names a fence's language by the info string's first word", () => {
    expect(parseMarkdown("```ts {1-3}\nx\n```")[0]).toEqual({ type: "code", language: "ts", code: "x" });
  });

  it("shows HTML as written instead of interpreting it", () => {
    expect(parseMarkdown("<div>hi</div>")).toEqual([paragraph("<div>hi</div>")]);
    expect(parseInline("a <b>c</b>")).toEqual([text("a <b>c</b>")]);
  });

  it("never loads an image the agent names — it becomes a link", () => {
    expect(parseInline("![diagram](https://x.test/a.png)")).toEqual([
      { type: "link", href: "https://x.test/a.png", children: [text("diagram")] },
    ]);
  });

  it("does not make a javascript: or data: href clickable", () => {
    expect(parseInline("[click](javascript:alert(1))")).toEqual([text("click")]);
    expect(parseInline("[x](data:text/html,hi)")).toEqual([text("x")]);
  });

  it("nests emphasis and code, and reads strikethrough and bare URLs", () => {
    expect(parseInline("**run `make`** ~~old~~ https://x.test")).toEqual([
      { type: "strong", children: [text("run "), { type: "code", value: "make" }] },
      text(" "),
      { type: "del", children: [text("old")] },
      text(" "),
      { type: "link", href: "https://x.test", children: [text("https://x.test")] },
    ]);
  });

  it("lexes with sticky rules, which keep it linear on JavaScriptCore", () => {
    // WKWebView and WebKitGTK run JSC, where marked's `^`-anchored rules scan
    // the rest of the input before failing: 8x the text took ~35x the time.
    // The sticky copies are what keep it linear. Checked by shape rather than
    // by timing — a timed ratio was noise on a shared CI runner — so a marked
    // upgrade that moves the rules out of reach fails here, not in the field.
    type Rules = Record<string, unknown>;
    const { rules } = (newLexer() as unknown as { tokenizer: { rules: { block: Rules; inline: Rules } } }).tokenizer;
    for (const set of [rules.block, rules.inline]) {
      const anchored = Object.values(set).filter(
        (rule): rule is RegExp => rule instanceof RegExp && rule.source.startsWith("^") && !rule.global,
      );
      expect(anchored.length).toBeGreaterThan(5);
      for (const rule of anchored) expect(rule.sticky).toBe(true);
    }
    // The ones measured slow, by name.
    for (const name of ["hr", "table", "html", "blockquote", "lheading"]) {
      expect((rules.block[name] as RegExp).sticky).toBe(true);
    }
    // Sticky, and still anchored to the start of each string it is handed.
    const hr = rules.block.hr as RegExp;
    expect(hr.exec("---\n")).not.toBeNull();
    expect(hr.exec("---\n")).not.toBeNull();
    expect(hr.exec("text\n---\n")).toBeNull();
  });

  it("reads inline code, links and emphasis", () => {
    expect(parseInline("see `Currency.scale()` in [the spec](http://x) — **now**")).toEqual([
      text("see "),
      { type: "code", value: "Currency.scale()" },
      text(" in "),
      { type: "link", href: "http://x", children: [text("the spec")] },
      text(" — "),
      { type: "strong", children: [text("now")] },
    ]);
  });

  it("leaves a snake_case identifier alone rather than reading it as emphasis", () => {
    // CommonMark disallows intraword underscore emphasis for exactly this
    // reason. Without that rule, program_config_auth read as `program`,
    // <em>config</em>, `auth` — its own underscores eaten as markup.
    expect(parseInline("column platform_client_id on program_config_auth")).toEqual([
      text("column platform_client_id on program_config_auth"),
    ]);
  });

  it("still reads underscore emphasis at a word boundary", () => {
    expect(parseInline("that is _not_ optional")).toEqual([
      text("that is "),
      { type: "em", children: [text("not")] },
      text(" optional"),
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
        text("token mẫu là "),
        { type: "secret", value: jwt },
      ]);
    });

    it("finds a token whose signature has underscores in it", () => {
      // marked ends a text run at every "_"; the pieces are joined first.
      const underscored = `${jwt.slice(0, -10)}_${jwt.slice(-9)}`;
      expect(parseInline(`a ${underscored} b`)).toEqual([
        text("a "),
        { type: "secret", value: underscored },
        text(" b"),
      ]);
    });

    it("reads a backtick-quoted token as a secret too, not as code", () => {
      expect(parseInline(`Authorization: \`${jwt}\``)).toEqual([
        text("Authorization: "),
        { type: "secret", value: jwt },
      ]);
    });

    it("leaves a short, unrelated eyJ-looking word as plain text", () => {
      // The anchor is "eyJ" plus the three-segment shape — "eyJust" alone,
      // with no dots, must never trip the mask.
      expect(parseInline("eyJust checking in")).toEqual([text("eyJust checking in")]);
    });
  });
});
