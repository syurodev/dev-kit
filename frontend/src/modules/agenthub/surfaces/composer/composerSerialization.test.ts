import { describe, expect, it } from "bun:test";
import { collectMentionKeys, deserializeComposerValue, serializeComposerValue } from "./composerSerialization";

const types = { paragraph: "p", mention: "mention", codeBlock: "code_block", codeLine: "code_line" };

describe("serializeComposerValue", () => {
  it("writes plain text exactly as composed, including a soft break", () => {
    const nodes = [{ type: "p", children: [{ text: "line one\nline two" }] }];
    expect(serializeComposerValue(nodes, types)).toBe("line one\nline two");
  });

  it("writes a mentioned file as @label, not the empty text it wraps", () => {
    const nodes = [
      {
        type: "p",
        children: [
          { text: "check " },
          { type: "mention", key: "src/app.ts", value: "app.ts", children: [{ text: "" }] },
          { text: " please" },
        ],
      },
    ];
    expect(serializeComposerValue(nodes, types)).toBe("check @app.ts please");
  });

  it("writes a code block as an ordinary fenced block, language and all", () => {
    const nodes = [
      {
        type: "code_block",
        lang: "ts",
        children: [
          { type: "code_line", children: [{ text: "const x = 1;" }] },
          { type: "code_line", children: [{ text: "console.log(x);" }] },
        ],
      },
    ];
    expect(serializeComposerValue(nodes, types)).toBe("```ts\nconst x = 1;\nconsole.log(x);\n```");
  });

  it("joins multiple blocks (e.g. text around a pasted code block) with a single newline", () => {
    const nodes = [
      { type: "p", children: [{ text: "before" }] },
      { type: "code_block", lang: "", children: [{ type: "code_line", children: [{ text: "code" }] }] },
      { type: "p", children: [{ text: "after" }] },
    ];
    expect(serializeComposerValue(nodes, types)).toBe("before\n```\ncode\n```\nafter");
  });
});

describe("deserializeComposerValue", () => {
  // The composer re-reads its prompt whenever it changes from outside — most
  // often a second composer on the same session. A lossy read there (markdown
  // dropping a trailing space) was echoed back and ate every space typed.
  it.each([
    "hello ",
    " leading",
    "hello  world",
    "a\nb",
    "a\n\nb",
    "trailing newline\n",
    "- not a list",
    "1. not a list",
    "# not a heading",
    "*",
    "> not a quote",
    "check @app.ts please",
    "before\n```ts\nconst x = 1;\n\nconsole.log(x);\n```\nafter",
    "```\ncode\n```",
    "```\na\n```\n\n```\nb\n```",
    "```ts\nnever closed",
    "```\n```",
    "`inline` and ``` mid-line",
  ])("reads %j back exactly as it was written", (text) => {
    expect(serializeComposerValue(deserializeComposerValue(text, types), types)).toBe(text);
  });

  it("reads a fenced block as a code block, language and all", () => {
    expect(deserializeComposerValue("hi\n```go\nx := 1\n```", types)).toEqual([
      { type: "p", children: [{ text: "hi" }] },
      { type: "code_block", lang: "go", children: [{ type: "code_line", children: [{ text: "x := 1" }] }] },
    ]);
  });

  it("reads an empty prompt as an empty document", () => {
    expect(deserializeComposerValue("", types)).toEqual([]);
  });
});

describe("collectMentionKeys", () => {
  it("finds every mention's key, nested inside a code line or not", () => {
    const nodes = [
      {
        type: "p",
        children: [
          { type: "mention", key: "a.ts", value: "a.ts", children: [{ text: "" }] },
          { text: " and " },
          { type: "mention", key: "b.ts", value: "b.ts", children: [{ text: "" }] },
        ],
      },
    ];
    expect(collectMentionKeys(nodes, "mention")).toEqual(new Set(["a.ts", "b.ts"]));
  });

  it("finds nothing in a document with no mentions", () => {
    expect(collectMentionKeys([{ type: "p", children: [{ text: "hi" }] }], "mention")).toEqual(new Set());
  });
});
