import { describe, expect, test } from "bun:test";
import { KEYS } from "platejs";
import { createPlateEditor } from "platejs/react";

import { ATTACHMENT_KEY } from "./attachments";
import { ComposerEditorKit } from "./composerEditorKit";
import { deserializeComposerValue, serializeComposerValue } from "./composerSerialization";

function setup(text: string) {
  const editor = createPlateEditor({ plugins: ComposerEditorKit });
  const types = {
    paragraph: editor.getType(KEYS.p),
    attachment: editor.getType(ATTACHMENT_KEY),
    codeBlock: editor.getType(KEYS.codeBlock),
    codeLine: editor.getType(KEYS.codeLine),
  };
  editor.tf.setValue(deserializeComposerValue(text, types) as never);
  return { editor, types };
}

/**
 * Evidence for the lost caret: Slate leaves no selection after `setValue`
 * replaces every node, and slate-react clears the DOM selection of a focused
 * field that has none (node_modules/slate-react/dist/index.js, the selection
 * layout effect: `domSelection.removeAllRanges()`).
 */
describe("replacing the composer's document from outside", () => {
  test("setValue alone leaves the editor with no selection", async () => {
    const { editor, types } = setup("/get");
    editor.tf.select({ path: [0, 0], offset: 4 });
    expect(editor.selection).not.toBeNull();
    editor.tf.setValue(deserializeComposerValue("/get /get ", types) as never);
    expect(editor.selection).toBeNull();
  });

  test("a focused field keeps a caret, at the end of the new text", async () => {
    const { editor, types } = setup("/get");
    editor.tf.select({ path: [0, 0], offset: 4 });
    const { replaceComposerValue } = await import("./externalValue");
    replaceComposerValue(editor, deserializeComposerValue("/get ", types), { focused: true });
    expect(serializeComposerValue(editor.children, types)).toBe("/get ");
    expect(editor.selection).toEqual({ anchor: { path: [0, 0], offset: 5 }, focus: { path: [0, 0], offset: 5 } });
  });

  test("a field that is not focused is not given a selection", async () => {
    const { editor, types } = setup("one");
    const { replaceComposerValue } = await import("./externalValue");
    replaceComposerValue(editor, deserializeComposerValue("two", types), { focused: false });
    expect(serializeComposerValue(editor.children, types)).toBe("two");
    expect(editor.selection).toBeNull();
  });
});
