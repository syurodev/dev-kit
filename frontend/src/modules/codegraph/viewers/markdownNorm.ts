import { createSlateEditor } from "platejs";
import { MarkdownFileEditorKit } from "./markdownEditorKit";

/**
 * What Plate writes for a piece of Markdown: read it into the tree and write it
 * back. The unit markdownSplice measures edits against. Block results are kept,
 * since a save asks for the same untouched blocks again and again.
 */
export function markdownNorm(): (markdown: string) => string {
  const editor = createSlateEditor({ plugins: MarkdownFileEditorKit, value: [] });
  const cache = new Map<string, string>();
  return (markdown) => {
    const hit = cache.get(markdown);
    if (hit !== undefined) return hit;
    // Straight through the plugin's functions: setting the editor's value instead made every call pay for removing the last one.
    const out = editor.api.markdown.serialize({ value: editor.api.markdown.deserialize(markdown) });
    if (cache.size > 2000) cache.clear();
    cache.set(markdown, out);
    return out;
  };
}
