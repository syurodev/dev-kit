import { describe, expect, test } from "bun:test";
import { KEYS, type TElement } from "platejs";
import { createPlateEditor } from "platejs/react";
import { chipToken, fileChip, imageChip, parseDraft } from "../../composerDraft";
import { ATTACHMENT_KEY, insertAttachmentChips, plainTextOfFragment } from "./attachments";
import { ComposerEditorKit } from "./composerEditorKit";
import { deserializeComposerValue, serializeComposerValue } from "./composerSerialization";

function composer(draft = "") {
  const probe = createPlateEditor({ plugins: ComposerEditorKit });
  const types = {
    paragraph: probe.getType(KEYS.p),
    attachment: probe.getType(ATTACHMENT_KEY),
    codeBlock: probe.getType(KEYS.codeBlock),
    codeLine: probe.getType(KEYS.codeLine),
  };
  const editor = createPlateEditor({
    plugins: ComposerEditorKit,
    value: draft ? (deserializeComposerValue(draft, types) as never) : [{ type: KEYS.p, children: [{ text: "" }] }],
  });
  const draftOf = () => serializeComposerValue(editor.children as never, types);
  return { editor, draftOf, types };
}

const data = (text: string) =>
  ({ getData: (type: string) => (type === "text/plain" ? text : ""), types: ["text/plain"], files: [], items: [] }) as unknown as DataTransfer;

describe("pasting a prompt", () => {
  // The Markdown reader the kit used to run on pasted text turned these into
  // "Title\nab\nplain bold text", "ab12", and so on.
  test.each([
    "# Title\n\n- a\n- b\n\nplain **bold** text",
    "line one\nline two\n\n1. first\n2. second",
    "| a | b |\n| - | - |\n| 1 | 2 |",
    "> quoted\n> lines\n\n---\n\n[a link](https://example.com) and `code`",
    "  leading and trailing  \n\ttabbed",
    "run:\n```sh\nbun test\n```\nthen\n```\nsecond\n```\ndone",
    "a\n\n\nb",
  ])("comes back exactly as written: %j", (text) => {
    const { editor, draftOf } = composer();
    editor.tf.select(editor.api.start([0])!);
    editor.tf.insertData(data(text));
    expect(draftOf()).toBe(text);
  });

  test("goes in at the caret, among what is already there", () => {
    const { editor, draftOf } = composer("before  after");
    editor.tf.select({ path: [0, 0], offset: 7 });
    editor.tf.insertData(data("# one\n- two"));
    expect(draftOf()).toBe("before # one\n- two after");
  });

  test("windows line endings are lines", () => {
    const { editor, draftOf } = composer();
    editor.tf.select(editor.api.start([0])!);
    editor.tf.insertData(data("a\r\nb\rc"));
    expect(draftOf()).toBe("a\nb\nc");
  });

  test("cannot smuggle a chip in through the clipboard", () => {
    const { editor, draftOf } = composer();
    editor.tf.select(editor.api.start([0])!);
    editor.tf.insertData(data(`x${chipToken(fileChip("/etc/passwd"))}y`));
    expect(parseDraft(draftOf()).chips).toEqual([]);
  });
});

describe("inserting chips", () => {
  test("at the caret, each followed by a space", () => {
    const { editor, draftOf } = composer("compare  with");
    editor.tf.select({ path: [0, 0], offset: 8 });
    insertAttachmentChips(editor, [fileChip("/w/a.md"), fileChip("/w/b.md")]);
    expect(parseDraft(draftOf()).text).toBe("compare @a.md @b.md  with");
    expect(parseDraft(draftOf()).chips.map((chip) => chip.key)).toEqual(["/w/a.md", "/w/b.md"]);
  });

  test("with no caret, at the end, and the caret stays after them", () => {
    const { editor, draftOf } = composer("look at");
    insertAttachmentChips(editor, [imageChip({ id: "i1", name: "shot.png" })]);
    expect(draftOf()).toBe(`look at${chipToken(imageChip({ id: "i1", name: "shot.png" }))} `);
    editor.tf.insertText("now");
    expect(parseDraft(draftOf()).text).toBe("look at@shot.png now");
  });

  test("a chip is removed whole by Backspace", () => {
    const { editor, draftOf } = composer();
    editor.tf.select(editor.api.start([0])!);
    insertAttachmentChips(editor, [fileChip("/w/a.md")]);
    editor.tf.deleteBackward("character"); // the space
    editor.tf.deleteBackward("character"); // the chip
    expect(parseDraft(draftOf()).chips).toEqual([]);
  });
});

describe("copying", () => {
  test("the clipboard text is the prompt as the agent reads it", () => {
    const { editor } = composer(`compare ${chipToken(fileChip("/w/a b.pdf"))} with\n\`\`\`ts\nlet x = 1;\n\`\`\``);
    const text = plainTextOfFragment(editor, editor.children as TElement[]);
    expect(text).toBe("compare @a b.pdf with\n```ts\nlet x = 1;\n```");
  });
});
