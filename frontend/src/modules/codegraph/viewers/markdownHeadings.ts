import type { InlineToken, MarkdownBlock } from "@/modules/agenthub/transcript/agentMarkdown";
import { uniqueSlugs } from "./markdownLinks";

/** A heading's id by the block that holds it, so a block's render can look its id up. */
export type Slugs = WeakMap<MarkdownBlock, string>;

/** Plain text of inline tokens — a heading's id, a table column's label. */
export function inlineText(tokens: InlineToken[]): string {
  return tokens
    .map((t) => {
      switch (t.type) {
        case "text":
        case "code":
        case "secret":
          return t.value;
        case "break":
          return " ";
        default:
          return inlineText(t.children);
      }
    })
    .join("");
}

/** Every heading of the file, however deep, mapped to its GitHub-style id in reading order. */
export function headingSlugs(blocks: MarkdownBlock[]): Slugs {
  const headings: Array<Extract<MarkdownBlock, { type: "heading" }>> = [];
  const walk = (list: MarkdownBlock[]) => {
    for (const b of list) {
      if (b.type === "heading") headings.push(b);
      else if (b.type === "quote") walk(b.blocks);
      else if (b.type === "list") b.items.forEach((item) => walk(item.blocks));
    }
  };
  walk(blocks);
  const ids = uniqueSlugs(headings.map((h) => inlineText(h.inline)));
  const map: Slugs = new WeakMap();
  headings.forEach((h, i) => map.set(h, ids[i]));
  return map;
}
