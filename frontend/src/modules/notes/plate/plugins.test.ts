import { describe, expect, test } from "bun:test";
import { KEYS } from "platejs";
import { createPlateEditor } from "platejs/react";
import { MermaidCodeBlock, mermaidSource } from "./MermaidCodeBlock";
import { NotesEditorKit } from "./plugins";

describe("NotesEditorKit", () => {
  // CodeBlockKit sets its component with configure(), which Plate applies after
  // extensions: a withComponent() override lost to it and every mermaid block
  // was drawn as source. The editor's resolved plugin is what renders.
  test("renders code blocks with MermaidCodeBlock", () => {
    const editor = createPlateEditor({ plugins: NotesEditorKit });
    const plugin = editor.getPlugin({ key: KEYS.codeBlock });
    expect(plugin.node.component).toBe(MermaidCodeBlock);
  });

  test("reads a mermaid block back with its line breaks", () => {
    const editor = createPlateEditor({ plugins: NotesEditorKit });
    const [block] = editor.api.markdown.deserialize("```mermaid\nflowchart LR\n  A --> B\n```");
    expect(block.type).toBe(KEYS.codeBlock);
    expect(mermaidSource(block as never)).toBe("flowchart LR\n  A --> B");
  });
});
