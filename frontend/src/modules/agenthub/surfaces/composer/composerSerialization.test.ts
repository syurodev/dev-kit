import { describe, expect, it } from "bun:test";
import { chipToken, fileChip, imageChip, parseDraft } from "../../composerDraft";
import { attachmentNode, deserializeComposerValue, serializeComposerValue } from "./composerSerialization";

const types = { paragraph: "p", attachment: "attachment", codeBlock: "code_block", codeLine: "code_line" };

describe("serializeComposerValue", () => {
  it("writes plain text exactly as composed, including a soft break", () => {
    const nodes = [{ type: "p", children: [{ text: "line one\nline two" }] }];
    expect(serializeComposerValue(nodes, types)).toBe("line one\nline two");
  });

  it("writes a chip as its token, not the empty text it wraps", () => {
    const nodes = [
      {
        type: "p",
        children: [
          { text: "check " },
          attachmentNode("attachment", fileChip("/work/src/app.ts")),
          { text: " please" },
        ],
      },
    ];
    const draft = serializeComposerValue(nodes, types);
    expect(draft).toBe(`check ${chipToken(fileChip("/work/src/app.ts"))} please`);
    expect(parseDraft(draft).text).toBe("check @app.ts please");
  });

  it("writes an image and a chip inside a code line too", () => {
    const nodes = [
      {
        type: "code_block",
        lang: "",
        children: [
          { type: "code_line", children: [{ text: "see " }, attachmentNode("attachment", imageChip({ id: "i1", name: "a.png" }))] },
        ],
      },
    ];
    expect(serializeComposerValue(nodes, types)).toBe("```\nsee " + chipToken(imageChip({ id: "i1", name: "a.png" })) + "\n```");
  });

  it("cannot be made to write a token by typing the delimiters", () => {
    const nodes = [{ type: "p", children: [{ text: "a\uE000file:x|y\uE001b" }] }];
    expect(parseDraft(serializeComposerValue(nodes, types)).chips).toEqual([]);
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
    `compare ${chipToken(fileChip("/work/a b (1).pdf"))} with ${chipToken(fileChip("/work/c.md"))}`,
    `${chipToken(fileChip("/work/a.md"))}${chipToken(fileChip("/work/b.md"))}`,
    `${chipToken(imageChip({ id: "i1", name: "shot.png" }))} what is wrong here?`,
    "before\n```ts\nconst x = 1; " + chipToken(fileChip("/w/a.ts")) + "\n```\nafter",
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

  it("reads a token as an attachment node between text nodes", () => {
    expect(deserializeComposerValue(`see ${chipToken(fileChip("/w/a.ts"))}`, types)).toEqual([
      {
        type: "p",
        children: [
          { text: "see " },
          { type: "attachment", kind: "file", key: "/w/a.ts", value: "a.ts", children: [{ text: "" }] },
          { text: "" },
        ],
      },
    ]);
  });

  it("puts empty text between two chips and around a lone one", () => {
    const [paragraph] = deserializeComposerValue(`${chipToken(fileChip("/a"))}${chipToken(fileChip("/b"))}`, types);
    expect(paragraph.children?.map((child) => child.type ?? "text")).toEqual(["text", "attachment", "text", "attachment", "text"]);
  });

  it("reads an empty prompt as an empty document", () => {
    expect(deserializeComposerValue("", types)).toEqual([]);
  });
});
