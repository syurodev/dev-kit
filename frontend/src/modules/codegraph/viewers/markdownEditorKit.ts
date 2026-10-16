import remarkGfm from "remark-gfm";
import { MarkdownPlugin } from "@platejs/markdown";

import { BasicBlocksKit } from "@/components/editor/plugins/basic-blocks-kit";
import { BasicMarksKit } from "@/components/editor/plugins/basic-marks-kit";
import { CodeBlockKit } from "@/components/editor/plugins/code-block-kit";
import { LinkKit } from "@/components/editor/plugins/link-kit";
import { ListKit } from "@/components/editor/plugins/list-kit";
import { TableKit } from "@/components/editor/plugins/table-kit";

/**
 * Plate for a Markdown file of a folder: the blocks and marks Notes writes with,
 * and its Markdown plugin, without what belongs to the vault (note links, image
 * uploads, mermaid, slash menu). One editor for notes and for files, so a
 * heading, a list or a table behaves the same in both.
 */
export const MarkdownFileEditorKit = [
  ...BasicBlocksKit,
  ...BasicMarksKit,
  ...ListKit,
  ...LinkKit,
  ...TableKit,
  ...CodeBlockKit,
  MarkdownPlugin.configure({
    options: {
      remarkPlugins: [remarkGfm],
    },
  }),
];
