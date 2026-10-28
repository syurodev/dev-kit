import type { PlateEditor } from "platejs/react";

/**
 * Puts a document the owner supplied (another composer's edit, a picked slash
 * command, a session switch) in place of the editor's own.
 *
 * `setValue` replaces every node, and Slate moves the selection off a removed
 * node onto nothing: afterwards `editor.selection` is null. While the field
 * still has focus that is the worst state to leave it in — slate-react's
 * selection effect sees a focused editor with no selection and clears the DOM
 * selection (`removeAllRanges`), so the caret vanishes although the field keeps
 * focus and keeps taking keystrokes. A field the person is typing in puts the
 * caret at the end of what it now says, where the text they picked ends.
 * An unfocused one is left alone: it has no caret to lose, and selecting would
 * pull the focus.
 */
export function replaceComposerValue(editor: PlateEditor, nodes: unknown, { focused }: { focused: boolean }): void {
  editor.tf.setValue(nodes as never);
  if (focused) editor.tf.select(editor.api.end([]));
}
