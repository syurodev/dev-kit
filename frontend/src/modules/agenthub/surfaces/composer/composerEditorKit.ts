import { ParagraphPlugin } from "platejs/react";

import { CodeBlockKit } from "@/components/editor/plugins/code-block-kit";
import { ParagraphElement } from "@/components/ui/paragraph-node";
import { AttachmentPlugin } from "./attachments";

/**
 * The composer's own tiny Plate kit — a paragraph, the code block CodeGraph
 * already ships (`CodeBlockKit`, unmodified — the same ` ``` ` markdown
 * input rule and lowlight highlighting Notes uses), and the chips a file, an
 * image or a reference becomes (AttachmentPlugin). Not NotesEditorKit: a chat
 * prompt has no headings, lists, tables or toggles, and pulling in Notes' full
 * ~2200-line document kit for two plugins would make the composer a rich-text
 * document editor it isn't.
 *
 * No MarkdownPlugin. The composer reads and writes its prompt with its own pair
 * (`deserializeComposerValue`, `serializeComposerValue`, composerSerialization.ts),
 * never as markdown: markdown drops a trailing space, a leading "- " and blank
 * lines, and the markdown serializer escapes a soft line break (Shift+Enter) as
 * a trailing backslash, which would put a literal "\" into a message that never
 * had one — fine for a document that round-trips through markdown, wrong for a
 * prompt that has to leave exactly as typed. A markdown reader on paste did the
 * same damage in the other direction (see AttachmentPlugin).
 *
 * "@" never opens a Plate combobox: that combobox types into an <input> of its
 * own, and moving focus there mid-composition lost the letter an input method
 * was composing (mentionQuery.ts). AgentComposer reads "@query" off the text
 * before the caret and lists the files itself, with the field keeping focus.
 */
export const ComposerEditorKit = [
  ParagraphPlugin.withComponent(ParagraphElement),
  ...CodeBlockKit,
  AttachmentPlugin,
];
