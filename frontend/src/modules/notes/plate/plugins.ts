import remarkGfm from "remark-gfm";
import { MarkdownPlugin } from "@platejs/markdown";
import { SlashInputPlugin, SlashPlugin } from "@platejs/slash-command/react";
import { TogglePlugin } from "@platejs/toggle/react";
import { KEYS, type SlateEditor } from "platejs";

import { BasicBlocksKit } from "@/components/editor/plugins/basic-blocks-kit";
import { BasicMarksKit } from "@/components/editor/plugins/basic-marks-kit";
import { CodeBlockKit } from "@/components/editor/plugins/code-block-kit";
import { LinkKit } from "@/components/editor/plugins/link-kit";
import { ListKit } from "@/components/editor/plugins/list-kit";
import { TableKit } from "@/components/editor/plugins/table-kit";

import { ImageElement } from "@/components/ui/media-image-node";

import { MermaidCodeBlock } from "./MermaidCodeBlock";
import { ImageUploadPlugin } from "./ImageUpload";
import { SlashInputElement } from "./SlashInput";

/**
 * CodeBlockKit's own code_block plugin renders every language with plain
 * syntax highlighting. Swap in MermaidCodeBlock only for that one plugin —
 * every other language keeps CodeBlockKit's default rendering unchanged.
 */
const codeBlockKitWithMermaid = CodeBlockKit.map((plugin) =>
  plugin.key === "code_block" ? plugin.withComponent(MermaidCodeBlock) : plugin,
);

export const NotesEditorKit = [
  ...BasicBlocksKit,
  ...BasicMarksKit,
  ...ListKit,
  ...LinkKit,
  ...TableKit,
  ...codeBlockKitWithMermaid,
  TogglePlugin,
  ImageUploadPlugin.withComponent(ImageElement),
  SlashPlugin.configure({
    options: {
      triggerQuery: (editor: SlateEditor) =>
        !editor.api.some({
          match: { type: editor.getType(KEYS.codeBlock) },
        }),
    },
  }),
  SlashInputPlugin.withComponent(SlashInputElement),
  MarkdownPlugin.configure({
    options: {
      remarkPlugins: [remarkGfm],
    },
  }),
];
