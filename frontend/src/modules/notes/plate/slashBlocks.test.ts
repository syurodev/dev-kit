import { describe, expect, test } from "bun:test";
import { KEYS } from "platejs";
import { createPlateEditor } from "platejs/react";
import { NotesEditorKit } from "./plugins";
import { slashItems } from "./SlashInput";

/** An editor holding the one empty paragraph a "/query" leaves behind. */
function emptyNote() {
  const editor = createPlateEditor({ plugins: NotesEditorKit, value: [{ type: KEYS.p, children: [{ text: "" }] }] });
  editor.tf.select(editor.api.start([0])!);
  return editor;
}

function choose(editor: ReturnType<typeof emptyNote>, label: string) {
  slashItems().find((item) => item.label === label)!.onSelect(editor);
}

const types = (editor: ReturnType<typeof emptyNote>): string[] => editor.children.map((node) => String(node.type));

describe("slash menu: technical docs", () => {
  test("offers the new blocks in their own group", () => {
    const tech = slashItems().filter((item) => item.group === "tech").map((item) => item.label);
    expect(tech).toEqual(["Callout", "Mermaid diagram", "Math equation", "Table of contents"]);
  });

  test("Callout wraps the empty paragraph, with the caret inside", () => {
    const editor = emptyNote();
    choose(editor, "Callout");
    expect(types(editor)).toEqual([KEYS.callout]);
    expect(editor.children[0]).toMatchObject({ variant: "note", children: [{ type: KEYS.p }] });
    editor.tf.insertText("Hello");
    expect(editor.api.markdown.serialize().trim()).toBe("> [!NOTE]\n> Hello");
  });

  test("Enter on the callout's empty last line leaves it", () => {
    const editor = emptyNote();
    choose(editor, "Callout");
    editor.tf.insertText("Hello");
    editor.tf.insertBreak();
    expect((editor.children[0] as { children: unknown[] }).children).toHaveLength(2);
    editor.tf.insertBreak();
    expect(types(editor)).toEqual([KEYS.callout, KEYS.p]);
    expect((editor.children[0] as { children: unknown[] }).children).toHaveLength(1);
    editor.tf.insertText("after");
    expect(editor.api.markdown.serialize().trim()).toBe("> [!NOTE]\n> Hello\n\nafter");
  });

  test("Backspace at the start of an empty callout takes it away", () => {
    const editor = emptyNote();
    choose(editor, "Callout");
    editor.tf.deleteBackward();
    expect(types(editor)).toEqual([KEYS.p]);
  });

  test("Mermaid diagram replaces the empty paragraph with a code block open on its source", () => {
    const editor = emptyNote();
    choose(editor, "Mermaid diagram");
    expect(types(editor)).toEqual([KEYS.codeBlock]);
    expect(editor.children[0]).toMatchObject({ lang: "mermaid", autoEdit: true });
    expect(editor.api.markdown.serialize().trim()).toStartWith("```mermaid\nflowchart LR\n");
  });

  test("Math equation is a math fence", () => {
    const editor = emptyNote();
    choose(editor, "Math equation");
    expect(editor.api.markdown.serialize().trim()).toBe("```math\nE = mc^2\n```");
  });

  test("Table of contents is followed by a paragraph to type in", () => {
    const editor = emptyNote();
    choose(editor, "Table of contents");
    expect(types(editor)).toEqual([KEYS.toc, KEYS.p]);
    expect(editor.api.markdown.serialize()).toStartWith("[TOC]");
  });

  test("a block chosen after text goes after that block, not over it", () => {
    const editor = createPlateEditor({ plugins: NotesEditorKit, value: [{ type: KEYS.p, children: [{ text: "keep me" }] }] });
    editor.tf.select(editor.api.end([0])!);
    choose(editor, "Mermaid diagram");
    expect(editor.api.markdown.serialize().trim()).toStartWith("keep me\n\n```mermaid");
  });
});
