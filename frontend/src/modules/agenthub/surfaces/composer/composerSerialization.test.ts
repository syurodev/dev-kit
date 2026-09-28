import { describe, expect, it } from "bun:test";
import { collectMentionKeys, serializeComposerValue } from "./composerSerialization";

const types = { mention: "mention", codeBlock: "code_block", codeLine: "code_line" };

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
