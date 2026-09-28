/**
 * The composer's own plain-text serializer — deliberately not
 * `editor.api.markdown.serialize()`. See composerEditorKit.ts for why: the
 * markdown serializer escapes a soft line break (Shift+Enter) as a trailing
 * backslash, which would send the agent a "\" the user never typed. Walking
 * the tree by hand keeps every character exactly as composed; a mention
 * becomes "@label" and a code block becomes an ordinary fenced block, same
 * as if either had been typed as plain text.
 *
 * Deserializing (loading a plain string INTO the editor, on an external
 * reset) still goes through `editor.api.markdown.deserialize` in
 * AgentComposer.tsx — that direction has no escaping to undo, and reusing it
 * is what gives fenced code blocks back their `code_block`/`code_line`
 * shape for free.
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
