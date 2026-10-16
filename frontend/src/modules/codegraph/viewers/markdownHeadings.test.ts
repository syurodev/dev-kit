import { describe, expect, test } from "bun:test";
import { parseMarkdown } from "@/lib/agentMarkdown";
import { headingSlugs, inlineText } from "./markdownHeadings";

function idsOf(source: string): string[] {
  const blocks = parseMarkdown(source);
  const slugs = headingSlugs(blocks);
  const out: string[] = [];
  const walk = (list: ReturnType<typeof parseMarkdown>) => {
    for (const b of list) {
      if (b.type === "heading") out.push(slugs.get(b)!);
      else if (b.type === "quote") walk(b.blocks);
      else if (b.type === "list") b.items.forEach((i) => walk(i.blocks));
    }
  };
  walk(blocks);
  return out;
}

describe("heading ids of a parsed file", () => {
  test("come from the heading's text, formatting stripped", () => {
    expect(idsOf("# The **bold** `code` title\n\n## Second")).toEqual(["the-bold-code-title", "second"]);
  });

  test("repeats are numbered in reading order, including inside quotes and lists", () => {
    expect(idsOf("# Notes\n\n> ## Notes\n\n- ### Notes\n")).toEqual(["notes", "notes-1", "notes-2"]);
  });

  test("a link in a heading contributes its label, not its address", () => {
    const [block] = parseMarkdown("# See [the docs](https://example.com/x)");
    expect(block.type === "heading" ? inlineText(block.inline) : "").toBe("See the docs");
  });
});

describe("what a file may smuggle into the tree", () => {
  test("raw HTML stays text and an image becomes a link to it", () => {
    const blocks = parseMarkdown('<script>alert(1)</script>\n\n![logo](https://tracker.example/p.png)\n\n[x](javascript:alert(1))');
    const json = JSON.stringify(blocks);
    expect(json).toContain("<script>alert(1)</script>");
    expect(blocks.every((b) => b.type === "paragraph")).toBe(true);
    // The image is a link (its address is clicked, never loaded) and the javascript: link is only its text.
    expect(json).toContain('"type":"link","href":"https://tracker.example/p.png"');
    expect(json).not.toContain("javascript:alert(1)\"");
    expect(json).not.toContain('"type":"image"');
  });
});
