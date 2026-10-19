import { KEYS, type TElement } from "platejs";
import { createPlatePlugin, PlateElement, type PlateEditor, type PlateElementProps } from "platejs/react";
import { parseDraft, scrubText, type ChipKind, type DraftChip } from "../../composerDraft";
import { AttachmentChip } from "./AttachmentChip";
import { attachmentNode, deserializeComposerValue, serializeComposerValue } from "./composerSerialization";

/** The node type of a chip in the composer's document. */
export const ATTACHMENT_KEY = "attachment";

/** A chip is a void inline node: Slate treats it as one unit that Backspace removes whole. */
export function AttachmentElement(props: PlateElementProps) {
  const { editor, element } = props;
  const node = element as { kind?: ChipKind; key?: unknown; value?: unknown };
  const key = String(node.key ?? "");
  const chip: DraftChip = { kind: node.kind ?? "file", key, label: String(node.value ?? key) };
  return (
    <PlateElement {...props} as="span">
      <AttachmentChip
        chip={chip}
        onRemove={() => {
          const at = editor.api.findPath(element);
          if (at) editor.tf.removeNodes({ at });
          editor.tf.focus();
        }}
      />
      {props.children}
    </PlateElement>
  );
}

function types(editor: PlateEditor) {
  return {
    paragraph: editor.getType(KEYS.p),
    attachment: editor.getType(ATTACHMENT_KEY),
    codeBlock: editor.getType(KEYS.codeBlock),
    codeLine: editor.getType(KEYS.codeLine),
  };
}

/**
 * Puts chips at the caret, each followed by a space so the next word is not
 * glued to a name ("@schema.sqlplease"). With no caret — the field was never
 * clicked — at the end.
 */
export function insertAttachmentChips(editor: PlateEditor, chips: readonly DraftChip[]) {
  if (chips.length === 0) return;
  if (!editor.selection) editor.tf.focus({ edge: "end" });
  const type = editor.getType(ATTACHMENT_KEY);
  for (const chip of chips) {
    editor.tf.insertNodes(attachmentNode(type, chip) as TElement);
    // Past the void, so the space and the next word go after it.
    editor.tf.move({ unit: "offset" });
    editor.tf.insertText(" ");
  }
}

/** What the clipboard's plain text says for a selection: the prompt as the agent would read it. */
export function plainTextOfFragment(editor: PlateEditor, fragment: readonly TElement[]): string {
  return parseDraft(serializeComposerValue(fragment as never, types(editor))).text;
}

/**
 * The composer's chips, and what copying and pasting do with them.
 *
 * Copy puts the prompt as the agent would read it on the clipboard as text
 * ("@name" for a chip), so it pastes sensibly anywhere; the editor's own
 * fragment still rides along, so pasting into another composer — the rail's,
 * another session's — brings the files with it.
 *
 * Paste of plain text goes in exactly as it is. This replaces the Markdown
 * reader the kit used to carry for pasted text, which read a prompt as a
 * document: "# Title\n- a\n- b" became "Titleab" (the heading and list markers
 * were nodes the composer has no plugin for, and are not written back), a
 * table became its cell text run together, `**bold**` lost its asterisks. A
 * prompt has to come back the way it was written, so a paste is read by
 * `deserializeComposerValue`, the exact inverse of what is sent: a fenced block
 * becomes a code block, everything else is text.
 */
export const AttachmentPlugin = createPlatePlugin({
  key: ATTACHMENT_KEY,
  node: { isElement: true, isInline: true, isVoid: true, component: AttachmentElement },
}).overrideEditor(({ editor, tf: { setFragmentData, insertTextData } }) => ({
  transforms: {
    setFragmentData(data, originEvent) {
      setFragmentData(data, originEvent);
      if (originEvent === "drag") return;
      const fragment = editor.api.fragment() as TElement[] | undefined;
      if (fragment?.length) data.setData("text/plain", plainTextOfFragment(editor, fragment));
    },
    insertTextData(data) {
      const text = data.getData("text/plain");
      if (!text) return insertTextData(data);
      const blocks = deserializeComposerValue(scrubText(text.replace(/\r\n?/g, "\n")), types(editor));
      if (blocks.length === 1 && blocks[0].type === editor.getType(KEYS.p)) {
        editor.tf.insertText(scrubText(text.replace(/\r\n?/g, "\n")));
      } else {
        editor.tf.insertFragment(blocks as never);
      }
      return true;
    },
  },
}));
