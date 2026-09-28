import remarkGfm from "remark-gfm";
import { MarkdownPlugin } from "@platejs/markdown";
import { MentionPlugin } from "@platejs/mention/react";
import { ParagraphPlugin } from "platejs/react";

import { CodeBlockKit } from "@/components/editor/plugins/code-block-kit";
import { ParagraphElement } from "@/components/ui/paragraph-node";
import { ContextMentionElement } from "./ContextMention";

/**
 * The composer's own tiny Plate kit — a paragraph, the code block CodeGraph
 * already ships (`CodeBlockKit`, unmodified — the same ` ``` ` markdown
 * input rule and lowlight highlighting Notes uses), and an "@" file mention.
 * Not NotesEditorKit: a chat prompt has no headings, lists, tables or
 * toggles, and pulling in Notes' full ~2200-line document kit for two
 * plugins would make the composer a rich-text document editor it isn't.
 *
 * MarkdownPlugin reads pasted text; it is NOT how the composer loads its own
 * prompt string (`deserializeComposerValue`, composerSerialization.ts) —
 * markdown drops a trailing space, a leading "- " and blank lines, and the
 * composer re-reads its prompt whenever it changes from outside. The
 * outgoing direction is `serializeComposerValue` (composerSerialization.ts),
 * not `editor.api.markdown.serialize()`: the markdown serializer escapes a
 * soft line break as a trailing backslash (commonmark's hard-break syntax),
 * which would put a literal "\" into a message that never had one — fine
 * for a document that round-trips through markdown, wrong for a prompt that
 * has to leave exactly as typed.
 */
export const ComposerEditorKit = [
  ParagraphPlugin.withComponent(ParagraphElement),
  ...CodeBlockKit,
  // The chip only. "@" never opens Plate's combobox: that combobox types into
  // an <input> of its own, and moving focus there mid-composition lost the
  // letter an input method was composing (mentionQuery.ts). AgentComposer
  // reads "@query" off the text before the caret and lists the files itself,
  // with the field keeping focus.
  // A space after the chip at the end of a line, so the next word is not
  // glued to the file name in what is sent ("@schema.sqlplease").
  MentionPlugin.configure({ options: { triggerQuery: () => false, insertSpaceAfterMention: true } }).withComponent(
    ContextMentionElement,
  ),
  MarkdownPlugin.configure({
    options: {
      remarkPlugins: [remarkGfm],
      rules: {
        // Deserialize-only in practice (nothing parses "@label" back into a
        // mention — that would need a real remark syntax extension, and
        // there's no evidence Notes' own "[[" note links have one either;
        // see NoteLink.tsx). Kept so `editor.api.markdown` never throws on
        // a document that happens to contain one. serializeComposerValue is
        // what actually runs on send.
        mention: {
          serialize: (node: { value?: unknown }) => ({
            type: "text" as const,
            value: `@${String(node.value ?? "")}`,
          }),
        },
      },
    },
  }),
];
