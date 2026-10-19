import { convertNodesDeserialize, convertNodesSerialize, defaultRules, type MdRules } from "@platejs/markdown";
import { KEYS } from "platejs";
import { calloutVariant } from "./callout";

/**
 * How the blocks only a note has are written as Markdown. A note is stored as
 * Markdown, so each must come back from it exactly, and read sensibly in any
 * other Markdown reader:
 *
 * - Callout  ⇄ a GitHub alert, `> [!NOTE]` … (NOTE, TIP, IMPORTANT, WARNING, CAUTION).
 * - Table of contents ⇄ the line `[TOC]` (Typora, VS Code and GitLab's `[[_TOC_]]` family).
 * - Math and Mermaid are ordinary ``` fences (`math`, `mermaid`); GitHub draws both.
 */

const ALERT_MARKER = /^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION)\][ \t]*(?:\r?\n)?/i;

type MdNode = { type: string; value?: string; children?: MdNode[] };

/** A blockquote whose first line is an alert marker: its type and the blocks after the marker. */
function alertOf(blockquote: MdNode) {
  const [first, ...others] = blockquote.children ?? [];
  if (first?.type !== "paragraph") return null;
  const [lead, ...tail] = first.children ?? [];
  if (lead?.type !== "text") return null;
  const marker = ALERT_MARKER.exec(lead.value ?? "");
  if (!marker) return null;
  const remainder = (lead.value ?? "").slice(marker[0].length);
  const inline = [...(remainder ? [{ ...lead, value: remainder }] : []), ...tail];
  return {
    variant: calloutVariant(marker[1]!.toLowerCase()),
    blocks: [...(inline.length ? [{ ...first, children: inline }] : []), ...others],
  };
}

function isBlank(block: MdNode | undefined) {
  return (
    block?.type === "paragraph" &&
    (block.children ?? []).every((child) => child.type === "text" && !(child.value ?? "").replace(/\u200B/g, "").trim())
  );
}

function isTocMarker(paragraph: MdNode) {
  const [only, ...rest] = paragraph.children ?? [];
  return rest.length === 0 && only?.type === "text" && only.value?.trim().toLowerCase() === "[toc]";
}

export const notesMarkdownRules: MdRules = {
  blockquote: {
    deserialize: (node, deco, options) => {
      const alert = alertOf(node as unknown as MdNode);
      if (!alert) return defaultRules.blockquote!.deserialize!(node, deco, options);
      const children = convertNodesDeserialize(alert.blocks as never, deco, options);
      return {
        type: options.editor!.getType(KEYS.callout),
        variant: alert.variant,
        children: children.length ? children : [{ type: options.editor!.getType(KEYS.p), children: [{ text: "" }] }],
      };
    },
  },
  callout: {
    serialize: (node, options) => {
      const variant = calloutVariant((node as { variant?: unknown }).variant);
      const marker = { type: "html", value: `[!${variant.toUpperCase()}]` };
      const serialized = convertNodesSerialize(node.children, options) as MdNode[];
      // An empty paragraph serializes as a zero-width space, which would sit
      // after the marker as an invisible line: leave it out.
      const [first, ...rest] = isBlank(serialized[0]) ? serialized.slice(1) : serialized;
      // The first paragraph shares the marker's line (`> [!NOTE]` / `> text`), the
      // way GitHub's own examples are written.
      if (first?.type === "paragraph") {
        const inline = first.children ?? [];
        const lead = inline.length ? [marker, { type: "text", value: "\n" }] : [marker];
        return { type: "blockquote", children: [{ ...first, children: [...lead, ...inline] }, ...rest] } as never;
      }
      return {
        type: "blockquote",
        children: [{ type: "paragraph", children: [marker] }, ...(first ? [first] : []), ...rest],
      } as never;
    },
  },
  p: {
    deserialize: (node, deco, options) => {
      if (isTocMarker(node as unknown as MdNode)) {
        return { type: options.editor!.getType(KEYS.toc), children: [{ text: "" }] };
      }
      return defaultRules.p!.deserialize!(node, deco, options);
    },
  },
  toc: { serialize: () => ({ type: "html", value: "[TOC]" }) as never },
};
