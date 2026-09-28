/**
 * The composer's own plain-text serializer — deliberately not
 * `editor.api.markdown.serialize()`. See composerEditorKit.ts for why: the
 * markdown serializer escapes a soft line break (Shift+Enter) as a trailing
 * backslash, which would send the agent a "\" the user never typed. Walking
 * the tree by hand keeps every character exactly as composed; a mention
 * becomes "@label" and a code block becomes an ordinary fenced block, same
 * as if either had been typed as plain text.
 *
 * Loading a string back INTO the editor is `deserializeComposerValue`, the
 * exact inverse — not `editor.api.markdown.deserialize`, which parses the
 * prompt as a markdown document and loses what markdown treats as syntax:
 * a trailing space, a leading "- " or "# ", a blank line. The composer
 * re-reads its own prompt whenever it changes from outside (a second
 * composer on the same session, a slash command, a queued prompt loaded
 * for editing), so a lossy read ate every space as it was typed.
 */

type ComposerNode = {
  text?: string;
  type?: string;
  value?: unknown;
  lang?: unknown;
  children?: ComposerNode[];
};

function plainText(node: ComposerNode, mentionType: string): string {
  if (typeof node.text === "string") return node.text;
  if (node.type === mentionType) return `@${String(node.value ?? "")}`;
  return (node.children ?? []).map((child) => plainText(child, mentionType)).join("");
}

/** The plain string a Plate document sends as the agent prompt. */
export function serializeComposerValue(
  nodes: readonly ComposerNode[],
  types: { mention: string; codeBlock: string; codeLine: string },
): string {
  return nodes
    .map((block) => {
      if (block.type === types.codeBlock) {
        const lang = block.lang ? String(block.lang) : "";
        const lines = (block.children ?? [])
          .filter((child) => child.type === types.codeLine)
          .map((line) => plainText(line, types.mention));
        return "```" + lang + "\n" + lines.join("\n") + "\n```";
      }
      return plainText(block, types.mention);
    })
    .join("\n");
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
 * closing fence the prompt never had. "@label" stays text too — it names a
 * file, but not which one.
 */
export function deserializeComposerValue(
  text: string,
  types: { paragraph: string; codeBlock: string; codeLine: string },
): ComposerNode[] {
  if (!text) return [];
  const lines = text.split("\n");
  const blocks: ComposerNode[] = [];
  let paragraph: string[] | null = null;
  const flush = () => {
    if (paragraph) blocks.push({ type: types.paragraph, children: [{ text: paragraph.join("\n") }] });
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
        children: lines.slice(i + 1, close).map((line) => ({ type: types.codeLine, children: [{ text: line }] })),
      });
      i = close;
      continue;
    }
    (paragraph ??= []).push(lines[i]);
  }
  flush();
  return blocks;
}

/**
 * Every mention's `key` (the attached file's path) currently in the
 * document — mentions are inline children of a paragraph, or of a code line
 * inside a code block, so this walks the whole tree rather than one level.
 */
export function collectMentionKeys(nodes: readonly ComposerNode[], mentionType: string): Set<string> {
  const keys = new Set<string>();
  const walk = (list: readonly ComposerNode[]) => {
    for (const node of list) {
      if (node.type === mentionType) {
        const key = (node as { key?: unknown }).key;
        if (key != null) keys.add(String(key));
      }
      if (node.children) walk(node.children);
    }
  };
  walk(nodes);
  return keys;
}
