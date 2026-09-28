import remarkGfm from "remark-gfm";
import { MarkdownPlugin } from "@platejs/markdown";
import { MentionInputPlugin, MentionPlugin } from "@platejs/mention/react";
import { KEYS, type SlateEditor } from "platejs";
import { ParagraphPlugin } from "platejs/react";

import { CodeBlockKit } from "@/components/editor/plugins/code-block-kit";
import { ParagraphElement } from "@/components/ui/paragraph-node";
import { ContextMentionElement, ContextMentionInputElement } from "./ContextMention";

/**
 * The composer's own tiny Plate kit — a paragraph, the code block CodeGraph
 * already ships (`CodeBlockKit`, unmodified — the same ` ``` ` markdown
 * input rule and lowlight highlighting Notes uses), and an "@" file mention.
 * Not NotesEditorKit: a chat prompt has no headings, lists, tables or
 * toggles, and pulling in Notes' full ~2200-line document kit for two
 * plugins would make the composer a rich-text document editor it isn't.
 *
 * MarkdownPlugin is used only to DESERIALIZE (load a plain string into the
 * editor on an external reset — see AgentComposer's `lastEmittedRef`). The
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
  MentionPlugin.configure({
    options: {
      // Same guard SlashPlugin uses in Notes: don't open a combobox mid-code.
      triggerQuery: (editor: SlateEditor) =>
        !editor.api.some({ match: { type: editor.getType(KEYS.codeBlock) } }),
    },
  }).withComponent(ContextMentionElement),
  MentionInputPlugin.withComponent(ContextMentionInputElement),
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
