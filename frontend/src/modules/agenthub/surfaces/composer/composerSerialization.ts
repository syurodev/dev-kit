/**
 * The composer's own serializer — deliberately not
 * `editor.api.markdown.serialize()`. See composerEditorKit.ts for why: the
 * markdown serializer escapes a soft line break (Shift+Enter) as a trailing
 * backslash, which would send the agent a "\" the user never typed. Walking
 * the tree by hand keeps every character exactly as composed, and a code block
 * becomes an ordinary fenced block, same as if it had been typed as plain text.
 *
 * What this writes is the DRAFT, not the prompt: an attached file, image or
 * reference — a chip in the field — is written as a token (composerDraft.ts)
 * so the draft keeps what it stands for. `parseDraft` turns the draft into what
 * the agent reads, where a chip is "@label".
 *
 * Loading a string back INTO the editor is `deserializeComposerValue`, the
 * exact inverse — not `editor.api.markdown.deserialize`, which parses the
 * prompt as a markdown document and loses what markdown treats as syntax:
 * a trailing space, a leading "- " or "# ", a blank line. The composer
 * re-reads its prompt whenever it changes from outside (a second
 * composer on the same session, a slash command, a queued prompt loaded
 * for editing), so a lossy read ate every space as it was typed.
 */

import { chipToken, scrubText, splitDraft, type ChipKind } from "../../composerDraft";

type ComposerNode = {
  text?: string;
  type?: string;
  value?: unknown;
  kind?: unknown;
  key?: unknown;
  lang?: unknown;
  children?: ComposerNode[];
};

/** The attachment chip's node: void and inline, empty text inside. */
export function attachmentNode(type: string, chip: { kind: ChipKind; key: string; label: string }): ComposerNode {
  return { type, kind: chip.kind, key: chip.key, value: chip.label, children: [{ text: "" }] };
}

function draftText(node: ComposerNode, attachmentType: string): string {
  if (typeof node.text === "string") return scrubText(node.text);
  if (node.type === attachmentType) {
    const key = String(node.key ?? "");
    return chipToken({ kind: (node.kind as ChipKind) ?? "file", key, label: String(node.value ?? key) });
  }
  return (node.children ?? []).map((child) => draftText(child, attachmentType)).join("");
}

/** The draft string a Plate document holds: its text, its chips as tokens. */
export function serializeComposerValue(
  nodes: readonly ComposerNode[],
  types: { attachment: string; codeBlock: string; codeLine: string },
): string {
  return nodes
    .map((block) => {
      if (block.type === types.codeBlock) {
        const lang = block.lang ? String(block.lang) : "";
        const lines = (block.children ?? [])
          .filter((child) => child.type === types.codeLine)
          .map((line) => draftText(line, types.attachment));
        return "```" + lang + "\n" + lines.join("\n") + "\n```";
      }
      return draftText(block, types.attachment);
    })
    .join("\n");
}

/**
 * A run of draft text as a block's children: its words, and each chip as an
 * attachment node. Slate wants a text node at either end of an inline and
 * between two of them, so those are written out (empty ones add nothing to the
 * string). A run with no chips is the single text node it always was.
 */
function inlineChildren(text: string, attachmentType: string): ComposerNode[] {
  const out: ComposerNode[] = [];
  for (const part of splitDraft(text)) {
    if (part.type === "text") {
      out.push({ text: part.text });
      continue;
    }
    if (out.length === 0 || out[out.length - 1].type) out.push({ text: "" });
    out.push(attachmentNode(attachmentType, part.chip));
  }
  if (out.length === 0 || out[out.length - 1].type) out.push({ text: "" });
  return out;
}

const FENCE_OPEN = /^```(\S*)$/;

/**
 * The Plate document for a plain prompt string — the inverse of
 * `serializeComposerValue`, so that serializing the result gives back the
 * same string character for character. A fenced block (an opening ```lang
 * line, at least one line, a closing ``` line) becomes a code block; every
 * run of text between them is one paragraph whose line breaks are soft
 * breaks, the way Shift+Enter writes them. A fence that is never closed, or
 * closes on nothing, stays text: as a code block it would serialize with a
 * closing fence the prompt never had. "@label" typed by hand stays text too —
 * it names a file, but not which one; only a chip's token says that.
 */
export function deserializeComposerValue(
  text: string,
  types: { paragraph: string; attachment: string; codeBlock: string; codeLine: string },
): ComposerNode[] {
  if (!text) return [];
  const lines = text.split("\n");
  const blocks: ComposerNode[] = [];
  let paragraph: string[] | null = null;
  const flush = () => {
    if (paragraph) blocks.push({ type: types.paragraph, children: inlineChildren(paragraph.join("\n"), types.attachment) });
    paragraph = null;
  };
  for (let i = 0; i < lines.length; i++) {
    const open = FENCE_OPEN.exec(lines[i]);
    const close = open ? lines.indexOf("```", i + 1) : -1;
    if (open && close > i + 1) {
      flush();
      blocks.push({
        type: types.codeBlock,
        lang: open[1],
        children: lines
          .slice(i + 1, close)
          .map((line) => ({ type: types.codeLine, children: inlineChildren(line, types.attachment) })),
      });
      i = close;
      continue;
    }
    (paragraph ??= []).push(lines[i]);
  }
  flush();
  return blocks;
}
